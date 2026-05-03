package logger

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"testing"
	"time"
)

func fixedClockTime(t *testing.T) func() time.Time {
	t.Helper()
	stamp, err := time.Parse(time.RFC3339, "2026-05-04T00:00:00Z")
	if err != nil {
		t.Fatal(err)
	}
	return func() time.Time { return stamp }
}

func TestLoggerEmitsEnvelope(t *testing.T) {
	cap := NewCaptureTransport(0)
	l := New(Config{
		Component: ComponentDaemonAPI,
		MinLevel:  LevelDebug,
		Now:       fixedClockTime(t),
		Outputs:   []Transport{cap},
	})
	l.Info("ping", Field{"path", "/v1/health"}, Field{"status", 200})

	entries := cap.Entries()
	if len(entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(entries))
	}
	e := entries[0]
	if e.Level != LevelInfo {
		t.Errorf("level=%s", e.Level)
	}
	if e.Msg != "ping" {
		t.Errorf("msg=%s", e.Msg)
	}
	if e.Component != ComponentDaemonAPI {
		t.Errorf("component=%s", e.Component)
	}
	if e.Fields["path"] != "/v1/health" {
		t.Errorf("fields.path=%v", e.Fields["path"])
	}
	if e.Fields["status"] != 200 {
		t.Errorf("fields.status=%v", e.Fields["status"])
	}
	if e.TS.IsZero() {
		t.Error("ts not set")
	}
}

func TestLoggerLevelFiltering(t *testing.T) {
	cap := NewCaptureTransport(0)
	l := New(Config{
		Component: ComponentDaemonAPI,
		MinLevel:  LevelWarn,
		Outputs:   []Transport{cap},
	})
	l.Trace("trace1")
	l.Debug("debug1")
	l.Info("info1")
	l.Warn("warn1")
	l.Error("error1")
	entries := cap.Entries()
	if len(entries) != 2 {
		t.Fatalf("expected 2 entries (warn+error), got %d: %+v", len(entries), entries)
	}
	if entries[0].Level != LevelWarn || entries[1].Level != LevelError {
		t.Errorf("level filtering failed: %+v", entries)
	}
}

func TestLoggerWithScopesEnvelope(t *testing.T) {
	cap := NewCaptureTransport(0)
	l := New(Config{
		Component: ComponentDaemonAPI,
		MinLevel:  LevelDebug,
		Outputs:   []Transport{cap},
	}).With(
		Field{"correlationId", "corr-1"},
		Field{"jobId", "job-42"},
		Field{"actor", Actor{Kind: ActorSystem, ID: "scheduler"}},
		Field{"foo", "bar"}, // non-envelope → fields.foo
	)

	l.Info("hello")
	entries := cap.Entries()
	if len(entries) != 1 {
		t.Fatalf("entries=%d", len(entries))
	}
	e := entries[0]
	if e.CorrelationID != "corr-1" {
		t.Errorf("correlationId=%s", e.CorrelationID)
	}
	if e.JobID != "job-42" {
		t.Errorf("jobId=%s", e.JobID)
	}
	if e.Actor == nil || e.Actor.Kind != ActorSystem {
		t.Errorf("actor=%+v", e.Actor)
	}
	if e.Fields["foo"] != "bar" {
		t.Errorf("fields.foo=%v", e.Fields["foo"])
	}
}

func TestLoggerWithDoesNotMutateParent(t *testing.T) {
	cap := NewCaptureTransport(0)
	root := New(Config{
		Component: ComponentDaemonAPI,
		MinLevel:  LevelDebug,
		Outputs:   []Transport{cap},
	})
	child := root.With(Field{"correlationId", "child-only"})

	root.Info("from-root")
	child.Info("from-child")

	entries := cap.Entries()
	if len(entries) != 2 {
		t.Fatalf("entries=%d", len(entries))
	}
	if entries[0].CorrelationID != "" {
		t.Errorf("root carried child correlationId: %+v", entries[0])
	}
	if entries[1].CorrelationID != "child-only" {
		t.Errorf("child missing correlationId: %+v", entries[1])
	}
}

func TestLoggerRedactsBeforeBuffering(t *testing.T) {
	cap := NewCaptureTransport(0)
	l := New(Config{
		Component: ComponentDaemonAuth,
		MinLevel:  LevelDebug,
		Outputs:   []Transport{cap},
	})
	l.Info("token=sk-abcdef0123456789ABCDEF0123456789", Field{"argv", []any{
		"--key", "ghp_abcdefghijklmnopqrstuvwxyz0123456789",
	}})
	entries := cap.Entries()
	if len(entries) != 1 {
		t.Fatalf("entries=%d", len(entries))
	}
	e := entries[0]
	if strings.Contains(e.Msg, "sk-abcdef") {
		t.Errorf("msg leaked secret: %s", e.Msg)
	}
	argv, ok := e.Fields["argv"].([]any)
	if !ok {
		t.Fatalf("argv not array: %T", e.Fields["argv"])
	}
	if v, _ := argv[1].(string); strings.Contains(v, "ghp_") {
		t.Errorf("argv leaked secret: %v", v)
	}
}

func TestLoggerJSONEncoding(t *testing.T) {
	cap := NewCaptureTransport(0)
	l := New(Config{
		Component: ComponentDaemonAPI,
		MinLevel:  LevelDebug,
		Now:       fixedClockTime(t),
		Outputs:   []Transport{cap},
	}).With(Field{"correlationId", "corr-1"})

	l.Info("ping")
	lines := cap.JSONLines()
	var decoded Entry
	if err := json.Unmarshal([]byte(strings.TrimSpace(lines)), &decoded); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if decoded.Level != LevelInfo || decoded.CorrelationID != "corr-1" {
		t.Errorf("decoded=%+v", decoded)
	}
}

func TestCaptureTransportRingBuffer(t *testing.T) {
	cap := NewCaptureTransport(3)
	for i := 0; i < 5; i++ {
		cap.Emit(Entry{Msg: "msg" + intToStr(i)})
	}
	got := cap.Entries()
	if len(got) != 3 {
		t.Fatalf("len=%d", len(got))
	}
	// Should hold the last three: msg2, msg3, msg4
	want := []string{"msg2", "msg3", "msg4"}
	for i, e := range got {
		if e.Msg != want[i] {
			t.Errorf("entries[%d]=%s want %s", i, e.Msg, want[i])
		}
	}
}

func TestCaptureTransportLenAndReset(t *testing.T) {
	cap := NewCaptureTransport(2)
	cap.Emit(Entry{Msg: "a"})
	if cap.Len() != 1 {
		t.Errorf("len=%d", cap.Len())
	}
	cap.Reset()
	if cap.Len() != 0 {
		t.Errorf("after reset len=%d", cap.Len())
	}
}

func TestContextLoggerRoundTrip(t *testing.T) {
	cap := NewCaptureTransport(0)
	l := New(Config{
		Component: ComponentDaemonAPI,
		MinLevel:  LevelDebug,
		Outputs:   []Transport{cap},
	}).With(Field{"correlationId", "corr-x"})
	ctx := IntoContext(context.Background(), l)

	got := FromContext(ctx)
	got.Info("from-ctx")
	if cap.Len() != 1 {
		t.Fatalf("len=%d", cap.Len())
	}
	if cap.Entries()[0].CorrelationID != "corr-x" {
		t.Error("context-bound logger lost correlationId")
	}
}

func TestFromContextFallback(t *testing.T) {
	// Empty context returns a no-op logger. Calling .Info on it must NOT
	// panic and must NOT emit anywhere observable.
	l := FromContext(context.Background())
	if l == nil {
		t.Fatal("FromContext returned nil")
	}
	// No-op fallback is FATAL-level; sub-fatal calls drop silently.
	_ = l.Info("dropped")
}

func TestConcurrentEmits(t *testing.T) {
	cap := NewCaptureTransport(1024)
	l := New(Config{
		Component: ComponentDaemonAPI,
		MinLevel:  LevelDebug,
		Outputs:   []Transport{cap},
	})
	var wg sync.WaitGroup
	const goroutines = 16
	const perGoroutine = 50
	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			scoped := l.With(Field{"correlationId", "g" + intToStr(id)})
			for j := 0; j < perGoroutine; j++ {
				scoped.Info("hit")
			}
		}(i)
	}
	wg.Wait()
	if cap.Len() != goroutines*perGoroutine {
		t.Errorf("expected %d entries, got %d", goroutines*perGoroutine, cap.Len())
	}
}

func intToStr(i int) string {
	if i == 0 {
		return "0"
	}
	const digits = "0123456789"
	out := ""
	neg := false
	if i < 0 {
		neg = true
		i = -i
	}
	for i > 0 {
		out = string(digits[i%10]) + out
		i /= 10
	}
	if neg {
		out = "-" + out
	}
	return out
}
