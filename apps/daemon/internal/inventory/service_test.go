package inventory

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/hoopoe-cockpit/hoopoe/apps/daemon/internal/adapters/caam"
	"github.com/hoopoe-cockpit/hoopoe/apps/daemon/internal/capabilities"
)

func TestSnapshotListsCapabilityRegistryToolsAndCAAMSubscriptions(t *testing.T) {
	now := time.Date(2026, 5, 4, 4, 0, 0, 0, time.UTC)
	cooldown := now.Add(time.Hour)
	registry := testRegistry(t, now)
	client := &fakeCAAM{
		list: &caam.ListResponse{Profiles: []caam.Profile{
			{Tool: caam.ToolClaude, Name: "claude-primary", IsActive: true, HealthStatus: "healthy"},
			{Tool: caam.ToolClaude, Name: "claude-cooling", CooldownUntil: &cooldown, HealthStatus: "rate-limited"},
			{Tool: caam.ToolCodex, Name: "codex-pro", IsFavorite: true, HealthStatus: "healthy"},
			{Tool: caam.ToolCursor, Name: "cursor-ignored", HealthStatus: "healthy"},
		}},
		status: &caam.StatusResponse{Tools: []caam.ToolStatus{
			{Tool: caam.ToolClaude, LoggedIn: true, ActiveProfile: "claude-primary", Health: "healthy"},
			{Tool: caam.ToolCodex, LoggedIn: false},
			{Tool: caam.ToolGemini, LoggedIn: false},
		}},
		limits: &caam.LimitsResponse{Limits: []caam.Limit{
			{Provider: "anthropic", Profile: "claude-primary", UsedPct: 12.5},
			{Provider: "openai", Profile: "codex-pro", UsedPct: 22},
		}},
		detect: &caam.DetectResponse{Agents: []caam.AgentInventoryEntry{
			{Name: "claude", DisplayName: "Claude Code", Installed: true, Version: "1.2.3", BinaryPath: "/usr/local/bin/claude"},
		}},
	}
	service := NewService(Config{
		Registry: registry,
		CAAM:     client,
		Now:      func() time.Time { return now },
	})

	snapshot, err := service.Snapshot(context.Background())
	if err != nil {
		t.Fatalf("Snapshot returned error: %v", err)
	}

	if snapshot.SchemaVersion != SchemaVersion || snapshot.SnapshotAt != "2026-05-04T04:00:00Z" {
		t.Fatalf("unexpected snapshot envelope: %+v", snapshot)
	}
	if len(snapshot.Tools) != 2 {
		t.Fatalf("tools length = %d, want 2", len(snapshot.Tools))
	}
	if snapshot.Tools[0].ID != capabilities.ToolBR || snapshot.Tools[0].Status != capabilities.StatusDegraded {
		t.Fatalf("first tool = %+v, want br/degraded", snapshot.Tools[0])
	}
	if snapshot.Tools[1].ID != capabilities.ToolGit || snapshot.Tools[1].Status != capabilities.StatusBlockedByPolicy {
		t.Fatalf("second tool = %+v, want git/blocked-by-policy", snapshot.Tools[1])
	}
	if got := snapshot.Tools[1].CapabilityIDs; len(got) != 2 || got[0] != "git.push" || got[1] != "git.status.read" {
		t.Fatalf("git capability ids = %#v", got)
	}

	subscriptions := snapshot.SubscriptionVerification
	if subscriptions.Status != VerificationOK {
		t.Fatalf("subscription status = %s, want ok; warnings=%+v", subscriptions.Status, subscriptions.Warnings)
	}
	if subscriptions.TotalAccountCount != 3 || subscriptions.TotalAvailableAccounts != 2 || subscriptions.SignedInCount != 1 {
		t.Fatalf("subscription counts = %+v", subscriptions)
	}
	claude := subscriptions.RequiredTools[0]
	if claude.Tool != "claude" || !claude.SignedIn || claude.AvailableAccounts != 1 || claude.DetectedAgent == nil {
		t.Fatalf("claude subscription = %+v", claude)
	}
	if len(claude.Limits) != 1 || claude.Limits[0].Provider != "anthropic" {
		t.Fatalf("claude limits = %+v", claude.Limits)
	}
}

func TestSnapshotWarnsWhenNoSubscriptionsAreConfigured(t *testing.T) {
	now := time.Date(2026, 5, 4, 4, 30, 0, 0, time.UTC)
	service := NewService(Config{
		Registry: capabilities.New("v1"),
		CAAM: &fakeCAAM{
			list:   &caam.ListResponse{},
			status: &caam.StatusResponse{},
			limits: &caam.LimitsResponse{},
			detect: &caam.DetectResponse{},
		},
		Now: func() time.Time { return now },
	})

	snapshot, err := service.Snapshot(context.Background())
	if err != nil {
		t.Fatalf("Snapshot returned error: %v", err)
	}

	verification := snapshot.SubscriptionVerification
	if verification.Status != VerificationWarning {
		t.Fatalf("status = %s, want warning", verification.Status)
	}
	if !verification.ZeroSubscriptionWarning {
		t.Fatal("zero subscription warning was not set")
	}
	if len(verification.RequiredTools) != 3 {
		t.Fatalf("required tools length = %d, want 3", len(verification.RequiredTools))
	}
	if len(snapshot.Warnings) == 0 || snapshot.Warnings[len(snapshot.Warnings)-1].Code != "caam.zero_subscriptions" {
		t.Fatalf("warnings = %+v, want zero subscription warning", snapshot.Warnings)
	}
}

func TestPhase3InventoryCoversCanonicalToolSetAndSubscriptionTriad(t *testing.T) {
	now := time.Date(2026, 5, 4, 4, 45, 0, 0, time.UTC)
	registry := capabilities.New("v1")
	registry.SetClock(func() time.Time { return now })
	expectedTools := []capabilities.ToolID{
		capabilities.ToolNTM,
		capabilities.ToolBR,
		capabilities.ToolBV,
		capabilities.ToolAgentMail,
		capabilities.ToolCAAM,
		capabilities.ToolCAUT,
		capabilities.ToolDCG,
		capabilities.ToolCASR,
		capabilities.ToolPT,
		capabilities.ToolSRP,
		capabilities.ToolSBH,
		capabilities.ToolUBS,
		capabilities.ToolJSM,
		capabilities.ToolJFP,
		capabilities.ToolRU,
		capabilities.ToolOracle,
	}
	for _, tool := range expectedTools {
		if err := registry.SetReport(&capabilities.ToolReport{
			Tool:          tool,
			Version:       "1.0.0",
			Source:        "fixture",
			LastCheckedAt: now.Format(time.RFC3339),
			Capabilities: map[string]capabilities.Capability{
				string(tool) + ".probe": {Status: capabilities.StatusOK},
			},
		}); err != nil {
			t.Fatalf("set %s report: %v", tool, err)
		}
	}
	client := &fakeCAAM{
		list: &caam.ListResponse{Profiles: []caam.Profile{
			{Tool: caam.ToolClaude, Name: "claude-max", IsActive: true, HealthStatus: "healthy"},
			{Tool: caam.ToolCodex, Name: "codex-pro", IsFavorite: true, HealthStatus: "healthy"},
			{Tool: caam.ToolGemini, Name: "gemini-ultra", HealthStatus: "healthy"},
		}},
		status: &caam.StatusResponse{Tools: []caam.ToolStatus{
			{Tool: caam.ToolClaude, LoggedIn: true, ActiveProfile: "claude-max", Health: "healthy"},
			{Tool: caam.ToolCodex, LoggedIn: true, ActiveProfile: "codex-pro", Health: "healthy"},
			{Tool: caam.ToolGemini, LoggedIn: true, ActiveProfile: "gemini-ultra", Health: "healthy"},
		}},
		limits: &caam.LimitsResponse{Limits: []caam.Limit{
			{Provider: "anthropic", Profile: "claude-max", UsedPct: 10},
			{Provider: "openai", Profile: "codex-pro", UsedPct: 20},
			{Provider: "google", Profile: "gemini-ultra", UsedPct: 30},
		}},
		detect: &caam.DetectResponse{Agents: []caam.AgentInventoryEntry{
			{Name: "claude", DisplayName: "Claude Code", Installed: true, Version: "1.0.0", BinaryPath: "/usr/local/bin/claude"},
			{Name: "codex", DisplayName: "Codex CLI", Installed: true, Version: "1.0.0", BinaryPath: "/usr/local/bin/codex"},
			{Name: "gemini", DisplayName: "Gemini CLI", Installed: true, Version: "1.0.0", BinaryPath: "/usr/local/bin/gemini"},
		}},
	}
	service := NewService(Config{
		Registry: registry,
		CAAM:     client,
		Now:      func() time.Time { return now },
	})

	snapshot, err := service.Snapshot(context.Background())
	if err != nil {
		t.Fatalf("Snapshot returned error: %v", err)
	}

	seen := map[capabilities.ToolID]bool{}
	for _, tool := range snapshot.Tools {
		seen[tool.ID] = true
	}
	for _, tool := range expectedTools {
		if !seen[tool] {
			t.Fatalf("snapshot missing canonical Phase 3 tool %s; tools=%+v", tool, snapshot.Tools)
		}
	}
	verification := snapshot.SubscriptionVerification
	if verification.Status != VerificationOK || verification.SignedInCount != 3 ||
		verification.TotalAccountCount != 3 || verification.TotalAvailableAccounts != 3 {
		t.Fatalf("subscription verification = %+v", verification)
	}
	wantSubscriptions := map[string]string{
		"claude": "Claude Max",
		"codex":  "GPT Pro",
		"gemini": "Gemini Ultra",
	}
	for _, tool := range verification.RequiredTools {
		if wantSubscriptions[tool.Tool] != tool.ExpectedSubscription {
			t.Fatalf("%s expected subscription = %q, want %q", tool.Tool, tool.ExpectedSubscription, wantSubscriptions[tool.Tool])
		}
		if !tool.SignedIn || tool.DetectedAgent == nil || len(tool.Limits) != 1 {
			t.Fatalf("%s subscription details = %+v", tool.Tool, tool)
		}
	}
}

func TestSnapshotDegradesWhenCAAMFails(t *testing.T) {
	service := NewService(Config{
		Registry: capabilities.New("v1"),
		CAAM: &fakeCAAM{
			listErr:   caam.ErrMissingBinary,
			statusErr: errors.New("status broke"),
			limitsErr: errors.New("limits broke"),
			detectErr: errors.New("detect broke"),
		},
		Now: func() time.Time { return time.Date(2026, 5, 4, 5, 0, 0, 0, time.UTC) },
	})

	snapshot, err := service.Snapshot(context.Background())
	if err != nil {
		t.Fatalf("Snapshot returned error: %v", err)
	}

	if snapshot.SubscriptionVerification.Status != VerificationUnavailable {
		t.Fatalf("status = %s, want unavailable", snapshot.SubscriptionVerification.Status)
	}
	if len(snapshot.SubscriptionVerification.Warnings) < 4 {
		t.Fatalf("warnings = %+v, want all CAAM failures surfaced", snapshot.SubscriptionVerification.Warnings)
	}
}

func TestRefreshRunsProbeAndThrottlesConcurrentRefreshes(t *testing.T) {
	now := time.Date(2026, 5, 4, 5, 30, 0, 0, time.UTC)
	registry := capabilities.New("v1")
	registry.SetClock(func() time.Time { return now })
	started := make(chan struct{})
	release := make(chan struct{})
	var calls atomic.Int64
	if err := registry.RegisterProbe(capabilities.ToolBR, func() (*capabilities.ToolReport, error) {
		calls.Add(1)
		close(started)
		<-release
		return okReport(capabilities.ToolBR, "br.issues.read", now), nil
	}); err != nil {
		t.Fatalf("register probe: %v", err)
	}
	service := NewService(Config{
		Registry: registry,
		CAAM:     &fakeCAAM{list: &caam.ListResponse{}, status: &caam.StatusResponse{}, limits: &caam.LimitsResponse{}, detect: &caam.DetectResponse{}},
		Now:      func() time.Time { return now },
	})

	done := make(chan error, 1)
	go func() {
		_, err := service.Refresh(context.Background())
		done <- err
	}()
	<-started

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()
	if _, err := service.Refresh(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("second Refresh error = %v, want deadline exceeded", err)
	}
	close(release)
	if err := <-done; err != nil {
		t.Fatalf("first Refresh returned error: %v", err)
	}
	if calls.Load() != 1 {
		t.Fatalf("probe calls = %d, want 1", calls.Load())
	}
}

func testRegistry(t *testing.T, now time.Time) *capabilities.Registry {
	t.Helper()
	registry := capabilities.New("v1")
	registry.SetClock(func() time.Time { return now })
	if err := registry.SetReport(&capabilities.ToolReport{
		Tool:          capabilities.ToolGit,
		Version:       "2.45.0",
		Source:        "CLI",
		LastCheckedAt: now.Format(time.RFC3339),
		Capabilities: map[string]capabilities.Capability{
			"git.status.read": {Status: capabilities.StatusOK},
			"git.push":        {Status: capabilities.StatusBlockedByPolicy, Notes: "daemon mediated only"},
		},
	}); err != nil {
		t.Fatalf("set git report: %v", err)
	}
	if err := registry.SetReport(&capabilities.ToolReport{
		Tool:          capabilities.ToolBR,
		Version:       "0.9.0",
		Source:        "CLI",
		LastCheckedAt: now.Format(time.RFC3339),
		Capabilities: map[string]capabilities.Capability{
			"br.issues.read":  {Status: capabilities.StatusOK},
			"br.issues.write": {Status: capabilities.StatusDegraded, Notes: "readonly fixture"},
		},
	}); err != nil {
		t.Fatalf("set br report: %v", err)
	}
	return registry
}

func okReport(tool capabilities.ToolID, capID string, now time.Time) *capabilities.ToolReport {
	return &capabilities.ToolReport{
		Tool:          tool,
		Version:       "1.0.0",
		Source:        "CLI",
		LastCheckedAt: now.Format(time.RFC3339),
		Capabilities: map[string]capabilities.Capability{
			capID: {Status: capabilities.StatusOK},
		},
	}
}

type fakeCAAM struct {
	list   *caam.ListResponse
	status *caam.StatusResponse
	limits *caam.LimitsResponse
	detect *caam.DetectResponse

	listErr   error
	statusErr error
	limitsErr error
	detectErr error
}

func (f *fakeCAAM) List(context.Context, caam.Tool) (*caam.ListResponse, error) {
	return f.list, f.listErr
}

func (f *fakeCAAM) Status(context.Context, caam.Tool) (*caam.StatusResponse, error) {
	return f.status, f.statusErr
}

func (f *fakeCAAM) Limits(context.Context, caam.Tool) (*caam.LimitsResponse, error) {
	return f.limits, f.limitsErr
}

func (f *fakeCAAM) Detect(context.Context) (*caam.DetectResponse, error) {
	return f.detect, f.detectErr
}
