// Package ntm wraps Named Tmux Manager's robot and live-server surfaces.
//
// Adapter precedence follows docs/integration-contracts/ntm.md:
// ntm serve REST/SSE/WS first, ntm --robot-* fallback second, tmux capture
// last. Mutating operations are exposed only as typed argv/action metadata so
// the daemon can route them through ActionPlan policy.
package ntm

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os/exec"
	"strings"
	"sync"
	"time"
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

// hp-h5yq: CLI-transport Adapter methods moved to client_cli.go.

// hp-h5yq: live-transport Adapter methods (LiveSessions, LiveSessionDetails,
// LivePaneState, ReadSSE, ReadWebSocket) moved to client_live.go.

// hp-h5yq: Probe + capability classification helpers moved to capabilities.go.
// hp-h5yq: parsers + Session-method receivers + ParseVersion moved to parsers.go.

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

// hp-h5yq: liveGET / liveGETAny / probeLive / parseSSE moved to client_live.go
// (probeLive now sits with the rest of the live transport in capabilities.go's
// nearby relative — kept under capabilities.go because it's the live-probe
// half of the capability detection).

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

// hp-h5yq: statusForError + missingCapabilities + blockPolicyCapabilities +
// probeTail + rawReportsFailure + versionAtLeast moved to capabilities.go.
// hp-h5yq: looksLikeVersion moved to parsers.go.

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

// hp-h5yq: liveURL / httpClient / addAuth moved to client_live.go.

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

// hp-h5yq: wsURL moved to client_live.go.
