// Package review owns Phase 12 review orchestration primitives: the round
// catalog, finding ledger lifecycle, source dedupe, and convergence projection.
//
// It deliberately does not call model providers. Rounds describe deterministic
// tools, subscription-backed CLI review, or delegated agent execution surfaces;
// callers execute those plans through the daemon's existing job/adaptor layers.
package review

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/hoopoe-cockpit/hoopoe/apps/daemon/internal/adapters/ubs"
	"github.com/hoopoe-cockpit/hoopoe/apps/daemon/internal/convergence"
)

const (
	SchemaVersion = 1

	SourceUBS   = "ubs"
	SourceAgent = "agent"
	SourceSkill = "skill"
	SourceHuman = "human"
)

var (
	ErrInvalidInput      = errors.New("review: invalid input")
	ErrUnknownRound      = errors.New("review: unknown round")
	ErrInvalidTransition = errors.New("review: invalid finding transition")
	ErrFindingNotFound   = errors.New("review: finding not found")
)

type ExecutionMode string

const (
	ModeDeterministicTool ExecutionMode = "deterministic_tool"
	ModeSubscriptionCLI   ExecutionMode = "subscription_cli"
	ModeDelegatedAgent    ExecutionMode = "delegated_agent"
)

type RoundKind string

const (
	RoundUBSFirstPass     RoundKind = "ubs_first_pass"
	RoundSelfReview       RoundKind = "original_agent_self_review"
	RoundCrossAgent       RoundKind = "cross_agent_review"
	RoundFreshEyes        RoundKind = "fresh_eyes_review"
	RoundRandomExplore    RoundKind = "random_code_exploration"
	RoundHotspotTargeted  RoundKind = "hotspot_targeted_review"
	RoundTestCoverage     RoundKind = "test_coverage_hardening"
	RoundUIPolish         RoundKind = "ui_ux_polish"
	RoundSpecializedAudit RoundKind = "specialized_audit"
	RoundFinalLanding     RoundKind = "final_landing_checklist"
)

type Severity string

const (
	SeverityCritical Severity = "critical"
	SeverityHigh     Severity = "high"
	SeverityMedium   Severity = "medium"
	SeverityLow      Severity = "low"
	SeverityInfo     Severity = "info"
)

type FindingStatus string

const (
	FindingNew           FindingStatus = "new"
	FindingTriaged       FindingStatus = "triaged"
	FindingFixNow        FindingStatus = "fix_now"
	FindingNewBead       FindingStatus = "new_bead"
	FindingFalsePositive FindingStatus = "false_positive"
	FindingNeedsHuman    FindingStatus = "needs_human"
	FindingClosed        FindingStatus = "closed"
)

type FindingDisposition string

const (
	DispositionFixNow        FindingDisposition = "fix_now"
	DispositionNewBead       FindingDisposition = "new_bead"
	DispositionFalsePositive FindingDisposition = "false_positive"
	DispositionNeedsHuman    FindingDisposition = "needs_human"
)

type ActorKind string

const (
	ActorUser       ActorKind = "user"
	ActorAgent      ActorKind = "agent"
	ActorTool       ActorKind = "tool"
	ActorSystem     ActorKind = "system"
	ActorTendingJob ActorKind = "tending_job"
)

type RoundSpec struct {
	SchemaVersion int             `json:"schemaVersion"`
	Index         int             `json:"index"`
	RoundID       string          `json:"roundId"`
	Kind          RoundKind       `json:"kind"`
	Label         string          `json:"label"`
	DefaultMode   ExecutionMode   `json:"defaultMode"`
	AllowedModes  []ExecutionMode `json:"allowedModes"`
	AutoStart     bool            `json:"autoStart"`
	Capabilities  []string        `json:"capabilities,omitempty"`
	Skills        []string        `json:"skills,omitempty"`
	Description   string          `json:"description"`
}

type RoundRunMetadata struct {
	ProjectID     string        `json:"projectId"`
	RoundID       string        `json:"roundId,omitempty"`
	Mode          ExecutionMode `json:"mode,omitempty"`
	Tool          string        `json:"tool,omitempty"`
	PromptHash    string        `json:"promptHash,omitempty"`
	PromptVersion string        `json:"promptVersion,omitempty"`
	AgentIDs      []string      `json:"agentIds,omitempty"`
	StartedAt     time.Time     `json:"startedAt,omitempty"`
	CompletedAt   time.Time     `json:"completedAt,omitempty"`
	CostUnits     float64       `json:"costUnits,omitempty"`
	EffortMinutes float64       `json:"effortMinutes,omitempty"`
	Actor         Actor         `json:"actor"`
	CorrelationID string        `json:"correlationId,omitempty"`
}

type RoundArtifact struct {
	SchemaVersion     int               `json:"schemaVersion"`
	ProjectID         string            `json:"projectId"`
	RoundID           string            `json:"roundId"`
	Index             int               `json:"index"`
	Kind              RoundKind         `json:"kind"`
	Mode              ExecutionMode     `json:"mode"`
	Tool              string            `json:"tool,omitempty"`
	PromptHash        string            `json:"promptHash,omitempty"`
	PromptVersion     string            `json:"promptVersion,omitempty"`
	AgentIDs          []string          `json:"agentIds,omitempty"`
	StartedAt         time.Time         `json:"startedAt,omitempty"`
	CompletedAt       time.Time         `json:"completedAt,omitempty"`
	Findings          []Finding         `json:"findings,omitempty"`
	Fixes             int               `json:"fixes,omitempty"`
	NewBeadIDs        []string          `json:"newBeadIds,omitempty"`
	FalsePositives    int               `json:"falsePositives,omitempty"`
	TestFailuresFixed int               `json:"testFailuresFixed,omitempty"`
	CoverageDelta     float64           `json:"coverageDelta,omitempty"`
	ComplexityDelta   int               `json:"complexityDelta,omitempty"`
	CostUnits         float64           `json:"costUnits,omitempty"`
	EffortMinutes     float64           `json:"effortMinutes,omitempty"`
	Summary           RoundSummary      `json:"summary"`
	Events            []Event           `json:"events,omitempty"`
	Metadata          map[string]string `json:"metadata,omitempty"`
}

type RoundSummary struct {
	FindingsEmitted int            `json:"findingsEmitted"`
	StoredFindings  int            `json:"storedFindings"`
	DedupedFindings int            `json:"dedupedFindings"`
	BySource        map[string]int `json:"bySource,omitempty"`
	BySeverity      map[string]int `json:"bySeverity,omitempty"`
}

type Ledger struct {
	SchemaVersion int             `json:"schemaVersion"`
	ProjectID     string          `json:"projectId"`
	Findings      []Finding       `json:"findings"`
	Rounds        []RoundArtifact `json:"rounds"`
	Events        []Event         `json:"events,omitempty"`
	GeneratedAt   time.Time       `json:"generatedAt,omitempty"`
}

type Finding struct {
	SchemaVersion int                 `json:"schemaVersion"`
	ID            string              `json:"id"`
	ProjectID     string              `json:"projectId,omitempty"`
	RoundID       string              `json:"roundId,omitempty"`
	Source        string              `json:"source"`
	Sources       []string            `json:"sources,omitempty"`
	Mode          ExecutionMode       `json:"mode,omitempty"`
	Severity      Severity            `json:"severity"`
	Status        FindingStatus       `json:"status"`
	Disposition   FindingDisposition  `json:"disposition,omitempty"`
	Fingerprint   string              `json:"fingerprint"`
	DuplicateOf   string              `json:"duplicateOf,omitempty"`
	Category      string              `json:"category,omitempty"`
	RuleID        string              `json:"ruleId,omitempty"`
	Message       string              `json:"message"`
	FilePath      string              `json:"filePath,omitempty"`
	StartLine     int                 `json:"startLine,omitempty"`
	EndLine       int                 `json:"endLine,omitempty"`
	CodeContext   string              `json:"codeContext,omitempty"`
	BeadID        string              `json:"beadId,omitempty"`
	PlanSectionID string              `json:"planSectionId,omitempty"`
	Evidence      []EvidenceRef       `json:"evidence,omitempty"`
	Transitions   []FindingTransition `json:"transitions,omitempty"`
	CreatedAt     time.Time           `json:"createdAt"`
	UpdatedAt     time.Time           `json:"updatedAt"`
	Metadata      map[string]string   `json:"metadata,omitempty"`
}

type EvidenceRef struct {
	Kind   string `json:"kind"`
	ID     string `json:"id,omitempty"`
	URI    string `json:"uri,omitempty"`
	Digest string `json:"digest,omitempty"`
}

type Actor struct {
	Kind ActorKind `json:"kind"`
	ID   string    `json:"id"`
}

type FindingTransition struct {
	From   FindingStatus     `json:"from"`
	To     FindingStatus     `json:"to"`
	Actor  Actor             `json:"actor"`
	Reason string            `json:"reason,omitempty"`
	At     time.Time         `json:"at"`
	Data   map[string]string `json:"data,omitempty"`
}

type TransitionRequest struct {
	FindingID   string             `json:"findingId"`
	To          FindingStatus      `json:"to"`
	Disposition FindingDisposition `json:"disposition,omitempty"`
	Actor       Actor              `json:"actor"`
	Reason      string             `json:"reason,omitempty"`
	BeadID      string             `json:"beadId,omitempty"`
	At          time.Time          `json:"at,omitempty"`
	Data        map[string]string  `json:"data,omitempty"`
}

type Event struct {
	SchemaVersion int               `json:"schemaVersion"`
	EventID       string            `json:"eventId"`
	ProjectID     string            `json:"projectId,omitempty"`
	Action        string            `json:"action"`
	RoundID       string            `json:"roundId,omitempty"`
	FindingID     string            `json:"findingId,omitempty"`
	Actor         Actor             `json:"actor"`
	At            time.Time         `json:"at"`
	CorrelationID string            `json:"correlationId,omitempty"`
	Data          map[string]string `json:"data,omitempty"`
}

type LandingInput = convergence.LandingChecklistInput

type DashboardInput struct {
	Landing    LandingInput           `json:"landing"`
	Thresholds convergence.Thresholds `json:"thresholds,omitempty"`
	Now        func() time.Time       `json:"-"`
}

type RoundProgression struct {
	SchemaVersion int    `json:"schemaVersion"`
	ProjectID     string `json:"projectId"`
	NextRoundID   string `json:"nextRoundId,omitempty"`
	Complete      bool   `json:"complete"`
	Reason        string `json:"reason"`
}

func RoundCatalog() []RoundSpec {
	return []RoundSpec{
		{
			SchemaVersion: SchemaVersion,
			Index:         0,
			RoundID:       "round-0",
			Kind:          RoundUBSFirstPass,
			Label:         "UBS first-pass scan",
			DefaultMode:   ModeDeterministicTool,
			AllowedModes:  []ExecutionMode{ModeDeterministicTool},
			AutoStart:     true,
			Capabilities:  []string{"ubs.scan"},
			Description:   "Fast deterministic UBS scan when entering hardening.",
		},
		{
			SchemaVersion: SchemaVersion,
			Index:         1,
			RoundID:       "round-1",
			Kind:          RoundSelfReview,
			Label:         "Original-agent self-review",
			DefaultMode:   ModeDelegatedAgent,
			AllowedModes:  []ExecutionMode{ModeDelegatedAgent},
			Description:   "Ask implementers to review their own landed beads while context is fresh.",
		},
		{
			SchemaVersion: SchemaVersion,
			Index:         2,
			RoundID:       "round-2",
			Kind:          RoundCrossAgent,
			Label:         "Cross-agent review",
			DefaultMode:   ModeDelegatedAgent,
			AllowedModes:  []ExecutionMode{ModeDelegatedAgent, ModeSubscriptionCLI},
			Description:   "A different reviewer inspects another agent's implementation evidence.",
		},
		{
			SchemaVersion: SchemaVersion,
			Index:         3,
			RoundID:       "round-3",
			Kind:          RoundFreshEyes,
			Label:         "Fresh-eyes review",
			DefaultMode:   ModeSubscriptionCLI,
			AllowedModes:  []ExecutionMode{ModeSubscriptionCLI, ModeDelegatedAgent},
			Description:   "A fresh context reviews plan, diffs, health, and findings through subscription-backed CLIs.",
		},
		{
			SchemaVersion: SchemaVersion,
			Index:         4,
			RoundID:       "round-4",
			Kind:          RoundRandomExplore,
			Label:         "Random code exploration",
			DefaultMode:   ModeSubscriptionCLI,
			AllowedModes:  []ExecutionMode{ModeSubscriptionCLI, ModeDelegatedAgent},
			Description:   "Randomized project exploration to catch integration issues outside known hotspots.",
		},
		{
			SchemaVersion: SchemaVersion,
			Index:         5,
			RoundID:       "round-5",
			Kind:          RoundHotspotTargeted,
			Label:         "Hotspot-targeted review",
			DefaultMode:   ModeDeterministicTool,
			AllowedModes:  []ExecutionMode{ModeDeterministicTool, ModeSubscriptionCLI, ModeDelegatedAgent},
			Capabilities:  []string{"ubs.scan", "health.coverage", "health.complexity"},
			Description:   "Re-run UBS or review high-churn, low-coverage, high-complexity files.",
		},
		{
			SchemaVersion: SchemaVersion,
			Index:         6,
			RoundID:       "round-6",
			Kind:          RoundTestCoverage,
			Label:         "Test/coverage hardening",
			DefaultMode:   ModeDelegatedAgent,
			AllowedModes:  []ExecutionMode{ModeDelegatedAgent, ModeDeterministicTool},
			Capabilities:  []string{"health.coverage", "health.test_runs"},
			Description:   "Drive coverage and failing-test hardening in isolated health worktrees.",
		},
		{
			SchemaVersion: SchemaVersion,
			Index:         7,
			RoundID:       "round-7",
			Kind:          RoundUIPolish,
			Label:         "UI/UX polish",
			DefaultMode:   ModeDelegatedAgent,
			AllowedModes:  []ExecutionMode{ModeDelegatedAgent, ModeSubscriptionCLI},
			Skills:        []string{"ui-polish", "ux-audit"},
			Description:   "Review UI surfaces when the project has a renderer or web app.",
		},
		{
			SchemaVersion: SchemaVersion,
			Index:         8,
			RoundID:       "round-8",
			Kind:          RoundSpecializedAudit,
			Label:         "Specialized audits",
			DefaultMode:   ModeDelegatedAgent,
			AllowedModes:  []ExecutionMode{ModeDelegatedAgent},
			Skills:        []string{"mock-code-finder", "deadlock-finder-and-fixer", "security-audit-for-saas", "profiling-software-performance"},
			Description:   "Skill-attached audits for mock code, concurrency, security, performance, and similar risks.",
		},
		{
			SchemaVersion: SchemaVersion,
			Index:         9,
			RoundID:       "round-9",
			Kind:          RoundFinalLanding,
			Label:         "Final landing checklist",
			DefaultMode:   ModeDeterministicTool,
			AllowedModes:  []ExecutionMode{ModeDeterministicTool},
			Description:   "Verify tests/builds, code-health follow-ups, and Git/beads sync before shipping.",
		},
	}
}

func RoundByID(roundID string) (RoundSpec, bool) {
	roundID = strings.TrimSpace(roundID)
	for _, spec := range RoundCatalog() {
		if spec.RoundID == roundID {
			return spec, true
		}
	}
	return RoundSpec{}, false
}

func RoundByIndex(index int) (RoundSpec, bool) {
	for _, spec := range RoundCatalog() {
		if spec.Index == index {
			return spec, true
		}
	}
	return RoundSpec{}, false
}

func NewLedger(projectID string, now time.Time) (Ledger, error) {
	projectID = strings.TrimSpace(projectID)
	if projectID == "" {
		return Ledger{}, fmt.Errorf("%w: projectId is required", ErrInvalidInput)
	}
	return Ledger{
		SchemaVersion: SchemaVersion,
		ProjectID:     projectID,
		Findings:      []Finding{},
		Rounds:        []RoundArtifact{},
		GeneratedAt:   now.UTC(),
	}, nil
}

func ArtifactFromUBS(result ubs.ScanResult, metadata RoundRunMetadata) (RoundArtifact, error) {
	projectID := strings.TrimSpace(metadata.ProjectID)
	if projectID == "" {
		return RoundArtifact{}, fmt.Errorf("%w: projectId is required", ErrInvalidInput)
	}
	index, err := indexForUBSRound(result.Round)
	if err != nil {
		return RoundArtifact{}, err
	}
	spec, ok := RoundByIndex(index)
	if !ok {
		return RoundArtifact{}, ErrUnknownRound
	}
	metadata.RoundID = choose(metadata.RoundID, spec.RoundID)
	metadata.Mode = normalizeMode(metadata.Mode, spec.DefaultMode)
	metadata.Tool = choose(metadata.Tool, SourceUBS)
	completedAt := metadata.CompletedAt
	if completedAt.IsZero() {
		completedAt = result.CheckedAt
	}
	findings := make([]Finding, 0, len(result.Findings))
	for _, finding := range result.Findings {
		findings = append(findings, Finding{
			SchemaVersion: SchemaVersion,
			ID:            choose(finding.FindingID, stableFindingID(SourceUBS, finding.FilePath, finding.LineRange.StartLine, finding.RuleID, finding.Message)),
			ProjectID:     projectID,
			RoundID:       metadata.RoundID,
			Source:        SourceUBS,
			Sources:       normalizeSources(append(finding.Sources, SourceUBS)...),
			Mode:          metadata.Mode,
			Severity:      severityFromUBS(finding.Severity),
			Status:        FindingNew,
			Category:      strings.TrimSpace(finding.Category),
			RuleID:        strings.TrimSpace(finding.RuleID),
			Message:       strings.TrimSpace(finding.Message),
			FilePath:      filepath.ToSlash(filepath.Clean(strings.TrimSpace(finding.FilePath))),
			StartLine:     finding.LineRange.StartLine,
			EndLine:       finding.LineRange.EndLine,
			CodeContext:   strings.TrimSpace(finding.CodeContext),
			CreatedAt:     finding.Time.UTC(),
			UpdatedAt:     finding.Time.UTC(),
			Evidence: []EvidenceRef{
				{Kind: "ubs_scan", ID: string(result.Round), URI: result.ProjectDir},
			},
		})
	}
	return NewRoundArtifact(spec, metadata, findings)
}

func NewRoundArtifact(spec RoundSpec, metadata RoundRunMetadata, findings []Finding) (RoundArtifact, error) {
	if spec.RoundID == "" {
		return RoundArtifact{}, ErrUnknownRound
	}
	projectID := strings.TrimSpace(metadata.ProjectID)
	if projectID == "" {
		return RoundArtifact{}, fmt.Errorf("%w: projectId is required", ErrInvalidInput)
	}
	if !modeAllowed(normalizeMode(metadata.Mode, spec.DefaultMode), spec.AllowedModes) {
		return RoundArtifact{}, fmt.Errorf("%w: mode %q is not allowed for %s", ErrInvalidInput, metadata.Mode, spec.RoundID)
	}
	roundID := choose(metadata.RoundID, spec.RoundID)
	mode := normalizeMode(metadata.Mode, spec.DefaultMode)
	artifact := RoundArtifact{
		SchemaVersion: SchemaVersion,
		ProjectID:     projectID,
		RoundID:       roundID,
		Index:         spec.Index,
		Kind:          spec.Kind,
		Mode:          mode,
		Tool:          strings.TrimSpace(metadata.Tool),
		PromptHash:    strings.TrimSpace(metadata.PromptHash),
		PromptVersion: strings.TrimSpace(metadata.PromptVersion),
		AgentIDs:      uniqueSortedTrimmed(metadata.AgentIDs),
		StartedAt:     metadata.StartedAt.UTC(),
		CompletedAt:   metadata.CompletedAt.UTC(),
		CostUnits:     metadata.CostUnits,
		EffortMinutes: metadata.EffortMinutes,
		Findings:      make([]Finding, 0, len(findings)),
	}
	if artifact.CompletedAt.IsZero() {
		artifact.CompletedAt = artifact.StartedAt
	}
	for _, finding := range findings {
		normalized := normalizeFinding(finding, projectID, roundID, mode, artifact.CompletedAt)
		artifact.Findings = append(artifact.Findings, normalized)
	}
	artifact.Summary = summarizeFindings(artifact.Findings, 0)
	artifact.Events = []Event{newEvent(projectID, "review.round_recorded", roundID, "", metadata.Actor, artifact.CompletedAt, metadata.CorrelationID, map[string]string{
		"kind":            string(spec.Kind),
		"mode":            string(mode),
		"findingsEmitted": strconv.Itoa(len(artifact.Findings)),
	})}
	return artifact, nil
}

func (ledger Ledger) IngestRound(artifact RoundArtifact) (Ledger, RoundSummary, error) {
	if strings.TrimSpace(ledger.ProjectID) == "" {
		return Ledger{}, RoundSummary{}, fmt.Errorf("%w: ledger projectId is required", ErrInvalidInput)
	}
	if artifact.ProjectID != ledger.ProjectID {
		return Ledger{}, RoundSummary{}, fmt.Errorf("%w: artifact project %q does not match ledger project %q", ErrInvalidInput, artifact.ProjectID, ledger.ProjectID)
	}
	out := ledger.copy()
	fingerprintIndex := out.fingerprintIndex()
	stored := 0
	deduped := 0
	for i := range artifact.Findings {
		finding := normalizeFinding(artifact.Findings[i], artifact.ProjectID, artifact.RoundID, artifact.Mode, artifact.CompletedAt)
		if existingIdx, ok := fingerprintIndex[finding.Fingerprint]; ok {
			existing := out.Findings[existingIdx]
			finding.DuplicateOf = existing.ID
			finding.Status = FindingClosed
			finding.Disposition = DispositionFalsePositive
			finding.Sources = mergeSources(existing.Sources, finding.Sources)
			out.Findings[existingIdx].Sources = finding.Sources
			out.Findings[existingIdx].UpdatedAt = maxTime(out.Findings[existingIdx].UpdatedAt, finding.UpdatedAt)
			deduped++
			artifact.Findings[i] = finding
			out.Events = append(out.Events, newEvent(artifact.ProjectID, "review.finding_deduped", artifact.RoundID, existing.ID, Actor{Kind: ActorSystem, ID: "review-ledger"}, artifact.CompletedAt, "", map[string]string{
				"duplicateFindingId": finding.ID,
				"source":             finding.Source,
			}))
			continue
		}
		fingerprintIndex[finding.Fingerprint] = len(out.Findings)
		out.Findings = append(out.Findings, finding)
		stored++
		artifact.Findings[i] = finding
		out.Events = append(out.Events, newEvent(artifact.ProjectID, "review.finding_created", artifact.RoundID, finding.ID, Actor{Kind: ActorSystem, ID: "review-ledger"}, finding.CreatedAt, "", map[string]string{
			"source":   finding.Source,
			"severity": string(finding.Severity),
		}))
	}
	artifact.Summary = summarizeFindings(artifact.Findings, deduped)
	artifact.Summary.StoredFindings = stored
	out.Rounds = append(out.Rounds, artifact)
	out.Events = append(out.Events, artifact.Events...)
	out.GeneratedAt = maxTime(out.GeneratedAt, artifact.CompletedAt)
	return out, artifact.Summary, nil
}

func (ledger Ledger) Transition(req TransitionRequest) (Ledger, Event, error) {
	out := ledger.copy()
	idx := -1
	for i, finding := range out.Findings {
		if finding.ID == req.FindingID {
			idx = i
			break
		}
	}
	if idx < 0 {
		return Ledger{}, Event{}, ErrFindingNotFound
	}
	finding := out.Findings[idx]
	at := req.At
	if at.IsZero() {
		at = time.Now()
	}
	at = at.UTC()
	if req.Disposition == "" {
		req.Disposition = dispositionForStatus(req.To)
	}
	if err := validateTransition(finding.Status, req.To, req.Disposition, req.BeadID, req.Reason); err != nil {
		return Ledger{}, Event{}, err
	}
	transition := FindingTransition{
		From:   finding.Status,
		To:     req.To,
		Actor:  normalizeActor(req.Actor, ActorSystem, "review-ledger"),
		Reason: strings.TrimSpace(req.Reason),
		At:     at,
		Data:   cleanStringMap(req.Data),
	}
	finding.Status = req.To
	if req.Disposition != "" {
		finding.Disposition = req.Disposition
	}
	if strings.TrimSpace(req.BeadID) != "" {
		finding.BeadID = strings.TrimSpace(req.BeadID)
	}
	finding.UpdatedAt = at
	finding.Transitions = append(finding.Transitions, transition)
	out.Findings[idx] = finding
	event := newEvent(out.ProjectID, "review.finding_transitioned", finding.RoundID, finding.ID, transition.Actor, at, "", map[string]string{
		"from":        string(transition.From),
		"to":          string(transition.To),
		"disposition": string(finding.Disposition),
		"beadId":      finding.BeadID,
	})
	out.Events = append(out.Events, event)
	out.GeneratedAt = maxTime(out.GeneratedAt, at)
	return out, event, nil
}

func (ledger Ledger) Dashboard(input DashboardInput) (convergence.Dashboard, error) {
	currentFindings := map[string]Finding{}
	for _, finding := range ledger.Findings {
		currentFindings[finding.ID] = finding
	}
	rounds := make([]convergence.ReviewRound, 0, len(ledger.Rounds))
	for _, artifact := range ledger.Rounds {
		roundFindings := make([]convergence.Finding, 0, len(artifact.Findings))
		newBeads := append([]string(nil), artifact.NewBeadIDs...)
		fixes := artifact.Fixes
		for _, finding := range artifact.Findings {
			if finding.DuplicateOf == "" {
				if current, ok := currentFindings[finding.ID]; ok {
					finding = current
				}
			}
			resolution := convergenceResolution(finding)
			if resolution == convergence.ResolutionFixed {
				fixes++
			}
			if resolution == convergence.ResolutionNewBead && finding.BeadID != "" {
				newBeads = append(newBeads, finding.BeadID)
			}
			roundFindings = append(roundFindings, convergence.Finding{
				ID:          finding.ID,
				Source:      finding.Source,
				Severity:    convergenceSeverity(finding.Severity),
				Resolution:  resolution,
				DuplicateOf: finding.DuplicateOf,
				BeadID:      finding.BeadID,
			})
		}
		rounds = append(rounds, convergence.ReviewRound{
			RoundID:           artifact.RoundID,
			Index:             artifact.Index,
			StartedAt:         artifact.StartedAt,
			CompletedAt:       artifact.CompletedAt,
			Findings:          roundFindings,
			Fixes:             fixes,
			NewBeads:          uniqueSortedTrimmed(newBeads),
			TestFailuresFixed: artifact.TestFailuresFixed,
			CoverageDelta:     artifact.CoverageDelta,
			ComplexityDelta:   artifact.ComplexityDelta,
			CostUnits:         artifact.CostUnits,
			EffortMinutes:     artifact.EffortMinutes,
		})
	}
	return convergence.Evaluate(convergence.EvaluationInput{
		ProjectID:  ledger.ProjectID,
		Rounds:     rounds,
		Landing:    convergence.LandingChecklistInput(input.Landing),
		Thresholds: input.Thresholds,
		Now:        input.Now,
	})
}

func dispositionForStatus(status FindingStatus) FindingDisposition {
	switch status {
	case FindingFixNow:
		return DispositionFixNow
	case FindingNewBead:
		return DispositionNewBead
	case FindingFalsePositive:
		return DispositionFalsePositive
	case FindingNeedsHuman:
		return DispositionNeedsHuman
	default:
		return ""
	}
}

func (ledger Ledger) NextRound() RoundProgression {
	seen := map[string]struct{}{}
	for _, artifact := range ledger.Rounds {
		seen[artifact.RoundID] = struct{}{}
	}
	for _, spec := range RoundCatalog() {
		if _, ok := seen[spec.RoundID]; !ok {
			return RoundProgression{
				SchemaVersion: SchemaVersion,
				ProjectID:     ledger.ProjectID,
				NextRoundID:   spec.RoundID,
				Reason:        "next review round has not run",
			}
		}
	}
	return RoundProgression{
		SchemaVersion: SchemaVersion,
		ProjectID:     ledger.ProjectID,
		Complete:      true,
		Reason:        "all review rounds have artifacts",
	}
}

func (ledger Ledger) copy() Ledger {
	out := ledger
	out.Findings = append([]Finding(nil), ledger.Findings...)
	out.Rounds = append([]RoundArtifact(nil), ledger.Rounds...)
	out.Events = append([]Event(nil), ledger.Events...)
	return out
}

func (ledger Ledger) fingerprintIndex() map[string]int {
	index := make(map[string]int, len(ledger.Findings))
	for i, finding := range ledger.Findings {
		if finding.Fingerprint != "" {
			index[finding.Fingerprint] = i
		}
	}
	return index
}

func normalizeFinding(finding Finding, projectID string, roundID string, mode ExecutionMode, at time.Time) Finding {
	finding.SchemaVersion = SchemaVersion
	finding.ProjectID = choose(finding.ProjectID, projectID)
	finding.RoundID = choose(finding.RoundID, roundID)
	finding.Source = choose(finding.Source, SourceAgent)
	finding.Sources = normalizeSources(append(finding.Sources, finding.Source)...)
	finding.Mode = normalizeMode(finding.Mode, mode)
	if finding.Severity == "" {
		finding.Severity = SeverityMedium
	}
	if finding.Status == "" {
		finding.Status = FindingNew
	}
	finding.Message = strings.TrimSpace(finding.Message)
	finding.Category = strings.TrimSpace(finding.Category)
	finding.RuleID = strings.TrimSpace(finding.RuleID)
	finding.FilePath = cleanPath(finding.FilePath)
	finding.CodeContext = strings.TrimSpace(finding.CodeContext)
	finding.BeadID = strings.TrimSpace(finding.BeadID)
	finding.PlanSectionID = strings.TrimSpace(finding.PlanSectionID)
	if finding.EndLine == 0 {
		finding.EndLine = finding.StartLine
	}
	if finding.Fingerprint == "" {
		finding.Fingerprint = fingerprintForFinding(finding)
	}
	if finding.ID == "" {
		finding.ID = stableFindingID(finding.Source, finding.FilePath, finding.StartLine, finding.RuleID, finding.Message)
	}
	if at.IsZero() {
		at = time.Now()
	}
	if finding.CreatedAt.IsZero() {
		finding.CreatedAt = at.UTC()
	}
	if finding.UpdatedAt.IsZero() {
		finding.UpdatedAt = finding.CreatedAt
	}
	return finding
}

func validateTransition(from FindingStatus, to FindingStatus, disposition FindingDisposition, beadID string, reason string) error {
	switch from {
	case FindingNew:
		if to != FindingTriaged {
			return fmt.Errorf("%w: new findings must move to triaged before %q", ErrInvalidTransition, to)
		}
	case FindingTriaged:
		switch to {
		case FindingFixNow:
			if disposition != "" && disposition != DispositionFixNow {
				return fmt.Errorf("%w: fix_now transition has incompatible disposition %q", ErrInvalidTransition, disposition)
			}
		case FindingNewBead:
			if strings.TrimSpace(beadID) == "" {
				return fmt.Errorf("%w: new_bead transition requires beadId", ErrInvalidTransition)
			}
		case FindingFalsePositive:
			if strings.TrimSpace(reason) == "" {
				return fmt.Errorf("%w: false_positive transition requires reason", ErrInvalidTransition)
			}
		case FindingNeedsHuman:
		default:
			return fmt.Errorf("%w: triaged findings cannot move to %q", ErrInvalidTransition, to)
		}
	case FindingFixNow, FindingNewBead, FindingFalsePositive, FindingNeedsHuman:
		if to != FindingClosed {
			return fmt.Errorf("%w: %q findings can only close", ErrInvalidTransition, from)
		}
	case FindingClosed:
		return fmt.Errorf("%w: closed findings cannot transition", ErrInvalidTransition)
	default:
		return fmt.Errorf("%w: unsupported source state %q", ErrInvalidTransition, from)
	}
	return nil
}

func summarizeFindings(findings []Finding, deduped int) RoundSummary {
	summary := RoundSummary{
		FindingsEmitted: len(findings),
		DedupedFindings: deduped,
		BySource:        map[string]int{},
		BySeverity:      map[string]int{},
	}
	for _, finding := range findings {
		if finding.DuplicateOf == "" {
			summary.StoredFindings++
		}
		summary.BySource[finding.Source]++
		summary.BySeverity[string(finding.Severity)]++
	}
	return summary
}

func convergenceResolution(finding Finding) convergence.Resolution {
	if finding.DuplicateOf != "" {
		return convergence.ResolutionDuplicate
	}
	switch finding.Status {
	case FindingClosed:
		switch finding.Disposition {
		case DispositionFixNow:
			return convergence.ResolutionFixed
		case DispositionNewBead:
			return convergence.ResolutionNewBead
		case DispositionFalsePositive:
			return convergence.ResolutionFalsePositive
		case DispositionNeedsHuman:
			return convergence.ResolutionAcceptedRisk
		default:
			return convergence.ResolutionOpen
		}
	case FindingNewBead:
		return convergence.ResolutionNewBead
	case FindingFalsePositive:
		return convergence.ResolutionFalsePositive
	default:
		return convergence.ResolutionOpen
	}
}

func convergenceSeverity(severity Severity) convergence.Severity {
	switch severity {
	case SeverityCritical:
		return convergence.SeverityCritical
	case SeverityHigh:
		return convergence.SeverityHigh
	case SeverityLow:
		return convergence.SeverityLow
	case SeverityInfo:
		return convergence.SeverityInfo
	default:
		return convergence.SeverityMedium
	}
}

func severityFromUBS(severity ubs.Severity) Severity {
	switch severity {
	case ubs.SeverityCritical:
		return SeverityCritical
	case ubs.SeverityInfo:
		return SeverityInfo
	default:
		return SeverityMedium
	}
}

func indexForUBSRound(round ubs.ScanRound) (int, error) {
	switch round {
	case ubs.RoundFirstPass, "":
		return 0, nil
	case ubs.RoundHotspotTarget:
		return 5, nil
	default:
		return 0, fmt.Errorf("%w: %q", ErrUnknownRound, round)
	}
}

func fingerprintForFinding(finding Finding) string {
	key := strings.Join([]string{
		cleanPath(finding.FilePath),
		strconv.Itoa(finding.StartLine),
		strings.TrimSpace(finding.RuleID),
		normalizeMessage(finding.Message),
	}, "\x00")
	sum := sha256.Sum256([]byte(key))
	return hex.EncodeToString(sum[:])
}

func stableFindingID(source string, filePath string, startLine int, ruleID string, message string) string {
	sum := sha256.Sum256([]byte(strings.Join([]string{
		strings.TrimSpace(source),
		cleanPath(filePath),
		strconv.Itoa(startLine),
		strings.TrimSpace(ruleID),
		normalizeMessage(message),
	}, "\x00")))
	return "rf_" + hex.EncodeToString(sum[:])[:20]
}

func newEvent(projectID string, action string, roundID string, findingID string, actor Actor, at time.Time, correlationID string, data map[string]string) Event {
	if at.IsZero() {
		at = time.Now()
	}
	actor = normalizeActor(actor, ActorSystem, "review")
	sum := sha256.Sum256([]byte(strings.Join([]string{
		strings.TrimSpace(projectID),
		strings.TrimSpace(action),
		strings.TrimSpace(roundID),
		strings.TrimSpace(findingID),
		at.UTC().Format(time.RFC3339Nano),
	}, "\x00")))
	return Event{
		SchemaVersion: SchemaVersion,
		EventID:       "rev_evt_" + hex.EncodeToString(sum[:])[:20],
		ProjectID:     strings.TrimSpace(projectID),
		Action:        strings.TrimSpace(action),
		RoundID:       strings.TrimSpace(roundID),
		FindingID:     strings.TrimSpace(findingID),
		Actor:         actor,
		At:            at.UTC(),
		CorrelationID: strings.TrimSpace(correlationID),
		Data:          cleanStringMap(data),
	}
}

func modeAllowed(mode ExecutionMode, allowed []ExecutionMode) bool {
	if len(allowed) == 0 {
		return true
	}
	for _, item := range allowed {
		if mode == item {
			return true
		}
	}
	return false
}

func normalizeMode(mode ExecutionMode, fallback ExecutionMode) ExecutionMode {
	if mode != "" {
		return mode
	}
	if fallback != "" {
		return fallback
	}
	return ModeDelegatedAgent
}

func normalizeActor(actor Actor, fallbackKind ActorKind, fallbackID string) Actor {
	if actor.Kind == "" {
		actor.Kind = fallbackKind
	}
	actor.ID = strings.TrimSpace(actor.ID)
	if actor.ID == "" {
		actor.ID = fallbackID
	}
	return actor
}

func normalizeSources(sources ...string) []string {
	return uniqueSortedTrimmed(sources)
}

func mergeSources(a []string, b []string) []string {
	return uniqueSortedTrimmed(append(append([]string(nil), a...), b...))
}

func uniqueSortedTrimmed(values []string) []string {
	seen := map[string]struct{}{}
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		seen[value] = struct{}{}
	}
	out := make([]string, 0, len(seen))
	for value := range seen {
		out = append(out, value)
	}
	sort.Strings(out)
	return out
}

func cleanStringMap(values map[string]string) map[string]string {
	if len(values) == 0 {
		return nil
	}
	out := make(map[string]string, len(values))
	for key, value := range values {
		key = strings.TrimSpace(key)
		value = strings.TrimSpace(value)
		if key != "" && value != "" {
			out[key] = value
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func cleanPath(path string) string {
	path = strings.TrimSpace(path)
	if path == "" {
		return ""
	}
	return filepath.ToSlash(filepath.Clean(path))
}

func normalizeMessage(message string) string {
	return strings.Join(strings.Fields(strings.TrimSpace(message)), " ")
}

func choose(value string, fallback string) string {
	value = strings.TrimSpace(value)
	if value != "" {
		return value
	}
	return strings.TrimSpace(fallback)
}

func maxTime(a time.Time, b time.Time) time.Time {
	if a.IsZero() {
		return b.UTC()
	}
	if b.IsZero() || a.After(b) {
		return a.UTC()
	}
	return b.UTC()
}
