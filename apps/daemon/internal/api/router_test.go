package api

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/hoopoe-cockpit/hoopoe/apps/daemon/internal/approvals"
	"github.com/hoopoe-cockpit/hoopoe/apps/daemon/internal/capabilities"
	jobstore "github.com/hoopoe-cockpit/hoopoe/apps/daemon/internal/jobs"
	projectstore "github.com/hoopoe-cockpit/hoopoe/apps/daemon/internal/projects"
	"github.com/hoopoe-cockpit/hoopoe/apps/daemon/internal/redaction"
	schemas "github.com/hoopoe-cockpit/hoopoe/packages/schemas/go"
	_ "modernc.org/sqlite"
	"nhooyr.io/websocket"
)

func TestHealthRoundTrip(t *testing.T) {
	router := NewRouter(Config{
		Now: func() time.Time {
			return time.Unix(10, 0).UTC()
		},
	})

	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}

	var body struct {
		OK            bool   `json:"ok"`
		SchemaVersion int    `json:"schemaVersion"`
		Time          string `json:"time"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode health response: %v", err)
	}
	if !body.OK {
		t.Fatal("health response ok=false")
	}
	if body.SchemaVersion != schemaVersion {
		t.Fatalf("schemaVersion = %d, want %d", body.SchemaVersion, schemaVersion)
	}
}

func TestJobsRoundTripUsesRegistryReader(t *testing.T) {
	created := time.Unix(20, 0).UTC()
	router := NewRouter(Config{
		Jobs: staticJobReader{
			jobs: []jobstore.Job{{
				ID:            "job_test",
				Kind:          "daemon.smoke",
				SchemaVersion: jobstore.SchemaVersion,
				Status:        jobstore.StatusQueued,
				CreatedAt:     created,
				UpdatedAt:     created,
			}},
		},
	})

	req := httptest.NewRequest(http.MethodGet, "/v1/jobs", nil)
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}

	var body schemas.JobListResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode jobs response: %v", err)
	}
	if len(body.Items) != 1 {
		t.Fatalf("jobs length = %d, want 1", len(body.Items))
	}
	if body.Items[0].Id != "job_test" || body.Items[0].Type != "daemon.smoke" || body.Items[0].Status != schemas.JobStatusQueued {
		t.Fatalf("unexpected job payload: %+v", body.Items[0])
	}
}

func TestJobRoutesWithoutRegistryReturnUnavailableProblem(t *testing.T) {
	router := NewRouter(Config{})

	for _, tc := range []struct {
		name string
		path string
	}{
		{name: "list", path: "/v1/jobs"},
		{name: "log", path: "/v1/jobs/job_missing/log?offset=0"},
		{name: "artifacts", path: "/v1/jobs/job_missing/artifacts"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, tc.path, nil)
			rec := httptest.NewRecorder()

			router.ServeHTTP(rec, req)

			if rec.Code != http.StatusServiceUnavailable {
				t.Fatalf("status = %d, want %d; body=%s", rec.Code, http.StatusServiceUnavailable, rec.Body.String())
			}
			if got := rec.Header().Get("Content-Type"); !strings.HasPrefix(got, "application/problem+json") {
				t.Fatalf("content-type = %q, want problem+json", got)
			}
			var body schemas.Problem
			if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
				t.Fatalf("decode problem: %v", err)
			}
			if body.Code != "jobs.registry_unavailable" {
				t.Fatalf("problem code = %q, want jobs.registry_unavailable", body.Code)
			}
		})
	}
}

func TestRouterFallbacksReturnGeneratedProblems(t *testing.T) {
	router := NewRouter(Config{})

	for _, tc := range []struct {
		name         string
		method       string
		path         string
		status       int
		code         string
		allowInclude string
	}{
		{
			name:   "not found",
			method: http.MethodGet,
			path:   "/v1/does-not-exist",
			status: http.StatusNotFound,
			code:   "route.not_found",
		},
		{
			name:         "method not allowed",
			method:       http.MethodPost,
			path:         "/v1/version",
			status:       http.StatusMethodNotAllowed,
			code:         "route.method_not_allowed",
			allowInclude: http.MethodGet,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(tc.method, tc.path, nil)
			rec := httptest.NewRecorder()

			router.ServeHTTP(rec, req)

			if rec.Code != tc.status {
				t.Fatalf("status = %d, want %d; body=%s", rec.Code, tc.status, rec.Body.String())
			}
			if got := rec.Header().Get("Content-Type"); got != "application/problem+json; charset=utf-8" {
				t.Fatalf("content-type = %q, want problem+json", got)
			}
			if tc.allowInclude != "" {
				allow := strings.Join(rec.Header().Values("Allow"), ",")
				if !strings.Contains(allow, tc.allowInclude) {
					t.Fatalf("allow = %q, want method %q", allow, tc.allowInclude)
				}
			}
			var body schemas.Problem
			if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
				t.Fatalf("decode problem: %v", err)
			}
			if body.Code != tc.code || body.Status != tc.status {
				t.Fatalf("problem = %+v", body)
			}
		})
	}
}

func TestWriteJSONEncodingFailureReturnsProblem(t *testing.T) {
	rec := httptest.NewRecorder()

	writeJSON(rec, http.StatusOK, map[string]any{"bad": func() {}})

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want %d; body=%s", rec.Code, http.StatusInternalServerError, rec.Body.String())
	}
	if got := rec.Header().Get("Content-Type"); got != "application/problem+json; charset=utf-8" {
		t.Fatalf("content-type = %q, want problem+json", got)
	}
	var body schemas.Problem
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode problem: %v", err)
	}
	if body.Code != "daemon.encoding_failed" || body.Status != http.StatusInternalServerError {
		t.Fatalf("problem = %+v", body)
	}
}

func TestVersionRoundTrip(t *testing.T) {
	router := NewRouter(Config{
		Build: BuildInfo{
			Version:    "1.2.3",
			Commit:     "abc123",
			BuildDate:  "2026-05-03T00:00:00Z",
			APIVersion: "v1",
		},
	})

	req := httptest.NewRequest(http.MethodGet, "/v1/version", nil)
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}

	var body schemas.VersionResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode version response: %v", err)
	}
	if body.SchemaVersion != schemaVersion || body.Daemon.Version != "1.2.3" || body.Daemon.Commit == nil || *body.Daemon.Commit != "abc123" {
		t.Fatalf("unexpected version payload: %+v", body)
	}
}

func TestSeedContractGeneratedSchemaRoundTrips(t *testing.T) {
	now := time.Date(2026, 5, 4, 0, 0, 0, 0, time.UTC)
	approvalQueue := approvals.NewQueue(approvals.Config{Now: func() time.Time { return now }})
	router := NewRouter(Config{
		Now:       func() time.Time { return now },
		Approvals: approvalQueue,
	})

	for _, tc := range []struct {
		name   string
		path   string
		target any
	}{
		{name: "health", path: "/v1/health", target: &schemas.HealthResponse{}},
		{name: "version", path: "/v1/version", target: &schemas.VersionResponse{}},
		{name: "projects", path: "/v1/projects", target: &schemas.ProjectListResponse{}},
		{name: "readiness", path: "/v1/projects/proj_01/readiness", target: &schemas.ProjectReadiness{}},
		// /plans and /beads are deliberately excluded: when no adapter is
		// wired they return 501 plans.unavailable / beads.unavailable
		// (problem+json), not 200 with an empty list. The 501 path is
		// covered by TestPlansAndBeadsRoutesRequireAdapter and the wired
		// path by TestPlansAndBeadsRoutesUseAdapter.
		{name: "approvals", path: "/v1/projects/proj_01/approvals", target: &schemas.ApprovalListResponse{}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, tc.path, nil)
			rec := httptest.NewRecorder()

			router.ServeHTTP(rec, req)

			if rec.Code != http.StatusOK {
				t.Fatalf("status = %d, want %d; body=%s", rec.Code, http.StatusOK, rec.Body.String())
			}
			if err := json.Unmarshal(rec.Body.Bytes(), tc.target); err != nil {
				t.Fatalf("decode generated schema target: %v", err)
			}
		})
	}
}

func TestPlansAndBeadsRoutesRequireAdapter(t *testing.T) {
	// When no PlansReader/BeadsReader is configured the seed contract
	// must return 501 with a problem envelope, never 200 with an empty
	// list — silent empty would be indistinguishable from "project
	// genuinely has no plans/beads."
	router := NewRouter(Config{Now: func() time.Time { return time.Unix(10, 0).UTC() }})

	cases := []struct {
		name     string
		path     string
		wantCode string
	}{
		{name: "plans-list", path: "/v1/projects/proj_01/plans", wantCode: "plans.unavailable"},
		{name: "beads-list", path: "/v1/projects/proj_01/beads", wantCode: "beads.unavailable"},
		{name: "beads-ready", path: "/v1/projects/proj_01/beads/ready", wantCode: "beads.unavailable"},
		{name: "beads-show", path: "/v1/projects/proj_01/beads/hp-1ry", wantCode: "beads.unavailable"},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, tc.path, nil)
			rec := httptest.NewRecorder()
			router.ServeHTTP(rec, req)
			if rec.Code != http.StatusNotImplemented {
				t.Fatalf("status = %d, want %d; body=%s", rec.Code, http.StatusNotImplemented, rec.Body.String())
			}
			if got := rec.Header().Get("Content-Type"); !strings.HasPrefix(got, "application/problem+json") {
				t.Fatalf("content-type = %q, want problem+json", got)
			}
			var problem schemas.Problem
			if err := json.Unmarshal(rec.Body.Bytes(), &problem); err != nil {
				t.Fatalf("decode problem: %v", err)
			}
			if problem.Code != tc.wantCode {
				t.Fatalf("problem.Code = %q, want %q", problem.Code, tc.wantCode)
			}
			if problem.Status != http.StatusNotImplemented {
				t.Fatalf("problem.Status = %d, want %d", problem.Status, http.StatusNotImplemented)
			}
		})
	}
}

func TestPlansAndBeadsRoutesUseAdapter(t *testing.T) {
	// With adapters wired, the routes must delegate and surface real
	// adapter data. /beads and /beads/ready dispatch to different reader
	// methods so callers can tell which column of the Stage 02 board
	// they're looking at.
	plans := &fakePlansReader{
		response: schemas.PlanListResponse{
			Items: []schemas.Plan{{
				Id:            "plan_01",
				ProjectId:     "proj_01",
				SchemaVersion: 1,
				State:         schemas.PlanLifecycleState("draft"),
				CreatedAt:     time.Unix(10, 0).UTC(),
			}},
			Page: schemas.PageMeta{HasMore: false},
		},
	}
	beads := &fakeBeadsReader{
		list: schemas.BeadListResponse{
			Items: []schemas.Bead{{
				Id:            "hp-1ry",
				Title:         "wired",
				IssueType:     schemas.BeadIssueType("task"),
				SchemaVersion: 1,
				Status:        schemas.BeadStatus("open"),
				Priority:      1,
			}},
			Page: schemas.PageMeta{HasMore: false},
		},
		ready: schemas.BeadListResponse{
			Items: []schemas.Bead{{
				Id:            "hp-1ry",
				Title:         "ready",
				IssueType:     schemas.BeadIssueType("task"),
				SchemaVersion: 1,
				Status:        schemas.BeadStatus("open"),
				Priority:      1,
			}},
			Page: schemas.PageMeta{HasMore: false},
		},
		single: schemas.Bead{
			Id:            "hp-1ry",
			Title:         "single",
			IssueType:     schemas.BeadIssueType("task"),
			SchemaVersion: 1,
			Status:        schemas.BeadStatus("open"),
			Priority:      1,
		},
	}
	router := NewRouter(Config{
		Plans: plans,
		Beads: beads,
		Now:   func() time.Time { return time.Unix(10, 0).UTC() },
	})

	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/v1/projects/proj_01/plans", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("plans status = %d, want %d; body=%s", rec.Code, http.StatusOK, rec.Body.String())
	}
	var plansBody schemas.PlanListResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &plansBody); err != nil {
		t.Fatalf("decode plans body: %v", err)
	}
	if len(plansBody.Items) != 1 || plansBody.Items[0].Id != "plan_01" {
		t.Fatalf("plans body = %+v", plansBody)
	}
	if plans.lastProjectID != "proj_01" {
		t.Fatalf("plans reader projectID = %q, want proj_01", plans.lastProjectID)
	}

	rec = httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/v1/projects/proj_01/beads", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("beads status = %d, want %d; body=%s", rec.Code, http.StatusOK, rec.Body.String())
	}
	var beadsBody schemas.BeadListResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &beadsBody); err != nil {
		t.Fatalf("decode beads body: %v", err)
	}
	if len(beadsBody.Items) != 1 || beadsBody.Items[0].Title != "wired" {
		t.Fatalf("beads body = %+v", beadsBody)
	}

	rec = httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/v1/projects/proj_01/beads/ready", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("beads/ready status = %d, want %d; body=%s", rec.Code, http.StatusOK, rec.Body.String())
	}
	var readyBody schemas.BeadListResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &readyBody); err != nil {
		t.Fatalf("decode beads/ready body: %v", err)
	}
	if len(readyBody.Items) != 1 || readyBody.Items[0].Title != "ready" {
		t.Fatalf("beads/ready dispatched to wrong reader method: %+v", readyBody)
	}

	rec = httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/v1/projects/proj_01/beads/hp-1ry", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("beads/{id} status = %d, want %d; body=%s", rec.Code, http.StatusOK, rec.Body.String())
	}
	var showBody schemas.Bead
	if err := json.Unmarshal(rec.Body.Bytes(), &showBody); err != nil {
		t.Fatalf("decode beads/{id} body: %v", err)
	}
	if showBody.Title != "single" {
		t.Fatalf("beads show body = %+v", showBody)
	}
	if beads.lastBeadID != "hp-1ry" {
		t.Fatalf("beads reader beadID = %q, want hp-1ry", beads.lastBeadID)
	}
}

func TestBeadsShowMapsNotFoundError(t *testing.T) {
	// ErrBeadNotFound from the adapter must surface as a 404 problem
	// envelope (bead.not_found), not a 500.
	beads := &fakeBeadsReader{singleErr: ErrBeadNotFound}
	router := NewRouter(Config{
		Beads: beads,
		Now:   func() time.Time { return time.Unix(10, 0).UTC() },
	})

	req := httptest.NewRequest(http.MethodGet, "/v1/projects/proj_01/beads/hp-missing", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d; body=%s", rec.Code, http.StatusNotFound, rec.Body.String())
	}
	var problem schemas.Problem
	if err := json.Unmarshal(rec.Body.Bytes(), &problem); err != nil {
		t.Fatalf("decode problem: %v", err)
	}
	if problem.Code != "bead.not_found" {
		t.Fatalf("problem.Code = %q, want bead.not_found", problem.Code)
	}
}

type fakePlansReader struct {
	response      schemas.PlanListResponse
	err           error
	lastProjectID string
}

func (f *fakePlansReader) ListPlans(_ context.Context, projectID string) (schemas.PlanListResponse, error) {
	f.lastProjectID = projectID
	return f.response, f.err
}

type fakeBeadsReader struct {
	list          schemas.BeadListResponse
	listErr       error
	ready         schemas.BeadListResponse
	readyErr      error
	single        schemas.Bead
	singleErr     error
	lastProjectID string
	lastBeadID    string
}

func (f *fakeBeadsReader) ListBeads(_ context.Context, projectID string) (schemas.BeadListResponse, error) {
	f.lastProjectID = projectID
	return f.list, f.listErr
}

func (f *fakeBeadsReader) ReadyBeads(_ context.Context, projectID string) (schemas.BeadListResponse, error) {
	f.lastProjectID = projectID
	return f.ready, f.readyErr
}

func (f *fakeBeadsReader) Bead(_ context.Context, projectID, beadID string) (schemas.Bead, error) {
	f.lastProjectID = projectID
	f.lastBeadID = beadID
	return f.single, f.singleErr
}

func TestApprovalRoutesMutateQueueUsedByAuthLookup(t *testing.T) {
	now := time.Date(2026, 5, 4, 12, 0, 0, 0, time.UTC)
	queue := approvals.NewQueue(approvals.Config{
		Now: func() time.Time { return now },
		NewID: func(approvals.Request) (string, error) {
			return "appr_rotate", nil
		},
	})
	approval, created, err := queue.Request(context.Background(), approvals.Request{
		Source:          schemas.ApprovalSourceHoopoePolicy,
		PolicyRule:      "hoopoe-policy:auth.rotate_secret",
		RequestedAction: schemas.CommandSpec{Kind: "auth.rotate_secret", Target: map[string]interface{}{"scope": "daemon"}},
		RequestActor:    schemas.Actor{Kind: schemas.ActorKindSystem},
		ProjectID:       "proj_01",
		Scope:           schemas.Once,
		RiskClass:       schemas.Critical,
	})
	if err != nil || !created {
		t.Fatalf("Request approval = %v created=%v", err, created)
	}
	router := NewRouter(Config{
		Approvals: queue,
		Now:       func() time.Time { return now },
	})

	listReq := httptest.NewRequest(http.MethodGet, "/v1/projects/proj_01/approvals", nil)
	listRec := httptest.NewRecorder()
	router.ServeHTTP(listRec, listReq)
	if listRec.Code != http.StatusOK {
		t.Fatalf("list status = %d; body=%s", listRec.Code, listRec.Body.String())
	}
	var list schemas.ApprovalListResponse
	if err := json.Unmarshal(listRec.Body.Bytes(), &list); err != nil {
		t.Fatalf("decode list: %v", err)
	}
	if len(list.Items) != 1 || list.Items[0].Id != approval.Id {
		t.Fatalf("list = %+v, want queued approval", list)
	}

	approveReq := httptest.NewRequest(http.MethodPost, "/v1/projects/proj_01/approvals/appr_rotate/approve", strings.NewReader(`{"decisionActor":{"kind":"user","id":"owner"}}`))
	approveReq.Header.Set("Content-Type", "application/json")
	approveReq.Header.Set("Idempotency-Key", "01HXTESTAPPROVE")
	approveRec := httptest.NewRecorder()
	router.ServeHTTP(approveRec, approveReq)
	if approveRec.Code != http.StatusOK {
		t.Fatalf("approve status = %d; body=%s", approveRec.Code, approveRec.Body.String())
	}
	var approved schemas.Approval
	if err := json.Unmarshal(approveRec.Body.Bytes(), &approved); err != nil {
		t.Fatalf("decode approve: %v", err)
	}
	if approved.State != schemas.Approved {
		t.Fatalf("approved state = %s, want approved", approved.State)
	}
	lookup := ApprovalQueueLookup{Queue: queue}
	fromAuthLookup, ok, err := lookup.LookupApproval(context.Background(), "appr_rotate")
	if err != nil || !ok {
		t.Fatalf("auth lookup ok=%v err=%v", ok, err)
	}
	if fromAuthLookup.State != schemas.Approved {
		t.Fatalf("auth lookup state = %s, want approved", fromAuthLookup.State)
	}
}

func TestApprovalDecisionRoutesRequireIdempotencyKey(t *testing.T) {
	// hp-0uh: approval approve/deny are retryable mutating routes; per the
	// OpenAPI Idempotency-Key contract they must reject requests that omit
	// the header. (handleProjectCreate has been requiring the header
	// since 2026-05-04; this slice extends enforcement to the unified
	// approvals queue. Audit-export, action-plan, provider create/destroy,
	// job-cancel are deferred — those need OpenAPI alignment first.)
	now := time.Date(2026, 5, 4, 12, 0, 0, 0, time.UTC)
	queue := approvals.NewQueue(approvals.Config{
		Now:   func() time.Time { return now },
		NewID: func(approvals.Request) (string, error) { return "appr_idem", nil },
	})
	if _, _, err := queue.Request(context.Background(), approvals.Request{
		Source:          schemas.ApprovalSourceHoopoePolicy,
		PolicyRule:      "hoopoe-policy:auth.rotate_secret",
		RequestedAction: schemas.CommandSpec{Kind: "auth.rotate_secret"},
		RequestActor:    schemas.Actor{Kind: schemas.ActorKindSystem},
		ProjectID:       "proj_01",
		Scope:           schemas.Once,
		RiskClass:       schemas.Critical,
	}); err != nil {
		t.Fatalf("queue.Request: %v", err)
	}
	router := NewRouter(Config{
		Approvals: queue,
		Now:       func() time.Time { return now },
	})

	for _, verb := range []string{"approve", "deny"} {
		verb := verb
		t.Run(verb+"-rejects-missing-idempotency-key", func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, "/v1/projects/proj_01/approvals/appr_idem/"+verb, strings.NewReader(`{"decisionActor":{"kind":"user","id":"owner"}}`))
			req.Header.Set("Content-Type", "application/json")
			rec := httptest.NewRecorder()
			router.ServeHTTP(rec, req)
			if rec.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want %d; body=%s", rec.Code, http.StatusBadRequest, rec.Body.String())
			}
			if got := rec.Header().Get("Content-Type"); !strings.HasPrefix(got, "application/problem+json") {
				t.Fatalf("content-type = %q, want problem+json", got)
			}
			var problem schemas.Problem
			if err := json.Unmarshal(rec.Body.Bytes(), &problem); err != nil {
				t.Fatalf("decode problem: %v", err)
			}
			if problem.Code != "idempotency.required" {
				t.Fatalf("problem.Code = %q, want idempotency.required", problem.Code)
			}
			if problem.Detail == nil || !strings.Contains(*problem.Detail, "/"+verb) {
				t.Fatalf("problem.Detail = %v, want a hint at the verb", problem.Detail)
			}
		})

		t.Run(verb+"-rejects-empty-idempotency-key", func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, "/v1/projects/proj_01/approvals/appr_idem/"+verb, strings.NewReader(`{"decisionActor":{"kind":"user","id":"owner"}}`))
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("Idempotency-Key", "   ")
			rec := httptest.NewRecorder()
			router.ServeHTTP(rec, req)
			if rec.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want %d; body=%s", rec.Code, http.StatusBadRequest, rec.Body.String())
			}
			var problem schemas.Problem
			if err := json.Unmarshal(rec.Body.Bytes(), &problem); err != nil {
				t.Fatalf("decode problem: %v", err)
			}
			if problem.Code != "idempotency.required" {
				t.Fatalf("problem.Code = %q, want idempotency.required (whitespace-only must be rejected)", problem.Code)
			}
		})
	}
}

func TestSeedWriteEndpointAcceptsIdempotencyKeyAndReturnsGeneratedProblem(t *testing.T) {
	router := NewRouter(Config{})
	req := httptest.NewRequest(http.MethodPost, "/v1/bootstrap/acfs/start", nil)
	req.Header.Set("Idempotency-Key", "01HXTESTIDEMPOTENCY")
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotImplemented {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusNotImplemented)
	}
	if got := rec.Header().Get("Content-Type"); !strings.HasPrefix(got, "application/problem+json") {
		t.Fatalf("content-type = %q, want problem+json", got)
	}
	var body schemas.Problem
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode generated problem: %v", err)
	}
	if body.Code != "bootstrap.acfs.start.unavailable" || body.Status != http.StatusNotImplemented {
		t.Fatalf("unexpected problem: %+v", body)
	}
}

func TestProjectRoutesUseRegistry(t *testing.T) {
	root := t.TempDir()
	writeRouterTestFile(t, filepath.Join(root, "AGENTS.md"), "agent instructions\n")
	writeRouterTestFile(t, filepath.Join(root, "README.md"), "readme\n")
	writeRouterTestFile(t, filepath.Join(root, "go.mod"), "module example.invalid/api\n\ngo 1.26\n")
	service := newRouterProjectService(t, root)
	router := NewRouter(Config{
		Projects: service,
		Now: func() time.Time {
			return time.Date(2026, 5, 4, 0, 0, 0, 0, time.UTC)
		},
	})
	body, err := json.Marshal(map[string]string{
		"id":       "proj_api",
		"name":     "API Project",
		"rootPath": root,
	})
	if err != nil {
		t.Fatalf("marshal project request: %v", err)
	}
	req := httptest.NewRequest(http.MethodPost, "/v1/projects", strings.NewReader(string(body)))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Idempotency-Key", "idem-api")
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d; body=%s", rec.Code, http.StatusCreated, rec.Body.String())
	}
	var created schemas.Project
	if err := json.Unmarshal(rec.Body.Bytes(), &created); err != nil {
		t.Fatalf("decode created project: %v", err)
	}
	if created.Id != "proj_api" || created.Repo.Origin != "https://example.invalid/api.git" {
		t.Fatalf("created project = %+v", created)
	}

	for _, tc := range []struct {
		name   string
		path   string
		target any
	}{
		{
			name:   "list",
			path:   "/v1/projects",
			target: &schemas.ProjectListResponse{},
		},
		{
			name:   "get",
			path:   "/v1/projects/proj_api",
			target: &schemas.Project{},
		},
		{
			name:   "readiness",
			path:   "/v1/projects/proj_api/readiness",
			target: &schemas.ProjectReadiness{},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, tc.path, nil)
			rec := httptest.NewRecorder()

			router.ServeHTTP(rec, req)

			if rec.Code != http.StatusOK {
				t.Fatalf("status = %d, want %d; body=%s", rec.Code, http.StatusOK, rec.Body.String())
			}
			if err := json.Unmarshal(rec.Body.Bytes(), tc.target); err != nil {
				t.Fatalf("decode response: %v", err)
			}
		})
	}

	readinessReq := httptest.NewRequest(http.MethodGet, "/v1/projects/proj_api/readiness", nil)
	readinessRec := httptest.NewRecorder()
	router.ServeHTTP(readinessRec, readinessReq)
	var readiness schemas.ProjectReadiness
	if err := json.Unmarshal(readinessRec.Body.Bytes(), &readiness); err != nil {
		t.Fatalf("decode readiness: %v", err)
	}
	if len(readiness.Gates) != 1 || !readiness.Gates[0].Satisfied {
		t.Fatalf("readiness = %+v, want satisfied", readiness)
	}

	missingReq := httptest.NewRequest(http.MethodGet, "/v1/projects/proj_missing", nil)
	missingRec := httptest.NewRecorder()
	router.ServeHTTP(missingRec, missingReq)
	if missingRec.Code != http.StatusNotFound {
		t.Fatalf("missing status = %d, want %d; body=%s", missingRec.Code, http.StatusNotFound, missingRec.Body.String())
	}
}

// projectCapabilityRegistry seeds a capabilities.Registry whose git/br
// reports default to OK. Per-test overrides flip individual capRefs to
// missing/blocked-by-policy/degraded so the gate paths in handleProjectCreate
// + handleProjectReadiness can be exercised without a live VPS.
func projectCapabilityRegistry(t *testing.T, overrides map[string]capabilities.CapabilityStatus) *capabilities.Registry {
	t.Helper()
	stamp := "2026-05-04T00:00:00Z"
	r := capabilities.New("0.1.0")
	stampParsed, err := time.Parse(time.RFC3339, stamp)
	if err != nil {
		t.Fatalf("parse stamp: %v", err)
	}
	r.SetClock(func() time.Time { return stampParsed })

	gitCaps := map[string]capabilities.Capability{
		"git.status.read": {Status: capabilities.StatusOK},
		"git.remote.read": {Status: capabilities.StatusOK},
		"git.branch.read": {Status: capabilities.StatusOK},
	}
	brCaps := map[string]capabilities.Capability{
		"br.create": {Status: capabilities.StatusOK},
	}
	for ref, status := range overrides {
		switch {
		case strings.HasPrefix(ref, "git."):
			gitCaps[ref] = capabilities.Capability{Status: status}
		case strings.HasPrefix(ref, "br."):
			brCaps[ref] = capabilities.Capability{Status: status}
		}
	}
	if err := r.SetReport(&capabilities.ToolReport{
		Tool: capabilities.ToolGit, Version: "2.40.0", Source: "test",
		LastCheckedAt: stamp, Capabilities: gitCaps,
	}); err != nil {
		t.Fatalf("seed git report: %v", err)
	}
	if err := r.SetReport(&capabilities.ToolReport{
		Tool: capabilities.ToolBR, Version: "0.5.0", Source: "test",
		LastCheckedAt: stamp, Capabilities: brCaps,
	}); err != nil {
		t.Fatalf("seed br report: %v", err)
	}
	return r
}

func TestProjectCreateBlockedWhenRequiredCapabilityMissing(t *testing.T) {
	root := t.TempDir()
	writeRouterTestFile(t, filepath.Join(root, "AGENTS.md"), "agent\n")
	writeRouterTestFile(t, filepath.Join(root, "go.mod"), "module example.invalid/api\n\ngo 1.26\n")
	service := newRouterProjectService(t, root)
	registry := projectCapabilityRegistry(t, map[string]capabilities.CapabilityStatus{
		"git.remote.read": capabilities.StatusMissing,
	})
	router := NewRouter(Config{Projects: service, Capabilities: registry})

	body, err := json.Marshal(map[string]string{"id": "proj_cap", "name": "P", "rootPath": root})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	req := httptest.NewRequest(http.MethodPost, "/v1/projects", strings.NewReader(string(body)))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Idempotency-Key", "idem-cap")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503; body=%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "projects.create.capabilities_unavailable") {
		t.Errorf("expected capability problem code in body, got %s", rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "git.remote.read") {
		t.Errorf("expected git.remote.read named in problem detail, got %s", rec.Body.String())
	}
}

func TestProjectCreateBlockedWhenRequiredCapabilityBlockedByPolicy(t *testing.T) {
	root := t.TempDir()
	writeRouterTestFile(t, filepath.Join(root, "go.mod"), "module example.invalid/api\n\ngo 1.26\n")
	service := newRouterProjectService(t, root)
	registry := projectCapabilityRegistry(t, map[string]capabilities.CapabilityStatus{
		"br.create": capabilities.StatusBlockedByPolicy,
	})
	router := NewRouter(Config{Projects: service, Capabilities: registry})

	body, err := json.Marshal(map[string]string{"id": "proj_blk", "name": "P", "rootPath": root})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	req := httptest.NewRequest(http.MethodPost, "/v1/projects", strings.NewReader(string(body)))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Idempotency-Key", "idem-blk")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503; body=%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "blocked-by-policy") {
		t.Errorf("expected blocked-by-policy in body, got %s", rec.Body.String())
	}
}

func TestProjectReadinessBlockedWhenGitCapabilityMissing(t *testing.T) {
	registry := projectCapabilityRegistry(t, map[string]capabilities.CapabilityStatus{
		"git.status.read": capabilities.StatusMissing,
	})
	router := NewRouter(Config{Capabilities: registry})

	req := httptest.NewRequest(http.MethodGet, "/v1/projects/proj_x/readiness", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503; body=%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "projects.readiness.capabilities_unavailable") {
		t.Errorf("expected readiness capability problem code, got %s", rec.Body.String())
	}
}

func TestProjectCreateAllowsWhenCapabilitiesOK(t *testing.T) {
	root := t.TempDir()
	writeRouterTestFile(t, filepath.Join(root, "go.mod"), "module example.invalid/api\n\ngo 1.26\n")
	service := newRouterProjectService(t, root)
	registry := projectCapabilityRegistry(t, nil)
	router := NewRouter(Config{Projects: service, Capabilities: registry})

	body, err := json.Marshal(map[string]string{"id": "proj_ok", "name": "P", "rootPath": root})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	req := httptest.NewRequest(http.MethodPost, "/v1/projects", strings.NewReader(string(body)))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Idempotency-Key", "idem-ok")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201; body=%s", rec.Code, rec.Body.String())
	}
}

func TestProjectCreateAllowsWhenCapabilityRegistryMissing(t *testing.T) {
	// Backwards-compat: a router built without Config.Capabilities (e.g.,
	// minimal smoke harness) must not gate project routes — the existing
	// TestProjectRoutesUseRegistry exercises this shape.
	root := t.TempDir()
	writeRouterTestFile(t, filepath.Join(root, "go.mod"), "module example.invalid/api\n\ngo 1.26\n")
	service := newRouterProjectService(t, root)
	router := NewRouter(Config{Projects: service})
	body, err := json.Marshal(map[string]string{"id": "proj_legacy", "name": "P", "rootPath": root})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	req := httptest.NewRequest(http.MethodPost, "/v1/projects", strings.NewReader(string(body)))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Idempotency-Key", "idem-legacy")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201; body=%s", rec.Code, rec.Body.String())
	}
}

func TestSeedContractCoversPlanSectionRoutes(t *testing.T) {
	router := NewRouter(Config{})
	approvalDecision := `{"decisionActor":{"kind":"user"}}`
	actionPlan := `{"schemaVersion":1,"jobId":"job_seed","runId":"run_seed","summary":"seed","riskClass":"low","actions":[]}`
	for _, tc := range []struct {
		method string
		path   string
		body   string
	}{
		{http.MethodGet, "/v1/health", ""},
		{http.MethodGet, "/v1/version", ""},
		{http.MethodGet, "/v1/system/specs", ""},
		{http.MethodGet, "/v1/system/processes", ""},
		{http.MethodPost, "/v1/auth/bootstrap/bearer", ""},
		{http.MethodPost, "/v1/auth/ws-token", ""},
		{http.MethodPost, "/v1/auth/session/revoke", ""},
		{http.MethodPost, "/v1/auth/rotate-secret", ""},
		{http.MethodGet, "/v1/events/replay?channel=_system&sinceSequence=0", ""},
		{http.MethodGet, "/v1/events/sse?channels=_system", ""},
		{http.MethodGet, "/v1/events/ws-token", ""},
		{http.MethodGet, "/v1/events/ws", ""},
		{http.MethodGet, "/v1/jobs", ""},
		{http.MethodPost, "/v1/jobs/job_seed/cancel", ""},
		{http.MethodGet, "/v1/jobs/job_seed/log?offset=0", ""},
		{http.MethodGet, "/v1/jobs/job_seed/artifacts", ""},
		{http.MethodPost, "/v1/bootstrap/preflight", ""},
		{http.MethodPost, "/v1/bootstrap/acfs/start", ""},
		{http.MethodPost, "/v1/bootstrap/acfs/resume", ""},
		{http.MethodPost, "/v1/bootstrap/daemon/upgrade", ""},
		{http.MethodGet, "/v1/projects", ""},
		{http.MethodPost, "/v1/projects", ""},
		{http.MethodGet, "/v1/projects/proj_01", ""},
		{http.MethodPost, "/v1/projects/proj_01/activate", ""},
		{http.MethodGet, "/v1/projects/proj_01/readiness", ""},
		{http.MethodGet, "/v1/projects/proj_01/git/status", ""},
		{http.MethodGet, "/v1/projects/proj_01/git/staged-diff", ""},
		{http.MethodGet, "/v1/projects/proj_01/git/unstaged-diff", ""},
		{http.MethodGet, "/v1/projects/proj_01/git/unpushed-commits", ""},
		{http.MethodPost, "/v1/projects/proj_01/git/push", ""},
		{http.MethodGet, "/v1/projects/proj_01/plans", ""},
		{http.MethodPost, "/v1/projects/proj_01/plans", ""},
		{http.MethodPost, "/v1/projects/proj_01/plans/plan_01/rounds", ""},
		{http.MethodPost, "/v1/projects/proj_01/plans/plan_01/lock", ""},
		{http.MethodGet, "/v1/projects/proj_01/plans/plan_01/artifacts", ""},
		{http.MethodGet, "/v1/projects/proj_01/beads", ""},
		{http.MethodGet, "/v1/projects/proj_01/beads/graph", ""},
		{http.MethodGet, "/v1/projects/proj_01/beads/ready", ""},
		{http.MethodGet, "/v1/projects/proj_01/beads/hp-1ry", ""},
		{http.MethodPatch, "/v1/projects/proj_01/beads/hp-1ry", ""},
		{http.MethodPost, "/v1/projects/proj_01/beads/conversion-runs", ""},
		{http.MethodPost, "/v1/projects/proj_01/beads/polish-runs", ""},
		{http.MethodPost, "/v1/projects/proj_01/swarms", ""},
		{http.MethodGet, "/v1/projects/proj_01/swarms/sw_01", ""},
		{http.MethodPost, "/v1/projects/proj_01/swarms/sw_01/broadcast", ""},
		{http.MethodPost, "/v1/projects/proj_01/agents/ag_01/send", ""},
		{http.MethodPost, "/v1/projects/proj_01/agents/ag_01/interrupt", ""},
		{http.MethodPost, "/v1/projects/proj_01/agents/ag_01/stop", ""},
		{http.MethodGet, "/v1/projects/proj_01/mail/messages", ""},
		{http.MethodPost, "/v1/projects/proj_01/mail/messages", ""},
		{http.MethodGet, "/v1/projects/proj_01/mail/threads/thread_01", ""},
		{http.MethodGet, "/v1/projects/proj_01/reservations", ""},
		{http.MethodPost, "/v1/projects/proj_01/reservations/force-release", ""},
		{http.MethodGet, "/v1/projects/proj_01/health/summary", ""},
		{http.MethodGet, "/v1/projects/proj_01/health/files", ""},
		{http.MethodPost, "/v1/projects/proj_01/health/snapshots", ""},
		{http.MethodPost, "/v1/projects/proj_01/reviews", ""},
		{http.MethodGet, "/v1/projects/proj_01/reviews/rev_01", ""},
		{http.MethodGet, "/v1/projects/proj_01/findings", ""},
		{http.MethodPatch, "/v1/projects/proj_01/findings/finding_01", ""},
		{http.MethodGet, "/v1/projects/proj_01/tending/jobs", ""},
		{http.MethodPost, "/v1/projects/proj_01/tending/jobs/job_01/run", ""},
		{http.MethodPatch, "/v1/projects/proj_01/tending/jobs/job_01", ""},
		{http.MethodPost, "/v1/projects/proj_01/tending/actionplans", actionPlan},
		{http.MethodGet, "/v1/projects/proj_01/approvals", ""},
		{http.MethodGet, "/v1/projects/proj_01/approvals/ap_01", ""},
		{http.MethodPost, "/v1/projects/proj_01/approvals/ap_01/approve", approvalDecision},
		{http.MethodPost, "/v1/projects/proj_01/approvals/ap_01/deny", approvalDecision},
		{http.MethodPost, "/v1/projects/proj_01/approvals/ap_01/extend", ""},
	} {
		t.Run(tc.method+" "+tc.path, func(t *testing.T) {
			ctx := context.Background()
			if strings.Contains(tc.path, "/v1/events/sse") {
				ctx = canceledContext()
			}
			var body *strings.Reader
			if tc.body == "" {
				body = strings.NewReader("")
			} else {
				body = strings.NewReader(tc.body)
			}
			req := httptest.NewRequest(tc.method, tc.path, body).WithContext(ctx)
			req.Header.Set("Idempotency-Key", "01HXTESTIDEMPOTENCY")
			if tc.body != "" {
				req.Header.Set("Content-Type", "application/json")
			}
			rec := httptest.NewRecorder()

			router.ServeHTTP(rec, req)

			contentType := rec.Header().Get("Content-Type")
			if rec.Code == http.StatusMethodNotAllowed || (rec.Code == http.StatusNotFound && !strings.HasPrefix(contentType, "application/problem+json")) {
				t.Fatalf("route was not handled: status=%d body=%s", rec.Code, rec.Body.String())
			}
			if rec.Code >= 400 && !strings.HasPrefix(contentType, "application/problem+json") && !strings.Contains(tc.path, "/v1/events/ws") {
				t.Fatalf("error content-type = %q, want problem+json; status=%d body=%s", contentType, rec.Code, rec.Body.String())
			}
		})
	}
}

func canceledContext() context.Context {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	return ctx
}

func TestJobCancelRoundTripUsesGeneratedRequestAndResponse(t *testing.T) {
	reader := &staticJobController{
		staticJobReader: staticJobReader{
			jobs: []jobstore.Job{{
				ID:            "job_test",
				Kind:          "daemon.smoke",
				SchemaVersion: jobstore.SchemaVersion,
				Status:        jobstore.StatusRunning,
				CreatedAt:     time.Unix(20, 0).UTC(),
				UpdatedAt:     time.Unix(20, 0).UTC(),
			}},
		},
	}
	router := NewRouter(Config{Jobs: reader})
	req := httptest.NewRequest(http.MethodPost, "/v1/jobs/job_test/cancel", strings.NewReader(`{"reason":"test cancel","graceSeconds":1}`))
	req.Header.Set("Idempotency-Key", "01HXTESTIDEMPOTENCY")
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want %d; body=%s", rec.Code, http.StatusAccepted, rec.Body.String())
	}
	var body schemas.Job
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode generated job: %v", err)
	}
	if body.Id != "job_test" || body.Status != schemas.JobStatusCanceling {
		t.Fatalf("unexpected cancel response: %+v", body)
	}
	if len(reader.cancelRequests) != 1 || reader.cancelRequests[0].Reason != "test cancel" {
		t.Fatalf("cancel requests = %+v", reader.cancelRequests)
	}
}

func TestJobLogAndArtifactsRoutesUseRegistryReader(t *testing.T) {
	stamp := time.Unix(20, 0).UTC()
	router := NewRouter(Config{
		Jobs: staticJobReader{
			logs: map[string][]byte{
				"job_test": []byte("hello log"),
			},
			artifacts: map[string][]jobstore.Artifact{
				"job_test": {{
					ID:        "artifact_1",
					Kind:      "log",
					URI:       "fixture://job/log",
					CreatedAt: stamp,
				}},
			},
		},
	})

	for _, path := range []string{"/v1/jobs/job_test/log?offset=0", "/v1/jobs/job_test/artifacts"} {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		rec := httptest.NewRecorder()

		router.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("%s status = %d, want %d; body=%s", path, rec.Code, http.StatusOK, rec.Body.String())
		}
	}
}

func TestWebSocketAcceptsValidToken(t *testing.T) {
	router := NewRouter(Config{
		WSValidator: StaticWebSocketTokenValidator{Token: "secret"},
	})
	server := httptest.NewServer(router)
	defer server.Close()

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http") + "/v1/events/ws?wsToken=secret"
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	conn, _, err := websocket.Dial(ctx, wsURL, nil)
	if err != nil {
		t.Fatalf("websocket dial with valid token: %v", err)
	}
	defer conn.Close(websocket.StatusNormalClosure, "test complete")
}

func TestSlowConsumerReceivesLagEvent(t *testing.T) {
	hub := NewEventHub(EventHubConfig{SubscriberCapacity: 1})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	sub := hub.Subscribe(ctx, []string{"project:test"})
	defer sub.Close()

	hub.Publish(PublishInput{Channel: "project:test", Type: "first", Data: map[string]any{"n": 1}})
	hub.Publish(PublishInput{Channel: "project:test", Type: "second", Data: map[string]any{"n": 2}})

	select {
	case ev := <-sub.Events():
		if ev.Type != "_lag" {
			t.Fatalf("event type = %q, want _lag", ev.Type)
		}
		if ev.Channel != "project:test" {
			t.Fatalf("event channel = %q, want project:test", ev.Channel)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for lag event")
	}
}

// TestHeartbeatDoesNotConsumeChannelSequence guards hp-2wrg: the
// per-connection heartbeat timer used to call nextSequenceLocked,
// burning _system sequence numbers and creating phantom gaps for
// Subscribe('_system') consumers (next real _system event arrived at
// seq=N+1 instead of seq=1; cursor=0 subscribers saw last>since with
// an empty replay buffer because heartbeats aren't appended).
//
// transientEvent must read h.sequences without mutating it, so the
// next real Publish on the channel still gets the next monotonic
// sequence.
func TestHeartbeatDoesNotConsumeChannelSequence(t *testing.T) {
	hub := NewEventHub(EventHubConfig{})

	// Publish one real _system event so the channel has a real seq=1.
	first := hub.Publish(PublishInput{Channel: "_system", Type: "boot", Data: map[string]any{}})
	if first.Sequence != 1 {
		t.Fatalf("first real _system event seq = %d, want 1", first.Sequence)
	}

	// Fire 100 heartbeats — none should burn a sequence number.
	for i := 0; i < 100; i++ {
		hb := hub.Heartbeat()
		if hb.Sequence != 1 {
			t.Fatalf("heartbeat %d sequence = %d, want 1 (channel last seq, not consumed)", i, hb.Sequence)
		}
	}
	// CompatibilityWarning must also not consume.
	for i := 0; i < 50; i++ {
		cw := hub.CompatibilityWarning(0)
		if cw.Sequence != 1 {
			t.Fatalf("compatibility warning %d sequence = %d, want 1", i, cw.Sequence)
		}
	}

	// The next real _system event must arrive at seq=2, NOT seq=152.
	second := hub.Publish(PublishInput{Channel: "_system", Type: "auth.rotated", Data: map[string]any{}})
	if second.Sequence != 2 {
		t.Fatalf("next real _system event seq = %d, want 2 (heartbeats inflated cursor)", second.Sequence)
	}

	// And LastSequence reports the real cursor, not heartbeat-inflated.
	if last := hub.LastSequence("_system"); last != 2 {
		t.Fatalf("LastSequence(_system) = %d, want 2", last)
	}
}

// TestEventHubSubscribeAcceptsNilCtxWithoutPanic guards hp-uvjg: the
// Subscribe watcher goroutine used to call ctx.Done() on whatever ctx
// the caller passed. A nil ctx panicked the goroutine ("nil pointer
// dereference"), and there was no recover guard — the panic crashed
// the daemon. Subscribe now substitutes context.Background for a nil
// ctx and the goroutine has a defer/recover mirror of
// scheduler.recoverDispatch.
func TestEventHubSubscribeAcceptsNilCtxWithoutPanic(t *testing.T) {
	hub := NewEventHub(EventHubConfig{})
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("Subscribe(nil, ...) panicked: %v", r)
		}
	}()
	//nolint:staticcheck // SA1012: passing nil context is the regression we're guarding
	sub := hub.Subscribe(nil, []string{"project:test"})
	sub.Close()
	select {
	case <-sub.watcherDone:
	case <-time.After(time.Second):
		t.Fatal("subscriber watcher goroutine did not exit after Close (nil-ctx path leaked it)")
	}
}

func TestEventHubSubscriberCloseStopsContextWatcher(t *testing.T) {
	hub := NewEventHub(EventHubConfig{})
	sub := hub.Subscribe(context.Background(), []string{"project:test"})

	sub.Close()
	select {
	case <-sub.watcherDone:
	case <-time.After(time.Second):
		t.Fatal("subscriber context watcher did not exit after manual Close")
	}
	sub.Close()

	if stillRegistered := func() bool {
		hub.mu.RLock()
		defer hub.mu.RUnlock()
		_, ok := hub.subscribers[sub.sub.id]
		return ok
	}(); stillRegistered {
		t.Fatal("subscriber still registered after Close")
	}
}

func TestEventHubRedactsPublishDataBeforeDeliveryAndReplay(t *testing.T) {
	// hp-aek: EventHub.Publish must scrub secret-shaped data before it
	// reaches WS/SSE subscribers or sits in the replay buffer. Mirrors
	// audit.Writer.redactEntry. Without the redactor, a commit message
	// containing `sk-...` or an agent-mail body referencing an API key
	// reaches subscribers raw.
	redactor := redaction.NewDefault()
	hub := NewEventHub(EventHubConfig{Redactor: redactor})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	sub := hub.Subscribe(ctx, []string{"project:test"})
	defer sub.Close()

	const secret = "sk-abcdef0123456789ABCDEF0123456789"
	hub.Publish(PublishInput{
		Channel: "project:test",
		Type:    "git.commit",
		Data: map[string]any{
			"message":  "leak " + secret + " in body",
			"metadata": map[string]any{"key": secret},
			"list":     []any{"safe", secret, "Bearer abcdefghijklmnopqrstuvwxyz012345"},
		},
	})

	select {
	case ev := <-sub.Events():
		body, err := json.Marshal(ev.Data)
		if err != nil {
			t.Fatalf("marshal delivered event: %v", err)
		}
		rendered := string(body)
		if strings.Contains(rendered, "sk-abcdef") {
			t.Fatalf("subscriber received raw secret in delivered Data: %s", rendered)
		}
		if strings.Contains(rendered, "abcdefghijklmnopqrstuvwxyz012345") {
			t.Fatalf("subscriber received raw bearer token in delivered Data: %s", rendered)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for delivered event")
	}

	// Replay buffer must also be redacted — the same surface stores it.
	replayed, _ := hub.Replay("project:test", 0)
	if len(replayed) == 0 {
		t.Fatal("replay buffer empty")
	}
	body, err := json.Marshal(replayed[0].Data)
	if err != nil {
		t.Fatalf("marshal replay event: %v", err)
	}
	if strings.Contains(string(body), "sk-abcdef") {
		t.Fatalf("replay buffer holds raw secret: %s", body)
	}
}

func TestEventHubNilRedactorAutoDefaultsAndStillRedacts(t *testing.T) {
	// hp-cy4: NewEventHub is safe-by-default. A nil EventHubConfig.Redactor
	// no longer means raw delivery — the constructor auto-creates a default
	// redaction.Redactor so production wiring that forgets to pass one (and
	// every test that omits one) still scrubs secrets. The opt-out is
	// NewEventHubWithoutRedactor for load/chaos fixtures.
	hub := NewEventHub(EventHubConfig{})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	sub := hub.Subscribe(ctx, []string{"project:default"})
	defer sub.Close()

	hub.Publish(PublishInput{Channel: "project:default", Type: "raw", Data: map[string]any{"message": "literal-secret-shaped sk-abcdef0123456789ABCDEF0123456789"}})
	select {
	case ev := <-sub.Events():
		body, _ := json.Marshal(ev.Data)
		if strings.Contains(string(body), "sk-abcdef0123456789") {
			t.Fatalf("auto-default redactor failed to scrub secret: %s", body)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for event")
	}
}

func TestEventHubWithoutRedactorEscapeHatchDeliversVerbatim(t *testing.T) {
	// Load/chaos fixtures assert raw delivery semantics — keep the explicit
	// opt-out path covered so future refactors do not silently re-redact.
	hub := NewEventHubWithoutRedactor(EventHubConfig{})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	sub := hub.Subscribe(ctx, []string{"project:nofilter"})
	defer sub.Close()

	hub.Publish(PublishInput{Channel: "project:nofilter", Type: "raw", Data: map[string]any{"message": "literal-secret-shaped sk-abcdef0123456789ABCDEF0123456789"}})
	select {
	case ev := <-sub.Events():
		body, _ := json.Marshal(ev.Data)
		if !strings.Contains(string(body), "sk-abcdef0123456789") {
			t.Fatalf("escape hatch unexpectedly redacted Data: %s", body)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for event")
	}
}

type secretCarryingPayload struct {
	ProjectID string                `json:"projectId"`
	Message   string                `json:"message"`
	Nested    *secretCarryingNested `json:"nested,omitempty"`
	List      []string              `json:"list,omitempty"`
	Extra     map[string]any        `json:"extra,omitempty"`
}

type secretCarryingNested struct {
	Subject string `json:"subject"`
}

func TestEventHubRedactsTypedStructPublishDataAndPreservesType(t *testing.T) {
	// hp-aek.1 / hp-aek.2: typed-struct producers (git watcher's
	// CommitCreatedPayload, activity ingestor's ActivityData) publish Data as a
	// concrete Go struct. The redactor walker only handles strings/maps/slices
	// natively, so before this fix a secret-shaped commit message or mail
	// subject inside a struct field reached subscribers raw. After redaction,
	// the original Go type must be preserved so existing callers and tests
	// still see the typed Data shape.
	const secret = "sk-abcdef0123456789ABCDEF0123456789"
	hub := NewEventHub(EventHubConfig{Redactor: redaction.NewDefault()})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	sub := hub.Subscribe(ctx, []string{"project:typed"})
	defer sub.Close()

	payload := secretCarryingPayload{
		ProjectID: "proj_01",
		Message:   "leak " + secret + " in commit",
		Nested:    &secretCarryingNested{Subject: "Bearer abcdefghijklmnopqrstuvwxyz012345 leaked"},
		List:      []string{"safe", secret},
		Extra:     map[string]any{"token": secret},
	}
	hub.Publish(PublishInput{Channel: "project:typed", Type: "project.event", Data: payload})

	select {
	case ev := <-sub.Events():
		typed, ok := ev.Data.(secretCarryingPayload)
		if !ok {
			t.Fatalf("typed Data shape lost after redaction: %T", ev.Data)
		}
		body, _ := json.Marshal(typed)
		if strings.Contains(string(body), "sk-abcdef0123456789") {
			t.Fatalf("subscriber received raw secret in struct field: %s", body)
		}
		if strings.Contains(string(body), "abcdefghijklmnopqrstuvwxyz012345") {
			t.Fatalf("subscriber received raw bearer token in nested struct field: %s", body)
		}
		if typed.ProjectID != "proj_01" {
			t.Fatalf("non-secret field mutated: projectId = %q", typed.ProjectID)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for typed-struct event")
	}

	replayed, _ := hub.Replay("project:typed", 0)
	if len(replayed) == 0 {
		t.Fatal("replay buffer empty after typed-struct publish")
	}
	body, err := json.Marshal(replayed[0].Data)
	if err != nil {
		t.Fatalf("marshal replay event: %v", err)
	}
	if strings.Contains(string(body), "sk-abcdef0123456789") {
		t.Fatalf("replay buffer holds raw secret in struct field: %s", body)
	}
}

func TestNormalizeConfigFallbackEventHubGetsRedactor(t *testing.T) {
	// hp-cy4: when api.Config.Events is nil (the production wiring path —
	// transport/server.go does not set it), normalizeConfig must construct
	// the EventHub with a Redactor. Asserts the fallback wiring is safe.
	srv := normalizeConfig(Config{})
	if srv.events == nil {
		t.Fatal("fallback EventHub not constructed")
	}
	if srv.events.redactor == nil {
		t.Fatal("fallback EventHub has nil redactor — production secrets would leak")
	}
	hub := srv.events
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	sub := hub.Subscribe(ctx, []string{"project:fallback"})
	defer sub.Close()
	hub.Publish(PublishInput{Channel: "project:fallback", Type: "test", Data: map[string]any{"k": "sk-abcdef0123456789ABCDEF0123456789"}})
	select {
	case ev := <-sub.Events():
		body, _ := json.Marshal(ev.Data)
		if strings.Contains(string(body), "sk-abcdef0123456789") {
			t.Fatalf("fallback EventHub leaked secret: %s", body)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for fallback event")
	}
}

func TestEventSequencesArePerChannel(t *testing.T) {
	hub := NewEventHub(EventHubConfig{})

	firstProject := hub.Publish(PublishInput{Channel: "project:test", Type: "project.changed", Data: map[string]any{"n": 1}})
	firstActivity := hub.Publish(PublishInput{Channel: "activity:test", Type: "activity.appended", Data: map[string]any{"n": 1}})
	secondProject := hub.Publish(PublishInput{Channel: "project:test", Type: "project.changed", Data: map[string]any{"n": 2}})

	if firstProject.Sequence != 1 {
		t.Fatalf("first project sequence = %d, want 1", firstProject.Sequence)
	}
	if firstActivity.Sequence != 1 {
		t.Fatalf("first activity sequence = %d, want 1", firstActivity.Sequence)
	}
	if secondProject.Sequence != 2 {
		t.Fatalf("second project sequence = %d, want 2", secondProject.Sequence)
	}
}

// TestReplayEndpointRejectsCursorAboveLastSequence guards hp-0vkn:
// the daemon's per-channel sequences are strictly monotonic via
// nextSequenceLocked, so a sinceSequence above the channel's last
// produced sequence is forward-time-travel — a stale fixture, a
// fabricated request, or a clock-skew bug. Without this guard, the
// replay endpoint silently returned an empty window and the client
// would believe its cursor was valid.
func TestReplayEndpointRejectsCursorAboveLastSequence(t *testing.T) {
	now := time.Unix(100, 0).UTC()
	hub := NewEventHub(EventHubConfig{
		ReplayCapacity: 16,
		Now:            func() time.Time { return now },
	})
	hub.Publish(PublishInput{Channel: "project:test", Type: "project.ready", Data: map[string]any{"n": 1}})
	hub.Publish(PublishInput{Channel: "project:test", Type: "bead.changed", Data: map[string]any{"n": 2}})

	router := NewRouter(Config{Events: hub})
	// Cursor=99 against a channel whose LastSequence is 2 must reject.
	req := httptest.NewRequest(http.MethodGet, "/v1/events/replay?channel=project:test&sinceSequence=99", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d (cursor exceeds LastSequence)", rec.Code, http.StatusBadRequest)
	}
	if !strings.Contains(rec.Body.String(), "non-monotonic") {
		t.Fatalf("response = %s, want 'non-monotonic' problem detail", rec.Body.String())
	}

	// Cursor=2 (== LastSequence) is valid — replay returns no events.
	req = httptest.NewRequest(http.MethodGet, "/v1/events/replay?channel=project:test&sinceSequence=2", nil)
	rec = httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("cursor==LastSequence status = %d, want 200", rec.Code)
	}
}

// TestSSEHandshakeRejectsCursorMapAboveLastSequence guards hp-0vkn
// for the cursors map path used by SSE/WS subscribers.
func TestSSEHandshakeRejectsCursorMapAboveLastSequence(t *testing.T) {
	now := time.Unix(100, 0).UTC()
	hub := NewEventHub(EventHubConfig{
		ReplayCapacity: 16,
		Now:            func() time.Time { return now },
	})
	hub.Publish(PublishInput{Channel: "project:test", Type: "project.ready", Data: map[string]any{"n": 1}})

	router := NewRouter(Config{Events: hub})
	cursors := `{"project:test":99}`
	req := httptest.NewRequest(http.MethodGet, "/v1/events/sse?channels=project:test&cursors="+url.QueryEscape(cursors), nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d (cursor map exceeds LastSequence)", rec.Code, http.StatusBadRequest)
	}
	if !strings.Contains(rec.Body.String(), "non-monotonic") {
		t.Fatalf("response = %s, want 'non-monotonic' problem detail", rec.Body.String())
	}
}

func TestReplayEndpointReturnsMissedEventsAfterDisconnect(t *testing.T) {
	now := time.Unix(100, 0).UTC()
	hub := NewEventHub(EventHubConfig{
		ReplayCapacity: 16,
		Now: func() time.Time {
			return now
		},
	})
	seen := hub.Publish(PublishInput{Channel: "project:test", Type: "project.ready", Data: map[string]any{"n": 1}})
	now = now.Add(10 * time.Minute)
	missedOne := hub.Publish(PublishInput{Channel: "project:test", Type: "bead.changed", Data: map[string]any{"n": 2}})
	missedTwo := hub.Publish(PublishInput{Channel: "project:test", Type: "agent.changed", Data: map[string]any{"n": 3}})
	hub.Publish(PublishInput{Channel: "activity:test", Type: "activity.appended", Data: map[string]any{"n": 4}})

	router := NewRouter(Config{Events: hub})
	req := httptest.NewRequest(http.MethodGet, "/v1/events/replay?channel=project:test&sinceSequence=1", nil)
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	var body schemas.EventReplayResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode replay response: %v", err)
	}
	if body.LatestSequence != int(missedTwo.Sequence) {
		t.Fatalf("latestSequence = %d, want %d", body.LatestSequence, missedTwo.Sequence)
	}
	if len(body.Events) != 2 {
		t.Fatalf("replayed events length = %d, want 2: %#v", len(body.Events), body.Events)
	}
	if body.Events[0].EventId != missedOne.EventID || body.Events[1].EventId != missedTwo.EventID {
		t.Fatalf("replayed event IDs = [%s %s], want [%s %s]", body.Events[0].EventId, body.Events[1].EventId, missedOne.EventID, missedTwo.EventID)
	}
	_ = seen
}

func TestWebSocketSubscribeEnvelopeSnapshotsThenLiveDeltas(t *testing.T) {
	hub := NewEventHub(EventHubConfig{})
	replayed := hub.Publish(PublishInput{Channel: "project:test", Type: "project.ready", Data: map[string]any{"n": 1}})
	server := httptest.NewServer(NewRouter(Config{Events: hub}))
	defer server.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	conn := dialEventWebSocket(t, ctx, server)
	defer conn.Close(websocket.StatusNormalClosure, "test complete")

	currentSchemaVersion := schemaVersion
	if err := writeWebSocketJSON(ctx, conn, subscribeRequest{
		Op:            "subscribe",
		Channels:      []string{"project:test"},
		Cursors:       map[string]uint64{"project:test": 0},
		SchemaVersion: &currentSchemaVersion,
	}); err != nil {
		t.Fatalf("write subscribe envelope: %v", err)
	}

	var snapshot struct {
		Op       string   `json:"op"`
		Snapshot Snapshot `json:"snapshot"`
	}
	readWebSocketJSON(t, ctx, conn, &snapshot)
	if snapshot.Op != "snapshot" {
		t.Fatalf("websocket op = %q, want snapshot", snapshot.Op)
	}
	projectSnapshot := snapshot.Snapshot.Channels["project:test"]
	if len(projectSnapshot.Replayed) != 1 || projectSnapshot.Replayed[0].EventID != replayed.EventID {
		t.Fatalf("snapshot replayed = %#v, want event %s", projectSnapshot.Replayed, replayed.EventID)
	}

	live := hub.Publish(PublishInput{Channel: "project:test", Type: "bead.changed", Data: map[string]any{"n": 2}})
	var delta Event
	readWebSocketJSON(t, ctx, conn, &delta)
	if delta.EventID != live.EventID || delta.Sequence != 2 || delta.Type != "bead.changed" {
		t.Fatalf("live delta = %+v, want %+v", delta, live)
	}
}

func TestWebSocketSubscribeEmitsGapForStaleCursor(t *testing.T) {
	hub := NewEventHub(EventHubConfig{ReplayCapacity: 2})
	hub.Publish(PublishInput{Channel: "project:test", Type: "first", Data: map[string]any{"n": 1}})
	hub.Publish(PublishInput{Channel: "project:test", Type: "second", Data: map[string]any{"n": 2}})
	hub.Publish(PublishInput{Channel: "project:test", Type: "third", Data: map[string]any{"n": 3}})
	hub.Publish(PublishInput{Channel: "project:test", Type: "fourth", Data: map[string]any{"n": 4}})
	server := httptest.NewServer(NewRouter(Config{Events: hub}))
	defer server.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	conn := dialEventWebSocket(t, ctx, server)
	defer conn.Close(websocket.StatusNormalClosure, "test complete")

	currentSchemaVersion := schemaVersion
	if err := writeWebSocketJSON(ctx, conn, subscribeRequest{
		Op:            "subscribe",
		Channels:      []string{"project:test"},
		Cursors:       map[string]uint64{"project:test": 1},
		SchemaVersion: &currentSchemaVersion,
	}); err != nil {
		t.Fatalf("write subscribe envelope: %v", err)
	}

	var snapshot struct {
		Op       string   `json:"op"`
		Snapshot Snapshot `json:"snapshot"`
	}
	readWebSocketJSON(t, ctx, conn, &snapshot)
	if !snapshot.Snapshot.Channels["project:test"].Gap {
		t.Fatalf("snapshot gap = false, want true: %+v", snapshot.Snapshot.Channels["project:test"])
	}

	var gap Event
	readWebSocketJSON(t, ctx, conn, &gap)
	if gap.Type != "_gap" || gap.Channel != "project:test" {
		t.Fatalf("gap event = %+v, want project:test _gap", gap)
	}
	data, ok := gap.Data.(map[string]any)
	if !ok {
		t.Fatalf("gap data type = %T, want map[string]any", gap.Data)
	}
	if data["repair"] != "replayEvents" || data["from"] != float64(2) || data["to"] != float64(2) {
		t.Fatalf("gap data = %#v, want from=2 to=2 repair=replayEvents", data)
	}
}

func TestWebSocketSubscribeSchemaMismatchEmitsCompatibilityWarning(t *testing.T) {
	hub := NewEventHub(EventHubConfig{})
	server := httptest.NewServer(NewRouter(Config{Events: hub}))
	defer server.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	conn := dialEventWebSocket(t, ctx, server)
	defer conn.Close(websocket.StatusNormalClosure, "test complete")

	olderSchemaVersion := schemaVersion - 1
	if err := writeWebSocketJSON(ctx, conn, subscribeRequest{
		Op:            "subscribe",
		Channels:      []string{"project:test"},
		Cursors:       map[string]uint64{"project:test": 0},
		SchemaVersion: &olderSchemaVersion,
	}); err != nil {
		t.Fatalf("write subscribe envelope: %v", err)
	}

	var snapshot struct {
		Op string `json:"op"`
	}
	readWebSocketJSON(t, ctx, conn, &snapshot)
	if snapshot.Op != "snapshot" {
		t.Fatalf("websocket op = %q, want snapshot", snapshot.Op)
	}

	var warning Event
	readWebSocketJSON(t, ctx, conn, &warning)
	if warning.Channel != "_system" || warning.Type != "_compatibility_warning" {
		t.Fatalf("warning event = %+v, want _system _compatibility_warning", warning)
	}
	data, ok := warning.Data.(map[string]any)
	if !ok {
		t.Fatalf("warning data type = %T, want map[string]any", warning.Data)
	}
	if data["clientSchemaVersion"] != float64(olderSchemaVersion) || data["serverSchemaVersion"] != float64(schemaVersion) {
		t.Fatalf("warning data = %#v, want client/server schema versions", data)
	}
}

type staticJobReader struct {
	jobs      []jobstore.Job
	logs      map[string][]byte
	artifacts map[string][]jobstore.Artifact
}

type routerProjectRunner struct {
	root string
}

func (r routerProjectRunner) Run(_ context.Context, dir string, argv []string) (projectstore.CommandResult, error) {
	switch {
	case reflect.DeepEqual(argv, []string{"git", "rev-parse", "--is-inside-work-tree"}):
		if filepath.Clean(dir) != filepath.Clean(r.root) {
			return projectstore.CommandResult{ExitCode: 1}, nil
		}
		return projectstore.CommandResult{Stdout: []byte("true\n")}, nil
	case reflect.DeepEqual(argv, []string{"git", "remote", "get-url", "origin"}):
		return projectstore.CommandResult{Stdout: []byte("https://example.invalid/api.git\n")}, nil
	case reflect.DeepEqual(argv, []string{"git", "branch", "--show-current"}):
		return projectstore.CommandResult{Stdout: []byte("main\n")}, nil
	case reflect.DeepEqual(argv, []string{"br", "init"}):
		if err := os.MkdirAll(filepath.Join(dir, ".beads"), 0o755); err != nil {
			return projectstore.CommandResult{}, err
		}
		return projectstore.CommandResult{}, nil
	default:
		return projectstore.CommandResult{ExitCode: 127, Stderr: []byte("unexpected command")}, nil
	}
}

func newRouterProjectService(t *testing.T, root string) *projectstore.Service {
	t.Helper()
	db, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "projects.db"))
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	t.Cleanup(func() {
		_ = db.Close()
	})
	store, err := projectstore.NewSQLStore(context.Background(), db)
	if err != nil {
		t.Fatalf("new project store: %v", err)
	}
	service, err := projectstore.NewService(projectstore.ServiceConfig{
		Store:  store,
		Runner: routerProjectRunner{root: root},
		Now: func() time.Time {
			return time.Date(2026, 5, 4, 0, 0, 0, 0, time.UTC)
		},
	})
	if err != nil {
		t.Fatalf("new project service: %v", err)
	}
	return service
}

func writeRouterTestFile(t *testing.T, path string, body string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func (r staticJobReader) List(context.Context, jobstore.ListFilter) ([]jobstore.Job, error) {
	return append([]jobstore.Job(nil), r.jobs...), nil
}

func (r staticJobReader) Get(_ context.Context, id string) (jobstore.Job, error) {
	for _, job := range r.jobs {
		if job.ID == id {
			return job, nil
		}
	}
	return jobstore.Job{}, jobstore.ErrNotFound
}

func (r staticJobReader) ReadLog(_ context.Context, id string, offset int64, limit int64) (jobstore.LogChunk, error) {
	body, ok := r.logs[id]
	if !ok {
		return jobstore.LogChunk{}, jobstore.ErrNotFound
	}
	if offset < 0 {
		return jobstore.LogChunk{}, jobstore.ErrInvalidRequest
	}
	if int(offset) > len(body) {
		offset = int64(len(body))
	}
	if limit <= 0 || int(offset+limit) > len(body) {
		limit = int64(len(body)) - offset
	}
	next := offset + limit
	return jobstore.LogChunk{
		JobID:      id,
		Offset:     offset,
		NextOffset: next,
		Data:       append([]byte(nil), body[int(offset):int(next)]...),
		EOF:        int(next) >= len(body),
	}, nil
}

func (r staticJobReader) ListArtifacts(_ context.Context, id string) ([]jobstore.Artifact, error) {
	artifacts, ok := r.artifacts[id]
	if !ok {
		return nil, jobstore.ErrNotFound
	}
	return append([]jobstore.Artifact(nil), artifacts...), nil
}

type staticJobController struct {
	staticJobReader
	cancelRequests []jobstore.CancelRequest
}

func (r *staticJobController) Cancel(_ context.Context, request jobstore.CancelRequest) (jobstore.Job, error) {
	r.cancelRequests = append(r.cancelRequests, request)
	for _, job := range r.jobs {
		if job.ID == request.JobID {
			job.Status = jobstore.StatusCanceling
			return job, nil
		}
	}
	return jobstore.Job{}, jobstore.ErrNotFound
}

func TestSafeSSEEventName(t *testing.T) {
	for _, tc := range []struct {
		name string
		in   string
		want string
	}{
		{name: "empty", in: "", want: "message"},
		{name: "known", in: "bead.changed", want: "bead.changed"},
		{name: "underscore", in: "_lag", want: "_lag"},
		{name: "newline", in: "bad\nevent", want: "message"},
		{name: "colon", in: "bad:event", want: "message"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := safeSSEEventName(tc.in); got != tc.want {
				t.Fatalf("safeSSEEventName(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

func TestParseChannelsDropsUnsafeNames(t *testing.T) {
	got := parseChannels("_system,project:abc,bad\nchannel,bad/channel")
	want := []string{"_system", "project:abc"}
	if len(got) != len(want) {
		t.Fatalf("parseChannels length = %d, want %d: %#v", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("parseChannels[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

func dialEventWebSocket(t *testing.T, ctx context.Context, server *httptest.Server) *websocket.Conn {
	t.Helper()
	wsURL := "ws" + strings.TrimPrefix(server.URL, "http") + "/v1/events/ws"
	conn, _, err := websocket.Dial(ctx, wsURL, nil)
	if err != nil {
		t.Fatalf("websocket dial: %v", err)
	}
	return conn
}

func readWebSocketJSON(t *testing.T, ctx context.Context, conn *websocket.Conn, target any) {
	t.Helper()
	messageType, body, err := conn.Read(ctx)
	if err != nil {
		t.Fatalf("read websocket message: %v", err)
	}
	if messageType != websocket.MessageText {
		t.Fatalf("websocket message type = %v, want text", messageType)
	}
	if err := json.Unmarshal(body, target); err != nil {
		t.Fatalf("decode websocket message %s: %v", string(body), err)
	}
}
