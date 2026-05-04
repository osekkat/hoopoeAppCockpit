// Package prescript runs Hoopoe tending layer-2 pre-scripts.
//
// Pre-scripts are deterministic subprocesses. They receive canonical state on
// stdin and return JSON on stdout; only their typed action intents can mutate
// state, and those intents are routed through the existing ActionPlan executor.
package prescript

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"sort"
	"strings"
	"time"

	"github.com/hoopoe-cockpit/hoopoe/apps/daemon/internal/agent"
	"github.com/hoopoe-cockpit/hoopoe/apps/daemon/internal/scheduler"
	schemas "github.com/hoopoe-cockpit/hoopoe/packages/schemas/go"
)

const SchemaVersion = 1

var (
	ErrInvalidInput  = errors.New("prescript: invalid input")
	ErrInvalidOutput = errors.New("prescript: invalid output")
)

type DefinitionSource interface {
	GetJob(ctx context.Context, id string) (scheduler.Job, error)
}

type SnapshotSource interface {
	Snapshot(ctx context.Context, job scheduler.Job, run scheduler.Run) (Snapshot, error)
}

type SnapshotSourceFunc func(context.Context, scheduler.Job, scheduler.Run) (Snapshot, error)

func (f SnapshotSourceFunc) Snapshot(ctx context.Context, job scheduler.Job, run scheduler.Run) (Snapshot, error) {
	return f(ctx, job, run)
}

type ScriptInvoker interface {
	Invoke(ctx context.Context, invocation Invocation) (InvocationResult, error)
}

type ScriptInvokerFunc func(context.Context, Invocation) (InvocationResult, error)

func (f ScriptInvokerFunc) Invoke(ctx context.Context, invocation Invocation) (InvocationResult, error) {
	return f(ctx, invocation)
}

type ActionExecutor interface {
	Execute(ctx context.Context, plan schemas.ActionPlan) (agent.ExecutionReport, error)
}

type AgentRuntime interface {
	Run(ctx context.Context, req agent.RuntimeRequest) (agent.RuntimeReport, error)
}

type Config struct {
	Definitions DefinitionSource
	Snapshots   SnapshotSource
	Scripts     ScriptInvoker
	Executor    ActionExecutor
	Agent       AgentRuntime
	Now         func() time.Time
}

type Runner struct {
	definitions DefinitionSource
	snapshots   SnapshotSource
	scripts     ScriptInvoker
	executor    ActionExecutor
	agent       AgentRuntime
	now         func() time.Time
}

func New(cfg Config) (*Runner, error) {
	if cfg.Definitions == nil {
		return nil, fmt.Errorf("%w: definition source is required", ErrInvalidInput)
	}
	scripts := cfg.Scripts
	if scripts == nil {
		scripts = ExecScriptInvoker{}
	}
	now := cfg.Now
	if now == nil {
		now = time.Now
	}
	return &Runner{
		definitions: cfg.Definitions,
		snapshots:   cfg.Snapshots,
		scripts:     scripts,
		executor:    cfg.Executor,
		agent:       cfg.Agent,
		now:         now,
	}, nil
}

func (r *Runner) Run(ctx context.Context, run scheduler.Run) (scheduler.RunResult, error) {
	if r == nil {
		return scheduler.RunResult{}, fmt.Errorf("%w: nil runner", ErrInvalidInput)
	}
	job, err := r.definitions.GetJob(ctx, run.JobID)
	if err != nil {
		return scheduler.RunResult{}, err
	}
	input, err := r.buildInput(ctx, job, run)
	if err != nil {
		return scheduler.RunResult{}, err
	}
	payload, err := json.Marshal(input)
	if err != nil {
		return scheduler.RunResult{}, fmt.Errorf("prescript: encode input: %w", err)
	}
	invocation := Invocation{
		Script: job.Definition.Script,
		Stdin:  append(payload, '\n'),
		Job:    job,
		Run:    run,
	}
	result, err := r.scripts.Invoke(ctx, invocation)
	if err != nil {
		return scheduler.RunResult{}, err
	}
	output, err := ParseOutput(result.Stdout)
	if err != nil {
		return scheduler.RunResult{}, err
	}
	runContext := cloneMap(output.Context)
	runContext["preScript"] = map[string]any{
		"schemaVersion": SchemaVersion,
		"stdoutLines":   countNonEmptyLines(result.Stdout),
		"stderrBytes":   len(result.Stderr),
	}
	var actionReport *agent.ExecutionReport
	if len(output.ActionIntents) > 0 {
		report, err := r.executeActionIntents(ctx, job, run, input, output)
		if err != nil {
			return scheduler.RunResult{}, err
		}
		actionReport = &report
		runContext["deterministicActionReport"] = report
	}
	var runtimeReport *agent.RuntimeReport
	if output.WakeAgent {
		manifest := BuildContextManifest(ManifestInput{
			Job:      job,
			Run:      run,
			Input:    input,
			Output:   output,
			Now:      r.now(),
			AgentRan: r.agent != nil,
		})
		runContext["contextManifest"] = manifest
		if r.agent != nil {
			report, err := r.agent.Run(ctx, agent.RuntimeRequest{
				JobID:          run.JobID,
				RunID:          run.ID,
				AgentID:        output.AgentID,
				PromptTemplate: job.Definition.Prompt,
				Context:        cloneMap(output.Context),
				Skills:         append([]string(nil), job.Definition.Skills...),
				ReadOnlyTools:  append([]string(nil), job.Definition.EnabledToolsets...),
			})
			if err != nil {
				return scheduler.RunResult{}, err
			}
			runtimeReport = &report
			runContext["agentRuntimeReport"] = report
		}
	}
	if actionReport != nil && runtimeReport != nil {
		runContext["actionFlow"] = "pre_script_and_agent"
	} else if actionReport != nil {
		runContext["actionFlow"] = "pre_script"
	} else if runtimeReport != nil {
		runContext["actionFlow"] = "agent"
	}
	return scheduler.RunResult{
		WakeAgent: output.WakeAgent,
		Silent:    runtimeReport != nil && runtimeReport.ActivitySuppressed,
		Context:   runContext,
	}, nil
}

func (r *Runner) buildInput(ctx context.Context, job scheduler.Job, run scheduler.Run) (Input, error) {
	snapshot := Snapshot{}
	var err error
	if r.snapshots != nil {
		snapshot, err = r.snapshots.Snapshot(ctx, job, run)
		if err != nil {
			return Input{}, err
		}
	}
	input := Input{
		SchemaVersion: SchemaVersion,
		GeneratedAt:   r.now().UTC(),
		Job:           job.Definition,
		Run:           run,
		Canonical:     cloneMap(snapshot.Canonical),
		Capabilities:  cloneMap(snapshot.Capabilities),
	}
	if input.Canonical == nil {
		input.Canonical = map[string]any{}
	}
	if input.Capabilities == nil {
		input.Capabilities = map[string]any{}
	}
	return input, nil
}

func (r *Runner) executeActionIntents(ctx context.Context, job scheduler.Job, run scheduler.Run, input Input, output Output) (agent.ExecutionReport, error) {
	if r.executor == nil {
		return agent.ExecutionReport{}, fmt.Errorf("%w: action intents require an executor", ErrInvalidInput)
	}
	plan, err := BuildActionPlan(job, run, input, output)
	if err != nil {
		return agent.ExecutionReport{}, err
	}
	return r.executor.Execute(ctx, plan)
}

type Snapshot struct {
	Canonical    map[string]any
	Capabilities map[string]any
}

type Input struct {
	SchemaVersion int                  `json:"schemaVersion"`
	GeneratedAt   time.Time            `json:"generatedAt"`
	Job           scheduler.Definition `json:"job"`
	Run           scheduler.Run        `json:"run"`
	Canonical     map[string]any       `json:"canonical"`
	Capabilities  map[string]any       `json:"capabilities"`
}

type Invocation struct {
	Script string
	Stdin  []byte
	Job    scheduler.Job
	Run    scheduler.Run
}

type InvocationResult struct {
	Stdout []byte
	Stderr []byte
}

type ExecScriptInvoker struct {
	Args []string
	Env  []string
}

func (i ExecScriptInvoker) Invoke(ctx context.Context, invocation Invocation) (InvocationResult, error) {
	script := strings.TrimSpace(invocation.Script)
	if script == "" {
		return InvocationResult{}, fmt.Errorf("%w: script path is required", ErrInvalidInput)
	}
	cmd := exec.CommandContext(ctx, script, i.Args...)
	if len(i.Env) > 0 {
		cmd.Env = append(cmd.Environ(), i.Env...)
	}
	cmd.Stdin = bytes.NewReader(invocation.Stdin)
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return InvocationResult{Stdout: stdout.Bytes(), Stderr: stderr.Bytes()}, fmt.Errorf("prescript: run %s: %w: %s", script, err, strings.TrimSpace(stderr.String()))
	}
	return InvocationResult{Stdout: stdout.Bytes(), Stderr: stderr.Bytes()}, nil
}

type Output struct {
	WakeAgent     bool                      `json:"wakeAgent"`
	Context       map[string]any            `json:"context,omitempty"`
	ActionIntents []ActionIntent            `json:"actionIntents,omitempty"`
	Summary       string                    `json:"summary,omitempty"`
	RiskClass     schemas.ApprovalRiskClass `json:"riskClass,omitempty"`
	EvidenceRefs  []string                  `json:"evidenceRefs,omitempty"`
	AgentID       string                    `json:"agentId,omitempty"`
	Manifest      ManifestHints             `json:"manifest,omitempty"`
}

type ActionIntent struct {
	Type           string             `json:"type,omitempty"`
	Kind           schemas.ActionKind `json:"kind,omitempty"`
	Target         map[string]any     `json:"target,omitempty"`
	Args           map[string]any     `json:"args,omitempty"`
	IdempotencyKey string             `json:"idempotencyKey"`
	Preconditions  []string           `json:"preconditions,omitempty"`
	Postconditions []string           `json:"postconditions,omitempty"`
}

func ParseOutput(stdout []byte) (Output, error) {
	line := lastNonEmptyLine(stdout)
	if line == "" {
		return Output{}, fmt.Errorf("%w: stdout did not contain a final JSON line", ErrInvalidOutput)
	}
	var output Output
	if err := json.Unmarshal([]byte(line), &output); err != nil {
		return Output{}, fmt.Errorf("%w: decode final JSON line: %w", ErrInvalidOutput, err)
	}
	if output.Context == nil {
		output.Context = map[string]any{}
	}
	if output.RiskClass != "" && !output.RiskClass.Valid() {
		return Output{}, fmt.Errorf("%w: invalid riskClass %q", ErrInvalidOutput, output.RiskClass)
	}
	for idx, intent := range output.ActionIntents {
		if _, err := intent.toAction(idx); err != nil {
			return Output{}, err
		}
	}
	return output, nil
}

func BuildActionPlan(job scheduler.Job, run scheduler.Run, input Input, output Output) (schemas.ActionPlan, error) {
	actions := make([]schemas.Action, 0, len(output.ActionIntents))
	for idx, intent := range output.ActionIntents {
		action, err := intent.toAction(idx)
		if err != nil {
			return schemas.ActionPlan{}, err
		}
		actions = append(actions, action)
	}
	risk := output.RiskClass
	if risk == "" {
		risk = maxActionRisk(actions)
	}
	if risk == "" {
		risk = schemas.Low
	}
	summary := strings.TrimSpace(output.Summary)
	if summary == "" {
		summary = fmt.Sprintf("deterministic pre-script actions for %s", run.JobID)
	}
	evidenceRefs := normalizeStrings(output.EvidenceRefs)
	if len(evidenceRefs) == 0 {
		evidenceRefs = []string{"prescript:run:" + run.ID}
	}
	plan := schemas.ActionPlan{
		SchemaVersion: agent.ActionPlanSchemaVersion,
		JobId:         run.JobID,
		RunId:         run.ID,
		Summary:       summary,
		RiskClass:     risk,
		EvidenceRefs:  &evidenceRefs,
		Actions:       actions,
	}
	if input.SchemaVersion == SchemaVersion {
		requiresApproval := planRequiresApproval(plan)
		plan.RequiresApproval = &requiresApproval
	}
	if err := agent.ValidatePlan(plan); err != nil {
		return schemas.ActionPlan{}, err
	}
	return plan, nil
}

func (i ActionIntent) toAction(idx int) (schemas.Action, error) {
	kind := i.Kind
	if kind == "" {
		kind = schemas.ActionKind(strings.TrimSpace(i.Type))
	}
	if !kind.Valid() {
		return schemas.Action{}, fmt.Errorf("%w: actionIntents[%d] has unknown kind %q", ErrInvalidOutput, idx, kind)
	}
	key := strings.TrimSpace(i.IdempotencyKey)
	if key == "" {
		return schemas.Action{}, fmt.Errorf("%w: actionIntents[%d].idempotencyKey is required", ErrInvalidOutput, idx)
	}
	action := schemas.Action{
		Kind:           kind,
		Target:         cloneMap(i.Target),
		IdempotencyKey: key,
	}
	if i.Args != nil {
		args := cloneMap(i.Args)
		action.Args = &args
	}
	if len(i.Preconditions) > 0 {
		values := normalizeStrings(i.Preconditions)
		action.Preconditions = &values
	}
	if len(i.Postconditions) > 0 {
		values := normalizeStrings(i.Postconditions)
		action.Postconditions = &values
	}
	if action.Target == nil {
		action.Target = map[string]any{}
	}
	return action, nil
}

type ManifestHints struct {
	Included      []string `json:"included,omitempty"`
	Excluded      []string `json:"excluded,omitempty"`
	Redactions    []string `json:"redactions,omitempty"`
	TokenEstimate int      `json:"tokenEstimate,omitempty"`
	TokenBudget   int      `json:"tokenBudget,omitempty"`
}

type ContextManifest struct {
	SchemaVersion   int           `json:"schemaVersion"`
	RunID           string        `json:"runId"`
	JobID           string        `json:"jobId"`
	ContextHash     string        `json:"contextHash"`
	SourceSnapshots []SnapshotRef `json:"sourceSnapshots"`
	SkillsLoaded    []string      `json:"skillsLoaded"`
	Included        []string      `json:"included,omitempty"`
	Excluded        []string      `json:"excluded,omitempty"`
	Redactions      []string      `json:"redactions,omitempty"`
	TokenEstimate   int           `json:"tokenEstimate,omitempty"`
	TokenBudget     int           `json:"tokenBudget,omitempty"`
	AgentRan        bool          `json:"agentRan"`
	CreatedAt       time.Time     `json:"createdAt"`
}

type SnapshotRef struct {
	Name   string `json:"name"`
	SHA256 string `json:"sha256"`
}

type ManifestInput struct {
	Job      scheduler.Job
	Run      scheduler.Run
	Input    Input
	Output   Output
	Now      time.Time
	AgentRan bool
}

func BuildContextManifest(in ManifestInput) ContextManifest {
	snapshots := make([]SnapshotRef, 0, len(in.Input.Canonical)+len(in.Input.Capabilities))
	for _, name := range sortedMapKeys(in.Input.Canonical) {
		value := in.Input.Canonical[name]
		snapshots = append(snapshots, SnapshotRef{Name: "canonical." + name, SHA256: hashJSON(value)})
	}
	for _, name := range sortedMapKeys(in.Input.Capabilities) {
		value := in.Input.Capabilities[name]
		snapshots = append(snapshots, SnapshotRef{Name: "capabilities." + name, SHA256: hashJSON(value)})
	}
	return ContextManifest{
		SchemaVersion:   SchemaVersion,
		RunID:           in.Run.ID,
		JobID:           in.Run.JobID,
		ContextHash:     hashJSON(in.Output.Context),
		SourceSnapshots: snapshots,
		SkillsLoaded:    append([]string(nil), in.Job.Definition.Skills...),
		Included:        normalizeStrings(in.Output.Manifest.Included),
		Excluded:        normalizeStrings(in.Output.Manifest.Excluded),
		Redactions:      normalizeStrings(in.Output.Manifest.Redactions),
		TokenEstimate:   in.Output.Manifest.TokenEstimate,
		TokenBudget:     in.Output.Manifest.TokenBudget,
		AgentRan:        in.AgentRan,
		CreatedAt:       in.Now.UTC(),
	}
}

func planRequiresApproval(plan schemas.ActionPlan) bool {
	catalog := agent.DefaultActionCatalog()
	for _, action := range plan.Actions {
		spec, ok := catalog[action.Kind]
		if !ok {
			return true
		}
		if spec.RequiresApproval(plan.RiskClass, nil) {
			return true
		}
	}
	return false
}

func maxActionRisk(actions []schemas.Action) schemas.ApprovalRiskClass {
	catalog := agent.DefaultActionCatalog()
	var risk schemas.ApprovalRiskClass
	for _, action := range actions {
		spec, ok := catalog[action.Kind]
		if !ok {
			return schemas.High
		}
		if riskRank(spec.RiskClass) > riskRank(risk) {
			risk = spec.RiskClass
		}
	}
	return risk
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

func lastNonEmptyLine(stdout []byte) string {
	lines := bytes.Split(stdout, []byte{'\n'})
	for idx := len(lines) - 1; idx >= 0; idx-- {
		line := strings.TrimSpace(string(lines[idx]))
		if line != "" {
			return line
		}
	}
	return ""
}

func countNonEmptyLines(stdout []byte) int {
	scanner := bufio.NewScanner(bytes.NewReader(stdout))
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	count := 0
	for scanner.Scan() {
		if strings.TrimSpace(scanner.Text()) != "" {
			count++
		}
	}
	return count
}

func sortedMapKeys(values map[string]any) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func hashJSON(value any) string {
	data, err := json.Marshal(value)
	if err != nil {
		data = []byte(fmt.Sprintf("%#v", value))
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func cloneMap(in map[string]any) map[string]any {
	if in == nil {
		return nil
	}
	out := make(map[string]any, len(in))
	for key, value := range in {
		out[key] = cloneAny(value)
	}
	return out
}

func cloneAny(value any) any {
	switch typed := value.(type) {
	case map[string]any:
		return cloneMap(typed)
	case []any:
		out := make([]any, len(typed))
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

func normalizeStrings(values []string) []string {
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" {
			out = append(out, value)
		}
	}
	return out
}
