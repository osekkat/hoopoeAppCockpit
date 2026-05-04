package watcher

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	gitadapter "github.com/hoopoe-cockpit/hoopoe/apps/daemon/internal/adapters/git"
	"github.com/hoopoe-cockpit/hoopoe/apps/daemon/internal/api"
	gitevents "github.com/hoopoe-cockpit/hoopoe/apps/daemon/internal/git"
)

func TestPollLocalEmitsVPSCommitCreatedForNewUnpushedCommit(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	client := &fakeGitClient{
		status: &gitadapter.Status{Branch: "main"},
		head: gitadapter.Commit{
			SHA:         "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
			ShortSHA:    "aaaaaaa",
			Subject:     "baseline",
			AuthorName:  "Alice",
			AuthorEmail: "alice@example.invalid",
		},
		filesChanged: 3,
	}
	publisher := &recordingPublisher{}
	w := NewGitWatcher("proj_01", client, publisher)
	w.Now = fixedWatcherNow
	if err := w.Seed(ctx); err != nil {
		t.Fatalf("Seed: %v", err)
	}

	client.head = gitadapter.Commit{
		SHA:         "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
		ShortSHA:    "bbbbbbb",
		Subject:     strings.Repeat("commit message ", 40),
		AuthorName:  "Bob",
		AuthorEmail: "bob@example.invalid",
		ParentSHAs:  []string{"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"},
	}
	client.unpushed = []gitadapter.Commit{{SHA: client.head.SHA}}

	events, err := w.PollLocal(ctx)
	if err != nil {
		t.Fatalf("PollLocal: %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("events = %+v", events)
	}
	if events[0].Channel != "project:proj_01" || events[0].Type != gitevents.EventVPSCommitCreated {
		t.Fatalf("event envelope = %+v", events[0])
	}
	payload, ok := events[0].Data.(gitevents.CommitCreatedPayload)
	if !ok {
		t.Fatalf("payload type = %T", events[0].Data)
	}
	if payload.CommitSHA != client.head.SHA || payload.ParentSHA != "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa" {
		t.Fatalf("payload shas = %+v", payload)
	}
	if payload.FilesChanged != 3 || payload.AuthorEmail != "bob@example.invalid" {
		t.Fatalf("payload metadata = %+v", payload)
	}
	if len(payload.Message) > 243 || !strings.HasSuffix(payload.Message, "...") {
		t.Fatalf("message was not truncated: %q", payload.Message)
	}
	if len(publisher.events) != 1 {
		t.Fatalf("publisher events = %+v", publisher.events)
	}
}

func TestPollLocalRetriesAfterPublishFailure(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	client := &fakeGitClient{
		status: &gitadapter.Status{Branch: "main"},
		head: gitadapter.Commit{
			SHA:     "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
			Subject: "baseline",
		},
	}
	publisher := &recordingPublisher{failNext: errors.New("publish unavailable")}
	w := NewGitWatcher("proj_01", client, publisher)
	w.Now = fixedWatcherNow
	if err := w.Seed(ctx); err != nil {
		t.Fatalf("Seed: %v", err)
	}

	client.head = gitadapter.Commit{
		SHA:        "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
		Subject:    "new commit",
		ParentSHAs: []string{"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"},
	}
	client.unpushed = []gitadapter.Commit{{SHA: client.head.SHA}}

	if _, err := w.PollLocal(ctx); err == nil {
		t.Fatalf("PollLocal should return publish error")
	}
	if w.lastLocalHead == client.head.SHA {
		t.Fatalf("local head advanced after failed publish")
	}

	events, err := w.PollLocal(ctx)
	if err != nil {
		t.Fatalf("PollLocal retry: %v", err)
	}
	if len(events) != 1 || events[0].Type != gitevents.EventVPSCommitCreated {
		t.Fatalf("retry events = %+v", events)
	}

	again, err := w.PollLocal(ctx)
	if err != nil {
		t.Fatalf("PollLocal duplicate: %v", err)
	}
	if len(again) != 0 {
		t.Fatalf("duplicate events = %+v", again)
	}
}

func TestPollLocalEmitsEveryNewUnpushedCommitInOrder(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	client := &fakeGitClient{
		status: &gitadapter.Status{Branch: "main"},
		head: gitadapter.Commit{
			SHA:     watcherTestSHA(0),
			Subject: "baseline",
		},
		filesChanged: 1,
	}
	publisher := &recordingPublisher{}
	w := NewGitWatcher("proj_01", client, publisher)
	w.Now = fixedWatcherNow
	if err := w.Seed(ctx); err != nil {
		t.Fatalf("Seed: %v", err)
	}

	chronological := make([]gitadapter.Commit, 0, 50)
	for i := 1; i <= 50; i++ {
		sha := watcherTestSHA(i)
		chronological = append(chronological, gitadapter.Commit{
			SHA:         sha,
			ShortSHA:    sha[:7],
			Subject:     fmt.Sprintf("commit %02d", i),
			AuthorName:  "Agent",
			AuthorEmail: "agent@example.invalid",
			ParentSHAs:  []string{watcherTestSHA(i - 1)},
		})
	}
	client.head = chronological[len(chronological)-1]
	client.log = newestFirstCommits(chronological)
	client.unpushed = newestFirstCommits(chronological)

	events, err := w.PollLocal(ctx)
	if err != nil {
		t.Fatalf("PollLocal: %v", err)
	}
	if len(events) != 50 {
		t.Fatalf("events = %d, want 50", len(events))
	}
	if len(publisher.events) != 50 {
		t.Fatalf("publisher events = %d, want 50", len(publisher.events))
	}
	for i, event := range events {
		payload, ok := event.Data.(gitevents.CommitCreatedPayload)
		if !ok {
			t.Fatalf("event %d payload type = %T", i, event.Data)
		}
		want := chronological[i]
		if payload.CommitSHA != want.SHA || payload.ParentSHA != want.ParentSHAs[0] {
			t.Fatalf("event %d payload shas = %+v, want %s parent %s", i, payload, want.SHA, want.ParentSHAs[0])
		}
		if payload.Message != want.Subject || payload.AuthorEmail != want.AuthorEmail {
			t.Fatalf("event %d metadata = %+v", i, payload)
		}
	}

	again, err := w.PollLocal(ctx)
	if err != nil {
		t.Fatalf("PollLocal duplicate: %v", err)
	}
	if len(again) != 0 {
		t.Fatalf("duplicate events = %+v", again)
	}
}

func TestRecordPushCompletedEmitsPushAndOriginUpdatedOnce(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	publisher := &recordingPublisher{}
	w := NewGitWatcher("proj_01", &fakeGitClient{status: &gitadapter.Status{Branch: "main"}}, publisher)
	w.Now = fixedWatcherNow

	push := PushCompleted{
		Branch:        "main",
		Remote:        "origin",
		CommitsPushed: []string{"bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb", "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"},
		Duration:      1200 * time.Millisecond,
		OK:            true,
		CausationID:   "push-job-1",
		CorrelationID: "corr-1",
		Refs: []gitevents.RefUpdate{{
			Name:   "refs/heads/main",
			OldSHA: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
			NewSHA: "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
		}},
	}
	events, err := w.RecordPushCompleted(ctx, push)
	if err != nil {
		t.Fatalf("RecordPushCompleted: %v", err)
	}
	if len(events) != 2 {
		t.Fatalf("events = %+v", events)
	}
	if events[0].Type != gitevents.EventVPSPushCompleted || events[1].Type != gitevents.EventOriginUpdated {
		t.Fatalf("event types = %+v", events)
	}
	pushPayload := events[0].Data.(gitevents.PushCompletedPayload)
	if pushPayload.DurationMs != 1200 || len(pushPayload.CommitsPushed) != 1 {
		t.Fatalf("push payload = %+v", pushPayload)
	}
	originPayload := events[1].Data.(gitevents.OriginUpdatedPayload)
	if originPayload.Source != gitevents.OriginUpdateSourceVPSPush || len(originPayload.Refs) != 1 {
		t.Fatalf("origin payload = %+v", originPayload)
	}

	again, err := w.RecordPushCompleted(ctx, push)
	if err != nil {
		t.Fatalf("RecordPushCompleted duplicate: %v", err)
	}
	if len(again) != 0 {
		t.Fatalf("duplicate emitted events: %+v", again)
	}
}

func TestPollOriginExternalPushEmitsOriginUpdatedWithinPoll(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	refs := &fakeRemoteRefs{snapshots: [][]gitevents.RefState{
		{{Name: "refs/heads/main", SHA: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}},
		{{Name: "refs/heads/main", SHA: "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"}},
		{{Name: "refs/heads/main", SHA: "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"}},
	}}
	publisher := &recordingPublisher{}
	w := NewGitWatcher("proj_01", &fakeGitClient{status: &gitadapter.Status{Branch: "main"}}, publisher)
	w.RemoteRefs = refs
	w.Now = fixedWatcherNow

	first, err := w.PollOrigin(ctx)
	if err != nil {
		t.Fatalf("PollOrigin first: %v", err)
	}
	if len(first) != 0 {
		t.Fatalf("first poll should seed only: %+v", first)
	}
	second, err := w.PollOrigin(ctx)
	if err != nil {
		t.Fatalf("PollOrigin second: %v", err)
	}
	if len(second) != 1 || second[0].Type != gitevents.EventOriginUpdated {
		t.Fatalf("second poll events = %+v", second)
	}
	payload := second[0].Data.(gitevents.OriginUpdatedPayload)
	if payload.Source != gitevents.OriginUpdateSourceExternalPush || payload.Refs[0].OldSHA == "" {
		t.Fatalf("payload = %+v", payload)
	}
	third, err := w.PollOrigin(ctx)
	if err != nil {
		t.Fatalf("PollOrigin third: %v", err)
	}
	if len(third) != 0 {
		t.Fatalf("third poll duplicate events = %+v", third)
	}
}

func TestPollOriginRetriesAfterPublishFailure(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	refs := &fakeRemoteRefs{snapshots: [][]gitevents.RefState{
		{{Name: "refs/heads/main", SHA: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}},
		{{Name: "refs/heads/main", SHA: "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"}},
		{{Name: "refs/heads/main", SHA: "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"}},
	}}
	publisher := &recordingPublisher{failNext: errors.New("event hub unavailable")}
	w := NewGitWatcher("proj_01", &fakeGitClient{status: &gitadapter.Status{Branch: "main"}}, publisher)
	w.RemoteRefs = refs
	w.Now = fixedWatcherNow

	if first, err := w.PollOrigin(ctx); err != nil || len(first) != 0 {
		t.Fatalf("first poll = (%+v, %v), want seed only", first, err)
	}
	if _, err := w.PollOrigin(ctx); err == nil {
		t.Fatalf("second poll should return publish error")
	}
	if got := w.lastRemote["refs/heads/main"]; got != "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa" {
		t.Fatalf("remote checkpoint advanced after failed publish: %s", got)
	}

	events, err := w.PollOrigin(ctx)
	if err != nil {
		t.Fatalf("retry poll: %v", err)
	}
	if len(events) != 1 || events[0].Type != gitevents.EventOriginUpdated {
		t.Fatalf("retry events = %+v", events)
	}
	payload := events[0].Data.(gitevents.OriginUpdatedPayload)
	if payload.Refs[0].OldSHA != "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa" ||
		payload.Refs[0].NewSHA != "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb" {
		t.Fatalf("retry payload = %+v", payload)
	}
	if got := w.lastRemote["refs/heads/main"]; got != "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb" {
		t.Fatalf("remote checkpoint after retry = %s", got)
	}
}

func TestEventHubPublisherFlowsThroughSequenceReplay(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	hub := api.NewEventHub(api.EventHubConfig{Now: fixedWatcherNow})
	client := &fakeGitClient{
		status: &gitadapter.Status{Branch: "main"},
		head: gitadapter.Commit{
			SHA:     "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
			Subject: "baseline",
		},
	}
	w := NewGitWatcher("proj_01", client, EventHubPublisher{Hub: hub})
	w.Now = fixedWatcherNow
	if err := w.Seed(ctx); err != nil {
		t.Fatalf("Seed: %v", err)
	}
	client.head = gitadapter.Commit{
		SHA:        "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
		Subject:    "new commit",
		ParentSHAs: []string{"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"},
	}
	client.unpushed = []gitadapter.Commit{{SHA: client.head.SHA}}

	if _, err := w.PollLocal(ctx); err != nil {
		t.Fatalf("PollLocal: %v", err)
	}
	replayed, gap := hub.Replay(gitevents.ProjectChannel("proj_01"), 0)
	if gap {
		t.Fatalf("unexpected replay gap")
	}
	if len(replayed) != 1 || replayed[0].Sequence != 1 || replayed[0].Type != gitevents.EventVPSCommitCreated {
		t.Fatalf("replayed events = %+v", replayed)
	}
}

func TestTrackingBranchRefSourceFetchesAndResolvesRemoteBranches(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	remote := &fakeRemoteGit{
		branches: []gitadapter.Branch{
			{Name: "main"},
			{Name: "feature"},
		},
		shas: map[string]string{
			"origin/main":    "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
			"origin/feature": "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
		},
	}
	refs, err := (TrackingBranchRefSource{Git: remote}).ListRemoteRefs(ctx, "origin")
	if err != nil {
		t.Fatalf("ListRemoteRefs: %v", err)
	}
	if len(remote.fetches) != 1 || remote.fetches[0] != "origin" {
		t.Fatalf("fetches = %+v", remote.fetches)
	}
	if len(refs) != 2 {
		t.Fatalf("refs = %+v", refs)
	}
	if refs[0].Name != "refs/heads/main" || refs[0].SHA == "" {
		t.Fatalf("first ref = %+v", refs[0])
	}
}

func TestCountFilesChangedFromShowCountsDiffHeaders(t *testing.T) {
	t.Parallel()
	show := []byte(strings.Join([]string{
		"commit abc",
		"diff --git a/README.md b/README.md",
		"index 1..2 100644",
		"diff --git a/apps/daemon/main.go b/apps/daemon/main.go",
		"diff --git a/README.md b/README.md",
	}, "\n"))
	if got := CountFilesChangedFromShow(show); got != 2 {
		t.Fatalf("files changed = %d, want 2", got)
	}
}

type fakeGitClient struct {
	status       *gitadapter.Status
	head         gitadapter.Commit
	log          []gitadapter.Commit
	unpushed     []gitadapter.Commit
	filesChanged int
}

func (f *fakeGitClient) Status(context.Context) (*gitadapter.Status, error) {
	if f.status == nil {
		return &gitadapter.Status{Branch: "main"}, nil
	}
	return f.status, nil
}

func (f *fakeGitClient) Log(_ context.Context, opts gitadapter.LogOpts) ([]gitadapter.Commit, error) {
	if f.log != nil {
		limit := opts.Limit
		if limit <= 0 {
			limit = len(f.log)
		}
		out := append([]gitadapter.Commit(nil), f.log...)
		if limit < len(out) {
			out = out[:limit]
		}
		return out, nil
	}
	if f.head.SHA == "" {
		return nil, nil
	}
	return []gitadapter.Commit{f.head}, nil
}

func (f *fakeGitClient) UnpushedCommits(context.Context, string) (*gitadapter.CommitDelta, error) {
	return &gitadapter.CommitDelta{From: "origin/main", To: "main", Commits: f.unpushed}, nil
}

func (f *fakeGitClient) FilesChanged(context.Context, string) (int, error) {
	return f.filesChanged, nil
}

type fakeRemoteRefs struct {
	snapshots [][]gitevents.RefState
	calls     int
}

func (f *fakeRemoteRefs) ListRemoteRefs(context.Context, string) ([]gitevents.RefState, error) {
	if f.calls >= len(f.snapshots) {
		return f.snapshots[len(f.snapshots)-1], nil
	}
	refs := f.snapshots[f.calls]
	f.calls++
	return refs, nil
}

type recordingPublisher struct {
	events   []Event
	failNext error
}

func (p *recordingPublisher) Publish(_ context.Context, event Event) error {
	if p.failNext != nil {
		err := p.failNext
		p.failNext = nil
		return err
	}
	p.events = append(p.events, event)
	return nil
}

func fixedWatcherNow() time.Time {
	return time.Date(2026, 5, 4, 12, 0, 0, 0, time.UTC)
}

func watcherTestSHA(n int) string {
	return fmt.Sprintf("%040d", n)
}

func newestFirstCommits(commits []gitadapter.Commit) []gitadapter.Commit {
	out := make([]gitadapter.Commit, 0, len(commits))
	for i := len(commits) - 1; i >= 0; i-- {
		out = append(out, commits[i])
	}
	return out
}

type fakeRemoteGit struct {
	branches []gitadapter.Branch
	shas     map[string]string
	fetches  []string
}

func (f *fakeRemoteGit) Fetch(_ context.Context, remote string) error {
	f.fetches = append(f.fetches, remote)
	return nil
}

func (f *fakeRemoteGit) Branches(context.Context) ([]gitadapter.Branch, error) {
	return f.branches, nil
}

func (f *fakeRemoteGit) RevParse(_ context.Context, ref string) (string, error) {
	return f.shas[ref], nil
}
