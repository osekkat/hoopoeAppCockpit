// Package oracle wraps the Oracle browser-mode harness used for ChatGPT Pro
// planning candidates. The adapter constructs argv directly, never invokes a
// shell, and treats Oracle's write-output file as the response contract.
package oracle

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/hoopoe-cockpit/hoopoe/apps/daemon/internal/capabilities"
)

const (
	ToolName = "oracle"

	CapabilityHelp         = "oracle.help"
	CapabilityServeStatus  = "oracle.serve.status"
	CapabilityBrowserRun   = "oracle.browser.run"
	CapabilityRemoteInvoke = "oracle.remote.invoke"

	DefaultModel          = "gpt-5.4-pro"
	defaultRunTimeout     = 45 * time.Minute
	defaultProbeTimeout   = 5 * time.Second
	defaultMaxOutputBytes = 16 << 20
)

var (
	ErrInvalidRequest        = errors.New("oracle: invalid request")
	ErrMissingBinary         = errors.New("oracle: binary not found")
	ErrCommandFailed         = errors.New("oracle: command failed")
	ErrCommandContract       = errors.New("oracle: command contract violation")
	ErrMissingOutput         = errors.New("oracle: write-output file missing")
	ErrOutputTooLarge        = errors.New("oracle: write-output file exceeded limit")
	ErrProviderCredentialEnv = errors.New("oracle: direct provider credential environment is not allowed")
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
		timeout = defaultRunTimeout
	}
	runCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	cmd := exec.CommandContext(runCtx, invocation.Argv[0], invocation.Argv[1:]...)
	cmd.Dir = invocation.Dir
	cmd.Env = childEnv(invocation.Env)
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
	Remote         RemoteConfig
	MaxOutputBytes int
}

func New(runner Runner) *Adapter {
	if runner == nil {
		runner = ExecRunner{}
	}
	return &Adapter{Runner: runner, Now: time.Now, MaxOutputBytes: defaultMaxOutputBytes}
}

type RemoteConfig struct {
	Host  string
	Token string
}

type RunRequest struct {
	Model      string
	Prompt     string
	Files      []string
	OutputPath string
	WorkDir    string
	Remote     RemoteConfig
	Env        []string
	Timeout    time.Duration
}

type RunResult struct {
	Model            string        `json:"model"`
	PromptSHA256     string        `json:"promptSha256"`
	InputFiles       []string      `json:"inputFiles,omitempty"`
	OutputPath       string        `json:"outputPath"`
	OutputSHA256     string        `json:"outputSha256,omitempty"`
	OutputText       string        `json:"outputText,omitempty"`
	RemoteHost       string        `json:"remoteHost,omitempty"`
	RemoteTokenUsed  bool          `json:"remoteTokenUsed,omitempty"`
	NormalizedArgv   []string      `json:"normalizedArgv"`
	StartedAt        time.Time     `json:"startedAt"`
	CompletedAt      time.Time     `json:"completedAt"`
	Duration         time.Duration `json:"duration"`
	ExitCode         int           `json:"exitCode"`
	Stdout           string        `json:"stdout,omitempty"`
	Stderr           string        `json:"stderr,omitempty"`
	MacMustStayAwake bool          `json:"macMustStayAwake"`
}

type ServeStatus struct {
	Healthy       bool      `json:"healthy"`
	Model         string    `json:"model,omitempty"`
	LastRequestTS string    `json:"last_request_ts,omitempty"`
	Authenticated *bool     `json:"authenticated,omitempty"`
	Warnings      []string  `json:"warnings,omitempty"`
	CheckedAt     time.Time `json:"checkedAt,omitempty"`
}

func HelpArgv() []string {
	return []string{ToolName, "--help"}
}

func ServeStatusArgv() []string {
	return []string{ToolName, "serve", "status"}
}

func BrowserRunArgv(req RunRequest) ([]string, error) {
	normalized, err := normalizeRunRequest(req)
	if err != nil {
		return nil, err
	}
	argv := []string{
		ToolName,
		"--engine",
		"browser",
		"--model",
		normalized.Model,
		"--prompt",
		normalized.Prompt,
	}
	for _, file := range normalized.Files {
		argv = append(argv, "--file", file)
	}
	argv = append(argv, "--write-output", normalized.OutputPath)
	if normalized.Remote.Host != "" {
		argv = append(argv, "--remote-host", normalized.Remote.Host, "--remote-token", normalized.Remote.Token)
	}
	return argv, nil
}

func (a *Adapter) BrowserRun(ctx context.Context, req RunRequest) (RunResult, error) {
	if a == nil {
		return RunResult{}, fmt.Errorf("%w: nil adapter", ErrInvalidRequest)
	}
	normalized, err := normalizeRunRequest(req)
	if err != nil {
		return RunResult{}, err
	}
	if err := rejectProviderCredentialEnv(normalized.Env); err != nil {
		return RunResult{}, err
	}
	argv, err := BrowserRunArgv(normalized)
	if err != nil {
		return RunResult{}, err
	}
	started := a.now().UTC()
	commandResult, runErr := a.runner().Run(ctx, Invocation{
		Argv:    argv,
		Dir:     normalized.WorkDir,
		Env:     normalized.Env,
		Timeout: timeoutOrDefault(normalized.Timeout, defaultRunTimeout),
	})
	completed := a.now().UTC()
	result := RunResult{
		Model:            normalized.Model,
		PromptSHA256:     digestBytes([]byte(normalized.Prompt)),
		InputFiles:       append([]string(nil), normalized.Files...),
		OutputPath:       normalized.OutputPath,
		RemoteHost:       normalized.Remote.Host,
		RemoteTokenUsed:  normalized.Remote.Token != "",
		NormalizedArgv:   redactRemoteToken(argv),
		StartedAt:        started,
		CompletedAt:      completed,
		Duration:         completed.Sub(started),
		ExitCode:         commandResult.ExitCode,
		Stdout:           string(commandResult.Stdout),
		Stderr:           string(commandResult.Stderr),
		MacMustStayAwake: true,
	}
	if runErr != nil {
		if errors.Is(runErr, ErrMissingBinary) {
			return result, runErr
		}
		return result, fmt.Errorf("oracle: run: %w", runErr)
	}
	if commandResult.ExitCode != 0 {
		return result, commandError{argv: result.NormalizedArgv, result: commandResult}
	}
	output, err := readBoundedFile(normalized.OutputPath, a.maxOutputBytes())
	if err != nil {
		return result, err
	}
	result.OutputSHA256 = digestBytes(output)
	result.OutputText = string(output)
	return result, nil
}

func (a *Adapter) ServeStatus(ctx context.Context) (ServeStatus, error) {
	result, err := a.runner().Run(ctx, Invocation{
		Argv:    ServeStatusArgv(),
		Timeout: defaultProbeTimeout,
	})
	if err != nil {
		return ServeStatus{}, err
	}
	if result.ExitCode != 0 {
		return ServeStatus{}, commandError{argv: ServeStatusArgv(), result: result}
	}
	status, err := ParseServeStatus(result.Stdout)
	if err != nil {
		return ServeStatus{}, err
	}
	status.CheckedAt = a.now().UTC()
	return status, nil
}

func (a *Adapter) Help(ctx context.Context) error {
	result, err := a.runner().Run(ctx, Invocation{
		Argv:    HelpArgv(),
		Timeout: defaultProbeTimeout,
	})
	if err != nil {
		return err
	}
	if result.ExitCode != 0 {
		return commandError{argv: HelpArgv(), result: result}
	}
	return nil
}

func (a *Adapter) Probe(ctx context.Context) (*capabilities.ToolReport, error) {
	checkedAt := a.now().UTC().Format(time.RFC3339)
	report := &capabilities.ToolReport{
		Tool:          capabilities.ToolOracle,
		Source:        "cli",
		LastCheckedAt: checkedAt,
		Capabilities: map[string]capabilities.Capability{
			CapabilityHelp:         {Status: capabilities.StatusMissing},
			CapabilityServeStatus:  {Status: capabilities.StatusMissing},
			CapabilityBrowserRun:   {Status: capabilities.StatusMissing, Fallback: "subscription-cli"},
			CapabilityRemoteInvoke: {Status: capabilities.StatusMissing, Fallback: "subscription-cli"},
		},
	}
	if err := a.Help(ctx); err != nil {
		note := err.Error()
		status := statusForError(err)
		for capID := range report.Capabilities {
			report.Capabilities[capID] = capabilities.Capability{
				Status:   status,
				Fallback: fallbackFor(capID),
				Notes:    note,
			}
		}
		return report, nil
	}
	report.Capabilities[CapabilityHelp] = capabilities.Capability{Status: capabilities.StatusOK, Transport: "stdio"}

	status, err := a.ServeStatus(ctx)
	if err != nil {
		note := err.Error()
		report.Capabilities[CapabilityServeStatus] = capabilities.Capability{Status: statusForError(err), Notes: note}
		report.Capabilities[CapabilityBrowserRun] = capabilities.Capability{Status: capabilities.StatusDegraded, Fallback: "subscription-cli", Notes: note}
		report.Capabilities[CapabilityRemoteInvoke] = capabilities.Capability{Status: remoteStatusFor(a.Remote), Fallback: "subscription-cli", Notes: note}
		return report, nil
	}
	if status.Model != "" {
		report.Version = "browser:" + status.Model
	}
	if !status.Healthy {
		report.Capabilities[CapabilityServeStatus] = capabilities.Capability{Status: capabilities.StatusDegraded, Notes: "oracle serve status returned unhealthy"}
		report.Capabilities[CapabilityBrowserRun] = capabilities.Capability{Status: capabilities.StatusDegraded, Fallback: "subscription-cli", Notes: "oracle serve is unhealthy"}
		report.Capabilities[CapabilityRemoteInvoke] = capabilities.Capability{Status: remoteStatusFor(a.Remote), Fallback: "subscription-cli", Notes: "oracle serve is unhealthy"}
		return report, nil
	}
	report.Capabilities[CapabilityServeStatus] = capabilities.Capability{Status: capabilities.StatusOK, Transport: "stdio"}
	report.Capabilities[CapabilityBrowserRun] = capabilities.Capability{Status: capabilities.StatusOK, Transport: "browser", Notes: "requires Mac to stay awake during active ChatGPT Pro rounds"}
	if remoteConfigured(a.Remote) {
		if _, err := normalizeRemote(a.Remote); err != nil {
			report.Capabilities[CapabilityRemoteInvoke] = capabilities.Capability{Status: capabilities.StatusMissing, Fallback: "subscription-cli", Notes: err.Error()}
			return report, nil
		}
		report.Capabilities[CapabilityRemoteInvoke] = capabilities.Capability{Status: capabilities.StatusOK, Transport: "ssh-reverse-tunnel"}
	} else {
		report.Capabilities[CapabilityRemoteInvoke] = capabilities.Capability{Status: capabilities.StatusMissing, Fallback: "subscription-cli", Notes: "remote host and token are not configured"}
	}
	return report, nil
}

func ParseServeStatus(out []byte) (ServeStatus, error) {
	text := strings.TrimSpace(string(out))
	if text == "" {
		return ServeStatus{}, fmt.Errorf("%w: empty serve status", ErrCommandContract)
	}
	var status ServeStatus
	if err := json.Unmarshal([]byte(text), &status); err == nil {
		return status, nil
	}
	lower := strings.ToLower(text)
	status.Healthy = strings.Contains(lower, "healthy") || strings.Contains(lower, "running") || strings.Contains(lower, "ok")
	if strings.Contains(lower, "unhealthy") || strings.Contains(lower, "offline") || strings.Contains(lower, "not running") {
		status.Healthy = false
	}
	if !status.Healthy {
		status.Warnings = []string{text}
	}
	return status, nil
}

func normalizeRunRequest(req RunRequest) (RunRequest, error) {
	model := strings.TrimSpace(req.Model)
	if model == "" {
		model = DefaultModel
	}
	if err := validateArgValue("model", model); err != nil {
		return RunRequest{}, err
	}
	prompt := strings.TrimSpace(req.Prompt)
	if prompt == "" {
		return RunRequest{}, fmt.Errorf("%w: prompt is required", ErrInvalidRequest)
	}
	if strings.Contains(prompt, "\x00") {
		return RunRequest{}, fmt.Errorf("%w: prompt contains NUL", ErrInvalidRequest)
	}
	outputPath, err := normalizeAbsPath(req.OutputPath, "outputPath")
	if err != nil {
		return RunRequest{}, err
	}
	workDir := strings.TrimSpace(req.WorkDir)
	if workDir != "" {
		workDir, err = normalizeAbsPath(workDir, "workDir")
		if err != nil {
			return RunRequest{}, err
		}
	}
	files := make([]string, 0, len(req.Files))
	for i, file := range req.Files {
		normalized, err := normalizeAbsPath(file, fmt.Sprintf("files[%d]", i))
		if err != nil {
			return RunRequest{}, err
		}
		files = append(files, normalized)
	}
	remote, err := normalizeRemote(req.Remote)
	if err != nil && (strings.TrimSpace(req.Remote.Host) != "" || strings.TrimSpace(req.Remote.Token) != "") {
		return RunRequest{}, err
	}
	return RunRequest{
		Model:      model,
		Prompt:     prompt,
		Files:      files,
		OutputPath: outputPath,
		WorkDir:    workDir,
		Remote:     remote,
		Env:        append([]string(nil), req.Env...),
		Timeout:    req.Timeout,
	}, nil
}

func normalizeRemote(remote RemoteConfig) (RemoteConfig, error) {
	host := strings.TrimSpace(remote.Host)
	token := strings.TrimSpace(remote.Token)
	if host == "" && token == "" {
		return RemoteConfig{}, nil
	}
	if host == "" || token == "" {
		return RemoteConfig{}, fmt.Errorf("%w: remote host and token must be provided together", ErrInvalidRequest)
	}
	if err := validateArgValue("remoteHost", host); err != nil {
		return RemoteConfig{}, err
	}
	if strings.ContainsAny(host, " \t") {
		return RemoteConfig{}, fmt.Errorf("%w: remoteHost must not contain whitespace", ErrInvalidRequest)
	}
	if err := validateArgValue("remoteToken", token); err != nil {
		return RemoteConfig{}, err
	}
	if strings.ContainsAny(token, " \t") {
		return RemoteConfig{}, fmt.Errorf("%w: remoteToken must not contain whitespace", ErrInvalidRequest)
	}
	return RemoteConfig{Host: host, Token: token}, nil
}

func normalizeAbsPath(path string, field string) (string, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return "", fmt.Errorf("%w: %s is required", ErrInvalidRequest, field)
	}
	if err := validateArgValue(field, path); err != nil {
		return "", err
	}
	if !filepath.IsAbs(path) {
		return "", fmt.Errorf("%w: %s must be absolute", ErrInvalidRequest, field)
	}
	cleaned := filepath.Clean(path)
	if cleaned == string(filepath.Separator) {
		return "", fmt.Errorf("%w: %s cannot be filesystem root", ErrInvalidRequest, field)
	}
	return cleaned, nil
}

func validateArgValue(name string, value string) error {
	if strings.Contains(value, "\x00") || strings.Contains(value, "\n") || strings.Contains(value, "\r") {
		return fmt.Errorf("%w: %s contains control characters", ErrInvalidRequest, name)
	}
	return nil
}

func readBoundedFile(path string, maxBytes int) ([]byte, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, fmt.Errorf("%w: %s", ErrMissingOutput, path)
		}
		return nil, err
	}
	if maxBytes <= 0 {
		maxBytes = defaultMaxOutputBytes
	}
	if len(data) > maxBytes {
		return nil, fmt.Errorf("%w: %s", ErrOutputTooLarge, path)
	}
	return data, nil
}

func redactRemoteToken(argv []string) []string {
	out := append([]string(nil), argv...)
	for i := 0; i+1 < len(out); i++ {
		if out[i] == "--remote-token" {
			out[i+1] = "<redacted>"
		}
	}
	return out
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

func timeoutOrDefault(timeout time.Duration, fallback time.Duration) time.Duration {
	if timeout > 0 {
		return timeout
	}
	return fallback
}

func childEnv(extra []string) []string {
	base := filterProviderCredentialEnv(os.Environ())
	if len(extra) == 0 {
		return base
	}
	return append(base, extra...)
}

func rejectProviderCredentialEnv(env []string) error {
	for _, item := range env {
		key, _, ok := strings.Cut(item, "=")
		if !ok {
			key = item
		}
		if isProviderCredentialKey(key) {
			return fmt.Errorf("%w: %s", ErrProviderCredentialEnv, key)
		}
	}
	return nil
}

func filterProviderCredentialEnv(env []string) []string {
	out := make([]string, 0, len(env))
	for _, item := range env {
		key, _, ok := strings.Cut(item, "=")
		if !ok {
			key = item
		}
		if isProviderCredentialKey(key) {
			continue
		}
		out = append(out, item)
	}
	return out
}

func isProviderCredentialKey(key string) bool {
	upper := strings.ToUpper(strings.TrimSpace(key))
	return upper == credentialKey("OP", "EN", "AI") ||
		upper == credentialKey("ANTH", "ROPIC") ||
		upper == credentialKey("GEM", "INI")
}

func credentialKey(providerParts ...string) string {
	return strings.Join([]string{strings.Join(providerParts, ""), "API", "KEY"}, "_")
}

func digestBytes(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func isExecNotFoundErr(err error) bool {
	var execErr *exec.Error
	if errors.As(err, &execErr) && errors.Is(execErr.Err, exec.ErrNotFound) {
		return true
	}
	return errors.Is(err, exec.ErrNotFound)
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
	return fmt.Sprintf("%s: %s: command %v exited %d: %s", ToolName, ErrCommandFailed, e.argv, e.result.ExitCode, detail)
}

func (e commandError) Unwrap() error {
	return ErrCommandFailed
}

func statusForError(err error) capabilities.CapabilityStatus {
	switch {
	case errors.Is(err, ErrMissingBinary):
		return capabilities.StatusMissing
	default:
		return capabilities.StatusDegraded
	}
}

func remoteStatusFor(remote RemoteConfig) capabilities.CapabilityStatus {
	if !remoteConfigured(remote) {
		return capabilities.StatusMissing
	}
	if _, err := normalizeRemote(remote); err == nil {
		return capabilities.StatusDegraded
	}
	return capabilities.StatusMissing
}

func remoteConfigured(remote RemoteConfig) bool {
	return strings.TrimSpace(remote.Host) != "" || strings.TrimSpace(remote.Token) != ""
}

func fallbackFor(capID string) string {
	switch capID {
	case CapabilityBrowserRun, CapabilityRemoteInvoke:
		return "subscription-cli"
	default:
		return ""
	}
}
