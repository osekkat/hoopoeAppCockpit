// Package sbh wraps the stale-bytes housekeeper. Cleanup is deterministic but
// mutating, so apply operations require explicit categories and are intended
// to run only through audited tending policy.
package sbh

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"sort"
	"strings"
	"time"

	"github.com/hoopoe-cockpit/hoopoe/apps/daemon/internal/capabilities"
)

const (
	ToolName = "sbh"

	CapabilityStatusRead    = "sbh.status.read"
	CapabilityCleanup       = "sbh.cleanup"
	CapabilityCleanupDryRun = "sbh.cleanup.dry_run"
	CapabilityCleanupApply  = "sbh.cleanup.apply"
)

var ErrInvalidRequest = errors.New("sbh: invalid request")

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

type Status struct {
	CleanableBytes int64               `json:"cleanable_bytes"`
	ByCategory     map[string]Category `json:"by_category,omitempty"`
	LastCleanupTS  string              `json:"last_cleanup_ts,omitempty"`
	Warnings       []string            `json:"warnings,omitempty"`
}

type Category struct {
	Bytes int64 `json:"bytes"`
	Files int   `json:"files,omitempty"`
}

type CleanupRequest struct {
	Categories []string
	Reason     string
}

type CleanupResult struct {
	Applied       bool             `json:"applied"`
	FreedBytes    int64            `json:"freed_bytes,omitempty"`
	ByCategory    map[string]int64 `json:"by_category,omitempty"`
	Skipped       []string         `json:"skipped,omitempty"`
	PostStatusRef string           `json:"post_status_ref,omitempty"`
}

type CleanupIntent struct {
	CapabilityID   string         `json:"capabilityId"`
	Action         string         `json:"action"`
	Args           map[string]any `json:"args"`
	Preconditions  []string       `json:"preconditions"`
	Postconditions []string       `json:"postconditions"`
}

func StatusArgv() []string {
	return []string{ToolName, "status", "--json"}
}

func CleanupDryRunArgv(req CleanupRequest) ([]string, error) {
	categories, err := normalizeCategories(req.Categories, true)
	if err != nil {
		return nil, err
	}
	argv := []string{ToolName, "cleanup", "--dry-run", "--json"}
	for _, category := range categories {
		argv = append(argv, "--category", category)
	}
	return argv, nil
}

func CleanupApplyArgv(req CleanupRequest) ([]string, error) {
	categories, err := normalizeCategories(req.Categories, false)
	if err != nil {
		return nil, err
	}
	argv := []string{ToolName, "cleanup", "--apply", "--json"}
	for _, category := range categories {
		argv = append(argv, "--category", category)
	}
	reason := strings.TrimSpace(req.Reason)
	if reason != "" {
		argv = append(argv, "--reason", reason)
	}
	return argv, nil
}

func CleanupApplyIntent(req CleanupRequest) (CleanupIntent, error) {
	categories, err := normalizeCategories(req.Categories, false)
	if err != nil {
		return CleanupIntent{}, err
	}
	reason := strings.TrimSpace(req.Reason)
	if reason == "" {
		reason = "disk pressure crossed deterministic threshold"
	}
	return CleanupIntent{
		CapabilityID: CapabilityCleanup,
		Action:       "sbh.cleanup",
		Args: map[string]any{
			"categories": categories,
			"reason":     reason,
		},
		Preconditions: []string{
			"srp.signals.read reports disk pressure over threshold",
			"sbh cleanup dry-run returns at least one candidate in the requested categories",
		},
		Postconditions: []string{
			"sbh status --json reports lower cleanable bytes or explains skipped in-use files",
			"audit log records cleanup categories and freed bytes",
		},
	}, nil
}

func (a *Adapter) Status(ctx context.Context) (Status, error) {
	var status Status
	if err := a.runJSON(ctx, StatusArgv(), &status); err != nil {
		return Status{}, err
	}
	if status.ByCategory == nil {
		status.ByCategory = map[string]Category{}
	}
	return status, nil
}

func (a *Adapter) DryRun(ctx context.Context, req CleanupRequest) (CleanupResult, error) {
	argv, err := CleanupDryRunArgv(req)
	if err != nil {
		return CleanupResult{}, err
	}
	var result CleanupResult
	if err := a.runJSON(ctx, argv, &result); err != nil {
		return CleanupResult{}, err
	}
	return result, nil
}

func (a *Adapter) Apply(ctx context.Context, req CleanupRequest) (CleanupResult, error) {
	argv, err := CleanupApplyArgv(req)
	if err != nil {
		return CleanupResult{}, err
	}
	var result CleanupResult
	if err := a.runJSON(ctx, argv, &result); err != nil {
		return CleanupResult{}, err
	}
	result.Applied = true
	return result, nil
}

func (a *Adapter) Probe(ctx context.Context) (*capabilities.ToolReport, error) {
	report := &capabilities.ToolReport{
		Tool:          capabilities.ToolSBH,
		Source:        "cli",
		LastCheckedAt: a.now().UTC().Format(time.RFC3339),
		Capabilities: map[string]capabilities.Capability{
			CapabilityStatusRead:    {Status: capabilities.StatusMissing},
			CapabilityCleanup:       {Status: capabilities.StatusMissing},
			CapabilityCleanupDryRun: {Status: capabilities.StatusMissing},
			CapabilityCleanupApply:  {Status: capabilities.StatusMissing},
		},
	}
	if _, err := a.Status(ctx); err != nil {
		state := statusForError(err)
		for capID := range report.Capabilities {
			report.Capabilities[capID] = capabilities.Capability{Status: state, Notes: err.Error()}
		}
		return report, nil
	}
	report.Capabilities[CapabilityStatusRead] = capabilities.Capability{Status: capabilities.StatusOK}
	report.Capabilities[CapabilityCleanupDryRun] = capabilities.Capability{Status: capabilities.StatusOK}
	report.Capabilities[CapabilityCleanup] = capabilities.Capability{
		Status: capabilities.StatusBlockedByPolicy,
		Notes:  "mutating cleanup; explicit category list required",
	}
	report.Capabilities[CapabilityCleanupApply] = capabilities.Capability{
		Status: capabilities.StatusBlockedByPolicy,
		Notes:  "mutating cleanup; explicit category list required",
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
		return fmt.Errorf("sbh: run %s: %w", argv[0], err)
	}
	if result.ExitCode != 0 {
		return commandError{argv: argv, result: result}
	}
	if len(bytes.TrimSpace(result.Stdout)) == 0 {
		return fmt.Errorf("sbh: empty JSON response from %v", argv)
	}
	if err := json.Unmarshal(result.Stdout, target); err != nil {
		return fmt.Errorf("sbh: decode JSON from %v: %w", argv, err)
	}
	return nil
}

type commandError struct {
	argv   []string
	result CommandResult
}

func (e commandError) Error() string {
	return fmt.Sprintf("sbh: command %v exited %d: %s", e.argv, e.result.ExitCode, strings.TrimSpace(string(e.result.Stderr)))
}

func normalizeCategories(input []string, allowEmpty bool) ([]string, error) {
	seen := map[string]struct{}{}
	out := make([]string, 0, len(input))
	for _, raw := range input {
		category := strings.TrimSpace(raw)
		if category == "" {
			continue
		}
		if category == "--all" || category == "all" || strings.Contains(category, " ") {
			return nil, fmt.Errorf("%w: unsafe cleanup category %q", ErrInvalidRequest, raw)
		}
		if _, ok := seen[category]; ok {
			continue
		}
		seen[category] = struct{}{}
		out = append(out, category)
	}
	sort.Strings(out)
	if !allowEmpty && len(out) == 0 {
		return nil, fmt.Errorf("%w: at least one cleanup category is required", ErrInvalidRequest)
	}
	return out, nil
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
