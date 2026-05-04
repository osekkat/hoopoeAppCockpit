// Package convergence owns deterministic Phase 12 review-round convergence
// scoring and final landing checklist evaluation.
//
// The package is intentionally adapter-free: callers pass findings, health
// deltas, Git/bead sync state, and checklist inputs from canonical tool
// surfaces. Convergence turns those typed inputs into dashboard state.
package convergence

import (
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"
)

const DashboardSchemaVersion = 1

var (
	ErrInvalidInput      = errors.New("convergence: invalid input")
	ErrFinalGateBlocked  = errors.New("convergence: final gate blocked")
	ErrFinalGateNotReady = errors.New("convergence: final gate not ready")
)

type State string

const (
	StateNotStarted     State = "not_started"
	StateHighYield      State = "high_yield"
	StateMediumYield    State = "medium_yield"
	StateLowYield       State = "low_yield"
	StateSaturated      State = "saturated"
	StateFinalGateReady State = "final_gate_ready"
)

type Severity string

const (
	SeverityCritical Severity = "critical"
	SeverityHigh     Severity = "high"
	SeverityMedium   Severity = "medium"
	SeverityLow      Severity = "low"
	SeverityInfo     Severity = "info"
)

type Resolution string

const (
	ResolutionOpen                Resolution = "open"
	ResolutionFixed               Resolution = "fixed"
	ResolutionNewBead             Resolution = "new_bead"
	ResolutionDuplicate           Resolution = "duplicate"
	ResolutionFalsePositive       Resolution = "false_positive"
	ResolutionTrackedExistingBead Resolution = "tracked_existing_bead"
	ResolutionAcceptedRisk        Resolution = "accepted_risk"
)

type StepStatus string

const (
	StepComplete StepStatus = "complete"
	StepCurrent  StepStatus = "current"
	StepUpcoming StepStatus = "upcoming"
	StepBlocked  StepStatus = "blocked"
)

type ChecklistItemID string

const (
	ChecklistTestsBuilds ChecklistItemID = "tests_builds"
	ChecklistCodeHealth  ChecklistItemID = "code_health"
	ChecklistGitSynced   ChecklistItemID = "git_synced"
	ChecklistBeadsSynced ChecklistItemID = "beads_synced"
)

type EvaluationInput struct {
	ProjectID  string                `json:"projectId,omitempty"`
	Rounds     []ReviewRound         `json:"rounds"`
	Landing    LandingChecklistInput `json:"landing"`
	Thresholds Thresholds            `json:"thresholds,omitempty"`
	Now        func() time.Time      `json:"-"`
}

type Thresholds struct {
	HighYieldUsefulFindings        int     `json:"highYieldUsefulFindings"`
	MediumYieldUsefulFindings      int     `json:"mediumYieldUsefulFindings"`
	SaturationMaxUsefulFindings    int     `json:"saturationMaxUsefulFindings"`
	SaturationDominatedRatio       float64 `json:"saturationDominatedRatio"`
	SaturationCostPerUsefulFinding float64 `json:"saturationCostPerUsefulFinding"`
	SaturationMinutesPerUseful     float64 `json:"saturationMinutesPerUseful"`
}

type ReviewRound struct {
	RoundID           string    `json:"roundId"`
	Index             int       `json:"index"`
	StartedAt         time.Time `json:"startedAt,omitempty"`
	CompletedAt       time.Time `json:"completedAt,omitempty"`
	Findings          []Finding `json:"findings,omitempty"`
	Fixes             int       `json:"fixes,omitempty"`
	NewBeads          []string  `json:"newBeads,omitempty"`
	TestFailuresFixed int       `json:"testFailuresFixed,omitempty"`
	CoverageDelta     float64   `json:"coverageDelta,omitempty"`
	ComplexityDelta   int       `json:"complexityDelta,omitempty"`
	CostUnits         float64   `json:"costUnits,omitempty"`
	EffortMinutes     float64   `json:"effortMinutes,omitempty"`
}

type Finding struct {
	ID          string     `json:"id"`
	Source      string     `json:"source,omitempty"`
	Severity    Severity   `json:"severity"`
	Resolution  Resolution `json:"resolution,omitempty"`
	DuplicateOf string     `json:"duplicateOf,omitempty"`
	BeadID      string     `json:"beadId,omitempty"`
}

type LandingChecklistInput struct {
	TestBuildsPassing   bool     `json:"testBuildsPassing"`
	TestBuildExceptions []string `json:"testBuildExceptions,omitempty"`
	CodeHealthPassing   bool     `json:"codeHealthPassing"`
	CodeHealthFollowUps []string `json:"codeHealthFollowUps,omitempty"`
	GitSynced           bool     `json:"gitSynced"`
	BeadsSynced         bool     `json:"beadsSynced"`
}

type Dashboard struct {
	SchemaVersion  int                  `json:"schemaVersion"`
	ProjectID      string               `json:"projectId,omitempty"`
	GeneratedAt    time.Time            `json:"generatedAt"`
	State          State                `json:"state"`
	Steps          []StateStep          `json:"steps"`
	Rounds         []RoundMetrics       `json:"rounds"`
	LatestRound    *RoundMetrics        `json:"latestRound,omitempty"`
	Saturation     SaturationAssessment `json:"saturation"`
	Landing        LandingChecklist     `json:"landing"`
	Recommendation string               `json:"recommendation"`
}

type StateStep struct {
	State  State      `json:"state"`
	Label  string     `json:"label"`
	Status StepStatus `json:"status"`
}

type RoundMetrics struct {
	RoundID                     string   `json:"roundId"`
	Index                       int      `json:"index"`
	Findings                    int      `json:"findings"`
	UsefulFindings              int      `json:"usefulFindings"`
	SevereFindings              int      `json:"severeFindings"`
	OpenSevereFindings          int      `json:"openSevereFindings"`
	DuplicateFindings           int      `json:"duplicateFindings"`
	RemainingFindings           int      `json:"remainingFindings"`
	LowDuplicateTrackedFindings int      `json:"lowDuplicateTrackedFindings"`
	LowDuplicateTrackedRatio    float64  `json:"lowDuplicateTrackedRatio"`
	Fixes                       int      `json:"fixes"`
	NewBeads                    int      `json:"newBeads"`
	NewBeadIDs                  []string `json:"newBeadIds,omitempty"`
	TestFailuresFixed           int      `json:"testFailuresFixed"`
	CoverageDelta               float64  `json:"coverageDelta"`
	ComplexityDelta             int      `json:"complexityDelta"`
	CostUnits                   float64  `json:"costUnits"`
	EffortMinutes               float64  `json:"effortMinutes"`
	CostUnitsPerUsefulFinding   float64  `json:"costUnitsPerUsefulFinding"`
	MinutesPerUsefulFinding     float64  `json:"minutesPerUsefulFinding"`
	State                       State    `json:"state"`
}

type SaturationAssessment struct {
	Saturated                 bool    `json:"saturated"`
	Reason                    string  `json:"reason"`
	UsefulFindings            int     `json:"usefulFindings"`
	OpenSevereFindings        int     `json:"openSevereFindings"`
	LowDuplicateTrackedRatio  float64 `json:"lowDuplicateTrackedRatio"`
	CostUnitsPerUsefulFinding float64 `json:"costUnitsPerUsefulFinding"`
	MinutesPerUsefulFinding   float64 `json:"minutesPerUsefulFinding"`
}

type LandingChecklist struct {
	Ready   bool            `json:"ready"`
	Items   []ChecklistItem `json:"items"`
	Missing []string        `json:"missing,omitempty"`
}

type ChecklistItem struct {
	ID      ChecklistItemID `json:"id"`
	Label   string          `json:"label"`
	Passed  bool            `json:"passed"`
	Detail  string          `json:"detail,omitempty"`
	Blocked bool            `json:"blocked"`
}

func DefaultThresholds() Thresholds {
	return Thresholds{
		HighYieldUsefulFindings:        5,
		MediumYieldUsefulFindings:      2,
		SaturationMaxUsefulFindings:    1,
		SaturationDominatedRatio:       0.75,
		SaturationCostPerUsefulFinding: 4,
		SaturationMinutesPerUseful:     45,
	}
}

func Evaluate(input EvaluationInput) (Dashboard, error) {
	if strings.TrimSpace(input.ProjectID) == "" && len(input.Rounds) == 0 {
		return Dashboard{}, fmt.Errorf("%w: projectId or at least one round is required", ErrInvalidInput)
	}
	now := input.Now
	if now == nil {
		now = time.Now
	}
	thresholds := normalizeThresholds(input.Thresholds)
	rounds, err := normalizeRounds(input.Rounds)
	if err != nil {
		return Dashboard{}, err
	}
	metrics := make([]RoundMetrics, 0, len(rounds))
	for _, round := range rounds {
		metric := computeRoundMetrics(round)
		metric.State = stateForMetrics(metric, thresholds)
		metrics = append(metrics, metric)
	}
	landing := EvaluateLandingChecklist(input.Landing)
	state := StateNotStarted
	saturation := SaturationAssessment{Reason: "no review rounds have completed"}
	var latest *RoundMetrics
	if len(metrics) > 0 {
		latestMetric := metrics[len(metrics)-1]
		latest = &latestMetric
		saturation = assessSaturation(latestMetric, thresholds)
		state = latestMetric.State
		if saturation.Saturated {
			state = StateSaturated
		}
		if saturation.Saturated && landing.Ready {
			state = StateFinalGateReady
		}
	}
	return Dashboard{
		SchemaVersion:  DashboardSchemaVersion,
		ProjectID:      strings.TrimSpace(input.ProjectID),
		GeneratedAt:    now().UTC(),
		State:          state,
		Steps:          buildSteps(state, landing.Ready),
		Rounds:         metrics,
		LatestRound:    latest,
		Saturation:     saturation,
		Landing:        landing,
		Recommendation: recommendation(state, landing),
	}, nil
}

func EvaluateLandingChecklist(input LandingChecklistInput) LandingChecklist {
	testBuildsPassed := input.TestBuildsPassing || len(trimmedStrings(input.TestBuildExceptions)) > 0
	codeHealthPassed := input.CodeHealthPassing || len(trimmedStrings(input.CodeHealthFollowUps)) > 0
	items := []ChecklistItem{
		{
			ID:      ChecklistTestsBuilds,
			Label:   "Tests/builds pass or exceptions documented",
			Passed:  testBuildsPassed,
			Detail:  checklistDetail(input.TestBuildsPassing, input.TestBuildExceptions, "exceptions documented"),
			Blocked: !testBuildsPassed,
		},
		{
			ID:      ChecklistCodeHealth,
			Label:   "Code health gates pass or follow-up beads exist",
			Passed:  codeHealthPassed,
			Detail:  checklistDetail(input.CodeHealthPassing, input.CodeHealthFollowUps, "follow-up beads exist"),
			Blocked: !codeHealthPassed,
		},
		{
			ID:      ChecklistGitSynced,
			Label:   "Git synced",
			Passed:  input.GitSynced,
			Blocked: !input.GitSynced,
		},
		{
			ID:      ChecklistBeadsSynced,
			Label:   "Beads synced",
			Passed:  input.BeadsSynced,
			Blocked: !input.BeadsSynced,
		},
	}
	missing := []string{}
	for _, item := range items {
		if !item.Passed {
			missing = append(missing, string(item.ID))
		}
	}
	return LandingChecklist{
		Ready:   len(missing) == 0,
		Items:   items,
		Missing: missing,
	}
}

func RequireFinalLandingReady(dashboard Dashboard) error {
	if dashboard.State != StateFinalGateReady {
		if len(dashboard.Landing.Missing) > 0 {
			return fmt.Errorf("%w: missing %s", ErrFinalGateBlocked, strings.Join(dashboard.Landing.Missing, ", "))
		}
		return ErrFinalGateNotReady
	}
	return nil
}

func normalizeThresholds(thresholds Thresholds) Thresholds {
	defaults := DefaultThresholds()
	if thresholds.HighYieldUsefulFindings <= 0 {
		thresholds.HighYieldUsefulFindings = defaults.HighYieldUsefulFindings
	}
	if thresholds.MediumYieldUsefulFindings <= 0 {
		thresholds.MediumYieldUsefulFindings = defaults.MediumYieldUsefulFindings
	}
	if thresholds.SaturationMaxUsefulFindings <= 0 {
		thresholds.SaturationMaxUsefulFindings = defaults.SaturationMaxUsefulFindings
	}
	if thresholds.SaturationDominatedRatio <= 0 || thresholds.SaturationDominatedRatio > 1 {
		thresholds.SaturationDominatedRatio = defaults.SaturationDominatedRatio
	}
	if thresholds.SaturationCostPerUsefulFinding <= 0 {
		thresholds.SaturationCostPerUsefulFinding = defaults.SaturationCostPerUsefulFinding
	}
	if thresholds.SaturationMinutesPerUseful <= 0 {
		thresholds.SaturationMinutesPerUseful = defaults.SaturationMinutesPerUseful
	}
	if thresholds.MediumYieldUsefulFindings > thresholds.HighYieldUsefulFindings {
		thresholds.MediumYieldUsefulFindings = thresholds.HighYieldUsefulFindings
	}
	return thresholds
}

func normalizeRounds(rounds []ReviewRound) ([]ReviewRound, error) {
	out := append([]ReviewRound(nil), rounds...)
	seen := map[string]struct{}{}
	for i := range out {
		out[i].RoundID = strings.TrimSpace(out[i].RoundID)
		if out[i].RoundID == "" {
			out[i].RoundID = fmt.Sprintf("round-%d", out[i].Index)
		}
		if _, ok := seen[out[i].RoundID]; ok {
			return nil, fmt.Errorf("%w: duplicate roundId %q", ErrInvalidInput, out[i].RoundID)
		}
		seen[out[i].RoundID] = struct{}{}
		if out[i].CostUnits < 0 || out[i].EffortMinutes < 0 || out[i].Fixes < 0 || out[i].TestFailuresFixed < 0 {
			return nil, fmt.Errorf("%w: negative round metric in %q", ErrInvalidInput, out[i].RoundID)
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Index != out[j].Index {
			return out[i].Index < out[j].Index
		}
		return out[i].CompletedAt.Before(out[j].CompletedAt)
	})
	return out, nil
}

func computeRoundMetrics(round ReviewRound) RoundMetrics {
	fixes := round.Fixes
	newBeads := append([]string(nil), round.NewBeads...)
	findings := len(round.Findings)
	useful := 0
	severe := 0
	openSevere := 0
	duplicates := 0
	remaining := 0
	dominated := 0
	for _, finding := range round.Findings {
		if isUseful(finding) {
			useful++
		}
		if isSevere(finding.Severity) {
			severe++
			if !isResolved(finding) {
				openSevere++
			}
		}
		if isDuplicate(finding) {
			duplicates++
		}
		if finding.Resolution == ResolutionFixed {
			fixes++
		}
		if finding.Resolution == ResolutionNewBead && strings.TrimSpace(finding.BeadID) != "" {
			newBeads = append(newBeads, finding.BeadID)
		}
		if !isResolved(finding) {
			remaining++
			if isDuplicate(finding) || isLowPriority(finding.Severity) || isTracked(finding) {
				dominated++
			}
		}
	}
	effortMinutes := round.EffortMinutes
	if effortMinutes == 0 && !round.StartedAt.IsZero() && round.CompletedAt.After(round.StartedAt) {
		effortMinutes = round.CompletedAt.Sub(round.StartedAt).Minutes()
	}
	newBeads = uniqueSortedTrimmed(newBeads)
	return RoundMetrics{
		RoundID:                     round.RoundID,
		Index:                       round.Index,
		Findings:                    findings,
		UsefulFindings:              useful,
		SevereFindings:              severe,
		OpenSevereFindings:          openSevere,
		DuplicateFindings:           duplicates,
		RemainingFindings:           remaining,
		LowDuplicateTrackedFindings: dominated,
		LowDuplicateTrackedRatio:    ratio(dominated, remaining),
		Fixes:                       fixes,
		NewBeads:                    len(newBeads),
		NewBeadIDs:                  newBeads,
		TestFailuresFixed:           round.TestFailuresFixed,
		CoverageDelta:               round.CoverageDelta,
		ComplexityDelta:             round.ComplexityDelta,
		CostUnits:                   round.CostUnits,
		EffortMinutes:               effortMinutes,
		CostUnitsPerUsefulFinding:   perUseful(round.CostUnits, useful),
		MinutesPerUsefulFinding:     perUseful(effortMinutes, useful),
	}
}

func stateForMetrics(metrics RoundMetrics, thresholds Thresholds) State {
	switch {
	case metrics.UsefulFindings >= thresholds.HighYieldUsefulFindings || metrics.OpenSevereFindings > 0:
		return StateHighYield
	case metrics.UsefulFindings >= thresholds.MediumYieldUsefulFindings:
		return StateMediumYield
	default:
		return StateLowYield
	}
}

func assessSaturation(metrics RoundMetrics, thresholds Thresholds) SaturationAssessment {
	assessment := SaturationAssessment{
		UsefulFindings:            metrics.UsefulFindings,
		OpenSevereFindings:        metrics.OpenSevereFindings,
		LowDuplicateTrackedRatio:  metrics.LowDuplicateTrackedRatio,
		CostUnitsPerUsefulFinding: metrics.CostUnitsPerUsefulFinding,
		MinutesPerUsefulFinding:   metrics.MinutesPerUsefulFinding,
	}
	if metrics.OpenSevereFindings > 0 {
		assessment.Reason = "open severe findings remain"
		return assessment
	}
	if metrics.UsefulFindings > thresholds.SaturationMaxUsefulFindings {
		assessment.Reason = "review is still producing useful findings"
		return assessment
	}
	if metrics.RemainingFindings > 0 && metrics.LowDuplicateTrackedRatio < thresholds.SaturationDominatedRatio {
		assessment.Reason = "remaining findings are not mostly duplicate, low severity, or bead-tracked"
		return assessment
	}
	if metrics.UsefulFindings == 0 || metrics.Findings == 0 {
		assessment.Saturated = true
		assessment.Reason = "latest round produced no untracked useful findings"
		return assessment
	}
	if metrics.CostUnitsPerUsefulFinding >= thresholds.SaturationCostPerUsefulFinding {
		assessment.Saturated = true
		assessment.Reason = "cost per useful finding crossed the saturation threshold"
		return assessment
	}
	if metrics.MinutesPerUsefulFinding >= thresholds.SaturationMinutesPerUseful {
		assessment.Saturated = true
		assessment.Reason = "time per useful finding crossed the saturation threshold"
		return assessment
	}
	assessment.Reason = "latest round is low yield but effort is still below saturation thresholds"
	return assessment
}

func buildSteps(current State, checklistReady bool) []StateStep {
	states := []State{
		StateNotStarted,
		StateHighYield,
		StateMediumYield,
		StateLowYield,
		StateSaturated,
		StateFinalGateReady,
	}
	currentIndex := stateIndex(current)
	steps := make([]StateStep, 0, len(states))
	for i, state := range states {
		status := StepUpcoming
		switch {
		case state == current:
			status = StepCurrent
		case i < currentIndex:
			status = StepComplete
		}
		if state == StateFinalGateReady && current != StateFinalGateReady && !checklistReady {
			status = StepBlocked
		}
		steps = append(steps, StateStep{
			State:  state,
			Label:  stateLabel(state),
			Status: status,
		})
	}
	return steps
}

func recommendation(state State, landing LandingChecklist) string {
	switch state {
	case StateNotStarted:
		return "run the first review round"
	case StateHighYield:
		return "fix severe and high-value findings, then run another review round"
	case StateMediumYield:
		return "triage actionable findings and continue review rounds"
	case StateLowYield:
		return "continue one more targeted round or convert remaining work into beads"
	case StateSaturated:
		if !landing.Ready {
			return "complete the final landing checklist"
		}
		return "advance to the final gate"
	case StateFinalGateReady:
		return "ship gate is ready"
	default:
		return "review convergence state"
	}
}

func stateIndex(state State) int {
	switch state {
	case StateNotStarted:
		return 0
	case StateHighYield:
		return 1
	case StateMediumYield:
		return 2
	case StateLowYield:
		return 3
	case StateSaturated:
		return 4
	case StateFinalGateReady:
		return 5
	default:
		return 0
	}
}

func stateLabel(state State) string {
	switch state {
	case StateNotStarted:
		return "Not started"
	case StateHighYield:
		return "High yield"
	case StateMediumYield:
		return "Medium yield"
	case StateLowYield:
		return "Low yield"
	case StateSaturated:
		return "Saturated"
	case StateFinalGateReady:
		return "Final gate ready"
	default:
		return string(state)
	}
}

func isUseful(finding Finding) bool {
	return !isDuplicate(finding) &&
		finding.Resolution != ResolutionFalsePositive &&
		!isTracked(finding)
}

func isResolved(finding Finding) bool {
	switch finding.Resolution {
	case ResolutionFixed, ResolutionDuplicate, ResolutionFalsePositive, ResolutionNewBead, ResolutionTrackedExistingBead, ResolutionAcceptedRisk:
		return true
	default:
		return false
	}
}

func isDuplicate(finding Finding) bool {
	return finding.Resolution == ResolutionDuplicate || strings.TrimSpace(finding.DuplicateOf) != ""
}

func isTracked(finding Finding) bool {
	return finding.Resolution == ResolutionNewBead ||
		finding.Resolution == ResolutionTrackedExistingBead ||
		strings.TrimSpace(finding.BeadID) != ""
}

func isSevere(severity Severity) bool {
	return severity == SeverityCritical || severity == SeverityHigh
}

func isLowPriority(severity Severity) bool {
	return severity == SeverityLow || severity == SeverityInfo || severity == ""
}

func ratio(numerator, denominator int) float64 {
	if denominator <= 0 {
		return 1
	}
	return float64(numerator) / float64(denominator)
}

func perUseful(total float64, useful int) float64 {
	if total <= 0 {
		return 0
	}
	if useful <= 0 {
		return total
	}
	return total / float64(useful)
}

func checklistDetail(pass bool, fallbacks []string, fallbackLabel string) string {
	if pass {
		return "passed"
	}
	if len(trimmedStrings(fallbacks)) > 0 {
		return fallbackLabel
	}
	return ""
}

func trimmedStrings(values []string) []string {
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" {
			out = append(out, value)
		}
	}
	return out
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
