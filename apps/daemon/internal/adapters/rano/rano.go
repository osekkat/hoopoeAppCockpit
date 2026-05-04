// Package rano wraps the diagnostics-only network observer for AI CLI calls.
//
// rano is an optional v1 signal source. It reports per-call latency and
// error observations for subscription-backed AI CLIs, but it is not a
// source of truth for subscription quota and is not wired into top-bar or
// tending decisions in v1.
package rano

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"os/exec"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/hoopoe-cockpit/hoopoe/apps/daemon/internal/capabilities"
)

const (
	ToolName = "rano"

	CapabilityPresent     = "rano._present"
	CapabilitySignalsRead = "rano.signals.read"

	defaultMaxStdoutBytes    = 4 << 20
	defaultSignalsTimeout    = 10 * time.Second
	defaultProbeTimeout      = 5 * time.Second
	defaultSignalsWindow     = 15 * time.Minute
	minCompatibleVersion     = "0.1.0"
	referenceFixturesVersion = "phase0-rano-contract-2026-05-04"
)

var (
	ErrInvalidRequest     = errors.New("rano: invalid request")
	ErrMissingBinary      = errors.New("rano: binary not found")
	ErrOutputTooLarge     = errors.New("rano: command output exceeded limit")
	ErrCommandContract    = errors.New("rano: command contract violation")
	ErrUnsupportedVersion = errors.New("rano: unsupported version")
	ErrRedactionContract  = errors.New("rano: observation included raw payload fields")
)

type Runner interface {
	Run(ctx context.Context, argv []string) (CommandResult, error)
}

type CommandResult struct {
	ExitCode int
	Stdout   []byte
	Stderr   []byte
}

type ExecRunner struct{}

func (ExecRunner) Run(ctx context.Context, argv []string) (CommandResult, error) {
	if len(argv) == 0 || strings.TrimSpace(argv[0]) == "" {
		return CommandResult{}, fmt.Errorf("%w: empty argv", ErrInvalidRequest)
	}
	cmd := exec.CommandContext(ctx, argv[0], argv[1:]...)
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	result := CommandResult{Stdout: stdout.Bytes(), Stderr: stderr.Bytes()}
	if err == nil {
		return result, nil
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		result.ExitCode = exitErr.ExitCode()
		return result, nil
	}
	if isExecNotFoundErr(err) {
		result.ExitCode = -1
		return result, ErrMissingBinary
	}
	result.ExitCode = -1
	return result, err
}

type Adapter struct {
	Runner         Runner
	Now            func() time.Time
	MaxStdoutBytes int
	SignalsTimeout time.Duration
}

func New(runner Runner) *Adapter {
	if runner == nil {
		runner = ExecRunner{}
	}
	return &Adapter{Runner: runner, Now: time.Now, MaxStdoutBytes: defaultMaxStdoutBytes}
}

type SignalQuery struct {
	Since time.Duration
}

type Window struct {
	Start time.Time `json:"start,omitempty"`
	End   time.Time `json:"end,omitempty"`
}

type ObservationStatus string

const (
	StatusOK    ObservationStatus = "ok"
	StatusError ObservationStatus = "error"
)

type Observation struct {
	Timestamp        time.Time         `json:"timestamp"`
	Harness          string            `json:"harness"`
	Model            string            `json:"model,omitempty"`
	LatencyMS        int               `json:"latency_ms"`
	Status           ObservationStatus `json:"status"`
	ErrorClass       string            `json:"error_class,omitempty"`
	HTTPStatus       int               `json:"http_status,omitempty"`
	EndpointRedacted string            `json:"endpoint_redacted,omitempty"`
}

type Summary struct {
	WindowStart    time.Time `json:"window_start,omitempty"`
	WindowEnd      time.Time `json:"window_end,omitempty"`
	TotalCalls     int       `json:"total_calls"`
	ErrorCount     int       `json:"error_count"`
	LatencyP50MS   int       `json:"latency_p50_ms,omitempty"`
	LatencyP95MS   int       `json:"latency_p95_ms,omitempty"`
	LastErrorClass string    `json:"last_error_class,omitempty"`
}

type HarnessSummary struct {
	Harness        string `json:"harness"`
	Model          string `json:"model,omitempty"`
	TotalCalls     int    `json:"total_calls"`
	ErrorCount     int    `json:"error_count"`
	LatencyP50MS   int    `json:"latency_p50_ms,omitempty"`
	LatencyP95MS   int    `json:"latency_p95_ms,omitempty"`
	LastErrorClass string `json:"last_error_class,omitempty"`
}

type SignalSnapshot struct {
	GeneratedAt    time.Time        `json:"generated_at"`
	RanoVersion    string           `json:"rano_version,omitempty"`
	Window         Window           `json:"window"`
	Observations   []Observation    `json:"observations"`
	Summary        Summary          `json:"summary"`
	ByHarnessModel []HarnessSummary `json:"by_harness_model,omitempty"`
	Raw            []byte           `json:"-"`
}

type signalsDocument struct {
	GeneratedAt    time.Time        `json:"generated_at"`
	RanoVersion    string           `json:"rano_version,omitempty"`
	Window         Window           `json:"window"`
	Observations   []Observation    `json:"observations"`
	Calls          []Observation    `json:"calls"`
	Summary        Summary          `json:"summary"`
	ByHarnessModel []HarnessSummary `json:"by_harness_model,omitempty"`
}

func VersionArgv() []string {
	return []string{ToolName, "--version"}
}

func SignalsArgv(query SignalQuery) []string {
	return []string{ToolName, "signals", "--json", "--since", durationArg(durationOrDefault(query.Since))}
}

func (a *Adapter) ReadSignals(ctx context.Context, query SignalQuery) (SignalSnapshot, error) {
	window := durationOrDefault(query.Since)
	runCtx, cancel := context.WithTimeout(ctx, a.signalsTimeout())
	defer cancel()
	raw, err := a.runText(runCtx, SignalsArgv(SignalQuery{Since: window}))
	if err != nil {
		return SignalSnapshot{}, err
	}
	snapshot, err := ParseSignals(raw)
	if err != nil {
		return SignalSnapshot{}, err
	}
	if snapshot.GeneratedAt.IsZero() {
		snapshot.GeneratedAt = a.now().UTC()
	}
	if snapshot.Window.End.IsZero() {
		snapshot.Window.End = snapshot.GeneratedAt
	}
	if snapshot.Window.Start.IsZero() {
		snapshot.Window.Start = snapshot.Window.End.Add(-window)
	}
	if snapshot.Summary.WindowStart.IsZero() {
		snapshot.Summary.WindowStart = snapshot.Window.Start
	}
	if snapshot.Summary.WindowEnd.IsZero() {
		snapshot.Summary.WindowEnd = snapshot.Window.End
	}
	return snapshot, nil
}

func (a *Adapter) Probe(ctx context.Context) (*capabilities.ToolReport, error) {
	report := &capabilities.ToolReport{
		Tool:            capabilities.ToolRano,
		Source:          "cli",
		LastCheckedAt:   a.now().UTC().Format(time.RFC3339),
		FixturesVersion: referenceFixturesVersion,
		Capabilities:    defaultCapabilities("not probed"),
	}

	versionCtx, cancel := context.WithTimeout(ctx, defaultProbeTimeout)
	defer cancel()
	versionRaw, err := a.runText(versionCtx, VersionArgv())
	if err != nil {
		state := statusForError(err)
		for id, cap := range report.Capabilities {
			cap.Status = state
			cap.Notes = err.Error()
			report.Capabilities[id] = cap
		}
		return report, nil
	}
	report.Version = normalizeVersion(versionRaw)
	report.Capabilities[CapabilityPresent] = capabilities.Capability{Status: capabilities.StatusOK, Transport: "stdio"}
	if !compatibleVersion(report.Version) {
		note := fmt.Sprintf("%v: got %q, need >= %s", ErrUnsupportedVersion, report.Version, minCompatibleVersion)
		report.Capabilities[CapabilityPresent] = capabilities.Capability{Status: capabilities.StatusDegraded, Notes: note, Transport: "stdio"}
		report.Capabilities[CapabilitySignalsRead] = capabilities.Capability{Status: capabilities.StatusDegraded, Notes: note, Transport: "stdio"}
		return report, nil
	}

	report.Capabilities[CapabilitySignalsRead] = probeOne(ctx, CapabilitySignalsRead, "signals --json", func(ctx context.Context) error {
		_, err := a.ReadSignals(ctx, SignalQuery{Since: defaultSignalsWindow})
		return err
	})
	return report, nil
}

func ParseSignals(data []byte) (SignalSnapshot, error) {
	trimmed := bytes.TrimSpace(data)
	if len(trimmed) == 0 {
		return SignalSnapshot{}, fmt.Errorf("%w: empty signals output", ErrCommandContract)
	}
	if !json.Valid(trimmed) {
		return SignalSnapshot{}, fmt.Errorf("rano: decode signals: malformed JSON")
	}
	if err := rejectRawPayloadFields(trimmed); err != nil {
		return SignalSnapshot{}, err
	}

	var doc signalsDocument
	if err := json.Unmarshal(trimmed, &doc); err != nil {
		return SignalSnapshot{}, fmt.Errorf("rano: decode signals: %w", err)
	}
	observations := doc.Observations
	if len(observations) == 0 && len(doc.Calls) > 0 {
		observations = doc.Calls
	}
	for i := range observations {
		if err := normalizeObservation(&observations[i]); err != nil {
			return SignalSnapshot{}, fmt.Errorf("rano: observation %d: %w", i, err)
		}
	}
	window := deriveWindow(doc.Window, observations)
	summary := doc.Summary
	if summary.TotalCalls == 0 && len(observations) > 0 {
		summary = Summarize(window, observations)
	} else {
		if summary.WindowStart.IsZero() {
			summary.WindowStart = window.Start
		}
		if summary.WindowEnd.IsZero() {
			summary.WindowEnd = window.End
		}
	}
	byHarness := doc.ByHarnessModel
	if len(byHarness) == 0 && len(observations) > 0 {
		byHarness = SummarizeByHarnessModel(observations)
	}
	return SignalSnapshot{
		GeneratedAt:    doc.GeneratedAt,
		RanoVersion:    strings.TrimSpace(doc.RanoVersion),
		Window:         window,
		Observations:   observations,
		Summary:        summary,
		ByHarnessModel: byHarness,
		Raw:            append([]byte(nil), data...),
	}, nil
}

func Summarize(window Window, observations []Observation) Summary {
	latencies := make([]int, 0, len(observations))
	summary := Summary{
		WindowStart: window.Start,
		WindowEnd:   window.End,
		TotalCalls:  len(observations),
	}
	var lastErrorAt time.Time
	for _, obs := range observations {
		latencies = append(latencies, obs.LatencyMS)
		if !obs.failed() {
			continue
		}
		summary.ErrorCount++
		errClass := obs.normalizedErrorClass()
		if errClass == "" {
			continue
		}
		if obs.Timestamp.IsZero() || obs.Timestamp.After(lastErrorAt) || lastErrorAt.IsZero() {
			lastErrorAt = obs.Timestamp
			summary.LastErrorClass = errClass
		}
	}
	summary.LatencyP50MS = percentileMS(latencies, 0.50)
	summary.LatencyP95MS = percentileMS(latencies, 0.95)
	return summary
}

func SummarizeByHarnessModel(observations []Observation) []HarnessSummary {
	type bucket struct {
		harness string
		model   string
		items   []Observation
	}
	buckets := map[string]*bucket{}
	keys := make([]string, 0)
	for _, obs := range observations {
		key := obs.Harness + "\x00" + obs.Model
		if buckets[key] == nil {
			buckets[key] = &bucket{harness: obs.Harness, model: obs.Model}
			keys = append(keys, key)
		}
		buckets[key].items = append(buckets[key].items, obs)
	}
	sort.Strings(keys)
	out := make([]HarnessSummary, 0, len(keys))
	for _, key := range keys {
		b := buckets[key]
		s := Summarize(deriveWindow(Window{}, b.items), b.items)
		out = append(out, HarnessSummary{
			Harness:        b.harness,
			Model:          b.model,
			TotalCalls:     s.TotalCalls,
			ErrorCount:     s.ErrorCount,
			LatencyP50MS:   s.LatencyP50MS,
			LatencyP95MS:   s.LatencyP95MS,
			LastErrorClass: s.LastErrorClass,
		})
	}
	return out
}

func (o Observation) failed() bool {
	return o.Status != "" && o.Status != StatusOK || o.ErrorClass != "" || o.HTTPStatus >= 400
}

func (o Observation) normalizedErrorClass() string {
	if strings.TrimSpace(o.ErrorClass) != "" {
		return strings.TrimSpace(o.ErrorClass)
	}
	if o.HTTPStatus >= 400 {
		return fmt.Sprintf("http_%d", o.HTTPStatus)
	}
	if o.Status != "" && o.Status != StatusOK {
		return string(o.Status)
	}
	return ""
}

func normalizeObservation(obs *Observation) error {
	if obs.Timestamp.IsZero() {
		return fmt.Errorf("%w: timestamp is required", ErrCommandContract)
	}
	obs.Harness = strings.TrimSpace(obs.Harness)
	if obs.Harness == "" {
		return fmt.Errorf("%w: harness is required", ErrCommandContract)
	}
	obs.Model = strings.TrimSpace(obs.Model)
	if obs.Model == "" {
		obs.Model = "unknown"
	}
	if obs.LatencyMS < 0 {
		return fmt.Errorf("%w: latency_ms must be non-negative", ErrCommandContract)
	}
	if obs.Status == "" {
		obs.Status = StatusOK
		if obs.ErrorClass != "" || obs.HTTPStatus >= 400 {
			obs.Status = StatusError
		}
	}
	obs.ErrorClass = strings.TrimSpace(obs.ErrorClass)
	obs.EndpointRedacted = strings.TrimSpace(obs.EndpointRedacted)
	return nil
}

func deriveWindow(window Window, observations []Observation) Window {
	if !window.Start.IsZero() && !window.End.IsZero() {
		return window
	}
	for _, obs := range observations {
		if window.Start.IsZero() || obs.Timestamp.Before(window.Start) {
			window.Start = obs.Timestamp
		}
		if window.End.IsZero() || obs.Timestamp.After(window.End) {
			window.End = obs.Timestamp
		}
	}
	return window
}

func rejectRawPayloadFields(data []byte) error {
	var top map[string]json.RawMessage
	if err := json.Unmarshal(data, &top); err != nil {
		return err
	}
	rawObservations := top["observations"]
	if len(rawObservations) == 0 {
		rawObservations = top["calls"]
	}
	if len(rawObservations) == 0 {
		return nil
	}
	var entries []map[string]json.RawMessage
	if err := json.Unmarshal(rawObservations, &entries); err != nil {
		return fmt.Errorf("rano: decode observations: %w", err)
	}
	for i, entry := range entries {
		for key := range entry {
			if forbiddenPayloadKey(key) {
				return fmt.Errorf("%w: observation %d field %q", ErrRedactionContract, i, key)
			}
		}
	}
	return nil
}

func forbiddenPayloadKey(key string) bool {
	normalized := strings.ToLower(strings.ReplaceAll(key, "-", "_"))
	switch normalized {
	case "body", "raw_body", "request_body", "response_body", "payload", "raw_payload":
		return true
	default:
		return false
	}
}

func percentileMS(values []int, percentile float64) int {
	if len(values) == 0 {
		return 0
	}
	sorted := append([]int(nil), values...)
	sort.Ints(sorted)
	rank := int(math.Ceil(percentile*float64(len(sorted)))) - 1
	if rank < 0 {
		rank = 0
	}
	if rank >= len(sorted) {
		rank = len(sorted) - 1
	}
	return sorted[rank]
}

func (a *Adapter) runText(ctx context.Context, argv []string) ([]byte, error) {
	if err := validateAdoptedArgv(argv); err != nil {
		return nil, err
	}
	runner := a.Runner
	if runner == nil {
		runner = ExecRunner{}
	}
	result, err := runner.Run(ctx, argv)
	if err != nil {
		if errors.Is(err, ErrMissingBinary) {
			return nil, err
		}
		return nil, fmt.Errorf("rano: invoke %q: %w (stderr: %s)", strings.Join(argv, " "), err, truncateStderr(result.Stderr))
	}
	if result.ExitCode != 0 {
		return nil, fmt.Errorf("rano: %q exited %d (stderr: %s)", strings.Join(argv, " "), result.ExitCode, truncateStderr(result.Stderr))
	}
	max := a.MaxStdoutBytes
	if max <= 0 {
		max = defaultMaxStdoutBytes
	}
	if len(result.Stdout) > max {
		return nil, fmt.Errorf("%w: %q produced %d bytes (limit %d)", ErrOutputTooLarge, strings.Join(argv, " "), len(result.Stdout), max)
	}
	return result.Stdout, nil
}

func validateAdoptedArgv(argv []string) error {
	if len(argv) == 0 || argv[0] != ToolName {
		return fmt.Errorf("%w: argv must start with rano", ErrInvalidRequest)
	}
	joined := strings.Join(argv, " ")
	switch {
	case joined == strings.Join(VersionArgv(), " "):
		return nil
	case len(argv) == 5 && argv[1] == "signals" && argv[2] == "--json" && argv[3] == "--since":
		if _, err := parseDurationArg(argv[4]); err != nil {
			return fmt.Errorf("%w: invalid --since %q", ErrInvalidRequest, argv[4])
		}
		return nil
	default:
		return fmt.Errorf("%w: rano surface not adopted: %s", ErrInvalidRequest, joined)
	}
}

func defaultCapabilities(note string) map[string]capabilities.Capability {
	caps := make(map[string]capabilities.Capability, len(CapabilityIDs()))
	for _, id := range CapabilityIDs() {
		caps[id] = capabilities.Capability{Status: capabilities.StatusUntested, Notes: note}
	}
	return caps
}

func CapabilityIDs() []string {
	return []string{CapabilityPresent, CapabilitySignalsRead}
}

func probeOne(ctx context.Context, id, summary string, call func(context.Context) error) capabilities.Capability {
	probeCtx, cancel := context.WithTimeout(ctx, defaultProbeTimeout)
	defer cancel()
	err := call(probeCtx)
	if err == nil {
		return capabilities.Capability{Status: capabilities.StatusOK, Transport: "stdio"}
	}
	return capabilities.Capability{
		Status:    statusForError(err),
		Transport: "stdio",
		Notes:     fmt.Sprintf("%s probe error: %s", summary, truncateStderr([]byte(err.Error()))),
	}
}

func statusForError(err error) capabilities.CapabilityStatus {
	switch {
	case errors.Is(err, ErrMissingBinary):
		return capabilities.StatusMissing
	default:
		return capabilities.StatusDegraded
	}
}

func compatibleVersion(version string) bool {
	major, minor, ok := parseVersionMajorMinor(version)
	if !ok {
		return false
	}
	return major > 0 || minor >= 1
}

func parseVersionMajorMinor(version string) (int, int, bool) {
	cleaned := strings.TrimSpace(strings.TrimPrefix(version, "v"))
	parts := strings.Split(cleaned, ".")
	if len(parts) < 2 {
		return 0, 0, false
	}
	major, errMajor := strconv.Atoi(parts[0])
	minor, errMinor := strconv.Atoi(parts[1])
	if errMajor != nil || errMinor != nil {
		return 0, 0, false
	}
	return major, minor, true
}

func normalizeVersion(data []byte) string {
	version := strings.TrimSpace(string(data))
	version = strings.TrimPrefix(version, "rano version ")
	version = strings.TrimPrefix(version, "rano ")
	version = strings.TrimPrefix(version, "v")
	if newline := strings.Index(version, "\n"); newline >= 0 {
		version = version[:newline]
	}
	return strings.TrimSpace(version)
}

func durationOrDefault(d time.Duration) time.Duration {
	if d <= 0 {
		return defaultSignalsWindow
	}
	return d
}

func durationArg(d time.Duration) string {
	if d < time.Second {
		d = time.Second
	}
	seconds := int64(d.Round(time.Second) / time.Second)
	if seconds%60 == 0 {
		return fmt.Sprintf("%dm", seconds/60)
	}
	return fmt.Sprintf("%ds", seconds)
}

func parseDurationArg(arg string) (time.Duration, error) {
	if strings.HasSuffix(arg, "m") || strings.HasSuffix(arg, "s") || strings.HasSuffix(arg, "h") {
		return time.ParseDuration(arg)
	}
	return 0, errors.New("duration must include unit")
}

func (a *Adapter) signalsTimeout() time.Duration {
	if a.SignalsTimeout > 0 {
		return a.SignalsTimeout
	}
	return defaultSignalsTimeout
}

func (a *Adapter) now() time.Time {
	if a.Now != nil {
		return a.Now()
	}
	return time.Now()
}

func truncateStderr(b []byte) string {
	const max = 512
	s := string(b)
	if len(s) <= max {
		return s
	}
	return s[:max] + "...(truncated)"
}

func isExecNotFoundErr(err error) bool {
	if err == nil {
		return false
	}
	s := err.Error()
	return strings.Contains(s, "executable file not found") ||
		strings.Contains(s, "no such file or directory")
}
