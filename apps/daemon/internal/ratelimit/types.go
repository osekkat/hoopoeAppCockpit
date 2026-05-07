package ratelimit

import "time"

// RateLimitSeverity is the four-valued classification consumed by
// tend-swarm (hp-fb0), the Activity panel agent tile (hp-8sm), the
// composition picker, and the §7.6 top-bar usage pill. The order of
// the constants matches the spec's escalation: a state of higher
// rank supersedes a lower-rank state when fusing signals.
type RateLimitSeverity string

const (
	SeverityNone      RateLimitSeverity = "none"
	SeveritySoft      RateLimitSeverity = "soft"
	SeverityHard      RateLimitSeverity = "hard"
	SeverityExhausted RateLimitSeverity = "exhausted"
)

// SignalSource identifies which of the six §7.3 detection rails
// produced an observation. The string values are stable (they appear
// in the daemon's audit log and in the WS event payload, so
// renaming them is a schema-version event).
type SignalSource string

const (
	SourceCaut         SignalSource = "caut"
	SourceCAAM         SignalSource = "caam"
	SourceCLIStatus    SignalSource = "cli_status"
	SourceNTMPane      SignalSource = "ntm_pane"
	SourceLongNoOutput SignalSource = "long_no_output"
	SourceRano         SignalSource = "rano"
)

// RecommendedActionKind is the closed action menu the aggregator
// recommends to tend-swarm. Matches the §7.3 "Recovery actions"
// catalog and overlaps with the typed ActionPlan kinds the daemon
// can actually execute.
type RecommendedActionKind string

const (
	ActionNone               RecommendedActionKind = "no_action"
	ActionSwitchAccount      RecommendedActionKind = "caam.switch_account"
	ActionResumeSession      RecommendedActionKind = "casr.resume_session"
	ActionSendMarchingOrders RecommendedActionKind = "agent.send_marching_orders"
	ActionKillWedgedProcess  RecommendedActionKind = "agent.kill_wedged_process"
	ActionPauseAgent         RecommendedActionKind = "swarm.pause_agent"
)

// ContributingSignal is one observation from one of the six rails.
// The aggregator collects these into a per-agent ring and the
// classifier consumes the most-recent observation from each source.
//
// Weight is set by the caller (the source-specific adapter) per
// the §7.3 weight table; the classifier multiplies it against the
// signal's "active" boolean to produce a confidence contribution.
//
// Degraded indicates the source itself is unavailable (e.g., caut
// has no data for a provider). Per the bead spec, degraded signals
// contribute 0 weight but are preserved in ContributingSignals so
// future archeology can see which sources were silent.
type ContributingSignal struct {
	Source   SignalSource `json:"source"`
	Active   bool         `json:"active"`
	Weight   float64      `json:"weight"`
	Degraded bool         `json:"degraded,omitempty"`

	// RawValue preserves the source-specific observation payload for
	// auditing — the aggregator does not introspect it.
	RawValue map[string]any `json:"rawValue,omitempty"`
}

// RecommendedAction is the typed recommendation surfaced to the
// tend-swarm pre-script + the Activity panel.
type RecommendedAction struct {
	Type       RecommendedActionKind `json:"type"`
	Reason     string                `json:"reason"`
	Confidence float64               `json:"confidence"`

	// BlockedBy is non-empty when the aggregator wanted to recommend
	// a recovery action but the required capability is missing
	// (e.g., switch_account when CAAM has no healthy accounts left).
	BlockedBy string `json:"blockedBy,omitempty"`
}

// RateLimitState is the public per-agent shape consumed by every
// downstream surface.
type RateLimitState struct {
	AgentID             string               `json:"agentId"`
	Severity            RateLimitSeverity    `json:"severity"`
	Confidence          float64              `json:"confidence"`
	ContributingSignals []ContributingSignal `json:"contributingSignals"`
	RecommendedAction   RecommendedAction    `json:"recommendedAction"`
	LastUpdated         time.Time            `json:"lastUpdated"`
	SequenceCursor      uint64               `json:"sequenceCursor"`
}

// SeverityRank returns a numeric rank usable for ordering /
// max-merging severities. Higher rank = more severe.
func (s RateLimitSeverity) Rank() int {
	switch s {
	case SeverityExhausted:
		return 3
	case SeverityHard:
		return 2
	case SeveritySoft:
		return 1
	case SeverityNone:
		return 0
	default:
		return -1
	}
}
