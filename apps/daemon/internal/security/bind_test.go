package security

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestEvaluateBindAllowsLoopbackByDefault(t *testing.T) {
	decision, err := EvaluateBind(context.Background(), BindRequest{Address: "127.0.0.1:1234"})
	if err != nil {
		t.Fatalf("EvaluateBind: %v", err)
	}
	if decision.EffectiveAddress != "127.0.0.1:1234" || decision.PublicExposure || decision.Warning != nil {
		t.Fatalf("unexpected loopback decision: %+v", decision)
	}
}

func TestEvaluateBindAllowsTailnetWithoutPublicWarning(t *testing.T) {
	decision, err := EvaluateBind(context.Background(), BindRequest{Address: "100.100.100.100:1234"})
	if err != nil {
		t.Fatalf("EvaluateBind: %v", err)
	}
	if decision.EffectiveAddress != "100.100.100.100:1234" || decision.PublicExposure || !decision.Tailnet || decision.Warning != nil {
		t.Fatalf("unexpected tailnet decision: %+v", decision)
	}
}

func TestEvaluateBindFallsBackWhenConfigOnlyRequestsPublicBind(t *testing.T) {
	decision, err := EvaluateBind(context.Background(), BindRequest{
		Address:            "0.0.0.0:8080",
		ConfigAllowsPublic: true,
	})
	if err != nil {
		t.Fatalf("EvaluateBind: %v", err)
	}
	if decision.EffectiveAddress != "127.0.0.1:8080" {
		t.Fatalf("effective address = %q", decision.EffectiveAddress)
	}
	if decision.RuntimeConfirmed {
		t.Fatal("runtime confirmed without token")
	}
	if decision.Warning == nil || decision.Warning.Code != PublicBindWarningCode {
		t.Fatalf("missing public-bind warning: %+v", decision.Warning)
	}
	if strings.Contains(decision.Warning.Message, "bound to 0.0.0.0:8080") {
		t.Fatalf("fallback warning reported the rejected address as bound: %q", decision.Warning.Message)
	}
	if !strings.Contains(decision.Warning.Message, "127.0.0.1:8080") {
		t.Fatalf("fallback warning does not name effective loopback address: %q", decision.Warning.Message)
	}
}

func TestEvaluateBindFallsBackWhenTokenOnlyRequestsPublicBind(t *testing.T) {
	target, err := ParseBindTarget("0.0.0.0:8080")
	if err != nil {
		t.Fatalf("parse target: %v", err)
	}
	confirmer := testConfirmer(t)
	token, err := confirmer.Mint(target, mustTime("2026-05-03T20:05:00Z"))
	if err != nil {
		t.Fatalf("mint token: %v", err)
	}

	decision, err := EvaluateBind(context.Background(), BindRequest{
		Address:           target.Address,
		ConfirmationToken: token,
		Confirmer:         confirmer,
	})
	if err != nil {
		t.Fatalf("EvaluateBind: %v", err)
	}
	if decision.EffectiveAddress != "127.0.0.1:8080" || decision.RuntimeConfirmed {
		t.Fatalf("token-only bind should fall back without config: %+v", decision)
	}
}

func TestEvaluateBindAllowsPublicBindWithConfigAndRuntimeConfirmation(t *testing.T) {
	target, err := ParseBindTarget("0.0.0.0:8080")
	if err != nil {
		t.Fatalf("parse target: %v", err)
	}
	confirmer := testConfirmer(t)
	token, err := confirmer.Mint(target, mustTime("2026-05-03T20:05:00Z"))
	if err != nil {
		t.Fatalf("mint token: %v", err)
	}

	decision, err := EvaluateBind(context.Background(), BindRequest{
		Address:            target.Address,
		ConfigAllowsPublic: true,
		ConfirmationToken:  token,
		Confirmer:          confirmer,
	})
	if err != nil {
		t.Fatalf("EvaluateBind: %v", err)
	}
	if decision.EffectiveAddress != "0.0.0.0:8080" || !decision.RuntimeConfirmed || !decision.PublicExposure {
		t.Fatalf("public bind not allowed after config + token: %+v", decision)
	}
	if decision.Warning == nil {
		t.Fatal("public bind should still carry diagnostics warning")
	}
}

func TestHMACPublicBindConfirmerRejectsWrongAddressAndExpiredToken(t *testing.T) {
	confirmer := testConfirmer(t)
	target, err := ParseBindTarget("0.0.0.0:8080")
	if err != nil {
		t.Fatalf("parse target: %v", err)
	}
	token, err := confirmer.Mint(target, mustTime("2026-05-03T20:05:00Z"))
	if err != nil {
		t.Fatalf("mint token: %v", err)
	}
	other, err := ParseBindTarget("0.0.0.0:9090")
	if err != nil {
		t.Fatalf("parse other target: %v", err)
	}
	if err := confirmer.ConfirmPublicBind(context.Background(), token, other); !errors.Is(err, ErrInvalidConfirmationToken) {
		t.Fatalf("wrong-address err = %v", err)
	}

	expired, err := confirmer.Mint(target, mustTime("2026-05-03T19:59:00Z"))
	if err != nil {
		t.Fatalf("mint expired token: %v", err)
	}
	if err := confirmer.ConfirmPublicBind(context.Background(), expired, target); !errors.Is(err, ErrInvalidConfirmationToken) {
		t.Fatalf("expired err = %v", err)
	}
}

func TestHMACPublicBindConfirmerConsumesTokenOnce(t *testing.T) {
	confirmer := testConfirmer(t)
	target, err := ParseBindTarget("0.0.0.0:8080")
	if err != nil {
		t.Fatalf("parse target: %v", err)
	}
	token, err := confirmer.Mint(target, mustTime("2026-05-03T20:05:00Z"))
	if err != nil {
		t.Fatalf("mint token: %v", err)
	}
	if err := confirmer.ConfirmPublicBind(context.Background(), token, target); err != nil {
		t.Fatalf("first confirm: %v", err)
	}
	if err := confirmer.ConfirmPublicBind(context.Background(), token, target); !errors.Is(err, ErrConfirmationTokenUsed) {
		t.Fatalf("second confirm err = %v, want ErrConfirmationTokenUsed", err)
	}
}

func TestParseBindTargetHandlesUnspecifiedHost(t *testing.T) {
	target, err := ParseBindTarget(":7777")
	if err != nil {
		t.Fatalf("ParseBindTarget: %v", err)
	}
	if target.Address != "0.0.0.0:7777" || target.Host != "0.0.0.0" || target.Port != "7777" {
		t.Fatalf("target = %+v", target)
	}
}

func testConfirmer(t *testing.T) *HMACPublicBindConfirmer {
	t.Helper()
	return &HMACPublicBindConfirmer{
		Secret: []byte("0123456789abcdef0123456789abcdef"),
		Now: func() time.Time {
			return mustTime("2026-05-03T20:00:00Z")
		},
	}
}

func mustTime(value string) time.Time {
	t, err := time.Parse(time.RFC3339, value)
	if err != nil {
		panic(err)
	}
	return t
}
