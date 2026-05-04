package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/hoopoe-cockpit/hoopoe/apps/daemon/internal/api/vps"
	"github.com/hoopoe-cockpit/hoopoe/apps/daemon/internal/approvals"
	"github.com/hoopoe-cockpit/hoopoe/apps/daemon/internal/capabilities"
	"github.com/hoopoe-cockpit/hoopoe/apps/daemon/internal/jobs"
	"github.com/hoopoe-cockpit/hoopoe/apps/daemon/internal/projects"
	schemas "github.com/hoopoe-cockpit/hoopoe/packages/schemas/go"
)

// projectImportRequiredCaps lists the canonical capability refs that must be
// available before /v1/projects (POST) runs git rev-parse / remote / branch
// reads and `br init`. Mirrors the gates the projects.Service exercises but
// consulted at the HTTP boundary so a degraded toolchain surfaces a problem
// envelope instead of letting the import optimistically run + fail mid-flight.
var projectImportRequiredCaps = []string{
	"git.status.read",
	"git.remote.read",
	"git.branch.read",
	"br.create",
}

// projectReadinessRequiredCaps lists the canonical capability refs that must
// be available before /v1/projects/{id}/readiness re-runs Git reads. The
// readiness handler does not invoke br, only Git inspection.
var projectReadinessRequiredCaps = []string{
	"git.status.read",
	"git.remote.read",
	"git.branch.read",
}

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
	if s.upgrade != nil {
		r.Method(http.MethodGet, "/v1/bootstrap/daemon/upgrade", s.upgrade)
		r.Method(http.MethodPost, "/v1/bootstrap/daemon/upgrade", s.upgrade)
	} else {
		r.Post("/v1/bootstrap/daemon/upgrade", s.handlePlannedWrite("bootstrap.daemon.upgrade"))
	}

	r.Route("/v1/jobs/{jobId}", func(r chi.Router) {
		r.Post("/cancel", s.handleJobCancel)
		r.Get("/log", s.handleJobLog)
		r.Get("/artifacts", s.handleJobArtifacts)
	})

	r.Get("/v1/projects", s.handleProjects)
	r.Post("/v1/projects", s.handleProjectCreate)
	r.Route("/v1/projects/{projectId}", func(r chi.Router) {
		r.Get("/", s.handleProject)
		r.Post("/activate", s.handlePlannedWrite("projects.activate"))
		r.Get("/readiness", s.handleProjectReadiness)

		gitCfg := vps.Config{
			Projects: s.projects,
			Logger:   s.logger,
			Now:      s.now,
		}
		if s.capabilities != nil {
			// Avoid the typed-nil interface trap: passing a nil
			// *capabilities.Registry through the CapabilityChecker
			// interface field would yield a non-nil interface value that
			// nil-panics on the first lookup.
			gitCfg.Capabilities = s.capabilities
		}
		vps.MountGitRoutes(r, gitCfg)
		r.Post("/git/push", s.handlePlannedWrite("git.push"))

		r.Get("/plans", s.handlePlans)
		r.Post("/plans", s.handlePlannedWrite("plans.create"))
		r.Post("/plans/{planId}/rounds", s.handlePlannedWrite("plans.rounds"))
		r.Post("/plans/{planId}/lock", s.handlePlannedWrite("plans.lock"))
		r.Get("/plans/{planId}/artifacts", s.handlePlannedRead("plans.artifacts"))

		r.Get("/beads", s.handleBeads)
		r.Get("/beads/graph", s.handlePlannedRead("beads.graph"))
		r.Get("/beads/ready", s.handleBeadsReady)
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
		r.Get("/approvals/{approvalId}", s.handleApproval)
		r.Post("/approvals/{approvalId}/approve", s.handleApprovalApprove)
		r.Post("/approvals/{approvalId}/deny", s.handleApprovalDeny)
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
	s.handleJobLogChunk(w, r)
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

func (s *server) handleProjects(w http.ResponseWriter, r *http.Request) {
	if s.projects == nil {
		writeJSON(w, http.StatusOK, schemas.ProjectListResponse{
			Items: []schemas.Project{},
			Page:  emptyPageMeta(),
		})
		return
	}
	items, err := s.projects.List(r.Context())
	if err != nil {
		s.writeProjectError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, schemas.ProjectListResponse{
		Items: items,
		Page:  pageMeta(len(items)),
	})
}

func (s *server) handleProject(w http.ResponseWriter, r *http.Request) {
	projectID, ok := s.projectIDParam(w, r)
	if !ok {
		return
	}
	if s.projects == nil {
		s.writeProblemCode(w, http.StatusNotFound, "project.not_found", "project not found", fmt.Sprintf("project %q is not registered in the seed daemon", projectID))
		return
	}
	project, err := s.projects.Project(r.Context(), projectID)
	if err != nil {
		s.writeProjectError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, project)
}

func (s *server) handleProjectReadiness(w http.ResponseWriter, r *http.Request) {
	projectID, ok := s.projectIDParam(w, r)
	if !ok {
		return
	}
	if !s.requireProjectCapabilities(w, "projects.readiness", projectReadinessRequiredCaps) {
		return
	}
	if s.projects != nil {
		readiness, err := s.projects.Readiness(r.Context(), projectID)
		if err != nil {
			s.writeProjectError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, readiness)
		return
	}
	state := schemas.ProjectLifecycleStateImported
	detail := "project registry adapter is not configured in the seed daemon"
	writeJSON(w, http.StatusOK, schemas.ProjectReadiness{
		SchemaVersion:         schemaVersion,
		ProjectId:             projectID,
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

func (s *server) projectIDParam(w http.ResponseWriter, r *http.Request) (string, bool) {
	projectID := strings.TrimSpace(chi.URLParam(r, "projectId"))
	if !validProjectID(projectID) {
		s.writeProblemCode(w, http.StatusBadRequest, "projects.invalid_id", "invalid project id", "projectId must be 1-128 characters of letters, numbers, dot, dash, or underscore")
		return "", false
	}
	return projectID, true
}

func validProjectID(projectID string) bool {
	if projectID == "" || len(projectID) > 128 {
		return false
	}
	for _, r := range projectID {
		if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '.' || r == '-' || r == '_' {
			continue
		}
		return false
	}
	return true
}

func (s *server) handleProjectCreate(w http.ResponseWriter, r *http.Request) {
	if s.projects == nil {
		s.writeProblemCode(w, http.StatusNotImplemented, "projects.create.unavailable", "endpoint unavailable", "project registry is not configured")
		return
	}
	if !s.requireProjectCapabilities(w, "projects.create", projectImportRequiredCaps) {
		return
	}
	idempotencyKey, ok := s.requireIdempotencyKey(w, r, "POST /v1/projects")
	if !ok {
		return
	}
	var body schemas.ProjectCreateRequest
	if err := decodeRequiredJSON(r, &body); err != nil {
		s.writeProblemCode(w, http.StatusBadRequest, "request.invalid_json", "invalid request body", err.Error())
		return
	}
	request := projects.ImportRequest{ProjectCreateRequest: body}
	request.IdempotencyKey = idempotencyKey
	project, err := s.projects.Import(r.Context(), request)
	if err != nil {
		s.writeProjectError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, project)
}

// ErrBeadNotFound is returned by BeadsReader.Bead when the requested bead id
// does not resolve in the underlying adapter. It maps to a 404 problem
// envelope; any other error maps to 500.
var ErrBeadNotFound = errors.New("bead not found")

// PlansReader yields the plan list for /v1/projects/{id}/plans. Until a
// reader is configured the route returns 501 plans.unavailable instead of an
// empty 200 — silent empties are indistinguishable from "project genuinely
// has no plans" and would let Stage 02 gate on a phantom success.
type PlansReader interface {
	ListPlans(ctx context.Context, projectID string) (schemas.PlanListResponse, error)
}

// BeadsReader yields list / ready / show responses for the
// /v1/projects/{id}/beads* routes. Until a reader is configured those routes
// return 501 beads.unavailable so callers can distinguish "br adapter not
// wired" from "project has no beads."
type BeadsReader interface {
	ListBeads(ctx context.Context, projectID string) (schemas.BeadListResponse, error)
	ReadyBeads(ctx context.Context, projectID string) (schemas.BeadListResponse, error)
	Bead(ctx context.Context, projectID, beadID string) (schemas.Bead, error)
}

func (s *server) handlePlans(w http.ResponseWriter, r *http.Request) {
	projectID, ok := s.projectIDParam(w, r)
	if !ok {
		return
	}
	if s.plans == nil {
		s.writeProblemCode(w, http.StatusNotImplemented, "plans.unavailable", "endpoint unavailable", "the planning adapter is not configured in the seed daemon")
		return
	}
	response, err := s.plans.ListPlans(r.Context(), projectID)
	if err != nil {
		s.writeProblemCode(w, http.StatusInternalServerError, "plans.error", "plan list failed", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, response)
}

func (s *server) handleBeads(w http.ResponseWriter, r *http.Request) {
	projectID, ok := s.projectIDParam(w, r)
	if !ok {
		return
	}
	if s.beads == nil {
		s.writeProblemCode(w, http.StatusNotImplemented, "beads.unavailable", "endpoint unavailable", "the beads adapter is not configured in the seed daemon")
		return
	}
	response, err := s.beads.ListBeads(r.Context(), projectID)
	if err != nil {
		s.writeProblemCode(w, http.StatusInternalServerError, "beads.error", "bead list failed", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, response)
}

func (s *server) handleBeadsReady(w http.ResponseWriter, r *http.Request) {
	projectID, ok := s.projectIDParam(w, r)
	if !ok {
		return
	}
	if s.beads == nil {
		s.writeProblemCode(w, http.StatusNotImplemented, "beads.unavailable", "endpoint unavailable", "the beads adapter is not configured in the seed daemon")
		return
	}
	response, err := s.beads.ReadyBeads(r.Context(), projectID)
	if err != nil {
		s.writeProblemCode(w, http.StatusInternalServerError, "beads.error", "ready beads failed", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, response)
}

func (s *server) handleBead(w http.ResponseWriter, r *http.Request) {
	projectID, ok := s.projectIDParam(w, r)
	if !ok {
		return
	}
	if s.beads == nil {
		s.writeProblemCode(w, http.StatusNotImplemented, "beads.unavailable", "endpoint unavailable", "the beads adapter is not configured in the seed daemon")
		return
	}
	beadID := strings.TrimSpace(chi.URLParam(r, "beadId"))
	bead, err := s.beads.Bead(r.Context(), projectID, beadID)
	if err != nil {
		if errors.Is(err, ErrBeadNotFound) {
			s.writeProblemCode(w, http.StatusNotFound, "bead.not_found", "bead not found", fmt.Sprintf("bead %q is not available", beadID))
			return
		}
		s.writeProblemCode(w, http.StatusInternalServerError, "bead.error", "bead lookup failed", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, bead)
}

func (s *server) handleApprovals(w http.ResponseWriter, r *http.Request) {
	if s.approvals == nil {
		s.writeProblemCode(w, http.StatusNotImplemented, "approvals.unavailable", "approvals unavailable", "the unified approval queue is not configured")
		return
	}
	items, err := s.approvals.List(r.Context(), approvals.ListFilter{
		ProjectID:      chi.URLParam(r, "projectId"),
		IncludeExpired: true,
	})
	if err != nil {
		s.writeApprovalQueueError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, schemas.ApprovalListResponse{
		Items: items,
		Page:  pageMeta(len(items)),
	})
}

func (s *server) handleApproval(w http.ResponseWriter, r *http.Request) {
	if s.approvals == nil {
		s.writeProblemCode(w, http.StatusNotImplemented, "approvals.unavailable", "approvals unavailable", "the unified approval queue is not configured")
		return
	}
	approval, ok, err := (ApprovalQueueLookup{Queue: s.approvals}).LookupApproval(r.Context(), chi.URLParam(r, "approvalId"))
	if err != nil {
		s.writeApprovalQueueError(w, err)
		return
	}
	if !ok {
		s.writeProblemCode(w, http.StatusNotFound, "approvals.not_found", "approval not found", "approval id does not exist")
		return
	}
	writeJSON(w, http.StatusOK, approval)
}

func (s *server) handleApprovalApprove(w http.ResponseWriter, r *http.Request) {
	s.handleApprovalDecision(w, r, true)
}

func (s *server) handleApprovalDeny(w http.ResponseWriter, r *http.Request) {
	s.handleApprovalDecision(w, r, false)
}

func (s *server) handleApprovalDecision(w http.ResponseWriter, r *http.Request, approve bool) {
	if s.approvals == nil {
		s.writeProblemCode(w, http.StatusNotImplemented, "approvals.unavailable", "approvals unavailable", "the unified approval queue is not configured")
		return
	}
	verb := "approve"
	if !approve {
		verb = "deny"
	}
	if _, ok := s.requireIdempotencyKey(w, r, "POST /v1/projects/{projectId}/approvals/{approvalId}/"+verb); !ok {
		return
	}
	var request schemas.ApprovalDecisionRequest
	if err := decodeRequiredJSON(r, &request); err != nil {
		s.writeProblemCode(w, http.StatusBadRequest, "request.invalid_json", "invalid request body", err.Error())
		return
	}
	var (
		approval schemas.Approval
		err      error
	)
	approvalID := chi.URLParam(r, "approvalId")
	if approve {
		approval, err = s.approvals.Approve(r.Context(), approvalID, request)
	} else {
		approval, err = s.approvals.Deny(r.Context(), approvalID, request)
	}
	if err != nil {
		s.writeApprovalQueueError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, approval)
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

// requireProjectCapabilities consults the capability registry for each
// capRef and returns false (after writing a problem envelope) if any required
// capability is absent, untested, or blocked by policy. Degraded statuses
// pass — the caller is expected to surface degradation in its own response
// payload (e.g., readiness gates).
//
// When s.capabilities is nil (daemon was started without a registry — e.g.,
// minimal test routers), this gate is a no-op so unrelated tests keep
// working. Production wires Config.Capabilities; the capability route itself
// 503s on nil registry.
func (s *server) requireProjectCapabilities(w http.ResponseWriter, code string, refs []string) bool {
	if s.capabilities == nil {
		return true
	}
	var unavailable, blocked []string
	for _, ref := range refs {
		status, ok := s.capabilities.LookupCapabilityStatus(ref)
		switch {
		case !ok || status == capabilities.StatusMissing || status == capabilities.StatusUntested:
			unavailable = append(unavailable, ref)
		case status == capabilities.StatusBlockedByPolicy:
			blocked = append(blocked, ref)
		}
	}
	if len(unavailable) == 0 && len(blocked) == 0 {
		return true
	}
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
	s.writeProblemCode(w, http.StatusServiceUnavailable, code+".capabilities_unavailable", "required capabilities unavailable", detail.String())
	return false
}

// requireIdempotencyKey enforces the OpenAPI Idempotency-Key contract for
// retryable mutating routes. Returns the trimmed key and true on success;
// writes a 400 idempotency.required problem and returns false when the
// header is missing or empty. Per plan.md §2.6 every retryable mutating
// route must accept the header so a network drop after the server has
// processed the request lets the client retry without double-applying.
//
// hp-0uh: this is the daemon-side helper; the OpenAPI side (which routes
// declare it required) and the desktop harness side (idempotency-contract
// table) are tracked in separate followups so the contract decision is
// made by COD1 + desktop before the daemon enforces it across all routes.
// This helper currently fronts handleProjectCreate and the unified
// approval decision routes.
func (s *server) requireIdempotencyKey(w http.ResponseWriter, r *http.Request, route string) (string, bool) {
	key := strings.TrimSpace(r.Header.Get("Idempotency-Key"))
	if key == "" {
		s.writeProblemCode(w, http.StatusBadRequest, "idempotency.required", "idempotency key required", route+" requires Idempotency-Key")
		return "", false
	}
	return key, true
}

func (s *server) writeJobError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, ErrJobsReaderUnavailable):
		s.writeProblemCode(w, http.StatusServiceUnavailable, "jobs.registry_unavailable", "job registry unavailable", err.Error())
	case errors.Is(err, jobs.ErrNotFound):
		s.writeProblemCode(w, http.StatusNotFound, "jobs.not_found", "job not found", err.Error())
	case errors.Is(err, jobs.ErrInvalidRequest):
		s.writeProblemCode(w, http.StatusBadRequest, "jobs.invalid_request", "invalid job request", err.Error())
	case errors.Is(err, jobs.ErrInvalidState):
		s.writeProblemCode(w, http.StatusConflict, "jobs.invalid_state", "invalid job state", err.Error())
	default:
		s.writeProblemCode(w, http.StatusInternalServerError, "jobs.error", "job request failed", err.Error())
	}
}

func (s *server) writeProjectError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, projects.ErrNotFound):
		s.writeProblemCode(w, http.StatusNotFound, "project.not_found", "project not found", err.Error())
	case errors.Is(err, projects.ErrInvalidRequest):
		s.writeProblemCode(w, http.StatusBadRequest, "projects.invalid_request", "invalid project request", err.Error())
	case errors.Is(err, projects.ErrNotGitRepo):
		s.writeProblemCode(w, http.StatusUnprocessableEntity, "projects.not_git_repo", "not a git repository", err.Error())
	case errors.Is(err, projects.ErrMissingOrigin):
		s.writeProblemCode(w, http.StatusUnprocessableEntity, "projects.missing_origin", "origin remote required", err.Error())
	case errors.Is(err, projects.ErrDetachedHead):
		s.writeProblemCode(w, http.StatusUnprocessableEntity, "projects.detached_head", "branch required", err.Error())
	case errors.Is(err, projects.ErrIdempotencyConflict):
		s.writeProblemCode(w, http.StatusConflict, "idempotency.conflict", "idempotency key conflict", err.Error())
	case errors.Is(err, projects.ErrCommandFailed):
		s.writeProblemCode(w, http.StatusBadGateway, "projects.command_failed", "project command failed", err.Error())
	default:
		s.writeProblemCode(w, http.StatusInternalServerError, "projects.error", "project request failed", err.Error())
	}
}

func (s *server) writeApprovalQueueError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, approvals.ErrNotFound):
		s.writeProblemCode(w, http.StatusNotFound, "approvals.not_found", "approval not found", err.Error())
	case errors.Is(err, approvals.ErrInvalidRequest):
		s.writeProblemCode(w, http.StatusBadRequest, "approvals.invalid_request", "invalid approval request", err.Error())
	case errors.Is(err, approvals.ErrInvalidTransition):
		s.writeProblemCode(w, http.StatusConflict, "approvals.invalid_transition", "invalid approval transition", err.Error())
	case errors.Is(err, approvals.ErrExpired):
		s.writeProblemCode(w, http.StatusConflict, "approvals.expired", "approval expired", err.Error())
	default:
		s.writeProblemCode(w, http.StatusInternalServerError, "approvals.error", "approval request failed", err.Error())
	}
}

func pageMeta(total int) schemas.PageMeta {
	return schemas.PageMeta{HasMore: false, Total: &total}
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
