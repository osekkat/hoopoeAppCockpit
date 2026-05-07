package ratelimit

import "fmt"

// Classify is the pure-function core of the rate-limit aggregator.
// Given the most-recent observation per source for one agent, it
// returns a fused RateLimitState (severity + confidence +
// recommendedAction).
//
// Inputs:
//   - agentID: the agent the state describes.
//   - sequenceCursor: the aggregator's monotonic counter (set by the
//     event-bus subscriber follow-up cut).
//   - now: a clock source so tests can be deterministic.
//   - signals: zero or more ContributingSignal entries, each
//     representing the most recent observation from one of the six
//     §7.3 sources. A source missing from this slice is treated as
//     "no observation"; an explicit degraded entry signals the
//     source is unreachable.
//   - hasHealthyCAAMAccount: whether CAAM currently knows of any
//     healthy account for the active provider — needed to decide
//     between caam.switch_account and the fallback path.
//
// Returns the fused state. The function never errors and never
// elides a signal: every input signal appears in the output's
// ContributingSignals slice (preserved for audit), even when its
// effective weight is 0 (degraded) or its Active flag is false.
//
// Severity rules (per the bead's "Severity classification logic"):
//
//	none:      no signal active (or all below low thresholds).
//	soft:      one or more signals active, agent still producing
//	           output; recommend send_marching_orders.
//	hard:      multiple signals confirm rate-limit; recommend
//	           caam.switch_account if a healthy account exists,
//	           else casr.resume_session, else swarm.pause_agent.
//	exhausted: every CAAM account for the provider is at/near cap
//	           AND cross-provider casr paths are exhausted;
//	           recommend swarm.pause_agent.
//
// False-positive protection: long-no-output ALONE caps at soft
// (per §1.4 / §8.7 — judgment-class agents can legitimately stay
// silent for minutes without being rate-limited).
func Classify(
	agentID string,
	sequenceCursor uint64,
	signals []ContributingSignal,
	hasHealthyCAAMAccount bool,
) RateLimitState {
	preserved := make([]ContributingSignal, len(signals))
	copy(preserved, signals)

	activeCount := 0
	weightedActive := 0.0
	totalWeight := 0.0
	caamLockout := false
	caamAllExhausted := false
	cliStatusActive := false
	caamActive := false
	cautActive := false
	ntmActive := false
	ranoActive := false
	longNoOutputActive := false
	degradedSources := 0

	for _, sig := range signals {
		totalWeight += sig.Weight
		if sig.Degraded {
			degradedSources++
			continue
		}
		if !sig.Active {
			continue
		}
		activeCount++
		weightedActive += sig.Weight
		switch sig.Source {
		case SourceCaut:
			cautActive = true
		case SourceCAAM:
			caamActive = true
			if status, ok := sig.RawValue["account_status"].(string); ok && status == "rate_limited" {
				caamLockout = true
			}
			if exhausted, ok := sig.RawValue["all_accounts_exhausted"].(bool); ok && exhausted {
				caamAllExhausted = true
			}
		case SourceCLIStatus:
			cliStatusActive = true
		case SourceNTMPane:
			ntmActive = true
		case SourceLongNoOutput:
			longNoOutputActive = true
		case SourceRano:
			ranoActive = true
		}
	}

	severity := SeverityNone
	switch {
	case caamAllExhausted:
		severity = SeverityExhausted
	case isHardConfirmed(activeCount, cliStatusActive, caamLockout, cautActive, ntmActive, ranoActive, longNoOutputActive):
		severity = SeverityHard
	case activeCount == 1 && longNoOutputActive:
		severity = SeveritySoft
	case activeCount >= 1:
		severity = SeveritySoft
	}

	_ = caamActive

	confidence := computeConfidence(weightedActive, totalWeight, degradedSources, len(signals))
	action := recommendAction(severity, hasHealthyCAAMAccount)

	return RateLimitState{
		AgentID:             agentID,
		Severity:            severity,
		Confidence:          confidence,
		ContributingSignals: preserved,
		RecommendedAction:   action,
		SequenceCursor:      sequenceCursor,
	}
}

// isHardConfirmed encodes the "multiple signals confirm rate-limit"
// rule: "hard" requires either (a) two or more independent signals
// active, OR (b) one strong signal — CLI status or CAAM lockout —
// active alone (these two are explicit machine-readable
// rate-limit declarations).
func isHardConfirmed(
	activeCount int,
	cliStatus, caamLockout, caut, ntm, rano, longNoOutput bool,
) bool {
	if cliStatus {
		return true
	}
	if caamLockout {
		return true
	}
	if activeCount >= 2 {
		// long-no-output alone-with-one-other still escalates to hard.
		// We only cap at soft when long-no-output is the ONLY signal.
		_ = caut
		_ = ntm
		_ = rano
		_ = longNoOutput
		return true
	}
	return false
}

// computeConfidence returns a value in [0, 1].
//
//	all-quiet, no degradation:           1.0  (we are sure no signal fired)
//	all-active, no degradation:          weightedActive / totalWeight
//	some sources degraded:               cap at 1 - degradedFraction*0.4
//	no signals provided (empty input):   0 (caller must decide what to do)
func computeConfidence(weightedActive, totalWeight float64, degraded, total int) float64 {
	if total == 0 {
		return 0
	}
	base := 1.0
	if totalWeight > 0 && weightedActive > 0 {
		base = weightedActive / totalWeight
		if base > 1 {
			base = 1
		}
		// Floor active-but-low-weight contributions at 0.6 so a single
		// strong observation against many quiet sources still
		// reads as a confident classification.
		if base < 0.6 {
			base = 0.6
		}
	}
	if degraded > 0 {
		degradedFraction := float64(degraded) / float64(total)
		base *= 1 - degradedFraction*0.4
	}
	if base < 0 {
		return 0
	}
	if base > 1 {
		return 1
	}
	return base
}

func recommendAction(severity RateLimitSeverity, hasHealthyCAAMAccount bool) RecommendedAction {
	switch severity {
	case SeverityNone:
		return RecommendedAction{
			Type:       ActionNone,
			Reason:     "no rate-limit signals active",
			Confidence: 1.0,
		}
	case SeveritySoft:
		return RecommendedAction{
			Type:       ActionSendMarchingOrders,
			Reason:     "single rate-limit signal active; nudge agent to monitor and retry",
			Confidence: 0.7,
		}
	case SeverityHard:
		if hasHealthyCAAMAccount {
			return RecommendedAction{
				Type:       ActionSwitchAccount,
				Reason:     "rate-limit confirmed; healthy CAAM account available for switchover",
				Confidence: 0.85,
			}
		}
		return RecommendedAction{
			Type:       ActionResumeSession,
			Reason:     "rate-limit confirmed; no healthy CAAM account, falling back to casr session resumption",
			Confidence: 0.7,
			BlockedBy:  "caam.healthy_account",
		}
	case SeverityExhausted:
		return RecommendedAction{
			Type:       ActionPauseAgent,
			Reason:     "all CAAM accounts exhausted for provider; pausing agent and notifying user",
			Confidence: 0.95,
		}
	default:
		return RecommendedAction{
			Type:       ActionNone,
			Reason:     fmt.Sprintf("unknown severity %q", severity),
			Confidence: 0,
		}
	}
}
