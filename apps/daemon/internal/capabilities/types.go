// Package capabilities is the daemon-side capability registry. It composes
// per-tool ToolReports into a CapabilityRegistry and serves the registry +
// /v1/compatibility composition (plan.md §2.6, §2.8). Every UI feature flag
// keys off declared capabilities; adapter contract tests assert capabilities
// not just parser success (§2.8).
//
// Schema parity: the canonical TypeScript shape lives in
// `packages/schemas/src/capability/`. Both files MUST move in lockstep. When
// hp-r3i wires OpenAPI codegen, `components.schemas` becomes the source of
// truth and this file gets regenerated; keep field names byte-identical.
package capabilities

import (
	"encoding/json"
	"fmt"
)

// SchemaVersion is the on-the-wire version for CapabilityRegistry and
// CompatibilityReport. Bumps follow plan.md §10.3 with migration evidence.
const SchemaVersion = 1

// CapabilityStatus enumerates the storage states for a single Capability. The
// renderer maps these into four UI buckets — see plan.md §2.8.
type CapabilityStatus string

const (
	StatusOK              CapabilityStatus = "ok"
	StatusDegraded        CapabilityStatus = "degraded"
	StatusMissing         CapabilityStatus = "missing"
	StatusBlockedByPolicy CapabilityStatus = "blocked-by-policy"
	// StatusUntested is carried through Phase 0 fixtures where the daemon
	// could not probe the capability in the snapshot. Renderer treats it as
	// unavailable; Diagnostics distinguishes it from missing so users know a
	// reprobe could change the verdict.
	StatusUntested CapabilityStatus = "untested"
)

func (s CapabilityStatus) Valid() bool {
	switch s {
	case StatusOK, StatusDegraded, StatusMissing, StatusBlockedByPolicy, StatusUntested:
		return true
	}
	return false
}

// ToolID is the namespace for adapter-reported tools per plan.md §2.8.
// Values follow the union enumerated in the bead description; `health_<lang>`
// is open-ended (e.g., health_ts, health_py, health_rs, health_go,
// health_generic) so per-ecosystem health probes can declare without growing
// the closed prefix list.
type ToolID string

const (
	ToolNTM       ToolID = "ntm"
	ToolBR        ToolID = "br"
	ToolBV        ToolID = "bv"
	ToolAgentMail ToolID = "agent_mail"
	ToolGit       ToolID = "git"
	ToolRU        ToolID = "ru"
	ToolCAAM      ToolID = "caam"
	ToolCAUT      ToolID = "caut"
	ToolDCG       ToolID = "dcg"
	ToolCASR      ToolID = "casr"
	ToolPT        ToolID = "pt"
	ToolSRP       ToolID = "srp"
	ToolSBH       ToolID = "sbh"
	ToolUBS       ToolID = "ubs"
	ToolJSM       ToolID = "jsm"
	ToolJFP       ToolID = "jfp"
	ToolOracle    ToolID = "oracle"
	ToolRCH       ToolID = "rch"
	ToolRano      ToolID = "rano"
)

// KnownClosedTools lists the ToolID values that are not health-prefixed. Used
// by validators and codegen drift checks.
var KnownClosedTools = []ToolID{
	ToolNTM, ToolBR, ToolBV, ToolAgentMail, ToolGit, ToolRU,
	ToolCAAM, ToolCAUT, ToolDCG, ToolCASR,
	ToolPT, ToolSRP, ToolSBH,
	ToolUBS, ToolJSM, ToolJFP, ToolOracle, ToolRCH, ToolRano,
}

// IsHealthTool reports whether id matches the open-ended health_<lang> shape.
func IsHealthTool(id ToolID) bool {
	const prefix = "health_"
	if len(id) <= len(prefix) {
		return false
	}
	return string(id[:len(prefix)]) == prefix
}

// Valid reports whether id matches a known closed tool or the health_<lang>
// pattern.
func (id ToolID) Valid() bool {
	if IsHealthTool(id) {
		return true
	}
	for _, known := range KnownClosedTools {
		if id == known {
			return true
		}
	}
	return false
}

// Capability is a single capId result inside a ToolReport.
type Capability struct {
	Status    CapabilityStatus `json:"status"`
	Fallback  string           `json:"fallback,omitempty"`
	Transport string           `json:"transport,omitempty"`
	Notes     string           `json:"notes,omitempty"`
}

// ToolReport is one tool's slice of the registry. Verbatim plan.md §2.8 plus
// `notes` carried inside each Capability.
type ToolReport struct {
	Tool            ToolID                `json:"tool"`
	Version         string                `json:"version"`
	Source          string                `json:"source"`
	Capabilities    map[string]Capability `json:"capabilities"`
	LastCheckedAt   string                `json:"lastCheckedAt"`
	FixturesVersion string                `json:"fixturesVersion"`
}

// Validate ensures the ToolReport's fields are well-formed. It is permissive
// about empty Version (some tools genuinely don't expose one) and empty
// FixturesVersion (live probes don't carry fixture tags).
func (r *ToolReport) Validate() error {
	if !r.Tool.Valid() {
		return fmt.Errorf("capabilities: invalid tool id %q", r.Tool)
	}
	if r.Source == "" {
		return fmt.Errorf("capabilities: tool %s has empty source", r.Tool)
	}
	if r.LastCheckedAt == "" {
		return fmt.Errorf("capabilities: tool %s has empty lastCheckedAt", r.Tool)
	}
	if r.Capabilities == nil {
		return fmt.Errorf("capabilities: tool %s has nil capabilities map", r.Tool)
	}
	for capID, cap := range r.Capabilities {
		if capID == "" {
			return fmt.Errorf("capabilities: tool %s has empty capability id", r.Tool)
		}
		if !cap.Status.Valid() {
			return fmt.Errorf("capabilities: tool %s capability %s has invalid status %q",
				r.Tool, capID, cap.Status)
		}
	}
	return nil
}

// CapabilityRegistry is the GET /v1/capabilities response.
type CapabilityRegistry struct {
	SchemaVersion    int                    `json:"schemaVersion"`
	SnapshotAt       string                 `json:"snapshotAt"`
	DaemonAPIVersion string                 `json:"daemonApiVersion"`
	FixturesVersion  string                 `json:"fixturesVersion"`
	Tools            map[ToolID]*ToolReport `json:"tools"`
}

// Validate ensures every embedded ToolReport is well-formed and the envelope
// fields are present.
func (cr *CapabilityRegistry) Validate() error {
	if cr.SchemaVersion != SchemaVersion {
		return fmt.Errorf("capabilities: registry schemaVersion %d != expected %d",
			cr.SchemaVersion, SchemaVersion)
	}
	if cr.SnapshotAt == "" {
		return fmt.Errorf("capabilities: registry has empty snapshotAt")
	}
	if cr.DaemonAPIVersion == "" {
		return fmt.Errorf("capabilities: registry has empty daemonApiVersion")
	}
	for id, report := range cr.Tools {
		if id != report.Tool {
			return fmt.Errorf("capabilities: registry key %q != ToolReport.Tool %q", id, report.Tool)
		}
		if err := report.Validate(); err != nil {
			return err
		}
	}
	return nil
}

// IfMissingRequired enumerates how a feature/job behaves when a required
// capability is missing (plan.md §2.8, §8.2).
type IfMissingRequired string

const (
	BlockJob       IfMissingRequired = "block_job"
	RunReadOnly    IfMissingRequired = "run_read_only"
	EmitDiagnostic IfMissingRequired = "emit_diagnostic"
)

func (v IfMissingRequired) Valid() bool {
	switch v {
	case BlockJob, RunReadOnly, EmitDiagnostic:
		return true
	}
	return false
}

// IfMissingOptional enumerates the policy when an optional capability is
// missing.
type IfMissingOptional string

const (
	ContinueWithWarning       IfMissingOptional = "continue_with_warning"
	SuppressRelatedDetections IfMissingOptional = "suppress_related_detections"
)

func (v IfMissingOptional) Valid() bool {
	switch v {
	case ContinueWithWarning, SuppressRelatedDetections:
		return true
	}
	return false
}

// ActivityBehavior controls how degraded execution surfaces in the Activity
// panel.
type ActivityBehavior string

const (
	ActivitySilent          ActivityBehavior = "silent"
	ActivityDiagnosticsOnly ActivityBehavior = "diagnostics_only"
	ActivityPanelWarning    ActivityBehavior = "activity_panel_warning"
)

func (v ActivityBehavior) Valid() bool {
	switch v {
	case ActivitySilent, ActivityDiagnosticsOnly, ActivityPanelWarning:
		return true
	}
	return false
}

// DegradedModeContract is the per-feature/per-job policy.
type DegradedModeContract struct {
	IfMissingRequired IfMissingRequired `json:"ifMissingRequired"`
	IfMissingOptional IfMissingOptional `json:"ifMissingOptional"`
	ActivityBehavior  ActivityBehavior  `json:"activityBehavior"`
}

func (c *DegradedModeContract) Validate() error {
	if !c.IfMissingRequired.Valid() {
		return fmt.Errorf("capabilities: invalid ifMissingRequired %q", c.IfMissingRequired)
	}
	if !c.IfMissingOptional.Valid() {
		return fmt.Errorf("capabilities: invalid ifMissingOptional %q", c.IfMissingOptional)
	}
	if !c.ActivityBehavior.Valid() {
		return fmt.Errorf("capabilities: invalid activityBehavior %q", c.ActivityBehavior)
	}
	return nil
}

// FeatureCapabilityRequirement is what a UI feature or tending job declares.
// The featureId namespace is consumer-defined; renderer hooks supply
// 'stage.swarm.launch', 'bead.kanban.refresh', etc.
type FeatureCapabilityRequirement struct {
	FeatureID            string               `json:"featureId"`
	CapabilitiesRequired []string             `json:"capabilitiesRequired"`
	CapabilitiesOptional []string             `json:"capabilitiesOptional"`
	DegradedMode         DegradedModeContract `json:"degradedMode"`
}

func (r *FeatureCapabilityRequirement) Validate() error {
	if r.FeatureID == "" {
		return fmt.Errorf("capabilities: feature requirement has empty featureId")
	}
	if r.CapabilitiesRequired == nil {
		// Allow empty (feature has zero hard requirements) but not nil.
		r.CapabilitiesRequired = []string{}
	}
	if r.CapabilitiesOptional == nil {
		r.CapabilitiesOptional = []string{}
	}
	return r.DegradedMode.Validate()
}

// CompatibilityReport is the GET /v1/compatibility response (plan.md §2.6,
// §10.3). It composes the registry with daemon API version, min-desktop
// version, event schema versions, migration state, and unsupported-client
// warnings.
type CompatibilityReport struct {
	SchemaVersion             int                 `json:"schemaVersion"`
	DaemonAPIVersion          string              `json:"daemonApiVersion"`
	MinDesktopVersion         string              `json:"minDesktopVersion"`
	EventSchemaVersions       map[string]int      `json:"eventSchemaVersions"`
	MigrationState            MigrationState      `json:"migrationState"`
	Capabilities              *CapabilityRegistry `json:"capabilities"`
	UnsupportedClientWarnings []string            `json:"unsupportedClientWarnings,omitempty"`
}

// MigrationPhase is an optional high-level signal complementing the
// structured fields of MigrationState. Exposed alongside the schemaVersion /
// appliedAt / pending triple so the top-bar Diagnostics signal can render a
// quick "are we in trouble" verdict without parsing the structured data.
type MigrationPhase string

const (
	MigrationIdle       MigrationPhase = "idle"
	MigrationRunning    MigrationPhase = "running"
	MigrationFailed     MigrationPhase = "failed"
	MigrationRolledBack MigrationPhase = "rolled_back"
)

// MigrationState mirrors §10.3 — daemon SQLite migration progress.
type MigrationState struct {
	SchemaVersion int            `json:"schemaVersion"`
	AppliedAt     string         `json:"appliedAt"`
	Pending       []string       `json:"pending"`
	Phase         MigrationPhase `json:"phase,omitempty"`
}

// FeatureRender is the renderer-side bucket: what UI shows for the feature.
// The mapping from CapabilityStatus → FeatureRender lives in determineRender.
type FeatureRender string

const (
	RenderAvailable       FeatureRender = "available"
	RenderDegraded        FeatureRender = "degraded"
	RenderUnavailable     FeatureRender = "unavailable"
	RenderBlockedByPolicy FeatureRender = "blocked-by-policy"
)

// MarshalJSON / UnmarshalJSON — these types are intentionally simple structs
// with json tags; Go's default encoding handles them. The only reason this
// file exposes a custom marshaler is to ensure the registry's `tools` map
// keys remain ToolID-typed (Go map[ToolID] marshals strings just fine).
var _ = json.Marshal
