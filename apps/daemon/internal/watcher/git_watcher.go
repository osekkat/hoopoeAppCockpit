// Package watcher owns daemon-side state watchers that publish typed events.
package watcher

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	gitadapter "github.com/hoopoe-cockpit/hoopoe/apps/daemon/internal/adapters/git"
	"github.com/hoopoe-cockpit/hoopoe/apps/daemon/internal/api"
	gitevents "github.com/hoopoe-cockpit/hoopoe/apps/daemon/internal/git"
)

const defaultRemote = "origin"

var ErrInvalidWatcherConfig = errors.New("watcher: invalid git watcher config")

type GitClient interface {
	Status(context.Context) (*gitadapter.Status, error)
	Log(context.Context, gitadapter.LogOpts) ([]gitadapter.Commit, error)
	UnpushedCommits(context.Context, string) (*gitadapter.CommitDelta, error)
}

type GitRemoteClient interface {
	Fetch(context.Context, string) error
	Branches(context.Context) ([]gitadapter.Branch, error)
	RevParse(context.Context, string) (string, error)
}

type GitShowClient interface {
	Show(context.Context, string) ([]byte, error)
}

type CommitFileCounter interface {
	FilesChanged(context.Context, string) (int, error)
}

type RemoteRefSource interface {
	ListRemoteRefs(context.Context, string) ([]gitevents.RefState, error)
}

type Publisher interface {
	Publish(context.Context, Event) error
}

type Event struct {
	Channel       string
	Type          string
	Actor         map[string]any
	CausationID   string
	CorrelationID string
	Data          any
}

type AdapterClient struct {
	*gitadapter.Client
}

func (c AdapterClient) FilesChanged(ctx context.Context, sha string) (int, error) {
	if c.Client == nil {
		return 0, fmt.Errorf("%w: nil git adapter", ErrInvalidWatcherConfig)
	}
	out, err := c.Show(ctx, sha)
	if err != nil {
		return 0, err
	}
	return CountFilesChangedFromShow(out), nil
}

type TrackingBranchRefSource struct {
	Git GitRemoteClient
}

func (s TrackingBranchRefSource) ListRemoteRefs(ctx context.Context, remote string) ([]gitevents.RefState, error) {
	if s.Git == nil {
		return nil, fmt.Errorf("%w: git remote client required", ErrInvalidWatcherConfig)
	}
	if strings.TrimSpace(remote) == "" {
		remote = defaultRemote
	}
	if err := s.Git.Fetch(ctx, remote); err != nil {
		return nil, err
	}
	branches, err := s.Git.Branches(ctx)
	if err != nil {
		return nil, err
	}
	refs := []gitevents.RefState{}
	for _, branch := range branches {
		if strings.TrimSpace(branch.Name) == "" {
			continue
		}
		remoteRef := remote + "/" + strings.TrimSpace(branch.Name)
		sha, err := s.Git.RevParse(ctx, remoteRef)
		if err != nil {
			continue
		}
		if strings.TrimSpace(sha) == "" {
			continue
		}
		refs = append(refs, gitevents.RefState{Name: "refs/heads/" + strings.TrimSpace(branch.Name), SHA: sha})
	}
	return refs, nil
}

type EventHubPublisher struct {
	Hub *api.EventHub
}

func (p EventHubPublisher) Publish(ctx context.Context, event Event) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if p.Hub == nil {
		return fmt.Errorf("%w: nil event hub", ErrInvalidWatcherConfig)
	}
	p.Hub.Publish(api.PublishInput{
		Channel:       event.Channel,
		Type:          event.Type,
		Actor:         event.Actor,
		CausationID:   event.CausationID,
		CorrelationID: event.CorrelationID,
		Data:          event.Data,
	})
	return nil
}

type GitWatcher struct {
	ProjectID  string
	Remote     string
	Git        GitClient
	RemoteRefs RemoteRefSource
	Publisher  Publisher
	Actor      map[string]any
	Now        func() time.Time

	lastLocalHead string
	lastRemote    map[string]string
	seen          map[string]bool
}

type PushCompleted struct {
	Branch        string
	Remote        string
	CommitsPushed []string
	Refs          []gitevents.RefUpdate
	Duration      time.Duration
	OK            bool
	Reason        string
	CausationID   string
	CorrelationID string
}

func NewGitWatcher(projectID string, client GitClient, publisher Publisher) *GitWatcher {
	return &GitWatcher{
		ProjectID: projectID,
		Remote:    defaultRemote,
		Git:       client,
		Publisher: publisher,
		Now:       time.Now,
	}
}

func (w *GitWatcher) Seed(ctx context.Context) error {
	if err := w.validate(false); err != nil {
		return err
	}
	commit, _, err := w.currentHead(ctx)
	if err != nil {
		return err
	}
	w.lastLocalHead = commit.SHA
	if w.RemoteRefs != nil {
		refs, err := w.RemoteRefs.ListRemoteRefs(ctx, w.remote())
		if err != nil {
			return err
		}
		w.lastRemote = refMap(refs)
	}
	return nil
}

func (w *GitWatcher) PollLocal(ctx context.Context) ([]Event, error) {
	if err := w.validate(true); err != nil {
		return nil, err
	}
	head, branch, err := w.currentHead(ctx)
	if err != nil {
		return nil, err
	}
	if head.SHA == "" {
		return nil, nil
	}
	if w.lastLocalHead == "" {
		w.lastLocalHead = head.SHA
		return nil, nil
	}
	if head.SHA == w.lastLocalHead {
		return nil, nil
	}
	commits, err := w.newUnpushedCommits(ctx, branch, head)
	if err != nil {
		return nil, err
	}
	if len(commits) == 0 {
		w.lastLocalHead = head.SHA
		return nil, nil
	}

	out := make([]Event, 0, len(commits))
	for _, commit := range commits {
		filesChanged := 0
		if counter, ok := w.Git.(CommitFileCounter); ok {
			count, err := counter.FilesChanged(ctx, commit.SHA)
			if err != nil {
				return nil, err
			}
			filesChanged = count
		}
		payload := gitevents.CommitCreatedPayload{
			ProjectID:    w.ProjectID,
			CommitSHA:    commit.SHA,
			Branch:       branch,
			ParentSHA:    firstParent(commit),
			AuthorName:   commit.AuthorName,
			AuthorEmail:  commit.AuthorEmail,
			Message:      gitevents.TruncateCommitMessage(commit.Subject),
			FilesChanged: filesChanged,
			Time:         w.now(),
		}
		event := w.gitEvent(gitevents.EventVPSCommitCreated, payload)
		key := eventKey(event.Type, payload.ProjectID, payload.CommitSHA)
		if w.hasSeenEvent(key) {
			continue
		}
		if err := w.Publisher.Publish(ctx, event); err != nil {
			return nil, err
		}
		w.markSeenEvent(key)
		out = append(out, event)
	}
	w.lastLocalHead = head.SHA
	return out, nil
}

func (w *GitWatcher) RecordPushCompleted(ctx context.Context, push PushCompleted) ([]Event, error) {
	if err := w.validate(true); err != nil {
		return nil, err
	}
	remote := strings.TrimSpace(push.Remote)
	if remote == "" {
		remote = w.remote()
	}
	payload := gitevents.PushCompletedPayload{
		ProjectID:     w.ProjectID,
		Branch:        strings.TrimSpace(push.Branch),
		CommitsPushed: uniqueStrings(push.CommitsPushed),
		Remote:        remote,
		Time:          w.now(),
		DurationMs:    push.Duration.Milliseconds(),
		OK:            push.OK,
		Reason:        strings.TrimSpace(push.Reason),
	}
	event := w.gitEvent(gitevents.EventVPSPushCompleted, payload)
	event.CausationID = push.CausationID
	event.CorrelationID = push.CorrelationID
	out := []Event{}
	key := eventKey(event.Type, payload.ProjectID, payload.Branch, payload.Remote, strings.Join(payload.CommitsPushed, ","), payload.Reason)
	if !w.hasSeenEvent(key) {
		if err := w.Publisher.Publish(ctx, event); err != nil {
			return nil, err
		}
		w.markSeenEvent(key)
		out = append(out, event)
	}
	if push.OK && len(push.Refs) > 0 {
		originPayload := gitevents.OriginUpdatedPayload{
			ProjectID: w.ProjectID,
			Refs:      normalizeRefUpdates(push.Refs),
			Source:    gitevents.OriginUpdateSourceVPSPush,
			Time:      payload.Time,
		}
		originEvent := w.gitEvent(gitevents.EventOriginUpdated, originPayload)
		originEvent.CausationID = event.CausationID
		originEvent.CorrelationID = event.CorrelationID
		originKey := originUpdateKey(originPayload)
		if !w.hasSeenEvent(originKey) {
			if err := w.Publisher.Publish(ctx, originEvent); err != nil {
				return nil, err
			}
			w.markSeenEvent(originKey)
			out = append(out, originEvent)
		}
		w.mergeRemoteRefs(originPayload.Refs)
	}
	return out, nil
}

func (w *GitWatcher) PollOrigin(ctx context.Context) ([]Event, error) {
	if err := w.validate(true); err != nil {
		return nil, err
	}
	if w.RemoteRefs == nil {
		return nil, fmt.Errorf("%w: remote ref source required", ErrInvalidWatcherConfig)
	}
	current, err := w.RemoteRefs.ListRemoteRefs(ctx, w.remote())
	if err != nil {
		return nil, err
	}
	next := refMap(current)
	if w.lastRemote == nil {
		w.lastRemote = next
		return nil, nil
	}
	updates := changedRefs(w.lastRemote, next)
	if len(updates) == 0 {
		w.lastRemote = next
		return nil, nil
	}
	payload := gitevents.OriginUpdatedPayload{
		ProjectID: w.ProjectID,
		Refs:      updates,
		Source:    gitevents.OriginUpdateSourceExternalPush,
		Time:      w.now(),
	}
	key := originUpdateKey(payload)
	if w.hasSeenEvent(key) {
		w.lastRemote = next
		return nil, nil
	}
	event := w.gitEvent(gitevents.EventOriginUpdated, payload)
	if err := w.Publisher.Publish(ctx, event); err != nil {
		return nil, err
	}
	w.markSeenEvent(key)
	w.lastRemote = next
	return []Event{event}, nil
}

func (w *GitWatcher) validate(requirePublisher bool) error {
	if strings.TrimSpace(w.ProjectID) == "" {
		return fmt.Errorf("%w: project id required", ErrInvalidWatcherConfig)
	}
	if w.Git == nil {
		return fmt.Errorf("%w: git client required", ErrInvalidWatcherConfig)
	}
	if requirePublisher && w.Publisher == nil {
		return fmt.Errorf("%w: publisher required", ErrInvalidWatcherConfig)
	}
	if w.Now == nil {
		w.Now = time.Now
	}
	if w.seen == nil {
		w.seen = map[string]bool{}
	}
	return nil
}

func (w *GitWatcher) currentHead(ctx context.Context) (gitadapter.Commit, string, error) {
	status, err := w.Git.Status(ctx)
	if err != nil {
		return gitadapter.Commit{}, "", err
	}
	branch := status.Branch
	if branch == "" || status.Detached {
		branch = "HEAD"
	}
	commits, err := w.Git.Log(ctx, gitadapter.LogOpts{Ref: "HEAD", Limit: 1})
	if err != nil {
		return gitadapter.Commit{}, "", err
	}
	if len(commits) == 0 {
		return gitadapter.Commit{}, branch, nil
	}
	return commits[0], branch, nil
}

func (w *GitWatcher) newUnpushedCommits(ctx context.Context, branch string, head gitadapter.Commit) ([]gitadapter.Commit, error) {
	delta, err := w.Git.UnpushedCommits(ctx, branch)
	if err != nil {
		return nil, err
	}
	if delta == nil {
		return nil, nil
	}
	if !commitDeltaContains(delta.Commits, head.SHA) {
		return nil, nil
	}
	recent, err := w.Git.Log(ctx, gitadapter.LogOpts{Ref: "HEAD", Limit: len(delta.Commits)})
	if err != nil {
		return nil, err
	}
	details := commitDetails(recent, head)
	commits := make([]gitadapter.Commit, 0, len(delta.Commits))
	for i := len(delta.Commits) - 1; i >= 0; i-- {
		commit := mergeCommitDetails(delta.Commits[i], details[delta.Commits[i].SHA])
		if strings.TrimSpace(commit.SHA) == "" {
			continue
		}
		key := eventKey(gitevents.EventVPSCommitCreated, w.ProjectID, commit.SHA)
		if w.hasSeenEvent(key) {
			continue
		}
		commits = append(commits, commit)
	}
	return commits, nil
}

func commitDeltaContains(commits []gitadapter.Commit, sha string) bool {
	for _, commit := range commits {
		if strings.TrimSpace(commit.SHA) == sha {
			return true
		}
	}
	return false
}

func commitDetails(commits []gitadapter.Commit, head gitadapter.Commit) map[string]gitadapter.Commit {
	out := map[string]gitadapter.Commit{}
	for _, commit := range commits {
		if strings.TrimSpace(commit.SHA) != "" {
			out[commit.SHA] = commit
		}
	}
	if strings.TrimSpace(head.SHA) != "" {
		out[head.SHA] = head
	}
	return out
}

func mergeCommitDetails(base, detail gitadapter.Commit) gitadapter.Commit {
	if strings.TrimSpace(base.SHA) == "" {
		return detail
	}
	out := base
	if out.ShortSHA == "" {
		out.ShortSHA = detail.ShortSHA
	}
	if out.AuthorName == "" {
		out.AuthorName = detail.AuthorName
	}
	if out.AuthorEmail == "" {
		out.AuthorEmail = detail.AuthorEmail
	}
	if out.AuthoredAt.IsZero() {
		out.AuthoredAt = detail.AuthoredAt
	}
	if out.CommitterName == "" {
		out.CommitterName = detail.CommitterName
	}
	if out.CommitterEmail == "" {
		out.CommitterEmail = detail.CommitterEmail
	}
	if out.CommittedAt.IsZero() {
		out.CommittedAt = detail.CommittedAt
	}
	if out.Subject == "" {
		out.Subject = detail.Subject
	}
	if out.Body == "" {
		out.Body = detail.Body
	}
	if len(out.ParentSHAs) == 0 {
		out.ParentSHAs = append([]string(nil), detail.ParentSHAs...)
	}
	return out
}

func (w *GitWatcher) gitEvent(eventType string, data any) Event {
	return Event{
		Channel: gitevents.ProjectChannel(w.ProjectID),
		Type:    eventType,
		Actor:   w.Actor,
		Data:    data,
	}
}

func (w *GitWatcher) now() time.Time {
	if w.Now == nil {
		return time.Now().UTC()
	}
	return w.Now().UTC()
}

func (w *GitWatcher) remote() string {
	if strings.TrimSpace(w.Remote) == "" {
		return defaultRemote
	}
	return strings.TrimSpace(w.Remote)
}

func eventKey(parts ...string) string {
	return strings.Join(parts, "\x00")
}

func (w *GitWatcher) hasSeenEvent(key string) bool {
	if w.seen == nil {
		w.seen = map[string]bool{}
	}
	return w.seen[key]
}

func (w *GitWatcher) markSeenEvent(key string) {
	if w.seen == nil {
		w.seen = map[string]bool{}
	}
	w.seen[key] = true
}

func originUpdateKey(payload gitevents.OriginUpdatedPayload) string {
	parts := []string{gitevents.EventOriginUpdated, payload.ProjectID, payload.Source}
	for _, ref := range normalizeRefUpdates(payload.Refs) {
		parts = append(parts, ref.Name, ref.OldSHA, ref.NewSHA)
	}
	return eventKey(parts...)
}

func (w *GitWatcher) mergeRemoteRefs(refs []gitevents.RefUpdate) {
	if w.lastRemote == nil {
		w.lastRemote = map[string]string{}
	}
	for _, ref := range refs {
		if ref.Name != "" && ref.NewSHA != "" {
			w.lastRemote[ref.Name] = ref.NewSHA
		}
	}
}

func firstParent(commit gitadapter.Commit) string {
	if len(commit.ParentSHAs) == 0 {
		return ""
	}
	return commit.ParentSHAs[0]
}

func refMap(refs []gitevents.RefState) map[string]string {
	out := map[string]string{}
	for _, ref := range refs {
		name := strings.TrimSpace(ref.Name)
		sha := strings.TrimSpace(ref.SHA)
		if name == "" || sha == "" {
			continue
		}
		out[name] = sha
	}
	return out
}

func changedRefs(old, next map[string]string) []gitevents.RefUpdate {
	updates := []gitevents.RefUpdate{}
	for name, newSHA := range next {
		oldSHA, ok := old[name]
		if ok && oldSHA == newSHA {
			continue
		}
		updates = append(updates, gitevents.RefUpdate{Name: name, OldSHA: oldSHA, NewSHA: newSHA})
	}
	return normalizeRefUpdates(updates)
}

func normalizeRefUpdates(refs []gitevents.RefUpdate) []gitevents.RefUpdate {
	out := make([]gitevents.RefUpdate, 0, len(refs))
	for _, ref := range refs {
		ref.Name = strings.TrimSpace(ref.Name)
		ref.OldSHA = strings.TrimSpace(ref.OldSHA)
		ref.NewSHA = strings.TrimSpace(ref.NewSHA)
		if ref.Name == "" || ref.NewSHA == "" {
			continue
		}
		out = append(out, ref)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

func uniqueStrings(values []string) []string {
	seen := map[string]bool{}
	out := []string{}
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" || seen[value] {
			continue
		}
		seen[value] = true
		out = append(out, value)
	}
	sort.Strings(out)
	return out
}

func CountFilesChangedFromShow(show []byte) int {
	seen := map[string]bool{}
	for _, line := range strings.Split(string(show), "\n") {
		if !strings.HasPrefix(line, "diff --git ") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 4 {
			continue
		}
		path := strings.TrimPrefix(fields[3], "b/")
		if path == "" || path == "/dev/null" {
			path = strings.TrimPrefix(fields[2], "a/")
		}
		if path != "" && path != "/dev/null" {
			seen[path] = true
		}
	}
	return len(seen)
}
