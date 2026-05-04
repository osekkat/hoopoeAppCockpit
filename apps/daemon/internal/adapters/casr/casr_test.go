package casr

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/hoopoe-cockpit/hoopoe/apps/daemon/internal/capabilities"
)

func fixedNow() time.Time {
	return time.Date(2026, 5, 4, 0, 0, 0, 0, time.UTC)
}

func validRequest() ResumeRequest {
	return ResumeRequest{
		SessionID:   "sess_abc",
		From:        HarnessClaudeCode,
		To:          HarnessCodexCLI,
		FromAccount: "claude-max-1",
		ToAccount:   "gpt-pro-1",
		Reason:      "claude max rate-limited",
	}
}

func TestResumeArgvHappyPath(t *testing.T) {
	got, err := ResumeArgv(validRequest())
	if err != nil {
		t.Fatalf("ResumeArgv: %v", err)
	}
	want := []string{
		"casr", "resume",
		"--session", "sess_abc",
		"--from", "claude-code",
		"--to", "codex",
		"--reason", "claude max rate-limited",
		"--json",
		"--from-account", "claude-max-1",
		"--to-account", "gpt-pro-1",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("argv = %#v\n want %#v", got, want)
	}
}

func TestResumeArgvOmitsAccountFlagsWhenEmpty(t *testing.T) {
	req := validRequest()
	req.FromAccount = ""
	req.ToAccount = ""
	got, err := ResumeArgv(req)
	if err != nil {
		t.Fatalf("ResumeArgv: %v", err)
	}
	for _, flag := range []string{"--from-account", "--to-account"} {
		for _, v := range got {
			if v == flag {
				t.Fatalf("argv %v should not contain %s when account empty", got, flag)
			}
		}
	}
}

func TestResumeArgvValidatesInputs(t *testing.T) {
	cases := []struct {
		name string
		mut  func(*ResumeRequest)
	}{
		{"missing session", func(r *ResumeRequest) { r.SessionID = "  " }},
		{"missing reason", func(r *ResumeRequest) { r.Reason = "" }},
		{"unknown from", func(r *ResumeRequest) { r.From = "kestrel" }},
		{"unknown to", func(r *ResumeRequest) { r.To = "kestrel" }},
		{"same harness", func(r *ResumeRequest) { r.To = r.From }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := validRequest()
			tc.mut(&req)
			if _, err := ResumeArgv(req); !errors.Is(err, ErrInvalidRequest) {
				t.Fatalf("err = %v, want ErrInvalidRequest", err)
			}
		})
	}
}

func TestResumeSessionParsesJSON(t *testing.T) {
	stdout := []byte(`{
		"resumed": true,
		"new_session_id": "sess_def",
		"from_harness": "claude-code",
		"to_harness": "codex",
		"context_lines": 1024,
		"post_status_ref": "audit_evt_77"
	}`)
	argv, err := ResumeArgv(validRequest())
	if err != nil {
		t.Fatalf("argv: %v", err)
	}
	adapter := New(&fakeRunner{responses: map[string]CommandResult{
		strings.Join(argv, " "): {Stdout: stdout},
	}})
	result, err := adapter.ResumeSession(context.Background(), validRequest())
	if err != nil {
		t.Fatalf("ResumeSession: %v", err)
	}
	if !result.Resumed || result.NewSessionID != "sess_def" || result.ContextLines != 1024 {
		t.Fatalf("result = %+v", result)
	}
	if result.FromHarness != "claude-code" || result.ToHarness != "codex" {
		t.Fatalf("harness fields lost: %+v", result)
	}
}

func TestResumeSessionFillsHarnessFromRequestWhenAbsent(t *testing.T) {
	stdout := []byte(`{"resumed": true, "new_session_id": "sess_def"}`)
	argv, _ := ResumeArgv(validRequest())
	adapter := New(&fakeRunner{responses: map[string]CommandResult{
		strings.Join(argv, " "): {Stdout: stdout},
	}})
	result, err := adapter.ResumeSession(context.Background(), validRequest())
	if err != nil {
		t.Fatalf("ResumeSession: %v", err)
	}
	if result.FromHarness != "claude-code" || result.ToHarness != "codex" {
		t.Fatalf("default harness fill failed: %+v", result)
	}
}

func TestResumeSessionRejectsMalformedJSON(t *testing.T) {
	argv, _ := ResumeArgv(validRequest())
	adapter := New(&fakeRunner{responses: map[string]CommandResult{
		strings.Join(argv, " "): {Stdout: []byte(`{"resumed":`)},
	}})
	_, err := adapter.ResumeSession(context.Background(), validRequest())
	if err == nil || !strings.Contains(err.Error(), "decode JSON") {
		t.Fatalf("err = %v, want decode JSON", err)
	}
}

func TestResumeSessionSurfacesNonZeroExit(t *testing.T) {
	argv, _ := ResumeArgv(validRequest())
	adapter := New(&fakeRunner{responses: map[string]CommandResult{
		strings.Join(argv, " "): {ExitCode: 2, Stderr: []byte("rate-limited target")},
	}})
	_, err := adapter.ResumeSession(context.Background(), validRequest())
	if err == nil || !strings.Contains(err.Error(), "exited 2") {
		t.Fatalf("err = %v, want non-zero exit", err)
	}
}

func TestResumeSessionRejectsEmptyStdout(t *testing.T) {
	argv, _ := ResumeArgv(validRequest())
	adapter := New(&fakeRunner{responses: map[string]CommandResult{
		strings.Join(argv, " "): {Stdout: []byte("")},
	}})
	_, err := adapter.ResumeSession(context.Background(), validRequest())
	if err == nil || !strings.Contains(err.Error(), "empty JSON response") {
		t.Fatalf("err = %v, want empty JSON response", err)
	}
}

func TestProbeBlockedByPolicyWhenHealthy(t *testing.T) {
	// CLI present + healthy → resume capability is blocked-by-policy until
	// the post-MVP flag flips. The action executor must refuse to run.
	adapter := New(&fakeRunner{responses: map[string]CommandResult{
		"casr status --json": {Stdout: []byte(`{"healthy":true,"version":"0.1.0"}`)},
	}})
	adapter.Now = fixedNow
	report, err := adapter.Probe(context.Background())
	if err != nil {
		t.Fatalf("Probe: %v", err)
	}
	if report.Tool != capabilities.ToolCASR {
		t.Fatalf("tool = %s", report.Tool)
	}
	if report.Version != "0.1.0" {
		t.Fatalf("version = %q", report.Version)
	}
	if got := report.Capabilities[CapabilitySessionResume]; got.Status != capabilities.StatusBlockedByPolicy {
		t.Fatalf("session.resume = %+v, want blocked-by-policy", got)
	}
	if !strings.Contains(report.Capabilities[CapabilitySessionResume].Notes, "post-MVP") {
		t.Fatalf("blocked-by-policy notes must point at post-MVP enable: %q",
			report.Capabilities[CapabilitySessionResume].Notes)
	}
	if got := report.Capabilities[CapabilityStatusRead]; got.Status != capabilities.StatusOK {
		t.Fatalf("status.read = %+v, want ok", got)
	}
}

func TestProbeMissingWhenCLINotInstalled(t *testing.T) {
	adapter := New(&fakeRunner{
		responses: map[string]CommandResult{},
		err:       errors.New("exec: \"casr\": executable file not found in $PATH"),
	})
	adapter.Now = fixedNow
	report, err := adapter.Probe(context.Background())
	if err != nil {
		t.Fatalf("Probe: %v", err)
	}
	for _, capID := range []string{CapabilityStatusRead, CapabilitySessionResume} {
		if got := report.Capabilities[capID]; got.Status != capabilities.StatusMissing {
			t.Fatalf("%s = %+v, want missing", capID, got)
		}
	}
}

func TestProbeDegradedOnTimeout(t *testing.T) {
	adapter := New(&fakeRunner{responses: map[string]CommandResult{
		"casr status --json": {ExitCode: 124, Stderr: []byte("timeout")},
	}})
	adapter.Now = fixedNow
	report, err := adapter.Probe(context.Background())
	if err != nil {
		t.Fatalf("Probe: %v", err)
	}
	if got := report.Capabilities[CapabilitySessionResume]; got.Status != capabilities.StatusDegraded {
		t.Fatalf("session.resume (timeout) = %+v, want degraded", got)
	}
}

func TestProbeDegradedWhenStatusUnhealthy(t *testing.T) {
	adapter := New(&fakeRunner{responses: map[string]CommandResult{
		"casr status --json": {Stdout: []byte(`{"healthy":false,"warnings":["redis connection refused"]}`)},
	}})
	adapter.Now = fixedNow
	report, err := adapter.Probe(context.Background())
	if err != nil {
		t.Fatalf("Probe: %v", err)
	}
	got := report.Capabilities[CapabilitySessionResume]
	if got.Status != capabilities.StatusDegraded {
		t.Fatalf("session.resume = %+v, want degraded", got)
	}
	if !strings.Contains(got.Notes, "redis connection refused") {
		t.Fatalf("warnings lost: %q", got.Notes)
	}
}

func TestProbeDegradedOnMalformedJSON(t *testing.T) {
	adapter := New(&fakeRunner{responses: map[string]CommandResult{
		"casr status --json": {Stdout: []byte(`{"healthy":`)},
	}})
	adapter.Now = fixedNow
	report, err := adapter.Probe(context.Background())
	if err != nil {
		t.Fatalf("Probe: %v", err)
	}
	if got := report.Capabilities[CapabilitySessionResume]; got.Status != capabilities.StatusDegraded {
		t.Fatalf("session.resume (malformed) = %+v, want degraded", got)
	}
}

func TestResumeSessionIntentShape(t *testing.T) {
	intent, err := ResumeSessionIntent("agent.worker.7", validRequest())
	if err != nil {
		t.Fatalf("intent: %v", err)
	}
	if intent.Kind != ActionResumeSession || intent.CapabilityID != CapabilitySessionResume {
		t.Fatalf("intent kind/cap = %+v", intent)
	}
	if intent.Target["agentId"] != "agent.worker.7" {
		t.Fatalf("target = %+v", intent.Target)
	}
	if intent.Args["sessionId"] != "sess_abc" || intent.Args["from"] != "claude-code" || intent.Args["to"] != "codex" {
		t.Fatalf("args = %+v", intent.Args)
	}
	if intent.Args["fromAccount"] != "claude-max-1" || intent.Args["toAccount"] != "gpt-pro-1" {
		t.Fatalf("account args = %+v", intent.Args)
	}
	if len(intent.Preconditions) == 0 || len(intent.Postconditions) == 0 {
		t.Fatalf("pre/postconditions empty: %+v", intent)
	}
}

func TestResumeSessionIntentRejectsMissingAgentID(t *testing.T) {
	if _, err := ResumeSessionIntent("  ", validRequest()); !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("err = %v, want ErrInvalidRequest", err)
	}
}

func TestResumeSessionIntentRejectsInvalidRequest(t *testing.T) {
	bad := validRequest()
	bad.SessionID = ""
	if _, err := ResumeSessionIntent("agent.worker.7", bad); !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("err = %v, want ErrInvalidRequest", err)
	}
}

func TestResumeSessionIntentOmitsAccountArgsWhenEmpty(t *testing.T) {
	req := validRequest()
	req.FromAccount = ""
	req.ToAccount = ""
	intent, err := ResumeSessionIntent("agent.worker.7", req)
	if err != nil {
		t.Fatalf("intent: %v", err)
	}
	if _, ok := intent.Args["fromAccount"]; ok {
		t.Fatalf("fromAccount should be omitted: %+v", intent.Args)
	}
	if _, ok := intent.Args["toAccount"]; ok {
		t.Fatalf("toAccount should be omitted: %+v", intent.Args)
	}
}

func TestHarnessValid(t *testing.T) {
	valid := []Harness{HarnessClaudeCode, HarnessCodexCLI, HarnessGeminiCLI}
	for _, h := range valid {
		if !h.Valid() {
			t.Errorf("%s should be valid", h)
		}
	}
	for _, h := range []Harness{"", "kestrel", "GPT"} {
		if h.Valid() {
			t.Errorf("%s should not be valid", h)
		}
	}
}

type fakeRunner struct {
	responses map[string]CommandResult
	err       error
}

func (r *fakeRunner) Run(_ context.Context, argv []string) (CommandResult, error) {
	if r.err != nil {
		return CommandResult{}, r.err
	}
	key := strings.Join(argv, " ")
	result, ok := r.responses[key]
	if !ok {
		return CommandResult{ExitCode: 127, Stderr: []byte("unexpected argv: " + key)}, nil
	}
	return result, nil
}
