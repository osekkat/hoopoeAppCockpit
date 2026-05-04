// Package caut is the daemon-side adapter for the `caut` (coding agent
// usage tracker) tool — the SOLE source of subscription-window usage
// telemetry per plan.md §5.1, §17, §7.6.
//
// caut surfaces in:
//
//   - Top-bar subscription-usage pill (§7.6) — per-provider window %.
//   - watch-safety-thresholds tending job (§8.4) — alerts when usage
//     crosses configured thresholds.
//
// Failure mode (§7.6 + DOD): when caut is unavailable for a provider,
// the UI labels the cell `unmeasured` rather than fabricating a number.
// This package surfaces the unmeasured state via a sentinel error +
// per-provider Snapshot.Status field.
//
// caut is NOT installed locally during this bead's authorship — the
// adapter is built to a documented spec; integration validation
// happens against the research-spike VPS. Tests use a fake executor
// only; a real-binary smoke test will land when a caut-equipped
// fixture exists.
package caut

import "time"

// Provider is a closed enum of subscription tiers caut tracks.
type Provider string

const (
	ProviderClaude    Provider = "claude_max"
	ProviderCodex     Provider = "gpt_pro"
	ProviderGemini    Provider = "gemini_ultra"
	ProviderChatGPTPro Provider = "chatgpt_pro" // browser-mode (oracle)
)

// SnapshotStatus mirrors the per-provider availability of telemetry.
// `measured` = real numbers; `unmeasured` = caut couldn't query (per
// §7.6, surfaces in the UI as the literal label "unmeasured" rather
// than a fabricated zero).
type SnapshotStatus string

const (
	StatusMeasured   SnapshotStatus = "measured"
	StatusUnmeasured SnapshotStatus = "unmeasured"
	StatusError      SnapshotStatus = "error"
)

// ProviderSnapshot is one provider's window usage at the snapshot time.
//
// The renderer's top-bar pill consumes UsedPct + WindowResetsAt; the
// watch-safety-thresholds tending job consumes Status + UsedPct +
// BurnRate (tokens/minute, normalized to USD-per-hour at the provider's
// posted rate). Notes carries free-text from caut's CLI (e.g.,
// "rate-limited burst window 7/10").
type ProviderSnapshot struct {
	Provider       Provider       `json:"provider"`
	Status         SnapshotStatus `json:"status"`
	UsedPct        float64        `json:"used_pct,omitempty"`         // 0..1; absent when Status != measured
	WindowResetsAt time.Time      `json:"window_resets_at,omitempty"` // RFC3339; absent when Status != measured
	BurnRate       string         `json:"burn_rate,omitempty"`        // human-readable (e.g., "$2.40/hr")
	BurnRateUSDPerHour float64    `json:"burn_rate_usd_per_hour,omitempty"`
	Notes          string         `json:"notes,omitempty"`
	UnmeasuredReason string       `json:"unmeasured_reason,omitempty"` // populated when Status == unmeasured
}

// SnapshotResponse is the parsed result of `caut snapshot --json`.
//
// `caut snapshot --json` returns one entry per provider configured on
// the box. Providers not in the response are absent; the renderer
// treats absent providers as `unmeasured` (same UI bucket).
type SnapshotResponse struct {
	Snapshot SnapshotMeta       `json:"snapshot"`
	Providers []ProviderSnapshot `json:"providers"`
}

// SnapshotMeta is the per-snapshot metadata caut emits.
type SnapshotMeta struct {
	GeneratedAt   time.Time `json:"generated_at"`
	CautVersion   string    `json:"caut_version,omitempty"`
	Hostname      string    `json:"hostname,omitempty"`
}

// PerProviderStatus is a small flat helper the daemon's tend-watch-
// safety-thresholds job consumes — maps Provider → SnapshotStatus
// without the renderer having to walk the slice.
type PerProviderStatus map[Provider]SnapshotStatus

// IndexByProvider builds a PerProviderStatus from a SnapshotResponse.
// Convenience for the tending job's threshold-check loop.
func (r *SnapshotResponse) IndexByProvider() PerProviderStatus {
	out := PerProviderStatus{}
	if r == nil {
		return out
	}
	for _, p := range r.Providers {
		out[p.Provider] = p.Status
	}
	return out
}
