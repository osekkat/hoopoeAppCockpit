package gates

import (
	"errors"
	"reflect"
	"testing"
	"time"

	schemas "github.com/hoopoe-cockpit/hoopoe/packages/schemas/go"
)

var fixedTime = time.Date(2026, 5, 4, 12, 0, 0, 0, time.UTC)

func TestDefinitionsCoverCanonicalGates(t *testing.T) {
	definitions := Definitions()
	if len(definitions) != 8 {
		t.Fatalf("definition count = %d, want 8", len(definitions))
	}
	seen := map[schemas.ProjectGate]bool{}
	for _, definition := range definitions {
		if !definition.Gate.Valid() {
			t.Fatalf("definition has invalid gate %q", definition.Gate)
		}
		if len(definition.CheckIDs) == 0 {
			t.Fatalf("definition %s has no checks", definition.Gate)
		}
		seen[definition.Gate] = true
	}
	for _, gate := range CanonicalOrder() {
		if !seen[gate] {
			t.Fatalf("canonical gate %s missing from definitions", gate)
		}
	}
}

func TestEvaluateProjectImportedReportsMissingPreconditions(t *testing.T) {
	snapshot := completeSnapshot()
	snapshot.Project.AgentsMDPresent = false
	snapshot.Project.ToolDetectionDone = false

	readiness, err := Evaluate(snapshot, schemas.ProjectGateProjectImported)
	if err != nil {
		t.Fatalf("evaluate readiness: %v", err)
	}
	if len(readiness.Gates) != 1 {
		t.Fatalf("gate count = %d, want 1", len(readiness.Gates))
	}
	gate := readiness.Gates[0]
	if gate.Satisfied {
		t.Fatalf("project imported gate satisfied, want blocked")
	}
	if gate.BlockingCount == nil || *gate.BlockingCount != 2 {
		t.Fatalf("blocking count = %v, want 2", gate.BlockingCount)
	}
	missing := missingCheckSet(gate)
	for _, id := range []string{"agents.md", "tools.detected"} {
		if !missing[id] {
			t.Fatalf("missing checks = %+v, want %s", missing, id)
		}
	}
}

func TestLaunchReadyAllowsIntentionalScope(t *testing.T) {
	snapshot := completeSnapshot()
	snapshot.Launch.BRReadyNonempty = false
	snapshot.Launch.BRReadyCount = 0
	snapshot.Launch.IntentionallyScoped = true

	gate, err := EvaluateGate(snapshot, schemas.ProjectGateLaunchReady)
	if err != nil {
		t.Fatalf("evaluate gate: %v", err)
	}
	if !gate.Satisfied {
		t.Fatalf("launch gate = %+v, want satisfied by intentional scope", gate)
	}
}

func TestShipReadyAllowsDocumentedExceptionsAndFollowUps(t *testing.T) {
	snapshot := completeSnapshot()
	snapshot.Ship.TestsBuildsPass = false
	snapshot.Ship.TestBuildExceptions = []string{"accepted flaky external service"}
	snapshot.Ship.CodeHealthPass = false
	snapshot.Ship.FollowUpBeads = []string{"hp-follow"}

	gate, err := EvaluateGate(snapshot, schemas.ProjectGateShipReady)
	if err != nil {
		t.Fatalf("evaluate gate: %v", err)
	}
	if !gate.Satisfied {
		t.Fatalf("ship gate = %+v, want satisfied by exceptions and follow-ups", gate)
	}
}

func TestEvaluateUnknownGateFails(t *testing.T) {
	_, err := Evaluate(completeSnapshot(), schemas.ProjectGate("unknown_gate"))
	if !errors.Is(err, ErrUnknownGate) {
		t.Fatalf("err = %v, want ErrUnknownGate", err)
	}
}

func TestRequiredGatesForTransition(t *testing.T) {
	required, ok := RequiredGates(schemas.ProjectLifecycleStateBeadsFinalized, schemas.ProjectLifecycleStateSwarmRunning)
	if !ok {
		t.Fatalf("expected transition to be known")
	}
	want := []schemas.ProjectGate{schemas.ProjectGateVpsReady, schemas.ProjectGateLaunchReady}
	if !reflect.DeepEqual(required, want) {
		t.Fatalf("required gates = %+v, want %+v", required, want)
	}
	required[0] = schemas.ProjectGateShipReady
	again, _ := RequiredGates(schemas.ProjectLifecycleStateBeadsFinalized, schemas.ProjectLifecycleStateSwarmRunning)
	if !reflect.DeepEqual(again, want) {
		t.Fatalf("required gates mutated through caller slice: %+v", again)
	}
}

func TestTransitionRejectsBlockedGateWithProblemAndAudit(t *testing.T) {
	snapshot := completeSnapshot()
	snapshot.VPS.SSHVerified = false
	request := transitionRequest(schemas.ProjectLifecycleStateImported, schemas.ProjectLifecycleStatePlanning)

	decision, err := EvaluateTransition(snapshot, request)
	if err != nil {
		t.Fatalf("evaluate transition: %v", err)
	}
	if decision.Allowed {
		t.Fatalf("transition allowed, want blocked")
	}
	if decision.Problem == nil || decision.Problem.Status != 422 || decision.Problem.Code != "project.gate_blocked" {
		t.Fatalf("problem = %+v, want 422 gate blocked", decision.Problem)
	}
	if decision.BlockingGate == nil || decision.BlockingGate.Gate != schemas.ProjectGateVpsReady {
		t.Fatalf("blocking gate = %+v, want vps_ready", decision.BlockingGate)
	}
	if decision.AuditEvent.Action != ActionTransitionRejected || decision.AuditEvent.Result != "failure" {
		t.Fatalf("audit event = %+v, want rejected failure", decision.AuditEvent)
	}
	if !reflect.DeepEqual(decision.AuditEvent.MissingCheckIDs, []string{"vps.ssh_verified"}) {
		t.Fatalf("missing check IDs = %+v, want vps.ssh_verified", decision.AuditEvent.MissingCheckIDs)
	}
}

func TestTransitionRejectsLifecycleConflict(t *testing.T) {
	snapshot := completeSnapshot()
	snapshot.CurrentState = schemas.ProjectLifecycleStatePlanning
	request := transitionRequest(schemas.ProjectLifecycleStateImported, schemas.ProjectLifecycleStatePlanning)

	decision, err := EvaluateTransition(snapshot, request)
	if err != nil {
		t.Fatalf("evaluate transition: %v", err)
	}
	if decision.Allowed {
		t.Fatalf("transition allowed, want conflict")
	}
	if decision.Problem == nil || decision.Problem.Status != 409 || decision.Problem.Code != "project.lifecycle_conflict" {
		t.Fatalf("problem = %+v, want lifecycle conflict", decision.Problem)
	}
	if decision.AuditEvent.Action != ActionTransitionRejected {
		t.Fatalf("audit action = %s, want rejected", decision.AuditEvent.Action)
	}
}

func TestTransitionAllowedEmitsAuditReadyEvent(t *testing.T) {
	snapshot := completeSnapshot()
	request := transitionRequest(schemas.ProjectLifecycleStateImported, schemas.ProjectLifecycleStatePlanning)

	decision, err := EvaluateTransition(snapshot, request)
	if err != nil {
		t.Fatalf("evaluate transition: %v", err)
	}
	if !decision.Allowed {
		t.Fatalf("transition blocked: problem=%+v", decision.Problem)
	}
	if decision.Problem != nil || decision.BlockingGate != nil {
		t.Fatalf("unexpected blocked fields: problem=%+v blocking=%+v", decision.Problem, decision.BlockingGate)
	}
	if !reflect.DeepEqual(decision.RequiredGates, []schemas.ProjectGate{schemas.ProjectGateVpsReady, schemas.ProjectGateProjectImported}) {
		t.Fatalf("required gates = %+v", decision.RequiredGates)
	}
	if len(decision.Readiness.Gates) != 2 {
		t.Fatalf("readiness gate count = %d, want 2", len(decision.Readiness.Gates))
	}
	if decision.AuditEvent.Action != ActionTransitionAllowed || decision.AuditEvent.Result != "success" {
		t.Fatalf("audit event = %+v, want allowed success", decision.AuditEvent)
	}
	if decision.AuditEvent.EventID != "corr-1" || decision.AuditEvent.CorrelationID != "corr-1" {
		t.Fatalf("audit IDs = %s/%s, want corr-1", decision.AuditEvent.EventID, decision.AuditEvent.CorrelationID)
	}
}

func completeSnapshot() Snapshot {
	return Snapshot{
		ProjectID:    "proj_123",
		CurrentState: schemas.ProjectLifecycleStateImported,
		CheckedAt:    fixedTime,
		VPS: VPSFacts{
			SSHVerified:          true,
			DaemonReachable:      true,
			ACFSInstalled:        true,
			ToolVersionsRecorded: true,
			ToolVersions:         map[string]string{"br": "1.0.0"},
		},
		Project: ProjectFacts{
			GitRepoPresent:    true,
			Branch:            "main",
			AgentsMDPresent:   true,
			HoopoeInitialized: true,
			ToolDetectionDone: true,
		},
		Plan: PlanFacts{
			PlanID:                      "plan_1",
			Locked:                      true,
			SelfContained:               true,
			DecisionsExplicit:           true,
			TestingStrategyPresent:      true,
			UnresolvedDecisionsAccepted: true,
		},
		BeadsCreated: BeadsCreatedFacts{
			LinkedPlanBeads:          12,
			IssuesJSONLFlushed:       true,
			ConversionArtifactsSaved: true,
			ArtifactIDs:              []string{"conversion.json"},
		},
		BeadsFinalized: BeadsFinalizedFacts{
			CoverageChecked:       true,
			DependenciesChecked:   true,
			ReadySetSufficient:    true,
			ReadyCount:            4,
			ClarityAcceptable:     true,
			TestabilityAcceptable: true,
		},
		Launch: LaunchFacts{
			NTMHealthy:          true,
			AgentMailHealthy:    true,
			BVRobotHealthy:      true,
			BRReadyNonempty:     true,
			BRReadyCount:        3,
			BuildQueuePolicySet: true,
		},
		Hardening: HardeningFacts{
			ImplementationClosedOrDeferred: true,
			NoStuckInProgress:              true,
			ReviewPromptsAvailable:         true,
		},
		Ship: ShipFacts{
			TestsBuildsPass: true,
			CodeHealthPass:  true,
			GitSynced:       true,
			BeadsSynced:     true,
		},
	}
}

func transitionRequest(from, to schemas.ProjectLifecycleState) TransitionRequest {
	actorID := "agent-test"
	return TransitionRequest{
		ProjectID:     "proj_123",
		From:          from,
		To:            to,
		Actor:         schemas.Actor{Kind: schemas.ActorKind("agent"), Id: &actorID},
		Reason:        "test transition",
		CorrelationID: "corr-1",
		At:            fixedTime,
	}
}

func missingCheckSet(gate schemas.ProjectReadinessGate) map[string]bool {
	missing := map[string]bool{}
	for _, check := range gate.Checks {
		if !check.Ok {
			missing[check.Id] = true
		}
	}
	return missing
}
