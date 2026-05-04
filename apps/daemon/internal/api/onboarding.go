package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/hoopoe-cockpit/hoopoe/apps/daemon/internal/onboarding/checkpoints"
	schemas "github.com/hoopoe-cockpit/hoopoe/packages/schemas/go"
)

func (s *server) mountOnboardingRoutes(r chi.Router) {
	r.Get("/v1/bootstrap/runs/{runID}/checkpoints", s.handleOnboardingTimeline)
	r.Post("/v1/bootstrap/runs/{runID}/checkpoints/{stepID}/transition", s.handleOnboardingTransition)
	r.Get("/v1/diagnostics/bootstrap/runs/{runID}", s.handleOnboardingTimeline)
	r.Get("/v1/diagnostics/repair-actions", s.handleOnboardingRepairActions)
}

type checkpointTransitionBody struct {
	ProjectID     string             `json:"projectId,omitempty"`
	StepLabel     string             `json:"stepLabel,omitempty"`
	Status        checkpoints.Status `json:"status"`
	Actor         schemas.Actor      `json:"actor"`
	Reason        string             `json:"reason,omitempty"`
	EvidenceRefs  []string           `json:"evidenceRefs,omitempty"`
	FailureReason string             `json:"failureReason,omitempty"`
	ResumeHint    string             `json:"resumeHint,omitempty"`
	At            *time.Time         `json:"at,omitempty"`
}

func (s *server) handleOnboardingTimeline(w http.ResponseWriter, r *http.Request) {
	if s.onboarding == nil {
		writeProblemCode(w, http.StatusNotImplemented, "onboarding.checkpoints.unavailable", "onboarding checkpoints unavailable", "")
		return
	}
	timeline, err := s.onboarding.Timeline(r.Context(), chi.URLParam(r, "runID"))
	if err != nil {
		s.writeOnboardingError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, timeline)
}

func (s *server) handleOnboardingTransition(w http.ResponseWriter, r *http.Request) {
	if s.onboarding == nil {
		writeProblemCode(w, http.StatusNotImplemented, "onboarding.checkpoints.unavailable", "onboarding checkpoints unavailable", "")
		return
	}
	var body checkpointTransitionBody
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&body); err != nil {
		writeProblemCode(w, http.StatusBadRequest, "onboarding.transition.invalid_json", "invalid checkpoint transition", err.Error())
		return
	}
	at := time.Time{}
	if body.At != nil {
		at = *body.At
	}
	result, err := s.onboarding.Transition(r.Context(), checkpoints.TransitionRequest{
		RunID:         chi.URLParam(r, "runID"),
		StepID:        chi.URLParam(r, "stepID"),
		ProjectID:     body.ProjectID,
		StepLabel:     body.StepLabel,
		Status:        body.Status,
		Actor:         body.Actor,
		Reason:        body.Reason,
		EvidenceRefs:  body.EvidenceRefs,
		FailureReason: body.FailureReason,
		ResumeHint:    body.ResumeHint,
		At:            at,
	})
	if err != nil {
		s.writeOnboardingError(w, err)
		return
	}
	status := http.StatusOK
	if result.Created {
		status = http.StatusCreated
	}
	writeJSON(w, status, result)
}

func (s *server) handleOnboardingRepairActions(w http.ResponseWriter, r *http.Request) {
	if s.onboarding == nil {
		writeProblemCode(w, http.StatusNotImplemented, "onboarding.checkpoints.unavailable", "onboarding checkpoints unavailable", "")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"schemaVersion": checkpoints.SchemaVersion,
		"actions":       s.onboarding.RepairActions(),
	})
}

func (s *server) writeOnboardingError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, checkpoints.ErrInvalidInput):
		writeProblemCode(w, http.StatusBadRequest, "onboarding.checkpoints.invalid", "invalid onboarding checkpoint", err.Error())
	case errors.Is(err, checkpoints.ErrNotFound):
		writeProblemCode(w, http.StatusNotFound, "onboarding.checkpoints.not_found", "onboarding checkpoint not found", err.Error())
	default:
		writeProblemCode(w, http.StatusInternalServerError, "onboarding.checkpoints.error", "onboarding checkpoint error", err.Error())
	}
}
