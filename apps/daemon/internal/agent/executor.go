package agent

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	schemas "github.com/hoopoe-cockpit/hoopoe/packages/schemas/go"
)

type ActionStatus string

const (
	ActionStatusDryRunOK              ActionStatus = "dry_run_ok"
	ActionStatusCapabilityUnavailable ActionStatus = "capability_unavailable"
	ActionStatusApprovalRequired      ActionStatus = "approval_required"
	ActionStatusApprovalDenied        ActionStatus = "approval_denied"
	ActionStatusExecuted              ActionStatus = "executed"
	ActionStatusIdempotentReplay      ActionStatus = "idempotent_replay"
	ActionStatusPostconditionFailed   ActionStatus = "postcondition_failed"
	ActionStatusFailed                ActionStatus = "failed"
	ActionStatusSkippedAfterFailure   ActionStatus = "skipped_after_failure"
)

type ActionContext struct {
	Plan           schemas.ActionPlan
	Action         schemas.Action
	Spec           ActionSpec
	Index          int
	Preconditions  []string
	Postconditions []string
}

type DryRunResult struct {
	OK      bool
	Summary string
	Data    map[string]any
}

type ExecutionResult struct {
	OK           bool
	CanonicalRef string
	Summary      string
	Data         map[string]any
}

type PostconditionResult struct {
	OK           bool
	CanonicalRef string
	Summary      string
	Data         map[string]any
}

type FollowUpDetection struct {
	SourceActionID string
	Severity       schemas.ApprovalRiskClass
	Summary        string
	Data           map[string]any
}

type ActionResult struct {
	Kind              schemas.ActionKind
	IdempotencyKey    string
	Status            ActionStatus
	ApprovalID        string
	Error             string
	Capabilities      []CapabilityCheck
	DryRun            *DryRunResult
	Execution         *ExecutionResult
	Postconditions    *PostconditionResult
	FollowUpDetection *FollowUpDetection
}

type ExecutionReport struct {
	JobID       string
	RunID       string
	Summary     string
	DryRun      bool
	StartedAt   time.Time
	CompletedAt time.Time
	Results     []ActionResult
}

type ActionHandler interface {
	DryRun(ctx context.Context, action ActionContext) (DryRunResult, error)
	Execute(ctx context.Context, action ActionContext) (ExecutionResult, error)
	VerifyPostconditions(ctx context.Context, action ActionContext, result ExecutionResult) (PostconditionResult, error)
}

type HandlerFuncs struct {
	DryRunFunc               func(context.Context, ActionContext) (DryRunResult, error)
	ExecuteFunc              func(context.Context, ActionContext) (ExecutionResult, error)
	VerifyPostconditionsFunc func(context.Context, ActionContext, ExecutionResult) (PostconditionResult, error)
}

func (h HandlerFuncs) DryRun(ctx context.Context, action ActionContext) (DryRunResult, error) {
	if h.DryRunFunc == nil {
		return DryRunResult{}, fmt.Errorf("agent: dry-run handler missing for %s", action.Action.Kind)
	}
	return h.DryRunFunc(ctx, action)
}

func (h HandlerFuncs) Execute(ctx context.Context, action ActionContext) (ExecutionResult, error) {
	if h.ExecuteFunc == nil {
		return ExecutionResult{}, fmt.Errorf("agent: execute handler missing for %s", action.Action.Kind)
	}
	return h.ExecuteFunc(ctx, action)
}

func (h HandlerFuncs) VerifyPostconditions(ctx context.Context, action ActionContext, result ExecutionResult) (PostconditionResult, error) {
	if h.VerifyPostconditionsFunc == nil {
		return PostconditionResult{}, fmt.Errorf("agent: postcondition verifier missing for %s", action.Action.Kind)
	}
	return h.VerifyPostconditionsFunc(ctx, action, result)
}

type ApprovalState string

const (
	ApprovalApproved ApprovalState = "approved"
	ApprovalDenied   ApprovalState = "denied"
	ApprovalPending  ApprovalState = "pending"
)

type ApprovalRequest struct {
	Plan        schemas.ActionPlan
	Action      schemas.Action
	Spec        ActionSpec
	DryRun      DryRunResult
	RequestedAt time.Time
	Reason      string
}

type ApprovalDecision struct {
	State      ApprovalState
	ApprovalID string
	Reason     string
	DecidedAt  time.Time
}

type ApprovalGate interface {
	RequestApproval(ctx context.Context, req ApprovalRequest) (ApprovalDecision, error)
}

type IdempotencyStore interface {
	LookupActionResult(ctx context.Context, key string) (ActionResult, bool, error)
	RecordActionResult(ctx context.Context, key string, result ActionResult) error
}

type MemoryIdempotencyStore struct {
	mu      sync.Mutex
	results map[string]ActionResult
}

func NewMemoryIdempotencyStore() *MemoryIdempotencyStore {
	return &MemoryIdempotencyStore{results: make(map[string]ActionResult)}
}

func (s *MemoryIdempotencyStore) LookupActionResult(_ context.Context, key string) (ActionResult, bool, error) {
	if s == nil {
		return ActionResult{}, false, nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	result, ok := s.results[key]
	return cloneActionResult(result), ok, nil
}

func (s *MemoryIdempotencyStore) RecordActionResult(_ context.Context, key string, result ActionResult) error {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.results == nil {
		s.results = make(map[string]ActionResult)
	}
	s.results[key] = cloneActionResult(result)
	return nil
}

// FileIdempotencyStore persists action results to an append-only JSONL
// file so idempotency survives daemon restart (hp-cjmc). Each line is
// a {key, result} envelope; on construction the file is read end-to-
// end and the in-memory map is rebuilt with last-write-wins on
// duplicate keys. Lookups are O(1) via the map; writes append a line,
// fsync, and update the map under mu. The file grows append-only —
// duplicate keys produce extra lines but only the most recent value
// is observable. A future compaction pass can rewrite the file from
// the in-memory map; the format is forward-compatible with that.
//
// Mirrors hp-b7rx's bearer-revocation persistence shape: the on-disk
// log is the source of truth across restarts; the in-memory map is a
// read-cache rebuilt on every NewFileIdempotencyStore call.
type FileIdempotencyStore struct {
	mu      sync.Mutex
	path    string
	results map[string]ActionResult
}

type idempotencyEntry struct {
	Key    string       `json:"key"`
	Result ActionResult `json:"result"`
}

// idempotencyMaxLineBytes caps per-line JSONL parsing so a corrupted
// idempotency log cannot OOM the daemon at boot. 4 MiB matches the
// audit log cap and is several orders of magnitude larger than any
// realistic ActionResult — a runaway entry would point at upstream
// bug, not legitimate state.
const idempotencyMaxLineBytes = 4 * 1024 * 1024

// NewFileIdempotencyStore opens (or creates) the file at path and
// rebuilds the in-memory map from any existing entries. Empty path
// is a programmer error — callers that don't want persistence should
// use NewMemoryIdempotencyStore directly.
func NewFileIdempotencyStore(path string) (*FileIdempotencyStore, error) {
	if strings.TrimSpace(path) == "" {
		return nil, fmt.Errorf("agent: FileIdempotencyStore requires a non-empty path")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, fmt.Errorf("agent: mkdir %s: %w", filepath.Dir(path), err)
	}
	s := &FileIdempotencyStore{
		path:    path,
		results: make(map[string]ActionResult),
	}
	if err := s.loadFromDisk(); err != nil {
		return nil, err
	}
	return s, nil
}

func (s *FileIdempotencyStore) loadFromDisk() error {
	f, err := os.Open(s.path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("agent: open idempotency log %s: %w", s.path, err)
	}
	defer f.Close()
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 64*1024), idempotencyMaxLineBytes)
	lineNo := 0
	for scanner.Scan() {
		lineNo++
		raw := scanner.Bytes()
		if len(strings.TrimSpace(string(raw))) == 0 {
			continue
		}
		var entry idempotencyEntry
		if err := json.Unmarshal(raw, &entry); err != nil {
			return fmt.Errorf("agent: decode idempotency log line %d: %w", lineNo, err)
		}
		if strings.TrimSpace(entry.Key) == "" {
			continue
		}
		s.results[entry.Key] = entry.Result
	}
	if err := scanner.Err(); err != nil {
		return fmt.Errorf("agent: scan idempotency log: %w", err)
	}
	return nil
}

func (s *FileIdempotencyStore) LookupActionResult(_ context.Context, key string) (ActionResult, bool, error) {
	if s == nil {
		return ActionResult{}, false, nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	result, ok := s.results[key]
	if !ok {
		return ActionResult{}, false, nil
	}
	return cloneActionResult(result), true, nil
}

func (s *FileIdempotencyStore) RecordActionResult(_ context.Context, key string, result ActionResult) error {
	if s == nil {
		return nil
	}
	if strings.TrimSpace(key) == "" {
		return fmt.Errorf("agent: empty idempotency key")
	}
	body, err := json.Marshal(idempotencyEntry{Key: key, Result: result})
	if err != nil {
		return fmt.Errorf("agent: encode idempotency entry: %w", err)
	}
	body = append(body, '\n')
	s.mu.Lock()
	defer s.mu.Unlock()
	f, err := os.OpenFile(s.path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return fmt.Errorf("agent: open idempotency log for append: %w", err)
	}
	if _, writeErr := f.Write(body); writeErr != nil {
		_ = f.Close()
		return fmt.Errorf("agent: write idempotency entry: %w", writeErr)
	}
	if syncErr := f.Sync(); syncErr != nil {
		_ = f.Close()
		return fmt.Errorf("agent: sync idempotency log: %w", syncErr)
	}
	if closeErr := f.Close(); closeErr != nil {
		return fmt.Errorf("agent: close idempotency log: %w", closeErr)
	}
	if s.results == nil {
		s.results = make(map[string]ActionResult)
	}
	s.results[key] = cloneActionResult(result)
	return nil
}

type Executor struct {
	Catalog      ActionCatalog
	Capabilities CapabilityChecker
	Approvals    ApprovalGate
	Handlers     map[schemas.ActionKind]ActionHandler
	Idempotency  IdempotencyStore
	Audit        AuditSink
	Now          func() time.Time
}

func NewExecutor() *Executor {
	return &Executor{
		Catalog:     DefaultActionCatalog(),
		Idempotency: NewMemoryIdempotencyStore(),
		Now:         time.Now,
	}
}

func (e *Executor) DryRun(ctx context.Context, plan schemas.ActionPlan) (ExecutionReport, error) {
	return e.run(ctx, plan, true)
}

func (e *Executor) Execute(ctx context.Context, plan schemas.ActionPlan) (ExecutionReport, error) {
	return e.run(ctx, plan, false)
}

func (e *Executor) run(ctx context.Context, plan schemas.ActionPlan, dryRunOnly bool) (ExecutionReport, error) {
	if e == nil {
		return ExecutionReport{}, fmt.Errorf("agent: nil executor")
	}
	if err := ValidatePlan(plan); err != nil {
		return ExecutionReport{}, err
	}
	now := e.now()
	report := ExecutionReport{
		JobID:     plan.JobId,
		RunID:     plan.RunId,
		Summary:   plan.Summary,
		DryRun:    dryRunOnly,
		StartedAt: now,
		Results:   make([]ActionResult, 0, len(plan.Actions)),
	}
	catalog := e.catalog()
	priorFailure := false
	for idx, action := range plan.Actions {
		spec := catalog[action.Kind]
		result := ActionResult{
			Kind:           action.Kind,
			IdempotencyKey: strings.TrimSpace(action.IdempotencyKey),
		}
		if priorFailure {
			result.Status = ActionStatusSkippedAfterFailure
			report.Results = append(report.Results, result)
			continue
		}
		ctxAction := ActionContext{
			Plan:           plan,
			Action:         action,
			Spec:           spec,
			Index:          idx,
			Preconditions:  actionList(action.Preconditions, spec.Preconditions),
			Postconditions: actionList(action.Postconditions, spec.Postconditions),
		}
		if !dryRunOnly {
			if replay, ok, err := e.lookupIdempotency(ctx, result.IdempotencyKey); err != nil {
				result = failResult(result, err)
				priorFailure = true
				report.Results = append(report.Results, result)
				continue
			} else if ok {
				replay.Status = ActionStatusIdempotentReplay
				report.Results = append(report.Results, replay)
				continue
			}
		}
		if err := e.audit(ctx, plan, action, "action.started", string(result.Status), nil); err != nil {
			result = failResult(result, err)
			priorFailure = true
			report.Results = append(report.Results, result)
			continue
		}
		checks, err := e.checkCapabilities(ctx, ctxAction)
		result.Capabilities = checks
		if err != nil {
			result.Status = ActionStatusCapabilityUnavailable
			result.Error = err.Error()
			priorFailure = true
			report.Results = append(report.Results, result)
			_ = e.audit(ctx, plan, action, "action.capability_blocked", string(result.Status), map[string]any{"error": result.Error})
			continue
		}
		handler, ok := e.handlers()[action.Kind]
		if !ok || handler == nil {
			result = failResult(result, fmt.Errorf("agent: no handler registered for %s", action.Kind))
			priorFailure = true
			report.Results = append(report.Results, result)
			_ = e.audit(ctx, plan, action, "action.failed", string(result.Status), map[string]any{"error": result.Error})
			continue
		}
		dryRun, err := handler.DryRun(ctx, ctxAction)
		result.DryRun = &dryRun
		if err != nil || !dryRun.OK {
			result.Status = ActionStatusFailed
			if err != nil {
				result.Error = err.Error()
			} else {
				result.Error = "dry-run reported not ok"
			}
			priorFailure = true
			report.Results = append(report.Results, result)
			_ = e.audit(ctx, plan, action, "action.dry_run_failed", string(result.Status), map[string]any{"error": result.Error})
			continue
		}
		if dryRunOnly {
			result.Status = ActionStatusDryRunOK
			report.Results = append(report.Results, result)
			_ = e.audit(ctx, plan, action, "action.dry_run_ok", string(result.Status), nil)
			continue
		}
		if spec.RequiresApproval(plan.RiskClass, plan.RequiresApproval) {
			decision, err := e.requestApproval(ctx, ApprovalRequest{
				Plan:        plan,
				Action:      action,
				Spec:        spec,
				DryRun:      dryRun,
				RequestedAt: e.now(),
				Reason:      plan.Summary,
			})
			if err != nil {
				result = failResult(result, err)
				priorFailure = true
				report.Results = append(report.Results, result)
				_ = e.audit(ctx, plan, action, "action.approval_failed", string(result.Status), map[string]any{"error": result.Error})
				continue
			}
			result.ApprovalID = decision.ApprovalID
			switch decision.State {
			case ApprovalApproved:
			case ApprovalDenied:
				result.Status = ActionStatusApprovalDenied
				result.Error = decision.Reason
				priorFailure = true
				report.Results = append(report.Results, result)
				_ = e.audit(ctx, plan, action, "action.approval_denied", string(result.Status), map[string]any{"approvalId": decision.ApprovalID})
				continue
			case ApprovalPending, "":
				result.Status = ActionStatusApprovalRequired
				result.Error = decision.Reason
				priorFailure = true
				report.Results = append(report.Results, result)
				_ = e.audit(ctx, plan, action, "action.approval_required", string(result.Status), map[string]any{"approvalId": decision.ApprovalID})
				continue
			default:
				result = failResult(result, fmt.Errorf("agent: unknown approval state %q", decision.State))
				priorFailure = true
				report.Results = append(report.Results, result)
				continue
			}
		}
		execution, err := handler.Execute(ctx, ctxAction)
		result.Execution = &execution
		if err != nil || !execution.OK {
			result.Status = ActionStatusFailed
			if err != nil {
				result.Error = err.Error()
			} else {
				result.Error = "execution reported not ok"
			}
			priorFailure = true
			_ = e.recordIdempotency(ctx, result.IdempotencyKey, result)
			report.Results = append(report.Results, result)
			_ = e.audit(ctx, plan, action, "action.execute_failed", string(result.Status), map[string]any{"error": result.Error})
			continue
		}
		postconditions, err := handler.VerifyPostconditions(ctx, ctxAction, execution)
		result.Postconditions = &postconditions
		if err != nil || !postconditions.OK {
			result.Status = ActionStatusPostconditionFailed
			if err != nil {
				result.Error = err.Error()
			} else {
				result.Error = "postcondition verification reported not ok"
			}
			result.FollowUpDetection = &FollowUpDetection{
				SourceActionID: result.IdempotencyKey,
				Severity:       spec.RiskClass,
				Summary:        fmt.Sprintf("postcondition verification failed for %s", action.Kind),
				Data: map[string]any{
					"canonicalRef": postconditions.CanonicalRef,
					"summary":      postconditions.Summary,
				},
			}
			priorFailure = true
			_ = e.recordIdempotency(ctx, result.IdempotencyKey, result)
			report.Results = append(report.Results, result)
			_ = e.audit(ctx, plan, action, "action.postcondition_failed", string(result.Status), map[string]any{"error": result.Error})
			continue
		}
		result.Status = ActionStatusExecuted
		if err := e.recordIdempotency(ctx, result.IdempotencyKey, result); err != nil {
			result = failResult(result, err)
			priorFailure = true
		}
		report.Results = append(report.Results, result)
		_ = e.audit(ctx, plan, action, "action.executed", string(result.Status), map[string]any{"canonicalRef": postconditions.CanonicalRef})
	}
	report.CompletedAt = e.now()
	return report, nil
}

func (e *Executor) catalog() ActionCatalog {
	if e.Catalog != nil {
		return e.Catalog
	}
	return DefaultActionCatalog()
}

func (e *Executor) handlers() map[schemas.ActionKind]ActionHandler {
	if e.Handlers != nil {
		return e.Handlers
	}
	return nil
}

func (e *Executor) now() time.Time {
	if e.Now != nil {
		return e.Now()
	}
	return time.Now()
}

func (e *Executor) checkCapabilities(ctx context.Context, action ActionContext) ([]CapabilityCheck, error) {
	if len(action.Spec.RequiredCapabilities) == 0 {
		return nil, nil
	}
	if e.Capabilities == nil {
		return nil, fmt.Errorf("agent: capability checker is not configured")
	}
	return e.Capabilities.CheckCapabilities(ctx, CapabilityRequest{
		Plan:         action.Plan,
		Action:       action.Action,
		Spec:         action.Spec,
		Requirements: action.Spec.RequiredCapabilities,
	})
}

func (e *Executor) requestApproval(ctx context.Context, req ApprovalRequest) (ApprovalDecision, error) {
	if e.Approvals == nil {
		return ApprovalDecision{
			State:  ApprovalPending,
			Reason: "approval queue is not configured",
		}, nil
	}
	return e.Approvals.RequestApproval(ctx, req)
}

func (e *Executor) lookupIdempotency(ctx context.Context, key string) (ActionResult, bool, error) {
	if e.Idempotency == nil {
		return ActionResult{}, false, nil
	}
	return e.Idempotency.LookupActionResult(ctx, key)
}

func (e *Executor) recordIdempotency(ctx context.Context, key string, result ActionResult) error {
	if e.Idempotency == nil {
		return nil
	}
	return e.Idempotency.RecordActionResult(ctx, key, result)
}

func (e *Executor) audit(ctx context.Context, plan schemas.ActionPlan, action schemas.Action, eventAction string, status string, data map[string]any) error {
	if e.Audit == nil {
		return nil
	}
	return e.Audit.RecordAuditEvent(ctx, AuditEvent{
		Time:           e.now(),
		JobID:          plan.JobId,
		RunID:          plan.RunId,
		Action:         eventAction,
		ActionKind:     action.Kind,
		IdempotencyKey: action.IdempotencyKey,
		Status:         status,
		Reason:         plan.Summary,
		Data:           data,
	})
}

func failResult(result ActionResult, err error) ActionResult {
	result.Status = ActionStatusFailed
	if err != nil {
		result.Error = err.Error()
	}
	return result
}

func cloneActionResult(in ActionResult) ActionResult {
	out := in
	if in.Capabilities != nil {
		out.Capabilities = append([]CapabilityCheck(nil), in.Capabilities...)
	}
	if in.DryRun != nil {
		dryRun := *in.DryRun
		dryRun.Data = cloneMap(dryRun.Data)
		out.DryRun = &dryRun
	}
	if in.Execution != nil {
		execution := *in.Execution
		execution.Data = cloneMap(execution.Data)
		out.Execution = &execution
	}
	if in.Postconditions != nil {
		postconditions := *in.Postconditions
		postconditions.Data = cloneMap(postconditions.Data)
		out.Postconditions = &postconditions
	}
	if in.FollowUpDetection != nil {
		detection := *in.FollowUpDetection
		detection.Data = cloneMap(detection.Data)
		out.FollowUpDetection = &detection
	}
	return out
}

func cloneMap(in map[string]any) map[string]any {
	if in == nil {
		return nil
	}
	out := make(map[string]any, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}
