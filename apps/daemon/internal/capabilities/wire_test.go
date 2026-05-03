// wire_test.go — hp-r3i DOD #10: generated Go types compile in apps/daemon
// and are consumed in ≥1 handler/test.
//
// This file imports `schemas "github.com/hoopoe-cockpit/hoopoe/packages/
// schemas/go"` and asserts the daemon's hand-rolled `ToolReport` /
// `CompatibilityReport` JSON wire shape is byte-equivalent to the generated
// schemas.* shapes. The contract test in contract_test.go pins the wire
// shape against the OpenAPI spec; this test pins the same wire shape
// against the GENERATED Go bindings of that spec — closing the drift
// loop end-to-end:
//
//   openapi.yaml  ──oapi-codegen──▶  schemas.gen.go (canonical)
//                                              ▲
//                                              │ this test
//                                              ▼
//   apps/daemon/internal/capabilities/types.go (hand-rolled, swap-eligible)
//
// When WhiteCreek (hp-r33 owner) swaps types.go to import from `schemas`,
// this test becomes a tautology and can be deleted (or kept as a smoke).
package capabilities

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	schemas "github.com/hoopoe-cockpit/hoopoe/packages/schemas/go"
)

func TestToolReportWireShapeMatchesGeneratedSchema(t *testing.T) {
	t.Parallel()

	// Build a populated daemon-side ToolReport using the hand-rolled types.
	hand := ToolReport{
		Tool:    ToolNTM,
		Version: "0.4.2",
		Source:  "ntm serve",
		Capabilities: map[string]Capability{
			"ntm.sessions.list": {Status: StatusOK},
			"ntm.panes.stream": {
				Status:    StatusDegraded,
				Fallback:  "tmux capture last",
				Transport: "stdio",
				Notes:     "ntm serve unreachable; fallback active",
			},
			"ntm.serve.rest": {Status: StatusUntested},
		},
		LastCheckedAt:   "2026-05-04T00:00:00Z",
		FixturesVersion: "phase0-2026-04-30",
	}

	wire, err := json.Marshal(hand)
	if err != nil {
		t.Fatalf("marshal hand-rolled ToolReport: %v", err)
	}

	var gen schemas.ToolReport
	if err := json.Unmarshal(wire, &gen); err != nil {
		t.Fatalf("unmarshal into generated ToolReport (drift): %v", err)
	}

	if string(gen.Tool) != string(hand.Tool) {
		t.Fatalf("tool drift: hand=%q gen=%q", hand.Tool, gen.Tool)
	}
	if gen.Version != hand.Version {
		t.Fatalf("version drift: hand=%q gen=%q", hand.Version, gen.Version)
	}
	if gen.Source != hand.Source {
		t.Fatalf("source drift: hand=%q gen=%q", hand.Source, gen.Source)
	}
	if gen.FixturesVersion != hand.FixturesVersion {
		t.Fatalf("fixturesVersion drift: hand=%q gen=%q", hand.FixturesVersion, gen.FixturesVersion)
	}
	// Generated `LastCheckedAt` is a `time.Time` — comparing the formatted
	// RFC3339 string is enough for wire-equivalence here.
	if gen.LastCheckedAt.Format("2006-01-02T15:04:05Z") != hand.LastCheckedAt {
		t.Fatalf("lastCheckedAt drift: hand=%q gen=%q", hand.LastCheckedAt, gen.LastCheckedAt)
	}
	if len(gen.Capabilities) != len(hand.Capabilities) {
		t.Fatalf("capabilities count drift: hand=%d gen=%d", len(hand.Capabilities), len(gen.Capabilities))
	}

	// Untested case: schemas uses pointer types for optional fields.
	rest, ok := gen.Capabilities["ntm.serve.rest"]
	if !ok {
		t.Fatalf("ntm.serve.rest missing in generated ToolReport")
	}
	if string(rest.Status) != "untested" {
		t.Fatalf("untested status drift: %q", rest.Status)
	}

	// Degraded with fallback + transport + notes (all optional → pointers).
	stream, ok := gen.Capabilities["ntm.panes.stream"]
	if !ok {
		t.Fatalf("ntm.panes.stream missing")
	}
	if string(stream.Status) != "degraded" {
		t.Fatalf("expected degraded, got %q", stream.Status)
	}
	if stream.Fallback == nil || *stream.Fallback != "tmux capture last" {
		t.Fatalf("fallback drift: got %v", stream.Fallback)
	}
	if stream.Transport == nil || string(*stream.Transport) != "stdio" {
		t.Fatalf("transport drift: got %v", stream.Transport)
	}
	if stream.Notes == nil || *stream.Notes != "ntm serve unreachable; fallback active" {
		t.Fatalf("notes drift: got %v", stream.Notes)
	}
}

func TestCapabilityStatusEnumIsFiveValued(t *testing.T) {
	t.Parallel()

	want := []schemas.CapabilityStatus{
		schemas.CapabilityStatusOk,
		schemas.CapabilityStatusDegraded,
		schemas.CapabilityStatusMissing,
		schemas.CapabilityStatusBlockedByPolicy,
		schemas.CapabilityStatusUntested,
	}
	for _, s := range want {
		if !s.Valid() {
			t.Fatalf("expected status %q to be Valid()", s)
		}
	}
	if schemas.CapabilityStatus("nope").Valid() {
		t.Fatalf("expected unknown status to be invalid")
	}
}

func TestStaticCompatibilityComposerProducesGeneratedShape(t *testing.T) {
	t.Parallel()

	// Build a registry snapshot via the daemon's hand-rolled types.
	reg := New("0.1.0")
	frozen := time.Date(2026, 5, 4, 0, 0, 0, 0, time.UTC)
	reg.SetClock(func() time.Time { return frozen })
	reg.SetFixturesVersion("phase0-2026-04-30")
	if err := reg.SetReport(&ToolReport{
		Tool:    ToolGit,
		Version: "2.45.0",
		Source:  "CLI",
		Capabilities: map[string]Capability{
			"git.status.read": {Status: StatusOK},
		},
		LastCheckedAt:   "2026-05-04T00:00:00Z",
		FixturesVersion: "phase0-2026-04-30",
	}); err != nil {
		t.Fatalf("SetReport: %v", err)
	}

	composer := StaticCompatibilityComposer{
		MinDesktopVersion: "0.1.0",
		EventSchemaVersions: map[string]int{
			"_system": 1,
		},
		Migration: MigrationState{
			SchemaVersion: 0,
			AppliedAt:     "2026-05-04T00:00:00Z",
			Pending:       []string{},
			Phase:         "idle",
		},
	}

	report := composer.Compose(reg.Snapshot())
	if report == nil {
		t.Fatalf("composer returned nil")
	}

	wire, err := json.Marshal(report)
	if err != nil {
		t.Fatalf("marshal composer output: %v", err)
	}
	var gen schemas.CompatibilityReport
	if err := json.Unmarshal(wire, &gen); err != nil {
		t.Fatalf("unmarshal into generated CompatibilityReport (drift): %v", err)
	}

	if gen.DaemonApiVersion != "0.1.0" {
		t.Fatalf("daemonApiVersion drift: %q", gen.DaemonApiVersion)
	}
	if gen.MinDesktopVersion != "0.1.0" {
		t.Fatalf("minDesktopVersion drift: %q", gen.MinDesktopVersion)
	}
	if gen.MigrationState.SchemaVersion != 0 {
		t.Fatalf("migrationState.schemaVersion drift: %d", gen.MigrationState.SchemaVersion)
	}
	if gen.MigrationState.Phase == nil || string(*gen.MigrationState.Phase) != "idle" {
		t.Fatalf("migrationState.phase drift: got %v", gen.MigrationState.Phase)
	}
	if got := gen.EventSchemaVersions["_system"]; got != 1 {
		t.Fatalf("eventSchemaVersions._system drift: %d", got)
	}
	if gen.Capabilities.Tools == nil {
		t.Fatalf("capabilities.tools missing")
	}
	gitReport, ok := gen.Capabilities.Tools["git"]
	if !ok {
		t.Fatalf("git tool report missing in generated snapshot")
	}
	if status := gitReport.Capabilities["git.status.read"].Status; string(status) != "ok" {
		t.Fatalf("git.status.read drift: %q", status)
	}
}

// TestSlashV1CompatibilityServesSpecShape — hp-r3i DOD #11: /v1/compatibility
// returns versions matching the spec on a smoke run.
//
// Mounts HandleCompatibility on httptest.Server, hits it with http.Get,
// decodes the response into the GENERATED schemas.CompatibilityReport, and
// asserts the wire shape. This is the "smoke run" of the actual handler;
// full chi-router mounting in apps/daemon/internal/api/router.go is a
// follow-up (delegated to the next agent touching that file — see Agent
// Mail msg #175).
func TestSlashV1CompatibilityServesSpecShape(t *testing.T) {
	t.Parallel()

	reg := New("0.1.0")
	frozen := time.Date(2026, 5, 4, 0, 0, 0, 0, time.UTC)
	reg.SetClock(func() time.Time { return frozen })
	reg.SetFixturesVersion("phase0-2026-04-30")
	if err := reg.SetReport(&ToolReport{
		Tool:    ToolGit,
		Version: "2.45.0",
		Source:  "CLI",
		Capabilities: map[string]Capability{
			"git.status.read": {Status: StatusOK},
			"git.push":        {Status: StatusBlockedByPolicy, Notes: "non-owner agent"},
		},
		LastCheckedAt:   "2026-05-04T00:00:00Z",
		FixturesVersion: "phase0-2026-04-30",
	}); err != nil {
		t.Fatalf("SetReport: %v", err)
	}

	composer := StaticCompatibilityComposer{
		MinDesktopVersion:   "0.1.0",
		EventSchemaVersions: map[string]int{"_system": 1},
		Migration: MigrationState{
			SchemaVersion: 0,
			AppliedAt:     "2026-05-04T00:00:00Z",
			Pending:       []string{},
			Phase:         "idle",
		},
	}

	mux := http.NewServeMux()
	mux.Handle("/v1/compatibility", reg.HandleCompatibility(composer))
	mux.Handle("/v1/capabilities", http.HandlerFunc(reg.HandleCapabilities))

	srv := httptest.NewServer(mux)
	defer srv.Close()

	// Compatibility — full smoke against the wire.
	resp, err := http.Get(srv.URL + "/v1/compatibility")
	if err != nil {
		t.Fatalf("GET /v1/compatibility: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); ct != "application/json; charset=utf-8" {
		t.Fatalf("expected application/json content-type, got %q", ct)
	}

	var compat schemas.CompatibilityReport
	if err := json.NewDecoder(resp.Body).Decode(&compat); err != nil {
		t.Fatalf("decode CompatibilityReport: %v", err)
	}
	if compat.SchemaVersion != 1 {
		t.Fatalf("schemaVersion drift: %d", compat.SchemaVersion)
	}
	if compat.DaemonApiVersion != "0.1.0" {
		t.Fatalf("daemonApiVersion drift: %q", compat.DaemonApiVersion)
	}
	if compat.MinDesktopVersion != "0.1.0" {
		t.Fatalf("minDesktopVersion drift: %q", compat.MinDesktopVersion)
	}
	if compat.MigrationState.SchemaVersion != 0 {
		t.Fatalf("migrationState.schemaVersion drift: %d", compat.MigrationState.SchemaVersion)
	}
	if compat.MigrationState.Phase == nil || string(*compat.MigrationState.Phase) != "idle" {
		t.Fatalf("migrationState.phase drift: got %v", compat.MigrationState.Phase)
	}
	if got := compat.EventSchemaVersions["_system"]; got != 1 {
		t.Fatalf("eventSchemaVersions._system drift: %d", got)
	}
	if compat.Capabilities.Tools == nil {
		t.Fatalf("capabilities.tools missing in /v1/compatibility response")
	}
	gitReport, ok := compat.Capabilities.Tools["git"]
	if !ok {
		t.Fatalf("expected git tool in compatibility snapshot")
	}
	push, ok := gitReport.Capabilities["git.push"]
	if !ok {
		t.Fatalf("expected git.push in compatibility snapshot")
	}
	if string(push.Status) != "blocked-by-policy" {
		t.Fatalf("git.push status drift: %q", push.Status)
	}
	if push.Notes == nil || *push.Notes != "non-owner agent" {
		t.Fatalf("git.push notes drift: got %v", push.Notes)
	}

	// Capabilities — separate smoke; same handler family.
	resp2, err := http.Get(srv.URL + "/v1/capabilities")
	if err != nil {
		t.Fatalf("GET /v1/capabilities: %v", err)
	}
	defer resp2.Body.Close()
	if resp2.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp2.StatusCode)
	}
	var capReg schemas.CapabilityRegistry
	if err := json.NewDecoder(resp2.Body).Decode(&capReg); err != nil {
		t.Fatalf("decode CapabilityRegistry: %v", err)
	}
	if capReg.SchemaVersion != 1 {
		t.Fatalf("registry schemaVersion drift: %d", capReg.SchemaVersion)
	}
	if _, ok := capReg.Tools["git"]; !ok {
		t.Fatalf("registry missing git tool")
	}
}

// TestSlashV1CompatibilityRejectsNonGet — defensive smoke: handler refuses
// methods it doesn't allow (per the spec, /v1/compatibility is GET-only).
func TestSlashV1CompatibilityRejectsNonGet(t *testing.T) {
	t.Parallel()

	reg := New("0.1.0")
	composer := StaticCompatibilityComposer{
		MinDesktopVersion: "0.1.0",
		Migration:         MigrationState{Phase: "idle"},
	}
	mux := http.NewServeMux()
	mux.Handle("/v1/compatibility", reg.HandleCompatibility(composer))
	srv := httptest.NewServer(mux)
	defer srv.Close()

	req, err := http.NewRequest(http.MethodPost, srv.URL+"/v1/compatibility", nil)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("do: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusMethodNotAllowed {
		t.Fatalf("expected 405 for POST, got %d", resp.StatusCode)
	}
	if allow := resp.Header.Get("Allow"); allow != "GET" {
		t.Fatalf("expected Allow: GET, got %q", allow)
	}
}

