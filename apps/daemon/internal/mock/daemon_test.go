package mock

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/hoopoe-cockpit/hoopoe/apps/daemon/internal/api"
	"github.com/hoopoe-cockpit/hoopoe/apps/daemon/internal/capabilities"
)

func TestMockDaemonSmoke(t *testing.T) {
	now := func() time.Time { return time.Date(2026, 5, 3, 22, 11, 51, 0, time.UTC) }
	daemon, err := NewDaemon(Config{
		Scenario:    "fresh",
		FixtureRoot: phase0Root(),
		Build: api.BuildInfo{
			Version:    "test",
			Commit:     "mock",
			BuildDate:  "2026-05-03T22:11:51Z",
			APIVersion: "v1",
		},
		Now: now,
	})
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(daemon.Router())
	defer server.Close()

	var version api.BuildInfo
	getJSON(t, server.URL+"/v1/version", &version)
	if version.Commit != "mock" || version.APIVersion != "v1" {
		t.Fatalf("unexpected version payload: %+v", version)
	}

	var registry capabilities.CapabilityRegistry
	getJSON(t, server.URL+"/v1/capabilities", &registry)
	if registry.FixturesVersion != "phase0-2026-05-02" {
		t.Fatalf("fixturesVersion = %q", registry.FixturesVersion)
	}
	if registry.Tools[capabilities.ToolGit].Source != "fixture" {
		t.Fatalf("git source = %q, want fixture", registry.Tools[capabilities.ToolGit].Source)
	}
	if registry.Tools[capabilities.ToolBR].Capabilities["__probe__"].Status != capabilities.StatusMissing {
		t.Fatalf("br missing capability not reported: %+v", registry.Tools[capabilities.ToolBR])
	}

	var jobsResponse struct {
		Jobs []struct {
			ID     string `json:"id"`
			Kind   string `json:"kind"`
			Status string `json:"status"`
		} `json:"jobs"`
	}
	getJSON(t, server.URL+"/v1/jobs", &jobsResponse)
	if len(jobsResponse.Jobs) != 1 || jobsResponse.Jobs[0].Kind != "mock.flywheel.scenario" {
		t.Fatalf("unexpected jobs response: %+v", jobsResponse)
	}

	var replay struct {
		Events []struct {
			Type string `json:"type"`
		} `json:"events"`
	}
	getJSON(t, server.URL+"/v1/events/replay?channel=_system&sinceSequence=0", &replay)
	if len(replay.Events) == 0 || replay.Events[0].Type != "mock.scenario.loaded" {
		t.Fatalf("unexpected replay response: %+v", replay)
	}

	var scenario struct {
		Mock     bool `json:"mock"`
		Manifest struct {
			Scenario string `json:"scenario"`
		} `json:"manifest"`
	}
	getJSON(t, server.URL+"/v1/mock/scenario", &scenario)
	if !scenario.Mock || scenario.Manifest.Scenario != "fresh" {
		t.Fatalf("unexpected scenario response: %+v", scenario)
	}
}

func TestMockDaemonLoadsAllPhase0Scenarios(t *testing.T) {
	for _, scenario := range []string{"fresh", "active", "failure"} {
		t.Run(scenario, func(t *testing.T) {
			daemon, err := NewDaemon(Config{
				Scenario:    scenario,
				FixtureRoot: phase0Root(),
			})
			if err != nil {
				t.Fatal(err)
			}
			if daemon.Scenario.Manifest.Scenario != scenario {
				t.Fatalf("scenario = %q, want %s", daemon.Scenario.Manifest.Scenario, scenario)
			}
			if len(daemon.Scenario.Manifest.Adapters) == 0 {
				t.Fatal("adapter manifest is empty")
			}
		})
	}
}

func getJSON(t *testing.T, url string, target any) {
	t.Helper()
	resp, err := http.Get(url)
	if err != nil {
		t.Fatalf("GET %s: %v", url, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET %s status = %d", url, resp.StatusCode)
	}
	if err := json.NewDecoder(resp.Body).Decode(target); err != nil {
		t.Fatalf("decode %s: %v", url, err)
	}
}

func phase0Root() string {
	return filepath.Join("..", "..", "..", "..", "packages", "fixtures", "phase0-2026-05-02", "scenarios")
}
