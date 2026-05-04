package flip

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/hoopoe-cockpit/hoopoe/apps/daemon/internal/projects/gates"
	schemas "github.com/hoopoe-cockpit/hoopoe/packages/schemas/go"
)

var testNow = time.Date(2026, 5, 4, 12, 0, 0, 0, time.UTC)

func TestBuildPlanCompletesFlipWithSnapshotTendingUIAndAuditCorrelation(t *testing.T) {
	t.Parallel()
	req := baseRequest(t.TempDir())
	req.Drain = DrainInput{
		GraceStartedAt: testNow.Add(-9 * time.Minute),
		Now:            testNow,
		InFlight: []InFlightBead{
			{BeadID: "hp-carry", AgentID: "agent-1"},
			{BeadID: "hp-defer", AgentID: "agent-2"},
		},
		Resolutions: map[string]DrainResolution{
			"hp-carry": ResolutionCarryToHardening,
			"hp-defer": ResolutionDefer,
		},
	}
	req.TendingJobs = []TendingJob{
		{ID: "drift-check", Paused: false, Cadence: 30 * time.Minute, Mode: "implementation"},
		{ID: "tend-swarm", Paused: false, Cadence: 4 * time.Minute, Mode: "implementation"},
	}

	plan, err := BuildPlan(req)
	if err != nil {
		t.Fatalf("BuildPlan: %v", err)
	}
	if plan.State != StateCompleted {
		t.Fatalf("state = %q, want completed", plan.State)
	}
	if !plan.Gate.Satisfied || plan.Gate.OverrideAccepted {
		t.Fatalf("gate = %+v", plan.Gate)
	}
	if plan.Drain.CarryCount != 1 || plan.Drain.DeferCount != 1 || plan.Drain.State != DrainComplete {
		t.Fatalf("drain = %+v", plan.Drain)
	}
	if plan.Snapshot == nil {
		t.Fatalf("snapshot manifest missing")
	}
	if _, err := os.Stat(plan.Snapshot.Path); err != nil {
		t.Fatalf("snapshot file missing: %v", err)
	}
	if _, err := VerifySnapshot(plan.Snapshot.Path, plan.Snapshot.SHA256); err != nil {
		t.Fatalf("VerifySnapshot: %v", err)
	}
	if plan.HardeningSwarmID != "hardening-flip-1" || !plan.Hardening.Round0AutoStart || !plan.Hardening.FindingLedgerInitialized {
		t.Fatalf("hardening spec = %+v", plan.Hardening)
	}
	if plan.UI == nil || plan.UI.Route != "/projects/proj_1/hardening" || !plan.UI.ConvergenceMounted {
		t.Fatalf("ui = %+v", plan.UI)
	}
	if len(plan.Tending.Patches) != 5 {
		t.Fatalf("tending patches = %+v, want pause + 3 creates + retune", plan.Tending.Patches)
	}
	if len(plan.Audit) != 2 {
		t.Fatalf("audit events = %+v", plan.Audit)
	}
	for _, event := range plan.Audit {
		if event.FlipID != "flip-1" || event.CorrelationID != "flip-1" || event.ImplementationSwarmID != "sw_impl" {
			t.Fatalf("audit event not correlated: %+v", event)
		}
	}
	if plan.ReversibleUntil == nil || !plan.ReversibleUntil.Equal(testNow.Add(DefaultReversibilityWindow)) {
		t.Fatalf("reversibleUntil = %v", plan.ReversibleUntil)
	}
}

func TestGateRefusalReturnsProblemWithoutLaunching(t *testing.T) {
	t.Parallel()
	req := baseRequest("")
	req.GateSnapshot.Hardening.NoStuckInProgress = false
	req.GateSnapshot.Hardening.StuckCount = 1

	plan, err := BuildPlan(req)
	if err != nil {
		t.Fatalf("BuildPlan: %v", err)
	}
	if plan.State != StateRefused {
		t.Fatalf("state = %q, want refused", plan.State)
	}
	if plan.Problem == nil || plan.Problem.Status != 422 || plan.Problem.Code != "project.gate_blocked" {
		t.Fatalf("problem = %+v", plan.Problem)
	}
	if !contains(plan.Gate.MissingCheckIDs, "beads.no_stuck_in_progress") {
		t.Fatalf("missing checks = %+v", plan.Gate.MissingCheckIDs)
	}
	if plan.CompletedAt != nil || plan.HardeningSwarmID != "" || plan.UI != nil {
		t.Fatalf("refused plan launched side effects: %+v", plan)
	}
}

func TestOverrideApprovalAllowsBlockedGateToProceed(t *testing.T) {
	t.Parallel()
	req := baseRequest("")
	req.GateSnapshot.Hardening.NoStuckInProgress = false
	req.GateSnapshot.Hardening.StuckCount = 1
	req.Override = &OverrideApproval{
		ApprovalID: "appr_1",
		Approved:   true,
		Reason:     "stuck bead will be carried into hardening",
		Actor:      actor("owner"),
		DecidedAt:  testNow.Add(-time.Minute),
	}

	plan, err := BuildPlan(req)
	if err != nil {
		t.Fatalf("BuildPlan: %v", err)
	}
	if plan.State != StateCompleted {
		t.Fatalf("state = %q, want completed", plan.State)
	}
	if !plan.Gate.OverrideAccepted || plan.Gate.ApprovalID != "appr_1" {
		t.Fatalf("gate = %+v", plan.Gate)
	}
}

func TestDrainGraceExpirySurfacesCarryOrDeferAndResolvesWhenChosen(t *testing.T) {
	t.Parallel()
	input := DrainInput{
		GraceStartedAt: testNow.Add(-DefaultDrainGrace),
		Now:            testNow.Add(time.Second),
		InFlight: []InFlightBead{
			{BeadID: "hp-1", AgentID: "agent-1"},
			{BeadID: "hp-2", AgentID: "agent-2"},
		},
		Resolutions: map[string]DrainResolution{
			"hp-1": ResolutionCarryToHardening,
		},
	}
	report, err := EvaluateDrain(input, testNow)
	if err != nil {
		t.Fatalf("EvaluateDrain: %v", err)
	}
	if report.State != DrainAwaitingResolution || report.UnresolvedCount != 1 {
		t.Fatalf("report = %+v, want one unresolved choice", report)
	}
	if len(report.RequiredChoices) != 1 || report.RequiredChoices[0].BeadID != "hp-2" {
		t.Fatalf("choices = %+v", report.RequiredChoices)
	}

	input.Resolutions["hp-2"] = ResolutionDefer
	report, err = EvaluateDrain(input, testNow)
	if err != nil {
		t.Fatalf("resolved EvaluateDrain: %v", err)
	}
	if report.State != DrainComplete || report.CarryCount != 1 || report.DeferCount != 1 || report.UnresolvedCount != 0 {
		t.Fatalf("resolved report = %+v", report)
	}
}

func TestSnapshotRoundTripAndDigestVerification(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	snapshot := sampleSnapshot()
	manifest, err := WriteImplementationSnapshot(root, snapshot, testNow)
	if err != nil {
		t.Fatalf("WriteImplementationSnapshot: %v", err)
	}
	read, readManifest, err := ReadImplementationSnapshot(manifest.Path)
	if err != nil {
		t.Fatalf("ReadImplementationSnapshot: %v", err)
	}
	if !reflect.DeepEqual(read.Beads, snapshot.Beads) || !reflect.DeepEqual(read.Agents, snapshot.Agents) {
		t.Fatalf("snapshot round trip mismatch: %+v", read)
	}
	if readManifest.SHA256 != manifest.SHA256 || readManifest.SizeBytes != manifest.SizeBytes {
		t.Fatalf("manifest round trip = %+v want %+v", readManifest, manifest)
	}
	if _, err := VerifySnapshot(manifest.Path, "bad"); !errors.Is(err, ErrSnapshotMismatch) {
		t.Fatalf("VerifySnapshot bad digest err = %v, want ErrSnapshotMismatch", err)
	}
}

func TestTendingSwitchIsIdempotent(t *testing.T) {
	t.Parallel()
	current := []TendingJob{
		{ID: "drift-check", Paused: false, Cadence: 30 * time.Minute},
		{ID: "tend-swarm", Paused: false, Cadence: 4 * time.Minute},
	}
	first, err := SwitchTendingJobs(current, DefaultTendingSwitchPlan())
	if err != nil {
		t.Fatalf("first SwitchTendingJobs: %v", err)
	}
	if len(first.Patches) != 5 {
		t.Fatalf("first patches = %+v", first.Patches)
	}
	second, err := SwitchTendingJobs(first.Jobs, DefaultTendingSwitchPlan())
	if err != nil {
		t.Fatalf("second SwitchTendingJobs: %v", err)
	}
	if len(second.Patches) != 0 {
		t.Fatalf("second patches = %+v, want idempotent no-op", second.Patches)
	}
	if second.IdempotencyKey != first.IdempotencyKey {
		t.Fatalf("idempotency key changed: %s -> %s", first.IdempotencyKey, second.IdempotencyKey)
	}
}

func TestUIFlipEventsCoalesceToLatestHardeningState(t *testing.T) {
	t.Parallel()
	older := BuildUIFlipEvent("proj_1", "flip-1", "sw_impl", "sw_hard_1", testNow)
	newer := BuildUIFlipEvent("proj_1", "flip-2", "sw_impl", "sw_hard_2", testNow.Add(time.Second))
	state := CoalesceUIFlipEvents([]UIFlipEvent{newer, older})
	if state.AppliedEventID != newer.EventID || state.Route != "/projects/proj_1/hardening" || state.SwarmMode != "hardening" {
		t.Fatalf("state = %+v, want newer hardening event", state)
	}
}

func TestReversibilityWindowAllowsThenGatesReturn(t *testing.T) {
	t.Parallel()
	req := baseRequest("")
	plan, err := BuildPlan(req)
	if err != nil {
		t.Fatalf("BuildPlan: %v", err)
	}
	within := EvaluateReturn(plan, testNow.Add(time.Hour), []string{"hp-new", "hp-new"})
	if !within.Allowed || within.RequiresApproval || !reflect.DeepEqual(within.CarryBackBeads, []string{"hp-new"}) {
		t.Fatalf("within = %+v", within)
	}
	after := EvaluateReturn(plan, testNow.Add(25*time.Hour), nil)
	if after.Allowed || !after.RequiresApproval || after.ApprovalAction == nil {
		t.Fatalf("after = %+v, want approval required", after)
	}
	if after.ApprovalAction.Kind != "swarm.return_to_implementation" {
		t.Fatalf("approval action = %+v", after.ApprovalAction)
	}
}

func baseRequest(snapshotRoot string) Request {
	return Request{
		FlipID:                "flip-1",
		ProjectID:             "proj_1",
		ImplementationSwarmID: "sw_impl",
		Actor:                 actor("owner"),
		GateSnapshot:          readyGateSnapshot(),
		Drain: DrainInput{
			GraceStartedAt: testNow.Add(-time.Minute),
			Now:            testNow,
		},
		Snapshot:     sampleSnapshot(),
		SnapshotRoot: snapshotRoot,
		Now:          testNow,
	}
}

func actor(id string) schemas.Actor {
	return schemas.Actor{Kind: schemas.ActorKindUser, Id: stringPtr(id)}
}

func readyGateSnapshot() gates.Snapshot {
	return gates.Snapshot{
		ProjectID:    "proj_1",
		CurrentState: schemas.ProjectLifecycleStateSwarmRunning,
		CheckedAt:    testNow,
		VPS: gates.VPSFacts{
			SSHVerified:          true,
			DaemonReachable:      true,
			ACFSInstalled:        true,
			ToolVersionsRecorded: true,
			ToolVersions:         map[string]string{"br": "1.0.0"},
		},
		Hardening: gates.HardeningFacts{
			ImplementationClosedOrDeferred: true,
			NoStuckInProgress:              true,
			ReviewPromptsAvailable:         true,
		},
	}
}

func sampleSnapshot() ImplementationSnapshot {
	return ImplementationSnapshot{
		SchemaVersion:         SchemaVersion,
		FlipID:                "flip-1",
		ProjectID:             "proj_1",
		ImplementationSwarmID: "sw_impl",
		CapturedAt:            testNow,
		Beads: map[string]BeadSnapshot{
			"hp-1": {Status: "closed", AssignedAgentID: "agent-1", UpdatedAt: testNow.Add(-time.Minute)},
			"hp-2": {Status: "in_progress", AssignedAgentID: "agent-2", UpdatedAt: testNow.Add(-2 * time.Minute)},
		},
		Agents: map[string]AgentSnapshot{
			"agent-1": {LastClaimedBeadID: "hp-1", FinalStatus: "completed", LastActivityAt: testNow.Add(-time.Minute)},
			"agent-2": {LastClaimedBeadID: "hp-2", FinalStatus: "carried", LastActivityAt: testNow.Add(-2 * time.Minute)},
		},
		MailThreadIndexHashes: map[string]string{"hoopoe-phase2": "sha256:mail"},
		BuildQueue:            BuildQueueSnapshot{Running: 1, Queued: 2, JobIDs: []string{"build-2", "build-1"}},
		PushStatus: map[string]BranchPushStatus{
			"main": {Branch: "main", LastPushedSHA: "abc123", PushedAt: testNow.Add(-3 * time.Minute), Clean: true},
		},
		AuditRange: AuditSequenceRange{FirstSequence: 10, LastSequence: 42},
		Metadata:   map[string]string{"source": "test"},
	}
}

func stringPtr(value string) *string {
	return &value
}

func contains(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

func TestSnapshotPathShape(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	manifest, err := WriteImplementationSnapshot(root, sampleSnapshot(), testNow)
	if err != nil {
		t.Fatalf("WriteImplementationSnapshot: %v", err)
	}
	wantSuffix := filepath.Join("proj_1", "hardening-snapshots", "flip-1", SnapshotFileName)
	if filepath.ToSlash(manifest.Path[len(root)+1:]) != filepath.ToSlash(wantSuffix) {
		t.Fatalf("path = %s, want suffix %s", manifest.Path, wantSuffix)
	}
}
