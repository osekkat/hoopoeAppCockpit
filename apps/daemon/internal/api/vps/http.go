package vps

import (
	"bytes"
	"encoding/json"
	"net/http"
	"sort"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/hoopoe-cockpit/hoopoe/apps/daemon/internal/capabilities"
	"github.com/hoopoe-cockpit/hoopoe/apps/daemon/internal/search"
	schemas "github.com/hoopoe-cockpit/hoopoe/packages/schemas/go"
)

// gitReadRouteCaps maps each VPS Git read route to the fully qualified
// capability refs it requires from the daemon /v1/capabilities registry.
// A route is served only when every required cap is StatusOK or
// StatusDegraded; missing/untested/blocked-by-policy short-circuits to a
// 503 problem envelope.
var gitReadRouteCaps = struct {
	Status          []string
	Diff            []string
	UnpushedCommits []string
	OpenFiles       []string
	Grep            []string
}{
	Status:          []string{"git.status.read"},
	Diff:            []string{"git.diff.read"},
	UnpushedCommits: []string{"git.unpushed.list"},
	OpenFiles:       []string{"git.status.read"},
	Grep:            []string{"git.grep"},
}

func MountGitRoutes(r chi.Router, cfg Config) {
	h := &handler{
		service: NewService(cfg),
		caps:    cfg.Capabilities,
		logger:  cfg.Logger,
	}
	r.Get("/git/status", h.gateRead(gitReadRouteCaps.Status, h.status))
	r.Get("/git/staged-diff", h.gateRead(gitReadRouteCaps.Diff, h.diff(DiffKindStaged)))
	r.Get("/git/unstaged-diff", h.gateRead(gitReadRouteCaps.Diff, h.diff(DiffKindUnstaged)))
	r.Get("/git/unpushed-commits", h.gateRead(gitReadRouteCaps.UnpushedCommits, h.unpushedCommits))
	r.Get("/git/open-files", h.gateRead(gitReadRouteCaps.OpenFiles, h.openFiles))
	r.Get("/grep", h.gateRead(gitReadRouteCaps.Grep, h.grep))
}

type handler struct {
	service *Service
	caps    CapabilityChecker
	logger  Logger
}

// gateRead short-circuits a VPS Git read handler when the daemon capability
// registry reports any required cap as missing, untested, or blocked by
// policy. Degraded is allowed through but logged so the desktop's degraded
// indicator and the audit log can reflect the partial guarantee. When the
// registry is not configured the gate is a no-op for backward compatibility
// with tests and minimal deployments.
func (h *handler) gateRead(refs []string, next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if h.caps == nil || len(refs) == 0 {
			next(w, r)
			return
		}
		var unavailable, blocked, degraded []string
		for _, ref := range refs {
			status, ok := h.caps.LookupCapabilityStatus(ref)
			switch {
			case !ok || status == capabilities.StatusMissing || status == capabilities.StatusUntested:
				unavailable = append(unavailable, ref)
			case status == capabilities.StatusBlockedByPolicy:
				blocked = append(blocked, ref)
			case status == capabilities.StatusDegraded:
				degraded = append(degraded, ref)
			}
		}
		if len(unavailable) > 0 || len(blocked) > 0 {
			sort.Strings(unavailable)
			sort.Strings(blocked)
			detail := strings.Builder{}
			if len(unavailable) > 0 {
				detail.WriteString("missing or untested capabilities: ")
				detail.WriteString(strings.Join(unavailable, ", "))
			}
			if len(blocked) > 0 {
				if detail.Len() > 0 {
					detail.WriteString("; ")
				}
				detail.WriteString("blocked-by-policy: ")
				detail.WriteString(strings.Join(blocked, ", "))
			}
			detailStr := detail.String()
			writeProblem(w, http.StatusServiceUnavailable, schemas.Problem{
				Type:   "urn:hoopoe:problem:git-read-capabilities-unavailable",
				Title:  "required capabilities unavailable",
				Status: http.StatusServiceUnavailable,
				Code:   "git.read.capabilities_unavailable",
				Detail: &detailStr,
			})
			return
		}
		if len(degraded) > 0 && h.logger != nil {
			sort.Strings(degraded)
			h.logger.Info(r.Context(), "vps_git_read_capabilities_degraded", map[string]any{
				"path":         r.URL.Path,
				"capabilities": degraded,
			})
		}
		next(w, r)
	}
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
