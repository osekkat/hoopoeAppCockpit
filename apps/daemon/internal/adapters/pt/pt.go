// Package pt wraps the process-terminator CLI. It is a deterministic
// actuator used only through typed tending actions; the adapter never exposes
// free-form shell execution.
package pt

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"github.com/hoopoe-cockpit/hoopoe/apps/daemon/internal/capabilities"
)

const (
	ToolName = "pt"

	CapabilityList       = "pt.list"
	CapabilityKill       = "pt.kill"
	CapabilityStatusRead = "pt.status.read"

	ActionKillWedgedProcess = "agent.kill_wedged_process"
)

var ErrInvalidRequest = errors.New("pt: invalid request")

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
	result := CommandResult{
		ExitCode: 0,
		Stdout:   stdout.Bytes(),
		Stderr:   stderr.Bytes(),
	}
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

type Process struct {
	PID       int      `json:"pid"`
	PGID      int      `json:"pgid,omitempty"`
	Command   string   `json:"cmd,omitempty"`
	Cmdline   string   `json:"cmdline,omitempty"`
	ParentPID int      `json:"parent,omitempty"`
	AgeSec    int64    `json:"age_s,omitempty"`
	Tags      []string `json:"tags,omitempty"`
}

type Status struct {
	Healthy  bool     `json:"healthy"`
	Version  string   `json:"version,omitempty"`
	Warnings []string `json:"warnings,omitempty"`
}

type KillRequest struct {
	Target          string
	PID             int
	CmdlineContains string
	Reason          string
}

type KillResult struct {
	Target     string `json:"target,omitempty"`
	Terminated bool   `json:"terminated"`
	Signal     string `json:"signal,omitempty"`
	Message    string `json:"message,omitempty"`
}

type ActionIntent struct {
	Kind           string         `json:"kind"`
	CapabilityID   string         `json:"capabilityId"`
	Target         map[string]any `json:"target"`
	Args           map[string]any `json:"args"`
	Preconditions  []string       `json:"preconditions"`
	Postconditions []string       `json:"postconditions"`
}

func ListArgv() []string {
	return []string{ToolName, "list", "--json"}
}

func StatusArgv() []string {
	return []string{ToolName, "status", "--json"}
}

func KillArgv(req KillRequest) ([]string, error) {
	target := strings.TrimSpace(req.Target)
	if target == "" && req.PID > 0 {
		target = strconv.Itoa(req.PID)
	}
	if target == "" {
		return nil, fmt.Errorf("%w: target is required", ErrInvalidRequest)
	}
	cmdline := strings.TrimSpace(req.CmdlineContains)
	if cmdline == "" {
		return nil, fmt.Errorf("%w: cmdline substring is required", ErrInvalidRequest)
	}
	reason := strings.TrimSpace(req.Reason)
	if reason == "" {
		return nil, fmt.Errorf("%w: reason is required", ErrInvalidRequest)
	}
	return []string{
		ToolName,
		"kill",
		target,
		"--cmdline-contains",
		cmdline,
		"--reason",
		reason,
		"--json",
	}, nil
}

func KillWedgedProcessIntent(agentID string, req KillRequest, graceSeconds int) (ActionIntent, error) {
	if strings.TrimSpace(agentID) == "" {
		return ActionIntent{}, fmt.Errorf("%w: agentId is required", ErrInvalidRequest)
	}
	if _, err := KillArgv(req); err != nil {
		return ActionIntent{}, err
	}
	if graceSeconds < 0 {
		return ActionIntent{}, fmt.Errorf("%w: graceSeconds must be non-negative", ErrInvalidRequest)
	}
	return ActionIntent{
		Kind:         ActionKillWedgedProcess,
		CapabilityID: CapabilityKill,
		Target: map[string]any{
			"agentId": agentID,
		},
		Args: map[string]any{
			"graceSeconds": graceSeconds,
			"reason":       strings.TrimSpace(req.Reason),
			"ptTarget":     killTarget(req),
			"cmdlineMatch": strings.TrimSpace(req.CmdlineContains),
		},
		Preconditions: []string{
			"pt reports the agent.worker process group is alive",
			"wedged-evidence threshold has been crossed in the detection layer",
		},
		Postconditions: []string{
			"pt reports the process group no longer exists",
			"ntm.snapshot no longer lists the agent as running",
		},
	}, nil
}

func (a *Adapter) List(ctx context.Context) ([]Process, error) {
	var processes []Process
	if err := a.runJSON(ctx, ListArgv(), &processes); err != nil {
		return nil, err
	}
	return processes, nil
}

func (a *Adapter) Status(ctx context.Context) (Status, error) {
	var status Status
	if err := a.runJSON(ctx, StatusArgv(), &status); err != nil {
		return Status{}, err
	}
	return status, nil
}

func (a *Adapter) Kill(ctx context.Context, req KillRequest) (KillResult, error) {
	argv, err := KillArgv(req)
	if err != nil {
		return KillResult{}, err
	}
	var result KillResult
	if err := a.runJSON(ctx, argv, &result); err != nil {
		return KillResult{}, err
	}
	if result.Target == "" {
		result.Target = killTarget(req)
	}
	return result, nil
}

func (a *Adapter) Probe(ctx context.Context) (*capabilities.ToolReport, error) {
	checkedAt := a.now().UTC().Format(time.RFC3339)
	report := &capabilities.ToolReport{
		Tool:          capabilities.ToolPT,
		Source:        "cli",
		LastCheckedAt: checkedAt,
		Capabilities: map[string]capabilities.Capability{
			CapabilityList:       {Status: capabilities.StatusMissing},
			CapabilityStatusRead: {Status: capabilities.StatusMissing},
			CapabilityKill:       {Status: capabilities.StatusMissing},
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
	report.Capabilities[CapabilityList] = capabilities.Capability{Status: capabilities.StatusOK}
	report.Capabilities[CapabilityStatusRead] = capabilities.Capability{Status: capabilities.StatusOK}
	report.Capabilities[CapabilityKill] = capabilities.Capability{
		Status: capabilities.StatusBlockedByPolicy,
		Notes:  "mutating actuator; only executable through ActionPlan",
	}
	if !status.Healthy {
		report.Capabilities[CapabilityList] = capabilities.Capability{Status: capabilities.StatusDegraded, Notes: "pt status returned unhealthy"}
		report.Capabilities[CapabilityStatusRead] = capabilities.Capability{Status: capabilities.StatusDegraded, Notes: "pt status returned unhealthy"}
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
		return fmt.Errorf("pt: run %s: %w", argv[0], err)
	}
	if result.ExitCode != 0 {
		return commandError{argv: argv, result: result}
	}
	if len(bytes.TrimSpace(result.Stdout)) == 0 {
		if target, ok := target.(*KillResult); ok {
			target.Terminated = true
			return nil
		}
		return fmt.Errorf("pt: empty JSON response from %v", argv)
	}
	if err := json.Unmarshal(result.Stdout, target); err != nil {
		return fmt.Errorf("pt: decode JSON from %v: %w", argv, err)
	}
	return nil
}

type commandError struct {
	argv   []string
	result CommandResult
}

func (e commandError) Error() string {
	return fmt.Sprintf("pt: command %v exited %d: %s", e.argv, e.result.ExitCode, strings.TrimSpace(string(e.result.Stderr)))
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

func killTarget(req KillRequest) string {
	target := strings.TrimSpace(req.Target)
	if target != "" {
		return target
	}
	if req.PID > 0 {
		return strconv.Itoa(req.PID)
	}
	return ""
}
