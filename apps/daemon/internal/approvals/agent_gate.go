package approvals

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/hoopoe-cockpit/hoopoe/apps/daemon/internal/agent"
	schemas "github.com/hoopoe-cockpit/hoopoe/packages/schemas/go"
)

func (q *Queue) RequestApproval(ctx context.Context, req agent.ApprovalRequest) (agent.ApprovalDecision, error) {
	if q == nil {
		return agent.ApprovalDecision{}, fmt.Errorf("%w: nil queue", ErrInvalidRequest)
	}
	approval, _, err := q.Request(ctx, Request{
		Source:          schemas.ApprovalSourceHoopoePolicy,
		PolicyRule:      PolicyRulePrefixHoopoe + string(req.Action.Kind),
		RequestedAction: actionToCommandSpec(req.Action),
		RequestActor:    actorFromActionPlan(req.Plan),
		Reason:          approvalReason(req),
		EvidenceRefs:    stringSliceValue(req.Plan.EvidenceRefs),
		ProjectID:       targetString(req.Action.Target, "projectId"),
		BeadID:          targetString(req.Action.Target, "beadId"),
		AgentID:         targetString(req.Action.Target, "agentId"),
		SwarmID:         targetString(req.Action.Target, "swarmId"),
		Scope:           schemas.Once,
		RiskClass:       maxRisk(req.Plan.RiskClass, req.Spec.RiskClass),
		RequestedAt:     req.RequestedAt,
	})
	if err != nil {
		return agent.ApprovalDecision{}, err
	}
	return agent.ApprovalDecision{
		State:      agentApprovalState(approval.State),
		ApprovalID: approval.Id,
		Reason:     stringValue(approval.Reason),
		DecidedAt:  timeValue(approval.DecidedAt),
	}, nil
}

func actionToCommandSpec(action schemas.Action) schemas.CommandSpec {
	return schemas.CommandSpec{
		Kind:           string(action.Kind),
		Target:         cloneAnyMap(action.Target),
		Args:           cloneArgs(action.Args),
		IdempotencyKey: stringPtrOrNil(action.IdempotencyKey),
		Preconditions:  cloneStringSlicePtr(action.Preconditions),
		Postconditions: cloneStringSlicePtr(action.Postconditions),
	}
}

func actorFromActionPlan(plan schemas.ActionPlan) schemas.Actor {
	if strings.TrimSpace(plan.RunId) != "" {
		return schemas.Actor{
			Kind:        schemas.ActorKindAgent,
			Id:          stringPtrOrNil(plan.RunId),
			DisplayName: stringPtrOrNil(plan.JobId),
		}
	}
	return schemas.Actor{
		Kind: schemas.ActorKindScheduler,
		Id:   stringPtrOrNil(plan.JobId),
	}
}

func approvalReason(req agent.ApprovalRequest) string {
	if strings.TrimSpace(req.Reason) != "" {
		return req.Reason
	}
	if strings.TrimSpace(req.DryRun.Summary) != "" {
		return req.DryRun.Summary
	}
	return fmt.Sprintf("%s requires approval", req.Action.Kind)
}

func agentApprovalState(state schemas.ApprovalState) agent.ApprovalState {
	switch state {
	case schemas.Approved:
		return agent.ApprovalApproved
	case schemas.Denied, schemas.Revoked:
		return agent.ApprovalDenied
	default:
		return agent.ApprovalPending
	}
}

func maxRisk(first schemas.ApprovalRiskClass, second schemas.ApprovalRiskClass) schemas.ApprovalRiskClass {
	if riskRank(first) >= riskRank(second) {
		return first
	}
	return second
}

func riskRank(risk schemas.ApprovalRiskClass) int {
	switch risk {
	case schemas.Critical:
		return 4
	case schemas.High:
		return 3
	case schemas.Medium:
		return 2
	case schemas.Low:
		return 1
	default:
		return 0
	}
}

func targetString(target map[string]interface{}, key string) string {
	value, ok := target[key]
	if !ok {
		return ""
	}
	if str, ok := value.(string); ok {
		return strings.TrimSpace(str)
	}
	return strings.TrimSpace(fmt.Sprint(value))
}

func cloneArgs(args *map[string]interface{}) *map[string]interface{} {
	if args == nil {
		return nil
	}
	clone := cloneAnyMap(*args)
	return &clone
}

func stringSliceValue(values *[]string) []string {
	if values == nil {
		return nil
	}
	out := make([]string, len(*values))
	copy(out, *values)
	return out
}

func timeValue(value *time.Time) time.Time {
	if value == nil {
		return time.Time{}
	}
	return value.UTC()
}
