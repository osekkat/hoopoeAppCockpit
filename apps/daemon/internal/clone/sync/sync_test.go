package clonesync

import (
	"context"
	"errors"
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/hoopoe-cockpit/hoopoe/apps/daemon/internal/api"
	gitevents "github.com/hoopoe-cockpit/hoopoe/apps/daemon/internal/git"
)

func TestOriginUpdatedTriggersFetchWithEventMetadata(t *testing.T) {
	t.Parallel()
	fetcher := &fakeFetcher{}
	service := newTestService(t, fetcher)
	event := api.Event{
		EventID:       "evt-origin",
		Channel:       gitevents.ProjectChannel("proj_01"),
		Type:          gitevents.EventOriginUpdated,
		Sequence:      7,
		CausationID:   "push-job",
		CorrelationID: "corr-1",
		Data: gitevents.OriginUpdatedPayload{
			ProjectID: "proj_01",
			Source:    gitevents.OriginUpdateSourceVPSPush,
			Refs:      []gitevents.RefUpdate{{Name: "refs/heads/main", NewSHA: "bbbb"}},
			Time:      fixedSyncNow(),
		},
	}

	records, err := service.HandleEvent(context.Background(), event)
	if err != nil {
		t.Fatalf("HandleEvent: %v", err)
	}
	if len(records) != 1 {
		t.Fatalf("records = %+v", records)
	}
	record := records[0]
	if record.Trigger.Kind != TriggerOriginUpdated || record.Trigger.EventID != "evt-origin" || record.Trigger.Sequence != 7 {
		t.Fatalf("trigger = %+v", record.Trigger)
	}
	if len(fetcher.calls) != 1 || fetcher.calls[0].Project.ID != "proj_01" {
		t.Fatalf("fetch calls = %+v", fetcher.calls)
	}
	state, err := service.State("proj_01")
	if err != nil {
		t.Fatalf("State: %v", err)
	}
	if state.LastFetch == nil || state.LastFetch.Trigger.EventID != "evt-origin" {
		t.Fatalf("state last fetch = %+v", state.LastFetch)
	}
}

func TestVPSCommitCreatedUpdatesUnpushedOverlayWithoutFetch(t *testing.T) {
	t.Parallel()
	fetcher := &fakeFetcher{}
	service := newTestService(t, fetcher)
	event := api.Event{
		EventID:  "evt-commit",
		Channel:  gitevents.ProjectChannel("proj_01"),
		Type:     gitevents.EventVPSCommitCreated,
		Sequence: 3,
		Data: gitevents.CommitCreatedPayload{
			ProjectID: "proj_01",
			CommitSHA: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
			Branch:    "main",
			Time:      fixedSyncNow(),
		},
	}

	records, err := service.HandleEvent(context.Background(), event)
	if err != nil {
		t.Fatalf("HandleEvent: %v", err)
	}
	if len(records) != 0 || len(fetcher.calls) != 0 {
		t.Fatalf("commit event should not fetch, records=%+v calls=%+v", records, fetcher.calls)
	}
	state, err := service.State("proj_01")
	if err != nil {
		t.Fatalf("State: %v", err)
	}
	if state.UnpushedCount != 1 || !reflect.DeepEqual(state.UnpushedCommits, []string{"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}) {
		t.Fatalf("state overlay = %+v", state)
	}
}

func TestSuccessfulPushClearsPushedCommitsAndFetches(t *testing.T) {
	t.Parallel()
	fetcher := &fakeFetcher{}
	service := newTestService(t, fetcher)
	for _, sha := range []string{
		"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		"bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
	} {
		_, err := service.HandleEvent(context.Background(), api.Event{
			EventID: "commit-" + sha[:1],
			Channel: gitevents.ProjectChannel("proj_01"),
			Type:    gitevents.EventVPSCommitCreated,
			Data:    gitevents.CommitCreatedPayload{ProjectID: "proj_01", CommitSHA: sha},
		})
		if err != nil {
			t.Fatalf("commit event: %v", err)
		}
	}
	push := api.Event{
		EventID:  "evt-push",
		Channel:  gitevents.ProjectChannel("proj_01"),
		Type:     gitevents.EventVPSPushCompleted,
		Sequence: 9,
		Data: gitevents.PushCompletedPayload{
			ProjectID:     "proj_01",
			OK:            true,
			CommitsPushed: []string{"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"},
			Remote:        "origin",
			Time:          fixedSyncNow(),
		},
	}

	records, err := service.HandleEvent(context.Background(), push)
	if err != nil {
		t.Fatalf("HandleEvent: %v", err)
	}
	if len(records) != 1 || records[0].Trigger.Kind != TriggerVPSPush {
		t.Fatalf("records = %+v", records)
	}
	state, err := service.State("proj_01")
	if err != nil {
		t.Fatalf("State: %v", err)
	}
	if state.UnpushedCount != 1 || !reflect.DeepEqual(state.UnpushedCommits, []string{"bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"}) {
		t.Fatalf("state overlay after push = %+v", state)
	}
}

func TestFailedPushDoesNotFetchOrClearOverlay(t *testing.T) {
	t.Parallel()
	fetcher := &fakeFetcher{}
	service := newTestService(t, fetcher)
	_, err := service.HandleEvent(context.Background(), api.Event{
		EventID: "evt-commit",
		Channel: gitevents.ProjectChannel("proj_01"),
		Type:    gitevents.EventVPSCommitCreated,
		Data:    gitevents.CommitCreatedPayload{ProjectID: "proj_01", CommitSHA: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"},
	})
	if err != nil {
		t.Fatalf("commit event: %v", err)
	}

	records, err := service.HandleEvent(context.Background(), api.Event{
		EventID: "evt-push-fail",
		Channel: gitevents.ProjectChannel("proj_01"),
		Type:    gitevents.EventVPSPushCompleted,
		Data: gitevents.PushCompletedPayload{
			ProjectID:     "proj_01",
			OK:            false,
			CommitsPushed: []string{"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"},
		},
	})
	if err != nil {
		t.Fatalf("failed push event: %v", err)
	}
	if len(records) != 0 || len(fetcher.calls) != 0 {
		t.Fatalf("failed push should not fetch, records=%+v calls=%+v", records, fetcher.calls)
	}
	state, err := service.State("proj_01")
	if err != nil {
		t.Fatalf("State: %v", err)
	}
	if state.UnpushedCount != 1 {
		t.Fatalf("unpushed count = %d, want 1", state.UnpushedCount)
	}
}

func TestManualPollAndReconnectTriggers(t *testing.T) {
	t.Parallel()
	fetcher := &fakeFetcher{}
	service := newTestService(t, fetcher)

	manual, err := service.Refresh(context.Background(), "proj_01")
	if err != nil {
		t.Fatalf("Refresh: %v", err)
	}
	reconnect, err := service.Reconnect(context.Background(), "proj_01", 41)
	if err != nil {
		t.Fatalf("Reconnect: %v", err)
	}
	poll, err := service.Poll(context.Background())
	if err != nil {
		t.Fatalf("Poll: %v", err)
	}
	got := []string{manual.Trigger.Kind, reconnect.Trigger.Kind, poll[0].Trigger.Kind}
	want := []string{TriggerManualRefresh, TriggerReconnect, TriggerSafetyPoll}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("trigger kinds = %+v, want %+v", got, want)
	}
	if reconnect.Trigger.Sequence != 41 {
		t.Fatalf("reconnect sequence = %d, want 41", reconnect.Trigger.Sequence)
	}
}

func TestProjectFetchesAreSerialized(t *testing.T) {
	t.Parallel()
	fetcher := &fakeFetcher{hold: make(chan struct{})}
	service := newTestService(t, fetcher)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	errs := make(chan error, 2)
	go func() {
		_, err := service.Refresh(ctx, "proj_01")
		errs <- err
	}()
	fetcher.waitForActive(t)
	go func() {
		_, err := service.Refresh(ctx, "proj_01")
		errs <- err
	}()
	fetcher.waitForWaiting(t)

	close(fetcher.hold)
	for i := 0; i < 2; i++ {
		if err := <-errs; err != nil {
			t.Fatalf("refresh %d: %v", i, err)
		}
	}
	if fetcher.maxActive != 1 {
		t.Fatalf("max active fetches = %d, want 1", fetcher.maxActive)
	}
}

func TestRunSubscribesToProjectEventsAndPolls(t *testing.T) {
	t.Parallel()
	fetcher := &fakeFetcher{}
	source := newFakeSource()
	service, err := NewService(Config{
		Projects:     []Project{{ID: "proj_01", ClonePath: "/tmp/proj_01"}},
		Fetcher:      fetcher,
		EventSource:  source,
		PollInterval: 10 * time.Millisecond,
		Now:          fixedSyncNow,
	})
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- service.Run(ctx) }()
	source.waitForSubscription(t)
	source.publish(api.Event{
		EventID: "evt-origin",
		Channel: gitevents.ProjectChannel("proj_01"),
		Type:    gitevents.EventOriginUpdated,
		Data:    map[string]any{"projectId": "proj_01"},
	})
	fetcher.waitForCalls(t, 1)
	cancel()
	err = <-done
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Run err = %v, want context.Canceled", err)
	}
	if len(source.channels) != 1 || source.channels[0] != gitevents.ProjectChannel("proj_01") {
		t.Fatalf("subscribed channels = %+v", source.channels)
	}
}

func newTestService(t *testing.T, fetcher *fakeFetcher) *Service {
	t.Helper()
	service, err := NewService(Config{
		Projects: []Project{{ID: "proj_01", ClonePath: "/tmp/proj_01"}},
		Fetcher:  fetcher,
		Now:      fixedSyncNow,
	})
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	return service
}

type fakeFetcher struct {
	mu        sync.Mutex
	cond      *sync.Cond
	calls     []fetchCall
	active    int
	maxActive int
	waiting   int
	hold      chan struct{}
	err       error
}

type fetchCall struct {
	Project Project
	Trigger Trigger
}

func (f *fakeFetcher) Fetch(_ context.Context, project Project, trigger Trigger) (FetchResult, error) {
	f.mu.Lock()
	if f.cond == nil {
		f.cond = sync.NewCond(&f.mu)
	}
	f.active++
	if f.active > f.maxActive {
		f.maxActive = f.active
	}
	f.calls = append(f.calls, fetchCall{Project: project, Trigger: trigger})
	f.cond.Broadcast()
	hold := f.hold
	err := f.err
	f.mu.Unlock()

	if hold != nil {
		<-hold
	}

	f.mu.Lock()
	f.active--
	f.cond.Broadcast()
	f.mu.Unlock()
	return FetchResult{Remote: project.Remote}, err
}

func (f *fakeFetcher) waitForActive(t *testing.T) {
	t.Helper()
	f.waitUntil(t, func() bool { return f.active == 1 })
}

func (f *fakeFetcher) waitForWaiting(t *testing.T) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		f.mu.Lock()
		calls := len(f.calls)
		active := f.active
		f.mu.Unlock()
		if calls == 1 && active == 1 {
			time.Sleep(10 * time.Millisecond)
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("second fetch did not wait behind active fetch")
}

func (f *fakeFetcher) waitForCalls(t *testing.T, count int) {
	t.Helper()
	f.waitUntil(t, func() bool { return len(f.calls) >= count })
}

func (f *fakeFetcher) waitUntil(t *testing.T, ok func() bool) {
	t.Helper()
	if f.cond == nil {
		f.cond = sync.NewCond(&f.mu)
	}
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		f.mu.Lock()
		done := ok()
		f.mu.Unlock()
		if done {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("condition not met before deadline")
}

type fakeSource struct {
	mu       sync.Mutex
	channels []string
	sub      *fakeSubscription
}

type fakeSubscription struct {
	events chan api.Event
}

func newFakeSource() *fakeSource {
	return &fakeSource{}
}

func (f *fakeSource) Subscribe(_ context.Context, channels []string) Subscription {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.channels = append([]string(nil), channels...)
	f.sub = &fakeSubscription{events: make(chan api.Event, 8)}
	return f.sub
}

func (f *fakeSource) waitForSubscription(t *testing.T) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		f.mu.Lock()
		ok := f.sub != nil
		f.mu.Unlock()
		if ok {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("subscription was not created")
}

func (f *fakeSource) publish(event api.Event) {
	f.mu.Lock()
	sub := f.sub
	f.mu.Unlock()
	sub.events <- event
}

func (s *fakeSubscription) Events() <-chan api.Event {
	return s.events
}

func (s *fakeSubscription) Close() {
	close(s.events)
}

func fixedSyncNow() time.Time {
	return time.Date(2026, 5, 4, 12, 0, 0, 0, time.UTC)
}
