package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/hoopoe-cockpit/hoopoe/apps/daemon/internal/security"
)

func TestBindSafetyReportRouteReturnsDiagnosticsPayload(t *testing.T) {
	decision, err := security.EvaluateBind(context.Background(), security.BindRequest{
		Address:            "0.0.0.0:8080",
		ConfigAllowsPublic: true,
	})
	if err != nil {
		t.Fatalf("EvaluateBind: %v", err)
	}
	report := security.NewBindReport(decision, time.Unix(123, 0).UTC())
	router := WithBindSafetyReport(http.NotFoundHandler(), report)

	req := httptest.NewRequest(http.MethodGet, "/v1/security/bind-safety", nil)
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	var body security.BindReport
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode bind safety report: %v", err)
	}
	if body.SchemaVersion != 1 {
		t.Fatalf("schemaVersion = %d, want 1", body.SchemaVersion)
	}
	if body.Decision.EffectiveAddress != "127.0.0.1:8080" {
		t.Fatalf("effective address = %q", body.Decision.EffectiveAddress)
	}
	if body.Decision.Warning == nil || body.Decision.Warning.Code != security.PublicBindWarningCode {
		t.Fatalf("missing public-bind warning: %+v", body.Decision.Warning)
	}
}

func TestBindSafetyReportWrapperDelegatesOtherRoutes(t *testing.T) {
	delegated := false
	router := WithBindSafetyReport(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		delegated = true
		w.WriteHeader(http.StatusNoContent)
	}), security.BindReport{})

	req := httptest.NewRequest(http.MethodGet, "/v1/version", nil)
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	if !delegated {
		t.Fatal("wrapper did not delegate non-bind-safety route")
	}
	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusNoContent)
	}
}
