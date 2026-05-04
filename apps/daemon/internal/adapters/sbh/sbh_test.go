package sbh

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/hoopoe-cockpit/hoopoe/apps/daemon/internal/capabilities"
)

func TestCleanupArgvRequiresExplicitSafeCategoriesForApply(t *testing.T) {
	for _, req := range []CleanupRequest{
		{},
		{Categories: []string{"all"}},
		{Categories: []string{"--all"}},
		{Categories: []string{"health worktrees"}},
	} {
		if _, err := CleanupApplyArgv(req); !errors.Is(err, ErrInvalidRequest) {
			t.Fatalf("CleanupApplyArgv(%+v) err = %v, want ErrInvalidRequest", req, err)
		}
	}

	got, err := CleanupApplyArgv(CleanupRequest{
		Categories: []string{"tmp", "health-worktrees", "tmp"},
		Reason:     "disk pressure",
	})
	if err != nil {
		t.Fatalf("CleanupApplyArgv: %v", err)
	}
	want := []string{"sbh", "cleanup", "--apply", "--json", "--category", "health-worktrees", "--category", "tmp", "--reason", "disk pressure"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("argv = %#v, want %#v", got, want)
	}
}

func TestStatusDryRunAndApplyParseJSON(t *testing.T) {
	runner := &fakeRunner{responses: map[string]CommandResult{
		"sbh status --json":                                                {Stdout: []byte(`{"cleanable_bytes":2048,"by_category":{"tmp":{"bytes":1024,"files":2}}}`)},
		"sbh cleanup --dry-run --json --category tmp":                      {Stdout: []byte(`{"freed_bytes":1024,"by_category":{"tmp":1024}}`)},
		"sbh cleanup --apply --json --category tmp --reason disk pressure": {Stdout: []byte(`{"freed_bytes":1024,"by_category":{"tmp":1024},"post_status_ref":"audit_evt_1"}`)},
	}}
	adapter := New(runner)

	status, err := adapter.Status(context.Background())
	if err != nil {
		t.Fatalf("status: %v", err)
	}
	if status.CleanableBytes != 2048 || status.ByCategory["tmp"].Files != 2 {
		t.Fatalf("status = %+v", status)
	}
	dryRun, err := adapter.DryRun(context.Background(), CleanupRequest{Categories: []string{"tmp"}})
	if err != nil {
		t.Fatalf("dry run: %v", err)
	}
	if dryRun.FreedBytes != 1024 {
		t.Fatalf("dryRun = %+v", dryRun)
	}
	applied, err := adapter.Apply(context.Background(), CleanupRequest{Categories: []string{"tmp"}, Reason: "disk pressure"})
	if err != nil {
		t.Fatalf("apply: %v", err)
	}
	if !applied.Applied || applied.PostStatusRef != "audit_evt_1" {
		t.Fatalf("applied = %+v", applied)
	}
}

func TestProbeReportsRegistryCapabilities(t *testing.T) {
	adapter := New(&fakeRunner{responses: map[string]CommandResult{
		"sbh status --json": {Stdout: []byte(`{"cleanable_bytes":0}`)},
	}})
	adapter.Now = fixedNow
	registry := capabilities.New("test-api")
	if err := registry.RegisterProbe(capabilities.ToolSBH, func() (*capabilities.ToolReport, error) {
		return adapter.Probe(context.Background())
	}); err != nil {
		t.Fatal(err)
	}
	registry.Probe()
	if got, ok := registry.LookupCapability(capabilities.ToolSBH, CapabilityCleanup); !ok || got.Status != capabilities.StatusBlockedByPolicy {
		t.Fatalf("sbh.cleanup = %+v, %v", got, ok)
	}
	if got, ok := registry.LookupCapability(capabilities.ToolSBH, CapabilityCleanupDryRun); !ok || got.Status != capabilities.StatusOK {
		t.Fatalf("sbh.cleanup.dry_run = %+v, %v", got, ok)
	}
}

func TestProbeReportsMalformedAndTimeoutAsDegraded(t *testing.T) {
	malformed := New(&fakeRunner{responses: map[string]CommandResult{
		"sbh status --json": {Stdout: []byte(`{"cleanable_bytes":`)},
	}})
	malformed.Now = fixedNow
	report, err := malformed.Probe(context.Background())
	if err != nil {
		t.Fatalf("malformed probe: %v", err)
	}
	if report.Capabilities[CapabilityStatusRead].Status != capabilities.StatusDegraded {
		t.Fatalf("malformed status = %+v", report.Capabilities[CapabilityStatusRead])
	}

	timeout := New(&fakeRunner{responses: map[string]CommandResult{
		"sbh status --json": {ExitCode: 124, Stderr: []byte("timeout")},
	}})
	timeout.Now = fixedNow
	report, err = timeout.Probe(context.Background())
	if err != nil {
		t.Fatalf("timeout probe: %v", err)
	}
	if report.Capabilities[CapabilityCleanup].Status != capabilities.StatusDegraded {
		t.Fatalf("timeout cleanup = %+v", report.Capabilities[CapabilityCleanup])
	}
}

func TestCleanupApplyIntentIsDeterministicPreScriptSurface(t *testing.T) {
	intent, err := CleanupApplyIntent(CleanupRequest{
		Categories: []string{"tmp", "health-worktrees"},
		Reason:     "disk critical",
	})
	if err != nil {
		t.Fatalf("intent: %v", err)
	}
	if intent.CapabilityID != CapabilityCleanup || intent.Action != "sbh.cleanup" {
		t.Fatalf("intent = %+v", intent)
	}
	categories, ok := intent.Args["categories"].([]string)
	if !ok || !reflect.DeepEqual(categories, []string{"health-worktrees", "tmp"}) {
		t.Fatalf("intent categories = %#v", intent.Args["categories"])
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
