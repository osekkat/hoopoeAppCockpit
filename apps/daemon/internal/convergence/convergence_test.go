package convergence

import (
	"errors"
	"testing"
	"time"
)

func TestEvaluateNotStartedShowsBlockedLandingChecklist(t *testing.T) {
	t.Parallel()
	got, err := Evaluate(EvaluationInput{
		ProjectID: "hoopoe",
		Now:       fixedNow,
	})
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	if got.State != StateNotStarted {
		t.Fatalf("state = %q, want %q", got.State, StateNotStarted)
	}
	if got.GeneratedAt != fixedNow().UTC() {
		t.Fatalf("generatedAt = %v", got.GeneratedAt)
	}
	if got.LatestRound != nil || len(got.Rounds) != 0 {
		t.Fatalf("rounds = %+v latest = %+v", got.Rounds, got.LatestRound)
	}
	if got.Landing.Ready || len(got.Landing.Missing) != 4 {
		t.Fatalf("landing = %+v", got.Landing)
	}
	finalStep := got.Steps[len(got.Steps)-1]
	if finalStep.State != StateFinalGateReady || finalStep.Status != StepBlocked {
		t.Fatalf("final step = %+v", finalStep)
	}
}

func TestEvaluateHighYieldRoundComputesReviewMetrics(t *testing.T) {
	t.Parallel()
	got, err := Evaluate(EvaluationInput{
		ProjectID: "hoopoe",
		Rounds: []ReviewRound{
			{
				RoundID:           "round-0",
				Index:             0,
				Findings:          highYieldFindings(),
				Fixes:             1,
				NewBeads:          []string{"hp-a"},
				TestFailuresFixed: 2,
				CoverageDelta:     4.5,
				ComplexityDelta:   -3,
				CostUnits:         10,
				EffortMinutes:     50,
			},
		},
		Now: fixedNow,
	})
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	if got.State != StateHighYield {
		t.Fatalf("state = %q, want %q", got.State, StateHighYield)
	}
	latest := got.LatestRound
	if latest == nil {
		t.Fatal("latest round is nil")
	}
	if latest.Findings != 7 || latest.UsefulFindings != 5 || latest.SevereFindings != 2 || latest.OpenSevereFindings != 1 {
		t.Fatalf("finding metrics = %+v", latest)
	}
	if latest.DuplicateFindings != 1 || latest.Fixes != 2 || latest.NewBeads != 2 {
		t.Fatalf("resolution metrics = %+v", latest)
	}
	if latest.TestFailuresFixed != 2 || latest.CoverageDelta != 4.5 || latest.ComplexityDelta != -3 {
		t.Fatalf("health metrics = %+v", latest)
	}
	if latest.CostUnitsPerUsefulFinding != 2 || latest.MinutesPerUsefulFinding != 10 {
		t.Fatalf("cost/time metrics = %+v", latest)
	}
	if got.Saturation.Saturated || got.Saturation.Reason != "open severe findings remain" {
		t.Fatalf("saturation = %+v", got.Saturation)
	}
}

func TestEvaluateSaturatedWhenLatestRoundIsLowYieldAndDominated(t *testing.T) {
	t.Parallel()
	got, err := Evaluate(EvaluationInput{
		ProjectID: "hoopoe",
		Rounds: []ReviewRound{
			{
				RoundID: "round-3",
				Index:   3,
				Findings: []Finding{
					{ID: "f1", Severity: SeverityLow, Resolution: ResolutionDuplicate, DuplicateOf: "f0"},
					{ID: "f2", Severity: SeverityInfo, Resolution: ResolutionTrackedExistingBead, BeadID: "hp-old"},
					{ID: "f3", Severity: SeverityLow, Resolution: ResolutionOpen},
				},
				CostUnits:     9,
				EffortMinutes: 90,
			},
		},
		Now: fixedNow,
	})
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	if got.State != StateSaturated {
		t.Fatalf("state = %q, want %q", got.State, StateSaturated)
	}
	if !got.Saturation.Saturated {
		t.Fatalf("saturation = %+v", got.Saturation)
	}
	if got.LatestRound.LowDuplicateTrackedRatio != 1 {
		t.Fatalf("dominated ratio = %v", got.LatestRound.LowDuplicateTrackedRatio)
	}
	if got.LatestRound.UsefulFindings != 1 || got.LatestRound.CostUnitsPerUsefulFinding != 9 {
		t.Fatalf("latest = %+v", got.LatestRound)
	}
}

func TestEvaluateFinalGateReadyWhenSaturatedAndChecklistPasses(t *testing.T) {
	t.Parallel()
	got, err := Evaluate(EvaluationInput{
		ProjectID: "hoopoe",
		Rounds: []ReviewRound{
			{
				RoundID:       "round-9",
				Index:         9,
				Findings:      nil,
				CostUnits:     5,
				EffortMinutes: 35,
			},
		},
		Landing: LandingChecklistInput{
			TestBuildsPassing: true,
			CodeHealthPassing: true,
			GitSynced:         true,
			BeadsSynced:       true,
		},
		Now: fixedNow,
	})
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	if got.State != StateFinalGateReady {
		t.Fatalf("state = %q, want %q", got.State, StateFinalGateReady)
	}
	if !got.Landing.Ready {
		t.Fatalf("landing = %+v", got.Landing)
	}
	if err := RequireFinalLandingReady(got); err != nil {
		t.Fatalf("RequireFinalLandingReady: %v", err)
	}
	finalStep := got.Steps[len(got.Steps)-1]
	if finalStep.Status != StepCurrent {
		t.Fatalf("final step = %+v", finalStep)
	}
}

func TestLandingChecklistAcceptsDocumentedExceptionsAndFollowUpBeads(t *testing.T) {
	t.Parallel()
	checklist := EvaluateLandingChecklist(LandingChecklistInput{
		TestBuildExceptions: []string{"integration suite requires staging token"},
		CodeHealthFollowUps: []string{"hp-followup"},
		GitSynced:           true,
		BeadsSynced:         true,
	})
	if !checklist.Ready || len(checklist.Missing) != 0 {
		t.Fatalf("checklist = %+v", checklist)
	}
	if !checklist.Items[0].Passed || checklist.Items[0].Detail != "exceptions documented" {
		t.Fatalf("test/build item = %+v", checklist.Items[0])
	}
	if !checklist.Items[1].Passed || checklist.Items[1].Detail != "follow-up beads exist" {
		t.Fatalf("code health item = %+v", checklist.Items[1])
	}
}

func TestRequireFinalLandingReadyReportsBlockingChecklistItems(t *testing.T) {
	t.Parallel()
	got, err := Evaluate(EvaluationInput{
		ProjectID: "hoopoe",
		Rounds: []ReviewRound{
			{RoundID: "round-9", Index: 9},
		},
		Landing: LandingChecklistInput{
			TestBuildsPassing: true,
			CodeHealthPassing: true,
			GitSynced:         true,
			BeadsSynced:       false,
		},
		Now: fixedNow,
	})
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	if err := RequireFinalLandingReady(got); !errors.Is(err, ErrFinalGateBlocked) {
		t.Fatalf("RequireFinalLandingReady err = %v, want ErrFinalGateBlocked", err)
	}
}

func TestEvaluateRejectsDuplicateRounds(t *testing.T) {
	t.Parallel()
	_, err := Evaluate(EvaluationInput{
		Rounds: []ReviewRound{
			{RoundID: "round-1", Index: 1},
			{RoundID: "round-1", Index: 2},
		},
	})
	if !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("err = %v, want ErrInvalidInput", err)
	}
}

func highYieldFindings() []Finding {
	return []Finding{
		{ID: "f1", Source: "ubs", Severity: SeverityCritical, Resolution: ResolutionOpen},
		{ID: "f2", Source: "ubs", Severity: SeverityHigh, Resolution: ResolutionFixed},
		{ID: "f3", Source: "agent", Severity: SeverityMedium, Resolution: ResolutionOpen},
		{ID: "f4", Source: "agent", Severity: SeverityMedium, Resolution: ResolutionOpen},
		{ID: "f5", Source: "agent", Severity: SeverityLow, Resolution: ResolutionOpen},
		{ID: "f6", Source: "agent", Severity: SeverityLow, Resolution: ResolutionDuplicate, DuplicateOf: "f5"},
		{ID: "f7", Source: "agent", Severity: SeverityMedium, Resolution: ResolutionNewBead, BeadID: "hp-b"},
	}
}

func fixedNow() time.Time {
	return time.Date(2026, 5, 4, 12, 0, 0, 0, time.UTC)
}
