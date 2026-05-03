package audit

import (
	"bufio"
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/hoopoe-cockpit/hoopoe/apps/daemon/internal/redaction"
)

func TestWriterRedactsBeforeJSONLPersistence(t *testing.T) {
	now := time.Unix(200, 0).UTC()
	var buf bytes.Buffer
	writer, err := NewWriter(Config{
		Writer: &buf,
		Now:    func() time.Time { return now },
	})
	if err != nil {
		t.Fatalf("new writer: %v", err)
	}

	entry, traces, err := writer.Append(Entry{
		Type: "auth.bootstrap.exchanged",
		Actor: Actor{
			Kind: "user",
			ID:   "alice@example.com",
		},
		CorrelationID: "corr_1",
		Data: map[string]any{
			"authorization": "Authorization: Bearer eyJhbGciOiJIUzI1NiJ9.eyJzdWIiOiIxMjM0NTY3ODkwIn0.SflKxwRJSMeKKF2QT4fwpMeJf36POk6yJV_adQssw5c",
			"pairing":       "H-ABCDEFGHJKM",
			"argv":          []any{"--identity", "~/.ssh/id_ed25519"},
		},
	})
	if err != nil {
		t.Fatalf("append: %v", err)
	}

	body := buf.String()
	for _, leak := range []string{"alice@example.com", "eyJhbGciOiJIUzI1NiJ9", "H-ABCDEFGHJKM", ".ssh/id_ed25519"} {
		if strings.Contains(body, leak) {
			t.Fatalf("audit log leaked %q:\n%s", leak, body)
		}
	}
	if entry.SchemaVersion != SchemaVersion || entry.EventID == "" || !entry.Time.Equal(now) {
		t.Fatalf("entry defaults not applied: %+v", entry)
	}
	if len(traces) < 3 {
		t.Fatalf("trace count = %d, want at least 3: %#v", len(traces), traces)
	}

	records := decodeLines(t, body)
	if len(records) != 1+len(traces) {
		t.Fatalf("jsonl records = %d, want %d\n%s", len(records), 1+len(traces), body)
	}
	if records[0]["type"] != "auth.bootstrap.exchanged" {
		t.Fatalf("first record type = %v", records[0]["type"])
	}
	for _, record := range records[1:] {
		if record["type"] != "audit.redaction_trace" {
			t.Fatalf("trace record type = %v", record["type"])
		}
		data, ok := record["data"].(map[string]any)
		if !ok {
			t.Fatalf("trace data missing: %#v", record)
		}
		if data["pattern_id"] == "" || data["bytes_redacted"] == float64(0) {
			t.Fatalf("trace data incomplete: %#v", data)
		}
	}
}

func TestWriterPersistsToFileAndStats(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "audit.jsonl")
	redactor := redaction.NewDefault()
	writer, err := NewWriter(Config{Path: path, Redactor: redactor})
	if err != nil {
		t.Fatalf("new writer: %v", err)
	}
	defer writer.Close()

	if _, _, err := writer.Append(Entry{
		Type:  "job.log.appended",
		Actor: Actor{Kind: "agent", ID: "ag_1"},
		Data: map[string]any{
			"line": "Set-Cookie: claude_session=abcdef",
		},
	}); err != nil {
		t.Fatalf("append: %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read audit file: %v", err)
	}
	if strings.Contains(string(body), "claude_session=abcdef") {
		t.Fatalf("file leaked cookie:\n%s", string(body))
	}
	stats := writer.RedactionStats()
	if len(stats.Patterns) == 0 {
		t.Fatal("expected redaction stats")
	}
}

func decodeLines(t *testing.T, body string) []map[string]any {
	t.Helper()
	var out []map[string]any
	scanner := bufio.NewScanner(strings.NewReader(body))
	for scanner.Scan() {
		var record map[string]any
		if err := json.Unmarshal(scanner.Bytes(), &record); err != nil {
			t.Fatalf("decode line %q: %v", scanner.Text(), err)
		}
		out = append(out, record)
	}
	if err := scanner.Err(); err != nil {
		t.Fatalf("scan: %v", err)
	}
	return out
}
