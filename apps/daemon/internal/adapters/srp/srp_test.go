package srp

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/hoopoe-cockpit/hoopoe/apps/daemon/internal/capabilities"
)

func TestSignalsParseAndThresholdCrossings(t *testing.T) {
	adapter := New(&fakeRunner{responses: map[string]CommandResult{
		"srp signals --json": {Stdout: []byte(`{"healthy":true,"cpu":{"load1":2.5,"load5":1.5,"load15":1},"mem":{"used_mb":1000,"free_mb":500},"disk":{"/data":{"free_gb":4,"percent":96.5}},"thresholds":{"disk_warn_percent":90,"disk_critical_percent":95}}`)},
	}})
	adapter.Now = fixedNow

	signals, err := adapter.Signals(context.Background())
	if err != nil {
		t.Fatalf("signals: %v", err)
	}
	if signals.Source != "srp" || signals.CPU.Load1 != 2.5 || signals.Disk["/data"].Percent != 96.5 {
		t.Fatalf("signals = %+v", signals)
	}
	crossings := CrossedThresholds(signals)
	if len(crossings) != 1 || crossings[0].Action != "sbh.cleanup" || crossings[0].Severity != "critical" {
		t.Fatalf("crossings = %+v", crossings)
	}
}

func TestProbeReportsRegistryCapabilities(t *testing.T) {
	adapter := New(&fakeRunner{responses: map[string]CommandResult{
		"srp status --json": {Stdout: []byte(`{"healthy":true,"version":"0.4.0"}`)},
	}})
	adapter.Now = fixedNow
	registry := capabilities.New("test-api")
	if err := registry.RegisterProbe(capabilities.ToolSRP, func() (*capabilities.ToolReport, error) {
		return adapter.Probe(context.Background())
	}); err != nil {
		t.Fatal(err)
	}
	registry.Probe()
	if got, ok := registry.LookupCapability(capabilities.ToolSRP, CapabilitySignalsRead); !ok || got.Status != capabilities.StatusOK {
		t.Fatalf("srp.signals.read = %+v, %v", got, ok)
	}
}

func TestMissingSRPFallsBackToProcSignalsAndReportsDegraded(t *testing.T) {
	adapter := New(&fakeRunner{err: errors.New("exec: \"srp\": executable file not found")})
	adapter.Now = fixedNow
	adapter.Fallback = fakeFallback{snapshot: SignalSnapshot{
		Healthy: true,
		Disk:    map[string]DiskSignal{"/": {Percent: 50}},
	}}

	signals, err := adapter.Signals(context.Background())
	if err != nil {
		t.Fatalf("signals fallback: %v", err)
	}
	if signals.Source != "procfs" || len(signals.Warnings) == 0 {
		t.Fatalf("fallback signals = %+v", signals)
	}

	report, err := adapter.Probe(context.Background())
	if err != nil {
		t.Fatalf("probe: %v", err)
	}
	if report.Capabilities[CapabilitySignalsRead].Status != capabilities.StatusDegraded {
		t.Fatalf("signals capability = %+v", report.Capabilities[CapabilitySignalsRead])
	}
	if report.Capabilities[CapabilityStatusRead].Status != capabilities.StatusMissing {
		t.Fatalf("status capability = %+v", report.Capabilities[CapabilityStatusRead])
	}
}

func TestMalformedAndTimeoutDoNotPanic(t *testing.T) {
	malformed := New(&fakeRunner{responses: map[string]CommandResult{
		"srp status --json":  {Stdout: []byte(`{"healthy": tru`)},
		"srp signals --json": {Stdout: []byte(`{"healthy": tru`)},
	}})
	malformed.Now = fixedNow
	malformed.Fallback = nil
	report, err := malformed.Probe(context.Background())
	if err != nil {
		t.Fatalf("malformed probe: %v", err)
	}
	if report.Capabilities[CapabilityStatusRead].Status != capabilities.StatusDegraded {
		t.Fatalf("malformed status = %+v", report.Capabilities[CapabilityStatusRead])
	}

	timeout := New(&fakeRunner{responses: map[string]CommandResult{
		"srp status --json":  {ExitCode: 124, Stderr: []byte("timeout")},
		"srp signals --json": {ExitCode: 124, Stderr: []byte("timeout")},
	}})
	timeout.Now = fixedNow
	timeout.Fallback = nil
	report, err = timeout.Probe(context.Background())
	if err != nil {
		t.Fatalf("timeout probe: %v", err)
	}
	if report.Capabilities[CapabilitySignalsRead].Status != capabilities.StatusDegraded {
		t.Fatalf("timeout signals = %+v", report.Capabilities[CapabilitySignalsRead])
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
	key := join(argv)
	result, ok := r.responses[key]
	if !ok {
		return CommandResult{ExitCode: 127, Stderr: []byte("unexpected argv: " + key)}, nil
	}
	return result, nil
}

type fakeFallback struct {
	snapshot SignalSnapshot
	err      error
}

func (f fakeFallback) ReadSignals(context.Context) (SignalSnapshot, error) {
	return f.snapshot, f.err
}

func join(parts []string) string {
	out := ""
	for i, part := range parts {
		if i > 0 {
			out += " "
		}
		out += part
	}
	return out
}

func fixedNow() time.Time {
	return time.Date(2026, 5, 4, 0, 0, 0, 0, time.UTC)
}
