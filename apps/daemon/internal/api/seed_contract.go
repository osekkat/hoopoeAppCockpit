package api

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/hoopoe-cockpit/hoopoe/apps/daemon/internal/jobs"
	schemas "github.com/hoopoe-cockpit/hoopoe/packages/schemas/go"
)

func (s *server) mountSeedContractRoutes(r chi.Router) {
	r.Get("/v1/system/specs", s.handleSystemSpecs)
	r.Get("/v1/system/processes", s.handleSystemProcesses)

	r.Post("/v1/auth/bootstrap/bearer", s.handlePlannedWrite("auth.bootstrap.bearer"))
	r.Post("/v1/auth/ws-token", s.handlePlannedWrite("auth.ws_token"))
	r.Post("/v1/auth/session/revoke", s.handlePlannedWrite("auth.session.revoke"))
	r.Post("/v1/auth/rotate-secret", s.handlePlannedWrite("auth.rotate_secret"))
	r.Get("/v1/events/ws-token", s.handlePlannedRead("events.ws_token"))

	r.Post("/v1/bootstrap/preflight", s.handlePlannedWrite("bootstrap.preflight"))
	r.Post("/v1/bootstrap/acfs/start", s.handlePlannedWrite("bootstrap.acfs.start"))
	r.Post("/v1/bootstrap/acfs/resume", s.handlePlannedWrite("bootstrap.acfs.resume"))
	r.Post("/v1/bootstrap/daemon/upgrade", s.handlePlannedWrite("bootstrap.daemon.upgrade"))

	r.Route("/v1/jobs/{jobId}", func(r chi.Router) {
		r.Post("/cancel", s.handleJobCancel)
		r.Get("/log", s.handleJobLog)
		r.Get("/artifacts", s.handleJobArtifacts)
	})

	r.Get("/v1/projects", s.handleProjects)
	r.Post("/v1/projects", s.handlePlannedWrite("projects.create"))
	r.Route("/v1/projects/{projectId}", func(r chi.Router) {
		r.Get("/", s.handleProject)
		r.Post("/activate", s.handlePlannedWrite("projects.activate"))
		r.Get("/readiness", s.handleProjectReadiness)

		r.Get("/git/status", s.handlePlannedRead("git.status"))
		r.Get("/git/staged-diff", s.handlePlannedRead("git.staged_diff"))
		r.Get("/git/unstaged-diff", s.handlePlannedRead("git.unstaged_diff"))
		r.Get("/git/unpushed-commits", s.handlePlannedRead("git.unpushed_commits"))
		r.Post("/git/push", s.handlePlannedWrite("git.push"))

		r.Get("/plans", s.handlePlans)
		r.Post("/plans", s.handlePlannedWrite("plans.create"))
		r.Post("/plans/{planId}/rounds", s.handlePlannedWrite("plans.rounds"))
		r.Post("/plans/{planId}/lock", s.handlePlannedWrite("plans.lock"))
		r.Get("/plans/{planId}/artifacts", s.handlePlannedRead("plans.artifacts"))

		r.Get("/beads", s.handleBeads)
		r.Get("/beads/graph", s.handlePlannedRead("beads.graph"))
		r.Get("/beads/ready", s.handleBeads)
		r.Get("/beads/{beadId}", s.handleBead)
		r.Patch("/beads/{beadId}", s.handlePlannedWrite("beads.patch"))
		r.Post("/beads/conversion-runs", s.handlePlannedWrite("beads.conversion_runs"))
		r.Post("/beads/polish-runs", s.handlePlannedWrite("beads.polish_runs"))

		r.Post("/swarms", s.handlePlannedWrite("swarms.create"))
		r.Get("/swarms/{swarmId}", s.handlePlannedRead("swarms.get"))
		r.Post("/swarms/{swarmId}/broadcast", s.handlePlannedWrite("swarms.broadcast"))
		r.Post("/agents/{agentId}/send", s.handlePlannedWrite("agents.send"))
		r.Post("/agents/{agentId}/interrupt", s.handlePlannedWrite("agents.interrupt"))
		r.Post("/agents/{agentId}/stop", s.handlePlannedWrite("agents.stop"))

		r.Get("/mail/messages", s.handlePlannedRead("mail.messages"))
		r.Post("/mail/messages", s.handlePlannedWrite("mail.messages.create"))
		r.Get("/mail/threads/{threadId}", s.handlePlannedRead("mail.threads"))
		r.Get("/reservations", s.handlePlannedRead("reservations.list"))
		r.Post("/reservations/force-release", s.handlePlannedWrite("reservations.force_release"))

		r.Get("/health/summary", s.handlePlannedRead("health.summary"))
		r.Get("/health/files", s.handlePlannedRead("health.files"))
		r.Post("/health/snapshots", s.handlePlannedWrite("health.snapshots"))

		r.Post("/reviews", s.handlePlannedWrite("reviews.create"))
		r.Get("/reviews/{reviewId}", s.handlePlannedRead("reviews.get"))
		r.Get("/findings", s.handlePlannedRead("findings.list"))
		r.Patch("/findings/{findingId}", s.handlePlannedWrite("findings.patch"))

		r.Get("/tending/jobs", s.handlePlannedRead("tending.jobs"))
		r.Post("/tending/jobs/{jobId}/run", s.handlePlannedWrite("tending.jobs.run"))
		r.Patch("/tending/jobs/{jobId}", s.handlePlannedWrite("tending.jobs.patch"))
		r.Post("/tending/actionplans", s.handleActionPlan)

		r.Get("/approvals", s.handleApprovals)
		r.Get("/approvals/{approvalId}", s.handlePlannedRead("approvals.get"))
		r.Post("/approvals/{approvalId}/approve", s.handleApprovalDecision)
		r.Post("/approvals/{approvalId}/deny", s.handleApprovalDecision)
		r.Post("/approvals/{approvalId}/extend", s.handlePlannedWrite("approvals.extend"))
	})
}

func (s *server) handleSystemSpecs(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"schemaVersion":        schemaVersion,
		"openapi":              "packages/schemas/openapi.yaml",
		"websocketEnvelope":    "packages/schemas/events/ws-envelope.schema.json",
		"tendingActionsSchema": "packages/schemas/tending-actions.yaml",
	})
}

func (s *server) handleSystemProcesses(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"schemaVersion": schemaVersion,
		"processes":     []any{},
	})
}

func (s *server) handleJobCancel(w http.ResponseWriter, r *http.Request) {
	var request schemas.JobCancelRequest
	if err := decodeOptionalJSON(r, &request); err != nil {
		s.writeProblemCode(w, http.StatusBadRequest, "request.invalid_json", "invalid request body", err.Error())
		return
	}
	controller, ok := s.jobs.(jobs.Controller)
	if !ok {
		s.writeProblemCode(w, http.StatusNotImplemented, "jobs.cancel_unavailable", "job cancellation unavailable", "the configured job registry is read-only")
		return
	}
	jobID := chi.URLParam(r, "jobId")
	reason := ""
	if request.Reason != nil {
		reason = *request.Reason
	}
	job, err := controller.Cancel(r.Context(), jobs.CancelRequest{
		JobID:  jobID,
		Actor:  "api",
		Reason: reason,
		Audit: jobs.AuditMetadata{
			Actor:         "api",
			Reason:        reason,
			RequestID:     r.Header.Get("X-Request-Id"),
			CorrelationID: r.Header.Get("Idempotency-Key"),
		},
	})
	if err != nil {
		s.writeJobError(w, err)
		return
	}
	writeJSON(w, http.StatusAccepted, jobResponse(job))
}

func (s *server) handleJobLog(w http.ResponseWriter, r *http.Request) {
	offset, err := parseInt64Query(r, "offset", 0)
	if err != nil {
		s.writeProblemCode(w, http.StatusBadRequest, "request.invalid_offset", "invalid offset", err.Error())
		return
	}
	limit, err := parseInt64Query(r, "limit", 64*1024)
	if err != nil {
		s.writeProblemCode(w, http.StatusBadRequest, "request.invalid_limit", "invalid limit", err.Error())
		return
	}
	chunk, err := s.jobs.ReadLog(r.Context(), chi.URLParam(r, "jobId"), offset, limit)
	if err != nil {
		s.writeJobError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, chunk)
}

func (s *server) handleJobArtifacts(w http.ResponseWriter, r *http.Request) {
	artifacts, err := s.jobs.ListArtifacts(r.Context(), chi.URLParam(r, "jobId"))
	if err != nil {
		s.writeJobError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"schemaVersion": schemaVersion,
		"artifacts":     artifacts,
	})
}

func (s *server) handleProjects(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, schemas.ProjectListResponse{
		Items: []schemas.Project{},
		Page:  emptyPageMeta(),
	})
}

func (s *server) handleProject(w http.ResponseWriter, r *http.Request) {
	s.writeProblemCode(w, http.StatusNotFound, "project.not_found", "project not found", fmt.Sprintf("project %q is not registered in the seed daemon", chi.URLParam(r, "projectId")))
}

func (s *server) handleProjectReadiness(w http.ResponseWriter, r *http.Request) {
	state := schemas.ProjectLifecycleStateImported
	detail := "project registry adapter is not configured in the seed daemon"
	writeJSON(w, http.StatusOK, schemas.ProjectReadiness{
		SchemaVersion:         schemaVersion,
		ProjectId:             chi.URLParam(r, "projectId"),
		CheckedAt:             s.now().UTC(),
		CurrentLifecycleState: &state,
		Gates: []schemas.ProjectReadinessGate{{
			Gate:      schemas.ProjectGateProjectImported,
			Satisfied: false,
			Checks: []schemas.GateCheck{{
				Id:     "project_registry_configured",
				Ok:     false,
				Detail: &detail,
			}},
		}},
	})
}

func (s *server) handlePlans(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, schemas.PlanListResponse{
		Items: []schemas.Plan{},
		Page:  emptyPageMeta(),
	})
}

func (s *server) handleBeads(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, schemas.BeadListResponse{
		Items: []schemas.Bead{},
		Page:  emptyPageMeta(),
	})
}

func (s *server) handleBead(w http.ResponseWriter, r *http.Request) {
	s.writeProblemCode(w, http.StatusNotFound, "bead.not_found", "bead not found", fmt.Sprintf("bead %q is not available in the seed daemon", chi.URLParam(r, "beadId")))
}

func (s *server) handleApprovals(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, schemas.ApprovalListResponse{
		Items: []schemas.Approval{},
		Page:  emptyPageMeta(),
	})
}

func (s *server) handleApprovalDecision(w http.ResponseWriter, r *http.Request) {
	var request schemas.ApprovalDecisionRequest
	if err := decodeRequiredJSON(r, &request); err != nil {
		s.writeProblemCode(w, http.StatusBadRequest, "request.invalid_json", "invalid request body", err.Error())
		return
	}
	s.writeProblemCode(w, http.StatusNotImplemented, "approvals.decide_unavailable", "approval decision unavailable", "approval persistence is not configured in the seed daemon")
}

func (s *server) handleActionPlan(w http.ResponseWriter, r *http.Request) {
	var request schemas.ActionPlan
	if err := decodeRequiredJSON(r, &request); err != nil {
		s.writeProblemCode(w, http.StatusBadRequest, "request.invalid_json", "invalid request body", err.Error())
		return
	}
	s.writeProblemCode(w, http.StatusNotImplemented, "tending.actionplan_unavailable", "action plan execution unavailable", "the tending executor is not configured in the seed daemon")
}

func (s *server) handlePlannedRead(code string) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		s.writeProblemCode(w, http.StatusNotImplemented, code+".unavailable", "endpoint unavailable", "this seed contract route is wired but its backing adapter is not configured yet")
	}
}

func (s *server) handlePlannedWrite(code string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		_ = r.Header.Get("Idempotency-Key")
		s.writeProblemCode(w, http.StatusNotImplemented, code+".unavailable", "endpoint unavailable", "this seed contract route accepts Idempotency-Key but its backing adapter is not configured yet")
	}
}

func (s *server) writeJobError(w http.ResponseWriter, err error) {
	switch err {
	case jobs.ErrNotFound:
		s.writeProblemCode(w, http.StatusNotFound, "jobs.not_found", "job not found", err.Error())
	case jobs.ErrInvalidRequest:
		s.writeProblemCode(w, http.StatusBadRequest, "jobs.invalid_request", "invalid job request", err.Error())
	case jobs.ErrInvalidState:
		s.writeProblemCode(w, http.StatusConflict, "jobs.invalid_state", "invalid job state", err.Error())
	default:
		s.writeProblemCode(w, http.StatusInternalServerError, "jobs.error", "job request failed", err.Error())
	}
}

func decodeOptionalJSON(r *http.Request, target any) error {
	if r.Body == nil || r.ContentLength == 0 {
		return nil
	}
	return decodeRequiredJSON(r, target)
}

func decodeRequiredJSON(r *http.Request, target any) error {
	dec := json.NewDecoder(io.LimitReader(r.Body, 1<<20))
	dec.DisallowUnknownFields()
	if err := dec.Decode(target); err != nil {
		return err
	}
	return nil
}

func parseInt64Query(r *http.Request, name string, fallback int64) (int64, error) {
	raw := strings.TrimSpace(r.URL.Query().Get(name))
	if raw == "" {
		return fallback, nil
	}
	value, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || value < 0 {
		return 0, fmt.Errorf("%s must be a non-negative integer", name)
	}
	return value, nil
}
