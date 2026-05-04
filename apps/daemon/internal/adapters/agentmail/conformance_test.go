package agentmail

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

type agentMailOperationCapture struct {
	Capture struct {
		Classification string `json:"classification"`
		Operation      string `json:"operation"`
		ServerURL      string `json:"serverUrl"`
		Transport      string `json:"transport"`
	} `json:"capture"`
	Request struct {
		Method string `json:"method"`
		Params struct {
			Name      string         `json:"name"`
			Arguments map[string]any `json:"arguments"`
		} `json:"params"`
	} `json:"request"`
	Response struct {
		Status int             `json:"status"`
		Body   json.RawMessage `json:"body"`
	} `json:"response"`
}

type agentMailConformanceTransport struct {
	t        *testing.T
	captures map[string]agentMailOperationCapture
	seen     []string
}

type agentMailMessageSummary struct {
	ID         int      `json:"id"`
	ThreadID   string   `json:"threadId"`
	Subject    string   `json:"subject"`
	Importance string   `json:"importance"`
	From       string   `json:"from"`
	To         []string `json:"to,omitempty"`
	Kind       string   `json:"kind,omitempty"`
	BodyMD     string   `json:"bodyMd,omitempty"`
}

type agentMailSendSummary struct {
	Count      int                       `json:"count"`
	Project    string                    `json:"project"`
	Deliveries []agentMailMessageSummary `json:"deliveries"`
}

type agentMailInboxSummary struct {
	Count int                     `json:"count"`
	IDs   []int                   `json:"ids"`
	First agentMailMessageSummary `json:"first"`
}

type agentMailReservationSummary struct {
	ID          int    `json:"id"`
	PathPattern string `json:"pathPattern"`
	Exclusive   bool   `json:"exclusive"`
	Reason      string `json:"reason"`
}

type agentMailReserveSummary struct {
	GrantedCount  int                         `json:"grantedCount"`
	ConflictCount int                         `json:"conflictCount"`
	FirstGranted  agentMailReservationSummary `json:"firstGranted"`
}

type agentMailReleaseSummary struct {
	Released   int    `json:"released"`
	ReleasedAt string `json:"releasedAt"`
}

func TestPhase0AgentMailOperationConformance(t *testing.T) {
	captures := loadAgentMailOperationCaptures(t)
	transport := &agentMailConformanceTransport{t: t, captures: captures}
	client := New(captures["send_message"].Capture.ServerURL)
	client.HTTPClient = &http.Client{Transport: transport}

	sendReq := sendRequestFromCapture(t, captures["send_message"])
	send, err := client.SendMessage(context.Background(), sendReq)
	if err != nil {
		t.Fatalf("SendMessage: %v", err)
	}
	assertAgentMailConformanceEqual(t, "send_message", summarizeAgentMailSend(send), agentMailSendSummary{
		Count:   1,
		Project: "/tmp/hoopoe-phase0-agent-mail-hp-pr3d",
		Deliveries: []agentMailMessageSummary{
			{
				ID:         403,
				ThreadID:   "hp-pr3d-phase0-agent-mail-capture",
				Subject:    "[hp-pr3d] Phase 0 Agent Mail fixture capture",
				Importance: "normal",
				From:       "HoopoePhase0Sender",
				To:         []string{"HoopoePhase0Receiver"},
				BodyMD:     "Captured by hp-pr3d to freeze send_message JSON-RPC envelope semantics.",
			},
		},
	})

	inboxReq := fetchInboxRequestFromCapture(t, captures["fetch_inbox"])
	inbox, err := client.FetchInbox(context.Background(), inboxReq)
	if err != nil {
		t.Fatalf("FetchInbox: %v", err)
	}
	assertAgentMailConformanceEqual(t, "fetch_inbox", summarizeAgentMailInbox(inbox), agentMailInboxSummary{
		Count: 1,
		IDs:   []int{403},
		First: agentMailMessageSummary{
			ID:         403,
			ThreadID:   "hp-pr3d-phase0-agent-mail-capture",
			Subject:    "[hp-pr3d] Phase 0 Agent Mail fixture capture",
			Importance: "normal",
			From:       "HoopoePhase0Sender",
			Kind:       "to",
			BodyMD:     "Captured by hp-pr3d to freeze send_message JSON-RPC envelope semantics.",
		},
	})

	reserveReq := reservePathsRequestFromCapture(t, captures["file_reservation_paths"])
	reserved, err := client.ReservePaths(context.Background(), reserveReq)
	if err != nil {
		t.Fatalf("ReservePaths: %v", err)
	}
	assertAgentMailConformanceEqual(t, "file_reservation_paths", summarizeAgentMailReserve(reserved), agentMailReserveSummary{
		GrantedCount:  1,
		ConflictCount: 0,
		FirstGranted: agentMailReservationSummary{
			ID:          765,
			PathPattern: "packages/fixtures/phase0-agent-mail/**",
			Exclusive:   true,
			Reason:      "hp-pr3d Phase 0 fixture capture",
		},
	})

	releaseReq := releaseReservationsRequestFromCapture(t, captures["release_file_reservations"])
	released, err := client.ReleaseReservations(context.Background(), releaseReq)
	if err != nil {
		t.Fatalf("ReleaseReservations: %v", err)
	}
	assertAgentMailConformanceEqual(t, "release_file_reservations", summarizeAgentMailRelease(released), agentMailReleaseSummary{
		Released:   1,
		ReleasedAt: "2026-05-04T18:04:24.602705+00:00",
	})
	assertAgentMailConformanceEqual(t, "operation order", transport.seen, []string{
		"send_message",
		"fetch_inbox",
		"file_reservation_paths",
		"release_file_reservations",
	})
}

func loadAgentMailOperationCaptures(t *testing.T) map[string]agentMailOperationCapture {
	t.Helper()
	root := filepath.Join(findRepoRoot(t), "packages", "fixtures", "phase0-agent-mail")
	required := []string{
		"send_message",
		"fetch_inbox",
		"file_reservation_paths",
		"release_file_reservations",
	}
	out := make(map[string]agentMailOperationCapture, len(required))
	for _, operation := range required {
		path := filepath.Join(root, "operation-"+strings.ReplaceAll(operation, "_", "-")+".json")
		var capture agentMailOperationCapture
		if operation == "file_reservation_paths" {
			path = filepath.Join(root, "operation-file-reservation-paths.json")
		}
		if operation == "release_file_reservations" {
			path = filepath.Join(root, "operation-release-file-reservations.json")
		}
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s: %v", path, err)
		}
		if err := json.Unmarshal(data, &capture); err != nil {
			t.Fatalf("parse %s: %v", path, err)
		}
		if capture.Capture.Classification != "canonical-local-mcp-jsonrpc" {
			t.Fatalf("%s classification = %q", path, capture.Capture.Classification)
		}
		if capture.Capture.Operation != operation {
			t.Fatalf("%s operation = %q, want %q", path, capture.Capture.Operation, operation)
		}
		if capture.Request.Method != "tools/call" || capture.Response.Status != http.StatusOK {
			t.Fatalf("%s has nonconforming method/status", path)
		}
		out[operation] = capture
	}
	return out
}

func findRepoRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.work")); err == nil {
			return dir
		}
		next := filepath.Dir(dir)
		if next == dir {
			t.Fatalf("could not find repo root from %s", dir)
		}
		dir = next
	}
}

func (rt *agentMailConformanceTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if req.Method != http.MethodPost || req.URL.Path != defaultEndpoint {
		rt.t.Fatalf("unexpected request target: %s %s", req.Method, req.URL.String())
	}
	body, err := io.ReadAll(req.Body)
	if err != nil {
		return nil, err
	}
	var actual mcpRequest
	if err := json.Unmarshal(body, &actual); err != nil {
		return nil, err
	}
	operation := actual.Params.Name
	capture, ok := rt.captures[operation]
	if !ok {
		rt.t.Fatalf("missing Agent Mail operation capture for %q", operation)
	}
	assertAgentMailRequestMatchesCapture(rt.t, operation, actual, capture)
	rt.seen = append(rt.seen, operation)
	return &http.Response{
		StatusCode: capture.Response.Status,
		Header:     make(http.Header),
		Body:       io.NopCloser(bytes.NewReader(capture.Response.Body)),
	}, nil
}

func assertAgentMailRequestMatchesCapture(t *testing.T, operation string, actual mcpRequest, capture agentMailOperationCapture) {
	t.Helper()
	if actual.Method != capture.Request.Method {
		t.Fatalf("%s method = %q, want %q", operation, actual.Method, capture.Request.Method)
	}
	expectedArgs := capturedOperationArguments(t, capture)
	for key, expectedValue := range expectedArgs {
		actualValue, ok := actual.Params.Arguments[key]
		if !ok {
			t.Fatalf("%s missing captured arg %q", operation, key)
		}
		if !jsonEqual(actualValue, expectedValue) {
			t.Fatalf("%s arg %q mismatch\nwant: %s\ngot: %s", operation, key, compactJSON(expectedValue), compactJSON(actualValue))
		}
	}
	for key, value := range actual.Params.Arguments {
		if _, expected := expectedArgs[key]; expected {
			continue
		}
		if !isAllowedAgentMailDefaultArg(key, value) {
			t.Fatalf("%s sent unexpected arg %q=%s", operation, key, compactJSON(value))
		}
	}
}

func capturedOperationArguments(t *testing.T, capture agentMailOperationCapture) map[string]any {
	t.Helper()
	if capture.Request.Params.Name != "call_extended_tool" {
		return capture.Request.Params.Arguments
	}
	toolName, ok := capture.Request.Params.Arguments["tool_name"].(string)
	if !ok || toolName != capture.Capture.Operation {
		t.Fatalf("extended capture tool_name = %#v, want %q", capture.Request.Params.Arguments["tool_name"], capture.Capture.Operation)
	}
	args, ok := capture.Request.Params.Arguments["arguments"].(map[string]any)
	if !ok {
		t.Fatalf("extended capture %s lacks nested arguments", capture.Capture.Operation)
	}
	return args
}

func isAllowedAgentMailDefaultArg(key string, value any) bool {
	if key == "broadcast" || key == "urgent_only" {
		boolValue, ok := value.(bool)
		return ok && !boolValue
	}
	return false
}

func sendRequestFromCapture(t *testing.T, capture agentMailOperationCapture) SendMessageRequest {
	t.Helper()
	args := capturedOperationArguments(t, capture)
	return SendMessageRequest{
		ProjectKey:  stringArg(t, args, "project_key"),
		SenderName:  stringArg(t, args, "sender_name"),
		To:          stringsArg(t, args, "to"),
		Subject:     stringArg(t, args, "subject"),
		BodyMD:      stringArg(t, args, "body_md"),
		Importance:  stringArg(t, args, "importance"),
		AckRequired: boolArg(t, args, "ack_required"),
		ThreadID:    stringArg(t, args, "thread_id"),
	}
}

func fetchInboxRequestFromCapture(t *testing.T, capture agentMailOperationCapture) FetchInboxRequest {
	t.Helper()
	args := capturedOperationArguments(t, capture)
	return FetchInboxRequest{
		ProjectKey:    stringArg(t, args, "project_key"),
		AgentName:     stringArg(t, args, "agent_name"),
		Limit:         intArg(t, args, "limit"),
		IncludeBodies: boolArg(t, args, "include_bodies"),
	}
}

func reservePathsRequestFromCapture(t *testing.T, capture agentMailOperationCapture) ReservePathsRequest {
	t.Helper()
	args := capturedOperationArguments(t, capture)
	return ReservePathsRequest{
		ProjectKey: stringArg(t, args, "project_key"),
		AgentName:  stringArg(t, args, "agent_name"),
		Paths:      stringsArg(t, args, "paths"),
		TTLSeconds: intArg(t, args, "ttl_seconds"),
		Exclusive:  boolArg(t, args, "exclusive"),
		Reason:     stringArg(t, args, "reason"),
	}
}

func releaseReservationsRequestFromCapture(t *testing.T, capture agentMailOperationCapture) ReleaseReservationsRequest {
	t.Helper()
	args := capturedOperationArguments(t, capture)
	return ReleaseReservationsRequest{
		ProjectKey: stringArg(t, args, "project_key"),
		AgentName:  stringArg(t, args, "agent_name"),
		Paths:      stringsArg(t, args, "paths"),
	}
}

func summarizeAgentMailSend(response SendMessageResponse) agentMailSendSummary {
	out := agentMailSendSummary{Count: response.Count}
	for i, delivery := range response.Deliveries {
		if i == 0 {
			out.Project = delivery.Project
		}
		out.Deliveries = append(out.Deliveries, summarizeAgentMailMessage(delivery.Payload))
	}
	return out
}

func summarizeAgentMailInbox(messages []Message) agentMailInboxSummary {
	out := agentMailInboxSummary{Count: len(messages)}
	for _, message := range messages {
		out.IDs = append(out.IDs, message.ID)
	}
	if len(messages) > 0 {
		out.First = summarizeAgentMailMessage(messages[0])
	}
	return out
}

func summarizeAgentMailMessage(message Message) agentMailMessageSummary {
	return agentMailMessageSummary{
		ID:         message.ID,
		ThreadID:   message.ThreadID,
		Subject:    message.Subject,
		Importance: message.Importance,
		From:       message.From,
		To:         nonEmptyStringSlice(message.To),
		Kind:       message.Kind,
		BodyMD:     message.BodyMD,
	}
}

func summarizeAgentMailReserve(response ReservePathsResponse) agentMailReserveSummary {
	out := agentMailReserveSummary{
		GrantedCount:  len(response.Granted),
		ConflictCount: len(response.Conflicts),
	}
	if len(response.Granted) > 0 {
		out.FirstGranted = summarizeAgentMailReservation(response.Granted[0])
	}
	return out
}

func summarizeAgentMailReservation(reservation Reservation) agentMailReservationSummary {
	return agentMailReservationSummary{
		ID:          reservation.ID,
		PathPattern: reservation.PathPattern,
		Exclusive:   reservation.Exclusive,
		Reason:      reservation.Reason,
	}
}

func summarizeAgentMailRelease(response ReleaseReservationsResponse) agentMailReleaseSummary {
	return agentMailReleaseSummary{
		Released:   response.Released,
		ReleasedAt: response.ReleasedAt,
	}
}

func stringArg(t *testing.T, args map[string]any, key string) string {
	t.Helper()
	value, ok := args[key].(string)
	if !ok || value == "" {
		t.Fatalf("arg %q = %#v, want non-empty string", key, args[key])
	}
	return value
}

func stringsArg(t *testing.T, args map[string]any, key string) []string {
	t.Helper()
	raw, ok := args[key].([]any)
	if !ok {
		t.Fatalf("arg %q = %#v, want string array", key, args[key])
	}
	values := make([]string, 0, len(raw))
	for _, entry := range raw {
		value, ok := entry.(string)
		if !ok {
			t.Fatalf("arg %q entry = %#v, want string", key, entry)
		}
		values = append(values, value)
	}
	return values
}

func boolArg(t *testing.T, args map[string]any, key string) bool {
	t.Helper()
	value, ok := args[key].(bool)
	if !ok {
		t.Fatalf("arg %q = %#v, want bool", key, args[key])
	}
	return value
}

func intArg(t *testing.T, args map[string]any, key string) int {
	t.Helper()
	value, ok := args[key].(float64)
	if !ok {
		t.Fatalf("arg %q = %#v, want number", key, args[key])
	}
	return int(value)
}

func nonEmptyStringSlice(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	return append([]string(nil), values...)
}

func assertAgentMailConformanceEqual(t *testing.T, label string, got any, want any) {
	t.Helper()
	if reflect.DeepEqual(got, want) {
		return
	}
	t.Fatalf("%s conformance mismatch\nwant:\n%s\ngot:\n%s", label, prettyJSON(want), prettyJSON(got))
}

func jsonEqual(left any, right any) bool {
	return compactJSON(left) == compactJSON(right)
}

func compactJSON(value any) string {
	data, err := json.Marshal(value)
	if err != nil {
		return fmt.Sprintf("<marshal failed: %v>", err)
	}
	return string(data)
}

func prettyJSON(value any) string {
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return fmt.Sprintf("<marshal failed: %v>", err)
	}
	return string(data)
}
