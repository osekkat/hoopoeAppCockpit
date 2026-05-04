// Package approvals owns Hoopoe's unified approval queue.
//
// The queue is source-agnostic: Hoopoe policy gates, DCG verdicts, and optional
// SLB co-signature requirements all land as the same schema.Approval record.
package approvals

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	dcgadapter "github.com/hoopoe-cockpit/hoopoe/apps/daemon/internal/adapters/dcg"
	schemas "github.com/hoopoe-cockpit/hoopoe/packages/schemas/go"
)

const (
	SchemaVersion schemas.SchemaVersion = 1

	DefaultTTL = 15 * time.Minute

	PolicyRulePrefixDCG    = "dcg:"
	PolicyRulePrefixHoopoe = "hoopoe-policy:"
	PolicyRulePrefixSLB    = "slb:"

	defaultDCGActionKind = "dcg.guarded_command"
)

var (
	ErrInvalidRequest    = errors.New("approvals: invalid request")
	ErrNotFound          = errors.New("approvals: approval not found")
	ErrInvalidTransition = errors.New("approvals: invalid state transition")
	ErrExpired           = errors.New("approvals: approval expired")
)

type IDGenerator func(Request) (string, error)

type Config struct {
	Store      Store
	Now        func() time.Time
	NewID      IDGenerator
	DefaultTTL time.Duration
}

type Queue struct {
	mu         sync.Mutex
	store      Store
	now        func() time.Time
	newID      IDGenerator
	defaultTTL time.Duration
}

func NewQueue(config Config) *Queue {
	store := config.Store
	if store == nil {
		store = NewMemoryStore()
	}
	now := config.Now
	if now == nil {
		now = time.Now
	}
	newID := config.NewID
	if newID == nil {
		newID = randomApprovalID
	}
	defaultTTL := config.DefaultTTL
	if defaultTTL <= 0 {
		defaultTTL = DefaultTTL
	}
	return &Queue{
		store:      store,
		now:        now,
		newID:      newID,
		defaultTTL: defaultTTL,
	}
}

type Store interface {
	Save(ctx context.Context, approval schemas.Approval) error
	Get(ctx context.Context, id string) (schemas.Approval, bool, error)
	List(ctx context.Context) ([]schemas.Approval, error)
}

type MemoryStore struct {
	mu    sync.Mutex
	items map[string]schemas.Approval
	order []string
}

func NewMemoryStore() *MemoryStore {
	return &MemoryStore{items: make(map[string]schemas.Approval)}
}

func (s *MemoryStore) Save(_ context.Context, approval schemas.Approval) error {
	if s == nil {
		return fmt.Errorf("%w: nil store", ErrInvalidRequest)
	}
	if strings.TrimSpace(approval.Id) == "" {
		return fmt.Errorf("%w: approval id is required", ErrInvalidRequest)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.items == nil {
		s.items = make(map[string]schemas.Approval)
	}
	if _, exists := s.items[approval.Id]; !exists {
		s.order = append(s.order, approval.Id)
	}
	s.items[approval.Id] = cloneApproval(approval)
	return nil
}

func (s *MemoryStore) Get(_ context.Context, id string) (schemas.Approval, bool, error) {
	if s == nil {
		return schemas.Approval{}, false, fmt.Errorf("%w: nil store", ErrInvalidRequest)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	approval, ok := s.items[id]
	if !ok {
		return schemas.Approval{}, false, nil
	}
	return cloneApproval(approval), true, nil
}

func (s *MemoryStore) List(_ context.Context) ([]schemas.Approval, error) {
	if s == nil {
		return nil, fmt.Errorf("%w: nil store", ErrInvalidRequest)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	items := make([]schemas.Approval, 0, len(s.order))
	for _, id := range s.order {
		if approval, ok := s.items[id]; ok {
			items = append(items, cloneApproval(approval))
		}
	}
	return items, nil
}

type Request struct {
	Source          schemas.ApprovalSource
	PolicyRule      string
	RequestedAction schemas.CommandSpec
	RequestActor    schemas.Actor
	Reason          string
	EvidenceRefs    []string
	ProjectID       string
	BeadID          string
	AgentID         string
	SwarmID         string
	Scope           schemas.ApprovalScope
	RiskClass       schemas.ApprovalRiskClass
	ExpiresAt       *time.Time
	RequestedAt     time.Time
	IdempotencyKey  string
}

func (q *Queue) Request(ctx context.Context, req Request) (schemas.Approval, bool, error) {
	if q == nil {
		return schemas.Approval{}, false, fmt.Errorf("%w: nil queue", ErrInvalidRequest)
	}
	normalized, err := q.normalizeRequest(req)
	if err != nil {
		return schemas.Approval{}, false, err
	}
	q.mu.Lock()
	defer q.mu.Unlock()
	if existing, ok, err := q.findExistingLocked(ctx, normalized); err != nil {
		return schemas.Approval{}, false, err
	} else if ok {
		return existing, false, nil
	}
	id, err := q.newID(normalized)
	if err != nil {
		return schemas.Approval{}, false, fmt.Errorf("approvals: generate id: %w", err)
	}
	id = strings.TrimSpace(id)
	if id == "" {
		return schemas.Approval{}, false, fmt.Errorf("%w: generated approval id is empty", ErrInvalidRequest)
	}
	approval := schemas.Approval{
		Id:              id,
		SchemaVersion:   SchemaVersion,
		Source:          normalized.Source,
		PolicyRule:      stringPtrOrNil(normalized.PolicyRule),
		RequestedAction: normalized.RequestedAction,
		RequestActor:    normalized.RequestActor,
		Reason:          stringPtrOrNil(normalized.Reason),
		EvidenceRefs:    stringSlicePtrOrNil(normalized.EvidenceRefs),
		ProjectId:       stringPtrOrNil(normalized.ProjectID),
		BeadId:          stringPtrOrNil(normalized.BeadID),
		AgentId:         stringPtrOrNil(normalized.AgentID),
		SwarmId:         stringPtrOrNil(normalized.SwarmID),
		RequestedAt:     normalized.RequestedAt,
		ExpiresAt:       cloneTimePtr(normalized.ExpiresAt),
		RiskClass:       normalized.RiskClass,
		Scope:           normalized.Scope,
		State:           schemas.Pending,
	}
	if err := q.store.Save(ctx, approval); err != nil {
		return schemas.Approval{}, false, err
	}
	return cloneApproval(approval), true, nil
}

func (q *Queue) Get(ctx context.Context, id string) (schemas.Approval, bool, error) {
	if q == nil {
		return schemas.Approval{}, false, fmt.Errorf("%w: nil queue", ErrInvalidRequest)
	}
	q.mu.Lock()
	defer q.mu.Unlock()
	approval, ok, err := q.store.Get(ctx, strings.TrimSpace(id))
	if err != nil || !ok {
		return approval, ok, err
	}
	approval, err = q.expireIfNeededLocked(ctx, approval, q.now())
	if err != nil {
		return schemas.Approval{}, false, err
	}
	return cloneApproval(approval), true, nil
}

type ListFilter struct {
	ProjectID      string
	Source         *schemas.ApprovalSource
	States         []schemas.ApprovalState
	IncludeExpired bool
}

func (q *Queue) List(ctx context.Context, filter ListFilter) ([]schemas.Approval, error) {
	if q == nil {
		return nil, fmt.Errorf("%w: nil queue", ErrInvalidRequest)
	}
	q.mu.Lock()
	defer q.mu.Unlock()
	items, err := q.listLocked(ctx)
	if err != nil {
		return nil, err
	}
	stateSet := make(map[schemas.ApprovalState]struct{}, len(filter.States))
	for _, state := range filter.States {
		stateSet[state] = struct{}{}
	}
	out := make([]schemas.Approval, 0, len(items))
	for _, approval := range items {
		if filter.ProjectID != "" && stringValue(approval.ProjectId) != filter.ProjectID {
			continue
		}
		if filter.Source != nil && approval.Source != *filter.Source {
			continue
		}
		if !filter.IncludeExpired && approval.State == schemas.Expired {
			continue
		}
		if len(stateSet) > 0 {
			if _, ok := stateSet[approval.State]; !ok {
				continue
			}
		}
		out = append(out, cloneApproval(approval))
	}
	return out, nil
}

func (q *Queue) Approve(ctx context.Context, id string, decision schemas.ApprovalDecisionRequest) (schemas.Approval, error) {
	return q.decide(ctx, id, schemas.Approved, decision)
}

func (q *Queue) Deny(ctx context.Context, id string, decision schemas.ApprovalDecisionRequest) (schemas.Approval, error) {
	return q.decide(ctx, id, schemas.Denied, decision)
}

func (q *Queue) Revoke(ctx context.Context, id string, decision schemas.ApprovalDecisionRequest) (schemas.Approval, error) {
	return q.decide(ctx, id, schemas.Revoked, decision)
}

func (q *Queue) ConsumeOnce(ctx context.Context, id string, decision schemas.ApprovalDecisionRequest) (schemas.Approval, error) {
	if q == nil {
		return schemas.Approval{}, fmt.Errorf("%w: nil queue", ErrInvalidRequest)
	}
	if !decision.DecisionActor.Kind.Valid() {
		return schemas.Approval{}, fmt.Errorf("%w: decision actor kind is required", ErrInvalidRequest)
	}
	q.mu.Lock()
	defer q.mu.Unlock()
	approval, ok, err := q.store.Get(ctx, strings.TrimSpace(id))
	if err != nil {
		return schemas.Approval{}, err
	}
	if !ok {
		return schemas.Approval{}, ErrNotFound
	}
	approval, err = q.expireIfNeededLocked(ctx, approval, q.now())
	if err != nil {
		return schemas.Approval{}, err
	}
	if approval.State == schemas.Expired {
		return schemas.Approval{}, ErrExpired
	}
	if approval.State != schemas.Approved {
		return schemas.Approval{}, fmt.Errorf("%w: approval is %s", ErrInvalidTransition, approval.State)
	}
	if approval.Scope != schemas.Once {
		return schemas.Approval{}, fmt.Errorf("%w: cannot consume %s approval", ErrInvalidTransition, approval.Scope)
	}
	consumedAt := q.now().UTC()
	approval.State = schemas.Revoked
	approval.DecidedAt = &consumedAt
	approval.DecisionActor = &decision.DecisionActor
	approval.DecisionNote = cloneStringPtr(decision.Note)
	if err := q.store.Save(ctx, approval); err != nil {
		return schemas.Approval{}, err
	}
	return cloneApproval(approval), nil
}

func (q *Queue) Extend(ctx context.Context, id string, expiresAt time.Time) (schemas.Approval, error) {
	if q == nil {
		return schemas.Approval{}, fmt.Errorf("%w: nil queue", ErrInvalidRequest)
	}
	if expiresAt.IsZero() {
		return schemas.Approval{}, fmt.Errorf("%w: expiresAt is required", ErrInvalidRequest)
	}
	q.mu.Lock()
	defer q.mu.Unlock()
	approval, ok, err := q.store.Get(ctx, strings.TrimSpace(id))
	if err != nil {
		return schemas.Approval{}, err
	}
	if !ok {
		return schemas.Approval{}, ErrNotFound
	}
	approval, err = q.expireIfNeededLocked(ctx, approval, q.now())
	if err != nil {
		return schemas.Approval{}, err
	}
	if approval.State == schemas.Denied || approval.State == schemas.Revoked || approval.State == schemas.Expired {
		return schemas.Approval{}, fmt.Errorf("%w: cannot extend %s approval", ErrInvalidTransition, approval.State)
	}
	expiresAt = expiresAt.UTC()
	approval.ExpiresAt = &expiresAt
	if err := q.store.Save(ctx, approval); err != nil {
		return schemas.Approval{}, err
	}
	return cloneApproval(approval), nil
}

type CheckRequest struct {
	RequestedAction schemas.CommandSpec
	ProjectID       string
	BeadID          string
	AgentID         string
	SwarmID         string
}

type CheckResult struct {
	Allowed    bool
	ApprovalID string
	Scope      schemas.ApprovalScope
	Reason     string
}

func (q *Queue) Check(ctx context.Context, req CheckRequest) (CheckResult, error) {
	if q == nil {
		return CheckResult{}, fmt.Errorf("%w: nil queue", ErrInvalidRequest)
	}
	q.mu.Lock()
	defer q.mu.Unlock()
	items, err := q.listLocked(ctx)
	if err != nil {
		return CheckResult{}, err
	}
	for _, approval := range items {
		if approval.State != schemas.Approved {
			continue
		}
		if approvalCovers(approval, req) {
			return CheckResult{Allowed: true, ApprovalID: approval.Id, Scope: approval.Scope}, nil
		}
	}
	return CheckResult{Reason: "no approved approval covers the requested action"}, nil
}

type DCGIngestAction string

const (
	DCGIngestAllowed DCGIngestAction = "allowed"
	DCGIngestQueued  DCGIngestAction = "queued"
	DCGIngestBlocked DCGIngestAction = "blocked"
)

type DCGIngestRequest struct {
	Entry           dcgadapter.ApprovalSourceEntry
	RequestedAction schemas.CommandSpec
	ProjectID       string
	BeadID          string
	AgentID         string
	SwarmID         string
	Scope           schemas.ApprovalScope
	RiskClass       schemas.ApprovalRiskClass
	ExpiresAt       *time.Time
	IdempotencyKey  string
}

type DCGIngestResult struct {
	Action   DCGIngestAction
	Approval *schemas.Approval
}

func (q *Queue) IngestDCG(ctx context.Context, req DCGIngestRequest) (DCGIngestResult, error) {
	decision := req.Entry.Decision
	switch decision {
	case dcgadapter.DecisionAllowed:
		return DCGIngestResult{Action: DCGIngestAllowed}, nil
	case dcgadapter.DecisionRequiresConfirmation:
		approval, _, err := q.Request(ctx, dcgRequestToApprovalRequest(req))
		if err != nil {
			return DCGIngestResult{}, err
		}
		return DCGIngestResult{Action: DCGIngestQueued, Approval: &approval}, nil
	case dcgadapter.DecisionBlocked:
		approval, _, err := q.Request(ctx, dcgRequestToApprovalRequest(req))
		if err != nil {
			return DCGIngestResult{}, err
		}
		decisionActor := schemas.Actor{Kind: schemas.ActorKindDcg, Id: stringPtrOrNil("dcg")}
		denied, err := q.Deny(ctx, approval.Id, schemas.ApprovalDecisionRequest{
			DecisionActor: decisionActor,
			Note:          stringPtrOrNil("DCG blocked the command before execution"),
		})
		if err != nil {
			return DCGIngestResult{}, err
		}
		return DCGIngestResult{Action: DCGIngestBlocked, Approval: &denied}, nil
	default:
		return DCGIngestResult{}, fmt.Errorf("%w: unknown DCG decision %q", ErrInvalidRequest, decision)
	}
}

type SLBRequest struct {
	Enabled         bool
	Class           string
	RequestedAction schemas.CommandSpec
	RequestActor    schemas.Actor
	Reason          string
	EvidenceRefs    []string
	ProjectID       string
	BeadID          string
	AgentID         string
	SwarmID         string
	Scope           schemas.ApprovalScope
	RiskClass       schemas.ApprovalRiskClass
	ExpiresAt       *time.Time
	IdempotencyKey  string
}

type SLBResult struct {
	Required bool
	Approval *schemas.Approval
}

func (q *Queue) RequireSLB(ctx context.Context, req SLBRequest) (SLBResult, error) {
	if !req.Enabled {
		return SLBResult{}, nil
	}
	class := strings.TrimSpace(req.Class)
	if class == "" {
		class = "destructive"
	}
	risk := req.RiskClass
	if risk == "" {
		risk = schemas.Critical
	}
	scope := req.Scope
	if scope == "" {
		scope = schemas.Once
	}
	reason := strings.TrimSpace(req.Reason)
	if reason == "" {
		reason = "SLB co-signature is required for this action class"
	}
	approval, _, err := q.Request(ctx, Request{
		Source:          schemas.ApprovalSourceHoopoePolicy,
		PolicyRule:      PolicyRulePrefixSLB + class,
		RequestedAction: req.RequestedAction,
		RequestActor:    req.RequestActor,
		Reason:          reason,
		EvidenceRefs:    req.EvidenceRefs,
		ProjectID:       req.ProjectID,
		BeadID:          req.BeadID,
		AgentID:         req.AgentID,
		SwarmID:         req.SwarmID,
		Scope:           scope,
		RiskClass:       risk,
		ExpiresAt:       req.ExpiresAt,
		IdempotencyKey:  req.IdempotencyKey,
	})
	if err != nil {
		return SLBResult{}, err
	}
	return SLBResult{Required: true, Approval: &approval}, nil
}

func (q *Queue) decide(ctx context.Context, id string, state schemas.ApprovalState, decision schemas.ApprovalDecisionRequest) (schemas.Approval, error) {
	if q == nil {
		return schemas.Approval{}, fmt.Errorf("%w: nil queue", ErrInvalidRequest)
	}
	if state != schemas.Approved && state != schemas.Denied && state != schemas.Revoked {
		return schemas.Approval{}, fmt.Errorf("%w: unsupported decision state %s", ErrInvalidRequest, state)
	}
	if !decision.DecisionActor.Kind.Valid() {
		return schemas.Approval{}, fmt.Errorf("%w: decision actor kind is required", ErrInvalidRequest)
	}
	q.mu.Lock()
	defer q.mu.Unlock()
	approval, ok, err := q.store.Get(ctx, strings.TrimSpace(id))
	if err != nil {
		return schemas.Approval{}, err
	}
	if !ok {
		return schemas.Approval{}, ErrNotFound
	}
	approval, err = q.expireIfNeededLocked(ctx, approval, q.now())
	if err != nil {
		return schemas.Approval{}, err
	}
	if approval.State != schemas.Pending {
		if approval.State == schemas.Expired {
			return schemas.Approval{}, ErrExpired
		}
		return schemas.Approval{}, fmt.Errorf("%w: approval is %s", ErrInvalidTransition, approval.State)
	}
	decidedAt := q.now().UTC()
	approval.State = state
	approval.DecidedAt = &decidedAt
	approval.DecisionActor = &decision.DecisionActor
	approval.DecisionNote = cloneStringPtr(decision.Note)
	if decision.Scope != nil {
		if !decision.Scope.Valid() {
			return schemas.Approval{}, fmt.Errorf("%w: invalid approval scope %q", ErrInvalidRequest, *decision.Scope)
		}
		approval.Scope = *decision.Scope
	}
	if err := q.store.Save(ctx, approval); err != nil {
		return schemas.Approval{}, err
	}
	return cloneApproval(approval), nil
}

func (q *Queue) normalizeRequest(req Request) (Request, error) {
	req.Source = defaultSource(req.Source)
	if !req.Source.Valid() {
		return Request{}, fmt.Errorf("%w: invalid approval source %q", ErrInvalidRequest, req.Source)
	}
	if req.RequestedAction.Kind == "" {
		return Request{}, fmt.Errorf("%w: requested action kind is required", ErrInvalidRequest)
	}
	if req.RequestedAction.Target == nil {
		req.RequestedAction.Target = map[string]interface{}{}
	}
	if !req.RequestActor.Kind.Valid() {
		return Request{}, fmt.Errorf("%w: request actor kind is required", ErrInvalidRequest)
	}
	req.Scope = defaultScope(req.Scope)
	if !req.Scope.Valid() {
		return Request{}, fmt.Errorf("%w: invalid approval scope %q", ErrInvalidRequest, req.Scope)
	}
	req.RiskClass = defaultRisk(req.RiskClass)
	if !req.RiskClass.Valid() {
		return Request{}, fmt.Errorf("%w: invalid risk class %q", ErrInvalidRequest, req.RiskClass)
	}
	if req.RequestedAt.IsZero() {
		req.RequestedAt = q.now()
	}
	req.RequestedAt = req.RequestedAt.UTC()
	expiresAt := req.ExpiresAt
	if expiresAt == nil || expiresAt.IsZero() {
		value := req.RequestedAt.Add(q.defaultTTL).UTC()
		expiresAt = &value
	} else {
		value := expiresAt.UTC()
		expiresAt = &value
	}
	req.ExpiresAt = expiresAt
	if !req.ExpiresAt.After(req.RequestedAt) {
		return Request{}, fmt.Errorf("%w: expiresAt must be after requestedAt", ErrInvalidRequest)
	}
	req.PolicyRule = strings.TrimSpace(req.PolicyRule)
	if req.PolicyRule == "" {
		req.PolicyRule = defaultPolicyRule(req.Source)
	}
	req.IdempotencyKey = strings.TrimSpace(req.IdempotencyKey)
	if req.IdempotencyKey == "" {
		req.IdempotencyKey = commandIdempotencyKey(req.RequestedAction)
	}
	if req.IdempotencyKey == "" {
		req.IdempotencyKey = derivedIdempotencyKey(req)
	}
	req.RequestedAction.IdempotencyKey = stringPtrOrNil(req.IdempotencyKey)
	return req, nil
}

func (q *Queue) findExistingLocked(ctx context.Context, req Request) (schemas.Approval, bool, error) {
	items, err := q.listLocked(ctx)
	if err != nil {
		return schemas.Approval{}, false, err
	}
	for _, approval := range items {
		if approval.State == schemas.Denied || approval.State == schemas.Revoked || approval.State == schemas.Expired {
			continue
		}
		if approval.Source != req.Source {
			continue
		}
		if stringValue(approval.PolicyRule) != req.PolicyRule {
			continue
		}
		if commandIdempotencyKey(approval.RequestedAction) == req.IdempotencyKey {
			return cloneApproval(approval), true, nil
		}
	}
	return schemas.Approval{}, false, nil
}

func (q *Queue) listLocked(ctx context.Context) ([]schemas.Approval, error) {
	items, err := q.store.List(ctx)
	if err != nil {
		return nil, err
	}
	now := q.now()
	out := make([]schemas.Approval, 0, len(items))
	for _, approval := range items {
		updated, err := q.expireIfNeededLocked(ctx, approval, now)
		if err != nil {
			return nil, err
		}
		out = append(out, updated)
	}
	return out, nil
}

func (q *Queue) expireIfNeededLocked(ctx context.Context, approval schemas.Approval, now time.Time) (schemas.Approval, error) {
	if approval.ExpiresAt == nil {
		return approval, nil
	}
	if approval.State != schemas.Pending && approval.State != schemas.Approved {
		return approval, nil
	}
	if now.UTC().Before(approval.ExpiresAt.UTC()) {
		return approval, nil
	}
	approval.State = schemas.Expired
	if err := q.store.Save(ctx, approval); err != nil {
		return schemas.Approval{}, err
	}
	return approval, nil
}

func approvalCovers(approval schemas.Approval, req CheckRequest) bool {
	if !sameCommandApproval(approval.RequestedAction, req.RequestedAction, approval.Scope) {
		return false
	}
	switch approval.Scope {
	case schemas.Once:
		return true
	case schemas.ThisBead:
		return stringValue(approval.ProjectId) != "" &&
			stringValue(approval.ProjectId) == req.ProjectID &&
			stringValue(approval.BeadId) != "" &&
			stringValue(approval.BeadId) == req.BeadID
	case schemas.ThisSwarm:
		return stringValue(approval.ProjectId) != "" &&
			stringValue(approval.ProjectId) == req.ProjectID &&
			stringValue(approval.SwarmId) != "" &&
			stringValue(approval.SwarmId) == req.SwarmID
	case schemas.ThisProjectSession:
		return stringValue(approval.ProjectId) != "" &&
			stringValue(approval.ProjectId) == req.ProjectID
	default:
		return false
	}
}

func sameCommandApproval(approved schemas.CommandSpec, requested schemas.CommandSpec, scope schemas.ApprovalScope) bool {
	if approved.Kind != requested.Kind {
		return false
	}
	if scope == schemas.Once {
		approvedKey := commandIdempotencyKey(approved)
		requestedKey := commandIdempotencyKey(requested)
		if approvedKey != "" || requestedKey != "" {
			return approvedKey != "" && approvedKey == requestedKey
		}
	}
	return canonicalJSON(approved.Target) == canonicalJSON(requested.Target)
}

func dcgRequestToApprovalRequest(req DCGIngestRequest) Request {
	entry := req.Entry
	action := req.RequestedAction
	if action.Kind == "" {
		action = dcgCommandSpec(entry)
	}
	reason := strings.TrimSpace(entry.Reason)
	if reason == "" {
		reason = fmt.Sprintf("DCG verdict %q from %s", entry.Decision, sourceOrDefault(entry.Source, "dcg"))
	}
	risk := req.RiskClass
	if risk == "" {
		risk = riskFromDCGSeverity(entry.Severity, entry.Decision)
	}
	actor := actorFromDCG(entry.Actor)
	return Request{
		Source:          schemas.ApprovalSourceDcg,
		PolicyRule:      sourceOrDefault(entry.Source, "dcg"),
		RequestedAction: action,
		RequestActor:    actor,
		Reason:          reason,
		EvidenceRefs:    dcgEvidenceRefs(entry),
		ProjectID:       req.ProjectID,
		BeadID:          req.BeadID,
		AgentID:         req.AgentID,
		SwarmID:         req.SwarmID,
		Scope:           defaultScope(req.Scope),
		RiskClass:       risk,
		ExpiresAt:       req.ExpiresAt,
		IdempotencyKey:  firstNonEmpty(req.IdempotencyKey, entry.Source+"|"+entry.Command+"|"+string(entry.Decision)),
	}
}

func dcgCommandSpec(entry dcgadapter.ApprovalSourceEntry) schemas.CommandSpec {
	target := map[string]interface{}{"source": sourceOrDefault(entry.Source, "dcg")}
	if strings.TrimSpace(entry.Command) != "" {
		target["commandSha256"] = sha256Hex(entry.Command)
	}
	return schemas.CommandSpec{
		Kind:   defaultDCGActionKind,
		Target: target,
	}
}

func dcgEvidenceRefs(entry dcgadapter.ApprovalSourceEntry) []string {
	refs := make([]string, 0, 4)
	if entry.Source != "" {
		refs = append(refs, "dcg:source:"+entry.Source)
	}
	if entry.Evidence.RuleID != "" {
		refs = append(refs, "dcg:rule:"+entry.Evidence.RuleID)
	}
	if entry.Evidence.PackID != "" {
		refs = append(refs, "dcg:pack:"+entry.Evidence.PackID)
	}
	if entry.Evidence.SchemaVersion > 0 {
		refs = append(refs, fmt.Sprintf("dcg:schema:%d", entry.Evidence.SchemaVersion))
	}
	return refs
}

func riskFromDCGSeverity(severity string, decision dcgadapter.Decision) schemas.ApprovalRiskClass {
	switch strings.ToLower(strings.TrimSpace(severity)) {
	case "critical", "destructive":
		return schemas.Critical
	case "high":
		return schemas.High
	case "medium", "moderate":
		return schemas.Medium
	case "low", "info", "informational":
		return schemas.Low
	default:
		if decision == dcgadapter.DecisionBlocked {
			return schemas.Critical
		}
		if decision == dcgadapter.DecisionRequiresConfirmation {
			return schemas.High
		}
		return schemas.Low
	}
}

func actorFromDCG(actor string) schemas.Actor {
	actor = strings.TrimSpace(actor)
	if actor == "" {
		return schemas.Actor{Kind: schemas.ActorKindDcg, Id: stringPtrOrNil("dcg")}
	}
	return schemas.Actor{Kind: schemas.ActorKindAgent, Id: &actor}
}

func defaultSource(source schemas.ApprovalSource) schemas.ApprovalSource {
	if source == "" {
		return schemas.ApprovalSourceHoopoePolicy
	}
	return source
}

func defaultScope(scope schemas.ApprovalScope) schemas.ApprovalScope {
	if scope == "" {
		return schemas.Once
	}
	return scope
}

func defaultRisk(risk schemas.ApprovalRiskClass) schemas.ApprovalRiskClass {
	if risk == "" {
		return schemas.High
	}
	return risk
}

func defaultPolicyRule(source schemas.ApprovalSource) string {
	if source == schemas.ApprovalSourceDcg {
		return "dcg"
	}
	return PolicyRulePrefixHoopoe + "unspecified"
}

func commandIdempotencyKey(spec schemas.CommandSpec) string {
	if spec.IdempotencyKey == nil {
		return ""
	}
	return strings.TrimSpace(*spec.IdempotencyKey)
}

func derivedIdempotencyKey(req Request) string {
	payload := map[string]interface{}{
		"source":    req.Source,
		"rule":      req.PolicyRule,
		"kind":      req.RequestedAction.Kind,
		"target":    req.RequestedAction.Target,
		"projectId": req.ProjectID,
		"beadId":    req.BeadID,
		"agentId":   req.AgentID,
		"swarmId":   req.SwarmID,
	}
	return sha256Hex(canonicalJSON(payload))
}

func randomApprovalID(Request) (string, error) {
	var buf [12]byte
	if _, err := rand.Read(buf[:]); err != nil {
		return "", err
	}
	return "appr_" + hex.EncodeToString(buf[:]), nil
}

func sha256Hex(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}

func canonicalJSON(value interface{}) string {
	data, err := json.Marshal(value)
	if err != nil {
		return fmt.Sprintf("%#v", value)
	}
	return string(data)
}

func sourceOrDefault(source string, fallback string) string {
	source = strings.TrimSpace(source)
	if source == "" {
		return fallback
	}
	return source
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" {
			return value
		}
	}
	return ""
}

func cloneApproval(approval schemas.Approval) schemas.Approval {
	approval.AgentId = cloneStringPtr(approval.AgentId)
	approval.BeadId = cloneStringPtr(approval.BeadId)
	approval.DecidedAt = cloneTimePtr(approval.DecidedAt)
	approval.DecisionActor = cloneActorPtr(approval.DecisionActor)
	approval.DecisionNote = cloneStringPtr(approval.DecisionNote)
	approval.EvidenceRefs = cloneStringSlicePtr(approval.EvidenceRefs)
	approval.ExpiresAt = cloneTimePtr(approval.ExpiresAt)
	approval.PolicyRule = cloneStringPtr(approval.PolicyRule)
	approval.ProjectId = cloneStringPtr(approval.ProjectId)
	approval.Reason = cloneStringPtr(approval.Reason)
	approval.RequestActor = cloneActor(approval.RequestActor)
	approval.RequestedAction = cloneCommandSpec(approval.RequestedAction)
	approval.SwarmId = cloneStringPtr(approval.SwarmId)
	return approval
}

func cloneCommandSpec(spec schemas.CommandSpec) schemas.CommandSpec {
	spec.IdempotencyKey = cloneStringPtr(spec.IdempotencyKey)
	if spec.Args != nil {
		args := cloneAnyMap(*spec.Args)
		spec.Args = &args
	}
	spec.Target = cloneAnyMap(spec.Target)
	spec.Preconditions = cloneStringSlicePtr(spec.Preconditions)
	spec.Postconditions = cloneStringSlicePtr(spec.Postconditions)
	return spec
}

func cloneActor(actor schemas.Actor) schemas.Actor {
	actor.DisplayName = cloneStringPtr(actor.DisplayName)
	actor.Id = cloneStringPtr(actor.Id)
	return actor
}

func cloneActorPtr(actor *schemas.Actor) *schemas.Actor {
	if actor == nil {
		return nil
	}
	clone := cloneActor(*actor)
	return &clone
}

func cloneAnyMap(in map[string]interface{}) map[string]interface{} {
	if in == nil {
		return nil
	}
	out := make(map[string]interface{}, len(in))
	for key, value := range in {
		out[key] = cloneAny(value)
	}
	return out
}

func cloneAny(value interface{}) interface{} {
	switch typed := value.(type) {
	case map[string]interface{}:
		return cloneAnyMap(typed)
	case []interface{}:
		out := make([]interface{}, len(typed))
		for i, item := range typed {
			out[i] = cloneAny(item)
		}
		return out
	case []string:
		out := make([]string, len(typed))
		copy(out, typed)
		return out
	default:
		return typed
	}
}

func cloneStringPtr(value *string) *string {
	if value == nil {
		return nil
	}
	clone := *value
	return &clone
}

func cloneTimePtr(value *time.Time) *time.Time {
	if value == nil {
		return nil
	}
	clone := value.UTC()
	return &clone
}

func cloneStringSlicePtr(value *[]string) *[]string {
	if value == nil {
		return nil
	}
	return stringSlicePtrOrNil(*value)
}

func stringPtrOrNil(value string) *string {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil
	}
	return &value
}

func stringSlicePtrOrNil(values []string) *[]string {
	if len(values) == 0 {
		return nil
	}
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" {
			out = append(out, value)
		}
	}
	if len(out) == 0 {
		return nil
	}
	return &out
}

func stringValue(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}
