// client_test.go — exercises the caut adapter against a fake executor.
//
// caut is NOT installed locally during hp-ier authoring; integration
// validation runs against the research-spike VPS. These tests cover
// the parser + classification paths without depending on the binary.
package caut

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

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

func (f *fakeExecutor) Run(_ context.Context, args []string) ([]byte, []byte, int, error) {
	key := strings.Join(args, " ")
	f.Calls = append(f.Calls, key)
	if err := f.Errors[key]; err != nil {
		return nil, f.Stderrs[key], f.Exits[key], err
	}
	return f.Stdouts[key], f.Stderrs[key], f.Exits[key], nil
}

func TestSnapshotParsesMixedStatuses(t *testing.T) {
	t.Parallel()
	fake := newFakeExecutor()
	fake.Stdouts["snapshot --json"] = []byte(`{
		"snapshot": {
			"generated_at": "2026-05-04T00:00:00Z",
			"caut_version": "1.2.3",
			"hostname": "hoopoe-vps-1"
		},
		"providers": [
			{"provider":"claude_max","status":"measured","used_pct":0.42,"window_resets_at":"2026-05-04T05:00:00Z","burn_rate":"$1.20/hr","burn_rate_usd_per_hour":1.2},
			{"provider":"gpt_pro","status":"measured","used_pct":0.78,"window_resets_at":"2026-05-04T08:00:00Z"},
			{"provider":"gemini_ultra","status":"unmeasured","unmeasured_reason":"provider API timed out"}
		]
	}`)
	c := NewWithExecutor(fake)
	resp, err := c.Snapshot(context.Background())
	if err != nil {
		t.Fatalf("Snapshot: %v", err)
	}
	if len(resp.Providers) != 3 {
		t.Fatalf("expected 3 providers, got %d", len(resp.Providers))
	}
	if resp.Providers[2].Status != StatusUnmeasured {
		t.Fatalf("expected gemini unmeasured, got %q", resp.Providers[2].Status)
	}
	if resp.Providers[0].UsedPct != 0.42 {
		t.Fatalf("claude used_pct: %v", resp.Providers[0].UsedPct)
	}
}

func TestSnapshotForProviderReturnsUnmeasuredWhenAbsent(t *testing.T) {
	t.Parallel()
	fake := newFakeExecutor()
	fake.Stdouts["snapshot --json"] = []byte(`{
		"snapshot": {"generated_at": "2026-05-04T00:00:00Z"},
		"providers": [
			{"provider":"claude_max","status":"measured","used_pct":0.5}
		]
	}`)
	c := NewWithExecutor(fake)
	snap, err := c.SnapshotForProvider(context.Background(), ProviderGemini)
	if err != nil {
		t.Fatalf("SnapshotForProvider: %v", err)
	}
	if snap.Status != StatusUnmeasured {
		t.Fatalf("expected unmeasured for absent provider, got %q", snap.Status)
	}
	if snap.UnmeasuredReason == "" {
		t.Fatalf("expected non-empty UnmeasuredReason")
	}
}

func TestSnapshotForProviderReturnsMeasuredWhenPresent(t *testing.T) {
	t.Parallel()
	fake := newFakeExecutor()
	fake.Stdouts["snapshot --json"] = []byte(`{
		"snapshot": {"generated_at": "2026-05-04T00:00:00Z"},
		"providers": [
			{"provider":"claude_max","status":"measured","used_pct":0.5}
		]
	}`)
	c := NewWithExecutor(fake)
	snap, err := c.SnapshotForProvider(context.Background(), ProviderClaude)
	if err != nil {
		t.Fatalf("SnapshotForProvider: %v", err)
	}
	if snap.Status != StatusMeasured {
		t.Fatalf("expected measured, got %q", snap.Status)
	}
	if snap.UsedPct != 0.5 {
		t.Fatalf("expected used_pct 0.5, got %v", snap.UsedPct)
	}
}

func TestSnapshotForProviderRequiresProvider(t *testing.T) {
	t.Parallel()
	c := NewWithExecutor(newFakeExecutor())
	if _, err := c.SnapshotForProvider(context.Background(), ""); err == nil {
		t.Fatalf("expected error on empty provider")
	}
}

func TestSnapshotMissingBinaryClassified(t *testing.T) {
	t.Parallel()
	fake := newFakeExecutor()
	fake.Errors["snapshot --json"] = ErrMissingBinary
	c := NewWithExecutor(fake)
	_, err := c.Snapshot(context.Background())
	if !errors.Is(err, ErrMissingBinary) {
		t.Fatalf("expected ErrMissingBinary, got %v", err)
	}
}

func TestSnapshotNonZeroExitClassified(t *testing.T) {
	t.Parallel()
	fake := newFakeExecutor()
	fake.Exits["snapshot --json"] = 1
	fake.Stderrs["snapshot --json"] = []byte("caut: failed to query provider")
	c := NewWithExecutor(fake)
	_, err := c.Snapshot(context.Background())
	if err == nil {
		t.Fatalf("expected error on non-zero exit")
	}
	if !strings.Contains(err.Error(), "exited 1") {
		t.Fatalf("error should include exit code, got %v", err)
	}
}

func TestProbeReportsMissingWhenBinaryAbsent(t *testing.T) {
	t.Parallel()
	fake := newFakeExecutor()
	fake.Errors["snapshot --json"] = ErrMissingBinary
	c := NewWithExecutor(fake)
	res := Probe(context.Background(), c, func() time.Time { return time.Date(2026, 5, 4, 0, 0, 0, 0, time.UTC) })
	report := res.Reports[CapUsageSnapshot]
	if report.Status != StatusMissing {
		t.Fatalf("expected missing, got %q", report.Status)
	}
	if !strings.Contains(report.Notes, "unmeasured") {
		t.Fatalf("notes should mention §7.6 unmeasured fallback, got %q", report.Notes)
	}
}

func TestProbeReportsOkWhenSnapshotSucceeds(t *testing.T) {
	t.Parallel()
	fake := newFakeExecutor()
	fake.Stdouts["snapshot --json"] = []byte(`{
		"snapshot": {"generated_at": "2026-05-04T00:00:00Z"},
		"providers": []
	}`)
	c := NewWithExecutor(fake)
	res := Probe(context.Background(), c, nil)
	if res.Reports[CapUsageSnapshot].Status != StatusOK {
		t.Fatalf("expected ok, got %q", res.Reports[CapUsageSnapshot].Status)
	}
}

func TestIndexByProvider(t *testing.T) {
	t.Parallel()
	resp := &SnapshotResponse{
		Providers: []ProviderSnapshot{
			{Provider: ProviderClaude, Status: StatusMeasured},
			{Provider: ProviderGemini, Status: StatusUnmeasured},
		},
	}
	idx := resp.IndexByProvider()
	if idx[ProviderClaude] != StatusMeasured {
		t.Fatalf("expected claude measured, got %q", idx[ProviderClaude])
	}
	if idx[ProviderGemini] != StatusUnmeasured {
		t.Fatalf("expected gemini unmeasured, got %q", idx[ProviderGemini])
	}
	if _, ok := idx[ProviderCodex]; ok {
		t.Fatalf("expected codex absent")
	}
}
