package beadflow

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestBuildTraceabilityFlagsCoverageGapsAndRisks(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 5, 4, 12, 0, 0, 0, time.UTC)
	sections := []PlanSection{
		{ID: "s1", Title: "Planning input", RequiredTests: []string{"unit: input parser"}},
		{ID: "s2", Title: "Bead conversion", RequiredTests: []string{"e2e: convert locked plan"}},
		{ID: "s3", Title: "Polish rounds"},
	}
	beads := []Bead{
		{
			ID:                 "hp-a",
			Title:              "Build parser",
			Description:        strings.Repeat("word ", 20),
			PlanSections:       []string{"s1"},
			TestObligations:    []string{"unit: input parser"},
			EstimatedMinutes:   90,
			AcceptanceCriteria: []string{"parser handles headings"},
		},
		{
			ID:                 "hp-b",
			Title:              "Convert plan",
			Description:        strings.Repeat("word ", 260),
			PlanSections:       []string{"s2"},
			EstimatedMinutes:   600,
			TestObligations:    []string{},
			AcceptanceCriteria: []string{},
		},
		{ID: "hp-c", Title: "Build parser", Description: "thin", PlanSections: nil},
	}

	trace := BuildTraceability(sections, beads, TraceabilityOptions{
		PlanID: "plan-1",
		Now:    func() time.Time { return now },
	})

	if trace.SchemaVersion != TraceabilitySchemaVersion || !trace.GeneratedAt.Equal(now) {
		t.Fatalf("trace metadata incorrect: %+v", trace)
	}
	if trace.PlanCoverageScore != 53 {
		t.Fatalf("coverage score = %d, want 53", trace.PlanCoverageScore)
	}
	if len(trace.UnmappedSections) != 1 || trace.UnmappedSections[0].SectionID != "s3" {
		t.Fatalf("unmapped sections = %+v", trace.UnmappedSections)
	}
	if len(trace.OrphanBeads) != 1 || trace.OrphanBeads[0].BeadID != "hp-c" {
		t.Fatalf("orphan beads = %+v", trace.OrphanBeads)
	}
	if len(trace.OversizedBeads) != 1 || trace.OversizedBeads[0].BeadID != "hp-b" {
		t.Fatalf("oversized beads = %+v", trace.OversizedBeads)
	}
	if len(trace.DuplicateGroups) != 1 {
		t.Fatalf("duplicate groups = %+v", trace.DuplicateGroups)
	}
	if len(trace.MissingTestBeads) != 2 {
		t.Fatalf("missing test beads = %+v", trace.MissingTestBeads)
	}
}

func TestComputeQualityRecommendsWorstPolishRound(t *testing.T) {
	t.Parallel()
	trace := TraceabilityMap{
		SchemaVersion:     TraceabilitySchemaVersion,
		PlanID:            "plan-1",
		PlanCoverageScore: 60,
		Beads: []BeadTrace{
			{BeadID: "hp-a", DescriptionWords: 80, TestObligations: []string{"unit"}},
			{BeadID: "hp-b", DescriptionWords: 10},
		},
		MissingTestBeads: []BeadTrace{{BeadID: "hp-b"}},
		OversizedBeads:   []BeadTrace{{BeadID: "hp-c"}},
		DuplicateGroups:  []DuplicateBeads{{Reason: "same title", Beads: []string{"hp-a", "hp-d"}}},
	}
	previous := &QualityReport{OverallScore: 50}
	report := ComputeQuality(QualityInput{
		Traceability: trace,
		Graph: GraphHealth{
			Cycles:       [][]string{{"hp-a", "hp-b", "hp-a"}},
			ReadyCount:   1,
			BlockedCount: 5,
		},
		Previous: previous,
		Now:      func() time.Time { return time.Date(2026, 5, 4, 12, 0, 0, 0, time.UTC) },
	})
	if report.OverallScore <= 0 || report.OverallScore >= 70 {
		t.Fatalf("unexpected overall score: %+v", report)
	}
	if report.Recommended != "round_2_dependency_correctness" {
		t.Fatalf("recommended = %q", report.Recommended)
	}
	if report.Delta == nil || report.Delta.CurrentScore != report.OverallScore {
		t.Fatalf("delta missing or wrong: %+v", report.Delta)
	}
	if len(report.Findings) == 0 {
		t.Fatalf("expected findings")
	}
}

func TestEvidenceLedgerAppendsJSONLAndRejectsTraversal(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	ledger := EvidenceLedger{
		Root: root,
		Now:  func() time.Time { return time.Date(2026, 5, 4, 12, 0, 0, 0, time.UTC) },
	}
	rel, err := ledger.Append(context.Background(), "plan-1", EvidenceEntry{
		Kind:          EvidenceCommitPushed,
		BeadID:        "hp-a",
		PlanSectionID: "s1",
		Branch:        "hp-a",
		Commits:       []string{"abc123"},
		FilesTouched:  []string{"apps/daemon/main.go"},
		Actor:         "agent",
	})
	if err != nil {
		t.Fatalf("Append: %v", err)
	}
	if rel != ".hoopoe/plans/plan-1/implementation-evidence.jsonl" {
		t.Fatalf("rel path = %q", rel)
	}
	data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(rel)))
	if err != nil {
		t.Fatalf("read ledger: %v", err)
	}
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	if len(lines) != 1 {
		t.Fatalf("ledger lines = %d", len(lines))
	}
	var entry EvidenceEntry
	if err := json.Unmarshal([]byte(lines[0]), &entry); err != nil {
		t.Fatalf("decode ledger line: %v", err)
	}
	if entry.SchemaVersion != EvidenceSchemaVersion || entry.PlanID != "plan-1" || entry.BeadID != "hp-a" {
		t.Fatalf("entry mismatch: %+v", entry)
	}
	if _, err := ledger.Append(context.Background(), "../plan", EvidenceEntry{Kind: EvidenceLanding, BeadID: "hp-a"}); err == nil {
		t.Fatalf("expected traversal rejection")
	}
}
