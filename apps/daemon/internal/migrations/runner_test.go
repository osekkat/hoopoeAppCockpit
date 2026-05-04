// runner_test.go — exercises the migration runner against modernc/sqlite
// in-memory databases. Covers the contract invariants from runner.go.
package migrations

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	_ "modernc.org/sqlite"
)

// helper: build an in-memory SQLite *sql.DB with shared cache so the
// runner sees the same instance across separate transactions.
func openMemDB(t *testing.T) *sql.DB {
	t.Helper()
	// `file:` URI with `cache=shared` keeps the in-memory DB alive
	// across connection-pool reuse for the test's lifetime.
	dsn := fmt.Sprintf("file:hp9xtt_%s?mode=memory&cache=shared", t.Name())
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	// Single connection for in-memory shared cache to behave predictably.
	db.SetMaxOpenConns(1)
	return db
}

// helper: build a test migration that creates a table.
func mkCreateTableMigration(id int, table string) Migration {
	return MigrationFunc{
		IDValue:          id,
		DescriptionValue: fmt.Sprintf("create %s", table),
		IsReversible:     true,
		UpFn: func(ctx context.Context, tx *sql.Tx) error {
			_, err := tx.ExecContext(ctx,
				fmt.Sprintf("CREATE TABLE %s (id INTEGER PRIMARY KEY, name TEXT)", table))
			return err
		},
		DownFn: func(ctx context.Context, tx *sql.Tx) error {
			_, err := tx.ExecContext(ctx, "DROP TABLE "+table)
			return err
		},
		PreCheckFn: func(ctx context.Context, tx *sql.Tx) error {
			var exists int
			err := tx.QueryRowContext(ctx,
				"SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name=?",
				table).Scan(&exists)
			if err != nil {
				return err
			}
			if exists != 0 {
				return fmt.Errorf("pre-check: %s already exists", table)
			}
			return nil
		},
		PostCheckFn: func(ctx context.Context, tx *sql.Tx) error {
			var exists int
			err := tx.QueryRowContext(ctx,
				"SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name=?",
				table).Scan(&exists)
			if err != nil {
				return err
			}
			if exists != 1 {
				return fmt.Errorf("post-check: %s missing after up", table)
			}
			return nil
		},
	}
}

func TestRegistryRefusesDuplicateID(t *testing.T) {
	t.Parallel()

	r := NewRegistry()
	if err := r.Register(mkCreateTableMigration(1, "a")); err != nil {
		t.Fatalf("first register: %v", err)
	}
	err := r.Register(mkCreateTableMigration(1, "b"))
	if !errors.Is(err, ErrDuplicateID) {
		t.Fatalf("expected ErrDuplicateID, got %v", err)
	}
}

func TestRegistryKeepsMigrationsInIDOrder(t *testing.T) {
	t.Parallel()

	r := NewRegistry()
	// Register out of order; List should return sorted.
	if err := r.Register(mkCreateTableMigration(3, "c")); err != nil {
		t.Fatalf("register 3: %v", err)
	}
	if err := r.Register(mkCreateTableMigration(1, "a")); err != nil {
		t.Fatalf("register 1: %v", err)
	}
	if err := r.Register(mkCreateTableMigration(2, "b")); err != nil {
		t.Fatalf("register 2: %v", err)
	}
	got := r.List()
	want := []int{1, 2, 3}
	for i, m := range got {
		if m.ID() != want[i] {
			t.Fatalf("position %d: got id=%d, want %d", i, m.ID(), want[i])
		}
	}
}

func TestRunAppliesPendingInOrderAndRecordsMeta(t *testing.T) {
	t.Parallel()

	db := openMemDB(t)
	registry := NewRegistry()
	registry.MustRegister(mkCreateTableMigration(1, "users"))
	registry.MustRegister(mkCreateTableMigration(2, "sessions"))
	registry.MustRegister(mkCreateTableMigration(3, "approvals"))

	backuper := &NoopBackuper{}
	runner, err := New(Config{DB: db, Registry: registry, Backuper: backuper})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	applied, err := runner.Run(context.Background())
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(applied) != 3 {
		t.Fatalf("expected 3 applied, got %d", len(applied))
	}

	current, err := runner.CurrentVersion(context.Background())
	if err != nil {
		t.Fatalf("CurrentVersion: %v", err)
	}
	if current != 3 {
		t.Fatalf("expected version 3, got %d", current)
	}

	// Each migration backed up exactly once before applying.
	if len(backuper.BackupCalls) != 3 {
		t.Fatalf("expected 3 backup calls, got %d", len(backuper.BackupCalls))
	}
	// No restores on a happy run.
	if len(backuper.RestoreCalls) != 0 {
		t.Fatalf("expected 0 restore calls, got %d", len(backuper.RestoreCalls))
	}

	// Verify each table actually exists.
	for _, table := range []string{"users", "sessions", "approvals"} {
		var exists int
		err := db.QueryRow(
			"SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name=?",
			table).Scan(&exists)
		if err != nil {
			t.Fatalf("verify %s: %v", table, err)
		}
		if exists != 1 {
			t.Fatalf("table %s not created", table)
		}
	}
}

func TestRunIsIdempotentWhenNothingPending(t *testing.T) {
	t.Parallel()

	db := openMemDB(t)
	registry := NewRegistry()
	registry.MustRegister(mkCreateTableMigration(1, "users"))

	backuper := &NoopBackuper{}
	runner, _ := New(Config{DB: db, Registry: registry, Backuper: backuper})

	if _, err := runner.Run(context.Background()); err != nil {
		t.Fatalf("first Run: %v", err)
	}
	// Second Run should be a no-op (no backup, no apply).
	applied, err := runner.Run(context.Background())
	if err != nil {
		t.Fatalf("second Run: %v", err)
	}
	if len(applied) != 0 {
		t.Fatalf("expected 0 applied on idempotent re-run, got %d", len(applied))
	}
	if len(backuper.BackupCalls) != 1 {
		t.Fatalf("expected 1 total backup call (only first run), got %d", len(backuper.BackupCalls))
	}
}

func TestRunRollsBackOnUpFailureAndRestoresBackup(t *testing.T) {
	t.Parallel()

	db := openMemDB(t)
	registry := NewRegistry()
	registry.MustRegister(mkCreateTableMigration(1, "first"))
	registry.MustRegister(MigrationFunc{
		IDValue:          2,
		DescriptionValue: "bad migration",
		IsReversible:     true,
		UpFn: func(_ context.Context, _ *sql.Tx) error {
			return errors.New("up exploded")
		},
	})
	registry.MustRegister(mkCreateTableMigration(3, "third"))

	backuper := &NoopBackuper{}
	runner, _ := New(Config{DB: db, Registry: registry, Backuper: backuper})

	applied, err := runner.Run(context.Background())
	if err == nil {
		t.Fatalf("expected error from bad migration, got nil")
	}
	if len(applied) != 1 {
		t.Fatalf("expected only migration 1 applied before failure, got %d", len(applied))
	}

	// Restore was called for the failed migration.
	if len(backuper.RestoreCalls) != 1 {
		t.Fatalf("expected 1 restore call after up failure, got %d", len(backuper.RestoreCalls))
	}

	// Schema version remains at 1 (the last successful migration).
	current, _ := runner.CurrentVersion(context.Background())
	if current != 1 {
		t.Fatalf("expected version 1 after rollback, got %d", current)
	}
}

func TestRunRollsBackOnPostCheckFailureAndRestoresBackup(t *testing.T) {
	t.Parallel()

	db := openMemDB(t)
	registry := NewRegistry()
	registry.MustRegister(MigrationFunc{
		IDValue:          1,
		DescriptionValue: "post-check liar",
		IsReversible:     true,
		UpFn: func(ctx context.Context, tx *sql.Tx) error {
			_, err := tx.ExecContext(ctx, "CREATE TABLE t (id INTEGER)")
			return err
		},
		PostCheckFn: func(_ context.Context, _ *sql.Tx) error {
			return errors.New("post-check decided up was wrong")
		},
	})

	backuper := &NoopBackuper{}
	runner, _ := New(Config{DB: db, Registry: registry, Backuper: backuper})

	_, err := runner.Run(context.Background())
	if err == nil {
		t.Fatalf("expected post-check failure")
	}
	if !strings.Contains(err.Error(), "post-check") {
		t.Fatalf("error should mention post-check, got %v", err)
	}
	if len(backuper.RestoreCalls) != 1 {
		t.Fatalf("expected restore call on post-check failure, got %d", len(backuper.RestoreCalls))
	}
}

func TestRunRefusesGapInRegistry(t *testing.T) {
	t.Parallel()

	db := openMemDB(t)
	registry := NewRegistry()
	registry.MustRegister(mkCreateTableMigration(1, "a"))
	registry.MustRegister(mkCreateTableMigration(3, "c")) // gap: 2 missing

	backuper := &NoopBackuper{}
	runner, _ := New(Config{DB: db, Registry: registry, Backuper: backuper})

	_, err := runner.Run(context.Background())
	if !errors.Is(err, ErrRegistryGap) {
		t.Fatalf("expected ErrRegistryGap, got %v", err)
	}
	if len(backuper.BackupCalls) != 0 {
		t.Fatalf("expected no backup attempts on registry-gap refusal, got %d", len(backuper.BackupCalls))
	}
}

func TestNonReversibleMigrationDownReturnsErrNotReversible(t *testing.T) {
	t.Parallel()

	m := MigrationFunc{
		IDValue:          1,
		DescriptionValue: "drops a column with data loss",
		IsReversible:     false,
		UpFn:             func(_ context.Context, _ *sql.Tx) error { return nil },
	}
	if err := m.Down(context.Background(), nil); !errors.Is(err, ErrNotReversible) {
		t.Fatalf("expected ErrNotReversible, got %v", err)
	}
}

func TestStateProviderReportsCurrentSchemaVersionAndPending(t *testing.T) {
	t.Parallel()

	db := openMemDB(t)
	registry := NewRegistry()
	registry.MustRegister(mkCreateTableMigration(1, "applied"))
	registry.MustRegister(mkCreateTableMigration(2, "pending_one"))
	registry.MustRegister(mkCreateTableMigration(3, "pending_two"))

	backuper := &NoopBackuper{}
	frozen := time.Date(2026, 5, 4, 12, 0, 0, 0, time.UTC)
	runner, _ := New(Config{
		DB:       db,
		Registry: registry,
		Backuper: backuper,
		Now:      func() time.Time { return frozen },
	})

	// Apply migration 1 only by trimming the registry temporarily.
	subset := NewRegistry()
	subset.MustRegister(mkCreateTableMigration(1, "applied"))
	subRunner, _ := New(Config{
		DB:       db,
		Registry: subset,
		Backuper: backuper,
		Now:      func() time.Time { return frozen },
	})
	if _, err := subRunner.Run(context.Background()); err != nil {
		t.Fatalf("apply subset: %v", err)
	}

	provider := NewRunnerStateProvider(runner)
	state, err := provider.State(context.Background())
	if err != nil {
		t.Fatalf("State: %v", err)
	}
	if state.SchemaVersion != 1 {
		t.Fatalf("expected schemaVersion 1, got %d", state.SchemaVersion)
	}
	if !state.AppliedAt.Equal(frozen) {
		t.Fatalf("expected appliedAt %v, got %v", frozen, state.AppliedAt)
	}
	if len(state.Pending) != 2 {
		t.Fatalf("expected 2 pending, got %d", len(state.Pending))
	}
	if !strings.Contains(state.Pending[0], "pending_one") {
		t.Fatalf("expected first pending to mention pending_one, got %q", state.Pending[0])
	}
}

func TestStateProviderReportsPhaseTransitions(t *testing.T) {
	t.Parallel()

	db := openMemDB(t)
	registry := NewRegistry()
	registry.MustRegister(mkCreateTableMigration(1, "users"))
	runner, _ := New(Config{DB: db, Registry: registry, Backuper: &NoopBackuper{}})
	provider := NewRunnerStateProvider(runner)

	// Default: idle.
	s, _ := provider.State(context.Background())
	if s.Phase != PhaseIdle {
		t.Fatalf("expected idle, got %q", s.Phase)
	}

	provider.SetPhase(PhaseRunning)
	s, _ = provider.State(context.Background())
	if s.Phase != PhaseRunning {
		t.Fatalf("expected running, got %q", s.Phase)
	}

	provider.SetPhase(PhaseFailed)
	s, _ = provider.State(context.Background())
	if s.Phase != PhaseFailed {
		t.Fatalf("expected failed, got %q", s.Phase)
	}

	provider.SetPhase(PhaseRollback)
	s, _ = provider.State(context.Background())
	if s.Phase != PhaseRollback {
		t.Fatalf("expected rolled_back, got %q", s.Phase)
	}
}

func TestFileBackuperHappyPath(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	source := filepath.Join(dir, "daemon.db")
	backupDir := filepath.Join(dir, "backups")

	// Create a fake source DB with known content.
	wantContent := []byte("SQLite format 3\x00 fake content")
	if err := writeFile(source, wantContent); err != nil {
		t.Fatalf("write source: %v", err)
	}

	frozen := time.Date(2026, 5, 4, 12, 0, 0, 0, time.UTC)
	b := &FileBackuper{
		SourcePath: source,
		BackupDir:  backupDir,
		KeepRecent: 5,
		Now:        func() time.Time { return frozen },
	}

	backupPath, err := b.Backup(context.Background(), 7)
	if err != nil {
		t.Fatalf("Backup: %v", err)
	}
	if !strings.Contains(filepath.Base(backupPath), "daemon-7-") {
		t.Fatalf("backup name should include daemon-7-, got %q", filepath.Base(backupPath))
	}

	gotContent, err := readFile(backupPath)
	if err != nil {
		t.Fatalf("read backup: %v", err)
	}
	if string(gotContent) != string(wantContent) {
		t.Fatalf("backup content mismatch")
	}

	// Restore by clobbering source then restoring.
	if err := writeFile(source, []byte("clobber me")); err != nil {
		t.Fatalf("clobber source: %v", err)
	}
	if err := b.Restore(context.Background(), backupPath); err != nil {
		t.Fatalf("Restore: %v", err)
	}
	gotRestored, _ := readFile(source)
	if string(gotRestored) != string(wantContent) {
		t.Fatalf("restore did not put original bytes back")
	}
}

func TestFileBackuperHandlesMissingSource(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	source := filepath.Join(dir, "daemon.db") // doesn't exist
	backupDir := filepath.Join(dir, "backups")

	b := NewFileBackuper(source, backupDir)
	backupPath, err := b.Backup(context.Background(), 0)
	if err != nil {
		t.Fatalf("Backup of absent source: %v", err)
	}
	if !strings.Contains(filepath.Base(backupPath), "-absent.") {
		t.Fatalf("expected absent suffix, got %q", filepath.Base(backupPath))
	}

	// Restore from an absent-marker should remove the source (or leave it absent).
	// First, create a source so we can verify it gets removed.
	if err := writeFile(source, []byte("should be removed")); err != nil {
		t.Fatalf("write source: %v", err)
	}
	if err := b.Restore(context.Background(), backupPath); err != nil {
		t.Fatalf("Restore from absent: %v", err)
	}
	if _, err := readFile(source); err == nil {
		t.Fatalf("expected source to be removed by absent-restore")
	}
}

func TestFileBackuperPrunesPastKeepRecent(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	source := filepath.Join(dir, "daemon.db")
	backupDir := filepath.Join(dir, "backups")
	if err := writeFile(source, []byte("source")); err != nil {
		t.Fatalf("write source: %v", err)
	}

	// Each Backup() call uses a unique timestamp; we move time forward
	// by hand so each backup file has a distinct mtime AND distinct path.
	base := time.Date(2026, 5, 4, 0, 0, 0, 0, time.UTC)
	now := base
	b := &FileBackuper{
		SourcePath: source,
		BackupDir:  backupDir,
		KeepRecent: 3,
		Now:        func() time.Time { return now },
	}

	for i := 0; i < 7; i++ {
		now = base.Add(time.Duration(i) * time.Minute)
		if _, err := b.Backup(context.Background(), i); err != nil {
			t.Fatalf("Backup %d: %v", i, err)
		}
	}

	entries, err := readBackupDir(backupDir)
	if err != nil {
		t.Fatalf("read backup dir: %v", err)
	}
	if len(entries) != 3 {
		t.Fatalf("expected 3 backups after pruning to KeepRecent=3, got %d (%v)", len(entries), entries)
	}
}
