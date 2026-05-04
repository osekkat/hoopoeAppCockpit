// Package gates owns the deterministic project gate state machine from plan
// section 4.2.
//
// The package is intentionally adapter-free. Callers pass facts collected from
// canonical tool surfaces, then gates returns schema-compatible readiness and
// audit-ready transition decisions.
package gates

import (
	"errors"
	"fmt"
	"strings"
	"time"

	schemas "github.com/hoopoe-cockpit/hoopoe/packages/schemas/go"
)

const (
	SchemaVersion = 1

	ActionTransitionAllowed  = "project.lifecycle_transition_allowed"
	ActionTransitionRejected = "project.lifecycle_transition_rejected"
)

var (
	ErrInvalidInput      = errors.New("gates: invalid input")
	ErrUnknownGate       = errors.New("gates: unknown gate")
	ErrInvalidTransition = errors.New("gates: invalid transition")
)

type VPSFacts struct {
	SSHVerified          bool              `json:"sshVerified"`
	DaemonReachable      bool              `json:"daemonReachable"`
	ACFSInstalled        bool              `json:"acfsInstalled"`
	ACFSSkipped          bool              `json:"acfsSkipped"`
	ToolVersions         map[string]string `json:"toolVersions,omitempty"`
	ToolVersionsRecorded bool              `json:"toolVersionsRecorded"`
}

type ProjectFacts struct {
	GitRepoPresent    bool   `json:"gitRepoPresent"`
	Branch            string `json:"branch,omitempty"`
	BranchKnown       bool   `json:"branchKnown"`
	AgentsMDPresent   bool   `json:"agentsMdPresent"`
	HoopoeInitialized bool   `json:"hoopoeInitialized"`
	ToolDetectionDone bool   `json:"toolDetectionDone"`
}

type PlanFacts struct {
	PlanID                      string `json:"planId,omitempty"`
	Locked                      bool   `json:"locked"`
	SelfContained               bool   `json:"selfContained"`
	DecisionsExplicit           bool   `json:"decisionsExplicit"`
	TestingStrategyPresent      bool   `json:"testingStrategyPresent"`
	UnresolvedDecisionsAccepted bool   `json:"unresolvedDecisionsAccepted"`
}

type BeadsCreatedFacts struct {
	LinkedPlanBeads          int      `json:"linkedPlanBeads"`
	IssuesJSONLFlushed       bool     `json:"issuesJsonlFlushed"`
	ConversionArtifactsSaved bool     `json:"conversionArtifactsSaved"`
	ArtifactIDs              []string `json:"artifactIds,omitempty"`
}

type BeadsFinalizedFacts struct {
	CoverageChecked       bool `json:"coverageChecked"`
	DependenciesChecked   bool `json:"dependenciesChecked"`
	ReadySetSufficient    bool `json:"readySetSufficient"`
	ReadyCount            int  `json:"readyCount"`
	ClarityAcceptable     bool `json:"clarityAcceptable"`
	TestabilityAcceptable bool `json:"testabilityAcceptable"`
}

type LaunchFacts struct {
	NTMHealthy          bool `json:"ntmHealthy"`
	AgentMailHealthy    bool `json:"agentMailHealthy"`
	BVRobotHealthy      bool `json:"bvRobotHealthy"`
	BRReadyNonempty     bool `json:"brReadyNonempty"`
	BRReadyCount        int  `json:"brReadyCount"`
	IntentionallyScoped bool `json:"intentionallyScoped"`
	BuildQueuePolicySet bool `json:"buildQueuePolicySet"`
}

type HardeningFacts struct {
	ImplementationClosedOrDeferred bool `json:"implementationClosedOrDeferred"`
	NoStuckInProgress              bool `json:"noStuckInProgress"`
	InProgressCount                int  `json:"inProgressCount"`
	StuckCount                     int  `json:"stuckCount"`
	ReviewPromptsAvailable         bool `json:"reviewPromptsAvailable"`
}

type ShipFacts struct {
	TestsBuildsPass     bool     `json:"testsBuildsPass"`
	TestBuildExceptions []string `json:"testBuildExceptions,omitempty"`
	CodeHealthPass      bool     `json:"codeHealthPass"`
	FollowUpBeads       []string `json:"followUpBeads,omitempty"`
	GitSynced           bool     `json:"gitSynced"`
	BeadsSynced         bool     `json:"beadsSynced"`
}

type Snapshot struct {
	ProjectID    string                        `json:"projectId"`
	CurrentState schemas.ProjectLifecycleState `json:"currentState,omitempty"`
	CheckedAt    time.Time                     `json:"checkedAt,omitempty"`

	VPS            VPSFacts            `json:"vps"`
	Project        ProjectFacts        `json:"project"`
	Plan           PlanFacts           `json:"plan"`
	BeadsCreated   BeadsCreatedFacts   `json:"beadsCreated"`
	BeadsFinalized BeadsFinalizedFacts `json:"beadsFinalized"`
	Launch         LaunchFacts         `json:"launch"`
	Hardening      HardeningFacts      `json:"hardening"`
	Ship           ShipFacts           `json:"ship"`
}

type Definition struct {
	Gate     schemas.ProjectGate `json:"gate"`
	Label    string              `json:"label"`
	CheckIDs []string            `json:"checkIds"`
}

type checkDefinition struct {
	id     string
	detail string
	ok     func(Snapshot) bool
}

type gateDefinition struct {
	gate   schemas.ProjectGate
	label  string
	checks []checkDefinition
}

type TransitionDefinition struct {
	From          schemas.ProjectLifecycleState `json:"from"`
	To            schemas.ProjectLifecycleState `json:"to"`
	RequiredGates []schemas.ProjectGate         `json:"requiredGates"`
}

type TransitionRequest struct {
	ProjectID     string                        `json:"projectId,omitempty"`
	From          schemas.ProjectLifecycleState `json:"from"`
	To            schemas.ProjectLifecycleState `json:"to"`
	Actor         schemas.Actor                 `json:"actor"`
	Reason        string                        `json:"reason,omitempty"`
	CorrelationID string                        `json:"correlationId,omitempty"`
	CausationID   string                        `json:"causationId,omitempty"`
	At            time.Time                     `json:"at,omitempty"`
}

type TransitionDecision struct {
	Allowed       bool                          `json:"allowed"`
	RequiredGates []schemas.ProjectGate         `json:"requiredGates"`
	Readiness     schemas.ProjectReadiness      `json:"readiness"`
	BlockingGate  *schemas.ProjectReadinessGate `json:"blockingGate,omitempty"`
	Problem       *schemas.Problem              `json:"problem,omitempty"`
	AuditEvent    TransitionAuditEvent          `json:"auditEvent"`
}

type TransitionAuditEvent struct {
	SchemaVersion   int                           `json:"schemaVersion"`
	EventID         string                        `json:"eventId"`
	ProjectID       string                        `json:"projectId"`
	Action          string                        `json:"action"`
	Result          string                        `json:"result"`
	Actor           schemas.Actor                 `json:"actor"`
	From            schemas.ProjectLifecycleState `json:"from"`
	To              schemas.ProjectLifecycleState `json:"to"`
	RequiredGates   []schemas.ProjectGate         `json:"requiredGates,omitempty"`
	BlockingGate    *schemas.ProjectGate          `json:"blockingGate,omitempty"`
	MissingCheckIDs []string                      `json:"missingCheckIds,omitempty"`
	Reason          string                        `json:"reason,omitempty"`
	CorrelationID   string                        `json:"correlationId,omitempty"`
	CausationID     string                        `json:"causationId,omitempty"`
	At              time.Time                     `json:"at"`
}

func CanonicalOrder() []schemas.ProjectGate {
	return append([]schemas.ProjectGate(nil), canonicalGateOrder...)
}

func Definitions() []Definition {
	out := make([]Definition, 0, len(gateDefinitions))
	for _, def := range gateDefinitions {
		checkIDs := make([]string, 0, len(def.checks))
		for _, check := range def.checks {
			checkIDs = append(checkIDs, check.id)
		}
		out = append(out, Definition{
			Gate:     def.gate,
			Label:    def.label,
			CheckIDs: checkIDs,
		})
	}
	return out
}

func TransitionDefinitions() []TransitionDefinition {
	out := make([]TransitionDefinition, 0, len(transitionDefinitions))
	for _, def := range transitionDefinitions {
		required := append([]schemas.ProjectGate(nil), def.RequiredGates...)
		out = append(out, TransitionDefinition{
			From:          def.From,
			To:            def.To,
			RequiredGates: required,
		})
	}
	return out
}

func RequiredGates(from, to schemas.ProjectLifecycleState) ([]schemas.ProjectGate, bool) {
	for _, def := range transitionDefinitions {
		if def.From == from && def.To == to {
			return append([]schemas.ProjectGate(nil), def.RequiredGates...), true
		}
	}
	return nil, false
}

func Evaluate(snapshot Snapshot, filters ...schemas.ProjectGate) (schemas.ProjectReadiness, error) {
	projectID := strings.TrimSpace(snapshot.ProjectID)
	if projectID == "" {
		return schemas.ProjectReadiness{}, fmt.Errorf("%w: projectId is required", ErrInvalidInput)
	}
	selected, err := selectedGates(filters)
	if err != nil {
		return schemas.ProjectReadiness{}, err
	}
	checkedAt := snapshot.CheckedAt
	if checkedAt.IsZero() {
		checkedAt = time.Now()
	}
	var currentState *schemas.ProjectLifecycleState
	if snapshot.CurrentState.Valid() {
		state := snapshot.CurrentState
		currentState = &state
	}
	readiness := schemas.ProjectReadiness{
		SchemaVersion:         SchemaVersion,
		ProjectId:             projectID,
		CheckedAt:             checkedAt.UTC(),
		CurrentLifecycleState: currentState,
		Gates:                 make([]schemas.ProjectReadinessGate, 0, len(canonicalGateOrder)),
	}
	for _, gate := range canonicalGateOrder {
		if selected != nil && !selected[gate] {
			continue
		}
		evaluated, err := EvaluateGate(snapshot, gate)
		if err != nil {
			return schemas.ProjectReadiness{}, err
		}
		readiness.Gates = append(readiness.Gates, evaluated)
	}
	return readiness, nil
}

func EvaluateGate(snapshot Snapshot, gate schemas.ProjectGate) (schemas.ProjectReadinessGate, error) {
	def, ok := gateDefinitionFor(gate)
	if !ok {
		return schemas.ProjectReadinessGate{}, fmt.Errorf("%w: %s", ErrUnknownGate, gate)
	}
	checks := make([]schemas.GateCheck, 0, len(def.checks))
	blocking := 0
	for _, check := range def.checks {
		evaluated := schemas.GateCheck{Id: check.id, Ok: check.ok(snapshot)}
		if !evaluated.Ok {
			detail := check.detail
			evaluated.Detail = &detail
			blocking++
		}
		checks = append(checks, evaluated)
	}
	return schemas.ProjectReadinessGate{
		Gate:          gate,
		Satisfied:     blocking == 0,
		Checks:        checks,
		BlockingCount: &blocking,
	}, nil
}

func EvaluateTransition(snapshot Snapshot, request TransitionRequest) (TransitionDecision, error) {
	projectID := strings.TrimSpace(request.ProjectID)
	if projectID == "" {
		projectID = strings.TrimSpace(snapshot.ProjectID)
	}
	if projectID == "" {
		return TransitionDecision{}, fmt.Errorf("%w: projectId is required", ErrInvalidInput)
	}
	snapshot.ProjectID = projectID
	occurredAt := request.At
	if occurredAt.IsZero() {
		if snapshot.CheckedAt.IsZero() {
			occurredAt = time.Now()
		} else {
			occurredAt = snapshot.CheckedAt
		}
	}
	correlationID := transitionCorrelationID(projectID, request, occurredAt)
	if !request.From.Valid() || !request.To.Valid() {
		problem := transitionProblem(400, "project.invalid_lifecycle_transition", "Invalid lifecycle transition", "urn:hoopoe:projects/invalid-lifecycle-transition", "from and to must be known project lifecycle states", "", occurredAt, correlationID)
		return rejectedDecision(snapshot, request, nil, nil, problem, occurredAt), nil
	}
	required, ok := RequiredGates(request.From, request.To)
	if !ok {
		detail := fmt.Sprintf("transition %s -> %s is not an allowed adjacent project lifecycle transition", request.From, request.To)
		problem := transitionProblem(400, "project.invalid_lifecycle_transition", "Invalid lifecycle transition", "urn:hoopoe:projects/invalid-lifecycle-transition", detail, "", occurredAt, correlationID)
		return rejectedDecision(snapshot, request, nil, nil, problem, occurredAt), nil
	}
	if snapshot.CurrentState.Valid() && snapshot.CurrentState != request.From {
		detail := fmt.Sprintf("project is currently %s; transition expected current state %s", snapshot.CurrentState, request.From)
		problem := transitionProblem(409, "project.lifecycle_conflict", "Lifecycle state conflict", "urn:hoopoe:projects/lifecycle-conflict", detail, "", occurredAt, correlationID)
		return rejectedDecision(snapshot, request, required, nil, problem, occurredAt), nil
	}
	snapshot.CurrentState = request.From
	readiness, err := Evaluate(snapshot, required...)
	if err != nil {
		return TransitionDecision{}, err
	}
	var blocking *schemas.ProjectReadinessGate
	for _, gate := range readiness.Gates {
		if !gate.Satisfied {
			copied := gate
			blocking = &copied
			break
		}
	}
	if blocking != nil {
		missing := missingCheckIDs(*blocking)
		detail := fmt.Sprintf("transition %s -> %s is blocked by gate %s: %s", request.From, request.To, blocking.Gate, strings.Join(missing, ", "))
		problem := transitionProblem(422, "project.gate_blocked", "Project gate blocked", "urn:hoopoe:projects/gate-blocked", detail, string(blocking.Gate), occurredAt, correlationID)
		return rejectedDecision(snapshot, request, required, blocking, problem, occurredAt), nil
	}
	return TransitionDecision{
		Allowed:       true,
		RequiredGates: append([]schemas.ProjectGate(nil), required...),
		Readiness:     readiness,
		AuditEvent:    auditEvent(snapshot, request, ActionTransitionAllowed, "success", required, nil, nil, occurredAt),
	}, nil
}

func selectedGates(filters []schemas.ProjectGate) (map[schemas.ProjectGate]bool, error) {
	if len(filters) == 0 {
		return nil, nil
	}
	selected := make(map[schemas.ProjectGate]bool, len(filters))
	for _, gate := range filters {
		if !gate.Valid() {
			return nil, fmt.Errorf("%w: %s", ErrUnknownGate, gate)
		}
		selected[gate] = true
	}
	return selected, nil
}

func gateDefinitionFor(gate schemas.ProjectGate) (gateDefinition, bool) {
	for _, def := range gateDefinitions {
		if def.gate == gate {
			return def, true
		}
	}
	return gateDefinition{}, false
}

func rejectedDecision(snapshot Snapshot, request TransitionRequest, required []schemas.ProjectGate, blocking *schemas.ProjectReadinessGate, problem *schemas.Problem, occurredAt time.Time) TransitionDecision {
	var readiness schemas.ProjectReadiness
	if len(required) > 0 {
		readiness, _ = Evaluate(snapshot, required...)
	}
	var missing []string
	var blockingGate *schemas.ProjectGate
	if blocking != nil {
		missing = missingCheckIDs(*blocking)
		gate := blocking.Gate
		blockingGate = &gate
	}
	return TransitionDecision{
		Allowed:       false,
		RequiredGates: append([]schemas.ProjectGate(nil), required...),
		Readiness:     readiness,
		BlockingGate:  blocking,
		Problem:       problem,
		AuditEvent:    auditEvent(snapshot, request, ActionTransitionRejected, "failure", required, blockingGate, missing, occurredAt),
	}
}

func auditEvent(snapshot Snapshot, request TransitionRequest, action, result string, required []schemas.ProjectGate, blocking *schemas.ProjectGate, missing []string, occurredAt time.Time) TransitionAuditEvent {
	eventID := transitionEventID(snapshot.ProjectID, request.From, request.To, occurredAt, request.CorrelationID)
	correlationID := strings.TrimSpace(request.CorrelationID)
	if correlationID == "" {
		correlationID = eventID
	}
	return TransitionAuditEvent{
		SchemaVersion:   SchemaVersion,
		EventID:         eventID,
		ProjectID:       snapshot.ProjectID,
		Action:          action,
		Result:          result,
		Actor:           request.Actor,
		From:            request.From,
		To:              request.To,
		RequiredGates:   append([]schemas.ProjectGate(nil), required...),
		BlockingGate:    blocking,
		MissingCheckIDs: append([]string(nil), missing...),
		Reason:          strings.TrimSpace(request.Reason),
		CorrelationID:   correlationID,
		CausationID:     strings.TrimSpace(request.CausationID),
		At:              occurredAt.UTC(),
	}
}

func transitionProblem(status int, code, title, problemType, detail, gate string, occurredAt time.Time, correlationID string) *schemas.Problem {
	eventID := sanitizeID(strings.TrimSpace(correlationID))
	if eventID == "" {
		eventID = transitionProblemID(code, occurredAt)
	}
	instance := "urn:hoopoe:incident:" + eventID
	retryable := status == 422 || status == 409
	problem := &schemas.Problem{
		Type:          problemType,
		Code:          code,
		Title:         title,
		Status:        status,
		Detail:        &detail,
		Instance:      &instance,
		CorrelationId: &eventID,
		Retryable:     &retryable,
	}
	if gate != "" {
		problem.Capability = &gate
	}
	return problem
}

func transitionCorrelationID(projectID string, request TransitionRequest, occurredAt time.Time) string {
	if trimmed := strings.TrimSpace(request.CorrelationID); trimmed != "" {
		return trimmed
	}
	return transitionEventID(projectID, request.From, request.To, occurredAt, "")
}

func transitionProblemID(code string, occurredAt time.Time) string {
	return sanitizeID(fmt.Sprintf("%s:%d", code, occurredAt.UTC().UnixNano()))
}

func transitionEventID(projectID string, from, to schemas.ProjectLifecycleState, occurredAt time.Time, correlationID string) string {
	if trimmed := strings.TrimSpace(correlationID); trimmed != "" {
		return sanitizeID(trimmed)
	}
	return sanitizeID(fmt.Sprintf("gate-transition:%s:%s:%s:%d", projectID, from, to, occurredAt.UTC().UnixNano()))
}

func sanitizeID(value string) string {
	var builder strings.Builder
	lastDash := false
	for _, r := range strings.TrimSpace(value) {
		allowed := r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '_' || r == '-' || r == '.' || r == ':'
		if allowed {
			builder.WriteRune(r)
			lastDash = false
			continue
		}
		if !lastDash {
			builder.WriteByte('-')
			lastDash = true
		}
	}
	out := strings.Trim(builder.String(), "-")
	if out == "" {
		return "event"
	}
	return out
}

func missingCheckIDs(gate schemas.ProjectReadinessGate) []string {
	missing := make([]string, 0)
	for _, check := range gate.Checks {
		if !check.Ok {
			missing = append(missing, check.Id)
		}
	}
	return missing
}

func hasToolVersions(facts VPSFacts) bool {
	if facts.ToolVersionsRecorded {
		return true
	}
	for name, version := range facts.ToolVersions {
		if strings.TrimSpace(name) != "" && strings.TrimSpace(version) != "" {
			return true
		}
	}
	return false
}

func branchKnown(facts ProjectFacts) bool {
	return facts.BranchKnown || strings.TrimSpace(facts.Branch) != ""
}

func hasTrimmed(items []string) bool {
	for _, item := range items {
		if strings.TrimSpace(item) != "" {
			return true
		}
	}
	return false
}

var canonicalGateOrder = []schemas.ProjectGate{
	schemas.ProjectGateVpsReady,
	schemas.ProjectGateProjectImported,
	schemas.ProjectGatePlanLocked,
	schemas.ProjectGateBeadsCreated,
	schemas.ProjectGateBeadsFinalized,
	schemas.ProjectGateLaunchReady,
	schemas.ProjectGateHardeningReady,
	schemas.ProjectGateShipReady,
}

var transitionDefinitions = []TransitionDefinition{
	{From: schemas.ProjectLifecycleStateImported, To: schemas.ProjectLifecycleStatePlanning, RequiredGates: []schemas.ProjectGate{schemas.ProjectGateVpsReady, schemas.ProjectGateProjectImported}},
	{From: schemas.ProjectLifecycleStatePlanning, To: schemas.ProjectLifecycleStatePlanFinalized, RequiredGates: []schemas.ProjectGate{schemas.ProjectGateVpsReady, schemas.ProjectGatePlanLocked}},
	{From: schemas.ProjectLifecycleStatePlanFinalized, To: schemas.ProjectLifecycleStateBeadsCreated, RequiredGates: []schemas.ProjectGate{schemas.ProjectGateVpsReady, schemas.ProjectGateBeadsCreated}},
	{From: schemas.ProjectLifecycleStateBeadsCreated, To: schemas.ProjectLifecycleStateBeadsFinalized, RequiredGates: []schemas.ProjectGate{schemas.ProjectGateVpsReady, schemas.ProjectGateBeadsFinalized}},
	{From: schemas.ProjectLifecycleStateBeadsFinalized, To: schemas.ProjectLifecycleStateSwarmRunning, RequiredGates: []schemas.ProjectGate{schemas.ProjectGateVpsReady, schemas.ProjectGateLaunchReady}},
	{From: schemas.ProjectLifecycleStateSwarmRunning, To: schemas.ProjectLifecycleStateHardeningRounds, RequiredGates: []schemas.ProjectGate{schemas.ProjectGateVpsReady, schemas.ProjectGateHardeningReady}},
	{From: schemas.ProjectLifecycleStateHardeningRounds, To: schemas.ProjectLifecycleStateQualityGates, RequiredGates: []schemas.ProjectGate{schemas.ProjectGateVpsReady, schemas.ProjectGateShipReady}},
	{From: schemas.ProjectLifecycleStateQualityGates, To: schemas.ProjectLifecycleStateCompleted, RequiredGates: []schemas.ProjectGate{schemas.ProjectGateVpsReady, schemas.ProjectGateShipReady}},
}

var gateDefinitions = []gateDefinition{
	{
		gate:  schemas.ProjectGateVpsReady,
		label: "VPS ready",
		checks: []checkDefinition{
			{id: "vps.ssh_verified", detail: "SSH has not been verified for the VPS", ok: func(s Snapshot) bool { return s.VPS.SSHVerified }},
			{id: "vps.daemon_reachable", detail: "Hoopoe daemon is not reachable on the VPS", ok: func(s Snapshot) bool { return s.VPS.DaemonReachable }},
			{id: "vps.acfs_resolved", detail: "ACFS must be installed or intentionally skipped", ok: func(s Snapshot) bool { return s.VPS.ACFSInstalled || s.VPS.ACFSSkipped }},
			{id: "vps.tool_versions_recorded", detail: "tool versions have not been recorded", ok: func(s Snapshot) bool { return hasToolVersions(s.VPS) }},
		},
	},
	{
		gate:  schemas.ProjectGateProjectImported,
		label: "Project imported",
		checks: []checkDefinition{
			{id: "git.repo_present", detail: "no git repository at project root", ok: func(s Snapshot) bool { return s.Project.GitRepoPresent }},
			{id: "git.branch_known", detail: "project branch is unknown", ok: func(s Snapshot) bool { return branchKnown(s.Project) }},
			{id: "agents.md", detail: "AGENTS.md is missing", ok: func(s Snapshot) bool { return s.Project.AgentsMDPresent }},
			{id: "hoopoe.dir", detail: ".hoopoe/ is not initialized", ok: func(s Snapshot) bool { return s.Project.HoopoeInitialized }},
			{id: "tools.detected", detail: "tool detection has not completed", ok: func(s Snapshot) bool { return s.Project.ToolDetectionDone }},
		},
	},
	{
		gate:  schemas.ProjectGatePlanLocked,
		label: "Plan locked",
		checks: []checkDefinition{
			{id: "plan.locked", detail: "plan is not locked", ok: func(s Snapshot) bool { return s.Plan.Locked }},
			{id: "plan.self_contained", detail: "plan is not self-contained", ok: func(s Snapshot) bool { return s.Plan.SelfContained }},
			{id: "plan.decisions_explicit", detail: "major decisions are not explicit", ok: func(s Snapshot) bool { return s.Plan.DecisionsExplicit }},
			{id: "plan.testing_strategy_present", detail: "testing strategy is missing", ok: func(s Snapshot) bool { return s.Plan.TestingStrategyPresent }},
			{id: "plan.unresolved_decisions_accepted", detail: "unresolved decisions must be listed or accepted", ok: func(s Snapshot) bool { return s.Plan.UnresolvedDecisionsAccepted }},
		},
	},
	{
		gate:  schemas.ProjectGateBeadsCreated,
		label: "Beads created",
		checks: []checkDefinition{
			{id: "br.beads_linked_to_plan", detail: "br has no beads linked to the plan", ok: func(s Snapshot) bool { return s.BeadsCreated.LinkedPlanBeads > 0 }},
			{id: "beads.issues_jsonl_flushed", detail: ".beads/issues.jsonl has not been flushed", ok: func(s Snapshot) bool { return s.BeadsCreated.IssuesJSONLFlushed }},
			{id: "beads.conversion_artifacts_saved", detail: "bead conversion artifacts are not saved", ok: func(s Snapshot) bool {
				return s.BeadsCreated.ConversionArtifactsSaved || hasTrimmed(s.BeadsCreated.ArtifactIDs)
			}},
		},
	},
	{
		gate:  schemas.ProjectGateBeadsFinalized,
		label: "Beads finalized",
		checks: []checkDefinition{
			{id: "beads.coverage_checked", detail: "plan-to-bead coverage has not been checked", ok: func(s Snapshot) bool { return s.BeadsFinalized.CoverageChecked }},
			{id: "beads.dependencies_checked", detail: "bead dependencies have not been checked", ok: func(s Snapshot) bool { return s.BeadsFinalized.DependenciesChecked }},
			{id: "beads.ready_set_sufficient", detail: "ready bead set is not sufficient", ok: func(s Snapshot) bool { return s.BeadsFinalized.ReadySetSufficient || s.BeadsFinalized.ReadyCount > 0 }},
			{id: "beads.clarity_acceptable", detail: "bead clarity is not acceptable", ok: func(s Snapshot) bool { return s.BeadsFinalized.ClarityAcceptable }},
			{id: "beads.testability_acceptable", detail: "bead testability is not acceptable", ok: func(s Snapshot) bool { return s.BeadsFinalized.TestabilityAcceptable }},
		},
	},
	{
		gate:  schemas.ProjectGateLaunchReady,
		label: "Launch ready",
		checks: []checkDefinition{
			{id: "ntm.healthy", detail: "NTM is not healthy", ok: func(s Snapshot) bool { return s.Launch.NTMHealthy }},
			{id: "agent_mail.healthy", detail: "Agent Mail is not healthy", ok: func(s Snapshot) bool { return s.Launch.AgentMailHealthy }},
			{id: "bv.robot_healthy", detail: "bv robot surface is not healthy", ok: func(s Snapshot) bool { return s.Launch.BVRobotHealthy }},
			{id: "br.ready_nonempty_or_intentionally_scoped", detail: "br ready --json is empty and scope was not intentionally accepted", ok: func(s Snapshot) bool {
				return s.Launch.BRReadyNonempty || s.Launch.BRReadyCount > 0 || s.Launch.IntentionallyScoped
			}},
			{id: "build_queue.policy_set", detail: "build queue policy is not set", ok: func(s Snapshot) bool { return s.Launch.BuildQueuePolicySet }},
		},
	},
	{
		gate:  schemas.ProjectGateHardeningReady,
		label: "Hardening ready",
		checks: []checkDefinition{
			{id: "implementation.closed_or_deferred", detail: "implementation beads are not closed or intentionally deferred", ok: func(s Snapshot) bool { return s.Hardening.ImplementationClosedOrDeferred }},
			{id: "beads.no_stuck_in_progress", detail: "stuck in-progress beads are present", ok: func(s Snapshot) bool { return s.Hardening.NoStuckInProgress && s.Hardening.StuckCount == 0 }},
			{id: "review.prompts_available", detail: "review prompts are not available", ok: func(s Snapshot) bool { return s.Hardening.ReviewPromptsAvailable }},
		},
	},
	{
		gate:  schemas.ProjectGateShipReady,
		label: "Ship ready",
		checks: []checkDefinition{
			{id: "tests_builds.pass_or_exceptions", detail: "tests/builds have not passed and no exceptions are documented", ok: func(s Snapshot) bool { return s.Ship.TestsBuildsPass || hasTrimmed(s.Ship.TestBuildExceptions) }},
			{id: "code_health.pass_or_followups", detail: "code health gates have not passed and no follow-up beads exist", ok: func(s Snapshot) bool { return s.Ship.CodeHealthPass || hasTrimmed(s.Ship.FollowUpBeads) }},
			{id: "git.synced", detail: "Git is not synced", ok: func(s Snapshot) bool { return s.Ship.GitSynced }},
			{id: "beads.synced", detail: "beads are not synced", ok: func(s Snapshot) bool { return s.Ship.BeadsSynced }},
		},
	},
}
