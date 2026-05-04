// Package migrations is the daemon-side schema migration runner per
// plan.md §10.3 + §11.
//
// Every persisted table/file in the daemon has a schema version. On daemon
// startup, the runner compares the registry of known migrations against the
// `_hoopoe_schema_migrations` meta table, computes the pending set, and
// applies them in id order with backup-then-apply semantics. On any
// failure (preCheck assertion, SQL error, postCheck assertion, OOM), the
// transaction rolls back and the database file is restored from the
// pre-migration backup. The daemon then refuses to start, writing a
// structured error to journald so the operator can intervene via the
// Diagnostics "Roll back to previous version" action.
//
// Design invariants (mirrored in tests):
//
//   - Migration ids are monotonic and unique across the entire registry.
//     The registry refuses duplicate ids at registration time.
//   - Pending migrations apply in id order — never out of order, never in
//     parallel.
//   - Each migration runs in its own transaction; failure rolls back the
//     entire migration AND restores the on-disk backup AND surfaces a
//     classified error.
//   - The current schema version comes from the meta table, NOT from
//     `len(applied)`. A gap in the meta table (e.g., id=1, id=3 with id=2
//     missing) is treated as a corrupt registry — the runner refuses to
//     start.
//   - Reversible() is the contract for whether Down() is callable. Some
//     migrations (e.g., DROP COLUMN with data loss) are intrinsically
//     non-reversible; they declare Reversible() == false and Down()
//     returns ErrNotReversible.
//
// The runner is intentionally driver-agnostic — it talks to *sql.DB so
// the daemon's choice of modernc/sqlite (per plan.md §3) is independent
// of this package. Tests use modernc/sqlite for in-memory databases.
package migrations

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"sort"
	"sync"
	"time"
)

// MetaTableName is the SQLite table the runner uses to track applied
// migrations. The table is created on first Run() if absent. Names are
// underscore-prefixed to keep them out of the consumer's namespace.
const MetaTableName = "_hoopoe_schema_migrations"

// Migration is one schema change. Implementations are typically value
// types built via MigrationFunc; user code rarely implements this
// directly.
type Migration interface {
	// ID is monotonic across the registry; uniqueness enforced at registration.
	// Convention: zero-padded to 4 digits in the description (e.g., "0001 ...").
	ID() int

	// Description is a one-line human-readable summary; surfaced in
	// Diagnostics + journald error messages.
	Description() string

	// Up applies the migration inside the runner's transaction. If it
	// returns nil the runner runs PostCheck and then commits; if it
	// errors the runner rolls back the transaction AND restores the
	// pre-migration backup.
	Up(ctx context.Context, tx *sql.Tx) error

	// Down attempts to roll back the migration inside the runner's
	// transaction. Best-effort; some migrations are non-reversible and
	// MUST return ErrNotReversible from Down(). The runner refuses to
	// call Down() when Reversible() returns false.
	Down(ctx context.Context, tx *sql.Tx) error

	// Reversible reports whether Down() is meaningful. Non-reversible
	// migrations are flagged so the operator knows a rollback requires
	// restoring from backup rather than running Down().
	Reversible() bool

	// PreCheck asserts the assumed schema state BEFORE Up(). Catches
	// drift between what the migration expects and what's actually in
	// the database.
	PreCheck(ctx context.Context, tx *sql.Tx) error

	// PostCheck asserts the new schema state AFTER Up(). Catches a
	// migration that "succeeded" in the SQL sense but didn't produce
	// the structure it claimed.
	PostCheck(ctx context.Context, tx *sql.Tx) error
}

// MigrationFunc is the canonical Migration implementation; consumers
// build values of this type and Register them.
type MigrationFunc struct {
	IDValue          int
	DescriptionValue string
	UpFn             func(context.Context, *sql.Tx) error
	DownFn           func(context.Context, *sql.Tx) error
	PreCheckFn       func(context.Context, *sql.Tx) error
	PostCheckFn      func(context.Context, *sql.Tx) error
	IsReversible     bool
}

func (m MigrationFunc) ID() int             { return m.IDValue }
func (m MigrationFunc) Description() string { return m.DescriptionValue }
func (m MigrationFunc) Reversible() bool    { return m.IsReversible }

func (m MigrationFunc) Up(ctx context.Context, tx *sql.Tx) error {
	if m.UpFn == nil {
		return fmt.Errorf("migrations: migration %d has no Up function", m.IDValue)
	}
	return m.UpFn(ctx, tx)
}

func (m MigrationFunc) Down(ctx context.Context, tx *sql.Tx) error {
	if !m.IsReversible {
		return ErrNotReversible
	}
	if m.DownFn == nil {
		return fmt.Errorf("migrations: migration %d has no Down function but claims Reversible", m.IDValue)
	}
	return m.DownFn(ctx, tx)
}

func (m MigrationFunc) PreCheck(ctx context.Context, tx *sql.Tx) error {
	if m.PreCheckFn == nil {
		return nil
	}
	return m.PreCheckFn(ctx, tx)
}

func (m MigrationFunc) PostCheck(ctx context.Context, tx *sql.Tx) error {
	if m.PostCheckFn == nil {
		return nil
	}
	return m.PostCheckFn(ctx, tx)
}

// ErrNotReversible is returned by Down() when the migration is non-reversible.
// The operator MUST restore from backup instead.
var ErrNotReversible = errors.New("migrations: migration is not reversible (restore from backup)")

// ErrDuplicateID is returned by Registry.Register when the same id is
// registered twice.
var ErrDuplicateID = errors.New("migrations: duplicate migration id")

// ErrRegistryGap is returned by Runner.Run when the registry skips an id
// (e.g., 1, 2, 4 with 3 missing). Indicates a corrupted import.
var ErrRegistryGap = errors.New("migrations: registry has a gap (non-monotonic ids)")

// ErrAppliedGap is returned by Runner.Run when the meta table has a gap
// in applied ids (e.g., id=1, id=3 with id=2 missing). Indicates DB
// corruption or a bad manual edit.
var ErrAppliedGap = errors.New("migrations: meta table has a gap (non-monotonic applied ids)")

// Registry holds the ordered list of known migrations.
type Registry struct {
	mu         sync.Mutex
	migrations []Migration
}

// NewRegistry returns an empty Registry.
func NewRegistry() *Registry { return &Registry{} }

// Register adds m to the registry. Returns ErrDuplicateID if a migration
// with the same id is already registered. Migrations are kept in id
// order; insertion does an in-place sort (cheap; registries are small).
func (r *Registry) Register(m Migration) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, existing := range r.migrations {
		if existing.ID() == m.ID() {
			return fmt.Errorf("%w: id=%d (%q vs %q)",
				ErrDuplicateID, m.ID(), existing.Description(), m.Description())
		}
	}
	r.migrations = append(r.migrations, m)
	sort.Slice(r.migrations, func(i, j int) bool {
		return r.migrations[i].ID() < r.migrations[j].ID()
	})
	return nil
}

// MustRegister calls Register and panics on error. Convenient for package
// init() registration when the failure would be a programmer error.
func (r *Registry) MustRegister(m Migration) {
	if err := r.Register(m); err != nil {
		panic(err)
	}
}

// List returns a defensive copy of the registry in id order.
func (r *Registry) List() []Migration {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]Migration, len(r.migrations))
	copy(out, r.migrations)
	return out
}

// Count returns the number of registered migrations.
func (r *Registry) Count() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.migrations)
}

// validate asserts the registry has no gaps in ids (1, 2, 3, ...). Used
// by Runner.Run to refuse a corrupted registry.
func (r *Registry) validate() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	for i, m := range r.migrations {
		want := i + 1
		if m.ID() != want {
			return fmt.Errorf("%w: position %d has id=%d (expected %d)",
				ErrRegistryGap, i, m.ID(), want)
		}
	}
	return nil
}

// Backuper backs the database file up before applying a migration.
// Production uses FileBackuper (in backup.go); tests inject a fake.
type Backuper interface {
	// Backup is called BEFORE applying a pending migration. fromVersion
	// is the schema version the backup represents (i.e., the version
	// before the next migration applies). Returns the backup path on
	// success — used by the runner if it needs to restore.
	Backup(ctx context.Context, fromVersion int) (backupPath string, err error)

	// Restore copies a previously-created backup back over the live
	// database. Called by the runner on migration failure.
	Restore(ctx context.Context, backupPath string) error
}

// NoopBackuper is a Backuper that records calls but doesn't touch any
// files. Used by tests that don't care about real on-disk backup.
type NoopBackuper struct {
	BackupCalls  []int
	RestoreCalls []string
}

// Backup records the call and returns a synthetic path.
func (n *NoopBackuper) Backup(_ context.Context, fromVersion int) (string, error) {
	n.BackupCalls = append(n.BackupCalls, fromVersion)
	return fmt.Sprintf("noop-backup://daemon-%d.db", fromVersion), nil
}

// Restore records the call and returns nil.
func (n *NoopBackuper) Restore(_ context.Context, backupPath string) error {
	n.RestoreCalls = append(n.RestoreCalls, backupPath)
	return nil
}

// Logger is the minimal logging interface the runner needs. Compatible
// with the hp-lxs structured logger via a thin adapter.
type Logger interface {
	Info(ctx context.Context, msg string, fields map[string]any)
	Error(ctx context.Context, msg string, fields map[string]any)
}

// noopLogger swallows all log output. Used when no Logger is wired.
type noopLogger struct{}

func (noopLogger) Info(context.Context, string, map[string]any)  {}
func (noopLogger) Error(context.Context, string, map[string]any) {}

// Runner applies pending migrations against a *sql.DB.
type Runner struct {
	db       *sql.DB
	registry *Registry
	backuper Backuper
	logger   Logger
	now      func() time.Time
	mu       sync.Mutex // serializes Run() calls
}

// Config bundles the dependencies a Runner needs.
type Config struct {
	DB       *sql.DB
	Registry *Registry
	Backuper Backuper
	Logger   Logger          // optional — defaults to a no-op
	Now      func() time.Time // optional — defaults to time.Now
}

// New returns a Runner. Required: DB, Registry, Backuper. Logger and Now
// fall back to no-op + time.Now respectively.
func New(cfg Config) (*Runner, error) {
	if cfg.DB == nil {
		return nil, errors.New("migrations: nil DB")
	}
	if cfg.Registry == nil {
		return nil, errors.New("migrations: nil Registry")
	}
	if cfg.Backuper == nil {
		return nil, errors.New("migrations: nil Backuper")
	}
	logger := cfg.Logger
	if logger == nil {
		logger = noopLogger{}
	}
	now := cfg.Now
	if now == nil {
		now = time.Now
	}
	return &Runner{
		db:       cfg.DB,
		registry: cfg.Registry,
		backuper: cfg.Backuper,
		logger:   logger,
		now:      now,
	}, nil
}

// EnsureMetaTable creates the _hoopoe_schema_migrations table if absent.
// Idempotent. Called by CurrentVersion + Run on every invocation.
func (r *Runner) EnsureMetaTable(ctx context.Context) error {
	const stmt = `
		CREATE TABLE IF NOT EXISTS ` + MetaTableName + ` (
			id           INTEGER PRIMARY KEY,
			description  TEXT    NOT NULL,
			applied_at   TEXT    NOT NULL
		)
	`
	_, err := r.db.ExecContext(ctx, stmt)
	if err != nil {
		return fmt.Errorf("migrations: create meta table: %w", err)
	}
	return nil
}

// CurrentVersion reads the highest-applied migration id from the meta
// table. Returns 0 when the table is empty (i.e., a fresh database).
// Returns ErrAppliedGap if the meta table contains a gap.
func (r *Runner) CurrentVersion(ctx context.Context) (int, error) {
	if err := r.EnsureMetaTable(ctx); err != nil {
		return 0, err
	}
	rows, err := r.db.QueryContext(ctx, "SELECT id FROM "+MetaTableName+" ORDER BY id ASC")
	if err != nil {
		return 0, fmt.Errorf("migrations: read meta table: %w", err)
	}
	defer func() { _ = rows.Close() }()

	want := 1
	last := 0
	for rows.Next() {
		var id int
		if err := rows.Scan(&id); err != nil {
			return 0, fmt.Errorf("migrations: scan meta row: %w", err)
		}
		if id != want {
			return 0, fmt.Errorf("%w: meta row id=%d (expected %d)",
				ErrAppliedGap, id, want)
		}
		last = id
		want++
	}
	if err := rows.Err(); err != nil {
		return 0, fmt.Errorf("migrations: iterate meta rows: %w", err)
	}
	return last, nil
}

// Pending returns the migrations whose id > current version, in id order.
func (r *Runner) Pending(ctx context.Context) ([]Migration, error) {
	current, err := r.CurrentVersion(ctx)
	if err != nil {
		return nil, err
	}
	all := r.registry.List()
	pending := make([]Migration, 0, len(all))
	for _, m := range all {
		if m.ID() > current {
			pending = append(pending, m)
		}
	}
	return pending, nil
}

// Run applies all pending migrations in order. For each migration:
//
//  1. Backup the database file at the current version.
//  2. Begin a transaction.
//  3. PreCheck — if it fails, roll back the tx + return.
//  4. Up — if it fails, roll back the tx + restore backup + return.
//  5. PostCheck — if it fails, roll back the tx + restore backup + return.
//  6. Insert the meta row + commit. If commit fails, restore backup + return.
//
// Run holds an internal mutex so concurrent calls serialize. Returns the
// applied migrations on success; any error short-circuits and the
// remaining pending set stays unapplied.
func (r *Runner) Run(ctx context.Context) (applied []Migration, err error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if err := r.registry.validate(); err != nil {
		return nil, err
	}

	pending, err := r.Pending(ctx)
	if err != nil {
		return nil, err
	}
	if len(pending) == 0 {
		r.logger.Info(ctx, "migrations: nothing pending", nil)
		return nil, nil
	}

	current, err := r.CurrentVersion(ctx)
	if err != nil {
		return nil, err
	}

	for _, m := range pending {
		r.logger.Info(ctx, "migrations: applying", map[string]any{
			"id":          m.ID(),
			"description": m.Description(),
			"reversible":  m.Reversible(),
		})

		backupPath, err := r.backuper.Backup(ctx, current)
		if err != nil {
			r.logger.Error(ctx, "migrations: backup failed; refusing to apply", map[string]any{
				"id":    m.ID(),
				"error": err.Error(),
			})
			return applied, fmt.Errorf("migrations: backup before %d: %w", m.ID(), err)
		}

		tx, err := r.db.BeginTx(ctx, nil)
		if err != nil {
			return applied, fmt.Errorf("migrations: begin tx for %d: %w", m.ID(), err)
		}

		if err := m.PreCheck(ctx, tx); err != nil {
			_ = tx.Rollback()
			r.logger.Error(ctx, "migrations: pre-check failed", map[string]any{
				"id":    m.ID(),
				"error": err.Error(),
			})
			return applied, fmt.Errorf("migrations: pre-check %d: %w", m.ID(), err)
		}

		if err := m.Up(ctx, tx); err != nil {
			_ = tx.Rollback()
			if rerr := r.backuper.Restore(ctx, backupPath); rerr != nil {
				r.logger.Error(ctx, "migrations: up + restore both failed", map[string]any{
					"id":             m.ID(),
					"error":          err.Error(),
					"restore_error":  rerr.Error(),
					"backup_path":    backupPath,
				})
				return applied, fmt.Errorf("migrations: up %d failed: %w (restore also failed: %v)",
					m.ID(), err, rerr)
			}
			r.logger.Error(ctx, "migrations: up failed; restored from backup", map[string]any{
				"id":          m.ID(),
				"error":       err.Error(),
				"backup_path": backupPath,
			})
			return applied, fmt.Errorf("migrations: up %d: %w", m.ID(), err)
		}

		if err := m.PostCheck(ctx, tx); err != nil {
			_ = tx.Rollback()
			if rerr := r.backuper.Restore(ctx, backupPath); rerr != nil {
				return applied, fmt.Errorf("migrations: post-check %d failed: %w (restore also failed: %v)",
					m.ID(), err, rerr)
			}
			r.logger.Error(ctx, "migrations: post-check failed; restored from backup", map[string]any{
				"id":          m.ID(),
				"error":       err.Error(),
				"backup_path": backupPath,
			})
			return applied, fmt.Errorf("migrations: post-check %d: %w", m.ID(), err)
		}

		appliedAt := r.now().UTC().Format(time.RFC3339Nano)
		_, err = tx.ExecContext(ctx,
			"INSERT INTO "+MetaTableName+" (id, description, applied_at) VALUES (?, ?, ?)",
			m.ID(), m.Description(), appliedAt)
		if err != nil {
			_ = tx.Rollback()
			if rerr := r.backuper.Restore(ctx, backupPath); rerr != nil {
				return applied, fmt.Errorf("migrations: insert meta %d failed: %w (restore also failed: %v)",
					m.ID(), err, rerr)
			}
			return applied, fmt.Errorf("migrations: insert meta %d: %w", m.ID(), err)
		}

		if err := tx.Commit(); err != nil {
			if rerr := r.backuper.Restore(ctx, backupPath); rerr != nil {
				return applied, fmt.Errorf("migrations: commit %d failed: %w (restore also failed: %v)",
					m.ID(), err, rerr)
			}
			return applied, fmt.Errorf("migrations: commit %d: %w", m.ID(), err)
		}

		applied = append(applied, m)
		current = m.ID()
		r.logger.Info(ctx, "migrations: applied", map[string]any{
			"id":          m.ID(),
			"description": m.Description(),
			"applied_at":  appliedAt,
		})
	}

	return applied, nil
}
