package mock

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/hoopoe-cockpit/hoopoe/apps/daemon/internal/fixtures"
	"github.com/hoopoe-cockpit/hoopoe/apps/daemon/internal/jobs"
)

type JobReader struct {
	jobs      []jobs.Job
	logs      map[string][]byte
	artifacts map[string][]jobs.Artifact
}

func NewJobReader(scenario *fixtures.Phase0Scenario, now func() time.Time) *JobReader {
	if now == nil {
		now = time.Now
	}
	stamp := now().UTC()
	jobID := "job_mock_" + sanitizeID(scenario.Manifest.Scenario)
	artifacts := []jobs.Artifact{
		{
			ID:        "artifact_" + jobID + "_snapshot",
			Kind:      "mock.snapshot",
			URI:       "fixture://" + scenario.Manifest.FixturesVersion + "/scenarios/" + scenario.Manifest.Scenario + "/snapshot.json",
			CreatedAt: stamp,
		},
		{
			ID:        "artifact_" + jobID + "_adapter_index",
			Kind:      "mock.adapter_index",
			URI:       "fixture://" + scenario.Manifest.FixturesVersion + "/scenarios/" + scenario.Manifest.Scenario + "/adapter-index.json",
			CreatedAt: stamp,
		},
	}
	return &JobReader{
		jobs: []jobs.Job{{
			ID:            jobID,
			Kind:          "mock.flywheel.scenario",
			SchemaVersion: jobs.SchemaVersion,
			Status:        jobs.StatusSucceeded,
			Audit: jobs.AuditMetadata{
				Actor:  "mock-flywheel",
				Reason: "fixture-backed daemon boot",
			},
			Artifacts: artifacts,
			CreatedAt: stamp,
			UpdatedAt: stamp,
		}},
		logs: map[string][]byte{
			jobID: []byte(fmt.Sprintf("mock flywheel scenario=%s fixturesVersion=%s adapters=%d\n",
				scenario.Manifest.Scenario, scenario.Manifest.FixturesVersion, len(scenario.Manifest.Adapters))),
		},
		artifacts: map[string][]jobs.Artifact{
			jobID: artifacts,
		},
	}
}

func (r *JobReader) List(_ context.Context, filter jobs.ListFilter) ([]jobs.Job, error) {
	if r == nil {
		return nil, nil
	}
	out := make([]jobs.Job, 0, len(r.jobs))
	for _, job := range r.jobs {
		if filter.Kind != "" && job.Kind != filter.Kind {
			continue
		}
		if len(filter.Statuses) > 0 && !statusAllowed(job.Status, filter.Statuses) {
			continue
		}
		out = append(out, job)
	}
	if filter.Limit > 0 && len(out) > filter.Limit {
		out = out[:filter.Limit]
	}
	return out, nil
}

func (r *JobReader) Get(_ context.Context, id string) (jobs.Job, error) {
	for _, job := range r.jobs {
		if job.ID == id {
			return job, nil
		}
	}
	return jobs.Job{}, jobs.ErrNotFound
}

func (r *JobReader) ReadLog(_ context.Context, id string, offset int64, limit int64) (jobs.LogChunk, error) {
	body, ok := r.logs[id]
	if !ok {
		return jobs.LogChunk{}, jobs.ErrNotFound
	}
	if offset < 0 {
		return jobs.LogChunk{}, jobs.ErrInvalidRequest
	}
	if int(offset) > len(body) {
		offset = int64(len(body))
	}
	if limit <= 0 || int(offset+limit) > len(body) {
		limit = int64(len(body)) - offset
	}
	next := offset + limit
	return jobs.LogChunk{
		JobID:      id,
		Offset:     offset,
		NextOffset: next,
		Data:       append([]byte(nil), body[offset:next]...),
		EOF:        int(next) >= len(body),
	}, nil
}

func (r *JobReader) ListArtifacts(_ context.Context, id string) ([]jobs.Artifact, error) {
	artifacts, ok := r.artifacts[id]
	if !ok {
		return nil, jobs.ErrNotFound
	}
	return append([]jobs.Artifact(nil), artifacts...), nil
}

func statusAllowed(status jobs.Status, allowed []jobs.Status) bool {
	for _, candidate := range allowed {
		if status == candidate {
			return true
		}
	}
	return false
}

func sanitizeID(value string) string {
	value = strings.TrimSpace(strings.ToLower(value))
	if value == "" {
		return "default"
	}
	var b strings.Builder
	for _, r := range value {
		switch {
		case r >= 'a' && r <= 'z':
			b.WriteRune(r)
		case r >= '0' && r <= '9':
			b.WriteRune(r)
		default:
			b.WriteByte('_')
		}
	}
	return strings.Trim(b.String(), "_")
}
