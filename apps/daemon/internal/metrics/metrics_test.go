package metrics

import (
	"strings"
	"testing"
	"time"
)

func TestRegistryRecordsBoundedSeriesAndEvaluatesTargets(t *testing.T) {
	now := time.Date(2026, 5, 4, 12, 0, 0, 0, time.UTC)
	registry := NewRegistry(Config{
		Now:                   func() time.Time { return now },
		MaxSamples:            3,
		IncludeDefaultTargets: true,
	})
	for _, value := range []time.Duration{
		1200 * time.Millisecond,
		1400 * time.Millisecond,
		1600 * time.Millisecond,
		1800 * time.Millisecond,
	} {
		if err := registry.ObserveDuration(MetricEventReplayAfterDisconnectMS, nil, value); err != nil {
			t.Fatalf("ObserveDuration: %v", err)
		}
	}
	if err := registry.SetGauge(MetricJobCancellationOrphans, nil, 0); err != nil {
		t.Fatalf("SetGauge: %v", err)
	}
	if err := registry.IncCounter(MetricEventsReplayedTotal, Labels{"channel": "swarm"}, 4); err != nil {
		t.Fatalf("IncCounter: %v", err)
	}

	snapshot := registry.Snapshot()
	if snapshot.SchemaVersion != SchemaVersion || !snapshot.GeneratedAt.Equal(now) {
		t.Fatalf("snapshot metadata = %+v", snapshot)
	}
	replay := firstSeries(snapshot, MetricEventReplayAfterDisconnectMS)
	if replay == nil || replay.Count != 4 || replay.P95 == nil || *replay.P95 != 1800 {
		t.Fatalf("replay series = %+v", replay)
	}
	target := targetReport(snapshot, "event_replay_after_disconnect")
	if target == nil || target.Status != TargetPass || target.SampleCount != 4 {
		t.Fatalf("target report = %+v", target)
	}
	orphanTarget := targetReport(snapshot, "job_cancellation_orphans")
	if orphanTarget == nil || orphanTarget.Status != TargetPass || orphanTarget.Observed == nil || *orphanTarget.Observed != 0 {
		t.Fatalf("orphan target = %+v", orphanTarget)
	}
	missing := targetReport(snapshot, "desktop_reconnect_after_wake")
	if missing == nil || missing.Status != TargetMissing {
		t.Fatalf("missing target = %+v", missing)
	}
}

func TestRegistryFailsTargetWhenObservedValueExceedsThreshold(t *testing.T) {
	registry := NewRegistry(Config{})
	if err := registry.RegisterTargets(Target{
		ID:         "slow",
		Area:       "slow path",
		Metric:     "slow_path_ms",
		Comparator: ComparatorP95LessEqual,
		Threshold:  10,
		Unit:       UnitMilliseconds,
	}); err != nil {
		t.Fatalf("RegisterTargets: %v", err)
	}
	if err := registry.Observe("slow_path_ms", nil, 15, UnitMilliseconds); err != nil {
		t.Fatalf("Observe: %v", err)
	}
	report := targetReport(registry.Snapshot(), "slow")
	if report == nil || report.Status != TargetFail || report.Observed == nil || *report.Observed != 15 {
		t.Fatalf("target report = %+v", report)
	}
}

func TestPrometheusTextIsDeterministicAndEscapesLabels(t *testing.T) {
	registry := NewRegistry(Config{})
	if err := registry.ObserveDuration(MetricRequestDurationSeconds, Labels{
		"method": "GET",
		"route":  `/v1/"quoted"`,
	}, 125*time.Millisecond); err != nil {
		t.Fatalf("ObserveDuration: %v", err)
	}
	if err := registry.ObserveDuration(MetricRequestDurationSeconds, Labels{
		"method": "POST",
		"route":  "/v1/jobs",
	}, 250*time.Millisecond); err != nil {
		t.Fatalf("ObserveDuration second label set: %v", err)
	}
	text := registry.PrometheusText()
	for _, want := range []string{
		"hoopoe_metrics_schema_version 1",
		`hoopoe_request_duration_seconds_count{method="GET",route="/v1/\"quoted\""} 1`,
		`hoopoe_request_duration_seconds_p95{method="GET",route="/v1/\"quoted\""} 0.125`,
		`hoopoe_request_duration_seconds_count{method="POST",route="/v1/jobs"} 1`,
		`hoopoe_request_duration_seconds_p95{method="POST",route="/v1/jobs"} 0.25`,
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("prometheus text missing %q:\n%s", want, text)
		}
	}
	for _, want := range []string{
		"# TYPE hoopoe_request_duration_seconds_count counter",
		"# TYPE hoopoe_request_duration_seconds_sum counter",
		"# TYPE hoopoe_request_duration_seconds_p95 gauge",
		"# TYPE hoopoe_request_duration_seconds_max gauge",
	} {
		if count := strings.Count(text, want); count != 1 {
			t.Fatalf("prometheus type line %q appears %d times:\n%s", want, count, text)
		}
	}
}

func firstSeries(snapshot Snapshot, name string) *Series {
	for i := range snapshot.Series {
		if snapshot.Series[i].Name == name {
			return &snapshot.Series[i]
		}
	}
	return nil
}

func targetReport(snapshot Snapshot, id string) *TargetReport {
	for i := range snapshot.Targets {
		if snapshot.Targets[i].Target.ID == id {
			return &snapshot.Targets[i]
		}
	}
	return nil
}
