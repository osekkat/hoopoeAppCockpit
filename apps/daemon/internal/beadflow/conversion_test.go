package beadflow

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/hoopoe-cockpit/hoopoe/apps/daemon/internal/adapters/br"
	"github.com/hoopoe-cockpit/hoopoe/apps/daemon/internal/adapters/bv"
)

func TestBuildConversionPlanCreatesUpdatesAndBuildsPolishPlan(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 5, 4, 12, 30, 0, 0, time.UTC)
	sections := []PlanSection{
		{ID: "s1", Title: "Parser", RequiredTests: []string{"unit: parser"}},
		{ID: "s2", Title: "Polish", RequiredTests: []string{"unit: polish"}},
	}
	existing := []Bead{{
		ID:                 "hp-existing",
		Title:              "Parser bead",
		Description:        strings.Repeat("context ", 50),
		PlanSections:       []string{"s1"},
		TestObligations:    []string{"unit: parser"},
		AcceptanceCriteria: []string{"parser accepts locked plan"},
	}}
	drafts := []Bead{
		{
			Title:               "Parser bead",
			Description:         strings.Repeat("updated ", 50),
			PlanSections:        []string{"s1"},
			TestObligations:     []string{"unit: parser"},
			AcceptanceCriteria:  []string{"parser keeps traceability tags"},
			EstimatedMinutes:    90,
			ImplementationNotes: []string{"preserve br source of truth"},
		},
		{
			Title:               "Polish bead graph",
			Description:         strings.Repeat("polish ", 50),
			PlanSections:        []string{"s2"},
			TestObligations:     []string{"unit: polish"},
			AcceptanceCriteria:  []string{"quality report recommends next round"},
			DependsOn:           []string{"hp-existing"},
			EstimatedMinutes:    120,
			ImplementationNotes: []string{"use bv robot plan only"},
		},
	}

	plan := BuildConversionPlan(ConversionInput{
		PlanID:        "plan-1",
		PlanLocked:    true,
		Sections:      sections,
		ExistingBeads: existing,
		DraftBeads:    drafts,
		Graph: GraphHealth{
			ReadyCount:   2,
			BlockedCount: 0,
			ParallelTracks: []Track{
				{ID: "track-1", BeadIDs: []string{"hp-existing"}},
				{ID: "track-2", BeadIDs: []string{"polish-bead-graph"}},
			},
		},
		Now: func() time.Time { return now },
	})

	if plan.Status != ConversionReady {
		t.Fatalf("status = %q findings=%v", plan.Status, plan.Findings)
	}
	if plan.SchemaVersion != ConversionSchemaVersion || !plan.GeneratedAt.Equal(now) {
		t.Fatalf("metadata = %+v", plan)
	}
	if len(plan.Operations) != 2 {
		t.Fatalf("operations = %+v", plan.Operations)
	}
	if plan.Operations[0].Kind != BeadOperationUpdate || plan.Operations[0].ExistingID != "hp-existing" {
		t.Fatalf("first op = %+v", plan.Operations[0])
	}
	if plan.Operations[1].Kind != BeadOperationCreate || plan.Operations[1].DraftID != "polish-bead-graph" {
		t.Fatalf("second op = %+v", plan.Operations[1])
	}
	if plan.Traceability.PlanCoverageScore != 100 || len(plan.Traceability.MissingTestBeads) != 0 {
		t.Fatalf("traceability = %+v", plan.Traceability)
	}
	if plan.Quality.OverallScore == 0 || plan.Polish.Recommended == "" || len(plan.Polish.ArtifactPaths) != 4 {
		t.Fatalf("quality/polish = %+v / %+v", plan.Quality, plan.Polish)
	}

	mutations, err := BRMutationsFromOperations(plan.Operations, br.CommonOptions{Actor: "WhiteDog"})
	if err != nil {
		t.Fatalf("BRMutationsFromOperations: %v", err)
	}
	if len(mutations) != 2 {
		t.Fatalf("mutations = %+v", mutations)
	}
	update := mutations[0].Update
	if update == nil || update.IDs[0] != "hp-existing" || update.Common.Actor != "WhiteDog" {
		t.Fatalf("update mutation = %+v", mutations[0])
	}
	create := mutations[1].Create
	if create == nil || create.Title != "Polish bead graph" || create.Deps != "hp-existing" {
		t.Fatalf("create mutation = %+v", mutations[1])
	}
	if got := strings.Join(create.Labels, ","); got != "plan-section:s2" {
		t.Fatalf("create labels = %q", got)
	}
	if !strings.Contains(create.Description, "Test obligations\n- unit: polish") {
		t.Fatalf("rendered description missing test obligations:\n%s", create.Description)
	}
	if _, err := br.UpdateArgv(*update); err != nil {
		t.Fatalf("update request did not satisfy br adapter: %v", err)
	}
	if _, err := br.CreateArgv(*create); err != nil {
		t.Fatalf("create request did not satisfy br adapter: %v", err)
	}
}

func TestBuildConversionPlanBlocksUnlockedPlanWithoutOverride(t *testing.T) {
	t.Parallel()
	plan := BuildConversionPlan(ConversionInput{
		PlanID:   "plan-1",
		Sections: []PlanSection{{ID: "s1", Title: "Parser"}},
		DraftBeads: []Bead{{
			Title:        "Parser bead",
			PlanSections: []string{"s1"},
		}},
	})
	if plan.Status != ConversionBlocked {
		t.Fatalf("status = %q", plan.Status)
	}
	if len(plan.Findings) == 0 || !strings.Contains(plan.Findings[0], "plan is not locked") {
		t.Fatalf("findings = %+v", plan.Findings)
	}
}

func TestBeadsFromBRIssuesExtractsTraceabilityConventions(t *testing.T) {
	t.Parallel()
	beads := BeadsFromBRIssues([]br.Issue{{
		ID:          "hp-a",
		Title:       "Build converter",
		Description: "Plan sections: s2, s3\n\nDo the work.",
		Status:      "open",
		Priority:    0,
		Labels:      []string{"phase6", "plan-section:s1"},
		ExternalRef: ".hoopoe/plans/plan-1.md#section=s4",
		Dependencies: []br.Dependency{{
			IssueID:     "hp-a",
			DependsOnID: "hp-root",
			Type:        "blocks",
		}},
	}})
	if len(beads) != 1 {
		t.Fatalf("beads = %+v", beads)
	}
	if got := strings.Join(beads[0].PlanSections, ","); got != "s1,s2,s4" {
		t.Fatalf("plan sections = %q", got)
	}
	if got := strings.Join(beads[0].DependsOn, ","); got != "hp-root" {
		t.Fatalf("dependsOn = %q", got)
	}
}

func TestGraphHealthFromBVMapsPlanTracksAndInsightsCycles(t *testing.T) {
	t.Parallel()
	health, err := GraphHealthFromBV(&bv.PlanOutput{
		Plan: bv.Plan{
			TotalActionable: 3,
			TotalBlocked:    1,
			Tracks: []bv.PlanTrack{
				{Items: []bv.PlanItem{{ID: "hp-a"}, {ID: "hp-b"}}},
				{Items: []bv.PlanItem{{ID: "hp-c"}}},
			},
		},
	}, &bv.InsightsOutput{
		Cycles: json.RawMessage(`[["hp-c","hp-d","hp-c"]]`),
	})
	if err != nil {
		t.Fatalf("GraphHealthFromBV: %v", err)
	}
	if health.ReadyCount != 3 || health.BlockedCount != 1 {
		t.Fatalf("counts = %+v", health)
	}
	if len(health.ParallelTracks) != 2 || strings.Join(health.ParallelTracks[0].BeadIDs, ",") != "hp-a,hp-b" {
		t.Fatalf("tracks = %+v", health.ParallelTracks)
	}
	if len(health.Cycles) != 1 || strings.Join(health.Cycles[0], ",") != "hp-c,hp-d" {
		t.Fatalf("cycles = %+v", health.Cycles)
	}
}

func TestBuildPolishPlanPrioritizesRecommendedRound(t *testing.T) {
	t.Parallel()
	report := QualityReport{
		PlanID:      "plan-1",
		Recommended: string(PolishRoundDependencies),
		Findings:    []string{"dependency_correctness: 1 dependency cycles detected"},
		Dimensions: []QualityDimension{{
			Name:     "dependency_correctness",
			Score:    0,
			Findings: []string{"1 dependency cycles detected"},
		}},
	}
	plan := BuildPolishPlan(PolishInput{
		PlanID:  "plan-1",
		Quality: report,
		Now:     func() time.Time { return time.Date(2026, 5, 4, 12, 0, 0, 0, time.UTC) },
	})
	if plan.Recommended != PolishRoundDependencies {
		t.Fatalf("recommended = %q", plan.Recommended)
	}
	if len(plan.Rounds) == 0 || plan.Rounds[0].ID != PolishRoundDependencies {
		t.Fatalf("round order = %+v", plan.Rounds)
	}
	if !strings.Contains(plan.Rounds[0].Prompt, "Never run bare bv") {
		t.Fatalf("dependency prompt = %q", plan.Rounds[0].Prompt)
	}
}
