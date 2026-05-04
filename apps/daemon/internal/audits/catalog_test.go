package audits

import (
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/hoopoe-cockpit/hoopoe/apps/daemon/internal/adapters/ubs"
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

func TestValidateCatalogRejectsBadDelegatedSource(t *testing.T) {
	t.Parallel()
	catalog := DefaultCatalog()
	catalog[2].Source = "skill:wrong"
	err := ValidateCatalog(catalog)
	if !errors.Is(err, ErrInvalidCatalog) {
		t.Fatalf("err = %v, want ErrInvalidCatalog", err)
	}
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
