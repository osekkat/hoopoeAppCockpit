package watcher

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
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

// TestPollOriginEmitsOriginUpdatedForDeletedBranch guards hp-ag1n: a
// branch deleted from origin previously updated lastRemote silently
// because changedRefs only iterated the new ref map. The fix iterates
// both maps, emits a RefUpdate with Op=delete (empty NewSHA, populated
// OldSHA), and surfaces the event to subscribers + the replay buffer.
func TestPollOriginEmitsOriginUpdatedForDeletedBranch(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	refs := &fakeRemoteRefs{snapshots: [][]gitevents.RefState{
		{
			{Name: "refs/heads/main", SHA: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"},
			{Name: "refs/heads/feature", SHA: "cccccccccccccccccccccccccccccccccccccccc"},
		},
		{
			// feature branch removed from origin; main unchanged.
			{Name: "refs/heads/main", SHA: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"},
		},
	}}
	publisher := &recordingPublisher{}
	w := NewGitWatcher("proj_01", &fakeGitClient{status: &gitadapter.Status{Branch: "main"}}, publisher)
	w.RemoteRefs = refs
	w.Now = fixedWatcherNow

	if first, err := w.PollOrigin(ctx); err != nil || len(first) != 0 {
		t.Fatalf("seed PollOrigin: events=%+v err=%v", first, err)
	}
	second, err := w.PollOrigin(ctx)
	if err != nil {
		t.Fatalf("PollOrigin after deletion: %v", err)
	}
	if len(second) != 1 || second[0].Type != gitevents.EventOriginUpdated {
		t.Fatalf("expected one origin_updated event for the deletion, got %+v", second)
	}
	payload := second[0].Data.(gitevents.OriginUpdatedPayload)
	if len(payload.Refs) != 1 {
		t.Fatalf("expected one RefUpdate (the deletion); got %+v", payload.Refs)
	}
	deleted := payload.Refs[0]
	if deleted.Op != gitevents.RefUpdateOpDelete {
		t.Fatalf("ref.Op = %q, want %q", deleted.Op, gitevents.RefUpdateOpDelete)
	}
	if deleted.Name != "refs/heads/feature" {
		t.Fatalf("ref.Name = %q, want feature branch", deleted.Name)
	}
	if deleted.NewSHA != "" {
		t.Fatalf("ref.NewSHA = %q, want empty for deletion", deleted.NewSHA)
	}
	if deleted.OldSHA != "cccccccccccccccccccccccccccccccccccccccc" {
		t.Fatalf("ref.OldSHA = %q, want feature SHA", deleted.OldSHA)
	}
	// Re-poll once: lastRemote is now updated, so no duplicate emission.
	if third, err := w.PollOrigin(ctx); err != nil || len(third) != 0 {
		t.Fatalf("third PollOrigin should be quiet: events=%+v err=%v", third, err)
	}
}

// TestPollOriginPreservesPushMergedRefsOnDeltaApply guards hp-dfad: a
// push that completes during PollOrigin's publish window used to be
// dropped by the post-publish `w.lastRemote = next` overwrite, then
// re-emitted by the next poll under a different OriginUpdatedPayload
// Source — bypassing the seen-set dedup (which keys on Source). The
// delta-apply fix preserves concurrently-merged push refs in
// lastRemote so the next poll sees no diff.
func TestPollOriginPreservesPushMergedRefsOnDeltaApply(t *testing.T) {
	t.Parallel()
	const (
		mainOldSHA = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
		mainNewSHA = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
		featureSHA = "cccccccccccccccccccccccccccccccccccccccc"
	)
	ctx := context.Background()
	refs := &fakeRemoteRefs{snapshots: [][]gitevents.RefState{
		// Seed sees only main.
		{{Name: "refs/heads/main", SHA: mainOldSHA}},
		// First PollOrigin sees main updated externally; feature is
		// pushed during the publish window but origin's tracking
		// branch hasn't propagated it yet.
		{{Name: "refs/heads/main", SHA: mainNewSHA}},
		// Second PollOrigin sees both refs (push propagated).
		{
			{Name: "refs/heads/main", SHA: mainNewSHA},
			{Name: "refs/heads/feature", SHA: featureSHA},
		},
	}}
	publisher := &recordingPublisher{}
	w := NewGitWatcher("proj_01", &fakeGitClient{status: &gitadapter.Status{Branch: "main"}}, publisher)
	w.RemoteRefs = refs
	w.Now = fixedWatcherNow
	if err := w.Seed(ctx); err != nil {
		t.Fatalf("Seed: %v", err)
	}

	// Mid-publish injection: simulate the post-receive hook firing
	// while PollOrigin is between its initial Unlock and its post-
	// publish re-Lock. RecordPushCompleted merges feature into
	// lastRemote — pre-fix, the subsequent `w.lastRemote = next`
	// overwrite dropped it.
	pushed := false
	publisher.onPublish = func(_ Event) {
		if pushed {
			return
		}
		pushed = true
		_, err := w.RecordPushCompleted(ctx, PushCompleted{
			Branch:        "feature",
			Remote:        "origin",
			CommitsPushed: []string{featureSHA},
			Refs: []gitevents.RefUpdate{{
				Name:   "refs/heads/feature",
				NewSHA: featureSHA,
			}},
			OK: true,
		})
		if err != nil {
			t.Errorf("RecordPushCompleted: %v", err)
		}
	}

	if _, err := w.PollOrigin(ctx); err != nil {
		t.Fatalf("first PollOrigin: %v", err)
	}

	// After the first poll: lastRemote must contain BOTH the external
	// main update AND the concurrently-merged feature push. Pre-fix,
	// feature would have been dropped by the post-publish overwrite.
	w.mu.Lock()
	gotMain := w.lastRemote["refs/heads/main"]
	gotFeature := w.lastRemote["refs/heads/feature"]
	w.mu.Unlock()
	if gotMain != mainNewSHA {
		t.Fatalf("lastRemote[main] = %q, want %q (delta-apply must update)", gotMain, mainNewSHA)
	}
	if gotFeature != featureSHA {
		t.Fatalf("lastRemote[feature] = %q, want %q — hp-dfad regression: push merge dropped by overwrite", gotFeature, featureSHA)
	}

	// Disable the injection so the second poll runs cleanly.
	publisher.onPublish = nil

	// Second poll: ListRemoteRefs now returns both refs. lastRemote
	// already has them, so no diff and no event. Pre-fix, feature
	// would have been re-emitted as a "create" with Source=external
	// (different from the push's Source=vps_push), bypassing the
	// seen-set dedup → duplicate origin event for the same push.
	events, err := w.PollOrigin(ctx)
	if err != nil {
		t.Fatalf("second PollOrigin: %v", err)
	}
	for _, ev := range events {
		if ev.Type != gitevents.EventOriginUpdated {
			continue
		}
		payload, ok := ev.Data.(gitevents.OriginUpdatedPayload)
		if !ok {
			continue
		}
		for _, ref := range payload.Refs {
			if ref.Name == "refs/heads/feature" {
				t.Fatalf("second poll re-emitted feature push: payload=%+v — hp-dfad regression", payload)
			}
		}
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

// TestGitWatcherEventHubRedactsCommitMessageSecret guards hp-aek.1: a commit
// subject containing a secret-shaped string must not survive the EventHub
// redactor on the subscriber wire or in the replay buffer. CommitCreatedPayload
// is a typed struct, which the original RedactValue walker did not traverse —
// without producer- or central-redactor coverage of struct fields, the secret
// would reach WS/SSE subscribers raw.
func TestGitWatcherEventHubRedactsCommitMessageSecret(t *testing.T) {
	t.Parallel()
	const secret = "sk-abcdef0123456789ABCDEF0123456789"
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	hub := api.NewEventHub(api.EventHubConfig{Now: fixedWatcherNow})
	sub := hub.Subscribe(ctx, []string{gitevents.ProjectChannel("proj_01")})
	defer sub.Close()

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
		Subject:    "feat: rotate provider " + secret + " trailer",
		ParentSHAs: []string{"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"},
	}
	client.unpushed = []gitadapter.Commit{{SHA: client.head.SHA, Subject: client.head.Subject}}

	if _, err := w.PollLocal(ctx); err != nil {
		t.Fatalf("PollLocal: %v", err)
	}

	select {
	case ev := <-sub.Events():
		body, err := json.Marshal(ev.Data)
		if err != nil {
			t.Fatalf("marshal delivered Data: %v", err)
		}
		if strings.Contains(string(body), "sk-abcdef") {
			t.Fatalf("subscriber received raw commit-message secret: %s", body)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for delivered commit event")
	}

	replayed, gap := hub.Replay(gitevents.ProjectChannel("proj_01"), 0)
	if gap {
		t.Fatalf("unexpected replay gap")
	}
	if len(replayed) != 1 || replayed[0].Type != gitevents.EventVPSCommitCreated {
		t.Fatalf("replayed events = %+v", replayed)
	}
	body, err := json.Marshal(replayed[0].Data)
	if err != nil {
		t.Fatalf("marshal replay Data: %v", err)
	}
	if strings.Contains(string(body), "sk-abcdef") {
		t.Fatalf("replay buffer holds raw commit-message secret: %s", body)
	}
}

// TestGitWatcherConcurrentMethodsRaceFree guards hp-zuvg: in production,
// PollLocal/PollOrigin run on a polling timer while RecordPushCompleted
// is HTTP-driven from the post-receive hook. Without internal
// synchronization, all three race on lastLocalHead/lastRemote/seen. This
// test fans out concurrent calls so that `go test -race` will flag any
// regression.
func TestGitWatcherConcurrentMethodsRaceFree(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	client := &concurrentFakeGitClient{}
	client.installHead("aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", "main")
	refs := &concurrentFakeRemoteRefs{}
	refs.set([]gitevents.RefState{{Name: "refs/heads/main", SHA: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}})

	w := NewGitWatcher("proj_01", client, &nopPublisher{})
	w.Now = fixedWatcherNow
	w.RemoteRefs = refs
	if err := w.Seed(ctx); err != nil {
		t.Fatalf("Seed: %v", err)
	}

	const iterations = 64
	var wg sync.WaitGroup
	worker := func(fn func()) {
		defer wg.Done()
		for i := 0; i < iterations; i++ {
			fn()
		}
	}

	wg.Add(3)
	go worker(func() {
		client.advanceLocal()
		_, _ = w.PollLocal(ctx)
	})
	go worker(func() {
		refs.advance()
		_, _ = w.PollOrigin(ctx)
	})
	go worker(func() {
		_, _ = w.RecordPushCompleted(ctx, PushCompleted{
			Branch:        "main",
			Remote:        "origin",
			CommitsPushed: []string{client.headSHA()},
			Refs: []gitevents.RefUpdate{{
				Name:   "refs/heads/main",
				NewSHA: client.headSHA(),
			}},
			OK: true,
		})
	})
	wg.Wait()
}

// TestGitWatcherSeenSetBoundedAndTTLEvicts guards hp-bxyc: the seen
// dedup map used to grow O(commits + pushes + origin updates) over the
// daemon's lifetime. After the bound, len(seenSet) must never exceed
// seenCapacity, and entries older than seenTTL are treated as not
// present (so a long-quiet watcher doesn't hold yesterday's keys
// indefinitely).
func TestGitWatcherSeenSetBoundedAndTTLEvicts(t *testing.T) {
	t.Parallel()
	w := &GitWatcher{ProjectID: "proj_bound"}
	now := time.Date(2026, 5, 4, 0, 0, 0, 0, time.UTC)
	w.Now = func() time.Time { return now }

	// Insert 4× capacity unique events; the bound must keep len(seenSet)
	// at exactly seenCapacity once the ring has wrapped.
	for i := 0; i < seenCapacity*4; i++ {
		key := fmt.Sprintf("evt-%07d", i)
		w.markSeenEvent(key)
	}
	w.mu.Lock()
	gotLen := len(w.seenSet)
	w.mu.Unlock()
	if gotLen != seenCapacity {
		t.Fatalf("len(seenSet) = %d, want %d (cap)", gotLen, seenCapacity)
	}

	// Earliest 3× capacity entries must have been evicted.
	earlyKey := "evt-0000000"
	if w.hasSeenEvent(earlyKey) {
		t.Fatalf("earliest event %q still resident; FIFO bound did not evict", earlyKey)
	}
	// Most recent capacity-worth of entries must still be resident
	// (and not yet TTL-expired since wall clock hasn't moved).
	recentKey := fmt.Sprintf("evt-%07d", seenCapacity*4-1)
	if !w.hasSeenEvent(recentKey) {
		t.Fatalf("most recent event %q evicted unexpectedly", recentKey)
	}

	// Advance past TTL — every remaining entry should now look unseen
	// to producers, so a re-issued event would be re-emitted (which is
	// the desired "old enough that we forget" behavior).
	now = now.Add(seenTTL + time.Hour)
	if w.hasSeenEvent(recentKey) {
		t.Fatalf("event %q still seen after TTL expiry; expected eviction on read", recentKey)
	}
}

type concurrentFakeGitClient struct {
	mu     sync.Mutex
	head   gitadapter.Commit
	branch string
	tick   atomic.Uint64
}

func (c *concurrentFakeGitClient) installHead(sha, branch string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.head = gitadapter.Commit{SHA: sha, Subject: "head"}
	c.branch = branch
}

func (c *concurrentFakeGitClient) advanceLocal() {
	c.mu.Lock()
	defer c.mu.Unlock()
	n := c.tick.Add(1)
	c.head = gitadapter.Commit{SHA: fmt.Sprintf("%040d", n), Subject: fmt.Sprintf("commit-%d", n)}
}

func (c *concurrentFakeGitClient) headSHA() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.head.SHA
}

func (c *concurrentFakeGitClient) Status(context.Context) (*gitadapter.Status, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return &gitadapter.Status{Branch: c.branch}, nil
}

func (c *concurrentFakeGitClient) Log(context.Context, gitadapter.LogOpts) ([]gitadapter.Commit, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.head.SHA == "" {
		return nil, nil
	}
	return []gitadapter.Commit{c.head}, nil
}

func (c *concurrentFakeGitClient) UnpushedCommits(context.Context, string) (*gitadapter.CommitDelta, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return &gitadapter.CommitDelta{From: "origin/main", To: "main", Commits: []gitadapter.Commit{c.head}}, nil
}

type concurrentFakeRemoteRefs struct {
	mu   sync.Mutex
	refs []gitevents.RefState
	tick atomic.Uint64
}

func (f *concurrentFakeRemoteRefs) set(refs []gitevents.RefState) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.refs = append([]gitevents.RefState(nil), refs...)
}

func (f *concurrentFakeRemoteRefs) advance() {
	f.mu.Lock()
	defer f.mu.Unlock()
	n := f.tick.Add(1)
	f.refs = []gitevents.RefState{{Name: "refs/heads/main", SHA: fmt.Sprintf("%040d", n)}}
}

func (f *concurrentFakeRemoteRefs) ListRemoteRefs(context.Context, string) ([]gitevents.RefState, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]gitevents.RefState(nil), f.refs...), nil
}

type nopPublisher struct{}

func (nopPublisher) Publish(context.Context, Event) error { return nil }

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
	// onPublish lets a test inject a side effect (e.g. a concurrent
	// RecordPushCompleted) at the moment Publish is invoked, simulating
	// concurrent activity that would otherwise be hard to schedule
	// deterministically. Used by hp-dfad's regression test.
	onPublish func(event Event)
}

func (p *recordingPublisher) Publish(_ context.Context, event Event) error {
	if p.failNext != nil {
		err := p.failNext
		p.failNext = nil
		return err
	}
	if p.onPublish != nil {
		p.onPublish(event)
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
