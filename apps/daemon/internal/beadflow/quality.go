package beadflow

import (
	"fmt"
	"sort"
	"time"
)

type QualityInput struct {
	Traceability TraceabilityMap
	Graph        GraphHealth
	Previous     *QualityReport
	Now          func() time.Time
}

func ComputeQuality(input QualityInput) QualityReport {
	now := input.Now
	if now == nil {
		now = time.Now
	}
	dimensions := []QualityDimension{
		planCoverageDimension(input.Traceability),
		dependencyDimension(input.Graph),
		granularityDimension(input.Traceability),
		readySetDimension(input.Graph),
		testabilityDimension(input.Traceability),
		duplicateRiskDimension(input.Traceability),
		parallelismDimension(input.Graph),
		contextRichnessDimension(input.Traceability),
	}
	overall := averageDimensions(dimensions)
	report := QualityReport{
		SchemaVersion: QualitySchemaVersion,
		PlanID:        input.Traceability.PlanID,
		GeneratedAt:   now().UTC(),
		OverallScore:  overall,
		Dimensions:    dimensions,
		Recommended:   recommendPolishRound(dimensions),
		Findings:      flattenFindings(dimensions),
	}
	if input.Previous != nil {
		report.Delta = &QualityDelta{
			PreviousScore: input.Previous.OverallScore,
			CurrentScore:  overall,
			Delta:         overall - input.Previous.OverallScore,
		}
	}
	return report
}

func planCoverageDimension(trace TraceabilityMap) QualityDimension {
	findings := []string{}
	if len(trace.UnmappedSections) > 0 {
		findings = append(findings, fmt.Sprintf("%d plan sections are unmapped", len(trace.UnmappedSections)))
	}
	return QualityDimension{Name: "plan_coverage", Score: trace.PlanCoverageScore, Findings: findings}
}

func dependencyDimension(graph GraphHealth) QualityDimension {
	score := 100
	findings := []string{}
	if len(graph.Cycles) > 0 {
		score = 0
		findings = append(findings, fmt.Sprintf("%d dependency cycles detected", len(graph.Cycles)))
	}
	return QualityDimension{Name: "dependency_correctness", Score: score, Findings: findings}
}

func granularityDimension(trace TraceabilityMap) QualityDimension {
	score := 100 - len(trace.OversizedBeads)*15
	findings := []string{}
	if len(trace.OversizedBeads) > 0 {
		findings = append(findings, fmt.Sprintf("%d beads look oversized", len(trace.OversizedBeads)))
	}
	return QualityDimension{Name: "granularity", Score: clampScore(score), Findings: findings}
}

func readySetDimension(graph GraphHealth) QualityDimension {
	score := 100
	findings := []string{}
	switch {
	case graph.ReadyCount == 0:
		score = 25
		findings = append(findings, "no ready beads available")
	case graph.ReadyCount < 3:
		score = 70
		findings = append(findings, "ready frontier is narrow")
	}
	return QualityDimension{Name: "ready_set_size", Score: score, Findings: findings}
}

func testabilityDimension(trace TraceabilityMap) QualityDimension {
	total := len(trace.Beads)
	if total == 0 {
		return QualityDimension{Name: "testability", Score: 0, Findings: []string{"no beads to test"}}
	}
	missing := len(trace.MissingTestBeads)
	score := clampScore(((total - missing) * 100) / total)
	findings := []string{}
	if missing > 0 {
		findings = append(findings, fmt.Sprintf("%d beads lack test obligations or acceptance criteria", missing))
	}
	return QualityDimension{Name: "testability", Score: score, Findings: findings}
}

func duplicateRiskDimension(trace TraceabilityMap) QualityDimension {
	score := 100 - len(trace.DuplicateGroups)*20
	findings := []string{}
	if len(trace.DuplicateGroups) > 0 {
		findings = append(findings, fmt.Sprintf("%d duplicate bead groups detected", len(trace.DuplicateGroups)))
	}
	return QualityDimension{Name: "duplicate_risk", Score: clampScore(score), Findings: findings}
}

func parallelismDimension(graph GraphHealth) QualityDimension {
	score := 100
	findings := []string{}
	if len(graph.ParallelTracks) == 0 {
		score = 50
		findings = append(findings, "bv plan tracks unavailable")
	} else if len(graph.ParallelTracks) == 1 {
		score = 70
		findings = append(findings, "only one execution track available")
	}
	return QualityDimension{Name: "parallelism", Score: score, Findings: findings}
}

func contextRichnessDimension(trace TraceabilityMap) QualityDimension {
	total := len(trace.Beads)
	if total == 0 {
		return QualityDimension{Name: "context_richness", Score: 0, Findings: []string{"no bead context available"}}
	}
	thin := 0
	for _, bead := range trace.Beads {
		if bead.DescriptionWords < 40 && len(bead.ImplementationNotes) == 0 {
			thin++
		}
	}
	score := clampScore(((total - thin) * 100) / total)
	findings := []string{}
	if thin > 0 {
		findings = append(findings, fmt.Sprintf("%d beads need richer context", thin))
	}
	return QualityDimension{Name: "context_richness", Score: score, Findings: findings}
}

func averageDimensions(dimensions []QualityDimension) int {
	if len(dimensions) == 0 {
		return 0
	}
	total := 0
	for _, dimension := range dimensions {
		total += dimension.Score
	}
	return clampScore(total / len(dimensions))
}

func recommendPolishRound(dimensions []QualityDimension) string {
	if len(dimensions) == 0 {
		return "round_1_plan_coverage"
	}
	worst := dimensions[0]
	for _, dimension := range dimensions[1:] {
		if dimension.Score < worst.Score {
			worst = dimension
		}
	}
	switch worst.Name {
	case "plan_coverage":
		return "round_1_plan_coverage"
	case "dependency_correctness":
		return "round_2_dependency_correctness"
	case "granularity", "duplicate_risk", "context_richness":
		return "round_3_granularity_split_merge"
	case "testability":
		return "round_4_test_obligations"
	case "ready_set_size", "parallelism":
		return "round_5_parallel_execution_tracks"
	default:
		return "round_6_fresh_eyes_review"
	}
}

func flattenFindings(dimensions []QualityDimension) []string {
	out := []string{}
	for _, dimension := range dimensions {
		for _, finding := range dimension.Findings {
			out = append(out, dimension.Name+": "+finding)
		}
	}
	sort.Strings(out)
	return out
}
