// Package hooks owns Hoopoe-managed Git hook lifecycle and the daemon-side
// push executor used by the post-commit auto-push policy.
package hooks

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	gitadapter "github.com/hoopoe-cockpit/hoopoe/apps/daemon/internal/adapters/git"
	"github.com/hoopoe-cockpit/hoopoe/apps/daemon/internal/audit"
	gitevents "github.com/hoopoe-cockpit/hoopoe/apps/daemon/internal/git"
	"github.com/hoopoe-cockpit/hoopoe/apps/daemon/internal/watcher"
)

const (
	DefaultRemote      = "origin"
	DefaultDaemonURL   = "http://127.0.0.1:39391"
	DefaultHookName    = "post-commit"
	DefaultHookTimeout = 15 * time.Second
	DefaultHookRetries = 2

	managedHookMarker = "# hoopoe-managed-post-commit-hook-v1"
	userHookName      = "post-commit.hoopoe-user"
	tmpHookName       = "post-commit.hoopoe-tmp"

	AuditActionGitPushStarted   = "git.push.started"
	AuditActionGitPushCompleted = "git.push.completed"
	ActivityGitPushFailed       = "git.push_failed"
)

var (
	ErrInvalidConfig        = errors.New("git hooks: invalid config")
	ErrUnmanagedHook        = errors.New("git hooks: existing hook is not managed by Hoopoe")
	ErrUserHookBackupExists = errors.New("git hooks: user hook backup already exists")
)

type PushPolicy string

const (
	PushPolicyCommitFast PushPolicy = "commit-fast"
	PushPolicyAfterTests PushPolicy = "after-tests"
	PushPolicyManual     PushPolicy = "manual"
)

type InstallOptions struct {
	ProjectID string
	RepoPath  string
	DaemonURL string
	Remote    string
	Policy    PushPolicy
	TokenPath string
	Timeout   time.Duration
	Retries   int
}

type InstallResult struct {
	HookPath           string
	UserHookBackupPath string
	Installed          bool
	Uninstalled        bool
	PreservedUserHook  bool
	RestoredUserHook   bool
}

type Installer struct{}

func (Installer) Reconcile(ctx context.Context, opts InstallOptions) (InstallResult, error) {
	if normalizePolicy(opts.Policy) == PushPolicyManual {
		return (Installer{}).Uninstall(ctx, opts)
	}
	return (Installer{}).Install(ctx, opts)
}

func (Installer) Install(_ context.Context, opts InstallOptions) (InstallResult, error) {
	opts = normalizeInstallOptions(opts)
	if err := validateInstallOptions(opts); err != nil {
		return InstallResult{}, err
	}
	hookDir, err := GitHookDir(opts.RepoPath)
	if err != nil {
		return InstallResult{}, err
	}
	if err := os.MkdirAll(hookDir, 0o755); err != nil {
		return InstallResult{}, fmt.Errorf("git hooks: mkdir hooks dir: %w", err)
	}
	hookPath := filepath.Join(hookDir, DefaultHookName)
	backupPath := filepath.Join(hookDir, userHookName)
	result := InstallResult{HookPath: hookPath, UserHookBackupPath: backupPath}

	current, err := os.ReadFile(hookPath)
	switch {
	case err == nil && isManagedHook(current):
	case err == nil:
		if _, statErr := os.Stat(backupPath); statErr == nil {
			return result, ErrUserHookBackupExists
		} else if !errors.Is(statErr, os.ErrNotExist) {
			return result, fmt.Errorf("git hooks: stat user hook backup: %w", statErr)
		}
		if err := os.Rename(hookPath, backupPath); err != nil {
			return result, fmt.Errorf("git hooks: preserve user hook: %w", err)
		}
		if err := os.Chmod(backupPath, 0o755); err != nil {
			return result, fmt.Errorf("git hooks: chmod user hook backup: %w", err)
		}
		result.PreservedUserHook = true
	case errors.Is(err, os.ErrNotExist):
	default:
		return result, fmt.Errorf("git hooks: read existing hook: %w", err)
	}

	script, err := RenderPostCommitHook(opts)
	if err != nil {
		return result, err
	}
	if err := writeExecutableHook(hookPath, []byte(script)); err != nil {
		return result, err
	}
	result.Installed = true
	return result, nil
}

func (Installer) Uninstall(_ context.Context, opts InstallOptions) (InstallResult, error) {
	opts = normalizeInstallOptions(opts)
	if strings.TrimSpace(opts.RepoPath) == "" {
		return InstallResult{}, fmt.Errorf("%w: repo path required", ErrInvalidConfig)
	}
	hookDir, err := GitHookDir(opts.RepoPath)
	if err != nil {
		return InstallResult{}, err
	}
	hookPath := filepath.Join(hookDir, DefaultHookName)
	backupPath := filepath.Join(hookDir, userHookName)
	result := InstallResult{HookPath: hookPath, UserHookBackupPath: backupPath}

	current, err := os.ReadFile(hookPath)
	switch {
	case err == nil && !isManagedHook(current):
		return result, ErrUnmanagedHook
	case err == nil:
		if _, statErr := os.Stat(backupPath); statErr == nil {
			if err := os.Rename(backupPath, hookPath); err != nil {
				return result, fmt.Errorf("git hooks: restore user hook: %w", err)
			}
			if err := os.Chmod(hookPath, 0o755); err != nil {
				return result, fmt.Errorf("git hooks: chmod restored hook: %w", err)
			}
			result.RestoredUserHook = true
		} else if errors.Is(statErr, os.ErrNotExist) {
			if err := os.Remove(hookPath); err != nil {
				return result, fmt.Errorf("git hooks: remove managed hook: %w", err)
			}
		} else {
			return result, fmt.Errorf("git hooks: stat user hook backup: %w", statErr)
		}
		result.Uninstalled = true
	case errors.Is(err, os.ErrNotExist):
		if _, statErr := os.Stat(backupPath); statErr == nil {
			if err := os.Rename(backupPath, hookPath); err != nil {
				return result, fmt.Errorf("git hooks: restore user hook without managed hook: %w", err)
			}
			result.Uninstalled = true
			result.RestoredUserHook = true
		} else if !errors.Is(statErr, os.ErrNotExist) {
			return result, fmt.Errorf("git hooks: stat user hook backup: %w", statErr)
		}
	default:
		return result, fmt.Errorf("git hooks: read managed hook: %w", err)
	}
	return result, nil
}

func GitHookDir(repoPath string) (string, error) {
	repoPath = strings.TrimSpace(repoPath)
	if repoPath == "" {
		return "", fmt.Errorf("%w: repo path required", ErrInvalidConfig)
	}
	gitPath := filepath.Join(repoPath, ".git")
	info, err := os.Stat(gitPath)
	if err == nil && info.IsDir() {
		return filepath.Join(gitPath, "hooks"), nil
	}
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return "", fmt.Errorf("git hooks: stat .git: %w", err)
	}
	data, err := os.ReadFile(gitPath)
	if err != nil {
		return "", fmt.Errorf("git hooks: read .git: %w", err)
	}
	gitDir, err := parseGitDirFile(repoPath, string(data))
	if err != nil {
		return "", err
	}
	return filepath.Join(gitDir, "hooks"), nil
}

type hookPayload struct {
	Remote string     `json:"remote"`
	Policy PushPolicy `json:"policy"`
	Source string     `json:"source"`
}

func RenderPostCommitHook(opts InstallOptions) (string, error) {
	opts = normalizeInstallOptions(opts)
	if err := validateInstallOptions(opts); err != nil {
		return "", err
	}
	payload := hookPayload{Remote: opts.Remote, Policy: opts.Policy, Source: "post_commit_hook"}
	payloadJSON, err := json.Marshal(payload)
	if err != nil {
		return "", fmt.Errorf("git hooks: marshal hook payload: %w", err)
	}
	endpoint := strings.TrimRight(opts.DaemonURL, "/") + "/v1/projects/" + url.PathEscape(opts.ProjectID) + "/git/push"
	timeoutSeconds := int(opts.Timeout.Seconds())
	if timeoutSeconds <= 0 {
		timeoutSeconds = int(DefaultHookTimeout.Seconds())
	}
	return fmt.Sprintf(`#!/bin/sh
%s
# Installed by Hoopoe at swarm-launch time. Existing user hook, if any, is
# preserved as %s and chained before the daemon auto-push RPC.

HOOK_DIR=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
USER_HOOK="$HOOK_DIR/%s"
if [ -x "$USER_HOOK" ]; then
  "$USER_HOOK" "$@" || exit $?
elif [ -f "$USER_HOOK" ]; then
  /bin/sh "$USER_HOOK" "$@" || exit $?
fi

PROJECT_ID=%s
REMOTE=%s
TOKEN_PATH=%s
ENDPOINT=%s
PAYLOAD=%s
MAX_ATTEMPTS=%d
TIMEOUT_SECONDS=%d

HEAD_SHA=$(git rev-parse HEAD 2>/dev/null || printf 'unknown')
IDEMPOTENCY_KEY="hoopoe-post-commit:${PROJECT_ID}:${REMOTE}:${HEAD_SHA}"
TOKEN=
if [ -n "$TOKEN_PATH" ] && [ -r "$TOKEN_PATH" ]; then
  TOKEN=$(tr -d '\r\n' < "$TOKEN_PATH")
fi

attempt=1
while [ "$attempt" -le "$MAX_ATTEMPTS" ]; do
  if [ -n "$TOKEN" ]; then
    if curl -fsS -m "$TIMEOUT_SECONDS" -X POST "$ENDPOINT" \
      -H "Content-Type: application/json" \
      -H "Idempotency-Key: $IDEMPOTENCY_KEY" \
      -H "Authorization: Bearer $TOKEN" \
      --data "$PAYLOAD" >/dev/null; then
      exit 0
    fi
  else
    if curl -fsS -m "$TIMEOUT_SECONDS" -X POST "$ENDPOINT" \
      -H "Content-Type: application/json" \
      -H "Idempotency-Key: $IDEMPOTENCY_KEY" \
      --data "$PAYLOAD" >/dev/null; then
      exit 0
    fi
  fi
  attempt=$((attempt + 1))
  sleep "$attempt"
done

printf 'Hoopoe auto-push hook could not reach daemon for project %%s\n' "$PROJECT_ID" >&2
exit 0
`, managedHookMarker, userHookName, userHookName, shellQuote(opts.ProjectID), shellQuote(opts.Remote), shellQuote(opts.TokenPath), shellQuote(endpoint), shellQuote(string(payloadJSON)), opts.Retries+1, timeoutSeconds), nil
}

type PushClient interface {
	Push(context.Context, gitadapter.PushOpts) (*gitadapter.PushResult, error)
	UnpushedCommits(context.Context, string) (*gitadapter.CommitDelta, error)
}

type AuditSink interface {
	RecordGitPushAudit(context.Context, PushAuditEvent) error
}

type ActivitySink interface {
	PublishGitPushActivity(context.Context, PushActivityEvent) error
}

type PushEventRecorder interface {
	RecordPushCompleted(context.Context, watcher.PushCompleted) ([]watcher.Event, error)
}

type IdempotencyStore interface {
	Lookup(string) (PushAttempt, bool)
	Put(string, PushAttempt)
}

type PushExecutor struct {
	Git         PushClient
	Audit       AuditSink
	Activity    ActivitySink
	Events      PushEventRecorder
	Idempotency IdempotencyStore
	Now         func() time.Time
}

type PushRequest struct {
	ProjectID      string
	Branch         string
	Remote         string
	Actor          audit.Actor
	IdempotencyKey string
	CausationID    string
	CorrelationID  string
}

type PushAttempt struct {
	ProjectID      string
	Branch         string
	Remote         string
	CommitsPushed  []string
	OK             bool
	Reason         string
	StartedAt      time.Time
	CompletedAt    time.Time
	Duration       time.Duration
	Summary        string
	UpdatedRefs    []gitevents.RefUpdate
	IdempotencyKey string
}

type PushAuditEvent struct {
	ProjectID      string
	Actor          audit.Actor
	Action         string
	Result         audit.Result
	Remote         string
	Branch         string
	Commits        []string
	Duration       time.Duration
	Reason         string
	IdempotencyKey string
	CorrelationID  string
	CausationID    string
}

type PushActivityEvent struct {
	ProjectID     string
	Kind          string
	Importance    string
	Summary       string
	Remote        string
	Branch        string
	Reason        string
	CorrelationID string
	CausationID   string
}

func (e *PushExecutor) ExecutePush(ctx context.Context, req PushRequest) (PushAttempt, error) {
	if e == nil || e.Git == nil {
		return PushAttempt{}, fmt.Errorf("%w: git client required", ErrInvalidConfig)
	}
	req = normalizePushRequest(req)
	if req.ProjectID == "" {
		return PushAttempt{}, fmt.Errorf("%w: project id required", ErrInvalidConfig)
	}
	if req.IdempotencyKey != "" && e.Idempotency != nil {
		if cached, found := e.Idempotency.Lookup(req.IdempotencyKey); found {
			return cached, nil
		}
	}
	started := e.now()
	if err := e.recordAudit(ctx, PushAuditEvent{
		ProjectID:      req.ProjectID,
		Actor:          req.Actor,
		Action:         AuditActionGitPushStarted,
		Result:         audit.ResultSuccess,
		Remote:         req.Remote,
		Branch:         req.Branch,
		IdempotencyKey: req.IdempotencyKey,
		CorrelationID:  req.CorrelationID,
		CausationID:    req.CausationID,
	}); err != nil {
		return PushAttempt{}, err
	}

	commits, listErr := e.unpushed(ctx, req.Branch)
	res, pushErr := e.Git.Push(ctx, gitadapter.PushOpts{Remote: req.Remote, Branch: req.Branch})
	completed := e.now()
	attempt := PushAttempt{
		ProjectID:      req.ProjectID,
		Branch:         req.Branch,
		Remote:         req.Remote,
		CommitsPushed:  commits,
		StartedAt:      started,
		CompletedAt:    completed,
		Duration:       completed.Sub(started),
		IdempotencyKey: req.IdempotencyKey,
	}
	if listErr != nil {
		attempt.Reason = "pre-push unpushed list failed: " + listErr.Error()
	}
	switch {
	case pushErr != nil:
		attempt.OK = false
		attempt.Reason = pushErr.Error()
	case res == nil:
		attempt.OK = false
		attempt.Reason = "git push returned no result"
	default:
		attempt.OK = res.OK
		attempt.Summary = res.Summary
		attempt.UpdatedRefs = pushedRefUpdates(res)
		if !res.OK {
			attempt.Reason = pushFailureReason(res)
		}
	}
	if attempt.OK && len(attempt.UpdatedRefs) == 0 {
		attempt.UpdatedRefs = defaultPushedRefUpdates(req.Branch, res)
	}
	result := audit.ResultSuccess
	if !attempt.OK {
		result = audit.ResultFailure
	}
	auditErr := e.recordAudit(ctx, PushAuditEvent{
		ProjectID:      req.ProjectID,
		Actor:          req.Actor,
		Action:         AuditActionGitPushCompleted,
		Result:         result,
		Remote:         attempt.Remote,
		Branch:         attempt.Branch,
		Commits:        attempt.CommitsPushed,
		Duration:       attempt.Duration,
		Reason:         attempt.Reason,
		IdempotencyKey: attempt.IdempotencyKey,
		CorrelationID:  req.CorrelationID,
		CausationID:    req.CausationID,
	})
	var eventErr error
	if e.Events != nil {
		_, eventErr = e.Events.RecordPushCompleted(ctx, watcher.PushCompleted{
			Branch:        attempt.Branch,
			Remote:        attempt.Remote,
			CommitsPushed: attempt.CommitsPushed,
			Refs:          attempt.UpdatedRefs,
			Duration:      attempt.Duration,
			OK:            attempt.OK,
			Reason:        attempt.Reason,
			CausationID:   req.CausationID,
			CorrelationID: req.CorrelationID,
		})
	}
	if !attempt.OK && e.Activity != nil {
		_ = e.Activity.PublishGitPushActivity(ctx, PushActivityEvent{
			ProjectID:     attempt.ProjectID,
			Kind:          ActivityGitPushFailed,
			Importance:    "urgent",
			Summary:       "Auto-push failed for " + attempt.Branch,
			Remote:        attempt.Remote,
			Branch:        attempt.Branch,
			Reason:        attempt.Reason,
			CorrelationID: req.CorrelationID,
			CausationID:   req.CausationID,
		})
	}
	if attempt.OK && req.IdempotencyKey != "" && e.Idempotency != nil {
		e.Idempotency.Put(req.IdempotencyKey, attempt)
	}
	if !attempt.OK {
		return attempt, errors.New(attempt.Reason)
	}
	if auditErr != nil {
		return attempt, auditErr
	}
	if eventErr != nil {
		return attempt, eventErr
	}
	return attempt, nil
}

type AuditWriterSink struct {
	Writer *audit.Writer
}

func (s AuditWriterSink) RecordGitPushAudit(ctx context.Context, event PushAuditEvent) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if s.Writer == nil {
		return nil
	}
	actor := event.Actor
	if actor.Kind == "" {
		actor = audit.Actor{Kind: audit.ActorSystem, ID: "hoopoe-git-hooks"}
	}
	_, _, err := s.Writer.Append(audit.Entry{
		ProjectID:     event.ProjectID,
		Actor:         actor,
		Action:        event.Action,
		Result:        event.Result,
		CorrelationID: event.CorrelationID,
		CausationID:   event.CausationID,
		Data: map[string]any{
			"remote":         event.Remote,
			"branch":         event.Branch,
			"commits":        event.Commits,
			"durationMs":     event.Duration.Milliseconds(),
			"reason":         event.Reason,
			"idempotencyKey": event.IdempotencyKey,
		},
	})
	return err
}

type MemoryIdempotencyStore struct {
	entries map[string]PushAttempt
}

func NewMemoryIdempotencyStore() *MemoryIdempotencyStore {
	return &MemoryIdempotencyStore{entries: map[string]PushAttempt{}}
}

func (s *MemoryIdempotencyStore) Lookup(key string) (PushAttempt, bool) {
	if s == nil || s.entries == nil {
		return PushAttempt{}, false
	}
	attempt, ok := s.entries[key]
	return attempt, ok
}

func (s *MemoryIdempotencyStore) Put(key string, attempt PushAttempt) {
	if s == nil || key == "" {
		return
	}
	if s.entries == nil {
		s.entries = map[string]PushAttempt{}
	}
	s.entries[key] = attempt
}

func (e *PushExecutor) recordAudit(ctx context.Context, event PushAuditEvent) error {
	if e.Audit == nil {
		return nil
	}
	return e.Audit.RecordGitPushAudit(ctx, event)
}

func (e *PushExecutor) unpushed(ctx context.Context, branch string) ([]string, error) {
	delta, err := e.Git.UnpushedCommits(ctx, branch)
	if err != nil {
		return nil, err
	}
	commits := make([]string, 0, len(delta.Commits))
	for _, commit := range delta.Commits {
		if strings.TrimSpace(commit.SHA) != "" {
			commits = append(commits, strings.TrimSpace(commit.SHA))
		}
	}
	sort.Strings(commits)
	return commits, nil
}

func (e *PushExecutor) now() time.Time {
	if e.Now == nil {
		return time.Now().UTC()
	}
	return e.Now().UTC()
}

func normalizeInstallOptions(opts InstallOptions) InstallOptions {
	opts.ProjectID = strings.TrimSpace(opts.ProjectID)
	opts.RepoPath = strings.TrimSpace(opts.RepoPath)
	opts.DaemonURL = strings.TrimSpace(opts.DaemonURL)
	if opts.DaemonURL == "" {
		opts.DaemonURL = DefaultDaemonURL
	}
	opts.Remote = strings.TrimSpace(opts.Remote)
	if opts.Remote == "" {
		opts.Remote = DefaultRemote
	}
	opts.Policy = normalizePolicy(opts.Policy)
	opts.TokenPath = strings.TrimSpace(opts.TokenPath)
	if opts.Timeout <= 0 {
		opts.Timeout = DefaultHookTimeout
	}
	switch {
	case opts.Retries < 0:
		opts.Retries = 0
	case opts.Retries == 0:
		opts.Retries = DefaultHookRetries
	}
	return opts
}

func normalizePolicy(policy PushPolicy) PushPolicy {
	switch policy {
	case PushPolicyAfterTests, PushPolicyManual:
		return policy
	default:
		return PushPolicyCommitFast
	}
}

func validateInstallOptions(opts InstallOptions) error {
	if opts.ProjectID == "" {
		return fmt.Errorf("%w: project id required", ErrInvalidConfig)
	}
	if opts.RepoPath == "" {
		return fmt.Errorf("%w: repo path required", ErrInvalidConfig)
	}
	if opts.Policy == PushPolicyManual {
		return fmt.Errorf("%w: manual policy does not install a hook", ErrInvalidConfig)
	}
	return nil
}

func normalizePushRequest(req PushRequest) PushRequest {
	req.ProjectID = strings.TrimSpace(req.ProjectID)
	req.Branch = strings.TrimSpace(req.Branch)
	if req.Branch == "" {
		req.Branch = "HEAD"
	}
	req.Remote = strings.TrimSpace(req.Remote)
	if req.Remote == "" {
		req.Remote = DefaultRemote
	}
	req.IdempotencyKey = strings.TrimSpace(req.IdempotencyKey)
	req.CausationID = strings.TrimSpace(req.CausationID)
	req.CorrelationID = strings.TrimSpace(req.CorrelationID)
	if req.Actor.Kind == "" {
		req.Actor = audit.Actor{Kind: audit.ActorSystem, ID: "hoopoe-post-commit-hook"}
	}
	return req
}

func parseGitDirFile(repoPath, body string) (string, error) {
	body = strings.TrimSpace(body)
	if !strings.HasPrefix(body, "gitdir:") {
		return "", fmt.Errorf("%w: .git is neither dir nor gitdir file", ErrInvalidConfig)
	}
	gitDir := strings.TrimSpace(strings.TrimPrefix(body, "gitdir:"))
	if gitDir == "" {
		return "", fmt.Errorf("%w: .git gitdir is empty", ErrInvalidConfig)
	}
	if !filepath.IsAbs(gitDir) {
		gitDir = filepath.Join(repoPath, gitDir)
	}
	return filepath.Clean(gitDir), nil
}

func isManagedHook(body []byte) bool {
	return strings.Contains(string(body), managedHookMarker)
}

func writeExecutableHook(path string, body []byte) error {
	tmpPath := filepath.Join(filepath.Dir(path), tmpHookName)
	if err := os.WriteFile(tmpPath, body, 0o755); err != nil {
		return fmt.Errorf("git hooks: write temp hook: %w", err)
	}
	if err := os.Chmod(tmpPath, 0o755); err != nil {
		return fmt.Errorf("git hooks: chmod temp hook: %w", err)
	}
	if err := os.Rename(tmpPath, path); err != nil {
		return fmt.Errorf("git hooks: install hook: %w", err)
	}
	return nil
}

func shellQuote(value string) string {
	if value == "" {
		return "''"
	}
	return "'" + strings.ReplaceAll(value, "'", "'\"'\"'") + "'"
}

func pushedRefUpdates(result *gitadapter.PushResult) []gitevents.RefUpdate {
	if result == nil {
		return nil
	}
	updates := make([]gitevents.RefUpdate, 0, len(result.UpdatedRefs))
	for _, ref := range result.UpdatedRefs {
		name := normalizeDestinationRef(ref.Destination)
		if name == "" {
			continue
		}
		oldSHA, newSHA := parsePushSummary(ref.Summary)
		if result.OldSHA != "" {
			oldSHA = result.OldSHA
		}
		if result.NewSHA != "" {
			newSHA = result.NewSHA
		}
		if newSHA == "" {
			newSHA = ref.Source
		}
		updates = append(updates, gitevents.RefUpdate{Name: name, OldSHA: oldSHA, NewSHA: newSHA})
	}
	return updates
}

func defaultPushedRefUpdates(branch string, result *gitadapter.PushResult) []gitevents.RefUpdate {
	if result == nil || result.NewSHA == "" {
		return nil
	}
	return []gitevents.RefUpdate{{
		Name:   normalizeDestinationRef(branch),
		OldSHA: result.OldSHA,
		NewSHA: result.NewSHA,
	}}
}

func normalizeDestinationRef(ref string) string {
	ref = strings.TrimSpace(ref)
	ref = strings.TrimPrefix(ref, "origin/")
	if ref == "" || ref == "HEAD" {
		return ""
	}
	if strings.HasPrefix(ref, "refs/") {
		return ref
	}
	return "refs/heads/" + ref
}

func parsePushSummary(summary string) (string, string) {
	summary = strings.TrimSpace(summary)
	for _, sep := range []string{"...", ".."} {
		if idx := strings.Index(summary, sep); idx > 0 {
			return summary[:idx], summary[idx+len(sep):]
		}
	}
	if strings.HasPrefix(summary, "[new branch]") || strings.HasPrefix(summary, "*") {
		return "", ""
	}
	return "", ""
}

func pushFailureReason(result *gitadapter.PushResult) string {
	if result == nil {
		return "git push returned no result"
	}
	for _, ref := range result.UpdatedRefs {
		if strings.TrimSpace(ref.Reason) != "" {
			return strings.TrimSpace(ref.Reason)
		}
	}
	if strings.TrimSpace(result.Summary) != "" {
		return strings.TrimSpace(result.Summary)
	}
	return "git push failed"
}

func HashHook(body string) string {
	sum := sha256.Sum256([]byte(body))
	return hex.EncodeToString(sum[:])
}
