package flake

import (
	"testing"
	"time"
)

func now(min int) time.Time {
	return time.Date(2026, 5, 7, 12, min, 0, 0, time.UTC)
}

func TestFirstRedTransitionsToNew(t *testing.T) {
	t.Parallel()
	got, err := Classify(LedgerEntry{}, ClassifyInput{
		Fingerprint: "fp-1",
		TestID:      "TestX",
		CacheKey:    "cache-A",
		Outcome:     OutcomeRed,
		Now:         now(0),
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.UpdatedEntry.Status != FailureStatusNew {
		t.Errorf("Status = %s, want new", got.UpdatedEntry.Status)
	}
	if got.UpdatedEntry.Occurrences != 1 {
		t.Errorf("Occurrences = %d, want 1", got.UpdatedEntry.Occurrences)
	}
	if got.Detection != DetectionNewFailure {
		t.Errorf("Detection = %s, want new_failure", got.Detection)
	}
}

func TestSecondRedSameCacheKeyTransitionsToRealUnchanged(t *testing.T) {
	t.Parallel()
	prior := LedgerEntry{
		Fingerprint: "fp-1",
		TestID:      "TestX",
		CacheKey:    "cache-A",
		Occurrences: 1,
		Status:      FailureStatusNew,
		Window:      []RunOutcome{OutcomeRed},
		FlakeScore:  computeFlakeScore([]RunOutcome{OutcomeRed}),
	}
	got, err := Classify(prior, ClassifyInput{
		Fingerprint: "fp-1",
		TestID:      "TestX",
		CacheKey:    "cache-A",
		Outcome:     OutcomeRed,
		Now:         now(1),
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.UpdatedEntry.Status != FailureStatusRealUnchanged {
		t.Errorf("Status = %s, want real_unchanged", got.UpdatedEntry.Status)
	}
	if got.Detection != DetectionKnownUnchangedFailure {
		t.Errorf("Detection = %s, want known_unchanged_failure", got.Detection)
	}
	if got.UpdatedEntry.Occurrences != 2 {
		t.Errorf("Occurrences = %d, want 2", got.UpdatedEntry.Occurrences)
	}
}

func TestRedDifferentCacheKeyResetsToNew(t *testing.T) {
	t.Parallel()
	prior := LedgerEntry{
		Fingerprint: "fp-1",
		TestID:      "TestX",
		CacheKey:    "cache-A",
		Occurrences: 1,
		Status:      FailureStatusNew,
		Window:      []RunOutcome{OutcomeRed},
	}
	got, err := Classify(prior, ClassifyInput{
		Fingerprint: "fp-1",
		TestID:      "TestX",
		CacheKey:    "cache-B",
		Outcome:     OutcomeRed,
		Now:         now(2),
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.UpdatedEntry.Status != FailureStatusNew {
		t.Errorf("Status = %s, want new (cache_key changed)", got.UpdatedEntry.Status)
	}
	if got.UpdatedEntry.CacheKey != "cache-B" {
		t.Errorf("CacheKey = %s, want cache-B", got.UpdatedEntry.CacheKey)
	}
}

func TestAlternatingRedGreenTransitionsToFlakeAndAutoCreatesBead(t *testing.T) {
	t.Parallel()
	// Sequence: R G R G R G R G — 4 reds, 4 greens. Last red ⇒ flake.
	entry := LedgerEntry{}
	sequence := []RunOutcome{
		OutcomeRed, OutcomeGreen,
		OutcomeRed, OutcomeGreen,
		OutcomeRed, OutcomeGreen,
		OutcomeRed,
	}
	var lastResult ClassifyResult
	for i, outcome := range sequence {
		got, err := Classify(entry, ClassifyInput{
			Fingerprint: "fp-flaky",
			TestID:      "TestFlaky",
			CacheKey:    "cache-A",
			Outcome:     outcome,
			Now:         now(i),
		})
		if err != nil {
			t.Fatalf("step %d (%s): %v", i, outcome, err)
		}
		entry = got.UpdatedEntry
		lastResult = got
	}
	if entry.Status != FailureStatusFlake {
		t.Errorf("Status = %s, want flake (alternating R/G must trip threshold)", entry.Status)
	}
	if !lastResult.ShouldCreateFlakeBead {
		t.Errorf("ShouldCreateFlakeBead must be true on first flake transition")
	}
	if lastResult.Detection != DetectionActiveFlake {
		t.Errorf("Detection = %s, want active_flake", lastResult.Detection)
	}
}

func TestThreeConsecutiveRedsIsNotFlake(t *testing.T) {
	t.Parallel()
	// Per the bead: "Three consecutive reds = NOT flake
	// (real_unchanged or new)" — green count requirement is the
	// distinguishing rule.
	entry := LedgerEntry{}
	for i := 0; i < 3; i++ {
		got, err := Classify(entry, ClassifyInput{
			Fingerprint: "fp-real",
			TestID:      "TestReal",
			CacheKey:    "cache-A",
			Outcome:     OutcomeRed,
			Now:         now(i),
		})
		if err != nil {
			t.Fatalf("step %d: %v", i, err)
		}
		entry = got.UpdatedEntry
	}
	if entry.Status == FailureStatusFlake {
		t.Errorf("3 consecutive reds must NOT be flake, got status=%s", entry.Status)
	}
}

func TestSecondFlakeTransitionDoesNotDuplicateBead(t *testing.T) {
	t.Parallel()
	// Once a flake bead exists, further red runs must NOT
	// re-trigger ShouldCreateFlakeBead — idempotency on
	// auto-bead-create.
	entry := LedgerEntry{
		Fingerprint: "fp-flaky",
		TestID:      "TestFlaky",
		CacheKey:    "cache-A",
		Occurrences: 5,
		Status:      FailureStatusFlake,
		FlakeBeadID: "br-existing-flake",
		Window:      []RunOutcome{OutcomeRed, OutcomeGreen, OutcomeRed, OutcomeGreen, OutcomeRed},
		FlakeScore:  0.5,
	}
	got, err := Classify(entry, ClassifyInput{
		Fingerprint: "fp-flaky",
		TestID:      "TestFlaky",
		CacheKey:    "cache-A",
		Outcome:     OutcomeRed,
		Now:         now(10),
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.ShouldCreateFlakeBead {
		t.Errorf("ShouldCreateFlakeBead must be false when FlakeBeadID is already set")
	}
}

func TestFlakeResolvesAfterFiveConsecutiveGreens(t *testing.T) {
	t.Parallel()
	// Start in flake state with one red in the (otherwise green)
	// tail. Five consecutive greens drop the flake_score under
	// FlakeResolvedScoreCeiling and clear the consecutive-greens
	// requirement.
	priorWindow := []RunOutcome{
		OutcomeGreen, OutcomeGreen, OutcomeGreen, OutcomeGreen, OutcomeGreen,
		OutcomeRed,
	}
	entry := LedgerEntry{
		Fingerprint: "fp-flaky",
		TestID:      "TestFlaky",
		CacheKey:    "cache-A",
		Occurrences: 4,
		Status:      FailureStatusFlake,
		FlakeBeadID: "br-existing-flake",
		Window:      priorWindow,
		FlakeScore:  computeFlakeScore(priorWindow),
	}
	for i := 0; i < FlakeResolvedConsecutiveGreens; i++ {
		got, err := Classify(entry, ClassifyInput{
			Fingerprint: "fp-flaky",
			TestID:      "TestFlaky",
			CacheKey:    "cache-A",
			Outcome:     OutcomeGreen,
			Now:         now(10 + i),
		})
		if err != nil {
			t.Fatalf("step %d: %v", i, err)
		}
		entry = got.UpdatedEntry
	}
	if entry.Status != FailureStatusResolved {
		t.Errorf("Status = %s, want resolved (5 consecutive greens after 1 red)", entry.Status)
	}
}

func TestGreenRunOnNonFlakeEmitsGreenRunDetection(t *testing.T) {
	t.Parallel()
	entry := LedgerEntry{
		Fingerprint: "fp-1",
		Status:      FailureStatusNew,
		Window:      []RunOutcome{OutcomeRed},
		FlakeScore:  computeFlakeScore([]RunOutcome{OutcomeRed}),
	}
	got, err := Classify(entry, ClassifyInput{
		Fingerprint: "fp-1",
		Outcome:     OutcomeGreen,
		Now:         now(2),
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Detection != DetectionGreenRun {
		t.Errorf("Detection = %s, want green_run", got.Detection)
	}
}

func TestEmptyFingerprintReturnsError(t *testing.T) {
	t.Parallel()
	_, err := Classify(LedgerEntry{}, ClassifyInput{
		Fingerprint: "",
		Outcome:     OutcomeRed,
		Now:         now(0),
	})
	if err == nil {
		t.Errorf("expected error for empty fingerprint")
	}
}

func TestUnknownOutcomeReturnsError(t *testing.T) {
	t.Parallel()
	_, err := Classify(LedgerEntry{}, ClassifyInput{
		Fingerprint: "fp-1",
		Outcome:     RunOutcome("yellow"),
		Now:         now(0),
	})
	if err == nil {
		t.Errorf("expected error for unknown outcome")
	}
}

func TestComputeFlakeScoreOnAllGreensIsZero(t *testing.T) {
	t.Parallel()
	got := computeFlakeScore([]RunOutcome{OutcomeGreen, OutcomeGreen, OutcomeGreen, OutcomeGreen})
	if got != 0 {
		t.Errorf("flake score on all greens = %f, want 0", got)
	}
}

func TestComputeFlakeScoreOnAllRedsIsBoundedByGreenSuppression(t *testing.T) {
	t.Parallel()
	// All reds → consecutive_greens=0 → suppression=1.0 →
	// score = redCount/windowSize. Three reds with windowSize=20
	// gives 3/20 = 0.15.
	got := computeFlakeScore([]RunOutcome{OutcomeRed, OutcomeRed, OutcomeRed})
	want := 3.0 / float64(FlakeScoreWindowSize)
	if got != want {
		t.Errorf("flake score = %f, want %f", got, want)
	}
}

func TestComputeFlakeScoreFavorsAlternatingPatterns(t *testing.T) {
	t.Parallel()
	// R G R G R G has 3 reds + 3 greens but the trailing run is
	// green so consecutive_greens=1; the formula's green
	// suppression dampens but does not zero the score.
	got := computeFlakeScore([]RunOutcome{
		OutcomeRed, OutcomeGreen, OutcomeRed, OutcomeGreen, OutcomeRed, OutcomeGreen,
	})
	if got <= 0 {
		t.Errorf("alternating pattern must produce positive flake score, got %f", got)
	}
}

func TestWindowCapsAtConfiguredSize(t *testing.T) {
	t.Parallel()
	w := []RunOutcome{}
	for i := 0; i < FlakeScoreWindowSize+5; i++ {
		w = appendOutcome(w, OutcomeRed)
	}
	if len(w) != FlakeScoreWindowSize {
		t.Errorf("window length = %d, want %d", len(w), FlakeScoreWindowSize)
	}
}

func TestClassifyPopulatesFirstSeenAtOnNew(t *testing.T) {
	t.Parallel()
	got, err := Classify(LedgerEntry{}, ClassifyInput{
		Fingerprint: "fp-1",
		Outcome:     OutcomeRed,
		Now:         now(5),
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.UpdatedEntry.FirstSeenAt != now(5) {
		t.Errorf("FirstSeenAt = %v, want %v", got.UpdatedEntry.FirstSeenAt, now(5))
	}
	if got.UpdatedEntry.LastSeenAt != now(5) {
		t.Errorf("LastSeenAt = %v, want %v", got.UpdatedEntry.LastSeenAt, now(5))
	}
}

func TestClassifyAdvancesLastSeenAtOnRepeatRed(t *testing.T) {
	t.Parallel()
	prior := LedgerEntry{
		Fingerprint: "fp-1",
		TestID:      "TestX",
		CacheKey:    "cache-A",
		Occurrences: 1,
		Status:      FailureStatusNew,
		FirstSeenAt: now(0),
		LastSeenAt:  now(0),
		Window:      []RunOutcome{OutcomeRed},
	}
	got, err := Classify(prior, ClassifyInput{
		Fingerprint: "fp-1",
		TestID:      "TestX",
		CacheKey:    "cache-A",
		Outcome:     OutcomeRed,
		Now:         now(10),
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.UpdatedEntry.LastSeenAt != now(10) {
		t.Errorf("LastSeenAt = %v, want %v", got.UpdatedEntry.LastSeenAt, now(10))
	}
	if got.UpdatedEntry.FirstSeenAt != now(0) {
		t.Errorf("FirstSeenAt must not move on repeat red")
	}
}
