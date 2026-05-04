package fixtures

import (
	"path/filepath"
	"testing"

	"github.com/hoopoe-cockpit/hoopoe/apps/daemon/internal/capabilities"
)

func TestLoadPhase0Scenario(t *testing.T) {
	root := filepath.Join("..", "..", "..", "..", "packages", "fixtures", Phase0Version, "scenarios")
	scenario, err := LoadPhase0Scenario(root, "fresh")
	if err != nil {
		t.Fatal(err)
	}
	if scenario.Manifest.Scenario != "fresh" {
		t.Fatalf("scenario = %q, want fresh", scenario.Manifest.Scenario)
	}
	if scenario.Manifest.FixturesVersion != Phase0Version {
		t.Fatalf("fixturesVersion = %q, want %s", scenario.Manifest.FixturesVersion, Phase0Version)
	}
	if len(scenario.Manifest.Adapters) == 0 {
		t.Fatal("adapters are empty")
	}

	git, err := scenario.Adapter("git")
	if err != nil {
		t.Fatal(err)
	}
	if !git.Present {
		t.Fatal("git adapter should be present in fresh fixture")
	}
	if git.Capabilities["git.status.read"].Status != capabilities.StatusOK {
		t.Fatalf("git.status.read status = %q", git.Capabilities["git.status.read"].Status)
	}
}

func TestPhase0CapabilityReportsUseFixtureSource(t *testing.T) {
	root := filepath.Join("..", "..", "..", "..", "packages", "fixtures", Phase0Version, "scenarios")
	scenario, err := LoadPhase0Scenario(root, "fresh")
	if err != nil {
		t.Fatal(err)
	}
	reports, err := scenario.CapabilityReports()
	if err != nil {
		t.Fatal(err)
	}
	byTool := map[capabilities.ToolID]*capabilities.ToolReport{}
	for _, report := range reports {
		byTool[report.Tool] = report
	}
	if byTool[capabilities.ToolGit].Source != "fixture" {
		t.Fatalf("git source = %q, want fixture", byTool[capabilities.ToolGit].Source)
	}
	brReport := byTool[capabilities.ToolBR]
	if brReport == nil {
		t.Fatal("br fixture should produce a capability report")
	}
	if brReport.Capabilities["br.list.read"].Status != capabilities.StatusOK {
		t.Fatalf("br.list.read status = %q, want ok", brReport.Capabilities["br.list.read"].Status)
	}
	if brReport.Capabilities["br.sync.write"].Status != capabilities.StatusOK {
		t.Fatalf("br.sync.write status = %q, want ok", brReport.Capabilities["br.sync.write"].Status)
	}
	if _, ok := brReport.Capabilities["__probe__"]; ok {
		t.Fatalf("br __probe__ should be absent when real fixture capabilities are present: %+v", brReport)
	}
	bvReport := byTool[capabilities.ToolBV]
	if bvReport == nil {
		t.Fatal("bv fixture should produce a capability report")
	}
	if bvReport.Capabilities["__probe__"].Status != capabilities.StatusMissing {
		t.Fatalf("bv __probe__ status = %q, want missing", bvReport.Capabilities["__probe__"].Status)
	}
	if byTool["health_generic"] == nil {
		t.Fatal("health fixture should map to health_generic")
	}
}
