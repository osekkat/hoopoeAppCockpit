package swarmlaunch

import (
	"strings"
	"testing"
	"time"
)

func TestSequenceCatalogContainsAllTenSection73Stages(t *testing.T) {
	t.Parallel()
	want := []StageID{
		StageReconcileProjectState,
		StageVerifyLaunchGates,
		StageShowWarnings,
		StageCreateSwarmSpec,
		StageNTMSpawn,
		StageStaggerStarts,
		StageSendKickoffPrompt,
		StageStartEventSubs,
		StageActivateTendingJobs,
		StageShowSwarmDashboard,
	}
	got := SequenceCatalog()
	if len(got) != len(want) {
		t.Fatalf("sequence catalog length = %d, want %d", len(got), len(want))
	}
	for i, stage := range got {
		if stage.ID != want[i] {
			t.Errorf("catalog[%d].ID = %q, want %q (order must match plan.md §7.3)", i, stage.ID, want[i])
		}
	}
}

func TestSequenceNumbersAreSequentialFromOne(t *testing.T) {
	t.Parallel()
	for i, stage := range SequenceCatalog() {
		if stage.Number != i+1 {
			t.Errorf("stage[%d].Number = %d, want %d", i, stage.Number, i+1)
		}
	}
}

func TestEveryStageHasNonEmptyTitleDescription(t *testing.T) {
	t.Parallel()
	for _, stage := range SequenceCatalog() {
		if stage.Title == "" {
			t.Errorf("%s: Title is empty", stage.ID)
		}
		if stage.Description == "" {
			t.Errorf("%s: Description is empty", stage.ID)
		}
	}
}

func TestEveryStageIDIsSwarmlaunchNamespaced(t *testing.T) {
	t.Parallel()
	for _, stage := range SequenceCatalog() {
		if !strings.HasPrefix(string(stage.ID), "swarmlaunch.") {
			t.Errorf("%s: ID is missing the `swarmlaunch.` namespace prefix", stage.ID)
		}
	}
}

func TestOnlyShowWarningsAndDashboardAreNonBlocking(t *testing.T) {
	t.Parallel()
	nonBlocking := map[StageID]bool{
		StageShowWarnings:       true,
		StageShowSwarmDashboard: true,
	}
	for _, stage := range SequenceCatalog() {
		if bool(stage.Blocking) == nonBlocking[stage.ID] {
			t.Errorf("%s: Blocking = %v, want %v", stage.ID, bool(stage.Blocking), !nonBlocking[stage.ID])
		}
	}
}

func TestEveryStageHasAuditAction(t *testing.T) {
	t.Parallel()
	for _, stage := range SequenceCatalog() {
		if stage.AuditAction == "" {
			t.Errorf("%s: AuditAction is empty (Guardrail 10)", stage.ID)
		}
	}
}

func TestStageIDsAreUnique(t *testing.T) {
	t.Parallel()
	seen := make(map[StageID]bool, 10)
	for _, stage := range SequenceCatalog() {
		if seen[stage.ID] {
			t.Errorf("duplicate stage ID: %s", stage.ID)
		}
		seen[stage.ID] = true
	}
}

func TestLookupStageReturnsOkOnHitFalseOnMiss(t *testing.T) {
	t.Parallel()
	if _, ok := LookupStage(StageReconcileProjectState); !ok {
		t.Errorf("LookupStage must return true for a known ID")
	}
	if _, ok := LookupStage(StageID("does_not_exist")); ok {
		t.Errorf("LookupStage must return false for unknown ID")
	}
}

func TestDefaultStaggerIntervalIs30Seconds(t *testing.T) {
	t.Parallel()
	if DefaultStaggerInterval != 30*time.Second {
		t.Errorf("DefaultStaggerInterval = %s, want 30s (per §7.3)", DefaultStaggerInterval)
	}
}

func TestGateCatalogContainsAllSixSection42Gates(t *testing.T) {
	t.Parallel()
	want := []GateID{
		GateVPSReady,
		GateProjectImported,
		GatePlanLocked,
		GateBeadsFinalized,
		GateLaunchReady,
		GateBuildQueuePolicy,
	}
	got := GateCatalog()
	if len(got) != len(want) {
		t.Fatalf("gate catalog length = %d, want %d", len(got), len(want))
	}
	for i, gate := range got {
		if gate.ID != want[i] {
			t.Errorf("gates[%d].ID = %q, want %q", i, gate.ID, want[i])
		}
	}
}

func TestEveryGateHasBlockerProblemType(t *testing.T) {
	t.Parallel()
	for _, gate := range GateCatalog() {
		if gate.BlockerProblemType == "" {
			t.Errorf("%s: BlockerProblemType is empty (every gate failure must surface a typed Problem)", gate.ID)
		}
		if !strings.HasPrefix(gate.BlockerProblemType, "urn:hoopoe:") {
			t.Errorf("%s: BlockerProblemType = %q, must use urn:hoopoe: prefix", gate.ID, gate.BlockerProblemType)
		}
	}
}

func TestLookupGateReturnsOkOnHitFalseOnMiss(t *testing.T) {
	t.Parallel()
	if _, ok := LookupGate(GateVPSReady); !ok {
		t.Errorf("LookupGate must return true for a known ID")
	}
	if _, ok := LookupGate(GateID("does_not_exist")); ok {
		t.Errorf("LookupGate must return false for unknown ID")
	}
}

func TestWarningCatalogContainsAllSixSection73Warnings(t *testing.T) {
	t.Parallel()
	want := []WarningID{
		WarningDirtyGit,
		WarningStaleReservations,
		WarningNoReadyBeads,
		WarningLowDisk,
		WarningMissingAgentMail,
		WarningUnpushedVPSCommits,
	}
	got := WarningCatalog()
	if len(got) != len(want) {
		t.Fatalf("warning catalog length = %d, want %d", len(got), len(want))
	}
	for i, warning := range got {
		if warning.ID != want[i] {
			t.Errorf("warnings[%d].ID = %q, want %q", i, warning.ID, want[i])
		}
	}
}

func TestEveryWarningHasNonEmptyTitleAndDescription(t *testing.T) {
	t.Parallel()
	for _, warning := range WarningCatalog() {
		if warning.Title == "" {
			t.Errorf("%s: Title is empty", warning.ID)
		}
		if warning.Description == "" {
			t.Errorf("%s: Description is empty", warning.ID)
		}
	}
}

func TestLookupWarningReturnsOkOnHitFalseOnMiss(t *testing.T) {
	t.Parallel()
	if _, ok := LookupWarning(WarningDirtyGit); !ok {
		t.Errorf("LookupWarning must return true for a known ID")
	}
	if _, ok := LookupWarning(WarningID("does_not_exist")); ok {
		t.Errorf("LookupWarning must return false for unknown ID")
	}
}

func TestPolicyCatalogContainsAllTwelveSection73Directives(t *testing.T) {
	t.Parallel()
	got := PolicyCatalog()
	if len(got) != 12 {
		t.Fatalf("policy catalog length = %d, want 12 (the §7.3 default-launch-policy directives)", len(got))
	}
}

func TestPolicyIDsIncludeKeyDirectives(t *testing.T) {
	t.Parallel()
	must := map[PolicyID]bool{
		PolicyForceAgentsReadme:           false,
		PolicyRequireAgentMailRegistration: false,
		PolicyRequireBVTriageBeforeClaim:  false,
		PolicyReserveFilesBeforeEdits:     false,
		PolicyIncludeBeadIDInArtifacts:    false,
		PolicyUseRCHForBuilds:             false,
		PolicyNeverInvokeBareBV:           false,
	}
	for _, p := range PolicyCatalog() {
		if _, want := must[p.ID]; want {
			must[p.ID] = true
		}
	}
	for id, found := range must {
		if !found {
			t.Errorf("policy directive %s missing from catalog", id)
		}
	}
}

func TestEveryPolicyHasNonEmptyTitleAndDescription(t *testing.T) {
	t.Parallel()
	for _, p := range PolicyCatalog() {
		if p.Title == "" {
			t.Errorf("%s: Title is empty", p.ID)
		}
		if p.Description == "" {
			t.Errorf("%s: Description is empty", p.ID)
		}
	}
}

func TestNeverInvokeBareBVPolicyReferencesGuardrail(t *testing.T) {
	t.Parallel()
	policy, ok := LookupPolicy(PolicyNeverInvokeBareBV)
	if !ok {
		t.Fatal("never_invoke_bare_bv policy missing")
	}
	if !strings.Contains(policy.Description, "Guardrail 1") &&
		!strings.Contains(policy.Description, "TUI") {
		t.Errorf("never_invoke_bare_bv description should reference Guardrail 1 / TUI: %q", policy.Description)
	}
}

func TestAgentMailPolicyReferencesGuardrail8(t *testing.T) {
	t.Parallel()
	policy, ok := LookupPolicy(PolicyRequireAgentMailRegistration)
	if !ok {
		t.Fatal("require_agent_mail_registration policy missing")
	}
	if !strings.Contains(policy.Description, "Guardrail 8") &&
		!strings.Contains(policy.Description, "coordinate-by-pane") {
		t.Errorf("agent-mail policy should reference Guardrail 8 / coordinate-by-pane forbidden: %q", policy.Description)
	}
}

func TestPolicyIDsAreUnique(t *testing.T) {
	t.Parallel()
	seen := make(map[PolicyID]bool, 12)
	for _, p := range PolicyCatalog() {
		if seen[p.ID] {
			t.Errorf("duplicate policy ID: %s", p.ID)
		}
		seen[p.ID] = true
	}
}

func TestLookupPolicyReturnsOkOnHitFalseOnMiss(t *testing.T) {
	t.Parallel()
	if _, ok := LookupPolicy(PolicyForceAgentsReadme); !ok {
		t.Errorf("LookupPolicy must return true for a known ID")
	}
	if _, ok := LookupPolicy(PolicyID("does_not_exist")); ok {
		t.Errorf("LookupPolicy must return false for unknown ID")
	}
}

func TestSequenceCatalogIsImmutableAcrossCalls(t *testing.T) {
	t.Parallel()
	a := SequenceCatalog()
	b := SequenceCatalog()
	if len(a) != len(b) {
		t.Fatalf("sequence catalog length differs across calls")
	}
	for i := range a {
		if a[i].ID != b[i].ID {
			t.Errorf("sequence[%d] differs across calls", i)
		}
	}
}

func TestGateCatalogIsImmutableAcrossCalls(t *testing.T) {
	t.Parallel()
	a := GateCatalog()
	b := GateCatalog()
	if len(a) != len(b) {
		t.Fatalf("gate catalog length differs across calls")
	}
}
