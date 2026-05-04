// Package chaos defines deterministic fault-injection probes for the daemon
// contracts that must survive real-world tunnel, process, stream, and tool
// failures. The probes are intentionally non-destructive; real VPS/systemd/git
// hooks can be wired to the same scenario names later.
package chaos

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"
)

type FaultKind string

const (
	FaultTunnelDrop              FaultKind = "tunnel_drop"
	FaultDaemonRestart           FaultKind = "daemon_restart"
	FaultDesktopCrashRestart     FaultKind = "desktop_crash_restart"
	FaultVPSReboot               FaultKind = "vps_reboot"
	FaultDiskPressure            FaultKind = "disk_pressure"
	FaultSlowRenderer            FaultKind = "slow_renderer"
	FaultMalformedAdapterOutput  FaultKind = "malformed_adapter_output"
	FaultLongRunningSchedulerJob FaultKind = "long_running_scheduler_job"
	FaultStuckTerminalStream     FaultKind = "stuck_terminal_stream"
	FaultRateLimit               FaultKind = "rate_limit"
	FaultGitPushFailure          FaultKind = "git_push_failure"
	FaultMissingTool             FaultKind = "missing_tool"
)

var BaseFaults = []FaultKind{
	FaultTunnelDrop,
	FaultDaemonRestart,
	FaultDesktopCrashRestart,
	FaultVPSReboot,
	FaultDiskPressure,
	FaultSlowRenderer,
	FaultMalformedAdapterOutput,
	FaultLongRunningSchedulerJob,
}

var AdditionalFaults = []FaultKind{
	FaultStuckTerminalStream,
	FaultRateLimit,
	FaultGitPushFailure,
	FaultMissingTool,
}

type Scenario struct {
	Kind        FaultKind
	Name        string
	Description string
	Run         ScenarioFunc
}

type ScenarioFunc func(context.Context, Environment, *Recorder) error

type Config struct {
	Scenarios    []Scenario
	Now          func() time.Time
	WorkDir      string
	ReconnectSLO time.Duration
	ReplaySLO    time.Duration
}

type Environment struct {
	Now          func() time.Time
	WorkDir      string
	ReconnectSLO time.Duration
	ReplaySLO    time.Duration
}

type Runner struct {
	cfg Config
}

type Report struct {
	StartedAt time.Time `json:"startedAt"`
	WorkDir   string    `json:"workDir"`
	Results   []Result  `json:"results"`
}

type Result struct {
	Kind         FaultKind     `json:"kind"`
	Name         string        `json:"name"`
	Passed       bool          `json:"passed"`
	Duration     time.Duration `json:"duration"`
	Error        string        `json:"error,omitempty"`
	Observations []Observation `json:"observations,omitempty"`
	Metrics      []Metric      `json:"metrics,omitempty"`
}

type Observation struct {
	Key   string `json:"key"`
	Value string `json:"value"`
}

type Metric struct {
	Name  string  `json:"name"`
	Value float64 `json:"value"`
	Unit  string  `json:"unit"`
}

type Recorder struct {
	observations []Observation
	metrics      []Metric
}

func NewRunner(cfg Config) Runner {
	return Runner{cfg: cfg}
}

func RunDefault(ctx context.Context, cfg Config) (Report, error) {
	return NewRunner(cfg).Run(ctx)
}

func (r Runner) Run(ctx context.Context) (Report, error) {
	now := r.cfg.Now
	if now == nil {
		now = time.Now
	}
	workDir, err := resolveWorkDir(r.cfg.WorkDir)
	if err != nil {
		return Report{}, err
	}
	env := Environment{
		Now:          now,
		WorkDir:      workDir,
		ReconnectSLO: defaultDuration(r.cfg.ReconnectSLO, 5*time.Second),
		ReplaySLO:    defaultDuration(r.cfg.ReplaySLO, 5*time.Second),
	}
	scenarios := r.cfg.Scenarios
	if len(scenarios) == 0 {
		scenarios = DefaultScenarios()
	}
	report := Report{
		StartedAt: now().UTC(),
		WorkDir:   workDir,
		Results:   make([]Result, 0, len(scenarios)),
	}
	var failed []error
	for _, scenario := range scenarios {
		result := runScenario(ctx, env, scenario)
		report.Results = append(report.Results, result)
		if !result.Passed {
			failed = append(failed, fmt.Errorf("%s: %s", result.Kind, result.Error))
		}
	}
	return report, errors.Join(failed...)
}

func DefaultScenarios() []Scenario {
	return []Scenario{
		{
			Kind:        FaultTunnelDrop,
			Name:        "Tunnel drop",
			Description: "Reconnect keeps the bearer, replays the sequence gap, and applies replay idempotently.",
			Run:         runTunnelDrop,
		},
		{
			Kind:        FaultDaemonRestart,
			Name:        "Daemon restart",
			Description: "Durable job state is recovered after daemon restart and orphaned work is interrupted with evidence.",
			Run:         runDaemonRestart,
		},
		{
			Kind:        FaultDesktopCrashRestart,
			Name:        "Desktop crash and restart",
			Description: "A crashed desktop releases daemon-side resources and can resume from the last cursor.",
			Run:         runDesktopCrashRestart,
		},
		{
			Kind:        FaultVPSReboot,
			Name:        "VPS reboot",
			Description: "The daemon returns through systemd, the client reconnects, and tool inventory reruns.",
			Run:         runVPSReboot,
		},
		{
			Kind:        FaultDiskPressure,
			Name:        "Disk pressure",
			Description: "Job logs truncate with warning while audit entries still persist.",
			Run:         runDiskPressure,
		},
		{
			Kind:        FaultSlowRenderer,
			Name:        "Slow renderer",
			Description: "Bounded event delivery emits _lag with a persisted offset and repairs via replay.",
			Run:         runSlowRenderer,
		},
		{
			Kind:        FaultMalformedAdapterOutput,
			Name:        "Malformed adapter output",
			Description: "Malformed robot JSON degrades the capability and audit data is redacted.",
			Run:         runMalformedAdapterOutput,
		},
		{
			Kind:        FaultLongRunningSchedulerJob,
			Name:        "Long-running scheduler job",
			Description: "A blocked scheduler run does not prevent unrelated due jobs from completing.",
			Run:         runLongRunningSchedulerJob,
		},
		{
			Kind:        FaultStuckTerminalStream,
			Name:        "Stuck terminal stream",
			Description: "Terminal chunks remain offset-addressable when a stream consumer stalls.",
			Run:         runStuckTerminalStream,
		},
		{
			Kind:        FaultRateLimit,
			Name:        "Rate limit",
			Description: "Rate-limit detection surfaces an urgent recovery action without blocking unrelated jobs.",
			Run:         runRateLimit,
		},
		{
			Kind:        FaultGitPushFailure,
			Name:        "Git push failure",
			Description: "A failed push records audit evidence and leaves the local commit pending for retry.",
			Run:         runGitPushFailure,
		},
		{
			Kind:        FaultMissingTool,
			Name:        "Missing tool",
			Description: "A missing required tool degrades the capability registry and blocks dependent work.",
			Run:         runMissingTool,
		},
	}
}

func (r Report) Result(kind FaultKind) (Result, bool) {
	for _, result := range r.Results {
		if result.Kind == kind {
			return result, true
		}
	}
	return Result{}, false
}

func (r Report) Failed() []Result {
	failed := make([]Result, 0)
	for _, result := range r.Results {
		if !result.Passed {
			failed = append(failed, result)
		}
	}
	return failed
}

func (r Report) RequireCovered(kinds ...FaultKind) error {
	missing := make([]string, 0)
	for _, kind := range kinds {
		if _, ok := r.Result(kind); !ok {
			missing = append(missing, string(kind))
		}
	}
	if len(missing) > 0 {
		sort.Strings(missing)
		return fmt.Errorf("chaos: missing fault coverage: %v", missing)
	}
	return nil
}

func (r Report) RequirePassed(kinds ...FaultKind) error {
	if err := r.RequireCovered(kinds...); err != nil {
		return err
	}
	var failed []error
	for _, kind := range kinds {
		result, _ := r.Result(kind)
		if !result.Passed {
			failed = append(failed, fmt.Errorf("%s: %s", kind, result.Error))
		}
	}
	return errors.Join(failed...)
}

func (r *Recorder) Observe(key string, value string) {
	r.observations = append(r.observations, Observation{Key: key, Value: value})
}

func (r *Recorder) Measure(name string, value float64, unit string) {
	r.metrics = append(r.metrics, Metric{Name: name, Value: value, Unit: unit})
}

func (r *Recorder) Require(ok bool, format string, args ...any) error {
	if ok {
		return nil
	}
	return fmt.Errorf(format, args...)
}

func (e Environment) ScenarioDir(kind FaultKind) (string, error) {
	if e.WorkDir == "" {
		return "", fmt.Errorf("chaos: empty work dir")
	}
	path := filepath.Join(e.WorkDir, string(kind))
	if err := os.MkdirAll(path, 0o700); err != nil {
		return "", err
	}
	return path, nil
}

func runScenario(ctx context.Context, env Environment, scenario Scenario) Result {
	rec := &Recorder{}
	start := env.Now()
	result := Result{
		Kind: scenario.Kind,
		Name: scenario.Name,
	}
	if scenario.Run == nil {
		result.Error = "nil scenario runner"
		result.Duration = env.Now().Sub(start)
		return result
	}
	if err := scenario.Run(ctx, env, rec); err != nil {
		result.Error = err.Error()
	}
	result.Passed = result.Error == ""
	result.Duration = env.Now().Sub(start)
	result.Observations = rec.observations
	result.Metrics = rec.metrics
	return result
}

func resolveWorkDir(path string) (string, error) {
	if path != "" {
		if err := os.MkdirAll(path, 0o700); err != nil {
			return "", err
		}
		return path, nil
	}
	return os.MkdirTemp("", "hoopoe-chaos-*")
}

func defaultDuration(value time.Duration, fallback time.Duration) time.Duration {
	if value <= 0 {
		return fallback
	}
	return value
}
