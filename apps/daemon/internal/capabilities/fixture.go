package capabilities

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// FixtureMeta is the subset of `packages/fixtures/scenarios/<id>/meta.json`
// the capability registry consumes. The full file may carry additional
// scenario fields; only these are load-bearing for ToolReport composition.
type FixtureMeta struct {
	Scenario        string `json:"scenario"`
	FixturesVersion string `json:"fixturesVersion"`
	CapturedAt      string `json:"capturedAt"`
	Source          string `json:"source"`
}

// LoadCapabilitySnapshot reads the per-tool capabilities map from
// `<scenarioDir>/capabilities.json` and the registry-level fixture metadata
// from `<scenarioDir>/meta.json`, then composes one ToolReport per tool.
//
// Fixture shape (per BrownStone alignment in hoopoe-phase2 thread):
//
//	{
//	  "git": { "git.status.read": {"status":"ok"}, ... },
//	  "ntm": { ... },
//	  ...
//	}
//
// The fixture is the *capabilities* sub-object of ToolReport; this loader
// wraps each tool's entry with `source:"fixture"`, `version:""`,
// `lastCheckedAt:meta.capturedAt`, `fixturesVersion:meta.fixturesVersion`.
//
// Unknown tool keys (e.g., a future tool added to the fixture before the
// closed ToolID list catches up) are skipped with a warning so the loader
// never fails open during corpus migration.
func LoadCapabilitySnapshot(scenarioDir string) ([]*ToolReport, *FixtureMeta, error) {
	metaPath := filepath.Join(scenarioDir, "meta.json")
	capPath := filepath.Join(scenarioDir, "capabilities.json")

	metaBytes, err := os.ReadFile(metaPath)
	if err != nil {
		return nil, nil, fmt.Errorf("capabilities: read fixture meta %s: %w", metaPath, err)
	}
	var meta FixtureMeta
	if err := json.Unmarshal(metaBytes, &meta); err != nil {
		return nil, nil, fmt.Errorf("capabilities: parse fixture meta %s: %w", metaPath, err)
	}
	if meta.FixturesVersion == "" {
		return nil, nil, fmt.Errorf("capabilities: fixture meta %s missing fixturesVersion", metaPath)
	}
	if meta.CapturedAt == "" {
		// Fall back to a deterministic synthetic capture time so reports
		// remain comparable. Real fixtures always carry capturedAt.
		meta.CapturedAt = time.Unix(0, 0).UTC().Format(time.RFC3339)
	}

	capBytes, err := os.ReadFile(capPath)
	if err != nil {
		return nil, nil, fmt.Errorf("capabilities: read fixture capabilities %s: %w", capPath, err)
	}
	var raw map[string]map[string]Capability
	if err := json.Unmarshal(capBytes, &raw); err != nil {
		return nil, nil, fmt.Errorf("capabilities: parse fixture capabilities %s: %w", capPath, err)
	}

	reports := make([]*ToolReport, 0, len(raw))
	for toolKey, capabilities := range raw {
		toolID := ToolID(toolKey)
		// The fixture historically uses the bare tool name 'health'; map it
		// to 'health_generic' so the closed ToolID validator accepts it. A
		// per-language fixture corpus (health_ts/py/rs/go) supersedes this
		// when those snapshots land.
		if toolID == "health" {
			toolID = "health_generic"
		}
		if !toolID.Valid() {
			// Skip silently — corpus may contain forward-compatible entries.
			// Drift detection (hp-q3t) flags these; we don't fail loading.
			continue
		}
		// Empty maps are valid (the corpus has `"caut": {}` for tools whose
		// capabilities haven't been probed yet). Keep the tool in the
		// registry so consumers can distinguish "no probe" from "probe found
		// nothing".
		if capabilities == nil {
			capabilities = map[string]Capability{}
		}
		report := &ToolReport{
			Tool:            toolID,
			Version:         "",
			Source:          "fixture",
			Capabilities:    capabilities,
			LastCheckedAt:   meta.CapturedAt,
			FixturesVersion: meta.FixturesVersion,
		}
		if err := report.Validate(); err != nil {
			return nil, nil, fmt.Errorf("capabilities: invalid fixture report for %s: %w", toolID, err)
		}
		reports = append(reports, report)
	}
	return reports, &meta, nil
}

// LoadFixtureBacked populates the Registry from a fixture scenario directory
// in one call. After this, /v1/capabilities will reflect the snapshot until
// a probe rewrites it. Used by mock-flywheel mode (hp-dr8).
func (r *Registry) LoadFixtureBacked(scenarioDir string) error {
	reports, meta, err := LoadCapabilitySnapshot(scenarioDir)
	if err != nil {
		return err
	}
	r.SetFixturesVersion(meta.FixturesVersion)
	for _, report := range reports {
		if err := r.SetReport(report); err != nil {
			return err
		}
	}
	return nil
}
