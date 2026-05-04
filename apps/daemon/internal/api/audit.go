package api

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/hoopoe-cockpit/hoopoe/apps/daemon/internal/approvals"
	"github.com/hoopoe-cockpit/hoopoe/apps/daemon/internal/audit"
	"github.com/hoopoe-cockpit/hoopoe/apps/daemon/internal/redaction"
	schemas "github.com/hoopoe-cockpit/hoopoe/packages/schemas/go"
)

const (
	maxAuditQueryLimit                 = 1000
	maxAuditSearchQueryLen             = 256
	auditExportApprovalActionKind      = "audit.export"
	auditExportApprovalHeader          = "X-Hoopoe-Approval-Id"
	auditExportApprovalMaxAge          = 2 * time.Minute
	auditHTTPStatusOK                  = 200
	auditHTTPStatusUnauthorized        = 401
	auditHTTPStatusBadRequest          = 400
	auditHTTPStatusUnprocessableEntity = 422
	auditHTTPStatusServiceUnavailable  = 503
	auditHTTPStatusInternalServerError = 500
)

type auditAppender interface {
	Append(entry audit.Entry) (audit.Entry, []redaction.TraceEvent, error)
}

type auditQueryResponse struct {
	SchemaVersion int                  `json:"schemaVersion"`
	Items         []auditEntryResponse `json:"items"`
	Page          schemas.PageMeta     `json:"page"`
	Facets        auditFacetResponse   `json:"facets"`
}

type auditExportRequest struct {
	ProjectID     string `json:"projectId,omitempty"`
	ActorKind     string `json:"actorKind,omitempty"`
	ActorID       string `json:"actorId,omitempty"`
	Action        string `json:"action,omitempty"`
	Outcome       string `json:"outcome,omitempty"`
	CorrelationID string `json:"correlationId,omitempty"`
	CausationID   string `json:"causationId,omitempty"`
	Query         string `json:"q,omitempty"`
	From          string `json:"from,omitempty"`
	To            string `json:"to,omitempty"`
}

type auditExportResponse struct {
	SchemaVersion int                  `json:"schemaVersion"`
	FileName      string               `json:"fileName"`
	Sha256        string               `json:"sha256"`
	TotalEntries  int                  `json:"totalEntries"`
	Redacted      bool                 `json:"redacted"`
	ExportedAt    time.Time            `json:"exportedAt"`
	ApprovalID    *string              `json:"approvalId,omitempty"`
	Items         []auditEntryResponse `json:"items"`
}

type auditEntryResponse struct {
	SchemaVersion   int                   `json:"schemaVersion"`
	EventID         string                `json:"eventId"`
	Seq             uint64                `json:"seq"`
	Time            time.Time             `json:"time"`
	ProjectID       string                `json:"projectId"`
	Actor           audit.Actor           `json:"actor"`
	Action          string                `json:"action"`
	Reason          string                `json:"reason,omitempty"`
	CommandPreview  string                `json:"commandPreview,omitempty"`
	Result          audit.Result          `json:"result,omitempty"`
	ArtifactRefs    []audit.ArtifactRef   `json:"artifactRefs,omitempty"`
	CorrelationID   string                `json:"correlationId,omitempty"`
	CausationID     string                `json:"causationId,omitempty"`
	ApprovalID      string                `json:"approvalId,omitempty"`
	Data            map[string]any        `json:"data,omitempty"`
	Summary         string                `json:"summary"`
	Severity        string                `json:"severity"`
	Category        string                `json:"category"`
	LinkedArtifacts []auditLinkedArtifact `json:"linkedArtifacts,omitempty"`
}

type auditLinkedArtifact struct {
	Kind     string `json:"kind"`
	ID       string `json:"id,omitempty"`
	URI      string `json:"uri,omitempty"`
	Resolved bool   `json:"resolved"`
}

type auditFacetResponse struct {
	ActorKinds []auditFacetCount `json:"actorKinds"`
	Actions    []auditFacetCount `json:"actions"`
	Outcomes   []auditFacetCount `json:"outcomes"`
	Projects   []auditFacetCount `json:"projects"`
}

type auditFacetCount struct {
	Value string `json:"value"`
	Count int    `json:"count"`
}

func (s *server) mountAuditRoutes(r chi.Router) {
	r.Get("/v1/audit/query", s.handleAuditQuery)
	r.Post("/v1/audit/export", s.handleAuditExport)
}

func (s *server) handleAuditQuery(w http.ResponseWriter, r *http.Request) {
	query, ok := s.auditQueryFromRequest(w, r)
	if !ok {
		return
	}
	entries, err := s.queryAudit(query)
	if err != nil {
		s.writeProblemCode(w, auditHTTPStatusInternalServerError, "audit.query_failed", "audit query failed", err.Error())
		return
	}
	items := auditEntryResponses(entries)
	writeJSON(w, auditHTTPStatusOK, auditQueryResponse{
		SchemaVersion: schemaVersion,
		Items:         items,
		Page:          pageMeta(len(items)),
		Facets:        auditFacets(items),
	})
}

func (s *server) handleAuditExport(w http.ResponseWriter, r *http.Request) {
	var request auditExportRequest
	if err := decodeOptionalJSON(r, &request); err != nil {
		s.writeProblemCode(w, auditHTTPStatusBadRequest, "request.invalid_json", "invalid request body", err.Error())
		return
	}
	query, err := auditQueryFromExportRequest(request)
	if err != nil {
		s.writeProblemCode(w, auditHTTPStatusBadRequest, "audit.invalid_export_filter", "invalid audit export filter", err.Error())
		return
	}
	approvalRef := ""
	if requiresAuditExportApproval(query) {
		approvalRef = firstAuditHeaderValue(r.Header, auditExportApprovalHeader)
		if approvalRef == "" {
			s.writeProblemCode(w, auditHTTPStatusUnprocessableEntity, "audit.export_requires_approval", "audit export requires approval", "exports wider than seven days require "+auditExportApprovalHeader)
			return
		}
		parsedApprovalRef, err := parseAuditApprovalID(approvalRef)
		if err != nil {
			writeAuditExportApprovalProblem(s, w, auditExportApprovalError{
				status: auditHTTPStatusUnauthorized,
				code:   "audit.export_approval_invalid",
				title:  "approval invalid",
				detail: err.Error(),
			})
			return
		}
		approvalRef = parsedApprovalRef
		if err := s.validateAuditExportApproval(r.Context(), approvalRef, query); err != nil {
			writeAuditExportApprovalProblem(s, w, err)
			return
		}
		if err := s.consumeAuditExportApproval(r.Context(), approvalRef); err != nil {
			writeAuditExportApprovalProblem(s, w, err)
			return
		}
	}
	_ = s.appendAudit("audit.export_started", audit.ResultSuccess, request.ProjectID, approvalRef, map[string]any{
		"correlationId": request.CorrelationID,
		"actorKind":     request.ActorKind,
	})
	entries, err := s.queryAudit(query)
	if err != nil {
		_ = s.appendAudit("audit.export_failed", audit.ResultFailure, request.ProjectID, approvalRef, map[string]any{"error": err.Error()})
		s.writeProblemCode(w, auditHTTPStatusInternalServerError, "audit.export_failed", "audit export failed", err.Error())
		return
	}
	items := auditEntryResponses(entries)
	exportedAt := s.now().UTC()
	body, err := json.Marshal(items)
	if err != nil {
		s.writeProblemCode(w, auditHTTPStatusInternalServerError, "audit.export_encode_failed", "audit export encode failed", err.Error())
		return
	}
	sum := sha256.Sum256(body)
	fileName := fmt.Sprintf("audit-slice-%s.json", exportedAt.Format("20060102T150405Z"))
	_ = s.appendAudit("audit.export_completed", audit.ResultSuccess, request.ProjectID, approvalRef, map[string]any{
		"fileName":     fileName,
		"sha256":       hex.EncodeToString(sum[:]),
		"totalEntries": len(items),
	})
	var approval *string
	if approvalRef != "" {
		approval = &approvalRef
	}
	writeJSON(w, auditHTTPStatusOK, auditExportResponse{
		SchemaVersion: schemaVersion,
		FileName:      fileName,
		Sha256:        hex.EncodeToString(sum[:]),
		TotalEntries:  len(items),
		Redacted:      true,
		ExportedAt:    exportedAt,
		ApprovalID:    approval,
		Items:         items,
	})
}

type auditExportApprovalError struct {
	status int
	code   string
	title  string
	detail string
}

func (e auditExportApprovalError) Error() string { return e.detail }

func (s *server) validateAuditExportApproval(ctx context.Context, approvalRef string, query audit.Query) error {
	if s.approvals == nil {
		return auditExportApprovalError{
			status: auditHTTPStatusServiceUnavailable,
			code:   "audit.export_approvals_unavailable",
			title:  "approval queue unavailable",
			detail: "audit export requires the unified approvals queue",
		}
	}
	approval, ok, err := (ApprovalQueueLookup{Queue: s.approvals}).LookupApproval(ctx, approvalRef)
	if err != nil {
		return auditExportApprovalError{
			status: auditHTTPStatusInternalServerError,
			code:   "audit.export_approval_lookup_failed",
			title:  "approval lookup failed",
			detail: err.Error(),
		}
	}
	if !ok {
		return auditExportApprovalError{
			status: auditHTTPStatusUnauthorized,
			code:   "audit.export_approval_not_found",
			title:  "approval not found",
			detail: "approval id does not exist",
		}
	}
	if approval.State == schemas.Revoked {
		return auditExportApprovalError{
			status: auditHTTPStatusUnauthorized,
			code:   "audit.export_approval_consumed",
			title:  "approval already used",
			detail: "audit export approval must still be approved and unused",
		}
	}
	if approval.State != schemas.Approved {
		return auditExportApprovalError{
			status: auditHTTPStatusUnauthorized,
			code:   "audit.export_approval_not_approved",
			title:  "approval is not approved",
			detail: "approval must be approved before exporting audit data",
		}
	}
	if approval.RiskClass != schemas.Critical || approval.Scope != schemas.Once {
		return auditExportApprovalError{
			status: auditHTTPStatusUnauthorized,
			code:   "audit.export_approval_scope_invalid",
			title:  "approval scope invalid",
			detail: "approval must be riskClass=critical and scope=once",
		}
	}
	if approval.RequestedAction.Kind != auditExportApprovalActionKind {
		return auditExportApprovalError{
			status: auditHTTPStatusUnauthorized,
			code:   "audit.export_approval_action_invalid",
			title:  "approval action invalid",
			detail: "approval must cover audit.export",
		}
	}
	if approval.ProjectId != nil {
		approvedProject := strings.TrimSpace(*approval.ProjectId)
		if approvedProject != "" && approvedProject != strings.TrimSpace(query.ProjectID) {
			return auditExportApprovalError{
				status: auditHTTPStatusUnauthorized,
				code:   "audit.export_approval_project_invalid",
				title:  "approval project invalid",
				detail: "approval does not cover the requested project",
			}
		}
	}

	now := s.now().UTC()
	freshFrom := approval.RequestedAt
	if approval.DecidedAt != nil {
		freshFrom = *approval.DecidedAt
	}
	if now.Sub(freshFrom.UTC()) > auditExportApprovalMaxAge {
		return auditExportApprovalError{
			status: auditHTTPStatusUnauthorized,
			code:   "audit.export_approval_expired",
			title:  "approval too old",
			detail: "audit export approval must be fresh within 2 minutes",
		}
	}
	return nil
}

func (s *server) consumeAuditExportApproval(ctx context.Context, approvalRef string) error {
	if s.approvals == nil {
		return auditExportApprovalError{
			status: auditHTTPStatusServiceUnavailable,
			code:   "audit.export_approvals_unavailable",
			title:  "approval queue unavailable",
			detail: "audit export requires the unified approvals queue",
		}
	}
	actorID := "daemon.api"
	note := "consumed by audit.export"
	if _, err := (ApprovalQueueLookup{Queue: s.approvals}).ConsumeOnceApproval(ctx, approvalRef, schemas.ApprovalDecisionRequest{
		DecisionActor: schemas.Actor{Kind: schemas.ActorKindSystem, Id: &actorID},
		Note:          &note,
	}); err != nil {
		switch {
		case errors.Is(err, approvals.ErrNotFound):
			return auditExportApprovalError{
				status: auditHTTPStatusUnauthorized,
				code:   "audit.export_approval_not_found",
				title:  "approval not found",
				detail: "approval id does not exist",
			}
		case errors.Is(err, approvals.ErrExpired):
			return auditExportApprovalError{
				status: auditHTTPStatusUnauthorized,
				code:   "audit.export_approval_expired",
				title:  "approval too old",
				detail: "audit export approval must be fresh within 2 minutes",
			}
		case errors.Is(err, approvals.ErrInvalidTransition):
			return auditExportApprovalError{
				status: auditHTTPStatusUnauthorized,
				code:   "audit.export_approval_consumed",
				title:  "approval already used",
				detail: "audit export approval must still be approved and unused",
			}
		default:
			return auditExportApprovalError{
				status: auditHTTPStatusInternalServerError,
				code:   "audit.export_approval_consume_failed",
				title:  "approval consume failed",
				detail: err.Error(),
			}
		}
	}
	return nil
}

func writeAuditExportApprovalProblem(s *server, w http.ResponseWriter, err error) {
	var approvalErr auditExportApprovalError
	if errors.As(err, &approvalErr) {
		s.writeProblemCode(w, approvalErr.status, approvalErr.code, approvalErr.title, approvalErr.detail)
		return
	}
	s.writeProblemCode(w, auditHTTPStatusInternalServerError, "audit.export_approval_error", "approval check failed", err.Error())
}

func (s *server) auditQueryFromRequest(w http.ResponseWriter, r *http.Request) (audit.Query, bool) {
	values := r.URL.Query()
	correlationID, err := parseAuditLookupToken(values.Get("correlationId"), "correlationId")
	if err != nil {
		s.writeProblemCode(w, auditHTTPStatusBadRequest, "audit.invalid_correlation", "invalid correlation id", err.Error())
		return audit.Query{}, false
	}
	causationID, err := parseAuditLookupToken(values.Get("causationId"), "causationId")
	if err != nil {
		s.writeProblemCode(w, auditHTTPStatusBadRequest, "audit.invalid_causation", "invalid causation id", err.Error())
		return audit.Query{}, false
	}
	limit, err := parseAuditLimit(values.Get("limit"))
	if err != nil {
		s.writeProblemCode(w, auditHTTPStatusBadRequest, "audit.invalid_limit", "invalid audit limit", err.Error())
		return audit.Query{}, false
	}
	from, err := parseOptionalAuditTime(values.Get("from"))
	if err != nil {
		s.writeProblemCode(w, auditHTTPStatusBadRequest, "audit.invalid_from", "invalid from timestamp", err.Error())
		return audit.Query{}, false
	}
	to, err := parseOptionalAuditTime(values.Get("to"))
	if err != nil {
		s.writeProblemCode(w, auditHTTPStatusBadRequest, "audit.invalid_to", "invalid to timestamp", err.Error())
		return audit.Query{}, false
	}
	search, err := parseAuditSearch(values.Get("q"))
	if err != nil {
		s.writeProblemCode(w, auditHTTPStatusBadRequest, "audit.invalid_search", "invalid audit search query", err.Error())
		return audit.Query{}, false
	}
	return audit.Query{
		ProjectID:     strings.TrimSpace(values.Get("projectId")),
		ActorKind:     audit.ActorKind(strings.TrimSpace(values.Get("actorKind"))),
		ActorID:       strings.TrimSpace(values.Get("actorId")),
		Action:        strings.TrimSpace(values.Get("action")),
		Result:        audit.Result(strings.TrimSpace(values.Get("outcome"))),
		CorrelationID: correlationID,
		CausationID:   causationID,
		Search:        search,
		From:          from,
		To:            to,
		Limit:         limit,
		Reverse:       true,
	}, true
}

func auditQueryFromExportRequest(request auditExportRequest) (audit.Query, error) {
	correlationID, err := parseAuditLookupToken(request.CorrelationID, "correlationId")
	if err != nil {
		return audit.Query{}, err
	}
	causationID, err := parseAuditLookupToken(request.CausationID, "causationId")
	if err != nil {
		return audit.Query{}, err
	}
	from, err := parseOptionalAuditTime(request.From)
	if err != nil {
		return audit.Query{}, err
	}
	to, err := parseOptionalAuditTime(request.To)
	if err != nil {
		return audit.Query{}, err
	}
	search, err := parseAuditSearch(request.Query)
	if err != nil {
		return audit.Query{}, err
	}
	return audit.Query{
		ProjectID:     strings.TrimSpace(request.ProjectID),
		ActorKind:     audit.ActorKind(strings.TrimSpace(request.ActorKind)),
		ActorID:       strings.TrimSpace(request.ActorID),
		Action:        strings.TrimSpace(request.Action),
		Result:        audit.Result(strings.TrimSpace(request.Outcome)),
		CorrelationID: correlationID,
		CausationID:   causationID,
		Search:        search,
		From:          from,
		To:            to,
		Limit:         maxAuditQueryLimit,
		Reverse:       false,
	}, nil
}

func (s *server) queryAudit(query audit.Query) ([]audit.Entry, error) {
	if s.auditLog == nil {
		return nil, nil
	}
	return s.auditLog.Query(query)
}

func (s *server) appendAudit(action string, result audit.Result, projectID string, approvalID string, data map[string]any) error {
	appender, ok := s.auditLog.(auditAppender)
	if !ok {
		return nil
	}
	entry := audit.Entry{
		ProjectID:  projectID,
		Action:     action,
		Actor:      audit.Actor{Kind: audit.ActorSystem, ID: "daemon.api"},
		Result:     result,
		ApprovalID: approvalID,
		Data:       data,
	}
	_, _, err := appender.Append(entry)
	return err
}

func parseAuditLimit(value string) (int, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return 500, nil
	}
	limit, err := strconv.Atoi(value)
	if err != nil {
		return 0, err
	}
	if limit < 1 || limit > maxAuditQueryLimit {
		return 0, fmt.Errorf("limit must be between 1 and %d", maxAuditQueryLimit)
	}
	return limit, nil
}

func parseOptionalAuditTime(value string) (time.Time, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return time.Time{}, nil
	}
	parsed, err := time.Parse(time.RFC3339, value)
	if err == nil {
		return parsed.UTC(), nil
	}
	parsed, err = time.Parse(time.RFC3339Nano, value)
	if err != nil {
		return time.Time{}, err
	}
	return parsed.UTC(), nil
}

func firstAuditHeaderValue(header http.Header, name string) string {
	values := header.Values(name)
	if len(values) == 0 {
		return ""
	}
	return strings.TrimSpace(values[0])
}

func parseAuditApprovalID(value string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return "", nil
	}
	if len(value) > 160 {
		return "", fmt.Errorf("approvalId is too long")
	}
	for _, r := range value {
		if isAuditLookupRune(r) {
			continue
		}
		return "", fmt.Errorf("approvalId contains unsupported character %q", r)
	}
	return value, nil
}

func parseAuditLookupToken(value string, field string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return "", nil
	}
	if len(value) > 160 {
		return "", fmt.Errorf("%s is too long", field)
	}
	for _, r := range value {
		if isAuditLookupRune(r) {
			continue
		}
		return "", fmt.Errorf("%s contains unsupported character %q", field, r)
	}
	return value, nil
}

// parseAuditSearch validates the free-form `q` search string. Unlike
// correlationId/causationId, q is user-typed and may contain arbitrary
// printable characters, so charset is not restricted — only length is
// bounded to keep entryMatchesSearch's substring scan O(small) per
// candidate field.
func parseAuditSearch(value string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return "", nil
	}
	if len(value) > maxAuditSearchQueryLen {
		return "", fmt.Errorf("q is too long (max %d bytes)", maxAuditSearchQueryLen)
	}
	return value, nil
}

func isAuditLookupRune(r rune) bool {
	return r >= 'a' && r <= 'z' ||
		r >= 'A' && r <= 'Z' ||
		r >= '0' && r <= '9' ||
		r == '-' ||
		r == '_' ||
		r == '.' ||
		r == ':' ||
		r == '/'
}

func requiresAuditExportApproval(query audit.Query) bool {
	if isNarrowAuditExportLookup(query) {
		return false
	}
	if query.From.IsZero() || query.To.IsZero() {
		return true
	}
	return query.To.Sub(query.From) > 7*24*time.Hour
}

func isNarrowAuditExportLookup(query audit.Query) bool {
	return strings.TrimSpace(query.CorrelationID) != "" || strings.TrimSpace(query.CausationID) != ""
}

func auditEntryResponses(entries []audit.Entry) []auditEntryResponse {
	items := make([]auditEntryResponse, 0, len(entries))
	for _, entry := range entries {
		items = append(items, auditEntryResponseFor(entry))
	}
	return items
}

func auditEntryResponseFor(entry audit.Entry) auditEntryResponse {
	linked := make([]auditLinkedArtifact, 0, len(entry.ArtifactRefs))
	for _, ref := range entry.ArtifactRefs {
		linked = append(linked, auditLinkedArtifact{
			Kind:     ref.Kind,
			ID:       ref.ID,
			URI:      ref.URI,
			Resolved: ref.ID != "" || ref.URI != "",
		})
	}
	return auditEntryResponse{
		SchemaVersion:   entry.SchemaVersion,
		EventID:         entry.EventID,
		Seq:             entry.Seq,
		Time:            entry.Time,
		ProjectID:       entry.ProjectID,
		Actor:           entry.Actor,
		Action:          entry.Action,
		Reason:          entry.Reason,
		CommandPreview:  entry.CommandPreview,
		Result:          entry.Result,
		ArtifactRefs:    entry.ArtifactRefs,
		CorrelationID:   entry.CorrelationID,
		CausationID:     entry.CausationID,
		ApprovalID:      entry.ApprovalID,
		Data:            entry.Data,
		Summary:         auditSummary(entry),
		Severity:        auditSeverity(entry),
		Category:        auditCategory(entry.Action),
		LinkedArtifacts: linked,
	}
}

func auditSummary(entry audit.Entry) string {
	if strings.TrimSpace(entry.Reason) != "" {
		return entry.Reason
	}
	if entry.ApprovalID != "" {
		return fmt.Sprintf("%s (%s)", entry.Action, entry.ApprovalID)
	}
	return entry.Action
}

func auditSeverity(entry audit.Entry) string {
	switch entry.Result {
	case audit.ResultFailure:
		return "urgent"
	case audit.ResultApprovalRequired:
		return "warning"
	case audit.ResultPartial:
		return "notice"
	default:
		if strings.Contains(entry.Action, "denied") || strings.Contains(entry.Action, "failed") {
			return "warning"
		}
		return "info"
	}
}

func auditCategory(action string) string {
	switch {
	case strings.HasPrefix(action, "auth."):
		return "auth"
	case strings.HasPrefix(action, "bead."):
		return "beads"
	case strings.HasPrefix(action, "mail."):
		return "mail"
	case strings.HasPrefix(action, "approval."):
		return "approval"
	case strings.HasPrefix(action, "health."):
		return "health"
	case strings.HasPrefix(action, "review."):
		return "review"
	case strings.HasPrefix(action, "tending."):
		return "tending"
	case strings.HasPrefix(action, "repair."):
		return "repair"
	case strings.HasPrefix(action, "audit."):
		return "audit"
	case strings.HasPrefix(action, "project."):
		return "project"
	case strings.HasPrefix(action, "plan."):
		return "plan"
	case strings.HasPrefix(action, "swarm."):
		return "swarm"
	default:
		return "config"
	}
}

func auditFacets(items []auditEntryResponse) auditFacetResponse {
	return auditFacetResponse{
		ActorKinds: facetCounts(items, func(item auditEntryResponse) string { return string(item.Actor.Kind) }),
		Actions:    facetCounts(items, func(item auditEntryResponse) string { return item.Action }),
		Outcomes:   facetCounts(items, func(item auditEntryResponse) string { return string(item.Result) }),
		Projects:   facetCounts(items, func(item auditEntryResponse) string { return item.ProjectID }),
	}
}

func facetCounts(items []auditEntryResponse, value func(auditEntryResponse) string) []auditFacetCount {
	counts := make(map[string]int)
	for _, item := range items {
		key := strings.TrimSpace(value(item))
		if key == "" {
			key = "unknown"
		}
		counts[key]++
	}
	out := make([]auditFacetCount, 0, len(counts))
	for key, count := range counts {
		out = append(out, auditFacetCount{Value: key, Count: count})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Count == out[j].Count {
			return out[i].Value < out[j].Value
		}
		return out[i].Count > out[j].Count
	})
	return out
}
