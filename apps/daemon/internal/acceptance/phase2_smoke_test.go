package acceptance

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/hoopoe-cockpit/hoopoe/apps/daemon/internal/auth"
)

func TestPhase2AcceptanceSmokePassesMockFlywheelEquivalent(t *testing.T) {
	report, err := RunPhase2Smoke(context.Background(), Config{
		WorkDir:      t.TempDir(),
		Now:          fixedNow,
		ReconnectSLO: 10 * time.Second,
		ReplaySLO:    5 * time.Second,
	})
	if err != nil {
		t.Fatalf("phase2 smoke failed: %v", err)
	}
	if got, want := len(report.Steps), 6; got != want {
		t.Fatalf("steps = %d, want %d", got, want)
	}
	for _, id := range []string{
		"bootstrap_bearer_ws",
		"job_log_stream",
		"disconnect_reconnect_replay",
		"mac_sleep_reconnect",
		"daemon_restart_recovery",
		"secret_rotation_invalidation",
	} {
		step, ok := report.Step(id)
		if !ok {
			t.Fatalf("missing step %s", id)
		}
		if !step.Passed {
			t.Fatalf("step %s did not pass: %s", id, step.Error)
		}
		if len(step.Evidence) == 0 {
			t.Fatalf("step %s recorded no evidence", id)
		}
	}
	replay, ok := report.Step("disconnect_reconnect_replay")
	if !ok {
		t.Fatal("missing replay step")
	}
	if replay.Evidence["replayedEvents"] != "3" || replay.Evidence["uniqueApplied"] != "4" {
		t.Fatalf("bad replay evidence: %#v", replay.Evidence)
	}
	sleep, ok := report.Step("mac_sleep_reconnect")
	if !ok {
		t.Fatal("missing sleep step")
	}
	if !strings.Contains(sleep.Evidence["fsmTrace"], "ready -> reconnecting -> tunnel_connecting -> authenticating -> ready") {
		t.Fatalf("bad sleep trace: %#v", sleep.Evidence)
	}
	if _, ok := report.Metric("mac_sleep_reconnect_p95"); !ok {
		t.Fatalf("missing reconnect p95 metric: %#v", report.Metrics)
	}
	rotation, ok := report.Step("secret_rotation_invalidation")
	if !ok {
		t.Fatal("missing secret rotation step")
	}
	if rotation.Evidence["bearerInvalidated"] != "true" || rotation.Evidence["wsInvalidated"] != "true" || rotation.Evidence["sessionsAfter"] != "0" {
		t.Fatalf("bad secret rotation evidence: %#v", rotation.Evidence)
	}
}

func TestPhase2AcceptanceSmokeFailsWhenReconnectSLOIsTooTight(t *testing.T) {
	_, err := RunPhase2Smoke(context.Background(), Config{
		WorkDir:      t.TempDir(),
		Now:          fixedNow,
		ReconnectSLO: time.Second,
		ReplaySLO:    5 * time.Second,
	})
	if err == nil {
		t.Fatal("expected reconnect SLO failure")
	}
	if !strings.Contains(err.Error(), "mac_sleep_reconnect") {
		t.Fatalf("failure did not identify sleep step: %v", err)
	}
}

func TestPhase2AcceptanceSmokeReportIsJSONEvidence(t *testing.T) {
	report, err := RunPhase2Smoke(context.Background(), Config{
		WorkDir: t.TempDir(),
		Now:     fixedNow,
	})
	if err != nil {
		t.Fatalf("phase2 smoke failed: %v", err)
	}
	data, err := json.Marshal(report)
	if err != nil {
		t.Fatalf("marshal report: %v", err)
	}
	if !strings.Contains(string(data), `"schemaVersion":1`) {
		t.Fatalf("report missing schema version: %s", string(data))
	}
	if !strings.Contains(string(data), `"daemon_restart_recovery"`) {
		t.Fatalf("report missing restart evidence: %s", string(data))
	}
}

func TestPhase2AcceptanceSmokeEvidenceDoesNotLeakCredentialMaterial(t *testing.T) {
	report, err := RunPhase2Smoke(context.Background(), Config{
		WorkDir: t.TempDir(),
		Now:     fixedNow,
	})
	if err != nil {
		t.Fatalf("phase2 smoke failed: %v", err)
	}
	for _, step := range report.Steps {
		for key, value := range step.Evidence {
			if looksLikeBearerOrWSToken(value) {
				t.Fatalf("step %s evidence %s leaked bearer/ws-shaped token", step.ID, key)
			}
			if looksLikePairingToken(value) {
				t.Fatalf("step %s evidence %s leaked pairing-token-shaped value", step.ID, key)
			}
		}
	}
}

func fixedNow() time.Time {
	return time.Date(2026, 5, 4, 12, 0, 0, 0, time.UTC)
}

func looksLikeBearerOrWSToken(value string) bool {
	parts := strings.Split(value, ".")
	if len(parts) != 2 {
		return false
	}
	return len(parts[0]) > 40 && len(parts[1]) > 30
}

func looksLikePairingToken(value string) bool {
	if len(value) != auth.PairingTokenLength {
		return false
	}
	for _, ch := range value {
		if !strings.ContainsRune(auth.PairingAlphabet, ch) {
			return false
		}
	}
	return true
}
