package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/hoopoe-cockpit/hoopoe/apps/daemon/internal/onboarding/checkpoints"
)

func TestOnboardingCheckpointRoutes(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 5, 4, 14, 0, 0, 0, time.UTC)
	service := checkpoints.NewService(checkpoints.Config{
		Now:   func() time.Time { return now },
		NewID: func() (string, error) { return "evt_api", nil },
	})
	router := NewRouter(Config{Onboarding: service})
	body := map[string]any{
		"projectId":     "proj_api",
		"status":        checkpoints.StatusFailed,
		"failureReason": "doctor failed",
		"resumeHint":    "/v1/bootstrap/acfs/resume",
		"evidenceRefs":  []string{"acfs-log:run_api:10"},
	}
	payload, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("marshal request: %v", err)
	}
	req := httptest.NewRequest(http.MethodPost, "/v1/bootstrap/runs/run_api/checkpoints/acfs-install.doctor/transition", bytes.NewReader(payload))
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d; body=%s", rec.Code, http.StatusCreated, rec.Body.String())
	}
	var transition checkpoints.TransitionResult
	if err := json.Unmarshal(rec.Body.Bytes(), &transition); err != nil {
		t.Fatalf("decode transition: %v", err)
	}
	if transition.Checkpoint.Status != checkpoints.StatusFailed || transition.Checkpoint.ProjectID != "proj_api" {
		t.Fatalf("transition = %+v", transition)
	}

	req = httptest.NewRequest(http.MethodGet, "/v1/diagnostics/bootstrap/runs/run_api", nil)
	rec = httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("timeline status = %d; body=%s", rec.Code, rec.Body.String())
	}
	var timeline checkpoints.Timeline
	if err := json.Unmarshal(rec.Body.Bytes(), &timeline); err != nil {
		t.Fatalf("decode timeline: %v", err)
	}
	if len(timeline.Checkpoints) != 1 || len(timeline.Actions) != 1 {
		t.Fatalf("timeline = %+v", timeline)
	}
}

func TestOnboardingRoutesUnavailableWithoutService(t *testing.T) {
	t.Parallel()
	router := NewRouter(Config{})
	req := httptest.NewRequest(http.MethodGet, "/v1/diagnostics/repair-actions", nil)
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotImplemented {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusNotImplemented)
	}
}

func TestOnboardingTransitionRejectsOversizedBody(t *testing.T) {
	// hp-c5rb: checkpoint transitions are reachable during onboarding before
	// the daemon has structured rate limiting; the handler must cap the
	// decoded request body so a malicious or buggy client cannot drive the
	// daemon out of memory by streaming a huge body.
	t.Parallel()
	now := time.Date(2026, 5, 4, 14, 0, 0, 0, time.UTC)
	service := checkpoints.NewService(checkpoints.Config{
		Now:   func() time.Time { return now },
		NewID: func() (string, error) { return "evt_oversize", nil },
	})
	router := NewRouter(Config{Onboarding: service})

	oversized := bytes.Repeat([]byte("E"), (1<<20)+1024)
	payload := append([]byte(`{"projectId":"proj_oversize","status":"failed","failureReason":"`), oversized...)
	payload = append(payload, []byte(`"}`)...)
	req := httptest.NewRequest(http.MethodPost, "/v1/bootstrap/runs/run_oversize/checkpoints/acfs-install.doctor/transition", bytes.NewReader(payload))
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d; body=%s", rec.Code, http.StatusBadRequest, rec.Body.String())
	}
}
