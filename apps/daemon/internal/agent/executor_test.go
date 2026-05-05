package agent

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	schemas "github.com/hoopoe-cockpit/hoopoe/packages/schemas/go"
)

func TestDefaultCatalogMatchesGeneratedActionKindEnum(t *testing.T) {
	want := []schemas.ActionKind{
		schemas.AgentAskStatus,
		schemas.AgentKillWedgedProcess,
		schemas.AgentPause,
		schemas.AgentSendMarchingOrders,
		schemas.BeadCreateBlocker,
		schemas.CaamSwitchAccount,
		schemas.CasrResumeSession,
		schemas.GitPushBranch,
		schemas.ReservationForceRelease,
		schemas.ReviewProposeFlip,
		schemas.SwarmHalt,
	}
	if got := KnownActionKinds(); !reflect.DeepEqual(got, want) {
		t.Fatalf("catalog action kinds drifted from generated ActionKind enum\ngot:  %v\nwant: %v", got, want)
	}
	for _, kind := range KnownActionKinds() {
		if !kind.Valid() {
			t.Fatalf("catalog contains non-generated action kind %q", kind)
		}
	}
}

func TestValidatePlanRejectsUnknownKindAndMissingFields(t *testing.T) {
	plan := basePlan()
	plan.Actions = []schemas.Action{{
		Kind:           schemas.ActionKind("shell.exec"),
		Target:         map[string]any{},
		IdempotencyKey: "run-1:0",
	}}
	err := ValidatePlan(plan)
	if err == nil {
		t.Fatal("expected validation error")
	}
	if !strings.Contains(err.Error(), "shell.exec") {
		t.Fatalf("validation error did not name unknown kind: %v", err)
	}

	plan = basePlan()
	args := map[string]any{}
	plan.Actions = []schemas.Action{{
		Kind:           schemas.GitPushBranch,
		Target:         map[string]any{"projectId": "proj"},
		Args:           &args,
		IdempotencyKey: "run-1:0",
	}}
	err = ValidatePlan(plan)
	if err == nil {
		t.Fatal("expected missing field validation error")
	}
	for _, want := range []string{"target.branch", "args.expectedSha"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("validation error missing %q: %v", want, err)
		}
	}
}

func TestDryRunValidatesCapabilitiesAndDoesNotExecute(t *testing.T) {
	plan := basePlan()
	plan.Actions = []schemas.Action{askStatusAction("idem-ask-status")}
	handler := &countingHandler{}
	exec := NewExecutor()
	exec.Capabilities = AllowAllCapabilities{}
	exec.Handlers = map[schemas.ActionKind]ActionHandler{
		schemas.AgentAskStatus: handler,
	}
	report, err := exec.DryRun(context.Background(), plan)
	if err != nil {
		t.Fatalf("dry run: %v", err)
	}
	if len(report.Results) != 1 || report.Results[0].Status != ActionStatusDryRunOK {
		t.Fatalf("unexpected dry-run report: %+v", report.Results)
	}
	if handler.dryRuns != 1 || handler.executes != 0 || handler.verifies != 0 {
		t.Fatalf("handler counts = dry:%d exec:%d verify:%d", handler.dryRuns, handler.executes, handler.verifies)
	}
}

func TestExecuteRequiresApprovalBeforeMutatingHighRiskAction(t *testing.T) {
	plan := basePlan()
	requiresApproval := true
	plan.RequiresApproval = &requiresApproval
	plan.RiskClass = schemas.High
	plan.Actions = []schemas.Action{killAction("idem-kill")}
	handler := &countingHandler{}
	exec := NewExecutor()
	exec.Capabilities = AllowAllCapabilities{}
	exec.Handlers = map[schemas.ActionKind]ActionHandler{
		schemas.AgentKillWedgedProcess: handler,
	}
	report, err := exec.Execute(context.Background(), plan)
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if len(report.Results) != 1 || report.Results[0].Status != ActionStatusApprovalRequired {
		t.Fatalf("expected approval_required, got %+v", report.Results)
	}
	if handler.executes != 0 || handler.verifies != 0 {
		t.Fatalf("approval-required action executed before approval: exec:%d verify:%d", handler.executes, handler.verifies)
	}
}

func TestExecuteRunsApprovedActionAndReplaysIdempotency(t *testing.T) {
	plan := basePlan()
	plan.Actions = []schemas.Action{askStatusAction("idem-ask-status")}
	handler := &countingHandler{}
	exec := NewExecutor()
	exec.Capabilities = AllowAllCapabilities{}
	exec.Handlers = map[schemas.ActionKind]ActionHandler{
		schemas.AgentAskStatus: handler,
	}
	report, err := exec.Execute(context.Background(), plan)
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if len(report.Results) != 1 || report.Results[0].Status != ActionStatusExecuted {
		t.Fatalf("expected executed, got %+v", report.Results)
	}
	if handler.executes != 1 || handler.verifies != 1 {
		t.Fatalf("first execution counts = exec:%d verify:%d", handler.executes, handler.verifies)
	}
	replay, err := exec.Execute(context.Background(), plan)
	if err != nil {
		t.Fatalf("replay execute: %v", err)
	}
	if len(replay.Results) != 1 || replay.Results[0].Status != ActionStatusIdempotentReplay {
		t.Fatalf("expected idempotent replay, got %+v", replay.Results)
	}
	if handler.executes != 1 || handler.verifies != 1 {
		t.Fatalf("idempotent replay re-executed: exec:%d verify:%d", handler.executes, handler.verifies)
	}
}

func TestPostconditionFailureEmitsFollowUpDetection(t *testing.T) {
	plan := basePlan()
	plan.Actions = []schemas.Action{askStatusAction("idem-ask-status")}
	handler := &countingHandler{failVerification: true}
	exec := NewExecutor()
	exec.Capabilities = AllowAllCapabilities{}
	exec.Handlers = map[schemas.ActionKind]ActionHandler{
		schemas.AgentAskStatus: handler,
	}
	report, err := exec.Execute(context.Background(), plan)
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if len(report.Results) != 1 || report.Results[0].Status != ActionStatusPostconditionFailed {
		t.Fatalf("expected postcondition failure, got %+v", report.Results)
	}
	detection := report.Results[0].FollowUpDetection
	if detection == nil || detection.SourceActionID != "idem-ask-status" || detection.Severity != schemas.Low {
		t.Fatalf("follow-up detection not populated correctly: %+v", detection)
	}
}

func TestRuntimeLoadsSkillsRendersPromptSuppressesSilentActivityAndExecutesPlan(t *testing.T) {
	plan := basePlan()
	plan.Actions = []schemas.Action{askStatusAction("idem-runtime")}
	handler := &countingHandler{}
	exec := NewExecutor()
	exec.Capabilities = AllowAllCapabilities{}
	exec.Handlers = map[schemas.ActionKind]ActionHandler{
		schemas.AgentAskStatus: handler,
	}
	audit := &recordingAudit{}
	runtime := &Runtime{
		Skills: StaticSkillLoader{
			"vibing-with-ntm": {Name: "vibing-with-ntm", Content: "skill body", Source: "fixture"},
		},
		Runner: &fakeRunner{reply: AgentReply{
			Body:       "[SILENT]\nqueued typed actions",
			ActionPlan: &plan,
		}},
		Executor: exec,
		Audit:    audit,
		Now:      fixedNow,
	}
	report, err := runtime.Run(context.Background(), RuntimeRequest{
		JobID:          "job-1",
		RunID:          "run-1",
		AgentID:        "agent-1",
		PromptTemplate: "hello {{.bead}}",
		Context:        map[string]any{"bead": "hp-209"},
		Skills:         []string{"vibing-with-ntm"},
		ReadOnlyTools:  []string{"br.show", "ntm.snapshot"},
	})
	if err != nil {
		t.Fatalf("runtime run: %v", err)
	}
	if !report.ActivitySuppressed {
		t.Fatalf("expected [SILENT] reply to suppress activity: %+v", report)
	}
	if report.ActionReport == nil || len(report.ActionReport.Results) != 1 || report.ActionReport.Results[0].Status != ActionStatusExecuted {
		t.Fatalf("expected executed action report, got %+v", report.ActionReport)
	}
	if !audit.saw("runtime.reply") || !audit.saw("runtime.completed") {
		t.Fatalf("runtime audit did not preserve silent reply events: %+v", audit.events)
	}
}

func TestRuntimePropagatesPromptTemplateErrors(t *testing.T) {
	runtime := &Runtime{Runner: &fakeRunner{}}
	_, err := runtime.Run(context.Background(), RuntimeRequest{
		JobID:          "job-1",
		RunID:          "run-1",
		PromptTemplate: "hello {{.missing}}",
	})
	if err == nil {
		t.Fatal("expected prompt template error")
	}
}

type countingHandler struct {
	dryRuns          int
	executes         int
	verifies         int
	failVerification bool
}

func (h *countingHandler) DryRun(context.Context, ActionContext) (DryRunResult, error) {
	h.dryRuns++
	return DryRunResult{OK: true, Summary: "dry-run ok"}, nil
}

func (h *countingHandler) Execute(context.Context, ActionContext) (ExecutionResult, error) {
	h.executes++
	return ExecutionResult{OK: true, CanonicalRef: "canonical://action"}, nil
}

func (h *countingHandler) VerifyPostconditions(context.Context, ActionContext, ExecutionResult) (PostconditionResult, error) {
	h.verifies++
	if h.failVerification {
		return PostconditionResult{OK: false, CanonicalRef: "canonical://failed", Summary: "canonical state did not change"}, nil
	}
	return PostconditionResult{OK: true, CanonicalRef: "canonical://verified"}, nil
}

type fakeRunner struct {
	reply AgentReply
	err   error
}

func (r *fakeRunner) RunAgent(context.Context, AgentInvocation) (AgentReply, error) {
	if r.err != nil {
		return AgentReply{}, r.err
	}
	return r.reply, nil
}

type recordingAudit struct {
	events []AuditEvent
	err    error
}

func (a *recordingAudit) RecordAuditEvent(_ context.Context, event AuditEvent) error {
	if a.err != nil {
		return a.err
	}
	a.events = append(a.events, event)
	return nil
}

func (a *recordingAudit) saw(action string) bool {
	for _, event := range a.events {
		if event.Action == action {
			return true
		}
	}
	return false
}

func basePlan() schemas.ActionPlan {
	return schemas.ActionPlan{
		SchemaVersion: ActionPlanSchemaVersion,
		JobId:         "job-1",
		RunId:         "run-1",
		Summary:       "test plan",
		RiskClass:     schemas.Low,
		Actions:       []schemas.Action{askStatusAction("idem-ask-status")},
	}
}

func askStatusAction(key string) schemas.Action {
	return schemas.Action{
		Kind: schemas.AgentAskStatus,
		Target: map[string]any{
			"agentId": "agent-1",
		},
		IdempotencyKey: key,
	}
}

func killAction(key string) schemas.Action {
	args := map[string]any{"reason": "wedged"}
	return schemas.Action{
		Kind: schemas.AgentKillWedgedProcess,
		Target: map[string]any{
			"agentId": "agent-1",
		},
		Args:           &args,
		IdempotencyKey: key,
	}
}

func fixedNow() time.Time {
	return time.Date(2026, 5, 4, 1, 0, 0, 0, time.UTC)
}

// TestFileIdempotencyStorePersistsAcrossRestart guards hp-cjmc: action
// idempotency must survive a daemon restart. Pre-fix, NewExecutor()
// defaulted to MemoryIdempotencyStore, so a mid-tick crash followed
// by restart would replay an already-completed mutating action and
// duplicate its side effects (mail sends, br creates, commits).
func TestFileIdempotencyStorePersistsAcrossRestart(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "tending", "idempotency.jsonl")

	plan := basePlan()
	plan.Actions = []schemas.Action{askStatusAction("hp-cjmc-restart")}
	handler := &countingHandler{}

	// First daemon lifetime: execute the plan against a file-backed
	// idempotency store.
	store, err := NewFileIdempotencyStore(path)
	if err != nil {
		t.Fatalf("NewFileIdempotencyStore (boot 1): %v", err)
	}
	exec := NewExecutor()
	exec.Capabilities = AllowAllCapabilities{}
	exec.Handlers = map[schemas.ActionKind]ActionHandler{
		schemas.AgentAskStatus: handler,
	}
	exec.Idempotency = store
	report, err := exec.Execute(context.Background(), plan)
	if err != nil {
		t.Fatalf("execute (boot 1): %v", err)
	}
	if len(report.Results) != 1 || report.Results[0].Status != ActionStatusExecuted {
		t.Fatalf("boot 1 expected executed, got %+v", report.Results)
	}
	if handler.executes != 1 {
		t.Fatalf("boot 1 executes = %d, want 1", handler.executes)
	}

	// Simulate restart: drop the in-process store reference + executor,
	// reopen the same path, run the same plan. The persistent store
	// must report ActionStatusIdempotentReplay and the handler must
	// not re-execute.
	store2, err := NewFileIdempotencyStore(path)
	if err != nil {
		t.Fatalf("NewFileIdempotencyStore (boot 2): %v", err)
	}
	exec2 := NewExecutor()
	exec2.Capabilities = AllowAllCapabilities{}
	exec2.Handlers = map[schemas.ActionKind]ActionHandler{
		schemas.AgentAskStatus: handler,
	}
	exec2.Idempotency = store2
	replay, err := exec2.Execute(context.Background(), plan)
	if err != nil {
		t.Fatalf("execute (boot 2): %v", err)
	}
	if len(replay.Results) != 1 || replay.Results[0].Status != ActionStatusIdempotentReplay {
		t.Fatalf("boot 2 expected idempotent replay, got %+v — hp-cjmc regression: idempotency was lost across restart", replay.Results)
	}
	if handler.executes != 1 {
		t.Fatalf("boot 2 re-executed handler: executes = %d, want 1 (memory-only-store regression)", handler.executes)
	}
}

// TestFileIdempotencyStoreLastWriteWinsOnDuplicateKeys covers the
// reload semantics: duplicate JSONL entries for the same key must
// resolve to the most recent value after reload. The store is
// append-only by design so duplicate keys are a normal consequence of
// retries; the in-memory map must reflect last-write-wins.
func TestFileIdempotencyStoreLastWriteWinsOnDuplicateKeys(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "idempotency.jsonl")

	store, err := NewFileIdempotencyStore(path)
	if err != nil {
		t.Fatalf("NewFileIdempotencyStore: %v", err)
	}
	first := ActionResult{Kind: schemas.AgentAskStatus, IdempotencyKey: "k", Status: ActionStatusExecuted, Error: "first"}
	second := ActionResult{Kind: schemas.AgentAskStatus, IdempotencyKey: "k", Status: ActionStatusExecuted, Error: "second"}
	if err := store.RecordActionResult(context.Background(), "k", first); err != nil {
		t.Fatalf("Record first: %v", err)
	}
	if err := store.RecordActionResult(context.Background(), "k", second); err != nil {
		t.Fatalf("Record second: %v", err)
	}

	// Drop the in-process store and reopen — the JSONL has both
	// entries; the reload must keep the second one.
	reloaded, err := NewFileIdempotencyStore(path)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	got, ok, err := reloaded.LookupActionResult(context.Background(), "k")
	if err != nil {
		t.Fatalf("LookupActionResult after reload: %v", err)
	}
	if !ok {
		t.Fatal("LookupActionResult after reload: missing key")
	}
	if got.Error != "second" {
		t.Fatalf("reload Error = %q, want %q (last-write-wins regression)", got.Error, "second")
	}
}

// TestFileIdempotencyStoreRejectsCorruptLogAtBoot pins down the
// boot-time validation contract: a corrupted JSONL line surfaces a
// wrapped error from NewFileIdempotencyStore rather than panicking
// or silently losing all entries past the corruption point. An
// operator hitting this error has a clear signal to inspect the log.
func TestFileIdempotencyStoreRejectsCorruptLogAtBoot(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "idempotency.jsonl")
	if err := os.WriteFile(path, []byte(`{"key":"k","result":{"kind":"agent.ask_status"}}`+"\n"+`{not json`+"\n"), 0o600); err != nil {
		t.Fatalf("seed corrupt log: %v", err)
	}
	_, err := NewFileIdempotencyStore(path)
	if err == nil {
		t.Fatal("NewFileIdempotencyStore accepted a corrupt log; want a wrapped decode error")
	}
	if !strings.Contains(err.Error(), "agent: decode idempotency log line 2") {
		t.Fatalf("err = %v, want a 'decode idempotency log line 2' wrap", err)
	}
}

// TestIsSilentReplyCovers cases enumerates the [SILENT] sentinel
// matching contract from plan.md §8.3 and pins the hp-rlh9 fix.
// Trailing whitespace between the sentinel and the newline is
// permitted; content that begins with [SILENT] but no newline is
// NOT silent (it's ordinary reply content that happens to start
// with the literal token).
func TestIsSilentReplyCovers(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name   string
		body   string
		silent bool
	}{
		{"bare", "[SILENT]", true},
		{"trailing newline", "[SILENT]\n", true},
		{"surrounded by outer whitespace", "  [SILENT]  ", true},
		{"sentinel with newline and content", "[SILENT]\nfollow-up notes", true},
		{"sentinel with internal trailing space then newline", "[SILENT]   \nfollow-up", true},
		{"sentinel with internal tab then newline", "[SILENT]\t\nfollow-up", true},
		{"sentinel with mixed horizontal whitespace then newline", "[SILENT] \t \nfollow-up", true},
		{"empty body", "", false},
		{"different sentinel", "[SILENT-ish]", false},
		{"sentinel followed by content without newline", "[SILENT]content", false},
		{"sentinel followed by content with no separator", "[SILENT]inline-text", false},
		{"unrelated reply", "ack: agent processed the bead", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := IsSilentReply(tc.body); got != tc.silent {
				t.Errorf("IsSilentReply(%q) = %v, want %v", tc.body, got, tc.silent)
			}
		})
	}
}

// TestFileIdempotencyStoreEmptyKeyRejected pins the contract that an
// empty idempotency key is a programmer error: callers must always
// generate a deterministic key per ActionPlan.BuildActionPlan
// derivation. Empty would silently coalesce all anonymous actions
// into one slot and produce false replays.
func TestFileIdempotencyStoreEmptyKeyRejected(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "idempotency.jsonl")
	store, err := NewFileIdempotencyStore(path)
	if err != nil {
		t.Fatalf("NewFileIdempotencyStore: %v", err)
	}
	err = store.RecordActionResult(context.Background(), "", ActionResult{Kind: schemas.AgentAskStatus, Status: ActionStatusExecuted})
	if err == nil {
		t.Fatal("RecordActionResult accepted an empty key")
	}
	if !strings.Contains(err.Error(), "empty idempotency key") {
		t.Fatalf("err = %v, want 'empty idempotency key'", err)
	}
}
