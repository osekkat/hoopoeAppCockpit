// Package ntm wraps Named Tmux Manager's robot and live-server surfaces.
//
// Adapter precedence follows docs/integration-contracts/ntm.md:
// ntm serve REST/SSE/WS first, ntm --robot-* fallback second, tmux capture
// last. Mutating operations are exposed only as typed argv/action metadata so
// the daemon can route them through ActionPlan policy.
package ntm

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/hoopoe-cockpit/hoopoe/apps/daemon/internal/capabilities"
	"nhooyr.io/websocket"
)

const (
	ToolName = "ntm"

	CapabilityPresent            = "ntm._present"
	CapabilitySessionsList       = "ntm.sessions.list"
	CapabilitySessionsSpawn      = "ntm.sessions.spawn"
	CapabilitySessionsTerminate  = "ntm.sessions.terminate"
	CapabilitySessionsAttach     = "ntm.sessions.attach"
	CapabilityPanesStream        = "ntm.panes.stream"
	CapabilityServeREST          = "ntm.serve.rest"
	CapabilityServeSSE           = "ntm.serve.sse"
	CapabilityServeWS            = "ntm.serve.ws"
	CapabilityRobotSnapshot      = "ntm.robot.snapshot"
	CapabilityRobotStatus        = "ntm.robot.status"
	CapabilityRobotTail          = "ntm.robot.tail"
	CapabilityRobotTriage        = "ntm.robot.triage"
	CapabilityRobotActivity      = "ntm.robot.activity"
	CapabilityApprovalsList      = "ntm.approvals.list"
	CapabilityApprovalsApprove   = "ntm.approvals.approve"
	CapabilityApprovalsDeny      = "ntm.approvals.deny"
	CapabilitySwarmHalt          = "ntm.swarm.halt"
	CapabilitySpawn              = "ntm.spawn"
	CapabilitySendMarchingOrders = "ntm.send_marching_orders"
	CapabilityPaneKill           = "ntm.pane.kill"

	ActionSwarmHalt          = "swarm.halt"
	ActionSessionTerminate   = "ntm.session.terminate"
	ActionSessionAttach      = "ntm.session.attach"
	ActionSendMarchingOrders = "agent.send_marching_orders"
	ActionPaneKill           = "agent.kill_wedged_process"
	ActionApprovalApprove    = "approval.approve"
	ActionApprovalDeny       = "approval.deny"

	defaultMaxStdoutBytes = 1 << 20
)

var (
	ErrInvalidRequest     = errors.New("ntm: invalid request")
	ErrOutputTooLarge     = errors.New("ntm: command output exceeded limit")
	ErrLiveUnavailable    = errors.New("ntm: live server unavailable")
	ErrUnsupportedVersion = errors.New("ntm: unsupported version")
)

type Runner interface {
	Run(ctx context.Context, argv []string) (CommandResult, error)
}

type CommandResult struct {
	ExitCode int
	Stdout   []byte
	Stderr   []byte
}

type ExecRunner struct{}

func (ExecRunner) Run(ctx context.Context, argv []string) (CommandResult, error) {
	if len(argv) == 0 || strings.TrimSpace(argv[0]) == "" {
		return CommandResult{}, fmt.Errorf("%w: empty argv", ErrInvalidRequest)
	}
	cmd := exec.CommandContext(ctx, argv[0], argv[1:]...)
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	result := CommandResult{Stdout: stdout.Bytes(), Stderr: stderr.Bytes()}
	if err == nil {
		return result, nil
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		result.ExitCode = exitErr.ExitCode()
		return result, nil
	}
	result.ExitCode = -1
	return result, err
}

type HTTPDoer interface {
	Do(req *http.Request) (*http.Response, error)
}

type Adapter struct {
	Runner         Runner
	HTTPClient     HTTPDoer
	LiveBaseURL    string
	LiveToken      string
	Now            func() time.Time
	MaxStdoutBytes int
}

func New(runner Runner) *Adapter {
	if runner == nil {
		runner = ExecRunner{}
	}
	return &Adapter{
		Runner:         runner,
		HTTPClient:     http.DefaultClient,
		Now:            time.Now,
		MaxStdoutBytes: defaultMaxStdoutBytes,
	}
}

type TailRequest struct {
	Session string
	Lines   int
	Panes   []string
	All     bool
}

type SpawnRequest struct {
	Session string
	Claude  int
	Codex   int
	Gemini  int
	Wait    bool
	DryRun  bool
}

type SendRequest struct {
	Session string
	Message string
	Panes   []string
	Type    string
	All     bool
	Track   bool
	DryRun  bool
}

type WaitRequest struct {
	Session   string
	Timeout   string
	Condition string
}

type InterruptRequest struct {
	Session string
	Message string
	Panes   []string
	All     bool
	DryRun  bool
}

type ApprovalRequest struct {
	Token  string
	Reason string
}

type TmuxCaptureRequest struct {
	TargetPane  string
	StartLine   int
	EndLine     int
	JoinWrapped bool
}

type SessionsResponse struct {
	Sessions []SavedSession `json:"sessions"`
	Count    int            `json:"count"`
}

type SavedSession struct {
	Name      string `json:"name,omitempty"`
	ID        string `json:"id,omitempty"`
	Exists    bool   `json:"exists,omitempty"`
	Attached  bool   `json:"attached,omitempty"`
	Windows   int    `json:"windows,omitempty"`
	Panes     int    `json:"panes,omitempty"`
	CreatedAt string `json:"created_at,omitempty"`
	UpdatedAt string `json:"updated_at,omitempty"`
}

type Snapshot struct {
	Success       bool            `json:"success"`
	Timestamp     string          `json:"timestamp,omitempty"`
	Version       string          `json:"version,omitempty"`
	OutputFormat  string          `json:"output_format,omitempty"`
	GeneratedAt   string          `json:"generated_at,omitempty"`
	SafetyProfile string          `json:"safety_profile,omitempty"`
	System        SystemInfo      `json:"system,omitempty"`
	Sessions      []Session       `json:"sessions"`
	Tools         json.RawMessage `json:"tools,omitempty"`
	AgentMail     json.RawMessage `json:"agent_mail,omitempty"`
	Alerts        json.RawMessage `json:"alerts,omitempty"`
	Raw           json.RawMessage `json:"-"`
}

type SystemInfo struct {
	Version       string `json:"version,omitempty"`
	Commit        string `json:"commit,omitempty"`
	BuildDate     string `json:"build_date,omitempty"`
	GoVersion     string `json:"go_version,omitempty"`
	OS            string `json:"os,omitempty"`
	Arch          string `json:"arch,omitempty"`
	TmuxAvailable bool   `json:"tmux_available,omitempty"`
}

type Session struct {
	Name     string  `json:"name,omitempty"`
	ID       string  `json:"id,omitempty"`
	Exists   bool    `json:"exists,omitempty"`
	Attached bool    `json:"attached,omitempty"`
	Windows  int     `json:"windows,omitempty"`
	Panes    []Pane  `json:"panes,omitempty"`
	Agents   []Agent `json:"agents,omitempty"`
}

type Agent struct {
	Pane                 string  `json:"pane,omitempty"`
	Type                 string  `json:"type,omitempty"`
	TypeConfidence       float64 `json:"type_confidence,omitempty"`
	TypeMethod           string  `json:"type_method,omitempty"`
	State                string  `json:"state,omitempty"`
	LastOutputAgeSec     int64   `json:"last_output_age_sec,omitempty"`
	OutputTailLines      int     `json:"output_tail_lines,omitempty"`
	CurrentBead          string  `json:"current_bead,omitempty"`
	PendingMail          int     `json:"pending_mail,omitempty"`
	Window               int     `json:"window,omitempty"`
	PaneIndex            int     `json:"pane_idx,omitempty"`
	IsActive             bool    `json:"is_active,omitempty"`
	PID                  int     `json:"pid,omitempty"`
	ChildPID             int     `json:"child_pid,omitempty"`
	LastOutputTS         string  `json:"last_output_ts,omitempty"`
	ProcessState         string  `json:"process_state,omitempty"`
	ProcessStateName     string  `json:"process_state_name,omitempty"`
	MemoryMB             int     `json:"memory_mb,omitempty"`
	OutputLinesSinceLast int     `json:"output_lines_since_last,omitempty"`
	ContextTokens        int     `json:"context_tokens,omitempty"`
	ContextLimit         int     `json:"context_limit,omitempty"`
	ContextPercent       float64 `json:"context_percent,omitempty"`
	ContextModel         string  `json:"context_model,omitempty"`
}

type Pane struct {
	ID                  string `json:"id,omitempty"`
	Agent               string `json:"agent,omitempty"`
	Program             string `json:"program,omitempty"`
	Model               string `json:"model,omitempty"`
	State               string `json:"state,omitempty"`
	UnifiedState        string `json:"unified_state,omitempty"`
	LastActivityTS      string `json:"last_activity_ts,omitempty"`
	Bead                string `json:"bead,omitempty"`
	IdleSeconds         int64  `json:"idle_seconds,omitempty"`
	WedgeClassification string `json:"wedge_classification,omitempty"`
	Evidence            string `json:"evidence,omitempty"`
}

type TailResponse struct {
	Success    bool                `json:"success"`
	Session    string              `json:"session,omitempty"`
	CapturedAt string              `json:"captured_at,omitempty"`
	Panes      map[string]PaneTail `json:"panes,omitempty"`
	Raw        json.RawMessage     `json:"-"`
}

type PaneTail struct {
	PaneID     string `json:"pane_id,omitempty"`
	PaneIndex  int    `json:"pane_idx,omitempty"`
	Agent      string `json:"agent,omitempty"`
	Output     string `json:"output,omitempty"`
	Text       string `json:"text,omitempty"`
	Truncated  bool   `json:"truncated,omitempty"`
	ByteOffset int64  `json:"byte_offset,omitempty"`
}

type EventEnvelope struct {
	Seq     int64           `json:"seq"`
	TS      string          `json:"ts,omitempty"`
	Kind    string          `json:"kind,omitempty"`
	Payload json.RawMessage `json:"payload,omitempty"`
}

type PaneChunk struct {
	PaneID     string `json:"paneId"`
	Offset     int64  `json:"offset"`
	Bytes      []byte `json:"bytes"`
	Length     int    `json:"length"`
	Seq        int64  `json:"seq,omitempty"`
	Source     string `json:"source"`
	CapturedAt string `json:"capturedAt,omitempty"`
}

type OffsetTracker struct {
	mu      sync.Mutex
	offsets map[string]int64
	now     func() time.Time
}

func NewOffsetTracker(now func() time.Time) *OffsetTracker {
	if now == nil {
		now = time.Now
	}
	return &OffsetTracker{offsets: map[string]int64{}, now: now}
}

func (t *OffsetTracker) Record(paneID string, data []byte, seq int64, source string) PaneChunk {
	t.mu.Lock()
	defer t.mu.Unlock()
	offset := t.offsets[paneID]
	t.offsets[paneID] += int64(len(data))
	return PaneChunk{
		PaneID:     paneID,
		Offset:     offset,
		Bytes:      append([]byte(nil), data...),
		Length:     len(data),
		Seq:        seq,
		Source:     source,
		CapturedAt: t.now().UTC().Format(time.RFC3339Nano),
	}
}

// hp-h5yq: argv builders moved to argv.go (same package). Behavior is
// unchanged; signatures and exported names match for compatibility with
// existing callers and golden-fixture contract tests.

// hp-h5yq: ActionIntent type + intent constructors moved to intents.go.

func (a *Adapter) SessionsList(ctx context.Context) (SessionsResponse, error) {
	raw, err := a.runRawJSON(ctx, SessionsListArgv())
	if err != nil {
		return SessionsResponse{}, err
	}
	return ParseSessionsResponse(raw)
}

func (a *Adapter) SessionDetails(ctx context.Context, session string) (json.RawMessage, error) {
	argv, err := SessionDetailsArgv(session)
	if err != nil {
		return nil, err
	}
	return a.runRawJSON(ctx, argv)
}

func (a *Adapter) Snapshot(ctx context.Context) (Snapshot, error) {
	raw, err := a.runRawJSON(ctx, SnapshotArgv())
	if err != nil {
		return Snapshot{}, err
	}
	return ParseSnapshot(raw)
}

func (a *Adapter) Status(ctx context.Context) (Snapshot, error) {
	raw, err := a.runRawJSON(ctx, StatusArgv())
	if err != nil {
		return Snapshot{}, err
	}
	return ParseSnapshot(raw)
}

func (a *Adapter) Triage(ctx context.Context) (json.RawMessage, error) {
	return a.runRawJSON(ctx, TriageArgv())
}

func (a *Adapter) Tail(ctx context.Context, req TailRequest) (TailResponse, error) {
	argv, err := TailArgv(req)
	if err != nil {
		return TailResponse{}, err
	}
	raw, err := a.runRawJSON(ctx, argv)
	if err != nil {
		return TailResponse{}, err
	}
	return ParseTailResponse(raw)
}

func (a *Adapter) Activity(ctx context.Context, session string) (json.RawMessage, error) {
	argv, err := ActivityArgv(session)
	if err != nil {
		return nil, err
	}
	return a.runRawJSON(ctx, argv)
}

func (a *Adapter) Spawn(ctx context.Context, req SpawnRequest) (json.RawMessage, error) {
	argv, err := SpawnArgv(req)
	if err != nil {
		return nil, err
	}
	return a.runRawJSON(ctx, argv)
}

func (a *Adapter) Send(ctx context.Context, req SendRequest) (json.RawMessage, error) {
	argv, err := SendArgv(req)
	if err != nil {
		return nil, err
	}
	return a.runRawJSON(ctx, argv)
}

func (a *Adapter) Wait(ctx context.Context, req WaitRequest) (json.RawMessage, error) {
	argv, err := WaitArgv(req)
	if err != nil {
		return nil, err
	}
	return a.runRawJSON(ctx, argv)
}

func (a *Adapter) Interrupt(ctx context.Context, req InterruptRequest) (json.RawMessage, error) {
	argv, err := InterruptArgv(req)
	if err != nil {
		return nil, err
	}
	return a.runRawJSON(ctx, argv)
}

func (a *Adapter) ApprovalsList(ctx context.Context) (json.RawMessage, error) {
	return a.runRawJSON(ctx, ApprovalsListArgv())
}

func (a *Adapter) ApprovalShow(ctx context.Context, token string) (json.RawMessage, error) {
	argv, err := ApprovalShowArgv(token)
	if err != nil {
		return nil, err
	}
	return a.runRawJSON(ctx, argv)
}

func (a *Adapter) Approve(ctx context.Context, req ApprovalRequest) (json.RawMessage, error) {
	argv, err := ApproveArgv(req)
	if err != nil {
		return nil, err
	}
	return a.runRawJSON(ctx, argv)
}

func (a *Adapter) Deny(ctx context.Context, req ApprovalRequest) (json.RawMessage, error) {
	argv, err := DenyArgv(req)
	if err != nil {
		return nil, err
	}
	return a.runRawJSON(ctx, argv)
}

func (a *Adapter) CapturePaneFallback(ctx context.Context, req TmuxCaptureRequest) (PaneChunk, error) {
	argv, err := TmuxCaptureArgv(req)
	if err != nil {
		return PaneChunk{}, err
	}
	data, err := a.runText(ctx, argv)
	if err != nil {
		return PaneChunk{}, err
	}
	return PaneChunk{
		PaneID:     strings.TrimSpace(req.TargetPane),
		Offset:     0,
		Bytes:      append([]byte(nil), data...),
		Length:     len(data),
		Source:     "tmux-capture-pane:fallback-mode",
		CapturedAt: a.now().UTC().Format(time.RFC3339Nano),
	}, nil
}

func (a *Adapter) LiveSessions(ctx context.Context) (json.RawMessage, error) {
	return a.liveGETAny(ctx, "/v1/sessions", "/api/sessions")
}

func (a *Adapter) LiveSessionDetails(ctx context.Context, sessionID string) (json.RawMessage, error) {
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return nil, fmt.Errorf("%w: session id is required", ErrInvalidRequest)
	}
	escaped := url.PathEscape(sessionID)
	return a.liveGETAny(ctx, "/v1/sessions/"+escaped, "/api/sessions/"+escaped)
}

func (a *Adapter) LivePaneState(ctx context.Context, paneID string) (json.RawMessage, error) {
	paneID = strings.TrimSpace(paneID)
	if paneID == "" {
		return nil, fmt.Errorf("%w: pane id is required", ErrInvalidRequest)
	}
	escaped := url.PathEscape(paneID)
	return a.liveGETAny(ctx, "/v1/panes/"+escaped+"/state", "/api/panes/"+escaped+"/state")
}

func (a *Adapter) ReadSSE(ctx context.Context, path string, handle func(EventEnvelope) error) error {
	if handle == nil {
		return fmt.Errorf("%w: event handler is required", ErrInvalidRequest)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, a.liveURL(path), nil)
	if err != nil {
		return err
	}
	a.addAuth(req)
	resp, err := a.httpClient().Do(req)
	if err != nil {
		return fmt.Errorf("%w: %s", ErrLiveUnavailable, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return fmt.Errorf("%w: SSE status %d", ErrLiveUnavailable, resp.StatusCode)
	}
	return parseSSE(resp.Body, handle)
}

func (a *Adapter) ReadWebSocket(ctx context.Context, path string, handle func(EventEnvelope) error) error {
	if handle == nil {
		return fmt.Errorf("%w: event handler is required", ErrInvalidRequest)
	}
	header := http.Header{}
	if a.LiveToken != "" {
		header.Set("Authorization", "Bearer "+a.LiveToken)
	}
	conn, _, err := websocket.Dial(ctx, wsURL(a.liveURL(path)), &websocket.DialOptions{HTTPHeader: header})
	if err != nil {
		return fmt.Errorf("%w: websocket dial: %w", ErrLiveUnavailable, err)
	}
	defer conn.Close(websocket.StatusNormalClosure, "")
	for {
		_, data, err := conn.Read(ctx)
		if err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			return err
		}
		var event EventEnvelope
		if err := json.Unmarshal(data, &event); err != nil {
			return fmt.Errorf("ntm: decode websocket event: %w", err)
		}
		if err := handle(event); err != nil {
			return err
		}
	}
}

func (a *Adapter) Probe(ctx context.Context) (*capabilities.ToolReport, error) {
	report := &capabilities.ToolReport{
		Tool:          capabilities.ToolNTM,
		Source:        "cli",
		LastCheckedAt: a.now().UTC().Format(time.RFC3339),
		Capabilities:  missingCapabilities("not probed"),
	}
	versionText, err := a.runText(ctx, VersionArgv())
	if err != nil {
		state := statusForError(err)
		for capID, cap := range report.Capabilities {
			cap.Status = state
			cap.Notes = err.Error()
			report.Capabilities[capID] = cap
		}
		blockPolicyCapabilities(report)
		return report, nil
	}
	version := ParseVersion(string(versionText))
	report.Version = version
	report.Capabilities[CapabilityPresent] = capabilities.Capability{Status: capabilities.StatusOK}
	if !versionAtLeast(version, 1, 5) {
		note := fmt.Sprintf("%v: observed %q, min-compatible is 1.5", ErrUnsupportedVersion, version)
		for capID, cap := range report.Capabilities {
			cap.Status = capabilities.StatusMissing
			cap.Notes = note
			report.Capabilities[capID] = cap
		}
		blockPolicyCapabilities(report)
		return report, nil
	}
	blockPolicyCapabilities(report)
	report.Capabilities[CapabilityServeREST] = capabilities.Capability{Status: capabilities.StatusUntested, Transport: "http", Notes: "ntm serve not configured"}
	report.Capabilities[CapabilityServeSSE] = capabilities.Capability{Status: capabilities.StatusUntested, Transport: "sse", Notes: "ntm serve not configured"}
	report.Capabilities[CapabilityServeWS] = capabilities.Capability{Status: capabilities.StatusUntested, Transport: "ws", Notes: "ntm serve not configured"}
	if a.LiveBaseURL != "" {
		if err := a.probeLive(ctx); err == nil {
			report.Capabilities[CapabilityServeREST] = capabilities.Capability{Status: capabilities.StatusOK, Transport: "http"}
			report.Capabilities[CapabilityServeSSE] = capabilities.Capability{Status: capabilities.StatusUntested, Transport: "sse", Notes: "stream probe deferred until subscription"}
			report.Capabilities[CapabilityServeWS] = capabilities.Capability{Status: capabilities.StatusUntested, Transport: "ws", Notes: "stream probe deferred until subscription"}
			report.Capabilities[CapabilityPanesStream] = capabilities.Capability{Status: capabilities.StatusOK, Transport: "ws,sse", Notes: "ntm serve live stream available; websocket preferred"}
		} else {
			report.Capabilities[CapabilityServeREST] = capabilities.Capability{Status: capabilities.StatusDegraded, Transport: "http", Notes: err.Error()}
		}
	}

	if _, err := a.SessionsList(ctx); err != nil {
		report.Capabilities[CapabilitySessionsList] = capabilities.Capability{Status: statusForError(err), Notes: err.Error(), Transport: "stdio"}
	} else {
		report.Capabilities[CapabilitySessionsList] = capabilities.Capability{Status: capabilities.StatusOK, Transport: "stdio"}
	}
	snapshot, err := a.Snapshot(ctx)
	if err != nil {
		report.Capabilities[CapabilityRobotSnapshot] = capabilities.Capability{Status: statusForError(err), Notes: err.Error(), Transport: "stdio"}
	} else {
		report.Capabilities[CapabilityRobotSnapshot] = capabilities.Capability{Status: capabilities.StatusOK, Transport: "stdio"}
		if len(snapshot.Sessions) > 0 {
			tailCap := probeTail(ctx, a, snapshot.Sessions[0].SessionID())
			report.Capabilities[CapabilityRobotTail] = tailCap
			if tailCap.Status == capabilities.StatusOK && report.Capabilities[CapabilityPanesStream].Status != capabilities.StatusOK {
				report.Capabilities[CapabilityPanesStream] = capabilities.Capability{
					Status:    capabilities.StatusOK,
					Transport: "poll",
					Fallback:  CapabilityRobotTail,
					Notes:     "live stream unavailable; using bounded ntm.robot.tail polling",
				}
			}
		} else {
			report.Capabilities[CapabilityRobotTail] = capabilities.Capability{Status: capabilities.StatusUntested, Transport: "stdio", Notes: "no active sessions to tail"}
		}
	}
	if _, err := a.Status(ctx); err != nil {
		report.Capabilities[CapabilityRobotStatus] = capabilities.Capability{Status: statusForError(err), Notes: err.Error(), Transport: "stdio"}
	} else {
		report.Capabilities[CapabilityRobotStatus] = capabilities.Capability{Status: capabilities.StatusOK, Transport: "stdio"}
	}
	if _, err := a.Triage(ctx); err != nil {
		report.Capabilities[CapabilityRobotTriage] = capabilities.Capability{Status: statusForError(err), Notes: err.Error(), Transport: "stdio"}
	} else {
		report.Capabilities[CapabilityRobotTriage] = capabilities.Capability{Status: capabilities.StatusOK, Transport: "stdio"}
	}
	report.Capabilities[CapabilityRobotActivity] = capabilities.Capability{Status: capabilities.StatusUntested, Transport: "stdio", Notes: "requires active session"}
	if raw, err := a.ApprovalsList(ctx); err != nil {
		report.Capabilities[CapabilityApprovalsList] = capabilities.Capability{Status: statusForError(err), Notes: err.Error(), Transport: "stdio"}
	} else if note, failed := rawReportsFailure(raw); failed {
		report.Capabilities[CapabilityApprovalsList] = capabilities.Capability{Status: capabilities.StatusDegraded, Notes: note, Transport: "stdio"}
	} else {
		report.Capabilities[CapabilityApprovalsList] = capabilities.Capability{Status: capabilities.StatusOK, Transport: "stdio"}
	}
	return report, nil
}

func ParseSessionsResponse(data []byte) (SessionsResponse, error) {
	var raw struct {
		Sessions json.RawMessage `json:"sessions"`
		Count    int             `json:"count"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return SessionsResponse{}, fmt.Errorf("ntm: decode sessions list: %w", err)
	}
	resp := SessionsResponse{Count: raw.Count}
	if len(raw.Sessions) == 0 || bytes.Equal(bytes.TrimSpace(raw.Sessions), []byte("null")) {
		return resp, nil
	}
	if err := json.Unmarshal(raw.Sessions, &resp.Sessions); err != nil {
		return SessionsResponse{}, fmt.Errorf("ntm: decode sessions: %w", err)
	}
	if resp.Count == 0 {
		resp.Count = len(resp.Sessions)
	}
	return resp, nil
}

func ParseSnapshot(data []byte) (Snapshot, error) {
	var snap Snapshot
	if err := json.Unmarshal(data, &snap); err != nil {
		return Snapshot{}, fmt.Errorf("ntm: decode snapshot: %w", err)
	}
	var sessionsRaw struct {
		Sessions json.RawMessage `json:"sessions"`
	}
	if err := json.Unmarshal(data, &sessionsRaw); err != nil {
		return Snapshot{}, fmt.Errorf("ntm: inspect sessions: %w", err)
	}
	if len(sessionsRaw.Sessions) == 0 || bytes.Equal(bytes.TrimSpace(sessionsRaw.Sessions), []byte("null")) {
		snap.Sessions = nil
	}
	for i := range snap.Sessions {
		for j := range snap.Sessions[i].Panes {
			pane := &snap.Sessions[i].Panes[j]
			pane.UnifiedState = MapRobotState(pane.State, pane.WedgeClassification)
		}
	}
	snap.Raw = append(json.RawMessage(nil), data...)
	return snap, nil
}

func ParseTailResponse(data []byte) (TailResponse, error) {
	var tail TailResponse
	if err := json.Unmarshal(data, &tail); err != nil {
		return TailResponse{}, fmt.Errorf("ntm: decode tail: %w", err)
	}
	tail.Raw = append(json.RawMessage(nil), data...)
	return tail, nil
}

func MapRobotState(state, wedge string) string {
	if strings.EqualFold(strings.TrimSpace(wedge), "wedged") {
		return "wedged"
	}
	switch strings.ToUpper(strings.TrimSpace(state)) {
	case "IDLE":
		return "idle"
	case "TYPING":
		return "typing"
	case "THINKING":
		return "thinking"
	case "TOOL_USE", "TOOL-USE", "TOOLUSE":
		return "tool_use"
	case "COMPLETE", "COMPLETED", "DONE":
		return "complete"
	case "ERROR", "FAILED":
		return "error"
	case "ACTIVE", "RUNNING", "WORKING":
		return "working"
	case "":
		return "unknown"
	default:
		return strings.ToLower(strings.TrimSpace(state))
	}
}

func (s Session) SessionID() string {
	if strings.TrimSpace(s.Name) != "" {
		return strings.TrimSpace(s.Name)
	}
	return strings.TrimSpace(s.ID)
}

func (s Session) NormalizedPanes() []Pane {
	out := append([]Pane(nil), s.Panes...)
	for _, agent := range s.Agents {
		pane := Pane{
			ID:             agent.Pane,
			Program:        agent.Type,
			State:          agent.State,
			LastActivityTS: agent.LastOutputTS,
			Bead:           agent.CurrentBead,
		}
		pane.UnifiedState = MapRobotState(pane.State, "")
		out = append(out, pane)
	}
	return out
}

func ParseVersion(text string) string {
	for _, line := range strings.Split(text, "\n") {
		fields := strings.Fields(strings.TrimSpace(line))
		for _, field := range fields {
			field = strings.TrimPrefix(field, "v")
			if looksLikeVersion(field) {
				return field
			}
		}
	}
	return strings.TrimSpace(text)
}

func (a *Adapter) runRawJSON(ctx context.Context, argv []string) (json.RawMessage, error) {
	result, err := a.run(ctx, argv)
	if err != nil {
		return nil, err
	}
	if len(bytes.TrimSpace(result.Stdout)) == 0 {
		return nil, fmt.Errorf("ntm: empty JSON response from %v", argv)
	}
	if max := a.maxStdoutBytes(); max > 0 && len(result.Stdout) > max {
		return nil, outputTooLargeError{limit: max, got: len(result.Stdout), argv: argv}
	}
	var raw json.RawMessage
	if err := json.Unmarshal(result.Stdout, &raw); err != nil {
		return nil, fmt.Errorf("ntm: decode JSON from %v: %w", argv, err)
	}
	return raw, nil
}

func (a *Adapter) runText(ctx context.Context, argv []string) ([]byte, error) {
	result, err := a.run(ctx, argv)
	if err != nil {
		return nil, err
	}
	if max := a.maxStdoutBytes(); max > 0 && len(result.Stdout) > max {
		return nil, outputTooLargeError{limit: max, got: len(result.Stdout), argv: argv}
	}
	return result.Stdout, nil
}

func (a *Adapter) run(ctx context.Context, argv []string) (CommandResult, error) {
	if a == nil {
		return CommandResult{}, fmt.Errorf("%w: nil adapter", ErrInvalidRequest)
	}
	runner := a.Runner
	if runner == nil {
		runner = ExecRunner{}
	}
	result, err := runner.Run(ctx, argv)
	if err != nil {
		return CommandResult{}, fmt.Errorf("ntm: run %s: %w", argv[0], err)
	}
	if result.ExitCode != 0 {
		return CommandResult{}, commandError{argv: argv, result: result}
	}
	return result, nil
}

func (a *Adapter) liveGET(ctx context.Context, path string) (json.RawMessage, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, a.liveURL(path), nil)
	if err != nil {
		return nil, err
	}
	a.addAuth(req)
	resp, err := a.httpClient().Do(req)
	if err != nil {
		return nil, fmt.Errorf("%w: %s", ErrLiveUnavailable, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return nil, fmt.Errorf("%w: GET %s status %d", ErrLiveUnavailable, path, resp.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, int64(a.maxStdoutBytes()+1)))
	if err != nil {
		return nil, fmt.Errorf("ntm: read live response: %w", err)
	}
	if max := a.maxStdoutBytes(); max > 0 && len(data) > max {
		return nil, outputTooLargeError{limit: max, got: len(data), argv: []string{"GET", path}}
	}
	var raw json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("ntm: decode live JSON: %w", err)
	}
	return raw, nil
}

func (a *Adapter) liveGETAny(ctx context.Context, paths ...string) (json.RawMessage, error) {
	var lastErr error
	for _, path := range paths {
		raw, err := a.liveGET(ctx, path)
		if err == nil {
			return raw, nil
		}
		lastErr = err
	}
	if lastErr == nil {
		lastErr = fmt.Errorf("%w: no live paths configured", ErrLiveUnavailable)
	}
	return nil, lastErr
}

func (a *Adapter) probeLive(ctx context.Context) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, a.liveURL("/health"), nil)
	if err != nil {
		return err
	}
	a.addAuth(req)
	resp, err := a.httpClient().Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return fmt.Errorf("health status %d", resp.StatusCode)
	}
	return nil
}

func parseSSE(reader io.Reader, handle func(EventEnvelope) error) error {
	scanner := bufio.NewScanner(reader)
	scanner.Buffer(make([]byte, 64*1024), 1024*1024)
	var data strings.Builder
	flush := func() error {
		if data.Len() == 0 {
			return nil
		}
		var event EventEnvelope
		if err := json.Unmarshal([]byte(data.String()), &event); err != nil {
			return fmt.Errorf("ntm: decode SSE event: %w", err)
		}
		data.Reset()
		return handle(event)
	}
	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			if err := flush(); err != nil {
				return err
			}
			continue
		}
		if strings.HasPrefix(line, "data:") {
			if data.Len() > 0 {
				data.WriteByte('\n')
			}
			data.WriteString(strings.TrimSpace(strings.TrimPrefix(line, "data:")))
		}
	}
	if err := scanner.Err(); err != nil {
		return err
	}
	return flush()
}

type commandError struct {
	argv   []string
	result CommandResult
}

func (e commandError) Error() string {
	stderr := strings.TrimSpace(string(e.result.Stderr))
	if stderr == "" {
		stderr = strings.TrimSpace(string(e.result.Stdout))
	}
	return fmt.Sprintf("ntm: command %v exited %d: %s", e.argv, e.result.ExitCode, stderr)
}

type outputTooLargeError struct {
	limit int
	got   int
	argv  []string
}

func (e outputTooLargeError) Error() string {
	return fmt.Sprintf("%v: %v produced %d bytes, limit %d", ErrOutputTooLarge, e.argv, e.got, e.limit)
}

func (e outputTooLargeError) Unwrap() error {
	return ErrOutputTooLarge
}

func statusForError(err error) capabilities.CapabilityStatus {
	var commandErr commandError
	if errors.As(err, &commandErr) {
		switch commandErr.result.ExitCode {
		case 124:
			return capabilities.StatusDegraded
		case 127:
			return capabilities.StatusMissing
		default:
			return capabilities.StatusDegraded
		}
	}
	if errors.Is(err, ErrOutputTooLarge) || strings.Contains(err.Error(), "decode JSON") {
		return capabilities.StatusDegraded
	}
	if errors.Is(err, exec.ErrNotFound) || strings.Contains(err.Error(), "executable file not found") || strings.Contains(err.Error(), "command not found") {
		return capabilities.StatusMissing
	}
	return capabilities.StatusDegraded
}

func missingCapabilities(note string) map[string]capabilities.Capability {
	caps := map[string]capabilities.Capability{}
	for _, capID := range []string{
		CapabilityPresent,
		CapabilitySessionsList,
		CapabilitySessionsSpawn,
		CapabilitySessionsTerminate,
		CapabilitySessionsAttach,
		CapabilityPanesStream,
		CapabilityServeREST,
		CapabilityServeSSE,
		CapabilityServeWS,
		CapabilityRobotSnapshot,
		CapabilityRobotStatus,
		CapabilityRobotTail,
		CapabilityRobotTriage,
		CapabilityRobotActivity,
		CapabilityApprovalsList,
		CapabilityApprovalsApprove,
		CapabilityApprovalsDeny,
		CapabilitySwarmHalt,
		CapabilitySpawn,
		CapabilitySendMarchingOrders,
		CapabilityPaneKill,
	} {
		caps[capID] = capabilities.Capability{Status: capabilities.StatusMissing, Notes: note}
	}
	return caps
}

func blockPolicyCapabilities(report *capabilities.ToolReport) {
	for _, capID := range []string{
		CapabilitySessionsSpawn,
		CapabilitySessionsTerminate,
		CapabilitySessionsAttach,
		CapabilityApprovalsApprove,
		CapabilityApprovalsDeny,
		CapabilitySwarmHalt,
		CapabilitySpawn,
		CapabilitySendMarchingOrders,
		CapabilityPaneKill,
	} {
		report.Capabilities[capID] = capabilities.Capability{
			Status: capabilities.StatusBlockedByPolicy,
			Notes:  "mutating NTM operation; executable only through daemon ActionPlan/job policy",
		}
	}
}

func probeTail(ctx context.Context, adapter *Adapter, session string) capabilities.Capability {
	if strings.TrimSpace(session) == "" {
		return capabilities.Capability{Status: capabilities.StatusUntested, Transport: "stdio", Notes: "no session id available"}
	}
	if _, err := adapter.Tail(ctx, TailRequest{Session: session, Lines: 1}); err != nil {
		return capabilities.Capability{Status: statusForError(err), Notes: err.Error(), Transport: "stdio"}
	}
	return capabilities.Capability{Status: capabilities.StatusOK, Transport: "stdio"}
}

func rawReportsFailure(raw json.RawMessage) (string, bool) {
	var response struct {
		Success *bool  `json:"success"`
		Error   string `json:"error"`
	}
	if err := json.Unmarshal(raw, &response); err != nil {
		return "", false
	}
	if response.Success != nil && !*response.Success {
		note := strings.TrimSpace(response.Error)
		if note == "" {
			note = "response reported success=false"
		}
		return note, true
	}
	if strings.TrimSpace(response.Error) != "" {
		return strings.TrimSpace(response.Error), true
	}
	return "", false
}

func versionAtLeast(version string, major, minor int) bool {
	parts := strings.Split(version, ".")
	if len(parts) < 2 {
		return false
	}
	gotMajor, err := strconv.Atoi(strings.TrimLeft(parts[0], "v"))
	if err != nil {
		return false
	}
	gotMinor, err := strconv.Atoi(parts[1])
	if err != nil {
		return false
	}
	return gotMajor > major || (gotMajor == major && gotMinor >= minor)
}

func looksLikeVersion(value string) bool {
	parts := strings.Split(value, ".")
	if len(parts) < 2 {
		return false
	}
	for _, part := range parts[:2] {
		if _, err := strconv.Atoi(strings.TrimLeft(part, "v")); err != nil {
			return false
		}
	}
	return true
}

func trimNonEmpty(values []string) []string {
	out := values[:0]
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" {
			out = append(out, value)
		}
	}
	return out
}

func (a *Adapter) liveURL(path string) string {
	base := strings.TrimRight(a.LiveBaseURL, "/")
	if base == "" {
		base = "http://127.0.0.1:7337"
	}
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	return base + path
}

func (a *Adapter) httpClient() HTTPDoer {
	if a != nil && a.HTTPClient != nil {
		return a.HTTPClient
	}
	return http.DefaultClient
}

func (a *Adapter) addAuth(req *http.Request) {
	if a != nil && a.LiveToken != "" {
		req.Header.Set("Authorization", "Bearer "+a.LiveToken)
	}
}

func (a *Adapter) now() time.Time {
	if a != nil && a.Now != nil {
		return a.Now()
	}
	return time.Now()
}

func (a *Adapter) maxStdoutBytes() int {
	if a == nil || a.MaxStdoutBytes == 0 {
		return defaultMaxStdoutBytes
	}
	if a.MaxStdoutBytes < 0 {
		return 0
	}
	return a.MaxStdoutBytes
}

func wsURL(httpURL string) string {
	if strings.HasPrefix(httpURL, "https://") {
		return "wss://" + strings.TrimPrefix(httpURL, "https://")
	}
	if strings.HasPrefix(httpURL, "http://") {
		return "ws://" + strings.TrimPrefix(httpURL, "http://")
	}
	return httpURL
}
