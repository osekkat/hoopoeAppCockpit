// Package telemetry owns Hoopoe's opt-in crash report and aggregate
// telemetry substrate. It is local-first by design: disabled services do not
// record anything, and this package never uploads directly to a collector.
package telemetry

import (
	"bufio"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/hoopoe-cockpit/hoopoe/apps/daemon/internal/redaction"
)

const (
	SchemaVersion = 1

	EventStageUsage      = "stage.usage"
	EventSwarmSize       = "swarm.size"
	EventTendingJobFire  = "tending.job.fire"
	EventErrorRate       = "error.rate"
	EventRepairActionUse = "diagnostics.repair.action"

	MaxCrashReportBytes = 512 * 1024
)

var (
	ErrDisabled       = errors.New("telemetry: opt-in required")
	ErrInvalidRequest = errors.New("telemetry: invalid request")
	ErrNotFound       = errors.New("telemetry: not found")
)

type Config struct {
	Enabled      bool
	Path         string
	CollectorURL string
	Redactor     *redaction.Redactor
	Now          func() time.Time
}

type Service struct {
	mu           sync.Mutex
	enabled      bool
	path         string
	collectorURL string
	redactor     *redaction.Redactor
	now          func() time.Time
}

type Versions struct {
	Daemon  string `json:"daemon,omitempty"`
	Desktop string `json:"desktop,omitempty"`
	API     string `json:"api,omitempty"`
}

type PrivacyStatus struct {
	SchemaVersion          int      `json:"schemaVersion"`
	Enabled                bool     `json:"enabled"`
	LocalOnly              bool     `json:"localOnly"`
	UploadConfigured       bool     `json:"uploadConfigured"`
	PendingCrashReports    int      `json:"pendingCrashReports"`
	PendingTelemetryEvents int      `json:"pendingTelemetryEvents"`
	AllowedTelemetryEvents []string `json:"allowedTelemetryEvents"`
	DisallowedFields       []string `json:"disallowedFields"`
}

type CrashReportInput struct {
	ID             string            `json:"id,omitempty"`
	DaemonVersion  string            `json:"daemonVersion,omitempty"`
	DesktopVersion string            `json:"desktopVersion,omitempty"`
	APIVersion     string            `json:"apiVersion,omitempty"`
	StackTrace     string            `json:"stackTrace"`
	AuditTail      []string          `json:"auditTail,omitempty"`
	Context        map[string]string `json:"context,omitempty"`
}

type CrashReport struct {
	SchemaVersion int               `json:"schemaVersion"`
	ID            string            `json:"id"`
	CreatedAt     time.Time         `json:"createdAt"`
	Versions      Versions          `json:"versions"`
	StackTrace    string            `json:"stackTrace"`
	AuditTail     []string          `json:"auditTail,omitempty"`
	Context       map[string]string `json:"context,omitempty"`
	Redactions    []RedactionTrace  `json:"redactions,omitempty"`
	DeletedAt     *time.Time        `json:"deletedAt,omitempty"`
}

type RedactionTrace struct {
	PatternID     string `json:"patternId"`
	Context       string `json:"context"`
	Count         int    `json:"count"`
	BytesRedacted int    `json:"bytesRedacted"`
}

type EventInput struct {
	Type       string            `json:"type"`
	Count      int64             `json:"count,omitempty"`
	Dimensions map[string]string `json:"dimensions,omitempty"`
}

type Event struct {
	SchemaVersion int               `json:"schemaVersion"`
	ID            string            `json:"id"`
	CreatedAt     time.Time         `json:"createdAt"`
	Type          string            `json:"type"`
	Count         int64             `json:"count"`
	Dimensions    map[string]string `json:"dimensions,omitempty"`
}

type record struct {
	Kind  string       `json:"kind"`
	Crash *CrashReport `json:"crash,omitempty"`
	Event *Event       `json:"event,omitempty"`
	ID    string       `json:"id,omitempty"`
	Time  time.Time    `json:"time,omitempty"`
}

func NewService(cfg Config) (*Service, error) {
	now := cfg.Now
	if now == nil {
		now = time.Now
	}
	redactor := cfg.Redactor
	if redactor == nil {
		redactor = redaction.New(redaction.Config{Now: now})
	}
	if cfg.Path != "" && !filepath.IsAbs(cfg.Path) {
		return nil, fmt.Errorf("%w: path must be absolute", ErrInvalidRequest)
	}
	return &Service{
		enabled:      cfg.Enabled,
		path:         cfg.Path,
		collectorURL: strings.TrimSpace(cfg.CollectorURL),
		redactor:     redactor,
		now:          now,
	}, nil
}

func DefaultPath(stateDir string) string {
	if stateDir == "" {
		return ""
	}
	return filepath.Join(stateDir, "telemetry", "records.jsonl")
}

func (s *Service) PrivacyStatus(ctx context.Context) (PrivacyStatus, error) {
	if s == nil {
		return PrivacyStatus{SchemaVersion: SchemaVersion, LocalOnly: true, AllowedTelemetryEvents: AllowedTelemetryEvents(), DisallowedFields: DisallowedFields()}, nil
	}
	crashes, events, err := s.countPending(ctx)
	if err != nil {
		return PrivacyStatus{}, err
	}
	return PrivacyStatus{
		SchemaVersion:          SchemaVersion,
		Enabled:                s.enabled,
		LocalOnly:              true,
		UploadConfigured:       s.collectorURL != "",
		PendingCrashReports:    crashes,
		PendingTelemetryEvents: events,
		AllowedTelemetryEvents: AllowedTelemetryEvents(),
		DisallowedFields:       DisallowedFields(),
	}, nil
}

func (s *Service) RecordEvent(ctx context.Context, input EventInput) (Event, error) {
	if s == nil || !s.enabled {
		return Event{}, ErrDisabled
	}
	if err := validateEventInput(input); err != nil {
		return Event{}, err
	}
	count := input.Count
	if count == 0 {
		count = 1
	}
	id, err := newID("evt")
	if err != nil {
		return Event{}, err
	}
	event := Event{
		SchemaVersion: SchemaVersion,
		ID:            id,
		CreatedAt:     s.now().UTC(),
		Type:          input.Type,
		Count:         count,
		Dimensions:    cloneStringMap(input.Dimensions),
	}
	if err := s.appendRecord(ctx, record{Kind: "event", Event: &event}); err != nil {
		return Event{}, err
	}
	return event, nil
}

func (s *Service) SaveCrashReport(ctx context.Context, input CrashReportInput) (CrashReport, error) {
	if s == nil || !s.enabled {
		return CrashReport{}, ErrDisabled
	}
	if err := validateCrashReportInput(input); err != nil {
		return CrashReport{}, err
	}
	id := strings.TrimSpace(input.ID)
	if id == "" {
		var err error
		id, err = newID("crash")
		if err != nil {
			return CrashReport{}, err
		}
	} else if !safeID(id) {
		return CrashReport{}, fmt.Errorf("%w: invalid crash report id", ErrInvalidRequest)
	}
	report := CrashReport{
		SchemaVersion: SchemaVersion,
		ID:            id,
		CreatedAt:     s.now().UTC(),
		Versions: Versions{
			Daemon:  strings.TrimSpace(input.DaemonVersion),
			Desktop: strings.TrimSpace(input.DesktopVersion),
			API:     strings.TrimSpace(input.APIVersion),
		},
		Context: cloneStringMap(input.Context),
	}
	var traces []redaction.TraceEvent
	report.StackTrace, traces = s.redactor.RedactText(redaction.Surface("telemetry:crash"), "stackTrace", input.StackTrace)
	for i, line := range input.AuditTail {
		redacted, lineTraces := s.redactor.RedactText(redaction.Surface("telemetry:crash"), fmt.Sprintf("auditTail[%d]", i), line)
		report.AuditTail = append(report.AuditTail, redacted)
		traces = append(traces, lineTraces...)
	}
	report.Context, traces = redactContext(s.redactor, report.Context, traces)
	report.Redactions = summarizeRedactions(traces)
	if err := s.appendRecord(ctx, record{Kind: "crash", Crash: &report}); err != nil {
		return CrashReport{}, err
	}
	return report, nil
}

func (s *Service) ListCrashReports(ctx context.Context) ([]CrashReport, error) {
	state, err := s.load(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]CrashReport, 0, len(state.crashes))
	for _, report := range state.crashes {
		if report.DeletedAt == nil {
			out = append(out, report)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.Before(out[j].CreatedAt) })
	return out, nil
}

func (s *Service) CrashReport(ctx context.Context, id string) (CrashReport, error) {
	state, err := s.load(ctx)
	if err != nil {
		return CrashReport{}, err
	}
	report, ok := state.crashes[id]
	if !ok || report.DeletedAt != nil {
		return CrashReport{}, ErrNotFound
	}
	return report, nil
}

func (s *Service) DeleteCrashReport(ctx context.Context, id string) error {
	if s == nil || !s.enabled {
		return ErrDisabled
	}
	if !safeID(id) {
		return fmt.Errorf("%w: invalid crash report id", ErrInvalidRequest)
	}
	if _, err := s.CrashReport(ctx, id); err != nil {
		return err
	}
	return s.appendRecord(ctx, record{Kind: "delete_crash", ID: id, Time: s.now().UTC()})
}

type state struct {
	crashes map[string]CrashReport
	events  []Event
}

func (s *Service) countPending(ctx context.Context) (int, int, error) {
	state, err := s.load(ctx)
	if err != nil {
		return 0, 0, err
	}
	crashes := 0
	for _, report := range state.crashes {
		if report.DeletedAt == nil {
			crashes++
		}
	}
	return crashes, len(state.events), nil
}

func (s *Service) load(ctx context.Context) (state, error) {
	if s == nil || s.path == "" {
		return state{crashes: map[string]CrashReport{}}, nil
	}
	if err := ctx.Err(); err != nil {
		return state{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.loadLocked(ctx)
}

func (s *Service) loadLocked(ctx context.Context) (state, error) {
	out := state{crashes: map[string]CrashReport{}}
	file, err := os.Open(s.path)
	if os.IsNotExist(err) {
		return out, nil
	}
	if err != nil {
		return state{}, err
	}
	defer file.Close()
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 64*1024), MaxCrashReportBytes)
	for scanner.Scan() {
		if err := ctx.Err(); err != nil {
			return state{}, err
		}
		var rec record
		if err := json.Unmarshal(scanner.Bytes(), &rec); err != nil {
			return state{}, err
		}
		switch rec.Kind {
		case "crash":
			if rec.Crash != nil {
				out.crashes[rec.Crash.ID] = *rec.Crash
			}
		case "event":
			if rec.Event != nil {
				out.events = append(out.events, *rec.Event)
			}
		case "delete_crash":
			report, ok := out.crashes[rec.ID]
			if ok {
				deletedAt := rec.Time
				report.DeletedAt = &deletedAt
				out.crashes[rec.ID] = report
			}
		}
	}
	if err := scanner.Err(); err != nil {
		return state{}, err
	}
	return out, nil
}

func (s *Service) appendRecord(ctx context.Context, rec record) error {
	if s.path == "" {
		return nil
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := os.MkdirAll(filepath.Dir(s.path), 0o700); err != nil {
		return err
	}
	file, err := os.OpenFile(s.path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	enc := json.NewEncoder(file)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(rec); err != nil {
		_ = file.Close()
		return err
	}
	if err := file.Sync(); err != nil {
		_ = file.Close()
		return err
	}
	return file.Close()
}

func AllowedTelemetryEvents() []string {
	return []string{EventErrorRate, EventRepairActionUse, EventStageUsage, EventSwarmSize, EventTendingJobFire}
}

func DisallowedFields() []string {
	return []string{"bead", "code", "content", "conversation", "file", "model", "path", "plan", "prompt", "source"}
}

func validateEventInput(input EventInput) error {
	if !allowedEventType(input.Type) {
		return fmt.Errorf("%w: unsupported event type", ErrInvalidRequest)
	}
	if input.Count < 0 {
		return fmt.Errorf("%w: count must be non-negative", ErrInvalidRequest)
	}
	for key, value := range input.Dimensions {
		if !safeDimensionKey(key) {
			return fmt.Errorf("%w: unsafe dimension key %q", ErrInvalidRequest, key)
		}
		if unsafeDimensionValue(value) {
			return fmt.Errorf("%w: unsafe dimension value for %q", ErrInvalidRequest, key)
		}
	}
	return nil
}

func validateCrashReportInput(input CrashReportInput) error {
	if strings.TrimSpace(input.StackTrace) == "" {
		return fmt.Errorf("%w: stackTrace is required", ErrInvalidRequest)
	}
	if len(input.StackTrace) > MaxCrashReportBytes {
		return fmt.Errorf("%w: crash report too large", ErrInvalidRequest)
	}
	if len(input.AuditTail) > 200 {
		return fmt.Errorf("%w: audit tail too large", ErrInvalidRequest)
	}
	for key := range input.Context {
		if !safeDimensionKey(key) || disallowedField(key) {
			return fmt.Errorf("%w: unsafe crash context key %q", ErrInvalidRequest, key)
		}
	}
	return nil
}

func allowedEventType(kind string) bool {
	for _, allowed := range AllowedTelemetryEvents() {
		if kind == allowed {
			return true
		}
	}
	return false
}

func safeDimensionKey(key string) bool {
	if key == "" || len(key) > 64 || disallowedField(key) {
		return false
	}
	for _, r := range key {
		switch {
		case r >= 'a' && r <= 'z':
		case r >= 'A' && r <= 'Z':
		case r >= '0' && r <= '9':
		case r == '_' || r == '-' || r == '.':
		default:
			return false
		}
	}
	return true
}

func disallowedField(key string) bool {
	lower := strings.ToLower(key)
	for _, bad := range DisallowedFields() {
		if strings.Contains(lower, bad) {
			return true
		}
	}
	return false
}

func unsafeDimensionValue(value string) bool {
	if len(value) > 80 || strings.ContainsAny(value, "\r\n") {
		return true
	}
	return strings.HasPrefix(value, "/") || strings.Contains(value, "/home/") || strings.Contains(value, "/Users/") || strings.Contains(value, "/data/projects/")
}

func redactContext(redactor *redaction.Redactor, context map[string]string, traces []redaction.TraceEvent) (map[string]string, []redaction.TraceEvent) {
	if len(context) == 0 {
		return nil, traces
	}
	out := make(map[string]string, len(context))
	for key, value := range context {
		redacted, next := redactor.RedactText(redaction.Surface("telemetry:crash"), "context."+key, value)
		out[key] = redacted
		traces = append(traces, next...)
	}
	return out, traces
}

func summarizeRedactions(traces []redaction.TraceEvent) []RedactionTrace {
	if len(traces) == 0 {
		return nil
	}
	out := make([]RedactionTrace, 0, len(traces))
	for _, trace := range traces {
		out = append(out, RedactionTrace{
			PatternID:     trace.PatternID,
			Context:       trace.Context,
			Count:         trace.Count,
			BytesRedacted: trace.BytesRedacted,
		})
	}
	return out
}

func cloneStringMap(in map[string]string) map[string]string {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]string, len(in))
	for key, value := range in {
		out[key] = value
	}
	return out
}

func newID(prefix string) (string, error) {
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	return prefix + "_" + hex.EncodeToString(b[:]), nil
}

func safeID(id string) bool {
	if id == "" || len(id) > 80 {
		return false
	}
	for _, r := range id {
		switch {
		case r >= 'a' && r <= 'z':
		case r >= 'A' && r <= 'Z':
		case r >= '0' && r <= '9':
		case r == '_' || r == '-':
		default:
			return false
		}
	}
	return true
}
