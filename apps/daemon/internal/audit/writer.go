// Package audit owns the daemon audit-log writer. Entries are redacted before
// they are encoded or written to disk, sequence numbers are assigned while an
// advisory file lock is held, and redaction trace events are written without
// secret payloads.
package audit

import (
	"bufio"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/hoopoe-cockpit/hoopoe/apps/daemon/internal/redaction"
)

const (
	SchemaVersion       = 2
	legacySchemaVersion = 1
	GlobalProjectID     = "global"

	ActionRedactionTrace = "audit.redaction_trace"

	maxAuditLineBytes = 4 * 1024 * 1024
)

type ActorKind string

const (
	ActorUser         ActorKind = "user"
	ActorTendingJob   ActorKind = "tending_job"
	ActorAgent        ActorKind = "agent"
	ActorRepairAction ActorKind = "repair_action"
	ActorPreScript    ActorKind = "pre_script"
	ActorAdapter      ActorKind = "adapter"
	ActorSystem       ActorKind = "system"
)

type Result string

const (
	ResultSuccess          Result = "success"
	ResultFailure          Result = "failure"
	ResultPartial          Result = "partial"
	ResultApprovalRequired Result = "approval_required"
)

type Actor struct {
	Kind  ActorKind `json:"kind"`
	ID    string    `json:"id"`
	RunID string    `json:"runId,omitempty"`
}

type ArtifactRef struct {
	Kind   string `json:"kind"`
	ID     string `json:"id,omitempty"`
	URI    string `json:"uri,omitempty"`
	Digest string `json:"digest,omitempty"`
}

type Entry struct {
	SchemaVersion  int            `json:"schemaVersion"`
	EventID        string         `json:"eventId"`
	Seq            uint64         `json:"seq"`
	Time           time.Time      `json:"time"`
	ProjectID      string         `json:"projectId"`
	Actor          Actor          `json:"actor"`
	Action         string         `json:"action"`
	Reason         string         `json:"reason,omitempty"`
	CommandPreview string         `json:"commandPreview,omitempty"`
	Result         Result         `json:"result,omitempty"`
	ArtifactRefs   []ArtifactRef  `json:"artifactRefs,omitempty"`
	CorrelationID  string         `json:"correlationId,omitempty"`
	CausationID    string         `json:"causationId,omitempty"`
	ApprovalID     string         `json:"approvalId,omitempty"`
	Data           map[string]any `json:"data,omitempty"`
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
	file     *os.File
	path     string
	redactor *redaction.Redactor
	now      func() time.Time
	seqs     map[string]uint64
	index    *Index
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
	if cfg.Path == "" && cfg.Writer == nil {
		path, err := DefaultPath()
		if err != nil {
			return nil, err
		}
		cfg.Path = path
	}
	out := cfg.Writer
	var closer io.Closer
	var file *os.File
	var opened *os.File
	keepOpened := false
	defer func() {
		if opened != nil && !keepOpened {
			_ = opened.Close()
		}
	}()
	if cfg.Path != "" {
		if err := os.MkdirAll(filepath.Dir(cfg.Path), 0o750); err != nil {
			return nil, fmt.Errorf("audit: mkdir: %w", err)
		}
		var err error
		opened, err = os.OpenFile(cfg.Path, os.O_CREATE|os.O_RDWR|os.O_APPEND, 0o600)
		if err != nil {
			return nil, fmt.Errorf("audit: open: %w", err)
		}
		out = opened
		closer = opened
		file = opened
		keepOpened = true
	}
	if out == nil {
		return nil, fmt.Errorf("audit: writer or path is required")
	}
	writer := &Writer{
		out:      out,
		closer:   closer,
		file:     file,
		path:     cfg.Path,
		redactor: redactor,
		now:      now,
		seqs:     make(map[string]uint64),
		index:    NewIndex(nil),
	}
	if writer.path != "" {
		if err := writer.lockFile(); err != nil {
			_ = writer.Close()
			return nil, err
		}
		entries, err := readEntries(writer.path)
		unlockErr := writer.unlockFile()
		if err != nil {
			_ = writer.Close()
			return nil, err
		}
		if unlockErr != nil {
			_ = writer.Close()
			return nil, unlockErr
		}
		writer.index = NewIndex(entries)
		for _, entry := range entries {
			writer.recordSequenceLocked(entry.ProjectID, entry.Seq)
		}
	}
	return writer, nil
}

func DefaultPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("audit: resolve home: %w", err)
	}
	return filepath.Join(home, ".hoopoe", "audit.jsonl"), nil
}

func (w *Writer) Append(entry Entry) (Entry, []redaction.TraceEvent, error) {
	if w == nil {
		return Entry{}, nil, fmt.Errorf("audit: nil writer")
	}
	entry.Seq = 0
	entry = w.prepareEntry(entry)
	redacted, traces, err := w.redactEntry(entry)
	if err != nil {
		return Entry{}, nil, err
	}
	records := make([]Entry, 0, 1+len(traces))
	records = append(records, redacted)
	for _, trace := range traces {
		records = append(records, w.prepareEntry(traceEntry(trace, redacted)))
	}

	w.mu.Lock()
	defer w.mu.Unlock()
	if err := w.lockFile(); err != nil {
		return Entry{}, nil, err
	}
	locked := w.file != nil
	defer func() {
		if locked {
			_ = w.unlockFile()
		}
	}()
	if err := w.refreshSequencesFromFileLocked(projectsIn(records)); err != nil {
		return Entry{}, nil, err
	}
	for i := range records {
		records[i].Seq = w.nextSequenceLocked(records[i].ProjectID)
		if err := writeJSONLine(w.out, records[i]); err != nil {
			return Entry{}, nil, err
		}
		w.index.add(records[i])
	}
	if err := syncWriter(w.out); err != nil {
		return Entry{}, nil, err
	}
	return records[0], traces, nil
}

// Query returns audit entries matching `query` from the in-memory index.
// hp-1hwb: this no longer takes the writer mutex or re-reads the file on
// every call. The daemon is the single writer for its own audit-log path,
// so the in-memory index — bootstrapped from disk in NewWriter and updated
// incrementally by Append — is authoritative for runtime reads. The Index
// itself uses its own RWMutex so concurrent Queries do not block each
// other and do not block in-flight Appends (Appends only need the index
// lock for the duration of `add`, not for the whole file write).
func (w *Writer) Query(query Query) ([]Entry, error) {
	if w == nil {
		return nil, fmt.Errorf("audit: nil writer")
	}
	return w.index.Query(query), nil
}

func (w *Writer) RecentAuditEvents(projectID string, limit int) ([]Entry, error) {
	return w.Query(Query{
		ProjectID: projectID,
		Limit:     limit,
		Reverse:   true,
	})
}

func (w *Writer) Close() error {
	if w == nil || w.closer == nil {
		return nil
	}
	closer := w.closer
	if file, ok := closer.(*os.File); ok && file == w.file {
		w.file = nil
	}
	err := closer.Close()
	w.closer = nil
	return err
}

func (w *Writer) RedactionStats() redaction.Stats {
	if w == nil || w.redactor == nil {
		return redaction.Stats{SchemaVersion: redaction.SchemaVersion}
	}
	return w.redactor.SnapshotStats()
}

func (w *Writer) prepareEntry(entry Entry) Entry {
	entry.SchemaVersion = SchemaVersion
	if entry.EventID == "" {
		entry.EventID = newEventID()
	}
	if entry.Time.IsZero() {
		entry.Time = w.now().UTC()
	} else {
		entry.Time = entry.Time.UTC()
	}
	entry.ProjectID = normalizeProjectID(entry.ProjectID)
	return entry
}

func (w *Writer) redactEntry(entry Entry) (Entry, []redaction.TraceEvent, error) {
	if w.redactor == nil {
		return entry, nil, nil
	}
	body, err := json.Marshal(entry)
	if err != nil {
		return Entry{}, nil, fmt.Errorf("audit: encode before redaction: %w", err)
	}
	var value map[string]any
	if err := json.Unmarshal(body, &value); err != nil {
		return Entry{}, nil, fmt.Errorf("audit: decode before redaction: %w", err)
	}
	redactedValue, traces := w.redactor.RedactValue(redaction.SurfaceAudit, "audit", value)
	redactedMap, ok := redactedValue.(map[string]any)
	if !ok {
		return entry, traces, nil
	}
	redactedBody, err := json.Marshal(redactedMap)
	if err != nil {
		return Entry{}, nil, fmt.Errorf("audit: encode redacted entry: %w", err)
	}
	var redacted Entry
	if err := json.Unmarshal(redactedBody, &redacted); err != nil {
		return Entry{}, nil, fmt.Errorf("audit: decode redacted entry: %w", err)
	}
	return w.prepareEntry(redacted), traces, nil
}

func traceEntry(trace redaction.TraceEvent, source Entry) Entry {
	return Entry{
		SchemaVersion: SchemaVersion,
		EventID:       newEventID(),
		ProjectID:     source.ProjectID,
		Action:        ActionRedactionTrace,
		Time:          trace.Time,
		Actor:         Actor{Kind: ActorSystem, ID: "daemon.redaction"},
		Result:        ResultSuccess,
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

type Query struct {
	ProjectID     string
	ActorKind     ActorKind
	ActorID       string
	Action        string
	Result        Result
	CorrelationID string
	CausationID   string
	Search        string
	From          time.Time
	To            time.Time
	Limit         int
	Reverse       bool
}

// Index is the in-memory query side of the audit log. Append mutates it
// under the writer's serialization mutex; Query reads it under its own
// RWMutex so /v1/audit/query and /v1/audit/export do not block each other
// or block in-flight Appends (hp-1hwb). The file on disk is the durable
// canonical state; this index is rebuilt from the file at NewWriter time
// and incrementally updated thereafter — the daemon is the single writer
// for its own audit-log path, so an in-memory index is sufficient for
// runtime reads.
type Index struct {
	mu        sync.RWMutex
	entries   []Entry
	byProject map[string][]int
	byActor   map[string][]int
	byAction  map[string][]int
}

func LoadIndex(path string) (*Index, error) {
	entries, err := readEntries(path)
	if err != nil {
		return nil, err
	}
	return NewIndex(entries), nil
}

func NewIndex(entries []Entry) *Index {
	index := &Index{
		entries:   make([]Entry, 0, len(entries)),
		byProject: make(map[string][]int),
		byActor:   make(map[string][]int),
		byAction:  make(map[string][]int),
	}
	// Single-goroutine bootstrap; bypass the lock to avoid the defer/Unlock
	// overhead on potentially many entries.
	for _, entry := range entries {
		index.addLocked(entry)
	}
	return index
}

func (i *Index) Entries() []Entry {
	if i == nil {
		return nil
	}
	i.mu.RLock()
	defer i.mu.RUnlock()
	if len(i.entries) == 0 {
		return nil
	}
	out := make([]Entry, len(i.entries))
	copy(out, i.entries)
	return out
}

func (i *Index) Query(query Query) []Entry {
	if i == nil {
		return nil
	}
	i.mu.RLock()
	defer i.mu.RUnlock()
	if len(i.entries) == 0 {
		return nil
	}
	indexes := i.candidateIndexesLocked(query)
	out := make([]Entry, 0, len(indexes))
	for _, idx := range indexes {
		entry := i.entries[idx]
		if !query.matches(entry) {
			continue
		}
		out = append(out, entry)
	}
	sort.SliceStable(out, func(a, b int) bool {
		left := out[a]
		right := out[b]
		if query.Reverse {
			return laterEntry(left, right)
		}
		return earlierEntry(left, right)
	})
	if query.Limit > 0 && len(out) > query.Limit {
		out = out[:query.Limit]
	}
	return out
}

func (i *Index) add(entry Entry) {
	if i == nil {
		return
	}
	i.mu.Lock()
	defer i.mu.Unlock()
	i.addLocked(entry)
}

func (i *Index) addLocked(entry Entry) {
	entry.ProjectID = normalizeProjectID(entry.ProjectID)
	pos := len(i.entries)
	i.entries = append(i.entries, entry)
	i.byProject[entry.ProjectID] = append(i.byProject[entry.ProjectID], pos)
	if entry.Action != "" {
		i.byAction[entry.Action] = append(i.byAction[entry.Action], pos)
	}
	for _, key := range actorIndexKeys(entry.Actor.Kind, entry.Actor.ID) {
		i.byActor[key] = append(i.byActor[key], pos)
	}
}

func (i *Index) candidateIndexesLocked(query Query) []int {
	var candidates []int
	choose := func(next []int) {
		if next == nil {
			return
		}
		if candidates == nil || len(next) < len(candidates) {
			candidates = next
		}
	}
	if query.ProjectID != "" {
		choose(i.byProject[normalizeProjectID(query.ProjectID)])
	}
	if query.Action != "" {
		choose(i.byAction[query.Action])
	}
	if query.ActorKind != "" || query.ActorID != "" {
		choose(i.byActor[actorQueryKey(query.ActorKind, query.ActorID)])
	}
	if candidates == nil {
		candidates = make([]int, len(i.entries))
		for idx := range i.entries {
			candidates[idx] = idx
		}
		return candidates
	}
	out := make([]int, len(candidates))
	copy(out, candidates)
	return out
}

func (q Query) matches(entry Entry) bool {
	if q.ProjectID != "" && normalizeProjectID(q.ProjectID) != normalizeProjectID(entry.ProjectID) {
		return false
	}
	if q.ActorKind != "" && q.ActorKind != entry.Actor.Kind {
		return false
	}
	if q.ActorID != "" && q.ActorID != entry.Actor.ID {
		return false
	}
	if q.Action != "" && q.Action != entry.Action {
		return false
	}
	if q.Result != "" && q.Result != entry.Result {
		return false
	}
	if q.CorrelationID != "" && q.CorrelationID != entry.CorrelationID {
		return false
	}
	if q.CausationID != "" && q.CausationID != entry.CausationID {
		return false
	}
	if q.Search != "" && !entryMatchesSearch(entry, q.Search) {
		return false
	}
	if !q.From.IsZero() && entry.Time.Before(q.From) {
		return false
	}
	if !q.To.IsZero() && entry.Time.After(q.To) {
		return false
	}
	return true
}

func entryMatchesSearch(entry Entry, query string) bool {
	query = strings.ToLower(strings.TrimSpace(query))
	if query == "" {
		return true
	}
	haystack := []string{
		entry.EventID,
		entry.ProjectID,
		entry.Actor.ID,
		string(entry.Actor.Kind),
		entry.Action,
		string(entry.Result),
		entry.Reason,
		entry.CommandPreview,
		entry.CorrelationID,
		entry.CausationID,
		entry.ApprovalID,
	}
	for _, ref := range entry.ArtifactRefs {
		haystack = append(haystack, ref.Kind, ref.ID, ref.URI, ref.Digest)
	}
	for key, value := range entry.Data {
		haystack = append(haystack, key, fmt.Sprint(value))
	}
	for _, candidate := range haystack {
		if strings.Contains(strings.ToLower(candidate), query) {
			return true
		}
	}
	return false
}

type MigrationState struct {
	SchemaVersion     int       `json:"schemaVersion"`
	CurrentVersion    int       `json:"currentVersion"`
	SupportedVersions []int     `json:"supportedVersions"`
	AppliedAt         time.Time `json:"appliedAt"`
	Pending           bool      `json:"pending"`
}

func MigrationStatus(now time.Time) MigrationState {
	if now.IsZero() {
		now = time.Now()
	}
	return MigrationState{
		SchemaVersion:     SchemaVersion,
		CurrentVersion:    SchemaVersion,
		SupportedVersions: []int{legacySchemaVersion, SchemaVersion},
		AppliedAt:         now.UTC(),
		Pending:           false,
	}
}

func DecodeEntry(line []byte) (Entry, error) {
	line = []byte(strings.TrimSpace(string(line)))
	if len(line) == 0 {
		return Entry{}, fmt.Errorf("audit: empty line")
	}
	var header struct {
		SchemaVersion int `json:"schemaVersion"`
	}
	if err := json.Unmarshal(line, &header); err != nil {
		return Entry{}, fmt.Errorf("audit: decode schema header: %w", err)
	}
	switch header.SchemaVersion {
	case SchemaVersion:
		var entry Entry
		if err := json.Unmarshal(line, &entry); err != nil {
			return Entry{}, fmt.Errorf("audit: decode entry: %w", err)
		}
		return normalizeDecodedEntry(entry), nil
	case legacySchemaVersion:
		var legacy legacyEntry
		if err := json.Unmarshal(line, &legacy); err != nil {
			return Entry{}, fmt.Errorf("audit: decode legacy entry: %w", err)
		}
		return normalizeDecodedEntry(Entry{
			SchemaVersion: SchemaVersion,
			EventID:       legacy.EventID,
			Time:          legacy.Time,
			ProjectID:     GlobalProjectID,
			Actor: Actor{
				Kind: ActorKind(legacy.Actor.Kind),
				ID:   legacy.Actor.ID,
			},
			Action:        legacy.Type,
			CorrelationID: legacy.CorrelationID,
			CausationID:   legacy.CausationID,
			Data:          legacy.Data,
		}), nil
	default:
		return Entry{}, fmt.Errorf("audit: unsupported schema version %d", header.SchemaVersion)
	}
}

type legacyEntry struct {
	SchemaVersion int            `json:"schemaVersion"`
	EventID       string         `json:"eventId"`
	Type          string         `json:"type"`
	Time          time.Time      `json:"time"`
	Actor         legacyActor    `json:"actor"`
	CorrelationID string         `json:"correlationId,omitempty"`
	CausationID   string         `json:"causationId,omitempty"`
	Data          map[string]any `json:"data,omitempty"`
}

type legacyActor struct {
	Kind string `json:"kind"`
	ID   string `json:"id"`
}

func readEntries(path string) ([]Entry, error) {
	file, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("audit: open for read: %w", err)
	}
	defer file.Close()
	return readEntriesFrom(file)
}

func readEntriesFrom(reader io.Reader) ([]Entry, error) {
	scanner := bufio.NewScanner(reader)
	scanner.Buffer(make([]byte, 0, 64*1024), maxAuditLineBytes)
	var entries []Entry
	lineNumber := 0
	for scanner.Scan() {
		lineNumber++
		if strings.TrimSpace(scanner.Text()) == "" {
			continue
		}
		entry, err := DecodeEntry(scanner.Bytes())
		if err != nil {
			return nil, fmt.Errorf("audit: decode line %d: %w", lineNumber, err)
		}
		entries = append(entries, entry)
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("audit: scan: %w", err)
	}
	return entries, nil
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

func (w *Writer) lockFile() error {
	if w == nil || w.file == nil {
		return nil
	}
	if err := syscall.Flock(int(w.file.Fd()), syscall.LOCK_EX); err != nil {
		return fmt.Errorf("audit: lock: %w", err)
	}
	return nil
}

func (w *Writer) unlockFile() error {
	if w == nil || w.file == nil {
		return nil
	}
	if err := syscall.Flock(int(w.file.Fd()), syscall.LOCK_UN); err != nil {
		return fmt.Errorf("audit: unlock: %w", err)
	}
	return nil
}

func (w *Writer) refreshSequencesFromFileLocked(projects map[string]struct{}) error {
	if w.path == "" || len(projects) == 0 {
		return nil
	}
	entries, err := readEntries(w.path)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if _, ok := projects[normalizeProjectID(entry.ProjectID)]; ok {
			w.recordSequenceLocked(entry.ProjectID, entry.Seq)
		}
	}
	return nil
}

func (w *Writer) nextSequenceLocked(projectID string) uint64 {
	projectID = normalizeProjectID(projectID)
	next := w.seqs[projectID] + 1
	w.seqs[projectID] = next
	return next
}

func (w *Writer) recordSequenceLocked(projectID string, seq uint64) {
	projectID = normalizeProjectID(projectID)
	if seq > w.seqs[projectID] {
		w.seqs[projectID] = seq
	}
}

func projectsIn(entries []Entry) map[string]struct{} {
	out := make(map[string]struct{}, len(entries))
	for _, entry := range entries {
		out[normalizeProjectID(entry.ProjectID)] = struct{}{}
	}
	return out
}

func normalizeDecodedEntry(entry Entry) Entry {
	entry.ProjectID = normalizeProjectID(entry.ProjectID)
	if entry.SchemaVersion == 0 {
		entry.SchemaVersion = SchemaVersion
	}
	return entry
}

func normalizeProjectID(projectID string) string {
	if strings.TrimSpace(projectID) == "" {
		return GlobalProjectID
	}
	return projectID
}

func actorIndexKeys(kind ActorKind, id string) []string {
	keys := make([]string, 0, 3)
	if kind != "" {
		keys = append(keys, string(kind))
	}
	if id != "" {
		keys = append(keys, "\x00"+id)
	}
	if kind != "" && id != "" {
		keys = append(keys, actorQueryKey(kind, id))
	}
	return keys
}

func actorQueryKey(kind ActorKind, id string) string {
	switch {
	case kind != "" && id != "":
		return string(kind) + "\x00" + id
	case kind != "":
		return string(kind)
	case id != "":
		return "\x00" + id
	default:
		return ""
	}
}

func earlierEntry(left Entry, right Entry) bool {
	if !left.Time.Equal(right.Time) {
		return left.Time.Before(right.Time)
	}
	if left.ProjectID == right.ProjectID && left.Seq != right.Seq {
		return left.Seq < right.Seq
	}
	return left.EventID < right.EventID
}

func laterEntry(left Entry, right Entry) bool {
	if !left.Time.Equal(right.Time) {
		return left.Time.After(right.Time)
	}
	if left.ProjectID == right.ProjectID && left.Seq != right.Seq {
		return left.Seq > right.Seq
	}
	return left.EventID > right.EventID
}

func newEventID() string {
	var buf [12]byte
	if _, err := rand.Read(buf[:]); err != nil {
		return "audit_evt_unavailable"
	}
	return "audit_evt_" + hex.EncodeToString(buf[:])
}
