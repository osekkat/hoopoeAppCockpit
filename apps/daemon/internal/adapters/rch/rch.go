// Package rch wraps Remote Compilation Helper as Hoopoe's preferred
// build/test offload adapter. It constructs argv directly, never invokes a
// shell, and records enough normalized metadata for the future build queue to
// dedupe repeated failures across rch and direct execution paths.
package rch

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/hoopoe-cockpit/hoopoe/apps/daemon/internal/capabilities"
)

const (
	ToolName = "rch"

	CapabilityRun = "rch.run"

	defaultMaxOutputBytes = 8 << 20
	defaultTimeout        = 30 * time.Minute
)

var (
	ErrInvalidRequest  = errors.New("rch: invalid request")
	ErrMissingBinary   = errors.New("rch: binary not found")
	ErrCommandContract = errors.New("rch: command contract violation")
)

type Runner interface {
	Run(ctx context.Context, invocation Invocation) (CommandResult, error)
}

type Invocation struct {
	Argv    []string
	Dir     string
	Env     []string
	Timeout time.Duration
}

type CommandResult struct {
	ExitCode int
	Stdout   []byte
	Stderr   []byte
}

type ExecRunner struct{}

func (ExecRunner) Run(ctx context.Context, invocation Invocation) (CommandResult, error) {
	if len(invocation.Argv) == 0 || strings.TrimSpace(invocation.Argv[0]) == "" {
		return CommandResult{}, fmt.Errorf("%w: empty argv", ErrInvalidRequest)
	}
	timeout := invocation.Timeout
	if timeout <= 0 {
		timeout = defaultTimeout
	}
	runCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	cmd := exec.CommandContext(runCtx, invocation.Argv[0], invocation.Argv[1:]...)
	cmd.Dir = invocation.Dir
	if len(invocation.Env) > 0 {
		cmd.Env = append(os.Environ(), invocation.Env...)
	}
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
	if isExecNotFoundErr(err) {
		result.ExitCode = -1
		return result, ErrMissingBinary
	}
	result.ExitCode = -1
	return result, err
}

type Adapter struct {
	Runner         Runner
	Now            func() time.Time
	MaxOutputBytes int
}

func New(runner Runner) *Adapter {
	if runner == nil {
		runner = ExecRunner{}
	}
	return &Adapter{Runner: runner, Now: time.Now, MaxOutputBytes: defaultMaxOutputBytes}
}

type RunRequest struct {
	ProjectID     string
	WorktreePath  string
	Branch        string
	CommitSHA     string
	Command       []string
	Env           map[string]string
	RunnerProfile string
	WorkerTarget  string
	Timeout       time.Duration
}

type RunResult struct {
	ProjectID          string        `json:"projectId,omitempty"`
	WorktreePath       string        `json:"worktreePath"`
	Branch             string        `json:"branch,omitempty"`
	CommitSHA          string        `json:"commitSha,omitempty"`
	Command            []string      `json:"command"`
	NormalizedArgv     []string      `json:"normalizedArgv"`
	EnvironmentDigest  string        `json:"environmentDigest"`
	RunnerProfile      string        `json:"runnerProfile,omitempty"`
	WorkerTarget       string        `json:"workerTarget,omitempty"`
	StartedAt          time.Time     `json:"startedAt"`
	CompletedAt        time.Time     `json:"completedAt"`
	Duration           time.Duration `json:"duration"`
	ExitCode           int           `json:"exitCode"`
	Stdout             string        `json:"stdout,omitempty"`
	Stderr             string        `json:"stderr,omitempty"`
	OutputTruncated    bool          `json:"outputTruncated,omitempty"`
	Summary            RCHSummary    `json:"summary"`
	FailureFingerprint string        `json:"failureFingerprint,omitempty"`
}

type RCHSummary struct {
	Mode        string        `json:"mode,omitempty"`
	Worker      string        `json:"worker,omitempty"`
	FailureCode string        `json:"failureCode,omitempty"`
	QueueWait   time.Duration `json:"queueWait,omitempty"`
	ExecTime    time.Duration `json:"execTime,omitempty"`
	RawLine     string        `json:"rawLine,omitempty"`
}

func VersionArgv() []string {
	return []string{ToolName, "--version"}
}

func RunArgv(req RunRequest) ([]string, error) {
	command, err := normalizeCommand(req.Command)
	if err != nil {
		return nil, err
	}
	return append([]string{ToolName, "exec", "--"}, command...), nil
}

func (a *Adapter) Run(ctx context.Context, req RunRequest) (RunResult, error) {
	if a == nil {
		return RunResult{}, fmt.Errorf("%w: nil adapter", ErrInvalidRequest)
	}
	worktree, err := normalizeWorktree(req.WorktreePath)
	if err != nil {
		return RunResult{}, err
	}
	command, err := normalizeCommand(req.Command)
	if err != nil {
		return RunResult{}, err
	}
	argv := append([]string{ToolName, "exec", "--"}, command...)
	env := invocationEnv(req.Env)
	started := a.now().UTC()
	invocation := Invocation{
		Argv:    argv,
		Dir:     worktree,
		Env:     env,
		Timeout: req.Timeout,
	}
	result, runErr := a.runner().Run(ctx, invocation)
	completed := a.now().UTC()
	if runErr != nil {
		if errors.Is(runErr, ErrMissingBinary) {
			return RunResult{}, runErr
		}
		return RunResult{}, fmt.Errorf("rch: run: %w", runErr)
	}
	stdout, stdoutTruncated := trimOutput(result.Stdout, a.maxOutputBytes())
	stderr, stderrTruncated := trimOutput(result.Stderr, a.maxOutputBytes())
	summary := ParseSummary(append(append([]byte(nil), result.Stderr...), result.Stdout...))
	if summary.Worker == "" && req.WorkerTarget != "" {
		summary.Worker = req.WorkerTarget
	}
	out := RunResult{
		ProjectID:         strings.TrimSpace(req.ProjectID),
		WorktreePath:      worktree,
		Branch:            strings.TrimSpace(req.Branch),
		CommitSHA:         strings.TrimSpace(req.CommitSHA),
		Command:           command,
		NormalizedArgv:    argv,
		EnvironmentDigest: EnvironmentDigest(req.Env),
		RunnerProfile:     strings.TrimSpace(req.RunnerProfile),
		WorkerTarget:      summary.Worker,
		StartedAt:         started,
		CompletedAt:       completed,
		Duration:          completed.Sub(started),
		ExitCode:          result.ExitCode,
		Stdout:            string(stdout),
		Stderr:            string(stderr),
		OutputTruncated:   stdoutTruncated || stderrTruncated,
		Summary:           summary,
	}
	if out.ExitCode != 0 {
		out.FailureFingerprint = FailureFingerprint(out)
	}
	return out, nil
}

func (a *Adapter) Version(ctx context.Context) (string, error) {
	result, err := a.runner().Run(ctx, Invocation{
		Argv:    VersionArgv(),
		Env:     baseEnv(nil),
		Timeout: 5 * time.Second,
	})
	if err != nil {
		return "", err
	}
	if result.ExitCode != 0 {
		return "", commandError{argv: VersionArgv(), result: result}
	}
	version := ParseVersion(result.Stdout)
	if version == "" {
		return "", fmt.Errorf("%w: missing version", ErrCommandContract)
	}
	return version, nil
}

func (a *Adapter) Probe(ctx context.Context) (*capabilities.ToolReport, error) {
	report := &capabilities.ToolReport{
		Tool:          capabilities.ToolRCH,
		Source:        "cli",
		LastCheckedAt: a.now().UTC().Format(time.RFC3339),
		Capabilities: map[string]capabilities.Capability{
			CapabilityRun: {Status: capabilities.StatusMissing},
		},
	}
	version, err := a.Version(ctx)
	if err != nil {
		report.Capabilities[CapabilityRun] = capabilities.Capability{
			Status: statusForError(err),
			Notes:  err.Error(),
		}
		return report, nil
	}
	report.Version = version
	report.Capabilities[CapabilityRun] = capabilities.Capability{
		Status:    capabilities.StatusOK,
		Transport: "stdio",
	}
	return report, nil
}

func ParseVersion(out []byte) string {
	fields := strings.Fields(strings.TrimSpace(string(out)))
	for i, field := range fields {
		cleaned := strings.TrimPrefix(strings.TrimPrefix(field, "v"), "version")
		if looksLikeVersion(cleaned) {
			return cleaned
		}
		if (field == "version" || field == "v") && i+1 < len(fields) {
			next := strings.TrimPrefix(fields[i+1], "v")
			if looksLikeVersion(next) {
				return next
			}
		}
	}
	return ""
}

func ParseSummary(output []byte) RCHSummary {
	lines := strings.Split(string(output), "\n")
	for _, line := range lines {
		line = stripANSI(strings.TrimSpace(line))
		if !strings.HasPrefix(line, "[RCH]") {
			continue
		}
		summary := RCHSummary{RawLine: line}
		fields := strings.Fields(line)
		if len(fields) >= 2 {
			summary.Mode = fields[1]
		}
		if summary.Mode == "local" {
			summary.Worker = localReason(line, fields)
		} else if len(fields) >= 3 && summary.Mode == "remote" {
			summary.Worker = strings.Trim(fields[2], "()")
		}
		if idx := strings.Index(line, "[RCH-"); idx >= 0 {
			end := strings.Index(line[idx:], "]")
			if end > 0 {
				summary.FailureCode = line[idx+1 : idx+end]
			}
		}
		summary.QueueWait = parseNamedDuration(line, "queue")
		summary.ExecTime = parseNamedDuration(line, "exec")
		return summary
	}
	return RCHSummary{}
}

func EnvironmentDigest(env map[string]string) string {
	keys := make([]string, 0, len(env))
	for key := range env {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	h := sha256.New()
	for _, key := range keys {
		h.Write([]byte(key))
		h.Write([]byte{0})
		h.Write([]byte(env[key]))
		h.Write([]byte{0})
	}
	return hex.EncodeToString(h.Sum(nil))
}

func FailureFingerprint(result RunResult) string {
	h := sha256.New()
	for _, arg := range result.Command {
		h.Write([]byte(arg))
		h.Write([]byte{0})
	}
	h.Write([]byte(strconv.Itoa(result.ExitCode)))
	h.Write([]byte{0})
	h.Write([]byte(result.EnvironmentDigest))
	h.Write([]byte{0})
	h.Write([]byte(firstLine(result.Stderr)))
	h.Write([]byte{0})
	h.Write([]byte(firstLine(result.Stdout)))
	return hex.EncodeToString(h.Sum(nil))
}

func normalizeCommand(command []string) ([]string, error) {
	if len(command) == 0 {
		return nil, fmt.Errorf("%w: command is required", ErrInvalidRequest)
	}
	out := make([]string, 0, len(command))
	for i, arg := range command {
		arg = strings.TrimSpace(arg)
		if arg == "" {
			return nil, fmt.Errorf("%w: command arg %d is empty", ErrInvalidRequest, i)
		}
		if i == 0 && isShellBinary(arg) {
			return nil, fmt.Errorf("%w: shell commands are not accepted", ErrInvalidRequest)
		}
		if strings.Contains(arg, "\x00") || strings.Contains(arg, "\n") || strings.Contains(arg, "\r") {
			return nil, fmt.Errorf("%w: command arg %d contains control characters", ErrInvalidRequest, i)
		}
		out = append(out, arg)
	}
	return out, nil
}

func normalizeWorktree(path string) (string, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return "", fmt.Errorf("%w: worktreePath is required", ErrInvalidRequest)
	}
	if !filepath.IsAbs(path) {
		return "", fmt.Errorf("%w: worktreePath must be absolute", ErrInvalidRequest)
	}
	cleaned := filepath.Clean(path)
	if cleaned == string(filepath.Separator) {
		return "", fmt.Errorf("%w: worktreePath cannot be filesystem root", ErrInvalidRequest)
	}
	return cleaned, nil
}

func invocationEnv(env map[string]string) []string {
	return baseEnv(env)
}

func baseEnv(env map[string]string) []string {
	out := []string{"LC_ALL=C", "LANG=C", "NO_COLOR=1", "RCH_VISIBILITY=summary"}
	keys := make([]string, 0, len(env))
	for key := range env {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		if validEnvKey(key) {
			out = append(out, key+"="+env[key])
		}
	}
	return out
}

func validEnvKey(key string) bool {
	if key == "" {
		return false
	}
	for i, r := range key {
		switch {
		case r >= 'A' && r <= 'Z':
		case r >= 'a' && r <= 'z':
		case r >= '0' && r <= '9' && i > 0:
		case r == '_' && i > 0:
		default:
			return false
		}
	}
	return true
}

func trimOutput(in []byte, limit int) ([]byte, bool) {
	if limit <= 0 {
		limit = defaultMaxOutputBytes
	}
	if len(in) <= limit {
		return append([]byte(nil), in...), false
	}
	return append([]byte(nil), in[len(in)-limit:]...), true
}

func (a *Adapter) runner() Runner {
	if a == nil || a.Runner == nil {
		return ExecRunner{}
	}
	return a.Runner
}

func (a *Adapter) now() time.Time {
	if a != nil && a.Now != nil {
		return a.Now().UTC()
	}
	return time.Now().UTC()
}

func (a *Adapter) maxOutputBytes() int {
	if a != nil && a.MaxOutputBytes > 0 {
		return a.MaxOutputBytes
	}
	return defaultMaxOutputBytes
}

func statusForError(err error) capabilities.CapabilityStatus {
	switch {
	case errors.Is(err, ErrMissingBinary):
		return capabilities.StatusMissing
	default:
		return capabilities.StatusDegraded
	}
}

type commandError struct {
	argv   []string
	result CommandResult
}

func (e commandError) Error() string {
	detail := strings.TrimSpace(string(e.result.Stderr))
	if detail == "" {
		detail = strings.TrimSpace(string(e.result.Stdout))
	}
	return fmt.Sprintf("rch: command %v exited %d: %s", e.argv, e.result.ExitCode, detail)
}

func isShellBinary(arg string) bool {
	base := filepath.Base(arg)
	switch base {
	case "sh", "bash", "zsh", "fish", "dash":
		return true
	}
	return false
}

func isExecNotFoundErr(err error) bool {
	var execErr *exec.Error
	if errors.As(err, &execErr) && errors.Is(execErr.Err, exec.ErrNotFound) {
		return true
	}
	return errors.Is(err, exec.ErrNotFound)
}

func looksLikeVersion(s string) bool {
	if s == "" {
		return false
	}
	dot := false
	for _, r := range s {
		switch {
		case r >= '0' && r <= '9':
		case r == '.':
			dot = true
		default:
			return false
		}
	}
	return dot
}

func stripANSI(line string) string {
	var out strings.Builder
	for i := 0; i < len(line); i++ {
		ch := line[i]
		if ch != 0x1b {
			out.WriteByte(ch)
			continue
		}
		i++
		if i >= len(line) {
			break
		}
		if line[i] == '[' {
			i++
			for i < len(line) {
				if line[i] >= '@' && line[i] <= '~' {
					break
				}
				i++
			}
			continue
		}
	}
	return out.String()
}

func localReason(line string, fields []string) string {
	start := strings.Index(line, "(")
	end := strings.LastIndex(line, ")")
	if start >= 0 && end > start {
		return strings.TrimSpace(line[start+1 : end])
	}
	if len(fields) >= 3 {
		return strings.Trim(fields[2], "()")
	}
	return ""
}

func parseNamedDuration(line string, label string) time.Duration {
	idx := strings.Index(line, label+" ")
	if idx < 0 {
		idx = strings.Index(line, label+"=")
	}
	if idx < 0 {
		return 0
	}
	rest := strings.TrimLeft(line[idx+len(label):], " =")
	fields := strings.Fields(strings.Trim(rest, "(),"))
	if len(fields) == 0 {
		return 0
	}
	raw := strings.Trim(fields[0], "(),")
	duration, err := time.ParseDuration(raw)
	if err != nil {
		return 0
	}
	return duration
}

func firstLine(text string) string {
	line, _, _ := strings.Cut(strings.TrimSpace(text), "\n")
	if len(line) > 512 {
		return line[:512]
	}
	return line
}
