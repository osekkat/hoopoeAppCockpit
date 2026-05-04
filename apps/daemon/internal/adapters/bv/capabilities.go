// capabilities.go — bv.* capability declarations for the
// `internal/capabilities` registry per §2.8.
//
// Adapter contract tests assert capabilities, not just parser success
// (§18.3). A fixture that parses but cannot satisfy the operation is
// `degraded` not `ok`. The Probe function below runs each robot
// command against the live binary and reports per-capability status.
package bv

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
)

// Capability IDs declared by this adapter (mirrors plan.md §2.8 list +
// hp-r33 capability IDs). Each ID is what the renderer and tending
// jobs gate on when deciding whether the bv-backed feature is
// available.
const (
	CapTriage   = "bv.robot.triage"
	CapPlan     = "bv.robot.plan"
	CapInsights = "bv.robot.insights"
	CapDiff     = "bv.robot.diff"
	CapNext     = "bv.robot.next"
)

// AllCapabilityIDs returns every capability this adapter probes.
// Used by the registry initialization to enumerate what to ask.
func AllCapabilityIDs() []string {
	return []string{CapTriage, CapPlan, CapInsights, CapDiff, CapNext}
}

// CapabilityStatus mirrors the wire enum from packages/schemas (5-valued
// per WhiteCreek's hp-r33 delta 1: ok, degraded, missing, blocked-by-policy,
// untested). Re-declared here so the adapter doesn't import the
// capabilities package (which would create a cycle once the registry
// imports this adapter).
type CapabilityStatus string

const (
	StatusOK              CapabilityStatus = "ok"
	StatusDegraded        CapabilityStatus = "degraded"
	StatusMissing         CapabilityStatus = "missing"
	StatusBlockedByPolicy CapabilityStatus = "blocked-by-policy"
	StatusUntested        CapabilityStatus = "untested"
)

// CapabilityReport is one capability's probe result. The capabilities
// registry maps these into its own ToolReport shape via a thin adapter.
type CapabilityReport struct {
	ID        string
	Status    CapabilityStatus
	Notes     string
	Transport string
}

// ProbeResult is the full probe output — one report per capability ID
// plus the bv binary version + when the probe ran.
type ProbeResult struct {
	Tool        string             // always "bv" — included for registry adapter convenience.
	Version     string             // bv --version output, parsed; empty if --version unavailable.
	Source      string             // "CLI" — distinguishes from fixture-based reports.
	LastChecked time.Time
	Reports     map[string]CapabilityReport
}

// Probe runs each robot command against the live bv binary and reports
// per-capability status. A short timeout (per call) keeps a hung bv
// from hanging the registry init.
//
// Probe never blocks the registry on `degraded` results — those still
// allow features to render with their fallback. `missing` is the only
// status that gates a feature out entirely.
func Probe(ctx context.Context, client *Client, now func() time.Time) ProbeResult {
	if now == nil {
		now = time.Now
	}
	res := ProbeResult{
		Tool:        "bv",
		Source:      "CLI",
		LastChecked: now().UTC(),
		Reports:     make(map[string]CapabilityReport, 5),
	}

	res.Version = probeVersion(ctx, client)

	// Triage — the canonical entry point. If this works, the others
	// usually do too; if it fails the whole adapter is degraded.
	res.Reports[CapTriage] = probeOne(ctx, CapTriage, "--robot-triage", func(ctx context.Context) error {
		_, err := client.Triage(ctx)
		return err
	})

	res.Reports[CapPlan] = probeOne(ctx, CapPlan, "--robot-plan", func(ctx context.Context) error {
		_, err := client.Plan(ctx, "")
		return err
	})

	res.Reports[CapInsights] = probeOne(ctx, CapInsights, "--robot-insights", func(ctx context.Context) error {
		_, err := client.Insights(ctx)
		return err
	})

	res.Reports[CapDiff] = probeOne(ctx, CapDiff, "--robot-diff --diff-since HEAD", func(ctx context.Context) error {
		_, err := client.Diff(ctx, "HEAD")
		return err
	})

	res.Reports[CapNext] = probeOne(ctx, CapNext, "--robot-next", func(ctx context.Context) error {
		_, err := client.Next(ctx)
		return err
	})

	return res
}

// probeOne runs a single capability probe with a per-call timeout.
// Returns CapabilityReport with the appropriate status:
//   - ok if the call returns nil error.
//   - missing if the binary is missing (exec ENOENT-style).
//   - degraded if the call errored but for a recoverable reason.
//   - blocked-by-policy if the wrapper refused (Guardrail 1).
func probeOne(ctx context.Context, id, summary string, call func(context.Context) error) CapabilityReport {
	probeCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	err := call(probeCtx)
	if err == nil {
		return CapabilityReport{ID: id, Status: StatusOK, Transport: "stdio"}
	}

	switch {
	case errors.Is(err, ErrBareInvocationRefused):
		return CapabilityReport{
			ID:     id,
			Status: StatusBlockedByPolicy,
			Notes:  "wrapper refused bare invocation (Guardrail 1)",
		}
	case isMissingBinary(err):
		return CapabilityReport{
			ID:     id,
			Status: StatusMissing,
			Notes:  fmt.Sprintf("bv binary not found: %s", err.Error()),
		}
	default:
		return CapabilityReport{
			ID:        id,
			Status:    StatusDegraded,
			Transport: "stdio",
			Notes:     fmt.Sprintf("%s probe error: %s", summary, truncateStderr([]byte(err.Error()))),
		}
	}
}

// probeVersion captures `bv --version` output for the ToolReport.
// Best-effort — failure returns "" and the caller surfaces version
// as unknown in Diagnostics.
func probeVersion(ctx context.Context, client *Client) string {
	versionCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	out, _, exit, err := client.Exec.Run(versionCtx, []string{"--version"})
	if err != nil || exit != 0 {
		return ""
	}
	// bv --version typically returns "bv X.Y.Z\n" or similar; trim
	// whitespace + the leading "bv " prefix if present.
	v := strings.TrimSpace(string(out))
	v = strings.TrimPrefix(v, "bv ")
	v = strings.TrimPrefix(v, "bv (Beads Viewer)")
	v = strings.TrimSpace(v)
	if newline := strings.Index(v, "\n"); newline > 0 {
		v = v[:newline]
	}
	return v
}

// isMissingBinary reports whether err looks like an exec-not-found
// error from os/exec. Used to classify probe failures as `missing`
// vs `degraded`.
func isMissingBinary(err error) bool {
	if err == nil {
		return false
	}
	s := err.Error()
	return strings.Contains(s, "executable file not found") ||
		strings.Contains(s, "no such file or directory") ||
		strings.Contains(s, "exec: \"bv\"") ||
		strings.Contains(s, "command not found")
}
