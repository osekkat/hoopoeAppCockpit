package audit

import (
	"bufio"
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/hoopoe-cockpit/hoopoe/apps/daemon/internal/redaction"
)

func TestWriterRedactsFullAuditEntryBeforeJSONLPersistence(t *testing.T) {
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
		ProjectID: "proj_auth",
		Action:    "auth.bootstrap.exchanged",
		Actor: Actor{
			Kind:  ActorUser,
			ID:    "alice@example.com",
			RunID: "run_1",
		},
		CorrelationID:  "corr_1",
		CausationID:    "cmd_1",
		Reason:         "pairing token H-ABCDEFGHJKM was exchanged",
		CommandPreview: `curl -H "Authorization: Bearer eyJhbGciOiJIUzI1NiJ9.eyJzdWIiOiIxMjM0NTY3ODkwIn0.SflKxwRJSMeKKF2QT4fwpMeJf36POk6yJV_adQssw5c" --data '{"api_key":"sk-ant-abcdefghijklmnopqrstuvwxyz"}'`,
		Result:         ResultSuccess,
		ArtifactRefs: []ArtifactRef{{
			Kind: "job_log",
			URI:  "/home/alice/.ssh/id_ed25519",
		}},
		Data: map[string]any{
			"embedded_json": `{"openai":"sk-abcdefghijklmnopqrstuvwxyz123456","aws":"AKIAABCDEFGHIJKLMNOP","cookie":"__Secure-next-auth.session-token=abcdef"}`,
			"argv":          []any{"--identity", "~/.ssh/id_ed25519"},
		},
	})
	if err != nil {
		t.Fatalf("append: %v", err)
	}

	body := buf.String()
	for _, leak := range []string{
		"alice@example.com",
		"eyJhbGciOiJIUzI1NiJ9",
		"H-ABCDEFGHJKM",
		"sk-ant-abcdefghijklmnopqrstuvwxyz",
		"sk-abcdefghijklmnopqrstuvwxyz123456",
		"AKIAABCDEFGHIJKLMNOP",
		"__Secure-next-auth.session-token=abcdef",
		".ssh/id_ed25519",
	} {
		if strings.Contains(body, leak) {
			t.Fatalf("audit log leaked %q:\n%s", leak, body)
		}
	}
	if entry.SchemaVersion != SchemaVersion || entry.EventID == "" || entry.Seq != 1 || entry.ProjectID != "proj_auth" || !entry.Time.Equal(now) {
		t.Fatalf("entry defaults not applied: %+v", entry)
	}
	if entry.Action != "auth.bootstrap.exchanged" || entry.Result != ResultSuccess {
		t.Fatalf("entry action/result not preserved: %+v", entry)
	}
	if len(traces) < 6 {
		t.Fatalf("trace count = %d, want at least 6: %#v", len(traces), traces)
	}

	records := decodeLines(t, body)
	if len(records) != 1+len(traces) {
		t.Fatalf("jsonl records = %d, want %d\n%s", len(records), 1+len(traces), body)
	}
	if records[0]["action"] != "auth.bootstrap.exchanged" {
		t.Fatalf("first record action = %v", records[0]["action"])
	}
	if records[0]["seq"] != float64(1) {
		t.Fatalf("first record seq = %v", records[0]["seq"])
	}
	for idx, record := range records[1:] {
		if record["action"] != ActionRedactionTrace {
			t.Fatalf("trace record action = %v", record["action"])
		}
		if record["seq"] != float64(idx+2) {
			t.Fatalf("trace record seq = %v, want %d", record["seq"], idx+2)
		}
		if record["projectId"] != "proj_auth" || record["causationId"] != entry.EventID {
			t.Fatalf("trace did not point back to source: %#v", record)
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
		ProjectID: "proj_logs",
		Action:    "job.log.appended",
		Actor:     Actor{Kind: ActorAgent, ID: "ag_1"},
		Result:    ResultSuccess,
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
	recent, err := writer.RecentAuditEvents("proj_logs", 5)
	if err != nil {
		t.Fatalf("recent audit events: %v", err)
	}
	if len(recent) == 0 || recent[len(recent)-1].ProjectID != "proj_logs" {
		t.Fatalf("recent audit events missing project entry: %#v", recent)
	}
}

func TestWriterSyncsAfterAppend(t *testing.T) {
	sink := &syncBuffer{}
	writer, err := NewWriter(Config{Writer: sink})
	if err != nil {
		t.Fatalf("new writer: %v", err)
	}
	if _, _, err := writer.Append(Entry{
		ProjectID: "proj_sync",
		Action:    "diagnostics.probe",
		Actor:     Actor{Kind: ActorSystem, ID: "daemon"},
		Result:    ResultSuccess,
	}); err != nil {
		t.Fatalf("append: %v", err)
	}
	if sink.syncs != 1 {
		t.Fatalf("sync count = %d, want 1", sink.syncs)
	}
}

func TestWriterAssignsMonotonicPerProjectSeqUnderConcurrentWriters(t *testing.T) {
	path := filepath.Join(t.TempDir(), "audit.jsonl")
	writerA, err := NewWriter(Config{Path: path})
	if err != nil {
		t.Fatalf("new writer A: %v", err)
	}
	defer writerA.Close()
	writerB, err := NewWriter(Config{Path: path})
	if err != nil {
		t.Fatalf("new writer B: %v", err)
	}
	defer writerB.Close()

	const total = 80
	start := make(chan struct{})
	errs := make(chan error, total)
	var wg sync.WaitGroup
	for i := 0; i < total; i++ {
		writer := writerA
		if i%2 == 1 {
			writer = writerB
		}
		wg.Add(1)
		go func(w *Writer) {
			defer wg.Done()
			<-start
			_, _, err := w.Append(Entry{
				ProjectID: "proj_seq",
				Action:    "process.restart",
				Actor:     Actor{Kind: ActorTendingJob, ID: "tender"},
				Result:    ResultSuccess,
			})
			errs <- err
		}(writer)
	}
	close(start)
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatalf("append failed: %v", err)
		}
	}

	index, err := LoadIndex(path)
	if err != nil {
		t.Fatalf("load index: %v", err)
	}
	var seqEntries []Entry
	for _, entry := range index.Entries() {
		if entry.ProjectID == "proj_seq" && entry.Action == "process.restart" {
			seqEntries = append(seqEntries, entry)
		}
	}
	if len(seqEntries) != total {
		t.Fatalf("entries = %d, want %d", len(seqEntries), total)
	}
	seen := make(map[uint64]bool, total)
	var prev uint64
	for _, entry := range seqEntries {
		if entry.Seq <= prev {
			t.Fatalf("seq not monotonic in file order: prev=%d current=%d", prev, entry.Seq)
		}
		prev = entry.Seq
		seen[entry.Seq] = true
	}
	for seq := uint64(1); seq <= total; seq++ {
		if !seen[seq] {
			t.Fatalf("missing seq %d in %#v", seq, seen)
		}
	}

	if _, _, err := writerA.Append(Entry{ProjectID: "proj_other", Action: "other", Actor: Actor{Kind: ActorSystem, ID: "daemon"}}); err != nil {
		t.Fatalf("append other project: %v", err)
	}
	if _, _, err := writerA.Append(Entry{ProjectID: "proj_seq", Action: "process.restart", Actor: Actor{Kind: ActorSystem, ID: "daemon"}}); err != nil {
		t.Fatalf("append original project: %v", err)
	}
	index, err = LoadIndex(path)
	if err != nil {
		t.Fatalf("reload index: %v", err)
	}
	other := index.Query(Query{ProjectID: "proj_other"})
	if len(other) != 1 || other[0].Seq != 1 {
		t.Fatalf("other project seq = %#v, want seq 1", other)
	}
	seqProject := index.Query(Query{ProjectID: "proj_seq", Action: "process.restart"})
	if got := seqProject[len(seqProject)-1].Seq; got != total+1 {
		t.Fatalf("proj_seq last seq = %d, want %d", got, total+1)
	}
}

func TestIndexQueriesByProjectTimeActorAndAction(t *testing.T) {
	base := time.Unix(1000, 0).UTC()
	index := NewIndex([]Entry{
		{SchemaVersion: SchemaVersion, EventID: "evt_1", Seq: 1, ProjectID: "proj_a", Time: base.Add(1 * time.Second), Actor: Actor{Kind: ActorAgent, ID: "agent_1"}, Action: "job.start"},
		{SchemaVersion: SchemaVersion, EventID: "evt_2", Seq: 1, ProjectID: "proj_b", Time: base.Add(2 * time.Second), Actor: Actor{Kind: ActorUser, ID: "user_1"}, Action: "job.start"},
		{SchemaVersion: SchemaVersion, EventID: "evt_3", Seq: 2, ProjectID: "proj_a", Time: base.Add(3 * time.Second), Actor: Actor{Kind: ActorAgent, ID: "agent_1"}, Action: "job.restart"},
		{SchemaVersion: SchemaVersion, EventID: "evt_4", Seq: 3, ProjectID: "proj_a", Time: base.Add(4 * time.Second), Actor: Actor{Kind: ActorAgent, ID: "agent_2"}, Action: "job.restart"},
	})

	got := index.Query(Query{
		ProjectID: "proj_a",
		ActorKind: ActorAgent,
		ActorID:   "agent_1",
		Action:    "job.restart",
		From:      base.Add(2 * time.Second),
		To:        base.Add(4 * time.Second),
	})
	if len(got) != 1 || got[0].EventID != "evt_3" {
		t.Fatalf("filtered query = %#v, want evt_3", got)
	}

	recent := index.Query(Query{ProjectID: "proj_a", Reverse: true, Limit: 2})
	if len(recent) != 2 || recent[0].EventID != "evt_4" || recent[1].EventID != "evt_3" {
		t.Fatalf("recent query = %#v, want evt_4 then evt_3", recent)
	}
}

func TestDecodeEntryMigratesLegacySchema(t *testing.T) {
	line := []byte(`{"schemaVersion":1,"eventId":"legacy_evt","type":"legacy.action","time":"1970-01-01T00:00:12Z","actor":{"kind":"agent","id":"agent_1"},"correlationId":"corr_1","data":{"ok":true}}`)
	entry, err := DecodeEntry(line)
	if err != nil {
		t.Fatalf("decode legacy entry: %v", err)
	}
	if entry.SchemaVersion != SchemaVersion || entry.ProjectID != GlobalProjectID || entry.Action != "legacy.action" {
		t.Fatalf("legacy migration mismatch: %+v", entry)
	}
	if entry.Actor.Kind != ActorAgent || entry.Actor.ID != "agent_1" || entry.CorrelationID != "corr_1" {
		t.Fatalf("legacy actor/correlation mismatch: %+v", entry)
	}

	status := MigrationStatus(time.Unix(99, 0).UTC())
	if status.SchemaVersion != SchemaVersion || status.CurrentVersion != SchemaVersion || status.Pending {
		t.Fatalf("migration status mismatch: %+v", status)
	}
	if len(status.SupportedVersions) != 2 || status.SupportedVersions[0] != legacySchemaVersion || status.SupportedVersions[1] != SchemaVersion {
		t.Fatalf("supported versions mismatch: %+v", status.SupportedVersions)
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

type syncBuffer struct {
	bytes.Buffer
	syncs int
}

func (s *syncBuffer) Sync() error {
	s.syncs++
	return nil
}
