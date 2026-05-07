package scheduler

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"
)

// CapabilityStatus is the narrow status enum the scheduler reads from
// the daemon's capability registry. Mirrors capabilities.CapabilityStatus
// without taking a hard import dependency, so the registry package can
// be wired in via a CapabilityChecker interface and tests can substitute
// a fake without pulling the full capabilities subsystem.
type CapabilityStatus string

const (
	CapabilityStatusOK              CapabilityStatus = "ok"
	CapabilityStatusDegraded        CapabilityStatus = "degraded"
	CapabilityStatusMissing         CapabilityStatus = "missing"
	CapabilityStatusBlockedByPolicy CapabilityStatus = "blocked-by-policy"
	CapabilityStatusUntested        CapabilityStatus = "untested"
)

// CapabilityChecker is the contract the scheduler uses to pre-flight
// job capabilities before resolving a Run to OutcomeStarted. Production
// wiring is the daemon's *capabilities.Registry (its
// LookupCapabilityStatus method satisfies this interface via
// CapabilityStatus(string) conversion through a thin adapter — see
// cmd/hoopoe/tending.go). Tests substitute fakes that exercise OK /
// degraded / missing / blocked / untested branches.
type CapabilityChecker interface {
	LookupCapabilityStatus(ref string) (CapabilityStatus, bool)
}

type Registry struct {
	mu                   sync.Mutex
	store                Store
	state                State
	now                  func() time.Time
	runCounter           uint64
	leaseHolder          string
	leaseTTL             time.Duration
	terminalRunRetention int
	dedupeRetention      int
	capabilities         CapabilityChecker
}

type RegistryConfig struct {
	Store       Store
	Now         func() time.Time
	LeaseHolder string
	LeaseTTL    time.Duration
	// TerminalRunRetention caps how many terminal (succeeded / failed /
	// interrupted / skipped) runs are retained in state.Runs. Without
	// this bound the registry's state.json grows O(time) and every
	// persistLocked re-encodes the full slice — disk and CPU pressure
	// on long-running daemons. Zero or negative disables pruning
	// (legacy behavior; tests that walk the full history opt in).
	TerminalRunRetention int
	// DedupeRetention caps the size of state.EventDedupe. Without it,
	// jobs that fire with unique eventKeys (commit SHA, message id)
	// accumulate dedup entries forever. Zero or negative disables.
	DedupeRetention int
	// Capabilities, when non-nil, is consulted in resolveDueLocked
	// before a Run is moved to OutcomeStarted. Any required
	// capability that resolves to missing / blocked-by-policy /
	// untested / unknown produces an audited
	// OutcomeBlockedByCapability decision in place of the started
	// run. Degraded statuses still dispatch — that matches the
	// capabilities.Registry.Determine precedence (degraded ≠
	// unavailable). Nil disables the gate entirely; legacy callers
	// keep their pre-hp-8gq behavior.
	Capabilities CapabilityChecker
}

func NewRegistry(ctx context.Context, cfg RegistryConfig) (*Registry, error) {
	if cfg.Store == nil {
		return nil, fmt.Errorf("%w: nil store", ErrInvalidState)
	}
	now := cfg.Now
	if now == nil {
		now = time.Now
	}
	holder := cfg.LeaseHolder
	if holder == "" {
		holder = "hoopoe-scheduler"
	}
	ttl := cfg.LeaseTTL
	if ttl <= 0 {
		ttl = time.Minute
	}
	state, err := cfg.Store.Load(ctx)
	if err != nil {
		return nil, err
	}
	normalizeState(&state)
	reg := &Registry{
		store:                cfg.Store,
		state:                state,
		now:                  now,
		leaseHolder:          holder,
		leaseTTL:             ttl,
		terminalRunRetention: cfg.TerminalRunRetention,
		dedupeRetention:      cfg.DedupeRetention,
		capabilities:         cfg.Capabilities,
	}
	if err := reg.reclaimExpiredLeasesLocked(ctx, reg.now().UTC()); err != nil {
		return nil, err
	}
	return reg, nil
}

func (r *Registry) ImportDefinition(ctx context.Context, definition Definition) (Job, error) {
	definition = definition.normalized()
	if err := definition.Validate(); err != nil {
		return Job{}, err
	}
	r.mu.Lock()
	defer r.mu.Unlock()

	now := r.now().UTC()
	existing, ok := r.state.Jobs[definition.ID]
	if ok && definition.Revision <= existing.Definition.Revision {
		definition.Revision = existing.Definition.Revision + 1
	}
	var next *time.Time
	if definition.Schedule.Type == ScheduleCron || definition.Schedule.Type == ScheduleInterval {
		nextRun, err := definition.Schedule.NextAfter(now.Add(-time.Nanosecond))
		if err != nil {
			return Job{}, err
		}
		if !nextRun.IsZero() {
			next = &nextRun
		}
	}
	status := JobStatusReady
	if definition.Paused {
		status = JobStatusPaused
	}
	job := Job{
		Definition:          definition,
		Status:              status,
		ImportedAt:          now,
		UpdatedAt:           now,
		NextRunAt:           next,
		SuccessfulRuns:      existing.SuccessfulRuns,
		ConsecutiveFailures: existing.ConsecutiveFailures,
		DeadLetteredAt:      existing.DeadLetteredAt,
		LastDecision:        existing.LastDecision,
		LastDecisionPayload: cloneAnyMap(existing.LastDecisionPayload),
	}
	if existing.ImportedAt.IsZero() {
		job.ImportedAt = now
	} else {
		job.ImportedAt = existing.ImportedAt
	}
	if job.DeadLetteredAt != nil {
		job.Status = JobStatusDeadLettered
	}
	r.state.Jobs[job.Definition.ID] = job
	if err := r.persistLocked(ctx); err != nil {
		return Job{}, err
	}
	return cloneJob(job), nil
}

func (r *Registry) GetJob(ctx context.Context, id string) (Job, error) {
	if err := ctx.Err(); err != nil {
		return Job{}, err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	job, ok := r.state.Jobs[id]
	if !ok {
		return Job{}, ErrNotFound
	}
	return cloneJob(job), nil
}

func (r *Registry) GetRun(ctx context.Context, id string) (Run, error) {
	if err := ctx.Err(); err != nil {
		return Run{}, err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	run, ok := r.state.Runs[id]
	if !ok {
		return Run{}, ErrNotFound
	}
	return cloneRun(run), nil
}

func (r *Registry) ListJobs(ctx context.Context) ([]Job, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	jobs := make([]Job, 0, len(r.state.Jobs))
	for _, id := range sortedJobIDs(r.state.Jobs) {
		jobs = append(jobs, cloneJob(r.state.Jobs[id]))
	}
	return jobs, nil
}

func (r *Registry) Snapshot(ctx context.Context) (State, error) {
	if err := ctx.Err(); err != nil {
		return State{}, err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	return cloneState(r.state), nil
}

func (r *Registry) PauseJob(ctx context.Context, id string) (Job, error) {
	if err := ctx.Err(); err != nil {
		return Job{}, err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	job, ok := r.state.Jobs[id]
	if !ok {
		return Job{}, ErrNotFound
	}
	now := r.now().UTC()
	job.Definition.Paused = true
	if job.Status != JobStatusDeadLettered && job.Status != JobStatusCompleted {
		job.Status = JobStatusPaused
	}
	job.UpdatedAt = now
	r.state.Jobs[id] = job
	if err := r.persistLocked(ctx); err != nil {
		return Job{}, err
	}
	return cloneJob(job), nil
}

func (r *Registry) ResumeJob(ctx context.Context, id string) (Job, error) {
	if err := ctx.Err(); err != nil {
		return Job{}, err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	job, ok := r.state.Jobs[id]
	if !ok {
		return Job{}, ErrNotFound
	}
	now := r.now().UTC()
	job.Definition.Paused = false
	switch {
	case job.DeadLetteredAt != nil:
		job.Status = JobStatusDeadLettered
	case job.Status == JobStatusCompleted:
		// Preserve completed repeat-limited jobs; resume only clears the
		// paused flag and does not restart exhausted work.
	default:
		job.Status = JobStatusReady
		if job.NextRunAt == nil && (job.Definition.Schedule.Type == ScheduleCron || job.Definition.Schedule.Type == ScheduleInterval) {
			next, err := job.Definition.Schedule.NextAfter(now)
			if err != nil {
				return Job{}, err
			}
			if !next.IsZero() {
				job.NextRunAt = &next
			}
		}
	}
	job.UpdatedAt = now
	r.state.Jobs[id] = job
	if err := r.persistLocked(ctx); err != nil {
		return Job{}, err
	}
	return cloneJob(job), nil
}

func (r *Registry) RemoveJob(ctx context.Context, id string) (Job, error) {
	if err := ctx.Err(); err != nil {
		return Job{}, err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	job, ok := r.state.Jobs[id]
	if !ok {
		return Job{}, ErrNotFound
	}
	delete(r.state.Jobs, id)
	prefix := id + "\x00"
	for key := range r.state.EventDedupe {
		if len(key) >= len(prefix) && key[:len(prefix)] == prefix {
			delete(r.state.EventDedupe, key)
		}
	}
	if err := r.persistLocked(ctx); err != nil {
		return Job{}, err
	}
	return cloneJob(job), nil
}

func (r *Registry) SelectDue(ctx context.Context, limit int) ([]Run, []Decision, error) {
	if err := ctx.Err(); err != nil {
		return nil, nil, err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	now := r.now().UTC()
	if err := r.reclaimExpiredLeasesLocked(ctx, now); err != nil {
		return nil, nil, err
	}
	started := make([]Run, 0)
	decisions := make([]Decision, 0)
	for _, id := range sortedJobIDs(r.state.Jobs) {
		if limit > 0 && len(decisions) >= limit {
			break
		}
		job := r.state.Jobs[id]
		if job.NextRunAt == nil || job.NextRunAt.After(now) {
			continue
		}
		run, decision := r.resolveDueLocked(job, Trigger{
			Type:  TriggerScheduled,
			DueAt: copyTime(job.NextRunAt),
		}, now)
		decisions = append(decisions, decision)
		if decision.Outcome == OutcomeStarted {
			started = append(started, run)
		}
	}
	if len(decisions) == 0 {
		return nil, nil, nil
	}
	// hp-f1vy: SelectDue's resolveDueLocked path adds RunStatusSkipped
	// records for paused / dead-lettered / concurrency-capped jobs.
	// Without this prune, a paused job under continuous scheduling
	// pressure grows state.Runs linearly until the next CompleteRun
	// fires (which never happens for a paused job).
	r.pruneTerminalRunsLocked()
	if err := r.persistLocked(ctx); err != nil {
		return nil, nil, err
	}
	return cloneRuns(started), cloneDecisions(decisions), nil
}

func (r *Registry) RunNow(ctx context.Context, jobID string) (Run, Decision, error) {
	if err := ctx.Err(); err != nil {
		return Run{}, Decision{}, err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	now := r.now().UTC()
	if err := r.reclaimExpiredLeasesLocked(ctx, now); err != nil {
		return Run{}, Decision{}, err
	}
	job, ok := r.state.Jobs[jobID]
	if !ok {
		return Run{}, Decision{}, ErrNotFound
	}
	run, decision := r.resolveDueLocked(job, Trigger{Type: TriggerOnDemand}, now)
	// hp-f1vy: RunNow on a paused / dead-lettered / capped job records
	// a skip without ever calling CompleteRun, so the terminal-run
	// prune that CompleteRun owns never fires for this code path.
	r.pruneTerminalRunsLocked()
	if err := r.persistLocked(ctx); err != nil {
		return Run{}, Decision{}, err
	}
	return cloneRun(run), decision, nil
}

func (r *Registry) EmitEvent(ctx context.Context, eventType string, eventKey string, data map[string]string) ([]Run, []Decision, error) {
	if err := ctx.Err(); err != nil {
		return nil, nil, err
	}
	if eventType == "" {
		return nil, nil, fmt.Errorf("%w: empty event type", ErrInvalidState)
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	now := r.now().UTC()
	if err := r.reclaimExpiredLeasesLocked(ctx, now); err != nil {
		return nil, nil, err
	}
	started := make([]Run, 0)
	decisions := make([]Decision, 0)
	for _, id := range sortedJobIDs(r.state.Jobs) {
		job := r.state.Jobs[id]
		if job.Definition.Schedule.Type != ScheduleEvent || job.Definition.Schedule.Event != eventType {
			continue
		}
		key := eventDedupeKey(job.Definition.ID, eventType, eventKey)
		if eventKey != "" {
			if _, exists := r.state.EventDedupe[key]; exists {
				run, decision := r.recordSkipLocked(job, Trigger{Type: TriggerEvent, EventType: eventType, EventKey: eventKey, Data: cloneStringMap(data)}, OutcomeSkippedByPolicy, "duplicate event trigger", now)
				decisions = append(decisions, decision)
				r.state.Runs[run.ID] = run
				continue
			}
		}
		run, decision := r.resolveDueLocked(job, Trigger{Type: TriggerEvent, EventType: eventType, EventKey: eventKey, Data: cloneStringMap(data)}, now)
		decisions = append(decisions, decision)
		if decision.Outcome == OutcomeStarted {
			started = append(started, run)
			if eventKey != "" {
				r.state.EventDedupe[key] = run.ID
			}
		}
	}
	if len(decisions) == 0 {
		return nil, nil, nil
	}
	r.pruneEventDedupeLocked()
	// hp-f1vy: EmitEvent's duplicate-event branch (above) records a
	// skip Run for every duplicate, and the resolveDueLocked branch
	// can record skips for paused / dead-lettered jobs. Both flows
	// add to state.Runs without a corresponding CompleteRun, so the
	// terminal-run prune must fire here too — otherwise a high-
	// frequency event source against a paused job grows state.Runs
	// unbounded.
	r.pruneTerminalRunsLocked()
	if err := r.persistLocked(ctx); err != nil {
		return nil, nil, err
	}
	return cloneRuns(started), cloneDecisions(decisions), nil
}

func (r *Registry) CompleteRun(ctx context.Context, runID string, result RunResult, runErr error) (Run, error) {
	if err := ctx.Err(); err != nil {
		return Run{}, err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	run, ok := r.state.Runs[runID]
	if !ok {
		return Run{}, ErrNotFound
	}
	if run.Status != RunStatusRunning {
		return Run{}, ErrInvalidState
	}
	now := r.now().UTC()
	completed := now
	run.CompletedAt = &completed
	if runErr != nil {
		run.Status = RunStatusFailed
		run.Error = runErr.Error()
	} else {
		run.Status = RunStatusSucceeded
		run.Result = &result
		run.Context = cloneAnyMap(result.Context)
	}
	r.state.Runs[runID] = run

	job, ok := r.state.Jobs[run.JobID]
	if ok {
		if job.Lease != nil && run.Lease != nil && job.Lease.Holder == run.Lease.Holder {
			job.Lease = nil
		}
		job.UpdatedAt = now
		if runErr != nil {
			job.ConsecutiveFailures++
			if job.Definition.DeadLetterAfter > 0 && job.ConsecutiveFailures >= job.Definition.DeadLetterAfter {
				deadAt := now
				job.DeadLetteredAt = &deadAt
				job.Status = JobStatusDeadLettered
				r.state.Metrics.DeadLetters++
			}
		} else {
			job.SuccessfulRuns++
			job.ConsecutiveFailures = 0
			job.Status = JobStatusReady
			job.LastDecisionPayload = cloneAnyMap(result.Context)
			if job.Definition.Repeat.exhausted(job.SuccessfulRuns) {
				job.Status = JobStatusCompleted
				job.NextRunAt = nil
			}
		}
		r.state.Jobs[run.JobID] = job
	}
	r.pruneTerminalRunsLocked()
	if err := r.persistLocked(ctx); err != nil {
		// hp-lsx: persist failure leaves memory and disk disagreeing —
		// in-memory the run is terminal and the lease released, on disk
		// the run still looks `running`. Bump CompletionPersistFailures
		// so the divergence is observable via Snapshot/Metrics; the
		// caller (Scheduler.completeRun) gets the error and can log it.
		r.state.Metrics.CompletionPersistFailures++
		return Run{}, err
	}
	return cloneRun(run), nil
}

// pruneTerminalRunsLocked drops the oldest terminal runs (Succeeded /
// Failed / Interrupted / Skipped) once state.Runs exceeds the
// configured retention bound. Active runs (RunStatusRunning,
// RunStatusQueued) are always retained — they are part of the live
// state machine, not history. Eviction order is by CompletedAt
// ascending, so the oldest completion is dropped first.
func (r *Registry) pruneTerminalRunsLocked() {
	if r.terminalRunRetention <= 0 {
		return
	}
	terminal := make([]Run, 0)
	for _, run := range r.state.Runs {
		switch run.Status {
		case RunStatusSucceeded, RunStatusFailed, RunStatusInterrupted, RunStatusSkipped:
			terminal = append(terminal, run)
		}
	}
	excess := len(terminal) - r.terminalRunRetention
	if excess <= 0 {
		return
	}
	sort.Slice(terminal, func(i, j int) bool {
		ti, tj := terminal[i].CompletedAt, terminal[j].CompletedAt
		switch {
		case ti == nil && tj == nil:
			return terminal[i].QueuedAt.Before(terminal[j].QueuedAt)
		case ti == nil:
			return true
		case tj == nil:
			return false
		default:
			return ti.Before(*tj)
		}
	})
	for i := 0; i < excess; i++ {
		delete(r.state.Runs, terminal[i].ID)
	}
}

// pruneEventDedupeLocked caps state.EventDedupe at dedupeRetention by
// dropping arbitrary entries when over the bound. Strict insertion
// order isn't preserved on disk, so eviction is best-effort: pick
// entries whose corresponding run is already terminal (or absent),
// since those keys are no longer load-bearing for in-flight dedup
// decisions.
func (r *Registry) pruneEventDedupeLocked() {
	if r.dedupeRetention <= 0 {
		return
	}
	excess := len(r.state.EventDedupe) - r.dedupeRetention
	if excess <= 0 {
		return
	}
	// Two-pass: first prefer entries pointing at terminal-or-missing
	// runs, then fall back to arbitrary entries if still over the cap.
	for key, runID := range r.state.EventDedupe {
		if excess <= 0 {
			return
		}
		run, ok := r.state.Runs[runID]
		if !ok || run.Status == RunStatusSucceeded || run.Status == RunStatusFailed || run.Status == RunStatusInterrupted || run.Status == RunStatusSkipped {
			delete(r.state.EventDedupe, key)
			excess--
		}
	}
	for key := range r.state.EventDedupe {
		if excess <= 0 {
			return
		}
		delete(r.state.EventDedupe, key)
		excess--
	}
}

func (r *Registry) RecordTickDuration(ctx context.Context, duration time.Duration) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.state.Metrics.Ticks++
	count := time.Duration(r.state.Metrics.Ticks)
	r.state.Metrics.AverageTickDuration = ((r.state.Metrics.AverageTickDuration * (count - 1)) + duration) / count
	return r.persistLocked(ctx)
}

func (r *Registry) resolveDueLocked(job Job, trigger Trigger, now time.Time) (Run, Decision) {
	if job.Status == JobStatusPaused || job.Definition.Paused {
		run, decision := r.recordSkipLocked(job, trigger, OutcomePaused, "job is paused", now)
		r.state.Runs[run.ID] = run
		r.advanceJobAfterDecisionLocked(&job, now)
		r.state.Jobs[job.Definition.ID] = job
		return run, decision
	}
	if job.Status == JobStatusDeadLettered || job.DeadLetteredAt != nil {
		run, decision := r.recordSkipLocked(job, trigger, OutcomeDeadLettered, "job is dead-lettered", now)
		r.state.Runs[run.ID] = run
		r.state.Jobs[job.Definition.ID] = job
		return run, decision
	}
	if job.Status == JobStatusCompleted {
		run, decision := r.recordSkipLocked(job, trigger, OutcomeSkippedByPolicy, "repeat count exhausted", now)
		r.state.Runs[run.ID] = run
		return run, decision
	}
	if job.Lease != nil && job.Lease.ExpiresAt.After(now) {
		run, decision := r.recordSkipLocked(job, trigger, OutcomeLeasedElsewhere, "job has an active lease", now)
		r.state.Runs[run.ID] = run
		r.advanceJobAfterDecisionLocked(&job, now)
		r.state.Jobs[job.Definition.ID] = job
		return run, decision
	}
	if running := r.runningRunsLocked(job.Definition.ID, now); running >= maxConcurrency(job.Definition) {
		run, decision := r.recordSkipLocked(job, trigger, OutcomeLeasedElsewhere, "max concurrency reached", now)
		r.state.Runs[run.ID] = run
		r.advanceJobAfterDecisionLocked(&job, now)
		r.state.Jobs[job.Definition.ID] = job
		return run, decision
	}
	if trigger.Type == TriggerScheduled && trigger.DueAt != nil && now.Sub(*trigger.DueAt) > job.Definition.MisfireGrace {
		r.state.Metrics.Misfires++
		if job.Definition.MisfirePolicy.normalize() == MisfireSkip {
			run, decision := r.recordSkipLocked(job, trigger, OutcomeSkippedByMisfirePolicy, "missed scheduled run exceeded misfire grace", now)
			r.state.Runs[run.ID] = run
			r.advanceJobAfterDecisionLocked(&job, now)
			r.state.Jobs[job.Definition.ID] = job
			return run, decision
		}
	}
	// hp-8gq: pre-dispatch capability gate. Job declarations are the
	// scheduling-time contract — if a required capability is missing
	// (probe failed), blocked by policy (operator opted out),
	// untested (probe never ran), or unknown to the wired registry,
	// dispatch is refused and an audited OutcomeBlockedByCapability
	// decision is recorded in place of the started run. Degraded
	// statuses still dispatch — they mirror plan.md §2.8's "available
	// but reduced" semantics. The check is gated on a non-nil
	// CapabilityChecker so legacy wiring without a registry stays
	// pre-hp-8gq behavior.
	if r.capabilities != nil {
		if reasons := r.evaluateRequiredCapabilitiesLocked(job); len(reasons) > 0 {
			run, decision := r.recordSkipLocked(job, trigger, OutcomeBlockedByCapability, strings.Join(reasons, "; "), now)
			r.state.Runs[run.ID] = run
			r.advanceJobAfterDecisionLocked(&job, now)
			r.state.Jobs[job.Definition.ID] = job
			return run, decision
		}
	}
	run := r.newRunLocked(job, trigger, OutcomeStarted, now)
	lease := Lease{Holder: r.leaseHolder, ExpiresAt: now.Add(r.leaseTTL)}
	started := now
	run.Status = RunStatusRunning
	run.StartedAt = &started
	run.Lease = &lease
	r.state.Runs[run.ID] = run
	job.Lease = &lease
	job.Status = JobStatusReady
	r.advanceJobAfterDecisionLocked(&job, now)
	job.LastDecision = &Decision{
		RunID:     run.ID,
		JobID:     job.Definition.ID,
		Outcome:   OutcomeStarted,
		Trigger:   trigger,
		Audit:     job.Definition.AuditAlways,
		CreatedAt: now,
	}
	r.state.Jobs[job.Definition.ID] = job
	r.state.Metrics.DueRuns++
	r.state.Metrics.StartedRuns++
	delay := now.Sub(run.QueuedAt)
	if delay > r.state.Metrics.LongestQueuedDelay {
		r.state.Metrics.LongestQueuedDelay = delay
	}
	return run, *job.LastDecision
}

// evaluateRequiredCapabilitiesLocked walks job.Definition.CapabilitiesRequired
// against the wired CapabilityChecker. Returns a slice of human-readable
// reasons — one per failing required capability — for inclusion in the
// audited OutcomeBlockedByCapability decision. Empty slice means every
// required capability is OK or degraded (both acceptable for dispatch).
//
// The status precedence follows capabilities.Registry.Determine:
//   - blocked-by-policy → block (operator opt-out)
//   - missing → block (probe never succeeded)
//   - untested → block (probe never ran; conservative)
//   - unknown ref (LookupCapabilityStatus returns ok=false) → block
//   - degraded → permitted (matches "Available with degraded annotation")
//   - ok → permitted
//
// Caller must hold r.mu so the LookupCapabilityStatus walk happens
// against a consistent set of declared capabilities. The
// CapabilityChecker's own concurrency is its responsibility — the
// production wiring (capabilities.Registry) takes its own RWMutex; it
// does not call back into the scheduler so there is no inversion.
func (r *Registry) evaluateRequiredCapabilitiesLocked(job Job) []string {
	if r.capabilities == nil || len(job.Definition.CapabilitiesRequired) == 0 {
		return nil
	}
	var reasons []string
	for _, capRef := range job.Definition.CapabilitiesRequired {
		ref := strings.TrimSpace(capRef)
		if ref == "" {
			continue
		}
		status, ok := r.capabilities.LookupCapabilityStatus(ref)
		if !ok {
			reasons = append(reasons, fmt.Sprintf("required capability %q is unknown to the registry", ref))
			continue
		}
		switch status {
		case CapabilityStatusOK, CapabilityStatusDegraded:
			// Permitted to dispatch.
		case CapabilityStatusBlockedByPolicy:
			reasons = append(reasons, fmt.Sprintf("required capability %q is blocked by policy", ref))
		case CapabilityStatusMissing:
			reasons = append(reasons, fmt.Sprintf("required capability %q is missing", ref))
		case CapabilityStatusUntested:
			reasons = append(reasons, fmt.Sprintf("required capability %q is untested", ref))
		default:
			reasons = append(reasons, fmt.Sprintf("required capability %q has unrecognized status %q", ref, status))
		}
	}
	return reasons
}

func (r *Registry) recordSkipLocked(job Job, trigger Trigger, outcome Outcome, reason string, now time.Time) (Run, Decision) {
	run := r.newRunLocked(job, trigger, outcome, now)
	run.Status = RunStatusSkipped
	run.Error = reason
	completed := now
	run.CompletedAt = &completed
	decision := Decision{
		RunID:     run.ID,
		JobID:     job.Definition.ID,
		Outcome:   outcome,
		Reason:    reason,
		Trigger:   trigger,
		Audit:     job.Definition.AuditAlways,
		CreatedAt: now,
	}
	job.LastDecision = &decision
	job.UpdatedAt = now
	r.state.Jobs[job.Definition.ID] = job
	r.state.Metrics.DueRuns++
	r.state.Metrics.SkippedRuns++
	return run, decision
}

func (r *Registry) newRunLocked(job Job, trigger Trigger, outcome Outcome, now time.Time) Run {
	r.runCounter++
	return Run{
		ID:       fmt.Sprintf("trun_%s_%06d", now.Format("20060102T150405.000000000Z"), r.runCounter),
		JobID:    job.Definition.ID,
		Revision: job.Definition.Revision,
		Trigger:  trigger,
		Status:   RunStatusQueued,
		Outcome:  outcome,
		Attempt:  job.ConsecutiveFailures + 1,
		QueuedAt: now,
	}
}

func (r *Registry) advanceJobAfterDecisionLocked(job *Job, now time.Time) {
	if job.Definition.Schedule.Type != ScheduleCron && job.Definition.Schedule.Type != ScheduleInterval {
		job.NextRunAt = nil
		return
	}
	next, err := job.Definition.Schedule.NextAfter(now)
	if err != nil || next.IsZero() {
		job.Status = JobStatusError
		if err != nil {
			job.Error = err.Error()
		}
		job.NextRunAt = nil
		return
	}
	job.NextRunAt = &next
	job.UpdatedAt = now
}

func (r *Registry) reclaimExpiredLeasesLocked(ctx context.Context, now time.Time) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	changed := false
	for id, run := range r.state.Runs {
		if run.Status != RunStatusRunning || run.Lease == nil || run.Lease.ExpiresAt.After(now) {
			continue
		}
		completed := now
		run.Status = RunStatusInterrupted
		run.CompletedAt = &completed
		run.Error = "scheduler lease expired before run completed"
		r.state.Runs[id] = run
		if job, ok := r.state.Jobs[run.JobID]; ok && job.Lease != nil && job.Lease.Holder == run.Lease.Holder {
			job.Lease = nil
			job.UpdatedAt = now
			r.state.Jobs[run.JobID] = job
		}
		r.state.Metrics.LeaseSteals++
		changed = true
	}
	if !changed {
		return nil
	}
	return r.persistLocked(ctx)
}

func (r *Registry) runningRunsLocked(jobID string, now time.Time) int {
	count := 0
	for _, run := range r.state.Runs {
		if run.JobID == jobID && run.Status == RunStatusRunning && run.Lease != nil && run.Lease.ExpiresAt.After(now) {
			count++
		}
	}
	return count
}

func (r *Registry) persistLocked(ctx context.Context) error {
	return r.store.Save(ctx, r.state)
}

func maxConcurrency(def Definition) int {
	if def.MaxConcurrency <= 0 {
		return 1
	}
	return def.MaxConcurrency
}

func eventDedupeKey(jobID string, eventType string, eventKey string) string {
	return jobID + "\x00" + eventType + "\x00" + eventKey
}

func copyTime(in *time.Time) *time.Time {
	if in == nil {
		return nil
	}
	out := *in
	return &out
}

func cloneRuns(in []Run) []Run {
	if len(in) == 0 {
		return nil
	}
	out := make([]Run, 0, len(in))
	for _, run := range in {
		out = append(out, cloneRun(run))
	}
	return out
}

func cloneDecisions(in []Decision) []Decision {
	if len(in) == 0 {
		return nil
	}
	out := make([]Decision, len(in))
	copy(out, in)
	return out
}
