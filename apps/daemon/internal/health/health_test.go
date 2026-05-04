package health

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/hoopoe-cockpit/hoopoe/apps/daemon/internal/capabilities"
)

func TestAdaptersPlanDedicatedWorktreeAndSafeArgv(t *testing.T) {
	home := t.TempDir()
	req := WorktreeRequest{
		ProjectID: "proj_1",
		RunID:     "run_1",
		RepoPath:  "/data/projects/hoopoe",
		Commit:    "abc123",
		HomeDir:   home,
	}

	for _, adapter := range Adapters() {
		plan, err := adapter.Plan(req)
		if err != nil {
			t.Fatalf("%s Plan: %v", adapter.Ecosystem, err)
		}
		wantRoot := filepath.Join(home, ".hoopoe", "work", "proj_1", "health", "run_1")
		wantRepo := filepath.Join(wantRoot, "repo")
		if plan.SchemaVersion != SchemaVersion || plan.Worktree.Root != wantRoot || plan.Worktree.Path != wantRepo {
			t.Fatalf("%s worktree = %+v", adapter.Ecosystem, plan.Worktree)
		}
		if reflect.DeepEqual(plan.Worktree.SetupArgv, []string{"git", "-C", req.RepoPath, "worktree", "add", "--detach", req.RepoPath, "abc123"}) {
			t.Fatalf("%s setup argv targets active repo", adapter.Ecosystem)
		}
		if len(plan.Commands) == 0 {
			t.Fatalf("%s produced no commands", adapter.Ecosystem)
		}
		for _, command := range plan.Commands {
			if command.WorkDir != wantRepo {
				t.Fatalf("%s command %s workdir = %q, want %q", adapter.Ecosystem, command.Label, command.WorkDir, wantRepo)
			}
			if command.WorkDir == req.RepoPath {
				t.Fatalf("%s command %s uses active repo", adapter.Ecosystem, command.Label)
			}
			if err := ValidateArgv(command.Argv); err != nil {
				t.Fatalf("%s command %s argv unsafe: %v", adapter.Ecosystem, command.Label, err)
			}
		}
	}
}

func TestBuildWorktreeSpecRejectsUnsafeInputs(t *testing.T) {
	_, err := BuildWorktreeSpec(WorktreeRequest{
		ProjectID: "proj/one",
		RunID:     "run_1",
		RepoPath:  "/data/projects/hoopoe",
		HomeDir:   "/home/agent",
	})
	if !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("project traversal err = %v, want ErrInvalidRequest", err)
	}
	_, err = BuildWorktreeSpec(WorktreeRequest{
		ProjectID: "proj_1",
		RunID:     "run_1",
		RepoPath:  "relative",
		HomeDir:   "/home/agent",
	})
	if !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("relative repo err = %v, want ErrInvalidRequest", err)
	}
}

func TestValidateArgvRejectsShellEntrypointsAndTokens(t *testing.T) {
	for _, argv := range [][]string{
		{"sh", "-c", "go test ./..."},
		{"bash", "-lc", "pytest"},
		{"go", "test", "&&", "echo", "done"},
		{"golangci-lint", "run", "|", "cat"},
	} {
		if err := ValidateArgv(argv); !errors.Is(err, ErrUnsafeCommand) {
			t.Fatalf("ValidateArgv(%v) err = %v, want ErrUnsafeCommand", argv, err)
		}
	}
}

func TestCapabilityReportsRegisterHealthToolsPerLanguage(t *testing.T) {
	now := time.Date(2026, 5, 4, 2, 0, 0, 0, time.UTC)
	reports := CapabilityReports(now)
	if len(reports) != 5 {
		t.Fatalf("reports len = %d, want 5", len(reports))
	}
	for _, report := range reports {
		if err := report.Validate(); err != nil {
			t.Fatalf("%s Validate: %v", report.Tool, err)
		}
		for _, capID := range []string{CapabilityCoverage, CapabilityComplexity, CapabilityChurn, CapabilityTestRuns} {
			cap, ok := report.Capabilities[capID]
			if !ok {
				t.Fatalf("%s missing %s", report.Tool, capID)
			}
			if report.Tool != capabilities.ToolID("health_generic") && cap.Status != capabilities.StatusOK {
				t.Fatalf("%s %s = %+v, want ok", report.Tool, capID, cap)
			}
		}
	}
	generic := CapabilityReport(EcosystemGeneric, now)
	if generic.Capabilities[CapabilityCoverage].Status != capabilities.StatusMissing {
		t.Fatalf("generic coverage = %+v, want missing", generic.Capabilities[CapabilityCoverage])
	}

	reg := capabilities.New("0.1.0")
	if err := RegisterStaticProbes(reg, func() time.Time { return now }); err != nil {
		t.Fatalf("RegisterStaticProbes: %v", err)
	}
	reg.Probe()
	snap := reg.Snapshot()
	for _, ecosystem := range AllEcosystems() {
		report := snap.Tools[ecosystem.ToolID()]
		if report == nil {
			t.Fatalf("registry missing %s", ecosystem.ToolID())
		}
		if report.Capabilities[CapabilityComplexity].Status != capabilities.StatusOK {
			t.Fatalf("%s complexity = %+v", ecosystem.ToolID(), report.Capabilities[CapabilityComplexity])
		}
	}
}

func TestDetectEcosystemsStableOrder(t *testing.T) {
	got := DetectEcosystems([]string{
		"cmd/server/main.go",
		"package.json",
		"src/lib.rs",
		"pyproject.toml",
		"README.md",
	})
	want := []Ecosystem{EcosystemTS, EcosystemPython, EcosystemRust, EcosystemGo, EcosystemGeneric}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("DetectEcosystems = %#v, want %#v", got, want)
	}
}

func TestCommandPlansCoverExpectedEcosystemSurfaces(t *testing.T) {
	home := t.TempDir()
	req := WorktreeRequest{ProjectID: "p", RunID: "r", RepoPath: "/data/projects/p", HomeDir: home}
	tests := map[Ecosystem][]string{
		EcosystemTS:      {"vitest_coverage", "c8_coverage", "eslint_complexity", "git_churn"},
		EcosystemPython:  {"pytest_cov", "coverage_unittest", "radon_cc", "ruff_check", "git_churn"},
		EcosystemRust:    {"cargo_llvm_cov", "cargo_tarpaulin", "cargo_clippy", "cargo_nextest", "git_churn"},
		EcosystemGo:      {"go_test_cover", "go_cover_func", "gocognit", "gocyclo", "golangci_lint", "git_churn"},
		EcosystemGeneric: {"lizard_complexity", "scc_loc", "tokei_loc", "cloc_loc", "git_churn"},
	}
	for ecosystem, labels := range tests {
		adapter, err := NewAdapter(ecosystem)
		if err != nil {
			t.Fatalf("NewAdapter(%s): %v", ecosystem, err)
		}
		plan, err := adapter.Plan(req)
		if err != nil {
			t.Fatalf("%s Plan: %v", ecosystem, err)
		}
		got := map[string]bool{}
		for _, command := range plan.Commands {
			got[command.Label] = true
		}
		for _, label := range labels {
			if !got[label] {
				t.Fatalf("%s missing command %s in %+v", ecosystem, label, got)
			}
		}
	}
}

func TestRustPlanIsolatesCargoTargetDirOutsideRepo(t *testing.T) {
	home := t.TempDir()
	adapter, err := NewAdapter(EcosystemRust)
	if err != nil {
		t.Fatal(err)
	}
	plan, err := adapter.Plan(WorktreeRequest{ProjectID: "p", RunID: "r", RepoPath: "/data/projects/p", HomeDir: home})
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}
	for _, command := range plan.Commands {
		if !strings.HasPrefix(command.Label, "cargo_") {
			continue
		}
		targetDir := command.Env["CARGO_TARGET_DIR"]
		if targetDir == "" {
			t.Fatalf("%s missing CARGO_TARGET_DIR", command.Label)
		}
		if strings.HasPrefix(filepath.Clean(targetDir), filepath.Clean(plan.Worktree.Path)+string(filepath.Separator)) {
			t.Fatalf("%s target dir inside repo: %s", command.Label, targetDir)
		}
	}
}

func TestHotspotScoringUsesDefaultThresholds(t *testing.T) {
	hotspots := ScoreHotspots([]FileSignals{
		{Path: "too-simple.go", Complexity: 19, CoveragePercent: pct(0), Churn30Days: 1000},
		{Path: "covered.go", Complexity: 40, CoveragePercent: pct(80), Churn30Days: 1000},
		{Path: "default-threshold.go", Complexity: 20, CoveragePercent: pct(59), Churn30Days: 0},
		{Path: "missing-coverage.go", Complexity: 25, CoveragePercent: nil, Churn30Days: 20},
	}, HotspotThresholds{})
	if len(hotspots) != 2 {
		t.Fatalf("hotspots len = %d, want 2: %+v", len(hotspots), hotspots)
	}
	if hotspots[0].Rank != 1 || hotspots[1].Rank != 2 {
		t.Fatalf("ranks = %+v", hotspots)
	}
	gotPaths := []string{hotspots[0].Path, hotspots[1].Path}
	wantPaths := []string{"missing-coverage.go", "default-threshold.go"}
	if !reflect.DeepEqual(gotPaths, wantPaths) {
		t.Fatalf("hotspot paths = %#v, want %#v", gotPaths, wantPaths)
	}
}

func TestBuildSnapshotCapturesAlwaysOnSummaryAndMetrics(t *testing.T) {
	snap := BuildSnapshot(SnapshotInput{
		ID:         "snap_1",
		ProjectID:  "proj_1",
		SnapshotAt: time.Date(2026, 5, 4, 2, 1, 0, 0, time.UTC),
		Files: []FileSignals{
			{Path: "a.go", CoveragePercent: pct(50), Complexity: 21, LOC: 100, EffectiveLOC: 70, Churn7Days: 3, Churn30Days: 10, LintCount: 1},
			{Path: "b.go", CoveragePercent: nil, Complexity: 8, LOC: 40, EffectiveLOC: 30, Churn7Days: 1, Churn30Days: 5, SecurityCount: 1},
		},
		Tests: TestRunSummary{Passed: 9, Failed: 1, DurationMS: 1234},
	})
	if snap.SchemaVersion != SchemaVersion || snap.ID != "snap_1" || snap.ProjectID != "proj_1" {
		t.Fatalf("snapshot identity = %+v", snap)
	}
	if snap.Summary.LOC != 140 || snap.Summary.EffectiveLOC != 100 || snap.Summary.Churn7Days != 4 || snap.Summary.Churn30Days != 15 {
		t.Fatalf("summary counts = %+v", snap.Summary)
	}
	if snap.Summary.TestPassed != 9 || snap.Summary.TestFailed != 1 || snap.Summary.TestDurationMS != 1234 {
		t.Fatalf("test summary = %+v", snap.Summary)
	}
	if snap.Summary.FilesLackingCoverageRatio != 0.5 {
		t.Fatalf("lacking coverage ratio = %v", snap.Summary.FilesLackingCoverageRatio)
	}
	dims := map[Dimension]bool{}
	for _, metric := range snap.Files {
		dims[metric.Dimension] = true
	}
	for _, dim := range []Dimension{DimensionCoverage, DimensionComplexity, DimensionChurn, DimensionHotspot, DimensionLint, DimensionSecurity} {
		if !dims[dim] {
			t.Fatalf("snapshot missing file metric dimension %s in %+v", dim, snap.Files)
		}
	}
}

func TestParsersNormalizeGoldenLikeOutputs(t *testing.T) {
	coverage, total, err := ParseGoCoverFunc([]byte("github.com/acme/p/a.go:10:\tA\t50.0%\ngithub.com/acme/p/b.go:22:\tB\t100.0%\ntotal:\t(statements)\t75.0%\n"))
	if err != nil {
		t.Fatalf("ParseGoCoverFunc: %v", err)
	}
	if total != 75 || len(coverage) != 2 || coverage[0].Path != "github.com/acme/p/a.go" || coverage[0].CoveragePercent != 50 {
		t.Fatalf("go coverage = %+v total=%v", coverage, total)
	}

	churn := ParseChurnNumstat([]byte("10\t2\tsrc/a.go\n-\t-\tassets/logo.png\n3\t7\tsrc/a.go\n1\t1\tpath with spaces.go\n"))
	if churn["src/a.go"].Changes != 22 || churn["path with spaces.go"].Changes != 2 {
		t.Fatalf("churn = %+v", churn)
	}

	complexity, err := ParseLizardXML([]byte(`<cppncss><measure type="Function"><item name="Build"><value name="Filename">src/a.go</value><value name="CCN">21</value></item></measure></cppncss>`))
	if err != nil {
		t.Fatalf("ParseLizardXML: %v", err)
	}
	if len(complexity) != 1 || complexity[0].Path != "src/a.go" || complexity[0].Cyclomatic != 21 {
		t.Fatalf("complexity = %+v", complexity)
	}

	loc, err := ParseSCCJSON([]byte(`[{"Name":"Go","Files":3,"Lines":120,"Code":90,"Complexity":12}]`))
	if err != nil {
		t.Fatalf("ParseSCCJSON: %v", err)
	}
	if len(loc) != 1 || loc[0].Language != "Go" || loc[0].LOC != 120 || loc[0].EffectiveLOC != 90 {
		t.Fatalf("loc = %+v", loc)
	}

	istanbul, total, err := ParseCoverageSummaryJSON([]byte(`{"total":{"lines":{"pct":66.6}},"src/a.ts":{"lines":{"pct":50}}}`))
	if err != nil {
		t.Fatalf("ParseCoverageSummaryJSON istanbul: %v", err)
	}
	if total != 66.6 || len(istanbul) != 1 || istanbul[0].Path != "src/a.ts" {
		t.Fatalf("istanbul = %+v total=%v", istanbul, total)
	}

	pycov, total, err := ParseCoverageSummaryJSON([]byte(`{"files":{"pkg/a.py":{"summary":{"percent_covered":87.5}}},"totals":{"percent_covered":87.5}}`))
	if err != nil {
		t.Fatalf("ParseCoverageSummaryJSON coverage.py: %v", err)
	}
	if total != 87.5 || len(pycov) != 1 || pycov[0].Path != "pkg/a.py" {
		t.Fatalf("coverage.py = %+v total=%v", pycov, total)
	}
}

func TestGoldenOutputStubFixtureKeepsHealthCapabilityVocabulary(t *testing.T) {
	fixturePath := findRepoFile(t, filepath.Join("packages", "fixtures", "golden-outputs", "health", "normal.json"))
	raw, err := os.ReadFile(fixturePath)
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	var fixture struct {
		Capabilities map[string]struct {
			Status string `json:"status"`
			Notes  string `json:"notes"`
		} `json:"capabilities"`
	}
	if err := json.Unmarshal(raw, &fixture); err != nil {
		t.Fatalf("decode fixture: %v", err)
	}
	if fixture.Capabilities[CapabilityCoverage].Status != string(capabilities.StatusUntested) {
		t.Fatalf("fixture coverage = %+v", fixture.Capabilities[CapabilityCoverage])
	}
	if fixture.Capabilities[CapabilityComplexity].Status != string(capabilities.StatusUntested) {
		t.Fatalf("fixture complexity = %+v", fixture.Capabilities[CapabilityComplexity])
	}
}

func pct(v float64) *float64 {
	return &v
}

func findRepoFile(t *testing.T, rel string) string {
	t.Helper()
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 8; i++ {
		candidate := filepath.Join(wd, rel)
		if _, err := os.Stat(candidate); err == nil {
			return candidate
		}
		wd = filepath.Dir(wd)
	}
	t.Fatalf("could not find %s", rel)
	return ""
}
