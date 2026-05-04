// Package search implements safe project search over a checked-out repo.
package search

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

const (
	SchemaVersion         = 1
	DefaultMaxResults     = 500
	HardMaxResults        = 5000
	DefaultMaxStdoutBytes = 16 << 20
	DefaultMaxStderrBytes = 128 << 10
)

var (
	ErrInvalidRequest  = errors.New("search: invalid request")
	ErrPathOutsideRoot = errors.New("search: path outside repo root")
	ErrCommandFailed   = errors.New("search: command failed")
	ErrMalformedOutput = errors.New("search: malformed ripgrep output")
	ErrOutputTooLarge  = errors.New("search: command output exceeded limit")
)

type Request struct {
	ProjectID  string
	RepoPath   string
	Query      string
	Paths      []string
	Literal    bool
	MaxResults int
}

type Response struct {
	SchemaVersion int       `json:"schemaVersion"`
	ProjectID     string    `json:"projectId"`
	RepoPath      string    `json:"repoPath"`
	Query         string    `json:"query"`
	Literal       bool      `json:"literal"`
	MaxResults    int       `json:"maxResults"`
	MatchCount    int       `json:"matchCount"`
	Capped        bool      `json:"capped"`
	Results       []Result  `json:"results"`
	CheckedAt     time.Time `json:"checkedAt"`
}

type Result struct {
	Path       string     `json:"path"`
	Line       int        `json:"line"`
	Column     int        `json:"column"`
	Text       string     `json:"text"`
	Submatches []Submatch `json:"submatches,omitempty"`
}

type Submatch struct {
	Start int    `json:"start"`
	End   int    `json:"end"`
	Text  string `json:"text"`
}

type CommandSpec struct {
	Path           string
	Args           []string
	Dir            string
	MaxStdoutBytes int64
	MaxStderrBytes int64
}

type RunResult struct {
	Stdout   []byte
	Stderr   []byte
	ExitCode int
}

type Runner interface {
	Run(context.Context, CommandSpec) (RunResult, error)
}

type Config struct {
	Binary string
	Runner Runner
	Now    func() time.Time
}

type Service struct {
	binary string
	runner Runner
	now    func() time.Time
}

func NewService(cfg Config) *Service {
	binary := strings.TrimSpace(cfg.Binary)
	if binary == "" {
		binary = "rg"
	}
	runner := cfg.Runner
	if runner == nil {
		runner = OSRunner{}
	}
	now := cfg.Now
	if now == nil {
		now = time.Now
	}
	return &Service{binary: binary, runner: runner, now: now}
}

func (s *Service) Search(ctx context.Context, req Request) (Response, error) {
	req.MaxResults = normalizeMaxResults(req.MaxResults)
	spec, err := BuildCommand(s.binary, req)
	if err != nil {
		return Response{}, err
	}
	run, err := s.runner.Run(ctx, spec)
	if err != nil {
		return Response{}, fmt.Errorf("%w: %w", ErrCommandFailed, err)
	}
	if run.ExitCode > 1 {
		return Response{}, fmt.Errorf("%w: exit %d: %s", ErrCommandFailed, run.ExitCode, strings.TrimSpace(string(run.Stderr)))
	}
	parsed, err := ParseRipgrepJSON(bytes.NewReader(run.Stdout), req.MaxResults)
	if err != nil {
		return Response{}, err
	}
	return Response{
		SchemaVersion: SchemaVersion,
		ProjectID:     strings.TrimSpace(req.ProjectID),
		RepoPath:      spec.Dir,
		Query:         strings.TrimSpace(req.Query),
		Literal:       req.Literal,
		MaxResults:    req.MaxResults,
		MatchCount:    parsed.MatchCount,
		Capped:        parsed.Capped,
		Results:       parsed.Results,
		CheckedAt:     s.now().UTC(),
	}, nil
}

func BuildCommand(binary string, req Request) (CommandSpec, error) {
	binary = strings.TrimSpace(binary)
	if binary == "" {
		binary = "rg"
	}
	if isShellBinary(binary) {
		return CommandSpec{}, fmt.Errorf("%w: shell binary %q is not allowed", ErrInvalidRequest, binary)
	}
	query := strings.TrimSpace(req.Query)
	if query == "" || hasControl(query) {
		return CommandSpec{}, fmt.Errorf("%w: query required and must not contain control characters", ErrInvalidRequest)
	}
	root, err := resolveRoot(req.RepoPath)
	if err != nil {
		return CommandSpec{}, err
	}
	maxResults := normalizeMaxResults(req.MaxResults)
	args := []string{
		"--json",
		"--color=never",
		"--hidden",
		"--glob",
		"!.git",
		"--max-count",
		strconv.Itoa(maxResults),
	}
	if req.Literal {
		args = append(args, "--fixed-strings")
	}
	if _, err := os.Stat(filepath.Join(root, ".hoopoeignore")); err == nil {
		args = append(args, "--ignore-file", ".hoopoeignore")
	}
	args = append(args, "--", query)
	paths, err := resolvePaths(root, req.Paths)
	if err != nil {
		return CommandSpec{}, err
	}
	args = append(args, paths...)
	return CommandSpec{
		Path:           binary,
		Args:           args,
		Dir:            root,
		MaxStdoutBytes: DefaultMaxStdoutBytes,
		MaxStderrBytes: DefaultMaxStderrBytes,
	}, nil
}

type ParsedOutput struct {
	Results    []Result
	MatchCount int
	Capped     bool
}

func ParseRipgrepJSON(r io.Reader, maxResults int) (ParsedOutput, error) {
	maxResults = normalizeMaxResults(maxResults)
	decoder := json.NewDecoder(r)
	out := ParsedOutput{Results: make([]Result, 0)}
	for {
		var message rgMessage
		err := decoder.Decode(&message)
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return ParsedOutput{}, fmt.Errorf("%w: %v", ErrMalformedOutput, err)
		}
		if message.Type != "match" {
			continue
		}
		result := message.result()
		if result.Path == "" || result.Line <= 0 {
			continue
		}
		out.MatchCount++
		if len(out.Results) >= maxResults {
			out.Capped = true
			continue
		}
		out.Results = append(out.Results, result)
	}
	return out, nil
}

type rgMessage struct {
	Type string `json:"type"`
	Data struct {
		Path struct {
			Text string `json:"text"`
		} `json:"path"`
		Lines struct {
			Text string `json:"text"`
		} `json:"lines"`
		LineNumber int `json:"line_number"`
		Submatches []struct {
			Match struct {
				Text string `json:"text"`
			} `json:"match"`
			Start int `json:"start"`
			End   int `json:"end"`
		} `json:"submatches"`
	} `json:"data"`
}

func (m rgMessage) result() Result {
	submatches := make([]Submatch, 0, len(m.Data.Submatches))
	column := 1
	for index, submatch := range m.Data.Submatches {
		if index == 0 && submatch.Start >= 0 {
			column = submatch.Start + 1
		}
		submatches = append(submatches, Submatch{
			Start: submatch.Start,
			End:   submatch.End,
			Text:  submatch.Match.Text,
		})
	}
	return Result{
		Path:       filepath.ToSlash(m.Data.Path.Text),
		Line:       m.Data.LineNumber,
		Column:     column,
		Text:       strings.TrimRight(m.Data.Lines.Text, "\r\n"),
		Submatches: submatches,
	}
}

type OSRunner struct{}

func (OSRunner) Run(ctx context.Context, spec CommandSpec) (RunResult, error) {
	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	cmd := exec.CommandContext(runCtx, spec.Path, spec.Args...)
	cmd.Dir = spec.Dir
	cmd.Env = append(os.Environ(), "LC_ALL=C")
	stdoutPipe, err := cmd.StdoutPipe()
	if err != nil {
		return RunResult{ExitCode: -1}, err
	}
	stderrPipe, err := cmd.StderrPipe()
	if err != nil {
		return RunResult{ExitCode: -1}, err
	}
	if err := cmd.Start(); err != nil {
		return RunResult{ExitCode: -1}, err
	}
	stdout := newLimitedBuffer("stdout", normalizeByteLimit(spec.MaxStdoutBytes, DefaultMaxStdoutBytes))
	stderr := newLimitedBuffer("stderr", normalizeByteLimit(spec.MaxStderrBytes, DefaultMaxStderrBytes))
	errs := make(chan error, 2)
	go copyCommandOutput(cancel, errs, stdout, stdoutPipe)
	go copyCommandOutput(cancel, errs, stderr, stderrPipe)
	err = cmd.Wait()
	stdoutErr := <-errs
	stderrErr := <-errs
	exitCode := 0
	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			exitCode = exitErr.ExitCode()
		} else {
			exitCode = -1
		}
	}
	result := RunResult{Stdout: stdout.Bytes(), Stderr: stderr.Bytes(), ExitCode: exitCode}
	if stdoutErr != nil {
		return result, stdoutErr
	}
	if stderrErr != nil {
		return result, stderrErr
	}
	if err != nil && exitCode == -1 {
		return result, err
	}
	return result, nil
}

func copyCommandOutput(cancel context.CancelFunc, errs chan<- error, dst *limitedBuffer, src io.Reader) {
	_, err := io.Copy(dst, src)
	if err != nil {
		cancel()
	}
	errs <- err
}

type limitedBuffer struct {
	stream string
	limit  int64
	buf    bytes.Buffer
}

func newLimitedBuffer(stream string, limit int64) *limitedBuffer {
	return &limitedBuffer{stream: stream, limit: limit}
}

func (b *limitedBuffer) Write(p []byte) (int, error) {
	remaining := b.limit - int64(b.buf.Len())
	if remaining <= 0 {
		return 0, outputLimitError{stream: b.stream, limit: b.limit}
	}
	if int64(len(p)) > remaining {
		_, _ = b.buf.Write(p[:remaining])
		return int(remaining), outputLimitError{stream: b.stream, limit: b.limit}
	}
	return b.buf.Write(p)
}

func (b *limitedBuffer) Bytes() []byte {
	return append([]byte(nil), b.buf.Bytes()...)
}

type outputLimitError struct {
	stream string
	limit  int64
}

func (e outputLimitError) Error() string {
	return fmt.Sprintf("%v: %s exceeded %d bytes", ErrOutputTooLarge, e.stream, e.limit)
}

func (e outputLimitError) Unwrap() error {
	return ErrOutputTooLarge
}

func resolveRoot(root string) (string, error) {
	root = strings.TrimSpace(root)
	if root == "" {
		return "", fmt.Errorf("%w: repo path required", ErrInvalidRequest)
	}
	if hasControl(root) {
		return "", fmt.Errorf("%w: repo path contains control characters", ErrInvalidRequest)
	}
	abs, err := filepath.Abs(root)
	if err != nil {
		return "", fmt.Errorf("%w: %v", ErrInvalidRequest, err)
	}
	resolved, err := filepath.EvalSymlinks(abs)
	if err != nil {
		return "", fmt.Errorf("%w: repo path %q: %v", ErrInvalidRequest, root, err)
	}
	info, err := os.Stat(resolved)
	if err != nil {
		return "", fmt.Errorf("%w: repo path %q: %v", ErrInvalidRequest, root, err)
	}
	if !info.IsDir() {
		return "", fmt.Errorf("%w: repo path must be a directory", ErrInvalidRequest)
	}
	return resolved, nil
}

func resolvePaths(root string, paths []string) ([]string, error) {
	if len(paths) == 0 {
		return []string{"."}, nil
	}
	out := make([]string, 0, len(paths))
	for _, requested := range paths {
		resolved, err := resolveInside(root, requested)
		if err != nil {
			return nil, err
		}
		out = append(out, resolved)
	}
	return out, nil
}

func resolveInside(root string, requested string) (string, error) {
	requested = strings.TrimSpace(requested)
	if requested == "" {
		requested = "."
	}
	if hasControl(requested) {
		return "", fmt.Errorf("%w: requested path contains control characters", ErrInvalidRequest)
	}
	candidate := requested
	if !filepath.IsAbs(candidate) {
		candidate = filepath.Join(root, candidate)
	}
	abs, err := filepath.Abs(candidate)
	if err != nil {
		return "", fmt.Errorf("%w: %v", ErrInvalidRequest, err)
	}
	resolved, err := filepath.EvalSymlinks(abs)
	if err != nil {
		return "", fmt.Errorf("%w: path %q: %v", ErrInvalidRequest, requested, err)
	}
	rel, err := filepath.Rel(root, resolved)
	if err != nil {
		return "", fmt.Errorf("%w: %v", ErrInvalidRequest, err)
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || filepath.IsAbs(rel) {
		return "", fmt.Errorf("%w: %q", ErrPathOutsideRoot, requested)
	}
	if rel == "" {
		rel = "."
	}
	return filepath.ToSlash(rel), nil
}

func normalizeMaxResults(maxResults int) int {
	if maxResults <= 0 {
		return DefaultMaxResults
	}
	if maxResults > HardMaxResults {
		return HardMaxResults
	}
	return maxResults
}

func normalizeByteLimit(limit int64, fallback int64) int64 {
	if limit <= 0 {
		return fallback
	}
	return limit
}

func hasControl(value string) bool {
	return strings.ContainsAny(value, "\x00\r\n")
}

func isShellBinary(path string) bool {
	base := strings.ToLower(filepath.Base(strings.TrimSpace(path)))
	base = strings.TrimSuffix(base, ".exe")
	switch base {
	case "sh", "bash", "dash", "zsh", "fish", "cmd", "powershell", "pwsh":
		return true
	default:
		return false
	}
}
