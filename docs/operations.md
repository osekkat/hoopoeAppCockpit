# Hoopoe daemon operations

> Operator-facing reference for the daemon's startup, migrations, backups, and rollback procedures. Source of truth: `plan.md §10.3` (retention/compaction/migrations) + `§11` (packaging + upgrades).

## Daemon database paths

| Path                                 | Purpose                                                                         |
| ------------------------------------ | ------------------------------------------------------------------------------- |
| `~/.hoopoe/db/daemon.db`             | Live SQLite database (job registry, tending state, audit log, auth bearers).    |
| `~/.hoopoe/db/backups/`              | Pre-migration backups (`daemon-<old-version>-<RFC3339-UTC-timestamp>.db`).      |
| `~/.hoopoe/db/backups/release/`      | Pinned release-boundary backups (never pruned by retention).                    |
| `~/.hoopoe/logs/`                    | Structured logs (per-job + daemon).                                             |
| `~/.hoopoe/audit.jsonl`              | Append-only audit log (every state-changing call).                              |
| `~/.hoopoe/skills.lock.json`         | Per-project skill SHA-256 pins (when `jsm` is the loader).                      |

## Schema migration model

Every persisted table/file in the daemon declares a schema version. On daemon startup, the migration runner (`apps/daemon/internal/migrations`) compares the registry of known migrations against the `_hoopoe_schema_migrations` meta table, computes the pending set, and applies them in id order with backup-before-apply semantics.

Migration shape:

```go
type Migration interface {
    ID() int                               // monotonic; e.g., 1, 2, 3...
    Description() string                   // 1-line human-readable
    Up(ctx, tx) error                      // applies the migration
    Down(ctx, tx) error                    // best-effort rollback (or ErrNotReversible)
    Reversible() bool                      // false → operator MUST restore from backup
    PreCheck(ctx, tx) error                // assert assumed schema state before Up
    PostCheck(ctx, tx) error               // assert new schema state after Up
}
```

For each pending migration the runner:

1. **Backs up** `~/.hoopoe/db/daemon.db` → `~/.hoopoe/db/backups/daemon-<old>-<ts>.db` (atomic via `tmp + fsync + rename`).
2. Begins a transaction.
3. Runs `PreCheck`. On failure: `tx.Rollback()` + return classified error.
4. Runs `Up`. On failure: `tx.Rollback()` + restore backup + return classified error.
5. Runs `PostCheck`. On failure: `tx.Rollback()` + restore backup + return classified error.
6. `INSERT INTO _hoopoe_schema_migrations(id, description, applied_at)` + `tx.Commit()`. On failure: restore backup + return classified error.

The runner refuses to start when the registry has an id gap (`ErrRegistryGap`) or the meta table has an applied gap (`ErrAppliedGap`) — both indicate corruption that needs operator attention rather than silent fixup.

## Backup retention

Retention runs **after a successful migration**; failed migrations never trigger pruning (their backup is the rollback target). Default policy:

- Keep the last **5** backups in `~/.hoopoe/db/backups/`.
- Keep **every** file in `~/.hoopoe/db/backups/release/` (pinned at release boundaries; never pruned).

The retention count is configurable via `FileBackuper.KeepRecent`. Set to `0` to disable retention pruning entirely (manual janitor required).

## Rollback procedure

When a migration fails, the daemon:

1. Rolls back the transaction.
2. Restores the live database from the pre-migration backup.
3. Refuses to bind its HTTP listener (`exit code 1`).
4. Writes a structured error to journald with the failed-migration id + classification.

systemd's `Restart=on-failure` typically re-launches the daemon, which re-tries the same migration set. Without operator intervention this loops; the daemon is intentionally NOT designed to skip a failing migration on its own.

### Operator action — Diagnostics "Roll back to previous version"

The desktop's Diagnostics panel exposes a one-click rollback action that:

1. Downloads the previous daemon binary (matching the backup's recorded version).
2. Verifies the checksum + signature + provenance attestation.
3. Stops the failed daemon service.
4. Swaps the binary on disk.
5. Restarts the service.
6. Records the rollback as an audit event with the operator's actor ID + the failed-migration id + the restored binary version.

If the previous binary is unavailable (e.g., release pruning), the action falls back to "restore database backup only" and the operator decides whether to manually downgrade.

### Operator action — manual rollback

If Diagnostics is unreachable (e.g., daemon refuses to bind so the desktop can't talk to it):

1. Stop the daemon service: `systemctl --user stop hoopoed`.
2. Identify the most recent backup matching the FAILED schema version: `ls -lt ~/.hoopoe/db/backups/`.
3. Restore manually: `cp ~/.hoopoe/db/backups/daemon-<v>-<ts>.db ~/.hoopoe/db/daemon.db`.
4. Inspect the failed migration's id + description (search journald for `migrations: up failed` or `migrations: post-check failed`).
5. Either downgrade the daemon binary (`hoopoe install --version <previous>`) OR fix the migration in the next daemon release.
6. Restart: `systemctl --user start hoopoed`.

### journald error format

The runner emits structured-log entries via the hp-lxs logger surface. Failed migrations look like:

```json
{
  "ts":"2026-05-04T12:34:56.789Z",
  "level":"error",
  "msg":"migrations: up failed; restored from backup",
  "component":"daemon-migrations",
  "fields":{
    "id":7,
    "error":"create table foo: SQL syntax error near 'CREAT'",
    "backup_path":"/home/user/.hoopoe/db/backups/daemon-6-2026-05-04T12-34-55.123Z.db"
  }
}
```

Useful filters:

```bash
journalctl --user -u hoopoed | grep '"daemon-migrations"'
journalctl --user -u hoopoed | grep -E 'migrations:.*(failed|restored)'
```

## How to add a migration

1. **Pick the next monotonic id.** Convention: `0001_create_jobs_table`, `0002_add_idempotency_key`, etc. — zero-padded so `ls`-sorted output matches application order.

2. **Write the migration as a `MigrationFunc` value** in `apps/daemon/internal/migrations/manifest/<id>_<short_name>.go`:

   ```go
   package manifest

   import (
       "context"
       "database/sql"

       "github.com/hoopoe-cockpit/hoopoe/apps/daemon/internal/migrations"
   )

   var Migration0007AddIdempotencyKey = migrations.MigrationFunc{
       IDValue:          7,
       DescriptionValue: "add idempotency_key column to jobs",
       IsReversible:     true,
       UpFn: func(ctx context.Context, tx *sql.Tx) error {
           _, err := tx.ExecContext(ctx,
               "ALTER TABLE jobs ADD COLUMN idempotency_key TEXT")
           return err
       },
       DownFn: func(ctx context.Context, tx *sql.Tx) error {
           // SQLite ALTER TABLE DROP COLUMN landed in 3.35; pre-3.35 needs
           // table-rebuild. The migration file should declare the SQLite
           // version assumption in its description.
           _, err := tx.ExecContext(ctx,
               "ALTER TABLE jobs DROP COLUMN idempotency_key")
           return err
       },
       PreCheckFn: func(ctx context.Context, tx *sql.Tx) error {
           // Assert the column does NOT exist yet.
           rows, err := tx.QueryContext(ctx, "PRAGMA table_info(jobs)")
           if err != nil {
               return err
           }
           defer rows.Close()
           for rows.Next() {
               var (
                   cid     int
                   name    string
                   ctype   string
                   notnull int
                   dflt    sql.NullString
                   pk      int
               )
               if err := rows.Scan(&cid, &name, &ctype, &notnull, &dflt, &pk); err != nil {
                   return err
               }
               if name == "idempotency_key" {
                   return fmt.Errorf("pre-check: idempotency_key already exists")
               }
           }
           return nil
       },
       PostCheckFn: func(ctx context.Context, tx *sql.Tx) error {
           // Assert the column NOW exists.
           // ... mirror the PRAGMA scan, expect to find the column ...
       },
   }
   ```

3. **Register the migration** in the manifest's `init()` (or a central registry-construction function):

   ```go
   // apps/daemon/internal/migrations/manifest/manifest.go
   func DefaultRegistry() *migrations.Registry {
       r := migrations.NewRegistry()
       r.MustRegister(Migration0001CreateJobsTable)
       r.MustRegister(Migration0002CreateAuditTable)
       // ...
       r.MustRegister(Migration0007AddIdempotencyKey)
       return r
   }
   ```

4. **Add a contract test** that exercises the migration end-to-end against an in-memory SQLite (modernc/sqlite). The runner test suite shows the pattern: register the migration set, call `Run()`, verify the resulting schema + meta table.

5. **Update `packages/schemas/openapi.yaml`** if the migration changes a wire-visible shape. The `schemaVersion` field on the affected entity stays the same number unless the WIRE shape changed (which is a different event from a STORAGE-shape change).

6. **Document the migration's rollback story** in the file's header comment if it's non-reversible. Operators read this before deciding whether to attempt `Down()` or restore from backup.

## Compatibility surface

`/v1/compatibility` (per `packages/schemas/openapi.yaml`) returns the live migration state:

```json
{
  "schemaVersion": 7,
  "appliedAt": "2026-05-04T12:34:56.789Z",
  "pending": ["0008 add foo column", "0009 split sessions table"],
  "phase": "idle"
}
```

The desktop's chrome consumes this surface to decide whether to refuse writes (when `phase == "running" || "failed"`) and to render the Diagnostics migration-state panel. The wire shape lives in `packages/schemas/openapi.yaml` and is mirrored 1:1 into `apps/daemon/internal/migrations/state.go`'s `State` type.

## See also

- `plan.md §10.3` — retention, compaction, migrations.
- `plan.md §11` — daemon distribution + upgrade flow.
- `plan.md §18.4` release smoke check #4 — daemon upgrade backs up config/db.
- `apps/daemon/internal/migrations/runner.go` — the runner contract + invariants.
- `apps/daemon/internal/migrations/backup.go` — FileBackuper + retention policy.
- `apps/daemon/internal/migrations/state.go` — `/v1/compatibility` migration-state surface.
- hp-r3i — `packages/schemas/src/schema-versions.test.ts` enumerates the persisted entities + asserts each declares `schemaVersion`.
- hp-4iz — daemon upgrade flow (consumer of this runner).
- hp-o42 — backup/export/restore.
