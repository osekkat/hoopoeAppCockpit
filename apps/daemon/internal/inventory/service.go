package inventory

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/hoopoe-cockpit/hoopoe/apps/daemon/internal/adapters/caam"
	"github.com/hoopoe-cockpit/hoopoe/apps/daemon/internal/capabilities"
)

var ErrRegistryUnavailable = errors.New("inventory: capability registry unavailable")

type CAAMClient interface {
	List(ctx context.Context, tool caam.Tool) (*caam.ListResponse, error)
	Status(ctx context.Context, tool caam.Tool) (*caam.StatusResponse, error)
	Limits(ctx context.Context, tool caam.Tool) (*caam.LimitsResponse, error)
	Detect(ctx context.Context) (*caam.DetectResponse, error)
}

type Config struct {
	Registry           *capabilities.Registry
	CAAM               CAAMClient
	Now                func() time.Time
	RefreshConcurrency int
}

type Service struct {
	registry *capabilities.Registry
	caam     CAAMClient
	now      func() time.Time

	refreshSem chan struct{}
}

func NewService(cfg Config) *Service {
	now := cfg.Now
	if now == nil {
		now = time.Now
	}
	concurrency := cfg.RefreshConcurrency
	if concurrency <= 0 {
		concurrency = 1
	}
	return &Service{
		registry:   cfg.Registry,
		caam:       cfg.CAAM,
		now:        now,
		refreshSem: make(chan struct{}, concurrency),
	}
}

func (s *Service) Snapshot(ctx context.Context) (*Snapshot, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if s == nil || s.registry == nil {
		return nil, ErrRegistryUnavailable
	}
	capabilitySnapshot := s.registry.Snapshot()
	checkedAt := s.now().UTC()
	subscriptions := s.verifySubscriptions(ctx, checkedAt)
	warnings := append([]Warning(nil), subscriptions.Warnings...)
	return &Snapshot{
		SchemaVersion:             SchemaVersion,
		SnapshotAt:                checkedAt.Format(time.RFC3339),
		CapabilitiesSchemaVersion: capabilitySnapshot.SchemaVersion,
		DaemonAPIVersion:          capabilitySnapshot.DaemonAPIVersion,
		FixturesVersion:           capabilitySnapshot.FixturesVersion,
		Tools:                     buildTools(capabilitySnapshot),
		SubscriptionVerification:  subscriptions,
		Warnings:                  warnings,
	}, nil
}

func (s *Service) Refresh(ctx context.Context) (*Snapshot, error) {
	if s == nil || s.registry == nil {
		return nil, ErrRegistryUnavailable
	}
	select {
	case s.refreshSem <- struct{}{}:
		defer func() { <-s.refreshSem }()
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	s.registry.Probe()
	return s.Snapshot(ctx)
}

func buildTools(snapshot *capabilities.CapabilityRegistry) []Tool {
	if snapshot == nil || len(snapshot.Tools) == 0 {
		return []Tool{}
	}
	ids := make([]capabilities.ToolID, 0, len(snapshot.Tools))
	for id := range snapshot.Tools {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })

	tools := make([]Tool, 0, len(ids))
	for _, id := range ids {
		report := snapshot.Tools[id]
		if report == nil {
			continue
		}
		caps := cloneCapabilities(report.Capabilities)
		capIDs := sortedCapabilityIDs(caps)
		tools = append(tools, Tool{
			ID:              id,
			Name:            toolName(id),
			Repo:            toolRepo(id),
			Version:         report.Version,
			Source:          report.Source,
			Status:          aggregateStatus(caps),
			Capabilities:    caps,
			CapabilityIDs:   capIDs,
			LastCheckedAt:   report.LastCheckedAt,
			FixturesVersion: report.FixturesVersion,
		})
	}
	return tools
}

func cloneCapabilities(in map[string]capabilities.Capability) map[string]capabilities.Capability {
	out := make(map[string]capabilities.Capability, len(in))
	for id, cap := range in {
		out[id] = cap
	}
	return out
}

func sortedCapabilityIDs(caps map[string]capabilities.Capability) []string {
	ids := make([]string, 0, len(caps))
	for id := range caps {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}

func aggregateStatus(caps map[string]capabilities.Capability) capabilities.CapabilityStatus {
	if len(caps) == 0 {
		return capabilities.StatusMissing
	}
	hasBlocked := false
	hasDegraded := false
	hasMissing := false
	for _, cap := range caps {
		switch cap.Status {
		case capabilities.StatusBlockedByPolicy:
			hasBlocked = true
		case capabilities.StatusDegraded:
			hasDegraded = true
		case capabilities.StatusMissing, capabilities.StatusUntested:
			hasMissing = true
		}
	}
	switch {
	case hasBlocked:
		return capabilities.StatusBlockedByPolicy
	case hasDegraded:
		return capabilities.StatusDegraded
	case hasMissing:
		return capabilities.StatusMissing
	default:
		return capabilities.StatusOK
	}
}

func (s *Service) verifySubscriptions(ctx context.Context, checkedAt time.Time) SubscriptionVerification {
	verification := SubscriptionVerification{
		Status:        VerificationOK,
		CheckedAt:     checkedAt.Format(time.RFC3339),
		RequiredTools: make([]SubscriptionTool, 0, len(requiredSubscriptionTools)),
	}
	if s.caam == nil {
		verification.Status = VerificationUnavailable
		verification.Warnings = append(verification.Warnings, Warning{
			Code:    "caam.not_configured",
			Message: "CAAM client is not configured; subscription verification is unavailable.",
		})
		verification.RequiredTools = emptyRequiredTools()
		return verification
	}

	statuses, statusWarnings := s.caamStatuses(ctx)
	profiles, listWarnings := s.caamProfiles(ctx, checkedAt)
	limits, limitWarnings := s.caamLimits(ctx)
	detected, detectWarnings := s.caamDetectedAgents(ctx)
	verification.Warnings = append(verification.Warnings, statusWarnings...)
	verification.Warnings = append(verification.Warnings, listWarnings...)
	verification.Warnings = append(verification.Warnings, limitWarnings...)
	verification.Warnings = append(verification.Warnings, detectWarnings...)

	for _, spec := range requiredSubscriptionTools {
		status := statuses[spec.Tool]
		toolProfiles := profiles[spec.Tool]
		toolLimits := limitsForTool(spec, status.ActiveProfile, toolProfiles, limits)
		detectedAgent := detected[spec.Tool]
		entry := SubscriptionTool{
			Tool:                 string(spec.Tool),
			DisplayName:          spec.DisplayName,
			ExpectedSubscription: spec.ExpectedSubscription,
			SignedIn:             status.LoggedIn,
			ActiveProfile:        status.ActiveProfile,
			Health:               status.Health,
			AccountCount:         len(toolProfiles),
			AvailableAccounts:    countAvailable(toolProfiles),
			Profiles:             toolProfiles,
			Limits:               toolLimits,
			DetectedAgent:        detectedAgent,
		}
		verification.RequiredTools = append(verification.RequiredTools, entry)
		verification.TotalAccountCount += entry.AccountCount
		verification.TotalAvailableAccounts += entry.AvailableAccounts
		if entry.SignedIn {
			verification.SignedInCount++
		}
		if detectedAgent != nil {
			verification.DetectedAgents = append(verification.DetectedAgents, *detectedAgent)
		}
	}

	sort.Slice(verification.DetectedAgents, func(i, j int) bool {
		return verification.DetectedAgents[i].Tool < verification.DetectedAgents[j].Tool
	})
	if verification.TotalAccountCount == 0 {
		verification.ZeroSubscriptionWarning = true
		verification.Warnings = append(verification.Warnings, Warning{
			Code:    "caam.zero_subscriptions",
			Message: "No CAAM profiles are configured for Claude Code, Codex CLI, or Gemini CLI; onboarding can continue, but model-backed workflows will need subscription-backed CLI login first.",
		})
	}
	switch {
	case statusUnavailable(verification.Warnings):
		verification.Status = VerificationUnavailable
	case len(verification.Warnings) > 0 || verification.ZeroSubscriptionWarning:
		verification.Status = VerificationWarning
	default:
		verification.Status = VerificationOK
	}
	return verification
}

func (s *Service) caamProfiles(ctx context.Context, checkedAt time.Time) (map[caam.Tool][]AccountProfile, []Warning) {
	resp, err := s.caam.List(ctx, "")
	if err != nil {
		return map[caam.Tool][]AccountProfile{}, []Warning{caamWarning("caam.list_failed", "CAAM profile inventory failed", err)}
	}
	if resp == nil {
		return map[caam.Tool][]AccountProfile{}, []Warning{{
			Code:    "caam.list_failed",
			Message: "CAAM profile inventory returned an empty response.",
		}}
	}
	out := map[caam.Tool][]AccountProfile{}
	for _, profile := range resp.Profiles {
		tool := profile.Tool
		if !isRequiredTool(tool) {
			continue
		}
		out[tool] = append(out[tool], AccountProfile{
			Name:          profile.Name,
			Active:        profile.IsActive,
			Favorite:      profile.IsFavorite,
			Health:        profile.HealthStatus,
			Available:     profileAvailable(profile, checkedAt),
			LastUsedAt:    optionalTime(profile.LastUsedAt),
			CooldownUntil: profile.CooldownUntil,
		})
	}
	for tool := range out {
		sort.Slice(out[tool], func(i, j int) bool {
			if out[tool][i].Active != out[tool][j].Active {
				return out[tool][i].Active
			}
			if out[tool][i].Favorite != out[tool][j].Favorite {
				return out[tool][i].Favorite
			}
			return out[tool][i].Name < out[tool][j].Name
		})
	}
	return out, nil
}

func (s *Service) caamStatuses(ctx context.Context) (map[caam.Tool]caam.ToolStatus, []Warning) {
	resp, err := s.caam.Status(ctx, "")
	if err != nil {
		return map[caam.Tool]caam.ToolStatus{}, []Warning{caamWarning("caam.status_failed", "CAAM sign-in status failed", err)}
	}
	if resp == nil {
		return map[caam.Tool]caam.ToolStatus{}, []Warning{{
			Code:    "caam.status_failed",
			Message: "CAAM sign-in status returned an empty response.",
		}}
	}
	out := map[caam.Tool]caam.ToolStatus{}
	for _, status := range resp.Tools {
		if isRequiredTool(status.Tool) {
			out[status.Tool] = status
		}
	}
	return out, nil
}

func (s *Service) caamLimits(ctx context.Context) ([]caam.Limit, []Warning) {
	resp, err := s.caam.Limits(ctx, "")
	if err != nil {
		return nil, []Warning{caamWarning("caam.limits_failed", "CAAM subscription-limit inventory failed", err)}
	}
	if resp == nil {
		return nil, []Warning{{
			Code:    "caam.limits_failed",
			Message: "CAAM subscription-limit inventory returned an empty response.",
		}}
	}
	return append([]caam.Limit(nil), resp.Limits...), nil
}

func (s *Service) caamDetectedAgents(ctx context.Context) (map[caam.Tool]*DetectedAgent, []Warning) {
	resp, err := s.caam.Detect(ctx)
	if err != nil {
		return map[caam.Tool]*DetectedAgent{}, []Warning{caamWarning("caam.detect_failed", "CAAM CLI detection failed", err)}
	}
	if resp == nil {
		return map[caam.Tool]*DetectedAgent{}, []Warning{{
			Code:    "caam.detect_failed",
			Message: "CAAM CLI detection returned an empty response.",
		}}
	}
	out := map[caam.Tool]*DetectedAgent{}
	for _, agent := range resp.Agents {
		tool, ok := detectTool(agent)
		if !ok || !isRequiredTool(tool) {
			continue
		}
		entry := DetectedAgent{
			Tool:        string(tool),
			DisplayName: firstNonEmpty(agent.DisplayName, requiredSpec(tool).DisplayName),
			Installed:   agent.Installed,
			Version:     agent.Version,
			BinaryPath:  agent.BinaryPath,
		}
		out[tool] = &entry
	}
	return out, nil
}

func emptyRequiredTools() []SubscriptionTool {
	out := make([]SubscriptionTool, 0, len(requiredSubscriptionTools))
	for _, spec := range requiredSubscriptionTools {
		out = append(out, SubscriptionTool{
			Tool:                 string(spec.Tool),
			DisplayName:          spec.DisplayName,
			ExpectedSubscription: spec.ExpectedSubscription,
		})
	}
	return out
}

func limitsForTool(spec subscriptionToolSpec, activeProfile string, profiles []AccountProfile, limits []caam.Limit) []LimitSummary {
	profileSet := map[string]struct{}{}
	for _, profile := range profiles {
		if profile.Name != "" {
			profileSet[profile.Name] = struct{}{}
		}
	}
	if activeProfile != "" {
		profileSet[activeProfile] = struct{}{}
	}
	out := []LimitSummary{}
	for _, limit := range limits {
		if !limitMatches(spec, profileSet, limit) {
			continue
		}
		out = append(out, LimitSummary{
			Provider:       limit.Provider,
			Profile:        limit.Profile,
			UsedPct:        limit.UsedPct,
			WindowResetsAt: optionalTime(limit.WindowResetsAt),
			BurnRate:       limit.BurnRate,
			Notes:          limit.Notes,
		})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Provider != out[j].Provider {
			return out[i].Provider < out[j].Provider
		}
		return out[i].Profile < out[j].Profile
	})
	return out
}

func limitMatches(spec subscriptionToolSpec, profileSet map[string]struct{}, limit caam.Limit) bool {
	if _, ok := profileSet[limit.Profile]; ok && limit.Profile != "" {
		return true
	}
	provider := strings.ToLower(limit.Provider)
	return spec.ProviderHint != "" && strings.Contains(provider, spec.ProviderHint)
}

func countAvailable(profiles []AccountProfile) int {
	count := 0
	for _, profile := range profiles {
		if profile.Available {
			count++
		}
	}
	return count
}

func profileAvailable(profile caam.Profile, checkedAt time.Time) bool {
	if profile.CooldownUntil != nil && profile.CooldownUntil.After(checkedAt) {
		return false
	}
	health := strings.ToLower(profile.HealthStatus)
	for _, marker := range []string{"expired", "invalid", "rate-limited", "cooldown", "unhealthy"} {
		if strings.Contains(health, marker) {
			return false
		}
	}
	return true
}

func caamWarning(code, prefix string, err error) Warning {
	message := fmt.Sprintf("%s: %v", prefix, err)
	if errors.Is(err, caam.ErrMissingBinary) {
		message = "CAAM is not installed or not on PATH; subscription verification is unavailable."
	}
	return Warning{Code: code, Message: message}
}

func statusUnavailable(warnings []Warning) bool {
	for _, warning := range warnings {
		switch warning.Code {
		case "caam.not_configured", "caam.status_failed", "caam.list_failed":
			return true
		}
	}
	return false
}

func optionalTime(t time.Time) *time.Time {
	if t.IsZero() {
		return nil
	}
	utc := t.UTC()
	return &utc
}

type subscriptionToolSpec struct {
	Tool                 caam.Tool
	DisplayName          string
	ExpectedSubscription string
	ProviderHint         string
}

var requiredSubscriptionTools = []subscriptionToolSpec{
	{Tool: caam.ToolClaude, DisplayName: "Claude Code", ExpectedSubscription: "Claude Max", ProviderHint: "anthropic"},
	{Tool: caam.ToolCodex, DisplayName: "Codex CLI", ExpectedSubscription: "GPT Pro", ProviderHint: "openai"},
	{Tool: caam.ToolGemini, DisplayName: "Gemini CLI", ExpectedSubscription: "Gemini Ultra", ProviderHint: "google"},
}

func isRequiredTool(tool caam.Tool) bool {
	for _, spec := range requiredSubscriptionTools {
		if spec.Tool == tool {
			return true
		}
	}
	return false
}

func requiredSpec(tool caam.Tool) subscriptionToolSpec {
	for _, spec := range requiredSubscriptionTools {
		if spec.Tool == tool {
			return spec
		}
	}
	return subscriptionToolSpec{}
}

func detectTool(agent caam.AgentInventoryEntry) (caam.Tool, bool) {
	key := strings.ToLower(strings.TrimSpace(firstNonEmpty(agent.Name, agent.DisplayName)))
	key = strings.ReplaceAll(key, "_", "-")
	switch {
	case strings.Contains(key, "claude"):
		return caam.ToolClaude, true
	case strings.Contains(key, "codex"):
		return caam.ToolCodex, true
	case strings.Contains(key, "gemini"):
		return caam.ToolGemini, true
	default:
		return "", false
	}
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

func toolName(id capabilities.ToolID) string {
	if name, ok := knownTools[id]; ok {
		return name.name
	}
	return string(id)
}

func toolRepo(id capabilities.ToolID) string {
	if name, ok := knownTools[id]; ok {
		return name.repo
	}
	return ""
}

type toolMeta struct {
	name string
	repo string
}

var knownTools = map[capabilities.ToolID]toolMeta{
	capabilities.ToolNTM:       {name: "NTM", repo: "github.com/Dicklesworthstone/ntm"},
	capabilities.ToolBR:        {name: "br", repo: "github.com/Dicklesworthstone/beads_rust"},
	capabilities.ToolBV:        {name: "bv", repo: "github.com/Dicklesworthstone/beads-bv"},
	capabilities.ToolAgentMail: {name: "Agent Mail", repo: "mcp_agent_mail"},
	capabilities.ToolGit:       {name: "Git", repo: "git-scm.com"},
	capabilities.ToolRU:        {name: "ru", repo: "github.com/Dicklesworthstone/ru"},
	capabilities.ToolCAAM:      {name: "CAAM", repo: "github.com/Dicklesworthstone/caam"},
	capabilities.ToolCAUT:      {name: "caut", repo: "github.com/Dicklesworthstone/caut"},
	capabilities.ToolDCG:       {name: "DCG", repo: "github.com/Dicklesworthstone/dcg"},
	capabilities.ToolCASR:      {name: "CASR", repo: "github.com/Dicklesworthstone/casr"},
	capabilities.ToolPT:        {name: "pt", repo: "github.com/Dicklesworthstone/pt"},
	capabilities.ToolSRP:       {name: "srp", repo: "github.com/Dicklesworthstone/srp"},
	capabilities.ToolSBH:       {name: "sbh", repo: "github.com/Dicklesworthstone/sbh"},
	capabilities.ToolUBS:       {name: "UBS", repo: "github.com/Dicklesworthstone/ubs"},
	capabilities.ToolJSM:       {name: "jsm", repo: "github.com/Dicklesworthstone/jsm"},
	capabilities.ToolJFP:       {name: "jfp", repo: "github.com/Dicklesworthstone/jfp"},
	capabilities.ToolOracle:    {name: "Oracle", repo: "github.com/Dicklesworthstone/oracle"},
	capabilities.ToolRCH:       {name: "rch", repo: "github.com/Dicklesworthstone/rch"},
}
