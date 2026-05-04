package rano

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/hoopoe-cockpit/hoopoe/apps/daemon/internal/capabilities"
)

func TestArgvBuildersUseAdoptedSurfacesAndNoShell(t *testing.T) {
	tests := []struct {
		name string
		got  []string
		want []string
	}{
		{name: "version", got: VersionArgv(), want: []string{"rano", "--version"}},
		{name: "signals default", got: SignalsArgv(SignalQuery{}), want: []string{"rano", "signals", "--json", "--since", "15m"}},
		{name: "signals seconds", got: SignalsArgv(SignalQuery{Since: 90 * time.Second}), want: []string{"rano", "signals", "--json", "--since", "90s"}},
	}
	for _, tt := range tests {
		if !reflect.DeepEqual(tt.got, tt.want) {
			t.Fatalf("%s argv = %#v, want %#v", tt.name, tt.got, tt.want)
		}
		assertNoShellTokens(t, tt.got)
		if err := validateAdoptedArgv(tt.got); err != nil {
			t.Fatalf("%s validate: %v", tt.name, err)
		}
	}
}

func TestArgvGuardsRejectUnadoptedSurfaces(t *testing.T) {
	for _, argv := range [][]string{
		{"rano"},
		{"rano", "signals", "--since", "15m"},
		{"rano", "signals", "--json", "--since", "fifteen"},
		{"rano", "proxy", "--json"},
		{"sh", "-c", "rano signals --json"},
	} {
		if err := validateAdoptedArgv(argv); !errors.Is(err, ErrInvalidRequest) {
			t.Fatalf("validateAdoptedArgv(%v) err = %v, want ErrInvalidRequest", argv, err)
		}
	}
}

func TestReadSignalsParsesObservationsAndSummaries(t *testing.T) {
	runner := &fakeRunner{responses: map[string]CommandResult{
		"rano signals --json --since 15m": {Stdout: []byte(`{
			"generated_at":"2026-05-04T00:15:00Z",
			"rano_version":"0.2.1",
			"window":{"start":"2026-05-04T00:00:00Z","end":"2026-05-04T00:15:00Z"},
			"observations":[
				{"timestamp":"2026-05-04T00:01:00Z","harness":"claude","model":"claude-opus","latency_ms":100,"status":"ok","http_status":200,"endpoint_redacted":"https://api.anthropic.com/[redacted]"},
				{"timestamp":"2026-05-04T00:02:00Z","harness":"claude","model":"claude-opus","latency_ms":200,"status":"ok","http_status":200},
				{"timestamp":"2026-05-04T00:03:00Z","harness":"claude","model":"claude-opus","latency_ms":900,"status":"error","error_class":"rate_limit","http_status":429},
				{"timestamp":"2026-05-04T00:04:00Z","harness":"codex","model":"gpt-pro","latency_ms":300,"status":"ok","http_status":200}
			]
		}`)},
	}}
	adapter := New(runner)

	snapshot, err := adapter.ReadSignals(context.Background(), SignalQuery{Since: 15 * time.Minute})
	if err != nil {
		t.Fatalf("ReadSignals: %v", err)
	}
	if snapshot.RanoVersion != "0.2.1" || len(snapshot.Observations) != 4 {
		t.Fatalf("snapshot identity = %+v", snapshot)
	}
	if snapshot.Summary.TotalCalls != 4 || snapshot.Summary.ErrorCount != 1 {
		t.Fatalf("summary counts = %+v", snapshot.Summary)
	}
	if snapshot.Summary.LatencyP50MS != 200 || snapshot.Summary.LatencyP95MS != 900 {
		t.Fatalf("summary latency = %+v", snapshot.Summary)
	}
	if snapshot.Summary.LastErrorClass != "rate_limit" {
		t.Fatalf("summary last error = %q", snapshot.Summary.LastErrorClass)
	}
	if len(snapshot.ByHarnessModel) != 2 {
		t.Fatalf("expected 2 harness/model summaries, got %+v", snapshot.ByHarnessModel)
	}
	claude := snapshot.ByHarnessModel[0]
	if claude.Harness != "claude" || claude.TotalCalls != 3 || claude.ErrorCount != 1 || claude.LatencyP95MS != 900 {
		t.Fatalf("claude summary = %+v", claude)
	}
}

func TestReadSignalsFillsMissingSnapshotTimes(t *testing.T) {
	runner := &fakeRunner{responses: map[string]CommandResult{
		"rano signals --json --since 15m": {Stdout: []byte(`{"observations":[]}`)},
	}}
	adapter := New(runner)
	adapter.Now = fixedNow

	snapshot, err := adapter.ReadSignals(context.Background(), SignalQuery{})
	if err != nil {
		t.Fatalf("ReadSignals: %v", err)
	}
	if !snapshot.GeneratedAt.Equal(fixedNow()) {
		t.Fatalf("generatedAt = %s, want fixed now", snapshot.GeneratedAt)
	}
	if snapshot.Window.End.Sub(snapshot.Window.Start) != defaultSignalsWindow {
		t.Fatalf("window = %+v", snapshot.Window)
	}
}

func TestParseSignalsAcceptsCallsAliasAndDefaultsUnknownModel(t *testing.T) {
	snapshot, err := ParseSignals([]byte(`{
		"calls":[
			{"timestamp":"2026-05-04T00:01:00Z","harness":"gemini","latency_ms":50,"http_status":503}
		]
	}`))
	if err != nil {
		t.Fatalf("ParseSignals: %v", err)
	}
	if snapshot.Observations[0].Model != "unknown" {
		t.Fatalf("model = %q", snapshot.Observations[0].Model)
	}
	if snapshot.Summary.ErrorCount != 1 || snapshot.Summary.LastErrorClass != "http_503" {
		t.Fatalf("summary = %+v", snapshot.Summary)
	}
}

func TestParseSignalsRejectsRawPayloadFields(t *testing.T) {
	_, err := ParseSignals([]byte(`{
		"observations":[
			{"timestamp":"2026-05-04T00:01:00Z","harness":"codex","latency_ms":50,"body":"secret"}
		]
	}`))
	if !errors.Is(err, ErrRedactionContract) {
		t.Fatalf("ParseSignals err = %v, want ErrRedactionContract", err)
	}
}

func TestProbeReportsHappyMissingUnsupportedMalformedTimeoutAndHighVolume(t *testing.T) {
	happy := New(&fakeRunner{responses: map[string]CommandResult{
		"rano --version":                  {Stdout: []byte("rano version 0.2.1\n")},
		"rano signals --json --since 15m": {Stdout: []byte(`{"observations":[]}`)},
	}})
	happy.Now = fixedNow
	report, err := happy.Probe(context.Background())
	if err != nil {
		t.Fatalf("happy probe: %v", err)
	}
	if report.Tool != capabilities.ToolRano || report.Version != "0.2.1" {
		t.Fatalf("report identity = %+v", report)
	}
	if report.Capabilities[CapabilitySignalsRead].Status != capabilities.StatusOK {
		t.Fatalf("signals read = %+v", report.Capabilities[CapabilitySignalsRead])
	}

	missing := New(&fakeRunner{err: ErrMissingBinary})
	missing.Now = fixedNow
	report, err = missing.Probe(context.Background())
	if err != nil {
		t.Fatalf("missing probe: %v", err)
	}
	if report.Capabilities[CapabilityPresent].Status != capabilities.StatusMissing {
		t.Fatalf("missing present = %+v", report.Capabilities[CapabilityPresent])
	}

	unsupported := New(&fakeRunner{responses: map[string]CommandResult{
		"rano --version": {Stdout: []byte("rano version 0.0.9\n")},
	}})
	unsupported.Now = fixedNow
	report, err = unsupported.Probe(context.Background())
	if err != nil {
		t.Fatalf("unsupported probe: %v", err)
	}
	if report.Capabilities[CapabilityPresent].Status != capabilities.StatusDegraded {
		t.Fatalf("unsupported present = %+v", report.Capabilities[CapabilityPresent])
	}
	if report.Capabilities[CapabilitySignalsRead].Status != capabilities.StatusDegraded {
		t.Fatalf("unsupported signals = %+v", report.Capabilities[CapabilitySignalsRead])
	}

	malformed := New(&fakeRunner{responses: map[string]CommandResult{
		"rano --version":                  {Stdout: []byte("rano version 0.2.1\n")},
		"rano signals --json --since 15m": {Stdout: []byte("{not-json")},
	}})
	report, err = malformed.Probe(context.Background())
	if err != nil {
		t.Fatalf("malformed probe: %v", err)
	}
	if report.Capabilities[CapabilitySignalsRead].Status != capabilities.StatusDegraded {
		t.Fatalf("malformed signals = %+v", report.Capabilities[CapabilitySignalsRead])
	}

	timeout := New(&fakeRunner{responses: map[string]CommandResult{
		"rano --version":                  {Stdout: []byte("rano version 0.2.1\n")},
		"rano signals --json --since 15m": {ExitCode: 124, Stderr: []byte("timeout")},
	}})
	report, err = timeout.Probe(context.Background())
	if err != nil {
		t.Fatalf("timeout probe: %v", err)
	}
	if report.Capabilities[CapabilitySignalsRead].Status != capabilities.StatusDegraded {
		t.Fatalf("timeout signals = %+v", report.Capabilities[CapabilitySignalsRead])
	}

	highVolume := New(&fakeRunner{responses: map[string]CommandResult{
		"rano --version":                  {Stdout: []byte("rano version 0.2.1\n")},
		"rano signals --json --since 15m": {Stdout: []byte(`{"observations":[]}` + strings.Repeat(" ", 128))},
	}})
	highVolume.MaxStdoutBytes = 32
	report, err = highVolume.Probe(context.Background())
	if err != nil {
		t.Fatalf("high-volume probe: %v", err)
	}
	if report.Capabilities[CapabilitySignalsRead].Status != capabilities.StatusDegraded {
		t.Fatalf("high-volume signals = %+v", report.Capabilities[CapabilitySignalsRead])
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

func assertNoShellTokens(t *testing.T, argv []string) {
	t.Helper()
	for _, part := range argv {
		if part == "sh" || part == "-c" || part == "bash" {
			t.Fatalf("argv used shell token: %#v", argv)
		}
	}
}

func fixedNow() time.Time {
	return time.Date(2026, 5, 4, 0, 15, 0, 0, time.UTC)
}
