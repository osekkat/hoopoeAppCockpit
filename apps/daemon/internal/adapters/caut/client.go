// client.go — caut binary wrapper. Production wires os/exec; tests
// inject a fake executor.
//
// Per §7.6 + the bead's DOD: when caut is unavailable for a provider,
// the UI labels the cell `unmeasured` rather than fabricating a number.
// The adapter surfaces this:
//
//   - If the caut binary itself is missing: Snapshot returns
//     ErrMissingBinary; capability probe reports `missing` so the
//     top-bar pill stays empty rather than showing zeros.
//   - If caut runs but reports Status: "unmeasured" for a provider
//     (e.g., that provider's API didn't respond): the response carries
//     ProviderSnapshot{Status: "unmeasured", UnmeasuredReason: "..."}.
//
// This file deliberately mirrors the structure of the caam adapter
// (Executor + Client + run + classified errors) for consistency.
package caut

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"
)

// Defaults applied when a Client field is left empty.
const (
	DefaultBinary  = "caut"
	DefaultTimeout = 10 * time.Second
)

// Sentinel errors callers can use with errors.Is.
var (
	// ErrMissingBinary is returned when the caut binary cannot be
	// found on PATH. Capability probe classifies it as `missing`;
	// renderer treats the entire pill as unmeasured.
	ErrMissingBinary = errors.New("caut: binary not found")
)

// Executor abstracts caut binary invocation so tests can inject canned
// output.
type Executor interface {
	Run(ctx context.Context, args []string) (stdout []byte, stderr []byte, exitCode int, err error)
}

// OSExecutor invokes the real `caut` binary via os/exec.
type OSExecutor struct {
	Binary  string
	Timeout time.Duration
	Env     []string
}

// Run executes `caut <args>`.
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
		cmd.Env = append([]string(nil), "PATH="+osPath(), "LC_ALL=C", "LANG=C", "NO_COLOR=1")
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

var osPath = func() string { return os.Getenv("PATH") }

func isExecNotFoundErr(err error) bool {
	if err == nil {
		return false
	}
	s := err.Error()
	return strings.Contains(s, "executable file not found") ||
		strings.Contains(s, "no such file or directory")
}

// Client is the high-level caut adapter.
type Client struct {
	Exec Executor
	Now  func() time.Time
}

// New returns a Client wired to the OS executor with default settings.
func New() *Client { return &Client{Exec: &OSExecutor{}, Now: time.Now} }

// NewWithExecutor returns a Client backed by the supplied Executor.
func NewWithExecutor(exec Executor) *Client { return &Client{Exec: exec, Now: time.Now} }

// Snapshot runs `caut snapshot --json` and parses the result. When the
// binary itself is missing, returns ErrMissingBinary so the caller can
// surface the unmeasured-pill state per §7.6.
func (c *Client) Snapshot(ctx context.Context) (*SnapshotResponse, error) {
	out, err := c.run(ctx, []string{"snapshot", "--json"})
	if err != nil {
		return nil, err
	}
	var resp SnapshotResponse
	if err := json.Unmarshal(out, &resp); err != nil {
		return nil, fmt.Errorf("caut: decode snapshot: %w", err)
	}
	if resp.Snapshot.GeneratedAt.IsZero() {
		resp.Snapshot.GeneratedAt = c.now()
	}
	return &resp, nil
}

// SnapshotForProvider runs `caut snapshot --json --provider <provider>`
// and returns the single ProviderSnapshot (or nil + ErrProviderAbsent
// when the binary returned a snapshot but the provider isn't in it).
func (c *Client) SnapshotForProvider(ctx context.Context, p Provider) (*ProviderSnapshot, error) {
	if p == "" {
		return nil, errors.New("caut: SnapshotForProvider requires a non-empty provider")
	}
	resp, err := c.Snapshot(ctx)
	if err != nil {
		return nil, err
	}
	for i := range resp.Providers {
		if resp.Providers[i].Provider == p {
			return &resp.Providers[i], nil
		}
	}
	// Per §7.6: absent provider == unmeasured (not an error).
	return &ProviderSnapshot{
		Provider:         p,
		Status:           StatusUnmeasured,
		UnmeasuredReason: "provider absent from snapshot",
	}, nil
}

func (c *Client) now() time.Time {
	if c.Now != nil {
		return c.Now().UTC()
	}
	return time.Now().UTC()
}

// run is the central executor invocation + classified error wrap.
func (c *Client) run(ctx context.Context, args []string) ([]byte, error) {
	stdout, stderr, exit, err := c.Exec.Run(ctx, args)
	if err != nil {
		if errors.Is(err, ErrMissingBinary) {
			return nil, err
		}
		return nil, fmt.Errorf("caut: invoke %q: %w (stderr: %s)",
			strings.Join(args, " "), err, truncateStderr(stderr))
	}
	if exit != 0 {
		return nil, fmt.Errorf("caut: %q exited %d (stderr: %s)",
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
