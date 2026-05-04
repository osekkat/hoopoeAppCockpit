// Package metrics provides the daemon's in-process dev metrics registry.
//
// The registry is intentionally local and bounded: it is useful for Mock
// Flywheel, Diagnostics, and CI regression checks without becoming a canonical
// datastore or a production telemetry pipeline.
package metrics

import (
	"errors"
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	SchemaVersion = 1

	DefaultMaxSamples = 2048

	MetricEventsReplayedTotal     = "events_replayed_total"
	MetricRequestDurationSeconds  = "request_duration_seconds"
	MetricLogFetchDurationSeconds = "log_fetch_duration_seconds"
	MetricActiveWSConnections     = "active_ws_connections"
	MetricInFlightJobs            = "in_flight_jobs"

	MetricDesktopReconnectAfterWakeMS  = "desktop_reconnect_after_wake_ms"
	MetricEventReplayAfterDisconnectMS = "event_replay_after_disconnect_ms"
	MetricActivityEventDisplayMS       = "activity_event_display_latency_ms"
	MetricTerminalStreamAttachMS       = "terminal_stream_attach_ms"
	MetricLocalFileOpenCachedMS        = "local_file_open_cached_ms"
	MetricBeadKanbanLoadMS             = "bead_kanban_load_ms"
	MetricDAGVisibleNodes              = "dag_visible_nodes"
	MetricJobCancellationOrphans       = "job_cancellation_orphans"
)

var (
	ErrInvalidMetric = errors.New("metrics: invalid metric")
	ErrInvalidTarget = errors.New("metrics: invalid target")
)

type Kind string

const (
	KindCounter   Kind = "counter"
	KindGauge     Kind = "gauge"
	KindHistogram Kind = "histogram"
)

type Unit string

const (
	UnitCount        Unit = "count"
	UnitSeconds      Unit = "seconds"
	UnitMilliseconds Unit = "milliseconds"
	UnitNodes        Unit = "nodes"
)

type Comparator string

const (
	ComparatorP95LessEqual    Comparator = "p95_le"
	ComparatorMaxLessEqual    Comparator = "max_le"
	ComparatorLatestLessEqual Comparator = "latest_le"
	ComparatorLatestGreaterEq Comparator = "latest_ge"
	ComparatorLatestEqual     Comparator = "latest_eq"
)

type TargetStatus string

const (
	TargetMissing TargetStatus = "missing"
	TargetPass    TargetStatus = "pass"
	TargetFail    TargetStatus = "fail"
)

type Labels map[string]string

type Config struct {
	Now                   func() time.Time
	MaxSamples            int
	IncludeDefaultTargets bool
}

type Registry struct {
	mu         sync.RWMutex
	now        func() time.Time
	maxSamples int
	counters   map[string]*counterSeries
	gauges     map[string]*gaugeSeries
	histograms map[string]*histogramSeries
	targets    []Target
}

type Target struct {
	ID          string     `json:"id"`
	Area        string     `json:"area"`
	Metric      string     `json:"metric"`
	Source      string     `json:"source"`
	Description string     `json:"description"`
	Comparator  Comparator `json:"comparator"`
	Threshold   float64    `json:"threshold"`
	Unit        Unit       `json:"unit"`
}

type TargetReport struct {
	Target      Target       `json:"target"`
	Status      TargetStatus `json:"status"`
	Observed    *float64     `json:"observed,omitempty"`
	SampleCount uint64       `json:"sampleCount"`
	Message     string       `json:"message,omitempty"`
}

type Snapshot struct {
	SchemaVersion int            `json:"schemaVersion"`
	GeneratedAt   time.Time      `json:"generatedAt"`
	Series        []Series       `json:"series"`
	Targets       []TargetReport `json:"targets"`
}

type Series struct {
	Name   string `json:"name"`
	Kind   Kind   `json:"kind"`
	Unit   Unit   `json:"unit"`
	Labels Labels `json:"labels,omitempty"`

	Value  *float64 `json:"value,omitempty"`
	Latest *float64 `json:"latest,omitempty"`
	Count  uint64   `json:"count,omitempty"`
	Sum    float64  `json:"sum,omitempty"`
	Min    *float64 `json:"min,omitempty"`
	Max    *float64 `json:"max,omitempty"`
	P50    *float64 `json:"p50,omitempty"`
	P95    *float64 `json:"p95,omitempty"`
}

type counterSeries struct {
	name   string
	labels Labels
	value  float64
}

type gaugeSeries struct {
	name   string
	labels Labels
	value  float64
}

type histogramSeries struct {
	name    string
	labels  Labels
	unit    Unit
	count   uint64
	sum     float64
	min     float64
	max     float64
	samples []float64
	next    int
	full    bool
}

func NewRegistry(cfg Config) *Registry {
	now := cfg.Now
	if now == nil {
		now = time.Now
	}
	maxSamples := cfg.MaxSamples
	if maxSamples <= 0 {
		maxSamples = DefaultMaxSamples
	}
	r := &Registry{
		now:        now,
		maxSamples: maxSamples,
		counters:   make(map[string]*counterSeries),
		gauges:     make(map[string]*gaugeSeries),
		histograms: make(map[string]*histogramSeries),
	}
	if cfg.IncludeDefaultTargets {
		_ = r.RegisterTargets(DefaultTargets()...)
	}
	return r
}

func (r *Registry) RegisterTargets(targets ...Target) error {
	if r == nil {
		return ErrInvalidMetric
	}
	for _, target := range targets {
		if err := validateTarget(target); err != nil {
			return err
		}
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, target := range targets {
		replaced := false
		for i := range r.targets {
			if r.targets[i].ID == target.ID {
				r.targets[i] = target
				replaced = true
				break
			}
		}
		if !replaced {
			r.targets = append(r.targets, target)
		}
	}
	sort.Slice(r.targets, func(i, j int) bool { return r.targets[i].ID < r.targets[j].ID })
	return nil
}

func (r *Registry) IncCounter(name string, labels Labels, delta float64) error {
	if r == nil {
		return ErrInvalidMetric
	}
	if delta < 0 || math.IsNaN(delta) || math.IsInf(delta, 0) {
		return fmt.Errorf("%w: counter delta must be finite and non-negative", ErrInvalidMetric)
	}
	normalized, err := normalizeLabels(labels)
	if err != nil {
		return err
	}
	if err := validateName(name); err != nil {
		return err
	}
	key := seriesKey(name, normalized)
	r.mu.Lock()
	defer r.mu.Unlock()
	series := r.counters[key]
	if series == nil {
		series = &counterSeries{name: name, labels: normalized}
		r.counters[key] = series
	}
	series.value += delta
	return nil
}

func (r *Registry) SetGauge(name string, labels Labels, value float64) error {
	if r == nil {
		return ErrInvalidMetric
	}
	if math.IsNaN(value) || math.IsInf(value, 0) {
		return fmt.Errorf("%w: gauge value must be finite", ErrInvalidMetric)
	}
	normalized, err := normalizeLabels(labels)
	if err != nil {
		return err
	}
	if err := validateName(name); err != nil {
		return err
	}
	key := seriesKey(name, normalized)
	r.mu.Lock()
	defer r.mu.Unlock()
	series := r.gauges[key]
	if series == nil {
		series = &gaugeSeries{name: name, labels: normalized}
		r.gauges[key] = series
	}
	series.value = value
	return nil
}

func (r *Registry) AddGauge(name string, labels Labels, delta float64) error {
	if r == nil {
		return ErrInvalidMetric
	}
	if math.IsNaN(delta) || math.IsInf(delta, 0) {
		return fmt.Errorf("%w: gauge delta must be finite", ErrInvalidMetric)
	}
	normalized, err := normalizeLabels(labels)
	if err != nil {
		return err
	}
	if err := validateName(name); err != nil {
		return err
	}
	key := seriesKey(name, normalized)
	r.mu.Lock()
	defer r.mu.Unlock()
	series := r.gauges[key]
	if series == nil {
		series = &gaugeSeries{name: name, labels: normalized}
		r.gauges[key] = series
	}
	series.value += delta
	return nil
}

func (r *Registry) Observe(name string, labels Labels, value float64, unit Unit) error {
	if r == nil {
		return ErrInvalidMetric
	}
	if math.IsNaN(value) || math.IsInf(value, 0) || value < 0 {
		return fmt.Errorf("%w: observation must be finite and non-negative", ErrInvalidMetric)
	}
	normalized, err := normalizeLabels(labels)
	if err != nil {
		return err
	}
	if err := validateName(name); err != nil {
		return err
	}
	if !unit.Valid() {
		return fmt.Errorf("%w: invalid unit %q", ErrInvalidMetric, unit)
	}
	key := seriesKey(name, normalized)
	r.mu.Lock()
	defer r.mu.Unlock()
	series := r.histograms[key]
	if series == nil {
		series = &histogramSeries{name: name, labels: normalized, unit: unit}
		r.histograms[key] = series
	}
	series.observe(value, r.maxSamples)
	return nil
}

func (r *Registry) ObserveDuration(name string, labels Labels, duration time.Duration) error {
	if strings.HasSuffix(name, "_ms") || strings.HasSuffix(name, "_milliseconds") {
		return r.Observe(name, labels, float64(duration)/float64(time.Millisecond), UnitMilliseconds)
	}
	return r.Observe(name, labels, duration.Seconds(), UnitSeconds)
}

func (r *Registry) Snapshot() Snapshot {
	if r == nil {
		return Snapshot{}
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	series := make([]Series, 0, len(r.counters)+len(r.gauges)+len(r.histograms))
	for _, item := range r.counters {
		value := item.value
		series = append(series, Series{
			Name:   item.name,
			Kind:   KindCounter,
			Unit:   UnitCount,
			Labels: cloneLabels(item.labels),
			Value:  &value,
		})
	}
	for _, item := range r.gauges {
		value := item.value
		series = append(series, Series{
			Name:   item.name,
			Kind:   KindGauge,
			Unit:   inferUnit(item.name),
			Labels: cloneLabels(item.labels),
			Value:  &value,
			Latest: &value,
		})
	}
	for _, item := range r.histograms {
		series = append(series, item.snapshot())
	}
	sort.Slice(series, func(i, j int) bool {
		if series[i].Name != series[j].Name {
			return series[i].Name < series[j].Name
		}
		return encodeLabels(series[i].Labels) < encodeLabels(series[j].Labels)
	})
	return Snapshot{
		SchemaVersion: SchemaVersion,
		GeneratedAt:   r.now().UTC(),
		Series:        series,
		Targets:       r.evaluateTargetsLocked(),
	}
}

func (r *Registry) PrometheusText() string {
	snapshot := r.Snapshot()
	var b strings.Builder
	b.WriteString("# HELP hoopoe_metrics_schema_version Hoopoe metrics schema version.\n")
	b.WriteString("# TYPE hoopoe_metrics_schema_version gauge\n")
	b.WriteString("hoopoe_metrics_schema_version ")
	b.WriteString(strconv.Itoa(snapshot.SchemaVersion))
	b.WriteByte('\n')
	for _, series := range snapshot.Series {
		writePrometheusSeries(&b, series)
	}
	return b.String()
}

func (s Snapshot) SeriesByName(name string) []Series {
	out := make([]Series, 0)
	for _, series := range s.Series {
		if series.Name == name {
			out = append(out, series)
		}
	}
	return out
}

func (u Unit) Valid() bool {
	switch u {
	case UnitCount, UnitSeconds, UnitMilliseconds, UnitNodes:
		return true
	default:
		return false
	}
}

func (h *histogramSeries) observe(value float64, maxSamples int) {
	if h.count == 0 {
		h.min = value
		h.max = value
	} else {
		if value < h.min {
			h.min = value
		}
		if value > h.max {
			h.max = value
		}
	}
	h.count++
	h.sum += value
	if maxSamples <= 0 {
		maxSamples = DefaultMaxSamples
	}
	if len(h.samples) < maxSamples {
		h.samples = append(h.samples, value)
		return
	}
	h.samples[h.next] = value
	h.next = (h.next + 1) % maxSamples
	h.full = true
}

func (h *histogramSeries) snapshot() Series {
	samples := h.orderedSamples()
	minValue := h.min
	maxValue := h.max
	p50 := percentile(samples, 0.50)
	p95 := percentile(samples, 0.95)
	return Series{
		Name:   h.name,
		Kind:   KindHistogram,
		Unit:   h.unit,
		Labels: cloneLabels(h.labels),
		Count:  h.count,
		Sum:    h.sum,
		Min:    &minValue,
		Max:    &maxValue,
		P50:    &p50,
		P95:    &p95,
	}
}

func (h *histogramSeries) orderedSamples() []float64 {
	if len(h.samples) == 0 {
		return nil
	}
	out := make([]float64, len(h.samples))
	if !h.full {
		copy(out, h.samples)
		return out
	}
	copy(out, h.samples[h.next:])
	copy(out[len(h.samples)-h.next:], h.samples[:h.next])
	return out
}

func (r *Registry) evaluateTargetsLocked() []TargetReport {
	reports := make([]TargetReport, 0, len(r.targets))
	for _, target := range r.targets {
		report := TargetReport{Target: target, Status: TargetMissing}
		observed, samples, ok := r.observedForTargetLocked(target)
		report.SampleCount = samples
		if !ok {
			report.Message = "metric has not been observed"
			reports = append(reports, report)
			continue
		}
		report.Observed = &observed
		if target.compare(observed) {
			report.Status = TargetPass
			reports = append(reports, report)
			continue
		}
		report.Status = TargetFail
		report.Message = fmt.Sprintf("%s observed %.3f, threshold %.3f", target.Comparator, observed, target.Threshold)
		reports = append(reports, report)
	}
	return reports
}

func (r *Registry) observedForTargetLocked(target Target) (float64, uint64, bool) {
	switch target.Comparator {
	case ComparatorP95LessEqual, ComparatorMaxLessEqual:
		var values []float64
		var total uint64
		var maxValue float64
		for _, series := range r.histograms {
			if series.name != target.Metric {
				continue
			}
			total += series.count
			if total == series.count || series.max > maxValue {
				maxValue = series.max
			}
			values = append(values, series.orderedSamples()...)
		}
		if total == 0 {
			return 0, 0, false
		}
		if target.Comparator == ComparatorMaxLessEqual {
			return maxValue, total, true
		}
		return percentile(values, 0.95), total, true
	case ComparatorLatestLessEqual, ComparatorLatestGreaterEq, ComparatorLatestEqual:
		var (
			latest float64
			found  bool
		)
		for _, series := range r.gauges {
			if series.name != target.Metric {
				continue
			}
			latest += series.value
			found = true
		}
		if !found {
			return 0, 0, false
		}
		return latest, 1, true
	default:
		return 0, 0, false
	}
}

func (t Target) compare(observed float64) bool {
	switch t.Comparator {
	case ComparatorP95LessEqual, ComparatorMaxLessEqual, ComparatorLatestLessEqual:
		return observed <= t.Threshold
	case ComparatorLatestGreaterEq:
		return observed >= t.Threshold
	case ComparatorLatestEqual:
		return observed == t.Threshold
	default:
		return false
	}
}

func validateTarget(target Target) error {
	if target.ID == "" || target.Area == "" || target.Metric == "" {
		return fmt.Errorf("%w: id, area, and metric are required", ErrInvalidTarget)
	}
	if err := validateName(target.Metric); err != nil {
		return err
	}
	if !target.Unit.Valid() {
		return fmt.Errorf("%w: invalid unit %q", ErrInvalidTarget, target.Unit)
	}
	switch target.Comparator {
	case ComparatorP95LessEqual, ComparatorMaxLessEqual, ComparatorLatestLessEqual, ComparatorLatestGreaterEq, ComparatorLatestEqual:
	default:
		return fmt.Errorf("%w: invalid comparator %q", ErrInvalidTarget, target.Comparator)
	}
	if math.IsNaN(target.Threshold) || math.IsInf(target.Threshold, 0) {
		return fmt.Errorf("%w: threshold must be finite", ErrInvalidTarget)
	}
	return nil
}

func validateName(name string) error {
	if name == "" || len(name) > 128 {
		return fmt.Errorf("%w: invalid name %q", ErrInvalidMetric, name)
	}
	for i, r := range name {
		switch {
		case r >= 'a' && r <= 'z':
		case r >= 'A' && r <= 'Z':
		case r >= '0' && r <= '9' && i > 0:
		case r == '_' || r == ':':
		default:
			return fmt.Errorf("%w: invalid name %q", ErrInvalidMetric, name)
		}
	}
	return nil
}

func normalizeLabels(labels Labels) (Labels, error) {
	if len(labels) == 0 {
		return nil, nil
	}
	out := make(Labels, len(labels))
	for key, value := range labels {
		if err := validateName(key); err != nil {
			return nil, err
		}
		cleanValue := strings.TrimSpace(value)
		if len(cleanValue) > 160 {
			return nil, fmt.Errorf("%w: label value too long for %q", ErrInvalidMetric, key)
		}
		out[key] = cleanValue
	}
	return out, nil
}

func seriesKey(name string, labels Labels) string {
	return name + "\x00" + encodeLabels(labels)
}

func encodeLabels(labels Labels) string {
	if len(labels) == 0 {
		return ""
	}
	keys := make([]string, 0, len(labels))
	for key := range labels {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, key := range keys {
		parts = append(parts, key+"="+labels[key])
	}
	return strings.Join(parts, ",")
}

func cloneLabels(labels Labels) Labels {
	if len(labels) == 0 {
		return nil
	}
	out := make(Labels, len(labels))
	for key, value := range labels {
		out[key] = value
	}
	return out
}

func percentile(values []float64, q float64) float64 {
	if len(values) == 0 {
		return 0
	}
	sorted := append([]float64(nil), values...)
	sort.Float64s(sorted)
	if len(sorted) == 1 {
		return sorted[0]
	}
	index := int(math.Ceil(q*float64(len(sorted)))) - 1
	if index < 0 {
		index = 0
	}
	if index >= len(sorted) {
		index = len(sorted) - 1
	}
	return sorted[index]
}

func inferUnit(name string) Unit {
	switch {
	case strings.HasSuffix(name, "_seconds"):
		return UnitSeconds
	case strings.HasSuffix(name, "_ms") || strings.HasSuffix(name, "_milliseconds"):
		return UnitMilliseconds
	case strings.Contains(name, "nodes"):
		return UnitNodes
	default:
		return UnitCount
	}
}
