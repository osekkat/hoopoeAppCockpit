// Package agent owns the tending agent runtime and typed ActionPlan executor.
// Agents may reason with read-only tools, but every mutation flows through the
// closed action catalog and daemon-owned handlers in this package.
package agent

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	agentmailadapter "github.com/hoopoe-cockpit/hoopoe/apps/daemon/internal/adapters/agentmail"
	bradapter "github.com/hoopoe-cockpit/hoopoe/apps/daemon/internal/adapters/br"
	caamadapter "github.com/hoopoe-cockpit/hoopoe/apps/daemon/internal/adapters/caam"
	gitadapter "github.com/hoopoe-cockpit/hoopoe/apps/daemon/internal/adapters/git"
	ntmadapter "github.com/hoopoe-cockpit/hoopoe/apps/daemon/internal/adapters/ntm"
	ptadapter "github.com/hoopoe-cockpit/hoopoe/apps/daemon/internal/adapters/pt"
	"github.com/hoopoe-cockpit/hoopoe/apps/daemon/internal/capabilities"
	schemas "github.com/hoopoe-cockpit/hoopoe/packages/schemas/go"
)

const ActionPlanSchemaVersion = 1

type CapabilityRequirement struct {
	ID                   string
	AllowBlockedByPolicy bool
}

type ActionSpec struct {
	Kind                    schemas.ActionKind
	RiskClass               schemas.ApprovalRiskClass
	RequiresApprovalDefault bool
	RequiredTargetKeys      []string
	RequiredArgKeys         []string
	RequiredCapabilities    []CapabilityRequirement
	Preconditions           []string
	Postconditions          []string
}

func (s ActionSpec) RequiresApproval(planRisk schemas.ApprovalRiskClass, planRequires *bool) bool {
	if planRequires != nil && *planRequires {
		return true
	}
	return s.RequiresApprovalDefault || riskRank(planRisk) >= riskRank(schemas.High)
}

type ActionCatalog map[schemas.ActionKind]ActionSpec

func DefaultActionCatalog() ActionCatalog {
	return ActionCatalog{
		schemas.AgentAskStatus: {
			Kind:      schemas.AgentAskStatus,
			RiskClass: schemas.Low,
			RequiredTargetKeys: []string{
				"agentId",
			},
			RequiredCapabilities: []CapabilityRequirement{
				{ID: agentmailadapter.CapabilityMessagesSend},
				{ID: ntmadapter.CapabilityRobotSnapshot},
			},
			Preconditions: []string{
				"ntm.snapshot reports agent.pane is alive",
			},
			Postconditions: []string{
				"agent_mail.fetch_inbox returns a status reply within the bounded window OR an idle marker is recorded in audit",
			},
		},
		schemas.AgentSendMarchingOrders: {
			Kind:      schemas.AgentSendMarchingOrders,
			RiskClass: schemas.Medium,
			RequiredTargetKeys: []string{
				"agentId",
			},
			RequiredArgKeys: []string{
				"marchingOrdersBody",
			},
			RequiredCapabilities: []CapabilityRequirement{
				{ID: ntmadapter.CapabilitySendMarchingOrders},
			},
			Preconditions: []string{
				"ntm.snapshot reports agent.pane is alive",
				"agent has no destructive action in flight",
			},
			Postconditions: []string{
				"ntm.send-marching-orders RPC returned ok",
				"audit log records the new marching-orders body hash for this agent",
			},
		},
		schemas.AgentPause: {
			Kind:      schemas.AgentPause,
			RiskClass: schemas.Low,
			RequiredTargetKeys: []string{
				"agentId",
			},
			RequiredCapabilities: []CapabilityRequirement{
				{ID: ntmadapter.CapabilityRobotSnapshot},
			},
			Preconditions: []string{
				"agent is not already paused (job-registry state)",
			},
			Postconditions: []string{
				"tending pre-scripts skip the agent on the next tick",
				"pause record visible in /v1/tending/agents",
			},
		},
		schemas.AgentKillWedgedProcess: {
			Kind:                    schemas.AgentKillWedgedProcess,
			RiskClass:               schemas.High,
			RequiresApprovalDefault: true,
			RequiredTargetKeys: []string{
				"agentId",
			},
			RequiredCapabilities: []CapabilityRequirement{
				{ID: ptadapter.CapabilityKill, AllowBlockedByPolicy: true},
				{ID: ntmadapter.CapabilityPaneKill, AllowBlockedByPolicy: true},
				{ID: ntmadapter.CapabilityRobotSnapshot},
			},
			Preconditions: []string{
				"pt reports the agent.worker process group is alive",
				"wedged-evidence threshold has been crossed in the detection layer",
			},
			Postconditions: []string{
				"pt reports the process group no longer exists",
				"ntm.snapshot no longer lists the agent as running",
				"any held file_reservations were released or transferred per policy",
			},
		},
		schemas.ReservationForceRelease: {
			Kind:                    schemas.ReservationForceRelease,
			RiskClass:               schemas.High,
			RequiresApprovalDefault: true,
			RequiredTargetKeys: []string{
				"reservationId",
			},
			RequiredCapabilities: []CapabilityRequirement{
				{ID: agentmailadapter.CapabilityReservationsForceRelease},
				{ID: agentmailadapter.CapabilityForceReleaseNotification},
			},
			Preconditions: []string{
				"agent_mail.reservations reports the reservation as still held",
				"holding agent is verifiably gone (no inbox activity past stale-threshold OR pane is dead)",
			},
			Postconditions: []string{
				"agent_mail.reservations no longer reports the reservation",
				"release notice posted in the bead.thread (visible via agent_mail.fetch_inbox)",
			},
		},
		schemas.CaamSwitchAccount: {
			Kind:      schemas.CaamSwitchAccount,
			RiskClass: schemas.Medium,
			RequiredTargetKeys: []string{
				"agentId",
				"targetAccountId",
			},
			RequiredCapabilities: []CapabilityRequirement{
				{ID: caamadapter.CapAccountSwitch},
				{ID: caamadapter.CapAgentsDetect},
			},
			Preconditions: []string{
				"caam reports targetAccountId is registered AND not rate-limited",
				"rate-limit signal is present on the current account",
			},
			Postconditions: []string{
				"caam reports the agent is now bound to targetAccountId",
				"agent pane resumes producing output within the bounded resume window",
			},
		},
		schemas.CasrResumeSession: {
			Kind:      schemas.CasrResumeSession,
			RiskClass: schemas.Low,
			RequiredTargetKeys: []string{
				"sessionId",
			},
			RequiredCapabilities: []CapabilityRequirement{
				{ID: "casr.session.resume"},
			},
			Preconditions: []string{
				"casr reports the session is suspended (not active, not deleted)",
			},
			Postconditions: []string{
				"casr reports the target CLI is now driving the session",
				"session context (last N exchanges) is preserved in the resumed CLI",
			},
		},
		schemas.GitPushBranch: {
			Kind:      schemas.GitPushBranch,
			RiskClass: schemas.Medium,
			RequiredTargetKeys: []string{
				"projectId",
				"branch",
			},
			RequiredArgKeys: []string{
				"expectedSha",
			},
			RequiredCapabilities: []CapabilityRequirement{
				{ID: gitadapter.CapPush},
				{ID: gitadapter.CapRemoteRead},
			},
			Preconditions: []string{
				"local branch tip equals expectedSha (git rev-parse HEAD)",
				"remote ref state has been re-read since the last push attempt for this branch",
			},
			Postconditions: []string{
				"git ls-remote on origin reports the branch ref equal to expectedSha (NOT trusting git push stdout)",
			},
		},
		schemas.SwarmHalt: {
			Kind:                    schemas.SwarmHalt,
			RiskClass:               schemas.High,
			RequiresApprovalDefault: true,
			RequiredTargetKeys: []string{
				"swarmId",
			},
			RequiredArgKeys: []string{
				"reason",
			},
			RequiredCapabilities: []CapabilityRequirement{
				{ID: ntmadapter.CapabilitySwarmHalt},
			},
			Preconditions: []string{
				"swarm.session reports the swarm is in a haltable state (running or paused)",
			},
			Postconditions: []string{
				"swarm.session reports state=halted",
				"every agent in the swarm is paused or terminated per policy",
				"build queue is paused for the swarm",
				"audit log records the halt event with reason + actor",
			},
		},
		schemas.ReviewProposeFlip: {
			Kind:                    schemas.ReviewProposeFlip,
			RiskClass:               schemas.Medium,
			RequiresApprovalDefault: true,
			RequiredTargetKeys: []string{
				"projectId",
				"swarmId",
			},
			Preconditions: []string{
				"project state is swarm_running",
				"br.ready report is below the configured threshold for the project",
			},
			Postconditions: []string{
				"an Approval is queued in /v1/approvals with kind=review.flip",
				"no project state change has happened (the flip itself waits for approval)",
			},
		},
		schemas.BeadCreateBlocker: {
			Kind:      schemas.BeadCreateBlocker,
			RiskClass: schemas.Low,
			RequiredTargetKeys: []string{
				"projectId",
				"sourceBeadId",
			},
			RequiredArgKeys: []string{
				"title",
			},
			RequiredCapabilities: []CapabilityRequirement{
				{ID: bradapter.CapabilityCreate},
				{ID: bradapter.CapabilityDepAdd},
			},
			Preconditions: []string{
				"br.show sourceBeadId returns a non-closed bead",
			},
			Postconditions: []string{
				"br.show on the new bead id returns the requested title + priority",
				"br.dep list reports the new bead blocks sourceBeadId",
			},
		},
	}
}

func KnownActionKinds() []schemas.ActionKind {
	catalog := DefaultActionCatalog()
	kinds := make([]schemas.ActionKind, 0, len(catalog))
	for kind := range catalog {
		kinds = append(kinds, kind)
	}
	sort.Slice(kinds, func(i, j int) bool { return kinds[i] < kinds[j] })
	return kinds
}

func ValidatePlan(plan schemas.ActionPlan) error {
	var problems []string
	if plan.SchemaVersion != ActionPlanSchemaVersion {
		problems = append(problems, fmt.Sprintf("schemaVersion must be %d", ActionPlanSchemaVersion))
	}
	if strings.TrimSpace(plan.JobId) == "" {
		problems = append(problems, "jobId is required")
	}
	if strings.TrimSpace(plan.RunId) == "" {
		problems = append(problems, "runId is required")
	}
	if strings.TrimSpace(plan.Summary) == "" {
		problems = append(problems, "summary is required")
	}
	if !plan.RiskClass.Valid() {
		problems = append(problems, fmt.Sprintf("riskClass %q is not valid", plan.RiskClass))
	}
	if len(plan.Actions) == 0 {
		problems = append(problems, "actions must contain at least one action")
	}

	catalog := DefaultActionCatalog()
	seenKeys := make(map[string]struct{}, len(plan.Actions))
	requiresApproval := false
	for idx, action := range plan.Actions {
		prefix := fmt.Sprintf("actions[%d]", idx)
		spec, ok := catalog[action.Kind]
		if !action.Kind.Valid() || !ok {
			problems = append(problems, fmt.Sprintf("%s.kind %q is not an allowed tending action", prefix, action.Kind))
			continue
		}
		if rank := riskRank(plan.RiskClass); rank < riskRank(spec.RiskClass) {
			problems = append(problems, fmt.Sprintf("%s.kind %s has risk %s above plan riskClass %s", prefix, action.Kind, spec.RiskClass, plan.RiskClass))
		}
		if spec.RequiresApproval(plan.RiskClass, plan.RequiresApproval) {
			requiresApproval = true
		}
		key := strings.TrimSpace(action.IdempotencyKey)
		if key == "" {
			problems = append(problems, fmt.Sprintf("%s.idempotencyKey is required", prefix))
		} else if _, ok := seenKeys[key]; ok {
			problems = append(problems, fmt.Sprintf("%s.idempotencyKey %q duplicates another action", prefix, key))
		} else {
			seenKeys[key] = struct{}{}
		}
		problems = append(problems, validateRequiredFields(prefix+".target", action.Target, spec.RequiredTargetKeys)...)
		problems = append(problems, validateRequiredFields(prefix+".args", actionArgs(action), spec.RequiredArgKeys)...)
		problems = append(problems, validateStringList(prefix+".preconditions", action.Preconditions)...)
		problems = append(problems, validateStringList(prefix+".postconditions", action.Postconditions)...)
	}
	if plan.RequiresApproval != nil && !*plan.RequiresApproval && requiresApproval {
		problems = append(problems, "requiresApproval=false conflicts with approval-required action or plan risk")
	}
	if len(problems) > 0 {
		return ValidationError{Problems: problems}
	}
	return nil
}

type ValidationError struct {
	Problems []string
}

func (e ValidationError) Error() string {
	return "agent: invalid ActionPlan: " + strings.Join(e.Problems, "; ")
}

func actionArgs(action schemas.Action) map[string]any {
	if action.Args == nil {
		return nil
	}
	return *action.Args
}

func actionList(values *[]string, fallback []string) []string {
	if values == nil {
		return append([]string(nil), fallback...)
	}
	return append([]string(nil), (*values)...)
}

func validateRequiredFields(path string, values map[string]any, keys []string) []string {
	if len(keys) == 0 {
		return nil
	}
	var problems []string
	if values == nil {
		return []string{path + " is required"}
	}
	for _, key := range keys {
		value, ok := values[key]
		if !ok {
			problems = append(problems, fmt.Sprintf("%s.%s is required", path, key))
			continue
		}
		if s, ok := value.(string); ok && strings.TrimSpace(s) == "" {
			problems = append(problems, fmt.Sprintf("%s.%s must be non-empty", path, key))
		}
		if value == nil {
			problems = append(problems, fmt.Sprintf("%s.%s must be non-null", path, key))
		}
	}
	return problems
}

func validateStringList(path string, values *[]string) []string {
	if values == nil {
		return nil
	}
	var problems []string
	for idx, value := range *values {
		if strings.TrimSpace(value) == "" {
			problems = append(problems, fmt.Sprintf("%s[%d] must be non-empty", path, idx))
		}
	}
	return problems
}

func riskRank(risk schemas.ApprovalRiskClass) int {
	switch risk {
	case schemas.Low:
		return 1
	case schemas.Medium:
		return 2
	case schemas.High:
		return 3
	case schemas.Critical:
		return 4
	default:
		return 0
	}
}

type CapabilityCheck struct {
	ID      string
	Status  capabilities.CapabilityStatus
	Allowed bool
	Reason  string
}

type CapabilityRequest struct {
	Plan         schemas.ActionPlan
	Action       schemas.Action
	Spec         ActionSpec
	Requirements []CapabilityRequirement
}

type CapabilityChecker interface {
	CheckCapabilities(ctx context.Context, req CapabilityRequest) ([]CapabilityCheck, error)
}

type RegistryCapabilityChecker struct {
	Registry *capabilities.Registry
}

func (c RegistryCapabilityChecker) CheckCapabilities(_ context.Context, req CapabilityRequest) ([]CapabilityCheck, error) {
	if c.Registry == nil {
		return nil, fmt.Errorf("agent: capability registry is not configured")
	}
	checks := make([]CapabilityCheck, 0, len(req.Requirements))
	var blocked []string
	for _, required := range req.Requirements {
		status, ok := c.Registry.LookupCapabilityStatus(required.ID)
		check := CapabilityCheck{
			ID:     required.ID,
			Status: status,
		}
		switch {
		case !ok:
			check.Status = capabilities.StatusMissing
			check.Reason = "capability is not reported"
		case status == capabilities.StatusOK || status == capabilities.StatusDegraded:
			check.Allowed = true
		case status == capabilities.StatusBlockedByPolicy && required.AllowBlockedByPolicy:
			check.Allowed = true
			check.Reason = "allowed because this action is executing through ActionPlan policy"
		default:
			check.Reason = fmt.Sprintf("capability status %s is not executable", status)
		}
		checks = append(checks, check)
		if !check.Allowed {
			blocked = append(blocked, required.ID)
		}
	}
	if len(blocked) > 0 {
		sort.Strings(blocked)
		return checks, fmt.Errorf("agent: unavailable capabilities: %s", strings.Join(blocked, ", "))
	}
	return checks, nil
}

type AllowAllCapabilities struct{}

func (AllowAllCapabilities) CheckCapabilities(_ context.Context, req CapabilityRequest) ([]CapabilityCheck, error) {
	checks := make([]CapabilityCheck, 0, len(req.Requirements))
	for _, required := range req.Requirements {
		checks = append(checks, CapabilityCheck{
			ID:      required.ID,
			Status:  capabilities.StatusOK,
			Allowed: true,
		})
	}
	return checks, nil
}

type AuditEvent struct {
	Time           time.Time
	JobID          string
	RunID          string
	Action         string
	ActionKind     schemas.ActionKind
	IdempotencyKey string
	Status         string
	Reason         string
	Data           map[string]any
}

type AuditSink interface {
	RecordAuditEvent(ctx context.Context, event AuditEvent) error
}
