// Package casr scaffolds the Cross-Agent Session Resumer adapter. casr
// converts an in-flight CLI session to a different harness/provider while
// preserving context — used by the §7.3 / §8.4 recovery action 'Resume the
// session via casr' when account-switching alone is not enough (e.g.,
// Claude Max exhausted but GPT Pro available).
//
// Status: post-MVP per plan.md §13 'Can defer'. The full cross-CLI
// conversion path is not stable enough to ship in v1; CAAM-only account
// switching ships first. This file freezes the integration shape so wiring
// the live path is mechanical when the time comes:
//   - capability ID (`casr.session.resume`) reported through the capability
//     registry; gated `blocked-by-policy` when the CLI is present so even
//     'autopilot' callers cannot bypass the post-MVP flag through the
//     normal RPC surface.
//   - typed ActionIntent (`casr.resume_session`) so the daemon's action
//     executor can register the action type without a v1 runtime hook.
//   - argv constructor + JSON parser exercised by contract tests so the
//     post-MVP enable path is mechanical.
package casr

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"time"

	"github.com/hoopoe-cockpit/hoopoe/apps/daemon/internal/capabilities"
)

const (
	ToolName = "casr"

	CapabilitySessionResume = "casr.session.resume"
	CapabilityStatusRead    = "casr.status.read"

	ActionResumeSession = "casr.resume_session"
)

var ErrInvalidRequest = errors.New("casr: invalid request")

type Runner interface {
	Run(ctx context.Context, argv []string) (CommandResult, error)
}

type CommandResult struct {
	ExitCode int
	Stdout   []byte
	Stderr   []byte
}

type ExecRunner struct{}

func (ExecRunner) Run(ctx context.Context, argv []string) (CommandResult, error) {
	if len(argv) == 0 || strings.TrimSpace(argv[0]) == "" {
		return CommandResult{}, fmt.Errorf("%w: empty argv", ErrInvalidRequest)
	}
	cmd := exec.CommandContext(ctx, argv[0], argv[1:]...)
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	result := CommandResult{Stdout: stdout.Bytes(), Stderr: stderr.Bytes()}
	if err == nil {
		return result, nil
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		result.ExitCode = exitErr.ExitCode()
		return result, nil
	}
	result.ExitCode = -1
	return result, err
}

type Adapter struct {
	Runner Runner
	Now    func() time.Time
}

func New(runner Runner) *Adapter {
	if runner == nil {
		runner = ExecRunner{}
	}
	return &Adapter{Runner: runner, Now: time.Now}
}

// Harness identifies a CLI in casr's vocabulary. Values must match the
// CAAM/§7.3 enumeration so account-switch and casr-resume agree on the same
// harness names.
type Harness string

const (
	HarnessClaudeCode Harness = "claude-code"
	HarnessCodexCLI   Harness = "codex"
	HarnessGeminiCLI  Harness = "gemini"
)

func (h Harness) Valid() bool {
	switch h {
	case HarnessClaudeCode, HarnessCodexCLI, HarnessGeminiCLI:
		return true
	}
	return false
}

// Status mirrors `casr status --json` for capability probing.
type Status struct {
	Healthy  bool     `json:"healthy"`
	Version  string   `json:"version,omitempty"`
	Warnings []string `json:"warnings,omitempty"`
}

// ResumeRequest is the structured input for `casr.resume_session`. The
// SessionID identifies the in-flight session in the source harness; agents
// of `From` must release the session before agents of `To` claim it.
type ResumeRequest struct {
	SessionID   string
	From        Harness
	To          Harness
	FromAccount string
	ToAccount   string
	Reason      string
}

// ResumeResult mirrors `casr resume --json` and lets the action executor
// verify postconditions without re-running casr.
type ResumeResult struct {
	Resumed       bool   `json:"resumed"`
	NewSessionID  string `json:"new_session_id,omitempty"`
	FromHarness   string `json:"from_harness,omitempty"`
	ToHarness     string `json:"to_harness,omitempty"`
	ContextLines  int    `json:"context_lines,omitempty"`
	PostStatusRef string `json:"post_status_ref,omitempty"`
	Message       string `json:"message,omitempty"`
}

// ActionIntent is the deterministic actuator surface (§8.3.1). Every casr
// invocation goes through ActionPlan; the adapter never exposes a
// 'resume-by-shell-string' API.
type ActionIntent struct {
	Kind           string         `json:"kind"`
	CapabilityID   string         `json:"capabilityId"`
	Target         map[string]any `json:"target"`
	Args           map[string]any `json:"args"`
	Preconditions  []string       `json:"preconditions"`
	Postconditions []string       `json:"postconditions"`
}

func StatusArgv() []string {
	return []string{ToolName, "status", "--json"}
}

// ResumeArgv builds the argv for `casr resume`. The validator rejects
// missing/invalid harness/session/reason fields — the adapter must never
// invoke casr with partial inputs because a failed resume can leave the
// source session orphaned.
func ResumeArgv(req ResumeRequest) ([]string, error) {
	sessionID := strings.TrimSpace(req.SessionID)
	if sessionID == "" {
		return nil, fmt.Errorf("%w: sessionId is required", ErrInvalidRequest)
	}
	if !req.From.Valid() {
		return nil, fmt.Errorf("%w: from harness %q is not recognized", ErrInvalidRequest, req.From)
	}
	if !req.To.Valid() {
		return nil, fmt.Errorf("%w: to harness %q is not recognized", ErrInvalidRequest, req.To)
	}
	if req.From == req.To {
		return nil, fmt.Errorf("%w: from and to harness must differ", ErrInvalidRequest)
	}
	reason := strings.TrimSpace(req.Reason)
	if reason == "" {
		return nil, fmt.Errorf("%w: reason is required", ErrInvalidRequest)
	}
	argv := []string{
		ToolName,
		"resume",
		"--session", sessionID,
		"--from", string(req.From),
		"--to", string(req.To),
		"--reason", reason,
		"--json",
	}
	if from := strings.TrimSpace(req.FromAccount); from != "" {
		argv = append(argv, "--from-account", from)
	}
	if to := strings.TrimSpace(req.ToAccount); to != "" {
		argv = append(argv, "--to-account", to)
	}
	return argv, nil
}

// ResumeSessionIntent is the typed action surface the §8.3 ActionPlan
// executor consumes. Postconditions reference both harness sides so the
// executor can verify the resume actually completed against canonical state.
func ResumeSessionIntent(agentID string, req ResumeRequest) (ActionIntent, error) {
	if strings.TrimSpace(agentID) == "" {
		return ActionIntent{}, fmt.Errorf("%w: agentId is required", ErrInvalidRequest)
	}
	if _, err := ResumeArgv(req); err != nil {
		return ActionIntent{}, err
	}
	args := map[string]any{
		"sessionId": strings.TrimSpace(req.SessionID),
		"from":      string(req.From),
		"to":        string(req.To),
		"reason":    strings.TrimSpace(req.Reason),
	}
	if from := strings.TrimSpace(req.FromAccount); from != "" {
		args["fromAccount"] = from
	}
	if to := strings.TrimSpace(req.ToAccount); to != "" {
		args["toAccount"] = to
	}
	return ActionIntent{
		Kind:         ActionResumeSession,
		CapabilityID: CapabilitySessionResume,
		Target: map[string]any{
			"agentId": strings.TrimSpace(agentID),
		},
		Args: args,
		Preconditions: []string{
			"caut reports the source harness/account is rate-limited",
			"caam reports the target harness/account is healthy",
			"agent has acked the resume request",
		},
		Postconditions: []string{
			"casr reports the new session is driving the agent",
			"original session context preserved (context_lines > 0)",
			"caut reports the target harness usage incremented",
		},
	}, nil
}

func (a *Adapter) Status(ctx context.Context) (Status, error) {
	var status Status
	if err := a.runJSON(ctx, StatusArgv(), &status); err != nil {
		return Status{}, err
	}
	return status, nil
}

// ResumeSession runs `casr resume` against req. The adapter never gates the
// call itself: the capability registry reports `casr.session.resume` as
// `blocked-by-policy` until the post-MVP enable, so the ActionPlan executor
// refuses to execute unless an operator flips the policy. Once enabled,
// callers reach this method through the executor only.
func (a *Adapter) ResumeSession(ctx context.Context, req ResumeRequest) (ResumeResult, error) {
	argv, err := ResumeArgv(req)
	if err != nil {
		return ResumeResult{}, err
	}
	var result ResumeResult
	if err := a.runJSON(ctx, argv, &result); err != nil {
		return ResumeResult{}, err
	}
	if result.FromHarness == "" {
		result.FromHarness = string(req.From)
	}
	if result.ToHarness == "" {
		result.ToHarness = string(req.To)
	}
	return result, nil
}

// Probe declares the capability registry view of casr. Default posture:
//   - no CLI installed → `missing` (post-MVP, expected for v1).
//   - CLI present but unhealthy → `degraded` with the warning surfaced.
//   - CLI present and healthy → `blocked-by-policy` with notes pointing at
//     the post-MVP flag; mutating actions only flow through ActionPlan and
//     are refused until the policy is enabled.
func (a *Adapter) Probe(ctx context.Context) (*capabilities.ToolReport, error) {
	checkedAt := a.now().UTC().Format(time.RFC3339)
	report := &capabilities.ToolReport{
		Tool:          capabilities.ToolCASR,
		Source:        "cli",
		LastCheckedAt: checkedAt,
		Capabilities: map[string]capabilities.Capability{
			CapabilityStatusRead:    {Status: capabilities.StatusMissing},
			CapabilitySessionResume: {Status: capabilities.StatusMissing},
		},
	}
	status, err := a.Status(ctx)
	if err != nil {
		statusValue := statusForError(err)
		note := err.Error()
		for capID := range report.Capabilities {
			report.Capabilities[capID] = capabilities.Capability{Status: statusValue, Notes: note}
		}
		return report, nil
	}
	report.Version = status.Version
	if !status.Healthy {
		warning := strings.Join(status.Warnings, "; ")
		if warning == "" {
			warning = "casr status returned unhealthy"
		}
		report.Capabilities[CapabilityStatusRead] = capabilities.Capability{Status: capabilities.StatusDegraded, Notes: warning}
		report.Capabilities[CapabilitySessionResume] = capabilities.Capability{Status: capabilities.StatusDegraded, Notes: warning}
		return report, nil
	}
	report.Capabilities[CapabilityStatusRead] = capabilities.Capability{Status: capabilities.StatusOK}
	report.Capabilities[CapabilitySessionResume] = capabilities.Capability{
		Status: capabilities.StatusBlockedByPolicy,
		Notes:  "post-MVP recovery action; enable via daemon policy when cross-CLI resume stabilizes",
	}
	return report, nil
}

func (a *Adapter) runJSON(ctx context.Context, argv []string, target any) error {
	if a == nil {
		return fmt.Errorf("%w: nil adapter", ErrInvalidRequest)
	}
	runner := a.Runner
	if runner == nil {
		runner = ExecRunner{}
	}
	result, err := runner.Run(ctx, argv)
	if err != nil {
		return fmt.Errorf("casr: run %s: %w", argv[0], err)
	}
	if result.ExitCode != 0 {
		return commandError{argv: argv, result: result}
	}
	if len(bytes.TrimSpace(result.Stdout)) == 0 {
		return fmt.Errorf("casr: empty JSON response from %v", argv)
	}
	if err := json.Unmarshal(result.Stdout, target); err != nil {
		return fmt.Errorf("casr: decode JSON from %v: %w", argv, err)
	}
	return nil
}

type commandError struct {
	argv   []string
	result CommandResult
}

func (e commandError) Error() string {
	return fmt.Sprintf("casr: command %v exited %d: %s", e.argv, e.result.ExitCode, strings.TrimSpace(string(e.result.Stderr)))
}

func statusForError(err error) capabilities.CapabilityStatus {
	var commandErr commandError
	if errors.As(err, &commandErr) {
		if commandErr.result.ExitCode == 124 {
			return capabilities.StatusDegraded
		}
		return capabilities.StatusMissing
	}
	if strings.Contains(err.Error(), "decode JSON") {
		return capabilities.StatusDegraded
	}
	if errors.Is(err, exec.ErrNotFound) || strings.Contains(err.Error(), "executable file not found") || strings.Contains(err.Error(), "command not found") {
		return capabilities.StatusMissing
	}
	return capabilities.StatusDegraded
}

func (a *Adapter) now() time.Time {
	if a != nil && a.Now != nil {
		return a.Now()
	}
	return time.Now()
}
