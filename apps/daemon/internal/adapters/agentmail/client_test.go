package agentmail

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/hoopoe-cockpit/hoopoe/apps/daemon/internal/capabilities"
)

func TestSendBeadMessageUsesMCPHTTPAndThreadConvention(t *testing.T) {
	t.Parallel()
	rt := &recordingRoundTripper{
		response: mcpResultResponse(map[string]any{
			"deliveries": []map[string]any{
				{
					"project": "/repo",
					"payload": map[string]any{
						"id":         41,
						"thread_id":  "br-hp-ay4",
						"subject":    "[hp-ay4] hello",
						"body_md":    "body",
						"importance": "normal",
						"from":       "WhiteStream",
						"to":         []string{"BlueLake"},
					},
				},
			},
			"count": 1,
		}),
	}
	client := New("http://127.0.0.1:8765")
	client.Token = "test-token"
	client.HTTPClient = &http.Client{Transport: rt}

	out, err := client.SendBeadMessage(context.Background(), "hp-ay4", SendMessageRequest{
		ProjectKey: "/repo",
		SenderName: "WhiteStream",
		To:         []string{"BlueLake"},
		Subject:    "[hp-ay4] hello",
		BodyMD:     "body",
	})
	if err != nil {
		t.Fatalf("SendBeadMessage: %v", err)
	}
	if out.Count != 1 || out.Deliveries[0].Payload.ThreadID != "br-hp-ay4" {
		t.Fatalf("unexpected send response: %+v", out)
	}
	if rt.request.URL.Path != "/mcp" {
		t.Fatalf("path = %s, want /mcp", rt.request.URL.Path)
	}
	if got := rt.request.Header.Get("Authorization"); got != "Bearer test-token" {
		t.Fatalf("Authorization = %q", got)
	}
	var envelope mcpRequest
	if err := json.Unmarshal(rt.body, &envelope); err != nil {
		t.Fatalf("decode request: %v", err)
	}
	if envelope.Method != "tools/call" || envelope.Params.Name != "send_message" {
		t.Fatalf("unexpected MCP request: %+v", envelope)
	}
	if envelope.Params.Arguments["thread_id"] != "br-hp-ay4" {
		t.Fatalf("thread_id argument = %#v", envelope.Params.Arguments["thread_id"])
	}
}

func TestInboxThreadsAndReservationMethodsDecodeStructuredContent(t *testing.T) {
	t.Parallel()
	rt := &queueRoundTripper{responses: []*http.Response{
		mcpResultResponse([]map[string]any{{"id": 1, "subject": "urgent", "importance": "urgent", "from": "A"}}),
		mcpResultResponse([]map[string]any{{"id": 2, "subject": "thread hit", "thread_id": "br-hp-ay4", "from": "A"}}),
		mcpResultResponse(map[string]any{"thread_id": "br-hp-ay4", "summary": map[string]any{"key_points": []string{"done"}}}),
		jsonResponse(map[string]any{"reservations": []map[string]any{{"id": 7, "agent": "A", "path_pattern": "old/**", "exclusive": true, "expires_ts": "2026-05-04T01:00:00Z"}}}),
		mcpResultResponse(map[string]any{"granted": []map[string]any{{"id": 9, "path_pattern": "a/**", "exclusive": true, "reason": "hp-ay4", "expires_ts": "2026-05-04T01:00:00Z"}}, "conflicts": []any{}}),
		mcpResultResponse(map[string]any{"released": 1, "released_at": "2026-05-04T01:00:00Z"}),
	}}
	client := New("http://agent-mail.test")
	client.HTTPClient = &http.Client{Transport: rt}

	inbox, err := client.FetchInbox(context.Background(), FetchInboxRequest{ProjectKey: "/repo", AgentName: "WhiteStream", UrgentOnly: true})
	if err != nil {
		t.Fatalf("FetchInbox: %v", err)
	}
	if len(inbox) != 1 || inbox[0].Importance != "urgent" {
		t.Fatalf("inbox = %+v", inbox)
	}
	threads, err := client.ListThreads(context.Background(), ListThreadsRequest{ProjectKey: "/repo", Query: "hp-ay4"})
	if err != nil {
		t.Fatalf("ListThreads: %v", err)
	}
	if threads[0].ID != "br-hp-ay4" {
		t.Fatalf("threads = %+v", threads)
	}
	summary, err := client.SummarizeThread(context.Background(), ThreadSummaryRequest{ProjectKey: "/repo", ThreadID: "br-hp-ay4"})
	if err != nil {
		t.Fatalf("SummarizeThread: %v", err)
	}
	if summary.ThreadID != "br-hp-ay4" {
		t.Fatalf("summary = %+v", summary)
	}
	reservations, err := client.ListReservations(context.Background(), ListReservationsRequest{Project: "repo", ActiveOnly: true})
	if err != nil {
		t.Fatalf("ListReservations: %v", err)
	}
	if len(reservations) != 1 || reservations[0].ID != 7 {
		t.Fatalf("reservations = %+v", reservations)
	}
	reserved, err := client.ReservePaths(context.Background(), ReservePathsRequest{ProjectKey: "/repo", AgentName: "WhiteStream", Paths: []string{"a/**"}, Reason: "hp-ay4"})
	if err != nil {
		t.Fatalf("ReservePaths: %v", err)
	}
	if len(reserved.Granted) != 1 || reserved.Granted[0].ID != 9 {
		t.Fatalf("reserved = %+v", reserved)
	}
	released, err := client.ReleaseReservations(context.Background(), ReleaseReservationsRequest{ProjectKey: "/repo", AgentName: "WhiteStream", FileReservationIDs: []int{9}})
	if err != nil {
		t.Fatalf("ReleaseReservations: %v", err)
	}
	if released.Released != 1 {
		t.Fatalf("released = %+v", released)
	}

	gotTools := rt.toolNames()
	wantTools := []string{"fetch_inbox", "search_messages", "summarize_thread", "file_reservation_paths", "release_file_reservations"}
	if strings.Join(gotTools, ",") != strings.Join(wantTools, ",") {
		t.Fatalf("tool calls = %v, want %v", gotTools, wantTools)
	}
}

func TestForceReleaseRequiresNoteAuditsAndNotifiesPreviousHolder(t *testing.T) {
	t.Parallel()
	rt := &recordingRoundTripper{
		response: mcpResultResponse(map[string]any{
			"released":    1,
			"released_at": "2026-05-04T01:00:00Z",
			"reservation": map[string]any{
				"id":       42,
				"notified": true,
			},
		}),
	}
	audit := &recordingAudit{}
	client := New("http://agent-mail.test")
	client.HTTPClient = &http.Client{Transport: rt}
	client.Audit = audit
	client.Now = func() time.Time { return time.Date(2026, 5, 4, 1, 0, 0, 0, time.UTC) }

	if _, err := client.ForceReleaseReservation(context.Background(), ForceReleaseReservationRequest{
		ProjectKey:        "/repo",
		AgentName:         "WhiteStream",
		FileReservationID: 42,
	}); !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("empty note err = %v, want ErrInvalidRequest", err)
	}

	out, err := client.ForceReleaseReservation(context.Background(), ForceReleaseReservationRequest{
		ProjectKey:        "/repo",
		AgentName:         "WhiteStream",
		FileReservationID: 42,
		Note:              "stale reservation blocking hp-ay4",
	})
	if err != nil {
		t.Fatalf("ForceReleaseReservation: %v", err)
	}
	if out.Released != 1 {
		t.Fatalf("force-release response = %+v", out)
	}
	var envelope mcpRequest
	if err := json.Unmarshal(rt.body, &envelope); err != nil {
		t.Fatalf("decode request: %v", err)
	}
	if envelope.Params.Name != "force_release_file_reservation" {
		t.Fatalf("tool = %s", envelope.Params.Name)
	}
	if envelope.Params.Arguments["notify_previous"] != true {
		t.Fatalf("notify_previous = %#v", envelope.Params.Arguments["notify_previous"])
	}
	if len(audit.events) != 2 || audit.events[0].Result != "started" || audit.events[1].Result != "success" {
		t.Fatalf("audit events = %+v", audit.events)
	}
}

// TestProbeOnMalformedJSONGoldenFixtureDegradesAllCapabilities loads the
// committed Phase 0 golden artifact at
// packages/fixtures/golden-outputs/agent_mail/malformed-json.json and pins the
// adapter contract from plan.md §18.3: when the MCP transport returns a
// truncated/non-JSON envelope, the probe must wrap the parser error as
// ErrDecode (no panic) and mark every declared capability as Degraded with the
// underlying decode error preserved in the notes. The test also re-checks the
// fixture's own self-declared capability/state pairs so a future fixture
// edit that drifts from the contract fails this test rather than silently
// passing.
func TestProbeOnMalformedJSONGoldenFixtureDegradesAllCapabilities(t *testing.T) {
	t.Parallel()
	fixture := loadAgentMailGoldenFixture(t, "malformed-json.json")

	if fixture.Meta.State != "malformed-json" {
		t.Fatalf("fixture state = %q, want malformed-json", fixture.Meta.State)
	}
	parseCap, ok := fixture.Capabilities["agent_mail._parse"]
	if !ok || parseCap.Status != "degraded" {
		t.Fatalf("fixture must declare agent_mail._parse=degraded, got %+v", fixture.Capabilities)
	}

	rt := &queueRoundTripper{responses: []*http.Response{
		{
			StatusCode: http.StatusOK,
			Header:     make(http.Header),
			Body:       io.NopCloser(strings.NewReader(fixture.StdoutText)),
		},
	}}
	client := New("http://127.0.0.1:8765")
	client.Token = "test-token"
	client.HTTPClient = &http.Client{Transport: rt}

	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("Probe panicked on malformed JSON fixture: %v", r)
		}
	}()
	report := Probe(
		context.Background(),
		client,
		"/repo",
		"RoseCastle",
		func() time.Time { return time.Date(2026, 5, 7, 0, 0, 0, 0, time.UTC) },
	)
	if report == nil {
		t.Fatalf("Probe returned nil report")
	}
	if err := report.Validate(); err != nil {
		t.Fatalf("report invalid: %v", err)
	}
	if report.Tool != capabilities.ToolAgentMail {
		t.Fatalf("tool = %s, want %s", report.Tool, capabilities.ToolAgentMail)
	}

	wantIDs := CapabilityIDs()
	if len(report.Capabilities) != len(wantIDs) {
		t.Fatalf("capability count = %d, want %d", len(report.Capabilities), len(wantIDs))
	}
	for _, id := range wantIDs {
		got, present := report.Capabilities[id]
		if !present {
			t.Fatalf("capability %s missing from report", id)
		}
		if got.Status != capabilities.StatusDegraded {
			t.Fatalf("capability %s status = %s, want degraded (fixture state=malformed-json)", id, got.Status)
		}
		if got.Notes == "" || !strings.Contains(got.Notes, "decode") {
			t.Fatalf("capability %s notes = %q, want non-empty with decode wrapping", id, got.Notes)
		}
	}

	if rt.toolNames()[0] != "fetch_inbox" {
		t.Fatalf("probe should call fetch_inbox first, got %v", rt.toolNames())
	}

	// Confirm that the error surfaced through Probe is wrapped as ErrDecode
	// rather than a bare string. We drive callTool with a second copy of the
	// fixture body so the assertion exercises the wrapping site directly.
	rt.responses = append(rt.responses, &http.Response{
		StatusCode: http.StatusOK,
		Header:     make(http.Header),
		Body:       io.NopCloser(strings.NewReader(fixture.StdoutText)),
	})
	directErr := client.callTool(context.Background(), "fetch_inbox", map[string]any{
		"project_key": "/repo",
		"agent_name":  "RoseCastle",
		"limit":       1,
	}, &[]Message{})
	if directErr == nil {
		t.Fatalf("callTool should error on malformed JSON")
	}
	if !errors.Is(directErr, ErrDecode) {
		t.Fatalf("callTool err = %v, want ErrDecode wrap", directErr)
	}
}

// agentMailGoldenFixture mirrors the shape of the committed Phase 0 golden
// outputs at packages/fixtures/golden-outputs/agent_mail/*.json. Only the
// fields the adapter contract observes are decoded — fixtures may carry
// additional metadata (capturedAt, durationMs, etc.) that we deliberately
// ignore so the contract stays loose around capture provenance.
type agentMailGoldenFixture struct {
	Meta struct {
		Adapter string `json:"adapter"`
		State   string `json:"state"`
	} `json:"meta"`
	StdoutText   string `json:"stdoutText"`
	Capabilities map[string]struct {
		Status string `json:"status"`
		Notes  string `json:"notes"`
	} `json:"capabilities"`
}

func loadAgentMailGoldenFixture(t *testing.T, name string) agentMailGoldenFixture {
	t.Helper()
	root := findRepoRoot(t)
	path := filepath.Join(root, "packages", "fixtures", "golden-outputs", "agent_mail", name)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read fixture %s: %v", path, err)
	}
	var fixture agentMailGoldenFixture
	if err := json.Unmarshal(data, &fixture); err != nil {
		t.Fatalf("parse fixture %s: %v", path, err)
	}
	if fixture.Meta.Adapter != "agent_mail" {
		t.Fatalf("fixture %s adapter = %q, want agent_mail", path, fixture.Meta.Adapter)
	}
	return fixture
}

func TestThreadIDValidationAndCapabilityReport(t *testing.T) {
	t.Parallel()
	threadID, err := BeadThreadID("hp-ay4")
	if err != nil {
		t.Fatalf("BeadThreadID: %v", err)
	}
	if threadID != "br-hp-ay4" {
		t.Fatalf("threadID = %s", threadID)
	}
	if err := ValidateBeadThreadID("hp-ay4"); !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("ValidateBeadThreadID err = %v, want ErrInvalidRequest", err)
	}
	report := StaticReport(func() time.Time { return time.Date(2026, 5, 4, 0, 0, 0, 0, time.UTC) })
	if err := report.Validate(); err != nil {
		t.Fatalf("capability report invalid: %v", err)
	}
	if report.Tool != capabilities.ToolAgentMail {
		t.Fatalf("tool = %s", report.Tool)
	}
	if report.Capabilities[CapabilityReservationsForceRelease].Status != capabilities.StatusOK {
		t.Fatalf("force release capability = %+v", report.Capabilities[CapabilityReservationsForceRelease])
	}
}

type recordingRoundTripper struct {
	request  *http.Request
	body     []byte
	response *http.Response
	err      error
}

func (r *recordingRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	r.request = req
	data, err := io.ReadAll(req.Body)
	if err != nil {
		return nil, err
	}
	r.body = data
	if r.err != nil {
		return nil, r.err
	}
	return cloneResponse(r.response), nil
}

type queueRoundTripper struct {
	responses []*http.Response
	bodies    [][]byte
}

func (q *queueRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	var data []byte
	if req.Body != nil {
		var err error
		data, err = io.ReadAll(req.Body)
		if err != nil {
			return nil, err
		}
	}
	q.bodies = append(q.bodies, data)
	if len(q.responses) == 0 {
		return nil, errors.New("unexpected request")
	}
	resp := q.responses[0]
	q.responses = q.responses[1:]
	return cloneResponse(resp), nil
}

func (q *queueRoundTripper) toolNames() []string {
	out := make([]string, 0, len(q.bodies))
	for _, body := range q.bodies {
		if len(body) == 0 {
			continue
		}
		var req mcpRequest
		if err := json.Unmarshal(body, &req); err == nil {
			out = append(out, req.Params.Name)
		}
	}
	return out
}

type recordingAudit struct {
	events []AuditEvent
}

func (r *recordingAudit) RecordAgentMailAction(_ context.Context, event AuditEvent) error {
	r.events = append(r.events, event)
	return nil
}

func mcpResultResponse(result any) *http.Response {
	data, err := json.Marshal(map[string]any{
		"jsonrpc": "2.0",
		"id":      "test",
		"result": map[string]any{
			"structuredContent": map[string]any{
				"result": result,
			},
		},
	})
	if err != nil {
		panic(err)
	}
	return &http.Response{
		StatusCode: http.StatusOK,
		Header:     make(http.Header),
		Body:       io.NopCloser(strings.NewReader(string(data))),
	}
}

func jsonResponse(result any) *http.Response {
	data, err := json.Marshal(result)
	if err != nil {
		panic(err)
	}
	return &http.Response{
		StatusCode: http.StatusOK,
		Header:     make(http.Header),
		Body:       io.NopCloser(strings.NewReader(string(data))),
	}
}

func cloneResponse(resp *http.Response) *http.Response {
	if resp == nil {
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     make(http.Header),
			Body:       io.NopCloser(strings.NewReader(`{"jsonrpc":"2.0","id":"test","result":{"structuredContent":{"result":{}}}}`)),
		}
	}
	out := *resp
	data, _ := io.ReadAll(resp.Body)
	resp.Body = io.NopCloser(strings.NewReader(string(data)))
	out.Body = io.NopCloser(strings.NewReader(string(data)))
	return &out
}
