package api

import (
	"time"

	"github.com/hoopoe-cockpit/hoopoe/apps/daemon/internal/capabilities"
	"github.com/hoopoe-cockpit/hoopoe/apps/daemon/internal/jobs"
	schemas "github.com/hoopoe-cockpit/hoopoe/packages/schemas/go"
)

// healthResponse builds the /v1/health response.
//
// hp-snmn closed the pre-existing 200-OK stub: DaemonId is now derived
// from build.Version + per-instance UUID (no more hardcoded "daemon-dev"
// shared across processes), Status is computed from real subsystem
// liveness checks, the Adapters map carries per-subsystem statuses, and
// UptimeSeconds reflects the bootedAt timestamp captured at NewRouter.
//
// The shape is the schemas.HealthResponse type generated from
// packages/schemas/openapi.yaml; the readiness-vs-liveness split is in
// `handleHealth` (always 200 if the HTTP server can respond) vs
// `handleReadiness` (503 when any required subsystem is degraded or
// missing).
func (s *server) healthResponse() schemas.HealthResponse {
	now := s.now().UTC()
	adapters := s.subsystemStatuses()
	resp := schemas.HealthResponse{
		DaemonId: s.daemonID(),
		Status:   aggregateHealthStatus(adapters),
		Time:     now,
		Adapters: &adapters,
	}
	if !s.bootedAt.IsZero() {
		uptime := int(now.Sub(s.bootedAt.UTC()).Seconds())
		if uptime < 0 {
			uptime = 0
		}
		resp.UptimeSeconds = &uptime
	}
	return resp
}

// daemonID combines the build.Version + per-process instanceID so two
// daemons sharing the same build version still answer with distinct
// IDs. The exact format ("<version>+<instanceID>") is the same family
// as Go's build-info pseudoversion; downstream consumers (audit,
// pairing) can split on `+` to recover either half.
func (s *server) daemonID() string {
	if s.instanceID == "" {
		return s.build.Version
	}
	return s.build.Version + "+" + s.instanceID
}

// subsystemStatuses returns the per-subsystem status map fed into the
// HealthResponse.Adapters field. The keys are the subsystem names the
// /v1/health response surfaces; the values are the schema-defined
// adapter status enum (ok/degraded/missing/unknown).
func (s *server) subsystemStatuses() map[string]schemas.HealthResponseAdapters {
	out := make(map[string]schemas.HealthResponseAdapters, 6)
	out["events"] = boolToAdapterStatus(s.events != nil)
	out["audit"] = boolToAdapterStatus(s.auditLog != nil)
	out["jobs"] = jobsAdapterStatus(s.jobs)
	out["capabilities"] = capabilitiesAdapterStatus(s.capabilities)
	out["approvals"] = boolToAdapterStatus(s.approvals != nil)
	out["telemetry"] = boolToAdapterStatus(s.telemetry != nil)
	return out
}

func boolToAdapterStatus(ok bool) schemas.HealthResponseAdapters {
	if ok {
		return schemas.HealthResponseAdaptersOk
	}
	return schemas.HealthResponseAdaptersMissing
}

// jobsAdapterStatus distinguishes a real registry from the
// MissingJobsReader fallback that NewRouter substitutes when no jobs
// reader is wired. The fallback responds OK to its interface methods
// but cannot list any jobs — it is not a healthy subsystem.
func jobsAdapterStatus(reader JobsReader) schemas.HealthResponseAdapters {
	if reader == nil {
		return schemas.HealthResponseAdaptersMissing
	}
	if _, ok := reader.(MissingJobsReader); ok {
		return schemas.HealthResponseAdaptersMissing
	}
	return schemas.HealthResponseAdaptersOk
}

// capabilitiesAdapterStatus reports the registry as missing when nil
// AND degraded when the registry exists but has zero tool reports
// (probe never ran or every probe failed). Stage routes are gated on
// capability IDs (plan.md §2.8); a registry with no reports cannot
// satisfy any gate and the desktop wizard must know.
func capabilitiesAdapterStatus(registry *capabilities.Registry) schemas.HealthResponseAdapters {
	if registry == nil {
		return schemas.HealthResponseAdaptersMissing
	}
	snapshot := registry.Snapshot()
	if snapshot == nil || len(snapshot.Tools) == 0 {
		return schemas.HealthResponseAdaptersDegraded
	}
	return schemas.HealthResponseAdaptersOk
}

// aggregateHealthStatus picks the worst per-subsystem status and maps
// it to the overall HealthResponseStatus enum. Missing → Degraded
// (the daemon is up but at least one subsystem is unreachable);
// Degraded → Degraded (something is partially functional); all OK →
// Ok. There is no Unavailable in the schema's overall-status enum;
// the closest signal is to set 503 at the readiness probe instead.
func aggregateHealthStatus(adapters map[string]schemas.HealthResponseAdapters) schemas.HealthResponseStatus {
	for _, status := range adapters {
		switch status {
		case schemas.HealthResponseAdaptersMissing, schemas.HealthResponseAdaptersDegraded:
			return schemas.Degraded
		}
	}
	return schemas.Ok
}

// readinessRequiredSubsystems is the subset of subsystems a /v1/readiness
// probe must see as "ok" for the daemon to be ready to serve API
// traffic. capabilities is intentionally omitted because cold-start
// readiness fires before the first probe sweep completes, and gating
// readiness on a probe-completed registry would block the wizard from
// transitioning past Stage 0 for the first ~5 seconds even on a
// healthy daemon. The capability degradation surfaces in /v1/health
// instead.
var readinessRequiredSubsystems = []string{"events", "audit"}

// readinessOK returns true when every required subsystem in the
// adapter map is "ok". A missing or degraded required subsystem flips
// readiness to false and the handler responds 503.
func readinessOK(adapters map[string]schemas.HealthResponseAdapters) bool {
	for _, name := range readinessRequiredSubsystems {
		status, present := adapters[name]
		if !present {
			return false
		}
		if status != schemas.HealthResponseAdaptersOk {
			return false
		}
	}
	return true
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
