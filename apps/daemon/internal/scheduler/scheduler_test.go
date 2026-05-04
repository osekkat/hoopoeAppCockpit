package scheduler

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestRegistryIntervalSelectionSurvivesRestartAndReclaimsLease(t *testing.T) {
	ctx := context.Background()
	clock := newTestClock(time.Date(2026, 5, 4, 10, 0, 0, 0, time.UTC))
	store := FileStore{Path: filepath.Join(t.TempDir(), "scheduler-state.json")}
	registry, err := NewRegistry(ctx, RegistryConfig{
		Store:       store,
		Now:         clock.Now,
		LeaseHolder: "daemon-a",
		LeaseTTL:    time.Minute,
	})
	if err != nil {
		t.Fatal(err)
	}
	definition := testDefinition("health", Schedule{Type: ScheduleInterval, Interval: 5 * time.Minute})
	if _, err := registry.ImportDefinition(ctx, definition); err != nil {
		t.Fatal(err)
	}

	clock.Advance(5 * time.Minute)
	runs, decisions, err := registry.SelectDue(ctx, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(runs) != 1 || len(decisions) != 1 {
		t.Fatalf("expected one started run and one decision, got runs=%d decisions=%d", len(runs), len(decisions))
	}
	if decisions[0].Outcome != OutcomeStarted {
		t.Fatalf("expected started outcome, got %s", decisions[0].Outcome)
	}
	startedRunID := runs[0].ID

	clock.Advance(2 * time.Minute)
	restarted, err := NewRegistry(ctx, RegistryConfig{
		Store:       store,
		Now:         clock.Now,
		LeaseHolder: "daemon-b",
		LeaseTTL:    time.Minute,
	})
	if err != nil {
		t.Fatal(err)
	}
	run, err := restarted.GetRun(ctx, startedRunID)
	if err != nil {
		t.Fatal(err)
	}
	if run.Status != RunStatusInterrupted {
		t.Fatalf("expected expired run to be interrupted after restart, got %s", run.Status)
	}
	job, err := restarted.GetJob(ctx, "health")
	if err != nil {
		t.Fatal(err)
	}
	if job.Lease != nil {
		t.Fatalf("expected expired job lease to be cleared, got %#v", job.Lease)
	}
	state, err := restarted.Snapshot(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if state.Metrics.LeaseSteals != 1 {
		t.Fatalf("expected one reclaimed lease, got %d", state.Metrics.LeaseSteals)
	}
}

func TestSchedulerEventDedupeAndOnDemandAudit(t *testing.T) {
	ctx := context.Background()
	clock := newTestClock(time.Date(2026, 5, 4, 11, 0, 0, 0, time.UTC))
	registry := newTestRegistry(t, clock)
	if _, err := registry.ImportDefinition(ctx, testDefinition("mail-watch", Schedule{Type: ScheduleEvent, Event: "agent_mail.received"})); err != nil {
		t.Fatal(err)
	}
	if _, err := registry.ImportDefinition(ctx, testDefinition("manual-scan", Schedule{Type: ScheduleOnDemand})); err != nil {
		t.Fatal(err)
	}
	var runCount atomic.Int32
	audit := &recordingAudit{}
	scheduler, err := New(Config{
		Registry: registry,
		Runner: RunnerFunc(func(context.Context, Run) (RunResult, error) {
			runCount.Add(1)
			return RunResult{WakeAgent: false, Context: map[string]any{"checked": true}}, nil
		}),
		AuditSink:  audit,
		MaxWorkers: 2,
	})
	if err != nil {
		t.Fatal(err)
	}

	decisions, err := scheduler.EmitEvent(ctx, "agent_mail.received", "msg-1", map[string]string{"thread": "hoopoe-phase2"})
	if err != nil {
		t.Fatal(err)
	}
	if len(decisions) != 1 || decisions[0].Outcome != OutcomeStarted {
		t.Fatalf("expected event to start one run, got %#v", decisions)
	}
	scheduler.Wait()
	if runCount.Load() != 1 {
		t.Fatalf("expected one runner call after first event, got %d", runCount.Load())
	}

	decisions, err = scheduler.EmitEvent(ctx, "agent_mail.received", "msg-1", nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(decisions) != 1 || decisions[0].Outcome != OutcomeSkippedByPolicy {
		t.Fatalf("expected duplicate event to be skipped by policy, got %#v", decisions)
	}
	scheduler.Wait()
	if runCount.Load() != 1 {
		t.Fatalf("duplicate event should not dispatch runner, got %d calls", runCount.Load())
	}

	decision, err := scheduler.RunNow(ctx, "manual-scan")
	if err != nil {
		t.Fatal(err)
	}
	if decision.Outcome != OutcomeStarted {
		t.Fatalf("expected on-demand run to start, got %s", decision.Outcome)
	}
	scheduler.Wait()
	if runCount.Load() != 2 {
		t.Fatalf("expected on-demand runner call, got %d total calls", runCount.Load())
	}
	if got := audit.Len(); got != 3 {
		t.Fatalf("expected audit for started, skipped duplicate, and on-demand decisions, got %d", got)
	}
}

func TestMisfirePolicySkipRecordsSkippedDecision(t *testing.T) {
	ctx := context.Background()
	clock := newTestClock(time.Date(2026, 5, 4, 12, 0, 0, 0, time.UTC))
	registry := newTestRegistry(t, clock)
	definition := testDefinition("budget-watch", Schedule{Type: ScheduleInterval, Interval: time.Minute})
	definition.MisfirePolicy = MisfireSkip
	definition.MisfireGrace = 30 * time.Second
	if _, err := registry.ImportDefinition(ctx, definition); err != nil {
		t.Fatal(err)
	}

	clock.Advance(10 * time.Minute)
	runs, decisions, err := registry.SelectDue(ctx, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(runs) != 0 {
		t.Fatalf("misfire skip should not return started runs, got %d", len(runs))
	}
	if len(decisions) != 1 || decisions[0].Outcome != OutcomeSkippedByMisfirePolicy {
		t.Fatalf("expected skipped_by_misfire_policy decision, got %#v", decisions)
	}
	job, err := registry.GetJob(ctx, "budget-watch")
	if err != nil {
		t.Fatal(err)
	}
	if job.NextRunAt == nil || !job.NextRunAt.After(clock.Now()) {
		t.Fatalf("expected next run to advance after misfire skip, got %#v", job.NextRunAt)
	}
	state, err := registry.Snapshot(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if state.Metrics.Misfires != 1 || state.Metrics.SkippedRuns != 1 {
		t.Fatalf("expected one misfire and one skipped run, got metrics=%#v", state.Metrics)
	}
}

func TestDeadLetterAfterFailedRuns(t *testing.T) {
	ctx := context.Background()
	clock := newTestClock(time.Date(2026, 5, 4, 13, 0, 0, 0, time.UTC))
	registry := newTestRegistry(t, clock)
	definition := testDefinition("agent-gate", Schedule{Type: ScheduleOnDemand})
	definition.DeadLetterAfter = 2
	if _, err := registry.ImportDefinition(ctx, definition); err != nil {
		t.Fatal(err)
	}

	for i := 0; i < 2; i++ {
		run, decision, err := registry.RunNow(ctx, "agent-gate")
		if err != nil {
			t.Fatal(err)
		}
		if decision.Outcome != OutcomeStarted {
			t.Fatalf("expected failed attempt %d to start, got %s", i+1, decision.Outcome)
		}
		if _, err := registry.CompleteRun(ctx, run.ID, RunResult{}, errors.New("pre-script failed")); err != nil {
			t.Fatal(err)
		}
		clock.Advance(time.Second)
	}

	job, err := registry.GetJob(ctx, "agent-gate")
	if err != nil {
		t.Fatal(err)
	}
	if job.Status != JobStatusDeadLettered {
		t.Fatalf("expected job to be dead-lettered, got %s", job.Status)
	}
	_, decision, err := registry.RunNow(ctx, "agent-gate")
	if err != nil {
		t.Fatal(err)
	}
	if decision.Outcome != OutcomeDeadLettered {
		t.Fatalf("expected dead-lettered job to skip, got %s", decision.Outcome)
	}
	state, err := registry.Snapshot(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if state.Metrics.DeadLetters != 1 {
		t.Fatalf("expected one dead letter, got %d", state.Metrics.DeadLetters)
	}
}

func TestSchedulerWorkersAllowUnrelatedRunsToComplete(t *testing.T) {
	ctx := context.Background()
	clock := newTestClock(time.Date(2026, 5, 4, 14, 0, 0, 0, time.UTC))
	registry := newTestRegistry(t, clock)
	for _, id := range []string{"slow", "fast"} {
		if _, err := registry.ImportDefinition(ctx, testDefinition(id, Schedule{Type: ScheduleOnDemand})); err != nil {
			t.Fatal(err)
		}
	}
	slowStarted := make(chan struct{})
	releaseSlow := make(chan struct{})
	fastDone := make(chan struct{})
	scheduler, err := New(Config{
		Registry: registry,
		Runner: RunnerFunc(func(ctx context.Context, run Run) (RunResult, error) {
			switch run.JobID {
			case "slow":
				close(slowStarted)
				select {
				case <-releaseSlow:
					return RunResult{WakeAgent: false}, nil
				case <-ctx.Done():
					return RunResult{}, ctx.Err()
				}
			case "fast":
				close(fastDone)
				return RunResult{WakeAgent: false}, nil
			default:
				t.Fatalf("unexpected job %q", run.JobID)
			}
			return RunResult{}, nil
		}),
		MaxWorkers: 2,
	})
	if err != nil {
		t.Fatal(err)
	}

	if _, err := scheduler.RunNow(ctx, "slow"); err != nil {
		t.Fatal(err)
	}
	waitForSignal(t, slowStarted, "slow run to start")
	if _, err := scheduler.RunNow(ctx, "fast"); err != nil {
		t.Fatal(err)
	}
	waitForSignal(t, fastDone, "fast run to complete while slow run is blocked")
	close(releaseSlow)
	scheduler.Wait()
}

func TestDefinitionFilesRoundTrip(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	jsonPath := filepath.Join(dir, "cron.json")
	definition := testDefinition("cron-job", Schedule{Type: ScheduleCron, Cron: "*/15 * * * *"})
	definition.Timeout = 30 * time.Second
	definition.DeadLetterAfter = 3
	if err := WriteDefinitionFile(ctx, jsonPath, definition); err != nil {
		t.Fatal(err)
	}
	loadedJSON, err := LoadDefinitionFile(ctx, jsonPath)
	if err != nil {
		t.Fatal(err)
	}
	if loadedJSON.ID != "cron-job" || loadedJSON.Schedule.Type != ScheduleCron || loadedJSON.Schedule.Cron != "*/15 * * * *" {
		t.Fatalf("unexpected JSON definition: %#v", loadedJSON)
	}

	yamlPath := filepath.Join(dir, "event.yaml")
	yaml := []byte("id: yaml-job\nname: YAML Job\nkind: deterministic\nversion: 1\nschedule: on event: bead.closed\ntimeout: 45s\nmisfire_policy: skip\ndead_letter_after: 2\n")
	if err := os.WriteFile(yamlPath, yaml, 0o600); err != nil {
		t.Fatal(err)
	}
	definitions, err := LoadDefinitions(ctx, dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(definitions) != 2 {
		t.Fatalf("expected two loaded definitions, got %d", len(definitions))
	}
	byID := make(map[string]Definition, len(definitions))
	for _, def := range definitions {
		byID[def.ID] = def
	}
	loadedYAML := byID["yaml-job"]
	if loadedYAML.Schedule.Type != ScheduleEvent || loadedYAML.Schedule.Event != "bead.closed" {
		t.Fatalf("unexpected YAML schedule: %#v", loadedYAML.Schedule)
	}
	if loadedYAML.MisfirePolicy != MisfireSkip || loadedYAML.DeadLetterAfter != 2 {
		t.Fatalf("unexpected YAML policy fields: %#v", loadedYAML)
	}
}

type testClock struct {
	mu  sync.Mutex
	now time.Time
}

func newTestClock(now time.Time) *testClock {
	return &testClock{now: now.UTC()}
}

func (c *testClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *testClock) Advance(duration time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = c.now.Add(duration)
}

type recordingAudit struct {
	mu        sync.Mutex
	decisions []Decision
}

func (a *recordingAudit) RecordSchedulerDecision(_ context.Context, decision Decision) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.decisions = append(a.decisions, decision)
	return nil
}

func (a *recordingAudit) Len() int {
	a.mu.Lock()
	defer a.mu.Unlock()
	return len(a.decisions)
}

func newTestRegistry(t *testing.T, clock *testClock) *Registry {
	t.Helper()
	registry, err := NewRegistry(context.Background(), RegistryConfig{
		Store:       NewMemoryStore(),
		Now:         clock.Now,
		LeaseHolder: "test-daemon",
		LeaseTTL:    time.Minute,
	})
	if err != nil {
		t.Fatal(err)
	}
	return registry
}

func testDefinition(id string, schedule Schedule) Definition {
	return Definition{
		ID:       id,
		Name:     id,
		Kind:     KindDeterministic,
		Version:  SchemaVersion,
		Schedule: schedule,
		Repeat:   RepeatForever(),
		Timeout:  time.Minute,
	}
}

func waitForSignal(t *testing.T, signal <-chan struct{}, label string) {
	t.Helper()
	select {
	case <-signal:
	case <-time.After(2 * time.Second):
		t.Fatalf("timed out waiting for %s", label)
	}
}
