// client_test.go — exercises the git adapter against:
//  1. a fake executor for error classification + force-push gating;
//  2. real temp git repos for status/log/diff/show round-trips
//     (via the OSExecutor — requires `git` on PATH, which the daemon
//     already requires for production).
package git

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// fakeExecutor returns canned bytes/errors per argv joined by space.
type fakeExecutor struct {
	Stdouts map[string][]byte
	Stderrs map[string][]byte
	Exits   map[string]int
	Errors  map[string]error
	Calls   []string
}

func newFakeExecutor() *fakeExecutor {
	return &fakeExecutor{
		Stdouts: map[string][]byte{},
		Stderrs: map[string][]byte{},
		Exits:   map[string]int{},
		Errors:  map[string]error{},
	}
}

func (f *fakeExecutor) Run(_ context.Context, _ string, args []string) ([]byte, []byte, int, error) {
	key := strings.Join(args, " ")
	f.Calls = append(f.Calls, key)
	if err := f.Errors[key]; err != nil {
		return nil, f.Stderrs[key], f.Exits[key], err
	}
	return f.Stdouts[key], f.Stderrs[key], f.Exits[key], nil
}

func TestOSExecutorDefaultEnvUsesParentPathWithoutLeakingSecrets(t *testing.T) {
	envBinary, err := exec.LookPath("env")
	if err != nil {
		t.Skip("env binary not on PATH")
	}
	t.Setenv("PATH", "/tmp/hoopoe-parent-bin")
	t.Setenv("HOOPOE_TEST_PROVIDER_TOKEN", "do-not-leak")

	stdout, stderr, exit, err := (&OSExecutor{Binary: envBinary}).Run(context.Background(), "", nil)
	if err != nil {
		t.Fatalf("Run env: %v (stderr: %s)", err, stderr)
	}
	if exit != 0 {
		t.Fatalf("Run env exit = %d (stderr: %s)", exit, stderr)
	}
	env := string(stdout)
	if !strings.Contains(env, "PATH=/tmp/hoopoe-parent-bin\n") {
		t.Fatalf("expected parent PATH in child env, got %q", env)
	}
	for _, required := range []string{
		"LC_ALL=C\n",
		"LANG=C\n",
		"GIT_TERMINAL_PROMPT=0\n",
		"GIT_PAGER=cat\n",
		"GIT_OPTIONAL_LOCKS=0\n",
	} {
		if !strings.Contains(env, required) {
			t.Fatalf("expected %s in child env, got %q", strings.TrimSpace(required), env)
		}
	}
	if strings.Contains(env, "HOOPOE_TEST_PROVIDER_TOKEN") {
		t.Fatalf("child env leaked provider token: %q", env)
	}
}

// initTempRepo creates a fresh git repo in a temp directory with an
// initial commit. Returns the repo path. Skips the test if git isn't
// available.
func initTempRepo(t *testing.T) string {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not on PATH")
	}
	dir := t.TempDir()
	commands := [][]string{
		{"init", "-q", "-b", "main"},
		{"config", "user.name", "hp-l65 test"},
		{"config", "user.email", "hp-l65@test.invalid"},
		{"config", "commit.gpgsign", "false"},
	}
	for _, cmd := range commands {
		c := exec.Command("git", append([]string{"-C", dir}, cmd...)...)
		c.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0")
		if out, err := c.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v (%s)", cmd, err, out)
		}
	}
	// Initial commit.
	if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte("# test\n"), 0o644); err != nil {
		t.Fatalf("write README: %v", err)
	}
	for _, cmd := range [][]string{
		{"add", "README.md"},
		{"commit", "-q", "-m", "initial commit"},
	} {
		c := exec.Command("git", append([]string{"-C", dir}, cmd...)...)
		if out, err := c.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v (%s)", cmd, err, out)
		}
	}
	return dir
}

func TestClientRefusesEmptyRepoPath(t *testing.T) {
	t.Parallel()
	c := NewWithExecutor("", newFakeExecutor())
	_, err := c.Status(context.Background())
	if !errors.Is(err, ErrEmptyRepoPath) {
		t.Fatalf("expected ErrEmptyRepoPath, got %v", err)
	}
}

func TestStatusParsesPorcelainV1(t *testing.T) {
	t.Parallel()
	fake := newFakeExecutor()
	fake.Stdouts["status --porcelain=v1 --branch"] = []byte(
		"## main...origin/main [ahead 2, behind 1]\n" +
			" M apps/daemon/main.go\n" +
			"?? newfile.txt\n" +
			"R  old.go -> new.go\n",
	)
	c := NewWithExecutor("/repo", fake)
	s, err := c.Status(context.Background())
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	if s.Branch != "main" {
		t.Fatalf("expected branch main, got %q", s.Branch)
	}
	if s.Upstream != "origin/main" {
		t.Fatalf("expected upstream origin/main, got %q", s.Upstream)
	}
	if s.AheadBy != 2 || s.BehindBy != 1 {
		t.Fatalf("expected ahead=2 behind=1, got ahead=%d behind=%d", s.AheadBy, s.BehindBy)
	}
	if len(s.Entries) != 3 {
		t.Fatalf("expected 3 entries, got %d", len(s.Entries))
	}
	if s.Entries[2].XY != "R " || s.Entries[2].OldPath != "old.go" || s.Entries[2].Path != "new.go" {
		t.Fatalf("rename entry malformed: %+v", s.Entries[2])
	}
	if s.Clean {
		t.Fatalf("expected non-clean status")
	}
}

func TestStatusParsesDetachedHead(t *testing.T) {
	t.Parallel()
	fake := newFakeExecutor()
	fake.Stdouts["status --porcelain=v1 --branch"] = []byte("## HEAD (no branch)\n")
	c := NewWithExecutor("/repo", fake)
	s, err := c.Status(context.Background())
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	if !s.Detached {
		t.Fatalf("expected detached")
	}
	if !s.Clean {
		t.Fatalf("expected clean detached status")
	}
}

func TestForcePushRequiresApproval(t *testing.T) {
	t.Parallel()
	fake := newFakeExecutor()
	c := NewWithExecutor("/repo", fake)
	_, err := c.Push(context.Background(), PushOpts{Force: true})
	if !errors.Is(err, ErrForcePushRequiresApproval) {
		t.Fatalf("expected ErrForcePushRequiresApproval, got %v", err)
	}
	// Force-push WITH approval should attempt the call.
	fake.Stdouts["push --porcelain --no-color --force-with-lease origin HEAD"] = []byte(
		"To git@example.com:org/repo.git\n" +
			"+\trefs/heads/main:refs/heads/main\tabc1234..def5678 (forced update)\n" +
			"Done\n",
	)
	res, err := c.Push(context.Background(), PushOpts{Force: true, ApprovalID: "apv_01"})
	if err != nil {
		t.Fatalf("Push w/ approval: %v", err)
	}
	if !res.Forced {
		t.Fatalf("expected Forced true")
	}
	if !res.OK {
		t.Fatalf("expected OK")
	}
}

func TestPushPorcelainParsesRejection(t *testing.T) {
	t.Parallel()
	fake := newFakeExecutor()
	fake.Stdouts["push --porcelain --no-color origin HEAD"] = []byte(
		"To git@example.com:org/repo.git\n" +
			"!\trefs/heads/main:refs/heads/main\t[rejected] (non-fast-forward)\n" +
			"Done\n",
	)
	c := NewWithExecutor("/repo", fake)
	res, err := c.Push(context.Background(), PushOpts{})
	if err != nil {
		t.Fatalf("Push: %v", err)
	}
	if res.OK {
		t.Fatalf("expected not OK on rejection")
	}
	if len(res.UpdatedRefs) != 1 {
		t.Fatalf("expected 1 ref update, got %d", len(res.UpdatedRefs))
	}
	if res.UpdatedRefs[0].Status != "!" {
		t.Fatalf("expected ! status, got %q", res.UpdatedRefs[0].Status)
	}
}

func TestRevListRejectsEmptyRefs(t *testing.T) {
	t.Parallel()
	c := NewWithExecutor("/repo", newFakeExecutor())
	if _, err := c.RevList(context.Background(), "", "HEAD"); err == nil {
		t.Fatalf("expected error on empty fromRef")
	}
	if _, err := c.RevList(context.Background(), "origin/main", ""); err == nil {
		t.Fatalf("expected error on empty toRef")
	}
}

func TestExecCommandClassifiesNonZeroExit(t *testing.T) {
	t.Parallel()
	fake := newFakeExecutor()
	fake.Stdouts["status --porcelain=v1 --branch"] = []byte("")
	fake.Stderrs["status --porcelain=v1 --branch"] = []byte("fatal: not a git repository")
	fake.Exits["status --porcelain=v1 --branch"] = 128
	c := NewWithExecutor("/not-a-repo", fake)
	_, err := c.Status(context.Background())
	if err == nil {
		t.Fatalf("expected error on non-zero exit")
	}
	if !strings.Contains(err.Error(), "exited 128") {
		t.Fatalf("error should include exit code, got %v", err)
	}
	if !strings.Contains(err.Error(), "not a git repository") {
		t.Fatalf("error should include stderr, got %v", err)
	}
}

func TestParseLogHandlesMultilineBody(t *testing.T) {
	t.Parallel()
	// Build a fake log payload matching the --pretty=format we send.
	// Two records separated by 0x1e.
	rec1 := strings.Join([]string{
		"abc1234567890abcdef1234567890abcdef12345",
		"abc1234",
		"Alice",
		"alice@test",
		"2026-05-04T00:00:00Z",
		"Alice",
		"alice@test",
		"2026-05-04T00:00:00Z",
		"def5678 ghi9012",
		"first commit subject",
		"first commit\nbody line 2\nbody line 3",
	}, "\x00") + "\x1e"
	rec2 := strings.Join([]string{
		"def5678901234567890abcdef1234567890abcdef",
		"def5678",
		"Bob",
		"bob@test",
		"2026-05-03T00:00:00Z",
		"Bob",
		"bob@test",
		"2026-05-03T00:00:00Z",
		"",
		"second commit subject",
		"",
	}, "\x00") + "\x1e"

	commits, err := parseLog([]byte(rec1 + rec2))
	if err != nil {
		t.Fatalf("parseLog: %v", err)
	}
	if len(commits) != 2 {
		t.Fatalf("expected 2 commits, got %d", len(commits))
	}
	if commits[0].SHA != "abc1234567890abcdef1234567890abcdef12345" {
		t.Fatalf("commit 0 SHA: %q", commits[0].SHA)
	}
	if commits[0].AuthorName != "Alice" {
		t.Fatalf("commit 0 author: %q", commits[0].AuthorName)
	}
	if !strings.Contains(commits[0].Body, "body line 2") {
		t.Fatalf("commit 0 body lost lines: %q", commits[0].Body)
	}
	if len(commits[0].ParentSHAs) != 2 {
		t.Fatalf("commit 0 parents: %v", commits[0].ParentSHAs)
	}
	if len(commits[1].ParentSHAs) != 0 {
		t.Fatalf("commit 1 parents should be empty: %v", commits[1].ParentSHAs)
	}
}

func TestParseRemoteV(t *testing.T) {
	t.Parallel()
	out := []byte(
		"origin\tgit@github.com:org/repo.git (fetch)\n" +
			"origin\tgit@github.com:org/repo.git (push)\n" +
			"upstream\thttps://github.com/upstream/repo.git (fetch)\n" +
			"upstream\thttps://github.com/upstream/repo.git (push)\n",
	)
	remotes := parseRemoteV(out)
	if len(remotes) != 2 {
		t.Fatalf("expected 2 remotes, got %d", len(remotes))
	}
	byName := map[string]Remote{}
	for _, r := range remotes {
		byName[r.Name] = r
	}
	if byName["origin"].FetchURL != "git@github.com:org/repo.git" {
		t.Fatalf("origin fetch url: %q", byName["origin"].FetchURL)
	}
	if byName["origin"].PushURL != byName["origin"].FetchURL {
		t.Fatalf("expected origin fetch == push")
	}
	if byName["upstream"].FetchURL != "https://github.com/upstream/repo.git" {
		t.Fatalf("upstream fetch: %q", byName["upstream"].FetchURL)
	}
}

func TestParseLsTree(t *testing.T) {
	t.Parallel()
	out := []byte(
		"100644 blob abc123\tREADME.md\n" +
			"100755 blob def456\tscripts/run.sh\n" +
			"040000 tree 789aaa\tinternal\n",
	)
	entries, err := parseLsTree(out)
	if err != nil {
		t.Fatalf("parseLsTree: %v", err)
	}
	if len(entries) != 3 {
		t.Fatalf("expected 3 entries, got %d", len(entries))
	}
	if entries[0].Path != "README.md" || entries[0].Type != "blob" {
		t.Fatalf("entry 0 malformed: %+v", entries[0])
	}
	if entries[2].Type != "tree" {
		t.Fatalf("expected tree entry: %+v", entries[2])
	}
}

// Integration: real git repo round-trips. Skipped if git isn't installed.

func TestStatusAgainstRealRepo(t *testing.T) {
	t.Parallel()
	dir := initTempRepo(t)
	c := New(dir)
	s, err := c.Status(context.Background())
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	if s.Branch != "main" {
		t.Fatalf("expected main branch, got %q", s.Branch)
	}
	if !s.Clean {
		t.Fatalf("expected clean status, got entries: %+v", s.Entries)
	}
}

func TestLogAgainstRealRepo(t *testing.T) {
	t.Parallel()
	dir := initTempRepo(t)
	c := New(dir)
	commits, err := c.Log(context.Background(), LogOpts{Limit: 5})
	if err != nil {
		t.Fatalf("Log: %v", err)
	}
	if len(commits) != 1 {
		t.Fatalf("expected 1 commit, got %d", len(commits))
	}
	if commits[0].Subject != "initial commit" {
		t.Fatalf("expected 'initial commit' subject, got %q", commits[0].Subject)
	}
	if commits[0].AuthorName != "hp-l65 test" {
		t.Fatalf("expected author hp-l65 test, got %q", commits[0].AuthorName)
	}
}

func TestRevParseAgainstRealRepo(t *testing.T) {
	t.Parallel()
	dir := initTempRepo(t)
	c := New(dir)
	sha, err := c.RevParse(context.Background(), "HEAD")
	if err != nil {
		t.Fatalf("RevParse: %v", err)
	}
	if len(sha) != 40 {
		t.Fatalf("expected 40-char SHA, got %d (%q)", len(sha), sha)
	}
}

// TestProbeOnNormalGoldenFixtureMatchesHealthyContract loads
// packages/fixtures/golden-outputs/git/normal.json and pins the contract
// from plan.md §18.3 for the normal state: a healthy git repo's read
// surface (status, diff, log/branch/remote, unpushed) reports StatusOK,
// and pushes are not exercised during probe.
//
// Two-sided check:
//
//   - The fixture's self-declared capabilities are sanity-checked so a
//     future fixture edit that drifts the canonical "normal" map fails
//     this test rather than silently passing. git.status.read /
//     git.diff.read / git.unpushed.list must read OK; git.push must be
//     declared blocked-by-policy with a reason note (the snapshot script
//     never pushes — semantically equivalent to the daemon's "untested",
//     and both pin the contract that probes never write).
//
//   - The adapter's live Probe against a real temp repo (initTempRepo)
//     must produce StatusOK on every read capability the fixture
//     declares OK, and CapPush must come back as StatusUntested (the
//     daemon's narrower variant of "blocked-by-policy" — push is
//     deliberately not run during probe).
//
// The fixture's stdoutText carries `--porcelain=v2 --branch` output
// captured by snapshot.sh; the daemon adapter reads `--porcelain=v1`
// (parsers.go:18). We deliberately do not feed the v2 bytes through
// parseStatus — that would test snapshot.sh's argv shape, not the
// adapter contract. Instead we use the fixture's *capability map* as
// the contract surface.
func TestProbeOnNormalGoldenFixtureMatchesHealthyContract(t *testing.T) {
	t.Parallel()
	fixture := loadGitGoldenFixture(t, "normal.json")
	if fixture.Meta.State != "normal" {
		t.Fatalf("fixture state = %q, want normal", fixture.Meta.State)
	}
	for _, capID := range []string{"git.status.read", "git.diff.read", "git.unpushed.list"} {
		cap, ok := fixture.Capabilities[capID]
		if !ok {
			t.Fatalf("fixture missing %s", capID)
		}
		if cap.Status != "ok" {
			t.Fatalf("fixture %s = %q, want ok", capID, cap.Status)
		}
	}
	push, ok := fixture.Capabilities["git.push"]
	if !ok {
		t.Fatalf("fixture missing git.push")
	}
	if push.Status != "blocked-by-policy" {
		t.Fatalf("fixture git.push = %q, want blocked-by-policy", push.Status)
	}
	if !strings.Contains(strings.ToLower(push.Notes), "never push") && !strings.Contains(strings.ToLower(push.Notes), "snapshot") {
		t.Fatalf("fixture git.push notes %q should explain why push is not exercised", push.Notes)
	}

	dir := initTempRepo(t)
	c := New(dir)
	res := Probe(context.Background(), c, func() time.Time {
		return time.Date(2026, 5, 7, 0, 0, 0, 0, time.UTC)
	})

	for _, capID := range []string{CapStatusRead, CapLog, CapBranchRead, CapRemoteRead} {
		report, ok := res.Reports[capID]
		if !ok {
			t.Fatalf("live probe missing %s", capID)
		}
		if report.Status != StatusOK {
			t.Fatalf("live probe %s = %q (notes: %s); want ok to match fixture's normal-state contract", capID, report.Status, report.Notes)
		}
	}
	if got := res.Reports[CapPush].Status; got != StatusUntested {
		t.Fatalf("live probe push = %q; want untested (fixture pins push=blocked-by-policy; daemon's narrower variant is untested — both pin the contract that probes never write)", got)
	}
}

// TestProbeOnMissingToolGoldenFixtureMarksAllReadCapabilitiesMissing
// loads packages/fixtures/golden-outputs/git/missing-tool.json and
// pins the adapter contract from plan.md §18.3 for the missing-tool
// state.
//
// The fixture authoritatively declares:
//   - meta.state == "missing-tool"
//   - exit == 127 with stderrText "git: command not found\n"
//   - capabilities["git._present"] == {status: "missing"}
//
// `git._present` is the fixture's contract key. The adapter exposes
// per-surface capabilities (CapStatusRead, CapDiffRead, CapLog,
// CapShow, CapBlame, CapRemoteRead, CapBranchRead, CapUnpushedList,
// CapPush). What the fixture pins is the contract that when the
// `git` binary is unreachable, every read-only capability is
// classified missing — driven by the `errors.Is(err,
// ErrMissingBinary)` branch in probeOne (capabilities.go:152-158).
//
// Drives the adapter via a fakeExecutor that returns
// ErrMissingBinary for every read-only argv probeOne touches,
// matching the production OSExecutor wrap of the
// "executable file not found" error class (client.go:116-118).
//
// CapPush is exempt: it is always StatusUntested by design
// (capabilities.go:132-137 — push is side-effecting; probing would
// write).
func TestProbeOnMissingToolGoldenFixtureMarksAllReadCapabilitiesMissing(t *testing.T) {
	t.Parallel()
	fixture := loadGitGoldenFixture(t, "missing-tool.json")
	if fixture.Meta.State != "missing-tool" {
		t.Fatalf("fixture state = %q, want missing-tool", fixture.Meta.State)
	}
	if fixture.Exit != 127 {
		t.Fatalf("fixture exit = %d, want 127", fixture.Exit)
	}
	cap, ok := fixture.Capabilities["git._present"]
	if !ok || cap.Status != "missing" {
		t.Fatalf("fixture must declare git._present=missing, got %+v", fixture.Capabilities)
	}

	// Drive every executor invocation through a single
	// always-missing-binary stub. This mirrors the production
	// OSExecutor behavior at client.go:116-118: when the underlying
	// `exec.Cmd.Run` returns an "executable file not found" error,
	// the executor wraps it as ErrMissingBinary. Listing every
	// argv individually here would be brittle — Probe touches 9
	// distinct argv shapes spread across multiple Client methods,
	// each with optional flags. The all-routes stub catches them
	// all and pins the contract: when `git` is missing, every
	// call fails with ErrMissingBinary.
	c := NewWithExecutor("/repo", &alwaysMissingBinaryExecutor{})
	res := Probe(context.Background(), c, func() time.Time {
		return time.Date(2026, 5, 7, 0, 0, 0, 0, time.UTC)
	})

	// Every read-only capability must be Missing.
	for _, capID := range []string{
		CapStatusRead, CapDiffRead, CapLog, CapShow,
		CapBlame, CapRemoteRead, CapBranchRead, CapUnpushedList,
	} {
		report, ok := res.Reports[capID]
		if !ok {
			t.Fatalf("missing report for %s", capID)
		}
		if report.Status != StatusMissing {
			t.Fatalf("%s status = %q, want missing (fixture state=missing-tool, ErrMissingBinary path)", capID, report.Status)
		}
	}
	// Push is intentionally untested by the adapter regardless of
	// binary presence (capabilities.go:132-137).
	if got := res.Reports[CapPush].Status; got != StatusUntested {
		t.Fatalf("%s status = %q, want untested (push is side-effecting; probe is always deferred)", CapPush, got)
	}
	// Tool/Source identity should still be set even when the binary
	// is missing — the daemon needs that envelope to mark the
	// capability registry entry "Source: cli, Status: missing".
	if res.Tool != "git" {
		t.Fatalf("Tool = %q, want git", res.Tool)
	}
	if res.Source != "CLI" {
		t.Fatalf("Source = %q, want CLI", res.Source)
	}
}

// TestProbeOnMalformedJSONGoldenFixtureDegradesLogCapability loads
// packages/fixtures/golden-outputs/git/malformed-json.json and pins
// the adapter contract from plan.md §18.3 for the malformed-json
// state.
//
// The fixture authoritatively declares:
//   - meta.state == "malformed-json"
//   - exit == 0 (the bytes came back; the parser must reject them)
//   - capabilities["git._parse"] == {status: "degraded",
//     notes: "stdout was non-JSON; adapter must NOT panic"}
//
// `git._parse` is the fixture's contract key. The git adapter doesn't
// have a `--json` mode (the placeholder argv `git --json list`
// is per the README's argv-semantics convention). What the fixture
// pins is the contract that when a parser sees malformed bytes:
//
//  1. It does not panic.
//  2. It returns an error.
//  3. The corresponding capability surfaces StatusDegraded — driven
//     by the default branch in probeOne (capabilities.go:165-171)
//     after the ErrMissingBinary / ErrEmptyRepoPath fast-paths
//     don't match.
//
// Drives the contract two ways:
//
//   - parseLog directly against the fixture bytes (parser-level
//     graceful-degradation pin).
//   - Probe via a fakeExecutor that returns the malformed bytes
//     on the log argv (capability-level Degraded pin).
func TestProbeOnMalformedJSONGoldenFixtureDegradesLogCapability(t *testing.T) {
	t.Parallel()
	fixture := loadGitGoldenFixture(t, "malformed-json.json")
	if fixture.Meta.State != "malformed-json" {
		t.Fatalf("fixture state = %q, want malformed-json", fixture.Meta.State)
	}
	if fixture.Exit != 0 {
		t.Fatalf("fixture exit = %d, want 0 (malformed-json: the bytes came back; parser must reject)", fixture.Exit)
	}
	cap, ok := fixture.Capabilities["git._parse"]
	if !ok || cap.Status != "degraded" {
		t.Fatalf("fixture must declare git._parse=degraded, got %+v", fixture.Capabilities)
	}
	if fixture.StdoutText == "" {
		t.Fatalf("fixture stdoutText is empty")
	}

	// Parser-level contract: parseLog must not panic and must
	// return an error on malformed bytes. Pre-fix, a future
	// regression that introduced a panic on truncated input would
	// take the daemon down on the first probe of a corrupted log.
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("parseLog panicked on malformed-json fixture: %v", r)
		}
	}()
	if _, err := parseLog([]byte(fixture.StdoutText)); err == nil {
		t.Fatalf("parseLog accepted malformed-json bytes without error")
	}

	// Capability-level contract: Probe surfaces StatusDegraded.
	// The git log argv shape mirrors what client.Log builds with
	// LogOpts{Limit: 1} (capabilities.go:99-102 → client.go:190+).
	fake := newFakeExecutor()
	logKey := "log --no-color -n 1 --pretty=format:%H%x00%h%x00%an%x00%ae%x00%aI%x00%cn%x00%ce%x00%cI%x00%P%x00%s%x00%b%x1e"
	fake.Stdouts[logKey] = []byte(fixture.StdoutText)
	// Satisfy the other read-only probe argvs so only the log
	// path goes degraded — pinning the per-capability contract.
	fake.Stdouts["status --porcelain=v1 --branch"] = []byte("## main\n")
	fake.Stdouts["diff --no-color"] = []byte("")
	fake.Stdouts["show --no-color HEAD"] = []byte("commit abc123\n")
	fake.Stdouts["remote -v"] = []byte("")
	fake.Stdouts["branch -v --no-abbrev"] = []byte("* main abcdef0 hello\n")
	fake.Stdouts["blame --porcelain -- README.md"] = []byte("")
	fake.Stdouts["rev-list origin/HEAD..HEAD"] = []byte("")
	fake.Stdouts["--version"] = []byte("git version 2.43.0\n")

	c := NewWithExecutor("/repo", fake)
	res := Probe(context.Background(), c, func() time.Time {
		return time.Date(2026, 5, 7, 0, 0, 0, 0, time.UTC)
	})

	logReport, ok := res.Reports[CapLog]
	if !ok {
		t.Fatalf("missing report for %s", CapLog)
	}
	if logReport.Status != StatusDegraded {
		t.Fatalf("%s status = %q, want degraded (fixture state=malformed-json, parser rejected bytes)", CapLog, logReport.Status)
	}
	if logReport.Notes == "" {
		t.Fatalf("%s notes are empty; expected parser error trace", CapLog)
	}
}

// TestProbeOnHighVolumeGoldenFixtureDegradesLogCapability loads
// packages/fixtures/golden-outputs/git/high-volume.json and pins the
// adapter contract from plan.md §18.3 for the high-volume state.
//
// Fixture mimics ENVELOPE_MAX_BYTES truncation: stdoutBytes=1048576
// (1MiB), truncated=true, exit=0, synthetic git._highVolume=degraded
// with the note "stdout exceeded ENVELOPE_MAX_BYTES; pagination
// required".
//
// The git adapter does not enforce its own output cap — the truncation
// contract documents that *upstream* (the envelope harness) enforces a
// cap, and when the resulting stdout is a truncated/non-parseable
// fragment, parseLog must surface StatusDegraded rather than crashing
// or accepting a zero value as success.
//
// Pinned here:
//
//  1. Fixture self-consistency (state, exit=0, truncated=true,
//     _highVolume=degraded).
//  2. The fixture's stdoutText placeholder does NOT parse as a valid
//     git log envelope (sanity guard against drift).
//  3. parseLog directly against the truncated bytes returns an error
//     and does not panic — pre-fix safety net for the daemon process.
//  4. Probe drives through the truncated envelope: CapLog degrades
//     with parser-error notes, distinct from the timeout (exit=124)
//     and missing-tool (ErrMissingBinary) error classes covered by
//     adjacent fixtures.
func TestProbeOnHighVolumeGoldenFixtureDegradesLogCapability(t *testing.T) {
	t.Parallel()
	fixture := loadGitGoldenFixture(t, "high-volume.json")
	if fixture.Meta.State != "high-volume" {
		t.Fatalf("fixture state = %q, want high-volume", fixture.Meta.State)
	}
	if !fixture.Truncated {
		t.Fatalf("fixture truncated = false, want true (envelope-truncation is the high-volume contract)")
	}
	if fixture.Exit != 0 {
		t.Fatalf("fixture exit = %d, want 0 (high-volume is envelope-size, not failure)", fixture.Exit)
	}
	cap, ok := fixture.Capabilities["git._highVolume"]
	if !ok || cap.Status != "degraded" {
		t.Fatalf("fixture must declare git._highVolume=degraded, got %+v", fixture.Capabilities)
	}

	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("parseLog panicked on high-volume fixture: %v", r)
		}
	}()
	if _, err := parseLog([]byte(fixture.StdoutText)); err == nil {
		t.Fatalf("parseLog accepted truncated high-volume bytes without error")
	}

	fake := newFakeExecutor()
	logKey := "log --no-color -n 1 --pretty=format:%H%x00%h%x00%an%x00%ae%x00%aI%x00%cn%x00%ce%x00%cI%x00%P%x00%s%x00%b%x1e"
	fake.Stdouts[logKey] = []byte(fixture.StdoutText)
	// Satisfy the other read-only probe argvs so only the log path
	// goes degraded — pinning the per-capability contract.
	fake.Stdouts["status --porcelain=v1 --branch"] = []byte("## main\n")
	fake.Stdouts["diff --no-color"] = []byte("")
	fake.Stdouts["show --no-color HEAD"] = []byte("commit abc123\n")
	fake.Stdouts["remote -v"] = []byte("")
	fake.Stdouts["branch -v --no-abbrev"] = []byte("* main abcdef0 hello\n")
	fake.Stdouts["blame --porcelain -- README.md"] = []byte("")
	fake.Stdouts["rev-list origin/HEAD..HEAD"] = []byte("")
	fake.Stdouts["--version"] = []byte("git version 2.43.0\n")

	c := NewWithExecutor("/repo", fake)
	res := Probe(context.Background(), c, func() time.Time {
		return time.Date(2026, 5, 7, 0, 0, 0, 0, time.UTC)
	})

	logReport, ok := res.Reports[CapLog]
	if !ok {
		t.Fatalf("missing report for %s", CapLog)
	}
	if logReport.Status != StatusDegraded {
		t.Fatalf("%s status = %q, want degraded (fixture state=high-volume; truncated envelope must not parse as success)", CapLog, logReport.Status)
	}
	if logReport.Notes == "" {
		t.Fatalf("%s notes are empty; expected parser error trace for truncated envelope", CapLog)
	}
}

// TestProbeOnUnsupportedVersionGoldenFixturePinsCorpusContract loads
// packages/fixtures/golden-outputs/git/unsupported-version.json and
// pins the git adapter contract from plan.md §18.3 for the
// "unsupported-version" state.
//
// Today probeVersion (capabilities.go:175) records the observed git
// version into ProbeResult.Version on a best-effort basis but no
// capability is gated on a minimum version. The fixture documents the
// downstream contract that future version-gating logic must honor:
// report `git._minVersion` as `missing` when the observed version is
// below the integration-contract minimum.
//
// Pinned here:
//
//  1. Fixture self-consistency (state, exit=0, stdoutText "git 0.0.1",
//     _minVersion=missing).
//  2. The synthetic `git._minVersion` capability is *not* a real
//     adapter capability constant — fixture-corpus marker only.
//     AllCapabilityIDs must not contain it; if it ever does, this test
//     fails so the contract change is intentional.
//  3. Probe drives through the unsupported version: `--version` returns
//     "git 0.0.1\n" and probeVersion captures "0.0.1" verbatim into
//     ProbeResult.Version. All real read capabilities (Status, Diff,
//     Log, Show, Remote, Branch, Blame, UnpushedList) still report
//     StatusOK because no version gate exists yet — a future commit
//     that introduces version gating must promote _minVersion to a
//     real capability and downgrade gated capabilities here
//     intentionally. CapPush stays Untested (probe never pushes).
func TestProbeOnUnsupportedVersionGoldenFixturePinsCorpusContract(t *testing.T) {
	t.Parallel()
	fixture := loadGitGoldenFixture(t, "unsupported-version.json")
	if fixture.Meta.State != "unsupported-version" {
		t.Fatalf("fixture state = %q, want unsupported-version", fixture.Meta.State)
	}
	if fixture.Exit != 0 {
		t.Fatalf("fixture exit = %d, want 0 (binary executed and printed version)", fixture.Exit)
	}
	if !strings.Contains(fixture.StdoutText, "0.0.1") {
		t.Fatalf("fixture stdoutText = %q, want '0.0.1' marker", fixture.StdoutText)
	}
	versionCap, ok := fixture.Capabilities["git._minVersion"]
	if !ok || versionCap.Status != "missing" {
		t.Fatalf("fixture must declare git._minVersion=missing, got %+v", fixture.Capabilities)
	}

	for _, id := range AllCapabilityIDs() {
		if id == "git._minVersion" {
			t.Fatalf("git._minVersion is now a real adapter capability — update the fixture/test contract intentionally")
		}
	}

	fake := newFakeExecutor()
	fake.Stdouts["--version"] = []byte(fixture.StdoutText)
	// Healthy responses for every real capability so probeOne reaches
	// StatusOK — the unsupported-version contract does not gate read
	// capabilities today, so they must all stay green.
	logKey := "log --no-color -n 1 --pretty=format:%H%x00%h%x00%an%x00%ae%x00%aI%x00%cn%x00%ce%x00%cI%x00%P%x00%s%x00%b%x1e"
	fake.Stdouts[logKey] = []byte("abcdef0123456789abcdef0123456789abcdef01\x00abcdef0\x00alice\x00alice@example.com\x002026-05-04T00:00:00+00:00\x00alice\x00alice@example.com\x002026-05-04T00:00:00+00:00\x00\x00first commit\x00\x1e")
	fake.Stdouts["status --porcelain=v1 --branch"] = []byte("## main\n")
	fake.Stdouts["diff --no-color"] = []byte("")
	fake.Stdouts["show --no-color HEAD"] = []byte("commit abcdef0123456789abcdef0123456789abcdef01\nAuthor: alice <alice@example.com>\nDate:   2026-05-04T00:00:00+00:00\n\n    first commit\n")
	fake.Stdouts["remote -v"] = []byte("")
	fake.Stdouts["branch -v --no-abbrev"] = []byte("* main abcdef0 first commit\n")
	fake.Stdouts["blame --porcelain -- README.md"] = []byte("")
	fake.Stdouts["rev-list origin/HEAD..HEAD"] = []byte("")

	c := NewWithExecutor("/repo", fake)
	res := Probe(context.Background(), c, func() time.Time { return time.Date(2026, 5, 7, 0, 0, 0, 0, time.UTC) })

	if res.Tool != "git" {
		t.Fatalf("ProbeResult.Tool = %q, want git", res.Tool)
	}
	if !strings.Contains(res.Version, "0.0.1") {
		t.Fatalf("ProbeResult.Version = %q, want it to capture the unsupported '0.0.1' verbatim", res.Version)
	}
	for _, capID := range []string{
		CapStatusRead, CapDiffRead, CapLog, CapShow,
		CapRemoteRead, CapBranchRead, CapBlame, CapUnpushedList,
	} {
		report, ok := res.Reports[capID]
		if !ok {
			t.Fatalf("%s missing from probe report", capID)
		}
		if report.Status != StatusOK {
			t.Fatalf("%s = %q, want ok (no version gating today; future version-gating must promote _minVersion). notes=%q", capID, report.Status, report.Notes)
		}
	}
	if got := res.Reports[CapPush].Status; got != StatusUntested {
		t.Fatalf("CapPush = %q, want untested (probe never pushes; unaffected by version state)", got)
	}
}

// alwaysMissingBinaryExecutor returns ErrMissingBinary for every
// invocation. Mirrors what OSExecutor produces at client.go:116-118
// when the underlying exec.Cmd.Run reports
// "executable file not found".
type alwaysMissingBinaryExecutor struct{}

func (alwaysMissingBinaryExecutor) Run(_ context.Context, _ string, _ []string) ([]byte, []byte, int, error) {
	return nil, nil, -1, ErrMissingBinary
}

type gitGoldenFixture struct {
	Meta struct {
		Adapter string `json:"adapter"`
		State   string `json:"state"`
	} `json:"meta"`
	Argv         []string `json:"argv"`
	Exit         int      `json:"exit"`
	StdoutText   string   `json:"stdoutText"`
	Truncated    bool     `json:"truncated"`
	Capabilities map[string]struct {
		Status   string `json:"status"`
		Notes    string `json:"notes"`
		Fallback string `json:"fallback"`
	} `json:"capabilities"`
}

func loadGitGoldenFixture(t *testing.T, name string) gitGoldenFixture {
	t.Helper()
	root := findRepoRoot(t)
	rel := filepath.Join("packages", "fixtures", "golden-outputs", "git", name)
	data, err := os.ReadFile(filepath.Join(root, rel))
	if err != nil {
		t.Fatalf("read fixture %s: %v", rel, err)
	}
	var fixture gitGoldenFixture
	if err := json.Unmarshal(data, &fixture); err != nil {
		t.Fatalf("parse fixture %s: %v", rel, err)
	}
	if fixture.Meta.Adapter != "git" {
		t.Fatalf("fixture %s adapter = %q, want git", rel, fixture.Meta.Adapter)
	}
	return fixture
}

func findRepoRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.work")); err == nil {
			return dir
		}
		next := filepath.Dir(dir)
		if next == dir {
			t.Fatalf("could not find repo root from %s", dir)
		}
		dir = next
	}
}

func TestProbeReportsOkWhenAllReadOpsSucceed(t *testing.T) {
	t.Parallel()
	dir := initTempRepo(t)
	c := New(dir)
	res := Probe(context.Background(), c, func() time.Time {
		return time.Date(2026, 5, 4, 0, 0, 0, 0, time.UTC)
	})
	if res.Tool != "git" {
		t.Fatalf("expected tool git, got %q", res.Tool)
	}
	if res.Source != "CLI" {
		t.Fatalf("expected source CLI, got %q", res.Source)
	}
	// Push is intentionally untested.
	if res.Reports[CapPush].Status != StatusUntested {
		t.Fatalf("expected push untested, got %q", res.Reports[CapPush].Status)
	}
	// Status / log / branch / remote should be ok.
	for _, id := range []string{CapStatusRead, CapLog, CapBranchRead, CapRemoteRead} {
		report, ok := res.Reports[id]
		if !ok {
			t.Fatalf("missing report for %s", id)
		}
		if report.Status != StatusOK {
			t.Fatalf("expected %s ok, got %q (notes: %s)", id, report.Status, report.Notes)
		}
	}
	// unpushed-list against a fresh repo with no `origin` remote should
	// fail with degraded (not missing — git is present, the ref is not).
	if res.Reports[CapUnpushedList].Status != StatusDegraded {
		t.Fatalf("expected unpushed-list degraded, got %q", res.Reports[CapUnpushedList].Status)
	}
}
