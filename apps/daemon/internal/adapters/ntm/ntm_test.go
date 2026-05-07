package ntm

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/hoopoe-cockpit/hoopoe/apps/daemon/internal/capabilities"
	"nhooyr.io/websocket"
	"nhooyr.io/websocket/wsjson"
)

func TestArgvBuildersCoverRobotSurfacesAndAvoidShell(t *testing.T) {
	tests := []struct {
		name string
		got  []string
		want []string
	}{
		{name: "version", got: VersionArgv(), want: []string{"ntm", "version"}},
		{name: "sessions list", got: SessionsListArgv(), want: []string{"ntm", "sessions", "list", "--json"}},
		{name: "snapshot", got: SnapshotArgv(), want: []string{"ntm", "--robot-snapshot"}},
		{name: "status", got: StatusArgv(), want: []string{"ntm", "--robot-status"}},
		{name: "triage", got: TriageArgv(), want: []string{"ntm", "--robot-triage"}},
		{name: "approvals list", got: ApprovalsListArgv(), want: []string{"ntm", "approve", "list", "--json"}},
	}

	details, err := SessionDetailsArgv("proj")
	if err != nil {
		t.Fatalf("session details argv: %v", err)
	}
	tests = append(tests, struct {
		name string
		got  []string
		want []string
	}{"session details", details, []string{"ntm", "sessions", "show", "proj", "--json"}})

	tail, err := TailArgv(TailRequest{Session: "proj", Lines: 100, Panes: []string{"1", "2"}})
	if err != nil {
		t.Fatalf("tail argv: %v", err)
	}
	tests = append(tests, struct {
		name string
		got  []string
		want []string
	}{"tail", tail, []string{"ntm", "--robot-tail=proj", "--lines=100", "--panes=1,2"}})

	spawn, err := SpawnArgv(SpawnRequest{Session: "proj", Claude: 1, Codex: 2, Gemini: 1, Wait: true, DryRun: true})
	if err != nil {
		t.Fatalf("spawn argv: %v", err)
	}
	tests = append(tests, struct {
		name string
		got  []string
		want []string
	}{"spawn", spawn, []string{"ntm", "--robot-spawn=proj", "--spawn-cc=1", "--spawn-cod=2", "--spawn-gmi=1", "--spawn-wait", "--dry-run"}})

	send, err := SendArgv(SendRequest{Session: "proj", Message: "ship it", Panes: []string{"1"}, Type: "codex", Track: true})
	if err != nil {
		t.Fatalf("send argv: %v", err)
	}
	tests = append(tests, struct {
		name string
		got  []string
		want []string
	}{"send", send, []string{"ntm", "--robot-send=proj", "--msg=ship it", "--panes=1", "--type=codex", "--track"}})

	wait, err := WaitArgv(WaitRequest{Session: "proj", Timeout: "5m", Condition: "idle"})
	if err != nil {
		t.Fatalf("wait argv: %v", err)
	}
	tests = append(tests, struct {
		name string
		got  []string
		want []string
	}{"wait", wait, []string{"ntm", "--robot-wait=proj", "--timeout=5m", "--condition=idle"}})

	interrupt, err := InterruptArgv(InterruptRequest{Session: "proj", Message: "stop", Panes: []string{"2"}, DryRun: true})
	if err != nil {
		t.Fatalf("interrupt argv: %v", err)
	}
	tests = append(tests, struct {
		name string
		got  []string
		want []string
	}{"interrupt", interrupt, []string{"ntm", "--robot-interrupt=proj", "--interrupt-msg=stop", "--panes=2", "--dry-run"}})

	approve, err := ApproveArgv(ApprovalRequest{Token: "abc123"})
	if err != nil {
		t.Fatalf("approve argv: %v", err)
	}
	tests = append(tests, struct {
		name string
		got  []string
		want []string
	}{"approve", approve, []string{"ntm", "approve", "abc123", "--json"}})

	deny, err := DenyArgv(ApprovalRequest{Token: "abc123", Reason: "too risky"})
	if err != nil {
		t.Fatalf("deny argv: %v", err)
	}
	tests = append(tests, struct {
		name string
		got  []string
		want []string
	}{"deny", deny, []string{"ntm", "approve", "deny", "abc123", "--reason", "too risky", "--json"}})

	capture, err := TmuxCaptureArgv(TmuxCaptureRequest{TargetPane: "%1", StartLine: -100, JoinWrapped: true})
	if err != nil {
		t.Fatalf("capture argv: %v", err)
	}
	tests = append(tests, struct {
		name string
		got  []string
		want []string
	}{"tmux capture fallback", capture, []string{"tmux", "capture-pane", "-p", "-t", "%1", "-J", "-S", "-100"}})

	for _, tt := range tests {
		if !reflect.DeepEqual(tt.got, tt.want) {
			t.Fatalf("%s argv = %#v, want %#v", tt.name, tt.got, tt.want)
		}
		assertNoShellTokens(t, tt.got)
	}
}

func TestArgvBuildersRejectAmbiguousRequests(t *testing.T) {
	checks := []struct {
		name string
		err  error
	}{
		{name: "session details", err: errOnly(SessionDetailsArgv(""))},
		{name: "tail", err: errOnly(TailArgv(TailRequest{}))},
		{name: "spawn", err: errOnly(SpawnArgv(SpawnRequest{}))},
		{name: "send message", err: errOnly(SendArgv(SendRequest{Session: "proj"}))},
		{name: "approve", err: errOnly(ApproveArgv(ApprovalRequest{}))},
		{name: "deny reason", err: errOnly(DenyArgv(ApprovalRequest{Token: "abc"}))},
		{name: "tmux target", err: errOnly(TmuxCaptureArgv(TmuxCaptureRequest{}))},
	}
	for _, check := range checks {
		if !errors.Is(check.err, ErrInvalidRequest) {
			t.Fatalf("%s err = %v, want ErrInvalidRequest", check.name, check.err)
		}
	}
}

func TestParseSnapshotUsesGoldenAndScenarioFixtures(t *testing.T) {
	var fixture struct {
		StdoutJSON   json.RawMessage                    `json:"stdoutJson"`
		Capabilities map[string]capabilities.Capability `json:"capabilities"`
	}
	mustReadFixture(t, "packages/fixtures/golden-outputs/ntm/normal.json", &fixture)

	snap, err := ParseSnapshot(fixture.StdoutJSON)
	if err != nil {
		t.Fatalf("ParseSnapshot golden: %v", err)
	}
	if !snap.Success || len(snap.Sessions) != 2 {
		t.Fatalf("golden snapshot success/sessions = %v/%d", snap.Success, len(snap.Sessions))
	}
	panes := snap.Sessions[1].NormalizedPanes()
	if len(panes) != 4 || panes[0].UnifiedState != "working" || panes[2].UnifiedState != "idle" {
		t.Fatalf("normalized golden panes = %+v", panes)
	}
	if fixture.Capabilities[CapabilityRobotSnapshot].Status != capabilities.StatusOK {
		t.Fatalf("fixture robot snapshot cap = %+v", fixture.Capabilities[CapabilityRobotSnapshot])
	}

	var healthy Snapshot
	mustReadFixture(t, "packages/fixtures/scenarios/healthy-hour/ntm-snapshot.json", &healthy)
	healthy, err = ParseSnapshot(healthy.Raw)
	if err != nil {
		t.Fatalf("ParseSnapshot healthy scenario: %v", err)
	}
	healthyPanes := healthy.Sessions[0].NormalizedPanes()
	if healthyPanes[0].UnifiedState != "idle" || healthyPanes[1].UnifiedState != "typing" || healthyPanes[3].UnifiedState != "tool_use" {
		t.Fatalf("healthy unified states = %+v", healthyPanes)
	}

	wedgedData := mustReadFile(t, "packages/fixtures/scenarios/wedged-pane/ntm-snapshot.json")
	wedged, err := ParseSnapshot(wedgedData)
	if err != nil {
		t.Fatalf("ParseSnapshot wedged scenario: %v", err)
	}
	wedgedPanes := wedged.Sessions[0].NormalizedPanes()
	if wedgedPanes[0].UnifiedState != "wedged" || wedgedPanes[0].Evidence == "" {
		t.Fatalf("wedged pane = %+v", wedgedPanes[0])
	}
}

// TestProbeOnMissingToolGoldenFixtureMarksAllCapabilitiesMissing loads
// packages/fixtures/golden-outputs/ntm/missing-tool.json and pins the
// adapter contract from plan.md §18.3 for the missing-tool state.
//
// The existing TestProbeReportsMissing... case in this file already
// covers the runner-error path (`exec: ... not found`). The fixture
// declares a different observable surface: `exit: 127` plus a
// "ntm: command not found" stderr. That hits the
// commandError-with-ExitCode-127 branch of statusForError
// (ntm.go:1257-1261) — distinct from the exec.ErrNotFound /
// "executable file not found" / "command not found" string match
// branches at line 1269. A regression that flips that branch back to
// Degraded would slip past the existing test but trip this one.
//
// Acceptance pinned: the version probe at ntm.go:883 returns a
// commandError, statusForError fans StatusMissing across every
// capability, and CapabilityPresent in particular is reported
// Missing — matching the fixture's
// `capabilities.ntm._present.status == "missing"` declaration.
func TestProbeOnMissingToolGoldenFixtureMarksAllCapabilitiesMissing(t *testing.T) {
	fixture := loadNTMGoldenFixture(t, "missing-tool.json")
	if fixture.Meta.State != "missing-tool" {
		t.Fatalf("fixture state = %q, want missing-tool", fixture.Meta.State)
	}
	if cap, ok := fixture.Capabilities["ntm._present"]; !ok || cap.Status != "missing" {
		t.Fatalf("fixture must declare ntm._present=missing, got %+v", fixture.Capabilities)
	}
	if fixture.Exit != 127 {
		t.Fatalf("fixture exit = %d, want 127 (the discriminant for the commandError-127 branch this test pins)", fixture.Exit)
	}
	if !strings.Contains(fixture.StderrText, "command not found") {
		t.Fatalf("fixture stderrText = %q, want a 'command not found' marker", fixture.StderrText)
	}

	// Drive the version probe via a fakeRunner that replays the
	// fixture's exit code + stderr verbatim. The adapter's
	// runText helper (ntm.go:1121-1127) sees ExitCode != 0 and
	// returns a commandError wrapping that result; statusForError
	// (ntm.go:1257-1261) then maps ExitCode 127 → StatusMissing,
	// and Probe (ntm.go:884-892) fans that state across every
	// capability via missingCapabilities + the per-cap rewrite
	// loop at lines 886-890.
	adapter := New(&fakeRunner{responses: map[string]CommandResult{
		"ntm version": {
			ExitCode: fixture.Exit,
			Stderr:   []byte(fixture.StderrText),
		},
	}})
	adapter.Now = fixedNow
	report, err := adapter.Probe(context.Background())
	if err != nil {
		t.Fatalf("Probe: %v", err)
	}

	present, ok := report.Capabilities[CapabilityPresent]
	if !ok {
		t.Fatalf("report missing %s", CapabilityPresent)
	}
	if present.Status != capabilities.StatusMissing {
		t.Fatalf("%s status = %q, want missing (fixture state=missing-tool, exit=127)", CapabilityPresent, present.Status)
	}

	// Every other capability should also be Missing — the version
	// failure short-circuits the rest of Probe at ntm.go:892, and
	// missingCapabilities seeds the whole map. A future refactor
	// that lets some capabilities through to a separate probe path
	// (e.g., live-mode REST when the binary is missing) would slip
	// past the CapabilityPresent assertion above; this fan-out check
	// guards against that drift. Policy-blocked capabilities
	// (the mutating ones routed through blockPolicyCapabilities)
	// can legitimately end up as StatusBlockedByPolicy because the
	// blockPolicyCapabilities pass at ntm.go:891 fires after the
	// per-capability rewrite — accept either Missing or
	// BlockedByPolicy for those.
	policyBlocked := map[string]struct{}{
		CapabilitySessionsSpawn:      {},
		CapabilitySessionsTerminate:  {},
		CapabilitySessionsAttach:     {},
		CapabilityApprovalsApprove:   {},
		CapabilityApprovalsDeny:      {},
		CapabilitySwarmHalt:          {},
		CapabilitySpawn:              {},
		CapabilitySendMarchingOrders: {},
		CapabilityPaneKill:           {},
	}
	for capID, cap := range report.Capabilities {
		if _, ok := policyBlocked[capID]; ok {
			if cap.Status != capabilities.StatusMissing && cap.Status != capabilities.StatusBlockedByPolicy {
				t.Fatalf("%s status = %q, want missing or blocked_by_policy", capID, cap.Status)
			}
			continue
		}
		if cap.Status != capabilities.StatusMissing {
			t.Fatalf("%s status = %q, want missing (whole-tool missing fixture should fan StatusMissing across non-policy caps)", capID, cap.Status)
		}
	}
}

// TestProbeOnMalformedJSONGoldenFixtureDegradesSnapshotCapability loads
// packages/fixtures/golden-outputs/ntm/malformed-json.json and pins the
// adapter contract from plan.md §18.3: when the ntm CLI returns a truncated
// JSON envelope, the snapshot parser must wrap (not panic) and Probe must
// surface CapabilityRobotSnapshot as Degraded. Drives the malformed bytes
// through the same fakeRunner+Probe pipeline used by the existing
// TestProbeReportsMissingMalformedUnsupportedHighVolumeAndApprovalFailure
// test, so a fixture edit that drifts the byte stream away from the
// "non-JSON, must not panic" intent will fail this test.
func TestProbeOnMalformedJSONGoldenFixtureDegradesSnapshotCapability(t *testing.T) {
	fixture := loadNTMGoldenFixture(t, "malformed-json.json")
	if fixture.Meta.State != "malformed-json" {
		t.Fatalf("fixture state = %q, want malformed-json", fixture.Meta.State)
	}
	if cap, ok := fixture.Capabilities["ntm._parse"]; !ok || cap.Status != "degraded" {
		t.Fatalf("fixture must declare ntm._parse=degraded, got %+v", fixture.Capabilities)
	}
	if fixture.StdoutText == "" {
		t.Fatalf("fixture stdoutText is empty")
	}

	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("ParseSnapshot panicked on malformed JSON fixture: %v", r)
		}
	}()
	if _, err := ParseSnapshot([]byte(fixture.StdoutText)); err == nil {
		t.Fatalf("ParseSnapshot accepted truncated JSON without error")
	}

	adapter := New(&fakeRunner{responses: map[string]CommandResult{
		"ntm version":              {Stdout: []byte("ntm version 1.7.0\n")},
		"ntm sessions list --json": {Stdout: []byte(`{"sessions":null,"count":0}`)},
		"ntm --robot-snapshot":     {Stdout: []byte(fixture.StdoutText)},
		"ntm --robot-status":       {Stdout: []byte(`{"success":true,"sessions":[]}`)},
		"ntm --robot-triage":       {Stdout: []byte(`{"success":true,"items":[]}`)},
		"ntm approve list --json":  {Stdout: []byte(`{"success":true,"approvals":[]}`)},
	}})
	adapter.Now = fixedNow
	report, err := adapter.Probe(context.Background())
	if err != nil {
		t.Fatalf("Probe: %v", err)
	}
	snap, ok := report.Capabilities[CapabilityRobotSnapshot]
	if !ok {
		t.Fatalf("report missing %s", CapabilityRobotSnapshot)
	}
	if snap.Status != capabilities.StatusDegraded {
		t.Fatalf("%s status = %s, want degraded (fixture state=malformed-json)", CapabilityRobotSnapshot, snap.Status)
	}
	if snap.Notes == "" {
		t.Fatalf("%s notes are empty; expected parser error trace", CapabilityRobotSnapshot)
	}
}

type ntmGoldenFixture struct {
	Meta struct {
		Adapter string `json:"adapter"`
		State   string `json:"state"`
	} `json:"meta"`
	Exit         int    `json:"exit"`
	StdoutText   string `json:"stdoutText"`
	StderrText   string `json:"stderrText"`
	Capabilities map[string]struct {
		Status string `json:"status"`
		Notes  string `json:"notes"`
	} `json:"capabilities"`
}

func loadNTMGoldenFixture(t *testing.T, name string) ntmGoldenFixture {
	t.Helper()
	path := filepath.Join("packages", "fixtures", "golden-outputs", "ntm", name)
	data := mustReadFile(t, path)
	var fixture ntmGoldenFixture
	if err := json.Unmarshal(data, &fixture); err != nil {
		t.Fatalf("parse fixture %s: %v", path, err)
	}
	if fixture.Meta.Adapter != "ntm" {
		t.Fatalf("fixture %s adapter = %q, want ntm", path, fixture.Meta.Adapter)
	}
	return fixture
}

func TestParseNullSessionsAndStateMapping(t *testing.T) {
	sessions, err := ParseSessionsResponse([]byte(`{"sessions":null,"count":0}`))
	if err != nil {
		t.Fatalf("ParseSessionsResponse: %v", err)
	}
	if sessions.Count != 0 || len(sessions.Sessions) != 0 {
		t.Fatalf("sessions = %+v", sessions)
	}

	snap, err := ParseSnapshot([]byte(`{"success":true,"sessions":null}`))
	if err != nil {
		t.Fatalf("ParseSnapshot null sessions: %v", err)
	}
	if len(snap.Sessions) != 0 {
		t.Fatalf("snapshot sessions = %+v", snap.Sessions)
	}

	tests := map[string]string{
		"IDLE":      "idle",
		"TYPING":    "typing",
		"THINKING":  "thinking",
		"TOOL_USE":  "tool_use",
		"COMPLETE":  "complete",
		"ERROR":     "error",
		"active":    "working",
		"something": "something",
	}
	for in, want := range tests {
		if got := MapRobotState(in, ""); got != want {
			t.Fatalf("MapRobotState(%q) = %q, want %q", in, got, want)
		}
	}
	if got := MapRobotState("TOOL_USE", "wedged"); got != "wedged" {
		t.Fatalf("wedged override = %q", got)
	}
}

func TestAdapterMethodsWrapRobotLiveApprovalAndFallbackSurfaces(t *testing.T) {
	runner := &fakeRunner{responses: map[string]CommandResult{
		"ntm sessions list --json":                                  {Stdout: []byte(`{"sessions":[{"name":"proj","exists":true}],"count":1}`)},
		"ntm sessions show proj --json":                             {Stdout: []byte(`{"name":"proj","exists":true}`)},
		"ntm --robot-snapshot":                                      {Stdout: []byte(`{"success":true,"sessions":[{"name":"proj","panes":[{"id":"%1","state":"IDLE"}]}]}`)},
		"ntm --robot-status":                                        {Stdout: []byte(`{"success":true,"sessions":[]}`)},
		"ntm --robot-triage":                                        {Stdout: []byte(`{"success":true,"items":[]}`)},
		"ntm --robot-tail=proj --lines=10 --panes=1":                {Stdout: []byte(`{"success":true,"session":"proj","panes":{"1":{"output":"ok"}}}`)},
		"ntm --robot-activity=proj":                                 {Stdout: []byte(`{"success":true,"events":[]}`)},
		"ntm --robot-spawn=proj --spawn-cc=1 --spawn-wait":          {Stdout: []byte(`{"success":true}`)},
		"ntm --robot-send=proj --msg=ship --panes=1 --track":        {Stdout: []byte(`{"success":true}`)},
		"ntm --robot-wait=proj --timeout=1m --condition=idle":       {Stdout: []byte(`{"success":true}`)},
		"ntm --robot-interrupt=proj --interrupt-msg=stop --panes=1": {Stdout: []byte(`{"success":true}`)},
		"ntm approve list --json":                                   {Stdout: []byte(`{"success":true,"approvals":[]}`)},
		"ntm approve show abc --json":                               {Stdout: []byte(`{"success":true,"token":"abc"}`)},
		"ntm approve abc --json":                                    {Stdout: []byte(`{"success":true}`)},
		"ntm approve deny abc --reason no --json":                   {Stdout: []byte(`{"success":true}`)},
		"tmux capture-pane -p -t %1 -J -S -50":                      {Stdout: []byte("pane bytes\n")},
	}}
	adapter := New(runner)
	adapter.Now = fixedNow

	if got, err := adapter.SessionsList(context.Background()); err != nil || got.Count != 1 {
		t.Fatalf("SessionsList = %+v, %v", got, err)
	}
	if _, err := adapter.SessionDetails(context.Background(), "proj"); err != nil {
		t.Fatalf("SessionDetails: %v", err)
	}
	if _, err := adapter.Snapshot(context.Background()); err != nil {
		t.Fatalf("Snapshot: %v", err)
	}
	if _, err := adapter.Status(context.Background()); err != nil {
		t.Fatalf("Status: %v", err)
	}
	if _, err := adapter.Triage(context.Background()); err != nil {
		t.Fatalf("Triage: %v", err)
	}
	if got, err := adapter.Tail(context.Background(), TailRequest{Session: "proj", Lines: 10, Panes: []string{"1"}}); err != nil || got.Panes["1"].Output != "ok" {
		t.Fatalf("Tail = %+v, %v", got, err)
	}
	if _, err := adapter.Activity(context.Background(), "proj"); err != nil {
		t.Fatalf("Activity: %v", err)
	}
	if _, err := adapter.Spawn(context.Background(), SpawnRequest{Session: "proj", Claude: 1, Wait: true}); err != nil {
		t.Fatalf("Spawn: %v", err)
	}
	if _, err := adapter.Send(context.Background(), SendRequest{Session: "proj", Message: "ship", Panes: []string{"1"}, Track: true}); err != nil {
		t.Fatalf("Send: %v", err)
	}
	if _, err := adapter.Wait(context.Background(), WaitRequest{Session: "proj", Timeout: "1m", Condition: "idle"}); err != nil {
		t.Fatalf("Wait: %v", err)
	}
	if _, err := adapter.Interrupt(context.Background(), InterruptRequest{Session: "proj", Message: "stop", Panes: []string{"1"}}); err != nil {
		t.Fatalf("Interrupt: %v", err)
	}
	if _, err := adapter.ApprovalsList(context.Background()); err != nil {
		t.Fatalf("ApprovalsList: %v", err)
	}
	if _, err := adapter.ApprovalShow(context.Background(), "abc"); err != nil {
		t.Fatalf("ApprovalShow: %v", err)
	}
	if _, err := adapter.Approve(context.Background(), ApprovalRequest{Token: "abc"}); err != nil {
		t.Fatalf("Approve: %v", err)
	}
	if _, err := adapter.Deny(context.Background(), ApprovalRequest{Token: "abc", Reason: "no"}); err != nil {
		t.Fatalf("Deny: %v", err)
	}
	chunk, err := adapter.CapturePaneFallback(context.Background(), TmuxCaptureRequest{TargetPane: "%1", StartLine: -50, JoinWrapped: true})
	if err != nil {
		t.Fatalf("CapturePaneFallback: %v", err)
	}
	if chunk.Source != "tmux-capture-pane:fallback-mode" || string(chunk.Bytes) != "pane bytes\n" {
		t.Fatalf("fallback chunk = %+v", chunk)
	}
}

func TestLiveRESTSSEAndWebSocketSurfaces(t *testing.T) {
	stop := errors.New("stop after first event")
	mux := http.NewServeMux()
	mux.HandleFunc("/health", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	mux.HandleFunc("/api/sessions", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"sessions":[{"id":"proj"}]}`))
	})
	mux.HandleFunc("/api/sessions/proj", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"id":"proj","agents":[]}`))
	})
	mux.HandleFunc("/api/panes/%251/state", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"id":"%1","state":"IDLE"}`))
	})
	mux.HandleFunc("/events", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: {\"seq\":1,\"kind\":\"pane.output\",\"payload\":{\"pane\":\"%1\"}}\n\n"))
	})
	mux.HandleFunc("/v1/events", func(w http.ResponseWriter, r *http.Request) {
		conn, err := websocket.Accept(w, r, nil)
		if err != nil {
			t.Errorf("websocket accept: %v", err)
			return
		}
		defer conn.Close(websocket.StatusNormalClosure, "")
		_ = wsjson.Write(r.Context(), conn, EventEnvelope{Seq: 2, Kind: "pane.output", Payload: json.RawMessage(`{"pane":"%1"}`)})
	})
	server := httptest.NewServer(mux)
	defer server.Close()

	adapter := New(&fakeRunner{})
	adapter.LiveBaseURL = server.URL

	if raw, err := adapter.LiveSessions(context.Background()); err != nil || !strings.Contains(string(raw), "proj") {
		t.Fatalf("LiveSessions = %s, %v", raw, err)
	}
	if raw, err := adapter.LiveSessionDetails(context.Background(), "proj"); err != nil || !strings.Contains(string(raw), "agents") {
		t.Fatalf("LiveSessionDetails = %s, %v", raw, err)
	}
	if raw, err := adapter.LivePaneState(context.Background(), "%1"); err != nil || !strings.Contains(string(raw), "IDLE") {
		t.Fatalf("LivePaneState = %s, %v", raw, err)
	}

	err := adapter.ReadSSE(context.Background(), "/events", func(event EventEnvelope) error {
		if event.Seq != 1 || event.Kind != "pane.output" {
			t.Fatalf("SSE event = %+v", event)
		}
		return stop
	})
	if !errors.Is(err, stop) {
		t.Fatalf("ReadSSE err = %v, want stop", err)
	}

	err = adapter.ReadWebSocket(context.Background(), "/v1/events", func(event EventEnvelope) error {
		if event.Seq != 2 || event.Kind != "pane.output" {
			t.Fatalf("WS event = %+v", event)
		}
		return stop
	})
	if !errors.Is(err, stop) {
		t.Fatalf("ReadWebSocket err = %v, want stop", err)
	}
}

func TestOffsetTrackerPersistsByteOffsets(t *testing.T) {
	tracker := NewOffsetTracker(fixedNow)
	first := tracker.Record("%1", []byte("abc"), 1, "ws")
	second := tracker.Record("%1", []byte("de"), 2, "ws")
	other := tracker.Record("%2", []byte("x"), 3, "sse")

	if first.Offset != 0 || first.Length != 3 || second.Offset != 3 || second.Length != 2 {
		t.Fatalf("offsets first=%+v second=%+v", first, second)
	}
	if other.Offset != 0 || other.Source != "sse" {
		t.Fatalf("other offset = %+v", other)
	}
}

func TestProbeReportsCapabilitiesForRegistry(t *testing.T) {
	adapter := New(&fakeRunner{responses: map[string]CommandResult{
		"ntm version":                     {Stdout: []byte("ntm version 1.7.0\n")},
		"ntm sessions list --json":        {Stdout: []byte(`{"sessions":[{"name":"proj"}],"count":1}`)},
		"ntm --robot-snapshot":            {Stdout: []byte(`{"success":true,"version":"1.7.0","sessions":[{"name":"proj","panes":[{"id":"%1","state":"IDLE"}]}]}`)},
		"ntm --robot-status":              {Stdout: []byte(`{"success":true,"sessions":[]}`)},
		"ntm --robot-triage":              {Stdout: []byte(`{"success":true,"items":[]}`)},
		"ntm --robot-tail=proj --lines=1": {Stdout: []byte(`{"success":true,"session":"proj","panes":{"1":{"output":"ok"}}}`)},
		"ntm approve list --json":         {Stdout: []byte(`{"success":true,"approvals":[]}`)},
	}})
	adapter.Now = fixedNow

	registry := capabilities.New("test-api")
	if err := registry.RegisterProbe(capabilities.ToolNTM, func() (*capabilities.ToolReport, error) {
		return adapter.Probe(context.Background())
	}); err != nil {
		t.Fatal(err)
	}
	registry.Probe()

	assertCapability(t, registry, CapabilitySessionsList, capabilities.StatusOK)
	assertCapability(t, registry, CapabilityRobotSnapshot, capabilities.StatusOK)
	assertCapability(t, registry, CapabilityRobotStatus, capabilities.StatusOK)
	assertCapability(t, registry, CapabilityRobotTail, capabilities.StatusOK)
	assertCapability(t, registry, CapabilityRobotTriage, capabilities.StatusOK)
	assertCapability(t, registry, CapabilityPanesStream, capabilities.StatusOK)
	assertCapability(t, registry, CapabilityApprovalsList, capabilities.StatusOK)
	assertCapability(t, registry, CapabilitySessionsSpawn, capabilities.StatusBlockedByPolicy)
	assertCapability(t, registry, CapabilitySessionsTerminate, capabilities.StatusBlockedByPolicy)
	assertCapability(t, registry, CapabilityApprovalsApprove, capabilities.StatusBlockedByPolicy)
	assertCapability(t, registry, CapabilitySwarmHalt, capabilities.StatusBlockedByPolicy)

	stream, ok := registry.LookupCapability(capabilities.ToolNTM, CapabilityPanesStream)
	if !ok || stream.Transport != "poll" || stream.Fallback != CapabilityRobotTail {
		t.Fatalf("panes stream = %+v, %v", stream, ok)
	}
}

func TestProbeMarksLiveServeAsPreferredStreamTransport(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/health", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	server := httptest.NewServer(mux)
	defer server.Close()

	adapter := New(&fakeRunner{responses: map[string]CommandResult{
		"ntm version":              {Stdout: []byte("ntm version 1.7.0\n")},
		"ntm sessions list --json": {Stdout: []byte(`{"sessions":null,"count":0}`)},
		"ntm --robot-snapshot":     {Stdout: []byte(`{"success":true,"version":"1.7.0","sessions":null}`)},
		"ntm --robot-status":       {Stdout: []byte(`{"success":true,"sessions":[]}`)},
		"ntm --robot-triage":       {Stdout: []byte(`{"success":true,"items":[]}`)},
		"ntm approve list --json":  {Stdout: []byte(`{"success":true,"approvals":[]}`)},
	}})
	adapter.Now = fixedNow
	adapter.LiveBaseURL = server.URL

	report, err := adapter.Probe(context.Background())
	if err != nil {
		t.Fatalf("Probe: %v", err)
	}
	if report.Capabilities[CapabilityServeREST].Status != capabilities.StatusOK {
		t.Fatalf("serve rest = %+v", report.Capabilities[CapabilityServeREST])
	}
	stream := report.Capabilities[CapabilityPanesStream]
	if stream.Status != capabilities.StatusOK || stream.Transport != "ws,sse" {
		t.Fatalf("panes stream = %+v", stream)
	}
}

func TestProbeReportsMissingMalformedUnsupportedHighVolumeAndApprovalFailure(t *testing.T) {
	missing := New(&fakeRunner{err: errors.New(`exec: "ntm": executable file not found`)})
	missing.Now = fixedNow
	report, err := missing.Probe(context.Background())
	if err != nil {
		t.Fatalf("missing probe: %v", err)
	}
	if report.Capabilities[CapabilityPresent].Status != capabilities.StatusMissing {
		t.Fatalf("missing present = %+v", report.Capabilities[CapabilityPresent])
	}

	unsupported := New(&fakeRunner{responses: map[string]CommandResult{
		"ntm version": {Stdout: []byte("ntm version 0.9.0\n")},
	}})
	unsupported.Now = fixedNow
	report, err = unsupported.Probe(context.Background())
	if err != nil {
		t.Fatalf("unsupported probe: %v", err)
	}
	if report.Capabilities[CapabilityPresent].Status != capabilities.StatusMissing {
		t.Fatalf("unsupported present = %+v", report.Capabilities[CapabilityPresent])
	}

	malformed := New(&fakeRunner{responses: map[string]CommandResult{
		"ntm version":              {Stdout: []byte("ntm version 1.7.0\n")},
		"ntm sessions list --json": {Stdout: []byte(`{"sessions":null,"count":0}`)},
		"ntm --robot-snapshot":     {Stdout: []byte(`{"success":true`)},
		"ntm --robot-status":       {Stdout: []byte(`{"success":true,"sessions":[]}`)},
		"ntm --robot-triage":       {Stdout: []byte(`{"success":true,"items":[]}`)},
		"ntm approve list --json":  {Stdout: []byte(`{"success":true,"approvals":[]}`)},
	}})
	malformed.Now = fixedNow
	report, err = malformed.Probe(context.Background())
	if err != nil {
		t.Fatalf("malformed probe: %v", err)
	}
	if report.Capabilities[CapabilityRobotSnapshot].Status != capabilities.StatusDegraded {
		t.Fatalf("malformed snapshot = %+v", report.Capabilities[CapabilityRobotSnapshot])
	}

	highVolume := New(&fakeRunner{responses: map[string]CommandResult{
		"ntm version":              {Stdout: []byte("ntm version 1.7.0\n")},
		"ntm sessions list --json": {Stdout: []byte(`{"sessions":null,"count":0}` + strings.Repeat(" ", 64))},
		"ntm --robot-snapshot":     {Stdout: []byte(`{"success":true,"sessions":null}`)},
		"ntm --robot-status":       {Stdout: []byte(`{"success":true,"sessions":[]}`)},
		"ntm --robot-triage":       {Stdout: []byte(`{"success":true,"items":[]}`)},
		"ntm approve list --json":  {Stdout: []byte(`{"success":true,"approvals":[]}`)},
	}})
	highVolume.MaxStdoutBytes = 32
	highVolume.Now = fixedNow
	report, err = highVolume.Probe(context.Background())
	if err != nil {
		t.Fatalf("high-volume probe: %v", err)
	}
	if report.Capabilities[CapabilitySessionsList].Status != capabilities.StatusDegraded {
		t.Fatalf("high-volume sessions list = %+v", report.Capabilities[CapabilitySessionsList])
	}

	approvalFailure := New(&fakeRunner{responses: map[string]CommandResult{
		"ntm version":              {Stdout: []byte("ntm version 1.7.0\n")},
		"ntm sessions list --json": {Stdout: []byte(`{"sessions":null,"count":0}`)},
		"ntm --robot-snapshot":     {Stdout: []byte(`{"success":true,"sessions":null}`)},
		"ntm --robot-status":       {Stdout: []byte(`{"success":true,"sessions":[]}`)},
		"ntm --robot-triage":       {Stdout: []byte(`{"success":true,"items":[]}`)},
		"ntm approve list --json":  {Stdout: []byte(`{"success":false,"error":"store unavailable"}`)},
	}})
	approvalFailure.Now = fixedNow
	report, err = approvalFailure.Probe(context.Background())
	if err != nil {
		t.Fatalf("approval failure probe: %v", err)
	}
	if report.Capabilities[CapabilityApprovalsList].Status != capabilities.StatusDegraded {
		t.Fatalf("approval list failure = %+v", report.Capabilities[CapabilityApprovalsList])
	}
}

func TestActionIntentsDeclarePreconditionsAndPolicyCapabilities(t *testing.T) {
	intents := []ActionIntent{}
	send, err := SendMarchingOrdersIntent("proj", "agent-1", "continue")
	if err != nil {
		t.Fatalf("SendMarchingOrdersIntent: %v", err)
	}
	intents = append(intents, send)
	halt, err := SwarmHaltIntent("proj", "operator requested")
	if err != nil {
		t.Fatalf("SwarmHaltIntent: %v", err)
	}
	intents = append(intents, halt)
	terminate, err := SessionTerminateIntent("proj", "done")
	if err != nil {
		t.Fatalf("SessionTerminateIntent: %v", err)
	}
	intents = append(intents, terminate)
	attach, err := SessionAttachIntent("proj", "diagnostics")
	if err != nil {
		t.Fatalf("SessionAttachIntent: %v", err)
	}
	intents = append(intents, attach)
	approval, err := ApprovalDecisionIntent(ApprovalRequest{Token: "abc"}, true)
	if err != nil {
		t.Fatalf("ApprovalDecisionIntent approve: %v", err)
	}
	intents = append(intents, approval)
	deny, err := ApprovalDecisionIntent(ApprovalRequest{Token: "abc", Reason: "no"}, false)
	if err != nil {
		t.Fatalf("ApprovalDecisionIntent deny: %v", err)
	}
	intents = append(intents, deny)

	for _, intent := range intents {
		if intent.Kind == "" || intent.CapabilityID == "" || len(intent.Preconditions) == 0 || len(intent.Postconditions) == 0 {
			t.Fatalf("incomplete intent = %+v", intent)
		}
	}
}

func assertCapability(t *testing.T, registry *capabilities.Registry, capID string, want capabilities.CapabilityStatus) {
	t.Helper()
	got, ok := registry.LookupCapability(capabilities.ToolNTM, capID)
	if !ok || got.Status != want {
		t.Fatalf("%s = %+v, %v; want %s", capID, got, ok, want)
	}
}

func assertNoShellTokens(t *testing.T, argv []string) {
	t.Helper()
	for _, part := range argv {
		if part == "sh" || part == "-c" || part == "bash" {
			t.Fatalf("argv used shell token: %#v", argv)
		}
	}
}

type fakeRunner struct {
	responses map[string]CommandResult
	err       error
}

func (r *fakeRunner) Run(_ context.Context, argv []string) (CommandResult, error) {
	if r.err != nil {
		return CommandResult{}, r.err
	}
	key := strings.Join(argv, " ")
	result, ok := r.responses[key]
	if !ok {
		return CommandResult{ExitCode: 127, Stderr: []byte("unexpected argv: " + key)}, nil
	}
	return result, nil
}

func fixedNow() time.Time {
	return time.Date(2026, 5, 4, 0, 0, 0, 0, time.UTC)
}

func errOnly(_ []string, err error) error {
	return err
}

func mustReadFixture(t *testing.T, rel string, target any) {
	t.Helper()
	data := mustReadFile(t, rel)
	if rawTarget, ok := target.(*Snapshot); ok {
		rawTarget.Raw = append(json.RawMessage(nil), data...)
		return
	}
	if err := json.Unmarshal(data, target); err != nil {
		t.Fatalf("parse fixture %s: %v", rel, err)
	}
}

func mustReadFile(t *testing.T, rel string) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(findRepoRoot(t), rel))
	if err != nil {
		t.Fatalf("read fixture %s: %v", rel, err)
	}
	return data
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
