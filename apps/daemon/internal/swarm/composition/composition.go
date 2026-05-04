// Package composition selects the agent harness mix for an NTM swarm launch.
//
// The picker is deterministic and pure: callers pass the ready-bead count,
// CAAM account inventory, and optional manual ratios; the package returns the
// launchable harness/account assignment plus UI-facing warnings.
package composition

import (
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/hoopoe-cockpit/hoopoe/apps/daemon/internal/adapters/caam"
	"github.com/hoopoe-cockpit/hoopoe/apps/daemon/internal/adapters/cli"
)

const (
	DefaultMaxAgents = 12
)

var (
	ErrInvalidRequest = errors.New("composition: invalid request")
)

type Mode string

const (
	ModeManual Mode = "manual"
	ModeAuto   Mode = "auto"
)

type WarningSeverity string

const (
	SeverityInfo    WarningSeverity = "info"
	SeverityWarning WarningSeverity = "warning"
	SeverityError   WarningSeverity = "error"
)

type WarningCode string

const (
	WarningNoSubscription    WarningCode = "no_subscription"
	WarningAccountPressure   WarningCode = "account_pressure"
	WarningAutoScaledByCap   WarningCode = "auto_scaled_by_cap"
	WarningManualCountCapped WarningCode = "manual_count_capped"
)

type Account struct {
	Tool          caam.Tool  `json:"tool"`
	Name          string     `json:"name"`
	HealthStatus  string     `json:"healthStatus,omitempty"`
	IsActive      bool       `json:"isActive,omitempty"`
	IsFavorite    bool       `json:"isFavorite,omitempty"`
	CooldownUntil *time.Time `json:"cooldownUntil,omitempty"`
}

type HarnessSpec struct {
	Harness           cli.Harness `json:"harness"`
	DisplayName       string      `json:"displayName"`
	CAAMTool          caam.Tool   `json:"caamTool"`
	SubscriptionLabel string      `json:"subscriptionLabel"`
}

type Request struct {
	Mode       Mode            `json:"mode"`
	ReadyBeads int             `json:"readyBeads,omitempty"`
	MaxAgents  int             `json:"maxAgents,omitempty"`
	Inventory  []Account       `json:"inventory,omitempty"`
	Manual     []ManualHarness `json:"manual,omitempty"`
	Now        time.Time       `json:"-"`
}

type ManualHarness struct {
	Harness      cli.Harness `json:"harness"`
	Count        int         `json:"count"`
	AccountNames []string    `json:"accountNames,omitempty"`
}

type Selection struct {
	Mode       Mode               `json:"mode"`
	ReadyBeads int                `json:"readyBeads,omitempty"`
	MaxAgents  int                `json:"maxAgents"`
	Total      int                `json:"total"`
	Harnesses  []HarnessSelection `json:"harnesses"`
	Warnings   []Warning          `json:"warnings,omitempty"`
}

type HarnessSelection struct {
	Harness        cli.Harness `json:"harness"`
	DisplayName    string      `json:"displayName"`
	CAAMTool       caam.Tool   `json:"caamTool"`
	RequestedCount int         `json:"requestedCount"`
	SelectedCount  int         `json:"selectedCount"`
	Accounts       []Account   `json:"accounts,omitempty"`
	Disabled       bool        `json:"disabled"`
	DisabledReason string      `json:"disabledReason,omitempty"`
}

type Warning struct {
	Code     WarningCode     `json:"code"`
	Severity WarningSeverity `json:"severity"`
	Harness  cli.Harness     `json:"harness,omitempty"`
	Message  string          `json:"message"`
}

func Select(req Request) (Selection, error) {
	maxAgents := normalizeMaxAgents(req.MaxAgents)
	if req.ReadyBeads < 0 {
		return Selection{}, fmt.Errorf("%w: readyBeads cannot be negative", ErrInvalidRequest)
	}

	switch req.Mode {
	case ModeManual:
		return selectManual(req, maxAgents)
	case ModeAuto:
		return selectAuto(req, maxAgents)
	default:
		return Selection{}, fmt.Errorf("%w: unsupported mode %q", ErrInvalidRequest, req.Mode)
	}
}

func DefaultAutoTargets(readyBeads int) map[cli.Harness]int {
	targets := map[cli.Harness]int{}
	switch {
	case readyBeads >= 400:
		targets[cli.HarnessClaudeCode] = 4
		targets[cli.HarnessCodexCLI] = 4
		targets[cli.HarnessGeminiCLI] = 2
	case readyBeads >= 100:
		targets[cli.HarnessClaudeCode] = 3
		targets[cli.HarnessCodexCLI] = 3
		targets[cli.HarnessGeminiCLI] = 2
	default:
		targets[cli.HarnessClaudeCode] = 1
		targets[cli.HarnessCodexCLI] = 1
		targets[cli.HarnessGeminiCLI] = 1
	}
	return targets
}

func Specs() []HarnessSpec {
	return []HarnessSpec{
		{
			Harness:           cli.HarnessClaudeCode,
			DisplayName:       "Claude Code",
			CAAMTool:          caam.ToolClaude,
			SubscriptionLabel: "Claude Max",
		},
		{
			Harness:           cli.HarnessCodexCLI,
			DisplayName:       "Codex CLI",
			CAAMTool:          caam.ToolCodex,
			SubscriptionLabel: "GPT Pro",
		},
		{
			Harness:           cli.HarnessGeminiCLI,
			DisplayName:       "Gemini CLI",
			CAAMTool:          caam.ToolGemini,
			SubscriptionLabel: "Gemini Ultra",
		},
	}
}

func AccountFromProfile(profile caam.Profile) Account {
	return Account{
		Tool:          profile.Tool,
		Name:          profile.Name,
		HealthStatus:  profile.HealthStatus,
		IsActive:      profile.IsActive,
		IsFavorite:    profile.IsFavorite,
		CooldownUntil: profile.CooldownUntil,
	}
}

func AccountsFromProfiles(profiles []caam.Profile) []Account {
	accounts := make([]Account, 0, len(profiles))
	for _, profile := range profiles {
		accounts = append(accounts, AccountFromProfile(profile))
	}
	return accounts
}

func selectManual(req Request, maxAgents int) (Selection, error) {
	manualByHarness := map[cli.Harness]ManualHarness{}
	for _, entry := range req.Manual {
		spec, ok := specForHarness(entry.Harness)
		if !ok {
			return Selection{}, fmt.Errorf("%w: unsupported harness %q", ErrInvalidRequest, entry.Harness)
		}
		if entry.Count < 0 {
			return Selection{}, fmt.Errorf("%w: %s count cannot be negative", ErrInvalidRequest, spec.DisplayName)
		}
		if _, exists := manualByHarness[entry.Harness]; exists {
			return Selection{}, fmt.Errorf("%w: duplicate manual entry for %s", ErrInvalidRequest, spec.DisplayName)
		}
		manualByHarness[entry.Harness] = entry
	}

	totalRequested := 0
	for _, entry := range manualByHarness {
		totalRequested += entry.Count
	}
	if totalRequested > maxAgents {
		return Selection{}, fmt.Errorf("%w: requested %d agents exceeds cap %d", ErrInvalidRequest, totalRequested, maxAgents)
	}

	inventory := availableByTool(req.Inventory, requestTime(req))
	var warnings []Warning
	selections := make([]HarnessSelection, 0, len(Specs()))
	total := 0
	for _, spec := range Specs() {
		entry := manualByHarness[spec.Harness]
		available := inventory[spec.CAAMTool]
		selected, warningSet, err := selectHarnessAccounts(spec, available, entry.Count, entry.AccountNames, true)
		if err != nil {
			return Selection{}, err
		}
		warnings = append(warnings, warningSet...)
		selection := harnessSelection(spec, entry.Count, selected, available)
		selections = append(selections, selection)
		total += selection.SelectedCount
	}

	return Selection{
		Mode:       ModeManual,
		MaxAgents:  maxAgents,
		Total:      total,
		Harnesses:  selections,
		Warnings:   warnings,
		ReadyBeads: req.ReadyBeads,
	}, nil
}

func selectAuto(req Request, maxAgents int) (Selection, error) {
	targets := DefaultAutoTargets(req.ReadyBeads)
	targets, scaled := capTargets(targets, maxAgents)
	inventory := availableByTool(req.Inventory, requestTime(req))

	var warnings []Warning
	if scaled {
		warnings = append(warnings, Warning{
			Code:     WarningAutoScaledByCap,
			Severity: SeverityWarning,
			Message:  fmt.Sprintf("auto-selected composition was reduced to fit the %d-agent cap", maxAgents),
		})
	}

	selections := make([]HarnessSelection, 0, len(Specs()))
	total := 0
	for _, spec := range Specs() {
		available := inventory[spec.CAAMTool]
		requested := targets[spec.Harness]
		if len(available) < requested {
			requested = len(available)
		}
		selected, warningSet, err := selectHarnessAccounts(spec, available, requested, nil, false)
		if err != nil {
			return Selection{}, err
		}
		if len(available) == 0 && targets[spec.Harness] > 0 {
			warningSet = append(warningSet, noSubscriptionWarning(spec))
		}
		warnings = append(warnings, warningSet...)
		selection := harnessSelection(spec, targets[spec.Harness], selected, available)
		selections = append(selections, selection)
		total += selection.SelectedCount
	}

	return Selection{
		Mode:       ModeAuto,
		ReadyBeads: req.ReadyBeads,
		MaxAgents:  maxAgents,
		Total:      total,
		Harnesses:  selections,
		Warnings:   warnings,
	}, nil
}

func selectHarnessAccounts(spec HarnessSpec, available []Account, requested int, accountNames []string, allowPressure bool) ([]Account, []Warning, error) {
	if requested == 0 {
		return nil, nil, nil
	}

	if len(available) == 0 {
		return nil, []Warning{noSubscriptionWarning(spec)}, nil
	}

	pool, err := preferredAccountPool(spec, available, accountNames)
	if err != nil {
		return nil, nil, err
	}
	selected := make([]Account, 0, requested)
	for len(selected) < requested {
		if len(selected) >= len(pool) && !allowPressure {
			break
		}
		selected = append(selected, pool[len(selected)%len(pool)])
	}

	if requested > len(available) && allowPressure {
		return selected, []Warning{accountPressureWarning(spec, len(available), requested)}, nil
	}
	return selected, nil, nil
}

func preferredAccountPool(spec HarnessSpec, available []Account, accountNames []string) ([]Account, error) {
	if len(accountNames) == 0 {
		return available, nil
	}

	byName := map[string]Account{}
	for _, account := range available {
		byName[account.Name] = account
	}

	pool := make([]Account, 0, len(available))
	seen := map[string]bool{}
	for _, name := range accountNames {
		name = strings.TrimSpace(name)
		if name == "" || seen[name] {
			continue
		}
		account, ok := byName[name]
		if !ok {
			return nil, fmt.Errorf("%w: %s account %q is not available", ErrInvalidRequest, spec.DisplayName, name)
		}
		pool = append(pool, account)
		seen[name] = true
	}
	for _, account := range available {
		if !seen[account.Name] {
			pool = append(pool, account)
		}
	}
	return pool, nil
}

func harnessSelection(spec HarnessSpec, requested int, selected []Account, available []Account) HarnessSelection {
	selection := HarnessSelection{
		Harness:        spec.Harness,
		DisplayName:    spec.DisplayName,
		CAAMTool:       spec.CAAMTool,
		RequestedCount: requested,
		SelectedCount:  len(selected),
		Accounts:       selected,
	}
	if len(available) == 0 {
		selection.Disabled = true
		selection.DisabledReason = fmt.Sprintf("no configured %s account", spec.SubscriptionLabel)
	}
	return selection
}

func availableByTool(accounts []Account, now time.Time) map[caam.Tool][]Account {
	out := map[caam.Tool][]Account{}
	for _, account := range accounts {
		if account.Name == "" || !accountAvailable(account, now) {
			continue
		}
		out[account.Tool] = append(out[account.Tool], account)
	}

	for tool := range out {
		sort.SliceStable(out[tool], func(i, j int) bool {
			left := out[tool][i]
			right := out[tool][j]
			if left.IsActive != right.IsActive {
				return left.IsActive
			}
			if left.IsFavorite != right.IsFavorite {
				return left.IsFavorite
			}
			return left.Name < right.Name
		})
	}
	return out
}

func accountAvailable(account Account, now time.Time) bool {
	if account.CooldownUntil != nil && account.CooldownUntil.After(now) {
		return false
	}
	switch strings.ToLower(strings.TrimSpace(account.HealthStatus)) {
	case "", "ok", "healthy", "available", "ready":
		return true
	case "cooldown", "expired", "unhealthy", "missing", "logged_out", "logged-out", "rate_limited", "rate-limited":
		return false
	default:
		return true
	}
}

func capTargets(targets map[cli.Harness]int, maxAgents int) (map[cli.Harness]int, bool) {
	total := targetTotal(targets)
	if total <= maxAgents {
		return targets, false
	}

	capped := map[cli.Harness]int{}
	remaining := maxAgents
	for _, spec := range Specs() {
		base := targets[spec.Harness]
		next := base * maxAgents / total
		if next == 0 && base > 0 && remaining > 0 {
			next = 1
		}
		if next > remaining {
			next = remaining
		}
		capped[spec.Harness] = next
		remaining -= next
	}

	for remaining > 0 {
		changed := false
		for _, spec := range Specs() {
			if remaining == 0 {
				break
			}
			if capped[spec.Harness] < targets[spec.Harness] {
				capped[spec.Harness]++
				remaining--
				changed = true
			}
		}
		if !changed {
			break
		}
	}
	return capped, true
}

func targetTotal(targets map[cli.Harness]int) int {
	total := 0
	for _, count := range targets {
		total += count
	}
	return total
}

func normalizeMaxAgents(maxAgents int) int {
	if maxAgents <= 0 {
		return DefaultMaxAgents
	}
	return maxAgents
}

func requestTime(req Request) time.Time {
	if req.Now.IsZero() {
		return time.Now().UTC()
	}
	return req.Now.UTC()
}

func specForHarness(harness cli.Harness) (HarnessSpec, bool) {
	for _, spec := range Specs() {
		if spec.Harness == harness {
			return spec, true
		}
	}
	return HarnessSpec{}, false
}

func noSubscriptionWarning(spec HarnessSpec) Warning {
	return Warning{
		Code:     WarningNoSubscription,
		Severity: SeverityWarning,
		Harness:  spec.Harness,
		Message:  fmt.Sprintf("%s is disabled because no %s account is configured in CAAM", spec.DisplayName, spec.SubscriptionLabel),
	}
}

func accountPressureWarning(spec HarnessSpec, available int, requested int) Warning {
	return Warning{
		Code:     WarningAccountPressure,
		Severity: SeverityWarning,
		Harness:  spec.Harness,
		Message:  fmt.Sprintf("you have %d %s account%s but requested %d %s agents; they will share accounts and may rate-limit", available, spec.SubscriptionLabel, plural(available), requested, spec.DisplayName),
	}
}

func plural(count int) string {
	if count == 1 {
		return ""
	}
	return "s"
}
