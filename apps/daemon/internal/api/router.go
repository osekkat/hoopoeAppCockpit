// Package api wires the daemon HTTP and event-stream surface.
package api

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/hoopoe-cockpit/hoopoe/apps/daemon/internal/capabilities"
	"github.com/hoopoe-cockpit/hoopoe/apps/daemon/internal/jobs"
	"github.com/hoopoe-cockpit/hoopoe/apps/daemon/internal/onboarding/checkpoints"
	"github.com/hoopoe-cockpit/hoopoe/apps/daemon/internal/projects"
	schemas "github.com/hoopoe-cockpit/hoopoe/packages/schemas/go"
	"nhooyr.io/websocket"
)

const schemaVersion = 1

// BuildInfo is returned by GET /v1/version.
type BuildInfo struct {
	Version    string `json:"version"`
	Commit     string `json:"commit"`
	BuildDate  string `json:"buildDate"`
	APIVersion string `json:"apiVersion"`
}

// Config carries the daemon dependencies required by the HTTP transport.
type Config struct {
	Build        BuildInfo
	Events       *EventHub
	Jobs         JobsReader
	Projects     ProjectRegistry
	Logger       Logger
	Redactor     Redactor
	WSValidator  WebSocketTokenValidator
	Capabilities *capabilities.Registry
	Onboarding   *checkpoints.Service
	Auth         *AuthConfig
	Upgrade      http.Handler
	Now          func() time.Time
}

// Logger is the structured logging hook used by router middleware.
type Logger interface {
	Info(ctx context.Context, message string, fields map[string]any)
	Error(ctx context.Context, message string, fields map[string]any)
}

// Redactor lets the audit/log layer scrub secrets before fields are logged.
type Redactor interface {
	Redact(field string, value any) any
}

// WebSocketTokenValidator validates short-lived WS tokens. Phase 2 auth wiring
// swaps this for the real SessionCredentialService; the scaffold keeps the
// dependency explicit so the transport never owns auth state.
type WebSocketTokenValidator interface {
	ValidateWebSocketToken(ctx context.Context, token string) error
}

type ProjectRegistry interface {
	List(ctx context.Context) ([]schemas.Project, error)
	Project(ctx context.Context, id string) (schemas.Project, error)
	Import(ctx context.Context, req projects.ImportRequest) (schemas.Project, error)
	Readiness(ctx context.Context, id string) (schemas.ProjectReadiness, error)
}

type server struct {
	build        BuildInfo
	events       *EventHub
	jobs         JobsReader
	projects     ProjectRegistry
	logger       Logger
	redactor     Redactor
	wsValidator  WebSocketTokenValidator
	capabilities *capabilities.Registry
	onboarding   *checkpoints.Service
	authConfig   *AuthConfig
	upgrade      http.Handler
	now          func() time.Time
}

type subscribeRequest struct {
	Op            string            `json:"op"`
	Channels      []string          `json:"channels"`
	Cursors       map[string]uint64 `json:"cursors"`
	SchemaVersion *int              `json:"schemaVersion,omitempty"`
}

// NewRouter returns the daemon HTTP router. It includes both /health and
// /v1/health because early bootstrap probes use the short path while the seed
// API contract uses the versioned path.
func NewRouter(cfg Config) http.Handler {
	s := normalizeConfig(cfg)

	r := chi.NewRouter()
	r.Use(middleware.RequestID)
	r.Use(s.requestLogMiddleware)

	r.Get("/health", s.handleLegacyHealth)
	r.Get("/v1/health", s.handleHealth)
	r.Get("/v1/version", s.handleVersion)
	r.Get("/v1/jobs", s.handleJobs)
	r.Get("/v1/events/replay", s.handleEventReplay)
	r.Get("/v1/events/sse", s.handleEventSSE)
	r.Get("/v1/events/ws", s.handleEventWS)
	s.mountCapabilityRoutes(r)
	s.mountSeedContractRoutes(r)
	s.mountOnboardingRoutes(r)
	// Auth routes mount AFTER the seed contract so they override the
	// `handlePlannedWrite` stubs at /v1/auth/bootstrap/bearer + ws-token
	// + session/revoke (chi's last registration wins for a given path +
	// method). When the AuthConfig is unset, the stubs answer with 501.
	s.mountAuthRoutes(r)

	return r
}

func normalizeConfig(cfg Config) *server {
	now := cfg.Now
	if now == nil {
		now = time.Now
	}
	events := cfg.Events
	if events == nil {
		events = NewEventHub(EventHubConfig{Now: now})
	}
	build := cfg.Build
	if build.Version == "" {
		build.Version = "0.0.0"
	}
	if build.Commit == "" {
		build.Commit = "dev"
	}
	if build.BuildDate == "" {
		build.BuildDate = "dev"
	}
	if build.APIVersion == "" {
		build.APIVersion = "v1"
	}
	jobs := cfg.Jobs
	if jobs == nil {
		jobs = EmptyJobsReader{}
	}
	logger := cfg.Logger
	if logger == nil {
		logger = NoopLogger{}
	}
	redactor := cfg.Redactor
	if redactor == nil {
		redactor = NoopRedactor{}
	}
	wsValidator := cfg.WSValidator
	if wsValidator == nil {
		wsValidator = AllowAllWebSocketTokens{}
	}
	return &server{
		build:        build,
		events:       events,
		jobs:         jobs,
		projects:     cfg.Projects,
		logger:       logger,
		redactor:     redactor,
		wsValidator:  wsValidator,
		capabilities: cfg.Capabilities,
		onboarding:   cfg.Onboarding,
		authConfig:   cfg.Auth,
		upgrade:      cfg.Upgrade,
		now:          now,
	}
}

func (s *server) requestLogMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		start := s.now()
		next.ServeHTTP(rec, r)
		s.logger.Info(r.Context(), "http_request", map[string]any{
			"method":      r.Method,
			"path":        s.redactor.Redact("path", r.URL.Path),
			"remote":      s.redactor.Redact("remote", remoteHost(r.RemoteAddr)),
			"status":      rec.status,
			"duration_ms": time.Since(start).Milliseconds(),
		})
	})
}

type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (r *statusRecorder) WriteHeader(status int) {
	r.status = status
	r.ResponseWriter.WriteHeader(status)
}

func (r *statusRecorder) Unwrap() http.ResponseWriter {
	return r.ResponseWriter
}

func (r *statusRecorder) Flush() {
	if flusher, ok := r.ResponseWriter.(http.Flusher); ok {
		flusher.Flush()
	}
}

func (r *statusRecorder) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	hijacker, ok := r.ResponseWriter.(http.Hijacker)
	if !ok {
		return nil, nil, http.ErrNotSupported
	}
	return hijacker.Hijack()
}

func (s *server) handleHealth(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, s.healthResponse())
}

func (s *server) handleLegacyHealth(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":            true,
		"schemaVersion": schemaVersion,
		"time":          s.now().UTC().Format(time.RFC3339Nano),
	})
}

func (s *server) handleVersion(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, s.versionResponse())
}

func (s *server) handleJobs(w http.ResponseWriter, r *http.Request) {
	jobList, err := s.jobs.List(r.Context(), jobs.ListFilter{})
	if err != nil {
		s.writeProblem(w, http.StatusInternalServerError, "job list failed", err.Error())
		return
	}
	if jobList == nil {
		jobList = []jobs.Job{}
	}
	writeJSON(w, http.StatusOK, jobListResponse(jobList))
}

func (s *server) handleEventReplay(w http.ResponseWriter, r *http.Request) {
	channel := strings.TrimSpace(r.URL.Query().Get("channel"))
	if channel == "" {
		s.writeProblem(w, http.StatusBadRequest, "missing channel", "channel query parameter is required")
		return
	}
	since, err := parseSequence(r.URL.Query().Get("sinceSequence"))
	if err != nil {
		s.writeProblem(w, http.StatusBadRequest, "invalid sinceSequence", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, eventReplayResponse(s.events.replayWindow(channel, since), channel))
}

func (s *server) handleEventSSE(w http.ResponseWriter, r *http.Request) {
	channels := parseChannels(r.URL.Query().Get("channels"))
	cursors, err := parseCursorMap(r.URL.Query().Get("cursors"))
	if err != nil {
		s.writeProblem(w, http.StatusBadRequest, "invalid cursors", err.Error())
		return
	}
	clientSchemaVersion, err := parseSchemaVersion(r.URL.Query().Get("schemaVersion"))
	if err != nil {
		s.writeProblem(w, http.StatusBadRequest, "invalid schemaVersion", err.Error())
		return
	}

	flusher, ok := w.(http.Flusher)
	if !ok {
		s.writeProblem(w, http.StatusInternalServerError, "streaming unsupported", "response writer does not support flushing")
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	sub := s.events.Subscribe(r.Context(), channels)
	defer sub.Close()

	snapshot, gaps := s.events.SnapshotWithGaps(channels, cursors)
	s.writeSSE(w, "snapshot", snapshot)
	flusher.Flush()
	if clientSchemaVersion != nil && *clientSchemaVersion < schemaVersion {
		s.writeSSE(w, "_compatibility_warning", s.events.CompatibilityWarning(*clientSchemaVersion))
		flusher.Flush()
	}
	for _, ev := range gaps {
		s.writeSSE(w, ev.Type, ev)
		flusher.Flush()
	}

	heartbeat := time.NewTicker(25 * time.Second)
	defer heartbeat.Stop()

	for {
		select {
		case <-r.Context().Done():
			return
		case ev, ok := <-sub.Events():
			if !ok {
				return
			}
			s.writeSSE(w, ev.Type, ev)
			flusher.Flush()
		case <-heartbeat.C:
			s.writeSSE(w, "heartbeat", s.events.Heartbeat())
			flusher.Flush()
		}
	}
}

func (s *server) handleEventWS(w http.ResponseWriter, r *http.Request) {
	token := r.URL.Query().Get("wsToken")
	if err := s.wsValidator.ValidateWebSocketToken(r.Context(), token); err != nil {
		s.writeProblem(w, http.StatusUnauthorized, "invalid ws token", err.Error())
		return
	}

	conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{
		CompressionMode: websocket.CompressionDisabled,
	})
	if err != nil {
		s.logger.Error(r.Context(), "websocket_accept_failed", map[string]any{"error": err.Error()})
		return
	}
	defer conn.Close(websocket.StatusNormalClosure, "daemon stream closed")

	subscribe, err := s.webSocketSubscribeRequest(r.Context(), r, conn)
	if err != nil {
		_ = conn.Close(websocket.StatusPolicyViolation, err.Error())
		return
	}

	ctx := conn.CloseRead(r.Context())
	sub := s.events.Subscribe(ctx, subscribe.Channels)
	defer sub.Close()

	snapshot, gaps := s.events.SnapshotWithGaps(subscribe.Channels, subscribe.Cursors)
	if err := writeWebSocketJSON(ctx, conn, map[string]any{
		"op":       "snapshot",
		"snapshot": snapshot,
	}); err != nil {
		return
	}
	if subscribe.SchemaVersion != nil && *subscribe.SchemaVersion < schemaVersion {
		if err := writeWebSocketJSON(ctx, conn, s.events.CompatibilityWarning(*subscribe.SchemaVersion)); err != nil {
			return
		}
	}
	for _, ev := range gaps {
		if err := writeWebSocketJSON(ctx, conn, ev); err != nil {
			return
		}
	}

	heartbeat := time.NewTicker(25 * time.Second)
	defer heartbeat.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case ev, ok := <-sub.Events():
			if !ok {
				return
			}
			if err := writeWebSocketJSON(ctx, conn, ev); err != nil {
				return
			}
		case <-heartbeat.C:
			if err := writeWebSocketJSON(ctx, conn, s.events.Heartbeat()); err != nil {
				return
			}
		}
	}
}

func writeWebSocketJSON(ctx context.Context, conn *websocket.Conn, payload any) error {
	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	return conn.Write(ctx, websocket.MessageText, body)
}

func (s *server) webSocketSubscribeRequest(ctx context.Context, r *http.Request, conn *websocket.Conn) (subscribeRequest, error) {
	query := r.URL.Query()
	channelsRaw := firstQueryValue(query, "channels")
	cursorsRaw := firstQueryValue(query, "cursors")
	schemaVersionRaw := firstQueryValue(query, "schemaVersion")
	if channelsRaw != "" || cursorsRaw != "" || schemaVersionRaw != "" {
		cursors, err := parseCursorMap(cursorsRaw)
		if err != nil {
			return subscribeRequest{}, err
		}
		clientSchemaVersion, err := parseSchemaVersion(schemaVersionRaw)
		if err != nil {
			return subscribeRequest{}, err
		}
		return subscribeRequest{
			Op:            "subscribe",
			Channels:      parseChannels(channelsRaw),
			Cursors:       cursors,
			SchemaVersion: clientSchemaVersion,
		}, nil
	}

	messageType, body, err := conn.Read(ctx)
	if err != nil {
		return subscribeRequest{}, fmt.Errorf("read subscribe envelope: %w", err)
	}
	if messageType != websocket.MessageText {
		return subscribeRequest{}, fmt.Errorf("subscribe envelope must be a text message")
	}
	var req subscribeRequest
	if err := json.Unmarshal(body, &req); err != nil {
		return subscribeRequest{}, fmt.Errorf("subscribe envelope must be JSON")
	}
	if req.Op != "subscribe" {
		return subscribeRequest{}, fmt.Errorf("first websocket message must be op=subscribe")
	}
	req.Channels = normalizeChannels(req.Channels)
	if req.Cursors == nil {
		req.Cursors = map[string]uint64{}
	}
	return req, nil
}

func firstQueryValue(values map[string][]string, key string) string {
	parts := values[key]
	if len(parts) == 0 {
		return ""
	}
	return parts[0]
}

func (s *server) writeSSE(w http.ResponseWriter, eventType string, payload any) {
	body, err := json.Marshal(payload)
	if err != nil {
		s.logger.Error(context.Background(), "sse_encode_failed", map[string]any{"error": err.Error()})
		return
	}
	fmt.Fprintf(w, "event: %s\n", safeSSEEventName(eventType))
	fmt.Fprintf(w, "data: %s\n\n", body)
}

func (s *server) writeProblem(w http.ResponseWriter, status int, title string, detail string) {
	writeProblem(w, status, title, detail)
}

func (s *server) writeProblemCode(w http.ResponseWriter, status int, code string, title string, detail string) {
	writeProblemCode(w, status, code, title, detail)
}

func parseSequence(raw string) (uint64, error) {
	if raw == "" {
		return 0, nil
	}
	value, err := strconv.ParseUint(raw, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("sinceSequence must be an unsigned integer")
	}
	return value, nil
}

func parseChannels(raw string) []string {
	if raw == "" {
		return []string{"_system"}
	}
	parts := strings.Split(raw, ",")
	channels := make([]string, 0, len(parts))
	seen := map[string]struct{}{}
	for _, part := range parts {
		channel := strings.TrimSpace(part)
		if channel == "" {
			continue
		}
		if !safeEventChannel(channel) {
			continue
		}
		if _, ok := seen[channel]; ok {
			continue
		}
		seen[channel] = struct{}{}
		channels = append(channels, channel)
	}
	if len(channels) == 0 {
		return []string{"_system"}
	}
	return channels
}

func parseCursorMap(raw string) (map[string]uint64, error) {
	if raw == "" {
		return map[string]uint64{}, nil
	}
	var cursors map[string]uint64
	if err := json.Unmarshal([]byte(raw), &cursors); err != nil {
		return nil, fmt.Errorf("cursors must be a JSON object of channel to sequence")
	}
	if cursors == nil {
		return map[string]uint64{}, nil
	}
	return cursors, nil
}

func parseSchemaVersion(raw string) (*int, error) {
	if raw == "" {
		return nil, nil
	}
	value, err := strconv.ParseUint(raw, 10, 31)
	if err != nil {
		return nil, fmt.Errorf("schemaVersion must be an unsigned integer")
	}
	parsed := int(value)
	return &parsed, nil
}

func normalizeChannels(raw []string) []string {
	if len(raw) == 0 {
		return []string{"_system"}
	}
	channels := make([]string, 0, len(raw))
	seen := map[string]struct{}{}
	for _, part := range raw {
		channel := strings.TrimSpace(part)
		if channel == "" {
			continue
		}
		if !safeEventChannel(channel) {
			continue
		}
		if _, ok := seen[channel]; ok {
			continue
		}
		seen[channel] = struct{}{}
		channels = append(channels, channel)
	}
	if len(channels) == 0 {
		return []string{"_system"}
	}
	return channels
}

func remoteHost(remote string) string {
	host, _, err := net.SplitHostPort(remote)
	if err != nil {
		return remote
	}
	return host
}

func safeSSEEventName(raw string) string {
	if raw == "" {
		return "message"
	}
	for _, r := range raw {
		switch {
		case r >= 'a' && r <= 'z':
		case r >= 'A' && r <= 'Z':
		case r >= '0' && r <= '9':
		case r == '.' || r == '_' || r == '-':
		default:
			return "message"
		}
	}
	return raw
}

func safeEventChannel(raw string) bool {
	if raw == "" || len(raw) > 128 {
		return false
	}
	for _, r := range raw {
		switch {
		case r >= 'a' && r <= 'z':
		case r >= 'A' && r <= 'Z':
		case r >= '0' && r <= '9':
		case r == '.' || r == '_' || r == '-' || r == ':':
		default:
			return false
		}
	}
	return true
}

// NoopLogger keeps tests and early boots quiet until the audit/log substrate
// is wired.
type NoopLogger struct{}

func (NoopLogger) Info(context.Context, string, map[string]any)  {}
func (NoopLogger) Error(context.Context, string, map[string]any) {}

type NoopRedactor struct{}

func (NoopRedactor) Redact(_ string, value any) any { return value }

type AllowAllWebSocketTokens struct{}

func (AllowAllWebSocketTokens) ValidateWebSocketToken(context.Context, string) error {
	return nil
}

type StaticWebSocketTokenValidator struct {
	Token string
}

func (v StaticWebSocketTokenValidator) ValidateWebSocketToken(_ context.Context, token string) error {
	if v.Token == "" {
		return nil
	}
	if token != v.Token {
		return errors.New("websocket token rejected")
	}
	return nil
}
