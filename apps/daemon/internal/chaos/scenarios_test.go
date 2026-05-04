package chaos

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestDefaultSuitePassesBaseFaults(t *testing.T) {
	report, err := RunDefault(context.Background(), Config{
		Now:     fixedNow,
		WorkDir: t.TempDir(),
	})
	if err != nil {
		t.Fatalf("default chaos suite failed: %v", err)
	}
	if err := report.RequirePassed(BaseFaults...); err != nil {
		t.Fatal(err)
	}
	for _, kind := range BaseFaults {
		result, ok := report.Result(kind)
		if !ok {
			t.Fatalf("missing result for %s", kind)
		}
		if len(result.Observations) == 0 {
			t.Fatalf("%s recorded no observations", kind)
		}
	}
}

func TestDefaultSuiteIncludesAdditionalFaultSubstrate(t *testing.T) {
	report, err := RunDefault(context.Background(), Config{
		Now:     fixedNow,
		WorkDir: t.TempDir(),
	})
	if err != nil {
		t.Fatalf("default chaos suite failed: %v", err)
	}
	if err := report.RequirePassed(AdditionalFaults...); err != nil {
		t.Fatal(err)
	}
	if got, want := len(report.Results), len(BaseFaults)+len(AdditionalFaults)+len(SleepWakeFaults); got != want {
		t.Fatalf("result count = %d, want %d", got, want)
	}
}

func TestDefaultSuiteIncludesSleepWakeFaults(t *testing.T) {
	report, err := RunDefault(context.Background(), Config{
		Now:     fixedNow,
		WorkDir: t.TempDir(),
	})
	if err != nil {
		t.Fatalf("default chaos suite failed: %v", err)
	}
	if err := report.RequirePassed(SleepWakeFaults...); err != nil {
		t.Fatal(err)
	}
	if got, want := len(report.Results), len(BaseFaults)+len(AdditionalFaults)+len(SleepWakeFaults); got != want {
		t.Fatalf("result count = %d, want %d", got, want)
	}
}

func TestSleepWakeActiveSwarmAssertsReplayAndSLO(t *testing.T) {
	report, err := RunDefault(context.Background(), Config{
		Scenarios: []Scenario{
			{
				Kind: FaultSleepWakeActiveSwarm,
				Name: "sleep wake active swarm",
				Run:  runSleepWakeActiveSwarm,
			},
		},
		Now:          fixedNow,
		WorkDir:      t.TempDir(),
		ReconnectSLO: 3 * time.Second,
		ReplaySLO:    2 * time.Second,
	})
	if err != nil {
		t.Fatalf("sleep/wake active swarm scenario failed: %v", err)
	}
	result, ok := report.Result(FaultSleepWakeActiveSwarm)
	if !ok {
		t.Fatal("missing sleep/wake active swarm result")
	}
	if !hasObservation(result.Observations, "swarm.vps_continued", "true") {
		t.Fatalf("missing VPS continuity observation: %#v", result.Observations)
	}
	if got := metricValue(result.Metrics, "sleep_replayed_events"); got != 6 {
		t.Fatalf("sleep_replayed_events = %v, want 6", got)
	}
	if got := metricValue(result.Metrics, "wake_reconnect_p95"); got <= 0 || got >= 3 {
		t.Fatalf("wake_reconnect_p95 = %v, want under 3s", got)
	}
}

func TestSleepWakeBuildStreamResumesByOffset(t *testing.T) {
	report, err := RunDefault(context.Background(), Config{
		Scenarios: []Scenario{
			{
				Kind: FaultSleepWakeBuildStream,
				Name: "sleep wake build stream",
				Run:  runSleepWakeBuildStream,
			},
		},
		Now:     fixedNow,
		WorkDir: t.TempDir(),
	})
	if err != nil {
		t.Fatalf("sleep/wake build stream scenario failed: %v", err)
	}
	result, ok := report.Result(FaultSleepWakeBuildStream)
	if !ok {
		t.Fatal("missing sleep/wake build stream result")
	}
	if !hasObservation(result.Observations, "build_log.duplicates", "false") {
		t.Fatalf("missing duplicate-protection observation: %#v", result.Observations)
	}
	if metricValue(result.Metrics, "log_total_offset") <= metricValue(result.Metrics, "log_resume_offset") {
		t.Fatalf("log offsets did not advance: %#v", result.Metrics)
	}
}

func TestSlowRendererRecordsLagOffset(t *testing.T) {
	report, err := RunDefault(context.Background(), Config{
		Scenarios: []Scenario{
			{
				Kind: FaultSlowRenderer,
				Name: "slow renderer",
				Run:  runSlowRenderer,
			},
		},
		Now:     fixedNow,
		WorkDir: t.TempDir(),
	})
	if err != nil {
		t.Fatalf("slow renderer scenario failed: %v", err)
	}
	result, ok := report.Result(FaultSlowRenderer)
	if !ok {
		t.Fatal("missing slow renderer result")
	}
	if metricValue(result.Metrics, "last_persisted_offset") == 0 {
		t.Fatalf("missing last_persisted_offset metric: %#v", result.Metrics)
	}
}

func TestMalformedAdapterScenarioRedactsAuditPreview(t *testing.T) {
	report, err := RunDefault(context.Background(), Config{
		Scenarios: []Scenario{
			{
				Kind: FaultMalformedAdapterOutput,
				Name: "malformed adapter",
				Run:  runMalformedAdapterOutput,
			},
		},
		Now:     fixedNow,
		WorkDir: t.TempDir(),
	})
	if err != nil {
		t.Fatalf("malformed adapter scenario failed: %v", err)
	}
	result, ok := report.Result(FaultMalformedAdapterOutput)
	if !ok {
		t.Fatal("missing malformed adapter result")
	}
	if !hasObservation(result.Observations, "audit.redacted", "true") {
		t.Fatalf("missing audit redaction observation: %#v", result.Observations)
	}
	if strings.Contains(result.Error, "abcdefghijklmnopqrstuvwxyz123456") {
		t.Fatalf("error leaked bearer payload: %s", result.Error)
	}
}

func TestReportRequirePassedSurfacesMissingAndFailedFaults(t *testing.T) {
	report := Report{
		Results: []Result{
			{Kind: FaultTunnelDrop, Passed: false, Error: "no replay"},
		},
	}
	if err := report.RequirePassed(FaultTunnelDrop); err == nil || !strings.Contains(err.Error(), "no replay") {
		t.Fatalf("expected failed fault error, got %v", err)
	}
	if err := report.RequirePassed(FaultDaemonRestart); err == nil || !strings.Contains(err.Error(), "missing fault coverage") {
		t.Fatalf("expected missing coverage error, got %v", err)
	}
}

func fixedNow() time.Time {
	return time.Date(2026, 5, 4, 12, 0, 0, 0, time.UTC)
}

func metricValue(metrics []Metric, name string) float64 {
	for _, metric := range metrics {
		if metric.Name == name {
			return metric.Value
		}
	}
	return 0
}

func hasObservation(observations []Observation, key string, value string) bool {
	for _, observation := range observations {
		if observation.Key == key && observation.Value == value {
			return true
		}
	}
	return false
}
