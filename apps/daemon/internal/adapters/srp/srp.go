// Package srp wraps System Resource Protection signals. It is read-only; if
// the srp CLI is unavailable, it can fall back to procfs/statfs signals and
// reports degraded capability state.
package srp

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/hoopoe-cockpit/hoopoe/apps/daemon/internal/capabilities"
)

const (
	ToolName = "srp"

	CapabilitySignalsRead = "srp.signals.read"
	CapabilityStatusRead  = "srp.status.read"
)

var ErrInvalidRequest = errors.New("srp: invalid request")

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
	result.ExitCode = -1
	return result, err
}

type FallbackReader interface {
	ReadSignals(ctx context.Context) (SignalSnapshot, error)
}

type Adapter struct {
	Runner   Runner
	Fallback FallbackReader
	Now      func() time.Time
}

func New(runner Runner) *Adapter {
	if runner == nil {
		runner = ExecRunner{}
	}
	return &Adapter{Runner: runner, Fallback: ProcFallback{}, Now: time.Now}
}

type SignalSnapshot struct {
	CPU        CPUSignal             `json:"cpu,omitempty"`
	Memory     MemorySignal          `json:"mem,omitempty"`
	Swap       MemorySignal          `json:"swap,omitempty"`
	Disk       map[string]DiskSignal `json:"disk,omitempty"`
	Thresholds Thresholds            `json:"thresholds,omitempty"`
	CapturedAt time.Time             `json:"capturedAt,omitempty"`
	Healthy    bool                  `json:"healthy"`
	Stale      bool                  `json:"stale,omitempty"`
	Source     string                `json:"source,omitempty"`
	Warnings   []string              `json:"warnings,omitempty"`
}

type CPUSignal struct {
	Load1  float64 `json:"load1"`
	Load5  float64 `json:"load5"`
	Load15 float64 `json:"load15"`
}

type MemorySignal struct {
	UsedMB  int64 `json:"used_mb,omitempty"`
	FreeMB  int64 `json:"free_mb,omitempty"`
	TotalMB int64 `json:"total_mb,omitempty"`
}

type DiskSignal struct {
	FreeGB  float64 `json:"free_gb,omitempty"`
	UsedGB  float64 `json:"used_gb,omitempty"`
	TotalGB float64 `json:"total_gb,omitempty"`
	Percent float64 `json:"percent"`
}

type Thresholds struct {
	DiskWarnPercent     float64 `json:"disk_warn_percent,omitempty"`
	DiskCriticalPercent float64 `json:"disk_critical_percent,omitempty"`
	LoadWarn            float64 `json:"load_warn,omitempty"`
}

type Status struct {
	Healthy  bool     `json:"healthy"`
	Version  string   `json:"version,omitempty"`
	Warnings []string `json:"warnings,omitempty"`
}

type ThresholdCrossing struct {
	Kind      string  `json:"kind"`
	Severity  string  `json:"severity"`
	Mount     string  `json:"mount,omitempty"`
	Observed  float64 `json:"observed"`
	Threshold float64 `json:"threshold"`
	Action    string  `json:"action,omitempty"`
}

func SignalsArgv() []string {
	return []string{ToolName, "signals", "--json"}
}

func StatusArgv() []string {
	return []string{ToolName, "status", "--json"}
}

func (a *Adapter) Signals(ctx context.Context) (SignalSnapshot, error) {
	var snapshot SignalSnapshot
	if err := a.runJSON(ctx, SignalsArgv(), &snapshot); err != nil {
		if fallback := a.Fallback; fallback != nil {
			fallbackSnapshot, fallbackErr := fallback.ReadSignals(ctx)
			if fallbackErr == nil {
				fallbackSnapshot.Source = "procfs"
				fallbackSnapshot.Warnings = append(fallbackSnapshot.Warnings, err.Error())
				return normalizeSnapshot(fallbackSnapshot, a.now()), nil
			}
			return SignalSnapshot{}, fmt.Errorf("%v; fallback failed: %w", err, fallbackErr)
		}
		return SignalSnapshot{}, err
	}
	snapshot.Source = "srp"
	return normalizeSnapshot(snapshot, a.now()), nil
}

func (a *Adapter) Status(ctx context.Context) (Status, error) {
	var status Status
	if err := a.runJSON(ctx, StatusArgv(), &status); err != nil {
		return Status{}, err
	}
	return status, nil
}

func (a *Adapter) Probe(ctx context.Context) (*capabilities.ToolReport, error) {
	report := &capabilities.ToolReport{
		Tool:          capabilities.ToolSRP,
		Source:        "cli",
		LastCheckedAt: a.now().UTC().Format(time.RFC3339),
		Capabilities: map[string]capabilities.Capability{
			CapabilitySignalsRead: {Status: capabilities.StatusMissing},
			CapabilityStatusRead:  {Status: capabilities.StatusMissing},
		},
	}
	status, err := a.Status(ctx)
	if err != nil {
		snapshot, signalsErr := a.Signals(ctx)
		if signalsErr == nil && snapshot.Source == "procfs" {
			report.Source = "procfs"
			report.Capabilities[CapabilitySignalsRead] = capabilities.Capability{
				Status:   capabilities.StatusDegraded,
				Fallback: "procfs+statfs",
				Notes:    err.Error(),
			}
			report.Capabilities[CapabilityStatusRead] = capabilities.Capability{Status: capabilities.StatusMissing, Notes: err.Error()}
			return report, nil
		}
		state := statusForError(err)
		for capID := range report.Capabilities {
			report.Capabilities[capID] = capabilities.Capability{Status: state, Notes: err.Error()}
		}
		return report, nil
	}
	report.Version = status.Version
	report.Capabilities[CapabilitySignalsRead] = capabilities.Capability{Status: capabilities.StatusOK}
	report.Capabilities[CapabilityStatusRead] = capabilities.Capability{Status: capabilities.StatusOK}
	if !status.Healthy {
		report.Capabilities[CapabilitySignalsRead] = capabilities.Capability{Status: capabilities.StatusDegraded, Notes: "srp status returned unhealthy"}
		report.Capabilities[CapabilityStatusRead] = capabilities.Capability{Status: capabilities.StatusDegraded, Notes: "srp status returned unhealthy"}
	}
	return report, nil
}

func CrossedThresholds(snapshot SignalSnapshot) []ThresholdCrossing {
	warn := snapshot.Thresholds.DiskWarnPercent
	if warn == 0 {
		warn = 90
	}
	critical := snapshot.Thresholds.DiskCriticalPercent
	if critical == 0 {
		critical = 95
	}
	var out []ThresholdCrossing
	for mount, disk := range snapshot.Disk {
		switch {
		case disk.Percent >= critical:
			out = append(out, ThresholdCrossing{
				Kind: "disk_pressure", Severity: "critical", Mount: mount,
				Observed: disk.Percent, Threshold: critical, Action: "sbh.cleanup",
			})
		case disk.Percent >= warn:
			out = append(out, ThresholdCrossing{
				Kind: "disk_pressure", Severity: "warning", Mount: mount,
				Observed: disk.Percent, Threshold: warn, Action: "sbh.cleanup",
			})
		}
	}
	return out
}

func (a *Adapter) runJSON(ctx context.Context, argv []string, target any) error {
	if a == nil {
		return fmt.Errorf("%w: nil adapter", ErrInvalidRequest)
	}
	runner := a.Runner
	if runner == nil {
		runner = ExecRunner{}
	}
	result, err := runner.Run(ctx, argv)
	if err != nil {
		return fmt.Errorf("srp: run %s: %w", argv[0], err)
	}
	if result.ExitCode != 0 {
		return commandError{argv: argv, result: result}
	}
	if len(bytes.TrimSpace(result.Stdout)) == 0 {
		return fmt.Errorf("srp: empty JSON response from %v", argv)
	}
	if err := json.Unmarshal(result.Stdout, target); err != nil {
		return fmt.Errorf("srp: decode JSON from %v: %w", argv, err)
	}
	return nil
}

type commandError struct {
	argv   []string
	result CommandResult
}

func (e commandError) Error() string {
	return fmt.Sprintf("srp: command %v exited %d: %s", e.argv, e.result.ExitCode, strings.TrimSpace(string(e.result.Stderr)))
}

type ProcFallback struct {
	Mounts []string
}

func (p ProcFallback) ReadSignals(context.Context) (SignalSnapshot, error) {
	load, _ := readLoadavg("/proc/loadavg")
	mem, _ := readMeminfo("/proc/meminfo")
	mounts := p.Mounts
	if len(mounts) == 0 {
		mounts = []string{"/", "/data", "/var/log", filepath.Join(os.Getenv("HOME"), ".hoopoe")}
	}
	disks := make(map[string]DiskSignal, len(mounts))
	for _, mount := range mounts {
		if mount == "" {
			continue
		}
		disk, err := statDisk(mount)
		if err != nil {
			continue
		}
		disks[mount] = disk
	}
	if len(disks) == 0 && mem.TotalMB == 0 && load == (CPUSignal{}) {
		return SignalSnapshot{}, errors.New("srp: no procfs/statfs signals available")
	}
	return SignalSnapshot{
		CPU:        load,
		Memory:     mem,
		Disk:       disks,
		Thresholds: Thresholds{DiskWarnPercent: 90, DiskCriticalPercent: 95},
		Healthy:    true,
		Source:     "procfs",
	}, nil
}

func readLoadavg(path string) (CPUSignal, error) {
	body, err := os.ReadFile(path)
	if err != nil {
		return CPUSignal{}, err
	}
	fields := strings.Fields(string(body))
	if len(fields) < 3 {
		return CPUSignal{}, errors.New("srp: malformed loadavg")
	}
	var out CPUSignal
	if _, err := fmt.Sscanf(fields[0]+" "+fields[1]+" "+fields[2], "%f %f %f", &out.Load1, &out.Load5, &out.Load15); err != nil {
		return CPUSignal{}, err
	}
	return out, nil
}

func readMeminfo(path string) (MemorySignal, error) {
	body, err := os.ReadFile(path)
	if err != nil {
		return MemorySignal{}, err
	}
	var totalKB int64
	var freeKB int64
	for _, line := range strings.Split(string(body), "\n") {
		var key string
		var value int64
		if _, err := fmt.Sscanf(line, "%s %d kB", &key, &value); err != nil {
			continue
		}
		switch key {
		case "MemTotal:":
			totalKB = value
		case "MemAvailable:":
			freeKB = value
		}
	}
	if totalKB == 0 {
		return MemorySignal{}, errors.New("srp: MemTotal missing")
	}
	return MemorySignal{
		TotalMB: totalKB / 1024,
		FreeMB:  freeKB / 1024,
		UsedMB:  (totalKB - freeKB) / 1024,
	}, nil
}

func statDisk(path string) (DiskSignal, error) {
	var stat syscall.Statfs_t
	if err := syscall.Statfs(path, &stat); err != nil {
		return DiskSignal{}, err
	}
	total := float64(stat.Blocks) * float64(stat.Bsize)
	free := float64(stat.Bavail) * float64(stat.Bsize)
	if total <= 0 {
		return DiskSignal{}, errors.New("srp: zero-size filesystem")
	}
	used := total - free
	return DiskSignal{
		FreeGB:  free / (1024 * 1024 * 1024),
		UsedGB:  used / (1024 * 1024 * 1024),
		TotalGB: total / (1024 * 1024 * 1024),
		Percent: used / total * 100,
	}, nil
}

func normalizeSnapshot(snapshot SignalSnapshot, now time.Time) SignalSnapshot {
	if snapshot.Disk == nil {
		snapshot.Disk = map[string]DiskSignal{}
	}
	if snapshot.CapturedAt.IsZero() {
		snapshot.CapturedAt = now.UTC()
	}
	if snapshot.Source == "" {
		snapshot.Source = "srp"
	}
	if snapshot.Thresholds.DiskWarnPercent == 0 {
		snapshot.Thresholds.DiskWarnPercent = 90
	}
	if snapshot.Thresholds.DiskCriticalPercent == 0 {
		snapshot.Thresholds.DiskCriticalPercent = 95
	}
	return snapshot
}

func statusForError(err error) capabilities.CapabilityStatus {
	var commandErr commandError
	if errors.As(err, &commandErr) {
		if commandErr.result.ExitCode == 124 {
			return capabilities.StatusDegraded
		}
		return capabilities.StatusMissing
	}
	if strings.Contains(err.Error(), "decode JSON") {
		return capabilities.StatusDegraded
	}
	if errors.Is(err, exec.ErrNotFound) || strings.Contains(err.Error(), "executable file not found") || strings.Contains(err.Error(), "command not found") {
		return capabilities.StatusMissing
	}
	return capabilities.StatusDegraded
}

func (a *Adapter) now() time.Time {
	if a != nil && a.Now != nil {
		return a.Now()
	}
	return time.Now()
}
