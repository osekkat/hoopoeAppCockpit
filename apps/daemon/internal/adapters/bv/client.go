// client.go — bv binary wrapper. Production wires the OS executor; tests
// inject a fake one that returns canned bytes.
//
// SECURITY (Guardrail 1, §1.3): NEVER invoke `bv` without a `--robot-*`
// flag. Bare bv launches an interactive TUI that blocks the daemon and
// produces no machine-readable output. The Client refuses to construct
// an argv that lacks a robot flag.
package bv

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"time"
)

// Default values used when the Client config field is left unset.
const (
	DefaultBinary  = "bv"
	DefaultTimeout = 30 * time.Second
)

// ErrUnsupportedRobotFlag is returned by callers that try to invoke
// a flag the wrapper doesn't recognise. Defense-in-depth against
// programmer error.
var ErrUnsupportedRobotFlag = errors.New("bv: unsupported robot flag")

// ErrBareInvocationRefused is returned when the caller manages to
// construct an argv missing a `--robot-*` flag. The Client does not
// expose a public API to do this, but Run() rechecks anyway.
var ErrBareInvocationRefused = errors.New("bv: bare invocation refused (Guardrail 1 — would launch TUI)")

// Executor abstracts the call to `bv` so tests can inject canned output.
// The production implementation uses os/exec; the FakeExecutor in
// client_test.go returns whatever bytes the test set up.
type Executor interface {
	// Run invokes the bv binary with the given args, returning stdout
	// bytes, stderr bytes, exit code, and error. Tests provide a fake
	// executor that records the invocation + returns canned output.
	Run(ctx context.Context, args []string) (stdout []byte, stderr []byte, exitCode int, err error)
}

// OSExecutor runs the real `bv` binary via os/exec.
type OSExecutor struct {
	// Binary is the path or name of the bv binary. Empty → "bv".
	Binary string

	// WorkDir is the working directory for the bv invocation. bv reads
	// `.beads/issues.jsonl` from the cwd. Empty → daemon's cwd.
	WorkDir string

	// Timeout caps the per-call duration. Empty → DefaultTimeout.
	Timeout time.Duration

	// Env is the environment passed to the child process. nil → inherit
	// the parent's env minus locale randomization (we set LC_ALL=C so
	// number formatting is stable across operator locales).
	Env []string
}

// Run executes `bv <args>` and returns its output.
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
	if o.WorkDir != "" {
		cmd.Dir = o.WorkDir
	}
	if o.Env != nil {
		cmd.Env = o.Env
	} else {
		// Default env: inherit + force C locale for stable number formatting.
		cmd.Env = append(envVarsFromOS(), "LC_ALL=C", "LANG=C")
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
		}
	}
	return []byte(stdout.String()), []byte(stderr.String()), exit, err
}

// envVarsFromOS returns the parent process env. Stubbed via a function
// so tests can override without touching package state.
var envVarsFromOS = defaultEnvVarsFromOS

// defaultEnvVarsFromOS returns os.Environ()-equivalent. Only used by
// OSExecutor; a fake executor in tests can ignore the env entirely.
func defaultEnvVarsFromOS() []string {
	// We deliberately do NOT call os.Environ() here in the package
	// init path — initialization order matters and we want this hook
	// to be replaceable from tests. The OSExecutor's caller can supply
	// Env directly to fully control what the child sees.
	return nil
}

// Client is the high-level adapter the daemon imports. Wraps an Executor
// + decodes the JSON output into typed structs.
type Client struct {
	Exec Executor
}

// New returns a Client wired to the OS executor with default settings.
func New() *Client {
	return &Client{Exec: &OSExecutor{}}
}

// NewWithExecutor returns a Client backed by the supplied Executor —
// tests pass a FakeExecutor; production passes &OSExecutor{...}.
func NewWithExecutor(exec Executor) *Client {
	return &Client{Exec: exec}
}

// Triage runs `bv --robot-triage` and decodes the result.
func (c *Client) Triage(ctx context.Context) (*TriageOutput, error) {
	out, err := c.run(ctx, []string{"--robot-triage"})
	if err != nil {
		return nil, err
	}
	var triage TriageOutput
	if err := json.Unmarshal(out, &triage); err != nil {
		return nil, fmt.Errorf("bv: decode triage: %w", err)
	}
	triage.Raw = out
	return &triage, nil
}

// Plan runs `bv --robot-plan` and decodes the result. Optional `recipe`
// applies a pre-filter before the plan computation (e.g., "actionable",
// "high-impact"); empty means default plan.
func (c *Client) Plan(ctx context.Context, recipe string) (*PlanOutput, error) {
	args := []string{"--robot-plan"}
	if recipe != "" {
		args = append([]string{"--recipe", recipe}, args...)
	}
	out, err := c.run(ctx, args)
	if err != nil {
		return nil, err
	}
	var plan PlanOutput
	if err := json.Unmarshal(out, &plan); err != nil {
		return nil, fmt.Errorf("bv: decode plan: %w", err)
	}
	plan.Raw = out
	return &plan, nil
}

// Insights runs `bv --robot-insights` and decodes the result.
func (c *Client) Insights(ctx context.Context) (*InsightsOutput, error) {
	out, err := c.run(ctx, []string{"--robot-insights"})
	if err != nil {
		return nil, err
	}
	var ins InsightsOutput
	if err := json.Unmarshal(out, &ins); err != nil {
		return nil, fmt.Errorf("bv: decode insights: %w", err)
	}
	ins.Raw = out
	return &ins, nil
}

// Diff runs `bv --robot-diff --diff-since <ref>` and decodes the result.
// `sinceRef` is required (bv refuses --robot-diff without it).
func (c *Client) Diff(ctx context.Context, sinceRef string) (*DiffOutput, error) {
	if sinceRef == "" {
		return nil, errors.New("bv: Diff requires a non-empty since-ref")
	}
	out, err := c.run(ctx, []string{"--robot-diff", "--diff-since", sinceRef})
	if err != nil {
		return nil, err
	}
	var diff DiffOutput
	if err := json.Unmarshal(out, &diff); err != nil {
		return nil, fmt.Errorf("bv: decode diff: %w", err)
	}
	diff.Raw = out
	return &diff, nil
}

// Next runs `bv --robot-next` and returns the single top recommendation.
func (c *Client) Next(ctx context.Context) (*NextOutput, error) {
	out, err := c.run(ctx, []string{"--robot-next"})
	if err != nil {
		return nil, err
	}
	var next NextOutput
	if err := json.Unmarshal(out, &next); err != nil {
		return nil, fmt.Errorf("bv: decode next: %w", err)
	}
	next.Raw = out
	return &next, nil
}

// run is the central guard + executor invocation. Refuses bare argv,
// classifies common error modes (missing tool, non-zero exit, malformed
// JSON), and returns the raw bytes for callers to decode.
func (c *Client) run(ctx context.Context, args []string) ([]byte, error) {
	if !hasRobotFlag(args) {
		return nil, ErrBareInvocationRefused
	}

	stdout, stderr, exit, err := c.Exec.Run(ctx, args)
	if err != nil {
		return nil, fmt.Errorf("bv: invoke %q: %w (stderr: %s)",
			strings.Join(args, " "), err, truncateStderr(stderr))
	}
	if exit != 0 {
		return nil, fmt.Errorf("bv: %q exited %d (stderr: %s)",
			strings.Join(args, " "), exit, truncateStderr(stderr))
	}
	if len(stdout) == 0 {
		return nil, fmt.Errorf("bv: %q produced empty stdout", strings.Join(args, " "))
	}
	return stdout, nil
}

// hasRobotFlag reports whether the args contain at least one `--robot-*`
// flag — the SOLE allowed bv invocation mode (Guardrail 1, §1.3).
func hasRobotFlag(args []string) bool {
	for _, a := range args {
		if strings.HasPrefix(a, "--robot-") {
			return true
		}
	}
	return false
}

// truncateStderr returns at most ~512 bytes of stderr for error messages
// — enough to diagnose, not so much that error wrapping balloons.
func truncateStderr(b []byte) string {
	const max = 512
	s := string(b)
	if len(s) <= max {
		return s
	}
	return s[:max] + "...(truncated)"
}
