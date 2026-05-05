package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/hoopoe-cockpit/hoopoe/apps/daemon/internal/approvals"
	"github.com/hoopoe-cockpit/hoopoe/apps/daemon/internal/audit"
	"github.com/hoopoe-cockpit/hoopoe/apps/daemon/internal/redaction"
	schemas "github.com/hoopoe-cockpit/hoopoe/packages/schemas/go"
)

func TestAuditQueryFiltersAndReturnsFacets(t *testing.T) {
	now := time.Date(2026, 5, 4, 8, 0, 0, 0, time.UTC)
	writer, err := audit.NewWriter(audit.Config{Writer: nopSyncWriter{}, Now: func() time.Time { return now }})
	if err != nil {
		t.Fatalf("new audit writer: %v", err)
	}
	appendAuditEntry(t, writer, audit.Entry{
		ProjectID:     "proj_a",
		Action:        "swarm.halt",
		Actor:         audit.Actor{Kind: audit.ActorAgent, ID: "agent_1"},
		Result:        audit.ResultFailure,
		Reason:        "rate limit [REDACTED:provider-key]",
		CorrelationID: "corr_swarm",
		Data:          map[string]any{"source": "test"},
	})
	appendAuditEntry(t, writer, audit.Entry{
		ProjectID:     "proj_a",
		Action:        "audit.export_completed",
		Actor:         audit.Actor{Kind: audit.ActorSystem, ID: "daemon"},
		Result:        audit.ResultSuccess,
		CorrelationID: "corr_export",
	})
	router := NewRouter(Config{Audit: writer})

	req := httptest.NewRequest(http.MethodGet, "/v1/audit/query?projectId=proj_a&actorKind=agent&q=rate&limit=10", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body=%s", rec.Code, http.StatusOK, rec.Body.String())
	}
	var body auditQueryResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(body.Items) != 1 || body.Items[0].Action != "swarm.halt" {
		t.Fatalf("items = %+v, want swarm.halt", body.Items)
	}
	if body.Items[0].Severity != "urgent" || body.Items[0].Category != "swarm" {
		t.Fatalf("derived fields = %+v", body.Items[0])
	}
	if len(body.Facets.ActorKinds) != 1 || body.Facets.ActorKinds[0].Value != "agent" {
		t.Fatalf("facets = %+v", body.Facets)
	}
}

func TestAuditCorrelationFilterAndExportEndpoint(t *testing.T) {
	now := time.Date(2026, 5, 4, 8, 0, 0, 0, time.UTC)
	writer, err := audit.NewWriter(audit.Config{Writer: nopSyncWriter{}, Now: func() time.Time { return now }})
	if err != nil {
		t.Fatalf("new audit writer: %v", err)
	}
	parent := appendAuditEntry(t, writer, audit.Entry{
		ProjectID:     "proj_a",
		Action:        "approval.created",
		Actor:         audit.Actor{Kind: audit.ActorUser, ID: "operator"},
		Result:        audit.ResultApprovalRequired,
		CorrelationID: "corr_approval",
	})
	appendAuditEntry(t, writer, audit.Entry{
		ProjectID:     "proj_a",
		Action:        "approval.approved",
		Actor:         audit.Actor{Kind: audit.ActorUser, ID: "operator"},
		Result:        audit.ResultSuccess,
		CorrelationID: "corr_approval",
		CausationID:   parent.EventID,
		ApprovalID:    "appr_1",
	})
	router := NewRouter(Config{
		Audit: writer,
		Now:   func() time.Time { return now },
	})

	corrReq := httptest.NewRequest(http.MethodGet, "/v1/audit/query?correlationId=corr_approval", nil)
	corrRec := httptest.NewRecorder()
	router.ServeHTTP(corrRec, corrReq)
	if corrRec.Code != http.StatusOK {
		t.Fatalf("correlation status = %d; body=%s", corrRec.Code, corrRec.Body.String())
	}
	var corr auditQueryResponse
	if err := json.Unmarshal(corrRec.Body.Bytes(), &corr); err != nil {
		t.Fatalf("decode correlation: %v", err)
	}
	if len(corr.Items) != 2 || corr.Items[0].CausationID != parent.EventID {
		t.Fatalf("correlation response = %+v", corr)
	}

	exportReq := httptest.NewRequest(http.MethodPost, "/v1/audit/export", strings.NewReader(`{"projectId":"proj_a","correlationId":"corr_approval"}`))
	exportReq.Header.Set("Content-Type", "application/json")
	exportRec := httptest.NewRecorder()
	router.ServeHTTP(exportRec, exportReq)
	if exportRec.Code != http.StatusOK {
		t.Fatalf("export status = %d; body=%s", exportRec.Code, exportRec.Body.String())
	}
	var export auditExportResponse
	if err := json.Unmarshal(exportRec.Body.Bytes(), &export); err != nil {
		t.Fatalf("decode export: %v", err)
	}
	if export.TotalEntries != 2 || export.Sha256 == "" || !export.Redacted {
		t.Fatalf("export response = %+v", export)
	}

	for _, test := range []struct {
		name string
		body string
	}{
		{name: "empty", body: `{}`},
		{name: "from_only", body: `{"from":"2026-04-01T00:00:00Z"}`},
		{name: "to_only", body: `{"to":"2026-05-04T00:00:00Z"}`},
	} {
		t.Run("requires approval for "+test.name+" export window", func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, "/v1/audit/export", strings.NewReader(test.body))
			req.Header.Set("Content-Type", "application/json")
			rec := httptest.NewRecorder()
			router.ServeHTTP(rec, req)
			if rec.Code != http.StatusUnprocessableEntity {
				t.Fatalf("status = %d, want %d; body=%s", rec.Code, http.StatusUnprocessableEntity, rec.Body.String())
			}
		})
	}

	wideReq := httptest.NewRequest(http.MethodPost, "/v1/audit/export", strings.NewReader(`{"from":"2026-04-01T00:00:00Z","to":"2026-05-04T00:00:00Z"}`))
	wideRec := httptest.NewRecorder()
	router.ServeHTTP(wideRec, wideReq)
	if wideRec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("wide export status = %d, want %d", wideRec.Code, http.StatusUnprocessableEntity)
	}
}

func TestAuditExportApprovalIDMustValidateAndConsumeOnce(t *testing.T) {
	now := time.Date(2026, 5, 4, 8, 0, 0, 0, time.UTC)
	writer, err := audit.NewWriter(audit.Config{Writer: nopSyncWriter{}, Now: func() time.Time { return now }})
	if err != nil {
		t.Fatalf("new audit writer: %v", err)
	}
	appendAuditEntry(t, writer, audit.Entry{
		ProjectID: "proj_a",
		Action:    "auth.bootstrap",
		Actor:     audit.Actor{Kind: audit.ActorUser, ID: "operator"},
		Result:    audit.ResultSuccess,
	})
	approvalQueue := approvals.NewQueue(approvals.Config{
		Now: func() time.Time { return now },
		NewID: func(approvals.Request) (string, error) {
			return "appr_export_01", nil
		},
	})
	router := NewRouter(Config{
		Audit:     writer,
		Approvals: approvalQueue,
		Now:       func() time.Time { return now },
	})
	wideBody := `{"projectId":"proj_a","from":"2026-04-01T00:00:00Z","to":"2026-05-04T00:00:00Z"}`

	emptyHeader := httptest.NewRequest(http.MethodPost, "/v1/audit/export", strings.NewReader(wideBody))
	emptyHeader.Header.Set("Content-Type", "application/json")
	emptyRec := httptest.NewRecorder()
	router.ServeHTTP(emptyRec, emptyHeader)
	if emptyRec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("empty approval status = %d, want %d; body=%s", emptyRec.Code, http.StatusUnprocessableEntity, emptyRec.Body.String())
	}

	madeUp := httptest.NewRequest(http.MethodPost, "/v1/audit/export", strings.NewReader(wideBody))
	madeUp.Header.Set("Content-Type", "application/json")
	madeUp.Header.Set(auditExportApprovalHeader, "appr_made_up")
	madeUpRec := httptest.NewRecorder()
	router.ServeHTTP(madeUpRec, madeUp)
	if madeUpRec.Code != http.StatusUnauthorized {
		t.Fatalf("made-up approval status = %d, want %d; body=%s", madeUpRec.Code, http.StatusUnauthorized, madeUpRec.Body.String())
	}

	approvalID := createApprovedAuditExportApproval(t, approvalQueue, now, "proj_a")
	valid := httptest.NewRequest(http.MethodPost, "/v1/audit/export", strings.NewReader(wideBody))
	valid.Header.Set("Content-Type", "application/json")
	valid.Header.Set(auditExportApprovalHeader, approvalID)
	validRec := httptest.NewRecorder()
	router.ServeHTTP(validRec, valid)
	if validRec.Code != http.StatusOK {
		t.Fatalf("valid approval status = %d, want %d; body=%s", validRec.Code, http.StatusOK, validRec.Body.String())
	}
	var validExport auditExportResponse
	if err := json.Unmarshal(validRec.Body.Bytes(), &validExport); err != nil {
		t.Fatalf("decode valid export: %v", err)
	}
	if validExport.ApprovalID == nil || *validExport.ApprovalID != approvalID {
		t.Fatalf("export approval id = %v, want %q", validExport.ApprovalID, approvalID)
	}

	consumed, ok, err := approvalQueue.Get(context.Background(), approvalID)
	if err != nil || !ok {
		t.Fatalf("lookup consumed approval: approval=%+v ok=%v err=%v", consumed, ok, err)
	}
	if consumed.State != schemas.Revoked {
		t.Fatalf("approval state after export = %s, want revoked", consumed.State)
	}

	replay := httptest.NewRequest(http.MethodPost, "/v1/audit/export", strings.NewReader(wideBody))
	replay.Header.Set("Content-Type", "application/json")
	replay.Header.Set(auditExportApprovalHeader, approvalID)
	replayRec := httptest.NewRecorder()
	router.ServeHTTP(replayRec, replay)
	if replayRec.Code != http.StatusUnauthorized {
		t.Fatalf("replay approval status = %d, want %d; body=%s", replayRec.Code, http.StatusUnauthorized, replayRec.Body.String())
	}
}

func TestAuditQueryRejectsOverlongSearch(t *testing.T) {
	now := time.Date(2026, 5, 4, 8, 0, 0, 0, time.UTC)
	writer, err := audit.NewWriter(audit.Config{Writer: nopSyncWriter{}, Now: func() time.Time { return now }})
	if err != nil {
		t.Fatalf("new audit writer: %v", err)
	}
	router := NewRouter(Config{Audit: writer})

	overlong := strings.Repeat("a", maxAuditSearchQueryLen+1)
	req := httptest.NewRequest(http.MethodGet, "/v1/audit/query?q="+overlong, nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("query status = %d, want %d; body=%s", rec.Code, http.StatusBadRequest, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "audit.invalid_search") {
		t.Fatalf("expected audit.invalid_search problem code, got %s", rec.Body.String())
	}

	exportBody := `{"q":"` + overlong + `"}`
	exportReq := httptest.NewRequest(http.MethodPost, "/v1/audit/export", strings.NewReader(exportBody))
	exportReq.Header.Set("Content-Type", "application/json")
	exportRec := httptest.NewRecorder()
	router.ServeHTTP(exportRec, exportReq)
	if exportRec.Code != http.StatusBadRequest {
		t.Fatalf("export status = %d, want %d; body=%s", exportRec.Code, http.StatusBadRequest, exportRec.Body.String())
	}
}

func TestAuditQueryAcceptsBoundedSearch(t *testing.T) {
	now := time.Date(2026, 5, 4, 8, 0, 0, 0, time.UTC)
	writer, err := audit.NewWriter(audit.Config{Writer: nopSyncWriter{}, Now: func() time.Time { return now }})
	if err != nil {
		t.Fatalf("new audit writer: %v", err)
	}
	router := NewRouter(Config{Audit: writer})
	atMax := strings.Repeat("a", maxAuditSearchQueryLen)
	req := httptest.NewRequest(http.MethodGet, "/v1/audit/query?q="+atMax, nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("at-max query status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
}

// TestAuditQueryRejectsOverlongFilters guards hp-kjy: hp-fbz capped q
// at 256 bytes but left projectId, actorKind, actorId, action, and
// outcome unbounded. parseAuditFilterToken now caps each at
// maxAuditFilterTokenLen (128) and rejects control bytes; sending an
// overlong value through either /v1/audit/query (GET) or the typed
// /v1/audit/export (POST) body must surface as a 400 with the
// per-field problem code.
func TestAuditQueryRejectsOverlongFilters(t *testing.T) {
	now := time.Date(2026, 5, 4, 8, 0, 0, 0, time.UTC)
	overlong := strings.Repeat("a", maxAuditFilterTokenLen+1)

	cases := []struct {
		field        string
		queryParam   string
		exportField  string
		problemCode  string
	}{
		{field: "ProjectID", queryParam: "projectId", exportField: "projectId", problemCode: "audit.invalid_project"},
		{field: "ActorKind", queryParam: "actorKind", exportField: "actorKind", problemCode: "audit.invalid_actor_kind"},
		{field: "ActorID", queryParam: "actorId", exportField: "actorId", problemCode: "audit.invalid_actor_id"},
		{field: "Action", queryParam: "action", exportField: "action", problemCode: "audit.invalid_action"},
		{field: "Outcome", queryParam: "outcome", exportField: "outcome", problemCode: "audit.invalid_outcome"},
	}

	for _, tc := range cases {
		t.Run(tc.field, func(t *testing.T) {
			writer, err := audit.NewWriter(audit.Config{Writer: nopSyncWriter{}, Now: func() time.Time { return now }})
			if err != nil {
				t.Fatalf("new audit writer: %v", err)
			}
			router := NewRouter(Config{Audit: writer})

			// GET path.
			req := httptest.NewRequest(http.MethodGet, "/v1/audit/query?"+tc.queryParam+"="+overlong, nil)
			rec := httptest.NewRecorder()
			router.ServeHTTP(rec, req)
			if rec.Code != http.StatusBadRequest {
				t.Fatalf("GET status = %d, want %d; body=%s", rec.Code, http.StatusBadRequest, rec.Body.String())
			}
			if !strings.Contains(rec.Body.String(), tc.problemCode) {
				t.Fatalf("GET body missing %q: %s", tc.problemCode, rec.Body.String())
			}

			// POST /v1/audit/export path — same validator applies.
			body := `{"` + tc.exportField + `":"` + overlong + `"}`
			exportReq := httptest.NewRequest(http.MethodPost, "/v1/audit/export", strings.NewReader(body))
			exportReq.Header.Set("Content-Type", "application/json")
			exportRec := httptest.NewRecorder()
			router.ServeHTTP(exportRec, exportReq)
			if exportRec.Code != http.StatusBadRequest {
				t.Fatalf("POST status = %d, want %d; body=%s", exportRec.Code, http.StatusBadRequest, exportRec.Body.String())
			}
		})
	}
}

// TestAuditQueryRejectsControlBytesInFilters complements the length
// gate: control characters (NUL, BEL, etc.) in any of the five
// categorical filters are refused with a 400. URL-encoded so the
// request itself is valid.
func TestAuditQueryRejectsControlBytesInFilters(t *testing.T) {
	now := time.Date(2026, 5, 4, 8, 0, 0, 0, time.UTC)
	writer, err := audit.NewWriter(audit.Config{Writer: nopSyncWriter{}, Now: func() time.Time { return now }})
	if err != nil {
		t.Fatalf("new audit writer: %v", err)
	}
	router := NewRouter(Config{Audit: writer})

	// %00 = NUL — must be rejected.
	req := httptest.NewRequest(http.MethodGet, "/v1/audit/query?actorId=ag%00ent", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d; body=%s", rec.Code, http.StatusBadRequest, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "audit.invalid_actor_id") {
		t.Fatalf("body missing audit.invalid_actor_id: %s", rec.Body.String())
	}
}

// TestAuditQueryAcceptsBoundedFilters confirms the boundary case: a
// filter at maxAuditFilterTokenLen bytes (128) is accepted. Pins down
// the exact threshold so an off-by-one regression in the cap is caught.
func TestAuditQueryAcceptsBoundedFilters(t *testing.T) {
	now := time.Date(2026, 5, 4, 8, 0, 0, 0, time.UTC)
	writer, err := audit.NewWriter(audit.Config{Writer: nopSyncWriter{}, Now: func() time.Time { return now }})
	if err != nil {
		t.Fatalf("new audit writer: %v", err)
	}
	router := NewRouter(Config{Audit: writer})

	atMax := strings.Repeat("a", maxAuditFilterTokenLen)
	req := httptest.NewRequest(http.MethodGet, "/v1/audit/query?actorId="+atMax, nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("at-max query status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
}

func createApprovedAuditExportApproval(t *testing.T, queue *approvals.Queue, now time.Time, projectID string) string {
	t.Helper()
	approval, _, err := queue.Request(context.Background(), approvals.Request{
		Source:          schemas.ApprovalSourceHoopoePolicy,
		PolicyRule:      "hoopoe-policy:audit.export",
		RequestedAction: schemas.CommandSpec{Kind: auditExportApprovalActionKind, Target: map[string]any{"projectId": projectID}},
		RequestActor:    schemas.Actor{Kind: schemas.ActorKindUser, Id: stringPtr("operator")},
		Reason:          "wide audit export requires operator approval",
		EvidenceRefs:    []string{"audit.export.request"},
		ProjectID:       projectID,
		Scope:           schemas.Once,
		RiskClass:       schemas.Critical,
		RequestedAt:     now.Add(-time.Minute),
		ExpiresAt:       timePtr(now.Add(10 * time.Minute)),
		IdempotencyKey:  "audit-export-" + projectID,
	})
	if err != nil {
		t.Fatalf("request approval: %v", err)
	}
	approved, err := queue.Approve(context.Background(), approval.Id, schemas.ApprovalDecisionRequest{
		DecisionActor: schemas.Actor{Kind: schemas.ActorKindUser, Id: stringPtr("operator")},
		Note:          stringPtr("approved for test export"),
	})
	if err != nil {
		t.Fatalf("approve audit export approval: %v", err)
	}
	return approved.Id
}

func timePtr(value time.Time) *time.Time {
	return &value
}

func appendAuditEntry(t *testing.T, writer *audit.Writer, entry audit.Entry) audit.Entry {
	t.Helper()
	written, _, err := writer.Append(entry)
	if err != nil {
		t.Fatalf("append audit entry: %v", err)
	}
	return written
}

type nopSyncWriter struct{}

func (nopSyncWriter) Write(p []byte) (int, error) { return len(p), nil }
func (nopSyncWriter) Sync() error                 { return nil }

// failingAuditWriter satisfies AuditLog + auditAppender, returning the
// configured error from Append. Used to simulate disk full / lock
// contention on /v1/audit/export so we can assert the request fails when
// audit.export_completed cannot be persisted (hp-gysl item 5).
type failingAuditWriter struct {
	appendErr error
	queryErr  error
	appended  []audit.Entry
}

func (*failingAuditWriter) Query(audit.Query) ([]audit.Entry, error) { return nil, nil }

func (w *failingAuditWriter) Append(entry audit.Entry) (audit.Entry, []redaction.TraceEvent, error) {
	w.appended = append(w.appended, entry)
	return entry, nil, w.appendErr
}

func TestAuditExportFailsWhenCompletedAuditAppendFails(t *testing.T) {
	// hp-gysl item 5: if audit.export_completed cannot be persisted, the
	// request must fail. Otherwise the export was generated but no
	// forensics record exists — silent data egress, which is exactly the
	// failure mode the audit log is supposed to prevent.
	now := time.Date(2026, 5, 4, 8, 0, 0, 0, time.UTC)
	writer := &failingAuditWriter{appendErr: errors.New("disk full")}
	router := NewRouter(Config{
		Audit: writer,
		Now:   func() time.Time { return now },
	})
	req := httptest.NewRequest(http.MethodPost, "/v1/audit/export", strings.NewReader(`{"projectId":"proj_a","correlationId":"corr_lookup"}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want %d; body=%s", rec.Code, http.StatusInternalServerError, rec.Body.String())
	}
	if got := rec.Header().Get("Content-Type"); !strings.HasPrefix(got, "application/problem+json") {
		t.Fatalf("content-type = %q, want problem+json", got)
	}
	var problem schemas.Problem
	if err := json.Unmarshal(rec.Body.Bytes(), &problem); err != nil {
		t.Fatalf("decode problem: %v", err)
	}
	if problem.Code != "audit.export_audit_append_failed" {
		t.Fatalf("problem.Code = %q, want audit.export_audit_append_failed", problem.Code)
	}
	// Both started + completed should have been attempted; failed is
	// only attempted when the underlying query errors, so it stays at 0
	// here. Asserts we tried to write a completion record (it just
	// failed) — the gate is not "skip the append entirely."
	gotActions := make([]string, 0, len(writer.appended))
	for _, entry := range writer.appended {
		gotActions = append(gotActions, entry.Action)
	}
	wantContains := []string{"audit.export_started", "audit.export_completed"}
	for _, want := range wantContains {
		found := false
		for _, got := range gotActions {
			if got == want {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("appended actions = %v, missing %q", gotActions, want)
		}
	}
}

// FuzzAuditQueryFromRequest guards hp-bjm3: /v1/audit/query parses
// untrusted URL query parameters for projectId, actorKind, actorId,
// action, outcome, correlationId, causationId, q, from, and limit.
// The parser helpers (parseAuditLimit, parseOptionalAuditTime,
// parseAuditLookupToken, parseAuditSearch) enforce length, charset,
// and timestamp invariants. A panic at this boundary corrupts the
// read path; a missed length cap lets a malicious caller drive O(n²)
// substring scans across audit entries via a giant `q`. Run with:
//
//	go test -fuzz=FuzzAuditQueryFromRequest -fuzztime=20s ./apps/daemon/internal/api/
func FuzzAuditQueryFromRequest(f *testing.F) {
	// Realistic shapes.
	f.Add("projectId=p&limit=10")
	f.Add("from=2026-01-01T00:00:00Z&to=2026-01-02T00:00:00Z")
	f.Add("actorKind=agent&actorId=a1&action=swarm.halt&outcome=failure")
	// Limit boundaries.
	f.Add("limit=1")
	f.Add("limit=1000")
	f.Add("limit=0")
	f.Add("limit=1001")
	f.Add("limit=-1")
	f.Add("limit=abc")
	f.Add("limit=999999999999999999999")
	// Timestamp shapes.
	f.Add("from=2026-01-01T00:00:00.123456789Z")
	f.Add("from=not-a-time")
	f.Add("from=2026-01-01")
	f.Add("from=2026-01-01T00:00:00+05:30")
	// Lookup-token charset / length.
	f.Add("correlationId=" + strings.Repeat("a", 160))
	f.Add("correlationId=" + strings.Repeat("a", 161))
	f.Add("correlationId=abc%20def") // URL-decoded space — must reject.
	f.Add("causationId=αβγ")         // unicode — must reject.
	f.Add("correlationId=foo/bar.baz:qux-1_2")
	// Search length / unicode / control bytes.
	f.Add("q=" + strings.Repeat("x", 256))
	f.Add("q=" + strings.Repeat("x", 257))
	f.Add("q=漢字")
	f.Add("q=" + string([]byte{0x00, 0x01, 0x02, 0x03}))
	// URL-encoded weirdness.
	f.Add("q=hello%20world")
	f.Add("q=%E6%BC%A2%E5%AD%97")
	// Repeated keys (URL allows; only first is consumed).
	f.Add("limit=10&limit=999999")
	// Empty / pathological.
	f.Add("")
	f.Add("&&&&")
	f.Add("=")
	f.Add("?=&")
	f.Add("&=&=")

	srv := &server{}

	f.Fuzz(func(t *testing.T, query string) {
		// Construct *http.Request directly: httptest.NewRequest +
		// http.NewRequest both reject control characters in the URL by
		// panicking / erroring before the handler runs, but
		// auditQueryFromRequest reads r.URL.Query() which is exactly
		// the boundary we want to fuzz. By feeding RawQuery directly,
		// we exercise the production code path with bytes that a
		// permissive frontend (proxies, instrumentation, custom
		// transports) could plausibly forward through.
		req := &http.Request{
			Method: http.MethodGet,
			URL:    &url.URL{Path: "/v1/audit/query", RawQuery: query},
			Header: http.Header{},
		}
		rec := httptest.NewRecorder()

		result, ok := srv.auditQueryFromRequest(rec, req)
		if !ok {
			// Parse failure: must surface as a 400 problem+json envelope.
			if rec.Code != http.StatusBadRequest {
				t.Fatalf("auditQueryFromRequest failed but status = %d, want %d; body=%s",
					rec.Code, http.StatusBadRequest, rec.Body.String())
			}
			return
		}

		// Success path invariants ─────────────────────────────────────────
		if result.Limit < 1 || result.Limit > maxAuditQueryLimit {
			t.Fatalf("Limit=%d outside [1, %d] for query=%q", result.Limit, maxAuditQueryLimit, query)
		}
		if !result.From.IsZero() && result.From.Location() != time.UTC {
			t.Fatalf("From not UTC-normalized: %v (loc=%v) for query=%q",
				result.From, result.From.Location(), query)
		}
		if !result.To.IsZero() && result.To.Location() != time.UTC {
			t.Fatalf("To not UTC-normalized: %v (loc=%v) for query=%q",
				result.To, result.To.Location(), query)
		}
		if len(result.CorrelationID) > 160 {
			t.Fatalf("CorrelationID len=%d > 160 for query=%q", len(result.CorrelationID), query)
		}
		if len(result.CausationID) > 160 {
			t.Fatalf("CausationID len=%d > 160 for query=%q", len(result.CausationID), query)
		}
		if len(result.Search) > maxAuditSearchQueryLen {
			t.Fatalf("Search len=%d > %d for query=%q",
				len(result.Search), maxAuditSearchQueryLen, query)
		}
		// Lookup tokens must satisfy isAuditLookupRune (no control bytes,
		// no non-ASCII): the parser is the gate, so a successful return
		// is a claim that this holds.
		for _, r := range result.CorrelationID {
			if !isAuditLookupRune(r) {
				t.Fatalf("CorrelationID admits non-lookup rune %q for query=%q", r, query)
			}
		}
		for _, r := range result.CausationID {
			if !isAuditLookupRune(r) {
				t.Fatalf("CausationID admits non-lookup rune %q for query=%q", r, query)
			}
		}
	})
}

// FuzzAuditQueryFromExportRequest guards hp-bjm3 for the typed export
// surface: POST /v1/audit/export accepts a JSON body whose fields are
// fed through the same parser helpers as the URL query. The invariants
// are identical (no panic, length/charset bounded, UTC timestamps), but
// the input shape is structured so the fuzzer mutates each field
// independently. Run with:
//
//	go test -fuzz=FuzzAuditQueryFromExportRequest -fuzztime=20s ./apps/daemon/internal/api/
func FuzzAuditQueryFromExportRequest(f *testing.F) {
	f.Add("p", "user", "u1", "x.test", "success", "corr_a", "caus_b", "rate", "2026-01-01T00:00:00Z", "2026-01-02T00:00:00Z")
	f.Add("", "", "", "", "", "", "", "", "", "")
	f.Add("p", "agent", "a1", "swarm.halt", "failure", strings.Repeat("a", 161), "", "", "", "")
	f.Add("p", "agent", "a1", "swarm.halt", "failure", "", "αβγ", "", "", "")
	f.Add("p", "agent", "a1", "swarm.halt", "failure", "", "", strings.Repeat("x", 257), "", "")
	f.Add("p", "agent", "a1", "swarm.halt", "failure", "", "", "", "not-a-time", "")
	f.Add("p", "agent", "a1", "swarm.halt", "failure", "", "", string([]byte{0x00, 0x01}), "", "")

	f.Fuzz(func(t *testing.T,
		projectID, actorKind, actorID, action, outcome,
		correlationID, causationID, search, from, to string,
	) {
		req := auditExportRequest{
			ProjectID:     projectID,
			ActorKind:     actorKind,
			ActorID:       actorID,
			Action:        action,
			Outcome:       outcome,
			CorrelationID: correlationID,
			CausationID:   causationID,
			Query:         search,
			From:          from,
			To:            to,
		}
		query, err := auditQueryFromExportRequest(req)
		if err != nil {
			// All error paths are explicit parser-helper failures —
			// never a panic, never a bare json error. The error must be
			// a non-nil Go error; nothing else to assert here (callers
			// surface it via writeProblemCode at the HTTP layer).
			return
		}
		// Success-path invariants — identical to the URL query target.
		if query.Limit < 1 || query.Limit > maxAuditQueryLimit {
			t.Fatalf("Limit=%d outside [1, %d]", query.Limit, maxAuditQueryLimit)
		}
		if !query.From.IsZero() && query.From.Location() != time.UTC {
			t.Fatalf("From not UTC-normalized: %v (loc=%v)", query.From, query.From.Location())
		}
		if !query.To.IsZero() && query.To.Location() != time.UTC {
			t.Fatalf("To not UTC-normalized: %v (loc=%v)", query.To, query.To.Location())
		}
		if len(query.CorrelationID) > 160 {
			t.Fatalf("CorrelationID len=%d > 160", len(query.CorrelationID))
		}
		if len(query.CausationID) > 160 {
			t.Fatalf("CausationID len=%d > 160", len(query.CausationID))
		}
		if len(query.Search) > maxAuditSearchQueryLen {
			t.Fatalf("Search len=%d > %d", len(query.Search), maxAuditSearchQueryLen)
		}
		for _, r := range query.CorrelationID {
			if !isAuditLookupRune(r) {
				t.Fatalf("CorrelationID admits non-lookup rune %q", r)
			}
		}
		for _, r := range query.CausationID {
			if !isAuditLookupRune(r) {
				t.Fatalf("CausationID admits non-lookup rune %q", r)
			}
		}
	})
}
