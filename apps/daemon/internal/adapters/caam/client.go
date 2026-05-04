// client.go — caam binary wrapper. Production wires os/exec; tests inject
// a fake executor.
//
// Per plan.md §5.1: CAAM is the sole credential pathway. The daemon
// reads + activates profiles via this adapter; it never opens auth
// files directly. The audit log records every `activate` call (the
// caller wires that — this package emits the structured outcome and
// classified errors only).
package caam

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"time"
)

// Defaults applied when a Client field is left empty.
const (
	DefaultBinary  = "caam"
	DefaultTimeout = 30 * time.Second
)

// Sentinel errors callers can use with errors.Is.
var (
	// ErrMissingBinary is returned when the caam binary cannot be
	// found on PATH. Capability probes classify it as `missing`.
	ErrMissingBinary = errors.New("caam: binary not found")

	// ErrProfileNotFound is returned by Activate when CAAM reports
	// the named profile doesn't exist. Distinguished from a generic
	// failure so the caller can surface a precise problem+json code.
	ErrProfileNotFound = errors.New("caam: profile not found")

	// ErrToolNotInstalled is returned by Activate when the underlying
	// CLI tool isn't installed (CAAM can't activate a profile for a
	// missing tool).
	ErrToolNotInstalled = errors.New("caam: target tool not installed")
)

// Executor abstracts caam binary invocation so tests can inject canned
// output. Production uses OSExecutor.
type Executor interface {
	Run(ctx context.Context, args []string) (stdout []byte, stderr []byte, exitCode int, err error)
}

// OSExecutor invokes the real `caam` binary via os/exec.
type OSExecutor struct {
	Binary  string
	Timeout time.Duration
	Env     []string
}

// Run executes `caam <args>`.
func (o *OSExecutor) Run(ctx context.Context, args []string) ([]byte, []byte, int, error) {
	binary := o.Binary
	if binary == "" {
		binary = DefaultBinary
	}
	timeout := o.Timeout
	if timeout == 0 {
		timeout = DefaultTimeout
	}
	runCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	cmd := exec.CommandContext(runCtx, binary, args...)
	if o.Env != nil {
		cmd.Env = o.Env
	} else {
		// Default env: inherit + force C locale for stable formatting.
		cmd.Env = append([]string(nil), "PATH=" + osPath(), "LC_ALL=C", "LANG=C", "NO_COLOR=1")
	}

	var stdout, stderr strings.Builder
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	exit := 0
	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			exit = exitErr.ExitCode()
			err = nil
		} else if isExecNotFoundErr(err) {
			return []byte(stdout.String()), []byte(stderr.String()), -1, ErrMissingBinary
		}
	}
	return []byte(stdout.String()), []byte(stderr.String()), exit, err
}

var osPath = func() string { return "" }

func isExecNotFoundErr(err error) bool {
	if err == nil {
		return false
	}
	s := err.Error()
	return strings.Contains(s, "executable file not found") ||
		strings.Contains(s, "no such file or directory")
}

// Client is the high-level CAAM adapter.
type Client struct {
	Exec Executor
	Now  func() time.Time
}

// New returns a Client wired to the OS executor with default settings.
func New() *Client { return &Client{Exec: &OSExecutor{}, Now: time.Now} }

// NewWithExecutor returns a Client backed by the supplied Executor.
func NewWithExecutor(exec Executor) *Client { return &Client{Exec: exec, Now: time.Now} }

// List runs `caam ls --json [--tool <tool>]` and parses the result.
// Empty `tool` lists profiles for every CAAM-recognised tool.
func (c *Client) List(ctx context.Context, tool Tool) (*ListResponse, error) {
	args := []string{"ls", "--json"}
	if tool != "" {
		args = append(args, string(tool))
	}
	out, err := c.run(ctx, args)
	if err != nil {
		return nil, err
	}
	var resp ListResponse
	if err := json.Unmarshal(out, &resp); err != nil {
		return nil, fmt.Errorf("caam: decode list: %w", err)
	}
	return &resp, nil
}

// Status runs `caam status --json [--tool <tool>]`.
func (c *Client) Status(ctx context.Context, tool Tool) (*StatusResponse, error) {
	args := []string{"status", "--json"}
	if tool != "" {
		args = append(args, string(tool))
	}
	out, err := c.run(ctx, args)
	if err != nil {
		return nil, err
	}
	var resp StatusResponse
	if err := json.Unmarshal(out, &resp); err != nil {
		return nil, fmt.Errorf("caam: decode status: %w", err)
	}
	return &resp, nil
}

// Limits runs `caam limits --format json [--tool <tool>]`. CAAM returns
// a bare JSON array `[]` when there are no limits — we normalise that
// to an empty LimitsResponse.Limits slice.
func (c *Client) Limits(ctx context.Context, tool Tool) (*LimitsResponse, error) {
	args := []string{"limits", "--format", "json"}
	if tool != "" {
		args = append(args, string(tool))
	}
	out, err := c.run(ctx, args)
	if err != nil {
		return nil, err
	}

	trimmed := strings.TrimSpace(string(out))
	if strings.HasPrefix(trimmed, "[") {
		var limits []Limit
		if err := json.Unmarshal(out, &limits); err != nil {
			return nil, fmt.Errorf("caam: decode limits (array form): %w", err)
		}
		return &LimitsResponse{Limits: limits}, nil
	}

	var resp LimitsResponse
	if err := json.Unmarshal(out, &resp); err != nil {
		return nil, fmt.Errorf("caam: decode limits: %w", err)
	}
	return &resp, nil
}

// Detect runs `caam detect --json` and returns the agent inventory.
func (c *Client) Detect(ctx context.Context) (*DetectResponse, error) {
	out, err := c.run(ctx, []string{"detect", "--json"})
	if err != nil {
		return nil, err
	}
	var resp DetectResponse
	if err := json.Unmarshal(out, &resp); err != nil {
		return nil, fmt.Errorf("caam: decode detect: %w", err)
	}
	return &resp, nil
}

// Activate runs `caam activate <tool> <profile>`. Mutating — the caller
// MUST have an `caam.switch_account` ActionPlan + audit entry per
// §8.3.1 BEFORE invoking this. The adapter classifies failures
// (profile-not-found vs tool-not-installed vs other) and returns a
// structured ActivateResult on success.
func (c *Client) Activate(ctx context.Context, tool Tool, profile string) (*ActivateResult, error) {
	if tool == "" {
		return nil, errors.New("caam: Activate requires a non-empty tool")
	}
	if profile == "" {
		return nil, errors.New("caam: Activate requires a non-empty profile")
	}
	stdout, stderr, exit, err := c.Exec.Run(ctx, []string{"activate", string(tool), profile})
	if err != nil {
		if errors.Is(err, ErrMissingBinary) {
			return nil, err
		}
		return nil, fmt.Errorf("caam: activate: %w (stderr: %s)", err, truncateStderr(stderr))
	}
	if exit != 0 {
		stderrStr := string(stderr)
		switch {
		case strings.Contains(stderrStr, "profile not found") ||
			strings.Contains(stderrStr, "no such profile"):
			return nil, fmt.Errorf("%w: tool=%s profile=%s", ErrProfileNotFound, tool, profile)
		case strings.Contains(stderrStr, "not installed") ||
			strings.Contains(stderrStr, "binary not found"):
			return nil, fmt.Errorf("%w: tool=%s", ErrToolNotInstalled, tool)
		default:
			return nil, fmt.Errorf("caam: activate exited %d (stderr: %s)", exit, truncateStderr(stderr))
		}
	}
	return &ActivateResult{
		Tool:        tool,
		Profile:     profile,
		ActivatedAt: c.now(),
		OK:          true,
		Notes:       strings.TrimSpace(string(stdout)),
	}, nil
}

func (c *Client) now() time.Time {
	if c.Now != nil {
		return c.Now().UTC()
	}
	return time.Now().UTC()
}

// run is the central guard + executor invocation.
func (c *Client) run(ctx context.Context, args []string) ([]byte, error) {
	stdout, stderr, exit, err := c.Exec.Run(ctx, args)
	if err != nil {
		if errors.Is(err, ErrMissingBinary) {
			return nil, err
		}
		return nil, fmt.Errorf("caam: invoke %q: %w (stderr: %s)",
			strings.Join(args, " "), err, truncateStderr(stderr))
	}
	if exit != 0 {
		return nil, fmt.Errorf("caam: %q exited %d (stderr: %s)",
			strings.Join(args, " "), exit, truncateStderr(stderr))
	}
	return stdout, nil
}

func truncateStderr(b []byte) string {
	const max = 512
	s := string(b)
	if len(s) <= max {
		return s
	}
	return s[:max] + "...(truncated)"
}
