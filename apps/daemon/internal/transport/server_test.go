package transport

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	jobstore "github.com/hoopoe-cockpit/hoopoe/apps/daemon/internal/jobs"
	"github.com/hoopoe-cockpit/hoopoe/apps/daemon/internal/security"
)

func TestValidateListenAddressDefaultsToLoopbackOnly(t *testing.T) {
	for _, addr := range []string{"127.0.0.1:0", "127.10.20.30:9182", "localhost:0", "[::1]:0", "100.100.100.100:0"} {
		t.Run(addr, func(t *testing.T) {
			if err := ValidateListenAddress(addr, false); err != nil {
				t.Fatalf("ValidateListenAddress(%q) returned error: %v", addr, err)
			}
		})
	}
	if err := ValidateListenAddress("0.0.0.0:0", false); err == nil {
		t.Fatal("expected public bind without explicit flag to fail")
	}
	if err := ValidateListenAddress("0.0.0.0:0", true); !errors.Is(err, security.ErrPublicBindNotConfirmed) {
		t.Fatalf("config-only public bind err = %v, want ErrPublicBindNotConfirmed", err)
	}
}

func TestResolveListenDecisionFallsBackAndLogsConfigOnlyPublicBind(t *testing.T) {
	logger := &recordingLogger{}

	decision, err := resolveListenDecision(context.Background(), listenDecisionRequest{
		Address:            "0.0.0.0:8080",
		ConfigAllowsPublic: true,
		Logger:             logger,
	})
	if err != nil {
		t.Fatalf("resolve listen decision: %v", err)
	}
	if decision.EffectiveAddress != "127.0.0.1:8080" || decision.RuntimeConfirmed {
		t.Fatalf("config-only public bind should fall back: %+v", decision)
	}
	if len(logger.infos) != 1 {
		t.Fatalf("logged warnings = %d, want 1", len(logger.infos))
	}
	fields := logger.infos[0]
	if fields["code"] != security.PublicBindWarningCode || fields["effectiveAddress"] != "127.0.0.1:8080" {
		t.Fatalf("warning fields = %#v", fields)
	}
}

func TestResolveListenDecisionAllowsPublicBindWithRuntimeConfirmation(t *testing.T) {
	target, err := security.ParseBindTarget("0.0.0.0:8080")
	if err != nil {
		t.Fatalf("parse target: %v", err)
	}
	confirmer := &security.HMACPublicBindConfirmer{
		Secret: []byte("0123456789abcdef0123456789abcdef"),
		Now: func() time.Time {
			return time.Unix(100, 0).UTC()
		},
	}
	token, err := confirmer.Mint(target, time.Unix(200, 0).UTC())
	if err != nil {
		t.Fatalf("mint token: %v", err)
	}
	logger := &recordingLogger{}

	decision, err := resolveListenDecision(context.Background(), listenDecisionRequest{
		Address:            target.Address,
		ConfigAllowsPublic: true,
		ConfirmationToken:  token,
		Confirmer:          confirmer,
		Logger:             logger,
	})
	if err != nil {
		t.Fatalf("resolve listen decision: %v", err)
	}
	if decision.EffectiveAddress != target.Address || !decision.RuntimeConfirmed {
		t.Fatalf("public bind not allowed after config + token: %+v", decision)
	}
	if len(logger.infos) != 1 || logger.infos[0]["runtimeConfirmed"] != true {
		t.Fatalf("warning fields = %#v", logger.infos)
	}
}

func TestRunBootstrapTokenOnlyCreatesInitialPairingOnce(t *testing.T) {
	stateDir := t.TempDir()
	var first bytes.Buffer
	if err := Run(context.Background(), []string{"-state-dir", stateDir, "-bootstrap-token-only"}, Config{Stdout: &first}); err != nil {
		t.Fatalf("first bootstrap token run: %v", err)
	}
	firstOutput := first.String()
	if !strings.Contains(firstOutput, "HOOPOE_PAIRING_TOKEN=") || !strings.Contains(firstOutput, "HOOPOE_PAIRING_TOKEN_ID=") {
		t.Fatalf("first bootstrap output = %q, want token and token id", firstOutput)
	}

	var second bytes.Buffer
	if err := Run(context.Background(), []string{"-state-dir", stateDir, "-bootstrap-token-only"}, Config{Stdout: &second}); err != nil {
		t.Fatalf("second bootstrap token run: %v", err)
	}
	if got := strings.TrimSpace(second.String()); got != "HOOPOE_PAIRING_TOKEN_ALREADY_INITIALIZED=1" {
		t.Fatalf("second bootstrap output = %q, want already initialized marker", got)
	}
	if _, err := os.Stat(filepath.Join(stateDir, "onboarding.sqlite3")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("bootstrap-token-only should not create onboarding database, stat err=%v", err)
	}
}

func TestPrepareJobsRuntimeDefaultsToFileRegistry(t *testing.T) {
	now := time.Date(2026, 5, 4, 0, 0, 0, 0, time.UTC)
	reader, err := prepareJobsRuntime(context.Background(), t.TempDir(), func() time.Time { return now }, nil)
	if err != nil {
		t.Fatalf("prepareJobsRuntime: %v", err)
	}
	if _, ok := reader.(*jobstore.FileRegistry); !ok {
		t.Fatalf("default jobs reader = %T, want *jobs.FileRegistry", reader)
	}
	list, err := reader.List(context.Background(), jobstore.ListFilter{})
	if err != nil {
		t.Fatalf("default jobs List: %v", err)
	}
	if len(list) != 0 {
		t.Fatalf("default jobs length = %d, want 0", len(list))
	}
}

func TestRunNotifiesSystemdReadyAndWatchdog(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	notifier := &fakeSystemdNotifier{
		ready:    make(chan string, 1),
		watchdog: make(chan struct{}, 1),
		interval: time.Millisecond,
	}
	done := make(chan error, 1)
	go func() {
		done <- Run(ctx, []string{"-state-dir", t.TempDir(), "-addr", "127.0.0.1:0"}, Config{
			Stdout:          io.Discard,
			SystemdNotifier: notifier,
		})
	}()

	select {
	case status := <-notifier.ready:
		if status != "hoopoe daemon accepting requests" {
			t.Fatalf("ready status = %q", status)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for ready notification")
	}

	select {
	case <-notifier.watchdog:
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for watchdog notification")
	}

	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Run after cancel: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for Run to stop")
	}
}

type recordingLogger struct {
	infos []map[string]any
}

func (l *recordingLogger) Info(_ context.Context, _ string, fields map[string]any) {
	l.infos = append(l.infos, fields)
}

func (l *recordingLogger) Error(context.Context, string, map[string]any) {}

type fakeSystemdNotifier struct {
	ready    chan string
	watchdog chan struct{}
	interval time.Duration
}

func (n *fakeSystemdNotifier) Ready(_ context.Context, status string) error {
	n.ready <- status
	return nil
}

func (n *fakeSystemdNotifier) Watchdog(context.Context) error {
	select {
	case n.watchdog <- struct{}{}:
	default:
	}
	return nil
}

func (n *fakeSystemdNotifier) WatchdogInterval() (time.Duration, bool, error) {
	if n.interval <= 0 {
		return 0, false, nil
	}
	return n.interval, true, nil
}
