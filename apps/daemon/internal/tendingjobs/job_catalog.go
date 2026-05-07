package tendingjobs

import "time"

// JobID identifies one §8.4 default tending job. Renaming is a
// schema-version event because audit logs and scheduler_metrics
// rows key on it.
type JobID string

const (
	JobTendSwarm             JobID = "tend-swarm"
	JobWatchSafetyThresholds JobID = "watch-safety-thresholds"
	JobPushStaleCommits      JobID = "push-stale-commits"
	JobSnapshotHealth        JobID = "snapshot-health"
	JobDriftCheck            JobID = "drift-check"
	JobReviewReadinessCheck  JobID = "review-readiness-check"
	JobOrchestratorChat      JobID = "orchestrator-chat"
)

// TriggerKind describes the schedule shape of a job. The default
// catalog uses Interval triggers for cron-like jobs and Event
// triggers for jobs woken by named events; some jobs have both
// (e.g., snapshot-health on event vps_push_completed AND every
// 10 minutes).
type TriggerKind string

const (
	TriggerInterval TriggerKind = "interval"
	TriggerEvent    TriggerKind = "event"
	TriggerCombined TriggerKind = "combined"
)

// Toolset names the daemon-side capabilities a job's pre-script
// is allowed to use. Each toolset is gated by the capability
// registry (§2.8); a missing capability blocks the job in
// degraded mode.
type Toolset string

const (
	ToolsetBR            Toolset = "br"
	ToolsetBV            Toolset = "bv"
	ToolsetNTM           Toolset = "ntm"
	ToolsetAgentMail     Toolset = "agent_mail"
	ToolsetGitRead       Toolset = "git_read"
	ToolsetGitWrite      Toolset = "git_write"
	ToolsetCAAM          Toolset = "caam"
	ToolsetCASR          Toolset = "casr"
	ToolsetBudget        Toolset = "budget"
	ToolsetCAUT          Toolset = "caut"
	ToolsetSRP           Toolset = "srp"
	ToolsetSBH           Toolset = "sbh"
	ToolsetPT            Toolset = "pt"
	ToolsetHealthAdapter Toolset = "health_adapter"
	ToolsetFileRead      Toolset = "file_read"
	ToolsetRano          Toolset = "rano"
)

// SkillID names a skill the agent runtime loads at run time when
// the pre-script returns wakeAgent:true. Skills come from `jsm`
// (preferred, SHA-256 pinned) or `jfp` (fallback, advisory
// version strings).
type SkillID string

const (
	SkillVibingWithNTM SkillID = "vibing-with-ntm"
	SkillNTM           SkillID = "ntm"
)

// Delivery names the Activity-panel routing surface the job's
// outputs flow into.
type Delivery string

const (
	// DeliveryActivityPanel: surfaces in the standard Activity
	// panel timeline. Suppressed for [SILENT] runs (Guardrail 10
	// audit always still fires).
	DeliveryActivityPanel Delivery = "hoopoe_activity_panel"

	// DeliveryActivityUrgent: surfaces with urgent prominence
	// (top-bar pulse + persistent banner) for safety-threshold
	// breaches. Healthy hours produce zero of these.
	DeliveryActivityUrgent Delivery = "hoopoe_activity_urgent"

	// DeliveryAuditOnly: writes only to the audit log; never
	// surfaces in Activity. Used by purely-mechanical jobs whose
	// inner state is forensic.
	DeliveryAuditOnly Delivery = "audit_only"
)

// JobSpec is the typed shape of one §8.4 default-tending-job row.
// Each job's pre-script implementation file under this package
// references its JobSpec by JobID at registration time so the
// dispatch table stays in sync with the catalog.
type JobSpec struct {
	// ID is the stable identifier (kebab-case names matching the
	// §8.4 reading; audit + scheduler_metrics key on this).
	ID JobID `json:"id"`

	// Description is the §8.4 one-paragraph description.
	Description string `json:"description"`

	// Trigger declares whether the job runs on an interval, on a
	// named event, or both.
	Trigger TriggerKind `json:"trigger"`

	// Interval is the cron-equivalent schedule for Interval /
	// Combined triggers. Zero for pure-event jobs.
	Interval time.Duration `json:"interval"`

	// EventTriggers lists the event names that wake this job
	// when Trigger is Event or Combined. Empty for pure-interval
	// jobs.
	EventTriggers []string `json:"eventTriggers,omitempty"`

	// Toolsets lists the daemon capabilities the pre-script is
	// allowed to use. The capability registry gates dispatch.
	Toolsets []Toolset `json:"toolsets"`

	// Skills lists the skills the agent runtime loads when the
	// pre-script returns wakeAgent:true. Empty for jobs that
	// never wake the agent (push-stale-commits,
	// watch-safety-thresholds, snapshot-health).
	Skills []SkillID `json:"skills,omitempty"`

	// AlwaysDeterministic indicates the job NEVER returns
	// wakeAgent:true (a §8.6 healthy-hour invariant guarantee).
	// hp-ilrr's CheckInvariants takes this into account when
	// classifying a row's outcome.
	AlwaysDeterministic bool `json:"alwaysDeterministic"`

	// Delivery is the Activity-panel routing surface for this
	// job's outputs.
	Delivery Delivery `json:"delivery"`

	// AuditAlways enforces Guardrail 10: the audit log records
	// every tick regardless of wake/silence. All §8.4 jobs
	// declare true.
	AuditAlways bool `json:"auditAlways"`

	// ExcludeFromHealthyHourInvariants marks this job as
	// event-driven user-facing — its scheduler_metrics rows must
	// not be counted by the §8.6 healthy-hour validator's
	// spoke / run / token-budget checks. The integration contract
	// healthyhour.Invariants.ExcludedJobIDs picks this up via
	// HealthyHourExcludedJobIDs() at wire-up time.
	//
	// Default false: background tending jobs participate in the
	// panel-noise floor. The exclusion exists because
	// orchestrator-chat (the literal §7.5 user-can-chat-with-
	// orchestrator agent) spokes by design when the user types
	// in the Activity panel; counting that as a §8.6 violation
	// would mark every interactive minute as unhealthy.
	//
	// Structural guardrails (audit-on-every-tick per Guardrail 10,
	// unknown PreScriptOutcome per §10.3, pre-script errors)
	// still apply to excluded jobs — exclusion is targeted, not
	// blanket.
	ExcludeFromHealthyHourInvariants bool `json:"excludeFromHealthyHourInvariants,omitempty"`

	// SubstrateBeads point at the implementation-owner beads
	// this job depends on so a dispatch failure routes triage.
	SubstrateBeads []string `json:"substrateBeads,omitempty"`
}

// Catalog returns the §8.4 source-of-truth list of default
// tending jobs in plan.md ordering.
func Catalog() []JobSpec {
	return []JobSpec{
		{
			ID:          JobTendSwarm,
			Description: "Reconciles NTM/br/bv/Mail/Git/CAAM; detects idle/wedged/rate-limited/stalled-bead candidates, duplicate claims, agents not using Agent Mail, agents not updating br; surfaces CAAM-reported account exhaustion as first-class detection. If none → wakeAgent:false.",
			Trigger:     TriggerInterval,
			Interval:    4 * time.Minute,
			Toolsets: []Toolset{
				ToolsetBR, ToolsetBV, ToolsetNTM, ToolsetAgentMail,
				ToolsetGitRead, ToolsetCAAM, ToolsetCASR,
			},
			Skills:         []SkillID{SkillVibingWithNTM, SkillNTM},
			Delivery:       DeliveryActivityPanel,
			AuditAlways:    true,
			SubstrateBeads: []string{"hp-209", "hp-v6cq", "hp-rnn"},
		},
		{
			ID:          JobWatchSafetyThresholds,
			Description: "Checks per-agent + per-swarm subscription quota caps (caut), rate-limit halts, disk pressure (srp), CPU/load (srp), daemon health. HARD THRESHOLD CROSSING emits typed deterministic action intents (budget breach → swarm.halt, disk pressure → sbh cleanup, wedged process → kill). Always wakeAgent:false.",
			Trigger:     TriggerInterval,
			Interval:    30 * time.Second,
			Toolsets: []Toolset{
				ToolsetBudget, ToolsetNTM, ToolsetCAUT,
				ToolsetSRP, ToolsetSBH, ToolsetPT,
			},
			AlwaysDeterministic: true,
			Delivery:            DeliveryActivityUrgent,
			AuditAlways:         true,
			SubstrateBeads:      []string{"hp-6bj", "hp-v6cq"},
		},
		{
			ID:          JobPushStaleCommits,
			Description: "Detects unpushed commits older than threshold (default 5 min); pushes via daemon (audit; policy). Never wakes agent — push policy is mechanical (§7.3).",
			Trigger:     TriggerInterval,
			Interval:    1 * time.Minute,
			Toolsets:    []Toolset{ToolsetGitWrite},
			AlwaysDeterministic: true,
			Delivery:            DeliveryActivityPanel,
			AuditAlways:         true,
		},
		{
			ID:          JobSnapshotHealth,
			Description: "Runs language-appropriate coverage/complexity tools; writes snapshot artifact; emits health_snapshot_updated event. Never wakes agent — measurement is mechanical.",
			Trigger:     TriggerCombined,
			Interval:    10 * time.Minute,
			EventTriggers: []string{
				"vps_push_completed",
			},
			Toolsets:            []Toolset{ToolsetHealthAdapter, ToolsetFileRead},
			AlwaysDeterministic: true,
			Delivery:            DeliveryActivityPanel,
			AuditAlways:         true,
			SubstrateBeads:      []string{"hp-3at"},
		},
		{
			ID:          JobDriftCheck,
			Description: "Checks 'many commits / few beads closed', 'P0 critical path stale', 'review findings clustering in same domain', 'code health worsens while beads close', 'agents create many beads without closing old ones'. If none → wakeAgent:false.",
			Trigger:     TriggerInterval,
			Interval:    30 * time.Minute,
			Toolsets: []Toolset{
				ToolsetBR, ToolsetBV, ToolsetGitRead, ToolsetHealthAdapter,
			},
			Skills:         []SkillID{SkillVibingWithNTM},
			Delivery:       DeliveryActivityPanel,
			AuditAlways:    true,
			SubstrateBeads: []string{"hp-209"},
		},
		{
			ID:          JobReviewReadinessCheck,
			Description: "Checks if implementation beads mostly closed, P0/P1 ready beads handled, no obvious stuck in-progress, latest health snapshot available — i.e., §9.1 prerequisites. If review-mode threshold not crossed → wakeAgent:false.",
			Trigger:     TriggerInterval,
			Interval:    15 * time.Minute,
			Toolsets:    []Toolset{ToolsetBR, ToolsetBV},
			Skills:      []SkillID{SkillVibingWithNTM},
			Delivery:    DeliveryActivityPanel,
			AuditAlways: true,
		},
		{
			ID:          JobOrchestratorChat,
			Description: "Pre-script gathers user message + recent activity + current swarm state. Agent responds as orchestrator. Read-only freely; mutations as typed ActionPlan. The literal §7.5 'user can chat with orchestrator agent' realization.",
			Trigger:     TriggerEvent,
			EventTriggers: []string{
				"user_message_in_activity_panel",
			},
			Toolsets: []Toolset{
				ToolsetBR, ToolsetBV, ToolsetNTM, ToolsetAgentMail,
				ToolsetGitRead, ToolsetHealthAdapter,
			},
			Skills:                           []SkillID{SkillVibingWithNTM, SkillNTM},
			Delivery:                         DeliveryActivityPanel,
			AuditAlways:                      true,
			ExcludeFromHealthyHourInvariants: true,
			SubstrateBeads:                   []string{"hp-tg6", "hp-v6n"},
		},
	}
}

// Lookup returns the spec for the given job ID, or false when
// the ID is unknown.
func Lookup(id JobID) (JobSpec, bool) {
	for _, spec := range Catalog() {
		if spec.ID == id {
			return spec, true
		}
	}
	return JobSpec{}, false
}

// AlwaysDeterministicJobs returns the subset of the catalog that
// never wakes the agent runtime. The hp-ilrr healthy-hour
// invariant validator uses this to confirm the no-wake guarantee
// is enforced at registration time.
func AlwaysDeterministicJobs() []JobSpec {
	out := make([]JobSpec, 0, 4)
	for _, spec := range Catalog() {
		if spec.AlwaysDeterministic {
			out = append(out, spec)
		}
	}
	return out
}

// JobsWithSkill returns the subset that loads a particular skill
// at agent-wake time. Used by the skill loader to plan what
// jsm/jfp must verify before dispatch.
func JobsWithSkill(skill SkillID) []JobSpec {
	out := make([]JobSpec, 0, 4)
	for _, spec := range Catalog() {
		for _, s := range spec.Skills {
			if s == skill {
				out = append(out, spec)
				break
			}
		}
	}
	return out
}

// HealthyHourExcludedJobIDs returns the set of JobID strings
// whose scheduler_metrics rows the §8.6 healthy-hour validator
// must skip when counting spoke / run / token budgets. The set
// is consumed by healthyhour.Invariants.ExcludedJobIDs at
// wire-up time so the two packages share one source of truth.
//
// Returns string keys (not JobID) so the healthyhour package
// stays decoupled from the tendingjobs JobID type — its
// SchedulerMetric.JobID field is a plain string for the same
// reason.
func HealthyHourExcludedJobIDs() map[string]bool {
	out := make(map[string]bool)
	for _, spec := range Catalog() {
		if spec.ExcludeFromHealthyHourInvariants {
			out[string(spec.ID)] = true
		}
	}
	return out
}
