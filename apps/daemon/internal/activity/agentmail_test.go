package activity

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/hoopoe-cockpit/hoopoe/apps/daemon/internal/adapters/agentmail"
	"github.com/hoopoe-cockpit/hoopoe/apps/daemon/internal/api"
)

func TestAgentMailMessagesPublishActivityEvents(t *testing.T) {
	t.Parallel()
	ingestor := newTestIngestor(t, &fakeAgentMail{})

	events := ingestor.IngestMessages([]agentmail.Message{
		{
			ID:          10,
			ThreadID:    "br-hp-3se",
			Subject:     "status check",
			BodyMD:      "please reply when clear",
			Importance:  "urgent",
			AckRequired: true,
			CreatedTS:   "2026-05-04T00:00:00Z",
			From:        "TealPond",
			To:          []string{"WhiteStream"},
		},
		{
			ID:        11,
			ThreadID:  "br-hp-3se",
			Subject:   "sent note",
			CreatedTS: "2026-05-04T00:00:01Z",
			From:      "WhiteStream",
			To:        []string{"TealPond"},
		},
	})

	if len(events) != 2 {
		t.Fatalf("events len = %d, want 2", len(events))
	}
	if events[0].Channel != "activity:proj_01" || events[0].Type != EventMailUrgent {
		t.Fatalf("urgent event = %+v", events[0])
	}
	data := events[0].Data.(ActivityData)
	if data.Importance != "urgent" || data.BeadID != "hp-3se" || data.Pivot == nil || data.Pivot.BeadID != "hp-3se" {
		t.Fatalf("urgent data = %+v", data)
	}
	if len(data.Pills) != 2 {
		t.Fatalf("urgent pills = %+v, want bead + ack", data.Pills)
	}
	if events[1].Type != EventMailSent {
		t.Fatalf("sent event type = %s", events[1].Type)
	}

	deduped := ingestor.IngestMessages([]agentmail.Message{{ID: 10, Subject: "duplicate", From: "TealPond"}})
	if len(deduped) != 0 {
		t.Fatalf("duplicate message emitted events: %+v", deduped)
	}
}

func TestReservationSnapshotsPublishConflictsStaleRenewAndRelease(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 5, 4, 1, 0, 0, 0, time.UTC)
	ingestor := newTestIngestor(t, &fakeAgentMail{})
	ingestor.now = func() time.Time { return now }

	events := ingestor.IngestReservations([]agentmail.Reservation{
		{
			ID:          1,
			Agent:       "BlueLake",
			PathPattern: "apps/daemon/internal/api/**",
			Exclusive:   true,
			Reason:      "br-hp-3se",
			CreatedTS:   "2026-05-04T00:00:00Z",
			ExpiresTS:   "2026-05-04T02:00:00Z",
		},
		{
			ID:          2,
			Agent:       "WhiteStream",
			PathPattern: "apps/daemon/internal/api/router.go",
			Exclusive:   true,
			Reason:      "br-hp-3se",
			CreatedTS:   "2026-05-04T00:00:00Z",
			ExpiresTS:   "2026-05-04T02:00:00Z",
		},
		{
			ID:          3,
			Agent:       "GrayField",
			PathPattern: "apps/daemon/internal/jobs/**",
			Exclusive:   true,
			Reason:      "br-hp-3se",
			CreatedTS:   "2026-05-04T00:00:00Z",
			ExpiresTS:   "2026-05-04T00:59:00Z",
		},
	})

	if countType(events, EventReservationRequested) != 3 {
		t.Fatalf("requested count = %d, events=%+v", countType(events, EventReservationRequested), events)
	}
	if countType(events, EventReservationConflicted) != 2 {
		t.Fatalf("conflict count = %d, events=%+v", countType(events, EventReservationConflicted), events)
	}
	var sawOverlap bool
	var sawStale bool
	for _, event := range events {
		if event.Type != EventReservationConflicted {
			continue
		}
		data := event.Data.(ActivityData)
		if data.Importance != "urgent" {
			t.Fatalf("conflict importance = %s", data.Importance)
		}
		switch data.Conflict.Type {
		case "overlap":
			sawOverlap = true
			if data.Conflict.Other == nil || data.Conflict.Other.Agent != "WhiteStream" {
				t.Fatalf("overlap conflict = %+v", data.Conflict)
			}
		case "stale":
			sawStale = true
			if data.Conflict.Action["kind"] != "reservation.force_release" {
				t.Fatalf("stale action = %+v", data.Conflict.Action)
			}
		}
	}
	if !sawOverlap || !sawStale {
		t.Fatalf("sawOverlap=%t sawStale=%t", sawOverlap, sawStale)
	}

	events = ingestor.IngestReservations([]agentmail.Reservation{
		{
			ID:          1,
			Agent:       "BlueLake",
			PathPattern: "apps/daemon/internal/api/**",
			Exclusive:   true,
			Reason:      "br-hp-3se",
			CreatedTS:   "2026-05-04T00:00:00Z",
			ExpiresTS:   "2026-05-04T03:00:00Z",
		},
	})
	if countType(events, EventReservationRenewed) != 1 {
		t.Fatalf("renewed count = %d, events=%+v", countType(events, EventReservationRenewed), events)
	}
	if countType(events, EventReservationReleased) != 2 {
		t.Fatalf("released count = %d, events=%+v", countType(events, EventReservationReleased), events)
	}
}

func TestSyncOncePollsAgentMailAndPublishesToEventHub(t *testing.T) {
	t.Parallel()
	client := &fakeAgentMail{
		inbox: []agentmail.Message{{ID: 41, Subject: "hello", From: "BlueLake", To: []string{"WhiteStream"}}},
		reservations: []agentmail.Reservation{{
			ID:          9,
			Agent:       "BlueLake",
			PathPattern: "apps/daemon/internal/activity/**",
			Exclusive:   true,
			Reason:      "hp-3se",
			ExpiresTS:   "2026-05-04T02:00:00Z",
		}},
	}
	ingestor := newTestIngestor(t, client)

	events, err := ingestor.SyncOnce(context.Background())
	if err != nil {
		t.Fatalf("SyncOnce: %v", err)
	}
	if len(events) != 2 {
		t.Fatalf("events len = %d, want 2", len(events))
	}
	if client.fetch.ProjectKey != "/repo" || client.fetch.AgentName != "WhiteStream" || !client.fetch.IncludeBodies {
		t.Fatalf("fetch request = %+v", client.fetch)
	}
	if client.list.Project != "/repo" || client.list.ActiveOnly {
		t.Fatalf("list request = %+v", client.list)
	}

	replayed, gap := ingestor.events.(*api.EventHub).Replay("activity:proj_01", 0)
	if gap || len(replayed) != 2 {
		t.Fatalf("replay gap=%t events=%+v", gap, replayed)
	}
	data := replayed[1].Data.(ActivityData)
	if data.BeadID != "hp-3se" {
		t.Fatalf("reservation bead id = %q", data.BeadID)
	}
}

func TestForceReleaseDelegatesAndPublishesOutcome(t *testing.T) {
	t.Parallel()
	client := &fakeAgentMail{
		forceResponse: agentmail.ForceReleaseReservationResponse{Released: 1, ReleasedAt: "2026-05-04T00:00:00Z"},
	}
	ingestor := newTestIngestor(t, client)

	out, event, err := ingestor.ForceReleaseReservation(context.Background(), agentmail.ForceReleaseReservationRequest{
		FileReservationID: 7,
		Note:              "stale holder",
	})
	if err != nil {
		t.Fatalf("ForceReleaseReservation: %v", err)
	}
	if out.Released != 1 || event.Type != EventReservationReleased {
		t.Fatalf("out=%+v event=%+v", out, event)
	}
	if client.force.ProjectKey != "/repo" || client.force.AgentName != "WhiteStream" {
		t.Fatalf("force request defaults = %+v", client.force)
	}

	client.forceErr = errors.New("nope")
	_, failed, err := ingestor.ForceReleaseReservation(context.Background(), agentmail.ForceReleaseReservationRequest{
		FileReservationID: 8,
		Note:              "stale holder",
	})
	if err == nil || failed.Type != EventReservationConflicted {
		t.Fatalf("failed event=%+v err=%v", failed, err)
	}
}

func TestIngestMessagesPublishesAfterReleasingMutex(t *testing.T) {
	t.Parallel()
	publisher := &reentrantPublisher{}
	ingestor := newTestIngestorWithPublisher(t, &fakeAgentMail{}, publisher)
	reentered := false
	innerEvents := -1
	publisher.onPublish = func() {
		if reentered {
			return
		}
		reentered = true
		inner := ingestor.IngestMessages([]agentmail.Message{{
			ID:      102,
			Subject: "reentrant message",
			From:    "TealPond",
		}})
		innerEvents = len(inner)
	}

	done := make(chan []api.Event, 1)
	go func() {
		done <- ingestor.IngestMessages([]agentmail.Message{{
			ID:      101,
			Subject: "outer message",
			From:    "BlueLake",
		}})
	}()

	select {
	case events := <-done:
		if len(events) != 1 {
			t.Fatalf("outer events len = %d, want 1", len(events))
		}
	case <-time.After(time.Second):
		t.Fatal("IngestMessages deadlocked while publisher re-entered the ingestor")
	}
	if !reentered {
		t.Fatal("publisher did not exercise reentrant ingestor path")
	}
	if innerEvents != 1 {
		t.Fatalf("inner events len = %d, want 1", innerEvents)
	}
	if publisher.count() != 2 {
		t.Fatalf("publish count = %d, want 2", publisher.count())
	}
}

func TestIngestReservationsPublishesAfterReleasingMutex(t *testing.T) {
	t.Parallel()
	publisher := &reentrantPublisher{}
	ingestor := newTestIngestorWithPublisher(t, &fakeAgentMail{}, publisher)
	reentered := false
	innerEvents := -1
	publisher.onPublish = func() {
		if reentered {
			return
		}
		reentered = true
		inner := ingestor.IngestReservations([]agentmail.Reservation{{
			ID:          202,
			Agent:       "TealPond",
			PathPattern: "apps/daemon/internal/api/**",
			Exclusive:   true,
		}})
		innerEvents = len(inner)
	}

	done := make(chan []api.Event, 1)
	go func() {
		done <- ingestor.IngestReservations([]agentmail.Reservation{{
			ID:          201,
			Agent:       "BlueLake",
			PathPattern: "apps/daemon/internal/activity/**",
			Exclusive:   true,
		}})
	}()

	select {
	case events := <-done:
		if len(events) != 1 {
			t.Fatalf("outer events len = %d, want 1", len(events))
		}
	case <-time.After(time.Second):
		t.Fatal("IngestReservations deadlocked while publisher re-entered the ingestor")
	}
	if !reentered {
		t.Fatal("publisher did not exercise reentrant reservation path")
	}
	if innerEvents == 0 {
		t.Fatal("inner reservation ingest emitted no events")
	}
	if publisher.count() < 2 {
		t.Fatalf("publish count = %d, want at least 2", publisher.count())
	}
}

func TestReservationOverlapDetection(t *testing.T) {
	t.Parallel()
	conflicts := DetectReservationConflicts([]agentmail.Reservation{
		{ID: 1, Agent: "A", PathPattern: "apps/daemon/internal/api/**", Exclusive: true},
		{ID: 2, Agent: "B", PathPattern: "apps/daemon/internal/api/router.go", Exclusive: true},
		{ID: 3, Agent: "C", PathPattern: "apps/desktop/**", Exclusive: true},
		{ID: 4, Agent: "D", PathPattern: "apps/daemon/internal/api/**", Exclusive: false},
	})
	if len(conflicts) != 3 {
		t.Fatalf("conflicts = %+v, want 3", conflicts)
	}
}

func newTestIngestor(t *testing.T, client *fakeAgentMail) *Ingestor {
	t.Helper()
	hub := api.NewEventHub(api.EventHubConfig{
		Now: func() time.Time { return time.Date(2026, 5, 4, 0, 0, 0, 0, time.UTC) },
	})
	return newTestIngestorWithPublisher(t, client, hub)
}

func newTestIngestorWithPublisher(t *testing.T, client *fakeAgentMail, publisher Publisher) *Ingestor {
	t.Helper()
	ingestor, err := NewAgentMailIngestor(Config{
		ProjectID:     "proj_01",
		ProjectKey:    "/repo",
		AgentName:     "WhiteStream",
		Mail:          client,
		Events:        publisher,
		IncludeBodies: true,
		Now: func() time.Time {
			return time.Date(2026, 5, 4, 0, 0, 0, 0, time.UTC)
		},
	})
	if err != nil {
		t.Fatalf("NewAgentMailIngestor: %v", err)
	}
	return ingestor
}

type reentrantPublisher struct {
	onPublish func()
	inputs    []api.PublishInput
}

func (p *reentrantPublisher) Publish(input api.PublishInput) api.Event {
	p.inputs = append(p.inputs, input)
	if p.onPublish != nil {
		p.onPublish()
	}
	return api.Event{
		Channel:       input.Channel,
		Type:          input.Type,
		Actor:         input.Actor,
		CorrelationID: input.CorrelationID,
		Data:          input.Data,
	}
}

func (p *reentrantPublisher) count() int {
	return len(p.inputs)
}

type fakeAgentMail struct {
	inbox         []agentmail.Message
	reservations  []agentmail.Reservation
	forceResponse agentmail.ForceReleaseReservationResponse
	forceErr      error

	fetch agentmail.FetchInboxRequest
	list  agentmail.ListReservationsRequest
	force agentmail.ForceReleaseReservationRequest
}

func (f *fakeAgentMail) FetchInbox(_ context.Context, req agentmail.FetchInboxRequest) ([]agentmail.Message, error) {
	f.fetch = req
	return append([]agentmail.Message(nil), f.inbox...), nil
}

func (f *fakeAgentMail) ListReservations(_ context.Context, req agentmail.ListReservationsRequest) ([]agentmail.Reservation, error) {
	f.list = req
	return append([]agentmail.Reservation(nil), f.reservations...), nil
}

func (f *fakeAgentMail) ForceReleaseReservation(_ context.Context, req agentmail.ForceReleaseReservationRequest) (agentmail.ForceReleaseReservationResponse, error) {
	f.force = req
	if f.forceErr != nil {
		return agentmail.ForceReleaseReservationResponse{}, f.forceErr
	}
	return f.forceResponse, nil
}

func countType(events []api.Event, eventType string) int {
	var count int
	for _, event := range events {
		if event.Type == eventType {
			count++
		}
	}
	return count
}
