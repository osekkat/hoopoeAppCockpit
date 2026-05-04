package worktreecleanup

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/hoopoe-cockpit/hoopoe/apps/daemon/internal/scheduler"
)

func TestSweepCleansExpiredHealthWorktreesAndLeavesFreshRuns(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	now := time.Date(2026, 5, 4, 14, 0, 0, 0, time.UTC)
	root := t.TempDir()
	for i := 0; i < 100; i++ {
		createRunDir(t, root, "proj-a", KindHealth, "old-health-"+itoa3(i), now.Add(-4*24*time.Hour))
	}
	fresh := createRunDir(t, root, "proj-a", KindHealth, "fresh-health", now.Add(-time.Hour))
	reviewOld := createRunDir(t, root, "proj-b", KindReview, "old-review", now.Add(-8*24*time.Hour))
	reviewFresh := createRunDir(t, root, "proj-b", KindReview, "fresh-review", now.Add(-5*24*time.Hour))

	result, err := Sweep(ctx, Config{
		WorkRoot: root,
		Now:      func() time.Time { return now },
		Runner:   &fakeRunner{},
		Remover:  osRemover{},
	})
	if err != nil {
		t.Fatalf("Sweep: %v", err)
	}
	if result.WakeAgent {
		t.Fatalf("wakeAgent = true, want false")
	}
	if result.Scanned != 103 || result.Eligible != 101 || len(result.Cleaned) != 101 || len(result.Failed) != 0 {
		t.Fatalf("result counts = %+v", result)
	}
	if result.FreedBytes <= 0 {
		t.Fatalf("freed bytes = %d", result.FreedBytes)
	}
	assertMissing(t, filepath.Join(root, "proj-a", KindHealth, "old-health-000"))
	assertMissing(t, reviewOld)
	assertExists(t, fresh)
	assertExists(t, reviewFresh)
	if len(result.Events) != 101 || result.Events[0].Type != "worktree_cleaned" {
		events := result.Events
		if len(events) > 1 {
			events = events[:1]
		}
		t.Fatalf("events = %+v", events)
	}
}

func TestSweepRetriesWhenFallbackRemoveFailsAfterPrune(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	now := time.Date(2026, 5, 4, 14, 0, 0, 0, time.UTC)
	root := t.TempDir()
	stale := createRunDir(t, root, "proj-a", KindHealth, "busy-health", now.Add(-4*24*time.Hour))
	runner := &fakeRunner{failRemove: true}
	remover := &fakeRemover{err: errors.New("busy file")}

	result, err := Sweep(ctx, Config{
		WorkRoot: root,
		Now:      func() time.Time { return now },
		Runner:   runner,
		Remover:  remover,
	})
	if err != nil {
		t.Fatalf("Sweep: %v", err)
	}
	if len(result.Cleaned) != 0 || len(result.Failed) != 1 {
		t.Fatalf("result = %+v", result)
	}
	failed := result.Failed[0]
	if !failed.PrunedPointers || !failed.Retryable || failed.GitError == "" {
		t.Fatalf("failed cleanup did not record retry/prune/git evidence: %+v", failed)
	}
	assertExists(t, stale)

	remover.err = nil
	second, err := Sweep(ctx, Config{
		WorkRoot: root,
		Now:      func() time.Time { return now.Add(time.Hour) },
		Runner:   runner,
		Remover:  remover,
	})
	if err != nil {
		t.Fatalf("second Sweep: %v", err)
	}
	if len(second.Cleaned) != 1 || len(second.Failed) != 0 {
		t.Fatalf("second result = %+v", second)
	}
	assertMissing(t, stale)
}

func TestDefaultDefinitionYAMLLoadsAsDefaultOnDeterministicJob(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "worktree-cleanup.yaml")
	if err := os.WriteFile(path, DefaultDefinitionYAML(), 0o600); err != nil {
		t.Fatal(err)
	}
	loaded, err := scheduler.LoadDefinitionFile(ctx, path)
	if err != nil {
		t.Fatalf("LoadDefinitionFile: %v", err)
	}
	want := DefaultDefinition()
	if loaded.ID != want.ID || loaded.Kind != scheduler.KindDeterministic || loaded.Schedule.Type != scheduler.ScheduleInterval {
		t.Fatalf("loaded definition = %+v", loaded)
	}
	if loaded.Schedule.Interval != time.Hour || loaded.Script != "worktree-cleanup.go" || loaded.Deliver != "hoopoe_activity_panel" {
		t.Fatalf("loaded shape = %+v", loaded)
	}
	if loaded.Paused || !loaded.AuditAlways || len(loaded.EnabledToolsets) != 1 || loaded.EnabledToolsets[0] != "git_write" {
		t.Fatalf("loaded defaults = %+v", loaded)
	}
}

func createRunDir(t *testing.T, root, projectID, kind, runID string, modTime time.Time) string {
	t.Helper()
	path := filepath.Join(root, projectID, kind, runID)
	repo := filepath.Join(path, "repo")
	if err := os.MkdirAll(filepath.Join(repo, ".git"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, "artifact.txt"), []byte("health artifact"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := filepath.WalkDir(path, func(path string, _ os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		return os.Chtimes(path, modTime, modTime)
	}); err != nil {
		t.Fatal(err)
	}
	return path
}

func assertExists(t *testing.T, path string) {
	t.Helper()
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("expected %s to exist: %v", path, err)
	}
}

func assertMissing(t *testing.T, path string) {
	t.Helper()
	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("expected %s to be removed, stat err=%v", path, err)
	}
}

type fakeRunner struct {
	failRemove bool
	calls      [][]string
}

func (r *fakeRunner) Run(_ context.Context, _ string, argv []string) error {
	r.calls = append(r.calls, append([]string(nil), argv...))
	if r.failRemove && len(argv) >= 3 && argv[1] == "worktree" && argv[2] == "remove" {
		return errors.New("stale git worktree pointer")
	}
	return nil
}

type fakeRemover struct {
	err error
}

func (r *fakeRemover) RemoveAll(path string) error {
	if r.err != nil {
		return r.err
	}
	return os.RemoveAll(path)
}

func itoa3(n int) string {
	return fmt.Sprintf("%03d", n)
}
