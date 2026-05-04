package jobs

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	joblog "github.com/hoopoe-cockpit/hoopoe/apps/daemon/internal/jobs/log"
)

type idempotencyRecord struct {
	Key         string    `json:"key"`
	RequestHash string    `json:"requestHash"`
	JobID       string    `json:"jobId"`
	CreatedAt   time.Time `json:"createdAt"`
}

type snapshot struct {
	SchemaVersion int                          `json:"schemaVersion"`
	Jobs          map[string]Job               `json:"jobs"`
	Idempotency   map[string]idempotencyRecord `json:"idempotency"`
}

type Store interface {
	Load(context.Context) (snapshot, error)
	Save(context.Context, snapshot) error
}

// FileStore is a small durable store used until the daemon migration runner
// lands SQLite. It preserves the Registry interface so the SQLite store can
// replace persistence without changing HTTP or scheduler callers.
type FileStore struct {
	Path string
}

func (s FileStore) Load(ctx context.Context) (snapshot, error) {
	if err := ctx.Err(); err != nil {
		return snapshot{}, err
	}
	if s.Path == "" {
		return snapshot{}, fmt.Errorf("%w: empty store path", ErrInvalidRequest)
	}
	f, err := os.Open(s.Path)
	if os.IsNotExist(err) {
		return emptySnapshot(), nil
	}
	if err != nil {
		return snapshot{}, err
	}
	defer f.Close()

	var state snapshot
	if err := json.NewDecoder(f).Decode(&state); err != nil {
		return snapshot{}, err
	}
	normalizeSnapshot(&state)
	return state, nil
}

func (s FileStore) Save(ctx context.Context, state snapshot) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if s.Path == "" {
		return fmt.Errorf("%w: empty store path", ErrInvalidRequest)
	}
	normalizeSnapshot(&state)
	if err := os.MkdirAll(filepath.Dir(s.Path), 0o700); err != nil {
		return err
	}
	tempPath := fmt.Sprintf("%s.tmp.%d", s.Path, time.Now().UnixNano())
	f, err := os.OpenFile(tempPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return err
	}
	enc := json.NewEncoder(f)
	enc.SetIndent("", "  ")
	if err := enc.Encode(state); err != nil {
		_ = f.Close()
		return err
	}
	if err := f.Sync(); err != nil {
		_ = f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	return os.Rename(tempPath, s.Path)
}

// FileRegistry is a durable, restartable implementation of Registry.
type FileRegistry struct {
	mu      sync.Mutex
	store   Store
	state   snapshot
	logsDir string
	now     func() time.Time
	counter uint64
}

func NewFileRegistry(ctx context.Context, store Store, logsDir string) (*FileRegistry, error) {
	if store == nil {
		return nil, fmt.Errorf("%w: nil store", ErrInvalidRequest)
	}
	state, err := store.Load(ctx)
	if err != nil {
		return nil, err
	}
	normalizeSnapshot(&state)
	return &FileRegistry{
		store:   store,
		state:   state,
		logsDir: logsDir,
		now:     time.Now,
	}, nil
}

func (r *FileRegistry) SetClock(now func() time.Time) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if now == nil {
		r.now = time.Now
		return
	}
	r.now = now
}

func (r *FileRegistry) Create(ctx context.Context, req CreateRequest) (Job, error) {
	if err := validateCreate(req); err != nil {
		return Job{}, err
	}
	r.mu.Lock()
	defer r.mu.Unlock()

	if req.SchemaVersion == 0 {
		req.SchemaVersion = SchemaVersion
	}
	if req.IdempotencyKey != "" {
		hash := hashCreateRequest(req)
		if rec, ok := r.state.Idempotency[req.IdempotencyKey]; ok {
			if rec.RequestHash != hash {
				return Job{}, ErrIdempotencyConflict
			}
			job, ok := r.state.Jobs[rec.JobID]
			if !ok {
				return Job{}, fmt.Errorf("%w: idempotency record points at %s", ErrNotFound, rec.JobID)
			}
			return cloneJob(job), nil
		}
	}

	now := r.now().UTC()
	id := req.ID
	if id == "" {
		id = r.nextIDLocked(now)
	}
	if !validID(id) {
		return Job{}, fmt.Errorf("%w: invalid job id %q", ErrInvalidRequest, id)
	}
	if _, exists := r.state.Jobs[id]; exists {
		return Job{}, fmt.Errorf("%w: duplicate job id %q", ErrInvalidRequest, id)
	}
	job := Job{
		ID:             id,
		Kind:           req.Kind,
		SchemaVersion:  req.SchemaVersion,
		Status:         StatusQueued,
		CorrelationID:  req.CorrelationID,
		CausationID:    req.CausationID,
		IdempotencyKey: req.IdempotencyKey,
		Audit:          req.Audit,
		CreatedAt:      now,
		UpdatedAt:      now,
	}
	r.state.Jobs[id] = job
	if req.IdempotencyKey != "" {
		r.state.Idempotency[req.IdempotencyKey] = idempotencyRecord{
			Key:         req.IdempotencyKey,
			RequestHash: hashCreateRequest(req),
			JobID:       id,
			CreatedAt:   now,
		}
	}
	if err := r.persistLocked(ctx); err != nil {
		return Job{}, err
	}
	return cloneJob(job), nil
}

func (r *FileRegistry) List(ctx context.Context, filter ListFilter) ([]Job, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	r.mu.Lock()
	defer r.mu.Unlock()

	statusAllowed := make(map[Status]bool, len(filter.Statuses))
	for _, status := range filter.Statuses {
		if !status.Valid() {
			return nil, fmt.Errorf("%w: invalid status %q", ErrInvalidRequest, status)
		}
		statusAllowed[status] = true
	}
	jobs := make([]Job, 0, len(r.state.Jobs))
	for _, job := range r.state.Jobs {
		if filter.Kind != "" && job.Kind != filter.Kind {
			continue
		}
		if len(statusAllowed) > 0 && !statusAllowed[job.Status] {
			continue
		}
		jobs = append(jobs, cloneJob(job))
	}
	sort.Slice(jobs, func(i, j int) bool {
		if jobs[i].CreatedAt.Equal(jobs[j].CreatedAt) {
			return jobs[i].ID < jobs[j].ID
		}
		return jobs[i].CreatedAt.Before(jobs[j].CreatedAt)
	})
	if filter.Limit > 0 && len(jobs) > filter.Limit {
		jobs = jobs[:filter.Limit]
	}
	return jobs, nil
}

func (r *FileRegistry) Get(ctx context.Context, id string) (Job, error) {
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

func (r *FileRegistry) Lease(ctx context.Context, req LeaseRequest) (Job, error) {
	if req.JobID == "" || req.Holder == "" || req.Duration <= 0 {
		return Job{}, ErrInvalidRequest
	}
	return r.updateJob(ctx, req.JobID, func(job *Job, now time.Time) error {
		if job.Status.Terminal() || job.Status == StatusCanceling {
			return ErrInvalidState
		}
		if job.LeaseHolder != "" && job.LeaseHolder != req.Holder && !job.LeaseExpired(now) {
			return ErrLeaseHeld
		}
		expires := now.Add(req.Duration)
		job.Status = StatusRunning
		job.LeaseHolder = req.Holder
		job.LeaseExpiresAt = &expires
		if job.StartedAt == nil {
			started := now
			job.StartedAt = &started
		}
		return nil
	})
}

func (r *FileRegistry) Heartbeat(ctx context.Context, req HeartbeatRequest) (Job, error) {
	if req.JobID == "" || req.Holder == "" || req.Duration <= 0 {
		return Job{}, ErrInvalidRequest
	}
	return r.updateJob(ctx, req.JobID, func(job *Job, now time.Time) error {
		if job.Status != StatusRunning && job.Status != StatusWaitingApproval {
			return ErrInvalidState
		}
		if job.LeaseHolder != req.Holder {
			return ErrLeaseHeld
		}
		expires := now.Add(req.Duration)
		job.LeaseExpiresAt = &expires
		return nil
	})
}

func (r *FileRegistry) Complete(ctx context.Context, req CompleteRequest) (Job, error) {
	if req.JobID == "" {
		return Job{}, ErrInvalidRequest
	}
	return r.updateJob(ctx, req.JobID, func(job *Job, now time.Time) error {
		if job.Status == StatusSucceeded {
			return nil
		}
		if job.Status.Terminal() {
			return ErrInvalidState
		}
		if req.Holder != "" && job.LeaseHolder != "" && job.LeaseHolder != req.Holder {
			return ErrLeaseHeld
		}
		job.Status = StatusSucceeded
		job.Audit = mergeAudit(job.Audit, req.Audit)
		job.LeaseHolder = ""
		job.LeaseExpiresAt = nil
		completed := now
		job.CompletedAt = &completed
		return nil
	})
}

func (r *FileRegistry) Fail(ctx context.Context, req FailRequest) (Job, error) {
	if req.JobID == "" || req.Failure.Code == "" {
		return Job{}, ErrInvalidRequest
	}
	return r.updateJob(ctx, req.JobID, func(job *Job, now time.Time) error {
		if job.Status == StatusFailed {
			return nil
		}
		if job.Status.Terminal() {
			return ErrInvalidState
		}
		if req.Holder != "" && job.LeaseHolder != "" && job.LeaseHolder != req.Holder {
			return ErrLeaseHeld
		}
		failure := req.Failure
		job.Failure = &failure
		job.Status = StatusFailed
		job.Audit = mergeAudit(job.Audit, req.Audit)
		job.LeaseHolder = ""
		job.LeaseExpiresAt = nil
		completed := now
		job.CompletedAt = &completed
		return nil
	})
}

func (r *FileRegistry) Interrupt(ctx context.Context, req InterruptRequest) (Job, error) {
	if req.JobID == "" || req.Failure.Code == "" {
		return Job{}, ErrInvalidRequest
	}
	return r.updateJob(ctx, req.JobID, func(job *Job, now time.Time) error {
		if job.Status == StatusInterrupted {
			return nil
		}
		if job.Status.Terminal() {
			return ErrInvalidState
		}
		failure := req.Failure
		job.Failure = &failure
		job.Status = StatusInterrupted
		job.Audit = mergeAudit(job.Audit, req.Audit)
		job.LeaseHolder = ""
		job.LeaseExpiresAt = nil
		completed := now
		job.CompletedAt = &completed
		return nil
	})
}

func (r *FileRegistry) Cancel(ctx context.Context, req CancelRequest) (Job, error) {
	if req.JobID == "" {
		return Job{}, ErrInvalidRequest
	}
	return r.updateJob(ctx, req.JobID, func(job *Job, _ time.Time) error {
		if job.Status.Terminal() || job.Status == StatusCanceling {
			return nil
		}
		job.Status = StatusCanceling
		job.Audit = mergeAudit(job.Audit, req.Audit)
		return nil
	})
}

func (r *FileRegistry) AttachProcess(ctx context.Context, jobID string, proc ProcessRef) (Job, error) {
	if jobID == "" || proc.JobID != jobID || proc.PID <= 0 || proc.PGID <= 0 {
		return Job{}, ErrInvalidRequest
	}
	return r.updateJob(ctx, jobID, func(job *Job, _ time.Time) error {
		if job.Status.Terminal() {
			return ErrInvalidState
		}
		process := proc
		job.Process = &process
		return nil
	})
}

func (r *FileRegistry) DetachProcess(ctx context.Context, jobID string) (Job, error) {
	if jobID == "" {
		return Job{}, ErrInvalidRequest
	}
	return r.updateJob(ctx, jobID, func(job *Job, _ time.Time) error {
		job.Process = nil
		return nil
	})
}

// RecoverInterrupted enforces the restart invariant: a running job must either
// reattach to a live child process or be marked interrupted with
// crashedRecovered evidence.
func (r *FileRegistry) RecoverInterrupted(ctx context.Context, live []ProcessRef) ([]Job, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	r.mu.Lock()
	defer r.mu.Unlock()

	now := r.now().UTC()
	liveByJob := make(map[string]ProcessRef, len(live))
	for _, proc := range live {
		if proc.JobID != "" && proc.PID > 0 && proc.PGID > 0 {
			liveByJob[proc.JobID] = proc
		}
	}

	changed := make([]Job, 0)
	for id, job := range r.state.Jobs {
		if job.Status.Terminal() || job.Status == StatusQueued {
			continue
		}
		if proc, ok := liveByJob[id]; ok {
			reattached := now
			proc.ReattachedAt = &reattached
			job.Process = &proc
			job.UpdatedAt = now
			r.state.Jobs[id] = job
			changed = append(changed, cloneJob(job))
			continue
		}
		failure := Failure{
			Code:             "process.crashed_recovered",
			Message:          "daemon restarted and no live process could be reattached",
			CrashedRecovered: true,
		}
		job.Status = StatusInterrupted
		job.Process = nil
		job.Failure = &failure
		job.LeaseHolder = ""
		job.LeaseExpiresAt = nil
		completed := now
		job.CompletedAt = &completed
		job.UpdatedAt = now
		r.state.Jobs[id] = job
		changed = append(changed, cloneJob(job))
	}
	if len(changed) == 0 {
		return changed, nil
	}
	if err := r.persistLocked(ctx); err != nil {
		return nil, err
	}
	return changed, nil
}

func (r *FileRegistry) AppendLog(ctx context.Context, jobID string, data []byte) (int64, error) {
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	if !validID(jobID) {
		return 0, ErrInvalidRequest
	}
	if _, err := r.Get(ctx, jobID); err != nil {
		return 0, err
	}
	if r.logsDir == "" {
		return 0, fmt.Errorf("%w: empty logs dir", ErrInvalidRequest)
	}
	next, err := joblog.Store{Dir: r.logsDir}.Append(ctx, jobID, data)
	if err != nil {
		return 0, err
	}
	return next, nil
}

func (r *FileRegistry) ReadLog(ctx context.Context, jobID string, offset int64, limit int64) (LogChunk, error) {
	if err := ctx.Err(); err != nil {
		return LogChunk{}, err
	}
	if !validID(jobID) || offset < 0 {
		return LogChunk{}, ErrInvalidRequest
	}
	job, err := r.Get(ctx, jobID)
	if err != nil {
		return LogChunk{}, err
	}
	chunk, err := joblog.Store{Dir: r.logsDir}.Read(ctx, jobID, offset, limit)
	if err != nil {
		return LogChunk{}, err
	}
	return LogChunk{
		JobID:      jobID,
		Offset:     chunk.Offset,
		NextOffset: chunk.NextOffset,
		TotalBytes: chunk.TotalBytes,
		Data:       chunk.Data,
		EOF:        chunk.EOF,
		Final:      job.Status.Terminal(),
	}, nil
}

func (r *FileRegistry) AddArtifact(ctx context.Context, jobID string, artifact Artifact) (Job, error) {
	if jobID == "" || artifact.ID == "" || artifact.Kind == "" || artifact.URI == "" {
		return Job{}, ErrInvalidRequest
	}
	return r.updateJob(ctx, jobID, func(job *Job, now time.Time) error {
		if artifact.CreatedAt.IsZero() {
			artifact.CreatedAt = now
		}
		job.Artifacts = append(job.Artifacts, artifact)
		return nil
	})
}

func (r *FileRegistry) ListArtifacts(ctx context.Context, jobID string) ([]Artifact, error) {
	job, err := r.Get(ctx, jobID)
	if err != nil {
		return nil, err
	}
	out := make([]Artifact, len(job.Artifacts))
	copy(out, job.Artifacts)
	return out, nil
}

func (r *FileRegistry) updateJob(ctx context.Context, id string, fn func(*Job, time.Time) error) (Job, error) {
	if err := ctx.Err(); err != nil {
		return Job{}, err
	}
	if !validID(id) {
		return Job{}, ErrInvalidRequest
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	job, ok := r.state.Jobs[id]
	if !ok {
		return Job{}, ErrNotFound
	}
	now := r.now().UTC()
	if err := fn(&job, now); err != nil {
		return Job{}, err
	}
	job.UpdatedAt = now
	r.state.Jobs[id] = job
	if err := r.persistLocked(ctx); err != nil {
		return Job{}, err
	}
	return cloneJob(job), nil
}

func (r *FileRegistry) persistLocked(ctx context.Context) error {
	return r.store.Save(ctx, r.state)
}

func (r *FileRegistry) nextIDLocked(now time.Time) string {
	r.counter++
	return fmt.Sprintf("job_%s_%06d", now.Format("20060102T150405.000000000Z"), r.counter)
}

func (r *FileRegistry) logPath(jobID string) string {
	return filepath.Join(r.logsDir, jobID+".log")
}

func emptySnapshot() snapshot {
	return snapshot{
		SchemaVersion: SchemaVersion,
		Jobs:          make(map[string]Job),
		Idempotency:   make(map[string]idempotencyRecord),
	}
}

func normalizeSnapshot(state *snapshot) {
	if state.SchemaVersion == 0 {
		state.SchemaVersion = SchemaVersion
	}
	if state.Jobs == nil {
		state.Jobs = make(map[string]Job)
	}
	if state.Idempotency == nil {
		state.Idempotency = make(map[string]idempotencyRecord)
	}
}

func validateCreate(req CreateRequest) error {
	if req.Kind == "" {
		return fmt.Errorf("%w: empty kind", ErrInvalidRequest)
	}
	version := req.SchemaVersion
	if version == 0 {
		version = SchemaVersion
	}
	if version != SchemaVersion {
		return fmt.Errorf("%w: unsupported schema version %d", ErrInvalidRequest, version)
	}
	if req.ID != "" && !validID(req.ID) {
		return fmt.Errorf("%w: invalid job id %q", ErrInvalidRequest, req.ID)
	}
	return nil
}

func validID(id string) bool {
	if id == "" || len(id) > 128 || strings.Contains(id, "..") {
		return false
	}
	for _, r := range id {
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

func hashCreateRequest(req CreateRequest) string {
	req.ID = ""
	data, _ := json.Marshal(req)
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func cloneJob(in Job) Job {
	out := in
	if in.LeaseExpiresAt != nil {
		t := *in.LeaseExpiresAt
		out.LeaseExpiresAt = &t
	}
	if in.Process != nil {
		p := *in.Process
		if in.Process.ReattachedAt != nil {
			t := *in.Process.ReattachedAt
			p.ReattachedAt = &t
		}
		out.Process = &p
	}
	if in.Failure != nil {
		f := *in.Failure
		out.Failure = &f
	}
	if in.Artifacts != nil {
		out.Artifacts = make([]Artifact, len(in.Artifacts))
		copy(out.Artifacts, in.Artifacts)
	}
	if in.StartedAt != nil {
		t := *in.StartedAt
		out.StartedAt = &t
	}
	if in.CompletedAt != nil {
		t := *in.CompletedAt
		out.CompletedAt = &t
	}
	return out
}

func mergeAudit(base AuditMetadata, next AuditMetadata) AuditMetadata {
	if next.Actor != "" {
		base.Actor = next.Actor
	}
	if next.Reason != "" {
		base.Reason = next.Reason
	}
	if next.RequestID != "" {
		base.RequestID = next.RequestID
	}
	if next.CorrelationID != "" {
		base.CorrelationID = next.CorrelationID
	}
	if next.CausationID != "" {
		base.CausationID = next.CausationID
	}
	return base
}
