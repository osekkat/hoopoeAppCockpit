// Package logger is Hoopoe's single structured-logging library for the
// daemon. The desktop counterpart lives at `apps/desktop/src/shared/logger/`
// and shares the same envelope, level set, and redaction patterns.
//
// hp-lxs deliverable: same JSON envelope across desktop renderer, desktop
// main, daemon, and tests. Redaction runs BEFORE entries are buffered so
// secrets never reach a transport. Tests share the same logger via a
// `capture` transport that buffers entries per-test and dumps them on
// assertion failure.
//
// Cross-references:
//   - plan.md §1.4 — every meaningful action is inspectable.
//   - plan.md §5.4 — daemon redaction layer on logs / audit / events.
//   - plan.md §10 — audit log uses the same envelope.
//   - hp-g73 / hp-je1p — audit + redaction layer consume this library.
//   - docs/observability.md — authoritative envelope + redaction reference.
package logger

import "time"

// Level enumerates log severity. Stored as a string in the envelope per the
// bead spec; sorted ascending for filter comparisons via levelRank.
type Level string

const (
	LevelTrace Level = "trace"
	LevelDebug Level = "debug"
	LevelInfo  Level = "info"
	LevelWarn  Level = "warn"
	LevelError Level = "error"
	LevelFatal Level = "fatal"
)

// levelRank gives Level a total order. Higher rank = more severe.
var levelRank = map[Level]int{
	LevelTrace: 0,
	LevelDebug: 1,
	LevelInfo:  2,
	LevelWarn:  3,
	LevelError: 4,
	LevelFatal: 5,
}

// Rank returns the comparison rank for a Level. Unknown levels rank as -1.
func (l Level) Rank() int {
	r, ok := levelRank[l]
	if !ok {
		return -1
	}
	return r
}

// Valid reports whether the level is a known Hoopoe level.
func (l Level) Valid() bool {
	_, ok := levelRank[l]
	return ok
}

// ActorKind enumerates who/what produced a log entry. The envelope's
// `actor.kind` field uses these tokens.
type ActorKind string

const (
	ActorUser       ActorKind = "user"
	ActorAgent      ActorKind = "agent"
	ActorTendingJob ActorKind = "tending_job"
	ActorPreScript  ActorKind = "pre_script"
	ActorSystem     ActorKind = "system"
)

// Actor is the structured `actor` field on each log entry.
type Actor struct {
	Kind ActorKind `json:"kind"`
	ID   string    `json:"id"`
}

// Entry is the canonical envelope shared across desktop and daemon. JSON
// field names are the wire contract; do not rename without bumping the
// envelope schema version + audit replay tooling.
type Entry struct {
	TS            time.Time      `json:"ts"`
	Level         Level          `json:"level"`
	Msg           string         `json:"msg"`
	Component     string         `json:"component"`
	Subsystem     string         `json:"subsystem,omitempty"`
	CorrelationID string         `json:"correlationId,omitempty"`
	CausationID   string         `json:"causationId,omitempty"`
	Actor         *Actor         `json:"actor,omitempty"`
	JobID         string         `json:"jobId,omitempty"`
	BeadID        string         `json:"beadId,omitempty"`
	SwarmID       string         `json:"swarmId,omitempty"`
	PlanID        string         `json:"planId,omitempty"`
	RunID         string         `json:"runId,omitempty"`
	Fields        map[string]any `json:"fields,omitempty"`
}

// Field is a key/value pair attached to a log entry. Used by the With(...)
// helper instead of building a map at call sites.
type Field struct {
	Key   string
	Value any
}

// Common envelope component values the daemon emits. Other subsystems pass
// their own component string; these are just convenience constants that
// match the lint/audit allowlist.
const (
	ComponentDaemonAPI         = "daemon.api"
	ComponentDaemonAuth        = "daemon.auth"
	ComponentDaemonJobs        = "daemon.jobs"
	ComponentDaemonAdapters    = "daemon.adapters"
	ComponentDaemonScheduler   = "daemon.scheduler"
	ComponentDaemonRedaction   = "daemon.redaction"
	ComponentDaemonCapabilities = "daemon.capabilities"
)
