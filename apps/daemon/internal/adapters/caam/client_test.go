// client_test.go — exercises the caam adapter against a fake executor.
//
// The real binary is invoked at probe time (caam is installed during
// hp-ier authoring). The fake-executor tests cover the interesting
// classification paths without depending on the operator's caam state.
package caam

import (
	"context"
	"errors"
	"os/exec"
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

func TestListEmpty(t *testing.T) {
	t.Parallel()
	fake := newFakeExecutor()
	fake.Stdouts["ls --json"] = []byte(`{"profiles": [], "count": 0}`)
	c := NewWithExecutor(fake)
	resp, err := c.List(context.Background(), "")
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if resp.Count != 0 || len(resp.Profiles) != 0 {
		t.Fatalf("expected empty, got %+v", resp)
	}
}

func TestListWithProfiles(t *testing.T) {
	t.Parallel()
	fake := newFakeExecutor()
	fake.Stdouts["ls --json claude"] = []byte(`{
		"profiles": [
			{"tool":"claude","name":"work","is_active":true,"is_favorite":true,"health_status":"healthy"},
			{"tool":"claude","name":"personal","health_status":"expired"}
		],
		"count": 2
	}`)
	c := NewWithExecutor(fake)
	resp, err := c.List(context.Background(), ToolClaude)
	if err != nil {
		t.Fatalf("List(claude): %v", err)
	}
	if resp.Count != 2 || len(resp.Profiles) != 2 {
		t.Fatalf("expected 2 profiles, got %+v", resp)
	}
	if !resp.Profiles[0].IsActive || !resp.Profiles[0].IsFavorite {
		t.Fatalf("expected first profile active+favorite: %+v", resp.Profiles[0])
	}
}

func TestStatusJSON(t *testing.T) {
	t.Parallel()
	fake := newFakeExecutor()
	fake.Stdouts["status --json"] = []byte(`{
		"tools": [
			{"tool":"codex","logged_in":true},
			{"tool":"claude","logged_in":true,"active_profile":"work","health":"healthy"},
			{"tool":"gemini","logged_in":false}
		]
	}`)
	c := NewWithExecutor(fake)
	resp, err := c.Status(context.Background(), "")
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	if len(resp.Tools) != 3 {
		t.Fatalf("expected 3 tools, got %d", len(resp.Tools))
	}
	for _, tool := range resp.Tools {
		if tool.Tool == ToolGemini && tool.LoggedIn {
			t.Fatalf("gemini should be logged out")
		}
		if tool.Tool == ToolClaude && tool.ActiveProfile != "work" {
			t.Fatalf("claude active_profile: %q", tool.ActiveProfile)
		}
	}
}

func TestLimitsArrayFormNormalized(t *testing.T) {
	t.Parallel()
	// CAAM returns a bare array `[]` when the result set is empty.
	fake := newFakeExecutor()
	fake.Stdouts["limits --format json"] = []byte(`[]`)
	c := NewWithExecutor(fake)
	resp, err := c.Limits(context.Background(), "")
	if err != nil {
		t.Fatalf("Limits empty: %v", err)
	}
	if len(resp.Limits) != 0 {
		t.Fatalf("expected empty limits, got %d", len(resp.Limits))
	}
}

func TestLimitsArrayFormPopulated(t *testing.T) {
	t.Parallel()
	fake := newFakeExecutor()
	fake.Stdouts["limits --format json"] = []byte(`[
		{"provider":"claude","profile":"work","used_pct":0.42,"burn_rate":"$1.20/hr"},
		{"provider":"codex","profile":"main","used_pct":0.78}
	]`)
	c := NewWithExecutor(fake)
	resp, err := c.Limits(context.Background(), "")
	if err != nil {
		t.Fatalf("Limits: %v", err)
	}
	if len(resp.Limits) != 2 {
		t.Fatalf("expected 2 limits, got %d", len(resp.Limits))
	}
	if resp.Limits[0].UsedPct != 0.42 {
		t.Fatalf("limit 0 used_pct: %v", resp.Limits[0].UsedPct)
	}
}

func TestActivateRequiresArgs(t *testing.T) {
	t.Parallel()
	c := NewWithExecutor(newFakeExecutor())
	if _, err := c.Activate(context.Background(), "", "x"); err == nil {
		t.Fatalf("expected error on empty tool")
	}
	if _, err := c.Activate(context.Background(), ToolClaude, ""); err == nil {
		t.Fatalf("expected error on empty profile")
	}
}

func TestActivateClassifiesProfileNotFound(t *testing.T) {
	t.Parallel()
	fake := newFakeExecutor()
	fake.Exits["activate claude missing"] = 1
	fake.Stderrs["activate claude missing"] = []byte("error: profile not found: missing")
	c := NewWithExecutor(fake)
	_, err := c.Activate(context.Background(), ToolClaude, "missing")
	if !errors.Is(err, ErrProfileNotFound) {
		t.Fatalf("expected ErrProfileNotFound, got %v", err)
	}
}

func TestActivateClassifiesToolNotInstalled(t *testing.T) {
	t.Parallel()
	fake := newFakeExecutor()
	fake.Exits["activate gemini work"] = 2
	fake.Stderrs["activate gemini work"] = []byte("error: gemini binary not found in PATH")
	c := NewWithExecutor(fake)
	_, err := c.Activate(context.Background(), ToolGemini, "work")
	if !errors.Is(err, ErrToolNotInstalled) {
		t.Fatalf("expected ErrToolNotInstalled, got %v", err)
	}
}

func TestActivateOK(t *testing.T) {
	t.Parallel()
	fake := newFakeExecutor()
	fake.Stdouts["activate claude work"] = []byte("Activated claude/work")
	c := NewWithExecutor(fake)
	c.Now = func() time.Time { return time.Date(2026, 5, 4, 0, 0, 0, 0, time.UTC) }

	res, err := c.Activate(context.Background(), ToolClaude, "work")
	if err != nil {
		t.Fatalf("Activate: %v", err)
	}
	if !res.OK {
		t.Fatalf("expected OK")
	}
	if res.ActivatedAt.IsZero() {
		t.Fatalf("expected non-zero ActivatedAt")
	}
	if res.Notes != "Activated claude/work" {
		t.Fatalf("notes: %q", res.Notes)
	}
}

func TestRunClassifiesNonZeroExit(t *testing.T) {
	t.Parallel()
	fake := newFakeExecutor()
	fake.Exits["ls --json"] = 1
	fake.Stderrs["ls --json"] = []byte("vault is locked")
	c := NewWithExecutor(fake)
	_, err := c.List(context.Background(), "")
	if err == nil {
		t.Fatalf("expected error on non-zero exit")
	}
	if !strings.Contains(err.Error(), "exited 1") {
		t.Fatalf("error should include exit code, got %v", err)
	}
}

// Integration: real caam binary if present.

func TestProbeAgainstRealBinary(t *testing.T) {
	t.Parallel()
	if _, err := exec.LookPath("caam"); err != nil {
		t.Skip("caam not on PATH")
	}
	c := New()
	res := Probe(context.Background(), c, func() time.Time { return time.Date(2026, 5, 4, 0, 0, 0, 0, time.UTC) })
	if res.Tool != "caam" {
		t.Fatalf("expected tool caam, got %q", res.Tool)
	}
	if res.Reports[CapAccountSwitch].Status != StatusUntested {
		t.Fatalf("expected switch untested, got %q", res.Reports[CapAccountSwitch].Status)
	}
	// status / list / detect should be ok against any real install.
	for _, id := range []string{CapAccountStatus, CapAccountsList, CapAgentsDetect} {
		report, ok := res.Reports[id]
		if !ok {
			t.Fatalf("missing report for %s", id)
		}
		if report.Status != StatusOK {
			t.Fatalf("expected %s ok against real caam, got %q (notes: %s)",
				id, report.Status, report.Notes)
		}
	}
}
