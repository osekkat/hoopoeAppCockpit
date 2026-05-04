package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/hoopoe-cockpit/hoopoe/apps/daemon/internal/capabilities"
)

func newTestRegistry(t *testing.T, snapshotAt string) *capabilities.Registry {
	t.Helper()
	stamp, err := time.Parse(time.RFC3339, snapshotAt)
	if err != nil {
		t.Fatalf("parse stamp: %v", err)
	}
	r := capabilities.New("0.1.0")
	r.SetClock(func() time.Time { return stamp })
	r.SetFixturesVersion("phase0-test")
	if err := r.SetReport(&capabilities.ToolReport{
		Tool:    capabilities.ToolGit,
		Version: "2.40.0",
		Source:  "CLI",
		Capabilities: map[string]capabilities.Capability{
			"git.status.read": {Status: capabilities.StatusOK},
			"git.push":        {Status: capabilities.StatusBlockedByPolicy, Notes: "snapshot scripts never push"},
		},
		LastCheckedAt:   snapshotAt,
		FixturesVersion: "phase0-test",
	}); err != nil {
		t.Fatalf("seed report: %v", err)
	}
	return r
}

func TestCapabilitiesRouteServesRegistry(t *testing.T) {
	registry := newTestRegistry(t, "2026-05-04T00:00:00Z")
	stamp, _ := time.Parse(time.RFC3339, "2026-05-04T00:00:00Z")
	router := NewRouter(Config{
		Build:        BuildInfo{Version: "0.1.0", Commit: "test", BuildDate: "test", APIVersion: "v1"},
		Capabilities: registry,
		Now:          func() time.Time { return stamp },
	})

	req := httptest.NewRequest(http.MethodGet, "/v1/capabilities", nil)
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
	if got := rr.Header().Get("Content-Type"); got != "application/json; charset=utf-8" {
		t.Errorf("content-type=%q", got)
	}
	var snap capabilities.CapabilityRegistry
	if err := json.Unmarshal(rr.Body.Bytes(), &snap); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if snap.SchemaVersion != capabilities.SchemaVersion {
		t.Errorf("schemaVersion=%d", snap.SchemaVersion)
	}
	if snap.DaemonAPIVersion != "0.1.0" {
		t.Errorf("daemonApiVersion=%q", snap.DaemonAPIVersion)
	}
	gitReport := snap.Tools[capabilities.ToolGit]
	if gitReport == nil {
		t.Fatal("git report missing")
	}
	if gitReport.Capabilities["git.push"].Status != capabilities.StatusBlockedByPolicy {
		t.Errorf("git.push.status=%s", gitReport.Capabilities["git.push"].Status)
	}
}

func TestCompatibilityRouteEmbedsRegistry(t *testing.T) {
	registry := newTestRegistry(t, "2026-05-04T00:00:00Z")
	stamp, _ := time.Parse(time.RFC3339, "2026-05-04T00:00:00Z")
	router := NewRouter(Config{
		Build:        BuildInfo{Version: "0.1.0", Commit: "test", BuildDate: "test", APIVersion: "v1"},
		Capabilities: registry,
		Now:          func() time.Time { return stamp },
	})

	req := httptest.NewRequest(http.MethodGet, "/v1/compatibility", nil)
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
	var report capabilities.CompatibilityReport
	if err := json.Unmarshal(rr.Body.Bytes(), &report); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if report.SchemaVersion != capabilities.SchemaVersion {
		t.Errorf("schemaVersion=%d", report.SchemaVersion)
	}
	if report.DaemonAPIVersion != "0.1.0" {
		t.Errorf("daemonApiVersion=%q", report.DaemonAPIVersion)
	}
	if report.MinDesktopVersion != "0.0.0" {
		t.Errorf("minDesktopVersion=%q", report.MinDesktopVersion)
	}
	if report.MigrationState.Phase != capabilities.MigrationIdle {
		t.Errorf("migrationState.phase=%q", report.MigrationState.Phase)
	}
	if report.EventSchemaVersions["_system"] != schemaVersion {
		t.Errorf("eventSchemaVersions._system=%d", report.EventSchemaVersions["_system"])
	}
	if report.Capabilities == nil || report.Capabilities.Tools[capabilities.ToolGit] == nil {
		t.Errorf("compatibility report missing capabilities snapshot")
	}
}

func TestCapabilitiesRouteWithoutRegistryReturns503(t *testing.T) {
	stamp, _ := time.Parse(time.RFC3339, "2026-05-04T00:00:00Z")
	router := NewRouter(Config{
		Build: BuildInfo{Version: "0.1.0", Commit: "test", BuildDate: "test", APIVersion: "v1"},
		Now:   func() time.Time { return stamp },
		// Capabilities intentionally nil.
	})

	for _, path := range []string{"/v1/capabilities", "/v1/compatibility"} {
		t.Run(path, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, path, nil)
			rr := httptest.NewRecorder()
			router.ServeHTTP(rr, req)
			if rr.Code != http.StatusServiceUnavailable {
				t.Errorf("expected 503, got %d", rr.Code)
			}
			if got := rr.Header().Get("Content-Type"); got != "application/problem+json; charset=utf-8" {
				t.Errorf("content-type=%q", got)
			}
			// Body should decode to a problem envelope with a stable code.
			var problem map[string]any
			if err := json.Unmarshal(rr.Body.Bytes(), &problem); err != nil {
				t.Errorf("decode: %v", err)
			}
			if problem["status"].(float64) != http.StatusServiceUnavailable {
				t.Errorf("problem status=%v", problem["status"])
			}
		})
	}
}

func TestCapabilitiesRouteRejectsPOST(t *testing.T) {
	registry := newTestRegistry(t, "2026-05-04T00:00:00Z")
	router := NewRouter(Config{
		Build:        BuildInfo{Version: "0.1.0", Commit: "test", BuildDate: "test", APIVersion: "v1"},
		Capabilities: registry,
	})
	req := httptest.NewRequest(http.MethodPost, "/v1/capabilities", nil)
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)
	if rr.Code != http.StatusMethodNotAllowed {
		t.Errorf("expected 405, got %d", rr.Code)
	}
}
