package scheduler

import (
	"context"
	"fmt"
	"sync"
	"time"
)

type Scheduler struct {
	registry          *Registry
	runner            Runner
	audit             AuditSink
	workers           chan struct{}
	rootCtx           context.Context
	stop              chan struct{}
	stopOnce          sync.Once
	completionTimeout time.Duration
	waitMu            sync.Mutex
	activeRuns        int
	waiters           map[chan struct{}]struct{}
}

type Config struct {
	Registry          *Registry
	Runner            Runner
	AuditSink         AuditSink
	MaxWorkers        int
	Context           context.Context
	CompletionTimeout time.Duration
}

func New(cfg Config) (*Scheduler, error) {
	if cfg.Registry == nil {
		return nil, fmt.Errorf("%w: nil registry", ErrInvalidState)
	}
	workers := cfg.MaxWorkers
	if workers <= 0 {
		workers = 4
	}
	runner := cfg.Runner
	if runner == nil {
		runner = RunnerFunc(func(context.Context, Run) (RunResult, error) {
			return RunResult{WakeAgent: false}, nil
		})
	}
	root := cfg.Context
	if root == nil {
		root = context.Background()
	}
	completionTimeout := cfg.CompletionTimeout
	if completionTimeout <= 0 {
		completionTimeout = 5 * time.Second
	}
	return &Scheduler{
		registry:          cfg.Registry,
		runner:            runner,
		audit:             cfg.AuditSink,
		workers:           make(chan struct{}, workers),
		rootCtx:           root,
		stop:              make(chan struct{}),
		completionTimeout: completionTimeout,
		waiters:           make(map[chan struct{}]struct{}),
	}, nil
}

func (s *Scheduler) Tick(ctx context.Context) ([]Decision, error) {
	start := time.Now()
	runs, decisions, err := s.registry.SelectDue(ctx, 0)
	if err != nil {
		return nil, err
	}
	for _, decision := range decisions {
		if err := s.recordAudit(ctx, decision); err != nil {
			return nil, err
		}
	}
	for _, run := range runs {
		s.dispatch(ctx, run)
	}
	if err := s.registry.RecordTickDuration(ctx, time.Since(start)); err != nil {
		return nil, err
	}
	return decisions, nil
}

func (s *Scheduler) RunNow(ctx context.Context, jobID string) (Decision, error) {
	run, decision, err := s.registry.RunNow(ctx, jobID)
	if err != nil {
		return Decision{}, err
	}
	if err := s.recordAudit(ctx, decision); err != nil {
		return Decision{}, err
	}
	if decision.Outcome == OutcomeStarted {
		s.dispatch(ctx, run)
	}
	return decision, nil
}

func (s *Scheduler) EmitEvent(ctx context.Context, eventType string, eventKey string, data map[string]string) ([]Decision, error) {
	runs, decisions, err := s.registry.EmitEvent(ctx, eventType, eventKey, data)
	if err != nil {
		return nil, err
	}
	for _, decision := range decisions {
		if err := s.recordAudit(ctx, decision); err != nil {
			return nil, err
		}
	}
	for _, run := range runs {
		s.dispatch(ctx, run)
	}
	return decisions, nil
}

func (s *Scheduler) Wait() {
	_ = s.WaitContext(context.Background())
}

func (s *Scheduler) WaitContext(ctx context.Context) error {
	if ctx == nil {
		ctx = context.Background()
	}
	done := s.registerWaiter()
	if done == nil {
		return nil
	}
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return s.cancelWaiter(done, ctx.Err())
	}
}

func (s *Scheduler) Stop() {
	s.stopOnce.Do(func() {
		close(s.stop)
	})
}

func (s *Scheduler) Shutdown(ctx context.Context) error {
	s.Stop()
	return s.WaitContext(ctx)
}

func (s *Scheduler) dispatch(ctx context.Context, run Run) {
	s.startRun()
	go func() {
		defer s.finishRun()
		// Goroutine-boundary recover guards every call site in the dispatch
		// path — runTimeout (registry lock + cloneJob), completeRun (Store.Save
		// can panic on a buggy Store impl), dispatchContext, and any future
		// code added inside the goroutine. invokeRunner has its own recover
		// for finer-grained error reporting (the run is marked failed with
		// the runner's panic value); this boundary recover is the last line
		// of defense for everything else and falls back to a best-effort
		// completeRun under its own inner recover so a Store panic during
		// recovery cannot re-panic out of the goroutine.
		defer s.recoverDispatch(run.ID)

		dispatchCtx, cancel := s.dispatchContext(ctx)
		defer cancel()
		select {
		case s.workers <- struct{}{}:
		case <-dispatchCtx.Done():
			s.completeRun(run.ID, RunResult{}, dispatchCtx.Err())
			return
		}
		defer func() { <-s.workers }()

		runCtx := dispatchCtx
		if timeout := runTimeout(dispatchCtx, s.registry, run.JobID); timeout > 0 {
			var timeoutCancel context.CancelFunc
			runCtx, timeoutCancel = context.WithTimeout(runCtx, timeout)
			defer timeoutCancel()
		}
		result, err := s.invokeRunner(runCtx, run)
		if err == nil && runCtx.Err() != nil {
			err = runCtx.Err()
		}
		s.completeRun(run.ID, result, err)
	}()
}

func (s *Scheduler) startRun() {
	s.waitMu.Lock()
	s.activeRuns++
	s.waitMu.Unlock()
}

func (s *Scheduler) finishRun() {
	var waiters []chan struct{}
	s.waitMu.Lock()
	if s.activeRuns > 0 {
		s.activeRuns--
	}
	if s.activeRuns == 0 && len(s.waiters) > 0 {
		waiters = make([]chan struct{}, 0, len(s.waiters))
		for waiter := range s.waiters {
			waiters = append(waiters, waiter)
			delete(s.waiters, waiter)
		}
	}
	s.waitMu.Unlock()

	for _, waiter := range waiters {
		close(waiter)
	}
}

func (s *Scheduler) registerWaiter() chan struct{} {
	s.waitMu.Lock()
	defer s.waitMu.Unlock()
	if s.activeRuns == 0 {
		return nil
	}
	waiter := make(chan struct{})
	if s.waiters == nil {
		s.waiters = make(map[chan struct{}]struct{})
	}
	s.waiters[waiter] = struct{}{}
	return waiter
}

func (s *Scheduler) cancelWaiter(waiter chan struct{}, err error) error {
	s.waitMu.Lock()
	_, waiting := s.waiters[waiter]
	if waiting {
		delete(s.waiters, waiter)
		close(waiter)
	}
	s.waitMu.Unlock()
	if !waiting {
		return nil
	}
	return err
}

// recoverDispatch is the dispatch goroutine's last-resort panic guard. If
// any call inside the goroutine panics — runTimeout, completeRun, or any
// future code path — the recovered value is converted into a synthetic
// error and a best-effort registry write marks the run failed. That
// best-effort write is itself wrapped in a recover so a buggy Store.Save
// (or a registry that panics under load) cannot re-panic out of the
// goroutine and crash the daemon.
func (s *Scheduler) recoverDispatch(runID string) {
	r := recover()
	if r == nil {
		return
	}
	defer func() {
		_ = recover()
	}()
	s.completeRun(runID, RunResult{}, fmt.Errorf("scheduler: dispatch panic recovered: %v", r))
}

// invokeRunner calls the configured Runner under a recover guard so that a
// panicking implementation cannot take the daemon down. The recovered value
// is converted into an error so the run is marked failed and the registry
// stays consistent.
func (s *Scheduler) invokeRunner(ctx context.Context, run Run) (result RunResult, err error) {
	defer func() {
		if r := recover(); r != nil {
			result = RunResult{}
			err = fmt.Errorf("scheduler: runner panic recovered: %v", r)
		}
	}()
	return s.runner.Run(ctx, run)
}

func (s *Scheduler) dispatchContext(ctx context.Context) (context.Context, context.CancelFunc) {
	if ctx == nil {
		ctx = context.Background()
	}
	dispatchCtx, cancel := context.WithCancel(ctx)
	stopRootCancel := context.AfterFunc(s.rootCtx, cancel)
	stopSchedulerCancel := context.AfterFunc(channelContext{s.stop}, cancel)
	if s.rootCtx.Err() != nil || channelClosed(s.stop) {
		cancel()
	}
	return dispatchCtx, func() {
		stopRootCancel()
		stopSchedulerCancel()
		cancel()
	}
}

func (s *Scheduler) completeRun(runID string, result RunResult, runErr error) {
	ctx := context.Background()
	if s.completionTimeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, s.completionTimeout)
		defer cancel()
	}
	_, _ = s.registry.CompleteRun(ctx, runID, result, runErr)
}

func (s *Scheduler) recordAudit(ctx context.Context, decision Decision) error {
	if s.audit == nil || !decision.Audit {
		return nil
	}
	return s.audit.RecordSchedulerDecision(ctx, decision)
}

func runTimeout(ctx context.Context, registry *Registry, jobID string) time.Duration {
	if ctx == nil {
		ctx = context.Background()
	}
	job, err := registry.GetJob(ctx, jobID)
	if err != nil {
		return time.Minute
	}
	if job.Definition.Timeout <= 0 {
		return time.Minute
	}
	return job.Definition.Timeout
}

type channelContext struct {
	done <-chan struct{}
}

func (c channelContext) Deadline() (time.Time, bool) {
	return time.Time{}, false
}

func (c channelContext) Done() <-chan struct{} {
	return c.done
}

func (c channelContext) Err() error {
	if channelClosed(c.done) {
		return context.Canceled
	}
	return nil
}

func (c channelContext) Value(any) any {
	return nil
}

func channelClosed(done <-chan struct{}) bool {
	select {
	case <-done:
		return true
	default:
		return false
	}
}
