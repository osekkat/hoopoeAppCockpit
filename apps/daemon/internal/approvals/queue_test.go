package approvals

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	dcgadapter "github.com/hoopoe-cockpit/hoopoe/apps/daemon/internal/adapters/dcg"
	"github.com/hoopoe-cockpit/hoopoe/apps/daemon/internal/agent"
	schemas "github.com/hoopoe-cockpit/hoopoe/packages/schemas/go"
)

func TestRequestCreatesPendingApprovalWithDefaults(t *testing.T) {
	ctx := context.Background()
	now := fixedTime()
	queue := testQueue(&now)

	approval, created, err := queue.Request(ctx, Request{
		Source:          schemas.ApprovalSourceHoopoePolicy,
		PolicyRule:      "hoopoe-policy:git.force_push",
		RequestedAction: commandSpec("git.push_branch", "force-push-1"),
		RequestActor:    actor(schemas.ActorKindAgent, "agent-1"),
		Reason:          "force-push needs explicit approval",
		ProjectID:       "project-1",
		BeadID:          "hp-v0g",
		RiskClass:       schemas.Critical,
	})
	if err != nil {
		t.Fatalf("Request: %v", err)
	}
	if !created {
		t.Fatalf("created = false, want true")
	}
	if approval.Id != "appr_01" {
		t.Fatalf("id = %q, want appr_01", approval.Id)
	}
	if approval.State != schemas.Pending {
		t.Fatalf("state = %s, want pending", approval.State)
	}
	if approval.SchemaVersion != SchemaVersion {
		t.Fatalf("schema = %d, want %d", approval.SchemaVersion, SchemaVersion)
	}
	if approval.Scope != schemas.Once {
		t.Fatalf("scope = %s, want once", approval.Scope)
	}
	if approval.ExpiresAt == nil || !approval.ExpiresAt.Equal(now.Add(DefaultTTL)) {
		t.Fatalf("expiry = %v, want default ttl", approval.ExpiresAt)
	}
	if approval.ProjectId == nil || *approval.ProjectId != "project-1" {
		t.Fatalf("project id = %v", approval.ProjectId)
	}
	if approval.RequestedAction.IdempotencyKey == nil || *approval.RequestedAction.IdempotencyKey != "force-push-1" {
		t.Fatalf("idempotency key = %v", approval.RequestedAction.IdempotencyKey)
	}
}

func TestDuplicateRequestReturnsExistingApproval(t *testing.T) {
	ctx := context.Background()
	now := fixedTime()
	queue := testQueue(&now)
	req := Request{
		Source:          schemas.ApprovalSourceHoopoePolicy,
		PolicyRule:      "hoopoe-policy:git.force_push",
		RequestedAction: commandSpec("git.push_branch", "push-1"),
		RequestActor:    actor(schemas.ActorKindAgent, "agent-1"),
		ProjectID:       "project-1",
		RiskClass:       schemas.High,
	}

	first, created, err := queue.Request(ctx, req)
	if err != nil {
		t.Fatalf("first Request: %v", err)
	}
	if !created {
		t.Fatalf("first created = false, want true")
	}
	second, created, err := queue.Request(ctx, req)
	if err != nil {
		t.Fatalf("second Request: %v", err)
	}
	if created {
		t.Fatalf("second created = true, want false")
	}
	if second.Id != first.Id {
		t.Fatalf("duplicate id = %q, want %q", second.Id, first.Id)
	}
	items, err := queue.List(ctx, ListFilter{IncludeExpired: true})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("items = %d, want 1", len(items))
	}
}

func TestApproveDenyAndInvalidTransitions(t *testing.T) {
	ctx := context.Background()
	now := fixedTime()
	queue := testQueue(&now)
	approval, _, err := queue.Request(ctx, Request{
		PolicyRule:      "hoopoe-policy:reservation.force_release",
		RequestedAction: commandSpec("reservation.force_release", "reservation-1"),
		RequestActor:    actor(schemas.ActorKindAgent, "agent-1"),
		RiskClass:       schemas.High,
	})
	if err != nil {
		t.Fatalf("Request: %v", err)
	}

	approved, err := queue.Approve(ctx, approval.Id, schemas.ApprovalDecisionRequest{
		DecisionActor: actor(schemas.ActorKindUser, "user-1"),
		Note:          stringPtr("ok for this bead"),
	})
	if err != nil {
		t.Fatalf("Approve: %v", err)
	}
	if approved.State != schemas.Approved {
		t.Fatalf("state = %s, want approved", approved.State)
	}
	if approved.DecidedAt == nil || !approved.DecidedAt.Equal(now) {
		t.Fatalf("decidedAt = %v, want %v", approved.DecidedAt, now)
	}
	if _, err := queue.Deny(ctx, approval.Id, schemas.ApprovalDecisionRequest{
		DecisionActor: actor(schemas.ActorKindUser, "user-2"),
	}); !errors.Is(err, ErrInvalidTransition) {
		t.Fatalf("Deny approved err = %v, want ErrInvalidTransition", err)
	}
}

func TestExpiredApprovalCannotBeApproved(t *testing.T) {
	ctx := context.Background()
	now := fixedTime()
	queue := testQueue(&now)
	expiresAt := now.Add(time.Minute)
	approval, _, err := queue.Request(ctx, Request{
		PolicyRule:      "hoopoe-policy:swarm.halt",
		RequestedAction: commandSpec("swarm.halt", "halt-1"),
		RequestActor:    actor(schemas.ActorKindScheduler, "watch-safety-thresholds"),
		RiskClass:       schemas.Critical,
		ExpiresAt:       &expiresAt,
	})
	if err != nil {
		t.Fatalf("Request: %v", err)
	}

	now = expiresAt.Add(time.Second)
	_, err = queue.Approve(ctx, approval.Id, schemas.ApprovalDecisionRequest{
		DecisionActor: actor(schemas.ActorKindUser, "user-1"),
	})
	if !errors.Is(err, ErrExpired) {
		t.Fatalf("Approve expired err = %v, want ErrExpired", err)
	}
	items, err := queue.List(ctx, ListFilter{IncludeExpired: true})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(items) != 1 || items[0].State != schemas.Expired {
		t.Fatalf("items = %+v, want one expired approval", items)
	}
	visible, err := queue.List(ctx, ListFilter{})
	if err != nil {
		t.Fatalf("List visible: %v", err)
	}
	if len(visible) != 0 {
		t.Fatalf("visible expired items = %d, want 0", len(visible))
	}
}

func TestApprovedScopeAuthorizesMatchingAction(t *testing.T) {
	ctx := context.Background()
	now := fixedTime()
	queue := testQueue(&now)
	approval, _, err := queue.Request(ctx, Request{
		PolicyRule:      "hoopoe-policy:git.force_push",
		RequestedAction: commandSpec("git.push_branch", "push-1"),
		RequestActor:    actor(schemas.ActorKindAgent, "agent-1"),
		ProjectID:       "project-1",
		BeadID:          "hp-v0g",
		Scope:           schemas.ThisBead,
		RiskClass:       schemas.High,
	})
	if err != nil {
		t.Fatalf("Request: %v", err)
	}
	_, err = queue.Approve(ctx, approval.Id, schemas.ApprovalDecisionRequest{
		DecisionActor: actor(schemas.ActorKindUser, "user-1"),
	})
	if err != nil {
		t.Fatalf("Approve: %v", err)
	}

	got, err := queue.Check(ctx, CheckRequest{
		RequestedAction: commandSpec("git.push_branch", "push-1"),
		ProjectID:       "project-1",
		BeadID:          "hp-v0g",
	})
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if !got.Allowed || got.ApprovalID != approval.Id || got.Scope != schemas.ThisBead {
		t.Fatalf("check = %+v, want approved bead-scoped result", got)
	}
	miss, err := queue.Check(ctx, CheckRequest{
		RequestedAction: commandSpec("git.push_branch", "push-1"),
		ProjectID:       "project-1",
		BeadID:          "hp-other",
	})
	if err != nil {
		t.Fatalf("Check miss: %v", err)
	}
	if miss.Allowed {
		t.Fatalf("other bead unexpectedly allowed: %+v", miss)
	}
}

func TestIngestDCGRequiresConfirmationQueuesPendingApproval(t *testing.T) {
	ctx := context.Background()
	now := fixedTime()
	queue := testQueue(&now)
	entry := dcgadapter.ApprovalSourceEntry{
		Source:     "dcg:core.git:push-force",
		Actor:      "agent.worker.1",
		Command:    "git push --force",
		Decision:   dcgadapter.DecisionRequiresConfirmation,
		Approvable: true,
		Severity:   "high",
		Reason:     "force-push requires confirmation",
		Evidence: dcgadapter.Evidence{
			RuleID:        "core.git:push-force",
			SchemaVersion: 2,
		},
	}

	result, err := queue.IngestDCG(ctx, DCGIngestRequest{Entry: entry, ProjectID: "project-1"})
	if err != nil {
		t.Fatalf("IngestDCG: %v", err)
	}
	if result.Action != DCGIngestQueued || result.Approval == nil {
		t.Fatalf("result = %+v, want queued approval", result)
	}
	approval := result.Approval
	if approval.Source != schemas.ApprovalSourceDcg {
		t.Fatalf("source = %s, want dcg", approval.Source)
	}
	if approval.PolicyRule == nil || *approval.PolicyRule != "dcg:core.git:push-force" {
		t.Fatalf("policy rule = %v", approval.PolicyRule)
	}
	if approval.RequestActor.Kind != schemas.ActorKindAgent || approval.RequestActor.Id == nil || *approval.RequestActor.Id != "agent.worker.1" {
		t.Fatalf("actor = %+v", approval.RequestActor)
	}
	if approval.RequestedAction.Target["commandSha256"] == "" {
		t.Fatalf("expected command hash target, got %+v", approval.RequestedAction.Target)
	}
	if _, ok := approval.RequestedAction.Target["command"]; ok {
		t.Fatalf("raw command leaked into target: %+v", approval.RequestedAction.Target)
	}
}

func TestIngestDCGAllowedDoesNotCreateApproval(t *testing.T) {
	ctx := context.Background()
	now := fixedTime()
	queue := testQueue(&now)

	result, err := queue.IngestDCG(ctx, DCGIngestRequest{
		Entry: dcgadapter.ApprovalSourceEntry{
			Source:   "dcg",
			Command:  "git status",
			Decision: dcgadapter.DecisionAllowed,
			Final:    true,
		},
	})
	if err != nil {
		t.Fatalf("IngestDCG allowed: %v", err)
	}
	if result.Action != DCGIngestAllowed || result.Approval != nil {
		t.Fatalf("result = %+v, want allowed without approval", result)
	}
	items, err := queue.List(ctx, ListFilter{IncludeExpired: true})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(items) != 0 {
		t.Fatalf("items = %d, want 0", len(items))
	}
}

func TestIngestDCGBlockedRecordsDeniedApproval(t *testing.T) {
	ctx := context.Background()
	now := fixedTime()
	queue := testQueue(&now)

	result, err := queue.IngestDCG(ctx, DCGIngestRequest{
		Entry: dcgadapter.ApprovalSourceEntry{
			Source:   "dcg:core.git:reset-hard",
			Actor:    "agent.worker.2",
			Command:  "git reset --hard HEAD~1",
			Decision: dcgadapter.DecisionBlocked,
			Final:    true,
			Severity: "critical",
			Reason:   "hard reset destroys uncommitted work",
			Evidence: dcgadapter.Evidence{
				RuleID:        "core.git:reset-hard",
				SchemaVersion: 2,
			},
		},
	})
	if err != nil {
		t.Fatalf("IngestDCG blocked: %v", err)
	}
	if result.Action != DCGIngestBlocked || result.Approval == nil {
		t.Fatalf("result = %+v, want blocked approval record", result)
	}
	if result.Approval.State != schemas.Denied {
		t.Fatalf("state = %s, want denied", result.Approval.State)
	}
	if result.Approval.DecisionActor == nil || result.Approval.DecisionActor.Kind != schemas.ActorKindDcg {
		t.Fatalf("decision actor = %+v, want dcg", result.Approval.DecisionActor)
	}
}

func TestRequireSLBDisabledDoesNotBlock(t *testing.T) {
	ctx := context.Background()
	now := fixedTime()
	queue := testQueue(&now)

	result, err := queue.RequireSLB(ctx, SLBRequest{
		Enabled:         false,
		RequestedAction: commandSpec("swarm.halt", "halt-1"),
		RequestActor:    actor(schemas.ActorKindUser, "user-1"),
	})
	if err != nil {
		t.Fatalf("RequireSLB disabled: %v", err)
	}
	if result.Required || result.Approval != nil {
		t.Fatalf("result = %+v, want no requirement", result)
	}
}

func TestRequireSLBEnabledQueuesHoopoePolicyApproval(t *testing.T) {
	ctx := context.Background()
	now := fixedTime()
	queue := testQueue(&now)

	result, err := queue.RequireSLB(ctx, SLBRequest{
		Enabled:         true,
		Class:           "destructive",
		RequestedAction: commandSpec("swarm.halt", "halt-1"),
		RequestActor:    actor(schemas.ActorKindUser, "user-1"),
		ProjectID:       "project-1",
	})
	if err != nil {
		t.Fatalf("RequireSLB enabled: %v", err)
	}
	if !result.Required || result.Approval == nil {
		t.Fatalf("result = %+v, want required approval", result)
	}
	if result.Approval.Source != schemas.ApprovalSourceHoopoePolicy {
		t.Fatalf("source = %s, want hoopoe_policy", result.Approval.Source)
	}
	if result.Approval.PolicyRule == nil || *result.Approval.PolicyRule != "slb:destructive" {
		t.Fatalf("policy rule = %v", result.Approval.PolicyRule)
	}
	if result.Approval.RiskClass != schemas.Critical {
		t.Fatalf("risk = %s, want critical", result.Approval.RiskClass)
	}
}

func TestAgentApprovalGateQueuesAndReusesApprovedAction(t *testing.T) {
	ctx := context.Background()
	now := fixedTime()
	queue := testQueue(&now)
	req := agent.ApprovalRequest{
		Plan: schemas.ActionPlan{
			JobId:     "tend-swarm",
			RunId:     "run-1",
			RiskClass: schemas.High,
			EvidenceRefs: &[]string{
				"audit:dry-run-1",
			},
		},
		Action: schemas.Action{
			Kind:           schemas.SwarmHalt,
			IdempotencyKey: "halt-1",
			Target: map[string]interface{}{
				"projectId": "project-1",
				"swarmId":   "swarm-1",
			},
		},
		Spec:        agent.DefaultActionCatalog()[schemas.SwarmHalt],
		DryRun:      agent.DryRunResult{Summary: "budget threshold crossed"},
		RequestedAt: now,
		Reason:      "operator requested halt",
	}

	decision, err := queue.RequestApproval(ctx, req)
	if err != nil {
		t.Fatalf("RequestApproval: %v", err)
	}
	if decision.State != agent.ApprovalPending || decision.ApprovalID != "appr_01" {
		t.Fatalf("decision = %+v, want pending appr_01", decision)
	}
	approval, ok, err := queue.Get(ctx, decision.ApprovalID)
	if err != nil || !ok {
		t.Fatalf("Get approval: approval=%+v ok=%v err=%v", approval, ok, err)
	}
	if approval.PolicyRule == nil || *approval.PolicyRule != "hoopoe-policy:swarm.halt" {
		t.Fatalf("policy rule = %v", approval.PolicyRule)
	}
	if approval.RequestActor.Kind != schemas.ActorKindAgent || approval.RequestActor.Id == nil || *approval.RequestActor.Id != "run-1" {
		t.Fatalf("request actor = %+v", approval.RequestActor)
	}
	if approval.SwarmId == nil || *approval.SwarmId != "swarm-1" {
		t.Fatalf("swarm id = %v", approval.SwarmId)
	}
	if approval.RiskClass != schemas.High {
		t.Fatalf("risk = %s, want high", approval.RiskClass)
	}

	if _, err := queue.Approve(ctx, decision.ApprovalID, schemas.ApprovalDecisionRequest{
		DecisionActor: actor(schemas.ActorKindUser, "user-1"),
	}); err != nil {
		t.Fatalf("Approve: %v", err)
	}
	decision, err = queue.RequestApproval(ctx, req)
	if err != nil {
		t.Fatalf("RequestApproval after approve: %v", err)
	}
	if decision.State != agent.ApprovalApproved || decision.ApprovalID != "appr_01" {
		t.Fatalf("approved decision = %+v, want approved appr_01", decision)
	}
}

func testQueue(now *time.Time) *Queue {
	counter := 0
	return NewQueue(Config{
		Now: func() time.Time {
			return *now
		},
		NewID: func(Request) (string, error) {
			counter++
			return fmt.Sprintf("appr_%02d", counter), nil
		},
	})
}

func fixedTime() time.Time {
	return time.Date(2026, 5, 4, 12, 0, 0, 0, time.UTC)
}

func commandSpec(kind string, key string) schemas.CommandSpec {
	return schemas.CommandSpec{
		Kind:           kind,
		IdempotencyKey: stringPtr(key),
		Target: map[string]interface{}{
			"projectId": "project-1",
			"branch":    "main",
		},
	}
}

func actor(kind schemas.ActorKind, id string) schemas.Actor {
	return schemas.Actor{Kind: kind, Id: stringPtr(id)}
}

func stringPtr(value string) *string {
	return &value
}
