package rch

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"os/exec"
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
