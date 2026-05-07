package swarmlaunch

import "time"

// StageID identifies one ordered step in the §7.3 launch
// sequence. Renaming is a schema-version event because the audit
// log records launch attempts keyed on these.
type StageID string

const (
	StageReconcileProjectState StageID = "swarmlaunch.01.reconcile_project_state"
	StageVerifyLaunchGates     StageID = "swarmlaunch.02.verify_launch_gates"
	StageShowWarnings          StageID = "swarmlaunch.03.show_warnings"
	StageCreateSwarmSpec       StageID = "swarmlaunch.04.create_swarm_spec"
	StageNTMSpawn              StageID = "swarmlaunch.05.ntm_spawn"
	StageStaggerStarts         StageID = "swarmlaunch.06.stagger_starts"
	StageSendKickoffPrompt     StageID = "swarmlaunch.07.send_kickoff_prompt"
	StageStartEventSubs        StageID = "swarmlaunch.08.start_event_subscriptions"
	StageActivateTendingJobs   StageID = "swarmlaunch.09.activate_tending_jobs"
	StageShowSwarmDashboard    StageID = "swarmlaunch.10.show_swarm_dashboard"
)

// StageBlocking declares whether the stage failure aborts the
// launch (true) or surfaces a warning the user can dismiss
// (false). The §7.3 sequence has only one user-dismissable
// stage: warnings.
type StageBlocking bool

// Stage is one row in the §7.3 sequence catalog.
type Stage struct {
	ID          StageID       `json:"id"`
	Number      int           `json:"number"`
	Title       string        `json:"title"`
	Description string        `json:"description"`
	Blocking    StageBlocking `json:"blocking"`
	AuditAction string        `json:"auditAction"`
}

// SequenceCatalog returns the §7.3 source-of-truth list of
// launch stages in plan.md ordering.
func SequenceCatalog() []Stage {
	return []Stage{
		{
			ID:          StageReconcileProjectState,
			Number:      1,
			Title:       "Reconcile project state (canonical wins, §1.1)",
			Description: "Re-read br + bv + NTM + Agent Mail + Git + CAAM canonical state; rebuild the daemon read-model cache from scratch; refuse to launch if any canonical source is unreachable.",
			Blocking:    true,
			AuditAction: "swarmlaunch.reconcile_project_state",
		},
		{
			ID:          StageVerifyLaunchGates,
			Number:      2,
			Title:       "Verify launch gates per §4.2",
			Description: "Iterate the §4.2 gates (VPS ready / project imported / plan locked / beads created+finalized / launch ready); abort with a typed Problem on any gate failure; never proceed past a red gate.",
			Blocking:    true,
			AuditAction: "swarmlaunch.verify_launch_gates",
		},
		{
			ID:          StageShowWarnings,
			Number:      3,
			Title:       "Show warnings (user-dismissable)",
			Description: "Render the §7.3 warning checklist (dirty Git / stale reservations / no ready beads / low disk / missing Agent Mail / N unpushed commits on VPS). User confirms or aborts.",
			Blocking:    false,
			AuditAction: "swarmlaunch.show_warnings",
		},
		{
			ID:          StageCreateSwarmSpec,
			Number:      4,
			Title:       "Create swarm spec + audit event",
			Description: "Materialize the typed SwarmLaunchSpec (composition, agent count, kickoff prompt template version, launch policy version) and write the canonical audit event before any side-effecting call.",
			Blocking:    true,
			AuditAction: "swarmlaunch.create_swarm_spec",
		},
		{
			ID:          StageNTMSpawn,
			Number:      5,
			Title:       "Call NTM spawn / add for each agent",
			Description: "For each agent in the composition, call ntm spawn (or ntm add for an existing session). Failures are recoverable: if N-of-M agents come up, mark partial-launch and surface the failure list.",
			Blocking:    true,
			AuditAction: "swarmlaunch.ntm_spawn",
		},
		{
			ID:          StageStaggerStarts,
			Number:      6,
			Title:       "Stagger agent starts ≥ 30s apart",
			Description: "Default thundering-herd protection: each agent's kickoff fires at least StaggerInterval after the previous one. Configurable per-launch but defaults to 30s per §7.3.",
			Blocking:    true,
			AuditAction: "swarmlaunch.stagger_starts",
		},
		{
			ID:          StageSendKickoffPrompt,
			Number:      7,
			Title:       "Send kickoff prompt (§7.3 template)",
			Description: "Render the kickoff prompt template (versioned in packages/schemas/prompts/swarm-kickoff/) with the agent's bead context + AGENTS.md/README pointers + the launch policy directives; deliver via NTM.",
			Blocking:    true,
			AuditAction: "swarmlaunch.send_kickoff_prompt",
		},
		{
			ID:          StageStartEventSubs,
			Number:      8,
			Title:       "Start event subscriptions",
			Description: "Subscribe to NTM panes, Agent Mail thread events, and br update events for the launched agents; persist sequence cursors so reconnect resumes cleanly.",
			Blocking:    true,
			AuditAction: "swarmlaunch.start_event_subscriptions",
		},
		{
			ID:          StageActivateTendingJobs,
			Number:      9,
			Title:       "Activate the project's tending jobs (§8)",
			Description: "Register the project's §8.4 tending-job set with the scheduler (hp-fb0); jobs begin firing per their schedules + event triggers. Phase 10 fully wires this stage.",
			Blocking:    true,
			AuditAction: "swarmlaunch.activate_tending_jobs",
		},
		{
			ID:          StageShowSwarmDashboard,
			Number:      10,
			Title:       "Show swarm dashboard",
			Description: "Route the desktop UI to the Stage 03 abstracted swarm dashboard (bead board + agent grid + Activity panel; no terminals per Guardrail 12).",
			Blocking:    false,
			AuditAction: "swarmlaunch.show_swarm_dashboard",
		},
	}
}

// LookupStage returns the stage for the given ID, or false on
// miss.
func LookupStage(id StageID) (Stage, bool) {
	for _, s := range SequenceCatalog() {
		if s.ID == id {
			return s, true
		}
	}
	return Stage{}, false
}

// DefaultStaggerInterval is the §7.3 default thundering-herd
// stagger; configurable per-launch.
const DefaultStaggerInterval = 30 * time.Second

// GateID identifies one §4.2 launch gate. The verify stage
// iterates these in order; the first failed gate aborts launch
// and surfaces a typed Problem.
type GateID string

const (
	GateVPSReady          GateID = "swarmlaunch.gate.vps_ready"
	GateProjectImported   GateID = "swarmlaunch.gate.project_imported"
	GatePlanLocked        GateID = "swarmlaunch.gate.plan_locked"
	GateBeadsFinalized    GateID = "swarmlaunch.gate.beads_finalized"
	GateLaunchReady       GateID = "swarmlaunch.gate.launch_ready"
	GateBuildQueuePolicy  GateID = "swarmlaunch.gate.build_queue_policy"
)

// Gate is one row in the §4.2 launch-gate catalog.
type Gate struct {
	ID            GateID `json:"id"`
	Title         string `json:"title"`
	Description   string `json:"description"`
	BlockerProblemType string `json:"blockerProblemType"`
}

// GateCatalog returns the §4.2 source-of-truth list of launch
// gates in evaluation order.
func GateCatalog() []Gate {
	return []Gate{
		{
			ID:                 GateVPSReady,
			Title:              "VPS ready",
			Description:        "Daemon /v1/health green; SSH tunnel established; bearer + WS-token both present.",
			BlockerProblemType: "urn:hoopoe:swarmlaunch/vps-not-ready",
		},
		{
			ID:                 GateProjectImported,
			Title:              "Project imported",
			Description:        "Project lifecycle state is `ready`; readiness gates green; local clone fetched.",
			BlockerProblemType: "urn:hoopoe:swarmlaunch/project-not-imported",
		},
		{
			ID:                 GatePlanLocked,
			Title:              "Plan locked",
			Description:        "Plan lifecycle state is `locked` or `revising` with a locked snapshot; bead conversion has a source plan version.",
			BlockerProblemType: "urn:hoopoe:swarmlaunch/plan-not-locked",
		},
		{
			ID:                 GateBeadsFinalized,
			Title:              "Beads created + finalized",
			Description:        "Plan-to-beads conversion complete; bead-set quality score available; ready frontier non-empty (or intentionally scoped).",
			BlockerProblemType: "urn:hoopoe:swarmlaunch/beads-not-finalized",
		},
		{
			ID:                 GateLaunchReady,
			Title:              "Launch ready",
			Description:        "NTM healthy; Agent Mail healthy; bv healthy; br ready --json non-empty (or intentionally scoped).",
			BlockerProblemType: "urn:hoopoe:swarmlaunch/launch-not-ready",
		},
		{
			ID:                 GateBuildQueuePolicy,
			Title:              "Build queue policy set",
			Description:        "BuildQueuePolicy declared (rch preferred when available; concurrency caps; flake / known-failure rails wired).",
			BlockerProblemType: "urn:hoopoe:swarmlaunch/build-queue-policy-missing",
		},
	}
}

// LookupGate returns the gate for the given ID, or false on miss.
func LookupGate(id GateID) (Gate, bool) {
	for _, g := range GateCatalog() {
		if g.ID == id {
			return g, true
		}
	}
	return Gate{}, false
}

// WarningID identifies one §7.3 launch warning. The Show Warnings
// stage iterates these and renders a user-dismissable confirmation.
type WarningID string

const (
	WarningDirtyGit            WarningID = "swarmlaunch.warning.dirty_git"
	WarningStaleReservations   WarningID = "swarmlaunch.warning.stale_reservations"
	WarningNoReadyBeads        WarningID = "swarmlaunch.warning.no_ready_beads"
	WarningLowDisk             WarningID = "swarmlaunch.warning.low_disk"
	WarningMissingAgentMail    WarningID = "swarmlaunch.warning.missing_agent_mail"
	WarningUnpushedVPSCommits  WarningID = "swarmlaunch.warning.unpushed_vps_commits"
)

// Warning is one row in the §7.3 launch-warning catalog.
type Warning struct {
	ID          WarningID `json:"id"`
	Title       string    `json:"title"`
	Description string    `json:"description"`
}

// WarningCatalog returns the §7.3 launch-warning catalog in
// rendering order.
func WarningCatalog() []Warning {
	return []Warning{
		{
			ID:          WarningDirtyGit,
			Title:       "Dirty Git",
			Description: "VPS working tree has uncommitted changes; agents starting now may stack edits on top of unfinished work.",
		},
		{
			ID:          WarningStaleReservations,
			Title:       "Stale reservations",
			Description: "Agent Mail reports reservations whose owners are no longer active; force-release via Diagnostics before launching to avoid blocked agents.",
		},
		{
			ID:          WarningNoReadyBeads,
			Title:       "No ready beads",
			Description: "br ready --json is empty; without unblocked work, agents will stall immediately. Confirm scope is intentional or run bv triage first.",
		},
		{
			ID:          WarningLowDisk,
			Title:       "Low disk",
			Description: "VPS reports disk pressure approaching the §10.1 threshold; large concurrent builds may hit ENOSPC. Run sbh cleanup first.",
		},
		{
			ID:          WarningMissingAgentMail,
			Title:       "Missing Agent Mail",
			Description: "MCP Agent Mail unreachable; agents cannot coordinate file reservations or share progress; coordinate-by-pane is not a substitute (Guardrail 8).",
		},
		{
			ID:          WarningUnpushedVPSCommits,
			Title:       "N unpushed commits on VPS",
			Description: "VPS working tree has commits ahead of origin; agents starting now will branch off a state origin doesn't yet have. Push first or accept the divergence.",
		},
	}
}

// LookupWarning returns the warning for the given ID, or false
// on miss.
func LookupWarning(id WarningID) (Warning, bool) {
	for _, w := range WarningCatalog() {
		if w.ID == id {
			return w, true
		}
	}
	return Warning{}, false
}

// PolicyID identifies one §7.3 default-launch-policy directive.
// The kickoff prompt template renders these into the agent's
// runtime context.
type PolicyID string

const (
	PolicyForceAgentsReadme           PolicyID = "swarmlaunch.policy.force_agents_readme_reread"
	PolicyRequireAgentMailRegistration PolicyID = "swarmlaunch.policy.require_agent_mail_registration"
	PolicyRequireBVTriageBeforeClaim  PolicyID = "swarmlaunch.policy.require_bv_triage_before_claim"
	PolicyMarkClaimedBeadsInProgress  PolicyID = "swarmlaunch.policy.mark_claimed_beads_in_progress"
	PolicyReserveFilesBeforeEdits     PolicyID = "swarmlaunch.policy.reserve_files_before_edits"
	PolicyIncludeBeadIDInArtifacts    PolicyID = "swarmlaunch.policy.include_bead_id_in_artifacts"
	PolicyUseRCHForBuilds             PolicyID = "swarmlaunch.policy.use_rch_for_builds"
	PolicyNeverInvokeBareBV           PolicyID = "swarmlaunch.policy.never_invoke_bare_bv"
	PolicyAvoidConcurrentBuilds       PolicyID = "swarmlaunch.policy.avoid_concurrent_builds"
	PolicySelfReviewBeforeClose       PolicyID = "swarmlaunch.policy.self_review_before_close"
	PolicyReportBlockersQuickly       PolicyID = "swarmlaunch.policy.report_blockers_quickly"
	PolicyNoCommunicationPurgatory    PolicyID = "swarmlaunch.policy.no_communication_purgatory"
)

// PolicyDirective is one row in the §7.3 default-launch-policy
// catalog.
type PolicyDirective struct {
	ID          PolicyID `json:"id"`
	Title       string   `json:"title"`
	Description string   `json:"description"`
}

// PolicyCatalog returns the §7.3 default-launch-policy directive
// catalog. The kickoff prompt template renders these so the
// agent runtime knows the swarm-wide rules.
func PolicyCatalog() []PolicyDirective {
	return []PolicyDirective{
		{
			ID:          PolicyForceAgentsReadme,
			Title:       "Force AGENTS.md and README reread",
			Description: "Every agent must reread AGENTS.md + README.md at kickoff time, even if context is preserved across sessions.",
		},
		{
			ID:          PolicyRequireAgentMailRegistration,
			Title:       "Require Agent Mail registration",
			Description: "Agents register an identity in Agent Mail before any reservation or message; coordinate-by-pane is forbidden (Guardrail 8).",
		},
		{
			ID:          PolicyRequireBVTriageBeforeClaim,
			Title:       "Require bv --robot-triage and br ready --json before claiming work",
			Description: "Agents read bv robot output (Guardrail 1) + br ready --json before selecting a bead; never bare bv.",
		},
		{
			ID:          PolicyMarkClaimedBeadsInProgress,
			Title:       "Mark claimed beads in_progress",
			Description: "On claim, set the bead's status to in_progress so other agents see the lock; release on close or hand-off.",
		},
		{
			ID:          PolicyReserveFilesBeforeEdits,
			Title:       "Reserve files before edits",
			Description: "Agent Mail file reservation (exclusive, ttl ≤ 1h) must wrap every edit batch; file-level conflicts are surfaced immediately.",
		},
		{
			ID:          PolicyIncludeBeadIDInArtifacts,
			Title:       "Include bead ID in mail subjects, reservation reasons, commit messages",
			Description: "Every produced artifact carries the bead ID for traceability and audit-log linking.",
		},
		{
			ID:          PolicyUseRCHForBuilds,
			Title:       "Use rch for builds/tests when configured",
			Description: "Build/test execution prefers the §1.1 rch substrate when available; agents do not run heavy compiles in their pane unless rch is unavailable (which is itself surfaced as a degraded capability).",
		},
		{
			ID:          PolicyNeverInvokeBareBV,
			Title:       "Never invoke bare bv (Guardrail 1)",
			Description: "Bare bv launches an interactive TUI and blocks the agent's pane indefinitely. Always use bv --robot-* surfaces.",
		},
		{
			ID:          PolicyAvoidConcurrentBuilds,
			Title:       "Avoid concurrent builds for the same project",
			Description: "Coordinate via the build queue (hp-977) and the Agent Mail thread; concurrent unrelated builds OK, concurrent same-project builds dedupe via cache key.",
		},
		{
			ID:          PolicySelfReviewBeforeClose,
			Title:       "Self-review with fresh eyes before review/close",
			Description: "Agents run a self-review (UBS or fresh-eyes pass) before requesting review or closing the bead.",
		},
		{
			ID:          PolicyReportBlockersQuickly,
			Title:       "Report blockers quickly",
			Description: "On any blocker exceeding the §8.7 silence threshold, the agent posts to its mail thread and pings the orchestrator instead of stalling.",
		},
		{
			ID:          PolicyNoCommunicationPurgatory,
			Title:       "Do not wait in communication purgatory",
			Description: "If a question to the orchestrator goes unanswered beyond the configured wait threshold, the agent surfaces the wait via Activity panel and proceeds with the safest alternative; agents never sit silent indefinitely.",
		},
	}
}

// LookupPolicy returns the policy for the given ID, or false on
// miss.
func LookupPolicy(id PolicyID) (PolicyDirective, bool) {
	for _, p := range PolicyCatalog() {
		if p.ID == id {
			return p, true
		}
	}
	return PolicyDirective{}, false
}
