// Package health owns daemon-side code-health adapter planning and
// normalization primitives. It does not execute arbitrary project commands;
// callers receive typed argv/env/workdir plans that the job runner can route
// through policy, audit, and worktree isolation.
package health

import (
	"bytes"
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/hoopoe-cockpit/hoopoe/apps/daemon/internal/capabilities"
)

const (
	SchemaVersion            = 1
	referenceFixturesVersion = "phase0-2026-05-02"

	CapabilityCoverage   = "health.coverage"
	CapabilityComplexity = "health.complexity"
	CapabilityChurn      = "health.churn"
	CapabilityTestRuns   = "health.test_runs"
	CapabilityLOC        = "health.loc"
	CapabilityLint       = "health.lint"

	DefaultHotspotComplexityThreshold = 20
	DefaultHotspotCoverageThreshold   = 60.0
)

var (
	ErrInvalidRequest  = errors.New("health: invalid request")
	ErrUnknownAdapter  = errors.New("health: unknown adapter")
	ErrParseOutput     = errors.New("health: parse output")
	ErrUnsafeCommand   = errors.New("health: unsafe command")
	ErrActiveWorktree  = errors.New("health: active worktree requested")
	ErrNoCoverageTotal = errors.New("health: coverage total missing")
)

type Ecosystem string

const (
	EcosystemTS      Ecosystem = "ts"
	EcosystemPython  Ecosystem = "python"
	EcosystemRust    Ecosystem = "rust"
	EcosystemGo      Ecosystem = "go"
	EcosystemGeneric Ecosystem = "generic"
)

func AllEcosystems() []Ecosystem {
	return []Ecosystem{EcosystemTS, EcosystemPython, EcosystemRust, EcosystemGo, EcosystemGeneric}
}

func (e Ecosystem) Valid() bool {
	switch e {
	case EcosystemTS, EcosystemPython, EcosystemRust, EcosystemGo, EcosystemGeneric:
		return true
	default:
		return false
	}
}

func (e Ecosystem) ToolID() capabilities.ToolID {
	switch e {
	case EcosystemTS:
		return capabilities.ToolID("health_ts")
	case EcosystemPython:
		return capabilities.ToolID("health_py")
	case EcosystemRust:
		return capabilities.ToolID("health_rs")
	case EcosystemGo:
		return capabilities.ToolID("health_go")
	case EcosystemGeneric:
		return capabilities.ToolID("health_generic")
	default:
		return capabilities.ToolID("health_unknown")
	}
}

func (e Ecosystem) scopedCapability(suffix string) string {
	prefix := string(e)
	if e == EcosystemPython {
		prefix = "python"
	}
	return "health." + prefix + "." + suffix
}

type WorktreeRequest struct {
	ProjectID string
	RunID     string
	RepoPath  string
	Commit    string
	HomeDir   string
}

type WorktreeSpec struct {
	ProjectID string   `json:"projectId"`
	RunID     string   `json:"runId"`
	Source    string   `json:"source"`
	Commit    string   `json:"commit"`
	Root      string   `json:"root"`
	Path      string   `json:"path"`
	SetupArgv []string `json:"setupArgv"`
}

type CommandSpec struct {
	Label        string            `json:"label"`
	Argv         []string          `json:"argv"`
	Env          map[string]string `json:"env,omitempty"`
	WorkDir      string            `json:"workDir"`
	Produces     []string          `json:"produces,omitempty"`
	Capabilities []string          `json:"capabilities"`
	AllowExit    []int             `json:"allowExit"`
	RCHPreferred bool              `json:"rchPreferred"`
}

type RunPlan struct {
	SchemaVersion int                 `json:"schemaVersion"`
	Ecosystem     Ecosystem           `json:"ecosystem"`
	ToolID        capabilities.ToolID `json:"toolId"`
	Worktree      WorktreeSpec        `json:"worktree"`
	Commands      []CommandSpec       `json:"commands"`
	Capabilities  []string            `json:"capabilities"`
}

type Adapter struct {
	Ecosystem Ecosystem
}

func NewAdapter(ecosystem Ecosystem) (*Adapter, error) {
	if !ecosystem.Valid() {
		return nil, fmt.Errorf("%w: %q", ErrUnknownAdapter, ecosystem)
	}
	return &Adapter{Ecosystem: ecosystem}, nil
}

func Adapters() []*Adapter {
	out := make([]*Adapter, 0, len(AllEcosystems()))
	for _, ecosystem := range AllEcosystems() {
		out = append(out, &Adapter{Ecosystem: ecosystem})
	}
	return out
}

func (a *Adapter) Plan(req WorktreeRequest) (RunPlan, error) {
	if a == nil || !a.Ecosystem.Valid() {
		return RunPlan{}, ErrUnknownAdapter
	}
	worktree, err := BuildWorktreeSpec(req)
	if err != nil {
		return RunPlan{}, err
	}
	commands := commandPlan(a.Ecosystem, worktree)
	for i := range commands {
		if err := validateCommand(commands[i], worktree); err != nil {
			return RunPlan{}, err
		}
	}
	return RunPlan{
		SchemaVersion: SchemaVersion,
		Ecosystem:     a.Ecosystem,
		ToolID:        a.Ecosystem.ToolID(),
		Worktree:      worktree,
		Commands:      commands,
		Capabilities:  CapabilityIDs(a.Ecosystem),
	}, nil
}

func BuildWorktreeSpec(req WorktreeRequest) (WorktreeSpec, error) {
	projectID, err := cleanSegment("projectId", req.ProjectID)
	if err != nil {
		return WorktreeSpec{}, err
	}
	runID, err := cleanSegment("runId", req.RunID)
	if err != nil {
		return WorktreeSpec{}, err
	}
	if req.RepoPath == "" || !filepath.IsAbs(req.RepoPath) {
		return WorktreeSpec{}, fmt.Errorf("%w: repoPath must be absolute", ErrInvalidRequest)
	}
	home := req.HomeDir
	if home == "" {
		home, err = os.UserHomeDir()
		if err != nil {
			return WorktreeSpec{}, fmt.Errorf("%w: resolve home: %v", ErrInvalidRequest, err)
		}
	}
	if !filepath.IsAbs(home) {
		return WorktreeSpec{}, fmt.Errorf("%w: homeDir must be absolute", ErrInvalidRequest)
	}
	commit := strings.TrimSpace(req.Commit)
	if commit == "" {
		commit = "HEAD"
	}
	root := filepath.Join(filepath.Clean(home), ".hoopoe", "work", projectID, "health", runID)
	path := filepath.Join(root, "repo")
	source := filepath.Clean(req.RepoPath)
	if samePath(source, path) {
		return WorktreeSpec{}, ErrActiveWorktree
	}
	return WorktreeSpec{
		ProjectID: projectID,
		RunID:     runID,
		Source:    source,
		Commit:    commit,
		Root:      root,
		Path:      path,
		SetupArgv: []string{"git", "-C", source, "worktree", "add", "--detach", path, commit},
	}, nil
}

func CapabilityIDs(ecosystem Ecosystem) []string {
	base := []string{CapabilityCoverage, CapabilityComplexity, CapabilityChurn, CapabilityTestRuns}
	switch ecosystem {
	case EcosystemTS, EcosystemPython, EcosystemRust, EcosystemGo:
		return append(base,
			ecosystem.scopedCapability("coverage"),
			ecosystem.scopedCapability("complexity"),
			ecosystem.scopedCapability("churn"),
			ecosystem.scopedCapability("lint"),
		)
	case EcosystemGeneric:
		return append(base,
			EcosystemGeneric.scopedCapability("coverage"),
			EcosystemGeneric.scopedCapability("complexity"),
			EcosystemGeneric.scopedCapability("churn"),
			EcosystemGeneric.scopedCapability("loc"),
		)
	default:
		return base
	}
}

func CapabilityReports(now time.Time) []*capabilities.ToolReport {
	reports := make([]*capabilities.ToolReport, 0, len(AllEcosystems()))
	for _, ecosystem := range AllEcosystems() {
		reports = append(reports, CapabilityReport(ecosystem, now))
	}
	return reports
}

func CapabilityReport(ecosystem Ecosystem, now time.Time) *capabilities.ToolReport {
	report := &capabilities.ToolReport{
		Tool:            ecosystem.ToolID(),
		Version:         "planned",
		Source:          "planned-cli",
		LastCheckedAt:   now.UTC().Format(time.RFC3339),
		FixturesVersion: referenceFixturesVersion,
		Capabilities:    make(map[string]capabilities.Capability),
	}
	for _, id := range CapabilityIDs(ecosystem) {
		report.Capabilities[id] = capabilityFor(ecosystem, id)
	}
	return report
}

func RegisterStaticProbes(reg *capabilities.Registry, now func() time.Time) error {
	if reg == nil {
		return fmt.Errorf("%w: nil capability registry", ErrInvalidRequest)
	}
	if now == nil {
		now = time.Now
	}
	for _, ecosystem := range AllEcosystems() {
		ecosystem := ecosystem
		if err := reg.RegisterProbe(ecosystem.ToolID(), func() (*capabilities.ToolReport, error) {
			return CapabilityReport(ecosystem, now()), nil
		}); err != nil {
			return err
		}
	}
	return nil
}

func DetectEcosystems(paths []string) []Ecosystem {
	seen := map[Ecosystem]bool{EcosystemGeneric: true}
	for _, p := range paths {
		base := strings.ToLower(filepath.Base(p))
		ext := strings.ToLower(filepath.Ext(p))
		switch {
		case base == "package.json" || ext == ".ts" || ext == ".tsx" || ext == ".js" || ext == ".jsx":
			seen[EcosystemTS] = true
		case base == "pyproject.toml" || base == "requirements.txt" || base == "setup.py" || ext == ".py":
			seen[EcosystemPython] = true
		case base == "cargo.toml" || ext == ".rs":
			seen[EcosystemRust] = true
		case base == "go.mod" || ext == ".go":
			seen[EcosystemGo] = true
		}
	}
	ordered := AllEcosystems()
	out := make([]Ecosystem, 0, len(seen))
	for _, ecosystem := range ordered {
		if seen[ecosystem] {
			out = append(out, ecosystem)
		}
	}
	return out
}

type Dimension string

const (
	DimensionCoverage   Dimension = "coverage"
	DimensionComplexity Dimension = "complexity"
	DimensionChurn      Dimension = "churn"
	DimensionHotspot    Dimension = "hotspot"
	DimensionLint       Dimension = "lint"
	DimensionSecurity   Dimension = "security"
)

type FileMetric struct {
	SchemaVersion int       `json:"schemaVersion"`
	Path          string    `json:"path"`
	Dimension     Dimension `json:"dimension"`
	Value         float64   `json:"value"`
	Rank          *int      `json:"rank,omitempty"`
	Notes         string    `json:"notes,omitempty"`
}

type SummaryMetrics struct {
	CoveragePercent           float64 `json:"coveragePercent"`
	AverageComplexity         float64 `json:"averageComplexity"`
	LOC                       int     `json:"loc"`
	EffectiveLOC              int     `json:"effectiveLoc"`
	Churn7Days                int     `json:"churn7Days"`
	Churn30Days               int     `json:"churn30Days"`
	HotspotCount              int     `json:"hotspotCount"`
	TestPassed                int     `json:"testPassed"`
	TestFailed                int     `json:"testFailed"`
	TestDurationMS            int     `json:"testDurationMs"`
	FilesLackingCoverageRatio float64 `json:"filesLackingCoverageRatio"`
	ComplexityToCoverageDelta float64 `json:"complexityToCoverageDelta"`
}

type Snapshot struct {
	SchemaVersion int                `json:"schemaVersion"`
	ID            string             `json:"id"`
	ProjectID     string             `json:"projectId"`
	SnapshotAt    time.Time          `json:"snapshotAt"`
	Dimensions    map[string]float64 `json:"dimensions"`
	Files         []FileMetric       `json:"files"`
	Summary       SummaryMetrics     `json:"summary"`
}

type SnapshotInput struct {
	ID         string
	ProjectID  string
	SnapshotAt time.Time
	Files      []FileSignals
	Tests      TestRunSummary
	Thresholds HotspotThresholds
}

type FileSignals struct {
	Path            string
	CoveragePercent *float64
	Complexity      int
	LOC             int
	EffectiveLOC    int
	Churn7Days      int
	Churn30Days     int
	LintCount       int
	SecurityCount   int
}

type TestRunSummary struct {
	Passed     int
	Failed     int
	DurationMS int
}

type HotspotThresholds struct {
	Complexity      int
	CoveragePercent float64
}

func DefaultHotspotThresholds() HotspotThresholds {
	return HotspotThresholds{
		Complexity:      DefaultHotspotComplexityThreshold,
		CoveragePercent: DefaultHotspotCoverageThreshold,
	}
}

type Hotspot struct {
	Path            string   `json:"path"`
	Rank            int      `json:"rank"`
	Score           float64  `json:"score"`
	Complexity      int      `json:"complexity"`
	CoveragePercent *float64 `json:"coveragePercent,omitempty"`
	Churn30Days     int      `json:"churn30Days"`
	Notes           string   `json:"notes,omitempty"`
}

func ScoreHotspots(files []FileSignals, thresholds HotspotThresholds) []Hotspot {
	thresholds = normalizeThresholds(thresholds)
	hotspots := make([]Hotspot, 0)
	for _, file := range files {
		if file.Path == "" || file.Complexity < thresholds.Complexity {
			continue
		}
		coveragePenalty := 1.0
		note := ""
		if file.CoveragePercent != nil {
			if *file.CoveragePercent >= thresholds.CoveragePercent {
				continue
			}
			coveragePenalty = (thresholds.CoveragePercent - *file.CoveragePercent) / thresholds.CoveragePercent
		} else {
			note = "coverage missing"
		}
		complexityFactor := float64(file.Complexity) / float64(thresholds.Complexity)
		churnFactor := 1 + float64(file.Churn30Days)/100
		hotspots = append(hotspots, Hotspot{
			Path:            file.Path,
			Score:           complexityFactor * coveragePenalty * churnFactor,
			Complexity:      file.Complexity,
			CoveragePercent: file.CoveragePercent,
			Churn30Days:     file.Churn30Days,
			Notes:           note,
		})
	}
	sort.SliceStable(hotspots, func(i, j int) bool {
		if hotspots[i].Score == hotspots[j].Score {
			return hotspots[i].Path < hotspots[j].Path
		}
		return hotspots[i].Score > hotspots[j].Score
	})
	for i := range hotspots {
		hotspots[i].Rank = i + 1
	}
	return hotspots
}

func BuildSnapshot(input SnapshotInput) Snapshot {
	thresholds := normalizeThresholds(input.Thresholds)
	now := input.SnapshotAt
	if now.IsZero() {
		now = time.Now().UTC()
	}
	files := make([]FileMetric, 0, len(input.Files)*4)
	summary := summarize(input.Files, input.Tests, thresholds)
	for _, file := range input.Files {
		if file.Path == "" {
			continue
		}
		if file.CoveragePercent != nil {
			files = append(files, FileMetric{
				SchemaVersion: SchemaVersion,
				Path:          file.Path,
				Dimension:     DimensionCoverage,
				Value:         *file.CoveragePercent,
			})
		}
		if file.Complexity > 0 {
			files = append(files, FileMetric{
				SchemaVersion: SchemaVersion,
				Path:          file.Path,
				Dimension:     DimensionComplexity,
				Value:         float64(file.Complexity),
			})
		}
		if file.Churn30Days > 0 || file.Churn7Days > 0 {
			files = append(files, FileMetric{
				SchemaVersion: SchemaVersion,
				Path:          file.Path,
				Dimension:     DimensionChurn,
				Value:         float64(file.Churn30Days),
				Notes:         fmt.Sprintf("7d=%d", file.Churn7Days),
			})
		}
		if file.LintCount > 0 {
			files = append(files, FileMetric{
				SchemaVersion: SchemaVersion,
				Path:          file.Path,
				Dimension:     DimensionLint,
				Value:         float64(file.LintCount),
			})
		}
		if file.SecurityCount > 0 {
			files = append(files, FileMetric{
				SchemaVersion: SchemaVersion,
				Path:          file.Path,
				Dimension:     DimensionSecurity,
				Value:         float64(file.SecurityCount),
			})
		}
	}
	for _, hotspot := range ScoreHotspots(input.Files, thresholds) {
		rank := hotspot.Rank
		files = append(files, FileMetric{
			SchemaVersion: SchemaVersion,
			Path:          hotspot.Path,
			Dimension:     DimensionHotspot,
			Value:         hotspot.Score,
			Rank:          &rank,
			Notes:         hotspot.Notes,
		})
	}
	return Snapshot{
		SchemaVersion: SchemaVersion,
		ID:            input.ID,
		ProjectID:     input.ProjectID,
		SnapshotAt:    now.UTC(),
		Dimensions: map[string]float64{
			string(DimensionCoverage):   clampScore(summary.CoveragePercent / 10),
			string(DimensionComplexity): clampScore(10 - (summary.AverageComplexity / float64(thresholds.Complexity) * 10)),
			string(DimensionChurn):      clampScore(10 - float64(summary.Churn30Days)/100),
			string(DimensionHotspot):    clampScore(10 - float64(summary.HotspotCount)),
			string(DimensionLint):       clampScore(10 - float64(totalLint(input.Files))),
			string(DimensionSecurity):   clampScore(10 - float64(totalSecurity(input.Files))),
		},
		Files:   files,
		Summary: summary,
	}
}

type CoverageMetric struct {
	Path            string
	CoveragePercent float64
}

func ParseGoCoverFunc(data []byte) ([]CoverageMetric, float64, error) {
	lines := strings.Split(string(data), "\n")
	metrics := make([]CoverageMetric, 0)
	total := -1.0
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) == 0 {
			continue
		}
		percent, ok := parsePercent(fields[len(fields)-1])
		if !ok {
			return nil, 0, fmt.Errorf("%w: invalid go cover percent in %q", ErrParseOutput, line)
		}
		if fields[0] == "total:" {
			total = percent
			continue
		}
		pathPart, _, ok := strings.Cut(fields[0], ":")
		if !ok || pathPart == "" {
			return nil, 0, fmt.Errorf("%w: invalid go cover path in %q", ErrParseOutput, line)
		}
		metrics = append(metrics, CoverageMetric{Path: pathPart, CoveragePercent: percent})
	}
	if total < 0 {
		return metrics, 0, ErrNoCoverageTotal
	}
	return metrics, total, nil
}

type ChurnMetric struct {
	Path    string
	Added   int
	Deleted int
	Changes int
}

func ParseChurnNumstat(data []byte) map[string]ChurnMetric {
	out := make(map[string]ChurnMetric)
	for _, line := range strings.Split(string(data), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 3 || fields[0] == "-" || fields[1] == "-" {
			continue
		}
		added, errAdded := strconv.Atoi(fields[0])
		deleted, errDeleted := strconv.Atoi(fields[1])
		if errAdded != nil || errDeleted != nil {
			continue
		}
		path := strings.Join(fields[2:], " ")
		if path == "" {
			continue
		}
		metric := out[path]
		metric.Path = path
		metric.Added += added
		metric.Deleted += deleted
		metric.Changes += added + deleted
		out[path] = metric
	}
	return out
}

type ComplexityMetric struct {
	Path       string
	Function   string
	Cyclomatic int
}

func ParseLizardXML(data []byte) ([]ComplexityMetric, error) {
	var doc lizardDocument
	if err := xml.Unmarshal(data, &doc); err != nil {
		return nil, fmt.Errorf("%w: lizard xml: %v", ErrParseOutput, err)
	}
	out := make([]ComplexityMetric, 0)
	for _, measure := range doc.Measures {
		if measure.Type != "" && !strings.Contains(strings.ToLower(measure.Type), "function") {
			continue
		}
		for _, item := range measure.Items {
			metric := ComplexityMetric{Function: item.Name}
			for _, value := range item.Values {
				name := strings.ToLower(strings.TrimSpace(value.Name))
				text := strings.TrimSpace(value.Text)
				switch name {
				case "filename", "file", "path":
					metric.Path = text
				case "ccn", "cyclomatic complexity", "complexity":
					if n, err := strconv.Atoi(text); err == nil {
						metric.Cyclomatic = n
					}
				case "function", "name":
					if metric.Function == "" {
						metric.Function = text
					}
				}
			}
			if metric.Path == "" && looksPathLike(item.Name) {
				metric.Path = item.Name
			}
			if metric.Path != "" && metric.Cyclomatic > 0 {
				out = append(out, metric)
			}
		}
	}
	return out, nil
}

type LOCMetric struct {
	Language     string
	Files        int
	LOC          int
	EffectiveLOC int
}

func ParseSCCJSON(data []byte) ([]LOCMetric, error) {
	var rows []struct {
		Name       string `json:"Name"`
		Files      int    `json:"Files"`
		Code       int    `json:"Code"`
		Lines      int    `json:"Lines"`
		Complexity int    `json:"Complexity"`
	}
	if err := json.Unmarshal(data, &rows); err != nil {
		return nil, fmt.Errorf("%w: scc json: %v", ErrParseOutput, err)
	}
	out := make([]LOCMetric, 0, len(rows))
	for _, row := range rows {
		if row.Name == "" {
			continue
		}
		out = append(out, LOCMetric{
			Language:     row.Name,
			Files:        row.Files,
			LOC:          row.Lines,
			EffectiveLOC: row.Code,
		})
	}
	return out, nil
}

func ParseCoverageSummaryJSON(data []byte) ([]CoverageMetric, float64, error) {
	metrics, total, ok, err := parseIstanbulCoverage(data)
	if err != nil || ok {
		return metrics, total, err
	}
	metrics, total, ok, err = parseCoveragePyJSON(data)
	if err != nil || ok {
		return metrics, total, err
	}
	return nil, 0, fmt.Errorf("%w: unsupported coverage summary JSON", ErrParseOutput)
}

func commandPlan(ecosystem Ecosystem, worktree WorktreeSpec) []CommandSpec {
	churnAll := []string{"git", "log", "--since=30 days ago", "--pretty=format:", "--numstat"}
	churnTS := append(append([]string{}, churnAll...), "--", "*.ts", "*.tsx", "*.js", "*.jsx")
	churnPython := append(append([]string{}, churnAll...), "--", "*.py")
	churnRust := append(append([]string{}, churnAll...), "--", "*.rs")
	churnGo := append(append([]string{}, churnAll...), "--", "*.go")

	switch ecosystem {
	case EcosystemTS:
		return []CommandSpec{
			cmd("vitest_coverage", []string{"bun", "run", "vitest", "--coverage", "--coverage.reporter=json-summary", "--reporter=json"}, worktree.Path, []string{CapabilityCoverage, EcosystemTS.scopedCapability("coverage")}, []string{"coverage/coverage-summary.json"}, true, 0, 1),
			cmd("c8_coverage", []string{"bunx", "c8", "--reporter=json-summary", "--", "bun", "test"}, worktree.Path, []string{CapabilityCoverage, EcosystemTS.scopedCapability("coverage")}, []string{"coverage/coverage-summary.json"}, true, 0, 1),
			cmd("eslint_complexity", []string{"bunx", "eslint", "--format=json", "--rule", `{"complexity":["warn",10]}`, "."}, worktree.Path, []string{CapabilityComplexity, CapabilityLint, EcosystemTS.scopedCapability("complexity"), EcosystemTS.scopedCapability("lint")}, []string{"eslint.json"}, true, 0, 1),
			cmd("git_churn", churnTS, worktree.Path, []string{CapabilityChurn, EcosystemTS.scopedCapability("churn")}, nil, false, 0),
			cmd("tokei_loc", []string{"tokei", "--output", "json"}, worktree.Path, []string{CapabilityLOC}, []string{"tokei.json"}, false, 0),
		}
	case EcosystemPython:
		return []CommandSpec{
			cmd("pytest_cov", []string{"uv", "run", "pytest", "--cov", "--cov-report=json:coverage.json"}, worktree.Path, []string{CapabilityCoverage, CapabilityTestRuns, EcosystemPython.scopedCapability("coverage")}, []string{"coverage.json"}, true, 0, 1),
			cmd("coverage_unittest", []string{"python", "-m", "coverage", "run", "-m", "unittest", "discover"}, worktree.Path, []string{CapabilityCoverage, CapabilityTestRuns}, []string{".coverage"}, true, 0, 1),
			cmd("radon_cc", []string{"radon", "cc", "-j", "."}, worktree.Path, []string{CapabilityComplexity, EcosystemPython.scopedCapability("complexity")}, []string{"radon.json"}, false, 0),
			cmd("ruff_check", []string{"ruff", "check", "--format=json", "."}, worktree.Path, []string{CapabilityLint, EcosystemPython.scopedCapability("lint")}, []string{"ruff.json"}, false, 0, 1),
			cmd("lizard_complexity", []string{"lizard", "--xml"}, worktree.Path, []string{CapabilityComplexity, EcosystemPython.scopedCapability("complexity")}, []string{"lizard.xml"}, false, 0),
			cmd("git_churn", churnPython, worktree.Path, []string{CapabilityChurn, EcosystemPython.scopedCapability("churn")}, nil, false, 0),
		}
	case EcosystemRust:
		targetDir := filepath.Join(worktree.Root, "target")
		env := map[string]string{"CARGO_TARGET_DIR": targetDir}
		return []CommandSpec{
			cmdEnv("cargo_llvm_cov", []string{"cargo", "llvm-cov", "--workspace", "--json", "--output-path", "coverage.json"}, env, worktree.Path, []string{CapabilityCoverage, EcosystemRust.scopedCapability("coverage")}, []string{"coverage.json"}, true, 0, 1),
			cmdEnv("cargo_tarpaulin", []string{"cargo", "tarpaulin", "--workspace", "--out", "Json", "-o", "coverage.json"}, env, worktree.Path, []string{CapabilityCoverage, EcosystemRust.scopedCapability("coverage")}, []string{"coverage.json"}, true, 0, 1),
			cmdEnv("cargo_clippy", []string{"cargo", "clippy", "--workspace", "--message-format=json", "--", "-W", "clippy::cognitive_complexity"}, env, worktree.Path, []string{CapabilityComplexity, CapabilityLint, EcosystemRust.scopedCapability("complexity"), EcosystemRust.scopedCapability("lint")}, []string{"clippy.json"}, true, 0, 1),
			cmdEnv("cargo_nextest", []string{"cargo", "nextest", "run", "--workspace", "--message-format", "json"}, env, worktree.Path, []string{CapabilityTestRuns}, []string{"nextest.json"}, true, 0, 1),
			cmd("lizard_complexity", []string{"lizard", "--xml"}, worktree.Path, []string{CapabilityComplexity, EcosystemRust.scopedCapability("complexity")}, []string{"lizard.xml"}, false, 0),
			cmd("git_churn", churnRust, worktree.Path, []string{CapabilityChurn, EcosystemRust.scopedCapability("churn")}, nil, false, 0),
		}
	case EcosystemGo:
		return []CommandSpec{
			cmd("go_test_cover", []string{"go", "test", "-coverprofile=cover.out", "-covermode=atomic", "./..."}, worktree.Path, []string{CapabilityCoverage, CapabilityTestRuns, EcosystemGo.scopedCapability("coverage")}, []string{"cover.out"}, true, 0, 1),
			cmd("go_cover_func", []string{"go", "tool", "cover", "-func=cover.out"}, worktree.Path, []string{CapabilityCoverage, EcosystemGo.scopedCapability("coverage")}, []string{"cover.func.txt"}, false, 0),
			cmd("gocognit", []string{"gocognit", "-json", "."}, worktree.Path, []string{CapabilityComplexity, EcosystemGo.scopedCapability("complexity")}, []string{"gocognit.json"}, false, 0),
			cmd("gocyclo", []string{"gocyclo", "-over", "20", "."}, worktree.Path, []string{CapabilityComplexity, EcosystemGo.scopedCapability("complexity")}, []string{"gocyclo.txt"}, false, 0),
			cmd("golangci_lint", []string{"golangci-lint", "run", "--out-format", "json", "./..."}, worktree.Path, []string{CapabilityLint, EcosystemGo.scopedCapability("lint")}, []string{"golangci-lint.json"}, true, 0, 1),
			cmd("git_churn", churnGo, worktree.Path, []string{CapabilityChurn, EcosystemGo.scopedCapability("churn")}, nil, false, 0),
		}
	case EcosystemGeneric:
		return []CommandSpec{
			cmd("lizard_complexity", []string{"lizard", "--xml"}, worktree.Path, []string{CapabilityComplexity, EcosystemGeneric.scopedCapability("complexity")}, []string{"lizard.xml"}, false, 0),
			cmd("scc_loc", []string{"scc", "--format", "json", "--no-cocomo"}, worktree.Path, []string{CapabilityLOC, EcosystemGeneric.scopedCapability("loc")}, []string{"scc.json"}, false, 0),
			cmd("tokei_loc", []string{"tokei", "--output", "json"}, worktree.Path, []string{CapabilityLOC, EcosystemGeneric.scopedCapability("loc")}, []string{"tokei.json"}, false, 0),
			cmd("cloc_loc", []string{"cloc", "--json", "."}, worktree.Path, []string{CapabilityLOC, EcosystemGeneric.scopedCapability("loc")}, []string{"cloc.json"}, false, 0),
			cmd("git_churn", churnAll, worktree.Path, []string{CapabilityChurn, EcosystemGeneric.scopedCapability("churn")}, nil, false, 0),
		}
	default:
		return nil
	}
}

func cmd(label string, argv []string, workdir string, caps []string, produces []string, rch bool, exits ...int) CommandSpec {
	return cmdEnv(label, argv, nil, workdir, caps, produces, rch, exits...)
}

func cmdEnv(label string, argv []string, env map[string]string, workdir string, caps []string, produces []string, rch bool, exits ...int) CommandSpec {
	return CommandSpec{
		Label:        label,
		Argv:         append([]string(nil), argv...),
		Env:          cloneStringMap(env),
		WorkDir:      workdir,
		Produces:     append([]string(nil), produces...),
		Capabilities: append([]string(nil), caps...),
		AllowExit:    append([]int(nil), exits...),
		RCHPreferred: rch,
	}
}

func validateCommand(command CommandSpec, worktree WorktreeSpec) error {
	if command.Label == "" || len(command.Argv) == 0 {
		return fmt.Errorf("%w: empty command", ErrInvalidRequest)
	}
	if command.WorkDir == "" || samePath(command.WorkDir, worktree.Source) || !underPath(command.WorkDir, worktree.Root) {
		return fmt.Errorf("%w: command %s workdir %q", ErrActiveWorktree, command.Label, command.WorkDir)
	}
	return ValidateArgv(command.Argv)
}

func ValidateArgv(argv []string) error {
	if len(argv) == 0 {
		return fmt.Errorf("%w: empty argv", ErrUnsafeCommand)
	}
	switch filepath.Base(argv[0]) {
	case "sh", "bash", "zsh", "fish", "cmd", "powershell":
		return fmt.Errorf("%w: shell entrypoint %q", ErrUnsafeCommand, argv[0])
	}
	for _, arg := range argv {
		switch arg {
		case ";", "&&", "||", "|", ">", ">>", "<", "2>", "2>>":
			return fmt.Errorf("%w: shell token %q", ErrUnsafeCommand, arg)
		}
	}
	return nil
}

func capabilityFor(ecosystem Ecosystem, id string) capabilities.Capability {
	cap := capabilities.Capability{Status: capabilities.StatusOK, Transport: "worktree+stdio"}
	if ecosystem == EcosystemGeneric {
		switch id {
		case CapabilityCoverage, CapabilityTestRuns, EcosystemGeneric.scopedCapability("coverage"):
			return capabilities.Capability{
				Status:   capabilities.StatusMissing,
				Fallback: "language-specific health adapter",
				Notes:    "generic adapter intentionally avoids guessed coverage or test commands",
			}
		}
	}
	if id == CapabilityLOC || strings.HasSuffix(id, ".loc") {
		cap.Notes = "informational LOC metric; not a stage gate"
	}
	if id == CapabilityLint || strings.HasSuffix(id, ".lint") {
		cap.Notes = "lint findings are ingested into the finding ledger"
	}
	return cap
}

func summarize(files []FileSignals, tests TestRunSummary, thresholds HotspotThresholds) SummaryMetrics {
	var coverageSum float64
	var coverageCount int
	var lackingCoverage int
	var complexitySum int
	var complexityCount int
	var loc int
	var effectiveLOC int
	var churn7 int
	var churn30 int
	for _, file := range files {
		if file.CoveragePercent == nil {
			lackingCoverage++
		} else {
			coverageSum += *file.CoveragePercent
			coverageCount++
		}
		if file.Complexity > 0 {
			complexitySum += file.Complexity
			complexityCount++
		}
		loc += file.LOC
		effectiveLOC += file.EffectiveLOC
		churn7 += file.Churn7Days
		churn30 += file.Churn30Days
	}
	var coverageAvg float64
	if coverageCount > 0 {
		coverageAvg = coverageSum / float64(coverageCount)
	}
	var complexityAvg float64
	if complexityCount > 0 {
		complexityAvg = float64(complexitySum) / float64(complexityCount)
	}
	var lackingRatio float64
	if len(files) > 0 {
		lackingRatio = float64(lackingCoverage) / float64(len(files))
	}
	return SummaryMetrics{
		CoveragePercent:           coverageAvg,
		AverageComplexity:         complexityAvg,
		LOC:                       loc,
		EffectiveLOC:              effectiveLOC,
		Churn7Days:                churn7,
		Churn30Days:               churn30,
		HotspotCount:              len(ScoreHotspots(files, thresholds)),
		TestPassed:                tests.Passed,
		TestFailed:                tests.Failed,
		TestDurationMS:            tests.DurationMS,
		FilesLackingCoverageRatio: lackingRatio,
		ComplexityToCoverageDelta: complexityAvg - (coverageAvg / 10),
	}
}

func normalizeThresholds(thresholds HotspotThresholds) HotspotThresholds {
	if thresholds.Complexity <= 0 {
		thresholds.Complexity = DefaultHotspotComplexityThreshold
	}
	if thresholds.CoveragePercent <= 0 {
		thresholds.CoveragePercent = DefaultHotspotCoverageThreshold
	}
	return thresholds
}

func cleanSegment(field, value string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" || value == "." || value == ".." || strings.Contains(value, "/") || strings.Contains(value, `\`) {
		return "", fmt.Errorf("%w: invalid %s", ErrInvalidRequest, field)
	}
	return value, nil
}

func samePath(a, b string) bool {
	return filepath.Clean(a) == filepath.Clean(b)
}

func underPath(path, root string) bool {
	path = filepath.Clean(path)
	root = filepath.Clean(root)
	rel, err := filepath.Rel(root, path)
	return err == nil && rel != "." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) && rel != ".."
}

func parsePercent(s string) (float64, bool) {
	s = strings.TrimSpace(strings.TrimSuffix(s, "%"))
	v, err := strconv.ParseFloat(s, 64)
	return v, err == nil
}

func looksPathLike(s string) bool {
	return strings.Contains(s, "/") || strings.Contains(s, `\`) || strings.Contains(s, ".")
}

func clampScore(v float64) float64 {
	if v < 0 {
		return 0
	}
	if v > 10 {
		return 10
	}
	return v
}

func totalLint(files []FileSignals) int {
	total := 0
	for _, file := range files {
		total += file.LintCount
	}
	return total
}

func totalSecurity(files []FileSignals) int {
	total := 0
	for _, file := range files {
		total += file.SecurityCount
	}
	return total
}

func cloneStringMap(in map[string]string) map[string]string {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]string, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

type lizardDocument struct {
	Measures []lizardMeasure `xml:"measure"`
}

type lizardMeasure struct {
	Type  string       `xml:"type,attr"`
	Items []lizardItem `xml:"item"`
}

type lizardItem struct {
	Name   string        `xml:"name,attr"`
	Values []lizardValue `xml:"value"`
}

type lizardValue struct {
	Name string `xml:"name,attr"`
	Text string `xml:",chardata"`
}

func parseIstanbulCoverage(data []byte) ([]CoverageMetric, float64, bool, error) {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, 0, false, fmt.Errorf("%w: coverage json: %v", ErrParseOutput, err)
	}
	totalRaw, ok := raw["total"]
	if !ok {
		return nil, 0, false, nil
	}
	total, err := istanbulLinesPct(totalRaw)
	if err != nil {
		return nil, 0, true, err
	}
	metrics := make([]CoverageMetric, 0, len(raw)-1)
	keys := make([]string, 0, len(raw)-1)
	for key := range raw {
		if key != "total" {
			keys = append(keys, key)
		}
	}
	sort.Strings(keys)
	for _, key := range keys {
		pct, err := istanbulLinesPct(raw[key])
		if err != nil {
			continue
		}
		metrics = append(metrics, CoverageMetric{Path: key, CoveragePercent: pct})
	}
	return metrics, total, true, nil
}

func istanbulLinesPct(raw json.RawMessage) (float64, error) {
	var node struct {
		Lines struct {
			Pct float64 `json:"pct"`
		} `json:"lines"`
	}
	if err := json.Unmarshal(raw, &node); err != nil {
		return 0, fmt.Errorf("%w: istanbul coverage node: %v", ErrParseOutput, err)
	}
	return node.Lines.Pct, nil
}

func parseCoveragePyJSON(data []byte) ([]CoverageMetric, float64, bool, error) {
	var node struct {
		Files map[string]struct {
			Summary struct {
				PercentCovered float64 `json:"percent_covered"`
			} `json:"summary"`
		} `json:"files"`
		Totals struct {
			PercentCovered float64 `json:"percent_covered"`
		} `json:"totals"`
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&node); err != nil {
		var lax struct {
			Files map[string]struct {
				Summary struct {
					PercentCovered float64 `json:"percent_covered"`
				} `json:"summary"`
			} `json:"files"`
			Totals struct {
				PercentCovered float64 `json:"percent_covered"`
			} `json:"totals"`
		}
		if err := json.Unmarshal(data, &lax); err != nil {
			return nil, 0, false, fmt.Errorf("%w: coverage.py json: %v", ErrParseOutput, err)
		}
		node = lax
	}
	if len(node.Files) == 0 && node.Totals.PercentCovered == 0 {
		return nil, 0, false, nil
	}
	keys := make([]string, 0, len(node.Files))
	for key := range node.Files {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	metrics := make([]CoverageMetric, 0, len(keys))
	for _, key := range keys {
		metrics = append(metrics, CoverageMetric{
			Path:            key,
			CoveragePercent: node.Files[key].Summary.PercentCovered,
		})
	}
	return metrics, node.Totals.PercentCovered, true, nil
}
