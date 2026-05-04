package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/hoopoe-cockpit/hoopoe/apps/daemon/internal/audit"
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
