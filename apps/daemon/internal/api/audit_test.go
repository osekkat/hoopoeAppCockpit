package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
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
