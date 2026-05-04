package pt

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/hoopoe-cockpit/hoopoe/apps/daemon/internal/capabilities"
)

func TestKillArgvRequiresTargetReasonAndCmdlineMatch(t *testing.T) {
	for _, req := range []KillRequest{
		{PID: 123, CmdlineContains: "codex"},
		{PID: 123, Reason: "wedged"},
		{Reason: "wedged", CmdlineContains: "codex"},
	} {
		if _, err := KillArgv(req); !errors.Is(err, ErrInvalidRequest) {
			t.Fatalf("KillArgv(%+v) err = %v, want ErrInvalidRequest", req, err)
		}
	}

	got, err := KillArgv(KillRequest{PID: 123, CmdlineContains: "codex", Reason: "no syscalls for threshold"})
	if err != nil {
		t.Fatalf("KillArgv: %v", err)
	}
	want := []string{"pt", "kill", "123", "--cmdline-contains", "codex", "--reason", "no syscalls for threshold", "--json"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("argv = %#v, want %#v", got, want)
	}
	for _, part := range got {
		if part == "sh" || part == "-c" {
			t.Fatalf("argv used shell token: %#v", got)
		}
	}
}

func TestListStatusAndKillParseJSON(t *testing.T) {
	runner := &fakeRunner{responses: map[string]CommandResult{
		"pt status --json": {Stdout: []byte(`{"healthy":true,"version":"1.2.3"}`)},
		"pt list --json":   {Stdout: []byte(`[{"pid":42,"pgid":40,"cmd":"codex","cmdline":"codex run","age_s":600}]`)},
		"pt kill 42 --cmdline-contains codex --reason wedged --json": {Stdout: []byte(`{"terminated":true,"signal":"SIGTERM"}`)},
	}}
	adapter := New(runner)

	status, err := adapter.Status(context.Background())
	if err != nil {
		t.Fatalf("status: %v", err)
	}
	if !status.Healthy || status.Version != "1.2.3" {
		t.Fatalf("status = %+v", status)
	}

	processes, err := adapter.List(context.Background())
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(processes) != 1 || processes[0].PID != 42 || processes[0].Cmdline != "codex run" {
		t.Fatalf("processes = %+v", processes)
	}

	killed, err := adapter.Kill(context.Background(), KillRequest{PID: 42, CmdlineContains: "codex", Reason: "wedged"})
	if err != nil {
		t.Fatalf("kill: %v", err)
	}
	if !killed.Terminated || killed.Target != "42" {
		t.Fatalf("kill result = %+v", killed)
	}
}

func TestProbeReportsCapabilitiesForRegistry(t *testing.T) {
	adapter := New(&fakeRunner{responses: map[string]CommandResult{
		"pt status --json": {Stdout: []byte(`{"healthy":true,"version":"1.2.3"}`)},
	}})
	adapter.Now = fixedNow

	registry := capabilities.New("test-api")
	if err := registry.RegisterProbe(capabilities.ToolPT, func() (*capabilities.ToolReport, error) {
		return adapter.Probe(context.Background())
	}); err != nil {
		t.Fatal(err)
	}
	registry.Probe()

	if got, ok := registry.LookupCapability(capabilities.ToolPT, CapabilityList); !ok || got.Status != capabilities.StatusOK {
		t.Fatalf("pt.list = %+v, %v", got, ok)
	}
	if got, ok := registry.LookupCapability(capabilities.ToolPT, CapabilityKill); !ok || got.Status != capabilities.StatusBlockedByPolicy {
		t.Fatalf("pt.kill = %+v, %v", got, ok)
	}
}

func TestProbeReportsMissingAndMalformedStates(t *testing.T) {
	missing := New(&fakeRunner{err: errors.New("exec: \"pt\": executable file not found")})
	missing.Now = fixedNow
	report, err := missing.Probe(context.Background())
	if err != nil {
		t.Fatalf("probe missing: %v", err)
	}
	if report.Capabilities[CapabilityKill].Status != capabilities.StatusMissing {
		t.Fatalf("missing pt.kill = %+v", report.Capabilities[CapabilityKill])
	}

	malformed := New(&fakeRunner{responses: map[string]CommandResult{
		"pt status --json": {Stdout: []byte(`{"healthy": tru`)},
	}})
	malformed.Now = fixedNow
	report, err = malformed.Probe(context.Background())
	if err != nil {
		t.Fatalf("probe malformed: %v", err)
	}
	if report.Capabilities[CapabilityStatusRead].Status != capabilities.StatusDegraded {
		t.Fatalf("malformed pt.status.read = %+v", report.Capabilities[CapabilityStatusRead])
	}
}

func TestKillWedgedProcessIntentMatchesActionPlanKind(t *testing.T) {
	intent, err := KillWedgedProcessIntent("agent_1", KillRequest{
		PID:             99,
		CmdlineContains: "codex",
		Reason:          "no output and no syscalls past deterministic threshold",
	}, 5)
	if err != nil {
		t.Fatalf("intent: %v", err)
	}
	if intent.Kind != ActionKillWedgedProcess || intent.CapabilityID != CapabilityKill {
		t.Fatalf("intent = %+v", intent)
	}
	if intent.Target["agentId"] != "agent_1" || intent.Args["ptTarget"] != "99" {
		t.Fatalf("intent target/args = %+v / %+v", intent.Target, intent.Args)
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
