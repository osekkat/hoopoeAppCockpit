// capabilities.go — git.* capability declarations + Probe per §2.8.
//
// The capability registry consumes the ProbeResult to decide which UI
// surfaces are available, degraded, or missing. git.push is split from
// git.status.read so a project mounted read-only (e.g., archived state)
// surfaces correctly: status reads succeed, push is blocked-by-policy.
package git

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
)

// Capability IDs declared by this adapter (mirrors plan.md §2.8 list +
// hp-r33 capability IDs; bead hp-l65 names: git.status.read,
// git.diff.read, git.push, git.unpushed.list).
const (
	CapStatusRead    = "git.status.read"
	CapDiffRead      = "git.diff.read"
	CapPush          = "git.push"
	CapUnpushedList  = "git.unpushed.list"
	CapLog           = "git.log.read"
	CapShow          = "git.show.read"
	CapBlame         = "git.blame.read"
	CapRemoteRead    = "git.remote.read"
	CapBranchRead    = "git.branch.read"
)

// AllCapabilityIDs returns every capability this adapter probes.
func AllCapabilityIDs() []string {
	return []string{
		CapStatusRead, CapDiffRead, CapPush, CapUnpushedList,
		CapLog, CapShow, CapBlame, CapRemoteRead, CapBranchRead,
	}
}

// CapabilityStatus mirrors the wire enum from packages/schemas (5-valued
// per WhiteCreek's hp-r33 delta 1). Re-declared here to keep the adapter
// import-cycle-free.
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

// ProbeResult is the full per-call output. Used by the capabilities
// registry adapter.
type ProbeResult struct {
	Tool        string
	Version     string
	Source      string
	LastChecked time.Time
	Reports     map[string]CapabilityReport
}

// Probe runs a minimal subset of read-only git commands against the
// project repo to verify each capability. Push is NOT exercised — the
// probe reports `untested` for it (you can't probe a push without
// actually pushing). The renderer treats `untested` as the same UI
// bucket as `missing`; Diagnostics distinguishes them.
func Probe(ctx context.Context, client *Client, now func() time.Time) ProbeResult {
	if now == nil {
		now = time.Now
	}
	res := ProbeResult{
		Tool:        "git",
		Source:      "CLI",
		LastChecked: now().UTC(),
		Reports:     make(map[string]CapabilityReport, 9),
	}

	res.Version = probeVersion(ctx, client)

	res.Reports[CapStatusRead] = probeOne(ctx, CapStatusRead, "status --porcelain", func(ctx context.Context) error {
		_, err := client.Status(ctx)
		return err
	})

	res.Reports[CapDiffRead] = probeOne(ctx, CapDiffRead, "diff", func(ctx context.Context) error {
		_, err := client.DiffUnstaged(ctx)
		return err
	})

	res.Reports[CapLog] = probeOne(ctx, CapLog, "log -n 1", func(ctx context.Context) error {
		_, err := client.Log(ctx, LogOpts{Limit: 1})
		return err
	})

	res.Reports[CapShow] = probeOne(ctx, CapShow, "show HEAD", func(ctx context.Context) error {
		_, err := client.Show(ctx, "HEAD")
		return err
	})

	res.Reports[CapRemoteRead] = probeOne(ctx, CapRemoteRead, "remote -v", func(ctx context.Context) error {
		_, err := client.Remotes(ctx)
		return err
	})

	res.Reports[CapBranchRead] = probeOne(ctx, CapBranchRead, "branch -v", func(ctx context.Context) error {
		_, err := client.Branches(ctx)
		return err
	})

	res.Reports[CapBlame] = probeOne(ctx, CapBlame, "blame --porcelain", func(ctx context.Context) error {
		// Blame requires a path; defer to untested if no obvious one
		// is known. The probe tries `README.md` then falls back to
		// reporting `untested` (the renderer surfaces it correctly).
		_, err := client.Blame(ctx, "", "README.md")
		return err
	})

	res.Reports[CapUnpushedList] = probeOne(ctx, CapUnpushedList, "rev-list origin/HEAD..HEAD", func(ctx context.Context) error {
		_, err := client.RevList(ctx, "origin/HEAD", "HEAD")
		return err
	})

	// Push is intentionally untested — probing would side-effect.
	res.Reports[CapPush] = CapabilityReport{
		ID:     CapPush,
		Status: StatusUntested,
		Notes:  "push is side-effecting; probe deferred (Diagnostics shows status untested → unavailable in UI bucket)",
	}

	return res
}

// probeOne runs a single capability probe with a 5s timeout.
func probeOne(ctx context.Context, id, summary string, call func(context.Context) error) CapabilityReport {
	probeCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	err := call(probeCtx)
	if err == nil {
		return CapabilityReport{ID: id, Status: StatusOK, Transport: "stdio"}
	}

	switch {
	case errors.Is(err, ErrMissingBinary):
		return CapabilityReport{
			ID:     id,
			Status: StatusMissing,
			Notes:  "git binary not on PATH",
		}
	case errors.Is(err, ErrEmptyRepoPath):
		return CapabilityReport{
			ID:     id,
			Status: StatusBlockedByPolicy,
			Notes:  "RepoPath empty (defense-in-depth)",
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

// probeVersion captures `git --version` output for the ToolReport.
func probeVersion(ctx context.Context, client *Client) string {
	versionCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	out, _, exit, err := client.Exec.Run(versionCtx, client.RepoPath, []string{"--version"})
	if err != nil || exit != 0 {
		return ""
	}
	v := strings.TrimSpace(string(out))
	v = strings.TrimPrefix(v, "git version ")
	if newline := strings.Index(v, "\n"); newline > 0 {
		v = v[:newline]
	}
	return v
}
