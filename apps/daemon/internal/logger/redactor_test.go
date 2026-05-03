package logger

import (
	"strings"
	"testing"

	"github.com/hoopoe-cockpit/hoopoe/apps/daemon/internal/redaction"
)

// The canonical redactor tests live in `apps/daemon/internal/redaction/`
// (per hp-je1p). The logger-side tests below cover the Entry integration
// (RedactEntry) and back-compat re-exports.

func TestNewRedactorRetainsCanonicalPatterns(t *testing.T) {
	r := NewRedactor()
	if r == nil {
		t.Fatal("NewRedactor returned nil")
	}
	out, events := r.RedactText(redaction.SurfaceLogger, "test", "token=sk-abcdef0123456789ABCDEF0123456789")
	if strings.Contains(out, "sk-abcdef") {
		t.Errorf("redactor did not scrub: %s", out)
	}
	if len(events) == 0 {
		t.Errorf("expected events, got none")
	}
}

// RedactEntry mutates msg + every value in fields. Envelope columns
// (component, jobId, ...) are NOT redacted.
func TestRedactEntryScrubsMsgAndFields(t *testing.T) {
	r := NewRedactor()
	e := &Entry{
		Msg: "calling backend with sk-abcdef0123456789ABCDEF0123456789",
		Fields: map[string]any{
			"argv":   []any{"--token", "ghp_abcdefghijklmnopqrstuvwxyz0123456789"},
			"benign": "just a value",
		},
	}
	events := RedactEntry(r, e)
	if strings.Contains(e.Msg, "sk-abcdef") {
		t.Errorf("msg not redacted: %s", e.Msg)
	}
	argv, ok := e.Fields["argv"].([]any)
	if !ok {
		t.Fatalf("argv not array")
	}
	if v, _ := argv[1].(string); strings.Contains(v, "ghp_") {
		t.Errorf("argv token not redacted: %v", v)
	}
	if e.Fields["benign"] != "just a value" {
		t.Errorf("benign field mutated: %v", e.Fields["benign"])
	}
	if len(events) < 2 {
		t.Errorf("expected ≥2 events, got %d", len(events))
	}
}

func TestRedactEntryNilSafe(t *testing.T) {
	if events := RedactEntry(nil, &Entry{Msg: "x"}); events != nil {
		t.Errorf("nil redactor should yield nil events")
	}
	r := NewRedactor()
	if events := RedactEntry(r, nil); events != nil {
		t.Errorf("nil entry should yield nil events")
	}
}

func TestRedactEntryEmptyEntryIsNoOp(t *testing.T) {
	r := NewRedactor()
	e := &Entry{Msg: "", Fields: nil}
	events := RedactEntry(r, e)
	if len(events) != 0 {
		t.Errorf("empty entry produced events: %v", events)
	}
}

// Surface label flows through into the trace events so audit-log replay
// tools can distinguish where the redaction happened.
func TestRedactEntryStampsLoggerSurface(t *testing.T) {
	r := NewRedactor()
	e := &Entry{
		Msg:    "leak sk-abcdef0123456789ABCDEF0123456789",
		Fields: nil,
	}
	events := RedactEntry(r, e)
	if len(events) == 0 {
		t.Fatal("expected events")
	}
	if events[0].Redactor != string(redaction.SurfaceLogger) {
		t.Errorf("expected redactor=%s, got %s", redaction.SurfaceLogger, events[0].Redactor)
	}
}
