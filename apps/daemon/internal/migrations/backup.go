// backup.go — production Backuper that copies the SQLite database file
// before applying a migration.
//
// Backup path: <BackupDir>/daemon-<fromVersion>-<RFC3339-UTC-timestamp>.db
// Atomicity: write to <path>.tmp first, fsync, then os.Rename to the
// final path (POSIX atomic rename on the same filesystem).
//
// Retention policy (per §10.3):
//   - Keep the last KeepRecent backups (default 5).
//   - Keep every backup in the BackupDir/release/ subdirectory (release
//     boundaries — never pruned).
//
// Retention runs AFTER a successful migration; failed migrations never
// trigger pruning (their backup is the rollback target).
package migrations

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// FileBackuper is the production Backuper. It copies SourcePath →
// BackupDir/daemon-<fromVersion>-<ts>.db before each migration.
type FileBackuper struct {
	// SourcePath is the live database file (e.g.,
	// `~/.hoopoe/db/daemon.db`). Must be set before Backup().
	SourcePath string

	// BackupDir is the directory under which backups land. Created on
	// first Backup() if absent. Must be on the same filesystem as
	// SourcePath so os.Rename is atomic.
	BackupDir string

	// KeepRecent is the number of recent backups to retain. Defaults to
	// 5. Set to 0 to disable retention pruning entirely.
	KeepRecent int

	// Now is the clock — overridden in tests for deterministic output.
	Now func() time.Time
}

// NewFileBackuper returns a FileBackuper with default KeepRecent + Now.
func NewFileBackuper(sourcePath, backupDir string) *FileBackuper {
	return &FileBackuper{
		SourcePath: sourcePath,
		BackupDir:  backupDir,
		KeepRecent: 5,
		Now:        time.Now,
	}
}

// Backup copies SourcePath into BackupDir under a timestamped name and
// returns the final backup path.
func (b *FileBackuper) Backup(ctx context.Context, fromVersion int) (string, error) {
	if b.SourcePath == "" {
		return "", errors.New("migrations: FileBackuper.SourcePath is empty")
	}
	if b.BackupDir == "" {
		return "", errors.New("migrations: FileBackuper.BackupDir is empty")
	}
	if err := os.MkdirAll(b.BackupDir, 0o755); err != nil {
		return "", fmt.Errorf("migrations: mkdir backup dir: %w", err)
	}

	if _, err := os.Stat(b.SourcePath); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			// Fresh database: nothing to back up. Record this so the
			// runner has a non-empty path to pass to Restore() — which
			// will be a no-op against a missing source.
			placeholder := b.formatPath(fromVersion, b.now(), "absent")
			f, ferr := os.Create(placeholder)
			if ferr != nil {
				return "", fmt.Errorf("migrations: write absent-source placeholder: %w", ferr)
			}
			_ = f.Close()
			return placeholder, nil
		}
		return "", fmt.Errorf("migrations: stat source: %w", err)
	}

	finalPath := b.formatPath(fromVersion, b.now(), "")
	tmpPath := finalPath + ".tmp"

	if err := copyFile(ctx, b.SourcePath, tmpPath); err != nil {
		_ = os.Remove(tmpPath)
		return "", fmt.Errorf("migrations: copy to tmp: %w", err)
	}

	if err := os.Rename(tmpPath, finalPath); err != nil {
		_ = os.Remove(tmpPath)
		return "", fmt.Errorf("migrations: atomic rename: %w", err)
	}

	if b.KeepRecent > 0 {
		_ = b.prune()
	}
	return finalPath, nil
}

// Restore copies backupPath back over SourcePath, atomically. If the
// backup was created with the "absent" suffix (the source didn't exist
// at backup time), Restore() removes SourcePath instead so the source
// is left in the same "absent" state.
func (b *FileBackuper) Restore(ctx context.Context, backupPath string) error {
	if b.SourcePath == "" {
		return errors.New("migrations: FileBackuper.SourcePath is empty")
	}
	if backupPath == "" {
		return errors.New("migrations: Restore called with empty backup path")
	}

	if strings.Contains(filepath.Base(backupPath), "-absent.") {
		if err := os.Remove(b.SourcePath); err != nil && !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("migrations: remove source on absent-restore: %w", err)
		}
		return nil
	}

	tmpPath := b.SourcePath + ".restore.tmp"
	if err := copyFile(ctx, backupPath, tmpPath); err != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("migrations: copy backup to tmp: %w", err)
	}
	if err := os.Rename(tmpPath, b.SourcePath); err != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("migrations: atomic rename on restore: %w", err)
	}
	return nil
}

func (b *FileBackuper) now() time.Time {
	if b.Now != nil {
		return b.Now()
	}
	return time.Now()
}

func (b *FileBackuper) formatPath(fromVersion int, t time.Time, suffix string) string {
	ts := t.UTC().Format("2006-01-02T15-04-05.999999999Z")
	name := fmt.Sprintf("daemon-%d-%s.db", fromVersion, ts)
	if suffix != "" {
		name = fmt.Sprintf("daemon-%d-%s-%s.db", fromVersion, ts, suffix)
	}
	return filepath.Join(b.BackupDir, name)
}

// prune deletes old backups, keeping the most recent KeepRecent and
// preserving everything under BackupDir/release/. Errors bubble up to
// the caller; the runner currently swallows them (best-effort).
func (b *FileBackuper) prune() error {
	entries, err := os.ReadDir(b.BackupDir)
	if err != nil {
		return fmt.Errorf("migrations: read backup dir: %w", err)
	}

	type backupEntry struct {
		path string
		mod  time.Time
	}
	var candidates []backupEntry
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		if !strings.HasPrefix(e.Name(), "daemon-") || !strings.HasSuffix(e.Name(), ".db") {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		candidates = append(candidates, backupEntry{
			path: filepath.Join(b.BackupDir, e.Name()),
			mod:  info.ModTime(),
		})
	}

	if len(candidates) <= b.KeepRecent {
		return nil
	}

	sort.Slice(candidates, func(i, j int) bool {
		return candidates[i].mod.After(candidates[j].mod)
	})

	for _, c := range candidates[b.KeepRecent:] {
		if err := os.Remove(c.path); err != nil {
			return fmt.Errorf("migrations: prune backup %s: %w", c.path, err)
		}
	}
	return nil
}

// copyFile copies src to dst. The dst is overwritten if present. fsync
// is called on dst before close so the data hits the disk before we
// rename over it.
func copyFile(ctx context.Context, src, dst string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	srcFile, err := os.Open(src)
	if err != nil {
		return fmt.Errorf("open src: %w", err)
	}
	defer func() { _ = srcFile.Close() }()

	dstFile, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o644)
	if err != nil {
		return fmt.Errorf("open dst: %w", err)
	}
	defer func() { _ = dstFile.Close() }()

	if _, err := io.Copy(dstFile, srcFile); err != nil {
		return fmt.Errorf("io.Copy: %w", err)
	}
	if err := dstFile.Sync(); err != nil {
		return fmt.Errorf("fsync dst: %w", err)
	}
	return nil
}
