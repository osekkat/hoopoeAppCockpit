package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	daemonmetrics "github.com/hoopoe-cockpit/hoopoe/apps/daemon/internal/metrics"
)

func TestDiagnosticsMetricsRoutesExposeSnapshotAndPrometheusText(t *testing.T) {
	now := time.Date(2026, 5, 4, 13, 0, 0, 0, time.UTC)
	registry := daemonmetrics.NewRegistry(daemonmetrics.Config{
		Now:                   func() time.Time { return now },
		IncludeDefaultTargets: true,
	})
	if err := registry.SetGauge(daemonmetrics.MetricJobCancellationOrphans, nil, 0); err != nil {
		t.Fatalf("SetGauge: %v", err)
	}
	router := NewRouter(Config{Metrics: registry})

	versionReq := httptest.NewRequest(http.MethodGet, "/v1/version", nil)
	versionRec := httptest.NewRecorder()
	router.ServeHTTP(versionRec, versionReq)
	if versionRec.Code != http.StatusOK {
		t.Fatalf("version status = %d", versionRec.Code)
	}

	req := httptest.NewRequest(http.MethodGet, "/v1/diagnostics/metrics", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("metrics status = %d; body=%s", rec.Code, rec.Body.String())
	}
	var snapshot daemonmetrics.Snapshot
	if err := json.Unmarshal(rec.Body.Bytes(), &snapshot); err != nil {
		t.Fatalf("decode metrics snapshot: %v", err)
	}
	if snapshot.SchemaVersion != daemonmetrics.SchemaVersion || len(snapshot.Targets) == 0 {
		t.Fatalf("snapshot = %+v", snapshot)
	}
	if len(snapshot.SeriesByName(daemonmetrics.MetricRequestDurationSeconds)) == 0 {
		t.Fatalf("request duration metric missing: %+v", snapshot.Series)
	}

	promReq := httptest.NewRequest(http.MethodGet, "/v1/diagnostics/metrics/prometheus", nil)
	promRec := httptest.NewRecorder()
	router.ServeHTTP(promRec, promReq)
	if promRec.Code != http.StatusOK {
		t.Fatalf("prometheus status = %d", promRec.Code)
	}
	if contentType := promRec.Header().Get("Content-Type"); !strings.HasPrefix(contentType, "text/plain") {
		t.Fatalf("content type = %q", contentType)
	}
	if !strings.Contains(promRec.Body.String(), "hoopoe_metrics_schema_version 1") {
		t.Fatalf("prometheus text = %s", promRec.Body.String())
	}
}
