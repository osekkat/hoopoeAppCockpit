package audits

import (
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/hoopoe-cockpit/hoopoe/apps/daemon/internal/adapters/ubs"
	"github.com/hoopoe-cockpit/hoopoe/apps/daemon/internal/review"
)

func TestDefaultCatalogRegistersPhaseNinePointFiveAudits(t *testing.T) {
	t.Parallel()
	catalog := DefaultCatalog()
	if err := ValidateCatalog(catalog); err != nil {
		t.Fatalf("ValidateCatalog: %v", err)
	}
	wantIDs := []AuditID{
		AuditUBSStrict,
		AuditUBSRecentFiles,
		AuditMockCode,
		AuditDeadlock,
		AuditSecuritySaaS,
		AuditPerformance,
		AuditRealityCheck,
		AuditReasoningModes,
		AuditGoldenArtifacts,
		AuditFuzzing,
		AuditE2ENoMocks,
		AuditUIPolish,
	}
	if len(catalog) != len(wantIDs) {
		t.Fatalf("catalog length = %d, want %d", len(catalog), len(wantIDs))
	}
	for i, want := range wantIDs {
		if catalog[i].ID != want {
			t.Fatalf("catalog[%d] = %s, want %s", i, catalog[i].ID, want)
		}
		if !catalog[i].CreatesBeads {
			t.Fatalf("%s must create beads", catalog[i].ID)
		}
		if catalog[i].Round != RoundSpecialized {
			t.Fatalf("%s round = %s", catalog[i].ID, catalog[i].Round)
		}
	}
}

func TestDelegatedAuditsHaveSkillSourceStamps(t *testing.T) {
	t.Parallel()
	for _, definition := range DefaultCatalog() {
		if definition.ExecutionMode != ModeDelegatedAgent {
			continue
		}
		if definition.SkillID == "" {
			t.Fatalf("%s missing skill id", definition.ID)
		}
		if definition.Source != SourceForSkill(definition.SkillID) {
			t.Fatalf("%s source = %q, want %q", definition.ID, definition.Source, SourceForSkill(definition.SkillID))
		}
		if !reflect.DeepEqual(definition.RequiredSkills, []string{definition.SkillID}) {
			t.Fatalf("%s required skills = %#v", definition.ID, definition.RequiredSkills)
		}
		if !reflect.DeepEqual(definition.DedupeAgainst, []string{ubs.SourceUBS}) {
			t.Fatalf("%s dedupe = %#v", definition.ID, definition.DedupeAgainst)
		}
	}
}

func TestUBSAuditsUseAdapterCapabilityAndUBSSource(t *testing.T) {
	t.Parallel()
	for _, id := range []AuditID{AuditUBSStrict, AuditUBSRecentFiles} {
		definition, ok := Lookup(DefaultCatalog(), id)
		if !ok {
			t.Fatalf("missing %s", id)
		}
		if definition.ExecutionMode != ModeUBSAdapter {
			t.Fatalf("%s mode = %s", id, definition.ExecutionMode)
		}
		if definition.Source != ubs.SourceUBS {
			t.Fatalf("%s source = %s", id, definition.Source)
		}
		if !reflect.DeepEqual(definition.RequiredCapabilities, []string{ubs.CapabilityScan}) {
			t.Fatalf("%s capabilities = %#v", id, definition.RequiredCapabilities)
		}
	}
}

func TestPickerOptionsDisableMissingSkillsAndCapabilities(t *testing.T) {
	t.Parallel()
	options, err := PickerOptions(DefaultCatalog(), Availability{
		Capabilities: map[string]bool{ubs.CapabilityScan: true},
		Skills: map[string]bool{
			"mock-code-finder":               true,
			"deadlock-finder-and-fixer":      false,
			"security-audit-for-saas":        true,
			"profiling-software-performance": true,
		},
	})
	if err != nil {
		t.Fatalf("PickerOptions: %v", err)
	}
	assertEnabled(t, options, AuditUBSStrict, true, "")
	assertEnabled(t, options, AuditMockCode, true, "")
	assertEnabled(t, options, AuditDeadlock, false, "missing skill: deadlock-finder-and-fixer")
	assertEnabled(t, options, AuditGoldenArtifacts, false, "missing skill: testing-golden-artifacts")

	options, err = PickerOptions(DefaultCatalog(), Availability{
		Capabilities: map[string]bool{ubs.CapabilityScan: false},
	})
	if err != nil {
		t.Fatalf("PickerOptions missing cap: %v", err)
	}
	assertEnabled(t, options, AuditUBSRecentFiles, false, "missing capability: "+ubs.CapabilityScan)
}

func TestRequiredSkillRegistrationsAreDeterministic(t *testing.T) {
	t.Parallel()
	registrations, err := RequiredSkillRegistrations(DefaultCatalog())
	if err != nil {
		t.Fatalf("RequiredSkillRegistrations: %v", err)
	}
	wantSkills := []string{
		"deadlock-finder-and-fixer",
		"mock-code-finder",
		"modes-of-reasoning-project-analysis",
		"profiling-software-performance",
		"reality-check-for-project",
		"security-audit-for-saas",
		"testing-fuzzing",
		"testing-golden-artifacts",
		"testing-real-service-e2e-no-mocks",
		"ui-polish",
	}
	gotSkills := make([]string, 0, len(registrations))
	for _, registration := range registrations {
		gotSkills = append(gotSkills, registration.SkillID)
		if registration.Source != SourceForSkill(registration.SkillID) {
			t.Fatalf("%s source = %q", registration.SkillID, registration.Source)
		}
		if len(registration.AuditIDs) == 0 {
			t.Fatalf("%s missing audit ids", registration.SkillID)
		}
	}
	if !reflect.DeepEqual(gotSkills, wantSkills) {
		t.Fatalf("skill registrations = %#v, want %#v", gotSkills, wantSkills)
	}
}

func TestBuildRunnableSpecRequiresAvailableSkillAndCapability(t *testing.T) {
	t.Parallel()
	_, err := BuildRunnableSpec(DefaultCatalog(), Availability{
		Skills: map[string]bool{
			"deadlock-finder-and-fixer": false,
		},
	}, RunnerRequest{AuditID: AuditDeadlock})
	if !errors.Is(err, ErrUnavailable) || !strings.Contains(err.Error(), "missing skill: deadlock-finder-and-fixer") {
		t.Fatalf("deadlock readiness err = %v, want unavailable missing skill", err)
	}
	_, err = BuildRunnableSpec(DefaultCatalog(), Availability{
		Capabilities: map[string]bool{ubs.CapabilityScan: false},
	}, RunnerRequest{AuditID: AuditUBSStrict})
	if !errors.Is(err, ErrUnavailable) || !strings.Contains(err.Error(), "missing capability: "+ubs.CapabilityScan) {
		t.Fatalf("ubs readiness err = %v, want unavailable missing capability", err)
	}
	spec, err := BuildRunnableSpec(DefaultCatalog(), Availability{
		Capabilities: map[string]bool{ubs.CapabilityScan: true},
	}, RunnerRequest{AuditID: AuditUBSStrict, TargetPaths: []string{"apps/daemon/internal/audits"}})
	if err != nil {
		t.Fatalf("BuildRunnableSpec available: %v", err)
	}
	if spec.AuditID != AuditUBSStrict || spec.ExecutionMode != ModeUBSAdapter {
		t.Fatalf("spec = %+v", spec)
	}
}

func TestBuildRunnerSpecShapesDelegatedAgentPromptAndPolicy(t *testing.T) {
	t.Parallel()
	spec, err := BuildRunnerSpec(DefaultCatalog(), RunnerRequest{
		AuditID:         AuditDeadlock,
		ProjectID:       "proj_123",
		TargetPaths:     []string{"apps/daemon/internal/api", "apps/daemon/internal/api", "apps/daemon/internal/scheduler"},
		ExistingSources: []string{ubs.SourceUBS, "skill:mock-code-finder"},
		MaxFindings:     12,
	})
	if err != nil {
		t.Fatalf("BuildRunnerSpec: %v", err)
	}
	if spec.SchemaVersion != CatalogSchemaVersion || spec.ProjectID != "proj_123" {
		t.Fatalf("metadata = %+v", spec)
	}
	if spec.ExecutionMode != ModeDelegatedAgent {
		t.Fatalf("mode = %s", spec.ExecutionMode)
	}
	if !reflect.DeepEqual(spec.SkillIDs, []string{"deadlock-finder-and-fixer"}) {
		t.Fatalf("skills = %#v", spec.SkillIDs)
	}
	if spec.Source != "skill:deadlock-finder-and-fixer" || spec.FindingPolicy.Source != spec.Source {
		t.Fatalf("source policy = %+v", spec.FindingPolicy)
	}
	if spec.FindingPolicy.FreeFloatingAllowed || !spec.FindingPolicy.CreateBeads || !spec.FindingPolicy.StampRequired {
		t.Fatalf("finding policy = %+v", spec.FindingPolicy)
	}
	if !strings.Contains(spec.Prompt, "source: skill:deadlock-finder-and-fixer") {
		t.Fatalf("prompt missing source stamp: %s", spec.Prompt)
	}
	if !strings.Contains(spec.Prompt, "Convert actionable findings into beads") {
		t.Fatalf("prompt missing bead policy: %s", spec.Prompt)
	}
	if !reflect.DeepEqual(spec.TargetPaths, []string{"apps/daemon/internal/api", "apps/daemon/internal/scheduler"}) {
		t.Fatalf("target paths = %#v", spec.TargetPaths)
	}
}

func TestBuildRunnerSpecRejectsUnknownAuditAndUnsafeTargets(t *testing.T) {
	t.Parallel()
	_, err := BuildRunnerSpec(DefaultCatalog(), RunnerRequest{AuditID: "missing"})
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing audit err = %v", err)
	}
	_, err = BuildRunnerSpec(DefaultCatalog(), RunnerRequest{
		AuditID:     AuditMockCode,
		TargetPaths: []string{"../outside"},
	})
	if !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("unsafe target err = %v", err)
	}
}

func TestStampAndMergeFindingsPreservesCrossToolSources(t *testing.T) {
	t.Parallel()
	deadlock := StampFindings("skill:deadlock-finder-and-fixer", []Finding{
		{
			FilePath: "apps/daemon/internal/api/server.go",
			Line:     42,
			RuleID:   "lock-order",
			Category: "concurrency",
			Message:  "mutex order can deadlock",
		},
	})
	ubsFinding := FromUBSFindings([]ubs.Finding{
		{
			FindingID: "ubs_1",
			Source:    ubs.SourceUBS,
			Sources:   []string{ubs.SourceUBS},
			FilePath:  "apps/daemon/internal/api/server.go",
			LineRange: ubs.LineRange{StartLine: 42},
			RuleID:    "lock-order",
			Category:  "concurrency",
			Message:   "mutex order can deadlock",
		},
	})
	merged := MergeFindings(deadlock, ubsFinding)
	if len(merged) != 1 {
		t.Fatalf("merged = %+v", merged)
	}
	wantSources := []string{"skill:deadlock-finder-and-fixer", ubs.SourceUBS}
	if !reflect.DeepEqual(merged[0].Sources, wantSources) {
		t.Fatalf("sources = %#v, want %#v", merged[0].Sources, wantSources)
	}
	if merged[0].Source != "skill:deadlock-finder-and-fixer" {
		t.Fatalf("primary source = %q", merged[0].Source)
	}
}

func TestBeadDraftsFromFindingsCreateStampedBeadDrafts(t *testing.T) {
	t.Parallel()
	spec, err := BuildRunnerSpec(DefaultCatalog(), RunnerRequest{
		AuditID: AuditDeadlock,
	})
	if err != nil {
		t.Fatalf("BuildRunnerSpec: %v", err)
	}
	drafts, err := BeadDraftsFromFindings(spec, []Finding{
		{
			ID:       "deadlock-1",
			FilePath: "apps/daemon/internal/api/server.go",
			Line:     42,
			EndLine:  44,
			RuleID:   "lock-order",
			Severity: "critical",
			Category: "concurrency",
			Message:  "mutex order can deadlock. acquire project lock before session lock",
		},
		{
			Source:   ubs.SourceUBS,
			Sources:  []string{ubs.SourceUBS},
			FilePath: "apps/daemon/internal/api/server.go",
			Line:     42,
			RuleID:   "lock-order",
			Severity: "high",
			Category: "concurrency",
			Message:  "mutex order can deadlock",
		},
	})
	if err != nil {
		t.Fatalf("BeadDraftsFromFindings: %v", err)
	}
	if len(drafts) != 2 {
		t.Fatalf("drafts = %+v", drafts)
	}
	var first BeadDraft
	for _, draft := range drafts {
		if draft.FindingID == "deadlock-1" {
			first = draft
			break
		}
	}
	if first.FindingID == "" {
		t.Fatalf("drafts = %+v, missing deadlock-1", drafts)
	}
	if first.SchemaVersion != CatalogSchemaVersion || first.AuditID != AuditDeadlock || first.IssueType != "task" {
		t.Fatalf("draft metadata = %+v", first)
	}
	if first.Source != "skill:deadlock-finder-and-fixer" || !contains(first.Sources, first.Source) {
		t.Fatalf("source stamp = %+v", first)
	}
	if first.Priority != 0 {
		t.Fatalf("priority = %d, want critical P0", first.Priority)
	}
	for _, label := range []string{"review", "specialized-audit", "audit-deadlock-concurrency", "source-skill-deadlock-finder-and-fixer", "cat-concurrency", "sev-critical"} {
		if !contains(first.Labels, label) {
			t.Fatalf("labels = %#v, missing %q", first.Labels, label)
		}
	}
	if !strings.Contains(first.Description, "Source: skill:deadlock-finder-and-fixer") ||
		!strings.Contains(first.Description, "Location: apps/daemon/internal/api/server.go:42-44") ||
		!strings.Contains(first.Description, "Do not leave this as a free-floating TODO") {
		t.Fatalf("description = %s", first.Description)
	}
	if len(first.AcceptanceCriteria) != 3 || !strings.Contains(first.AcceptanceCriteria[0], "deadlock-1") {
		t.Fatalf("acceptance = %#v", first.AcceptanceCriteria)
	}
}

func TestBeadDraftsFromFindingsDedupeAndRespectMaxFindings(t *testing.T) {
	t.Parallel()
	spec, err := BuildRunnerSpec(DefaultCatalog(), RunnerRequest{
		AuditID:     AuditMockCode,
		MaxFindings: 1,
	})
	if err != nil {
		t.Fatalf("BuildRunnerSpec: %v", err)
	}
	findings := []Finding{
		{Source: "skill:mock-code-finder", FilePath: "a.go", Line: 1, RuleID: "placeholder", Message: "placeholder implementation"},
		{Source: ubs.SourceUBS, Sources: []string{ubs.SourceUBS}, FilePath: "a.go", Line: 1, RuleID: "placeholder", Message: "placeholder implementation"},
		{FilePath: "b.go", Line: 2, RuleID: "todo", Message: "TODO remains"},
	}
	drafts, err := BeadDraftsFromFindings(spec, findings)
	if err != nil {
		t.Fatalf("BeadDraftsFromFindings: %v", err)
	}
	if len(drafts) != 1 {
		t.Fatalf("drafts = %+v, want max 1", drafts)
	}
	if !contains(drafts[0].Sources, "skill:mock-code-finder") || !contains(drafts[0].Sources, ubs.SourceUBS) {
		t.Fatalf("sources = %#v, want skill + UBS dedupe", drafts[0].Sources)
	}
}

func TestReviewArtifactFromFindingsCreatesSpecializedRoundArtifact(t *testing.T) {
	t.Parallel()
	spec, err := BuildRunnerSpec(DefaultCatalog(), RunnerRequest{
		AuditID:   AuditDeadlock,
		ProjectID: "hoopoe",
	})
	if err != nil {
		t.Fatalf("BuildRunnerSpec: %v", err)
	}
	artifact, err := ReviewArtifactFromFindings(spec, review.RoundRunMetadata{
		StartedAt:   fixedAuditTime(),
		CompletedAt: fixedAuditTime().Add(time.Minute),
		Actor:       review.Actor{Kind: review.ActorAgent, ID: "deadlock-auditor"},
	}, []Finding{
		{
			ID:       "deadlock-1",
			FilePath: "apps/daemon/internal/api/server.go",
			Line:     42,
			EndLine:  44,
			RuleID:   "lock-order",
			Severity: "critical",
			Category: "concurrency",
			Message:  "mutex order can deadlock",
		},
	})
	if err != nil {
		t.Fatalf("ReviewArtifactFromFindings: %v", err)
	}
	if artifact.ProjectID != "hoopoe" || artifact.RoundID != "round-8" || artifact.Kind != review.RoundSpecializedAudit {
		t.Fatalf("artifact metadata = %+v", artifact)
	}
	if artifact.Mode != review.ModeDelegatedAgent || artifact.Tool != "skill:deadlock-finder-and-fixer" {
		t.Fatalf("artifact execution = %+v", artifact)
	}
	if artifact.Metadata["auditId"] != string(AuditDeadlock) || artifact.Metadata["skillIds"] != "deadlock-finder-and-fixer" {
		t.Fatalf("artifact audit metadata = %#v", artifact.Metadata)
	}
	if len(artifact.Findings) != 1 {
		t.Fatalf("findings = %+v", artifact.Findings)
	}
	finding := artifact.Findings[0]
	if finding.Source != "skill:deadlock-finder-and-fixer" || !contains(finding.Sources, "skill:deadlock-finder-and-fixer") {
		t.Fatalf("finding source = %+v", finding)
	}
	if finding.Severity != review.SeverityCritical || finding.Status != review.FindingNew {
		t.Fatalf("finding severity/status = %+v", finding)
	}
	if finding.Evidence[0].Kind != "specialized_audit" || finding.Evidence[0].ID != string(AuditDeadlock) {
		t.Fatalf("evidence = %+v", finding.Evidence)
	}
	if finding.Metadata["auditFindingId"] != "deadlock-1" || finding.Metadata["auditId"] != string(AuditDeadlock) {
		t.Fatalf("finding metadata = %#v", finding.Metadata)
	}
}

func TestReviewArtifactFromFindingsDedupesWithExistingReviewLedger(t *testing.T) {
	t.Parallel()
	ledger, err := review.NewLedger("hoopoe", fixedAuditTime())
	if err != nil {
		t.Fatalf("NewLedger: %v", err)
	}
	ubsRound, ok := review.RoundByIndex(0)
	if !ok {
		t.Fatal("round 0 not found")
	}
	ubsArtifact, err := review.NewRoundArtifact(ubsRound, review.RoundRunMetadata{
		ProjectID:   "hoopoe",
		Mode:        review.ModeDeterministicTool,
		Tool:        ubs.SourceUBS,
		StartedAt:   fixedAuditTime(),
		CompletedAt: fixedAuditTime().Add(time.Minute),
		Actor:       review.Actor{Kind: review.ActorTool, ID: "ubs"},
	}, []review.Finding{
		{
			Source:    ubs.SourceUBS,
			Severity:  review.SeverityHigh,
			Message:   "mutex order can deadlock",
			FilePath:  "apps/daemon/internal/api/server.go",
			StartLine: 42,
			EndLine:   42,
			RuleID:    "lock-order",
		},
	})
	if err != nil {
		t.Fatalf("NewRoundArtifact: %v", err)
	}
	ledger, _, err = ledger.IngestRound(ubsArtifact)
	if err != nil {
		t.Fatalf("ingest UBS: %v", err)
	}
	spec, err := BuildRunnerSpec(DefaultCatalog(), RunnerRequest{
		AuditID:   AuditDeadlock,
		ProjectID: "hoopoe",
	})
	if err != nil {
		t.Fatalf("BuildRunnerSpec: %v", err)
	}
	auditArtifact, err := ReviewArtifactFromFindings(spec, review.RoundRunMetadata{
		StartedAt:   fixedAuditTime().Add(2 * time.Minute),
		CompletedAt: fixedAuditTime().Add(3 * time.Minute),
		Actor:       review.Actor{Kind: review.ActorAgent, ID: "deadlock-auditor"},
	}, []Finding{
		{
			ID:       "deadlock-duplicate",
			FilePath: "apps/daemon/internal/api/server.go",
			Line:     42,
			EndLine:  42,
			RuleID:   "lock-order",
			Severity: "critical",
			Category: "concurrency",
			Message:  "mutex order can deadlock",
		},
	})
	if err != nil {
		t.Fatalf("ReviewArtifactFromFindings: %v", err)
	}
	ledger, summary, err := ledger.IngestRound(auditArtifact)
	if err != nil {
		t.Fatalf("ingest audit: %v", err)
	}
	if summary.DedupedFindings != 1 || summary.StoredFindings != 0 || len(ledger.Findings) != 1 {
		t.Fatalf("summary=%+v findings=%+v", summary, ledger.Findings)
	}
	if got := ledger.Rounds[1].Findings[0].DuplicateOf; got != ledger.Findings[0].ID {
		t.Fatalf("duplicateOf = %q, want %q", got, ledger.Findings[0].ID)
	}
	for _, source := range []string{ubs.SourceUBS, "skill:deadlock-finder-and-fixer"} {
		if !contains(ledger.Findings[0].Sources, source) {
			t.Fatalf("merged sources = %#v, missing %q", ledger.Findings[0].Sources, source)
		}
	}
}

func TestReviewArtifactFromFindingsMapsUBSAdapterToDeterministicRound(t *testing.T) {
	t.Parallel()
	spec, err := BuildRunnerSpec(DefaultCatalog(), RunnerRequest{
		AuditID:   AuditUBSStrict,
		ProjectID: "hoopoe",
	})
	if err != nil {
		t.Fatalf("BuildRunnerSpec: %v", err)
	}
	artifact, err := ReviewArtifactFromFindings(spec, review.RoundRunMetadata{
		StartedAt:   fixedAuditTime(),
		CompletedAt: fixedAuditTime(),
		Actor:       review.Actor{Kind: review.ActorTool, ID: "ubs"},
	}, []Finding{{
		FilePath: "apps/daemon/internal/audits/catalog.go",
		Line:     12,
		RuleID:   "go.errcheck",
		Message:  "error is ignored",
	}})
	if err != nil {
		t.Fatalf("ReviewArtifactFromFindings: %v", err)
	}
	if artifact.Kind != review.RoundSpecializedAudit || artifact.Mode != review.ModeDeterministicTool || artifact.Tool != ubs.SourceUBS {
		t.Fatalf("artifact = %+v", artifact)
	}
	if artifact.Metadata["requiredCapabilities"] != ubs.CapabilityScan {
		t.Fatalf("metadata = %#v", artifact.Metadata)
	}
	if artifact.Findings[0].Source != ubs.SourceUBS || artifact.Findings[0].Mode != review.ModeDeterministicTool {
		t.Fatalf("finding = %+v", artifact.Findings[0])
	}
}

func TestBeadDraftsFromFindingsRejectsFreeFloatingPolicy(t *testing.T) {
	t.Parallel()
	spec, err := BuildRunnerSpec(DefaultCatalog(), RunnerRequest{AuditID: AuditMockCode})
	if err != nil {
		t.Fatalf("BuildRunnerSpec: %v", err)
	}
	spec.FindingPolicy.CreateBeads = false
	_, err = BeadDraftsFromFindings(spec, []Finding{{Message: "finding"}})
	if !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("err = %v, want ErrInvalidRequest", err)
	}
}

func TestValidateCatalogRejectsBadDelegatedSource(t *testing.T) {
	t.Parallel()
	catalog := DefaultCatalog()
	catalog[2].Source = "skill:wrong"
	err := ValidateCatalog(catalog)
	if !errors.Is(err, ErrInvalidCatalog) {
		t.Fatalf("err = %v, want ErrInvalidCatalog", err)
	}
}

func fixedAuditTime() time.Time {
	return time.Date(2026, 5, 4, 12, 0, 0, 0, time.UTC)
}

func assertEnabled(t *testing.T, options []PickerOption, id AuditID, want bool, reason string) {
	t.Helper()
	for _, option := range options {
		if option.Definition.ID != id {
			continue
		}
		if option.Enabled != want {
			t.Fatalf("%s enabled = %v, want %v; reasons=%#v", id, option.Enabled, want, option.DisabledReasons)
		}
		if reason != "" && !contains(option.DisabledReasons, reason) {
			t.Fatalf("%s reasons = %#v, want %q", id, option.DisabledReasons, reason)
		}
		return
	}
	t.Fatalf("missing option %s", id)
}
