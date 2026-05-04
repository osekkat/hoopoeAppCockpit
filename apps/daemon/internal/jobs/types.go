// Package jobs owns the daemon job registry contract from plan.md §2.7.
// It is intentionally transport-agnostic: HTTP handlers depend on Reader or
// Controller, while schedulers and process runners depend on Registry.
package jobs

import (
	"context"
	"errors"
	"time"

	schemas "github.com/hoopoe-cockpit/hoopoe/packages/schemas/go"
)

// SchemaVersion is the persisted job entity schema version.
const SchemaVersion = 1

var (
	ErrNotFound              = errors.New("jobs: not found")
	ErrInvalidRequest        = errors.New("jobs: invalid request")
	ErrInvalidState          = errors.New("jobs: invalid state transition")
	ErrLeaseHeld             = errors.New("jobs: lease held by another worker")
	ErrIdempotencyConflict   = errors.New("jobs: idempotency key reused with different request")
	ErrResourceNotConfigured = errors.New("jobs: resource is not configured")
)

// Status is the structured job status persisted by the daemon.
type Status string

const (
	StatusQueued          Status = "queued"
	StatusRunning         Status = "running"
	StatusWaitingApproval Status = "waiting_approval"
	StatusCanceling       Status = "canceling"
	StatusSucceeded       Status = "succeeded"
	StatusFailed          Status = "failed"
	StatusInterrupted     Status = "interrupted"
)

func (s Status) Valid() bool {
	switch s {
	case StatusQueued, StatusRunning, StatusWaitingApproval, StatusCanceling,
		StatusSucceeded, StatusFailed, StatusInterrupted:
		return true
	}
	return false
}

func (s Status) Terminal() bool {
	switch s {
	case StatusSucceeded, StatusFailed, StatusInterrupted:
		return true
	}
	return false
}

// AuditMetadata is the job-local audit stamp. The audit log itself is owned by
// a later package; the registry keeps enough metadata to correlate entries.
type AuditMetadata struct {
	Actor         string `json:"actor,omitempty"`
	Reason        string `json:"reason,omitempty"`
	RequestID     string `json:"requestId,omitempty"`
	CorrelationID string `json:"correlationId,omitempty"`
	CausationID   string `json:"causationId,omitempty"`
}

// ProcessRef links a job to exactly one child process group.
type ProcessRef struct {
	JobID        string     `json:"jobId"`
	PID          int        `json:"pid"`
	PGID         int        `json:"pgid"`
	PTY          bool       `json:"pty"`
	StartedAt    time.Time  `json:"startedAt"`
	ReattachedAt *time.Time `json:"reattachedAt,omitempty"`
}

// Failure captures the stable failure fingerprint and user-facing message.
type Failure struct {
	Code               string `json:"code"`
	Message            string `json:"message"`
	FailureFingerprint string `json:"failureFingerprint,omitempty"`
	CrashedRecovered   bool   `json:"crashedRecovered,omitempty"`
}

// Artifact records durable outputs produced by a job without embedding blobs in
// list responses.
type Artifact struct {
	ID        string    `json:"id"`
	Kind      string    `json:"kind"`
	URI       string    `json:"uri"`
	Digest    string    `json:"digest,omitempty"`
	CreatedAt time.Time `json:"createdAt"`
}

func (a Artifact) ToSchema() schemas.ArtifactRef {
	createdAt := a.CreatedAt.UTC()
	out := schemas.ArtifactRef{
		Id:        a.ID,
		Kind:      artifactRefKind(a.Kind),
		CreatedAt: &createdAt,
	}
	if a.Digest != "" {
		out.Sha256 = stringPtr(a.Digest)
	}
	return out
}

// Job is the durable registry entity. Internal lease, process, and audit fields
// are daemon-only; ToSchema returns the OpenAPI wire shape.
type Job struct {
	ID             string        `json:"id"`
	Kind           string        `json:"kind"`
	SchemaVersion  int           `json:"schemaVersion"`
	Status         Status        `json:"status"`
	LeaseHolder    string        `json:"leaseHolder,omitempty"`
	LeaseExpiresAt *time.Time    `json:"leaseExpiresAt,omitempty"`
	CorrelationID  string        `json:"correlationId,omitempty"`
	CausationID    string        `json:"causationId,omitempty"`
	IdempotencyKey string        `json:"idempotencyKey,omitempty"`
	Audit          AuditMetadata `json:"audit"`
	Process        *ProcessRef   `json:"process,omitempty"`
	Failure        *Failure      `json:"failure,omitempty"`
	Artifacts      []Artifact    `json:"artifacts,omitempty"`
	CreatedAt      time.Time     `json:"createdAt"`
	UpdatedAt      time.Time     `json:"updatedAt"`
	StartedAt      *time.Time    `json:"startedAt,omitempty"`
	CompletedAt    *time.Time    `json:"completedAt,omitempty"`
}

func (j Job) ToSchema() schemas.Job {
	out := schemas.Job{
		Id:            j.ID,
		SchemaVersion: j.SchemaVersion,
		Status:        schemas.JobStatus(j.Status),
		Type:          j.Kind,
		StartedAt:     copyTimePtr(j.StartedAt),
		CompletedAt:   copyTimePtr(j.CompletedAt),
	}
	if j.Failure != nil && j.Failure.FailureFingerprint != "" {
		out.FailureFingerprint = stringPtr(j.Failure.FailureFingerprint)
	}
	if j.StartedAt != nil && j.CompletedAt != nil && !j.CompletedAt.Before(*j.StartedAt) {
		duration := int(j.CompletedAt.Sub(*j.StartedAt).Milliseconds())
		out.DurationMs = &duration
	}
	if len(j.Artifacts) > 0 {
		artifacts := make([]schemas.ArtifactRef, 0, len(j.Artifacts))
		for _, artifact := range j.Artifacts {
			artifacts = append(artifacts, artifact.ToSchema())
		}
		out.Artifacts = &artifacts
	}
	return out
}

func JobsToSchema(jobs []Job) []schemas.Job {
	out := make([]schemas.Job, 0, len(jobs))
	for _, job := range jobs {
		out = append(out, job.ToSchema())
	}
	return out
}

// HasLiveProcess reports whether the job is attached to a child process.
func (j Job) HasLiveProcess() bool {
	return j.Process != nil && j.Process.JobID == j.ID && j.Process.PID > 0 && j.Process.PGID > 0
}

// LeaseExpired reports whether the job lease is absent or older than now.
func (j Job) LeaseExpired(now time.Time) bool {
	return j.LeaseExpiresAt == nil || !j.LeaseExpiresAt.After(now)
}

type ListFilter struct {
	Statuses []Status
	Kind     string
	Limit    int
}

type CreateRequest struct {
	ID             string
	Kind           string
	SchemaVersion  int
	CorrelationID  string
	CausationID    string
	IdempotencyKey string
	Audit          AuditMetadata
}

type LeaseRequest struct {
	JobID    string
	Holder   string
	Duration time.Duration
}

type HeartbeatRequest struct {
	JobID    string
	Holder   string
	Duration time.Duration
}

type CompleteRequest struct {
	JobID  string
	Holder string
	Audit  AuditMetadata
}

type FailRequest struct {
	JobID   string
	Holder  string
	Failure Failure
	Audit   AuditMetadata
}

type InterruptRequest struct {
	JobID   string
	Failure Failure
	Audit   AuditMetadata
}

type CancelRequest struct {
	JobID  string
	Actor  string
	Reason string
	Audit  AuditMetadata
}

type LogChunk struct {
	JobID      string `json:"jobId"`
	Offset     int64  `json:"offset"`
	NextOffset int64  `json:"nextOffset"`
	TotalBytes int64  `json:"totalBytes"`
	Data       []byte `json:"data"`
	EOF        bool   `json:"eof"`
	Final      bool   `json:"final"`
}

func artifactRefKind(kind string) schemas.ArtifactRefKind {
	candidate := schemas.ArtifactRefKind(kind)
	if candidate.Valid() {
		return candidate
	}
	return schemas.Misc
}

func copyTimePtr(value *time.Time) *time.Time {
	if value == nil {
		return nil
	}
	copied := value.UTC()
	return &copied
}

func stringPtr(value string) *string {
	return &value
}

// Reader is the narrow surface the HTTP /v1/jobs read handlers consume.
type Reader interface {
	List(context.Context, ListFilter) ([]Job, error)
	Get(context.Context, string) (Job, error)
	ReadLog(context.Context, string, int64, int64) (LogChunk, error)
	ListArtifacts(context.Context, string) ([]Artifact, error)
}

// Controller is the state-changing surface needed by /v1/jobs/{id}/cancel.
type Controller interface {
	Reader
	Cancel(context.Context, CancelRequest) (Job, error)
}

// Registry is the full daemon-side job substrate used by schedulers and
// process runners.
type Registry interface {
	Controller
	Create(context.Context, CreateRequest) (Job, error)
	Lease(context.Context, LeaseRequest) (Job, error)
	Heartbeat(context.Context, HeartbeatRequest) (Job, error)
	Complete(context.Context, CompleteRequest) (Job, error)
	Fail(context.Context, FailRequest) (Job, error)
	Interrupt(context.Context, InterruptRequest) (Job, error)
	AttachProcess(context.Context, string, ProcessRef) (Job, error)
	DetachProcess(context.Context, string) (Job, error)
	RecoverInterrupted(context.Context, []ProcessRef) ([]Job, error)
	AppendLog(context.Context, string, []byte) (int64, error)
	AddArtifact(context.Context, string, Artifact) (Job, error)
}
