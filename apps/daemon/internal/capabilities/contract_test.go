package capabilities

import (
	"encoding/json"
	"testing"
	"time"
)

// JSON contract tests pinning the daemon's CapabilityRegistry / ToolReport /
// Capability output to the wire shape declared in
// `packages/schemas/openapi.yaml` (and now generated to
// `packages/schemas/go/schemas.gen.go`). The two surfaces must agree byte-
// for-byte; until the daemon imports the generated types directly (blocked
// on apps/daemon/go.mod gaining a replace directive — FuchsiaPond's
// domain, hp-38d), these tests guard the contract by hand.
//
// When apps/daemon imports `github.com/hoopoe-cockpit/hoopoe/packages/schemas/go`
// directly, these tests stay valid: they assert json shape, not Go-type
// identity. They will fail loudly if the wire format ever drifts.

func TestJSONShape_Capability(t *testing.T) {
	got := Capability{
		Status:    StatusDegraded,
		Fallback:  "tmux capture-pane",
		Transport: "stdio",
		Notes:     "ntm panes endpoint missing on 0.4.x",
	}
	bytes, err := json.Marshal(got)
	if err != nil {
		t.Fatal(err)
	}

	var raw map[string]any
	if err := json.Unmarshal(bytes, &raw); err != nil {
		t.Fatal(err)
	}

	// Required field per OpenAPI.
	if raw["status"] != "degraded" {
		t.Errorf("status=%v", raw["status"])
	}
	// Optional fields present when set.
	if raw["fallback"] != "tmux capture-pane" {
		t.Errorf("fallback=%v", raw["fallback"])
	}
	if raw["transport"] != "stdio" {
		t.Errorf("transport=%v", raw["transport"])
	}
	if raw["notes"] != "ntm panes endpoint missing on 0.4.x" {
		t.Errorf("notes=%v", raw["notes"])
	}
}

func TestJSONShape_CapabilityOmitsEmptyOptionals(t *testing.T) {
	// Optional fields must be omitted (not serialized as empty strings) so
	// the JSON matches OpenAPI's `*string` pointer-omitempty semantics.
	got := Capability{Status: StatusOK}
	bytes, err := json.Marshal(got)
	if err != nil {
		t.Fatal(err)
	}
	str := string(bytes)
	if str != `{"status":"ok"}` {
		t.Errorf("Capability with only status should serialize to %q, got %q",
			`{"status":"ok"}`, str)
	}
}

func TestJSONShape_ToolReport(t *testing.T) {
	got := ToolReport{
		Tool:    ToolGit,
		Version: "2.40.0",
		Source:  "CLI",
		Capabilities: map[string]Capability{
			"git.status.read": {Status: StatusOK},
			"git.push":        {Status: StatusBlockedByPolicy},
		},
		LastCheckedAt:   "2026-05-02T23:29:34Z",
		FixturesVersion: "phase0-2026-05-02",
	}
	bytes, err := json.Marshal(got)
	if err != nil {
		t.Fatal(err)
	}
	var raw map[string]any
	if err := json.Unmarshal(bytes, &raw); err != nil {
		t.Fatal(err)
	}

	// Field names must exactly match the OpenAPI schema.
	wantFields := []string{"tool", "version", "source", "capabilities", "lastCheckedAt", "fixturesVersion"}
	for _, f := range wantFields {
		if _, ok := raw[f]; !ok {
			t.Errorf("ToolReport missing required field %q in JSON output", f)
		}
	}
	if raw["tool"] != "git" {
		t.Errorf("tool=%v", raw["tool"])
	}
	caps, ok := raw["capabilities"].(map[string]any)
	if !ok {
		t.Fatalf("capabilities not a map: %T", raw["capabilities"])
	}
	gitPush, ok := caps["git.push"].(map[string]any)
	if !ok {
		t.Fatalf("git.push not a map: %T", caps["git.push"])
	}
	if gitPush["status"] != "blocked-by-policy" {
		t.Errorf("git.push.status=%v", gitPush["status"])
	}
}

func TestJSONShape_CapabilityRegistry(t *testing.T) {
	r := New("0.1.0")
	r.SetClock(fixedClock("2026-05-02T23:29:34Z"))
	r.SetFixturesVersion("phase0-2026-05-02")
	if err := r.SetReport(okReport(ToolGit, "git.status.read")); err != nil {
		t.Fatal(err)
	}
	bytes, err := json.Marshal(r.Snapshot())
	if err != nil {
		t.Fatal(err)
	}
	var raw map[string]any
	if err := json.Unmarshal(bytes, &raw); err != nil {
		t.Fatal(err)
	}

	wantFields := []string{"schemaVersion", "snapshotAt", "daemonApiVersion", "fixturesVersion", "tools"}
	for _, f := range wantFields {
		if _, ok := raw[f]; !ok {
			t.Errorf("CapabilityRegistry missing required field %q", f)
		}
	}
	if raw["schemaVersion"].(float64) != 1 {
		t.Errorf("schemaVersion=%v", raw["schemaVersion"])
	}
	tools, ok := raw["tools"].(map[string]any)
	if !ok {
		t.Fatalf("tools not a map (expected ToolId-keyed object)")
	}
	if _, ok := tools["git"]; !ok {
		t.Errorf("tools missing git report")
	}
}

func TestJSONShape_CompatibilityReport(t *testing.T) {
	r := New("0.1.0")
	r.SetClock(fixedClock("2026-05-02T23:29:34Z"))
	r.SetFixturesVersion("phase0-2026-05-02")
	if err := r.SetReport(okReport(ToolGit, "git.status.read")); err != nil {
		t.Fatal(err)
	}
	composer := StaticCompatibilityComposer{
		MinDesktopVersion: "0.1.0",
		EventSchemaVersions: map[string]int{
			"project": 1,
			"swarm":   1,
		},
		Migration: MigrationState{
			SchemaVersion: 7,
			AppliedAt:     "2026-05-02T23:00:00Z",
			Pending:       []string{},
			Phase:         MigrationIdle,
		},
	}
	report := composer.Compose(r.Snapshot())
	bytes, err := json.Marshal(report)
	if err != nil {
		t.Fatal(err)
	}
	var raw map[string]any
	if err := json.Unmarshal(bytes, &raw); err != nil {
		t.Fatal(err)
	}

	// Top-level fields per OpenAPI's CompatibilityReport.
	wantFields := []string{
		"schemaVersion", "daemonApiVersion", "minDesktopVersion",
		"eventSchemaVersions", "migrationState", "capabilities",
	}
	for _, f := range wantFields {
		if _, ok := raw[f]; !ok {
			t.Errorf("CompatibilityReport missing required field %q", f)
		}
	}
	migration, ok := raw["migrationState"].(map[string]any)
	if !ok {
		t.Fatalf("migrationState not a map (expected MigrationState object)")
	}
	for _, f := range []string{"schemaVersion", "appliedAt", "pending"} {
		if _, ok := migration[f]; !ok {
			t.Errorf("migrationState missing required field %q", f)
		}
	}
	if migration["phase"] != "idle" {
		t.Errorf("migrationState.phase=%v", migration["phase"])
	}
}

func TestJSONShape_MigrationStateOmitsPhaseWhenEmpty(t *testing.T) {
	// `phase` is optional in OpenAPI (`*MigrationStatePhase`); empty values
	// must be omitted, not serialized as `""`.
	m := MigrationState{
		SchemaVersion: 1,
		AppliedAt:     "2026-05-02T23:00:00Z",
		Pending:       []string{},
		// Phase intentionally zero.
	}
	bytes, err := json.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	var raw map[string]any
	if err := json.Unmarshal(bytes, &raw); err != nil {
		t.Fatal(err)
	}
	if _, present := raw["phase"]; present {
		t.Errorf("MigrationState.phase should be omitted when empty, raw=%v", raw)
	}
}

// TestJSONShape_TimestampsAreRFC3339 pins the daemon's string-formatted
// timestamps to the format OpenAPI declares (date-time → RFC3339).
func TestJSONShape_TimestampsAreRFC3339(t *testing.T) {
	r := New("0.1.0")
	r.SetClock(fixedClock("2026-05-02T23:29:34Z"))
	if err := r.SetReport(okReport(ToolGit, "git.status.read")); err != nil {
		t.Fatal(err)
	}
	snap := r.Snapshot()
	if _, err := time.Parse(time.RFC3339, snap.SnapshotAt); err != nil {
		t.Errorf("snapshotAt %q not RFC3339: %v", snap.SnapshotAt, err)
	}
	for _, report := range snap.Tools {
		if _, err := time.Parse(time.RFC3339, report.LastCheckedAt); err != nil {
			t.Errorf("lastCheckedAt %q not RFC3339: %v", report.LastCheckedAt, err)
		}
	}
}
