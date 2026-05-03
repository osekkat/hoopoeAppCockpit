// Package audit owns the daemon audit-log writer. Entries are redacted before
// they are encoded or written to disk, and redaction trace events are written
// without secret payloads.
package audit

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/hoopoe-cockpit/hoopoe/apps/daemon/internal/redaction"
)

const SchemaVersion = 1

type Actor struct {
	Kind string `json:"kind"`
	ID   string `json:"id"`
}

type Entry struct {
	SchemaVersion int            `json:"schemaVersion"`
	EventID       string         `json:"eventId"`
	Type          string         `json:"type"`
	Time          time.Time      `json:"time"`
	Actor         Actor          `json:"actor"`
	CorrelationID string         `json:"correlationId,omitempty"`
	CausationID   string         `json:"causationId,omitempty"`
	Data          map[string]any `json:"data,omitempty"`
}

type Config struct {
	Writer   io.Writer
	Path     string
	Redactor *redaction.Redactor
	Now      func() time.Time
}

type Writer struct {
	mu       sync.Mutex
	out      io.Writer
	closer   io.Closer
	redactor *redaction.Redactor
	now      func() time.Time
}

func NewWriter(cfg Config) (*Writer, error) {
	now := cfg.Now
	if now == nil {
		now = time.Now
	}
	redactor := cfg.Redactor
	if redactor == nil {
		redactor = redaction.New(redaction.Config{Now: now})
	}
	out := cfg.Writer
	var closer io.Closer
	if cfg.Path != "" {
		if err := os.MkdirAll(filepath.Dir(cfg.Path), 0o750); err != nil {
			return nil, fmt.Errorf("audit: mkdir: %w", err)
		}
		file, err := os.OpenFile(cfg.Path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
		if err != nil {
			return nil, fmt.Errorf("audit: open: %w", err)
		}
		out = file
		closer = file
	}
	if out == nil {
		return nil, fmt.Errorf("audit: writer or path is required")
	}
	return &Writer{
		out:      out,
		closer:   closer,
		redactor: redactor,
		now:      now,
	}, nil
}

func (w *Writer) Append(entry Entry) (Entry, []redaction.TraceEvent, error) {
	if w == nil {
		return Entry{}, nil, fmt.Errorf("audit: nil writer")
	}
	if entry.SchemaVersion == 0 {
		entry.SchemaVersion = SchemaVersion
	}
	if entry.EventID == "" {
		entry.EventID = newEventID()
	}
	if entry.Time.IsZero() {
		entry.Time = w.now().UTC()
	}
	redacted, traces := w.redactEntry(entry)

	w.mu.Lock()
	defer w.mu.Unlock()
	if err := writeJSONLine(w.out, redacted); err != nil {
		return Entry{}, nil, err
	}
	for _, trace := range traces {
		if err := writeJSONLine(w.out, traceEntry(trace, redacted)); err != nil {
			return Entry{}, nil, err
		}
	}
	if err := syncWriter(w.out); err != nil {
		return Entry{}, nil, err
	}
	return redacted, traces, nil
}

func (w *Writer) Close() error {
	if w == nil || w.closer == nil {
		return nil
	}
	err := w.closer.Close()
	w.closer = nil
	return err
}

func (w *Writer) RedactionStats() redaction.Stats {
	if w == nil || w.redactor == nil {
		return redaction.Stats{SchemaVersion: redaction.SchemaVersion}
	}
	return w.redactor.SnapshotStats()
}

func (w *Writer) redactEntry(entry Entry) (Entry, []redaction.TraceEvent) {
	if w.redactor == nil {
		return entry, nil
	}
	value := map[string]any{
		"actor": map[string]any{
			"kind": entry.Actor.Kind,
			"id":   entry.Actor.ID,
		},
		"correlationId": entry.CorrelationID,
		"causationId":   entry.CausationID,
		"data":          entry.Data,
	}
	redactedValue, traces := w.redactor.RedactValue(redaction.SurfaceAudit, "audit", value)
	redactedMap, ok := redactedValue.(map[string]any)
	if !ok {
		return entry, traces
	}
	entry.CorrelationID, _ = redactedMap["correlationId"].(string)
	entry.CausationID, _ = redactedMap["causationId"].(string)
	if actor, ok := redactedMap["actor"].(map[string]any); ok {
		entry.Actor.Kind, _ = actor["kind"].(string)
		entry.Actor.ID, _ = actor["id"].(string)
	}
	if data, ok := redactedMap["data"].(map[string]any); ok {
		entry.Data = data
	}
	return entry, traces
}

func traceEntry(trace redaction.TraceEvent, source Entry) Entry {
	return Entry{
		SchemaVersion: SchemaVersion,
		EventID:       newEventID(),
		Type:          "audit.redaction_trace",
		Time:          trace.Time,
		Actor:         Actor{Kind: "system", ID: "daemon.redaction"},
		CorrelationID: source.CorrelationID,
		CausationID:   source.EventID,
		Data: map[string]any{
			"redactor":       trace.Redactor,
			"pattern_id":     trace.PatternID,
			"context":        trace.Context,
			"bytes_redacted": trace.BytesRedacted,
			"count":          trace.Count,
		},
	}
}

func writeJSONLine(w io.Writer, value any) error {
	body, err := json.Marshal(value)
	if err != nil {
		return fmt.Errorf("audit: encode: %w", err)
	}
	body = append(body, '\n')
	if _, err := w.Write(body); err != nil {
		return fmt.Errorf("audit: write: %w", err)
	}
	return nil
}

func syncWriter(w io.Writer) error {
	syncer, ok := w.(interface{ Sync() error })
	if !ok {
		return nil
	}
	if err := syncer.Sync(); err != nil {
		return fmt.Errorf("audit: sync: %w", err)
	}
	return nil
}

func newEventID() string {
	var buf [12]byte
	if _, err := rand.Read(buf[:]); err != nil {
		return "audit_evt_unavailable"
	}
	return "audit_evt_" + hex.EncodeToString(buf[:])
}
