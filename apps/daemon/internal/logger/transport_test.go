package logger

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestWriterTransport(t *testing.T) {
	var buf bytes.Buffer
	transport := NewWriterTransport(&buf)
	transport.Emit(Entry{Level: LevelInfo, Msg: "hello"})
	transport.Emit(Entry{Level: LevelWarn, Msg: "watch"})

	lines := strings.Split(strings.TrimSpace(buf.String()), "\n")
	if len(lines) != 2 {
		t.Fatalf("lines=%d", len(lines))
	}
	for _, line := range lines {
		var e Entry
		if err := json.Unmarshal([]byte(line), &e); err != nil {
			t.Errorf("decode: %v", err)
		}
	}
}

func TestNullTransportDropsSilently(t *testing.T) {
	t.Parallel()
	transport := &NullTransport{}
	// Just verify no panic.
	transport.Emit(Entry{Msg: "anything"})
}

func TestFileTransportWritesAndRotates(t *testing.T) {
	dir := t.TempDir()
	transport, err := NewFileTransport(dir, "daemon.api")
	if err != nil {
		t.Fatal(err)
	}
	defer transport.Close()

	day1, _ := time.Parse(time.RFC3339, "2026-05-04T10:00:00Z")
	day2, _ := time.Parse(time.RFC3339, "2026-05-05T01:00:00Z")
	currentDay := day1
	transport.SetClock(func() time.Time { return currentDay })

	transport.Emit(Entry{Level: LevelInfo, Msg: "day1-a"})
	transport.Emit(Entry{Level: LevelInfo, Msg: "day1-b"})

	currentDay = day2
	transport.Emit(Entry{Level: LevelInfo, Msg: "day2-a"})

	// File for day1 should exist with two entries; day2 file with one.
	day1File := filepath.Join(dir, "daemon.api-2026-05-04.log")
	day2File := filepath.Join(dir, "daemon.api-2026-05-05.log")

	day1Bytes, err := os.ReadFile(day1File)
	if err != nil {
		t.Fatalf("day1 file: %v", err)
	}
	day1Lines := strings.Split(strings.TrimSpace(string(day1Bytes)), "\n")
	if len(day1Lines) != 2 {
		t.Errorf("day1 file lines=%d body=%s", len(day1Lines), string(day1Bytes))
	}

	day2Bytes, err := os.ReadFile(day2File)
	if err != nil {
		t.Fatalf("day2 file: %v", err)
	}
	day2Lines := strings.Split(strings.TrimSpace(string(day2Bytes)), "\n")
	if len(day2Lines) != 1 {
		t.Errorf("day2 file lines=%d", len(day2Lines))
	}
}

func TestFileTransportClosesCleanly(t *testing.T) {
	dir := t.TempDir()
	transport, err := NewFileTransport(dir, "daemon.api")
	if err != nil {
		t.Fatal(err)
	}
	transport.Emit(Entry{Level: LevelInfo, Msg: "hi"})
	if err := transport.Close(); err != nil {
		t.Errorf("close error: %v", err)
	}
	// Idempotent.
	if err := transport.Close(); err != nil {
		t.Errorf("second close error: %v", err)
	}
}

func TestCaptureTransportJSONLines(t *testing.T) {
	transport := NewCaptureTransport(10)
	transport.Emit(Entry{Level: LevelInfo, Msg: "first"})
	transport.Emit(Entry{Level: LevelWarn, Msg: "second"})
	out := transport.JSONLines()
	if !strings.Contains(out, `"first"`) || !strings.Contains(out, `"second"`) {
		t.Errorf("JSONLines missing entries: %s", out)
	}
	// Each line is independently parseable.
	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		var e Entry
		if err := json.Unmarshal([]byte(line), &e); err != nil {
			t.Errorf("invalid json line: %v", err)
		}
	}
}
