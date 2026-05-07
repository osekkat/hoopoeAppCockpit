package ratelimit

import (
	"testing"
)

func sig(source SignalSource, active bool, weight float64, raw map[string]any) ContributingSignal {
	return ContributingSignal{
		Source:   source,
		Active:   active,
		Weight:   weight,
		RawValue: raw,
	}
}

func degradedSig(source SignalSource, weight float64) ContributingSignal {
	return ContributingSignal{
		Source:   source,
		Active:   false,
		Weight:   weight,
		Degraded: true,
	}
}

func TestAllSixSignalsQuietProducesNone(t *testing.T) {
	t.Parallel()
	signals := []ContributingSignal{
		sig(SourceCaut, false, 0.4, nil),
		sig(SourceCAAM, false, 0.3, nil),
		sig(SourceCLIStatus, false, 0.5, nil),
		sig(SourceNTMPane, false, 0.2, nil),
		sig(SourceLongNoOutput, false, 0.1, nil),
		sig(SourceRano, false, 0.2, nil),
	}
	state := Classify("agent-1", 1, signals, true)
	if state.Severity != SeverityNone {
		t.Fatalf("severity = %s, want none", state.Severity)
	}
	if state.Confidence < 0.9 {
		t.Fatalf("confidence = %f, want >= 0.9", state.Confidence)
	}
	if state.RecommendedAction.Type != ActionNone {
		t.Fatalf("action = %s, want no_action", state.RecommendedAction.Type)
	}
	if len(state.ContributingSignals) != 6 {
		t.Fatalf("expected all 6 signals preserved for audit, got %d", len(state.ContributingSignals))
	}
}

func TestCautAloneAt95PercentProducesSoft(t *testing.T) {
	t.Parallel()
	signals := []ContributingSignal{
		sig(SourceCaut, true, 0.4, map[string]any{"provider": "claude_max", "percent": 95.0, "window": "daily"}),
	}
	state := Classify("agent-1", 1, signals, true)
	if state.Severity != SeveritySoft {
		t.Fatalf("severity = %s, want soft", state.Severity)
	}
	if state.RecommendedAction.Type != ActionSendMarchingOrders {
		t.Fatalf("action = %s, want agent.send_marching_orders", state.RecommendedAction.Type)
	}
}

func TestCautPlusCLIStatusProducesHardWithSwitchAccount(t *testing.T) {
	t.Parallel()
	signals := []ContributingSignal{
		sig(SourceCaut, true, 0.4, map[string]any{"percent": 95.0}),
		sig(SourceCLIStatus, true, 0.5, map[string]any{"matched_pattern": "rate_limit_exceeded"}),
	}
	state := Classify("agent-1", 1, signals, true)
	if state.Severity != SeverityHard {
		t.Fatalf("severity = %s, want hard", state.Severity)
	}
	if state.RecommendedAction.Type != ActionSwitchAccount {
		t.Fatalf("action = %s, want caam.switch_account", state.RecommendedAction.Type)
	}
}

func TestCAAMLockoutPlusNTMPlusRanoProducesHardWithHighConfidence(t *testing.T) {
	t.Parallel()
	signals := []ContributingSignal{
		sig(SourceCAAM, true, 0.3, map[string]any{"account_status": "rate_limited", "lockout_until": "2026-05-07T15:00:00Z"}),
		sig(SourceNTMPane, true, 0.2, map[string]any{"pattern_id": "claude-rate-limit"}),
		sig(SourceRano, true, 0.2, map[string]any{"recent_4xx_count": 7, "p95_latency_delta_ms": 4200}),
	}
	state := Classify("agent-1", 1, signals, true)
	if state.Severity != SeverityHard {
		t.Fatalf("severity = %s, want hard", state.Severity)
	}
	if state.Confidence < 0.85 {
		t.Fatalf("confidence = %f, want >= 0.85", state.Confidence)
	}
}

func TestAllProvidersExhaustedInCAAMProducesExhausted(t *testing.T) {
	t.Parallel()
	signals := []ContributingSignal{
		sig(SourceCAAM, true, 0.3, map[string]any{"account_status": "rate_limited", "all_accounts_exhausted": true}),
	}
	state := Classify("agent-1", 1, signals, false)
	if state.Severity != SeverityExhausted {
		t.Fatalf("severity = %s, want exhausted", state.Severity)
	}
	if state.RecommendedAction.Type != ActionPauseAgent {
		t.Fatalf("action = %s, want swarm.pause_agent", state.RecommendedAction.Type)
	}
}

func TestLongNoOutputAloneCappedAtSoft(t *testing.T) {
	t.Parallel()
	signals := []ContributingSignal{
		sig(SourceLongNoOutput, true, 0.1, map[string]any{"silence_seconds": 180, "agent_last_active_age_seconds": 420}),
	}
	state := Classify("agent-1", 1, signals, true)
	if state.Severity != SeveritySoft {
		t.Fatalf("severity = %s, want soft (long-no-output alone must be capped)", state.Severity)
	}
	if state.RecommendedAction.Type != ActionSendMarchingOrders {
		t.Fatalf("action = %s, want agent.send_marching_orders", state.RecommendedAction.Type)
	}
}

func TestCautDegradedPlusCLIStatusStillProducesHard(t *testing.T) {
	t.Parallel()
	signals := []ContributingSignal{
		degradedSig(SourceCaut, 0.4),
		sig(SourceCLIStatus, true, 0.5, map[string]any{"matched_pattern": "rate_limit_exceeded"}),
	}
	state := Classify("agent-1", 1, signals, true)
	if state.Severity != SeverityHard {
		t.Fatalf("severity = %s, want hard (degraded caut must not block hard classification)", state.Severity)
	}
	// caut signal preserved as degraded, not silently dropped.
	var foundDegraded bool
	for _, s := range state.ContributingSignals {
		if s.Source == SourceCaut && s.Degraded {
			foundDegraded = true
		}
	}
	if !foundDegraded {
		t.Fatalf("degraded caut signal must be preserved in audit output")
	}
}

func TestHardWithoutHealthyCAAMFallsBackToCasrResume(t *testing.T) {
	t.Parallel()
	signals := []ContributingSignal{
		sig(SourceCLIStatus, true, 0.5, map[string]any{"matched_pattern": "rate_limit_exceeded"}),
	}
	state := Classify("agent-1", 1, signals, false)
	if state.Severity != SeverityHard {
		t.Fatalf("severity = %s, want hard", state.Severity)
	}
	if state.RecommendedAction.Type != ActionResumeSession {
		t.Fatalf("action = %s, want casr.resume_session", state.RecommendedAction.Type)
	}
	if state.RecommendedAction.BlockedBy == "" {
		t.Fatalf("expected BlockedBy set when no healthy CAAM account; got empty")
	}
}

func TestSequenceCursorIsThreaded(t *testing.T) {
	t.Parallel()
	state := Classify("agent-1", 42, nil, true)
	if state.SequenceCursor != 42 {
		t.Fatalf("sequence cursor = %d, want 42", state.SequenceCursor)
	}
}

func TestEmptySignalsProducesNoneWithZeroConfidence(t *testing.T) {
	t.Parallel()
	state := Classify("agent-1", 1, nil, true)
	if state.Severity != SeverityNone {
		t.Fatalf("severity = %s, want none", state.Severity)
	}
	if state.Confidence != 0 {
		t.Fatalf("confidence = %f, want 0 (nothing to classify on)", state.Confidence)
	}
}

func TestSeverityRankOrdering(t *testing.T) {
	t.Parallel()
	if SeverityExhausted.Rank() <= SeverityHard.Rank() ||
		SeverityHard.Rank() <= SeveritySoft.Rank() ||
		SeveritySoft.Rank() <= SeverityNone.Rank() {
		t.Fatalf("severity ranks non-monotonic: none=%d soft=%d hard=%d exhausted=%d",
			SeverityNone.Rank(), SeveritySoft.Rank(), SeverityHard.Rank(), SeverityExhausted.Rank())
	}
}

func TestContributingSignalsPreservedExactly(t *testing.T) {
	t.Parallel()
	in := []ContributingSignal{
		sig(SourceCaut, true, 0.4, map[string]any{"k": "v"}),
		sig(SourceRano, false, 0.2, nil),
	}
	state := Classify("agent-1", 1, in, true)
	if len(state.ContributingSignals) != len(in) {
		t.Fatalf("len(ContributingSignals) = %d, want %d", len(state.ContributingSignals), len(in))
	}
	if state.ContributingSignals[0].Source != SourceCaut {
		t.Fatalf("signal order was not preserved")
	}
}

func TestClassificationIsDeterministic(t *testing.T) {
	t.Parallel()
	signals := []ContributingSignal{
		sig(SourceCaut, true, 0.4, map[string]any{"percent": 95.0}),
		sig(SourceCLIStatus, true, 0.5, nil),
	}
	a := Classify("agent-1", 1, signals, true)
	b := Classify("agent-1", 1, signals, true)
	if a.Severity != b.Severity || a.Confidence != b.Confidence ||
		a.RecommendedAction.Type != b.RecommendedAction.Type {
		t.Fatalf("classifier is non-deterministic on identical inputs:\n a=%+v\n b=%+v", a, b)
	}
}
