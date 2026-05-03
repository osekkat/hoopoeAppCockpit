// Package fixtures indexes the Phase 0 fixture corpus for daemon mock mode.
package fixtures

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/hoopoe-cockpit/hoopoe/apps/daemon/internal/capabilities"
)

const Phase0Version = "phase0-2026-05-02"

type AdapterIndex struct {
	Scenario string   `json:"scenario"`
	Mode     string   `json:"mode"`
	Adapters []string `json:"adapters"`
}

type Snapshot struct {
	Meta     SnapshotMeta               `json:"meta"`
	Captures map[string]json.RawMessage `json:"captures"`
	Raw      map[string]json.RawMessage `json:"-"`
}

type SnapshotMeta struct {
	SnapshotVersion   string            `json:"snapshotVersion"`
	SnapshotSchemaURL string            `json:"snapshotSchemaUrl"`
	CapturedAt        string            `json:"capturedAt"`
	VPSID             string            `json:"vpsId"`
	Scenario          string            `json:"scenario"`
	ToolVersions      map[string]string `json:"toolVersions"`
	CaptureDurationMs int               `json:"captureDurationMs"`
	FixturesVersion   string            `json:"fixturesVersion"`
}

type ScenarioManifest struct {
	Scenario         string   `json:"scenario"`
	Mode             string   `json:"mode"`
	FixturesVersion  string   `json:"fixturesVersion"`
	CapturedAt       string   `json:"capturedAt"`
	FixtureRoot      string   `json:"fixtureRoot"`
	ScenarioDir      string   `json:"scenarioDir"`
	AdapterIndexPath string   `json:"adapterIndexPath"`
	SnapshotPath     string   `json:"snapshotPath"`
	Adapters         []string `json:"adapters"`
}

type Phase0Scenario struct {
	Manifest ScenarioManifest
	Index    AdapterIndex
	Snapshot Snapshot
}

type AdapterCapture struct {
	Tool         string                             `json:"tool"`
	Present      bool                               `json:"present"`
	BinPath      string                             `json:"binPath,omitempty"`
	Version      string                             `json:"version,omitempty"`
	SkipReason   string                             `json:"skipReason,omitempty"`
	Capabilities map[string]capabilities.Capability `json:"capabilities,omitempty"`
	Captures     map[string]json.RawMessage         `json:"captures,omitempty"`
	Errors       []string                           `json:"errors,omitempty"`
	CapturedAt   string                             `json:"capturedAt"`
	Raw          map[string]json.RawMessage         `json:"-"`
}

func DefaultPhase0Root() (string, error) {
	wd, err := os.Getwd()
	if err != nil {
		return "", fmt.Errorf("fixtures: get working directory: %w", err)
	}
	for dir := wd; ; dir = filepath.Dir(dir) {
		candidate := filepath.Join(dir, "packages", "fixtures", Phase0Version, "scenarios")
		if info, err := os.Stat(candidate); err == nil && info.IsDir() {
			return candidate, nil
		}
		next := filepath.Dir(dir)
		if next == dir {
			break
		}
	}
	return "", fmt.Errorf("fixtures: could not find %s scenarios from %s", Phase0Version, wd)
}

func LoadPhase0Scenario(root, scenario string) (*Phase0Scenario, error) {
	if scenario == "" {
		scenario = "fresh"
	}
	if root == "" {
		var err error
		root, err = DefaultPhase0Root()
		if err != nil {
			return nil, err
		}
	}
	scenarioDir := filepath.Join(root, scenario)
	indexPath := filepath.Join(scenarioDir, "adapter-index.json")
	snapshotPath := filepath.Join(scenarioDir, "snapshot.json")

	var index AdapterIndex
	if err := readJSON(indexPath, &index); err != nil {
		return nil, err
	}
	if index.Scenario == "" {
		index.Scenario = scenario
	}

	var snapshot Snapshot
	if err := readJSON(snapshotPath, &snapshot); err != nil {
		return nil, err
	}
	snapshot.Meta.Scenario = firstNonEmpty(snapshot.Meta.Scenario, scenario)
	snapshot.Meta.FixturesVersion = firstNonEmpty(snapshot.Meta.FixturesVersion, Phase0Version)

	adapters := append([]string(nil), index.Adapters...)
	sort.Strings(adapters)
	return &Phase0Scenario{
		Manifest: ScenarioManifest{
			Scenario:         scenario,
			Mode:             firstNonEmpty(index.Mode, "real-vps"),
			FixturesVersion:  snapshot.Meta.FixturesVersion,
			CapturedAt:       snapshot.Meta.CapturedAt,
			FixtureRoot:      root,
			ScenarioDir:      scenarioDir,
			AdapterIndexPath: indexPath,
			SnapshotPath:     snapshotPath,
			Adapters:         adapters,
		},
		Index:    index,
		Snapshot: snapshot,
	}, nil
}

func (s *Phase0Scenario) Adapter(tool string) (*AdapterCapture, error) {
	if s == nil {
		return nil, fmt.Errorf("fixtures: nil scenario")
	}
	path := filepath.Join(s.Manifest.ScenarioDir, "adapters", tool+".json")
	var capture AdapterCapture
	if err := readJSON(path, &capture); err != nil {
		return nil, err
	}
	if capture.Tool == "" {
		capture.Tool = tool
	}
	if capture.CapturedAt == "" {
		capture.CapturedAt = s.Manifest.CapturedAt
	}
	return &capture, nil
}

func (s *Phase0Scenario) CapabilityReports() ([]*capabilities.ToolReport, error) {
	if s == nil {
		return nil, fmt.Errorf("fixtures: nil scenario")
	}
	reports := make([]*capabilities.ToolReport, 0, len(s.Manifest.Adapters))
	for _, adapterName := range s.Manifest.Adapters {
		capture, err := s.Adapter(adapterName)
		if err != nil {
			return nil, err
		}
		toolID := normalizeToolID(capture.Tool)
		if !toolID.Valid() {
			continue
		}
		caps := capture.Capabilities
		if caps == nil {
			caps = map[string]capabilities.Capability{}
		}
		if !capture.Present && len(caps) == 0 {
			caps["__probe__"] = capabilities.Capability{
				Status: capabilities.StatusMissing,
				Notes:  firstNonEmpty(capture.SkipReason, "tool absent in fixture"),
			}
		}
		report := &capabilities.ToolReport{
			Tool:            toolID,
			Version:         capture.Version,
			Source:          "fixture",
			Capabilities:    caps,
			LastCheckedAt:   firstNonEmpty(capture.CapturedAt, s.Manifest.CapturedAt, time.Unix(0, 0).UTC().Format(time.RFC3339)),
			FixturesVersion: s.Manifest.FixturesVersion,
		}
		if err := report.Validate(); err != nil {
			return nil, fmt.Errorf("fixtures: capability report %s: %w", adapterName, err)
		}
		reports = append(reports, report)
	}
	return reports, nil
}

func readJSON(path string, target any) error {
	bytes, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("fixtures: read %s: %w", path, err)
	}
	if err := json.Unmarshal(bytes, target); err != nil {
		return fmt.Errorf("fixtures: parse %s: %w", path, err)
	}
	return nil
}

func normalizeToolID(tool string) capabilities.ToolID {
	if tool == "health" {
		return "health_generic"
	}
	return capabilities.ToolID(tool)
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}
