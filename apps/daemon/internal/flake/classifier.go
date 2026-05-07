package flake

import (
	"errors"
	"time"
)

// hp-aa02 cut #2: classifier state machine + flake-score sliding-
// window logic. Builds on the cut #1 fingerprint normalizer.
// The persistence layer (failure_ledger SQLite table) and the
// auto-bead-creator are follow-up cuts.

// FailureStatus is the typed status of one fingerprint in the
// failure_ledger. The classifier transitions between these per
// the bead's "CLASSIFIER (per-failure-fingerprint state machine)"
// rules.
type FailureStatus string

const (
	// FailureStatusNew: a fingerprint observed for the first time.
	// tend-swarm gets a 'new failure' detection.
	FailureStatusNew FailureStatus = "new"

	// FailureStatusRealUnchanged: a fingerprint we have seen
	// before AND the cache_key matches the prior occurrence.
	// tend-swarm gets a 'known unchanged failure' detection per
	// §8.5; the agent skips reattempt unless explicitly directed.
	FailureStatusRealUnchanged FailureStatus = "real_unchanged"

	// FailureStatusFlake: same fingerprint flapping with green
	// runs interleaved on the same test_id (different cache_keys
	// or pass/fail oscillation). flake_score has crossed the
	// threshold.
	FailureStatusFlake FailureStatus = "flake"

	// FailureStatusResolved: a flake whose flake_score has
	// decayed below 0.05 with the last 5 runs all green; manual
	// review confirms before this auto-closes (the flake bead
	// gets a `verifying` comment, not a close).
	FailureStatusResolved FailureStatus = "resolved"
)

// RunOutcome is the binary outcome of a test/build run on a
// given (test_id, cache_key) pair.
type RunOutcome string

const (
	OutcomeRed   RunOutcome = "red"
	OutcomeGreen RunOutcome = "green"
)

// FlakeScoreWindowSize is the §"FLAKE SCORE" sliding-window size
// (last 20 runs of the same test_id).
const FlakeScoreWindowSize = 20

// Sliding-window thresholds per the bead's FLAKE SCORE section.
const (
	FlakeScoreThreshold        = 0.20
	FlakeMinRedCount           = 2
	FlakeMinGreenCount         = 2
	FlakeResolvedScoreCeiling  = 0.05
	FlakeResolvedConsecutiveGreens = 5
)

// LedgerEntry is the in-memory shape of one failure_ledger row.
// The persistence layer maps this to the SQLite table; the
// classifier consumes + returns these as plain values.
type LedgerEntry struct {
	Fingerprint string        `json:"fingerprint"`
	TestID      string        `json:"testId,omitempty"`
	CacheKey    string        `json:"cacheKey,omitempty"`
	FirstSeenAt time.Time     `json:"firstSeenAt"`
	LastSeenAt  time.Time     `json:"lastSeenAt"`
	Occurrences int           `json:"occurrences"`
	Status      FailureStatus `json:"status"`
	FlakeScore  float64       `json:"flakeScore"`

	// FlakeBeadID is set when status == flake AND a flake-
	// hardening bead has been auto-created. The bead-creation
	// rule's idempotency depends on this field being non-empty
	// preventing duplicate beads on a second flake transition.
	FlakeBeadID string `json:"flakeBeadId,omitempty"`

	// Window is the last N runs of this test_id with their
	// outcomes — feeds the flake-score calculation. Capped at
	// FlakeScoreWindowSize entries.
	Window []RunOutcome `json:"window,omitempty"`
}

// ClassifyInput is the typed observation the classifier receives
// when a new test/build run finishes.
type ClassifyInput struct {
	Fingerprint string
	TestID      string
	CacheKey    string
	Outcome     RunOutcome
	Now         time.Time
}

// ClassifyDetection is the typed signal the classifier emits for
// the tend-swarm pre-script detection payload (§8.5).
type ClassifyDetection string

const (
	DetectionNewFailure              ClassifyDetection = "new_failure"
	DetectionKnownUnchangedFailure   ClassifyDetection = "known_unchanged_failure"
	DetectionActiveFlake             ClassifyDetection = "active_flake"
	DetectionFlakeResolutionPending  ClassifyDetection = "flake_resolution_pending"
	DetectionGreenRun                ClassifyDetection = "green_run"
)

// ClassifyResult is the verdict the classifier returns for one
// observation. The caller persists the UpdatedEntry, fires the
// detection (if non-empty), and triggers the auto-bead-creator
// when ShouldCreateFlakeBead is true.
type ClassifyResult struct {
	UpdatedEntry         LedgerEntry
	Detection            ClassifyDetection
	ShouldCreateFlakeBead bool
}

// ErrInvalidObservation is returned when the input is missing
// the fingerprint or carries an unknown outcome.
var ErrInvalidObservation = errors.New("flake: invalid observation")

// Classify is the pure-function core of the classifier. Given
// the prior LedgerEntry (zero-valued for a fresh fingerprint)
// and a new observation, it returns the next ledger state,
// the detection signal for tend-swarm, and whether the caller
// should auto-create a flake bead.
//
// The function does not call out to any storage; the persistence
// layer's responsibility is to look up the prior entry by
// fingerprint, call this function, and persist the returned
// UpdatedEntry. That separation keeps this layer pure and
// testable.
func Classify(prior LedgerEntry, in ClassifyInput) (ClassifyResult, error) {
	if in.Fingerprint == "" {
		return ClassifyResult{}, ErrInvalidObservation
	}
	if in.Outcome != OutcomeRed && in.Outcome != OutcomeGreen {
		return ClassifyResult{}, ErrInvalidObservation
	}

	entry := prior
	if entry.Fingerprint == "" {
		entry.Fingerprint = in.Fingerprint
	}
	if entry.TestID == "" {
		entry.TestID = in.TestID
	}
	entry.LastSeenAt = in.Now

	if in.Outcome == OutcomeGreen {
		return classifyGreen(entry, in)
	}

	// First-time red on this fingerprint: status = new.
	if entry.Occurrences == 0 {
		entry.FirstSeenAt = in.Now
		entry.Occurrences = 1
		entry.CacheKey = in.CacheKey
		entry.Status = FailureStatusNew
		entry.Window = appendOutcome(entry.Window, in.Outcome)
		entry.FlakeScore = computeFlakeScore(entry.Window)
		return ClassifyResult{
			UpdatedEntry: entry,
			Detection:    DetectionNewFailure,
		}, nil
	}

	// Repeat red: increment counters, slide window, recompute.
	entry.Occurrences++
	priorCacheKey := entry.CacheKey
	entry.Window = appendOutcome(entry.Window, in.Outcome)
	entry.FlakeScore = computeFlakeScore(entry.Window)

	cacheKeyMatches := priorCacheKey == in.CacheKey

	switch {
	case isFlake(entry):
		// Flake threshold met. If we just transitioned from
		// new/real_unchanged → flake AND no bead exists yet, the
		// caller must create one.
		shouldCreateBead := entry.FlakeBeadID == "" && entry.Status != FailureStatusFlake
		entry.Status = FailureStatusFlake
		// Update CacheKey to the latest observed; the auto-bead
		// description will reference the recent run set, not the
		// fossilized first one.
		entry.CacheKey = in.CacheKey
		return ClassifyResult{
			UpdatedEntry:         entry,
			Detection:            DetectionActiveFlake,
			ShouldCreateFlakeBead: shouldCreateBead,
		}, nil
	case cacheKeyMatches:
		// Same fingerprint AND same cache_key → real_unchanged.
		// tend-swarm tells the agent to skip reattempts.
		entry.Status = FailureStatusRealUnchanged
		return ClassifyResult{
			UpdatedEntry: entry,
			Detection:    DetectionKnownUnchangedFailure,
		}, nil
	default:
		// Cache key changed but flake threshold not reached;
		// treat as a fresh new-class observation but keep the
		// existing window (the score will mature over time).
		entry.CacheKey = in.CacheKey
		entry.Status = FailureStatusNew
		return ClassifyResult{
			UpdatedEntry: entry,
			Detection:    DetectionNewFailure,
		}, nil
	}
}

func classifyGreen(entry LedgerEntry, in ClassifyInput) (ClassifyResult, error) {
	entry.Window = appendOutcome(entry.Window, OutcomeGreen)
	entry.FlakeScore = computeFlakeScore(entry.Window)

	if entry.Status == FailureStatusFlake {
		if isResolved(entry) {
			entry.Status = FailureStatusResolved
			return ClassifyResult{
				UpdatedEntry: entry,
				Detection:    DetectionFlakeResolutionPending,
			}, nil
		}
		return ClassifyResult{
			UpdatedEntry: entry,
			Detection:    DetectionActiveFlake,
		}, nil
	}
	return ClassifyResult{
		UpdatedEntry: entry,
		Detection:    DetectionGreenRun,
	}, nil
}

func appendOutcome(window []RunOutcome, outcome RunOutcome) []RunOutcome {
	out := append(window, outcome)
	if len(out) > FlakeScoreWindowSize {
		out = out[len(out)-FlakeScoreWindowSize:]
	}
	return out
}

// computeFlakeScore implements the bead's FLAKE SCORE formula:
//
//	flake_score = (red_count / window_size) *
//	              (1 - consecutive_green_runs / window_size)
func computeFlakeScore(window []RunOutcome) float64 {
	if len(window) == 0 {
		return 0
	}
	redCount := 0
	for _, o := range window {
		if o == OutcomeRed {
			redCount++
		}
	}
	consecutiveGreens := 0
	for i := len(window) - 1; i >= 0; i-- {
		if window[i] == OutcomeGreen {
			consecutiveGreens++
			continue
		}
		break
	}
	red := float64(redCount) / float64(FlakeScoreWindowSize)
	greenSuppression := 1.0 - float64(consecutiveGreens)/float64(FlakeScoreWindowSize)
	if greenSuppression < 0 {
		greenSuppression = 0
	}
	return red * greenSuppression
}

// isFlake encodes the flake-threshold rule: flake_score >=
// FlakeScoreThreshold AND red_count >= FlakeMinRedCount AND
// green_count >= FlakeMinGreenCount within the window.
//
// "Three consecutive reds = NOT flake (real_unchanged or new)"
// follows from the green-count requirement.
func isFlake(entry LedgerEntry) bool {
	if entry.FlakeScore < FlakeScoreThreshold {
		return false
	}
	red, green := windowCounts(entry.Window)
	return red >= FlakeMinRedCount && green >= FlakeMinGreenCount
}

// isResolved encodes the flake→resolved transition: flake_score
// < FlakeResolvedScoreCeiling AND last
// FlakeResolvedConsecutiveGreens runs are all green.
func isResolved(entry LedgerEntry) bool {
	if entry.FlakeScore >= FlakeResolvedScoreCeiling {
		return false
	}
	if len(entry.Window) < FlakeResolvedConsecutiveGreens {
		return false
	}
	tail := entry.Window[len(entry.Window)-FlakeResolvedConsecutiveGreens:]
	for _, o := range tail {
		if o != OutcomeGreen {
			return false
		}
	}
	return true
}

func windowCounts(window []RunOutcome) (red, green int) {
	for _, o := range window {
		switch o {
		case OutcomeRed:
			red++
		case OutcomeGreen:
			green++
		}
	}
	return
}
