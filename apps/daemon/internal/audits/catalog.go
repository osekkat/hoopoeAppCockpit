// Package audits owns the Phase 12 specialized-audit catalog.
//
// The package is deliberately adapter-light: it registers audit definitions,
// builds delegated-agent runner specs, and normalizes finding source stamps.
// Later review infrastructure can execute these specs through NTM without
// duplicating the catalog rules.
package audits

import (
	"errors"
	"fmt"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/hoopoe-cockpit/hoopoe/apps/daemon/internal/adapters/ubs"
)

const (
	CatalogSchemaVersion = 1

	RoundSpecialized ReviewRound = "round_8"
)

var (
	ErrInvalidCatalog = errors.New("audits: invalid catalog")
	ErrInvalidRequest = errors.New("audits: invalid request")
	ErrNotFound       = errors.New("audits: not found")
	ErrUnavailable    = errors.New("audits: unavailable")
)

type ReviewRound string

type AuditID string

const (
	AuditUBSStrict       AuditID = "ubs_strict"
	AuditUBSRecentFiles  AuditID = "ubs_recent_files"
	AuditMockCode        AuditID = "mock_code"
	AuditDeadlock        AuditID = "deadlock_concurrency"
	AuditSecuritySaaS    AuditID = "security_saas"
	AuditPerformance     AuditID = "performance_profiling"
	AuditRealityCheck    AuditID = "project_reality_check"
	AuditReasoningModes  AuditID = "reasoning_modes"
	AuditGoldenArtifacts AuditID = "golden_artifacts"
	AuditFuzzing         AuditID = "fuzzing"
	AuditE2ENoMocks      AuditID = "e2e_no_mocks"
	AuditUIPolish        AuditID = "ui_polish"
)

type ExecutionMode string

const (
	ModeUBSAdapter     ExecutionMode = "ubs_adapter"
	ModeDelegatedAgent ExecutionMode = "delegated_agent"
)

type Category string

const (
	CategoryStaticScan      Category = "static_scan"
	CategoryMockCode        Category = "mock_code"
	CategoryConcurrency     Category = "concurrency"
	CategorySecurity        Category = "security"
	CategoryPerformance     Category = "performance"
	CategoryRealityCheck    Category = "reality_check"
	CategoryReasoning       Category = "reasoning"
	CategoryGoldenArtifacts Category = "golden_artifacts"
	CategoryFuzzing         Category = "fuzzing"
	CategoryEndToEnd        Category = "e2e"
	CategoryUIPolish        Category = "ui_polish"
)

type ScopeKind string

const (
	ScopeWholeProject ScopeKind = "whole_project"
	ScopeRecentFiles  ScopeKind = "recent_files"
	ScopeHotspots     ScopeKind = "hotspots"
	ScopeUISurfaces   ScopeKind = "ui_surfaces"
)

type Definition struct {
	ID                   AuditID       `json:"id"`
	Title                string        `json:"title"`
	Summary              string        `json:"summary"`
	Category             Category      `json:"category"`
	Round                ReviewRound   `json:"round"`
	ExecutionMode        ExecutionMode `json:"executionMode"`
	SkillID              string        `json:"skillId,omitempty"`
	Source               string        `json:"source"`
	RequiredSkills       []string      `json:"requiredSkills,omitempty"`
	RequiredCapabilities []string      `json:"requiredCapabilities,omitempty"`
	DefaultScopes        []ScopeKind   `json:"defaultScopes"`
	CreatesBeads         bool          `json:"createsBeads"`
	DedupeAgainst        []string      `json:"dedupeAgainst,omitempty"`
	MarchingOrder        string        `json:"marchingOrder"`
}

type Availability struct {
	Skills       map[string]bool `json:"skills,omitempty"`
	Capabilities map[string]bool `json:"capabilities,omitempty"`
}

type PickerOption struct {
	Definition      Definition `json:"definition"`
	Enabled         bool       `json:"enabled"`
	DisabledReasons []string   `json:"disabledReasons,omitempty"`
}

type RunnerRequest struct {
	AuditID         AuditID  `json:"auditId"`
	ProjectID       string   `json:"projectId,omitempty"`
	TargetPaths     []string `json:"targetPaths,omitempty"`
	ExistingSources []string `json:"existingSources,omitempty"`
	MaxFindings     int      `json:"maxFindings,omitempty"`
}

type RunnerSpec struct {
	SchemaVersion        int           `json:"schemaVersion"`
	AuditID              AuditID       `json:"auditId"`
	Title                string        `json:"title"`
	ProjectID            string        `json:"projectId,omitempty"`
	Round                ReviewRound   `json:"round"`
	ExecutionMode        ExecutionMode `json:"executionMode"`
	SkillIDs             []string      `json:"skillIds,omitempty"`
	RequiredCapabilities []string      `json:"requiredCapabilities,omitempty"`
	Source               string        `json:"source"`
	TargetPaths          []string      `json:"targetPaths,omitempty"`
	MaxFindings          int           `json:"maxFindings,omitempty"`
	FindingPolicy        FindingPolicy `json:"findingPolicy"`
	Prompt               string        `json:"prompt"`
}

type SkillRegistration struct {
	SkillID  string    `json:"skillId"`
	AuditIDs []AuditID `json:"auditIds"`
	Source   string    `json:"source"`
}

type FindingPolicy struct {
	Source              string   `json:"source"`
	StampRequired       bool     `json:"stampRequired"`
	CreateBeads         bool     `json:"createBeads"`
	DedupeAgainst       []string `json:"dedupeAgainst,omitempty"`
	ExistingSources     []string `json:"existingSources,omitempty"`
	FreeFloatingAllowed bool     `json:"freeFloatingAllowed"`
}

type Finding struct {
	ID        string   `json:"id,omitempty"`
	Source    string   `json:"source"`
	Sources   []string `json:"sources,omitempty"`
	FilePath  string   `json:"filePath,omitempty"`
	Line      int      `json:"line,omitempty"`
	EndLine   int      `json:"endLine,omitempty"`
	RuleID    string   `json:"ruleId,omitempty"`
	Severity  string   `json:"severity,omitempty"`
	Category  string   `json:"category,omitempty"`
	Message   string   `json:"message"`
	DedupeKey string   `json:"dedupeKey,omitempty"`
}

func DefaultCatalog() []Definition {
	return []Definition{
		ubsDefinition(
			AuditUBSStrict,
			"UBS strict re-run",
			"Re-run UBS with stricter review-round rules before agent-driven audits spend tokens.",
			[]ScopeKind{ScopeWholeProject},
		),
		ubsDefinition(
			AuditUBSRecentFiles,
			"UBS recent-file re-run",
			"Re-run UBS against recently changed files and review hotspots.",
			[]ScopeKind{ScopeRecentFiles, ScopeHotspots},
		),
		skillDefinition(AuditMockCode, "Mock-code finder", CategoryMockCode, "mock-code-finder", []ScopeKind{ScopeWholeProject}, "Find stubs, placeholders, fake implementations, and tests that assert mock behavior instead of product behavior."),
		skillDefinition(AuditDeadlock, "Deadlock and concurrency audit", CategoryConcurrency, "deadlock-finder-and-fixer", []ScopeKind{ScopeHotspots, ScopeWholeProject}, "Find deadlocks, goroutine leaks, lock-order inversions, races, livelocks, and missing cancellation paths."),
		skillDefinition(AuditSecuritySaaS, "Security audit for SaaS", CategorySecurity, "security-audit-for-saas", []ScopeKind{ScopeWholeProject}, "Audit auth, billing, webhook integrity, tenant isolation, secret handling, command exposure, and audit/log redaction."),
		skillDefinition(AuditPerformance, "Performance profiling", CategoryPerformance, "profiling-software-performance", []ScopeKind{ScopeHotspots, ScopeWholeProject}, "Rank CPU, memory, I/O, and contention hotspots with an evidence-backed optimization target list."),
		skillDefinition(AuditRealityCheck, "Project reality check", CategoryRealityCheck, "reality-check-for-project", []ScopeKind{ScopeWholeProject}, "Compare the implemented system against README, plan, and roadmap promises and file drift as beads."),
		skillDefinition(AuditReasoningModes, "Reasoning-mode analysis", CategoryReasoning, "modes-of-reasoning-project-analysis", []ScopeKind{ScopeWholeProject}, "Run multiple reasoning lenses over the project and convert actionable contradictions into findings or beads."),
		skillDefinition(AuditGoldenArtifacts, "Golden artifact testing", CategoryGoldenArtifacts, "testing-golden-artifacts", []ScopeKind{ScopeHotspots, ScopeWholeProject}, "Identify outputs that should be frozen as golden artifacts and gaps in snapshot regression coverage."),
		skillDefinition(AuditFuzzing, "Fuzzing audit", CategoryFuzzing, "testing-fuzzing", []ScopeKind{ScopeHotspots, ScopeWholeProject}, "Find parser, adapter, serialization, and state-machine surfaces that need fuzzing harnesses."),
		skillDefinition(AuditE2ENoMocks, "E2E no-mocks audit", CategoryEndToEnd, "testing-real-service-e2e-no-mocks", []ScopeKind{ScopeWholeProject}, "Find workflows that require mock-free integration or E2E coverage with structured logging."),
		skillDefinition(AuditUIPolish, "UI polish review", CategoryUIPolish, "ui-polish", []ScopeKind{ScopeUISurfaces}, "Review product UI for density, affordances, accessibility, visual hierarchy, and production polish."),
	}
}

func ValidateCatalog(definitions []Definition) error {
	if len(definitions) == 0 {
		return fmt.Errorf("%w: empty catalog", ErrInvalidCatalog)
	}
	ids := map[AuditID]bool{}
	for _, definition := range definitions {
		if err := definition.Validate(); err != nil {
			return err
		}
		if ids[definition.ID] {
			return fmt.Errorf("%w: duplicate audit id %q", ErrInvalidCatalog, definition.ID)
		}
		ids[definition.ID] = true
	}
	return nil
}

func (d Definition) Validate() error {
	if d.ID == "" {
		return fmt.Errorf("%w: audit id is required", ErrInvalidCatalog)
	}
	if strings.TrimSpace(d.Title) == "" {
		return fmt.Errorf("%w: %s title is required", ErrInvalidCatalog, d.ID)
	}
	if d.Round != RoundSpecialized {
		return fmt.Errorf("%w: %s must run in %s", ErrInvalidCatalog, d.ID, RoundSpecialized)
	}
	if strings.TrimSpace(d.Source) == "" {
		return fmt.Errorf("%w: %s source is required", ErrInvalidCatalog, d.ID)
	}
	if !d.CreatesBeads {
		return fmt.Errorf("%w: %s must create beads for actionable findings", ErrInvalidCatalog, d.ID)
	}
	if len(d.DefaultScopes) == 0 {
		return fmt.Errorf("%w: %s needs at least one default scope", ErrInvalidCatalog, d.ID)
	}
	switch d.ExecutionMode {
	case ModeUBSAdapter:
		if d.Source != ubs.SourceUBS {
			return fmt.Errorf("%w: %s UBS audit source must be %q", ErrInvalidCatalog, d.ID, ubs.SourceUBS)
		}
		if !contains(d.RequiredCapabilities, ubs.CapabilityScan) {
			return fmt.Errorf("%w: %s requires %s", ErrInvalidCatalog, d.ID, ubs.CapabilityScan)
		}
	case ModeDelegatedAgent:
		if strings.TrimSpace(d.SkillID) == "" {
			return fmt.Errorf("%w: %s delegated audit missing skill id", ErrInvalidCatalog, d.ID)
		}
		if d.Source != SourceForSkill(d.SkillID) {
			return fmt.Errorf("%w: %s source must be %q", ErrInvalidCatalog, d.ID, SourceForSkill(d.SkillID))
		}
		if !contains(d.RequiredSkills, d.SkillID) {
			return fmt.Errorf("%w: %s required skills must include %s", ErrInvalidCatalog, d.ID, d.SkillID)
		}
	default:
		return fmt.Errorf("%w: %s unsupported execution mode %q", ErrInvalidCatalog, d.ID, d.ExecutionMode)
	}
	return nil
}

func Lookup(definitions []Definition, id AuditID) (Definition, bool) {
	for _, definition := range definitions {
		if definition.ID == id {
			return definition, true
		}
	}
	return Definition{}, false
}

func PickerOptions(definitions []Definition, availability Availability) ([]PickerOption, error) {
	if err := ValidateCatalog(definitions); err != nil {
		return nil, err
	}
	options := make([]PickerOption, 0, len(definitions))
	for _, definition := range definitions {
		option := PickerOption{
			Definition: definition,
			Enabled:    true,
		}
		for _, capability := range definition.RequiredCapabilities {
			if availability.Capabilities != nil && !availability.Capabilities[capability] {
				option.Enabled = false
				option.DisabledReasons = append(option.DisabledReasons, "missing capability: "+capability)
			}
		}
		for _, skillID := range definition.RequiredSkills {
			if availability.Skills != nil && !availability.Skills[skillID] {
				option.Enabled = false
				option.DisabledReasons = append(option.DisabledReasons, "missing skill: "+skillID)
			}
		}
		options = append(options, option)
	}
	return options, nil
}

func RequiredSkillRegistrations(definitions []Definition) ([]SkillRegistration, error) {
	if err := ValidateCatalog(definitions); err != nil {
		return nil, err
	}
	bySkill := map[string]*SkillRegistration{}
	for _, definition := range definitions {
		if definition.ExecutionMode != ModeDelegatedAgent {
			continue
		}
		registration := bySkill[definition.SkillID]
		if registration == nil {
			registration = &SkillRegistration{
				SkillID: definition.SkillID,
				Source:  definition.Source,
			}
			bySkill[definition.SkillID] = registration
		}
		registration.AuditIDs = append(registration.AuditIDs, definition.ID)
	}
	out := make([]SkillRegistration, 0, len(bySkill))
	for _, registration := range bySkill {
		sort.Slice(registration.AuditIDs, func(i, j int) bool {
			return registration.AuditIDs[i] < registration.AuditIDs[j]
		})
		out = append(out, *registration)
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].SkillID < out[j].SkillID
	})
	return out, nil
}

func BuildRunnableSpec(definitions []Definition, availability Availability, req RunnerRequest) (RunnerSpec, error) {
	options, err := PickerOptions(definitions, availability)
	if err != nil {
		return RunnerSpec{}, err
	}
	for _, option := range options {
		if option.Definition.ID != req.AuditID {
			continue
		}
		if !option.Enabled {
			return RunnerSpec{}, fmt.Errorf("%w: %s: %s", ErrUnavailable, req.AuditID, strings.Join(option.DisabledReasons, "; "))
		}
		return BuildRunnerSpec(definitions, req)
	}
	return RunnerSpec{}, fmt.Errorf("%w: audit %q", ErrNotFound, req.AuditID)
}

func BuildRunnerSpec(definitions []Definition, req RunnerRequest) (RunnerSpec, error) {
	if err := ValidateCatalog(definitions); err != nil {
		return RunnerSpec{}, err
	}
	definition, ok := Lookup(definitions, req.AuditID)
	if !ok {
		return RunnerSpec{}, fmt.Errorf("%w: audit %q", ErrNotFound, req.AuditID)
	}
	targets, err := normalizeTargetPaths(req.TargetPaths)
	if err != nil {
		return RunnerSpec{}, err
	}
	existingSources := normalizeSources(req.ExistingSources...)
	dedupeAgainst := normalizeSources(append(append([]string{}, definition.DedupeAgainst...), existingSources...)...)
	spec := RunnerSpec{
		SchemaVersion:        CatalogSchemaVersion,
		AuditID:              definition.ID,
		Title:                definition.Title,
		ProjectID:            strings.TrimSpace(req.ProjectID),
		Round:                definition.Round,
		ExecutionMode:        definition.ExecutionMode,
		SkillIDs:             append([]string(nil), definition.RequiredSkills...),
		RequiredCapabilities: append([]string(nil), definition.RequiredCapabilities...),
		Source:               definition.Source,
		TargetPaths:          targets,
		MaxFindings:          req.MaxFindings,
		FindingPolicy: FindingPolicy{
			Source:              definition.Source,
			StampRequired:       true,
			CreateBeads:         definition.CreatesBeads,
			DedupeAgainst:       dedupeAgainst,
			ExistingSources:     existingSources,
			FreeFloatingAllowed: false,
		},
	}
	spec.Prompt = buildPrompt(definition, spec)
	return spec, nil
}

func SourceForSkill(skillID string) string {
	return "skill:" + strings.TrimSpace(skillID)
}

func StampFindings(source string, findings []Finding) []Finding {
	source = strings.TrimSpace(source)
	stamped := make([]Finding, 0, len(findings))
	for _, finding := range findings {
		finding.Source = source
		finding.Sources = normalizeSources(append(finding.Sources, source)...)
		if finding.DedupeKey == "" {
			finding.DedupeKey = dedupeKey(finding)
		}
		stamped = append(stamped, finding)
	}
	return stamped
}

func MergeFindings(existing []Finding, incoming []Finding) []Finding {
	merged := make([]Finding, 0, len(existing)+len(incoming))
	index := map[string]int{}
	for _, finding := range existing {
		finding.Sources = normalizeSources(append(finding.Sources, finding.Source)...)
		if finding.DedupeKey == "" {
			finding.DedupeKey = dedupeKey(finding)
		}
		index[finding.DedupeKey] = len(merged)
		merged = append(merged, finding)
	}
	for _, finding := range incoming {
		finding.Sources = normalizeSources(append(finding.Sources, finding.Source)...)
		if finding.DedupeKey == "" {
			finding.DedupeKey = dedupeKey(finding)
		}
		if idx, ok := index[finding.DedupeKey]; ok {
			merged[idx].Sources = normalizeSources(append(merged[idx].Sources, finding.Sources...)...)
			if merged[idx].Source == "" {
				merged[idx].Source = finding.Source
			}
			continue
		}
		index[finding.DedupeKey] = len(merged)
		merged = append(merged, finding)
	}
	return merged
}

func FromUBSFindings(findings []ubs.Finding) []Finding {
	out := make([]Finding, 0, len(findings))
	for _, finding := range findings {
		out = append(out, Finding{
			ID:        finding.FindingID,
			Source:    ubs.SourceUBS,
			Sources:   normalizeSources(append(finding.Sources, ubs.SourceUBS)...),
			FilePath:  finding.FilePath,
			Line:      finding.LineRange.StartLine,
			EndLine:   finding.LineRange.EndLine,
			RuleID:    finding.RuleID,
			Severity:  string(finding.Severity),
			Category:  finding.Category,
			Message:   finding.Message,
			DedupeKey: dedupeKey(Finding{FilePath: finding.FilePath, Line: finding.LineRange.StartLine, RuleID: finding.RuleID, Category: finding.Category, Message: finding.Message}),
		})
	}
	return out
}

func ubsDefinition(id AuditID, title string, summary string, scopes []ScopeKind) Definition {
	return Definition{
		ID:                   id,
		Title:                title,
		Summary:              summary,
		Category:             CategoryStaticScan,
		Round:                RoundSpecialized,
		ExecutionMode:        ModeUBSAdapter,
		Source:               ubs.SourceUBS,
		RequiredCapabilities: []string{ubs.CapabilityScan},
		DefaultScopes:        scopes,
		CreatesBeads:         true,
		DedupeAgainst:        []string{ubs.SourceUBS},
		MarchingOrder:        "Run UBS as a deterministic specialized audit and convert actionable findings into beads.",
	}
}

func skillDefinition(id AuditID, title string, category Category, skillID string, scopes []ScopeKind, marchingOrder string) Definition {
	source := SourceForSkill(skillID)
	return Definition{
		ID:             id,
		Title:          title,
		Summary:        marchingOrder,
		Category:       category,
		Round:          RoundSpecialized,
		ExecutionMode:  ModeDelegatedAgent,
		SkillID:        skillID,
		Source:         source,
		RequiredSkills: []string{skillID},
		DefaultScopes:  scopes,
		CreatesBeads:   true,
		DedupeAgainst:  []string{ubs.SourceUBS},
		MarchingOrder:  marchingOrder,
	}
}

func buildPrompt(definition Definition, spec RunnerSpec) string {
	lines := []string{
		"Run " + definition.Title + " as a Phase 12 specialized audit.",
		"Use source: " + definition.Source + " on every finding.",
		"Convert actionable findings into beads; do not leave free-floating TODOs.",
		"Deduplicate against: " + strings.Join(spec.FindingPolicy.DedupeAgainst, ", ") + ".",
		"Audit goal: " + definition.MarchingOrder,
	}
	if len(spec.TargetPaths) > 0 {
		lines = append(lines, "Target paths: "+strings.Join(spec.TargetPaths, ", ")+".")
	}
	if spec.MaxFindings > 0 {
		lines = append(lines, "Maximum findings: "+strconv.Itoa(spec.MaxFindings)+".")
	}
	return strings.Join(lines, "\n")
}

func normalizeTargetPaths(paths []string) ([]string, error) {
	out := []string{}
	seen := map[string]bool{}
	for _, path := range paths {
		path = filepath.ToSlash(filepath.Clean(strings.TrimSpace(path)))
		if path == "." || path == "" {
			continue
		}
		if strings.HasPrefix(path, "../") || path == ".." || filepath.IsAbs(path) {
			return nil, fmt.Errorf("%w: target path must be repo-relative: %q", ErrInvalidRequest, path)
		}
		if strings.Contains(path, "\x00") || strings.Contains(path, ",") {
			return nil, fmt.Errorf("%w: invalid target path %q", ErrInvalidRequest, path)
		}
		if seen[path] {
			continue
		}
		seen[path] = true
		out = append(out, path)
	}
	sort.Strings(out)
	return out, nil
}

func normalizeSources(sources ...string) []string {
	out := []string{}
	seen := map[string]bool{}
	for _, source := range sources {
		source = strings.TrimSpace(source)
		if source == "" || seen[source] {
			continue
		}
		seen[source] = true
		out = append(out, source)
	}
	sort.Strings(out)
	return out
}

func dedupeKey(finding Finding) string {
	parts := []string{
		filepath.ToSlash(filepath.Clean(strings.TrimSpace(finding.FilePath))),
		strconv.Itoa(finding.Line),
		strings.TrimSpace(finding.RuleID),
		strings.TrimSpace(finding.Category),
		strings.ToLower(strings.Join(strings.Fields(finding.Message), " ")),
	}
	return strings.Join(parts, "|")
}

func contains(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}
