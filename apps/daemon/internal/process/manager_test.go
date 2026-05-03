package process

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"syscall"
	"testing"
	"time"
)

func TestManagerRejectsProcessWithoutJob(t *testing.T) {
	manager := NewManager()
	sleep := requireSleep(t)
	if _, err := manager.Start(context.Background(), Spec{Path: sleep, Args: []string{"1"}}); !errors.Is(err, ErrInvalidSpec) {
		t.Fatalf("expected invalid spec, got %v", err)
	}
}

func TestManagerStartRequiresOneProcessPerJob(t *testing.T) {
	manager := NewManager()
	sleep := requireSleep(t)
	ctx := context.Background()
	rec, err := manager.Start(ctx, Spec{JobID: "job-one", Path: sleep, Args: []string{"30"}})
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	defer func() {
		_, _ = manager.Kill(context.Background(), rec.JobID)
	}()

	if _, err := manager.Start(ctx, Spec{JobID: "job-one", Path: sleep, Args: []string{"30"}}); !errors.Is(err, ErrDuplicate) {
		t.Fatalf("expected duplicate error, got %v", err)
	}
}

func TestManagerStopTerminatesProcessGroup(t *testing.T) {
	manager := NewManager()
	sleep := requireSleep(t)
	ctx := context.Background()
	rec, err := manager.Start(ctx, Spec{JobID: "job-stop", Path: sleep, Args: []string{"30"}})
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	if rec.PID <= 0 || rec.PGID <= 0 || rec.PID != rec.PGID {
		t.Fatalf("expected child process group, got %+v", rec)
	}

	stopCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	stopped, err := manager.Stop(stopCtx, rec.JobID, 50*time.Millisecond)
	if err != nil {
		t.Fatalf("stop: %v", err)
	}
	if stopped.Status != StatusExited && stopped.Status != StatusKilled {
		t.Fatalf("unexpected stopped status: %+v", stopped)
	}
	if err := syscall.Kill(rec.PID, 0); !errors.Is(err, syscall.ESRCH) {
		t.Fatalf("process still appears live, kill(0) err=%v", err)
	}
}

func TestManagerAdoptRequiresLiveProcess(t *testing.T) {
	manager := NewManager()
	pgid, err := syscall.Getpgid(os.Getpid())
	if err != nil {
		t.Fatalf("getpgid: %v", err)
	}
	rec, err := manager.Adopt(context.Background(), "job-adopt", os.Getpid(), pgid, false)
	if err != nil {
		t.Fatalf("adopt current process: %v", err)
	}
	if rec.JobID != "job-adopt" || rec.Status != StatusAdopted {
		t.Fatalf("bad adopted record: %+v", rec)
	}
}

func requireSleep(t *testing.T) string {
	t.Helper()
	sleep, err := exec.LookPath("sleep")
	if err != nil {
		t.Skip("sleep command not available")
	}
	return sleep
}
