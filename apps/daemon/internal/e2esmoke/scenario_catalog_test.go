package e2esmoke

import (
	"strings"
	"testing"
)

func TestCatalogContainsAllSixteenSection182Steps(t *testing.T) {
	t.Parallel()
	want := []StepID{
		StepInstallSignedDMG,
		StepConnectFreshUbuntuVPS,
		StepRunACFSBootstrap,
		StepImportFixtureRepo,
		StepCreateOrImportPlan,
		StepGenerateOrLockPlan,
		StepConvertPlanToBeads,
		StepCurateBeadInUI,
		StepLaunchSmokeSwarm,
		StepIngestAgentMail,
		StepVPSCommitAndSync,
		StepRunHealthSnapshot,
		StepFreshEyesReview,
		StepKillRestartReplay,
		StepUpgradeDaemonCompatibility,
		StepNoSecretsInLogs,
	}
	got := Catalog()
	if len(got) != len(want) {
		t.Fatalf("catalog length = %d, want %d (the §18.2 scenario has 16 steps)", len(got), len(want))
	}
	for i, step := range got {
		if step.ID != want[i] {
			t.Errorf("catalog[%d].ID = %q, want %q (order must match plan.md §18.2)", i, step.ID, want[i])
		}
	}
}

func TestEveryStepHasNonEmptyTitleDescriptionEvidence(t *testing.T) {
	t.Parallel()
	for _, step := range Catalog() {
		if step.Title == "" {
			t.Errorf("%s: Title is empty", step.ID)
		}
		if step.Description == "" {
			t.Errorf("%s: Description is empty", step.ID)
		}
		if len(step.EvidenceArtifacts) == 0 {
			t.Errorf("%s: EvidenceArtifacts is empty (per §18.2 each step writes pass/fail evidence)", step.ID)
		}
	}
}

func TestEveryStepIDIsE2eSmokeNamespaced(t *testing.T) {
	t.Parallel()
	for _, step := range Catalog() {
		if !strings.HasPrefix(string(step.ID), "e2e_smoke.") {
			t.Errorf("%s: ID is missing the `e2e_smoke.` namespace prefix", step.ID)
		}
	}
}

func TestNumbersAreSequentialFromOne(t *testing.T) {
	t.Parallel()
	for i, step := range Catalog() {
		if step.Number != i+1 {
			t.Errorf("catalog[%d].Number = %d, want %d (§18.2 ordinals must match list order)", i, step.Number, i+1)
		}
	}
}

func TestEveryStepSupportsRealVPSVariant(t *testing.T) {
	t.Parallel()
	// §18.2 acceptance: "scenario runs end-to-end against a real
	// research-spike VPS." Every step must support real_vps.
	for _, step := range Catalog() {
		found := false
		for _, v := range step.SupportsVariants {
			if v == VariantRealVPS {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("%s: must support real_vps variant per §18.2 acceptance", step.ID)
		}
	}
}

func TestStepStagesMapToValidStageValues(t *testing.T) {
	t.Parallel()
	validStages := map[Stage]bool{
		StageOnboarding: true, StagePlanning: true, StageBeads: true,
		StageSwarm: true, StageHardening: true,
		StageOperations: true, StageRelease: true,
	}
	for _, step := range Catalog() {
		if !validStages[step.Stage] {
			t.Errorf("%s: Stage %q is not a recognized stage", step.ID, step.Stage)
		}
	}
}

func TestStepIDsAreUnique(t *testing.T) {
	t.Parallel()
	seen := make(map[StepID]bool, 16)
	for _, step := range Catalog() {
		if seen[step.ID] {
			t.Errorf("duplicate ID in catalog: %s", step.ID)
		}
		seen[step.ID] = true
	}
}

func TestLookupReturnsOkOnHitFalseOnMiss(t *testing.T) {
	t.Parallel()
	if _, ok := Lookup(StepInstallSignedDMG); !ok {
		t.Errorf("Lookup must return true for a known ID")
	}
	if _, ok := Lookup(StepID("e2e_smoke.99.does_not_exist")); ok {
		t.Errorf("Lookup must return false for an unknown ID")
	}
}

func TestByVariantFiltersCorrectly(t *testing.T) {
	t.Parallel()
	realVPS := ByVariant(VariantRealVPS)
	if len(realVPS) != 16 {
		t.Errorf("expected 16 real_vps steps, got %d (every §18.2 step must support real_vps)", len(realVPS))
	}
	mockFlywheel := ByVariant(VariantMockFlywheel)
	if len(mockFlywheel) == 0 {
		t.Errorf("expected ≥1 mock_flywheel step (the nightly CI variant requires Mock Flywheel coverage)")
	}
	for _, step := range realVPS {
		ok := false
		for _, v := range step.SupportsVariants {
			if v == VariantRealVPS {
				ok = true
				break
			}
		}
		if !ok {
			t.Errorf("ByVariant(real_vps) returned %s which doesn't support real_vps", step.ID)
		}
	}
}

func TestByStageFiltersCorrectly(t *testing.T) {
	t.Parallel()
	planning := ByStage(StagePlanning)
	if len(planning) == 0 {
		t.Fatalf("expected at least one §18.2 step in stage 01_planning")
	}
	for _, step := range planning {
		if step.Stage != StagePlanning {
			t.Errorf("ByStage(01_planning) returned %s with stage %s", step.ID, step.Stage)
		}
	}
}

func TestNoSecretsStepReferencesG73AuditSubstrate(t *testing.T) {
	t.Parallel()
	step, ok := Lookup(StepNoSecretsInLogs)
	if !ok {
		t.Fatal("no-secrets step missing from catalog")
	}
	hasG73 := false
	for _, sub := range step.SubstrateBeads {
		if sub == "hp-g73" {
			hasG73 = true
			break
		}
	}
	if !hasG73 {
		t.Errorf("no-secrets step must reference hp-g73 (audit + redaction substrate) in SubstrateBeads")
	}
	// Evidence must scan four artifact classes per §18.2.
	want := []string{"audit-log", "structured-log", "plan-job", "crash-report"}
	for _, prefix := range want {
		matched := false
		for _, art := range step.EvidenceArtifacts {
			if strings.Contains(art, prefix) {
				matched = true
				break
			}
		}
		if !matched {
			t.Errorf("no-secrets evidence missing artifact containing %q", prefix)
		}
	}
}

func TestUpgradeDaemonStepReferencesCompatibilityFlow(t *testing.T) {
	t.Parallel()
	step, ok := Lookup(StepUpgradeDaemonCompatibility)
	if !ok {
		t.Fatal("upgrade-daemon step missing from catalog")
	}
	if step.Stage != StageRelease {
		t.Errorf("upgrade-daemon step.Stage = %s, want release", step.Stage)
	}
	// Must reference both vN-1 and vN versions in evidence.
	hasPre := false
	hasPost := false
	for _, art := range step.EvidenceArtifacts {
		if strings.Contains(art, "pre-upgrade") {
			hasPre = true
		}
		if strings.Contains(art, "post-upgrade") {
			hasPost = true
		}
	}
	if !hasPre || !hasPost {
		t.Errorf("upgrade-daemon evidence must include pre+post upgrade version artifacts")
	}
}

func TestHealthSnapshotStepEnforcesGuardrail5(t *testing.T) {
	t.Parallel()
	step, ok := Lookup(StepRunHealthSnapshot)
	if !ok {
		t.Fatal("health-snapshot step missing from catalog")
	}
	if !strings.Contains(step.Description, "isolated worktree") &&
		!strings.Contains(step.Description, "Guardrail 5") {
		t.Errorf("health-snapshot description must reference isolated-worktree / Guardrail 5: %q", step.Description)
	}
}

func TestKillRestartStepCoversBothDesktopAndDaemon(t *testing.T) {
	t.Parallel()
	step, ok := Lookup(StepKillRestartReplay)
	if !ok {
		t.Fatal("kill-restart step missing from catalog")
	}
	if !strings.Contains(step.Description, "desktop") || !strings.Contains(step.Description, "daemon") {
		t.Errorf("kill-restart must cover both desktop and daemon kill+restart: %q", step.Description)
	}
	hasDesktop := false
	hasDaemon := false
	for _, art := range step.EvidenceArtifacts {
		if strings.Contains(art, "desktop") {
			hasDesktop = true
		}
		if strings.Contains(art, "daemon") {
			hasDaemon = true
		}
	}
	if !hasDesktop || !hasDaemon {
		t.Errorf("kill-restart evidence must include both desktop + daemon kill-restart logs")
	}
}

func TestCatalogIsImmutableAcrossCalls(t *testing.T) {
	t.Parallel()
	a := Catalog()
	b := Catalog()
	if len(a) != len(b) {
		t.Fatalf("catalog length differs across calls: %d vs %d", len(a), len(b))
	}
	for i := range a {
		if a[i].ID != b[i].ID {
			t.Errorf("catalog[%d] differs across calls: %q vs %q", i, a[i].ID, b[i].ID)
		}
	}
}
