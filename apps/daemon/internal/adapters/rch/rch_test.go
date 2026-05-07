package rch

import (
	"context"
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/hoopoe-cockpit/hoopoe/apps/daemon/internal/capabilities"
)

func TestRunBuildsRCHExecArgvAndCapturesFailureFingerprint(t *testing.T) {
	adapter := New(&fakeRunner{responses: map[string]CommandResult{
		"/data/projects/hoopoe\x00rch exec -- go test ./...": {
			ExitCode: 1,
			Stdout:   []byte("FAIL ./internal/api\n"),
			Stderr:   []byte("[RCH] remote worker-a (queue 120ms exec 2.4s)\n--- FAIL: TestAPI\n"),
		},
	}})
	adapter.Now = fixedNow
	req := RunRequest{
		ProjectID:     "proj_01",
		WorktreePath:  "/data/projects/hoopoe",
		Branch:        "feature/hp-r3l",
		CommitSHA:     "abc123",
		Command:       []string{"go", "test", "./..."},
		Env:           map[string]string{"RCH_PRIORITY": "high"},
		RunnerProfile: "rch",
	}
	got, err := adapter.Run(context.Background(), req)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	wantArgv := []string{"rch", "exec", "--", "go", "test", "./..."}
	if !reflect.DeepEqual(got.NormalizedArgv, wantArgv) {
		t.Fatalf("argv = %#v, want %#v", got.NormalizedArgv, wantArgv)
	}
	if got.ExitCode != 1 || got.FailureFingerprint == "" {
		t.Fatalf("expected build failure result with fingerprint, got %+v", got)
	}
	if got.Summary.Mode != "remote" || got.Summary.Worker != "worker-a" || got.Summary.QueueWait != 120*time.Millisecond || got.Summary.ExecTime != 2400*time.Millisecond {
		t.Fatalf("summary = %+v", got.Summary)
	}
	if got.WorkerTarget != "worker-a" || got.EnvironmentDigest != EnvironmentDigest(req.Env) {
		t.Fatalf("metadata = %+v", got)
	}
	if len(adapter.Runner.(*fakeRunner).calls) != 1 {
		t.Fatalf("runner calls = %+v", adapter.Runner.(*fakeRunner).calls)
	}
	call := adapter.Runner.(*fakeRunner).calls[0]
	if call.Dir != "/data/projects/hoopoe" {
		t.Fatalf("dir = %q", call.Dir)
	}
	if !contains(call.Env, "RCH_VISIBILITY=summary") || !contains(call.Env, "RCH_PRIORITY=high") {
		t.Fatalf("env = %#v", call.Env)
	}
	assertNoShell(t, call.Argv)
}

func TestRunRejectsUnsafeCommandAndWorktree(t *testing.T) {
	tests := []RunRequest{
		{},
		{WorktreePath: "relative", Command: []string{"go", "test"}},
		{WorktreePath: "/", Command: []string{"go", "test"}},
		{WorktreePath: "/repo", Command: []string{}},
		{WorktreePath: "/repo", Command: []string{"sh", "-c", "go test ./..."}},
		{WorktreePath: "/repo", Command: []string{"go", "test\n./..."}},
	}
	for _, req := range tests {
		if _, err := New(&fakeRunner{}).Run(context.Background(), req); !errors.Is(err, ErrInvalidRequest) {
			t.Fatalf("Run(%+v) err = %v, want ErrInvalidRequest", req, err)
		}
	}
}

func TestRunTruncatesHighVolumeOutputWithoutDroppingResult(t *testing.T) {
	adapter := New(&fakeRunner{responses: map[string]CommandResult{
		"/repo\x00rch exec -- go test ./...": {
			Stdout: []byte("[RCH] local (disabled)\n" + strings.Repeat("x", 64)),
		},
	}})
	adapter.Now = fixedNow
	adapter.MaxOutputBytes = 16
	got, err := adapter.Run(context.Background(), RunRequest{WorktreePath: "/repo", Command: []string{"go", "test", "./..."}})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !got.OutputTruncated || len(got.Stdout) != 16 {
		t.Fatalf("expected truncated stdout, got len=%d truncated=%t", len(got.Stdout), got.OutputTruncated)
	}
	if got.Summary.Mode != "local" || got.Summary.Worker != "disabled" {
		t.Fatalf("summary = %+v", got.Summary)
	}
}

func TestProbeReportsRCHCapability(t *testing.T) {
	adapter := New(&fakeRunner{responses: map[string]CommandResult{
		"\x00rch --version": {Stdout: []byte("rch 1.0.18\n")},
	}})
	adapter.Now = fixedNow
	report, err := adapter.Probe(context.Background())
	if err != nil {
		t.Fatalf("Probe: %v", err)
	}
	if report.Tool != capabilities.ToolRCH || report.Version != "1.0.18" {
		t.Fatalf("identity = %+v", report)
	}
	if got := report.Capabilities[CapabilityRun]; got.Status != capabilities.StatusOK || got.Transport != "stdio" {
		t.Fatalf("rch.run = %+v", got)
	}
	if err := report.Validate(); err != nil {
		t.Fatalf("report validation: %v", err)
	}
}

func TestProbeClassifiesMissingMalformedAndTimeout(t *testing.T) {
	missing := New(&fakeRunner{err: ErrMissingBinary})
	missing.Now = fixedNow
	report, err := missing.Probe(context.Background())
	if err != nil {
		t.Fatalf("missing probe: %v", err)
	}
	if report.Capabilities[CapabilityRun].Status != capabilities.StatusMissing {
		t.Fatalf("missing capability = %+v", report.Capabilities[CapabilityRun])
	}

	malformed := New(&fakeRunner{responses: map[string]CommandResult{
		"\x00rch --version": {Stdout: []byte("not a version")},
	}})
	malformed.Now = fixedNow
	report, err = malformed.Probe(context.Background())
	if err != nil {
		t.Fatalf("malformed probe: %v", err)
	}
	if report.Capabilities[CapabilityRun].Status != capabilities.StatusDegraded {
		t.Fatalf("malformed capability = %+v", report.Capabilities[CapabilityRun])
	}

	timeout := New(&fakeRunner{err: context.DeadlineExceeded})
	timeout.Now = fixedNow
	report, err = timeout.Probe(context.Background())
	if err != nil {
		t.Fatalf("timeout probe: %v", err)
	}
	if report.Capabilities[CapabilityRun].Status != capabilities.StatusDegraded {
		t.Fatalf("timeout capability = %+v", report.Capabilities[CapabilityRun])
	}
}

func TestParseSummaryHandlesRemoteLocalAndFailureCodes(t *testing.T) {
	tests := []struct {
		raw       string
		mode      string
		worker    string
		failure   string
		queueWait time.Duration
		execTime  time.Duration
	}{
		{"[RCH] remote worker-1 (queue 10ms exec 1.5s)", "remote", "worker-1", "", 10 * time.Millisecond, 1500 * time.Millisecond},
		{"[RCH] local (hook disabled)", "local", "hook disabled", "", 0, 0},
		{"[RCH] remote worker-2 failed [RCH-E210] disk pressure", "remote", "worker-2", "RCH-E210", 0, 0},
		{"\x1b[32m[RCH]\x1b[0m remote worker-3 (queue 20ms exec 3s)", "remote", "worker-3", "", 20 * time.Millisecond, 3 * time.Second},
	}
	for _, tc := range tests {
		got := ParseSummary([]byte(tc.raw))
		if got.Mode != tc.mode || got.Worker != tc.worker || got.FailureCode != tc.failure || got.QueueWait != tc.queueWait || got.ExecTime != tc.execTime {
			t.Fatalf("ParseSummary(%q) = %+v", tc.raw, got)
		}
	}
}

func TestCapabilityRegistryAcceptsRCHToolID(t *testing.T) {
	registry := capabilities.New("test-api")
	adapter := New(&fakeRunner{responses: map[string]CommandResult{
		"\x00rch --version": {Stdout: []byte("rch 1.0.18\n")},
	}})
	adapter.Now = fixedNow
	if err := registry.RegisterProbe(capabilities.ToolRCH, func() (*capabilities.ToolReport, error) {
		return adapter.Probe(context.Background())
	}); err != nil {
		t.Fatal(err)
	}
	registry.Probe()
	if got, ok := registry.LookupCapability(capabilities.ToolRCH, CapabilityRun); !ok || got.Status != capabilities.StatusOK {
		t.Fatalf("rch.run = %+v ok=%t", got, ok)
	}
}

func TestMissingBinaryDetectionDoesNotHideBadWorkdir(t *testing.T) {
	if !isExecNotFoundErr(&exec.Error{Name: ToolName, Err: exec.ErrNotFound}) {
		t.Fatalf("expected exec.ErrNotFound to classify as missing binary")
	}
	if isExecNotFoundErr(&os.PathError{Op: "chdir", Path: "/missing", Err: fs.ErrNotExist}) {
		t.Fatalf("bad worktree must not classify as missing binary")
	}
}

// TestRunOnHighVolumeGoldenFixtureMarksOutputTruncated loads
// packages/fixtures/golden-outputs/rch/high-volume.json and pins the
// adapter contract from plan.md §18.3 for the high-volume state.
//
// The fixture (added in commit 53e5ba9 [hp-ngfc]) declares the
// authoritative high-volume contract:
//
//   - meta.state == "high-volume"
//   - truncated == true
//   - capabilities["rch.run"].status == "degraded"
//
// Per packages/fixtures/golden-outputs/README.md, `argv` and the
// fixture's `stdoutText` line are placeholders — the byte-level
// payload must be supplied by the test, but the adapter's observable
// outcome (output truncated, capability surfaces the truncation) is
// what the fixture pins.
//
// The existing TestRunTruncatesHighVolumeOutputWithoutDroppingResult
// test exercises the same MaxOutputBytes path but without a
// fixture-loaded contract. This test wraps that path so a future
// fixture edit that drops `truncated: true` or flips
// `capabilities.rch.run` away from `degraded` is caught even if the
// underlying behavior happens to keep working.
//
// Specifically pins:
//   - RunResult.OutputTruncated fires when stdout > MaxOutputBytes
//   - The truncation flag survives all the way to the result struct
//     callers serialize across the daemon API surface (the field is
//     `OutputTruncated` on RunResult; rch.go:215)
func TestRunOnHighVolumeGoldenFixtureMarksOutputTruncated(t *testing.T) {
	fixture := loadRCHGoldenFixture(t, "high-volume.json")
	if fixture.Meta.State != "high-volume" {
		t.Fatalf("fixture state = %q, want high-volume", fixture.Meta.State)
	}
	if !fixture.Truncated {
		t.Fatalf("fixture truncated = false, want true (envelope-truncation is the high-volume contract)")
	}
	cap, ok := fixture.Capabilities["rch.run"]
	if !ok || cap.Status != "degraded" {
		t.Fatalf("fixture must declare rch.run=degraded, got %+v", fixture.Capabilities)
	}

	// Drive Run() with a stdout payload that exceeds MaxOutputBytes,
	// preserving the local-disabled summary line so ParseSummary
	// hits a non-failure path. Use a 16-byte cap with 64 bytes of
	// padding past the summary marker — the same pattern as the
	// existing TestRunTruncatesHighVolumeOutputWithoutDroppingResult
	// test, but driven by the fixture's truncation contract.
	const cap16 = 16
	stdout := []byte("[RCH] local (disabled)\n" + strings.Repeat("x", 64))
	adapter := New(&fakeRunner{responses: map[string]CommandResult{
		"/repo\x00rch exec -- go test ./...": {Stdout: stdout},
	}})
	adapter.Now = fixedNow
	adapter.MaxOutputBytes = cap16
	got, err := adapter.Run(context.Background(), RunRequest{
		WorktreePath: "/repo",
		Command:      []string{"go", "test", "./..."},
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !got.OutputTruncated {
		t.Fatalf("OutputTruncated = false, want true (high-volume contract: stdout > MaxOutputBytes must surface truncation)")
	}
	if len(got.Stdout) != cap16 {
		t.Fatalf("stdout len = %d, want %d (must be capped at MaxOutputBytes)", len(got.Stdout), cap16)
	}
	if got.ExitCode != 0 {
		// The fixture has exit=0 — high-volume is about envelope size,
		// not failure. Pin that the adapter doesn't synthesize a
		// non-zero exit just because output was truncated.
		t.Fatalf("ExitCode = %d, want 0 (high-volume fixture's exit field)", got.ExitCode)
	}
}

// TestRunOnMalformedJSONGoldenFixturePinsCapturePassthrough loads
// packages/fixtures/golden-outputs/rch/malformed-json.json and pins
// the rch adapter contract from plan.md §18.3 for the
// "malformed-json" state.
//
// Unlike br/agent-mail/bv/ntm, the rch Go adapter does NOT parse stdout
// as a JSON envelope — Run() captures bytes, runs ParseSummary on the
// `[RCH]` summary line, and surfaces the captured bytes verbatim
// through `RunResult.Stdout`. The fixture's `rch.run: degraded` marker
// documents the contract that *downstream consumers* of this output
// (scheduler, build-log surface) must treat truncated/non-JSON
// `rch status --json` output as a degraded signal — not a hard failure
// that drops the result.
//
// Pinned here:
//
//  1. Fixture self-consistency (state, exit=0, non-empty stdoutText
//     that fails JSON parse, _highVolume marker absent — this is the
//     malformed-not-truncated state).
//  2. The fixture's stdoutText *does not* parse as valid JSON
//     (sanity guard against a future fixture edit that drifts off the
//     "malformed payload" intent).
//  3. Run() captures the malformed bytes verbatim into RunResult.Stdout
//     without panicking, OutputTruncated stays false (the fixture's
//     truncated=false flag), and the result still carries the project
//     metadata callers serialize across the daemon API surface.
func TestRunOnMalformedJSONGoldenFixturePinsCapturePassthrough(t *testing.T) {
	fixture := loadRCHGoldenFixture(t, "malformed-json.json")
	if fixture.Meta.State != "malformed-json" {
		t.Fatalf("fixture state = %q, want malformed-json", fixture.Meta.State)
	}
	if fixture.Exit != 0 {
		t.Fatalf("fixture exit = %d, want 0 (rch CLI exited cleanly; only the JSON envelope is malformed)", fixture.Exit)
	}
	if fixture.Truncated {
		t.Fatalf("fixture truncated = true, want false (malformed-json is distinct from high-volume)")
	}
	cap, ok := fixture.Capabilities["rch.run"]
	if !ok || cap.Status != "degraded" {
		t.Fatalf("fixture must declare rch.run=degraded, got %+v", fixture.Capabilities)
	}
	if fixture.StdoutText == "" {
		t.Fatalf("fixture stdoutText is empty; expected truncated JSON sample")
	}

	// Sanity: the fixture's stdoutText must NOT be valid JSON.
	var parsed any
	if err := json.Unmarshal([]byte(fixture.StdoutText), &parsed); err == nil {
		t.Fatalf("fixture stdoutText parses as valid JSON; the malformed-json contract requires a non-JSON sample")
	}

	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("Run panicked on malformed JSON fixture stdout: %v", r)
		}
	}()
	adapter := New(&fakeRunner{responses: map[string]CommandResult{
		"/repo\x00rch exec -- go test ./...": {
			ExitCode: fixture.Exit,
			Stdout:   []byte(fixture.StdoutText),
		},
	}})
	adapter.Now = fixedNow
	got, err := adapter.Run(context.Background(), RunRequest{
		WorktreePath: "/repo",
		Command:      []string{"go", "test", "./..."},
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if got.Stdout != fixture.StdoutText {
		t.Fatalf("RunResult.Stdout = %q, want fixture verbatim %q", got.Stdout, fixture.StdoutText)
	}
	if got.OutputTruncated {
		t.Fatalf("OutputTruncated = true, want false (fixture truncated=false)")
	}
	if got.ExitCode != 0 {
		t.Fatalf("ExitCode = %d, want 0 (fixture exit field)", got.ExitCode)
	}
	if got.WorktreePath != "/repo" {
		t.Fatalf("WorktreePath = %q, want /repo (project metadata must survive malformed-stdout pass-through)", got.WorktreePath)
	}
}

// TestProbeOnTimeoutGoldenFixtureDegradesRunCapability loads
// packages/fixtures/golden-outputs/rch/timeout.json and pins the
// adapter contract from plan.md §18.3 for the timeout state.
//
// The fixture authoritatively declares:
//   - meta.state == "timeout"
//   - exit == 124 (standard `timeout(1)` exit code)
//   - stderrText carries "rch status timed out"
//   - capabilities["rch.run"] == {status: "degraded",
//     notes: "status probe timeout"}
//
// The discriminant: a `commandError` from Version() with ExitCode!=0
// (rch.go:233-234) bubbles up to Probe (rch.go:252-258) and
// `statusForError` (rch.go:450-457). statusForError returns
// StatusMissing only for ErrMissingBinary; everything else
// (including a 124-exit commandError) maps to StatusDegraded —
// matching the fixture's contract.
//
// Pins the "timeout surfaces, no silent retry" property by counting
// per-argv invocations: a regression that introduced an unbacked-off
// retry loop on timeout would call `rch --version` more than once
// and fail the assertion below.
func TestProbeOnTimeoutGoldenFixtureDegradesRunCapability(t *testing.T) {
	fixture := loadRCHGoldenFixture(t, "timeout.json")
	if fixture.Meta.State != "timeout" {
		t.Fatalf("fixture state = %q, want timeout", fixture.Meta.State)
	}
	if fixture.Exit != 124 {
		t.Fatalf("fixture exit = %d, want 124", fixture.Exit)
	}
	cap, ok := fixture.Capabilities["rch.run"]
	if !ok || cap.Status != "degraded" {
		t.Fatalf("fixture must declare rch.run=degraded, got %+v", fixture.Capabilities)
	}
	if !strings.Contains(fixture.StderrText, "timed out") {
		t.Fatalf("fixture stderrText = %q, want a 'timed out' marker", fixture.StderrText)
	}

	inner := &fakeRunner{responses: map[string]CommandResult{
		"\x00rch --version": {
			ExitCode: fixture.Exit,
			Stderr:   []byte(fixture.StderrText),
		},
	}}
	counter := &countingRCHRunner{inner: inner}
	adapter := New(counter)
	adapter.Now = fixedNow
	report, err := adapter.Probe(context.Background())
	if err != nil {
		t.Fatalf("Probe: %v", err)
	}

	got := report.Capabilities[CapabilityRun]
	if got.Status != capabilities.StatusDegraded {
		t.Fatalf("%s status = %q, want degraded (fixture state=timeout, exit=124)", CapabilityRun, got.Status)
	}
	if got.Notes == "" {
		t.Fatalf("%s notes are empty; expected commandError detail", CapabilityRun)
	}
	if !strings.Contains(got.Notes, "exited 124") {
		t.Fatalf("%s notes = %q; expected mention of exit 124", CapabilityRun, got.Notes)
	}

	// "No silent retry" contract: Probe runs `rch --version` exactly
	// once on the failure path. A regression that added an
	// unbacked-off retry loop would push the call count above 1.
	if c := counter.calls("\x00rch --version"); c != 1 {
		t.Fatalf("rch --version invocation count = %d, want 1 (timeout fixture pins no-retry-without-backoff)", c)
	}
}

// countingRCHRunner wraps a Runner and tracks per-key invocation
// counts. The key is the same `dir + "\x00" + argv` shape rch's
// fakeRunner uses internally so tests can pin retry-loop contracts.
type countingRCHRunner struct {
	inner    Runner
	keyCalls map[string]int
}

func (c *countingRCHRunner) Run(ctx context.Context, invocation Invocation) (CommandResult, error) {
	if c.keyCalls == nil {
		c.keyCalls = map[string]int{}
	}
	key := invocation.Dir + "\x00" + strings.Join(invocation.Argv, " ")
	c.keyCalls[key]++
	return c.inner.Run(ctx, invocation)
}

func (c *countingRCHRunner) calls(key string) int {
	if c.keyCalls == nil {
		return 0
	}
	return c.keyCalls[key]
}

type rchGoldenFixture struct {
	Meta struct {
		Adapter string `json:"adapter"`
		State   string `json:"state"`
	} `json:"meta"`
	Exit         int    `json:"exit"`
	StdoutText   string `json:"stdoutText"`
	StderrText   string `json:"stderrText"`
	Truncated    bool   `json:"truncated"`
	Capabilities map[string]struct {
		Status string `json:"status"`
		Notes  string `json:"notes"`
	} `json:"capabilities"`
}

func loadRCHGoldenFixture(t *testing.T, name string) rchGoldenFixture {
	t.Helper()
	path := filepath.Join(findRCHConformanceRepoRoot(t), "packages", "fixtures", "golden-outputs", "rch", name)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read fixture %s: %v", path, err)
	}
	var fixture rchGoldenFixture
	if err := json.Unmarshal(data, &fixture); err != nil {
		t.Fatalf("parse fixture %s: %v", path, err)
	}
	if fixture.Meta.Adapter != "rch" {
		t.Fatalf("fixture %s adapter = %q, want rch", path, fixture.Meta.Adapter)
	}
	return fixture
}

func assertNoShell(t *testing.T, argv []string) {
	t.Helper()
	if len(argv) < 4 || argv[0] != ToolName || argv[1] != "exec" || argv[2] != "--" {
		t.Fatalf("argv must invoke rch exec directly: %#v", argv)
	}
	for _, arg := range argv {
		if arg == "sh" || arg == "-c" || strings.Contains(arg, "&&") || strings.Contains(arg, ";") {
			t.Fatalf("argv contains shell token: %#v", argv)
		}
	}
}

func fixedNow() time.Time {
	return time.Date(2026, 5, 4, 2, 35, 0, 0, time.UTC)
}

type fakeRunner struct {
	responses map[string]CommandResult
	err       error
	calls     []Invocation
}

func (r *fakeRunner) Run(_ context.Context, invocation Invocation) (CommandResult, error) {
	r.calls = append(r.calls, Invocation{
		Argv:    append([]string(nil), invocation.Argv...),
		Dir:     invocation.Dir,
		Env:     append([]string(nil), invocation.Env...),
		Timeout: invocation.Timeout,
	})
	if r.err != nil {
		return CommandResult{ExitCode: -1}, r.err
	}
	key := invocation.Dir + "\x00" + strings.Join(invocation.Argv, " ")
	if result, ok := r.responses[key]; ok {
		return result, nil
	}
	return CommandResult{ExitCode: 127, Stderr: []byte("missing fake response for " + key)}, nil
}

func contains(values []string, needle string) bool {
	for _, value := range values {
		if value == needle {
			return true
		}
	}
	return false
}
