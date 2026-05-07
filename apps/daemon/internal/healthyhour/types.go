package healthyhour

import "time"

// PreScriptOutcome is the typed §8.3.2 outcome of one tick's
// deterministic pre-script. Stored on every scheduler_metrics
// row.
type PreScriptOutcome string

const (
	// PreScriptWakeAgentFalse: the deterministic pre-script
	// found nothing actionable; the agent runtime does NOT
	// wake. The §8.6 healthy-hour invariant: this should be the
	// outcome on (almost) every tick during a healthy hour.
	PreScriptWakeAgentFalse PreScriptOutcome = "wakeAgent:false"

	// PreScriptWakeAgentTrue: the pre-script identified a state
	// the deterministic layer cannot resolve; the agent runtime
	// is woken. Allowed during a healthy hour only for
	// snapshot-health's optional drift check.
	PreScriptWakeAgentTrue PreScriptOutcome = "wakeAgent:true"

	// PreScriptDeterministicAction: the pre-script itself took a
	// deterministic action (e.g., push-stale-commits pushed a
	// branch). The agent runtime does NOT wake; cost stays at
	// zero; an Activity entry may fire if the action is
	// user-relevant.
	PreScriptDeterministicAction PreScriptOutcome = "deterministic-action"

	// PreScriptError: the pre-script itself errored (a bug, not
	// a detection). Audit fires; the scheduler logs the error;
	// retry policy decides whether to re-run.
	PreScriptError PreScriptOutcome = "error"
)

// AgentRunOutcome is the typed §8.3.2 outcome of one agent
// runtime invocation, populated when PreScriptOutcome ==
// PreScriptWakeAgentTrue.
type AgentRunOutcome string

const (
	// AgentRunSilent: the agent decided no action was warranted
	// and returned a [SILENT] response. Activity panel suppresses;
	// audit always records (Guardrail 10).
	AgentRunSilent AgentRunOutcome = "silent"

	// AgentRunSpoke: the agent produced a non-silent response
	// without an ActionPlan (e.g., orchestrator-chat answering a
	// user message). Activity may surface depending on routing.
	AgentRunSpoke AgentRunOutcome = "spoke"

	// AgentRunActionPlan: the agent emitted a typed §8.3.1
	// ActionPlan. The daemon executes (after approvals where
	// required) and verifies postconditions.
	AgentRunActionPlan AgentRunOutcome = "action-plan"

	// AgentRunTimeout: the agent runtime timed out before
	// emitting a final response.
	AgentRunTimeout AgentRunOutcome = "timeout"

	// AgentRunError: the agent runtime errored (CLI process
	// crash, network failure, malformed output).
	AgentRunError AgentRunOutcome = "error"
)

// SchedulerMetric is the typed shape of one row in the
// scheduler_metrics SQLite table (per §8.3.2). The healthy-hour
// validator consumes a 60-minute window of these rows.
type SchedulerMetric struct {
	JobID               string           `json:"jobId"`
	TickAt              time.Time        `json:"tickAt"`
	PreScriptDurationMs int64            `json:"preScriptDurationMs"`
	PreScriptOutcome    PreScriptOutcome `json:"preScriptOutcome"`

	// AgentRunOutcome is set only when PreScriptOutcome is
	// wakeAgent:true. Empty otherwise.
	AgentRunOutcome AgentRunOutcome `json:"agentRunOutcome,omitempty"`

	ApprovalsRequested int `json:"approvalsRequested"`
	ApprovalsGranted   int `json:"approvalsGranted"`
	TokensConsumed     int `json:"tokensConsumed"`

	// AuditEntryWritten is the boolean evidence that Guardrail 10
	// fired for this tick. The validator asserts this is true on
	// EVERY row regardless of wake/silence.
	AuditEntryWritten bool `json:"auditEntryWritten"`
}

// ActivityCounters captures the panel-noise observations the
// validator asserts against. Populated by the scheduler-metrics
// instrumentation alongside the per-tick rows.
type ActivityCounters struct {
	// TotalEvents is the total Activity panel events emitted
	// during the window EXCLUDING system heartbeats and the
	// `health_snapshot_updated` event (the latter is acceptable
	// once when snapshot-health fires its optional drift check).
	TotalEvents int `json:"totalEvents"`

	// UrgentDeliveries is the count of `hoopoe_activity_urgent`
	// deliveries during the window. Healthy hour: must be zero.
	UrgentDeliveries int `json:"urgentDeliveries"`

	// ForceReleaseActions, KillProcessActions, SwarmHaltActions
	// count the typed destructive recovery actions. Healthy
	// hour: all must be zero.
	ForceReleaseActions int `json:"forceReleaseActions"`
	KillProcessActions  int `json:"killProcessActions"`
	SwarmHaltActions    int `json:"swarmHaltActions"`
}

// Invariants is the §8.6 threshold table the validator enforces.
// Constants below are the defaults; the validator function takes
// an Invariants struct so chaos / sensitivity tests can override.
type Invariants struct {
	// MaxAgentRuns is the total agentRunOutcome != "" count over
	// 60 minutes. Default 1 (allow snapshot-health's optional
	// drift check); a regression in tend-swarm's no-detection
	// shortcut would push this above the threshold.
	MaxAgentRuns int `json:"maxAgentRuns"`

	// MaxAgentSpokeRuns is the total agentRunOutcome == "spoke"
	// count. Default 0 — silent or no-run only on a healthy hour.
	MaxAgentSpokeRuns int `json:"maxAgentSpokeRuns"`

	// MaxTokensConsumed is the aggregate tokensConsumed budget
	// across the window. Default 5,000 tokens (a small allowance
	// for the optional drift check).
	MaxTokensConsumed int `json:"maxTokensConsumed"`

	// MaxActivityEvents is the panel-noise cap (system
	// heartbeats + `health_snapshot_updated` already excluded).
	// Default 1.
	MaxActivityEvents int `json:"maxActivityEvents"`

	// AuditEntryPerTickRequired enforces Guardrail 10. Default
	// true — every metric row must have AuditEntryWritten == true.
	AuditEntryPerTickRequired bool `json:"auditEntryPerTickRequired"`
}

// DefaultInvariants returns the §8.6 thresholds as the validator
// applies them by default. Override only for sensitivity / chaos
// tests where the cap is intentionally relaxed.
func DefaultInvariants() Invariants {
	return Invariants{
		MaxAgentRuns:              1,
		MaxAgentSpokeRuns:         0,
		MaxTokensConsumed:         5000,
		MaxActivityEvents:         1,
		AuditEntryPerTickRequired: true,
	}
}
