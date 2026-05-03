package jobs

import (
	"bytes"
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"
)

func TestCreateIsIdempotent(t *testing.T) {
	ctx := context.Background()
	reg := newTestRegistry(t)

	req := CreateRequest{
		ID:             "job_test_idempotent",
		Kind:           "bootstrap.acfs",
		IdempotencyKey: "idem_1234567890",
		CorrelationID:  "corr_1",
		Audit:          AuditMetadata{Actor: "user"},
	}
	first, err := reg.Create(ctx, req)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	second, err := reg.Create(ctx, req)
	if err != nil {
		t.Fatalf("idempotent create: %v", err)
	}
	if first.ID != second.ID {
		t.Fatalf("idempotent create returned different job: %s != %s", first.ID, second.ID)
	}

	req.Kind = "different.kind"
	if _, err := reg.Create(ctx, req); !errors.Is(err, ErrIdempotencyConflict) {
		t.Fatalf("expected idempotency conflict, got %v", err)
	}
}

func TestLeaseHeartbeatCompletePersists(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "jobs.json")
	logs := filepath.Join(t.TempDir(), "logs")
	reg, err := NewFileRegistry(ctx, FileStore{Path: path}, logs)
	if err != nil {
		t.Fatalf("new registry: %v", err)
	}
	reg.SetClock(fixedClock("2026-05-03T20:00:00Z"))

	job, err := reg.Create(ctx, CreateRequest{ID: "job_lifecycle", Kind: "git.push"})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	job, err = reg.Lease(ctx, LeaseRequest{JobID: job.ID, Holder: "worker-a", Duration: time.Minute})
	if err != nil {
		t.Fatalf("lease: %v", err)
	}
	if job.Status != StatusRunning || job.StartedAt == nil || job.LeaseExpiresAt == nil {
		t.Fatalf("lease did not mark running with timestamps: %+v", job)
	}
	reg.SetClock(fixedClock("2026-05-03T20:00:30Z"))
	job, err = reg.Heartbeat(ctx, HeartbeatRequest{JobID: job.ID, Holder: "worker-a", Duration: time.Minute})
	if err != nil {
		t.Fatalf("heartbeat: %v", err)
	}
	if !job.LeaseExpiresAt.Equal(mustTime("2026-05-03T20:01:30Z")) {
		t.Fatalf("heartbeat lease expiry = %s", job.LeaseExpiresAt)
	}
	job, err = reg.Complete(ctx, CompleteRequest{JobID: job.ID, Holder: "worker-a"})
	if err != nil {
		t.Fatalf("complete: %v", err)
	}
	if job.Status != StatusSucceeded || job.CompletedAt == nil {
		t.Fatalf("complete did not mark succeeded: %+v", job)
	}

	reloaded, err := NewFileRegistry(ctx, FileStore{Path: path}, logs)
	if err != nil {
		t.Fatalf("reload registry: %v", err)
	}
	got, err := reloaded.Get(ctx, job.ID)
	if err != nil {
		t.Fatalf("reload get: %v", err)
	}
	if got.Status != StatusSucceeded {
		t.Fatalf("persisted status = %s", got.Status)
	}
}

func TestRestartRecoveryMarksMissingProcessInterrupted(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "jobs.json")
	logs := filepath.Join(t.TempDir(), "logs")
	reg, err := NewFileRegistry(ctx, FileStore{Path: path}, logs)
	if err != nil {
		t.Fatalf("new registry: %v", err)
	}
	reg.SetClock(fixedClock("2026-05-03T20:00:00Z"))

	job, err := reg.Create(ctx, CreateRequest{ID: "job_recover_missing", Kind: "health.go"})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	job, err = reg.Lease(ctx, LeaseRequest{JobID: job.ID, Holder: "worker-a", Duration: time.Hour})
	if err != nil {
		t.Fatalf("lease: %v", err)
	}
	_, err = reg.AttachProcess(ctx, job.ID, ProcessRef{
		JobID:     job.ID,
		PID:       1234,
		PGID:      1234,
		StartedAt: mustTime("2026-05-03T20:00:01Z"),
	})
	if err != nil {
		t.Fatalf("attach process: %v", err)
	}

	restarted, err := NewFileRegistry(ctx, FileStore{Path: path}, logs)
	if err != nil {
		t.Fatalf("restart registry: %v", err)
	}
	restarted.SetClock(fixedClock("2026-05-03T20:02:00Z"))
	changed, err := restarted.RecoverInterrupted(ctx, nil)
	if err != nil {
		t.Fatalf("recover: %v", err)
	}
	if len(changed) != 1 {
		t.Fatalf("changed jobs = %d", len(changed))
	}
	got, err := restarted.Get(ctx, job.ID)
	if err != nil {
		t.Fatalf("get recovered: %v", err)
	}
	if got.Status != StatusInterrupted {
		t.Fatalf("status = %s", got.Status)
	}
	if got.Process != nil {
		t.Fatalf("process still attached: %+v", got.Process)
	}
	if got.Failure == nil || !got.Failure.CrashedRecovered {
		t.Fatalf("missing crashed-recovered failure: %+v", got.Failure)
	}
}

func TestRestartRecoveryReattachesLiveProcess(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "jobs.json")
	logs := filepath.Join(t.TempDir(), "logs")
	reg, err := NewFileRegistry(ctx, FileStore{Path: path}, logs)
	if err != nil {
		t.Fatalf("new registry: %v", err)
	}
	job, err := reg.Create(ctx, CreateRequest{ID: "job_recover_live", Kind: "health.go"})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	_, err = reg.Lease(ctx, LeaseRequest{JobID: job.ID, Holder: "worker-a", Duration: time.Hour})
	if err != nil {
		t.Fatalf("lease: %v", err)
	}

	restarted, err := NewFileRegistry(ctx, FileStore{Path: path}, logs)
	if err != nil {
		t.Fatalf("restart registry: %v", err)
	}
	changed, err := restarted.RecoverInterrupted(ctx, []ProcessRef{{
		JobID:     job.ID,
		PID:       4321,
		PGID:      4321,
		StartedAt: mustTime("2026-05-03T20:00:01Z"),
	}})
	if err != nil {
		t.Fatalf("recover: %v", err)
	}
	if len(changed) != 1 || changed[0].Status != StatusRunning {
		t.Fatalf("changed = %+v", changed)
	}
	got, err := restarted.Get(ctx, job.ID)
	if err != nil {
		t.Fatalf("get recovered: %v", err)
	}
	if !got.HasLiveProcess() || got.Process.ReattachedAt == nil {
		t.Fatalf("live process was not reattached: %+v", got.Process)
	}
}

func TestChunkedLogReadsByOffset(t *testing.T) {
	ctx := context.Background()
	reg := newTestRegistry(t)
	job, err := reg.Create(ctx, CreateRequest{ID: "job_logs", Kind: "bootstrap.acfs"})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if next, err := reg.AppendLog(ctx, job.ID, []byte("hello world")); err != nil || next != 11 {
		t.Fatalf("append next=%d err=%v", next, err)
	}
	chunk, err := reg.ReadLog(ctx, job.ID, 6, 5)
	if err != nil {
		t.Fatalf("read log: %v", err)
	}
	if !bytes.Equal(chunk.Data, []byte("world")) || chunk.NextOffset != 11 || !chunk.EOF {
		t.Fatalf("bad chunk: %+v data=%q", chunk, string(chunk.Data))
	}
}

func TestResourceLimiterBlocksAtCapacity(t *testing.T) {
	limiter, err := NewResourceLimiter(map[Resource]int{ResourceGitOpsPerProject: 1})
	if err != nil {
		t.Fatalf("new limiter: %v", err)
	}
	first, err := limiter.Acquire(context.Background(), ResourceGitOpsPerProject)
	if err != nil {
		t.Fatalf("first acquire: %v", err)
	}
	defer first.Release()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()
	if _, err := limiter.Acquire(ctx, ResourceGitOpsPerProject); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("expected blocked acquire to respect context, got %v", err)
	}

	first.Release()
	second, err := limiter.Acquire(context.Background(), ResourceGitOpsPerProject)
	if err != nil {
		t.Fatalf("second acquire after release: %v", err)
	}
	second.Release()
}

func newTestRegistry(t *testing.T) *FileRegistry {
	t.Helper()
	dir := t.TempDir()
	reg, err := NewFileRegistry(context.Background(), FileStore{Path: filepath.Join(dir, "jobs.json")}, filepath.Join(dir, "logs"))
	if err != nil {
		t.Fatalf("new registry: %v", err)
	}
	reg.SetClock(fixedClock("2026-05-03T20:00:00Z"))
	return reg
}

func fixedClock(value string) func() time.Time {
	t := mustTime(value)
	return func() time.Time { return t }
}

func mustTime(value string) time.Time {
	t, err := time.Parse(time.RFC3339, value)
	if err != nil {
		panic(err)
	}
	return t
}
