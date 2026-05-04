package api

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/hoopoe-cockpit/hoopoe/apps/daemon/internal/telemetry"
)

const maxTelemetryRequestBytes = telemetry.MaxCrashReportBytes

func (s *server) mountTelemetryRoutes(r chi.Router) {
	r.Get("/v1/diagnostics/telemetry/privacy", s.handleTelemetryPrivacy)
	r.Post("/v1/diagnostics/telemetry/events", s.handleTelemetryEvent)
	r.Get("/v1/diagnostics/crash-reports", s.handleCrashReportList)
	r.Post("/v1/diagnostics/crash-reports", s.handleCrashReportCreate)
	r.Get("/v1/diagnostics/crash-reports/{reportId}", s.handleCrashReportGet)
	r.Delete("/v1/diagnostics/crash-reports/{reportId}", s.handleCrashReportDelete)
}

func (s *server) handleTelemetryPrivacy(w http.ResponseWriter, r *http.Request) {
	status, err := s.telemetry.PrivacyStatus(r.Context())
	if err != nil {
		s.writeTelemetryError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, status)
}

func (s *server) handleTelemetryEvent(w http.ResponseWriter, r *http.Request) {
	var input telemetry.EventInput
	if err := decodeTelemetryJSON(w, r, &input); err != nil {
		s.writeProblemCode(w, http.StatusBadRequest, "telemetry.invalid_json", "invalid telemetry event", err.Error())
		return
	}
	event, err := s.telemetry.RecordEvent(r.Context(), input)
	if err != nil {
		s.writeTelemetryError(w, err)
		return
	}
	writeJSON(w, http.StatusAccepted, event)
}

func (s *server) handleCrashReportCreate(w http.ResponseWriter, r *http.Request) {
	var input telemetry.CrashReportInput
	if err := decodeTelemetryJSON(w, r, &input); err != nil {
		s.writeProblemCode(w, http.StatusBadRequest, "telemetry.invalid_json", "invalid crash report", err.Error())
		return
	}
	report, err := s.telemetry.SaveCrashReport(r.Context(), input)
	if err != nil {
		s.writeTelemetryError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, report)
}

func (s *server) handleCrashReportList(w http.ResponseWriter, r *http.Request) {
	reports, err := s.telemetry.ListCrashReports(r.Context())
	if err != nil {
		s.writeTelemetryError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"schemaVersion": telemetry.SchemaVersion,
		"items":         reports,
	})
}

func (s *server) handleCrashReportGet(w http.ResponseWriter, r *http.Request) {
	report, err := s.telemetry.CrashReport(r.Context(), chi.URLParam(r, "reportId"))
	if err != nil {
		s.writeTelemetryError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, report)
}

func (s *server) handleCrashReportDelete(w http.ResponseWriter, r *http.Request) {
	if err := s.telemetry.DeleteCrashReport(r.Context(), chi.URLParam(r, "reportId")); err != nil {
		s.writeTelemetryError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"schemaVersion": telemetry.SchemaVersion,
		"deleted":       true,
	})
}

func decodeTelemetryJSON(w http.ResponseWriter, r *http.Request, target any) error {
	r.Body = http.MaxBytesReader(w, r.Body, maxTelemetryRequestBytes)
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	return dec.Decode(target)
}

func (s *server) writeTelemetryError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, telemetry.ErrDisabled):
		s.writeProblemCode(w, http.StatusForbidden, "telemetry.opt_in_required", "telemetry opt-in required", "crash reporting and telemetry are disabled by default")
	case errors.Is(err, telemetry.ErrInvalidRequest):
		s.writeProblemCode(w, http.StatusBadRequest, "telemetry.invalid_request", "invalid telemetry request", err.Error())
	case errors.Is(err, telemetry.ErrNotFound):
		s.writeProblemCode(w, http.StatusNotFound, "telemetry.not_found", "telemetry record not found", err.Error())
	default:
		s.writeProblemCode(w, http.StatusInternalServerError, "telemetry.failed", "telemetry operation failed", err.Error())
	}
}
