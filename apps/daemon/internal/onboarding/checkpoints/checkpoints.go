// Package checkpoints stores resumable onboarding wizard progress.
//
// The wizard and ACFS installer remain the source of truth for what work is
// performed. This package records durable, queryable checkpoints so the
// desktop can resume safely after failures and Diagnostics can present repair
// actions without scraping terminal output.
package checkpoints

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	schemas "github.com/hoopoe-cockpit/hoopoe/packages/schemas/go"
)

const (
	SchemaVersion = 1

	ActionCheckpointTransition = "onboarding.checkpoint_transition"
)

var (
	ErrInvalidInput = errors.New("checkpoints: invalid input")
	ErrNotFound     = errors.New("checkpoints: not found")
)

type Status string

const (
	StatusPending   Status = "pending"
	StatusRunning   Status = "running"
	StatusSucceeded Status = "succeeded"
	StatusFailed    Status = "failed"
	StatusSkipped   Status = "skipped"
)

func (s Status) Valid() bool {
	switch s {
	case StatusPending, StatusRunning, StatusSucceeded, StatusFailed, StatusSkipped:
		return true
	default:
		return false
	}
}

func (s Status) terminal() bool {
	switch s {
	case StatusSucceeded, StatusFailed, StatusSkipped:
		return true
	default:
		return false
	}
}

type Checkpoint struct {
	SchemaVersion int        `json:"schemaVersion"`
	RunID         string     `json:"runId"`
	StepID        string     `json:"stepId"`
	ProjectID     string     `json:"projectId,omitempty"`
	StepLabel     string     `json:"stepLabel,omitempty"`
	Status        Status     `json:"status"`
	Attempt       int        `json:"attempt"`
	StartedAt     *time.Time `json:"startedAt,omitempty"`
	CompletedAt   *time.Time `json:"completedAt,omitempty"`
	UpdatedAt     time.Time  `json:"updatedAt"`
	EvidenceRefs  []string   `json:"evidenceRefs,omitempty"`
	FailureReason string     `json:"failureReason,omitempty"`
	ResumeHint    string     `json:"resumeHint,omitempty"`
}

type Timeline struct {
	SchemaVersion int          `json:"schemaVersion"`
	RunID         string       `json:"runId"`
	ProjectID     string       `json:"projectId,omitempty"`
	Checkpoints   []Checkpoint `json:"checkpoints"`
	Actions       []RepairHint `json:"actions,omitempty"`
	UpdatedAt     time.Time    `json:"updatedAt,omitempty"`
}

type RepairHint struct {
	StepID    string         `json:"stepId"`
	Status    Status         `json:"status"`
	Actions   []RepairAction `json:"actions"`
	Reason    string         `json:"reason,omitempty"`
	UpdatedAt time.Time      `json:"updatedAt"`
}

type TransitionRequest struct {
	RunID         string
	StepID        string
	ProjectID     string
	StepLabel     string
	Status        Status
	Actor         schemas.Actor
	Reason        string
	EvidenceRefs  []string
	FailureReason string
	ResumeHint    string
	At            time.Time
}

type TransitionResult struct {
	Checkpoint Checkpoint `json:"checkpoint"`
	AuditEvent AuditEvent `json:"auditEvent"`
	Created    bool       `json:"created"`
}

type AuditEvent struct {
	SchemaVersion int            `json:"schemaVersion"`
	EventID       string         `json:"eventId"`
	Action        string         `json:"action"`
	RunID         string         `json:"runId"`
	StepID        string         `json:"stepId"`
	ProjectID     string         `json:"projectId,omitempty"`
	FromStatus    Status         `json:"fromStatus,omitempty"`
	ToStatus      Status         `json:"toStatus"`
	Actor         schemas.Actor  `json:"actor"`
	Reason        string         `json:"reason,omitempty"`
	EvidenceRefs  []string       `json:"evidenceRefs,omitempty"`
	At            time.Time      `json:"at"`
	Data          map[string]any `json:"data,omitempty"`
}

type AuditSink interface {
	RecordCheckpointTransition(context.Context, AuditEvent) error
}

type Store interface {
	Save(context.Context, Checkpoint, AuditEvent) error
	Get(context.Context, string, string) (Checkpoint, bool, error)
	ListRun(context.Context, string) ([]Checkpoint, error)
}

type Service struct {
	store Store
	audit AuditSink
	now   func() time.Time
	newID func() (string, error)
}

type Config struct {
	Store Store
	Audit AuditSink
	Now   func() time.Time
	NewID func() (string, error)
}

func NewService(config Config) *Service {
	store := config.Store
	if store == nil {
		store = NewMemoryStore()
	}
	now := config.Now
	if now == nil {
		now = time.Now
	}
	newID := config.NewID
	if newID == nil {
		newID = randomEventID
	}
	return &Service{store: store, audit: config.Audit, now: now, newID: newID}
}

func (s *Service) Transition(ctx context.Context, req TransitionRequest) (TransitionResult, error) {
	if s == nil {
		return TransitionResult{}, fmt.Errorf("%w: nil service", ErrInvalidInput)
	}
	normalized, err := s.normalizeTransition(req)
	if err != nil {
		return TransitionResult{}, err
	}
	existing, found, err := s.store.Get(ctx, normalized.RunID, normalized.StepID)
	if err != nil {
		return TransitionResult{}, err
	}
	next := checkpointFromTransition(normalized, existing, found)
	eventID, err := s.newID()
	if err != nil {
		return TransitionResult{}, fmt.Errorf("checkpoints: event id: %w", err)
	}
	event := AuditEvent{
		SchemaVersion: SchemaVersion,
		EventID:       eventID,
		Action:        ActionCheckpointTransition,
		RunID:         next.RunID,
		StepID:        next.StepID,
		ProjectID:     next.ProjectID,
		ToStatus:      next.Status,
		Actor:         normalized.Actor,
		Reason:        normalized.Reason,
		EvidenceRefs:  cloneStrings(normalized.EvidenceRefs),
		At:            normalized.At,
		Data: map[string]any{
			"attempt": next.Attempt,
		},
	}
	if found {
		event.FromStatus = existing.Status
	}
	if next.FailureReason != "" {
		event.Data["failureReason"] = next.FailureReason
	}
	if next.ResumeHint != "" {
		event.Data["resumeHint"] = next.ResumeHint
	}
	if err := s.store.Save(ctx, next, event); err != nil {
		return TransitionResult{}, err
	}
	if s.audit != nil {
		if err := s.audit.RecordCheckpointTransition(ctx, event); err != nil {
			return TransitionResult{}, err
		}
	}
	return TransitionResult{Checkpoint: next, AuditEvent: event, Created: !found}, nil
}

func (s *Service) Timeline(ctx context.Context, runID string) (Timeline, error) {
	if s == nil {
		return Timeline{}, fmt.Errorf("%w: nil service", ErrInvalidInput)
	}
	runID = strings.TrimSpace(runID)
	if !safeID(runID) {
		return Timeline{}, fmt.Errorf("%w: runId", ErrInvalidInput)
	}
	items, err := s.store.ListRun(ctx, runID)
	if err != nil {
		return Timeline{}, err
	}
	timeline := Timeline{
		SchemaVersion: SchemaVersion,
		RunID:         runID,
		Checkpoints:   items,
	}
	for _, item := range items {
		if timeline.ProjectID == "" {
			timeline.ProjectID = item.ProjectID
		}
		if item.UpdatedAt.After(timeline.UpdatedAt) {
			timeline.UpdatedAt = item.UpdatedAt
		}
		if actions := RepairActionsForCheckpoint(item); len(actions) > 0 {
			timeline.Actions = append(timeline.Actions, RepairHint{
				StepID:    item.StepID,
				Status:    item.Status,
				Actions:   actions,
				Reason:    item.FailureReason,
				UpdatedAt: item.UpdatedAt,
			})
		}
	}
	return timeline, nil
}

func (s *Service) RepairActions() []RepairAction {
	return RepairCatalog()
}

func (s *Service) normalizeTransition(req TransitionRequest) (TransitionRequest, error) {
	req.RunID = strings.TrimSpace(req.RunID)
	req.StepID = strings.TrimSpace(req.StepID)
	req.ProjectID = strings.TrimSpace(req.ProjectID)
	req.StepLabel = strings.TrimSpace(req.StepLabel)
	req.Reason = strings.TrimSpace(req.Reason)
	req.FailureReason = strings.TrimSpace(req.FailureReason)
	req.ResumeHint = strings.TrimSpace(req.ResumeHint)
	req.EvidenceRefs = cleanStrings(req.EvidenceRefs)
	if !safeID(req.RunID) {
		return TransitionRequest{}, fmt.Errorf("%w: runId", ErrInvalidInput)
	}
	if !safeStepID(req.StepID) {
		return TransitionRequest{}, fmt.Errorf("%w: stepId", ErrInvalidInput)
	}
	if !req.Status.Valid() {
		return TransitionRequest{}, fmt.Errorf("%w: status", ErrInvalidInput)
	}
	if req.At.IsZero() {
		req.At = s.now().UTC()
	} else {
		req.At = req.At.UTC()
	}
	if req.Actor.Kind == "" {
		req.Actor.Kind = schemas.ActorKindSystem
	}
	if !req.Actor.Kind.Valid() {
		return TransitionRequest{}, fmt.Errorf("%w: actor.kind", ErrInvalidInput)
	}
	return req, nil
}

func checkpointFromTransition(req TransitionRequest, existing Checkpoint, found bool) Checkpoint {
	next := Checkpoint{
		SchemaVersion: SchemaVersion,
		RunID:         req.RunID,
		StepID:        req.StepID,
		ProjectID:     req.ProjectID,
		StepLabel:     req.StepLabel,
		Status:        req.Status,
		Attempt:       1,
		UpdatedAt:     req.At,
		EvidenceRefs:  cloneStrings(req.EvidenceRefs),
		FailureReason: req.FailureReason,
		ResumeHint:    req.ResumeHint,
	}
	if found {
		next = existing
		next.SchemaVersion = SchemaVersion
		next.ProjectID = chooseString(req.ProjectID, existing.ProjectID)
		next.StepLabel = chooseString(req.StepLabel, existing.StepLabel)
		next.Status = req.Status
		next.UpdatedAt = req.At
		next.EvidenceRefs = mergeStrings(existing.EvidenceRefs, req.EvidenceRefs)
		next.FailureReason = chooseString(req.FailureReason, existing.FailureReason)
		next.ResumeHint = chooseString(req.ResumeHint, existing.ResumeHint)
		if req.Status == StatusRunning && existing.Status != StatusRunning {
			next.Attempt++
		}
	}
	if req.Status == StatusRunning && next.StartedAt == nil {
		next.StartedAt = timePtr(req.At)
	}
	if next.StartedAt == nil && req.Status.terminal() {
		next.StartedAt = timePtr(req.At)
	}
	if req.Status.terminal() {
		next.CompletedAt = timePtr(req.At)
	} else if req.Status == StatusRunning {
		next.CompletedAt = nil
	}
	if next.Attempt <= 0 {
		next.Attempt = 1
	}
	return next
}

type MemoryStore struct {
	mu          sync.Mutex
	items       map[string]Checkpoint
	transitions []AuditEvent
}

func NewMemoryStore() *MemoryStore {
	return &MemoryStore{items: make(map[string]Checkpoint)}
}

func (s *MemoryStore) Save(_ context.Context, checkpoint Checkpoint, event AuditEvent) error {
	if s == nil {
		return fmt.Errorf("%w: nil memory store", ErrInvalidInput)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.items == nil {
		s.items = make(map[string]Checkpoint)
	}
	s.items[checkpointKey(checkpoint.RunID, checkpoint.StepID)] = cloneCheckpoint(checkpoint)
	s.transitions = append(s.transitions, cloneAuditEvent(event))
	return nil
}

func (s *MemoryStore) Get(_ context.Context, runID, stepID string) (Checkpoint, bool, error) {
	if s == nil {
		return Checkpoint{}, false, fmt.Errorf("%w: nil memory store", ErrInvalidInput)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	item, ok := s.items[checkpointKey(runID, stepID)]
	if !ok {
		return Checkpoint{}, false, nil
	}
	return cloneCheckpoint(item), true, nil
}

func (s *MemoryStore) ListRun(_ context.Context, runID string) ([]Checkpoint, error) {
	if s == nil {
		return nil, fmt.Errorf("%w: nil memory store", ErrInvalidInput)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []Checkpoint
	for _, item := range s.items {
		if item.RunID == runID {
			out = append(out, cloneCheckpoint(item))
		}
	}
	sortCheckpoints(out)
	return out, nil
}

type SQLStore struct {
	db *sql.DB
}

func NewSQLStore(ctx context.Context, db *sql.DB) (*SQLStore, error) {
	if db == nil {
		return nil, fmt.Errorf("%w: nil db", ErrInvalidInput)
	}
	store := &SQLStore{db: db}
	if err := store.ensureSchema(ctx); err != nil {
		return nil, err
	}
	return store, nil
}

func (s *SQLStore) ensureSchema(ctx context.Context) error {
	stmts := []string{
		`CREATE TABLE IF NOT EXISTS onboarding_checkpoints (
			run_id TEXT NOT NULL,
			step_id TEXT NOT NULL,
			project_id TEXT NOT NULL,
			step_label TEXT NOT NULL,
			status TEXT NOT NULL,
			attempt INTEGER NOT NULL,
			started_at TEXT NOT NULL,
			completed_at TEXT NOT NULL,
			updated_at TEXT NOT NULL,
			evidence_refs_json TEXT NOT NULL,
			failure_reason TEXT NOT NULL,
			resume_hint TEXT NOT NULL,
			schema_version INTEGER NOT NULL,
			PRIMARY KEY (run_id, step_id)
		)`,
		`CREATE TABLE IF NOT EXISTS onboarding_checkpoint_transitions (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			event_id TEXT NOT NULL UNIQUE,
			run_id TEXT NOT NULL,
			step_id TEXT NOT NULL,
			project_id TEXT NOT NULL,
			from_status TEXT NOT NULL,
			to_status TEXT NOT NULL,
			actor_json TEXT NOT NULL,
			reason TEXT NOT NULL,
			evidence_refs_json TEXT NOT NULL,
			data_json TEXT NOT NULL,
			at TEXT NOT NULL,
			schema_version INTEGER NOT NULL
		)`,
		`CREATE INDEX IF NOT EXISTS idx_onboarding_checkpoints_run_updated
			ON onboarding_checkpoints (run_id, updated_at)`,
	}
	for _, stmt := range stmts {
		if _, err := s.db.ExecContext(ctx, stmt); err != nil {
			return fmt.Errorf("checkpoints: ensure schema: %w", err)
		}
	}
	return nil
}

func (s *SQLStore) Save(ctx context.Context, checkpoint Checkpoint, event AuditEvent) error {
	if s == nil {
		return fmt.Errorf("%w: nil sql store", ErrInvalidInput)
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("checkpoints: begin save: %w", err)
	}
	defer tx.Rollback()

	evidenceJSON, err := marshalStringSlice(checkpoint.EvidenceRefs)
	if err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO onboarding_checkpoints
		(run_id, step_id, project_id, step_label, status, attempt, started_at, completed_at, updated_at,
		 evidence_refs_json, failure_reason, resume_hint, schema_version)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(run_id, step_id) DO UPDATE SET
			project_id = excluded.project_id,
			step_label = excluded.step_label,
			status = excluded.status,
			attempt = excluded.attempt,
			started_at = excluded.started_at,
			completed_at = excluded.completed_at,
			updated_at = excluded.updated_at,
			evidence_refs_json = excluded.evidence_refs_json,
			failure_reason = excluded.failure_reason,
			resume_hint = excluded.resume_hint,
			schema_version = excluded.schema_version`,
		checkpoint.RunID, checkpoint.StepID, checkpoint.ProjectID, checkpoint.StepLabel, checkpoint.Status,
		checkpoint.Attempt, formatTimePtr(checkpoint.StartedAt), formatTimePtr(checkpoint.CompletedAt),
		formatTime(checkpoint.UpdatedAt), string(evidenceJSON), checkpoint.FailureReason, checkpoint.ResumeHint,
		checkpoint.SchemaVersion); err != nil {
		return fmt.Errorf("checkpoints: save checkpoint: %w", err)
	}
	actorJSON, err := json.Marshal(event.Actor)
	if err != nil {
		return fmt.Errorf("checkpoints: marshal actor: %w", err)
	}
	eventEvidenceJSON, err := marshalStringSlice(event.EvidenceRefs)
	if err != nil {
		return err
	}
	dataJSON, err := json.Marshal(event.Data)
	if err != nil {
		return fmt.Errorf("checkpoints: marshal event data: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO onboarding_checkpoint_transitions
		(event_id, run_id, step_id, project_id, from_status, to_status, actor_json, reason,
		 evidence_refs_json, data_json, at, schema_version)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		event.EventID, event.RunID, event.StepID, event.ProjectID, event.FromStatus, event.ToStatus,
		string(actorJSON), event.Reason, string(eventEvidenceJSON), string(dataJSON), formatTime(event.At),
		event.SchemaVersion); err != nil {
		return fmt.Errorf("checkpoints: save transition: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("checkpoints: commit save: %w", err)
	}
	return nil
}

func (s *SQLStore) Get(ctx context.Context, runID, stepID string) (Checkpoint, bool, error) {
	if s == nil {
		return Checkpoint{}, false, fmt.Errorf("%w: nil sql store", ErrInvalidInput)
	}
	row := s.db.QueryRowContext(ctx, `SELECT run_id, step_id, project_id, step_label, status, attempt,
		started_at, completed_at, updated_at, evidence_refs_json, failure_reason, resume_hint, schema_version
		FROM onboarding_checkpoints WHERE run_id = ? AND step_id = ?`, runID, stepID)
	item, err := scanCheckpoint(row)
	if errors.Is(err, sql.ErrNoRows) {
		return Checkpoint{}, false, nil
	}
	if err != nil {
		return Checkpoint{}, false, err
	}
	return item, true, nil
}

func (s *SQLStore) ListRun(ctx context.Context, runID string) ([]Checkpoint, error) {
	if s == nil {
		return nil, fmt.Errorf("%w: nil sql store", ErrInvalidInput)
	}
	rows, err := s.db.QueryContext(ctx, `SELECT run_id, step_id, project_id, step_label, status, attempt,
		started_at, completed_at, updated_at, evidence_refs_json, failure_reason, resume_hint, schema_version
		FROM onboarding_checkpoints WHERE run_id = ? ORDER BY updated_at ASC, step_id ASC`, runID)
	if err != nil {
		return nil, fmt.Errorf("checkpoints: list run: %w", err)
	}
	defer rows.Close()
	var out []Checkpoint
	for rows.Next() {
		item, err := scanCheckpoint(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("checkpoints: list rows: %w", err)
	}
	return out, nil
}

type checkpointScanner interface {
	Scan(...any) error
}

func scanCheckpoint(row checkpointScanner) (Checkpoint, error) {
	var item Checkpoint
	var started, completed, updated, evidenceJSON, status string
	if err := row.Scan(&item.RunID, &item.StepID, &item.ProjectID, &item.StepLabel, &status, &item.Attempt,
		&started, &completed, &updated, &evidenceJSON, &item.FailureReason, &item.ResumeHint,
		&item.SchemaVersion); err != nil {
		return Checkpoint{}, err
	}
	item.Status = Status(status)
	startedAt, err := parseTimePtr(started)
	if err != nil {
		return Checkpoint{}, err
	}
	completedAt, err := parseTimePtr(completed)
	if err != nil {
		return Checkpoint{}, err
	}
	updatedAt, err := parseTime(updated)
	if err != nil {
		return Checkpoint{}, err
	}
	item.StartedAt = startedAt
	item.CompletedAt = completedAt
	item.UpdatedAt = updatedAt
	if err := json.Unmarshal([]byte(evidenceJSON), &item.EvidenceRefs); err != nil {
		return Checkpoint{}, fmt.Errorf("checkpoints: decode evidence refs: %w", err)
	}
	return item, nil
}

func randomEventID() (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	return "evt_" + hex.EncodeToString(b[:]), nil
}

func checkpointKey(runID, stepID string) string {
	return runID + "\x00" + stepID
}

func safeID(value string) bool {
	if value == "" || len(value) > 128 || strings.Contains(value, "..") {
		return false
	}
	for _, r := range value {
		switch {
		case r >= 'a' && r <= 'z':
		case r >= 'A' && r <= 'Z':
		case r >= '0' && r <= '9':
		case r == '.', r == '_', r == '-':
		default:
			return false
		}
	}
	return true
}

func safeStepID(value string) bool {
	if !safeID(value) {
		return false
	}
	return !strings.HasPrefix(value, ".") && !strings.HasSuffix(value, ".")
}

func cleanStrings(values []string) []string {
	out := make([]string, 0, len(values))
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	sort.Strings(out)
	return out
}

func mergeStrings(first, second []string) []string {
	return cleanStrings(append(cloneStrings(first), second...))
}

func cloneStrings(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	out := make([]string, len(values))
	copy(out, values)
	return out
}

func chooseString(first, second string) string {
	if strings.TrimSpace(first) != "" {
		return first
	}
	return second
}

func timePtr(value time.Time) *time.Time {
	value = value.UTC()
	return &value
}

func cloneTimePtr(value *time.Time) *time.Time {
	if value == nil {
		return nil
	}
	return timePtr(*value)
}

func cloneCheckpoint(item Checkpoint) Checkpoint {
	item.StartedAt = cloneTimePtr(item.StartedAt)
	item.CompletedAt = cloneTimePtr(item.CompletedAt)
	item.EvidenceRefs = cloneStrings(item.EvidenceRefs)
	return item
}

func cloneAuditEvent(event AuditEvent) AuditEvent {
	event.EvidenceRefs = cloneStrings(event.EvidenceRefs)
	if event.Data != nil {
		data := make(map[string]any, len(event.Data))
		for key, value := range event.Data {
			data[key] = value
		}
		event.Data = data
	}
	return event
}

func sortCheckpoints(items []Checkpoint) {
	sort.SliceStable(items, func(i, j int) bool {
		if !items[i].UpdatedAt.Equal(items[j].UpdatedAt) {
			return items[i].UpdatedAt.Before(items[j].UpdatedAt)
		}
		return items[i].StepID < items[j].StepID
	})
}

func marshalStringSlice(values []string) ([]byte, error) {
	data, err := json.Marshal(cleanStrings(values))
	if err != nil {
		return nil, fmt.Errorf("checkpoints: marshal string slice: %w", err)
	}
	return data, nil
}

func formatTimePtr(value *time.Time) string {
	if value == nil {
		return ""
	}
	return formatTime(*value)
}

func formatTime(value time.Time) string {
	if value.IsZero() {
		return ""
	}
	return value.UTC().Format(time.RFC3339Nano)
}

func parseTimePtr(value string) (*time.Time, error) {
	if strings.TrimSpace(value) == "" {
		return nil, nil
	}
	parsed, err := parseTime(value)
	if err != nil {
		return nil, err
	}
	return &parsed, nil
}

func parseTime(value string) (time.Time, error) {
	parsed, err := time.Parse(time.RFC3339Nano, value)
	if err != nil {
		return time.Time{}, fmt.Errorf("checkpoints: parse time %q: %w", value, err)
	}
	return parsed.UTC(), nil
}
