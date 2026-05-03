package logger

import (
	"context"
	"encoding/json"
	"io"
	"sync"
	"time"
)

// Transport is the sink for emitted log entries. Each transport receives
// already-redacted entries; transports must NOT do their own redaction.
type Transport interface {
	Emit(entry Entry)
}

// Logger is the production logger. It scopes contextual fields (component,
// subsystem, correlationId, jobId, etc.) via With(...) so call sites stay
// terse. Thread-safe.
type Logger struct {
	mu       sync.Mutex
	min      Level
	now      func() time.Time
	redactor *Redactor
	output   []Transport

	// Inherited scope.
	component     string
	subsystem     string
	correlationID string
	causationID   string
	actor         *Actor
	jobID         string
	beadID        string
	swarmID       string
	planID        string
	runID         string
	fields        map[string]any
}

// Config configures a root Logger. Transports are mandatory — at least one
// must be provided (use NewCaptureTransport for tests). MinLevel defaults
// to LevelInfo if zero.
type Config struct {
	Component string
	MinLevel  Level
	Now       func() time.Time
	Redactor  *Redactor
	Outputs   []Transport
}

// New constructs a root Logger. If `cfg.Outputs` is empty, a single stderr
// transport is wired by default — production deployments must override.
func New(cfg Config) *Logger {
	now := cfg.Now
	if now == nil {
		now = time.Now
	}
	min := cfg.MinLevel
	if !min.Valid() {
		min = LevelInfo
	}
	redactor := cfg.Redactor
	if redactor == nil {
		redactor = NewRedactor()
	}
	outputs := cfg.Outputs
	if len(outputs) == 0 {
		// Defensive default — production main.go must wire something
		// concrete. Tests should pass an explicit CaptureTransport.
		outputs = []Transport{&NullTransport{}}
	}
	return &Logger{
		min:       min,
		now:       now,
		redactor:  redactor,
		output:    outputs,
		component: cfg.Component,
	}
}

// Level returns the current minimum level. Entries below this rank are
// dropped (never redacted, never emitted).
func (l *Logger) Level() Level {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.min
}

// With returns a new logger that inherits the parent's scope and adds the
// given Fields. Common scope fields (component/subsystem/correlationId/
// jobId/beadId/swarmId/planId/runId/actor) are extracted into the
// dedicated envelope columns; everything else flows into `fields`.
func (l *Logger) With(fields ...Field) *Logger {
	if l == nil {
		return nil
	}
	child := l.clone()
	for _, f := range fields {
		child.applyField(f)
	}
	return child
}

func (l *Logger) clone() *Logger {
	l.mu.Lock()
	defer l.mu.Unlock()
	out := &Logger{
		min:           l.min,
		now:           l.now,
		redactor:      l.redactor,
		output:        l.output,
		component:     l.component,
		subsystem:     l.subsystem,
		correlationID: l.correlationID,
		causationID:   l.causationID,
		actor:         l.actor,
		jobID:         l.jobID,
		beadID:        l.beadID,
		swarmID:       l.swarmID,
		planID:        l.planID,
		runID:         l.runID,
	}
	if len(l.fields) > 0 {
		out.fields = make(map[string]any, len(l.fields))
		for k, v := range l.fields {
			out.fields[k] = v
		}
	}
	return out
}

func (l *Logger) applyField(f Field) {
	switch f.Key {
	case "component":
		if s, ok := f.Value.(string); ok {
			l.component = s
		}
	case "subsystem":
		if s, ok := f.Value.(string); ok {
			l.subsystem = s
		}
	case "correlationId":
		if s, ok := f.Value.(string); ok {
			l.correlationID = s
		}
	case "causationId":
		if s, ok := f.Value.(string); ok {
			l.causationID = s
		}
	case "actor":
		if a, ok := f.Value.(Actor); ok {
			l.actor = &a
		} else if a, ok := f.Value.(*Actor); ok {
			l.actor = a
		}
	case "jobId":
		if s, ok := f.Value.(string); ok {
			l.jobID = s
		}
	case "beadId":
		if s, ok := f.Value.(string); ok {
			l.beadID = s
		}
	case "swarmId":
		if s, ok := f.Value.(string); ok {
			l.swarmID = s
		}
	case "planId":
		if s, ok := f.Value.(string); ok {
			l.planID = s
		}
	case "runId":
		if s, ok := f.Value.(string); ok {
			l.runID = s
		}
	default:
		if l.fields == nil {
			l.fields = make(map[string]any)
		}
		l.fields[f.Key] = f.Value
	}
}

// log builds an Entry, runs redaction, and dispatches to every transport.
// Returns the redacted entry so callers can inspect it (mostly tests).
func (l *Logger) log(level Level, msg string, fields ...Field) Entry {
	if l == nil {
		return Entry{}
	}
	if level.Rank() < l.Level().Rank() {
		return Entry{}
	}
	l.mu.Lock()
	entryFields := mergeFields(l.fields, fields)
	entry := Entry{
		TS:            l.now().UTC(),
		Level:         level,
		Msg:           msg,
		Component:     l.component,
		Subsystem:     l.subsystem,
		CorrelationID: l.correlationID,
		CausationID:   l.causationID,
		Actor:         l.actor,
		JobID:         l.jobID,
		BeadID:        l.beadID,
		SwarmID:       l.swarmID,
		PlanID:        l.planID,
		RunID:         l.runID,
		Fields:        entryFields,
	}
	transports := l.output
	redactor := l.redactor
	l.mu.Unlock()

	if redactor != nil {
		RedactEntry(redactor, &entry)
	}
	for _, t := range transports {
		t.Emit(entry)
	}
	return entry
}

// mergeFields combines the logger's inherited fields with one-off Field
// args. Inherited fields apply first; per-call fields override on key
// collision. Special envelope keys (component, jobId, ...) are NOT routed
// here — they should go through With(...).
func mergeFields(inherited map[string]any, callFields []Field) map[string]any {
	if len(inherited) == 0 && len(callFields) == 0 {
		return nil
	}
	out := make(map[string]any, len(inherited)+len(callFields))
	for k, v := range inherited {
		out[k] = v
	}
	for _, f := range callFields {
		switch f.Key {
		case "component", "subsystem", "correlationId", "causationId",
			"actor", "jobId", "beadId", "swarmId", "planId", "runId":
			// Skip — these are envelope columns set via With.
			continue
		default:
			out[f.Key] = f.Value
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// Trace / Debug / Info / Warn / Error / Fatal are the per-level helpers.
// Fatal emits at the given level but does NOT call os.Exit; exit policy is
// the caller's responsibility (so tests can assert on emitted entries).

func (l *Logger) Trace(msg string, fields ...Field) Entry {
	return l.log(LevelTrace, msg, fields...)
}
func (l *Logger) Debug(msg string, fields ...Field) Entry {
	return l.log(LevelDebug, msg, fields...)
}
func (l *Logger) Info(msg string, fields ...Field) Entry {
	return l.log(LevelInfo, msg, fields...)
}
func (l *Logger) Warn(msg string, fields ...Field) Entry {
	return l.log(LevelWarn, msg, fields...)
}
func (l *Logger) Error(msg string, fields ...Field) Entry {
	return l.log(LevelError, msg, fields...)
}
func (l *Logger) Fatal(msg string, fields ...Field) Entry {
	return l.log(LevelFatal, msg, fields...)
}

// Context-keyed logger plumbing — request-scoped loggers ride along with the
// HTTP/WS context object so handler code can grab `logger.FromContext(r.Context())`
// without threading through the whole call graph.

type contextKey int

const loggerKey contextKey = 1

// IntoContext returns a derived context carrying the given logger.
func IntoContext(ctx context.Context, l *Logger) context.Context {
	return context.WithValue(ctx, loggerKey, l)
}

// FromContext returns the Logger attached to ctx, or a no-op fallback if
// none is present. The fallback is safe to call but emits nothing.
func FromContext(ctx context.Context) *Logger {
	if ctx == nil {
		return nopLogger
	}
	if l, ok := ctx.Value(loggerKey).(*Logger); ok && l != nil {
		return l
	}
	return nopLogger
}

// nopLogger is a singleton Logger backed by a NullTransport. Returned by
// FromContext when no logger is attached so callers don't have to nil-check.
var nopLogger = New(Config{
	Component: "daemon.unscoped",
	MinLevel:  LevelFatal,
	Outputs:   []Transport{&NullTransport{}},
})

// jsonTransport is the shared base for transports that emit one JSON line
// per entry. It is unexported; callers use NewWriterTransport / NewFileTransport.
type jsonTransport struct {
	mu sync.Mutex
	w  io.Writer
}

func (j *jsonTransport) Emit(entry Entry) {
	j.mu.Lock()
	defer j.mu.Unlock()
	bytes, err := json.Marshal(entry)
	if err != nil {
		// Encoding failure on a struct we control is a programmer error.
		// Fall back to a synthetic envelope describing the failure so the
		// transport still emits something deterministic.
		bytes = []byte(`{"level":"error","msg":"logger: failed to encode entry","component":"daemon.logger"}`)
	}
	bytes = append(bytes, '\n')
	_, _ = j.w.Write(bytes)
}

// NewWriterTransport wraps any io.Writer in a JSON-line transport. Used
// by the daemon's stderr emitter and for testing.
func NewWriterTransport(w io.Writer) Transport {
	return &jsonTransport{w: w}
}

// NullTransport drops every entry. Used by the no-op logger and as a
// fallback when no Outputs are configured.
type NullTransport struct{}

func (*NullTransport) Emit(Entry) {}
