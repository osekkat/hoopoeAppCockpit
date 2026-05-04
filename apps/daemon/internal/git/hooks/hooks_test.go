package hooks

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	gitadapter "github.com/hoopoe-cockpit/hoopoe/apps/daemon/internal/adapters/git"
	"github.com/hoopoe-cockpit/hoopoe/apps/daemon/internal/audit"
	gitevents "github.com/hoopoe-cockpit/hoopoe/apps/daemon/internal/git"
	"github.com/hoopoe-cockpit/hoopoe/apps/daemon/internal/watcher"
)

func TestInstallPreservesExistingHookAndRendersManagedHook(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	repo := gitRepo(t)
	hookPath := filepath.Join(repo, ".git", "hooks", DefaultHookName)
	userHook := "#!/bin/sh\nprintf user-hook\\n\n"
	if err := os.WriteFile(hookPath, []byte(userHook), 0o755); err != nil {
		t.Fatalf("write user hook: %v", err)
	}

	res, err := (Installer{}).Install(ctx, InstallOptions{
		ProjectID: "proj/01",
		RepoPath:  repo,
		DaemonURL: "http://127.0.0.1:9999/",
		Remote:    "origin",
		TokenPath: "/home/ubuntu/.hoopoe/bearer.token",
	})
	if err != nil {
		t.Fatalf("Install: %v", err)
	}
	if !res.Installed || !res.PreservedUserHook {
		t.Fatalf("install result = %+v", res)
	}
	backup, err := os.ReadFile(filepath.Join(repo, ".git", "hooks", userHookName))
	if err != nil {
		t.Fatalf("read backup: %v", err)
	}
	if string(backup) != userHook {
		t.Fatalf("backup = %q, want original user hook", backup)
	}
	hook, err := os.ReadFile(hookPath)
	if err != nil {
		t.Fatalf("read managed hook: %v", err)
	}
	body := string(hook)
	for _, want := range []string{
		managedHookMarker,
		"USER_HOOK=\"$HOOK_DIR/post-commit.hoopoe-user\"",
		"/v1/projects/proj%2F01/git/push",
		"Idempotency-Key: $IDEMPOTENCY_KEY",
		"Authorization: Bearer $TOKEN",
		"\"policy\":\"commit-fast\"",
		"MAX_ATTEMPTS=3",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("managed hook missing %q:\n%s", want, body)
		}
	}
	info, err := os.Stat(hookPath)
	if err != nil {
		t.Fatalf("stat hook: %v", err)
	}
	if info.Mode()&0o111 == 0 {
		t.Fatalf("managed hook is not executable: %s", info.Mode())
	}
}

func TestUninstallRestoresPreservedUserHook(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	repo := gitRepo(t)
	hookPath := filepath.Join(repo, ".git", "hooks", DefaultHookName)
	userHook := "#!/bin/sh\nprintf restored\\n\n"
	if err := os.WriteFile(hookPath, []byte(userHook), 0o755); err != nil {
		t.Fatalf("write user hook: %v", err)
	}
	opts := InstallOptions{ProjectID: "proj_01", RepoPath: repo}
	if _, err := (Installer{}).Install(ctx, opts); err != nil {
		t.Fatalf("Install: %v", err)
	}

	res, err := (Installer{}).Uninstall(ctx, opts)
	if err != nil {
		t.Fatalf("Uninstall: %v", err)
	}
	if !res.Uninstalled || !res.RestoredUserHook {
		t.Fatalf("uninstall result = %+v", res)
	}
	restored, err := os.ReadFile(hookPath)
	if err != nil {
		t.Fatalf("read restored hook: %v", err)
	}
	if string(restored) != userHook {
		t.Fatalf("restored hook = %q, want original", restored)
	}
	if _, err := os.Stat(filepath.Join(repo, ".git", "hooks", userHookName)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("backup should be gone after restore, err=%v", err)
	}
}

func TestReconcileManualPolicyUninstallsManagedHook(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	repo := gitRepo(t)
	opts := InstallOptions{ProjectID: "proj_01", RepoPath: repo}
	if _, err := (Installer{}).Install(ctx, opts); err != nil {
		t.Fatalf("Install: %v", err)
	}

	res, err := (Installer{}).Reconcile(ctx, InstallOptions{ProjectID: "proj_01", RepoPath: repo, Policy: PushPolicyManual})
	if err != nil {
		t.Fatalf("Reconcile manual: %v", err)
	}
	if !res.Uninstalled {
		t.Fatalf("manual reconcile result = %+v", res)
	}
	if _, err := os.Stat(filepath.Join(repo, ".git", "hooks", DefaultHookName)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("managed hook should be removed, err=%v", err)
	}
}

func TestInstallRefusesBackupCollision(t *testing.T) {
	t.Parallel()
	repo := gitRepo(t)
	hooksDir := filepath.Join(repo, ".git", "hooks")
	if err := os.WriteFile(filepath.Join(hooksDir, DefaultHookName), []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatalf("write hook: %v", err)
	}
	if err := os.WriteFile(filepath.Join(hooksDir, userHookName), []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatalf("write backup collision: %v", err)
	}

	_, err := (Installer{}).Install(context.Background(), InstallOptions{ProjectID: "proj_01", RepoPath: repo})
	if !errors.Is(err, ErrUserHookBackupExists) {
		t.Fatalf("Install error = %v, want ErrUserHookBackupExists", err)
	}
}

func TestGitHookDirResolvesGitdirFile(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	repo := filepath.Join(root, "worktree")
	gitDir := filepath.Join(root, "common.git")
	if err := os.MkdirAll(repo, 0o755); err != nil {
		t.Fatalf("mkdir repo: %v", err)
	}
	if err := os.MkdirAll(gitDir, 0o755); err != nil {
		t.Fatalf("mkdir gitdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(repo, ".git"), []byte("gitdir: ../common.git\n"), 0o644); err != nil {
		t.Fatalf("write gitdir file: %v", err)
	}

	got, err := GitHookDir(repo)
	if err != nil {
		t.Fatalf("GitHookDir: %v", err)
	}
	want := filepath.Join(gitDir, "hooks")
	if got != want {
		t.Fatalf("hook dir = %q, want %q", got, want)
	}
}

func TestRenderPostCommitHookEscapesAndIncludesAfterTestsPolicy(t *testing.T) {
	t.Parallel()
	script, err := RenderPostCommitHook(InstallOptions{
		ProjectID: "proj/with space",
		RepoPath:  gitRepo(t),
		DaemonURL: "http://daemon.local",
		Remote:    "origin",
		Policy:    PushPolicyAfterTests,
		TokenPath: "/tmp/token's",
		Retries:   1,
	})
	if err != nil {
		t.Fatalf("RenderPostCommitHook: %v", err)
	}
	for _, want := range []string{
		"/v1/projects/proj%2Fwith%20space/git/push",
		"\"policy\":\"after-tests\"",
		"TOKEN_PATH='/tmp/token'\"'\"'s'",
		"MAX_ATTEMPTS=2",
	} {
		if !strings.Contains(script, want) {
			t.Fatalf("script missing %q:\n%s", want, script)
		}
	}
	if strings.Contains(script, "AUTH_ARGS=") {
		t.Fatalf("script should not construct a split auth header:\n%s", script)
	}
}

func TestPushExecutorAuditsAndEmitsPushCompleted(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	git := &fakePushClient{
		unpushed: []gitadapter.Commit{
			{SHA: "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"},
			{SHA: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"},
		},
		result: &gitadapter.PushResult{
			OK:      true,
			OldSHA:  "1111111111111111111111111111111111111111",
			NewSHA:  "2222222222222222222222222222222222222222",
			Summary: "1111111111111111111111111111111111111111..2222222222222222222222222222222222222222",
			UpdatedRefs: []gitadapter.PushedRefUpdate{{
				Source:      "HEAD",
				Destination: "main",
				Summary:     "1111111111111111111111111111111111111111..2222222222222222222222222222222222222222",
			}},
		},
	}
	auditSink := &recordingAudit{}
	events := &recordingPushEvents{}
	executor := &PushExecutor{
		Git:    git,
		Audit:  auditSink,
		Events: events,
		Now:    steppingClock(time.Date(2026, 5, 4, 12, 0, 0, 0, time.UTC), 125*time.Millisecond),
	}

	attempt, err := executor.ExecutePush(ctx, PushRequest{
		ProjectID:      "proj_01",
		Branch:         "main",
		Remote:         "origin",
		IdempotencyKey: "push-1",
		CorrelationID:  "corr-1",
		CausationID:    "commit-1",
	})
	if err != nil {
		t.Fatalf("ExecutePush: %v", err)
	}
	if !attempt.OK || attempt.Duration != 125*time.Millisecond {
		t.Fatalf("attempt = %+v", attempt)
	}
	if len(git.pushes) != 1 || git.pushes[0].Branch != "main" || git.pushes[0].Remote != "origin" {
		t.Fatalf("pushes = %+v", git.pushes)
	}
	if len(auditSink.events) != 2 || auditSink.events[0].Action != AuditActionGitPushStarted || auditSink.events[1].Result != audit.ResultSuccess {
		t.Fatalf("audit events = %+v", auditSink.events)
	}
	if len(events.pushes) != 1 {
		t.Fatalf("push events = %+v", events.pushes)
	}
	push := events.pushes[0]
	if !push.OK || push.Branch != "main" || len(push.CommitsPushed) != 2 {
		t.Fatalf("push event = %+v", push)
	}
	if len(push.Refs) != 1 || push.Refs[0].Name != "refs/heads/main" || push.Refs[0].NewSHA != "2222222222222222222222222222222222222222" {
		t.Fatalf("ref updates = %+v", push.Refs)
	}
}

func TestPushExecutorFailurePublishesUrgentActivity(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	git := &fakePushClient{
		unpushed: []gitadapter.Commit{{SHA: "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"}},
		pushErr:  errors.New("authentication failed"),
	}
	auditSink := &recordingAudit{}
	activity := &recordingActivity{}
	events := &recordingPushEvents{}
	executor := &PushExecutor{
		Git:      git,
		Audit:    auditSink,
		Activity: activity,
		Events:   events,
		Now:      steppingClock(time.Date(2026, 5, 4, 12, 0, 0, 0, time.UTC), time.Second),
	}

	attempt, err := executor.ExecutePush(ctx, PushRequest{ProjectID: "proj_01", Branch: "main"})
	if err == nil {
		t.Fatalf("ExecutePush should fail")
	}
	if attempt.OK || attempt.Reason != "authentication failed" {
		t.Fatalf("attempt = %+v", attempt)
	}
	if len(auditSink.events) != 2 || auditSink.events[1].Result != audit.ResultFailure {
		t.Fatalf("audit events = %+v", auditSink.events)
	}
	if len(activity.events) != 1 || activity.events[0].Importance != "urgent" || activity.events[0].Kind != ActivityGitPushFailed {
		t.Fatalf("activity events = %+v", activity.events)
	}
	if len(events.pushes) != 1 || events.pushes[0].OK || events.pushes[0].Reason != "authentication failed" {
		t.Fatalf("push events = %+v", events.pushes)
	}
}

func TestPushExecutorIdempotencySuppressesDuplicateSuccess(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	git := &fakePushClient{
		unpushed: []gitadapter.Commit{{SHA: "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"}},
		result:   &gitadapter.PushResult{OK: true},
	}
	auditSink := &recordingAudit{}
	executor := &PushExecutor{
		Git:         git,
		Audit:       auditSink,
		Idempotency: NewMemoryIdempotencyStore(),
		Now:         steppingClock(time.Date(2026, 5, 4, 12, 0, 0, 0, time.UTC), time.Second),
	}

	first, err := executor.ExecutePush(ctx, PushRequest{ProjectID: "proj_01", Branch: "main", IdempotencyKey: "same"})
	if err != nil {
		t.Fatalf("first ExecutePush: %v", err)
	}
	second, err := executor.ExecutePush(ctx, PushRequest{ProjectID: "proj_01", Branch: "main", IdempotencyKey: "same"})
	if err != nil {
		t.Fatalf("second ExecutePush: %v", err)
	}
	if len(git.pushes) != 1 {
		t.Fatalf("push count = %d, want 1", len(git.pushes))
	}
	if len(auditSink.events) != 2 {
		t.Fatalf("duplicate should not re-audit, events = %+v", auditSink.events)
	}
	if first.CompletedAt != second.CompletedAt || !second.OK {
		t.Fatalf("idempotent attempts differ: first=%+v second=%+v", first, second)
	}
}

func gitRepo(t *testing.T) string {
	t.Helper()
	repo := t.TempDir()
	if err := os.MkdirAll(filepath.Join(repo, ".git", "hooks"), 0o755); err != nil {
		t.Fatalf("mkdir git hooks: %v", err)
	}
	return repo
}

type fakePushClient struct {
	pushes      []gitadapter.PushOpts
	result      *gitadapter.PushResult
	pushErr     error
	unpushed    []gitadapter.Commit
	unpushedErr error
}

func (f *fakePushClient) Push(_ context.Context, opts gitadapter.PushOpts) (*gitadapter.PushResult, error) {
	f.pushes = append(f.pushes, opts)
	return f.result, f.pushErr
}

func (f *fakePushClient) UnpushedCommits(context.Context, string) (*gitadapter.CommitDelta, error) {
	return &gitadapter.CommitDelta{Commits: f.unpushed}, f.unpushedErr
}

type recordingAudit struct {
	events []PushAuditEvent
}

func (r *recordingAudit) RecordGitPushAudit(_ context.Context, event PushAuditEvent) error {
	r.events = append(r.events, event)
	return nil
}

type recordingActivity struct {
	events []PushActivityEvent
}

func (r *recordingActivity) PublishGitPushActivity(_ context.Context, event PushActivityEvent) error {
	r.events = append(r.events, event)
	return nil
}

type recordingPushEvents struct {
	pushes []watcher.PushCompleted
}

func (r *recordingPushEvents) RecordPushCompleted(_ context.Context, push watcher.PushCompleted) ([]watcher.Event, error) {
	r.pushes = append(r.pushes, push)
	return []watcher.Event{{
		Type: gitevents.EventVPSPushCompleted,
		Data: push,
	}}, nil
}

func steppingClock(start time.Time, step time.Duration) func() time.Time {
	next := start.Add(-step)
	return func() time.Time {
		next = next.Add(step)
		return next
	}
}
