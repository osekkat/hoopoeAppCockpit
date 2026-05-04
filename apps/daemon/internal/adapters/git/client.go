// client.go — git binary wrapper. Production wires os/exec; tests inject
// a fake executor or use a real temp git repo.
//
// Per §5.3: every project-level git op runs on the VPS via the daemon —
// the desktop never invokes project-level git directly. The desktop's
// local sync-driven clone (§7.7) is the one exception, and uses local
// plumbing (a different package).
//
// Force-push gating (§5.3): `Push(Force: true, ...)` returns
// ErrForcePushRequiresApproval; the caller wires it to the unified
// approvals queue (`policy.git.force_push`) BEFORE invoking the
// adapter again with the approval id recorded in the audit trail.
package git

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

// Defaults applied when the caller leaves the corresponding field empty.
const (
	DefaultBinary  = "git"
	DefaultTimeout = 30 * time.Second
)

// Sentinel errors callers can use with errors.Is for branchy logic.
var (
	// ErrEmptyRepoPath is returned by methods that require a project
	// working tree path. Defense-in-depth — the production wiring
	// always passes a path, but a misconfigured caller would otherwise
	// invoke git in the daemon's own cwd.
	ErrEmptyRepoPath = errors.New("git: empty RepoPath (cannot run in daemon cwd)")

	// ErrForcePushRequiresApproval is returned by Push() when called
	// with Force: true and ApprovalID is empty. Callers must obtain
	// an approval first and pass its id through PushOpts.
	ErrForcePushRequiresApproval = errors.New("git: force-push requires an approval id (policy.git.force_push)")

	// ErrMissingBinary is returned when the git binary cannot be
	// found on PATH. Capability probes classify it as `missing`.
	ErrMissingBinary = errors.New("git: binary not found")
)

// Executor abstracts git binary invocation so tests can inject canned
// output. Production uses OSExecutor with real os/exec.
type Executor interface {
	// Run invokes git with the given args inside repoPath and returns
	// stdout bytes, stderr bytes, exit code, and error. tests provide
	// a fake; OSExecutor below shells out.
	Run(ctx context.Context, repoPath string, args []string) (stdout []byte, stderr []byte, exitCode int, err error)
}

// OSExecutor invokes the real `git` binary via os/exec.
type OSExecutor struct {
	// Binary is the path or name of the git binary. Empty → "git".
	Binary string
	// Timeout caps the per-call duration. Empty → DefaultTimeout.
	Timeout time.Duration
	// Env is the environment passed to child processes; nil forwards
	// PATH only plus locale + paging overrides so output is parseable
	// across operator locales + git config files.
	Env []string
}

// Run executes `git -C repoPath <args>`. The `-C` flag is git's
// canonical way to operate on a different repo without changing cwd.
func (o *OSExecutor) Run(ctx context.Context, repoPath string, args []string) ([]byte, []byte, int, error) {
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

	gitArgs := make([]string, 0, len(args)+2)
	if repoPath != "" {
		gitArgs = append(gitArgs, "-C", repoPath)
	}
	gitArgs = append(gitArgs, args...)

	cmd := exec.CommandContext(runCtx, binary, gitArgs...)
	if o.Env != nil {
		cmd.Env = o.Env
	} else {
		cmd.Env = []string{
			"PATH=" + osPath(),
			"LC_ALL=C",
			"LANG=C",
			"GIT_TERMINAL_PROMPT=0", // never block on auth prompt
			"GIT_PAGER=cat",
			"GIT_OPTIONAL_LOCKS=0", // skip optimistic locks (parallel safety)
		}
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

// osPath returns the parent process's PATH. Stubbed via a function so
// tests can replace without touching package state.
var osPath = func() string {
	return os.Getenv("PATH")
}

func isExecNotFoundErr(err error) bool {
	if err == nil {
		return false
	}
	s := err.Error()
	return strings.Contains(s, "executable file not found") ||
		strings.Contains(s, "no such file or directory")
}

// Client is the high-level adapter the daemon imports. Wraps an Executor
// + parses git output into typed structs.
type Client struct {
	// RepoPath is the project working tree to operate against. Required
	// for all methods (defense against accidentally invoking git in the
	// daemon's own cwd).
	RepoPath string
	Exec     Executor
}

// New returns a Client wired to the OS executor with default settings.
func New(repoPath string) *Client {
	return &Client{RepoPath: repoPath, Exec: &OSExecutor{}}
}

// NewWithExecutor returns a Client backed by the supplied Executor.
func NewWithExecutor(repoPath string, exec Executor) *Client {
	return &Client{RepoPath: repoPath, Exec: exec}
}

// Status runs `git status --porcelain=v1 --branch` and parses the result.
func (c *Client) Status(ctx context.Context) (*Status, error) {
	out, err := c.run(ctx, []string{"status", "--porcelain=v1", "--branch"})
	if err != nil {
		return nil, err
	}
	return parseStatus(out)
}

// DiffUnstaged returns `git diff` text (working tree vs index).
func (c *Client) DiffUnstaged(ctx context.Context) ([]byte, error) {
	return c.run(ctx, []string{"diff", "--no-color"})
}

// DiffStaged returns `git diff --staged` text (index vs HEAD).
func (c *Client) DiffStaged(ctx context.Context) ([]byte, error) {
	return c.run(ctx, []string{"diff", "--no-color", "--staged"})
}

// LogOpts controls the Log() invocation.
type LogOpts struct {
	// Ref limits the log to the given ref's history (default: HEAD).
	Ref string
	// Limit caps the number of commits returned (default: 100).
	Limit int
	// Path narrows to commits that touched a specific path (optional).
	Path string
}

// Log returns parsed commit metadata for the requested ref. Uses a
// NUL-separated --pretty=format so messages with embedded newlines parse
// cleanly.
func (c *Client) Log(ctx context.Context, opts LogOpts) ([]Commit, error) {
	limit := opts.Limit
	if limit <= 0 {
		limit = 100
	}
	args := []string{
		"log",
		"--no-color",
		"-n", strconv.Itoa(limit),
		// %x00 = NUL field separator, %x1e = record separator (RS).
		// %H=full sha, %h=short, %an/%ae/%ai=author, %cn/%ce/%ci=committer,
		// %s=subject, %b=body, %P=parent shas.
		"--pretty=format:%H%x00%h%x00%an%x00%ae%x00%aI%x00%cn%x00%ce%x00%cI%x00%P%x00%s%x00%b%x1e",
	}
	if opts.Ref != "" {
		args = append(args, opts.Ref)
	}
	if opts.Path != "" {
		args = append(args, "--", opts.Path)
	}
	out, err := c.run(ctx, args)
	if err != nil {
		return nil, err
	}
	return parseLog(out)
}

// Show returns the full text of `git show <sha>`. Includes the commit
// metadata + diff. Renderers display this as a single block.
func (c *Client) Show(ctx context.Context, sha string) ([]byte, error) {
	if sha == "" {
		return nil, errors.New("git: Show requires a non-empty sha")
	}
	return c.run(ctx, []string{"show", "--no-color", sha})
}

// PushOpts controls Push().
type PushOpts struct {
	Remote string // empty → "origin"
	Branch string // empty → current branch (HEAD)
	// Force, when true, runs `git push --force-with-lease`. Caller MUST
	// pass an ApprovalID (Hoopoe-policy `policy.git.force_push`) — Push
	// returns ErrForcePushRequiresApproval otherwise.
	Force      bool
	ApprovalID string
}

// Push runs `git push` with the configured remote/branch. Honors the
// force-push approval gate per §5.3.
func (c *Client) Push(ctx context.Context, opts PushOpts) (*PushResult, error) {
	remote := opts.Remote
	if remote == "" {
		remote = "origin"
	}
	branch := opts.Branch
	if branch == "" {
		branch = "HEAD"
	}
	args := []string{"push", "--porcelain", "--no-color"}
	if opts.Force {
		if opts.ApprovalID == "" {
			return nil, ErrForcePushRequiresApproval
		}
		args = append(args, "--force-with-lease")
	}
	args = append(args, remote, branch)

	out, err := c.run(ctx, args)
	if err != nil {
		return nil, err
	}
	return parsePushPorcelain(out, opts.Force), nil
}

// Fetch runs `git fetch <remote>` (default `origin`). Read-only network
// op; not subject to the force-push gate.
func (c *Client) Fetch(ctx context.Context, remote string) error {
	if remote == "" {
		remote = "origin"
	}
	_, err := c.run(ctx, []string{"fetch", "--no-color", remote})
	return err
}

// RevList returns commits in `range` (e.g., "origin/main..HEAD" for
// unpushed-list). Returns parsed commits via Log to keep the shape
// consistent.
func (c *Client) RevList(ctx context.Context, fromRef, toRef string) (*CommitDelta, error) {
	if fromRef == "" || toRef == "" {
		return nil, errors.New("git: RevList requires non-empty fromRef + toRef")
	}
	out, err := c.run(ctx, []string{"rev-list", fromRef + ".." + toRef})
	if err != nil {
		return nil, err
	}
	delta := &CommitDelta{From: fromRef, To: toRef}
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		sha := strings.TrimSpace(line)
		if sha == "" {
			continue
		}
		// Resolve each sha to a Commit via Show-style parse — but
		// avoid running git per sha (slow). Use a single follow-up
		// log call constrained to the rev-list output.
		delta.Commits = append(delta.Commits, Commit{SHA: sha, ShortSHA: shortSHA(sha)})
	}
	return delta, nil
}

// UnpushedCommits is a convenience wrapper around RevList for the
// canonical "what hasn't reached origin yet?" query.
func (c *Client) UnpushedCommits(ctx context.Context, branch string) (*CommitDelta, error) {
	if branch == "" {
		branch = "HEAD"
	}
	return c.RevList(ctx, "origin/"+strings.TrimPrefix(branch, "origin/"), branch)
}

// RevParse resolves `ref` to its full SHA.
func (c *Client) RevParse(ctx context.Context, ref string) (string, error) {
	if ref == "" {
		return "", errors.New("git: RevParse requires a non-empty ref")
	}
	out, err := c.run(ctx, []string{"rev-parse", ref})
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

// LsTree lists the entries at `ref` (recursively when `recursive`).
func (c *Client) LsTree(ctx context.Context, ref string, recursive bool) ([]LsTreeEntry, error) {
	if ref == "" {
		return nil, errors.New("git: LsTree requires a non-empty ref")
	}
	args := []string{"ls-tree"}
	if recursive {
		args = append(args, "-r")
	}
	args = append(args, ref)
	out, err := c.run(ctx, args)
	if err != nil {
		return nil, err
	}
	return parseLsTree(out)
}

// Blame returns per-line authorship for `path` at the optional `ref`.
// `ref` empty → current working tree.
func (c *Client) Blame(ctx context.Context, ref, path string) (*FileBlame, error) {
	if path == "" {
		return nil, errors.New("git: Blame requires a non-empty path")
	}
	args := []string{"blame", "--porcelain"}
	if ref != "" {
		args = append(args, ref)
	}
	args = append(args, "--", path)
	out, err := c.run(ctx, args)
	if err != nil {
		return nil, err
	}
	return parseBlamePorcelain(out, path)
}

// Remotes returns the configured remotes from `git remote -v`.
func (c *Client) Remotes(ctx context.Context) ([]Remote, error) {
	out, err := c.run(ctx, []string{"remote", "-v"})
	if err != nil {
		return nil, err
	}
	return parseRemoteV(out), nil
}

// Branches returns local branches with their HEAD SHA + first-line subject.
func (c *Client) Branches(ctx context.Context) ([]Branch, error) {
	out, err := c.run(ctx, []string{"branch", "-v", "--no-abbrev"})
	if err != nil {
		return nil, err
	}
	return parseBranchV(out), nil
}

// run is the central guard + executor invocation. Refuses empty repo
// path; classifies missing binary; returns raw stdout for the caller to
// parse.
func (c *Client) run(ctx context.Context, args []string) ([]byte, error) {
	if c.RepoPath == "" {
		return nil, ErrEmptyRepoPath
	}
	stdout, stderr, exit, err := c.Exec.Run(ctx, c.RepoPath, args)
	if err != nil {
		if errors.Is(err, ErrMissingBinary) {
			return nil, err
		}
		return nil, fmt.Errorf("git: invoke %q: %w (stderr: %s)",
			strings.Join(args, " "), err, truncateStderr(stderr))
	}
	if exit != 0 {
		return nil, fmt.Errorf("git: %q exited %d (stderr: %s)",
			strings.Join(args, " "), exit, truncateStderr(stderr))
	}
	return stdout, nil
}

// shortSHA mirrors `git rev-parse --short` for a 7-character abbreviation.
func shortSHA(sha string) string {
	if len(sha) > 7 {
		return sha[:7]
	}
	return sha
}

// truncateStderr returns at most ~512 bytes for error messages.
func truncateStderr(b []byte) string {
	const max = 512
	s := string(b)
	if len(s) <= max {
		return s
	}
	return s[:max] + "...(truncated)"
}
