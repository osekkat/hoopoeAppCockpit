package mock

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
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
	artifacts = append(artifacts, scenarioFileArtifacts(jobID, scenario, stamp)...)
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
			jobID: buildScenarioLog(scenario),
		},
		artifacts: map[string][]jobs.Artifact{
			jobID: artifacts,
		},
	}
}

type scenarioArtifactSpec struct {
	suffix   string
	kind     string
	relative string
}

var scenarioArtifactSpecs = []scenarioArtifactSpec{
	{suffix: "prepare_command", kind: "mock.prepare_command", relative: filepath.Join("prepare", "command.txt")},
	{suffix: "prepare_status", kind: "mock.prepare_status", relative: filepath.Join("prepare", "status.json")},
	{suffix: "prepare_transcript", kind: "mock.prepare_transcript", relative: filepath.Join("prepare", "transcript.txt")},
	{suffix: "snapshot_stdout", kind: "mock.snapshot_stdout", relative: "snapshot.stdout"},
	{suffix: "snapshot_stderr", kind: "mock.snapshot_stderr", relative: "snapshot.stderr"},
}

func scenarioFileArtifacts(jobID string, scenario *fixtures.Phase0Scenario, stamp time.Time) []jobs.Artifact {
	artifacts := make([]jobs.Artifact, 0, len(scenarioArtifactSpecs))
	for _, spec := range scenarioArtifactSpecs {
		path := filepath.Join(scenario.Manifest.ScenarioDir, spec.relative)
		if info, err := os.Stat(path); err != nil || info.IsDir() {
			continue
		}
		artifacts = append(artifacts, jobs.Artifact{
			ID:        "artifact_" + jobID + "_" + spec.suffix,
			Kind:      spec.kind,
			URI:       fixtureURI(scenario, spec.relative),
			CreatedAt: stamp,
		})
	}
	return artifacts
}

func buildScenarioLog(scenario *fixtures.Phase0Scenario) []byte {
	var b strings.Builder
	fmt.Fprintf(&b, "mock flywheel scenario=%s fixturesVersion=%s adapters=%d\n",
		scenario.Manifest.Scenario, scenario.Manifest.FixturesVersion, len(scenario.Manifest.Adapters))
	for _, relative := range []string{
		filepath.Join("prepare", "transcript.txt"),
		"snapshot.stdout",
		"snapshot.stderr",
	} {
		body, err := os.ReadFile(filepath.Join(scenario.Manifest.ScenarioDir, relative))
		if err != nil {
			continue
		}
		fmt.Fprintf(&b, "\n--- %s ---\n", relative)
		b.Write(body)
		if len(body) == 0 || body[len(body)-1] != '\n' {
			b.WriteByte('\n')
		}
	}
	return []byte(b.String())
}

func fixtureURI(scenario *fixtures.Phase0Scenario, relative string) string {
	return "fixture://" + scenario.Manifest.FixturesVersion + "/scenarios/" + scenario.Manifest.Scenario + "/" + filepath.ToSlash(relative)
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
