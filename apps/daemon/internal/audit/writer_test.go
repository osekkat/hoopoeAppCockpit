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

	correlated := NewIndex([]Entry{
		{
			SchemaVersion: SchemaVersion,
			EventID:       "evt_parent",
			Seq:           1,
			ProjectID:     "proj_a",
			Time:          base,
			Actor:         Actor{Kind: ActorUser, ID: "operator"},
			Action:        "audit.export_started",
			Result:        ResultSuccess,
			Reason:        "redacted slice requested",
			CorrelationID: "corr_audit",
			Data:          map[string]any{"range": "24h"},
		},
		{
			SchemaVersion: SchemaVersion,
			EventID:       "evt_child",
			Seq:           2,
			ProjectID:     "proj_a",
			Time:          base.Add(time.Second),
			Actor:         Actor{Kind: ActorSystem, ID: "daemon"},
			Action:        "audit.export_completed",
			Result:        ResultSuccess,
			CorrelationID: "corr_audit",
			CausationID:   "evt_parent",
			ArtifactRefs:  []ArtifactRef{{Kind: "audit_export", ID: "audit-slice-1"}},
		},
	})
	chain := correlated.Query(Query{CorrelationID: "corr_audit", Search: "audit-slice-1"})
	if len(chain) != 1 || chain[0].EventID != "evt_child" {
		t.Fatalf("correlation/search query = %#v, want evt_child", chain)
	}
	byCause := correlated.Query(Query{CausationID: "evt_parent", Result: ResultSuccess})
	if len(byCause) != 1 || byCause[0].EventID != "evt_child" {
		t.Fatalf("causation/result query = %#v, want evt_child", byCause)
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

// TestDecodeEntryRedactsLegacyData guards hp-yp9i: legacy v1 entries
// pre-date the redaction layer (commit 3b8c174). Their on-disk Data
// field may carry secrets that were never run through redact.RedactValue
// at append time. Pre-hp-yp9i, audit exports stamped Redacted: true on
// these entries even though the Data was unredacted. The fix re-runs
// legacy Data through the default audit redactor on read, so the export
// claim stays honest.
func TestDecodeEntryRedactsLegacyData(t *testing.T) {
	// Legacy v1 entry whose Data carries an Anthropic-shape secret.
	// Encoded as a Go string so the JSON literal stays readable.
	body := `{` +
		`"schemaVersion":1,` +
		`"eventId":"legacy_with_secret",` +
		`"type":"legacy.import",` +
		`"time":"1970-01-01T00:00:12Z",` +
		`"actor":{"kind":"system","id":"daemon"},` +
		`"data":{"note":"key is sk-ant-api-XXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXX-AAAAAAAAAA"}` +
		`}`
	entry, err := DecodeEntry([]byte(body))
	if err != nil {
		t.Fatalf("decode legacy entry: %v", err)
	}
	note, ok := entry.Data["note"].(string)
	if !ok {
		t.Fatalf("Data.note not a string: %+v", entry.Data)
	}
	if strings.Contains(note, "sk-ant-api") {
		t.Fatalf("legacy Data NOT redacted on read: %q", note)
	}
	// Redactor swaps the secret body for a sha256-tagged sentinel (e.g.
	// `[redacted-sha256:48f2bb67]`) — the literal token "redacted"
	// always appears in the sentinel.
	if !strings.Contains(note, "redacted") {
		t.Fatalf("legacy Data redaction sentinel missing: %q", note)
	}
}

// TestDecodeEntryLegacyEmptyDataIsAUnchanged confirms the
// fast-path: a legacy entry with no Data goes through the legacy
// branch without invoking the redactor, so a deployment that never
// had pre-redaction secrets pays nothing per read. This pins down
// the early-return in redactLegacyData.
func TestDecodeEntryLegacyEmptyDataIsUnchanged(t *testing.T) {
	body := `{"schemaVersion":1,"eventId":"legacy_no_data","type":"legacy.action","time":"1970-01-01T00:00:12Z","actor":{"kind":"system","id":"daemon"}}`
	entry, err := DecodeEntry([]byte(body))
	if err != nil {
		t.Fatalf("decode legacy entry: %v", err)
	}
	if len(entry.Data) != 0 {
		t.Fatalf("expected empty Data, got %+v", entry.Data)
	}
}

// TestSetLegacyDecodeRedactorOverride confirms the test-driver
// override hook: a custom redactor swapped in via SetLegacyDecodeRedactor
// is used on subsequent decodes. Restores the prior redactor at the
// end so it doesn't leak across tests.
func TestSetLegacyDecodeRedactorOverride(t *testing.T) {
	// Stub redactor that wraps everything in [STUB:...].
	stub := redaction.New(redaction.Config{})
	previous := SetLegacyDecodeRedactor(stub)
	defer SetLegacyDecodeRedactor(previous)

	body := `{"schemaVersion":1,"eventId":"e","type":"x","time":"1970-01-01T00:00:00Z","actor":{"kind":"system","id":"daemon"},"data":{"k":"v"}}`
	entry, err := DecodeEntry([]byte(body))
	if err != nil {
		t.Fatalf("decode legacy: %v", err)
	}
	if got := entry.Data["k"]; got != "v" {
		// A no-secret value passes through the default redactor unchanged.
		t.Fatalf("Data.k = %v, want \"v\"", got)
	}
}

// hp-ae4p: Index MaxEntries retention. Pre-fix the in-memory Index
// grew O(audit history) for the lifetime of the Writer; on a long-
// running tending fleet that's RSS pressure scaling with every
// recorded event. The fix bounds the Index at MaxEntries (default
// 50_000); older queries fall back to LoadIndex(path) which re-reads
// the full JSONL from disk.

func TestIndexEvictsOldestBeyondMaxEntries(t *testing.T) {
	const max = 5
	idx := NewIndexWithConfig(nil, IndexConfig{MaxEntries: max})
	for i := 0; i < max*2; i++ {
		idx.add(Entry{
			ProjectID: "p",
			Action:    "x",
			Actor:     Actor{Kind: ActorSystem, ID: "daemon"},
			Time:      time.Unix(int64(i), 0).UTC(),
			Data:      map[string]any{"i": i},
		})
	}
	got := idx.Entries()
	if len(got) != max {
		t.Fatalf("Entries len = %d, want %d", len(got), max)
	}
	for i, entry := range got {
		want := max + i
		if entry.Data["i"] != want {
			t.Fatalf("Entries[%d].Data.i = %v, want %d", i, entry.Data["i"], want)
		}
	}
}

func TestIndexEvictionRepairsSecondaryMaps(t *testing.T) {
	const max = 4
	idx := NewIndexWithConfig(nil, IndexConfig{MaxEntries: max})
	for i := 0; i < max*2; i++ {
		project := "p_a"
		if i%2 == 0 {
			project = "p_b"
		}
		actor := Actor{Kind: ActorSystem, ID: "daemon"}
		if i%3 == 0 {
			actor = Actor{Kind: ActorAgent, ID: "agent_1"}
		}
		action := "x.first"
		if i%2 == 1 {
			action = "x.second"
		}
		idx.add(Entry{
			ProjectID: project,
			Action:    action,
			Actor:     actor,
			Time:      time.Unix(int64(i), 0).UTC(),
		})
	}
	for _, q := range []Query{
		{ProjectID: "p_a"},
		{ProjectID: "p_b"},
		{ActorKind: ActorSystem, ActorID: "daemon"},
		{ActorKind: ActorAgent, ActorID: "agent_1"},
		{Action: "x.first"},
		{Action: "x.second"},
	} {
		got := idx.Query(q)
		if len(got) > max {
			t.Fatalf("Query %+v returned %d entries; cap is %d", q, len(got), max)
		}
	}
	combined := idx.Query(Query{})
	if len(combined) != max {
		t.Fatalf("unfiltered Query len = %d, want %d", len(combined), max)
	}
}

func TestIndexUnboundedWhenMaxEntriesZero(t *testing.T) {
	idx := NewIndex(nil)
	for i := 0; i < 100; i++ {
		idx.add(Entry{
			ProjectID: "p",
			Action:    "x",
			Actor:     Actor{Kind: ActorSystem, ID: "d"},
			Time:      time.Unix(int64(i), 0).UTC(),
		})
	}
	if got := idx.Entries(); len(got) != 100 {
		t.Fatalf("unbounded Entries len = %d, want 100", len(got))
	}
}

func TestIndexBootstrapAlreadyOverCapTrimsToWindow(t *testing.T) {
	const max = 10
	entries := make([]Entry, max*2)
	for i := range entries {
		entries[i] = Entry{
			ProjectID: "p",
			Action:    "x",
			Actor:     Actor{Kind: ActorSystem, ID: "d"},
			Time:      time.Unix(int64(i), 0).UTC(),
			Data:      map[string]any{"i": i},
		}
	}
	idx := NewIndexWithConfig(entries, IndexConfig{MaxEntries: max})
	got := idx.Entries()
	if len(got) != max {
		t.Fatalf("Entries len = %d, want %d", len(got), max)
	}
	if got[0].Data["i"] != max {
		t.Fatalf("Entries[0].Data.i = %v, want %d (oldest after trim)", got[0].Data["i"], max)
	}
}

func TestWriterAppliesDefaultMaxIndexEntries(t *testing.T) {
	w, err := NewWriter(Config{Writer: nopSyncWriter{}})
	if err != nil {
		t.Fatalf("new writer: %v", err)
	}
	if got := w.index.maxEntries; got != defaultIndexMaxEntries {
		t.Fatalf("index.maxEntries = %d, want default %d", got, defaultIndexMaxEntries)
	}
}

func TestWriterNegativeMaxIndexEntriesOptsOutOfBoundedRetention(t *testing.T) {
	w, err := NewWriter(Config{Writer: nopSyncWriter{}, MaxIndexEntries: -1})
	if err != nil {
		t.Fatalf("new writer: %v", err)
	}
	if got := w.index.maxEntries; got != 0 {
		t.Fatalf("index.maxEntries = %d, want 0 (unbounded)", got)
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

// BenchmarkWriterAppend pins the cost of a single audit Append on the
// in-memory writer (no file IO) so future regressions in the redaction
// path or index update are visible. hp-239z baseline.
func BenchmarkWriterAppend(b *testing.B) {
	now := time.Unix(2026, 0).UTC()
	writer, err := NewWriter(Config{
		Writer: nopSyncWriter{},
		Now:    func() time.Time { return now },
	})
	if err != nil {
		b.Fatalf("new writer: %v", err)
	}
	entry := Entry{
		ProjectID: "proj_bench",
		Action:    "bench.append",
		Actor:     Actor{Kind: ActorSystem, ID: "bench"},
		Result:    ResultSuccess,
		Data:      map[string]any{"k": "v"},
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, _, err := writer.Append(entry); err != nil {
			b.Fatalf("Append: %v", err)
		}
	}
}

// BenchmarkWriterQuery pins the cost of an audit Query post-hp-1hwb.
// Pre-hp-1hwb every Query took the writer mutex AND re-read the file;
// the new path delegates to the in-memory index under RLock. This
// benchmark establishes the baseline so we notice if anyone reverts.
func BenchmarkWriterQuery(b *testing.B) {
	now := time.Unix(2026, 0).UTC()
	writer, err := NewWriter(Config{
		Writer: nopSyncWriter{},
		Now:    func() time.Time { return now },
	})
	if err != nil {
		b.Fatalf("new writer: %v", err)
	}
	for i := 0; i < 1000; i++ {
		if _, _, err := writer.Append(Entry{
			ProjectID: "proj_bench",
			Action:    "bench.seed",
			Actor:     Actor{Kind: ActorSystem, ID: "bench"},
		}); err != nil {
			b.Fatalf("seed Append: %v", err)
		}
	}
	query := Query{ProjectID: "proj_bench", Limit: 50}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := writer.Query(query); err != nil {
			b.Fatalf("Query: %v", err)
		}
	}
}

type nopSyncWriter struct{}

func (nopSyncWriter) Write(p []byte) (int, error) { return len(p), nil }
func (nopSyncWriter) Sync() error                 { return nil }

func TestQueryAndAppendInterleaveWithoutDeadlockOrTearing(t *testing.T) {
	// hp-1hwb: Writer.Query no longer takes the writer mutex or re-reads
	// the file. Concurrent Queries + Appends must complete without
	// deadlock and without observing torn entries (an entry that's
	// half-written into the index while another goroutine reads it).
	dir := t.TempDir()
	now := time.Unix(2026, 0).UTC()
	writer, err := NewWriter(Config{
		Path: filepath.Join(dir, "audit.jsonl"),
		Now:  func() time.Time { return now },
	})
	if err != nil {
		t.Fatalf("new writer: %v", err)
	}
	defer writer.Close()

	const (
		writers       = 4
		readers       = 8
		appendsEach   = 50
		queriesEach   = 100
	)
	var wg sync.WaitGroup
	wg.Add(writers + readers)

	for w := 0; w < writers; w++ {
		go func(w int) {
			defer wg.Done()
			for i := 0; i < appendsEach; i++ {
				_, _, err := writer.Append(Entry{
					ProjectID: "proj_concurrent",
					Action:    "test.append",
					Actor:     Actor{Kind: ActorSystem, ID: "test"},
					Result:    ResultSuccess,
					Data:      map[string]any{"writer": w, "i": i},
				})
				if err != nil {
					t.Errorf("Append: %v", err)
					return
				}
			}
		}(w)
	}

	for r := 0; r < readers; r++ {
		go func() {
			defer wg.Done()
			for i := 0; i < queriesEach; i++ {
				entries, err := writer.Query(Query{ProjectID: "proj_concurrent", Limit: 10})
				if err != nil {
					t.Errorf("Query: %v", err)
					return
				}
				// Sanity: every returned entry must have a non-zero
				// EventID — the index would have to expose a partially
				// constructed Entry for this to fail.
				for _, entry := range entries {
					if entry.EventID == "" {
						t.Errorf("torn read: empty EventID")
						return
					}
				}
			}
		}()
	}

	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("timeout: Query may be blocking Append (regression of hp-1hwb)")
	}

	final, err := writer.Query(Query{ProjectID: "proj_concurrent"})
	if err != nil {
		t.Fatalf("final Query: %v", err)
	}
	if got, want := len(final), writers*appendsEach; got != want {
		t.Fatalf("final entry count = %d, want %d", got, want)
	}
}

func TestQueryDoesNotRereadFileAfterAppend(t *testing.T) {
	// hp-1hwb: previously every Query call took the writer mutex AND
	// re-read every line of the audit file from disk. With the in-memory
	// index now authoritative for runtime reads, an Append → Query
	// sequence should make zero ReadDir/Open syscalls against the
	// audit-log path. Asserting the absence of that is hard portably,
	// so we instead pin the contract that Query returns the just-
	// appended entry without any explicit refresh — proving the index
	// is the source of truth.
	dir := t.TempDir()
	now := time.Unix(3000, 0).UTC()
	writer, err := NewWriter(Config{
		Path: filepath.Join(dir, "audit.jsonl"),
		Now:  func() time.Time { return now },
	})
	if err != nil {
		t.Fatalf("new writer: %v", err)
	}
	defer writer.Close()

	if _, _, err := writer.Append(Entry{
		ProjectID: "proj_x",
		Action:    "test.fresh_append",
		Actor:     Actor{Kind: ActorSystem, ID: "x"},
	}); err != nil {
		t.Fatalf("Append: %v", err)
	}

	entries, err := writer.Query(Query{ProjectID: "proj_x"})
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if len(entries) != 1 || entries[0].Action != "test.fresh_append" {
		t.Fatalf("Query did not see just-appended entry: %+v", entries)
	}

	// Mutate the on-disk file out from under the writer (simulate an
	// outside process appending). The in-memory index should NOT see
	// this — it's the daemon's authoritative state for its own log;
	// cross-process visibility is out-of-scope for hp-1hwb.
	f, err := os.OpenFile(filepath.Join(dir, "audit.jsonl"), os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatalf("open for sneaky append: %v", err)
	}
	if _, err := f.WriteString(`{"schemaVersion":2,"eventId":"sneaky","action":"test.sneaky","projectId":"proj_x","time":"2026-05-04T00:00:00Z","actor":{"kind":"system","id":"sneaky"}}` + "\n"); err != nil {
		t.Fatalf("sneaky write: %v", err)
	}
	_ = f.Close()

	entries, err = writer.Query(Query{ProjectID: "proj_x"})
	if err != nil {
		t.Fatalf("Query after sneaky append: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("Query saw out-of-band file write — index rescan regression. entries=%+v", entries)
	}
}

// FuzzDecodeEntry guards hp-54bp: audit logs are append-only boundary
// artifacts. Replay or corruption of a JSONL line must surface as a
// wrapped "audit: ..." error, never a panic, and successful decodes
// must normalize the schema version (legacy v1 → current) and
// projectID (empty → GlobalProjectID) before the entry leaves
// DecodeEntry. Run with:
//
//	go test -fuzz=FuzzDecodeEntry -fuzztime=20s ./apps/daemon/internal/audit/
func FuzzDecodeEntry(f *testing.F) {
	// Current-schema entry (v2).
	f.Add([]byte(`{"schemaVersion":2,"eventId":"evt-1","seq":1,"time":"2026-01-01T00:00:00Z","projectId":"proj-1","actor":{"kind":"user","id":"u1"},"action":"x.test","result":"success"}`))
	// Legacy v1 entry (must migrate to v2 with GlobalProjectID).
	f.Add([]byte(`{"schemaVersion":1,"eventId":"evt-2","type":"x.legacy","time":"2026-01-01T00:00:00Z","actor":{"kind":"user","id":"u2"}}`))
	// Empty / whitespace-only — must error cleanly.
	f.Add([]byte(""))
	f.Add([]byte("   \t  \n"))
	// Malformed JSON.
	f.Add([]byte("{not json"))
	f.Add([]byte(`{"schemaVersion":2,`))
	// Wrong type for schemaVersion.
	f.Add([]byte(`{"schemaVersion":"two"}`))
	// Unsupported schema version.
	f.Add([]byte(`{"schemaVersion":99,"eventId":"future"}`))
	// Schema 2 with empty projectID — should normalize to GlobalProjectID.
	f.Add([]byte(`{"schemaVersion":2,"eventId":"e","projectId":""}`))
	// Deeply nested data field.
	f.Add([]byte(`{"schemaVersion":2,"eventId":"e","data":{"a":{"b":{"c":["x",1,null,true]}}}}`))
	// Larger array data — exercises decoder buffer growth without
	// approaching the 4 MiB scanner cap (which is enforced upstream
	// in readEntriesFrom, not in DecodeEntry itself).
	bigArr := append([]byte(`{"schemaVersion":2,"eventId":"big","data":{"items":[`), bytes.Repeat([]byte(`"x",`), 2048)...)
	bigArr = append(bigArr, []byte(`"x"]}}`)...)
	f.Add(bigArr)
	// JSON-escaped control chars (null, backspace, form-feed, tab) in
	// string fields — the JSON spec allows these as \uXXXX / \b / \f
	// escapes; the decoder must round-trip them without panic.
	f.Add([]byte("{\"schemaVersion\":2,\"eventId\":\"e\",\"action\":\"x\\u0000y\",\"reason\":\"\\b\\f\\t\"}"))
	// Unicode action / actor.id.
	f.Add([]byte(`{"schemaVersion":2,"eventId":"e","action":"漢字","actor":{"kind":"user","id":"αβγ"}}`))
	// Negative schemaVersion.
	f.Add([]byte(`{"schemaVersion":-1}`))
	// Float schemaVersion (should fail the int header decode cleanly).
	f.Add([]byte(`{"schemaVersion":2.5}`))

	f.Fuzz(func(t *testing.T, line []byte) {
		entry, err := DecodeEntry(line)
		if err != nil {
			// Errors must be wrapped with the "audit:" prefix; never
			// panic, never a bare json error.
			if !strings.Contains(err.Error(), "audit:") {
				t.Fatalf("DecodeEntry returned an unwrapped error for input %q: %v", line, err)
			}
			return
		}
		// Successful decodes must have a normalized schema version and
		// non-empty projectID (legacy migration → GlobalProjectID;
		// empty v2 → GlobalProjectID).
		if entry.SchemaVersion != SchemaVersion {
			t.Fatalf("decoded entry has SchemaVersion=%d, want %d (normalize)", entry.SchemaVersion, SchemaVersion)
		}
		if entry.ProjectID == "" {
			t.Fatalf("decoded entry has empty ProjectID; normalize should fall back to %q", GlobalProjectID)
		}
		// Round-trip: the decoded Entry must re-marshal without panic.
		// A fuzz finding that produced an unmarshalable Data field (e.g.
		// json.Number that can't round-trip) would surface here.
		if _, marshalErr := json.Marshal(entry); marshalErr != nil {
			t.Fatalf("decoded entry not re-marshalable: %v (input=%q, entry=%+v)", marshalErr, line, entry)
		}
	})
}

// TestReadEntriesFromRespectsMaxAuditLineBytes guards the scanner-side
// invariant from hp-54bp: a corrupted append that produces a line
// longer than maxAuditLineBytes (4 MiB) must surface as a clean
// "audit: scan: ..." error from readEntriesFrom rather than crashing
// or silently truncating.
func TestReadEntriesFromRespectsMaxAuditLineBytes(t *testing.T) {
	t.Parallel()
	// One line larger than maxAuditLineBytes followed by a newline so
	// the scanner is forced to attempt a single Scan over the entire
	// oversized payload.
	oversized := bytes.Repeat([]byte("x"), maxAuditLineBytes+512)
	oversized = append(oversized, '\n')
	_, err := readEntriesFrom(bytes.NewReader(oversized))
	if err == nil {
		t.Fatal("readEntriesFrom accepted a line > maxAuditLineBytes; want a wrapped scan error")
	}
	if !strings.Contains(err.Error(), "audit: scan") {
		t.Fatalf("err = %v, want a wrapped 'audit: scan: ...' error", err)
	}
}

// TestReadEntriesFromAcceptsLineUpToMaxAuditLineBytes confirms the
// boundary case in the opposite direction: a line at — but not over —
// the cap is accepted. This pins down the exact threshold so a future
// edit that lowers the cap or off-by-ones the scanner buffer is caught.
func TestReadEntriesFromAcceptsLineUpToMaxAuditLineBytes(t *testing.T) {
	t.Parallel()
	// Build a valid entry padded with a long action string to land just
	// under the cap. Reserve headroom for the JSON envelope and the
	// newline.
	prefix := []byte(`{"schemaVersion":2,"eventId":"e","projectId":"p","action":"`)
	suffix := []byte(`"}` + "\n")
	// bufio.Scanner returns ErrTooLong when a token is *larger* than
	// the max buffer size. A token equal to the cap is admissible.
	headroom := maxAuditLineBytes - len(prefix) - len(suffix)
	if headroom <= 0 {
		t.Skip("test envelope larger than maxAuditLineBytes; adjust prefix/suffix")
	}
	line := append([]byte{}, prefix...)
	line = append(line, bytes.Repeat([]byte("a"), headroom)...)
	line = append(line, suffix...)
	if len(line) > maxAuditLineBytes+1 {
		t.Fatalf("test built oversized line: len=%d", len(line))
	}
	entries, err := readEntriesFrom(bytes.NewReader(line))
	if err != nil {
		t.Fatalf("readEntriesFrom rejected at-cap line: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("got %d entries, want 1", len(entries))
	}
}
