package rch

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/hoopoe-cockpit/hoopoe/apps/daemon/internal/capabilities"
)

type rchConformancePack struct {
	Tool            string                           `json:"tool"`
	ToolVersion     string                           `json:"toolVersion"`
	FixturesVersion string                           `json:"fixturesVersion"`
	Source          string                           `json:"source"`
	Request         rchConformanceRequest            `json:"request"`
	Commands        map[string]rchConformanceCommand `json:"commands"`
	Expected        rchConformanceExpected           `json:"expected"`
}

type rchConformanceRequest struct {
	ProjectID     string            `json:"projectId"`
	WorktreePath  string            `json:"worktreePath"`
	Branch        string            `json:"branch"`
	CommitSHA     string            `json:"commitSha"`
	Command       []string          `json:"command"`
	Env           map[string]string `json:"env"`
	RunnerProfile string            `json:"runnerProfile"`
	WorkerTarget  string            `json:"workerTarget"`
	TimeoutMs     int64             `json:"timeoutMs"`
}

type rchConformanceCommand struct {
	Dir        string   `json:"dir"`
	Argv       []string `json:"argv"`
	Exit       int      `json:"exit"`
	StdoutText string   `json:"stdoutText"`
	StderrText string   `json:"stderrText,omitempty"`
}

type rchConformanceExpected struct {
	Run   rchRunConformanceSummary   `json:"run"`
	Probe rchProbeConformanceSummary `json:"probe"`
}

type rchRunConformanceSummary struct {
	ProjectID          string                `json:"projectId"`
	WorktreePath       string                `json:"worktreePath"`
	Branch             string                `json:"branch"`
	CommitSHA          string                `json:"commitSha"`
	Command            []string              `json:"command"`
	NormalizedArgv     []string              `json:"normalizedArgv"`
	EnvironmentDigest  string                `json:"environmentDigest"`
	RunnerProfile      string                `json:"runnerProfile"`
	WorkerTarget       string                `json:"workerTarget"`
	StartedAt          string                `json:"startedAt"`
	CompletedAt        string                `json:"completedAt"`
	DurationMs         int64                 `json:"durationMs"`
	ExitCode           int                   `json:"exitCode"`
	Stdout             string                `json:"stdout"`
	Stderr             string                `json:"stderr"`
	OutputTruncated    bool                  `json:"outputTruncated"`
	Summary            rchSummaryConformance `json:"summary"`
	FailureFingerprint string                `json:"failureFingerprint"`
}

type rchSummaryConformance struct {
	Mode        string `json:"mode"`
	Worker      string `json:"worker"`
	FailureCode string `json:"failureCode"`
	QueueWaitMs int64  `json:"queueWaitMs"`
	ExecTimeMs  int64  `json:"execTimeMs"`
	RawLine     string `json:"rawLine"`
}

type rchProbeConformanceSummary struct {
	Version      string            `json:"version"`
	Capabilities map[string]string `json:"capabilities"`
}

type rchConformanceRunner struct {
	responses map[string]rchConformanceCommand
	calls     []Invocation
}

func TestPhase0RCHConformanceCaptures(t *testing.T) {
	packs := loadRCHConformancePacks(t)
	if len(packs) == 0 {
		t.Fatal("no rch conformance capture packs found")
	}

	for _, pack := range packs {
		pack := pack
		t.Run(pack.ToolVersion, func(t *testing.T) {
			if pack.Tool != ToolName {
				t.Fatalf("tool = %q, want %q", pack.Tool, ToolName)
			}
			runner := newRCHConformanceRunner(t, pack)
			adapter := New(runner)
			adapter.Now = func() time.Time { return time.Date(2026, 5, 4, 2, 35, 0, 0, time.UTC) }

			run, err := adapter.Run(context.Background(), rchRunRequest(pack.Request))
			if err != nil {
				t.Fatalf("Run: %v", err)
			}
			assertRCHConformanceEqual(t, "run", summarizeRCHRun(run), pack.Expected.Run)

			probe, err := adapter.Probe(context.Background())
			if err != nil {
				t.Fatalf("Probe: %v", err)
			}
			if err := probe.Validate(); err != nil {
				t.Fatalf("probe report failed validation: %v", err)
			}
			assertRCHConformanceEqual(t, "probe", summarizeRCHProbe(probe), pack.Expected.Probe)
		})
	}
}

func loadRCHConformancePacks(t *testing.T) []rchConformancePack {
	t.Helper()
	capturesDir := filepath.Join(findRCHConformanceRepoRoot(t), "packages", "fixtures", "phase0-rch", "captures")
	entries, err := os.ReadDir(capturesDir)
	if err != nil {
		t.Fatalf("read rch conformance captures: %v", err)
	}
	packs := make([]rchConformancePack, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		path := filepath.Join(capturesDir, entry.Name())
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s: %v", path, err)
		}
		var pack rchConformancePack
		if err := json.Unmarshal(data, &pack); err != nil {
			t.Fatalf("parse %s: %v", path, err)
		}
		if pack.ToolVersion == "" || pack.FixturesVersion == "" {
			t.Fatalf("%s missing toolVersion/fixturesVersion", path)
		}
		if len(pack.Commands) == 0 {
			t.Fatalf("%s has no command captures", path)
		}
		packs = append(packs, pack)
	}
	sort.Slice(packs, func(i, j int) bool {
		return packs[i].ToolVersion < packs[j].ToolVersion
	})
	return packs
}

func newRCHConformanceRunner(t *testing.T, pack rchConformancePack) *rchConformanceRunner {
	t.Helper()
	runner := &rchConformanceRunner{responses: map[string]rchConformanceCommand{}}
	for name, command := range pack.Commands {
		if len(command.Argv) == 0 {
			t.Fatalf("%s command %q has empty argv", pack.ToolVersion, name)
		}
		key := rchConformanceKey(t, command.Dir, command.Argv)
		if _, exists := runner.responses[key]; exists {
			t.Fatalf("%s command %q duplicates invocation %q", pack.ToolVersion, name, key)
		}
		runner.responses[key] = command
	}
	return runner
}

func (r *rchConformanceRunner) Run(_ context.Context, invocation Invocation) (CommandResult, error) {
	r.calls = append(r.calls, Invocation{
		Argv:    append([]string(nil), invocation.Argv...),
		Dir:     invocation.Dir,
		Env:     append([]string(nil), invocation.Env...),
		Timeout: invocation.Timeout,
	})
	key := invocation.Dir + "\x00" + strings.Join(invocation.Argv, " ")
	command, ok := r.responses[key]
	if !ok {
		return CommandResult{ExitCode: 127, Stderr: []byte("missing conformance capture for " + key)}, nil
	}
	return CommandResult{
		ExitCode: command.Exit,
		Stdout:   []byte(command.StdoutText),
		Stderr:   []byte(command.StderrText),
	}, nil
}

func rchConformanceKey(t *testing.T, dir string, argv []string) string {
	t.Helper()
	if argv[0] != ToolName {
		t.Fatalf("capture argv must start with rch: %#v", argv)
	}
	for _, arg := range argv {
		if arg == "sh" || arg == "-c" || arg == "bash" || strings.Contains(arg, "&&") || strings.Contains(arg, ";") {
			t.Fatalf("capture argv contains shell token: %#v", argv)
		}
	}
	return dir + "\x00" + strings.Join(argv, " ")
}

func rchRunRequest(req rchConformanceRequest) RunRequest {
	return RunRequest{
		ProjectID:     req.ProjectID,
		WorktreePath:  req.WorktreePath,
		Branch:        req.Branch,
		CommitSHA:     req.CommitSHA,
		Command:       append([]string(nil), req.Command...),
		Env:           cloneStringMap(req.Env),
		RunnerProfile: req.RunnerProfile,
		WorkerTarget:  req.WorkerTarget,
		Timeout:       time.Duration(req.TimeoutMs) * time.Millisecond,
	}
}

func summarizeRCHRun(run RunResult) rchRunConformanceSummary {
	return rchRunConformanceSummary{
		ProjectID:          run.ProjectID,
		WorktreePath:       run.WorktreePath,
		Branch:             run.Branch,
		CommitSHA:          run.CommitSHA,
		Command:            append([]string(nil), run.Command...),
		NormalizedArgv:     append([]string(nil), run.NormalizedArgv...),
		EnvironmentDigest:  run.EnvironmentDigest,
		RunnerProfile:      run.RunnerProfile,
		WorkerTarget:       run.WorkerTarget,
		StartedAt:          run.StartedAt.UTC().Format(time.RFC3339),
		CompletedAt:        run.CompletedAt.UTC().Format(time.RFC3339),
		DurationMs:         run.Duration.Milliseconds(),
		ExitCode:           run.ExitCode,
		Stdout:             run.Stdout,
		Stderr:             run.Stderr,
		OutputTruncated:    run.OutputTruncated,
		Summary:            summarizeRCHSummary(run.Summary),
		FailureFingerprint: run.FailureFingerprint,
	}
}

func summarizeRCHSummary(summary RCHSummary) rchSummaryConformance {
	return rchSummaryConformance{
		Mode:        summary.Mode,
		Worker:      summary.Worker,
		FailureCode: summary.FailureCode,
		QueueWaitMs: summary.QueueWait.Milliseconds(),
		ExecTimeMs:  summary.ExecTime.Milliseconds(),
		RawLine:     summary.RawLine,
	}
}

func summarizeRCHProbe(report *capabilities.ToolReport) rchProbeConformanceSummary {
	return rchProbeConformanceSummary{
		Version: report.Version,
		Capabilities: map[string]string{
			CapabilityRun: string(report.Capabilities[CapabilityRun].Status),
		},
	}
}

func cloneStringMap(values map[string]string) map[string]string {
	if values == nil {
		return nil
	}
	out := make(map[string]string, len(values))
	for key, value := range values {
		out[key] = value
	}
	return out
}

func assertRCHConformanceEqual(t *testing.T, label string, got, want any) {
	t.Helper()
	if reflect.DeepEqual(got, want) {
		return
	}
	t.Fatalf("%s conformance mismatch\nwant:\n%s\ngot:\n%s", label, rchConformanceJSON(want), rchConformanceJSON(got))
}

func rchConformanceJSON(value any) string {
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return fmt.Sprintf("<marshal failed: %v>", err)
	}
	return string(data)
}

func findRCHConformanceRepoRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "AGENTS.md")); err == nil {
			if _, err := os.Stat(filepath.Join(dir, "apps", "daemon", "go.mod")); err == nil {
				return dir
			}
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatalf("repo root not found from %s", dir)
		}
		dir = parent
	}
}
