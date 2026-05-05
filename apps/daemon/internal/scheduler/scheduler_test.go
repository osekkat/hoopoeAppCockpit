package scheduler

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/hoopoe-cockpit/hoopoe/apps/daemon/internal/redaction"
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

func TestPauseResumePersistsAcrossRestart(t *testing.T) {
	ctx := context.Background()
	clock := newTestClock(time.Date(2026, 5, 4, 13, 30, 0, 0, time.UTC))
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
	if _, err := registry.ImportDefinition(ctx, testDefinition("snapshot-health", Schedule{Type: ScheduleInterval, Interval: time.Minute})); err != nil {
		t.Fatal(err)
	}
	if _, err := registry.PauseJob(ctx, "snapshot-health"); err != nil {
		t.Fatal(err)
	}

	restarted, err := NewRegistry(ctx, RegistryConfig{
		Store:       store,
		Now:         clock.Now,
		LeaseHolder: "daemon-b",
		LeaseTTL:    time.Minute,
	})
	if err != nil {
		t.Fatal(err)
	}
	job, err := restarted.GetJob(ctx, "snapshot-health")
	if err != nil {
		t.Fatal(err)
	}
	if job.Status != JobStatusPaused || !job.Definition.Paused {
		t.Fatalf("expected paused job after restart, got status=%s paused=%t", job.Status, job.Definition.Paused)
	}

	clock.Advance(2 * time.Minute)
	runs, decisions, err := restarted.SelectDue(ctx, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(runs) != 0 || len(decisions) != 1 || decisions[0].Outcome != OutcomePaused {
		t.Fatalf("expected paused due job to skip without dispatch, runs=%d decisions=%#v", len(runs), decisions)
	}

	resumed, err := restarted.ResumeJob(ctx, "snapshot-health")
	if err != nil {
		t.Fatal(err)
	}
	if resumed.Status != JobStatusReady || resumed.Definition.Paused {
		t.Fatalf("expected resumed ready job, got status=%s paused=%t", resumed.Status, resumed.Definition.Paused)
	}
}

func TestRemoveJobDropsRuntimeEntryAndEventDedupe(t *testing.T) {
	ctx := context.Background()
	clock := newTestClock(time.Date(2026, 5, 4, 13, 45, 0, 0, time.UTC))
	registry := newTestRegistry(t, clock)
	if _, err := registry.ImportDefinition(ctx, testDefinition("mail-watch", Schedule{Type: ScheduleEvent, Event: "agent_mail.received"})); err != nil {
		t.Fatal(err)
	}
	if _, _, err := registry.EmitEvent(ctx, "agent_mail.received", "msg-1", nil); err != nil {
		t.Fatal(err)
	}
	if _, err := registry.RemoveJob(ctx, "mail-watch"); err != nil {
		t.Fatal(err)
	}
	if _, err := registry.GetJob(ctx, "mail-watch"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected removed job to be missing, got %v", err)
	}
	state, err := registry.Snapshot(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(state.EventDedupe) != 0 {
		t.Fatalf("expected remove to drop event dedupe keys, got %#v", state.EventDedupe)
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

func TestSchedulerWorkerAcquireHonorsCallerCancellation(t *testing.T) {
	ctx := context.Background()
	clock := newTestClock(time.Date(2026, 5, 4, 14, 15, 0, 0, time.UTC))
	registry := newTestRegistry(t, clock)
	for _, id := range []string{"slow", "queued"} {
		if _, err := registry.ImportDefinition(ctx, testDefinition(id, Schedule{Type: ScheduleOnDemand})); err != nil {
			t.Fatal(err)
		}
	}
	slowStarted := make(chan struct{})
	releaseSlow := make(chan struct{})
	queuedRan := make(chan struct{})
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
			case "queued":
				close(queuedRan)
				return RunResult{WakeAgent: false}, nil
			default:
				t.Fatalf("unexpected job %q", run.JobID)
			}
			return RunResult{}, nil
		}),
		MaxWorkers: 1,
	})
	if err != nil {
		t.Fatal(err)
	}

	if _, err := scheduler.RunNow(ctx, "slow"); err != nil {
		t.Fatal(err)
	}
	waitForSignal(t, slowStarted, "slow run to occupy the worker")

	queuedCtx, cancelQueued := context.WithCancel(ctx)
	decision, err := scheduler.RunNow(queuedCtx, "queued")
	if err != nil {
		t.Fatal(err)
	}
	cancelQueued()
	run := waitForRunStatus(t, registry, decision.RunID, RunStatusFailed)
	if !strings.Contains(run.Error, context.Canceled.Error()) {
		t.Fatalf("queued run error = %q, want context canceled", run.Error)
	}
	select {
	case <-queuedRan:
		t.Fatal("queued runner executed after caller cancellation")
	default:
	}

	close(releaseSlow)
	scheduler.Wait()
}

func TestSchedulerStopCancelsRunningRunner(t *testing.T) {
	ctx := context.Background()
	clock := newTestClock(time.Date(2026, 5, 4, 14, 30, 0, 0, time.UTC))
	registry := newTestRegistry(t, clock)
	if _, err := registry.ImportDefinition(ctx, testDefinition("root-cancel", Schedule{Type: ScheduleOnDemand})); err != nil {
		t.Fatal(err)
	}
	started := make(chan struct{})
	scheduler, err := New(Config{
		Registry: registry,
		Runner: RunnerFunc(func(ctx context.Context, run Run) (RunResult, error) {
			if run.JobID != "root-cancel" {
				t.Fatalf("unexpected job %q", run.JobID)
			}
			close(started)
			<-ctx.Done()
			return RunResult{}, ctx.Err()
		}),
		MaxWorkers: 1,
	})
	if err != nil {
		t.Fatal(err)
	}

	decision, err := scheduler.RunNow(ctx, "root-cancel")
	if err != nil {
		t.Fatal(err)
	}
	waitForSignal(t, started, "running job")
	scheduler.Stop()
	waitCtx, cancelWait := context.WithTimeout(ctx, 2*time.Second)
	defer cancelWait()
	if err := scheduler.WaitContext(waitCtx); err != nil {
		t.Fatalf("scheduler wait after stop: %v", err)
	}
	run := waitForRunStatus(t, registry, decision.RunID, RunStatusFailed)
	if !strings.Contains(run.Error, context.Canceled.Error()) {
		t.Fatalf("stopped run error = %q, want context canceled", run.Error)
	}
}

func TestSchedulerWaitContextHonorsCallerCancellation(t *testing.T) {
	ctx := context.Background()
	clock := newTestClock(time.Date(2026, 5, 4, 14, 45, 0, 0, time.UTC))
	registry := newTestRegistry(t, clock)
	if _, err := registry.ImportDefinition(ctx, testDefinition("blocked", Schedule{Type: ScheduleOnDemand})); err != nil {
		t.Fatal(err)
	}
	started := make(chan struct{})
	release := make(chan struct{})
	scheduler, err := New(Config{
		Registry: registry,
		Runner: RunnerFunc(func(context.Context, Run) (RunResult, error) {
			close(started)
			<-release
			return RunResult{WakeAgent: false}, nil
		}),
		MaxWorkers: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := scheduler.RunNow(ctx, "blocked"); err != nil {
		t.Fatal(err)
	}
	waitForSignal(t, started, "blocked run")

	waitCtx, cancelWait := context.WithTimeout(ctx, time.Nanosecond)
	defer cancelWait()
	if err := scheduler.WaitContext(waitCtx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("WaitContext error = %v, want deadline exceeded", err)
	}
	close(release)
	scheduler.Wait()
}

func TestSchedulerRecoversRunnerPanic(t *testing.T) {
	ctx := context.Background()
	clock := newTestClock(time.Date(2026, 5, 4, 15, 0, 0, 0, time.UTC))
	registry := newTestRegistry(t, clock)
	if _, err := registry.ImportDefinition(ctx, testDefinition("panicker", Schedule{Type: ScheduleOnDemand})); err != nil {
		t.Fatal(err)
	}
	if _, err := registry.ImportDefinition(ctx, testDefinition("survivor", Schedule{Type: ScheduleOnDemand})); err != nil {
		t.Fatal(err)
	}
	survivorDone := make(chan struct{})
	scheduler, err := New(Config{
		Registry: registry,
		Runner: RunnerFunc(func(_ context.Context, run Run) (RunResult, error) {
			switch run.JobID {
			case "panicker":
				panic("synthetic runner failure")
			case "survivor":
				close(survivorDone)
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

	panicDecision, err := scheduler.RunNow(ctx, "panicker")
	if err != nil {
		t.Fatal(err)
	}
	run := waitForRunStatus(t, registry, panicDecision.RunID, RunStatusFailed)
	if !strings.Contains(run.Error, "panic recovered") {
		t.Fatalf("panicker run error = %q, want substring %q", run.Error, "panic recovered")
	}
	if !strings.Contains(run.Error, "synthetic runner failure") {
		t.Fatalf("panicker run error = %q, want recovered value in message", run.Error)
	}

	if _, err := scheduler.RunNow(ctx, "survivor"); err != nil {
		t.Fatalf("scheduler did not survive prior runner panic: %v", err)
	}
	waitForSignal(t, survivorDone, "survivor run after recovered panic")
	scheduler.Wait()
}

// panickingStore wraps a MemoryStore and panics on Save when armed. Used to
// prove the dispatch goroutine's boundary recover survives a Store.Save
// panic that would otherwise crash the daemon. Load and the unarmed Save
// path delegate to the inner store so registry construction + ImportDefinition
// succeed.
type panickingStore struct {
	inner *MemoryStore
	armed atomic.Bool
}

func newPanickingStore() *panickingStore {
	return &panickingStore{inner: NewMemoryStore()}
}

func (p *panickingStore) Load(ctx context.Context) (State, error) {
	return p.inner.Load(ctx)
}

func (p *panickingStore) Save(ctx context.Context, state State) error {
	if p.armed.Load() {
		panic("synthetic store save panic")
	}
	return p.inner.Save(ctx, state)
}

func (p *panickingStore) arm()    { p.armed.Store(true) }
func (p *panickingStore) disarm() { p.armed.Store(false) }

func TestSchedulerRecoversCompleteRunPanic(t *testing.T) {
	ctx := context.Background()
	clock := newTestClock(time.Date(2026, 5, 4, 15, 30, 0, 0, time.UTC))
	store := newPanickingStore()
	registry, err := NewRegistry(ctx, RegistryConfig{
		Store:       store,
		Now:         clock.Now,
		LeaseHolder: "test-daemon",
		LeaseTTL:    time.Minute,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := registry.ImportDefinition(ctx, testDefinition("crash-on-complete", Schedule{Type: ScheduleOnDemand})); err != nil {
		t.Fatal(err)
	}
	if _, err := registry.ImportDefinition(ctx, testDefinition("after-panic", Schedule{Type: ScheduleOnDemand})); err != nil {
		t.Fatal(err)
	}
	armNow := make(chan struct{})
	releaseRunner := make(chan struct{})
	survivorDone := make(chan struct{})
	scheduler, err := New(Config{
		Registry: registry,
		Runner: RunnerFunc(func(_ context.Context, run Run) (RunResult, error) {
			switch run.JobID {
			case "crash-on-complete":
				// Signal the test to arm the store, then wait for release
				// so that the runner returns AFTER the store will panic on
				// the dispatch goroutine's completeRun → persistLocked → Save.
				close(armNow)
				<-releaseRunner
			case "after-panic":
				close(survivorDone)
			}
			return RunResult{WakeAgent: false}, nil
		}),
		MaxWorkers: 2,
	})
	if err != nil {
		t.Fatal(err)
	}

	// Step 1: dispatch the run while the store is unarmed so Registry.RunNow's
	// own persistLocked succeeds. The dispatch goroutine starts the runner,
	// which blocks on releaseRunner.
	if _, err := scheduler.RunNow(ctx, "crash-on-complete"); err != nil {
		t.Fatalf("RunNow on unarmed store: %v", err)
	}
	waitForSignal(t, armNow, "runner reached arm-now signal")

	// Step 2: arm the store so completeRun's persistLocked panics in the
	// dispatch goroutine. The recoverDispatch boundary recover must catch
	// it and the inner recover in recoverDispatch must absorb the second
	// panic from the best-effort completeRun call.
	store.arm()
	close(releaseRunner)

	// Wait for the dispatch goroutine to finish (with its panic recovered).
	// scheduler.Wait blocks until the active-run counter reaches zero, which
	// happens inside the deferred finishRun call at the top of dispatch.
	scheduler.Wait()

	// Step 3: prove the daemon survived by running another job successfully.
	store.disarm()
	if _, err := scheduler.RunNow(ctx, "after-panic"); err != nil {
		t.Fatalf("scheduler did not survive prior store-save panic: %v", err)
	}
	waitForSignal(t, survivorDone, "after-panic run after recovered store-save panic")
	scheduler.Wait()
}

func TestSchedulerWaitContextDoesNotLeakWaitersOnCallerCancellation(t *testing.T) {
	ctx := context.Background()
	clock := newTestClock(time.Date(2026, 5, 4, 15, 45, 0, 0, time.UTC))
	registry := newTestRegistry(t, clock)
	if _, err := registry.ImportDefinition(ctx, testDefinition("blocked-wait", Schedule{Type: ScheduleOnDemand})); err != nil {
		t.Fatal(err)
	}
	started := make(chan struct{})
	release := make(chan struct{})
	scheduler, err := New(Config{
		Registry: registry,
		Runner: RunnerFunc(func(context.Context, Run) (RunResult, error) {
			close(started)
			<-release
			return RunResult{WakeAgent: false}, nil
		}),
		MaxWorkers: 1,
	})
	if err != nil {
		t.Fatal(err)
	}

	if _, err := scheduler.RunNow(ctx, "blocked-wait"); err != nil {
		t.Fatal(err)
	}
	waitForSignal(t, started, "blocked wait run")

	baseline := runtime.NumGoroutine()
	for i := 0; i < 40; i++ {
		waitCtx, cancelWait := context.WithCancel(ctx)
		cancelWait()
		if err := scheduler.WaitContext(waitCtx); !errors.Is(err, context.Canceled) {
			t.Fatalf("WaitContext attempt %d error = %v, want context canceled", i, err)
		}
	}
	runtime.Gosched()

	scheduler.waitMu.Lock()
	activeRuns := scheduler.activeRuns
	waiters := len(scheduler.waiters)
	scheduler.waitMu.Unlock()
	if activeRuns != 1 {
		t.Fatalf("activeRuns = %d, want 1 blocked run", activeRuns)
	}
	if waiters != 0 {
		t.Fatalf("waiters after canceled WaitContext calls = %d, want 0", waiters)
	}
	if after := runtime.NumGoroutine(); after > baseline+5 {
		t.Fatalf("goroutines after canceled WaitContext calls = %d, baseline %d; likely leaked waiters", after, baseline)
	}

	close(release)
	scheduler.Wait()
	if err := scheduler.WaitContext(ctx); err != nil {
		t.Fatalf("WaitContext after run release returned %v, want nil", err)
	}
}

// TestSchedulerRecoversRunnerPanicRedactsValue guards hp-dqxs: a
// panicking runner can pass arbitrary data to panic() — including
// errors that wrap secret-shaped strings. Without redaction, the
// secret renders verbatim into run.Error and lands in the on-disk
// scheduler state plus the audit log. With Config.Redactor set, the
// recovered value must be scrubbed before formatting into the error.
func TestSchedulerRecoversRunnerPanicRedactsValue(t *testing.T) {
	const secret = "sk-ant-api03-abcdefghijklmnopqrstuvwxyz0123456789"
	ctx := context.Background()
	clock := newTestClock(time.Date(2026, 5, 4, 18, 0, 0, 0, time.UTC))
	registry := newTestRegistry(t, clock)
	if _, err := registry.ImportDefinition(ctx, testDefinition("redact-panic", Schedule{Type: ScheduleOnDemand})); err != nil {
		t.Fatal(err)
	}
	scheduler, err := New(Config{
		Registry: registry,
		Runner: RunnerFunc(func(context.Context, Run) (RunResult, error) {
			panic(fmt.Errorf("upstream auth: %s", secret))
			// unreachable, but Go's flow analysis requires a return
			return RunResult{}, nil
		}),
		Redactor:   redaction.NewDefault(),
		MaxWorkers: 1,
	})
	if err != nil {
		t.Fatal(err)
	}

	decision, err := scheduler.RunNow(ctx, "redact-panic")
	if err != nil {
		t.Fatalf("RunNow: %v", err)
	}
	run := waitForRunStatus(t, registry, decision.RunID, RunStatusFailed)
	if !strings.Contains(run.Error, "panic recovered") {
		t.Fatalf("run.Error = %q, want substring %q", run.Error, "panic recovered")
	}
	if strings.Contains(run.Error, secret) {
		t.Fatalf("run.Error leaked raw secret: %q", run.Error)
	}
	scheduler.Wait()
}

// TestSchedulerRunNowDispatchesEvenWhenAuditFails guards hp-54te: the
// registry already persisted the run as RunStatusRunning by the time
// RunNow returns. If audit is recorded before dispatch and audit
// fails, the run is orphaned in the registry until lease expiry. The
// scheduler must dispatch first; the audit error is reported via the
// return value but never silently swallows the run.
func TestSchedulerRunNowDispatchesEvenWhenAuditFails(t *testing.T) {
	ctx := context.Background()
	clock := newTestClock(time.Date(2026, 5, 4, 16, 0, 0, 0, time.UTC))
	registry := newTestRegistry(t, clock)
	if _, err := registry.ImportDefinition(ctx, testDefinition("orphan-guard", Schedule{Type: ScheduleOnDemand})); err != nil {
		t.Fatal(err)
	}
	ran := make(chan struct{}, 1)
	auditErr := errors.New("synthetic audit failure")
	scheduler, err := New(Config{
		Registry: registry,
		Runner: RunnerFunc(func(context.Context, Run) (RunResult, error) {
			ran <- struct{}{}
			return RunResult{WakeAgent: false}, nil
		}),
		AuditSink: AuditSinkFunc(func(context.Context, Decision) error {
			return auditErr
		}),
		MaxWorkers: 2,
	})
	if err != nil {
		t.Fatal(err)
	}

	decision, err := scheduler.RunNow(ctx, "orphan-guard")
	if !errors.Is(err, auditErr) {
		t.Fatalf("RunNow err = %v, want errors.Is(_, auditErr)", err)
	}
	if decision.Outcome != OutcomeStarted {
		t.Fatalf("decision.Outcome = %s, want %s", decision.Outcome, OutcomeStarted)
	}
	waitForSignal(t, ran, "runner invoked despite audit failure")
	waitForRunStatus(t, registry, decision.RunID, RunStatusSucceeded)
	scheduler.Wait()
}

// TestSchedulerTickDispatchesAllRunsAndJoinsAuditErrors guards hp-54te:
// when SelectDue persists multiple runs and the audit sink fails on the
// first decision, the previous code returned immediately, leaving every
// run orphaned as RunStatusRunning. The fix must (a) dispatch every
// returned run, (b) audit every decision regardless of intermediate
// failures, and (c) return a joined error so callers see the failure
// without losing the rest of the batch.
func TestSchedulerTickDispatchesAllRunsAndJoinsAuditErrors(t *testing.T) {
	ctx := context.Background()
	clock := newTestClock(time.Date(2026, 5, 4, 17, 0, 0, 0, time.UTC))
	registry := newTestRegistry(t, clock)
	for _, id := range []string{"job-a", "job-b", "job-c"} {
		if _, err := registry.ImportDefinition(ctx, testDefinition(id, Schedule{Type: ScheduleInterval, Interval: time.Minute})); err != nil {
			t.Fatal(err)
		}
	}
	clock.Advance(2 * time.Minute)

	var auditedMu sync.Mutex
	audited := map[string]int{}
	auditErr := errors.New("synthetic audit failure")
	var ranMu sync.Mutex
	ran := map[string]int{}
	allRan := make(chan struct{})
	var ranOnce sync.Once
	scheduler, err := New(Config{
		Registry: registry,
		Runner: RunnerFunc(func(_ context.Context, run Run) (RunResult, error) {
			ranMu.Lock()
			ran[run.JobID]++
			n := len(ran)
			ranMu.Unlock()
			if n >= 3 {
				ranOnce.Do(func() { close(allRan) })
			}
			return RunResult{WakeAgent: false}, nil
		}),
		AuditSink: AuditSinkFunc(func(_ context.Context, decision Decision) error {
			auditedMu.Lock()
			audited[decision.JobID]++
			auditedMu.Unlock()
			if decision.JobID == "job-b" {
				return auditErr
			}
			return nil
		}),
		MaxWorkers: 4,
	})
	if err != nil {
		t.Fatal(err)
	}

	decisions, err := scheduler.Tick(ctx)
	if !errors.Is(err, auditErr) {
		t.Fatalf("Tick err = %v, want errors.Is(_, auditErr)", err)
	}
	if len(decisions) != 3 {
		t.Fatalf("len(decisions) = %d, want 3 (Tick must surface every decision even when audit fails mid-batch)", len(decisions))
	}

	waitForSignal(t, allRan, "every dispatched run executed")
	scheduler.Wait()

	auditedMu.Lock()
	defer auditedMu.Unlock()
	if len(audited) != 3 {
		t.Fatalf("audit invocations = %v, want all three jobs audited regardless of intermediate failure", audited)
	}
}

// TestSchedulerNewRefusesNilRunner guards hp-6pn: the previous silent
// no-op default made every dispatched run look like a healthy
// `wakeAgent: false` tick (Guardrail 9), which would mask missing
// production wiring of the layer-3 agent runtime. Construction must
// fail loudly the same way it does for a nil registry, so the bug is
// visible at startup instead of buried in audit logs.
func TestSchedulerNewRefusesNilRunner(t *testing.T) {
	ctx := context.Background()
	registry, err := NewRegistry(ctx, RegistryConfig{
		Store: NewMemoryStore(),
		Now:   time.Now,
	})
	if err != nil {
		t.Fatalf("NewRegistry: %v", err)
	}
	scheduler, err := New(Config{Registry: registry})
	if err == nil {
		scheduler.Stop()
		t.Fatal("New(Config{Registry: registry}) succeeded with nil Runner; expected ErrInvalidState")
	}
	if !errors.Is(err, ErrInvalidState) {
		t.Fatalf("err = %v, want errors.Is(_, ErrInvalidState)", err)
	}
	if !strings.Contains(err.Error(), "nil runner") {
		t.Fatalf("err = %q, want substring %q", err.Error(), "nil runner")
	}
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

// TestParseCronRejectsImpossibleDayMonthCombinations guards hp-qq4h:
// '* * 31 2 *' (Feb 31) and '* * 31 4,6,9,11 *' (31st of months
// without 31 days) are structurally valid (each field's range checks
// pass) but semantically impossible. Without parse-time rejection,
// cronExpr.Next walked the full 5-year deadline (~2.6M minute steps)
// every recompute, holding r.mu and spiking CPU.
func TestParseCronRejectsImpossibleDayMonthCombinations(t *testing.T) {
	t.Parallel()
	cases := []struct {
		expr string
		why  string
	}{
		{"* * 31 2 *", "Feb 31"},
		{"* * 30 2 *", "Feb 30"},
		{"* * 31 4 *", "April 31"},
		{"* * 31 6 *", "June 31"},
		{"* * 31 9 *", "September 31"},
		{"* * 31 11 *", "November 31"},
		{"* * 31 4,6,9,11 *", "31st of months without 31 days"},
	}
	for _, tc := range cases {
		_, err := parseCron(tc.expr)
		if err == nil {
			t.Errorf("parseCron(%q) accepted impossible expression (%s)", tc.expr, tc.why)
			continue
		}
		if !errors.Is(err, ErrInvalidDefinition) {
			t.Errorf("parseCron(%q) err = %v, want ErrInvalidDefinition", tc.expr, err)
		}
	}

	// Sanity: feasible expressions still parse.
	feasible := []string{
		"* * 31 * *",         // 31st when allowed
		"* * 29 2 *",         // Feb 29 (leap year only, but possible)
		"* * 1 2 *",          // Feb 1
		"0 9 * * 1-5",        // weekdays at 9am
		"* * 30 1,3,5,7 *",   // 30th of long months
	}
	for _, expr := range feasible {
		if _, err := parseCron(expr); err != nil {
			t.Errorf("parseCron(%q) rejected feasible expression: %v", expr, err)
		}
	}

	// Bonus: confirm the bound on Next() is now decided at parse time
	// rather than at iteration time. parseCron rejection short-circuits
	// the 2.6M-step walk before it can begin.
	start := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	t0 := time.Now()
	_, err := parseCron("* * 31 2 *")
	if err == nil {
		t.Fatal("parseCron(\"* * 31 2 *\") accepted impossible expression")
	}
	if elapsed := time.Since(t0); elapsed > 10*time.Millisecond {
		t.Errorf("parseCron took %v, expected <10ms — guard regressed and Next() is walking the full window", elapsed)
	}
	_ = start
}

// TestCronDayOfMonthDayOfWeekUnionSemantics guards hp-bpyg: when both
// day-of-month and day-of-week fields are restricted (raw token != "*"),
// POSIX/Vixie cron specifies the expression matches a day satisfying
// EITHER field. Before the fix, '0 12 15 * 1' silently meant '15th
// AND a Monday' (~once a year) instead of 'every Monday OR the 15th'
// (~every 4 days).
func TestCronDayOfMonthDayOfWeekUnionSemantics(t *testing.T) {
	t.Parallel()
	// 2026-05 calendar: 15th = Friday; Mondays = 4, 11, 18, 25.
	// '0 12 15 * 1' should fire on each Monday (4, 11, 18, 25) AND on
	// the 15th (a Friday). Eight matches total in May 2026.
	c, err := parseCron("0 12 15 * 1")
	if err != nil {
		t.Fatalf("parseCron(0 12 15 * 1): %v", err)
	}
	mayStart := time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)
	mayEnd := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	hits := []time.Time{}
	cursor := mayStart.Add(-time.Minute)
	for {
		next := c.Next(cursor)
		if next.IsZero() || !next.Before(mayEnd) {
			break
		}
		hits = append(hits, next)
		cursor = next
	}
	wantDays := []int{4, 11, 15, 18, 25}
	if len(hits) != len(wantDays) {
		t.Fatalf("hits = %d, want %d (Mondays + the 15th in May 2026)", len(hits), len(wantDays))
	}
	for i, hit := range hits {
		if hit.Day() != wantDays[i] {
			t.Errorf("hits[%d].Day() = %d, want %d", i, hit.Day(), wantDays[i])
		}
		if hit.Hour() != 12 || hit.Minute() != 0 {
			t.Errorf("hits[%d] = %s, want 12:00", i, hit.Format(time.RFC3339))
		}
	}
}

// TestCronDayFieldRestrictionAxes covers the three single-axis variants
// adjacent to hp-bpyg's UNION case so the semantics stay coherent:
// - both day fields '*': minute-by-minute every day (no day-side restriction).
// - only DOM restricted: fires only on those days (DOW '*' is trivially true).
// - only DOW restricted: fires only on those weekdays (DOM '*' is trivially true).
func TestCronDayFieldRestrictionAxes(t *testing.T) {
	t.Parallel()
	day1 := time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC) // Friday

	// Only DOM restricted: '0 12 15 * *' fires on the 15th only.
	domOnly, err := parseCron("0 12 15 * *")
	if err != nil {
		t.Fatalf("parseCron domOnly: %v", err)
	}
	hit := domOnly.Next(day1)
	if hit.Day() != 15 || hit.Month() != 5 {
		t.Errorf("domOnly first hit = %s, want 2026-05-15", hit.Format(time.RFC3339))
	}

	// Only DOW restricted: '0 12 * * 1' fires every Monday.
	dowOnly, err := parseCron("0 12 * * 1")
	if err != nil {
		t.Fatalf("parseCron dowOnly: %v", err)
	}
	hit = dowOnly.Next(day1)
	if hit.Weekday() != time.Monday {
		t.Errorf("dowOnly first hit = %s (%s), want a Monday", hit.Format(time.RFC3339), hit.Weekday())
	}
	if hit.Day() != 4 {
		t.Errorf("dowOnly first hit Day = %d, want 4 (first Monday after 2026-05-01)", hit.Day())
	}

	// Both day fields '*': '0 12 * * *' fires every day at 12:00.
	allDays, err := parseCron("0 12 * * *")
	if err != nil {
		t.Fatalf("parseCron allDays: %v", err)
	}
	hit = allDays.Next(day1)
	if hit.Day() != 1 || hit.Month() != 5 || hit.Hour() != 12 {
		t.Errorf("allDays first hit = %s, want 2026-05-01T12:00", hit.Format(time.RFC3339))
	}
}

// TestRegistryPrunesTerminalRunsBeyondRetention guards hp-dqm8: with
// TerminalRunRetention set, every CompleteRun must keep the in-memory
// state.Runs population at the configured cap by evicting the oldest
// terminal record, while leaving active runs untouched. Without the
// bound, state.Runs grew O(time) and persistLocked re-encoded the
// full slice on every disk write.
func TestRegistryPrunesTerminalRunsBeyondRetention(t *testing.T) {
	t.Parallel()
	clock := newTestClock(time.Date(2026, 5, 4, 12, 0, 0, 0, time.UTC))
	registry, err := NewRegistry(context.Background(), RegistryConfig{
		Store:                NewMemoryStore(),
		Now:                  clock.Now,
		LeaseHolder:          "retention-test",
		LeaseTTL:             time.Minute,
		TerminalRunRetention: 5,
	})
	if err != nil {
		t.Fatalf("NewRegistry: %v", err)
	}
	if _, err := registry.ImportDefinition(context.Background(), testDefinition("retention", Schedule{Type: ScheduleOnDemand})); err != nil {
		t.Fatalf("ImportDefinition: %v", err)
	}

	for i := 0; i < 25; i++ {
		clock.Advance(time.Second)
		run, _, err := registry.RunNow(context.Background(), "retention")
		if err != nil {
			t.Fatalf("RunNow %d: %v", i, err)
		}
		clock.Advance(time.Second)
		if _, err := registry.CompleteRun(context.Background(), run.ID, RunResult{}, nil); err != nil {
			t.Fatalf("CompleteRun %d: %v", i, err)
		}
	}

	snap, err := registry.Snapshot(context.Background())
	if err != nil {
		t.Fatalf("Snapshot: %v", err)
	}
	if len(snap.Runs) != 5 {
		t.Fatalf("len(state.Runs) = %d, want 5 (retention bound)", len(snap.Runs))
	}
	// Verify the retained runs are the 5 most-recent completions.
	completed := make([]time.Time, 0, len(snap.Runs))
	for _, run := range snap.Runs {
		if run.CompletedAt == nil {
			t.Fatalf("retained run %s has nil CompletedAt", run.ID)
		}
		completed = append(completed, *run.CompletedAt)
	}
	for _, ts := range completed {
		if ts.Before(clock.Now().Add(-15 * time.Second)) {
			t.Fatalf("retained run with old completion %s; eviction did not pick the oldest first", ts)
		}
	}
}

// TestRegistryPrunesEventDedupeBeyondRetention guards hp-dqm8: a job
// firing many ScheduleEvent triggers with unique eventKeys must not
// grow EventDedupe without bound.
func TestRegistryPrunesEventDedupeBeyondRetention(t *testing.T) {
	t.Parallel()
	clock := newTestClock(time.Date(2026, 5, 4, 13, 0, 0, 0, time.UTC))
	registry, err := NewRegistry(context.Background(), RegistryConfig{
		Store:           NewMemoryStore(),
		Now:             clock.Now,
		LeaseHolder:     "dedupe-test",
		LeaseTTL:        time.Minute,
		DedupeRetention: 8,
	})
	if err != nil {
		t.Fatalf("NewRegistry: %v", err)
	}
	if _, err := registry.ImportDefinition(context.Background(), testDefinition("evt", Schedule{Type: ScheduleEvent, Event: "agent_mail.received"})); err != nil {
		t.Fatalf("ImportDefinition: %v", err)
	}

	for i := 0; i < 40; i++ {
		clock.Advance(time.Second)
		key := fmt.Sprintf("msg-%04d", i)
		if _, _, err := registry.EmitEvent(context.Background(), "agent_mail.received", key, nil); err != nil {
			t.Fatalf("EmitEvent %d: %v", i, err)
		}
	}

	snap, err := registry.Snapshot(context.Background())
	if err != nil {
		t.Fatalf("Snapshot: %v", err)
	}
	if got := len(snap.EventDedupe); got > 8 {
		t.Fatalf("len(state.EventDedupe) = %d, want <= 8 (retention bound)", got)
	}
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

func waitForRunStatus(t *testing.T, registry *Registry, runID string, want RunStatus) Run {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		run, err := registry.GetRun(context.Background(), runID)
		if err != nil {
			t.Fatalf("get run %s: %v", runID, err)
		}
		if run.Status == want {
			return run
		}
		time.Sleep(10 * time.Millisecond)
	}
	run, err := registry.GetRun(context.Background(), runID)
	if err != nil {
		t.Fatalf("get run %s: %v", runID, err)
	}
	t.Fatalf("run %s status = %s, want %s", runID, run.Status, want)
	return Run{}
}

func TestFileStoreSaveDoesNotLeaveTmpOnSuccess(t *testing.T) {
	// hp-5la1: success path must rename tmp → final and leave no
	// .tmp.<unix_nano> orphan in the directory.
	dir := t.TempDir()
	store := FileStore{Path: filepath.Join(dir, "scheduler-state.json")}
	if err := store.Save(context.Background(), emptyState()); err != nil {
		t.Fatalf("Save: %v", err)
	}
	for _, name := range listDir(t, dir) {
		if strings.Contains(name, ".tmp.") {
			t.Fatalf("Save left orphan tmp file: %q", name)
		}
	}
}

func TestFileStoreSaveCleansTmpOnRenameFailure(t *testing.T) {
	// hp-5la1: when Rename fails (here: target path is a non-empty
	// directory, which os.Rename refuses), the deferred cleanup must
	// remove the tmp file. Before the fix this leaked
	// `<final>.tmp.<unix_nano>` indefinitely.
	dir := t.TempDir()
	target := filepath.Join(dir, "scheduler-state.json")
	// Make the target a non-empty directory so os.Rename fails on
	// every platform we care about (Linux: ENOTEMPTY; macOS: ENOTDIR
	// on the source side; both end as Rename errors).
	if err := os.MkdirAll(target, 0o700); err != nil {
		t.Fatalf("mkdir target dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(target, "blocker"), []byte("x"), 0o600); err != nil {
		t.Fatalf("write blocker: %v", err)
	}
	store := FileStore{Path: target}
	if err := store.Save(context.Background(), emptyState()); err == nil {
		t.Fatal("Save against directory-target unexpectedly succeeded")
	}
	for _, name := range listDir(t, dir) {
		if strings.Contains(name, ".tmp.") {
			t.Fatalf("rename-failure left orphan tmp file: %q", name)
		}
	}
}

func TestPruneOrphanTmpFilesRemovesOldOrphansAndKeepsRecent(t *testing.T) {
	// hp-5la1: pruneOrphanTmpFiles is the boot-time hygiene step that
	// sweeps up tmp files left by previous daemon crashes. Recent tmp
	// files (younger than minAge) must be preserved so we don't race a
	// concurrent Save mid-write.
	dir := t.TempDir()
	old := filepath.Join(dir, "state.json.tmp.111")
	recent := filepath.Join(dir, "definitions.yaml.tmp.999")
	keepUnrelated := filepath.Join(dir, "scheduler-state.json")
	for _, p := range []string{old, recent, keepUnrelated} {
		if err := os.WriteFile(p, []byte("x"), 0o600); err != nil {
			t.Fatalf("write %s: %v", p, err)
		}
	}
	// Backdate `old` past the minAge threshold; leave `recent` at now.
	pastModTime := time.Now().Add(-2 * time.Hour)
	if err := os.Chtimes(old, pastModTime, pastModTime); err != nil {
		t.Fatalf("chtimes: %v", err)
	}
	if err := pruneOrphanTmpFiles(dir, time.Hour, time.Now); err != nil {
		t.Fatalf("pruneOrphanTmpFiles: %v", err)
	}
	if _, err := os.Stat(old); !os.IsNotExist(err) {
		t.Fatalf("old tmp file still exists: stat err=%v", err)
	}
	if _, err := os.Stat(recent); err != nil {
		t.Fatalf("recent tmp file removed (expected to keep): %v", err)
	}
	if _, err := os.Stat(keepUnrelated); err != nil {
		t.Fatalf("non-tmp file removed (expected to keep): %v", err)
	}
}

func listDir(t *testing.T, dir string) []string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("ReadDir %s: %v", dir, err)
	}
	out := make([]string, 0, len(entries))
	for _, entry := range entries {
		out = append(out, entry.Name())
	}
	return out
}
