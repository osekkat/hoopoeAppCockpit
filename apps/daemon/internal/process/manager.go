// Package process owns child-process lifecycle supervision for daemon jobs.
// Normal API handlers never expose this as arbitrary shell execution; callers
// pass typed specs selected by daemon-owned job runners.
package process

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"sync"
	"syscall"
	"time"
)

var (
	ErrInvalidSpec = errors.New("process: invalid spec")
	ErrNotFound    = errors.New("process: not found")
	ErrDuplicate   = errors.New("process: job already has a process")
)

type Status string

const (
	StatusRunning  Status = "running"
	StatusStopping Status = "stopping"
	StatusExited   Status = "exited"
	StatusKilled   Status = "killed"
	StatusAdopted  Status = "adopted"
)

type Spec struct {
	JobID string
	Path  string
	Args  []string
	Dir   string
	Env   []string
	PTY   bool
}

type Record struct {
	JobID     string     `json:"jobId"`
	PID       int        `json:"pid"`
	PGID      int        `json:"pgid"`
	Status    Status     `json:"status"`
	PTY       bool       `json:"pty"`
	StartedAt time.Time  `json:"startedAt"`
	ExitedAt  *time.Time `json:"exitedAt,omitempty"`
	ExitCode  *int       `json:"exitCode,omitempty"`
}

type Manager struct {
	mu    sync.Mutex
	procs map[string]*trackedProcess
	now   func() time.Time
}

type trackedProcess struct {
	record Record
	cmd    *exec.Cmd
	done   chan struct{}
}

func NewManager() *Manager {
	return &Manager{
		procs: make(map[string]*trackedProcess),
		now:   time.Now,
	}
}

func (m *Manager) SetClock(now func() time.Time) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if now == nil {
		m.now = time.Now
		return
	}
	m.now = now
}

func (m *Manager) Start(ctx context.Context, spec Spec) (Record, error) {
	if err := ctx.Err(); err != nil {
		return Record{}, err
	}
	if spec.JobID == "" || spec.Path == "" {
		return Record{}, ErrInvalidSpec
	}

	m.mu.Lock()
	if _, exists := m.procs[spec.JobID]; exists {
		m.mu.Unlock()
		return Record{}, ErrDuplicate
	}
	m.mu.Unlock()

	cmd := exec.Command(spec.Path, spec.Args...)
	cmd.Dir = spec.Dir
	cmd.Env = append(os.Environ(), spec.Env...)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}

	if err := cmd.Start(); err != nil {
		return Record{}, err
	}
	pgid, err := syscall.Getpgid(cmd.Process.Pid)
	if err != nil {
		_ = cmd.Process.Kill()
		return Record{}, err
	}

	rec := Record{
		JobID:     spec.JobID,
		PID:       cmd.Process.Pid,
		PGID:      pgid,
		Status:    StatusRunning,
		PTY:       spec.PTY,
		StartedAt: m.now().UTC(),
	}
	proc := &trackedProcess{record: rec, cmd: cmd, done: make(chan struct{})}

	m.mu.Lock()
	if _, exists := m.procs[spec.JobID]; exists {
		m.mu.Unlock()
		_ = signalGroup(pgid, syscall.SIGKILL)
		_, _ = cmd.Process.Wait()
		return Record{}, ErrDuplicate
	}
	m.procs[spec.JobID] = proc
	m.mu.Unlock()

	go m.wait(spec.JobID, proc)
	return rec, nil
}

func (m *Manager) Adopt(ctx context.Context, jobID string, pid int, pgid int, pty bool) (Record, error) {
	if err := ctx.Err(); err != nil {
		return Record{}, err
	}
	if jobID == "" || pid <= 0 || pgid <= 0 {
		return Record{}, ErrInvalidSpec
	}
	if err := syscall.Kill(pid, 0); err != nil {
		return Record{}, err
	}
	rec := Record{
		JobID:     jobID,
		PID:       pid,
		PGID:      pgid,
		Status:    StatusAdopted,
		PTY:       pty,
		StartedAt: m.now().UTC(),
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, exists := m.procs[jobID]; exists {
		return Record{}, ErrDuplicate
	}
	m.procs[jobID] = &trackedProcess{record: rec, done: make(chan struct{})}
	return rec, nil
}

func (m *Manager) Status(ctx context.Context, jobID string) (Record, error) {
	if err := ctx.Err(); err != nil {
		return Record{}, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	proc, ok := m.procs[jobID]
	if !ok {
		return Record{}, ErrNotFound
	}
	return proc.record, nil
}

func (m *Manager) List(ctx context.Context) ([]Record, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]Record, 0, len(m.procs))
	for _, proc := range m.procs {
		out = append(out, proc.record)
	}
	return out, nil
}

func (m *Manager) Stop(ctx context.Context, jobID string, grace time.Duration) (Record, error) {
	return m.terminate(ctx, jobID, syscall.SIGTERM, grace)
}

func (m *Manager) Kill(ctx context.Context, jobID string) (Record, error) {
	return m.terminate(ctx, jobID, syscall.SIGKILL, 0)
}

func (m *Manager) wait(jobID string, proc *trackedProcess) {
	err := proc.cmd.Wait()
	exited := m.now().UTC()
	exitCode := 0
	if proc.cmd.ProcessState != nil {
		exitCode = proc.cmd.ProcessState.ExitCode()
	} else if err != nil {
		exitCode = -1
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	current, ok := m.procs[jobID]
	if !ok || current != proc {
		close(proc.done)
		return
	}
	current.record.Status = StatusExited
	current.record.ExitedAt = &exited
	current.record.ExitCode = &exitCode
	close(current.done)
}

func (m *Manager) terminate(ctx context.Context, jobID string, sig syscall.Signal, grace time.Duration) (Record, error) {
	if err := ctx.Err(); err != nil {
		return Record{}, err
	}
	m.mu.Lock()
	proc, ok := m.procs[jobID]
	if !ok {
		m.mu.Unlock()
		return Record{}, ErrNotFound
	}
	if proc.record.Status == StatusExited || proc.record.Status == StatusKilled {
		rec := proc.record
		m.mu.Unlock()
		return rec, nil
	}
	if sig == syscall.SIGTERM {
		proc.record.Status = StatusStopping
	}
	rec := proc.record
	done := proc.done
	adopted := proc.cmd == nil
	m.mu.Unlock()

	if err := signalGroup(rec.PGID, sig); err != nil && !errors.Is(err, syscall.ESRCH) {
		return Record{}, err
	}
	if adopted {
		return m.waitForAdoptedTerminal(ctx, jobID, rec.PID, sig == syscall.SIGKILL)
	}
	if sig == syscall.SIGKILL || grace <= 0 {
		return m.waitForTerminal(ctx, jobID, done, true)
	}

	timer := time.NewTimer(grace)
	defer timer.Stop()
	select {
	case <-done:
		return m.Status(ctx, jobID)
	case <-timer.C:
		if err := signalGroup(rec.PGID, syscall.SIGKILL); err != nil && !errors.Is(err, syscall.ESRCH) {
			return Record{}, err
		}
		return m.waitForTerminal(ctx, jobID, done, true)
	case <-ctx.Done():
		return Record{}, ctx.Err()
	}
}

func (m *Manager) waitForAdoptedTerminal(ctx context.Context, jobID string, pid int, killed bool) (Record, error) {
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	for {
		if err := syscall.Kill(pid, 0); errors.Is(err, syscall.ESRCH) {
			exited := m.now().UTC()
			m.mu.Lock()
			defer m.mu.Unlock()
			proc, ok := m.procs[jobID]
			if !ok {
				return Record{}, ErrNotFound
			}
			if killed {
				proc.record.Status = StatusKilled
			} else {
				proc.record.Status = StatusExited
			}
			proc.record.ExitedAt = &exited
			select {
			case <-proc.done:
			default:
				close(proc.done)
			}
			return proc.record, nil
		}
		select {
		case <-ticker.C:
		case <-ctx.Done():
			return Record{}, ctx.Err()
		}
	}
}

func (m *Manager) waitForTerminal(ctx context.Context, jobID string, done <-chan struct{}, killed bool) (Record, error) {
	select {
	case <-done:
		m.mu.Lock()
		defer m.mu.Unlock()
		proc, ok := m.procs[jobID]
		if !ok {
			return Record{}, ErrNotFound
		}
		if killed && proc.record.Status != StatusExited {
			proc.record.Status = StatusKilled
		}
		return proc.record, nil
	case <-ctx.Done():
		return Record{}, ctx.Err()
	}
}

func signalGroup(pgid int, sig syscall.Signal) error {
	if pgid <= 0 {
		return fmt.Errorf("%w: invalid process group %d", ErrInvalidSpec, pgid)
	}
	return syscall.Kill(-pgid, sig)
}
