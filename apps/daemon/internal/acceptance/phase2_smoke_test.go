package acceptance

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"
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
	if got, want := len(report.Steps), 5; got != want {
		t.Fatalf("steps = %d, want %d", got, want)
	}
	for _, id := range []string{
		"bootstrap_bearer_ws",
		"job_log_stream",
		"disconnect_reconnect_replay",
		"mac_sleep_reconnect",
		"daemon_restart_recovery",
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

func fixedNow() time.Time {
	return time.Date(2026, 5, 4, 12, 0, 0, 0, time.UTC)
}
