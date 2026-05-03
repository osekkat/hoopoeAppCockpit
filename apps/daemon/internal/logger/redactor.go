// Package logger's redactor is a thin shim over the canonical redaction
// package (`apps/daemon/internal/redaction/`, hp-je1p). Non-logger
// consumers (audit log, WS event fan-out, adapter capture) import that
// package directly; the logger uses it via this shim to keep the hp-lxs
// API stable.
package logger

import (
	"github.com/hoopoe-cockpit/hoopoe/apps/daemon/internal/redaction"
)

// Redactor is the canonical daemon redactor. Re-exported for hp-lxs
// back-compat.
type Redactor = redaction.Redactor

// RedactionEvent is a re-export of redaction.TraceEvent — the canonical
// trace shape consumed across audit / events / logger / adapter capture.
type RedactionEvent = redaction.TraceEvent

// NewRedactor returns a Redactor with the canonical Hoopoe patterns.
func NewRedactor() *Redactor { return redaction.NewDefault() }

// RedactEntry runs the redactor over an entry's msg + every fields entry
// in place; returns the trace events that fired. Lives in the logger
// package because it's specific to the Entry envelope shape.
func RedactEntry(r *Redactor, e *Entry) []RedactionEvent {
	if r == nil || e == nil {
		return nil
	}
	var events []RedactionEvent
	if e.Msg != "" {
		out, msgEvents := r.RedactText(redaction.SurfaceLogger, "msg", e.Msg)
		e.Msg = out
		events = append(events, msgEvents...)
	}
	if len(e.Fields) > 0 {
		out := make(map[string]any, len(e.Fields))
		for k, v := range e.Fields {
			redacted, fieldEvents := r.RedactValue(redaction.SurfaceLogger, "fields."+k, v)
			out[k] = redacted
			events = append(events, fieldEvents...)
		}
		e.Fields = out
	}
	return events
}
