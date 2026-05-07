package exportbundle

// SectionID identifies one slice of the §10.4 export bundle.
// Renaming is a schema-version event because restore validators
// key on it.
type SectionID string

// The §10.4 catalog. The export bundle archive lays each section
// out under its SectionID name as the directory inside the tarball.
const (
	SectionDaemonProjectMetadata    SectionID = "daemon_project_metadata"
	SectionAuditLogSlice            SectionID = "audit_log_slice"
	SectionEventReplayCheckpoints   SectionID = "event_replay_checkpoints"
	SectionPlanArtifacts            SectionID = "plan_artifacts"
	SectionBeadConversionTraces     SectionID = "bead_conversion_traces"
	SectionTraceabilityMaps         SectionID = "traceability_maps"
	SectionHealthSnapshots          SectionID = "health_snapshots"
	SectionReviewFindings           SectionID = "review_findings"
	SectionLandingQueueHistory      SectionID = "landing_queue_history"
	SectionArtifactRefs             SectionID = "artifact_refs"
	SectionCapabilityToolInventory  SectionID = "capability_tool_inventory"
	SectionSkillLockFile            SectionID = "skill_lock_file"
	SectionRedactedDiagnostics      SectionID = "redacted_diagnostics"
)

// SourceCanonicality records whether a section is sourced from
// canonical tool state (the desired path per Guardrail 4) or from
// the daemon's read-model cache. The bundle writer must prefer
// canonical sources; the field is recorded so reviewers can audit
// the source choice.
type SourceCanonicality string

const (
	// SourceCanonical: section read directly from the canonical
	// owner (`br`, `git`, NTM, Agent Mail, plan markdown files,
	// etc.). Per §1.1.
	SourceCanonical SourceCanonicality = "canonical"

	// SourceCacheReconciled: section read from the daemon's
	// cache after explicit reconciliation against canonical
	// state (e.g., bead read-model rebuilt from .beads/ before
	// inclusion). Acceptable but flagged for review.
	SourceCacheReconciled SourceCanonicality = "cache_reconciled"

	// SourceDaemonOwned: section is daemon-native (job state,
	// audit log, Hoopoe-generated health snapshots). The daemon
	// IS the canonical owner per §2.2.
	SourceDaemonOwned SourceCanonicality = "daemon_owned"
)

// SecretsHandling records what the bundle writer does with
// secret-shaped values found in this section.
type SecretsHandling string

const (
	// SecretsRedact: redact secrets per the apps/daemon/internal/
	// redaction layer before inclusion. Default for any section
	// that reads from logs / audit / pane output / plan jobs.
	SecretsRedact SecretsHandling = "redact"

	// SecretsExclude: omit values that match secret patterns
	// entirely. Used for diagnostic sections where partial
	// redaction would still leak structure.
	SecretsExclude SecretsHandling = "exclude"

	// SecretsNotApplicable: section contains no secret-shaped
	// data (e.g., capability inventory, schema-version
	// snapshots).
	SecretsNotApplicable SecretsHandling = "not_applicable"
)

// BundleSection is one row in the §10.4 manifest. The bundle
// writer iterates these in order; the restore validator reads
// the manifest, asserts SchemaVersion compatibility, and verifies
// per-section hashes before rehydrating.
type BundleSection struct {
	ID          SectionID `json:"id"`
	Description string    `json:"description"`

	// SchemaVersion of this section's payload format. Restore
	// MUST refuse a section whose SchemaVersion is unknown
	// (refuses incompatible bundles per §10.4 acceptance).
	SchemaVersion int `json:"schemaVersion"`

	// Source declares where the bundle writer must read from.
	Source SourceCanonicality `json:"source"`

	// Secrets declares the redaction posture for the section.
	Secrets SecretsHandling `json:"secrets"`

	// Optional indicates the section may be omitted if the
	// substrate is not present (e.g., review_findings is
	// optional for a project that has not run a review round).
	Optional bool `json:"optional"`

	// SubstrateBeads point at the implementation owner so a
	// missing-section failure routes triage.
	SubstrateBeads []string `json:"substrateBeads,omitempty"`
}

// BundleManifestSchemaVersion is the version of the manifest
// shape itself. Bumping this is a §10.3 schema-version event.
const BundleManifestSchemaVersion = 1

// SectionCatalog returns the §10.4 source-of-truth list of
// bundle sections in the order the bundle writer must produce.
func SectionCatalog() []BundleSection {
	return []BundleSection{
		{
			ID:             SectionDaemonProjectMetadata,
			Description:    "Daemon-side project metadata: VPS pairing, repo refs, readiness gates, lifecycle state.",
			SchemaVersion:  1,
			Source:         SourceDaemonOwned,
			Secrets:        SecretsRedact,
			Optional:       false,
		},
		{
			ID:             SectionAuditLogSlice,
			Description:    "Audit log entries scoped to the project (Guardrail 10 — every action recorded).",
			SchemaVersion:  1,
			Source:         SourceDaemonOwned,
			Secrets:        SecretsRedact,
			Optional:       false,
			SubstrateBeads: []string{"hp-g73"},
		},
		{
			ID:             SectionEventReplayCheckpoints,
			Description:    "Event-replay checkpoint snapshots that let a restored cockpit resume the project's WS event stream from a known cursor.",
			SchemaVersion:  1,
			Source:         SourceDaemonOwned,
			Secrets:        SecretsRedact,
			Optional:       false,
		},
		{
			ID:             SectionPlanArtifacts,
			Description:    "Plan markdown files under .hoopoe/plans/<plan-id>/ — the canonical owner per §1.1.",
			SchemaVersion:  1,
			Source:         SourceCanonical,
			Secrets:        SecretsRedact,
			Optional:       true,
			SubstrateBeads: []string{"hp-vh7"},
		},
		{
			ID:             SectionBeadConversionTraces,
			Description:    "Plan-to-beads conversion artifacts: prompts, intermediate model outputs, polish-round records.",
			SchemaVersion:  1,
			Source:         SourceDaemonOwned,
			Secrets:        SecretsRedact,
			Optional:       true,
			SubstrateBeads: []string{"hp-9kt"},
		},
		{
			ID:             SectionTraceabilityMaps,
			Description:    "traceability.json mapping plan sections → bead IDs (Phase 6 output).",
			SchemaVersion:  1,
			Source:         SourceDaemonOwned,
			Secrets:        SecretsNotApplicable,
			Optional:       true,
			SubstrateBeads: []string{"hp-ojh"},
		},
		{
			ID:             SectionHealthSnapshots,
			Description:    "Health snapshots Hoopoe generated in isolated worktrees (Guardrail 5); KPIs + per-file metrics + trend rollups.",
			SchemaVersion:  1,
			Source:         SourceDaemonOwned,
			Secrets:        SecretsNotApplicable,
			Optional:       true,
			SubstrateBeads: []string{"hp-3at"},
		},
		{
			ID:             SectionReviewFindings,
			Description:    "Phase 12 finding ledger entries with disposition history.",
			SchemaVersion:  1,
			Source:         SourceDaemonOwned,
			Secrets:        SecretsRedact,
			Optional:       true,
			SubstrateBeads: []string{"hp-k4j"},
		},
		{
			ID:             SectionLandingQueueHistory,
			Description:    "Landing queue history: bead-completion events, gate-passes, convergence transitions.",
			SchemaVersion:  1,
			Source:         SourceDaemonOwned,
			Secrets:        SecretsRedact,
			Optional:       true,
		},
		{
			ID:             SectionArtifactRefs,
			Description:    "Artifact references + selected blobs (build outputs, test reports, model raw artifacts within retention).",
			SchemaVersion:  1,
			Source:         SourceDaemonOwned,
			Secrets:        SecretsRedact,
			Optional:       true,
		},
		{
			ID:             SectionCapabilityToolInventory,
			Description:    "Capability registry + tool inventory snapshot (§2.8): which tools were available at export time + their reported versions.",
			SchemaVersion:  1,
			Source:         SourceDaemonOwned,
			Secrets:        SecretsNotApplicable,
			Optional:       false,
		},
		{
			ID:             SectionSkillLockFile,
			Description:    ".hoopoe/skills.lock.json: SHA-256 pins from jsm or advisory version strings from jfp; lets a restore reproduce the skill set.",
			SchemaVersion:  1,
			Source:         SourceCanonical,
			Secrets:        SecretsNotApplicable,
			Optional:       false,
		},
		{
			ID:             SectionRedactedDiagnostics,
			Description:    "Redacted diagnostics rollup for support: tool versions, daemon version, recent error fingerprints (no raw logs).",
			SchemaVersion:  1,
			Source:         SourceDaemonOwned,
			Secrets:        SecretsExclude,
			Optional:       true,
		},
	}
}

// BundleManifest is the top-level shape the bundle writer emits
// at `manifest.json` inside the archive. The restore validator
// reads this first, asserts SchemaVersion compatibility, then
// validates each section's hash + SchemaVersion against the
// catalog before rehydrating.
type BundleManifest struct {
	// SchemaVersion is BundleManifestSchemaVersion at write time.
	// Restore refuses unknown manifest versions.
	SchemaVersion int `json:"schemaVersion"`

	// ProjectID is the project being exported.
	ProjectID string `json:"projectId"`

	// CreatedAt is the RFC3339 timestamp of the export.
	CreatedAt string `json:"createdAt"`

	// HoopoeVersion is the daemon version that produced the
	// bundle. Restore uses this for the §10.3 compatibility check.
	HoopoeVersion string `json:"hoopoeVersion"`

	// Sections is the ordered list of section entries in the
	// archive. Each carries the section's SchemaVersion (so
	// individual sections can evolve independently of the manifest)
	// and its content hash (so restore can detect tampering).
	Sections []ManifestSection `json:"sections"`
}

// ManifestSection records the bundle's per-section metadata.
type ManifestSection struct {
	ID            SectionID `json:"id"`
	SchemaVersion int       `json:"schemaVersion"`
	Path          string    `json:"path"`
	SizeBytes     int64     `json:"sizeBytes"`
	SHA256        string    `json:"sha256"`

	// Source mirrors BundleSection.Source so a restore reviewer
	// can see whether the section came from canonical or cache
	// state at write time (audit transparency per §1.4).
	Source SourceCanonicality `json:"source"`

	// Secrets mirrors BundleSection.Secrets to record the redaction
	// posture applied at write time.
	Secrets SecretsHandling `json:"secrets"`

	// Omitted is true when the section was optional and not
	// produced (e.g., no plan artifacts because the project
	// hasn't run Stage 01 yet). The catalog row is still listed
	// so reviewers know it was considered and skipped.
	Omitted bool `json:"omitted,omitempty"`
}

// LookupSection returns the catalog entry for id, or false when
// id is unknown.
func LookupSection(id SectionID) (BundleSection, bool) {
	for _, section := range SectionCatalog() {
		if section.ID == id {
			return section, true
		}
	}
	return BundleSection{}, false
}

// RequiredSections returns only the non-optional sections.
// Restore must refuse a bundle missing any of these.
func RequiredSections() []BundleSection {
	out := make([]BundleSection, 0)
	for _, section := range SectionCatalog() {
		if !section.Optional {
			out = append(out, section)
		}
	}
	return out
}
