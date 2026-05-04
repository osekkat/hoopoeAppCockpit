package scheduler

import (
	"context"
	"fmt"
	"sync"
	"time"
)

type Scheduler struct {
	registry *Registry
	runner   Runner
	audit    AuditSink
	workers  chan struct{}
	wg       sync.WaitGroup
}

type Config struct {
	Registry   *Registry
	Runner     Runner
	AuditSink  AuditSink
	MaxWorkers int
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
	return &Scheduler{
		registry: cfg.Registry,
		runner:   runner,
		audit:    cfg.AuditSink,
		workers:  make(chan struct{}, workers),
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
		s.dispatch(run)
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
		s.dispatch(run)
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
		s.dispatch(run)
	}
	return decisions, nil
}

func (s *Scheduler) Wait() {
	s.wg.Wait()
}

func (s *Scheduler) dispatch(run Run) {
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		s.workers <- struct{}{}
		defer func() { <-s.workers }()

		ctx := context.Background()
		if timeout := runTimeout(s.registry, run.JobID); timeout > 0 {
			var cancel context.CancelFunc
			ctx, cancel = context.WithTimeout(ctx, timeout)
			defer cancel()
		}
		result, err := s.runner.Run(ctx, run)
		_, _ = s.registry.CompleteRun(context.Background(), run.ID, result, err)
	}()
}

func (s *Scheduler) recordAudit(ctx context.Context, decision Decision) error {
	if s.audit == nil || !decision.Audit {
		return nil
	}
	return s.audit.RecordSchedulerDecision(ctx, decision)
}

func runTimeout(registry *Registry, jobID string) time.Duration {
	job, err := registry.GetJob(context.Background(), jobID)
	if err != nil {
		return time.Minute
	}
	if job.Definition.Timeout <= 0 {
		return time.Minute
	}
	return job.Definition.Timeout
}
