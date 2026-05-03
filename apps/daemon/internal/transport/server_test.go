package transport

import (
	"context"
	"errors"
	"testing"
	"time"

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

type recordingLogger struct {
	infos []map[string]any
}

func (l *recordingLogger) Info(_ context.Context, _ string, fields map[string]any) {
	l.infos = append(l.infos, fields)
}

func (l *recordingLogger) Error(context.Context, string, map[string]any) {}
