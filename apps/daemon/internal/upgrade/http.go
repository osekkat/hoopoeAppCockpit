package upgrade

import (
	"encoding/json"
	"errors"
	"net/http"
)

// Handler serves the daemon upgrade REST surface. The daemon API package mounts
// it at POST /v1/bootstrap/daemon/upgrade once the production Service is wired.
type Handler struct {
	Service *Service
}

func (h Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		h.handleState(w)
	case http.MethodPost:
		h.handleUpgrade(w, r)
	default:
		w.Header().Set("Allow", "GET, POST")
		writeProblem(w, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed", "use GET for state or POST to start an upgrade")
	}
}

func (h Handler) handleState(w http.ResponseWriter) {
	if h.Service == nil {
		writeProblem(w, http.StatusServiceUnavailable, "upgrade_unavailable", "upgrade service unavailable", "daemon upgrade service is not configured")
		return
	}
	writeJSON(w, http.StatusOK, h.Service.State())
}

func (h Handler) handleUpgrade(w http.ResponseWriter, r *http.Request) {
	if h.Service == nil {
		writeProblem(w, http.StatusServiceUnavailable, "upgrade_unavailable", "upgrade service unavailable", "daemon upgrade service is not configured")
		return
	}
	defer func() { _ = r.Body.Close() }()
	var req Request
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&req); err != nil {
		writeProblem(w, http.StatusBadRequest, "invalid_json", "invalid request body", err.Error())
		return
	}
	result, err := h.Service.Upgrade(r.Context(), req)
	if err != nil {
		status := statusForError(err)
		writeProblemWithResult(w, status, codeForError(err), "daemon upgrade failed", err.Error(), result)
		return
	}
	writeJSON(w, http.StatusAccepted, result)
}

func statusForError(err error) int {
	switch {
	case errors.Is(err, ErrInvalidRequest):
		return http.StatusBadRequest
	case errors.Is(err, ErrUpgradeInProgress):
		return http.StatusConflict
	case errors.Is(err, ErrVerificationFailed), errors.Is(err, ErrDesktopUpgradeRequired):
		return http.StatusPreconditionFailed
	case errors.Is(err, ErrBackupFailed), errors.Is(err, ErrRollbackFailed):
		return http.StatusInternalServerError
	default:
		return http.StatusBadGateway
	}
}

func codeForError(err error) string {
	switch {
	case errors.Is(err, ErrInvalidRequest):
		return "upgrade.invalid_request"
	case errors.Is(err, ErrUpgradeInProgress):
		return "upgrade.in_progress"
	case errors.Is(err, ErrVerificationFailed):
		return "upgrade.verification_failed"
	case errors.Is(err, ErrDesktopUpgradeRequired):
		return "upgrade.desktop_upgrade_required"
	case errors.Is(err, ErrBackupFailed):
		return "upgrade.backup_failed"
	case errors.Is(err, ErrRollbackFailed):
		return "upgrade.rollback_failed"
	case errors.Is(err, ErrPostInstallFailed):
		return "upgrade.post_install_failed"
	default:
		return "upgrade.failed"
	}
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	_ = enc.Encode(payload)
}

func writeProblem(w http.ResponseWriter, status int, code, title, detail string) {
	writeProblemWithResult(w, status, code, title, detail, UpgradeResult{})
}

func writeProblemWithResult(w http.ResponseWriter, status int, code, title, detail string, result UpgradeResult) {
	body := map[string]any{
		"type":   "about:blank",
		"title":  title,
		"status": status,
		"code":   code,
		"detail": detail,
	}
	if result.SchemaVersion != 0 {
		body["result"] = result
	}
	w.Header().Set("Content-Type", "application/problem+json; charset=utf-8")
	w.WriteHeader(status)
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	_ = enc.Encode(body)
}
