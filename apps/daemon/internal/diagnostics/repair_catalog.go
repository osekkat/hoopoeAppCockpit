package diagnostics

// RepairActionID is the stable string identifier the renderer + the
// audit log + the API contract reference. Renaming an ID is a
// schema-version event (per plan.md §10.3).
type RepairActionID string

// The §10.2 Diagnostics-screen catalog. Twelve entries; this list
// is the single source of truth for the Diagnostics screen and the
// daemon RPC handler dispatch table.
const (
	RepairRestartDaemon          RepairActionID = "diagnostics.restart_daemon"
	RepairRestartNTMAgentMail    RepairActionID = "diagnostics.restart_ntm_agent_mail"
	RepairRunACFSDoctor          RepairActionID = "diagnostics.run_acfs_doctor"
	RepairClearLocalClone        RepairActionID = "diagnostics.clear_local_clone"
	RepairPruneOrphanClones      RepairActionID = "diagnostics.prune_orphan_clones"
	RepairForceReleaseReservation RepairActionID = "diagnostics.force_release_reservation"
	RepairReplayEvents           RepairActionID = "diagnostics.replay_events"
	RepairRebuildBeadReadModel   RepairActionID = "diagnostics.rebuild_bead_read_model"
	RepairRerunHealthSnapshot    RepairActionID = "diagnostics.rerun_health_snapshot"
	RepairVerifySkills           RepairActionID = "diagnostics.verify_skills"
	RepairShowRawPane            RepairActionID = "diagnostics.show_raw_pane"
	RepairRestartOracle          RepairActionID = "diagnostics.restart_oracle"
)

// SafetyClass categorizes the repair's impact surface so the
// renderer can pick the right confirmation copy + the daemon can
// pick the right audit log line + the approvals queue knows
// whether to gate.
type SafetyClass string

const (
	// SafetyReadOnly: the repair never mutates state outside its own
	// audit row. No confirmation needed; safe to surface as a
	// one-click affordance.
	SafetyReadOnly SafetyClass = "read_only"

	// SafetyCacheOnly: the repair mutates Hoopoe's read-model cache
	// but never canonical tool state (Guardrail 4). One-click with
	// a soft "this will rebuild local cache" note.
	SafetyCacheOnly SafetyClass = "cache_only"

	// SafetyDestructiveLocal: the repair deletes local-only state
	// (e.g., the desktop sync mirror). Confirmation required;
	// canonical state is untouched (Guardrail 3).
	SafetyDestructiveLocal SafetyClass = "destructive_local"

	// SafetyDestructiveShared: the repair affects shared state
	// (force-release someone else's reservation; restart a service
	// other agents depend on). Approval required; impact warning
	// shown; reason field required.
	SafetyDestructiveShared SafetyClass = "destructive_shared"

	// SafetyServiceLifecycle: the repair restarts a daemon-managed
	// service. Confirmation + impact warning; preserves what state
	// it can across the restart (job registry, in-flight uploads).
	SafetyServiceLifecycle SafetyClass = "service_lifecycle"
)

// RepairAction is the typed shape for one entry in the §10.2
// catalog. The renderer iterates these to produce the Diagnostics
// screen list; the daemon dispatches by ID.
type RepairAction struct {
	// ID is the stable string identifier. The renderer + audit log +
	// API contract reference this; renaming is a schema-version
	// event.
	ID RepairActionID `json:"id"`

	// Label is the short user-facing name in the Diagnostics list.
	Label string `json:"label"`

	// Description is the one-paragraph user-facing description
	// shown on hover or in the confirmation dialog.
	Description string `json:"description"`

	// Safety determines which confirmation/approval flow gates the
	// invocation.
	Safety SafetyClass `json:"safety"`

	// RequiresConfirmation gates the renderer-side confirmation
	// dialog. Implied true for any safety class above cache_only;
	// explicit so the renderer doesn't have to recompute.
	RequiresConfirmation bool `json:"requiresConfirmation"`

	// RequiresApproval routes through the unified approvals queue
	// (hp-v0g). Used for destructive_shared + a subset of
	// service_lifecycle (e.g., daemon restart on a multi-agent
	// session).
	RequiresApproval bool `json:"requiresApproval"`

	// RequiresReason: when true, the user must supply a non-empty
	// free-text reason that lands in the audit row + (where
	// relevant) the Agent Mail notice posted by the action.
	RequiresReason bool `json:"requiresReason"`

	// CapabilityRequired is the capability ID (§2.8) the daemon
	// asserts before allowing the dispatch. Empty when no specific
	// capability is needed (e.g., restart-daemon does not require
	// an external tool).
	CapabilityRequired string `json:"capabilityRequired,omitempty"`

	// AuditAction is the audit-log action string written for every
	// invocation, success or failure (Guardrail 10).
	AuditAction string `json:"auditAction"`

	// ImpactWarning is the renderer-side copy shown above the
	// confirmation button when Safety >= service_lifecycle. Empty
	// when no warning is appropriate.
	ImpactWarning string `json:"impactWarning,omitempty"`

	// PostsAgentMail signals that successful dispatch posts an
	// Agent Mail notice (e.g., force-release-reservation posts to
	// the original reserver's inbox). The renderer can preview the
	// notice in the confirmation dialog.
	PostsAgentMail bool `json:"postsAgentMail,omitempty"`
}

// Catalog returns the §10.2 source-of-truth list of repair actions.
//
// Order matches the plan.md table reading top-to-bottom; the
// Diagnostics screen renders the same order so users navigating
// the plan + the cockpit see them aligned.
func Catalog() []RepairAction {
	return []RepairAction{
		{
			ID:                   RepairRestartDaemon,
			Label:                "Restart daemon",
			Description:          "Restart the Hoopoe daemon process; streams systemd result and preserves the job registry where possible.",
			Safety:               SafetyServiceLifecycle,
			RequiresConfirmation: true,
			RequiresApproval:     true,
			AuditAction:          "diagnostics.restart_daemon",
			ImpactWarning:        "All connected sessions briefly disconnect; in-flight jobs are paused and resumed via the persisted registry.",
		},
		{
			ID:                   RepairRestartNTMAgentMail,
			Label:                "Restart NTM / Agent Mail",
			Description:          "Restart the NTM tmux orchestrator and Agent Mail MCP service.",
			Safety:               SafetyServiceLifecycle,
			RequiresConfirmation: true,
			AuditAction:          "diagnostics.restart_ntm_agent_mail",
			ImpactWarning:        "Active swarm panes briefly disconnect; in-flight Agent Mail deliveries are queued and retried.",
			CapabilityRequired:   "ntm.session.manage",
		},
		{
			ID:                   RepairRunACFSDoctor,
			Label:                "Re-run ACFS doctor",
			Description:          "Run ACFS doctor in read-only mode; user must approve before any fixes are applied.",
			Safety:               SafetyReadOnly,
			RequiresConfirmation: false,
			AuditAction:          "diagnostics.run_acfs_doctor",
			CapabilityRequired:   "acfs.doctor.run",
		},
		{
			ID:                   RepairClearLocalClone,
			Label:                "Clear desktop local clone",
			Description:          "Delete the desktop's sync-driven local mirror of origin. The VPS clone and origin are untouched (Guardrail 3).",
			Safety:               SafetyDestructiveLocal,
			RequiresConfirmation: true,
			AuditAction:          "diagnostics.clear_local_clone",
			ImpactWarning:        "Local diffs, blame, and ripgrep will re-fetch from origin on next use.",
		},
		{
			ID:                   RepairPruneOrphanClones,
			Label:                "Prune orphan project clones",
			Description:          "Run `ru prune` to detect orphans. --archive (non-destructive) is the default; --delete requires confirmation. No-op when no orphans found.",
			Safety:               SafetyDestructiveLocal,
			RequiresConfirmation: true,
			AuditAction:          "diagnostics.prune_orphan_clones",
			CapabilityRequired:   "ru.prune.run",
		},
		{
			ID:                   RepairForceReleaseReservation,
			Label:                "Force release stale reservation",
			Description:          "Release another agent's file reservation. Posts an Agent Mail notice to the original reserver and links to the stale-evidence audit entry.",
			Safety:               SafetyDestructiveShared,
			RequiresConfirmation: true,
			RequiresApproval:     true,
			RequiresReason:       true,
			AuditAction:          "diagnostics.force_release_reservation",
			ImpactWarning:        "If the reserver is still active, they may have unpushed work on the released paths.",
			PostsAgentMail:       true,
			CapabilityRequired:   "agent_mail.reservation.force_release",
		},
		{
			ID:                   RepairReplayEvents,
			Label:                "Replay events from sequence",
			Description:          "Re-emit events from a chosen sequence cursor to repair UI gaps. Read-only on canonical state.",
			Safety:               SafetyReadOnly,
			RequiresConfirmation: false,
			AuditAction:          "diagnostics.replay_events",
		},
		{
			ID:                   RepairRebuildBeadReadModel,
			Label:                "Rebuild bead read model",
			Description:          "Re-read .beads/ + br state into the daemon's read-model cache. Canonical bead state is untouched (Guardrail 4).",
			Safety:               SafetyCacheOnly,
			RequiresConfirmation: false,
			AuditAction:          "diagnostics.rebuild_bead_read_model",
			CapabilityRequired:   "br.issues.read",
		},
		{
			ID:                   RepairRerunHealthSnapshot,
			Label:                "Re-run health snapshot",
			Description:          "Trigger a fresh health snapshot in an isolated worktree. Queues behind active health jobs (Guardrail 5).",
			Safety:               SafetyReadOnly,
			RequiresConfirmation: false,
			AuditAction:          "diagnostics.rerun_health_snapshot",
			CapabilityRequired:   "health.snapshot.run",
		},
		{
			ID:                   RepairVerifySkills,
			Label:                "Update / verify skills",
			Description:          "Re-run the active skill installer (jsm preferred; jfp fallback); verify SHA-256 pins; report drift; one-click upgrade pinned versions.",
			Safety:               SafetyCacheOnly,
			RequiresConfirmation: false,
			AuditAction:          "diagnostics.verify_skills",
			CapabilityRequired:   "jsm.skill.verify",
		},
		{
			ID:                   RepairShowRawPane,
			Label:                "Show raw pane (per agent)",
			Description:          "Stream the agent's tmux pane output to the Diagnostics-only TerminalPane. Opt-in per agent; auto-closes after configurable idle window; read-only (Guardrail 12).",
			Safety:               SafetyReadOnly,
			RequiresConfirmation: true,
			AuditAction:          "diagnostics.show_raw_pane",
			ImpactWarning:        "Raw pane output bypasses the abstracted Swarm dashboard; only enable for forensic / debugging needs.",
			CapabilityRequired:   "ntm.pane.attach",
		},
		{
			ID:                   RepairRestartOracle,
			Label:                "Restart Oracle",
			Description:          "Tear down + re-launch local oracle serve. Pauses in-flight Pro plan rounds; does not affect VPS-side CLIs.",
			Safety:               SafetyServiceLifecycle,
			RequiresConfirmation: true,
			AuditAction:          "diagnostics.restart_oracle",
			ImpactWarning:        "Active ChatGPT-Pro-web plan rounds pause until Oracle is back; VPS-side CLIs (Claude Code / Codex / Gemini) are unaffected.",
			CapabilityRequired:   "oracle.serve.status",
		},
	}
}

// Lookup returns the catalog entry for id, or false when the id is
// not in the catalog. Use this in handler dispatch to refuse
// unknown ids before they reach side-effecting code.
func Lookup(id RepairActionID) (RepairAction, bool) {
	for _, action := range Catalog() {
		if action.ID == id {
			return action, true
		}
	}
	return RepairAction{}, false
}
