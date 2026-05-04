// Package caam is the daemon-side adapter for CAAM (Cross-Agent Account
// Manager) — the SOLE credential pathway per plan.md §5.1, §17.
//
// CAAM owns provider account auth files for AI coding CLIs (Claude
// Code, Codex CLI, Gemini CLI, etc.) and enables instant account
// switching for "all you can eat" subscription plans (GPT Pro, Claude
// Max, Gemini Ultra). When an agent hits a usage limit, the tending
// scheduler fires `caam.switch_account` (per tending-actions.yaml) and
// this adapter executes it.
//
// Wire shape: every command that supports JSON returns either
// {"profiles": [...], "count": N} or {"tools": [...]} or {"agents":
// [...]}. The adapter mirrors that into typed structs.
//
// SECRETS: CAAM exposes a `clear` (logout) command and a `bundle export`
// flow that surfaces auth tokens. The daemon NEVER calls those; this
// adapter is read + activate only. Production wiring uses the audit
// log for every activate (per §1.4); credential material never enters
// the daemon's process memory beyond the file path.
package caam

import "time"

// Tool is one of the CAAM-recognised CLI tools. Closed enum — adding
// a new tool requires a CAAM upgrade + a coordinated openapi.yaml
// schema bump (provider-plugin.yaml mirror discipline).
type Tool string

const (
	ToolClaude   Tool = "claude"
	ToolCodex    Tool = "codex"
	ToolGemini   Tool = "gemini"
	ToolOpenCode Tool = "opencode"
	ToolCursor   Tool = "cursor"
)

// Profile is one entry from `caam ls --json`. The shape mirrors what
// CAAM actually returns; fields not in the output are absent from the
// struct (renderer handles missing data gracefully).
type Profile struct {
	Tool         Tool      `json:"tool"`
	Name         string    `json:"name"`
	BackedUpAt   time.Time `json:"backed_up_at,omitempty"`
	LastUsedAt   time.Time `json:"last_used_at,omitempty"`
	Tags         []string  `json:"tags,omitempty"`
	IsActive     bool      `json:"is_active,omitempty"`
	IsFavorite   bool      `json:"is_favorite,omitempty"`
	HealthStatus string    `json:"health_status,omitempty"`
	CooldownUntil *time.Time `json:"cooldown_until,omitempty"`
}

// ListResponse is the parsed result of `caam ls --json`.
type ListResponse struct {
	Profiles []Profile `json:"profiles"`
	Count    int       `json:"count"`
}

// ToolStatus is one entry from `caam status --json`.
type ToolStatus struct {
	Tool      Tool   `json:"tool"`
	LoggedIn  bool   `json:"logged_in"`
	// ActiveProfile reports which CAAM profile (if any) matches the
	// current auth state. Empty when the live auth doesn't match any
	// vault profile.
	ActiveProfile string `json:"active_profile,omitempty"`
	// Health is a human-readable indicator (e.g., "healthy", "expired",
	// "rate-limited"); CAAM may add new values per release.
	Health string `json:"health,omitempty"`
}

// StatusResponse is the parsed result of `caam status --json`.
type StatusResponse struct {
	Tools []ToolStatus `json:"tools"`
}

// Limit is one entry from `caam limits --format json`. Shape varies
// per provider; we model the common fields and preserve the rest as
// raw JSON for forward-compat.
type Limit struct {
	Provider string  `json:"provider"`
	Profile  string  `json:"profile"`
	UsedPct  float64 `json:"used_pct,omitempty"`
	WindowResetsAt time.Time `json:"window_resets_at,omitempty"`
	BurnRate string  `json:"burn_rate,omitempty"`
	Notes    string  `json:"notes,omitempty"`
}

// LimitsResponse is the parsed result of `caam limits --format json`.
//
// Note: CAAM returns a top-level array (`[]`) when the result set is
// empty — the parser normalizes that to a struct with empty Limits.
type LimitsResponse struct {
	Limits []Limit `json:"limits"`
}

// AgentInventoryEntry is one entry from `caam detect --json`.
type AgentInventoryEntry struct {
	Name         string                  `json:"name"`
	DisplayName  string                  `json:"display_name"`
	BinaryPath   string                  `json:"binary_path,omitempty"`
	Version      string                  `json:"version,omitempty"`
	Installed    bool                    `json:"installed"`
	ConfigPaths  []AgentInventoryPath    `json:"config_paths,omitempty"`
	AuthPaths    []AgentInventoryPath    `json:"auth_paths,omitempty"`
}

// AgentInventoryPath is one config / auth path entry.
type AgentInventoryPath struct {
	Path        string `json:"path"`
	Exists      bool   `json:"exists"`
	Readable    bool   `json:"readable"`
	Description string `json:"description,omitempty"`
}

// DetectResponse is the parsed result of `caam detect --json`.
type DetectResponse struct {
	Timestamp time.Time              `json:"timestamp"`
	Agents    []AgentInventoryEntry  `json:"agents"`
}

// ActivateResult is the structured outcome of `caam activate <tool> <profile>`.
// CAAM's activate is text-output only; we synthesize this struct from
// exit code + stderr classification.
type ActivateResult struct {
	Tool       Tool
	Profile    string
	ActivatedAt time.Time
	OK         bool
	Notes      string
}
