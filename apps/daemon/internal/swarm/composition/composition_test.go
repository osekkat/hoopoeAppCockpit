package composition

import (
	"errors"
	"testing"
	"time"

	"github.com/hoopoe-cockpit/hoopoe/apps/daemon/internal/adapters/caam"
	"github.com/hoopoe-cockpit/hoopoe/apps/daemon/internal/adapters/cli"
)

func TestDefaultAutoTargets(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name       string
		readyBeads int
		claude     int
		codex      int
		gemini     int
	}{
		{name: "small project", readyBeads: 99, claude: 1, codex: 1, gemini: 1},
		{name: "medium project lower bound", readyBeads: 100, claude: 3, codex: 3, gemini: 2},
		{name: "medium project upper bound", readyBeads: 399, claude: 3, codex: 3, gemini: 2},
		{name: "large project", readyBeads: 400, claude: 4, codex: 4, gemini: 2},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			targets := DefaultAutoTargets(tt.readyBeads)
			assertTarget(t, targets, cli.HarnessClaudeCode, tt.claude)
			assertTarget(t, targets, cli.HarnessCodexCLI, tt.codex)
			assertTarget(t, targets, cli.HarnessGeminiCLI, tt.gemini)
		})
	}
}

func TestAutoSelectionFallsBackToAvailableAccounts(t *testing.T) {
	t.Parallel()
	selection, err := Select(Request{
		Mode:       ModeAuto,
		ReadyBeads: 450,
		Inventory: []Account{
			account(caam.ToolClaude, "claude-a"),
			account(caam.ToolCodex, "codex-a"),
			account(caam.ToolCodex, "codex-b"),
		},
	})
	if err != nil {
		t.Fatalf("Select: %v", err)
	}
	if selection.Total != 3 {
		t.Fatalf("total = %d, want 3", selection.Total)
	}
	assertHarness(t, selection, cli.HarnessClaudeCode, 4, 1, false)
	assertHarness(t, selection, cli.HarnessCodexCLI, 4, 2, false)
	assertHarness(t, selection, cli.HarnessGeminiCLI, 2, 0, true)
	if !hasWarning(selection.Warnings, WarningNoSubscription, cli.HarnessGeminiCLI) {
		t.Fatalf("expected gemini no-subscription warning: %#v", selection.Warnings)
	}
}

func TestAutoSelectionScalesByCap(t *testing.T) {
	t.Parallel()
	selection, err := Select(Request{
		Mode:       ModeAuto,
		ReadyBeads: 450,
		MaxAgents:  5,
		Inventory: []Account{
			account(caam.ToolClaude, "claude-a"),
			account(caam.ToolClaude, "claude-b"),
			account(caam.ToolClaude, "claude-c"),
			account(caam.ToolClaude, "claude-d"),
			account(caam.ToolCodex, "codex-a"),
			account(caam.ToolCodex, "codex-b"),
			account(caam.ToolCodex, "codex-c"),
			account(caam.ToolCodex, "codex-d"),
			account(caam.ToolGemini, "gemini-a"),
			account(caam.ToolGemini, "gemini-b"),
		},
	})
	if err != nil {
		t.Fatalf("Select: %v", err)
	}
	if selection.Total != 5 {
		t.Fatalf("total = %d, want 5", selection.Total)
	}
	assertHarness(t, selection, cli.HarnessClaudeCode, 2, 2, false)
	assertHarness(t, selection, cli.HarnessCodexCLI, 2, 2, false)
	assertHarness(t, selection, cli.HarnessGeminiCLI, 1, 1, false)
	if !hasWarning(selection.Warnings, WarningAutoScaledByCap, "") {
		t.Fatalf("expected cap warning: %#v", selection.Warnings)
	}
}

func TestManualSelectionWarnsWhenAgentsShareAccounts(t *testing.T) {
	t.Parallel()
	selection, err := Select(Request{
		Mode: ModeManual,
		Manual: []ManualHarness{
			{Harness: cli.HarnessClaudeCode, Count: 4},
		},
		Inventory: []Account{
			account(caam.ToolClaude, "main"),
		},
	})
	if err != nil {
		t.Fatalf("Select: %v", err)
	}
	claude := findHarness(t, selection, cli.HarnessClaudeCode)
	if claude.SelectedCount != 4 {
		t.Fatalf("selected count = %d, want 4", claude.SelectedCount)
	}
	for _, selected := range claude.Accounts {
		if selected.Name != "main" {
			t.Fatalf("selected account = %q, want main", selected.Name)
		}
	}
	if !hasWarning(selection.Warnings, WarningAccountPressure, cli.HarnessClaudeCode) {
		t.Fatalf("expected account pressure warning: %#v", selection.Warnings)
	}
}

func TestManualSelectionRejectsOverCap(t *testing.T) {
	t.Parallel()
	_, err := Select(Request{
		Mode:      ModeManual,
		MaxAgents: 3,
		Manual: []ManualHarness{
			{Harness: cli.HarnessClaudeCode, Count: 2},
			{Harness: cli.HarnessCodexCLI, Count: 2},
		},
		Inventory: []Account{
			account(caam.ToolClaude, "claude-a"),
			account(caam.ToolClaude, "claude-b"),
			account(caam.ToolCodex, "codex-a"),
			account(caam.ToolCodex, "codex-b"),
		},
	})
	if !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("expected ErrInvalidRequest, got %v", err)
	}
}

func TestManualSelectionGreysOutMissingSubscription(t *testing.T) {
	t.Parallel()
	selection, err := Select(Request{
		Mode: ModeManual,
		Manual: []ManualHarness{
			{Harness: cli.HarnessGeminiCLI, Count: 1},
		},
	})
	if err != nil {
		t.Fatalf("Select: %v", err)
	}
	gemini := findHarness(t, selection, cli.HarnessGeminiCLI)
	if !gemini.Disabled {
		t.Fatalf("gemini should be disabled")
	}
	if gemini.SelectedCount != 0 {
		t.Fatalf("gemini selected count = %d, want 0", gemini.SelectedCount)
	}
	if !hasWarning(selection.Warnings, WarningNoSubscription, cli.HarnessGeminiCLI) {
		t.Fatalf("expected no subscription warning: %#v", selection.Warnings)
	}
}

func TestManualSelectionHonorsExplicitAccounts(t *testing.T) {
	t.Parallel()
	selection, err := Select(Request{
		Mode: ModeManual,
		Manual: []ManualHarness{
			{Harness: cli.HarnessCodexCLI, Count: 3, AccountNames: []string{"codex-b"}},
		},
		Inventory: []Account{
			account(caam.ToolCodex, "codex-a"),
			account(caam.ToolCodex, "codex-b"),
		},
	})
	if err != nil {
		t.Fatalf("Select: %v", err)
	}
	codex := findHarness(t, selection, cli.HarnessCodexCLI)
	got := []string{codex.Accounts[0].Name, codex.Accounts[1].Name, codex.Accounts[2].Name}
	want := []string{"codex-b", "codex-a", "codex-b"}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("account[%d] = %q, want %q; all=%v", i, got[i], want[i], got)
		}
	}
}

func TestManualSelectionRejectsUnknownExplicitAccount(t *testing.T) {
	t.Parallel()
	_, err := Select(Request{
		Mode: ModeManual,
		Manual: []ManualHarness{
			{Harness: cli.HarnessCodexCLI, Count: 1, AccountNames: []string{"missing"}},
		},
		Inventory: []Account{
			account(caam.ToolCodex, "codex-a"),
		},
	})
	if !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("expected ErrInvalidRequest, got %v", err)
	}
}

func TestSelectionKeepsDeterministicHarnessOrder(t *testing.T) {
	t.Parallel()
	selection, err := Select(Request{
		Mode: ModeAuto,
		Inventory: []Account{
			account(caam.ToolGemini, "gemini-a"),
			account(caam.ToolCodex, "codex-a"),
			account(caam.ToolClaude, "claude-a"),
		},
	})
	if err != nil {
		t.Fatalf("Select: %v", err)
	}
	want := []cli.Harness{cli.HarnessClaudeCode, cli.HarnessCodexCLI, cli.HarnessGeminiCLI}
	for i := range want {
		if selection.Harnesses[i].Harness != want[i] {
			t.Fatalf("harness[%d] = %s, want %s", i, selection.Harnesses[i].Harness, want[i])
		}
	}
}

func TestUnavailableAccountsIgnored(t *testing.T) {
	t.Parallel()
	future := time.Now().UTC().Add(time.Hour)
	selection, err := Select(Request{
		Mode: ModeAuto,
		Inventory: []Account{
			{Tool: caam.ToolClaude, Name: "cooling", CooldownUntil: &future},
			{Tool: caam.ToolCodex, Name: "expired", HealthStatus: "expired"},
			account(caam.ToolGemini, "ready"),
		},
	})
	if err != nil {
		t.Fatalf("Select: %v", err)
	}
	assertHarness(t, selection, cli.HarnessClaudeCode, 1, 0, true)
	assertHarness(t, selection, cli.HarnessCodexCLI, 1, 0, true)
	assertHarness(t, selection, cli.HarnessGeminiCLI, 1, 1, false)
}

func assertTarget(t *testing.T, targets map[cli.Harness]int, harness cli.Harness, want int) {
	t.Helper()
	if targets[harness] != want {
		t.Fatalf("%s target = %d, want %d", harness, targets[harness], want)
	}
}

func assertHarness(t *testing.T, selection Selection, harness cli.Harness, requested int, selected int, disabled bool) {
	t.Helper()
	got := findHarness(t, selection, harness)
	if got.RequestedCount != requested || got.SelectedCount != selected || got.Disabled != disabled {
		t.Fatalf("%s = requested:%d selected:%d disabled:%v, want requested:%d selected:%d disabled:%v", harness, got.RequestedCount, got.SelectedCount, got.Disabled, requested, selected, disabled)
	}
}

func findHarness(t *testing.T, selection Selection, harness cli.Harness) HarnessSelection {
	t.Helper()
	for _, got := range selection.Harnesses {
		if got.Harness == harness {
			return got
		}
	}
	t.Fatalf("missing harness %s in %#v", harness, selection.Harnesses)
	return HarnessSelection{}
}

func hasWarning(warnings []Warning, code WarningCode, harness cli.Harness) bool {
	for _, warning := range warnings {
		if warning.Code == code && (harness == "" || warning.Harness == harness) {
			return true
		}
	}
	return false
}

func account(tool caam.Tool, name string) Account {
	return Account{
		Tool:         tool,
		Name:         name,
		HealthStatus: "healthy",
	}
}
