package vps

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"
	schemas "github.com/hoopoe-cockpit/hoopoe/packages/schemas/go"
)

func MountGitRoutes(r chi.Router, cfg Config) {
	h := &handler{service: NewService(cfg)}
	r.Get("/git/status", h.status)
	r.Get("/git/staged-diff", h.diff(DiffKindStaged))
	r.Get("/git/unstaged-diff", h.diff(DiffKindUnstaged))
	r.Get("/git/unpushed-commits", h.unpushedCommits)
	r.Get("/git/open-files", h.openFiles)
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

func writeServiceProblem(w http.ResponseWriter, err error) {
	status, code, title := mapProjectError(err)
	detail := err.Error()
	writeProblem(w, status, schemas.Problem{
		Type:   "urn:hoopoe:" + code,
		Title:  title,
		Status: status,
		Code:   code,
		Detail: &detail,
	})
}

func writeProblem(w http.ResponseWriter, status int, problem schemas.Problem) {
	w.Header().Set("Content-Type", "application/problem+json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(problem)
}

func writeJSON(w http.ResponseWriter, status int, response any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(response)
}
