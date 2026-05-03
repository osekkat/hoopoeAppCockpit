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
	schemas "github.com/hoopoe-cockpit/hoopoe/packages/schemas/go"
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

	var body schemas.JobListResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode jobs response: %v", err)
	}
	if len(body.Items) != 1 {
		t.Fatalf("jobs length = %d, want 1", len(body.Items))
	}
	if body.Items[0].Id != "job_test" || body.Items[0].Type != "daemon.smoke" || body.Items[0].Status != schemas.JobStatusQueued {
		t.Fatalf("unexpected job payload: %+v", body.Items[0])
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

	var body schemas.VersionResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode version response: %v", err)
	}
	if body.SchemaVersion != schemaVersion || body.Daemon.Version != "1.2.3" || body.Daemon.Commit == nil || *body.Daemon.Commit != "abc123" {
		t.Fatalf("unexpected version payload: %+v", body)
	}
}

func TestSeedContractGeneratedSchemaRoundTrips(t *testing.T) {
	now := time.Date(2026, 5, 4, 0, 0, 0, 0, time.UTC)
	router := NewRouter(Config{
		Now: func() time.Time { return now },
	})

	for _, tc := range []struct {
		name   string
		path   string
		target any
	}{
		{name: "health", path: "/v1/health", target: &schemas.HealthResponse{}},
		{name: "version", path: "/v1/version", target: &schemas.VersionResponse{}},
		{name: "projects", path: "/v1/projects", target: &schemas.ProjectListResponse{}},
		{name: "readiness", path: "/v1/projects/proj_01/readiness", target: &schemas.ProjectReadiness{}},
		{name: "plans", path: "/v1/projects/proj_01/plans", target: &schemas.PlanListResponse{}},
		{name: "beads", path: "/v1/projects/proj_01/beads", target: &schemas.BeadListResponse{}},
		{name: "approvals", path: "/v1/projects/proj_01/approvals", target: &schemas.ApprovalListResponse{}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, tc.path, nil)
			rec := httptest.NewRecorder()

			router.ServeHTTP(rec, req)

			if rec.Code != http.StatusOK {
				t.Fatalf("status = %d, want %d; body=%s", rec.Code, http.StatusOK, rec.Body.String())
			}
			if err := json.Unmarshal(rec.Body.Bytes(), tc.target); err != nil {
				t.Fatalf("decode generated schema target: %v", err)
			}
		})
	}
}

func TestSeedWriteEndpointAcceptsIdempotencyKeyAndReturnsGeneratedProblem(t *testing.T) {
	router := NewRouter(Config{})
	req := httptest.NewRequest(http.MethodPost, "/v1/bootstrap/acfs/start", nil)
	req.Header.Set("Idempotency-Key", "01HXTESTIDEMPOTENCY")
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotImplemented {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusNotImplemented)
	}
	if got := rec.Header().Get("Content-Type"); !strings.HasPrefix(got, "application/problem+json") {
		t.Fatalf("content-type = %q, want problem+json", got)
	}
	var body schemas.Problem
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode generated problem: %v", err)
	}
	if body.Code != "bootstrap.acfs.start.unavailable" || body.Status != http.StatusNotImplemented {
		t.Fatalf("unexpected problem: %+v", body)
	}
}

func TestSeedContractCoversPlanSectionRoutes(t *testing.T) {
	router := NewRouter(Config{})
	approvalDecision := `{"decisionActor":{"kind":"user"}}`
	actionPlan := `{"schemaVersion":1,"jobId":"job_seed","runId":"run_seed","summary":"seed","riskClass":"low","actions":[]}`
	for _, tc := range []struct {
		method string
		path   string
		body   string
	}{
		{http.MethodGet, "/v1/health", ""},
		{http.MethodGet, "/v1/version", ""},
		{http.MethodGet, "/v1/system/specs", ""},
		{http.MethodGet, "/v1/system/processes", ""},
		{http.MethodPost, "/v1/auth/bootstrap/bearer", ""},
		{http.MethodPost, "/v1/auth/ws-token", ""},
		{http.MethodPost, "/v1/auth/session/revoke", ""},
		{http.MethodPost, "/v1/auth/rotate-secret", ""},
		{http.MethodGet, "/v1/events/replay?channel=_system&sinceSequence=0", ""},
		{http.MethodGet, "/v1/events/sse?channels=_system", ""},
		{http.MethodGet, "/v1/events/ws-token", ""},
		{http.MethodGet, "/v1/events/ws", ""},
		{http.MethodGet, "/v1/jobs", ""},
		{http.MethodPost, "/v1/jobs/job_seed/cancel", ""},
		{http.MethodGet, "/v1/jobs/job_seed/log?offset=0", ""},
		{http.MethodGet, "/v1/jobs/job_seed/artifacts", ""},
		{http.MethodPost, "/v1/bootstrap/preflight", ""},
		{http.MethodPost, "/v1/bootstrap/acfs/start", ""},
		{http.MethodPost, "/v1/bootstrap/acfs/resume", ""},
		{http.MethodPost, "/v1/bootstrap/daemon/upgrade", ""},
		{http.MethodGet, "/v1/projects", ""},
		{http.MethodPost, "/v1/projects", ""},
		{http.MethodGet, "/v1/projects/proj_01", ""},
		{http.MethodPost, "/v1/projects/proj_01/activate", ""},
		{http.MethodGet, "/v1/projects/proj_01/readiness", ""},
		{http.MethodGet, "/v1/projects/proj_01/git/status", ""},
		{http.MethodGet, "/v1/projects/proj_01/git/staged-diff", ""},
		{http.MethodGet, "/v1/projects/proj_01/git/unstaged-diff", ""},
		{http.MethodGet, "/v1/projects/proj_01/git/unpushed-commits", ""},
		{http.MethodPost, "/v1/projects/proj_01/git/push", ""},
		{http.MethodGet, "/v1/projects/proj_01/plans", ""},
		{http.MethodPost, "/v1/projects/proj_01/plans", ""},
		{http.MethodPost, "/v1/projects/proj_01/plans/plan_01/rounds", ""},
		{http.MethodPost, "/v1/projects/proj_01/plans/plan_01/lock", ""},
		{http.MethodGet, "/v1/projects/proj_01/plans/plan_01/artifacts", ""},
		{http.MethodGet, "/v1/projects/proj_01/beads", ""},
		{http.MethodGet, "/v1/projects/proj_01/beads/graph", ""},
		{http.MethodGet, "/v1/projects/proj_01/beads/ready", ""},
		{http.MethodGet, "/v1/projects/proj_01/beads/hp-1ry", ""},
		{http.MethodPatch, "/v1/projects/proj_01/beads/hp-1ry", ""},
		{http.MethodPost, "/v1/projects/proj_01/beads/conversion-runs", ""},
		{http.MethodPost, "/v1/projects/proj_01/beads/polish-runs", ""},
		{http.MethodPost, "/v1/projects/proj_01/swarms", ""},
		{http.MethodGet, "/v1/projects/proj_01/swarms/sw_01", ""},
		{http.MethodPost, "/v1/projects/proj_01/swarms/sw_01/broadcast", ""},
		{http.MethodPost, "/v1/projects/proj_01/agents/ag_01/send", ""},
		{http.MethodPost, "/v1/projects/proj_01/agents/ag_01/interrupt", ""},
		{http.MethodPost, "/v1/projects/proj_01/agents/ag_01/stop", ""},
		{http.MethodGet, "/v1/projects/proj_01/mail/messages", ""},
		{http.MethodPost, "/v1/projects/proj_01/mail/messages", ""},
		{http.MethodGet, "/v1/projects/proj_01/mail/threads/thread_01", ""},
		{http.MethodGet, "/v1/projects/proj_01/reservations", ""},
		{http.MethodPost, "/v1/projects/proj_01/reservations/force-release", ""},
		{http.MethodGet, "/v1/projects/proj_01/health/summary", ""},
		{http.MethodGet, "/v1/projects/proj_01/health/files", ""},
		{http.MethodPost, "/v1/projects/proj_01/health/snapshots", ""},
		{http.MethodPost, "/v1/projects/proj_01/reviews", ""},
		{http.MethodGet, "/v1/projects/proj_01/reviews/rev_01", ""},
		{http.MethodGet, "/v1/projects/proj_01/findings", ""},
		{http.MethodPatch, "/v1/projects/proj_01/findings/finding_01", ""},
		{http.MethodGet, "/v1/projects/proj_01/tending/jobs", ""},
		{http.MethodPost, "/v1/projects/proj_01/tending/jobs/job_01/run", ""},
		{http.MethodPatch, "/v1/projects/proj_01/tending/jobs/job_01", ""},
		{http.MethodPost, "/v1/projects/proj_01/tending/actionplans", actionPlan},
		{http.MethodGet, "/v1/projects/proj_01/approvals", ""},
		{http.MethodGet, "/v1/projects/proj_01/approvals/ap_01", ""},
		{http.MethodPost, "/v1/projects/proj_01/approvals/ap_01/approve", approvalDecision},
		{http.MethodPost, "/v1/projects/proj_01/approvals/ap_01/deny", approvalDecision},
		{http.MethodPost, "/v1/projects/proj_01/approvals/ap_01/extend", ""},
	} {
		t.Run(tc.method+" "+tc.path, func(t *testing.T) {
			ctx := context.Background()
			if strings.Contains(tc.path, "/v1/events/sse") {
				ctx = canceledContext()
			}
			var body *strings.Reader
			if tc.body == "" {
				body = strings.NewReader("")
			} else {
				body = strings.NewReader(tc.body)
			}
			req := httptest.NewRequest(tc.method, tc.path, body).WithContext(ctx)
			req.Header.Set("Idempotency-Key", "01HXTESTIDEMPOTENCY")
			if tc.body != "" {
				req.Header.Set("Content-Type", "application/json")
			}
			rec := httptest.NewRecorder()

			router.ServeHTTP(rec, req)

			contentType := rec.Header().Get("Content-Type")
			if rec.Code == http.StatusMethodNotAllowed || (rec.Code == http.StatusNotFound && !strings.HasPrefix(contentType, "application/problem+json")) {
				t.Fatalf("route was not handled: status=%d body=%s", rec.Code, rec.Body.String())
			}
			if rec.Code >= 400 && !strings.HasPrefix(contentType, "application/problem+json") && !strings.Contains(tc.path, "/v1/events/ws") {
				t.Fatalf("error content-type = %q, want problem+json; status=%d body=%s", contentType, rec.Code, rec.Body.String())
			}
		})
	}
}

func canceledContext() context.Context {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	return ctx
}

func TestJobCancelRoundTripUsesGeneratedRequestAndResponse(t *testing.T) {
	reader := &staticJobController{
		staticJobReader: staticJobReader{
			jobs: []jobstore.Job{{
				ID:            "job_test",
				Kind:          "daemon.smoke",
				SchemaVersion: jobstore.SchemaVersion,
				Status:        jobstore.StatusRunning,
				CreatedAt:     time.Unix(20, 0).UTC(),
				UpdatedAt:     time.Unix(20, 0).UTC(),
			}},
		},
	}
	router := NewRouter(Config{Jobs: reader})
	req := httptest.NewRequest(http.MethodPost, "/v1/jobs/job_test/cancel", strings.NewReader(`{"reason":"test cancel","graceSeconds":1}`))
	req.Header.Set("Idempotency-Key", "01HXTESTIDEMPOTENCY")
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want %d; body=%s", rec.Code, http.StatusAccepted, rec.Body.String())
	}
	var body schemas.Job
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode generated job: %v", err)
	}
	if body.Id != "job_test" || body.Status != schemas.JobStatusCanceling {
		t.Fatalf("unexpected cancel response: %+v", body)
	}
	if len(reader.cancelRequests) != 1 || reader.cancelRequests[0].Reason != "test cancel" {
		t.Fatalf("cancel requests = %+v", reader.cancelRequests)
	}
}

func TestJobLogAndArtifactsRoutesUseRegistryReader(t *testing.T) {
	stamp := time.Unix(20, 0).UTC()
	router := NewRouter(Config{
		Jobs: staticJobReader{
			logs: map[string][]byte{
				"job_test": []byte("hello log"),
			},
			artifacts: map[string][]jobstore.Artifact{
				"job_test": {{
					ID:        "artifact_1",
					Kind:      "log",
					URI:       "fixture://job/log",
					CreatedAt: stamp,
				}},
			},
		},
	})

	for _, path := range []string{"/v1/jobs/job_test/log?offset=0", "/v1/jobs/job_test/artifacts"} {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		rec := httptest.NewRecorder()

		router.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("%s status = %d, want %d; body=%s", path, rec.Code, http.StatusOK, rec.Body.String())
		}
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
	var body schemas.EventReplayResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode replay response: %v", err)
	}
	if body.LatestSequence != int(missedTwo.Sequence) {
		t.Fatalf("latestSequence = %d, want %d", body.LatestSequence, missedTwo.Sequence)
	}
	if len(body.Events) != 2 {
		t.Fatalf("replayed events length = %d, want 2: %#v", len(body.Events), body.Events)
	}
	if body.Events[0].EventId != missedOne.EventID || body.Events[1].EventId != missedTwo.EventID {
		t.Fatalf("replayed event IDs = [%s %s], want [%s %s]", body.Events[0].EventId, body.Events[1].EventId, missedOne.EventID, missedTwo.EventID)
	}
	_ = seen
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
	jobs      []jobstore.Job
	logs      map[string][]byte
	artifacts map[string][]jobstore.Artifact
}

func (r staticJobReader) List(context.Context, jobstore.ListFilter) ([]jobstore.Job, error) {
	return append([]jobstore.Job(nil), r.jobs...), nil
}

func (r staticJobReader) Get(_ context.Context, id string) (jobstore.Job, error) {
	for _, job := range r.jobs {
		if job.ID == id {
			return job, nil
		}
	}
	return jobstore.Job{}, jobstore.ErrNotFound
}

func (r staticJobReader) ReadLog(_ context.Context, id string, offset int64, limit int64) (jobstore.LogChunk, error) {
	body, ok := r.logs[id]
	if !ok {
		return jobstore.LogChunk{}, jobstore.ErrNotFound
	}
	if offset < 0 {
		return jobstore.LogChunk{}, jobstore.ErrInvalidRequest
	}
	if int(offset) > len(body) {
		offset = int64(len(body))
	}
	if limit <= 0 || int(offset+limit) > len(body) {
		limit = int64(len(body)) - offset
	}
	next := offset + limit
	return jobstore.LogChunk{
		JobID:      id,
		Offset:     offset,
		NextOffset: next,
		Data:       append([]byte(nil), body[int(offset):int(next)]...),
		EOF:        int(next) >= len(body),
	}, nil
}

func (r staticJobReader) ListArtifacts(_ context.Context, id string) ([]jobstore.Artifact, error) {
	artifacts, ok := r.artifacts[id]
	if !ok {
		return nil, jobstore.ErrNotFound
	}
	return append([]jobstore.Artifact(nil), artifacts...), nil
}

type staticJobController struct {
	staticJobReader
	cancelRequests []jobstore.CancelRequest
}

func (r *staticJobController) Cancel(_ context.Context, request jobstore.CancelRequest) (jobstore.Job, error) {
	r.cancelRequests = append(r.cancelRequests, request)
	for _, job := range r.jobs {
		if job.ID == request.JobID {
			job.Status = jobstore.StatusCanceling
			return job, nil
		}
	}
	return jobstore.Job{}, jobstore.ErrNotFound
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
