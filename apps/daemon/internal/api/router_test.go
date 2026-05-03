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
