package api

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	jobstore "github.com/hoopoe-cockpit/hoopoe/apps/daemon/internal/jobs"
)

func TestJobLogEndpointReturnsRawRangeWithOffsetHeaders(t *testing.T) {
	reader := jobLogReader{
		jobs: map[string]jobstore.Job{
			"job_test": {
				ID:            "job_test",
				Kind:          "bootstrap.acfs",
				SchemaVersion: jobstore.SchemaVersion,
				Status:        jobstore.StatusSucceeded,
				CreatedAt:     time.Unix(20, 0).UTC(),
				UpdatedAt:     time.Unix(20, 0).UTC(),
			},
		},
		logs: map[string][]byte{
			"job_test": []byte("hello world"),
		},
	}
	router := NewRouter(Config{Jobs: reader})
	req := httptest.NewRequest(http.MethodGet, "/v1/jobs/job_test/log?offset=6&maxBytes=5", nil)
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body=%s", rec.Code, http.StatusOK, rec.Body.String())
	}
	if !bytes.Equal(rec.Body.Bytes(), []byte("world")) {
		t.Fatalf("body = %q, want world", rec.Body.String())
	}
	assertHeader(t, rec, "Content-Range", "bytes 6-10/11")
	assertHeader(t, rec, logTotalBytesHeader, "11")
	assertHeader(t, rec, logNextOffsetHeader, "11")
	assertHeader(t, rec, logFinalHeader, "true")
}

func TestJobLogEndpointSupportsReconnectFromOffsetWhileRunning(t *testing.T) {
	reader := jobLogReader{
		jobs: map[string]jobstore.Job{
			"job_active": {
				ID:            "job_active",
				Kind:          "health.go",
				SchemaVersion: jobstore.SchemaVersion,
				Status:        jobstore.StatusRunning,
				CreatedAt:     time.Unix(20, 0).UTC(),
				UpdatedAt:     time.Unix(20, 0).UTC(),
			},
		},
		logs: map[string][]byte{
			"job_active": []byte("0123456789abcdef"),
		},
	}
	router := NewRouter(Config{Jobs: reader})
	req := httptest.NewRequest(http.MethodGet, "/v1/jobs/job_active/log?offset=10&maxBytes=6", nil)
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body=%s", rec.Code, http.StatusOK, rec.Body.String())
	}
	if rec.Body.String() != "abcdef" {
		t.Fatalf("body = %q, want abcdef", rec.Body.String())
	}
	assertHeader(t, rec, "Content-Range", "bytes 10-15/16")
	assertHeader(t, rec, logTotalBytesHeader, "16")
	assertHeader(t, rec, logNextOffsetHeader, "16")
	assertHeader(t, rec, logFinalHeader, "false")
}

func TestJobLogEndpointRejectsNegativeOffset(t *testing.T) {
	router := NewRouter(Config{Jobs: jobLogReader{}})
	req := httptest.NewRequest(http.MethodGet, "/v1/jobs/job_test/log?offset=-1&maxBytes=1", nil)
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
}

type jobLogReader struct {
	jobs map[string]jobstore.Job
	logs map[string][]byte
}

func (r jobLogReader) List(context.Context, jobstore.ListFilter) ([]jobstore.Job, error) {
	out := make([]jobstore.Job, 0, len(r.jobs))
	for _, job := range r.jobs {
		out = append(out, job)
	}
	return out, nil
}

func (r jobLogReader) Get(_ context.Context, id string) (jobstore.Job, error) {
	job, ok := r.jobs[id]
	if !ok {
		return jobstore.Job{}, jobstore.ErrNotFound
	}
	return job, nil
}

func (r jobLogReader) ReadLog(_ context.Context, id string, offset int64, limit int64) (jobstore.LogChunk, error) {
	job, ok := r.jobs[id]
	if !ok {
		return jobstore.LogChunk{}, jobstore.ErrNotFound
	}
	body, ok := r.logs[id]
	if !ok {
		return jobstore.LogChunk{JobID: id, Offset: offset, NextOffset: offset, TotalBytes: 0, EOF: true, Final: job.Status.Terminal()}, nil
	}
	if offset < 0 {
		return jobstore.LogChunk{}, jobstore.ErrInvalidRequest
	}
	total := int64(len(body))
	if offset >= total {
		return jobstore.LogChunk{JobID: id, Offset: offset, NextOffset: offset, TotalBytes: total, EOF: true, Final: job.Status.Terminal()}, nil
	}
	if limit <= 0 || offset+limit > total {
		limit = total - offset
	}
	next := offset + limit
	return jobstore.LogChunk{
		JobID:      id,
		Offset:     offset,
		NextOffset: next,
		TotalBytes: total,
		Data:       append([]byte(nil), body[offset:next]...),
		EOF:        next >= total,
		Final:      job.Status.Terminal(),
	}, nil
}

func (r jobLogReader) ListArtifacts(context.Context, string) ([]jobstore.Artifact, error) {
	return nil, nil
}

func assertHeader(t *testing.T, rec *httptest.ResponseRecorder, key string, want string) {
	t.Helper()
	if got := rec.Header().Get(key); got != want {
		t.Fatalf("%s = %q, want %q\nheaders=%v\nbody=%q", key, got, want, rec.Header(), rec.Body.String())
	}
}

func TestLogContentRangeEmptyChunk(t *testing.T) {
	chunk := jobstore.LogChunk{Offset: 12, NextOffset: 12, TotalBytes: 10}
	if got, want := logContentRange(chunk), "bytes */10"; got != want {
		t.Fatalf("range = %q, want %q", got, want)
	}
}
