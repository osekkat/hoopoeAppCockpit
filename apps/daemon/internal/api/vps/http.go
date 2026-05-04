package vps

import (
	"bytes"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/hoopoe-cockpit/hoopoe/apps/daemon/internal/search"
	schemas "github.com/hoopoe-cockpit/hoopoe/packages/schemas/go"
)

func MountGitRoutes(r chi.Router, cfg Config) {
	h := &handler{service: NewService(cfg)}
	r.Get("/git/status", h.status)
	r.Get("/git/staged-diff", h.diff(DiffKindStaged))
	r.Get("/git/unstaged-diff", h.diff(DiffKindUnstaged))
	r.Get("/git/unpushed-commits", h.unpushedCommits)
	r.Get("/git/open-files", h.openFiles)
	r.Get("/grep", h.grep)
}

type handler struct {
	service *Service
}

func (h *handler) status(w http.ResponseWriter, r *http.Request) {
	projectID := strings.TrimSpace(chi.URLParam(r, "projectId"))
	response, err := h.service.WorkingTreeStatus(r.Context(), projectID)
	if err != nil {
		writeServiceProblem(w, err)
		return
	}
	writeJSON(w, http.StatusOK, response)
}

func (h *handler) diff(kind DiffKind) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		projectID := strings.TrimSpace(chi.URLParam(r, "projectId"))
		response, err := h.service.Diff(r.Context(), projectID, kind, parseDiffPage(r))
		if err != nil {
			writeServiceProblem(w, err)
			return
		}
		writeJSON(w, http.StatusOK, response)
	}
}

func (h *handler) unpushedCommits(w http.ResponseWriter, r *http.Request) {
	projectID := strings.TrimSpace(chi.URLParam(r, "projectId"))
	response, err := h.service.UnpushedCommits(r.Context(), projectID)
	if err != nil {
		writeServiceProblem(w, err)
		return
	}
	writeJSON(w, http.StatusOK, response)
}

func (h *handler) openFiles(w http.ResponseWriter, r *http.Request) {
	projectID := strings.TrimSpace(chi.URLParam(r, "projectId"))
	response, err := h.service.OpenFiles(r.Context(), projectID)
	if err != nil {
		writeServiceProblem(w, err)
		return
	}
	writeJSON(w, http.StatusOK, response)
}

func (h *handler) grep(w http.ResponseWriter, r *http.Request) {
	projectID := strings.TrimSpace(chi.URLParam(r, "projectId"))
	response, err := h.service.Grep(r.Context(), projectID, parseSearchRequest(r))
	if err != nil {
		writeServiceProblem(w, err)
		return
	}
	writeJSON(w, http.StatusOK, response)
}

func parseSearchRequest(r *http.Request) search.Request {
	query := strings.TrimSpace(r.URL.Query().Get("query"))
	if query == "" {
		query = strings.TrimSpace(r.URL.Query().Get("q"))
	}
	return search.Request{
		Query:      query,
		Paths:      cleanQueryValues(r.URL.Query()["path"]),
		Literal:    parseBool(r.URL.Query().Get("literal")),
		MaxResults: parsePositiveInt(r.URL.Query().Get("maxResults")),
	}
}

func parseDiffPage(r *http.Request) DiffPage {
	return DiffPage{
		StartLine: parsePositiveInt(r.URL.Query().Get("startLine")),
		Limit:     parsePositiveInt(r.URL.Query().Get("limit")),
	}
}

func parsePositiveInt(raw string) int {
	value, err := strconv.Atoi(strings.TrimSpace(raw))
	if err != nil || value <= 0 {
		return 0
	}
	return value
}

func parseBool(raw string) bool {
	value, err := strconv.ParseBool(strings.TrimSpace(raw))
	return err == nil && value
}

func cleanQueryValues(values []string) []string {
	clean := make([]string, 0, len(values))
	for _, value := range values {
		trimmed := strings.TrimSpace(value)
		if trimmed != "" {
			clean = append(clean, trimmed)
		}
	}
	return clean
}

func writeServiceProblem(w http.ResponseWriter, err error) {
	status, code, title := mapProjectError(err)
	detail := err.Error()
	writeProblem(w, status, schemas.Problem{
		Type:   "urn:hoopoe:problem:" + strings.ReplaceAll(code, ".", "-"),
		Title:  title,
		Status: status,
		Code:   code,
		Detail: &detail,
	})
}

func writeProblem(w http.ResponseWriter, status int, problem schemas.Problem) {
	body, err := encodeJSON(problem)
	if err != nil {
		body = []byte(`{"type":"urn:hoopoe:problem:daemon-encoding-failed","title":"internal encoding error","status":500,"code":"daemon.encoding_failed"}` + "\n")
		status = http.StatusInternalServerError
	}
	w.Header().Set("Content-Type", "application/problem+json; charset=utf-8")
	w.WriteHeader(status)
	_, _ = w.Write(body)
}

func writeJSON(w http.ResponseWriter, status int, response any) {
	body, err := encodeJSON(response)
	if err != nil {
		writeProblem(w, http.StatusInternalServerError, encodingProblem(err))
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_, _ = w.Write(body)
}

func encodingProblem(err error) schemas.Problem {
	detail := err.Error()
	return schemas.Problem{
		Type:   "urn:hoopoe:problem:daemon-encoding-failed",
		Title:  "internal encoding error",
		Status: http.StatusInternalServerError,
		Code:   "daemon.encoding_failed",
		Detail: &detail,
	}
}

func encodeJSON(payload any) ([]byte, error) {
	var body bytes.Buffer
	enc := json.NewEncoder(&body)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(payload); err != nil {
		return nil, err
	}
	return body.Bytes(), nil
}
