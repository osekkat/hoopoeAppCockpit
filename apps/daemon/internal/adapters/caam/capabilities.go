// capabilities.go — caam.* capability declarations + Probe per §2.8.
package caam

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
)

// Capability IDs declared by this adapter (mirrors plan.md §2.8 +
// hp-r33's bv-style namespace pattern).
const (
	CapAccountsList   = "caam.accounts.list"
	CapAccountStatus  = "caam.account.status"
	CapAccountSwitch  = "caam.account.switch"
	CapAccountLimits  = "caam.account.limits"
	CapAgentsDetect   = "caam.agents.detect"
)

// AllCapabilityIDs returns every capability this adapter probes.
func AllCapabilityIDs() []string {
	return []string{CapAccountsList, CapAccountStatus, CapAccountSwitch, CapAccountLimits, CapAgentsDetect}
}

// CapabilityStatus mirrors the wire 5-valued enum.
type CapabilityStatus string

const (
	StatusOK              CapabilityStatus = "ok"
	StatusDegraded        CapabilityStatus = "degraded"
	StatusMissing         CapabilityStatus = "missing"
	StatusBlockedByPolicy CapabilityStatus = "blocked-by-policy"
	StatusUntested        CapabilityStatus = "untested"
)

// CapabilityReport is one capability's probe result.
type CapabilityReport struct {
	ID        string
	Status    CapabilityStatus
	Notes     string
	Transport string
}

// ProbeResult is the full per-call output.
type ProbeResult struct {
	Tool        string
	Version     string
	Source      string
	LastChecked time.Time
	Reports     map[string]CapabilityReport
}

// Probe runs read-only caam commands. Account.switch is reported as
// `untested` (probing would side-effect — switching the live account
// off the operator's expected profile would be disruptive). The
// renderer treats `untested` like `missing` in the UI bucket.
func Probe(ctx context.Context, client *Client, now func() time.Time) ProbeResult {
	if now == nil {
		now = time.Now
	}
	res := ProbeResult{
		Tool:        "caam",
		Source:      "CLI",
		LastChecked: now().UTC(),
		Reports:     make(map[string]CapabilityReport, 5),
	}

	res.Version = probeVersion(ctx, client)

	res.Reports[CapAccountsList] = probeOne(ctx, CapAccountsList, "ls --json", func(ctx context.Context) error {
		_, err := client.List(ctx, "")
		return err
	})

	res.Reports[CapAccountStatus] = probeOne(ctx, CapAccountStatus, "status --json", func(ctx context.Context) error {
		_, err := client.Status(ctx, "")
		return err
	})

	res.Reports[CapAccountLimits] = probeOne(ctx, CapAccountLimits, "limits --format json", func(ctx context.Context) error {
		_, err := client.Limits(ctx, "")
		return err
	})

	res.Reports[CapAgentsDetect] = probeOne(ctx, CapAgentsDetect, "detect --json", func(ctx context.Context) error {
		_, err := client.Detect(ctx)
		return err
	})

	// Switch is intentionally untested — probing would side-effect.
	res.Reports[CapAccountSwitch] = CapabilityReport{
		ID:     CapAccountSwitch,
		Status: StatusUntested,
		Notes:  "switch is side-effecting; probe deferred (renderer buckets with missing)",
	}

	return res
}

func probeOne(ctx context.Context, id, summary string, call func(context.Context) error) CapabilityReport {
	probeCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	err := call(probeCtx)
	if err == nil {
		return CapabilityReport{ID: id, Status: StatusOK, Transport: "stdio"}
	}
	switch {
	case errors.Is(err, ErrMissingBinary):
		return CapabilityReport{ID: id, Status: StatusMissing, Notes: "caam binary not on PATH"}
	default:
		return CapabilityReport{
			ID:        id,
			Status:    StatusDegraded,
			Transport: "stdio",
			Notes:     fmt.Sprintf("%s probe error: %s", summary, truncateStderr([]byte(err.Error()))),
		}
	}
}

func probeVersion(ctx context.Context, client *Client) string {
	versionCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	out, _, exit, err := client.Exec.Run(versionCtx, []string{"--version"})
	if err != nil || exit != 0 {
		return ""
	}
	v := strings.TrimSpace(string(out))
	v = strings.TrimPrefix(v, "caam ")
	v = strings.TrimPrefix(v, "caam version ")
	if newline := strings.Index(v, "\n"); newline > 0 {
		v = v[:newline]
	}
	return v
}
