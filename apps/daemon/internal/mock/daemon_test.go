package mock

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/hoopoe-cockpit/hoopoe/apps/daemon/internal/api"
	"github.com/hoopoe-cockpit/hoopoe/apps/daemon/internal/capabilities"
	schemas "github.com/hoopoe-cockpit/hoopoe/packages/schemas/go"
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

	var version schemas.VersionResponse
	getJSON(t, server.URL+"/v1/version", &version)
	if version.Daemon.Commit == nil || *version.Daemon.Commit != "mock" || version.SchemaVersion != 1 {
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
	brReport := registry.Tools[capabilities.ToolBR]
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
	bvReport := registry.Tools[capabilities.ToolBV]
	if bvReport == nil {
		t.Fatal("bv fixture should produce a capability report")
	}
	if bvReport.Capabilities["__probe__"].Status != capabilities.StatusMissing {
		t.Fatalf("bv missing capability not reported: %+v", bvReport)
	}

	var jobsResponse struct {
		Items []struct {
			ID        string `json:"id"`
			Type      string `json:"type"`
			Status    string `json:"status"`
			Artifacts []struct {
				ID   string `json:"id"`
				Kind string `json:"kind"`
				URI  string `json:"uri"`
			} `json:"artifacts"`
		} `json:"items"`
	}
	getJSON(t, server.URL+"/v1/jobs", &jobsResponse)
	if len(jobsResponse.Items) != 1 || jobsResponse.Items[0].Type != "mock.flywheel.scenario" {
		t.Fatalf("unexpected jobs response: %+v", jobsResponse)
	}
	if !hasArtifact(jobsResponse.Items[0].Artifacts, "prepare_transcript") {
		t.Fatalf("mock prepare transcript artifact missing: %+v", jobsResponse.Items[0].Artifacts)
	}
	logChunk, err := daemon.Jobs.ReadLog(context.Background(), jobsResponse.Items[0].ID, 0, 1<<20)
	if err != nil {
		t.Fatalf("read mock job log: %v", err)
	}
	if !strings.Contains(string(logChunk.Data), "--- prepare/transcript.txt ---") {
		t.Fatalf("mock job log does not include prepare transcript: %q", string(logChunk.Data))
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

func hasArtifact(artifacts []struct {
	ID   string `json:"id"`
	Kind string `json:"kind"`
	URI  string `json:"uri"`
}, idFragment string) bool {
	for _, artifact := range artifacts {
		if strings.Contains(artifact.ID, idFragment) {
			return true
		}
	}
	return false
}

func phase0Root() string {
	return filepath.Join("..", "..", "..", "..", "packages", "fixtures", "phase0-2026-05-02", "scenarios")
}
