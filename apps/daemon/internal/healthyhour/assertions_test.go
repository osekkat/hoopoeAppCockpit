package healthyhour

import (
	"testing"
	"time"
)

func tick(jobID string, outcome PreScriptOutcome) SchedulerMetric {
	return SchedulerMetric{
		JobID:               jobID,
		TickAt:              time.Date(2026, 5, 7, 12, 0, 0, 0, time.UTC),
		PreScriptDurationMs: 12,
		PreScriptOutcome:    outcome,
		AuditEntryWritten:   true,
	}
}

func wakeTick(jobID string, outcome AgentRunOutcome, tokens int) SchedulerMetric {
	m := tick(jobID, PreScriptWakeAgentTrue)
	m.AgentRunOutcome = outcome
	m.TokensConsumed = tokens
	return m
}

func TestHealthyHourPassesAllInvariants(t *testing.T) {
	t.Parallel()
	metrics := make([]SchedulerMetric, 0, 200)
	for i := 0; i < 120; i++ {
		metrics = append(metrics, tick("watch-safety-thresholds", PreScriptWakeAgentFalse))
	}
	for i := 0; i < 60; i++ {
		metrics = append(metrics, tick("push-stale-commits", PreScriptWakeAgentFalse))
	}
	for i := 0; i < 15; i++ {
		metrics = append(metrics, tick("tend-swarm", PreScriptWakeAgentFalse))
	}
	// One snapshot-health drift wake (allowed).
	metrics = append(metrics, wakeTick("snapshot-health", AgentRunSilent, 4500))

	counters := ActivityCounters{TotalEvents: 1}
	result := CheckInvariants(metrics, counters, DefaultInvariants())

	if !result.OK {
		t.Fatalf("healthy hour must satisfy invariants, got violations: %+v", result.Violations)
	}
	if result.AgentRunCount != 1 {
		t.Errorf("AgentRunCount = %d, want 1", result.AgentRunCount)
	}
	if result.TokensConsumed != 4500 {
		t.Errorf("TokensConsumed = %d, want 4500", result.TokensConsumed)
	}
	if result.AuditedTickCount != result.TickCount {
		t.Errorf("audit-on-every-tick should equal TickCount: audited=%d tickCount=%d",
			result.AuditedTickCount, result.TickCount)
	}
}

func TestRegressionInTendSwarmShortcutFailsTooManyAgentRuns(t *testing.T) {
	t.Parallel()
	metrics := []SchedulerMetric{
		wakeTick("tend-swarm", AgentRunSilent, 1000),
		wakeTick("tend-swarm", AgentRunSilent, 1000),
		wakeTick("tend-swarm", AgentRunSilent, 1000),
	}
	counters := ActivityCounters{}
	result := CheckInvariants(metrics, counters, DefaultInvariants())
	if result.OK {
		t.Fatal("3 agent runs must violate MaxAgentRuns=1 default")
	}
	if !hasKind(result, ViolationTooManyAgentRuns) {
		t.Errorf("expected too_many_agent_runs, got %+v", result.Violations)
	}
}

func TestAgentSpokeOnHealthyHourFailsAgentSpoke(t *testing.T) {
	t.Parallel()
	metrics := []SchedulerMetric{
		wakeTick("orchestrator-chat", AgentRunSpoke, 1500),
	}
	counters := ActivityCounters{TotalEvents: 1}
	result := CheckInvariants(metrics, counters, DefaultInvariants())
	if result.OK {
		t.Fatal("an agent that 'spoke' on a healthy hour must violate the panel-noise floor")
	}
	if !hasKind(result, ViolationAgentSpoke) {
		t.Errorf("expected agent_spoke, got %+v", result.Violations)
	}
}

func TestTokenBudgetCrossedFailsTokenBudget(t *testing.T) {
	t.Parallel()
	metrics := []SchedulerMetric{
		wakeTick("snapshot-health", AgentRunSilent, 7000),
	}
	counters := ActivityCounters{}
	result := CheckInvariants(metrics, counters, DefaultInvariants())
	if !hasKind(result, ViolationTokenBudgetExceeded) {
		t.Errorf("expected token_budget_exceeded for tokens=7000 vs default 5000, got %+v", result.Violations)
	}
}

func TestActivityNoiseExceededFailsTooManyEvents(t *testing.T) {
	t.Parallel()
	metrics := []SchedulerMetric{tick("tend-swarm", PreScriptWakeAgentFalse)}
	counters := ActivityCounters{TotalEvents: 5}
	result := CheckInvariants(metrics, counters, DefaultInvariants())
	if !hasKind(result, ViolationTooManyActivityEvents) {
		t.Errorf("expected too_many_activity_events, got %+v", result.Violations)
	}
}

func TestUrgentDeliveryAlwaysFailsHealthyHour(t *testing.T) {
	t.Parallel()
	metrics := []SchedulerMetric{tick("tend-swarm", PreScriptWakeAgentFalse)}
	counters := ActivityCounters{UrgentDeliveries: 1}
	result := CheckInvariants(metrics, counters, DefaultInvariants())
	if !hasKind(result, ViolationUrgentDelivery) {
		t.Errorf("expected urgent_delivery, got %+v", result.Violations)
	}
}

func TestDestructiveRecoveryActionFailsHealthyHour(t *testing.T) {
	t.Parallel()
	metrics := []SchedulerMetric{tick("tend-swarm", PreScriptWakeAgentFalse)}
	counters := ActivityCounters{ForceReleaseActions: 1}
	result := CheckInvariants(metrics, counters, DefaultInvariants())
	if !hasKind(result, ViolationDestructiveRecoveryAction) {
		t.Errorf("expected destructive_recovery_action for force-release, got %+v", result.Violations)
	}
}

func TestMissingAuditEntryFailsAuditMissing(t *testing.T) {
	t.Parallel()
	m := tick("tend-swarm", PreScriptWakeAgentFalse)
	m.AuditEntryWritten = false
	metrics := []SchedulerMetric{m}
	counters := ActivityCounters{}
	result := CheckInvariants(metrics, counters, DefaultInvariants())
	if !hasKind(result, ViolationAuditMissing) {
		t.Errorf("expected audit_missing (Guardrail 10), got %+v", result.Violations)
	}
}

func TestUnknownPreScriptOutcomeFailsLoudly(t *testing.T) {
	t.Parallel()
	m := tick("tend-swarm", PreScriptOutcome("future-state-not-yet-defined"))
	metrics := []SchedulerMetric{m}
	counters := ActivityCounters{}
	result := CheckInvariants(metrics, counters, DefaultInvariants())
	if !hasKind(result, ViolationUnknownPreScriptOutcome) {
		t.Errorf("expected unknown_pre_script_outcome — extending the enum requires §10.3 schema-version event")
	}
}

func TestPreScriptErrorFailsHealthyHour(t *testing.T) {
	t.Parallel()
	m := tick("tend-swarm", PreScriptError)
	metrics := []SchedulerMetric{m}
	counters := ActivityCounters{}
	result := CheckInvariants(metrics, counters, DefaultInvariants())
	if !hasKind(result, ViolationPreScriptError) {
		t.Errorf("expected pre_script_error for errored pre-script, got %+v", result.Violations)
	}
}

func TestValidatorReportsAllViolationsNotJustFirst(t *testing.T) {
	t.Parallel()
	metrics := []SchedulerMetric{
		wakeTick("tend-swarm", AgentRunSpoke, 7000),
	}
	counters := ActivityCounters{
		TotalEvents:        5,
		UrgentDeliveries:   2,
		KillProcessActions: 1,
	}
	result := CheckInvariants(metrics, counters, DefaultInvariants())
	if len(result.Violations) < 4 {
		t.Errorf("expected ≥4 violations (agent_spoke + token_budget + activity + urgent + destructive), got %d: %+v",
			len(result.Violations), result.Violations)
	}
}

func TestAuditEntryRequiredCanBeRelaxedForChaosTests(t *testing.T) {
	t.Parallel()
	m := tick("tend-swarm", PreScriptWakeAgentFalse)
	m.AuditEntryWritten = false
	metrics := []SchedulerMetric{m}
	counters := ActivityCounters{}
	inv := DefaultInvariants()
	inv.AuditEntryPerTickRequired = false
	result := CheckInvariants(metrics, counters, inv)
	if !result.OK {
		t.Errorf("relaxed audit requirement should let the row pass: %+v", result.Violations)
	}
}

func TestDeterministicActionCountsAsHealthyOutcome(t *testing.T) {
	t.Parallel()
	m := tick("push-stale-commits", PreScriptDeterministicAction)
	if !IsHealthyOutcome(m) {
		t.Errorf("deterministic-action must count as a healthy outcome (no agent wake, zero LLM cost)")
	}
}

func TestWakeAgentFalseCountsAsHealthyOutcome(t *testing.T) {
	t.Parallel()
	m := tick("tend-swarm", PreScriptWakeAgentFalse)
	if !IsHealthyOutcome(m) {
		t.Errorf("wakeAgent:false must count as a healthy outcome")
	}
}

func TestWakeAgentTrueDoesNotCountAsHealthy(t *testing.T) {
	t.Parallel()
	m := tick("snapshot-health", PreScriptWakeAgentTrue)
	if IsHealthyOutcome(m) {
		t.Errorf("wakeAgent:true must NOT count as healthy (LLM did wake; spend will be non-zero)")
	}
}

func TestPreScriptErrorDoesNotCountAsHealthy(t *testing.T) {
	t.Parallel()
	m := tick("tend-swarm", PreScriptError)
	if IsHealthyOutcome(m) {
		t.Errorf("error outcome must NOT count as healthy (something is wrong)")
	}
}

func TestHealthyOutcomeRatioOnHealthyHourIs100Percent(t *testing.T) {
	t.Parallel()
	metrics := []SchedulerMetric{
		tick("tend-swarm", PreScriptWakeAgentFalse),
		tick("watch-safety-thresholds", PreScriptWakeAgentFalse),
		tick("push-stale-commits", PreScriptDeterministicAction),
	}
	if got := HealthyOutcomeRatio(metrics); got != 1.0 {
		t.Errorf("HealthyOutcomeRatio = %f, want 1.0", got)
	}
}

func TestHealthyOutcomeRatioReflectsRegression(t *testing.T) {
	t.Parallel()
	metrics := []SchedulerMetric{
		tick("tend-swarm", PreScriptWakeAgentFalse),
		wakeTick("tend-swarm", AgentRunSilent, 1000),
		wakeTick("tend-swarm", AgentRunSilent, 1000),
	}
	if got := HealthyOutcomeRatio(metrics); got >= 0.5 {
		t.Errorf("HealthyOutcomeRatio = %f, want < 0.5 (2 of 3 rows are wakes)", got)
	}
}

func TestHealthyOutcomeRatioOnEmptyReturnsZero(t *testing.T) {
	t.Parallel()
	if got := HealthyOutcomeRatio(nil); got != 0 {
		t.Errorf("HealthyOutcomeRatio on empty must be 0, got %f", got)
	}
}

func TestDefaultInvariantsMatchSection86Table(t *testing.T) {
	t.Parallel()
	inv := DefaultInvariants()
	if inv.MaxAgentRuns != 1 {
		t.Errorf("MaxAgentRuns = %d, want 1", inv.MaxAgentRuns)
	}
	if inv.MaxAgentSpokeRuns != 0 {
		t.Errorf("MaxAgentSpokeRuns = %d, want 0", inv.MaxAgentSpokeRuns)
	}
	if inv.MaxTokensConsumed != 5000 {
		t.Errorf("MaxTokensConsumed = %d, want 5000", inv.MaxTokensConsumed)
	}
	if inv.MaxActivityEvents != 1 {
		t.Errorf("MaxActivityEvents = %d, want 1", inv.MaxActivityEvents)
	}
	if !inv.AuditEntryPerTickRequired {
		t.Error("AuditEntryPerTickRequired must default to true (Guardrail 10)")
	}
}

func hasKind(result CheckResult, kind ViolationKind) bool {
	for _, v := range result.Violations {
		if v.Kind == kind {
			return true
		}
	}
	return false
}
