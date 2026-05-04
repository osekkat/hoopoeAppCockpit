package prescript

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"reflect"
	"testing"
	"time"

	"github.com/hoopoe-cockpit/hoopoe/apps/daemon/internal/agent"
	"github.com/hoopoe-cockpit/hoopoe/apps/daemon/internal/scheduler"
	schemas "github.com/hoopoe-cockpit/hoopoe/packages/schemas/go"
)

func TestRunnerWakeFalseDoesNotInvokeAgent(t *testing.T) {
	job := testJob()
	run := testRun(job.Definition.ID)
	agentRuntime := &fakeAgentRuntime{}
	runner := newTestRunner(t, job, ScriptInvokerFunc(func(_ context.Context, invocation Invocation) (InvocationResult, error) {
		var input Input
		if err := json.Unmarshal(invocation.Stdin, &input); err != nil {
			t.Fatalf("input decode: %v", err)
		}
		if input.Job.ID != job.Definition.ID || input.Run.ID != run.ID {
			t.Fatalf("input = job %s run %s", input.Job.ID, input.Run.ID)
		}
		return InvocationResult{Stdout: []byte("mechanical reconcile ok\n{\"wakeAgent\":false,\"context\":{\"healthy\":true}}\n")}, nil
	}), &fakeExecutor{}, agentRuntime)

	result, err := runner.Run(context.Background(), run)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if result.WakeAgent {
		t.Fatalf("wakeAgent = true, want false")
	}
	if result.Silent {
		t.Fatalf("silent = true without an agent run")
	}
	if got := result.Context["healthy"]; got != true {
		t.Fatalf("context healthy = %#v, want true", got)
	}
	if agentRuntime.calls != 0 {
		t.Fatalf("agent calls = %d, want 0", agentRuntime.calls)
	}
}

func TestRunnerExecutesDeterministicActionIntentThroughExecutor(t *testing.T) {
	job := testJob()
	run := testRun(job.Definition.ID)
	executor := &fakeExecutor{
		report: agent.ExecutionReport{
			JobID: "tend-swarm",
			RunID: "run-1",
			Results: []agent.ActionResult{{
				Kind:   schemas.AgentAskStatus,
				Status: agent.ActionStatusExecuted,
			}},
		},
	}
	runner := newTestRunner(t, job, ScriptInvokerFunc(func(context.Context, Invocation) (InvocationResult, error) {
		return InvocationResult{Stdout: []byte(`{"wakeAgent":false,"summary":"ask quiet agent for status","actionIntents":[{"type":"agent.ask_status","target":{"agentId":"agent-7"},"idempotencyKey":"run-1:ask"}]}`)}, nil
	}), executor, nil)

	result, err := runner.Run(context.Background(), run)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if result.WakeAgent {
		t.Fatalf("wakeAgent = true, want false")
	}
	if executor.calls != 1 {
		t.Fatalf("executor calls = %d, want 1", executor.calls)
	}
	plan := executor.plan
	if plan.SchemaVersion != agent.ActionPlanSchemaVersion || plan.JobId != run.JobID || plan.RunId != run.ID {
		t.Fatalf("plan identity = %+v", plan)
	}
	if plan.Summary != "ask quiet agent for status" {
		t.Fatalf("summary = %q", plan.Summary)
	}
	if plan.RiskClass != schemas.Low {
		t.Fatalf("risk = %s, want low", plan.RiskClass)
	}
	if len(plan.Actions) != 1 || plan.Actions[0].Kind != schemas.AgentAskStatus || plan.Actions[0].Target["agentId"] != "agent-7" {
		t.Fatalf("actions = %+v", plan.Actions)
	}
	if _, ok := result.Context["deterministicActionReport"]; !ok {
		t.Fatalf("deterministic action report missing from context: %+v", result.Context)
	}
}

func TestRunnerWakeAgentBuildsManifestAndPropagatesSilent(t *testing.T) {
	job := testJob()
	job.Definition.Skills = []string{"vibing-with-ntm", "ntm"}
	job.Definition.EnabledToolsets = []string{"br.show", "ntm.snapshot"}
	run := testRun(job.Definition.ID)
	agentRuntime := &fakeAgentRuntime{
		report: agent.RuntimeReport{
			JobID:              run.JobID,
			RunID:              run.ID,
			ActivitySuppressed: true,
			ReplyBody:          "[SILENT]\nno intervention warranted",
		},
	}
	runner := newTestRunner(t, job, ScriptInvokerFunc(func(context.Context, Invocation) (InvocationResult, error) {
		return InvocationResult{Stdout: []byte(`{"wakeAgent":true,"agentId":"orchestrator-chat","context":{"bead":"hp-0d7","signal":"wedged-pane"},"manifest":{"included":["ntm.snapshot","br.ready"],"excluded":["raw-pane"],"redactions":["paths"],"tokenEstimate":512,"tokenBudget":2048}}`)}, nil
	}), &fakeExecutor{}, agentRuntime)

	result, err := runner.Run(context.Background(), run)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !result.WakeAgent || !result.Silent {
		t.Fatalf("result = %+v, want wakeAgent and silent", result)
	}
	if agentRuntime.calls != 1 {
		t.Fatalf("agent calls = %d, want 1", agentRuntime.calls)
	}
	if agentRuntime.request.AgentID != "orchestrator-chat" || agentRuntime.request.Context["bead"] != "hp-0d7" {
		t.Fatalf("runtime request = %+v", agentRuntime.request)
	}
	if !reflect.DeepEqual(agentRuntime.request.Skills, job.Definition.Skills) {
		t.Fatalf("skills = %+v", agentRuntime.request.Skills)
	}
	manifest, ok := result.Context["contextManifest"].(ContextManifest)
	if !ok {
		t.Fatalf("manifest missing or wrong type: %#v", result.Context["contextManifest"])
	}
	if manifest.JobID != run.JobID || manifest.RunID != run.ID || manifest.ContextHash == "" {
		t.Fatalf("manifest identity/hash = %+v", manifest)
	}
	if !reflect.DeepEqual(manifest.SkillsLoaded, job.Definition.Skills) {
		t.Fatalf("manifest skills = %+v", manifest.SkillsLoaded)
	}
	if len(manifest.SourceSnapshots) != 2 {
		t.Fatalf("source snapshots = %+v, want canonical + capabilities refs", manifest.SourceSnapshots)
	}
	if manifest.TokenEstimate != 512 || manifest.TokenBudget != 2048 || !manifest.AgentRan {
		t.Fatalf("manifest policy fields = %+v", manifest)
	}
}

func TestParseOutputUsesFinalNonEmptyJSONLineAndValidatesIntents(t *testing.T) {
	output, err := ParseOutput([]byte("log line\n\n{\"wakeAgent\":true,\"context\":{\"ok\":true}}\n"))
	if err != nil {
		t.Fatalf("ParseOutput: %v", err)
	}
	if !output.WakeAgent || output.Context["ok"] != true {
		t.Fatalf("output = %+v", output)
	}
	_, err = ParseOutput([]byte(`{"wakeAgent":false,"actionIntents":[{"type":"shell.exec","idempotencyKey":"bad"}]}`))
	if err == nil {
		t.Fatalf("expected unknown action kind error")
	}
	_, err = ParseOutput([]byte(`{"wakeAgent":false,"actionIntents":[{"type":"agent.ask_status","target":{"agentId":"a"}}]}`))
	if err == nil {
		t.Fatalf("expected missing idempotency key error")
	}
}

func TestExecScriptInvokerPassesJSONStdinAndParsesFinalLine(t *testing.T) {
	if os.Getenv("HOOPOE_PRESCRIPT_HELPER") == "1" {
		helperProcess(t)
		return
	}
	job := testJob()
	run := testRun(job.Definition.ID)
	input := Input{
		SchemaVersion: SchemaVersion,
		GeneratedAt:   fixedTime(),
		Job:           job.Definition,
		Run:           run,
		Canonical:     map[string]any{"ntm": map[string]any{"agents": 2}},
		Capabilities:  map[string]any{"ntm.snapshot": "ok"},
	}
	stdin, err := json.Marshal(input)
	if err != nil {
		t.Fatalf("marshal input: %v", err)
	}
	invoker := ExecScriptInvoker{
		Args: []string{"-test.run=TestExecScriptInvokerPassesJSONStdinAndParsesFinalLine", "--"},
		Env:  []string{"HOOPOE_PRESCRIPT_HELPER=1"},
	}
	result, err := invoker.Invoke(context.Background(), Invocation{
		Script: os.Args[0],
		Stdin:  stdin,
		Job:    job,
		Run:    run,
	})
	if err != nil {
		t.Fatalf("Invoke: %v\nstderr=%s", err, result.Stderr)
	}
	output, err := ParseOutput(result.Stdout)
	if err != nil {
		t.Fatalf("ParseOutput: %v\nstdout=%s", err, result.Stdout)
	}
	if output.WakeAgent || output.Context["jobId"] != job.Definition.ID {
		t.Fatalf("output = %+v", output)
	}
}

func newTestRunner(t *testing.T, job scheduler.Job, invoker ScriptInvoker, executor ActionExecutor, runtime AgentRuntime) *Runner {
	t.Helper()
	runner, err := New(Config{
		Definitions: fakeDefinitionSource{job: job},
		Snapshots: SnapshotSourceFunc(func(context.Context, scheduler.Job, scheduler.Run) (Snapshot, error) {
			return Snapshot{
				Canonical: map[string]any{
					"ntm": map[string]any{"agents": 2},
				},
				Capabilities: map[string]any{
					"ntm.snapshot": map[string]any{"status": "ok"},
				},
			}, nil
		}),
		Scripts:  invoker,
		Executor: executor,
		Agent:    runtime,
		Now:      fixedTime,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return runner
}

func testJob() scheduler.Job {
	now := fixedTime()
	return scheduler.Job{
		Definition: scheduler.Definition{
			ID:              "tend-swarm",
			Name:            "Tend swarm",
			Kind:            scheduler.KindGatedAgent,
			Version:         scheduler.SchemaVersion,
			Revision:        1,
			Schedule:        scheduler.Schedule{Type: scheduler.ScheduleOnDemand},
			Script:          "/usr/local/lib/hoopoe/tend-swarm-prescript",
			Prompt:          "inspect {{.signal}} for {{.bead}}",
			Skills:          []string{"vibing-with-ntm"},
			EnabledToolsets: []string{"br.show"},
			AuditAlways:     true,
		},
		Status:     scheduler.JobStatusReady,
		ImportedAt: now,
		UpdatedAt:  now,
	}
}

func testRun(jobID string) scheduler.Run {
	now := fixedTime()
	started := now
	return scheduler.Run{
		ID:        "run-1",
		JobID:     jobID,
		Revision:  1,
		Trigger:   scheduler.Trigger{Type: scheduler.TriggerOnDemand},
		Status:    scheduler.RunStatusRunning,
		Outcome:   scheduler.OutcomeStarted,
		Attempt:   1,
		QueuedAt:  now,
		StartedAt: &started,
	}
}

func fixedTime() time.Time {
	return time.Date(2026, 5, 4, 12, 0, 0, 0, time.UTC)
}

type fakeDefinitionSource struct {
	job scheduler.Job
}

func (s fakeDefinitionSource) GetJob(_ context.Context, id string) (scheduler.Job, error) {
	if id != s.job.Definition.ID {
		return scheduler.Job{}, scheduler.ErrNotFound
	}
	return s.job, nil
}

type fakeExecutor struct {
	calls  int
	plan   schemas.ActionPlan
	report agent.ExecutionReport
}

func (e *fakeExecutor) Execute(_ context.Context, plan schemas.ActionPlan) (agent.ExecutionReport, error) {
	e.calls++
	e.plan = plan
	if e.report.JobID == "" {
		return agent.ExecutionReport{JobID: plan.JobId, RunID: plan.RunId}, nil
	}
	return e.report, nil
}

type fakeAgentRuntime struct {
	calls   int
	request agent.RuntimeRequest
	report  agent.RuntimeReport
}

func (r *fakeAgentRuntime) Run(_ context.Context, req agent.RuntimeRequest) (agent.RuntimeReport, error) {
	r.calls++
	r.request = req
	if r.report.JobID == "" {
		return agent.RuntimeReport{JobID: req.JobID, RunID: req.RunID}, nil
	}
	return r.report, nil
}

func helperProcess(t *testing.T) {
	t.Helper()
	data, err := io.ReadAll(os.Stdin)
	if err != nil {
		fmt.Fprintf(os.Stderr, "read stdin: %v\n", err)
		os.Exit(2)
	}
	var input Input
	if err := json.Unmarshal(data, &input); err != nil {
		fmt.Fprintf(os.Stderr, "decode stdin: %v\n", err)
		os.Exit(2)
	}
	fmt.Println("helper log line")
	fmt.Printf("{\"wakeAgent\":false,\"context\":{\"jobId\":%q}}\n", input.Job.ID)
	os.Exit(0)
}
