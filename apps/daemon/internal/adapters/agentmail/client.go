package agentmail

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync/atomic"
	"time"
)

const (
	defaultEndpoint         = "/mcp"
	defaultMaxResponseBytes = int64(4 << 20)
)

type HTTPDoer interface {
	Do(*http.Request) (*http.Response, error)
}

type Client struct {
	BaseURL          string
	Endpoint         string
	Token            string
	HTTPClient       HTTPDoer
	Audit            AuditSink
	Now              func() time.Time
	MaxResponseBytes int64

	nextID atomic.Uint64
}

func New(baseURL string) *Client {
	return &Client{
		BaseURL:          baseURL,
		Endpoint:         defaultEndpoint,
		HTTPClient:       http.DefaultClient,
		Now:              time.Now,
		MaxResponseBytes: defaultMaxResponseBytes,
	}
}

func (c *Client) SendMessage(ctx context.Context, req SendMessageRequest) (SendMessageResponse, error) {
	if err := validateSend(req); err != nil {
		return SendMessageResponse{}, err
	}
	args := map[string]any{
		"project_key":  req.ProjectKey,
		"sender_name":  req.SenderName,
		"to":           req.To,
		"subject":      strings.TrimSpace(req.Subject),
		"body_md":      req.BodyMD,
		"importance":   defaultString(req.Importance, "normal"),
		"ack_required": req.AckRequired,
		"broadcast":    req.Broadcast,
	}
	appendStrings(args, "cc", req.CC)
	appendStrings(args, "bcc", req.BCC)
	appendStrings(args, "attachment_paths", req.AttachmentPaths)
	appendNonEmpty(args, "thread_id", req.ThreadID)
	appendNonEmpty(args, "topic", req.Topic)
	if req.ConvertImages != nil {
		args["convert_images"] = *req.ConvertImages
	}
	var out SendMessageResponse
	if err := c.callTool(ctx, "send_message", args, &out); err != nil {
		return out, err
	}
	return out, nil
}

func (c *Client) SendBeadMessage(ctx context.Context, beadID string, req SendMessageRequest) (SendMessageResponse, error) {
	threadID, err := BeadThreadID(beadID)
	if err != nil {
		return SendMessageResponse{}, err
	}
	req.ThreadID = threadID
	return c.SendMessage(ctx, req)
}

func (c *Client) FetchInbox(ctx context.Context, req FetchInboxRequest) ([]Message, error) {
	if strings.TrimSpace(req.ProjectKey) == "" || strings.TrimSpace(req.AgentName) == "" {
		return nil, fmt.Errorf("%w: project_key and agent_name are required", ErrInvalidRequest)
	}
	args := map[string]any{
		"project_key":    req.ProjectKey,
		"agent_name":     req.AgentName,
		"limit":          positiveOrDefault(req.Limit, 20),
		"urgent_only":    req.UrgentOnly,
		"include_bodies": req.IncludeBodies,
	}
	appendNonEmpty(args, "since_ts", req.SinceTS)
	appendNonEmpty(args, "topic", req.Topic)
	var out []Message
	if err := c.callTool(ctx, "fetch_inbox", args, &out); err != nil {
		return nil, err
	}
	return out, nil
}

func (c *Client) SearchMessages(ctx context.Context, req SearchMessagesRequest) ([]Message, error) {
	if strings.TrimSpace(req.ProjectKey) == "" || strings.TrimSpace(req.Query) == "" {
		return nil, fmt.Errorf("%w: project_key and query are required", ErrInvalidRequest)
	}
	args := map[string]any{
		"project_key": req.ProjectKey,
		"query":       req.Query,
		"limit":       positiveOrDefault(req.Limit, 20),
	}
	var out []Message
	if err := c.callTool(ctx, "search_messages", args, &out); err != nil {
		return nil, err
	}
	return out, nil
}

func (c *Client) ListThreads(ctx context.Context, req ListThreadsRequest) ([]Thread, error) {
	query := strings.TrimSpace(req.Query)
	if query == "" {
		return nil, fmt.Errorf("%w: query is required for thread listing", ErrInvalidRequest)
	}
	messages, err := c.SearchMessages(ctx, SearchMessagesRequest{
		ProjectKey: req.ProjectKey,
		Query:      query,
		Limit:      positiveOrDefault(req.Limit, 50),
	})
	if err != nil {
		return nil, err
	}
	threads := make([]Thread, 0, len(messages))
	seen := make(map[string]int)
	for _, msg := range messages {
		threadID := strings.TrimSpace(msg.ThreadID)
		if threadID == "" {
			threadID = fmt.Sprintf("message-%d", msg.ID)
		}
		idx, ok := seen[threadID]
		if !ok {
			seen[threadID] = len(threads)
			threads = append(threads, Thread{
				ID:            threadID,
				Subject:       msg.Subject,
				LastMessageID: msg.ID,
				LastMessageAt: msg.CreatedTS,
				Participants:  []string{msg.From},
			})
			continue
		}
		threads[idx].Participants = appendUnique(threads[idx].Participants, msg.From)
	}
	return threads, nil
}

func (c *Client) SummarizeThread(ctx context.Context, req ThreadSummaryRequest) (ThreadSummary, error) {
	if strings.TrimSpace(req.ProjectKey) == "" || strings.TrimSpace(req.ThreadID) == "" {
		return ThreadSummary{}, fmt.Errorf("%w: project_key and thread_id are required", ErrInvalidRequest)
	}
	args := map[string]any{
		"project_key":      req.ProjectKey,
		"thread_id":        req.ThreadID,
		"include_examples": req.IncludeExamples,
		"llm_mode":         req.LLMMode,
		"per_thread_limit": positiveOrDefault(req.PerThreadLimit, 50),
	}
	appendNonEmpty(args, "llm_model", req.LLMModel)
	var out ThreadSummary
	if err := c.callTool(ctx, "summarize_thread", args, &out); err != nil {
		return out, err
	}
	return out, nil
}

func (c *Client) ListReservations(ctx context.Context, req ListReservationsRequest) ([]Reservation, error) {
	project := strings.TrimSpace(req.Project)
	if project == "" {
		return nil, fmt.Errorf("%w: project is required", ErrInvalidRequest)
	}
	path := "/mail/api/projects/" + url.PathEscape(project) + "/file-reservations"
	query := url.Values{}
	query.Set("active_only", fmt.Sprintf("%t", req.ActiveOnly))
	if req.Limit > 0 {
		query.Set("limit", fmt.Sprintf("%d", req.Limit))
	}
	var out struct {
		Reservations []Reservation `json:"reservations"`
	}
	if err := c.getJSON(ctx, path, query, &out); err != nil {
		var direct []Reservation
		if secondErr := c.getJSON(ctx, path, query, &direct); secondErr == nil {
			return direct, nil
		}
		return nil, err
	}
	return out.Reservations, nil
}

func (c *Client) ReservePaths(ctx context.Context, req ReservePathsRequest) (ReservePathsResponse, error) {
	if strings.TrimSpace(req.ProjectKey) == "" || strings.TrimSpace(req.AgentName) == "" || len(req.Paths) == 0 {
		return ReservePathsResponse{}, fmt.Errorf("%w: project_key, agent_name, and paths are required", ErrInvalidRequest)
	}
	args := map[string]any{
		"project_key": req.ProjectKey,
		"agent_name":  req.AgentName,
		"paths":       req.Paths,
		"ttl_seconds": positiveOrDefault(req.TTLSeconds, 3600),
		"exclusive":   req.Exclusive,
		"reason":      req.Reason,
	}
	var out ReservePathsResponse
	if err := c.callTool(ctx, "file_reservation_paths", args, &out); err != nil {
		return out, err
	}
	return out, nil
}

func (c *Client) ReleaseReservations(ctx context.Context, req ReleaseReservationsRequest) (ReleaseReservationsResponse, error) {
	if strings.TrimSpace(req.ProjectKey) == "" || strings.TrimSpace(req.AgentName) == "" {
		return ReleaseReservationsResponse{}, fmt.Errorf("%w: project_key and agent_name are required", ErrInvalidRequest)
	}
	args := map[string]any{
		"project_key": req.ProjectKey,
		"agent_name":  req.AgentName,
	}
	appendStrings(args, "paths", req.Paths)
	if len(req.FileReservationIDs) > 0 {
		args["file_reservation_ids"] = req.FileReservationIDs
	}
	var out ReleaseReservationsResponse
	if err := c.callTool(ctx, "release_file_reservations", args, &out); err != nil {
		return out, err
	}
	return out, nil
}

func (c *Client) ForceReleaseReservation(ctx context.Context, req ForceReleaseReservationRequest) (ForceReleaseReservationResponse, error) {
	if strings.TrimSpace(req.ProjectKey) == "" || strings.TrimSpace(req.AgentName) == "" || req.FileReservationID <= 0 {
		return ForceReleaseReservationResponse{}, fmt.Errorf("%w: project_key, agent_name, and file_reservation_id are required", ErrInvalidRequest)
	}
	if strings.TrimSpace(req.Note) == "" {
		return ForceReleaseReservationResponse{}, fmt.Errorf("%w: force release requires an operator note", ErrInvalidRequest)
	}
	start := c.nowUTC()
	if err := c.recordAudit(ctx, AuditEvent{
		Action:        "agent_mail.reservation.force_release.requested",
		ProjectKey:    req.ProjectKey,
		AgentName:     req.AgentName,
		ReservationID: req.FileReservationID,
		Reason:        req.Note,
		Result:        "started",
		At:            start,
	}); err != nil {
		return ForceReleaseReservationResponse{}, err
	}
	args := map[string]any{
		"project_key":         req.ProjectKey,
		"agent_name":          req.AgentName,
		"file_reservation_id": req.FileReservationID,
		"notify_previous":     true,
		"note":                req.Note,
	}
	var out ForceReleaseReservationResponse
	err := c.callTool(ctx, "force_release_file_reservation", args, &out)
	event := AuditEvent{
		Action:        "agent_mail.reservation.force_release.completed",
		ProjectKey:    req.ProjectKey,
		AgentName:     req.AgentName,
		ReservationID: req.FileReservationID,
		Reason:        req.Note,
		Result:        "success",
		At:            c.nowUTC(),
	}
	if err != nil {
		event.Result = "failure"
		event.Error = err.Error()
	}
	if auditErr := c.recordAudit(ctx, event); auditErr != nil && err == nil {
		err = auditErr
	}
	return out, err
}

func (c *Client) getJSON(ctx context.Context, path string, query url.Values, out any) error {
	if err := c.validateTransport(); err != nil {
		return err
	}
	base, err := url.Parse(c.BaseURL)
	if err != nil {
		return fmt.Errorf("%w: bad base URL: %v", ErrInvalidRequest, err)
	}
	if base.Scheme == "" || base.Host == "" {
		return fmt.Errorf("%w: base URL must include scheme and host", ErrInvalidRequest)
	}
	base.Path = strings.TrimRight(base.Path, "/") + path
	base.RawQuery = query.Encode()
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, base.String(), nil)
	if err != nil {
		return err
	}
	httpReq.Header.Set("Accept", "application/json")
	if c.Token != "" {
		httpReq.Header.Set("Authorization", "Bearer "+c.Token)
	}
	resp, err := c.HTTPClient.Do(httpReq)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	limit := c.MaxResponseBytes
	if limit <= 0 {
		limit = defaultMaxResponseBytes
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, limit+1))
	if err != nil {
		return err
	}
	if int64(len(data)) > limit {
		return fmt.Errorf("%w: response exceeded %d bytes", ErrDecode, limit)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("%w: status=%d body=%s", ErrHTTPStatus, resp.StatusCode, truncate(string(data), 512))
	}
	if err := json.Unmarshal(data, out); err != nil {
		return fmt.Errorf("%w: http json: %v", ErrDecode, err)
	}
	return nil
}

func (c *Client) callTool(ctx context.Context, name string, args map[string]any, out any) error {
	if err := c.validateTransport(); err != nil {
		return err
	}
	reqBody := mcpRequest{
		JSONRPC: "2.0",
		ID:      c.nextRequestID(),
		Method:  "tools/call",
		Params: mcpToolCallParams{
			Name:      name,
			Arguments: args,
		},
	}
	body, err := json.Marshal(reqBody)
	if err != nil {
		return fmt.Errorf("%w: encode %s request: %v", ErrInvalidRequest, name, err)
	}
	endpoint, err := c.endpointURL()
	if err != nil {
		return err
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "application/json")
	if c.Token != "" {
		httpReq.Header.Set("Authorization", "Bearer "+c.Token)
	}
	resp, err := c.HTTPClient.Do(httpReq)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	limit := c.MaxResponseBytes
	if limit <= 0 {
		limit = defaultMaxResponseBytes
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, limit+1))
	if err != nil {
		return err
	}
	if int64(len(data)) > limit {
		return fmt.Errorf("%w: response exceeded %d bytes", ErrDecode, limit)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("%w: status=%d body=%s", ErrHTTPStatus, resp.StatusCode, truncate(string(data), 512))
	}
	var envelope mcpResponse
	if err := json.Unmarshal(data, &envelope); err != nil {
		return fmt.Errorf("%w: mcp envelope: %v", ErrDecode, err)
	}
	if envelope.Error != nil {
		return fmt.Errorf("%w: %s", ErrMCPError, envelope.Error.Message)
	}
	if out == nil {
		return nil
	}
	payload := envelope.Result.StructuredContent
	if len(payload) == 0 || string(payload) == "null" {
		payload = envelope.Result.Raw
	}
	if len(payload) == 0 || string(payload) == "null" {
		payload = data
	}
	if nested := nestedResult(payload); len(nested) > 0 {
		payload = nested
	}
	if err := json.Unmarshal(payload, out); err != nil {
		return fmt.Errorf("%w: %s result: %v", ErrDecode, name, err)
	}
	return nil
}

func (c *Client) validateTransport() error {
	if strings.TrimSpace(c.BaseURL) == "" {
		return fmt.Errorf("%w: empty base URL", ErrInvalidRequest)
	}
	if c.HTTPClient == nil {
		c.HTTPClient = http.DefaultClient
	}
	if c.Endpoint == "" {
		c.Endpoint = defaultEndpoint
	}
	return nil
}

func (c *Client) endpointURL() (string, error) {
	base, err := url.Parse(c.BaseURL)
	if err != nil {
		return "", fmt.Errorf("%w: bad base URL: %v", ErrInvalidRequest, err)
	}
	if base.Scheme == "" || base.Host == "" {
		return "", fmt.Errorf("%w: base URL must include scheme and host", ErrInvalidRequest)
	}
	endpoint := c.Endpoint
	if endpoint == "" {
		endpoint = defaultEndpoint
	}
	if !strings.HasPrefix(endpoint, "/") {
		endpoint = "/" + endpoint
	}
	base.Path = strings.TrimRight(base.Path, "/") + endpoint
	return base.String(), nil
}

func (c *Client) nextRequestID() string {
	return fmt.Sprintf("hoopoe-agentmail-%d", c.nextID.Add(1))
}

func (c *Client) nowUTC() time.Time {
	now := c.Now
	if now == nil {
		now = time.Now
	}
	return now().UTC()
}

func (c *Client) recordAudit(ctx context.Context, event AuditEvent) error {
	if c.Audit == nil {
		return nil
	}
	return c.Audit.RecordAgentMailAction(ctx, event)
}

func validateSend(req SendMessageRequest) error {
	if strings.TrimSpace(req.ProjectKey) == "" || strings.TrimSpace(req.SenderName) == "" {
		return fmt.Errorf("%w: project_key and sender_name are required", ErrInvalidRequest)
	}
	if strings.TrimSpace(req.Subject) == "" || strings.TrimSpace(req.BodyMD) == "" {
		return fmt.Errorf("%w: subject and body_md are required", ErrInvalidRequest)
	}
	if req.Broadcast && (len(req.To) > 0 || len(req.CC) > 0 || len(req.BCC) > 0) {
		return fmt.Errorf("%w: broadcast cannot be combined with explicit recipients", ErrInvalidRequest)
	}
	if !req.Broadcast && len(req.To)+len(req.CC)+len(req.BCC) == 0 {
		return fmt.Errorf("%w: at least one recipient is required", ErrInvalidRequest)
	}
	return nil
}

type mcpRequest struct {
	JSONRPC string            `json:"jsonrpc"`
	ID      string            `json:"id"`
	Method  string            `json:"method"`
	Params  mcpToolCallParams `json:"params"`
}

type mcpToolCallParams struct {
	Name      string         `json:"name"`
	Arguments map[string]any `json:"arguments"`
}

type mcpResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      string          `json:"id"`
	Result  mcpResult       `json:"result"`
	Error   *mcpErrorObject `json:"error,omitempty"`
}

type mcpResult struct {
	StructuredContent json.RawMessage `json:"structuredContent,omitempty"`
	Raw               json.RawMessage `json:"result,omitempty"`
}

type mcpErrorObject struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

func nestedResult(data json.RawMessage) json.RawMessage {
	var wrapper map[string]json.RawMessage
	if err := json.Unmarshal(data, &wrapper); err != nil {
		return nil
	}
	for _, key := range []string{"result", "data"} {
		if raw := wrapper[key]; len(raw) > 0 {
			return raw
		}
	}
	return nil
}

func appendNonEmpty(args map[string]any, key string, value string) {
	if strings.TrimSpace(value) != "" {
		args[key] = value
	}
}

func appendStrings(args map[string]any, key string, values []string) {
	if len(values) > 0 {
		args[key] = values
	}
}

func positiveOrDefault(value int, fallback int) int {
	if value > 0 {
		return value
	}
	return fallback
}

func defaultString(value string, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return value
}

func truncate(value string, limit int) string {
	if len(value) <= limit {
		return value
	}
	return value[:limit] + "...[truncated]"
}

func appendUnique(values []string, value string) []string {
	value = strings.TrimSpace(value)
	if value == "" {
		return values
	}
	for _, existing := range values {
		if existing == value {
			return values
		}
	}
	return append(values, value)
}
