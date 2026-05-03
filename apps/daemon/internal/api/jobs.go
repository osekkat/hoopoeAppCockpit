package api

import (
	"context"

	"github.com/hoopoe-cockpit/hoopoe/apps/daemon/internal/jobs"
)

type JobsReader = jobs.Reader

type JobsResponse struct {
	SchemaVersion int        `json:"schemaVersion"`
	Jobs          []jobs.Job `json:"jobs"`
}

type EmptyJobsReader struct{}

func (EmptyJobsReader) List(context.Context, jobs.ListFilter) ([]jobs.Job, error) {
	return []jobs.Job{}, nil
}

func (EmptyJobsReader) Get(context.Context, string) (jobs.Job, error) {
	return jobs.Job{}, jobs.ErrNotFound
}

func (EmptyJobsReader) ReadLog(context.Context, string, int64, int64) (jobs.LogChunk, error) {
	return jobs.LogChunk{}, jobs.ErrNotFound
}

func (EmptyJobsReader) ListArtifacts(context.Context, string) ([]jobs.Artifact, error) {
	return []jobs.Artifact{}, nil
}
