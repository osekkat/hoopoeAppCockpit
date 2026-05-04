package cli

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"
)

const (
	DefaultCommandTimeout = 10 * time.Minute
	DefaultMaxOutputBytes = 16 << 20
)

var (
	ErrInvalidCommandSpec     = errors.New("cli: invalid command spec")
	ErrOutputTooLarge         = errors.New("cli: command output exceeded limit")
	ErrProviderCredentialEnv  = errors.New("cli: direct provider credential environment is not allowed")
	ErrStreamConsumerRejected = errors.New("cli: stream consumer rejected chunk")
)

type Executor interface {
	Run(ctx context.Context, spec CommandSpec, onChunk func(StreamChunk) error) (CommandResult, error)
}

type OSExecutor struct {
	Now func() time.Time
}

func (o OSExecutor) Run(ctx context.Context, spec CommandSpec, onChunk func(StreamChunk) error) (CommandResult, error) {
	if strings.TrimSpace(spec.Binary) == "" {
		return CommandResult{}, fmt.Errorf("%w: empty binary", ErrInvalidCommandSpec)
	}
	if err := rejectProviderCredentialEnv(spec.Env); err != nil {
		return CommandResult{}, err
	}
	now := o.Now
	if now == nil {
		now = time.Now
	}
	timeout := spec.Timeout
	if timeout == 0 {
		timeout = DefaultCommandTimeout
	}
	runCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	result := CommandResult{StartedAt: now()}
	cmd := exec.CommandContext(runCtx, spec.Binary, spec.Args...)
	cmd.Stdin = bytes.NewReader(spec.Stdin)
	cmd.Dir = spec.Dir
	cmd.Env = childEnv(spec.Env)

	stdoutPipe, err := cmd.StdoutPipe()
	if err != nil {
		return result, fmt.Errorf("cli: stdout pipe: %w", err)
	}
	stderrPipe, err := cmd.StderrPipe()
	if err != nil {
		return result, fmt.Errorf("cli: stderr pipe: %w", err)
	}
	if err := cmd.Start(); err != nil {
		result.CompletedAt = now()
		return result, fmt.Errorf("cli: start %s: %w", spec.Binary, err)
	}

	maxBytes := spec.MaxOutputBytes
	if maxBytes == 0 {
		maxBytes = DefaultMaxOutputBytes
	}
	var stdout streamBuffer
	var stderr streamBuffer
	stdout.maxBytes = maxBytes
	stderr.maxBytes = maxBytes
	stdout.stream = StreamStdout
	stderr.stream = StreamStderr
	stdout.now = now
	stderr.now = now
	stdout.onChunk = onChunk
	stderr.onChunk = onChunk

	var wg sync.WaitGroup
	errs := make(chan error, 2)
	copyStream := func(dst *streamBuffer, src io.Reader) {
		defer wg.Done()
		if _, err := io.Copy(dst, src); err != nil {
			cancel()
			errs <- err
		}
	}
	wg.Add(2)
	go copyStream(&stdout, stdoutPipe)
	go copyStream(&stderr, stderrPipe)

	waitErr := cmd.Wait()
	wg.Wait()
	close(errs)
	result.CompletedAt = now()
	result.Stdout = stdout.bytes()
	result.Stderr = stderr.bytes()

	var streamErr error
	for err := range errs {
		if streamErr == nil {
			streamErr = err
		}
	}
	if streamErr != nil {
		cancel()
		return result, streamErr
	}
	if waitErr != nil {
		var exitErr *exec.ExitError
		if errors.As(waitErr, &exitErr) {
			result.ExitCode = exitErr.ExitCode()
			return result, nil
		}
		return result, waitErr
	}
	return result, nil
}

type streamBuffer struct {
	mu       sync.Mutex
	buf      bytes.Buffer
	maxBytes int64
	stream   Stream
	onChunk  func(StreamChunk) error
	now      func() time.Time
}

func (b *streamBuffer) Write(p []byte) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}
	b.mu.Lock()
	if int64(b.buf.Len()+len(p)) > b.maxBytes {
		b.mu.Unlock()
		return 0, ErrOutputTooLarge
	}
	cp := append([]byte(nil), p...)
	_, _ = b.buf.Write(cp)
	b.mu.Unlock()
	if b.onChunk != nil {
		if err := b.onChunk(StreamChunk{Stream: b.stream, Data: cp, At: b.now()}); err != nil {
			return 0, fmt.Errorf("%w: %w", ErrStreamConsumerRejected, err)
		}
	}
	return len(p), nil
}

func (b *streamBuffer) bytes() []byte {
	b.mu.Lock()
	defer b.mu.Unlock()
	return append([]byte(nil), b.buf.Bytes()...)
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
	return upper == strings.Join([]string{"OPENAI", "API", "KEY"}, "_") ||
		upper == strings.Join([]string{"ANTHROPIC", "API", "KEY"}, "_") ||
		upper == strings.Join([]string{"GEMINI", "API", "KEY"}, "_")
}
