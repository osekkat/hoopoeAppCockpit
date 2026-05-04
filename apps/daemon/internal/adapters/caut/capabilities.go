// capabilities.go — caut.* capability declarations + Probe per §2.8.
package caut

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
)

const (
	CapUsageSnapshot = "caut.usage.snapshot"
)

func AllCapabilityIDs() []string { return []string{CapUsageSnapshot} }

type CapabilityStatus string

const (
	StatusOK              CapabilityStatus = "ok"
	StatusDegraded        CapabilityStatus = "degraded"
	StatusMissing         CapabilityStatus = "missing"
	StatusBlockedByPolicy CapabilityStatus = "blocked-by-policy"
	StatusUntested        CapabilityStatus = "untested"
)

type CapabilityReport struct {
	ID        string
	Status    CapabilityStatus
	Notes     string
	Transport string
}

type ProbeResult struct {
	Tool        string
	Version     string
	Source      string
	LastChecked time.Time
	Reports     map[string]CapabilityReport
}

// Probe runs `caut snapshot --json` (the canonical read). Missing
// binary classifies as `missing` (renderer treats whole pill as
// unmeasured per §7.6); per-provider unmeasured states surface inside
// the snapshot itself, not at the capability layer.
func Probe(ctx context.Context, client *Client, now func() time.Time) ProbeResult {
	if now == nil {
		now = time.Now
	}
	res := ProbeResult{
		Tool:        "caut",
		Source:      "CLI",
		LastChecked: now().UTC(),
		Reports:     make(map[string]CapabilityReport, 1),
	}
	res.Version = probeVersion(ctx, client)

	probeCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	_, err := client.Snapshot(probeCtx)
	switch {
	case err == nil:
		res.Reports[CapUsageSnapshot] = CapabilityReport{
			ID: CapUsageSnapshot, Status: StatusOK, Transport: "stdio",
		}
	case errors.Is(err, ErrMissingBinary):
		res.Reports[CapUsageSnapshot] = CapabilityReport{
			ID:     CapUsageSnapshot,
			Status: StatusMissing,
			Notes:  "caut binary not on PATH; subscription pill renders unmeasured (§7.6)",
		}
	default:
		res.Reports[CapUsageSnapshot] = CapabilityReport{
			ID:        CapUsageSnapshot,
			Status:    StatusDegraded,
			Transport: "stdio",
			Notes:     fmt.Sprintf("snapshot probe error: %s", truncateStderr([]byte(err.Error()))),
		}
	}
	return res
}

func probeVersion(ctx context.Context, client *Client) string {
	versionCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	out, _, exit, err := client.Exec.Run(versionCtx, []string{"--version"})
	if err != nil || exit != 0 {
		return ""
	}
	v := strings.TrimSpace(string(out))
	v = strings.TrimPrefix(v, "caut ")
	v = strings.TrimPrefix(v, "caut version ")
	if newline := strings.Index(v, "\n"); newline > 0 {
		v = v[:newline]
	}
	return v
}
