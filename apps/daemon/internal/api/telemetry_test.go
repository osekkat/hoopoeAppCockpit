package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/hoopoe-cockpit/hoopoe/apps/daemon/internal/telemetry"
)

func TestTelemetryPrivacyDefaultsToDisabledLocalOnly(t *testing.T) {
	router := NewRouter(Config{})
	req := httptest.NewRequest(http.MethodGet, "/v1/diagnostics/telemetry/privacy", nil)
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body=%s", rec.Code, http.StatusOK, rec.Body.String())
	}
	var status telemetry.PrivacyStatus
	if err := json.Unmarshal(rec.Body.Bytes(), &status); err != nil {
		t.Fatalf("decode privacy status: %v", err)
	}
	if status.Enabled || !status.LocalOnly || status.PendingCrashReports != 0 {
		t.Fatalf("privacy status = %+v", status)
	}
}

func TestTelemetryEventRequiresOptInAndRejectsUserData(t *testing.T) {
	service, err := telemetry.NewService(telemetry.Config{
		Enabled: true,
		Path:    filepath.Join(t.TempDir(), "records.jsonl"),
	})
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	router := NewRouter(Config{Telemetry: service})

	req := httptest.NewRequest(http.MethodPost, "/v1/diagnostics/telemetry/events", strings.NewReader(`{"type":"stage.usage","count":2,"dimensions":{"stage":"planning"}}`))
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want %d; body=%s", rec.Code, http.StatusAccepted, rec.Body.String())
	}

	badReq := httptest.NewRequest(http.MethodPost, "/v1/diagnostics/telemetry/events", strings.NewReader(`{"type":"stage.usage","dimensions":{"filePath":"/home/ubuntu/secret.go"}}`))
	badRec := httptest.NewRecorder()
	router.ServeHTTP(badRec, badReq)
	if badRec.Code != http.StatusBadRequest {
		t.Fatalf("bad status = %d, want %d; body=%s", badRec.Code, http.StatusBadRequest, badRec.Body.String())
	}

	disabledRouter := NewRouter(Config{})
	disabledReq := httptest.NewRequest(http.MethodPost, "/v1/diagnostics/telemetry/events", strings.NewReader(`{"type":"stage.usage"}`))
	disabledRec := httptest.NewRecorder()
	disabledRouter.ServeHTTP(disabledRec, disabledReq)
	if disabledRec.Code != http.StatusForbidden {
		t.Fatalf("disabled status = %d, want %d", disabledRec.Code, http.StatusForbidden)
	}
}

func TestCrashReportRoundTripRedactsAndDeletesByTombstone(t *testing.T) {
	service, err := telemetry.NewService(telemetry.Config{
		Enabled: true,
		Path:    filepath.Join(t.TempDir(), "records.jsonl"),
	})
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	router := NewRouter(Config{Telemetry: service})
	body := `{"id":"crash_api","daemonVersion":"1.0.0","stackTrace":"panic\nAuthorization: Bearer abcdefghijklmnopqrstuvwxyz","auditTail":["alice@example.com /home/ubuntu/secret"]}`
	req := httptest.NewRequest(http.MethodPost, "/v1/diagnostics/crash-reports", strings.NewReader(body))
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d; body=%s", rec.Code, http.StatusCreated, rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), "abcdefghijklmnopqrstuvwxyz") || strings.Contains(rec.Body.String(), "/home/ubuntu") {
		t.Fatalf("crash response leaked sensitive data: %s", rec.Body.String())
	}

	listReq := httptest.NewRequest(http.MethodGet, "/v1/diagnostics/crash-reports", nil)
	listRec := httptest.NewRecorder()
	router.ServeHTTP(listRec, listReq)
	if listRec.Code != http.StatusOK || !strings.Contains(listRec.Body.String(), "crash_api") {
		t.Fatalf("list status=%d body=%s", listRec.Code, listRec.Body.String())
	}

	delReq := httptest.NewRequest(http.MethodDelete, "/v1/diagnostics/crash-reports/crash_api", nil)
	delRec := httptest.NewRecorder()
	router.ServeHTTP(delRec, delReq)
	if delRec.Code != http.StatusOK {
		t.Fatalf("delete status=%d body=%s", delRec.Code, delRec.Body.String())
	}

	getReq := httptest.NewRequest(http.MethodGet, "/v1/diagnostics/crash-reports/crash_api", nil)
	getRec := httptest.NewRecorder()
	router.ServeHTTP(getRec, getReq)
	if getRec.Code != http.StatusNotFound {
		t.Fatalf("get deleted status=%d body=%s", getRec.Code, getRec.Body.String())
	}
}
