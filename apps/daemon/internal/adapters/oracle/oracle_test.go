package oracle

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/hoopoe-cockpit/hoopoe/apps/daemon/internal/capabilities"
)

func TestBrowserRunBuildsRemoteArgvReadsOutputAndRedactsToken(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	outputPath := filepath.Join(dir, "candidate.md")
	inputPath := filepath.Join(dir, "context.md")
	if err := os.WriteFile(inputPath, []byte("repo context"), 0o600); err != nil {
		t.Fatal(err)
	}
	runner := &fakeRunner{
		onRun: func(invocation Invocation) (CommandResult, error) {
			if err := os.WriteFile(outputPath, []byte("# ChatGPT Pro candidate\n"), 0o600); err != nil {
				return CommandResult{}, err
			}
			return CommandResult{Stdout: []byte("wrote output\n")}, nil
		},
	}
	adapter := New(runner)
	adapter.Now = fixedNow

	got, err := adapter.BrowserRun(context.Background(), RunRequest{
		Model:      "gpt-5.4-pro",
		Prompt:     "draft a planning candidate",
		Files:      []string{inputPath},
		OutputPath: outputPath,
		WorkDir:    dir,
		Remote:     RemoteConfig{Host: "127.0.0.1:7800", Token: "token-1234567890"},
		Env:        []string{"ORACLE_LOG=info"},
	})
	if err != nil {
		t.Fatalf("BrowserRun: %v", err)
	}
	if len(runner.calls) != 1 {
		t.Fatalf("runner calls = %d", len(runner.calls))
	}
	call := runner.calls[0]
	wantArgv := []string{
		"oracle",
		"--engine", "browser",
		"--model", "gpt-5.4-pro",
		"--prompt", "draft a planning candidate",
		"--file", inputPath,
		"--write-output", outputPath,
		"--remote-host", "127.0.0.1:7800",
		"--remote-token", "token-1234567890",
	}
	if !reflect.DeepEqual(call.Argv, wantArgv) {
		t.Fatalf("argv = %#v, want %#v", call.Argv, wantArgv)
	}
	if !contains(call.Env, "ORACLE_LOG=info") {
		t.Fatalf("env = %#v", call.Env)
	}
	if strings.Contains(strings.Join(got.NormalizedArgv, " "), "token-1234567890") {
		t.Fatalf("normalized argv leaked remote token: %#v", got.NormalizedArgv)
	}
	if got.OutputText != "# ChatGPT Pro candidate\n" || got.OutputSHA256 == "" {
		t.Fatalf("output fields = %+v", got)
	}
	if got.PromptSHA256 == "" || got.ExitCode != 0 || !got.MacMustStayAwake {
		t.Fatalf("result metadata = %+v", got)
	}
}

func TestBrowserRunRejectsInvalidRequestsAndProviderKeys(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	outputPath := filepath.Join(dir, "out.md")
	key := credentialKey("OP", "EN", "AI")
	tests := []RunRequest{
		{},
		{Prompt: "x", OutputPath: "relative"},
		{Prompt: "x", OutputPath: "/"},
		{Prompt: "x", OutputPath: outputPath, Files: []string{"relative"}},
		{Prompt: "x", OutputPath: outputPath, Remote: RemoteConfig{Host: "127.0.0.1:7800"}},
		{Prompt: "x", OutputPath: outputPath, Remote: RemoteConfig{Host: "127.0.0.1:7800", Token: "bad token"}},
		{Prompt: "x", OutputPath: outputPath, Env: []string{key + "=secret"}},
	}
	for _, req := range tests {
		_, err := New(&fakeRunner{}).BrowserRun(context.Background(), req)
		if !errors.Is(err, ErrInvalidRequest) && !errors.Is(err, ErrProviderCredentialEnv) {
			t.Fatalf("BrowserRun(%+v) err = %v, want invalid/provider-key error", req, err)
		}
	}
}

func TestBrowserRunMissingAndTooLargeOutputAreContractErrors(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	outputPath := filepath.Join(dir, "out.md")
	adapter := New(&fakeRunner{result: CommandResult{}})
	_, err := adapter.BrowserRun(context.Background(), RunRequest{Prompt: "x", OutputPath: outputPath})
	if !errors.Is(err, ErrMissingOutput) {
		t.Fatalf("missing output err = %v, want ErrMissingOutput", err)
	}

	runner := &fakeRunner{
		onRun: func(_ Invocation) (CommandResult, error) {
			return CommandResult{}, os.WriteFile(outputPath, []byte(strings.Repeat("x", 17)), 0o600)
		},
	}
	adapter = New(runner)
	adapter.MaxOutputBytes = 16
	_, err = adapter.BrowserRun(context.Background(), RunRequest{Prompt: "x", OutputPath: outputPath})
	if !errors.Is(err, ErrOutputTooLarge) {
		t.Fatalf("large output err = %v, want ErrOutputTooLarge", err)
	}
}

func TestBrowserRunNonZeroExitReturnsCommandFailedAndSkipsOutputRead(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	outputPath := filepath.Join(dir, "out.md")
	adapter := New(&fakeRunner{result: CommandResult{ExitCode: 7, Stderr: []byte("auth expired")}})
	got, err := adapter.BrowserRun(context.Background(), RunRequest{Prompt: "x", OutputPath: outputPath})
	if !errors.Is(err, ErrCommandFailed) {
		t.Fatalf("err = %v, want ErrCommandFailed", err)
	}
	if got.ExitCode != 7 || got.Stderr != "auth expired" {
		t.Fatalf("result = %+v", got)
	}
}

func TestProbeReportsHealthyRemoteCapabilities(t *testing.T) {
	t.Parallel()
	adapter := New(&fakeRunner{responses: map[string]CommandResult{
		key("", HelpArgv()):        {Stdout: []byte("oracle help\n")},
		key("", ServeStatusArgv()): {Stdout: []byte(`{"healthy":true,"model":"gpt-5.4-pro","last_request_ts":"2026-05-04T00:00:00Z"}`)},
	}})
	adapter.Now = fixedNow
	adapter.Remote = RemoteConfig{Host: "127.0.0.1:7800", Token: "token-1234567890"}

	report, err := adapter.Probe(context.Background())
	if err != nil {
		t.Fatalf("Probe: %v", err)
	}
	if report.Tool != capabilities.ToolOracle || report.Version != "browser:gpt-5.4-pro" {
		t.Fatalf("identity = %+v", report)
	}
	for _, capID := range []string{CapabilityHelp, CapabilityServeStatus, CapabilityBrowserRun, CapabilityRemoteInvoke} {
		if got := report.Capabilities[capID]; got.Status != capabilities.StatusOK {
			t.Fatalf("%s = %+v, want ok", capID, got)
		}
	}
	if report.Capabilities[CapabilityRemoteInvoke].Transport != "ssh-reverse-tunnel" {
		t.Fatalf("remote transport = %+v", report.Capabilities[CapabilityRemoteInvoke])
	}
	if err := report.Validate(); err != nil {
		t.Fatalf("report validation: %v", err)
	}
}

func TestProbeLeavesRemoteInvokeMissingWhenRemoteNotConfigured(t *testing.T) {
	t.Parallel()
	adapter := New(&fakeRunner{responses: map[string]CommandResult{
		key("", HelpArgv()):        {Stdout: []byte("oracle help\n")},
		key("", ServeStatusArgv()): {Stdout: []byte(`{"healthy":true,"model":"gpt-5.4-pro"}`)},
	}})
	adapter.Now = fixedNow

	report, err := adapter.Probe(context.Background())
	if err != nil {
		t.Fatalf("Probe: %v", err)
	}
	if report.Capabilities[CapabilityBrowserRun].Status != capabilities.StatusOK {
		t.Fatalf("browser capability = %+v", report.Capabilities[CapabilityBrowserRun])
	}
	if got := report.Capabilities[CapabilityRemoteInvoke]; got.Status != capabilities.StatusMissing {
		t.Fatalf("remote capability = %+v, want missing", got)
	}
}

func TestProbeClassifiesMissingAndUnhealthyServe(t *testing.T) {
	t.Parallel()
	missing := New(&fakeRunner{err: ErrMissingBinary})
	missing.Now = fixedNow
	report, err := missing.Probe(context.Background())
	if err != nil {
		t.Fatalf("missing probe: %v", err)
	}
	if report.Capabilities[CapabilityHelp].Status != capabilities.StatusMissing {
		t.Fatalf("missing help capability = %+v", report.Capabilities[CapabilityHelp])
	}

	unhealthy := New(&fakeRunner{responses: map[string]CommandResult{
		key("", HelpArgv()):        {Stdout: []byte("oracle help\n")},
		key("", ServeStatusArgv()): {Stdout: []byte(`{"healthy":false,"warnings":["signed out"]}`)},
	}})
	unhealthy.Now = fixedNow
	report, err = unhealthy.Probe(context.Background())
	if err != nil {
		t.Fatalf("unhealthy probe: %v", err)
	}
	if report.Capabilities[CapabilityServeStatus].Status != capabilities.StatusDegraded ||
		report.Capabilities[CapabilityBrowserRun].Status != capabilities.StatusDegraded ||
		report.Capabilities[CapabilityRemoteInvoke].Status != capabilities.StatusMissing {
		t.Fatalf("unhealthy capabilities = %+v", report.Capabilities)
	}
}

func TestParseServeStatusHandlesJSONAndText(t *testing.T) {
	t.Parallel()
	status, err := ParseServeStatus([]byte(`{"healthy":true,"model":"gpt-5.4-pro"}`))
	if err != nil || !status.Healthy || status.Model != "gpt-5.4-pro" {
		t.Fatalf("json status = %+v err=%v", status, err)
	}
	status, err = ParseServeStatus([]byte("Oracle serve running and healthy"))
	if err != nil || !status.Healthy {
		t.Fatalf("text status = %+v err=%v", status, err)
	}
	status, err = ParseServeStatus([]byte("Oracle serve offline"))
	if err != nil || status.Healthy || len(status.Warnings) != 1 {
		t.Fatalf("offline status = %+v err=%v", status, err)
	}
}

func fixedNow() time.Time {
	return time.Date(2026, 5, 4, 0, 45, 0, 0, time.UTC)
}

type fakeRunner struct {
	responses map[string]CommandResult
	result    CommandResult
	err       error
	onRun     func(Invocation) (CommandResult, error)
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
	if r.onRun != nil {
		return r.onRun(invocation)
	}
	if r.responses != nil {
		if result, ok := r.responses[key(invocation.Dir, invocation.Argv)]; ok {
			return result, nil
		}
	}
	return r.result, nil
}

func key(dir string, argv []string) string {
	return dir + "\x00" + strings.Join(argv, "\x00")
}

func contains(values []string, needle string) bool {
	for _, value := range values {
		if value == needle {
			return true
		}
	}
	return false
}
