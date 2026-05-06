// Package api wires the daemon HTTP and event-stream surface.
package api

import (
	"bufio"
	"context"
	cryptorand "crypto/rand"
	"encoding/hex"
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
	"github.com/hoopoe-cockpit/hoopoe/apps/daemon/internal/approvals"
	"github.com/hoopoe-cockpit/hoopoe/apps/daemon/internal/audit"
	"github.com/hoopoe-cockpit/hoopoe/apps/daemon/internal/capabilities"
	"github.com/hoopoe-cockpit/hoopoe/apps/daemon/internal/jobs"
	daemonmetrics "github.com/hoopoe-cockpit/hoopoe/apps/daemon/internal/metrics"
	"github.com/hoopoe-cockpit/hoopoe/apps/daemon/internal/onboarding/checkpoints"
	"github.com/hoopoe-cockpit/hoopoe/apps/daemon/internal/projects"
	"github.com/hoopoe-cockpit/hoopoe/apps/daemon/internal/redaction"
	"github.com/hoopoe-cockpit/hoopoe/apps/daemon/internal/telemetry"
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
	Plans        PlansReader
	Beads        BeadsReader
	Providers    schemas.ProviderRegistry
	Logger       Logger
	Redactor     Redactor
	WSValidator  WebSocketTokenValidator
	Capabilities *capabilities.Registry
	Inventory    InventoryService
	Onboarding   *checkpoints.Service
	Audit        AuditLog
	Auth         *AuthConfig
	Approvals    ApprovalQueue
	Upgrade      http.Handler
	Metrics      *daemonmetrics.Registry
	Telemetry    *telemetry.Service
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
	plans        PlansReader
	beads        BeadsReader
	providers    schemas.ProviderRegistry
	logger       Logger
	redactor     Redactor
	wsValidator  WebSocketTokenValidator
	capabilities *capabilities.Registry
	inventory    InventoryService
	onboarding   *checkpoints.Service
	authConfig   *AuthConfig
	approvals    ApprovalQueue
	upgrade      http.Handler
	metrics      *daemonmetrics.Registry
	telemetry    *telemetry.Service
	auditLog     AuditLog
	// hp-nlk8: type-assert AuditLog → auditAppender ONCE at construction
	// so appendAudit doesn't repeat the assertion per call AND a
	// misconfigured AuditLog (Query-only) is detectable at boot. nil
	// when auditLog is nil OR doesn't satisfy auditAppender.
	auditLogAppender auditAppender
	now              func() time.Time
	// hp-snmn: per-process identifier appended to DaemonId so the desktop
	// can distinguish between two daemons that share the same build
	// version (e.g., in dev or after a redeploy that didn't bump the
	// version). Generated once via crypto/rand at NewRouter; stable for
	// the daemon's lifetime.
	instanceID string
	// hp-snmn: NewRouter timestamp, used to compute UptimeSeconds in the
	// /v1/health response.
	bootedAt time.Time
}

type AuditLog interface {
	Query(query audit.Query) ([]audit.Entry, error)
}

type ApprovalQueue interface {
	Get(ctx context.Context, id string) (schemas.Approval, bool, error)
	List(ctx context.Context, filter approvals.ListFilter) ([]schemas.Approval, error)
	Approve(ctx context.Context, id string, decision schemas.ApprovalDecisionRequest) (schemas.Approval, error)
	Deny(ctx context.Context, id string, decision schemas.ApprovalDecisionRequest) (schemas.Approval, error)
	Extend(ctx context.Context, id string, expiresAt time.Time) (schemas.Approval, error)
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
	r.Get("/v1/readiness", s.handleReadiness)
	r.Get("/v1/version", s.handleVersion)
	r.Get("/v1/jobs", s.handleJobs)
	r.Get("/v1/events/replay", s.handleEventReplay)
	r.Get("/v1/events/sse", s.handleEventSSE)
	r.Get("/v1/events/ws", s.handleEventWS)
	r.Get("/v1/diagnostics/metrics", s.handleMetrics)
	r.Get("/v1/diagnostics/metrics/prometheus", s.handleMetricsPrometheus)
	s.mountAuditRoutes(r)
	s.mountTelemetryRoutes(r)
	s.mountCapabilityRoutes(r)
	s.mountInventoryRoutes(r)
	s.mountProviderRoutes(r)
	s.mountSeedContractRoutes(r)
	s.mountOnboardingRoutes(r)
	// Auth routes mount AFTER the seed contract so they override the
	// `handlePlannedWrite` stubs at /v1/auth/bootstrap/bearer + ws-token
	// + session/revoke (chi's last registration wins for a given path +
	// method). When the AuthConfig is unset, the stubs answer with 501.
	s.mountAuthRoutes(r)
	mountProblemFallbacks(r)

	return r
}

func mountProblemFallbacks(r chi.Router) {
	r.NotFound(func(w http.ResponseWriter, req *http.Request) {
		writeProblemCode(w, http.StatusNotFound, "route.not_found", "route not found", "no route matched "+req.URL.Path)
	})
	r.MethodNotAllowed(func(w http.ResponseWriter, req *http.Request) {
		for _, method := range allowedMethodsForPath(r, req.URL.Path) {
			w.Header().Add("Allow", method)
		}
		writeProblemCode(
			w,
			http.StatusMethodNotAllowed,
			"route.method_not_allowed",
			"method not allowed",
			req.Method+" is not supported for "+req.URL.Path,
		)
	})
}

func allowedMethodsForPath(routes chi.Routes, path string) []string {
	candidates := []string{
		http.MethodGet,
		http.MethodHead,
		http.MethodPost,
		http.MethodPut,
		http.MethodPatch,
		http.MethodDelete,
		http.MethodOptions,
		http.MethodConnect,
		http.MethodTrace,
	}
	allowed := make([]string, 0, len(candidates))
	for _, method := range candidates {
		if routes.Match(chi.NewRouteContext(), method, path) {
			allowed = append(allowed, method)
		}
	}
	return allowed
}

func normalizeConfig(cfg Config) *server {
	now := cfg.Now
	if now == nil {
		now = time.Now
	}
	logger := cfg.Logger
	if logger == nil {
		logger = NoopLogger{}
	}
	events := cfg.Events
	if events == nil {
		events = NewEventHub(EventHubConfig{
			Now:      now,
			Redactor: redaction.New(redaction.Config{Now: now}),
			Logger:   logger,
		})
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
		jobs = MissingJobsReader{}
	}
	redactor := cfg.Redactor
	if redactor == nil {
		redactor = NoopRedactor{}
	}
	wsValidator := cfg.WSValidator
	if wsValidator == nil {
		wsValidator = AllowAllWebSocketTokens{}
	}
	inventory := resolveInventoryService(cfg.Inventory, cfg.Capabilities, now)
	metrics := cfg.Metrics
	if metrics == nil {
		metrics = daemonmetrics.NewRegistry(daemonmetrics.Config{
			Now:                   now,
			IncludeDefaultTargets: true,
		})
	}
	telemetryService := cfg.Telemetry
	if telemetryService == nil {
		telemetryService, _ = telemetry.NewService(telemetry.Config{Now: now})
	}
	// hp-nlk8: lift the auditAppender type assertion to NewRouter time.
	// If cfg.Audit was provided but doesn't satisfy auditAppender,
	// emit a structured Error so operators see the misconfiguration
	// at boot — pre-fix this case silently disabled every audit write.
	// (Logger has Info/Error but no Warn; Error is appropriate here
	// because the runtime consequence is "every mutating audit write
	// returns ErrAuditAppendUnavailable" which IS an error condition.)
	var auditLogAppender auditAppender
	if cfg.Audit != nil {
		if appender, ok := cfg.Audit.(auditAppender); ok {
			auditLogAppender = appender
		} else {
			logger.Error(context.Background(),
				"api.NewRouter: configured AuditLog does not satisfy auditAppender; mutating-route audit writes will surface ErrAuditAppendUnavailable",
				map[string]any{
					"subsystem":    "api.audit",
					"auditLogType": fmt.Sprintf("%T", cfg.Audit),
				})
		}
	}
	return &server{
		build:            build,
		events:           events,
		jobs:             jobs,
		projects:         cfg.Projects,
		plans:            cfg.Plans,
		beads:            cfg.Beads,
		providers:        cfg.Providers,
		logger:           logger,
		redactor:         redactor,
		wsValidator:      wsValidator,
		capabilities:     cfg.Capabilities,
		inventory:        inventory,
		onboarding:       cfg.Onboarding,
		authConfig:       cfg.Auth,
		approvals:        cfg.Approvals,
		upgrade:          cfg.Upgrade,
		metrics:          metrics,
		telemetry:        telemetryService,
		auditLog:         cfg.Audit,
		auditLogAppender: auditLogAppender,
		now:              now,
		instanceID:       newDaemonInstanceID(),
		bootedAt:         now(),
	}
}

// newDaemonInstanceID returns 8 bytes of crypto/rand encoded as hex —
// 16 chars, sufficient to disambiguate concurrent daemons in audit and
// pairing flows. Falls back to a deterministic timestamp string if the
// OS RNG is unavailable (extremely rare; logs a warning at the call
// site is overkill for a startup-time fallback).
func newDaemonInstanceID() string {
	var b [8]byte
	if _, err := cryptorand.Read(b[:]); err != nil {
		return fmt.Sprintf("fallback-%d", time.Now().UnixNano())
	}
	return hex.EncodeToString(b[:])
}

func (s *server) requestLogMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		start := time.Now()
		next.ServeHTTP(rec, r)
		duration := time.Since(start)
		_ = s.metrics.ObserveDuration(daemonmetrics.MetricRequestDurationSeconds, daemonmetrics.Labels{
			"method": r.Method,
			"route":  metricRoute(r),
			"status": strconv.Itoa(rec.status),
		}, duration)
		s.logger.Info(r.Context(), "http_request", map[string]any{
			"method":      r.Method,
			"path":        s.redactor.Redact("path", r.URL.Path),
			"remote":      s.redactor.Redact("remote", remoteHost(r.RemoteAddr)),
			"status":      rec.status,
			"duration_ms": duration.Milliseconds(),
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
	// hp-snmn: /v1/health is the LIVENESS probe — always 200 when the
	// HTTP server is responsive enough to reach this handler. Per-
	// subsystem health surfaces in the response body's `adapters` map
	// + overall `status` field; clients that want a strict gate use
	// /v1/readiness instead.
	writeJSON(w, http.StatusOK, s.healthResponse())
}

func (s *server) handleReadiness(w http.ResponseWriter, r *http.Request) {
	// hp-snmn: /v1/readiness is the STRICT readiness probe. Returns
	// 200 with the same HealthResponse shape as /v1/health when every
	// required subsystem is ok; returns 503 (Service Unavailable) with
	// the same shape when any required subsystem is missing or
	// degraded. Modeled on the kubelet readinessProbe convention so
	// load balancers / pairing flows can refuse to route traffic to a
	// half-functional daemon.
	resp := s.healthResponse()
	status := http.StatusOK
	if resp.Adapters == nil || !readinessOK(*resp.Adapters) {
		status = http.StatusServiceUnavailable
	}
	writeJSON(w, status, resp)
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
		if errors.Is(err, ErrJobsReaderUnavailable) {
			s.writeProblemCode(w, http.StatusServiceUnavailable, "jobs.registry_unavailable", "job registry unavailable", err.Error())
			return
		}
		s.writeProblem(w, http.StatusInternalServerError, "job list failed", err.Error())
		return
	}
	_ = s.metrics.SetGauge(daemonmetrics.MetricInFlightJobs, nil, float64(countInFlightJobs(jobList)))
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
	// hp-0vkn: reject cursors that claim to have seen events the daemon
	// never produced. Per-channel sequences are strictly monotonic via
	// nextSequenceLocked; a sinceSequence above LastSequence is either
	// a stale fixture, a fabricated request, or a client/server clock-
	// skew bug. Without this guard, replay would silently return zero
	// events and the subscriber would believe their cursor is valid.
	if last := s.events.LastSequence(channel); since > last {
		s.writeProblem(w, http.StatusBadRequest, "non-monotonic sinceSequence",
			fmt.Sprintf("sinceSequence %d exceeds channel %q last sequence %d", since, channel, last))
		return
	}
	start := time.Now()
	window := s.events.replayWindow(channel, since)
	_ = s.metrics.IncCounter(daemonmetrics.MetricEventsReplayedTotal, daemonmetrics.Labels{"channel": channel}, float64(len(window.Events)))
	if since > 0 {
		_ = s.metrics.ObserveDuration(daemonmetrics.MetricEventReplayAfterDisconnectMS, daemonmetrics.Labels{
			"channel": channel,
			"gap":     strconv.FormatBool(window.Gap),
		}, time.Since(start))
	}
	writeJSON(w, http.StatusOK, eventReplayResponse(window, channel))
}

func (s *server) handleEventSSE(w http.ResponseWriter, r *http.Request) {
	channels := parseChannels(r.URL.Query().Get("channels"))
	cursors, err := parseCursorMap(r.URL.Query().Get("cursors"))
	if err != nil {
		s.writeProblem(w, http.StatusBadRequest, "invalid cursors", err.Error())
		return
	}
	// hp-0vkn: reject cursors that exceed the channel's last sequence.
	if err := validateCursorMap(s.events, cursors); err != nil {
		s.writeProblem(w, http.StatusBadRequest, "non-monotonic cursors", err.Error())
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
	_ = s.metrics.AddGauge(daemonmetrics.MetricActiveWSConnections, nil, 1)
	defer func() {
		_ = s.metrics.AddGauge(daemonmetrics.MetricActiveWSConnections, nil, -1)
	}()

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
		// hp-4qbg: marshal failure used to be returned to the caller,
		// which closed the WS connection. The client then reconnected,
		// replayed from cursor, hit the same poisoned event, and got
		// disconnected again — a tight reconnect loop. Publish now
		// sanitizes EventHub events up-front so this branch is mostly
		// unreachable for real Event payloads; the defensive sentinel
		// here keeps the connection alive for any other payload whose
		// nested types might still be unmarshalable.
		sentinel, sentErr := json.Marshal(map[string]any{
			"_encodeError": true,
			"marshalError": err.Error(),
			"type":         EventTypeEncodeError,
		})
		if sentErr != nil {
			return err
		}
		return conn.Write(ctx, websocket.MessageText, sentinel)
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
		// hp-0vkn: WS handshake must reject cursors that exceed the
		// channel's last sequence, mirroring the SSE + replay paths.
		if err := validateCursorMap(s.events, cursors); err != nil {
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
		// hp-4qbg: marshal failure used to silently drop the event,
		// creating phantom sequence gaps that subscribers couldn't
		// distinguish from real loss. Publish now sanitizes Event.Data
		// up-front so this branch should be unreachable for real
		// EventHub events; the defensive sentinel here covers any
		// other payload (snapshot envelopes, heartbeats, compatibility
		// warnings) whose nested types might still be unmarshalable.
		s.logger.Error(context.Background(), "sse_encode_failed", map[string]any{
			"error":     err.Error(),
			"eventType": eventType,
		})
		sentinel, sentErr := json.Marshal(map[string]any{
			"_encodeError": true,
			"originalType": eventType,
			"marshalError": err.Error(),
		})
		if sentErr != nil {
			// Hand-rolled fallback: even the sentinel encoder failed,
			// which would be a programmer error since the sentinel is
			// primitive types.
			sentinel = []byte(`{"_encodeError":true,"originalType":"unknown","marshalError":"sentinel encoder failed"}`)
		}
		fmt.Fprintf(w, "event: %s\n", EventTypeEncodeError)
		fmt.Fprintf(w, "data: %s\n\n", sentinel)
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

// validateCursorMap enforces the per-channel monotonic invariant
// (hp-0vkn): a cursor must not exceed the daemon's last produced
// sequence for that channel. The check covers stale fixture replays,
// fabricated requests, and clock-skew-driven cursor drift. Channels
// not in the cursors map are skipped — clients are allowed to start
// fresh at sequence 0.
func validateCursorMap(events *EventHub, cursors map[string]uint64) error {
	if events == nil {
		return nil
	}
	for channel, cursor := range cursors {
		if last := events.LastSequence(channel); cursor > last {
			return fmt.Errorf("cursor for channel %q is %d but daemon last sequence is %d", channel, cursor, last)
		}
	}
	return nil
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
