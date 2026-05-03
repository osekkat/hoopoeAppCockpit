package api

import (
	"time"

	"github.com/hoopoe-cockpit/hoopoe/apps/daemon/internal/jobs"
	schemas "github.com/hoopoe-cockpit/hoopoe/packages/schemas/go"
)

func (s *server) healthResponse() schemas.HealthResponse {
	return schemas.HealthResponse{
		DaemonId: "daemon-dev",
		Status:   schemas.Ok,
		Time:     s.now().UTC(),
	}
}

func (s *server) versionResponse() schemas.VersionResponse {
	channel := schemas.Dev
	resp := schemas.VersionResponse{
		SchemaVersion: schemaVersion,
	}
	resp.Daemon.Version = s.build.Version
	resp.Daemon.Commit = &s.build.Commit
	resp.Daemon.Channel = &channel
	if parsed, err := time.Parse(time.RFC3339, s.build.BuildDate); err == nil {
		resp.Daemon.BuildTime = &parsed
	}
	return resp
}

func jobListResponse(jobList []jobs.Job) schemas.JobListResponse {
	items := make([]schemas.Job, 0, len(jobList))
	for _, job := range jobList {
		items = append(items, jobResponse(job))
	}
	total := len(items)
	return schemas.JobListResponse{
		Items: items,
		Page: schemas.PageMeta{
			HasMore: false,
			Total:   &total,
		},
	}
}

func jobResponse(job jobs.Job) schemas.Job {
	status := schemas.JobStatus(job.Status)
	if !status.Valid() {
		status = schemas.JobStatusInterrupted
	}
	artifacts := artifactRefs(job.Artifacts)
	return schemas.Job{
		Id:            job.ID,
		Type:          job.Kind,
		SchemaVersion: firstPositive(job.SchemaVersion, jobs.SchemaVersion),
		Status:        status,
		Artifacts:     artifacts,
		StartedAt:     job.StartedAt,
		CompletedAt:   job.CompletedAt,
	}
}

func artifactRefs(artifacts []jobs.Artifact) *[]schemas.ArtifactRef {
	if len(artifacts) == 0 {
		return nil
	}
	out := make([]schemas.ArtifactRef, 0, len(artifacts))
	for _, artifact := range artifacts {
		kind := schemas.ArtifactRefKind(artifact.Kind)
		if !kind.Valid() {
			kind = schemas.Misc
		}
		out = append(out, schemas.ArtifactRef{
			Id:        artifact.ID,
			Kind:      kind,
			CreatedAt: &artifact.CreatedAt,
		})
	}
	return &out
}

func eventReplayResponse(window replayWindow, channel string) schemas.EventReplayResponse {
	events := make([]schemas.WsEventEnvelope, 0, len(window.Events))
	for _, ev := range window.Events {
		events = append(events, eventEnvelope(ev))
	}
	var horizon *int
	if window.OldestRetained > 0 {
		value := int(window.OldestRetained)
		horizon = &value
	}
	truncated := false
	return schemas.EventReplayResponse{
		Channel:          channel,
		Events:           events,
		LatestSequence:   int(window.LastSequence),
		RetentionHorizon: horizon,
		Truncated:        &truncated,
	}
}

func eventEnvelope(ev Event) schemas.WsEventEnvelope {
	stamp, err := time.Parse(time.RFC3339Nano, ev.Time)
	if err != nil {
		stamp = time.Unix(0, 0).UTC()
	}
	return schemas.WsEventEnvelope{
		EventId:       ev.EventID,
		SchemaVersion: ev.SchemaVersion,
		Channel:       ev.Channel,
		Type:          ev.Type,
		Sequence:      int(ev.Sequence),
		Time:          stamp,
		CausationId:   stringPtr(ev.CausationID),
		CorrelationId: stringPtr(ev.CorrelationID),
		Data:          ev.Data,
	}
}

func emptyPageMeta() schemas.PageMeta {
	total := 0
	return schemas.PageMeta{HasMore: false, Total: &total}
}

func stringPtr(value string) *string {
	if value == "" {
		return nil
	}
	return &value
}

func firstPositive(values ...int) int {
	for _, value := range values {
		if value > 0 {
			return value
		}
	}
	return 1
}
