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
	if got, want := len(report.Results), len(BaseFaults)+len(AdditionalFaults); got != want {
		t.Fatalf("result count = %d, want %d", got, want)
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
