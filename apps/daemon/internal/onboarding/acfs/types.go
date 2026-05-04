// Package acfs wraps the canonical ACFS bootstrap installer with structured
// phase parsing and raw-log fallback. The installer remains the source of
// truth; this package only observes its output and records resumable progress.
package acfs

import "time"

type EventType string

const (
	EventPhaseStart       EventType = "phase.start"
	EventPhaseLine        EventType = "phase.line"
	EventPhaseCheckpoint  EventType = "phase.checkpoint"
	EventPhaseEnd         EventType = "phase.end"
	EventPhaseFail        EventType = "phase.fail"
	EventParserConfidence EventType = "parser.confidence"
)

type Stream string

const (
	StreamStdout Stream = "stdout"
	StreamStderr Stream = "stderr"
)

type CheckpointStatus string

const (
	CheckpointPass CheckpointStatus = "pass"
	CheckpointWarn CheckpointStatus = "warn"
	CheckpointFail CheckpointStatus = "fail"
	CheckpointSkip CheckpointStatus = "skip"
)

type Confidence string

const (
	ConfidenceHigh Confidence = "high"
	ConfidenceLow  Confidence = "low"
)

type Event struct {
	Type           EventType        `json:"type"`
	RunID          string           `json:"runId,omitempty"`
	Phase          string           `json:"phase,omitempty"`
	Name           string           `json:"name,omitempty"`
	Stream         Stream           `json:"stream,omitempty"`
	Offset         int64            `json:"offset,omitempty"`
	Text           string           `json:"text,omitempty"`
	Key            string           `json:"key,omitempty"`
	Status         CheckpointStatus `json:"status,omitempty"`
	RC             int              `json:"rc,omitempty"`
	DurationMs     int64            `json:"durationMs,omitempty"`
	LastLines      []string         `json:"lastLines,omitempty"`
	ResumeHint     string           `json:"resumeHint,omitempty"`
	Confidence     Confidence       `json:"confidence,omitempty"`
	RawLogFallback bool             `json:"rawLogFallback,omitempty"`
	At             time.Time        `json:"at"`
}

type Line struct {
	Stream Stream
	Offset int64
	Text   string
	At     time.Time
}

type PhaseState struct {
	Phase       string
	Name        string
	StartedAt   time.Time
	EndedAt     time.Time
	RC          int
	DurationMs  int64
	Checkpoints map[string]CheckpointStatus
}

type ParserState struct {
	RunID          string
	CurrentPhase   string
	Completed      []PhaseState
	Confidence     Confidence
	RawLogFallback bool
	LastOffset     int64
	ResumeHint     string
}

type RunRequest struct {
	RunID     string
	ProjectID string
	Ref       string
	LogDir    string
}

type RunResult struct {
	RunID          string
	Ref            string
	LogPath        string
	ExitCode       int
	StartedAt      time.Time
	CompletedAt    time.Time
	DurationMs     int64
	Events         int
	ParserState    ParserState
	RawLogFallback bool
	ResumeHint     string
}

type CommandSpec struct {
	Ref      string
	CurlPath string
	BashPath string
	URL      string
	Timeout  time.Duration
}

type CommandResult struct {
	ExitCode    int
	StartedAt   time.Time
	CompletedAt time.Time
}
