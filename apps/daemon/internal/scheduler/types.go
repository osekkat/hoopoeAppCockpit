// Package scheduler owns layer 1 of Hoopoe tending: durable job definitions,
// due-run selection, leases, misfire handling, and bounded dispatch.
package scheduler

import (
	"context"
	"errors"
	"fmt"
	"time"
)

const SchemaVersion = 1

var (
	ErrInvalidDefinition = errors.New("scheduler: invalid definition")
	ErrNotFound          = errors.New("scheduler: not found")
	ErrLeaseHeld         = errors.New("scheduler: lease held")
	ErrInvalidState      = errors.New("scheduler: invalid state")
)

type JobKind string

const (
	KindDeterministic    JobKind = "deterministic"
	KindGatedAgent       JobKind = "gated_agent"
	KindOrchestratorChat JobKind = "orchestrator_chat"
	KindExternalWebhook  JobKind = "external_webhook"
)

func (k JobKind) Valid() bool {
	switch k {
	case KindDeterministic, KindGatedAgent, KindOrchestratorChat, KindExternalWebhook:
		return true
	}
	return false
}

type ScheduleType string

const (
	ScheduleCron     ScheduleType = "cron"
	ScheduleInterval ScheduleType = "interval"
	ScheduleEvent    ScheduleType = "event"
	ScheduleOnDemand ScheduleType = "on_demand"
)

type Schedule struct {
	Type     ScheduleType  `json:"type"`
	Cron     string        `json:"cron,omitempty"`
	Interval time.Duration `json:"interval,omitempty"`
	Event    string        `json:"event,omitempty"`
}

func (s Schedule) Validate() error {
	switch s.Type {
	case ScheduleCron:
		if _, err := parseCron(s.Cron); err != nil {
			return err
		}
	case ScheduleInterval:
		if s.Interval <= 0 {
			return fmt.Errorf("%w: interval schedule requires a positive duration", ErrInvalidDefinition)
		}
	case ScheduleEvent:
		if s.Event == "" {
			return fmt.Errorf("%w: event schedule requires an event type", ErrInvalidDefinition)
		}
	case ScheduleOnDemand:
	default:
		return fmt.Errorf("%w: unsupported schedule type %q", ErrInvalidDefinition, s.Type)
	}
	return nil
}

func (s Schedule) NextAfter(after time.Time) (time.Time, error) {
	after = after.UTC()
	switch s.Type {
	case ScheduleCron:
		cron, err := parseCron(s.Cron)
		if err != nil {
			return time.Time{}, err
		}
		return cron.Next(after), nil
	case ScheduleInterval:
		if s.Interval <= 0 {
			return time.Time{}, fmt.Errorf("%w: interval schedule requires a positive duration", ErrInvalidDefinition)
		}
		return after.Add(s.Interval), nil
	case ScheduleEvent, ScheduleOnDemand:
		return time.Time{}, nil
	default:
		return time.Time{}, fmt.Errorf("%w: unsupported schedule type %q", ErrInvalidDefinition, s.Type)
	}
}

type MisfirePolicy string

const (
	MisfireSkip           MisfirePolicy = "skip"
	MisfireRunOnce        MisfirePolicy = "run_once"
	MisfireCatchUpBounded MisfirePolicy = "catch_up_bounded"
)

func (p MisfirePolicy) normalize() MisfirePolicy {
	if p == "" {
		return MisfireRunOnce
	}
	return p
}

func (p MisfirePolicy) Valid() bool {
	switch p.normalize() {
	case MisfireSkip, MisfireRunOnce, MisfireCatchUpBounded:
		return true
	}
	return false
}

type RetryPolicy string

const (
	RetryNone        RetryPolicy = "none"
	RetryFixed       RetryPolicy = "fixed"
	RetryExponential RetryPolicy = "exponential"
)

func (p RetryPolicy) normalize() RetryPolicy {
	if p == "" {
		return RetryNone
	}
	return p
}

func (p RetryPolicy) Valid() bool {
	switch p.normalize() {
	case RetryNone, RetryFixed, RetryExponential:
		return true
	}
	return false
}

type Repeat struct {
	Forever bool `json:"forever"`
	Limit   int  `json:"limit,omitempty"`
}

func RepeatForever() Repeat {
	return Repeat{Forever: true}
}

func RepeatCount(n int) Repeat {
	return Repeat{Limit: n}
}

func (r Repeat) exhausted(successes int) bool {
	return !r.Forever && r.Limit > 0 && successes >= r.Limit
}

type Definition struct {
	ID                   string        `json:"id"`
	Name                 string        `json:"name"`
	Kind                 JobKind       `json:"kind"`
	Version              int           `json:"version"`
	Revision             int           `json:"revision"`
	Schedule             Schedule      `json:"schedule"`
	ProjectScope         string        `json:"projectScope,omitempty"`
	EnabledToolsets      []string      `json:"enabledToolsets,omitempty"`
	CapabilitiesRequired []string      `json:"capabilitiesRequired,omitempty"`
	CapabilitiesOptional []string      `json:"capabilitiesOptional,omitempty"`
	Script               string        `json:"script,omitempty"`
	Skills               []string      `json:"skills,omitempty"`
	Prompt               string        `json:"prompt,omitempty"`
	Deliver              string        `json:"deliver,omitempty"`
	Repeat               Repeat        `json:"repeat"`
	Paused               bool          `json:"paused"`
	Timeout              time.Duration `json:"timeout"`
	MaxConcurrency       int           `json:"maxConcurrency"`
	MisfirePolicy        MisfirePolicy `json:"misfirePolicy"`
	RetryPolicy          RetryPolicy   `json:"retryPolicy"`
	DeadLetterAfter      int           `json:"deadLetterAfter"`
	AuditAlways          bool          `json:"auditAlways"`
	MisfireGrace         time.Duration `json:"misfireGrace"`
}

func (d Definition) Validate() error {
	if d.ID == "" || !validID(d.ID) {
		return fmt.Errorf("%w: invalid id %q", ErrInvalidDefinition, d.ID)
	}
	if d.Name == "" {
		return fmt.Errorf("%w: empty name", ErrInvalidDefinition)
	}
	if !d.Kind.Valid() {
		return fmt.Errorf("%w: invalid kind %q", ErrInvalidDefinition, d.Kind)
	}
	if d.Version != SchemaVersion {
		return fmt.Errorf("%w: unsupported version %d", ErrInvalidDefinition, d.Version)
	}
	if err := d.Schedule.Validate(); err != nil {
		return err
	}
	if !d.MisfirePolicy.Valid() {
		return fmt.Errorf("%w: invalid misfire policy %q", ErrInvalidDefinition, d.MisfirePolicy)
	}
	if !d.RetryPolicy.Valid() {
		return fmt.Errorf("%w: invalid retry policy %q", ErrInvalidDefinition, d.RetryPolicy)
	}
	if d.Repeat.Limit < 0 {
		return fmt.Errorf("%w: repeat limit cannot be negative", ErrInvalidDefinition)
	}
	if d.MaxConcurrency < 0 {
		return fmt.Errorf("%w: maxConcurrency cannot be negative", ErrInvalidDefinition)
	}
	if d.Timeout < 0 || d.MisfireGrace < 0 {
		return fmt.Errorf("%w: durations cannot be negative", ErrInvalidDefinition)
	}
	return nil
}

func (d Definition) normalized() Definition {
	if d.Version == 0 {
		d.Version = SchemaVersion
	}
	if d.Kind == "" {
		d.Kind = KindDeterministic
	}
	if d.Repeat == (Repeat{}) {
		d.Repeat = RepeatForever()
	}
	if d.MaxConcurrency == 0 {
		d.MaxConcurrency = 1
	}
	if d.Timeout == 0 {
		d.Timeout = time.Minute
	}
	if d.MisfireGrace == 0 {
		d.MisfireGrace = time.Minute
	}
	if d.MisfirePolicy == "" {
		d.MisfirePolicy = MisfireRunOnce
	}
	if d.RetryPolicy == "" {
		d.RetryPolicy = RetryNone
	}
	d.AuditAlways = true
	return d
}

type JobStatus string

const (
	JobStatusReady        JobStatus = "ready"
	JobStatusPaused       JobStatus = "paused"
	JobStatusError        JobStatus = "error"
	JobStatusDeadLettered JobStatus = "dead_lettered"
	JobStatusCompleted    JobStatus = "completed"
)

type TriggerType string

const (
	TriggerScheduled TriggerType = "scheduled"
	TriggerEvent     TriggerType = "event"
	TriggerOnDemand  TriggerType = "on_demand"
)

type Trigger struct {
	Type      TriggerType       `json:"type"`
	EventType string            `json:"eventType,omitempty"`
	EventKey  string            `json:"eventKey,omitempty"`
	DueAt     *time.Time        `json:"dueAt,omitempty"`
	Data      map[string]string `json:"data,omitempty"`
}

type RunStatus string

const (
	RunStatusQueued      RunStatus = "queued"
	RunStatusRunning     RunStatus = "running"
	RunStatusSucceeded   RunStatus = "succeeded"
	RunStatusFailed      RunStatus = "failed"
	RunStatusInterrupted RunStatus = "interrupted"
	RunStatusSkipped     RunStatus = "skipped"
)

type Outcome string

const (
	OutcomeStarted                Outcome = "started"
	OutcomeSkippedByPolicy        Outcome = "skipped_by_policy"
	OutcomeSkippedByMisfirePolicy Outcome = "skipped_by_misfire_policy"
	OutcomePaused                 Outcome = "paused"
	OutcomeLeasedElsewhere        Outcome = "leased_elsewhere"
	OutcomeDeadLettered           Outcome = "dead_lettered"
	OutcomeSchedulerError         Outcome = "scheduler_error"
)

type Lease struct {
	Holder    string    `json:"holder"`
	ExpiresAt time.Time `json:"expiresAt"`
}

type Job struct {
	Definition          Definition     `json:"definition"`
	Status              JobStatus      `json:"status"`
	Error               string         `json:"error,omitempty"`
	ImportedAt          time.Time      `json:"importedAt"`
	UpdatedAt           time.Time      `json:"updatedAt"`
	NextRunAt           *time.Time     `json:"nextRunAt,omitempty"`
	Lease               *Lease         `json:"lease,omitempty"`
	SuccessfulRuns      int            `json:"successfulRuns"`
	ConsecutiveFailures int            `json:"consecutiveFailures"`
	DeadLetteredAt      *time.Time     `json:"deadLetteredAt,omitempty"`
	LastDecision        *Decision      `json:"lastDecision,omitempty"`
	LastDecisionPayload map[string]any `json:"lastDecisionPayload,omitempty"`
}

type Run struct {
	ID          string         `json:"id"`
	JobID       string         `json:"jobId"`
	Revision    int            `json:"revision"`
	Trigger     Trigger        `json:"trigger"`
	Status      RunStatus      `json:"status"`
	Outcome     Outcome        `json:"outcome"`
	Attempt     int            `json:"attempt"`
	Lease       *Lease         `json:"lease,omitempty"`
	QueuedAt    time.Time      `json:"queuedAt"`
	StartedAt   *time.Time     `json:"startedAt,omitempty"`
	CompletedAt *time.Time     `json:"completedAt,omitempty"`
	Error       string         `json:"error,omitempty"`
	Result      *RunResult     `json:"result,omitempty"`
	Context     map[string]any `json:"context,omitempty"`
}

type Decision struct {
	RunID     string    `json:"runId"`
	JobID     string    `json:"jobId"`
	Outcome   Outcome   `json:"outcome"`
	Reason    string    `json:"reason,omitempty"`
	Trigger   Trigger   `json:"trigger"`
	Audit     bool      `json:"audit"`
	CreatedAt time.Time `json:"createdAt"`
}

type RunResult struct {
	WakeAgent bool           `json:"wakeAgent"`
	Silent    bool           `json:"silent"`
	Context   map[string]any `json:"context,omitempty"`
}

type Runner interface {
	Run(context.Context, Run) (RunResult, error)
}

type RunnerFunc func(context.Context, Run) (RunResult, error)

func (f RunnerFunc) Run(ctx context.Context, run Run) (RunResult, error) {
	return f(ctx, run)
}

type AuditSink interface {
	RecordSchedulerDecision(context.Context, Decision) error
}

type AuditSinkFunc func(context.Context, Decision) error

func (f AuditSinkFunc) RecordSchedulerDecision(ctx context.Context, decision Decision) error {
	return f(ctx, decision)
}

type Metrics struct {
	DueRuns             uint64        `json:"dueRuns"`
	StartedRuns         uint64        `json:"startedRuns"`
	SkippedRuns         uint64        `json:"skippedRuns"`
	Misfires            uint64        `json:"misfires"`
	LeaseSteals         uint64        `json:"leaseSteals"`
	DeadLetters         uint64        `json:"deadLetters"`
	Ticks               uint64        `json:"ticks"`
	AverageTickDuration time.Duration `json:"averageTickDuration"`
	LongestQueuedDelay  time.Duration `json:"longestQueuedDelay"`
}
