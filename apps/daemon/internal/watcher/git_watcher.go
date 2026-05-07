// Package watcher owns daemon-side state watchers that publish typed events.
package watcher

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
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

	// mu guards lastLocalHead, lastRemote, and seen state. PollLocal,
	// PollOrigin, and RecordPushCompleted can race in production:
	// PollLocal/PollOrigin run on a polling timer, RecordPushCompleted
	// is HTTP-driven from the post-receive hook. The lock is never held
	// across external calls (Git, RemoteRefs, Publisher) so a synchronous
	// publisher cannot self-deadlock by re-entering the watcher.
	mu            sync.Mutex
	lastLocalHead string
	lastRemote    map[string]string
	// pushedSinceLastPoll records the timestamp at which each ref was
	// merged into lastRemote via RecordPushCompleted. It guards against
	// hp-5mc6: a push that completes during a PollOrigin window can be
	// reflected in lastRemote (via mergeRemoteRefs) before origin's
	// ListRemoteRefs view propagates the new ref. The next PollOrigin's
	// changedRefs(lastRemote, next) would then surface the pushed ref
	// as a deletion. Entries here let PollOrigin suppress those false
	// deletes within propagationGrace; older entries are GC'd each poll.
	pushedSinceLastPoll map[string]time.Time
	// seenSet + seenRing form a FIFO-bounded set with per-entry TTL so
	// dedup memory stays capped over the daemon's lifetime. The
	// unbounded map[string]bool version grew O(commits + pushes +
	// origin updates) forever — small per entry, unbounded across
	// months of operation. Capacity bounds memory; TTL ensures stale
	// entries don't linger past the dedup window even when traffic is
	// quiet.
	seenSet  map[string]int
	seenRing []seenEntry
	seenNext int
}

type seenEntry struct {
	key string
	ts  time.Time
}

const (
	seenCapacity = 1024
	seenTTL      = 24 * time.Hour
	// propagationGrace is how long after a push merge to ignore a
	// missing ref in origin's ListRemoteRefs view (hp-5mc6). Origin
	// host replication is typically sub-second; 30s is generous enough
	// to absorb fetch caching + polling jitter without keeping a
	// genuinely-deleted ref alive in the false-delete suppression set.
	propagationGrace = 30 * time.Second
)

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
	var seededRemote map[string]string
	if w.RemoteRefs != nil {
		refs, err := w.RemoteRefs.ListRemoteRefs(ctx, w.remote())
		if err != nil {
			return err
		}
		seededRemote = refMap(refs)
	}
	w.mu.Lock()
	w.lastLocalHead = commit.SHA
	if w.RemoteRefs != nil {
		// hp-dfad: a concurrent RecordPushCompleted may have lazy-
		// inited lastRemote between our ListRemoteRefs call and this
		// lock. Don't overwrite — merge seededRemote in for refs that
		// aren't already present. Pushes are more recent than the
		// tracking-branch snapshot, so an existing entry from the
		// merge wins on overlap.
		if w.lastRemote == nil {
			w.lastRemote = seededRemote
		} else {
			for name, sha := range seededRemote {
				if _, present := w.lastRemote[name]; !present {
					w.lastRemote[name] = sha
				}
			}
		}
	}
	w.mu.Unlock()
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
	w.mu.Lock()
	if w.lastLocalHead == "" {
		w.lastLocalHead = head.SHA
		w.mu.Unlock()
		return nil, nil
	}
	if head.SHA == w.lastLocalHead {
		w.mu.Unlock()
		return nil, nil
	}
	w.mu.Unlock()

	commits, err := w.newUnpushedCommits(ctx, branch, head)
	if err != nil {
		return nil, err
	}
	if len(commits) == 0 {
		w.mu.Lock()
		w.lastLocalHead = head.SHA
		w.mu.Unlock()
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
	w.mu.Lock()
	w.lastLocalHead = head.SHA
	w.mu.Unlock()
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

	w.mu.Lock()
	if w.lastRemote == nil {
		// Lazy-init: seed with next AS WELL AS any refs a concurrent
		// RecordPushCompleted has already merged in. mergeRemoteRefs
		// also lazy-inits under mu, so by the time we hold mu the map
		// either is nil (no merges yet) or already contains them.
		// Pushes are more recent than our ListRemoteRefs snapshot, so
		// the merge wins on overlapping ref names.
		w.lastRemote = next
		w.mu.Unlock()
		return nil, nil
	}
	updates := changedRefs(w.lastRemote, next)
	// hp-5mc6: suppress false-delete events for refs that were merged
	// via RecordPushCompleted within propagationGrace and haven't yet
	// propagated to origin's ListRemoteRefs view. GC entries older than
	// the grace window so a genuinely-deleted ref past grace surfaces
	// as a deletion on the next poll.
	now := w.now()
	if w.pushedSinceLastPoll != nil {
		filtered := updates[:0]
		for _, ref := range updates {
			if ref.Op == gitevents.RefUpdateOpDelete {
				if pushedAt, ok := w.pushedSinceLastPoll[ref.Name]; ok {
					if now.Sub(pushedAt) < propagationGrace {
						// Within grace — suppress; origin hasn't caught up.
						continue
					}
					// Past grace — drop the stamp and let the deletion fire.
					delete(w.pushedSinceLastPoll, ref.Name)
				}
			}
			filtered = append(filtered, ref)
		}
		updates = filtered
		// Independent GC so stamps for refs not in this poll's diff still
		// expire (e.g., the ref propagated cleanly via an update path).
		for name, pushedAt := range w.pushedSinceLastPoll {
			if now.Sub(pushedAt) >= propagationGrace {
				delete(w.pushedSinceLastPoll, name)
			}
		}
	}
	if len(updates) == 0 {
		// hp-dfad: no diff against next, but lastRemote may carry
		// freshly-merged push refs that aren't in next yet (origin
		// hasn't propagated). Don't overwrite — leave lastRemote alone
		// so the next poll can detect either propagation (no-op) or a
		// real divergence.
		w.mu.Unlock()
		return nil, nil
	}
	w.mu.Unlock()

	payload := gitevents.OriginUpdatedPayload{
		ProjectID: w.ProjectID,
		Refs:      updates,
		Source:    gitevents.OriginUpdateSourceExternalPush,
		Time:      w.now(),
	}
	key := originUpdateKey(payload)
	if w.hasSeenEvent(key) {
		// Already published this exact diff — apply it to lastRemote
		// so changedRefs doesn't return the same set on the next
		// poll. Use delta-apply (not overwrite) to preserve concurrent
		// push merges.
		w.applyRefDelta(updates)
		return nil, nil
	}
	event := w.gitEvent(gitevents.EventOriginUpdated, payload)
	if err := w.Publisher.Publish(ctx, event); err != nil {
		return nil, err
	}
	w.markSeenEvent(key)
	// hp-dfad: instead of `w.lastRemote = next` (which dropped any
	// refs RecordPushCompleted merged after we released mu), apply
	// only the diff we computed. Refs touched by a concurrent push
	// are preserved, so we don't re-emit them on the next poll
	// under a different Source (originUpdateKey differs by Source,
	// so the seen-set dedup wouldn't catch the duplicate).
	w.applyRefDelta(updates)
	return []Event{event}, nil
}

// applyRefDelta applies a slice of RefUpdate operations to lastRemote
// under mu. Used by PollOrigin in place of the previous overwrite
// `w.lastRemote = next`. Preserves entries that were not in the
// detected diff — specifically, refs added by a concurrent
// RecordPushCompleted via mergeRemoteRefs.
func (w *GitWatcher) applyRefDelta(updates []gitevents.RefUpdate) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.lastRemote == nil {
		w.lastRemote = map[string]string{}
	}
	for _, ref := range updates {
		switch ref.Op {
		case gitevents.RefUpdateOpDelete:
			delete(w.lastRemote, ref.Name)
		default:
			if ref.NewSHA != "" {
				w.lastRemote[ref.Name] = ref.NewSHA
			}
		}
	}
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
	// w.Now and w.seen are lazy-init'd under the mutex (now() guards
	// nil; hasSeenEvent/markSeenEvent acquire mu before reading the map)
	// so concurrent validate calls are race-free.
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
	w.mu.Lock()
	defer w.mu.Unlock()
	pos, ok := w.seenSet[key]
	if !ok {
		return false
	}
	entry := w.seenRing[pos]
	if entry.key != key {
		// Defensive: the ring slot was evicted by a wrap-around but the
		// map still pointed here. Drop the stale map entry.
		delete(w.seenSet, key)
		return false
	}
	if w.now().Sub(entry.ts) > seenTTL {
		w.seenRing[pos] = seenEntry{}
		delete(w.seenSet, key)
		return false
	}
	return true
}

func (w *GitWatcher) markSeenEvent(key string) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.seenSet == nil {
		w.seenSet = make(map[string]int, seenCapacity)
		w.seenRing = make([]seenEntry, seenCapacity)
	}
	if _, ok := w.seenSet[key]; ok {
		return
	}
	pos := w.seenNext % seenCapacity
	// Evict the oldest entry once the ring has wrapped. While the ring
	// is still filling, the slot holds the zero value and there is
	// nothing to evict.
	if old := w.seenRing[pos]; old.key != "" {
		delete(w.seenSet, old.key)
	}
	w.seenRing[pos] = seenEntry{key: key, ts: w.now()}
	w.seenSet[key] = pos
	w.seenNext++
}

func originUpdateKey(payload gitevents.OriginUpdatedPayload) string {
	parts := []string{gitevents.EventOriginUpdated, payload.ProjectID, payload.Source}
	for _, ref := range normalizeRefUpdates(payload.Refs) {
		parts = append(parts, ref.Name, ref.OldSHA, ref.NewSHA)
	}
	return eventKey(parts...)
}

func (w *GitWatcher) mergeRemoteRefs(refs []gitevents.RefUpdate) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.lastRemote == nil {
		w.lastRemote = map[string]string{}
	}
	if w.pushedSinceLastPoll == nil {
		w.pushedSinceLastPoll = map[string]time.Time{}
	}
	now := w.now()
	for _, ref := range refs {
		if ref.Name != "" && ref.NewSHA != "" {
			w.lastRemote[ref.Name] = ref.NewSHA
			// hp-5mc6: stamp so PollOrigin can ignore origin's lagging
			// view as a "false delete" within propagationGrace.
			w.pushedSinceLastPoll[ref.Name] = now
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

// changedRefs computes the diff between two ref-snapshots as a list of
// RefUpdate operations. It iterates BOTH maps so deletions (ref present
// in old but missing from next) are surfaced as Op=delete events —
// hp-ag1n. The previous implementation only walked next, which silently
// dropped deletions on the floor and broke restartability for branch
// removals.
func changedRefs(old, next map[string]string) []gitevents.RefUpdate {
	updates := []gitevents.RefUpdate{}
	for name, newSHA := range next {
		oldSHA, ok := old[name]
		if ok && oldSHA == newSHA {
			continue
		}
		op := gitevents.RefUpdateOpUpdate
		if !ok || strings.TrimSpace(oldSHA) == "" {
			op = gitevents.RefUpdateOpCreate
		}
		updates = append(updates, gitevents.RefUpdate{Name: name, OldSHA: oldSHA, NewSHA: newSHA, Op: op})
	}
	for name, oldSHA := range old {
		if _, stillPresent := next[name]; stillPresent {
			continue
		}
		updates = append(updates, gitevents.RefUpdate{Name: name, OldSHA: oldSHA, Op: gitevents.RefUpdateOpDelete})
	}
	return normalizeRefUpdates(updates)
}

// normalizeRefUpdates trims whitespace and drops malformed entries.
// Deletions (Op=delete) are kept even with empty NewSHA — the deletion
// shape is structurally distinct from a missing-data error. Updates
// without a NewSHA fall back to Op=delete classification when OldSHA
// is present, otherwise they're discarded (no signal).
func normalizeRefUpdates(refs []gitevents.RefUpdate) []gitevents.RefUpdate {
	out := make([]gitevents.RefUpdate, 0, len(refs))
	for _, ref := range refs {
		ref.Name = strings.TrimSpace(ref.Name)
		ref.OldSHA = strings.TrimSpace(ref.OldSHA)
		ref.NewSHA = strings.TrimSpace(ref.NewSHA)
		if ref.Name == "" {
			continue
		}
		// Reclassify based on shape so callers that build RefUpdate
		// without setting Op (e.g. RecordPushCompleted's push.Refs)
		// still get a correct Op value.
		switch {
		case ref.NewSHA == "" && ref.OldSHA == "":
			continue
		case ref.NewSHA == "":
			ref.Op = gitevents.RefUpdateOpDelete
		case ref.OldSHA == "":
			if ref.Op == "" {
				ref.Op = gitevents.RefUpdateOpCreate
			}
		default:
			if ref.Op == "" {
				ref.Op = gitevents.RefUpdateOpUpdate
			}
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
