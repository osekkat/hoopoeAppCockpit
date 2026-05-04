package checkpoints

import (
	"context"
	"database/sql"
	"testing"
	"time"

	schemas "github.com/hoopoe-cockpit/hoopoe/packages/schemas/go"
	_ "modernc.org/sqlite"
)

func TestServiceTransitionsPersistTimelineAndAudit(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	now := time.Date(2026, 5, 4, 13, 0, 0, 0, time.UTC)
	audit := &recordingAudit{}
	service := NewService(Config{
		Audit: audit,
		Now:   func() time.Time { return now },
		NewID: func() (string, error) { return "evt_test", nil },
	})
	actorID := "wizard"
	result, err := service.Transition(ctx, TransitionRequest{
		RunID:        "run_123",
		StepID:       "acfs-install.doctor",
		ProjectID:    "proj_1",
		Status:       StatusFailed,
		Actor:        schemas.Actor{Kind: schemas.ActorKindSystem, Id: &actorID},
		EvidenceRefs: []string{"log:run_123:20", "log:run_123:20"},
		Reason:       "installer checkpoint failed",
		ResumeHint:   "/v1/bootstrap/acfs/resume",
	})
	if err != nil {
		t.Fatalf("Transition: %v", err)
	}
	if !result.Created || result.Checkpoint.Status != StatusFailed || result.Checkpoint.Attempt != 1 {
		t.Fatalf("unexpected transition result: %+v", result)
	}
	if len(audit.events) != 1 || audit.events[0].ToStatus != StatusFailed {
		t.Fatalf("audit events = %+v", audit.events)
	}

	timeline, err := service.Timeline(ctx, "run_123")
	if err != nil {
		t.Fatalf("Timeline: %v", err)
	}
	if len(timeline.Checkpoints) != 1 {
		t.Fatalf("checkpoint count = %d", len(timeline.Checkpoints))
	}
	if len(timeline.Actions) != 1 || len(timeline.Actions[0].Actions) < 4 {
		t.Fatalf("repair actions = %+v", timeline.Actions)
	}
}

func TestSQLStoreSurvivesRestart(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db, err := sql.Open("sqlite", "file:"+t.Name()+"?mode=memory&cache=shared")
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	defer db.Close()
	store, err := NewSQLStore(ctx, db)
	if err != nil {
		t.Fatalf("NewSQLStore: %v", err)
	}
	service := NewService(Config{
		Store: store,
		Now:   func() time.Time { return time.Date(2026, 5, 4, 13, 1, 0, 0, time.UTC) },
		NewID: func() (string, error) { return "evt_sql_1", nil },
	})
	if _, err := service.Transition(ctx, TransitionRequest{
		RunID:        "run_sql",
		StepID:       "tool-inventory.caam",
		Status:       StatusRunning,
		EvidenceRefs: []string{"probe:caam"},
	}); err != nil {
		t.Fatalf("Transition running: %v", err)
	}
	service.newID = func() (string, error) { return "evt_sql_2", nil }
	if _, err := service.Transition(ctx, TransitionRequest{
		RunID:  "run_sql",
		StepID: "tool-inventory.caam",
		Status: StatusSucceeded,
	}); err != nil {
		t.Fatalf("Transition succeeded: %v", err)
	}

	restarted, err := NewSQLStore(ctx, db)
	if err != nil {
		t.Fatalf("restart NewSQLStore: %v", err)
	}
	items, err := restarted.ListRun(ctx, "run_sql")
	if err != nil {
		t.Fatalf("ListRun: %v", err)
	}
	if len(items) != 1 || items[0].Status != StatusSucceeded || items[0].StartedAt == nil || items[0].CompletedAt == nil {
		t.Fatalf("unexpected persisted checkpoint: %+v", items)
	}
}

func TestRetryFromFailureIncrementsAttempt(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	times := []time.Time{
		time.Date(2026, 5, 4, 13, 0, 0, 0, time.UTC),
		time.Date(2026, 5, 4, 13, 1, 0, 0, time.UTC),
	}
	index := 0
	service := NewService(Config{
		Now: func() time.Time {
			value := times[index]
			index++
			return value
		},
		NewID: func() (string, error) { return "evt_retry", nil },
	})
	if _, err := service.Transition(ctx, TransitionRequest{RunID: "run_retry", StepID: "daemon.install", Status: StatusFailed}); err != nil {
		t.Fatalf("fail transition: %v", err)
	}
	result, err := service.Transition(ctx, TransitionRequest{RunID: "run_retry", StepID: "daemon.install", Status: StatusRunning})
	if err != nil {
		t.Fatalf("retry transition: %v", err)
	}
	if result.Checkpoint.Attempt != 2 || result.Checkpoint.CompletedAt != nil {
		t.Fatalf("retry checkpoint = %+v", result.Checkpoint)
	}
}

type recordingAudit struct {
	events []AuditEvent
}

func (r *recordingAudit) RecordCheckpointTransition(_ context.Context, event AuditEvent) error {
	r.events = append(r.events, cloneAuditEvent(event))
	return nil
}
