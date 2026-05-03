package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	jobstore "github.com/hoopoe-cockpit/hoopoe/apps/daemon/internal/jobs"
	"nhooyr.io/websocket"
)

func TestHealthRoundTrip(t *testing.T) {
	router := NewRouter(Config{
		Now: func() time.Time {
			return time.Unix(10, 0).UTC()
		},
	})

	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}

	var body struct {
		OK            bool   `json:"ok"`
		SchemaVersion int    `json:"schemaVersion"`
		Time          string `json:"time"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode health response: %v", err)
	}
	if !body.OK {
		t.Fatal("health response ok=false")
	}
	if body.SchemaVersion != schemaVersion {
		t.Fatalf("schemaVersion = %d, want %d", body.SchemaVersion, schemaVersion)
	}
}

func TestJobsRoundTripUsesRegistryReader(t *testing.T) {
	created := time.Unix(20, 0).UTC()
	router := NewRouter(Config{
		Jobs: staticJobReader{
			jobs: []jobstore.Job{{
				ID:            "job_test",
				Kind:          "daemon.smoke",
				SchemaVersion: jobstore.SchemaVersion,
				Status:        jobstore.StatusQueued,
				CreatedAt:     created,
				UpdatedAt:     created,
			}},
		},
	})

	req := httptest.NewRequest(http.MethodGet, "/v1/jobs", nil)
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}

	var body struct {
		SchemaVersion int `json:"schemaVersion"`
		Jobs          []struct {
			ID     string `json:"id"`
			Kind   string `json:"kind"`
			Status string `json:"status"`
		} `json:"jobs"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode jobs response: %v", err)
	}
	if body.SchemaVersion != schemaVersion {
		t.Fatalf("schemaVersion = %d, want %d", body.SchemaVersion, schemaVersion)
	}
	if len(body.Jobs) != 1 {
		t.Fatalf("jobs length = %d, want 1", len(body.Jobs))
	}
	if body.Jobs[0].ID != "job_test" || body.Jobs[0].Kind != "daemon.smoke" || body.Jobs[0].Status != string(jobstore.StatusQueued) {
		t.Fatalf("unexpected job payload: %+v", body.Jobs[0])
	}
}

func TestVersionRoundTrip(t *testing.T) {
	router := NewRouter(Config{
		Build: BuildInfo{
			Version:    "1.2.3",
			Commit:     "abc123",
			BuildDate:  "2026-05-03T00:00:00Z",
			APIVersion: "v1",
		},
	})

	req := httptest.NewRequest(http.MethodGet, "/v1/version", nil)
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}

	var body BuildInfo
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode version response: %v", err)
	}
	if body.Version != "1.2.3" || body.Commit != "abc123" || body.BuildDate != "2026-05-03T00:00:00Z" || body.APIVersion != "v1" {
		t.Fatalf("unexpected version payload: %+v", body)
	}
}

func TestWebSocketAcceptsValidToken(t *testing.T) {
	router := NewRouter(Config{
		WSValidator: StaticWebSocketTokenValidator{Token: "secret"},
	})
	server := httptest.NewServer(router)
	defer server.Close()

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http") + "/v1/events/ws?wsToken=secret"
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	conn, _, err := websocket.Dial(ctx, wsURL, nil)
	if err != nil {
		t.Fatalf("websocket dial with valid token: %v", err)
	}
	defer conn.Close(websocket.StatusNormalClosure, "test complete")
}

func TestSlowConsumerReceivesLagEvent(t *testing.T) {
	hub := NewEventHub(EventHubConfig{SubscriberCapacity: 1})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	sub := hub.Subscribe(ctx, []string{"project:test"})
	defer sub.Close()

	hub.Publish(PublishInput{Channel: "project:test", Type: "first", Data: map[string]any{"n": 1}})
	hub.Publish(PublishInput{Channel: "project:test", Type: "second", Data: map[string]any{"n": 2}})

	select {
	case ev := <-sub.Events():
		if ev.Type != "_lag" {
			t.Fatalf("event type = %q, want _lag", ev.Type)
		}
		if ev.Channel != "project:test" {
			t.Fatalf("event channel = %q, want project:test", ev.Channel)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for lag event")
	}
}

func TestEventSequencesArePerChannel(t *testing.T) {
	hub := NewEventHub(EventHubConfig{})

	firstProject := hub.Publish(PublishInput{Channel: "project:test", Type: "project.changed", Data: map[string]any{"n": 1}})
	firstActivity := hub.Publish(PublishInput{Channel: "activity:test", Type: "activity.appended", Data: map[string]any{"n": 1}})
	secondProject := hub.Publish(PublishInput{Channel: "project:test", Type: "project.changed", Data: map[string]any{"n": 2}})

	if firstProject.Sequence != 1 {
		t.Fatalf("first project sequence = %d, want 1", firstProject.Sequence)
	}
	if firstActivity.Sequence != 1 {
		t.Fatalf("first activity sequence = %d, want 1", firstActivity.Sequence)
	}
	if secondProject.Sequence != 2 {
		t.Fatalf("second project sequence = %d, want 2", secondProject.Sequence)
	}
}

func TestReplayEndpointReturnsMissedEventsAfterDisconnect(t *testing.T) {
	now := time.Unix(100, 0).UTC()
	hub := NewEventHub(EventHubConfig{
		ReplayCapacity: 16,
		Now: func() time.Time {
			return now
		},
	})
	seen := hub.Publish(PublishInput{Channel: "project:test", Type: "project.ready", Data: map[string]any{"n": 1}})
	now = now.Add(10 * time.Minute)
	missedOne := hub.Publish(PublishInput{Channel: "project:test", Type: "bead.changed", Data: map[string]any{"n": 2}})
	missedTwo := hub.Publish(PublishInput{Channel: "project:test", Type: "agent.changed", Data: map[string]any{"n": 3}})
	hub.Publish(PublishInput{Channel: "activity:test", Type: "activity.appended", Data: map[string]any{"n": 4}})

	router := NewRouter(Config{Events: hub})
	req := httptest.NewRequest(http.MethodGet, "/v1/events/replay?channel=project:test&sinceSequence=1", nil)
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	var body ReplayResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode replay response: %v", err)
	}
	if body.Gap {
		t.Fatal("replay response reported an unexpected gap")
	}
	if body.SinceSequence != seen.Sequence {
		t.Fatalf("sinceSequence = %d, want %d", body.SinceSequence, seen.Sequence)
	}
	if len(body.Events) != 2 {
		t.Fatalf("replayed events length = %d, want 2: %#v", len(body.Events), body.Events)
	}
	if body.Events[0].EventID != missedOne.EventID || body.Events[1].EventID != missedTwo.EventID {
		t.Fatalf("replayed event IDs = [%s %s], want [%s %s]", body.Events[0].EventID, body.Events[1].EventID, missedOne.EventID, missedTwo.EventID)
	}
}

func TestWebSocketSubscribeEnvelopeSnapshotsThenLiveDeltas(t *testing.T) {
	hub := NewEventHub(EventHubConfig{})
	replayed := hub.Publish(PublishInput{Channel: "project:test", Type: "project.ready", Data: map[string]any{"n": 1}})
	server := httptest.NewServer(NewRouter(Config{Events: hub}))
	defer server.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	conn := dialEventWebSocket(t, ctx, server)
	defer conn.Close(websocket.StatusNormalClosure, "test complete")

	currentSchemaVersion := schemaVersion
	if err := writeWebSocketJSON(ctx, conn, subscribeRequest{
		Op:            "subscribe",
		Channels:      []string{"project:test"},
		Cursors:       map[string]uint64{"project:test": 0},
		SchemaVersion: &currentSchemaVersion,
	}); err != nil {
		t.Fatalf("write subscribe envelope: %v", err)
	}

	var snapshot struct {
		Op       string   `json:"op"`
		Snapshot Snapshot `json:"snapshot"`
	}
	readWebSocketJSON(t, ctx, conn, &snapshot)
	if snapshot.Op != "snapshot" {
		t.Fatalf("websocket op = %q, want snapshot", snapshot.Op)
	}
	projectSnapshot := snapshot.Snapshot.Channels["project:test"]
	if len(projectSnapshot.Replayed) != 1 || projectSnapshot.Replayed[0].EventID != replayed.EventID {
		t.Fatalf("snapshot replayed = %#v, want event %s", projectSnapshot.Replayed, replayed.EventID)
	}

	live := hub.Publish(PublishInput{Channel: "project:test", Type: "bead.changed", Data: map[string]any{"n": 2}})
	var delta Event
	readWebSocketJSON(t, ctx, conn, &delta)
	if delta.EventID != live.EventID || delta.Sequence != 2 || delta.Type != "bead.changed" {
		t.Fatalf("live delta = %+v, want %+v", delta, live)
	}
}

func TestWebSocketSubscribeEmitsGapForStaleCursor(t *testing.T) {
	hub := NewEventHub(EventHubConfig{ReplayCapacity: 2})
	hub.Publish(PublishInput{Channel: "project:test", Type: "first", Data: map[string]any{"n": 1}})
	hub.Publish(PublishInput{Channel: "project:test", Type: "second", Data: map[string]any{"n": 2}})
	hub.Publish(PublishInput{Channel: "project:test", Type: "third", Data: map[string]any{"n": 3}})
	hub.Publish(PublishInput{Channel: "project:test", Type: "fourth", Data: map[string]any{"n": 4}})
	server := httptest.NewServer(NewRouter(Config{Events: hub}))
	defer server.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	conn := dialEventWebSocket(t, ctx, server)
	defer conn.Close(websocket.StatusNormalClosure, "test complete")

	currentSchemaVersion := schemaVersion
	if err := writeWebSocketJSON(ctx, conn, subscribeRequest{
		Op:            "subscribe",
		Channels:      []string{"project:test"},
		Cursors:       map[string]uint64{"project:test": 1},
		SchemaVersion: &currentSchemaVersion,
	}); err != nil {
		t.Fatalf("write subscribe envelope: %v", err)
	}

	var snapshot struct {
		Op       string   `json:"op"`
		Snapshot Snapshot `json:"snapshot"`
	}
	readWebSocketJSON(t, ctx, conn, &snapshot)
	if !snapshot.Snapshot.Channels["project:test"].Gap {
		t.Fatalf("snapshot gap = false, want true: %+v", snapshot.Snapshot.Channels["project:test"])
	}

	var gap Event
	readWebSocketJSON(t, ctx, conn, &gap)
	if gap.Type != "_gap" || gap.Channel != "project:test" {
		t.Fatalf("gap event = %+v, want project:test _gap", gap)
	}
	data, ok := gap.Data.(map[string]any)
	if !ok {
		t.Fatalf("gap data type = %T, want map[string]any", gap.Data)
	}
	if data["repair"] != "replayEvents" || data["from"] != float64(2) || data["to"] != float64(2) {
		t.Fatalf("gap data = %#v, want from=2 to=2 repair=replayEvents", data)
	}
}

func TestWebSocketSubscribeSchemaMismatchEmitsCompatibilityWarning(t *testing.T) {
	hub := NewEventHub(EventHubConfig{})
	server := httptest.NewServer(NewRouter(Config{Events: hub}))
	defer server.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	conn := dialEventWebSocket(t, ctx, server)
	defer conn.Close(websocket.StatusNormalClosure, "test complete")

	olderSchemaVersion := schemaVersion - 1
	if err := writeWebSocketJSON(ctx, conn, subscribeRequest{
		Op:            "subscribe",
		Channels:      []string{"project:test"},
		Cursors:       map[string]uint64{"project:test": 0},
		SchemaVersion: &olderSchemaVersion,
	}); err != nil {
		t.Fatalf("write subscribe envelope: %v", err)
	}

	var snapshot struct {
		Op string `json:"op"`
	}
	readWebSocketJSON(t, ctx, conn, &snapshot)
	if snapshot.Op != "snapshot" {
		t.Fatalf("websocket op = %q, want snapshot", snapshot.Op)
	}

	var warning Event
	readWebSocketJSON(t, ctx, conn, &warning)
	if warning.Channel != "_system" || warning.Type != "_compatibility_warning" {
		t.Fatalf("warning event = %+v, want _system _compatibility_warning", warning)
	}
	data, ok := warning.Data.(map[string]any)
	if !ok {
		t.Fatalf("warning data type = %T, want map[string]any", warning.Data)
	}
	if data["clientSchemaVersion"] != float64(olderSchemaVersion) || data["serverSchemaVersion"] != float64(schemaVersion) {
		t.Fatalf("warning data = %#v, want client/server schema versions", data)
	}
}

type staticJobReader struct {
	jobs []jobstore.Job
}

func (r staticJobReader) List(context.Context, jobstore.ListFilter) ([]jobstore.Job, error) {
	return append([]jobstore.Job(nil), r.jobs...), nil
}

func (r staticJobReader) Get(context.Context, string) (jobstore.Job, error) {
	return jobstore.Job{}, jobstore.ErrNotFound
}

func (r staticJobReader) ReadLog(context.Context, string, int64, int64) (jobstore.LogChunk, error) {
	return jobstore.LogChunk{}, jobstore.ErrNotFound
}

func (r staticJobReader) ListArtifacts(context.Context, string) ([]jobstore.Artifact, error) {
	return nil, jobstore.ErrNotFound
}

func TestSafeSSEEventName(t *testing.T) {
	for _, tc := range []struct {
		name string
		in   string
		want string
	}{
		{name: "empty", in: "", want: "message"},
		{name: "known", in: "bead.changed", want: "bead.changed"},
		{name: "underscore", in: "_lag", want: "_lag"},
		{name: "newline", in: "bad\nevent", want: "message"},
		{name: "colon", in: "bad:event", want: "message"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := safeSSEEventName(tc.in); got != tc.want {
				t.Fatalf("safeSSEEventName(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

func TestParseChannelsDropsUnsafeNames(t *testing.T) {
	got := parseChannels("_system,project:abc,bad\nchannel,bad/channel")
	want := []string{"_system", "project:abc"}
	if len(got) != len(want) {
		t.Fatalf("parseChannels length = %d, want %d: %#v", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("parseChannels[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

func dialEventWebSocket(t *testing.T, ctx context.Context, server *httptest.Server) *websocket.Conn {
	t.Helper()
	wsURL := "ws" + strings.TrimPrefix(server.URL, "http") + "/v1/events/ws"
	conn, _, err := websocket.Dial(ctx, wsURL, nil)
	if err != nil {
		t.Fatalf("websocket dial: %v", err)
	}
	return conn
}

func readWebSocketJSON(t *testing.T, ctx context.Context, conn *websocket.Conn, target any) {
	t.Helper()
	messageType, body, err := conn.Read(ctx)
	if err != nil {
		t.Fatalf("read websocket message: %v", err)
	}
	if messageType != websocket.MessageText {
		t.Fatalf("websocket message type = %v, want text", messageType)
	}
	if err := json.Unmarshal(body, target); err != nil {
		t.Fatalf("decode websocket message %s: %v", string(body), err)
	}
}
