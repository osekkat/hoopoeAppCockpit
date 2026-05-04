package acfs

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"sync"
	"time"
)

type OSExecutor struct {
	Now func() time.Time
}

func (o OSExecutor) Run(ctx context.Context, spec CommandSpec, onLine func(Line) error) (CommandResult, error) {
	if spec.CurlPath == "" || spec.BashPath == "" || spec.URL == "" || !ValidRef(spec.Ref) {
		return CommandResult{}, ErrInvalidRunRequest
	}
	now := o.Now
	if now == nil {
		now = time.Now
	}
	timeout := spec.Timeout
	if timeout == 0 {
		timeout = DefaultInstallerTimeout
	}
	runCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	result := CommandResult{StartedAt: now().UTC()}
	curlCmd := exec.CommandContext(runCtx, spec.CurlPath, "-fsSL", spec.URL)
	bashCmd := exec.CommandContext(runCtx, spec.BashPath, "-s", "--", "--yes", "--mode", "vibe", "--ref", spec.Ref)

	reader, writer := io.Pipe()
	curlCmd.Stdout = writer
	bashCmd.Stdin = reader

	curlStderr, err := curlCmd.StderrPipe()
	if err != nil {
		return result, fmt.Errorf("acfs: curl stderr pipe: %w", err)
	}
	bashStdout, err := bashCmd.StdoutPipe()
	if err != nil {
		return result, fmt.Errorf("acfs: bash stdout pipe: %w", err)
	}
	bashStderr, err := bashCmd.StderrPipe()
	if err != nil {
		return result, fmt.Errorf("acfs: bash stderr pipe: %w", err)
	}
	if err := bashCmd.Start(); err != nil {
		return result, fmt.Errorf("acfs: start bash: %w", err)
	}
	if err := curlCmd.Start(); err != nil {
		_ = reader.Close()
		_ = bashCmd.Process.Kill()
		return result, fmt.Errorf("acfs: start curl: %w", err)
	}

	var wg sync.WaitGroup
	errs := make(chan error, 4)
	readLines := func(stream Stream, src io.Reader) {
		defer wg.Done()
		if err := copyLines(src, stream, now, onLine); err != nil {
			cancel()
			errs <- err
		}
	}
	wg.Add(3)
	go readLines(StreamStderr, curlStderr)
	go readLines(StreamStdout, bashStdout)
	go readLines(StreamStderr, bashStderr)

	curlErr := curlCmd.Wait()
	_ = writer.Close()
	bashErr := bashCmd.Wait()
	_ = reader.Close()
	wg.Wait()
	close(errs)
	result.CompletedAt = now().UTC()

	for err := range errs {
		if err != nil {
			return result, err
		}
	}
	curlExit, curlWaitErr := exitCode(curlErr)
	bashExit, bashWaitErr := exitCode(bashErr)
	if curlWaitErr != nil {
		result.ExitCode = -1
		return result, curlWaitErr
	}
	if bashWaitErr != nil {
		result.ExitCode = -1
		return result, bashWaitErr
	}
	if curlExit != 0 {
		result.ExitCode = curlExit
		return result, nil
	}
	result.ExitCode = bashExit
	return result, nil
}

func copyLines(src io.Reader, stream Stream, now func() time.Time, onLine func(Line) error) error {
	reader := bufio.NewReader(src)
	for {
		text, err := reader.ReadString('\n')
		if len(text) > 0 {
			text = trimLineEnding(text)
			if onLine != nil {
				if emitErr := onLine(Line{Stream: stream, Text: text, At: now().UTC()}); emitErr != nil {
					return emitErr
				}
			}
		}
		if err == nil {
			continue
		}
		if errors.Is(err, io.EOF) {
			return nil
		}
		return err
	}
}

func exitCode(err error) (int, error) {
	if err == nil {
		return 0, nil
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		return exitErr.ExitCode(), nil
	}
	return -1, err
}

func trimLineEnding(s string) string {
	if len(s) > 0 && s[len(s)-1] == '\n' {
		s = s[:len(s)-1]
	}
	if len(s) > 0 && s[len(s)-1] == '\r' {
		s = s[:len(s)-1]
	}
	return s
}
