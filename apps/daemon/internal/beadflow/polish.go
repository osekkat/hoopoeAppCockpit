package beadflow

import (
	"fmt"
	"time"
)

const PolishSchemaVersion = 1

type PolishRoundID string

const (
	PolishRoundPlanCoverage PolishRoundID = "round_1_plan_coverage"
	PolishRoundDependencies PolishRoundID = "round_2_dependency_correctness"
	PolishRoundGranularity  PolishRoundID = "round_3_granularity_split_merge"
	PolishRoundTests        PolishRoundID = "round_4_test_obligations"
	PolishRoundParallelism  PolishRoundID = "round_5_parallel_execution_tracks"
	PolishRoundFreshEyes    PolishRoundID = "round_6_fresh_eyes_review"
)

type PolishInput struct {
	PlanID       string
	Traceability TraceabilityMap
	Quality      QualityReport
	Graph        GraphHealth
	Now          func() time.Time
}

type PolishPlan struct {
	SchemaVersion   int            `json:"schemaVersion"`
	PlanID          string         `json:"planId"`
	GeneratedAt     time.Time      `json:"generatedAt"`
	Recommended     PolishRoundID  `json:"recommendedRound"`
	Rounds          []PolishRound  `json:"rounds"`
	ArtifactPaths   []ArtifactPath `json:"artifactPaths"`
	QualityFindings []string       `json:"qualityFindings,omitempty"`
}

type PolishRound struct {
	ID                PolishRoundID `json:"id"`
	Title             string        `json:"title"`
	Prompt            string        `json:"prompt"`
	TriggerDimensions []string      `json:"triggerDimensions"`
	RequiredArtifacts []string      `json:"requiredArtifacts"`
	Findings          []string      `json:"findings,omitempty"`
}

type ArtifactPath struct {
	Kind string `json:"kind"`
	Path string `json:"path"`
}

func BuildPolishPlan(input PolishInput) PolishPlan {
	now := input.Now
	if now == nil {
		now = time.Now
	}
	roundsByID := map[PolishRoundID]PolishRound{}
	for _, round := range defaultPolishRounds(input.PlanID) {
		roundsByID[round.ID] = round
	}
	for _, dimension := range input.Quality.Dimensions {
		if dimension.Score >= 100 && len(dimension.Findings) == 0 {
			continue
		}
		roundID := roundForDimension(dimension.Name)
		round := roundsByID[roundID]
		round.Findings = append(round.Findings, dimension.Findings...)
		roundsByID[roundID] = round
	}
	recommended := PolishRoundID(input.Quality.Recommended)
	if recommended == "" {
		recommended = PolishRoundFreshEyes
	}
	rounds := []PolishRound{}
	if round, ok := roundsByID[recommended]; ok {
		rounds = append(rounds, round)
	}
	for _, id := range []PolishRoundID{
		PolishRoundPlanCoverage,
		PolishRoundDependencies,
		PolishRoundGranularity,
		PolishRoundTests,
		PolishRoundParallelism,
		PolishRoundFreshEyes,
	} {
		if id == recommended {
			continue
		}
		round := roundsByID[id]
		if len(round.Findings) > 0 || id == PolishRoundFreshEyes {
			rounds = append(rounds, round)
		}
	}
	return PolishPlan{
		SchemaVersion:   PolishSchemaVersion,
		PlanID:          input.PlanID,
		GeneratedAt:     now().UTC(),
		Recommended:     recommended,
		Rounds:          rounds,
		ArtifactPaths:   BeadflowArtifactPaths(input.PlanID),
		QualityFindings: uniqueSorted(input.Quality.Findings),
	}
}

func BeadflowArtifactPaths(planID string) []ArtifactPath {
	if !safeSegment(planID) {
		return nil
	}
	base := fmt.Sprintf(".hoopoe/plans/%s/beadflow", planID)
	return []ArtifactPath{
		{Kind: "traceability", Path: base + "/traceability.json"},
		{Kind: "quality_report", Path: base + "/quality-report.json"},
		{Kind: "polish_plan", Path: base + "/polish-plan.json"},
		{Kind: "bv_graph_health", Path: base + "/bv-graph-health.json"},
	}
}

func defaultPolishRounds(planID string) []PolishRound {
	return []PolishRound{
		{
			ID:                PolishRoundPlanCoverage,
			Title:             "Plan coverage",
			Prompt:            fmt.Sprintf("Reread the locked plan %q and revise beads so every plan section maps to at least one self-contained bead. Use only br for bead mutations.", planID),
			TriggerDimensions: []string{"plan_coverage"},
			RequiredArtifacts: []string{"traceability.json", "quality-report.json"},
		},
		{
			ID:                PolishRoundDependencies,
			Title:             "Dependency correctness",
			Prompt:            "Use br dependency commands and bv robot insights to remove cycles and make blockers explicit. Never run bare bv.",
			TriggerDimensions: []string{"dependency_correctness"},
			RequiredArtifacts: []string{"bv-graph-health.json", "quality-report.json"},
		},
		{
			ID:                PolishRoundGranularity,
			Title:             "Granularity and split/merge",
			Prompt:            "Split oversized beads, merge duplicate beads, and enrich thin beads without dropping requirements. Use only br for mutations.",
			TriggerDimensions: []string{"granularity", "duplicate_risk", "context_richness"},
			RequiredArtifacts: []string{"traceability.json", "quality-report.json"},
		},
		{
			ID:                PolishRoundTests,
			Title:             "Test obligations",
			Prompt:            "Add explicit acceptance criteria and test obligations to every bead, including unit and e2e coverage where appropriate.",
			TriggerDimensions: []string{"testability"},
			RequiredArtifacts: []string{"traceability.json", "quality-report.json"},
		},
		{
			ID:                PolishRoundParallelism,
			Title:             "Parallel execution tracks",
			Prompt:            "Use bv robot plan output to widen the ready frontier and produce safe parallel tracks for swarm launch.",
			TriggerDimensions: []string{"ready_set_size", "parallelism"},
			RequiredArtifacts: []string{"bv-graph-health.json", "quality-report.json"},
		},
		{
			ID:                PolishRoundFreshEyes,
			Title:             "Fresh-eyes review",
			Prompt:            "Do one fresh pass over the bead graph for missing context, hidden dependencies, and over-simplified work units.",
			TriggerDimensions: []string{"fresh_eyes"},
			RequiredArtifacts: []string{"traceability.json", "quality-report.json", "polish-plan.json"},
		},
	}
}

func roundForDimension(name string) PolishRoundID {
	switch name {
	case "plan_coverage":
		return PolishRoundPlanCoverage
	case "dependency_correctness":
		return PolishRoundDependencies
	case "granularity", "duplicate_risk", "context_richness":
		return PolishRoundGranularity
	case "testability":
		return PolishRoundTests
	case "ready_set_size", "parallelism":
		return PolishRoundParallelism
	default:
		return PolishRoundFreshEyes
	}
}
