package agent

import (
	"bytes"
	"context"
	"fmt"
	"strings"
	"text/template"
	"time"

	schemas "github.com/hoopoe-cockpit/hoopoe/packages/schemas/go"
)

type Skill struct {
	Name    string
	Content string
	Source  string
}

type SkillLoader interface {
	LoadSkills(ctx context.Context, names []string) ([]Skill, error)
}

type StaticSkillLoader map[string]Skill

func (l StaticSkillLoader) LoadSkills(_ context.Context, names []string) ([]Skill, error) {
	skills := make([]Skill, 0, len(names))
	for _, name := range names {
		skill, ok := l[name]
		if !ok {
			return nil, fmt.Errorf("agent: skill %q is not available", name)
		}
		skills = append(skills, skill)
	}
	return skills, nil
}

type RuntimeRequest struct {
	JobID          string
	RunID          string
	AgentID        string
	PromptTemplate string
	Context        map[string]any
	Skills         []string
	ReadOnlyTools  []string
}

type AgentInvocation struct {
	JobID         string
	RunID         string
	AgentID       string
	Prompt        string
	Context       map[string]any
	Skills        []Skill
	ReadOnlyTools []string
}

type AgentReply struct {
	Body       string
	ActionPlan *schemas.ActionPlan
}

type AgentRunner interface {
	RunAgent(ctx context.Context, invocation AgentInvocation) (AgentReply, error)
}

type RuntimeReport struct {
	JobID              string
	RunID              string
	AgentID            string
	ActivitySuppressed bool
	ReplyBody          string
	ActionReport       *ExecutionReport
	StartedAt          time.Time
	CompletedAt        time.Time
}

type Runtime struct {
	Skills   SkillLoader
	Runner   AgentRunner
	Executor *Executor
	Audit    AuditSink
	Now      func() time.Time
}

func (r *Runtime) Run(ctx context.Context, req RuntimeRequest) (RuntimeReport, error) {
	if r == nil {
		return RuntimeReport{}, fmt.Errorf("agent: nil runtime")
	}
	if r.Runner == nil {
		return RuntimeReport{}, fmt.Errorf("agent: runner is required")
	}
	if strings.TrimSpace(req.JobID) == "" || strings.TrimSpace(req.RunID) == "" {
		return RuntimeReport{}, fmt.Errorf("agent: jobId and runId are required")
	}
	startedAt := r.now()
	prompt, err := RenderPrompt(req.PromptTemplate, req.Context)
	if err != nil {
		return RuntimeReport{}, err
	}
	skills, err := r.loadSkills(ctx, req.Skills)
	if err != nil {
		return RuntimeReport{}, err
	}
	if err := r.audit(ctx, req, "runtime.started", "started", map[string]any{"skills": req.Skills}); err != nil {
		return RuntimeReport{}, err
	}
	reply, err := r.Runner.RunAgent(ctx, AgentInvocation{
		JobID:         req.JobID,
		RunID:         req.RunID,
		AgentID:       req.AgentID,
		Prompt:        prompt,
		Context:       cloneMap(req.Context),
		Skills:        skills,
		ReadOnlyTools: append([]string(nil), req.ReadOnlyTools...),
	})
	if err != nil {
		_ = r.audit(ctx, req, "runtime.failed", "failed", map[string]any{"error": err.Error()})
		return RuntimeReport{}, err
	}
	report := RuntimeReport{
		JobID:              req.JobID,
		RunID:              req.RunID,
		AgentID:            req.AgentID,
		ActivitySuppressed: IsSilentReply(reply.Body),
		ReplyBody:          reply.Body,
		StartedAt:          startedAt,
	}
	if err := r.audit(ctx, req, "runtime.reply", "success", map[string]any{
		"silent":        report.ActivitySuppressed,
		"hasActionPlan": reply.ActionPlan != nil,
	}); err != nil {
		return RuntimeReport{}, err
	}
	if reply.ActionPlan != nil {
		if r.Executor == nil {
			return RuntimeReport{}, fmt.Errorf("agent: executor is required for ActionPlan replies")
		}
		actionReport, err := r.Executor.Execute(ctx, *reply.ActionPlan)
		if err != nil {
			_ = r.audit(ctx, req, "runtime.action_plan_failed", "failed", map[string]any{"error": err.Error()})
			return RuntimeReport{}, err
		}
		report.ActionReport = &actionReport
	}
	report.CompletedAt = r.now()
	_ = r.audit(ctx, req, "runtime.completed", "success", map[string]any{"silent": report.ActivitySuppressed})
	return report, nil
}

func RenderPrompt(promptTemplate string, context map[string]any) (string, error) {
	tmpl, err := template.New("agent_prompt").Option("missingkey=error").Parse(promptTemplate)
	if err != nil {
		return "", fmt.Errorf("agent: parse prompt template: %w", err)
	}
	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, context); err != nil {
		return "", fmt.Errorf("agent: render prompt template: %w", err)
	}
	return buf.String(), nil
}

// IsSilentReply reports whether an agent's reply body uses the
// [SILENT] sentinel (plan.md §8.3) to suppress Activity-panel noise.
// The sentinel is permitted to carry trailing whitespace on the
// marker line — '[SILENT]   ', '[SILENT]\t\nfoo', and similar are
// still silent. A reply that BEGINS with [SILENT] but continues
// without a newline (e.g. '[SILENT]content') is NOT silent — that's
// content that happens to start with the literal sentinel.
//
// hp-rlh9: the previous implementation only matched '[SILENT]' or
// '[SILENT]\n'-prefixed bodies, so an agent reply that happened to
// emit a trailing space on the marker line ('[SILENT]   \nfoo')
// fell through to the Activity panel.
func IsSilentReply(body string) bool {
	trimmed := strings.TrimSpace(body)
	if trimmed == "[SILENT]" {
		return true
	}
	if !strings.HasPrefix(trimmed, "[SILENT]") {
		return false
	}
	rest := trimmed[len("[SILENT]"):]
	// Skip any horizontal whitespace between the sentinel and the
	// first newline. A direct content character (not whitespace,
	// not newline) means this is an ordinary reply that happens to
	// start with the literal '[SILENT]'.
	stripped := strings.TrimLeft(rest, " \t\r")
	return strings.HasPrefix(stripped, "\n")
}

func (r *Runtime) loadSkills(ctx context.Context, names []string) ([]Skill, error) {
	if len(names) == 0 {
		return nil, nil
	}
	if r.Skills == nil {
		return nil, fmt.Errorf("agent: skill loader is required")
	}
	return r.Skills.LoadSkills(ctx, names)
}

func (r *Runtime) audit(ctx context.Context, req RuntimeRequest, action string, status string, data map[string]any) error {
	if r.Audit == nil {
		return nil
	}
	return r.Audit.RecordAuditEvent(ctx, AuditEvent{
		Time:   r.now(),
		JobID:  req.JobID,
		RunID:  req.RunID,
		Action: action,
		Status: status,
		Reason: "tending agent runtime",
		Data:   data,
	})
}

func (r *Runtime) now() time.Time {
	if r.Now != nil {
		return r.Now()
	}
	return time.Now()
}
