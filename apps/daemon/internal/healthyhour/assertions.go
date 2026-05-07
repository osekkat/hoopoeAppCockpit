package healthyhour

import "fmt"

// ViolationKind classifies one §8.6 invariant breach.
type ViolationKind string

const (
	// ViolationTooManyAgentRuns: the total agent-run count
	// exceeded MaxAgentRuns. The most likely cause is a
	// regression in a deterministic pre-script that started
	// returning wakeAgent:true on every tick.
	ViolationTooManyAgentRuns ViolationKind = "too_many_agent_runs"

	// ViolationAgentSpoke: the agent spoke (non-silent, non-
	// action-plan response) on a healthy hour. Even one spoke
	// run violates §8.6's panel-noise floor.
	ViolationAgentSpoke ViolationKind = "agent_spoke"

	// ViolationTokenBudgetExceeded: aggregate tokensConsumed
	// crossed MaxTokensConsumed. Indicates either too many wakes
	// or one wake that produced an unexpectedly large response.
	ViolationTokenBudgetExceeded ViolationKind = "token_budget_exceeded"

	// ViolationTooManyActivityEvents: the panel-noise cap was
	// crossed (excluding system heartbeats and
	// `health_snapshot_updated`).
	ViolationTooManyActivityEvents ViolationKind = "too_many_activity_events"

	// ViolationUrgentDelivery: an `hoopoe_activity_urgent`
	// delivery fired during a healthy hour. Healthy hours never
	// produce urgent deliveries.
	ViolationUrgentDelivery ViolationKind = "urgent_delivery"

	// ViolationDestructiveRecoveryAction: a force-release /
	// kill-process / swarm-halt action fired during a healthy
	// hour. None of these should happen when the system is
	// healthy.
	ViolationDestructiveRecoveryAction ViolationKind = "destructive_recovery_action"

	// ViolationAuditMissing: a metric row was missing
	// AuditEntryWritten == true. Guardrail 10 violation: audit
	// must fire on every tick regardless of wake / silence.
	ViolationAuditMissing ViolationKind = "audit_missing"

	// ViolationUnknownPreScriptOutcome: a row carries a
	// PreScriptOutcome value not in the closed set. The
	// validator refuses to silently accept new states — extending
	// the enum requires a §10.3 schema-version event.
	ViolationUnknownPreScriptOutcome ViolationKind = "unknown_pre_script_outcome"

	// ViolationPreScriptError: a deterministic pre-script errored
	// during a healthy-hour window. Audit still fires, but a
	// pre-script crash is itself a regression in the no-detection
	// shortcut path and must not pass as healthy.
	ViolationPreScriptError ViolationKind = "pre_script_error"
)

// Violation is one breach of the §8.6 invariants the validator
// found in the metric window.
type Violation struct {
	Kind      ViolationKind `json:"kind"`
	Detail    string        `json:"detail,omitempty"`
	JobID     string        `json:"jobId,omitempty"`
	Observed  int           `json:"observed,omitempty"`
	Threshold int           `json:"threshold,omitempty"`
}

// CheckResult is the validator's typed verdict over a 60-minute
// window of scheduler_metrics rows + activity counters. Empty
// Violations means "the window satisfies the §8.6 invariants".
type CheckResult struct {
	OK         bool        `json:"ok"`
	Violations []Violation `json:"violations,omitempty"`

	// Counts the validator computed during inspection. Surfaced
	// for log lines and test-evidence files even when OK is true.
	AgentRunCount    int `json:"agentRunCount"`
	AgentSpokeCount  int `json:"agentSpokeCount"`
	TokensConsumed   int `json:"tokensConsumed"`
	TickCount        int `json:"tickCount"`
	AuditedTickCount int `json:"auditedTickCount"`
}

// CheckInvariants takes a 60-minute window of scheduler_metrics
// rows and the matching ActivityCounters, plus the threshold
// table to enforce, and returns a typed CheckResult.
//
// The function is pure: no clock reads, no DB calls, no logger.
// The instrumentation layer collects rows + counters; this
// function decides whether they satisfy §8.6.
//
// The validator does NOT short-circuit at the first violation —
// it reports every breach in one pass so test failures surface
// the full diagnostic instead of one breach at a time.
func CheckInvariants(
	metrics []SchedulerMetric,
	counters ActivityCounters,
	inv Invariants,
) CheckResult {
	result := CheckResult{
		OK:        true,
		TickCount: len(metrics),
	}

	knownOutcomes := map[PreScriptOutcome]bool{
		PreScriptWakeAgentFalse:      true,
		PreScriptWakeAgentTrue:       true,
		PreScriptDeterministicAction: true,
		PreScriptError:               true,
	}

	for _, row := range metrics {
		if !knownOutcomes[row.PreScriptOutcome] {
			result.Violations = append(result.Violations, Violation{
				Kind:   ViolationUnknownPreScriptOutcome,
				Detail: fmt.Sprintf("unknown PreScriptOutcome %q (extending enum requires schema-version event per §10.3)", row.PreScriptOutcome),
				JobID:  row.JobID,
			})
		}
		if row.PreScriptOutcome == PreScriptError {
			result.Violations = append(result.Violations, Violation{
				Kind:   ViolationPreScriptError,
				Detail: "deterministic pre-script errored during a healthy-hour window",
				JobID:  row.JobID,
			})
		}
		if row.AgentRunOutcome != "" {
			result.AgentRunCount++
			if row.AgentRunOutcome == AgentRunSpoke {
				result.AgentSpokeCount++
			}
		}
		result.TokensConsumed += row.TokensConsumed
		if row.AuditEntryWritten {
			result.AuditedTickCount++
		} else if inv.AuditEntryPerTickRequired {
			result.Violations = append(result.Violations, Violation{
				Kind:   ViolationAuditMissing,
				Detail: "audit must fire on every tick regardless of wake/silence (Guardrail 10)",
				JobID:  row.JobID,
			})
		}
	}

	if result.AgentRunCount > inv.MaxAgentRuns {
		result.Violations = append(result.Violations, Violation{
			Kind:      ViolationTooManyAgentRuns,
			Detail:    "deterministic pre-scripts must shortcut almost every tick during a healthy hour",
			Observed:  result.AgentRunCount,
			Threshold: inv.MaxAgentRuns,
		})
	}

	if result.AgentSpokeCount > inv.MaxAgentSpokeRuns {
		result.Violations = append(result.Violations, Violation{
			Kind:      ViolationAgentSpoke,
			Detail:    "even one spoke run violates the §8.6 panel-noise floor on a healthy hour",
			Observed:  result.AgentSpokeCount,
			Threshold: inv.MaxAgentSpokeRuns,
		})
	}

	if result.TokensConsumed > inv.MaxTokensConsumed {
		result.Violations = append(result.Violations, Violation{
			Kind:      ViolationTokenBudgetExceeded,
			Detail:    "aggregate tokensConsumed crossed MaxTokensConsumed — likely a regression in a no-detection shortcut",
			Observed:  result.TokensConsumed,
			Threshold: inv.MaxTokensConsumed,
		})
	}

	if counters.TotalEvents > inv.MaxActivityEvents {
		result.Violations = append(result.Violations, Violation{
			Kind:      ViolationTooManyActivityEvents,
			Detail:    "Activity panel noise exceeded the healthy-hour cap (system heartbeats + health_snapshot_updated already excluded)",
			Observed:  counters.TotalEvents,
			Threshold: inv.MaxActivityEvents,
		})
	}

	if counters.UrgentDeliveries > 0 {
		result.Violations = append(result.Violations, Violation{
			Kind:     ViolationUrgentDelivery,
			Detail:   "hoopoe_activity_urgent must not fire on a healthy hour",
			Observed: counters.UrgentDeliveries,
		})
	}

	destructive := counters.ForceReleaseActions + counters.KillProcessActions + counters.SwarmHaltActions
	if destructive > 0 {
		result.Violations = append(result.Violations, Violation{
			Kind:     ViolationDestructiveRecoveryAction,
			Detail:   fmt.Sprintf("destructive recovery actions on a healthy hour: forceRelease=%d killProcess=%d swarmHalt=%d", counters.ForceReleaseActions, counters.KillProcessActions, counters.SwarmHaltActions),
			Observed: destructive,
		})
	}

	if len(result.Violations) > 0 {
		result.OK = false
	}
	return result
}

// IsHealthyOutcome returns true when the row's outcome counts as
// "healthy" — wakeAgent:false or a deterministic-action without
// agent wake. Used by the regression-tripwire test (the failing-
// tend-swarm fixture should produce a low IsHealthyOutcome ratio).
func IsHealthyOutcome(metric SchedulerMetric) bool {
	switch metric.PreScriptOutcome {
	case PreScriptWakeAgentFalse, PreScriptDeterministicAction:
		return true
	default:
		return false
	}
}

// HealthyOutcomeRatio is the fraction of rows that are healthy
// outcomes. Healthy hour expects a value > 0.99; a regression
// pushes it down sharply.
func HealthyOutcomeRatio(metrics []SchedulerMetric) float64 {
	if len(metrics) == 0 {
		return 0
	}
	healthy := 0
	for _, m := range metrics {
		if IsHealthyOutcome(m) {
			healthy++
		}
	}
	return float64(healthy) / float64(len(metrics))
}
