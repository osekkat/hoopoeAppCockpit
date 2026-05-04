package review

import (
	"errors"
	"testing"
	"time"

	"github.com/hoopoe-cockpit/hoopoe/apps/daemon/internal/adapters/ubs"
	"github.com/hoopoe-cockpit/hoopoe/apps/daemon/internal/convergence"
)

func TestRoundCatalogDefinesTenReviewRoundsWithoutProviderAPIMode(t *testing.T) {
	t.Parallel()
	catalog := RoundCatalog()
	if len(catalog) != 10 {
		t.Fatalf("catalog length = %d, want 10", len(catalog))
	}
	for i, spec := range catalog {
		if spec.Index != i || spec.RoundID == "" || spec.Kind == "" || spec.DefaultMode == "" {
			t.Fatalf("spec[%d] incomplete: %+v", i, spec)
		}
		for _, mode := range spec.AllowedModes {
			if mode == ExecutionMode("direct_provider_api") || mode == ExecutionMode("direct_llm") {
				t.Fatalf("round %s uses forbidden mode %q", spec.RoundID, mode)
			}
		}
	}
	if !catalog[0].AutoStart || catalog[0].DefaultMode != ModeDeterministicTool || catalog[0].Capabilities[0] != "ubs.scan" {
		t.Fatalf("round 0 = %+v", catalog[0])
	}
	if catalog[9].Kind != RoundFinalLanding {
		t.Fatalf("round 9 = %+v", catalog[9])
	}
}

func TestIngestUBSArtifactSourceStampsAndDedupesFindings(t *testing.T) {
	t.Parallel()
	ledger, err := NewLedger("hoopoe", fixedNow())
	if err != nil {
		t.Fatalf("NewLedger: %v", err)
	}
	artifact, err := ArtifactFromUBS(sampleUBSResult(), RoundRunMetadata{
		ProjectID:   "hoopoe",
		StartedAt:   fixedNow().Add(-time.Minute),
		CompletedAt: fixedNow(),
		Actor:       Actor{Kind: ActorTool, ID: "ubs"},
	})
	if err != nil {
		t.Fatalf("ArtifactFromUBS: %v", err)
	}
	ledger, summary, err := ledger.IngestRound(artifact)
	if err != nil {
		t.Fatalf("IngestRound: %v", err)
	}
	if summary.FindingsEmitted != 2 || summary.StoredFindings != 2 || summary.DedupedFindings != 0 {
		t.Fatalf("summary = %+v", summary)
	}
	if len(ledger.Findings) != 2 {
		t.Fatalf("findings = %+v", ledger.Findings)
	}
	if ledger.Findings[0].Source != SourceUBS || ledger.Findings[0].Severity != SeverityCritical {
		t.Fatalf("first finding = %+v", ledger.Findings[0])
	}

	secondArtifact, err := NewRoundArtifact(mustRound(t, 2), RoundRunMetadata{
		ProjectID:   "hoopoe",
		Mode:        ModeDelegatedAgent,
		StartedAt:   fixedNow().Add(time.Minute),
		CompletedAt: fixedNow().Add(2 * time.Minute),
		Actor:       Actor{Kind: ActorAgent, ID: "reviewer-1"},
	}, []Finding{
		{
			Source:    "agent:fresh-eyes",
			Severity:  SeverityHigh,
			Message:   "nil response can panic",
			FilePath:  "apps/daemon/internal/api/server.go",
			StartLine: 12,
			EndLine:   12,
			RuleID:    "go.nil-panic",
		},
	})
	if err != nil {
		t.Fatalf("NewRoundArtifact: %v", err)
	}
	ledger, summary, err = ledger.IngestRound(secondArtifact)
	if err != nil {
		t.Fatalf("second IngestRound: %v", err)
	}
	if summary.DedupedFindings != 1 || len(ledger.Findings) != 2 {
		t.Fatalf("dedupe summary=%+v findings=%+v", summary, ledger.Findings)
	}
	if got := ledger.Rounds[1].Findings[0].DuplicateOf; got != ledger.Findings[0].ID {
		t.Fatalf("duplicateOf = %q, want %q", got, ledger.Findings[0].ID)
	}
	if len(ledger.Findings[0].Sources) != 2 {
		t.Fatalf("merged sources = %+v", ledger.Findings[0].Sources)
	}
}

func TestFindingLifecycleTransitionsAndAuditEvents(t *testing.T) {
	t.Parallel()
	ledger, err := NewLedger("hoopoe", fixedNow())
	if err != nil {
		t.Fatalf("NewLedger: %v", err)
	}
	artifact, err := NewRoundArtifact(mustRound(t, 3), RoundRunMetadata{
		ProjectID:   "hoopoe",
		Mode:        ModeSubscriptionCLI,
		StartedAt:   fixedNow(),
		CompletedAt: fixedNow().Add(time.Minute),
		Actor:       Actor{Kind: ActorAgent, ID: "fresh-eyes"},
	}, []Finding{{
		Source:   "agent:fresh-eyes",
		Severity: SeverityHigh,
		Message:  "missing auth guard",
		FilePath: "apps/daemon/internal/api/server.go",
		RuleID:   "auth.guard",
	}})
	if err != nil {
		t.Fatalf("NewRoundArtifact: %v", err)
	}
	ledger, _, err = ledger.IngestRound(artifact)
	if err != nil {
		t.Fatalf("IngestRound: %v", err)
	}
	findingID := ledger.Findings[0].ID
	_, _, err = ledger.Transition(TransitionRequest{
		FindingID: findingID,
		To:        FindingClosed,
		Actor:     Actor{Kind: ActorUser, ID: "owner"},
		At:        fixedNow().Add(2 * time.Minute),
	})
	if !errors.Is(err, ErrInvalidTransition) {
		t.Fatalf("direct close err = %v, want ErrInvalidTransition", err)
	}

	ledger, event, err := ledger.Transition(TransitionRequest{
		FindingID: findingID,
		To:        FindingTriaged,
		Actor:     Actor{Kind: ActorUser, ID: "owner"},
		Reason:    "valid issue",
		At:        fixedNow().Add(2 * time.Minute),
	})
	if err != nil {
		t.Fatalf("triage: %v", err)
	}
	if event.Action != "review.finding_transitioned" || ledger.Findings[0].Status != FindingTriaged {
		t.Fatalf("triage event=%+v finding=%+v", event, ledger.Findings[0])
	}
	ledger, _, err = ledger.Transition(TransitionRequest{
		FindingID:   findingID,
		To:          FindingNewBead,
		Disposition: DispositionNewBead,
		Actor:       Actor{Kind: ActorUser, ID: "owner"},
		Reason:      "needs implementation bead",
		BeadID:      "hp-new",
		At:          fixedNow().Add(3 * time.Minute),
	})
	if err != nil {
		t.Fatalf("new bead: %v", err)
	}
	ledger, _, err = ledger.Transition(TransitionRequest{
		FindingID: findingID,
		To:        FindingClosed,
		Actor:     Actor{Kind: ActorAgent, ID: "reviewer"},
		Reason:    "bead filed",
		At:        fixedNow().Add(4 * time.Minute),
	})
	if err != nil {
		t.Fatalf("close: %v", err)
	}
	if ledger.Findings[0].Status != FindingClosed || ledger.Findings[0].Disposition != DispositionNewBead || ledger.Findings[0].BeadID != "hp-new" {
		t.Fatalf("finding = %+v", ledger.Findings[0])
	}
	if len(ledger.Findings[0].Transitions) != 3 {
		t.Fatalf("transitions = %+v", ledger.Findings[0].Transitions)
	}
	dashboard, err := ledger.Dashboard(DashboardInput{Now: fixedNow})
	if err != nil {
		t.Fatalf("Dashboard: %v", err)
	}
	if dashboard.LatestRound == nil || dashboard.LatestRound.NewBeads != 1 || dashboard.LatestRound.OpenSevereFindings != 0 {
		t.Fatalf("dashboard latest = %+v", dashboard.LatestRound)
	}
}

func TestDashboardProjectionUsesConvergenceDetector(t *testing.T) {
	t.Parallel()
	ledger, err := NewLedger("hoopoe", fixedNow())
	if err != nil {
		t.Fatalf("NewLedger: %v", err)
	}
	round0, err := NewRoundArtifact(mustRound(t, 0), RoundRunMetadata{
		ProjectID:   "hoopoe",
		Mode:        ModeDeterministicTool,
		Tool:        SourceUBS,
		StartedAt:   fixedNow(),
		CompletedAt: fixedNow().Add(10 * time.Minute),
		CostUnits:   10,
		Actor:       Actor{Kind: ActorTool, ID: "ubs"},
	}, []Finding{
		{Source: SourceUBS, Severity: SeverityCritical, Message: "secret in log", FilePath: "a.go", StartLine: 1, RuleID: "secret.log"},
		{Source: SourceUBS, Severity: SeverityHigh, Message: "unclosed body", FilePath: "b.go", StartLine: 2, RuleID: "http.body"},
		{Source: SourceUBS, Severity: SeverityMedium, Message: "ignored error", FilePath: "c.go", StartLine: 3, RuleID: "err.ignore"},
		{Source: SourceUBS, Severity: SeverityMedium, Message: "missing timeout", FilePath: "d.go", StartLine: 4, RuleID: "http.timeout"},
		{Source: SourceUBS, Severity: SeverityLow, Message: "style", FilePath: "e.go", StartLine: 5, RuleID: "style"},
	})
	if err != nil {
		t.Fatalf("round0: %v", err)
	}
	ledger, _, err = ledger.IngestRound(round0)
	if err != nil {
		t.Fatalf("ingest round0: %v", err)
	}
	dashboard, err := ledger.Dashboard(DashboardInput{Now: fixedNow})
	if err != nil {
		t.Fatalf("Dashboard: %v", err)
	}
	if dashboard.State != convergence.StateHighYield {
		t.Fatalf("state = %q, want high_yield", dashboard.State)
	}

	round9, err := NewRoundArtifact(mustRound(t, 9), RoundRunMetadata{
		ProjectID:     "hoopoe",
		Mode:          ModeDeterministicTool,
		StartedAt:     fixedNow().Add(time.Hour),
		CompletedAt:   fixedNow().Add(time.Hour + time.Minute),
		CostUnits:     4,
		EffortMinutes: 60,
		Actor:         Actor{Kind: ActorSystem, ID: "final-gate"},
	}, []Finding{})
	if err != nil {
		t.Fatalf("round9: %v", err)
	}
	ledger, _, err = ledger.IngestRound(round9)
	if err != nil {
		t.Fatalf("ingest round9: %v", err)
	}
	dashboard, err = ledger.Dashboard(DashboardInput{
		Landing: LandingInput{
			TestBuildsPassing: true,
			CodeHealthPassing: true,
			GitSynced:         true,
			BeadsSynced:       true,
		},
		Now: fixedNow,
	})
	if err != nil {
		t.Fatalf("Dashboard final: %v", err)
	}
	if dashboard.State != convergence.StateFinalGateReady {
		t.Fatalf("state = %q, want final_gate_ready dashboard=%+v", dashboard.State, dashboard)
	}
	if err := convergence.RequireFinalLandingReady(dashboard); err != nil {
		t.Fatalf("RequireFinalLandingReady: %v", err)
	}
}

func TestNextRoundProgression(t *testing.T) {
	t.Parallel()
	ledger, err := NewLedger("hoopoe", fixedNow())
	if err != nil {
		t.Fatalf("NewLedger: %v", err)
	}
	if next := ledger.NextRound(); next.NextRoundID != "round-0" || next.Complete {
		t.Fatalf("next empty = %+v", next)
	}
	artifact, err := NewRoundArtifact(mustRound(t, 0), RoundRunMetadata{
		ProjectID:   "hoopoe",
		Mode:        ModeDeterministicTool,
		StartedAt:   fixedNow(),
		CompletedAt: fixedNow(),
		Actor:       Actor{Kind: ActorTool, ID: "ubs"},
	}, nil)
	if err != nil {
		t.Fatalf("artifact: %v", err)
	}
	ledger, _, err = ledger.IngestRound(artifact)
	if err != nil {
		t.Fatalf("ingest: %v", err)
	}
	if next := ledger.NextRound(); next.NextRoundID != "round-1" || next.Complete {
		t.Fatalf("next after round0 = %+v", next)
	}
}

func sampleUBSResult() ubs.ScanResult {
	return ubs.ScanResult{
		ProjectDir: "/data/projects/hoopoe",
		Round:      ubs.RoundFirstPass,
		CheckedAt:  fixedNow(),
		Findings: []ubs.Finding{
			{
				FindingID: "ubs-one",
				Source:    SourceUBS,
				Sources:   []string{SourceUBS},
				FilePath:  "apps/daemon/internal/api/server.go",
				LineRange: ubs.LineRange{StartLine: 12, EndLine: 12},
				Severity:  ubs.SeverityCritical,
				Category:  "go",
				RuleID:    "go.nil-panic",
				Message:   "nil response can panic",
			},
			{
				FindingID: "ubs-two",
				Source:    SourceUBS,
				Sources:   []string{SourceUBS},
				FilePath:  "apps/daemon/internal/jobs/runner.go",
				LineRange: ubs.LineRange{StartLine: 44, EndLine: 45},
				Severity:  ubs.SeverityWarning,
				Category:  "go",
				RuleID:    "go.err-shadow",
				Message:   "shadowed error can hide failure",
			},
		},
	}
}

func mustRound(t *testing.T, index int) RoundSpec {
	t.Helper()
	spec, ok := RoundByIndex(index)
	if !ok {
		t.Fatalf("round %d not found", index)
	}
	return spec
}

func fixedNow() time.Time {
	return time.Date(2026, 5, 4, 12, 0, 0, 0, time.UTC)
}
