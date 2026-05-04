package acfs

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/hoopoe-cockpit/hoopoe/apps/daemon/internal/onboarding/checkpoints"
)

func TestParserCleanInstall(t *testing.T) {
	t.Parallel()
	events := parseTranscript(t, []string{
		"[acfs] phase.start preflight Verify OS",
		"[acfs] checkpoint preflight os pass",
		"[acfs] phase.end preflight rc=0 durationMs=10",
		"[acfs] phase.start acfs-install Canonical installer",
		"[acfs] checkpoint acfs-install sha256 pass",
		"[acfs] checkpoint acfs-install tools pass",
		"[acfs] phase.end acfs-install rc=0 durationMs=20",
	}, 0)

	types := eventTypes(events)
	want := []EventType{
		EventPhaseLine, EventPhaseStart,
		EventPhaseLine, EventPhaseCheckpoint,
		EventPhaseLine, EventPhaseEnd,
		EventPhaseLine, EventPhaseStart,
		EventPhaseLine, EventPhaseCheckpoint,
		EventPhaseLine, EventPhaseCheckpoint,
		EventPhaseLine, EventPhaseEnd,
	}
	if !reflect.DeepEqual(types, want) {
		t.Fatalf("event types = %v, want %v", types, want)
	}
	assertNoLowConfidence(t, events)
}

func TestParserPartialResumeScenario(t *testing.T) {
	t.Parallel()
	parser := NewParser(ParserConfig{RunID: "run_partial"})
	for _, line := range []string{
		"[acfs] phase.start preflight Verify OS",
		"[acfs] checkpoint preflight os pass",
		"[acfs] phase.end preflight rc=0 durationMs=10",
		"[acfs] phase.start acfs-install Canonical installer",
		"network dropped while fetching tool bundle",
	} {
		if _, err := parser.Observe(Line{Stream: StreamStdout, Text: line}); err != nil {
			t.Fatalf("Observe: %v", err)
		}
	}
	events, err := parser.Finish(255, time.Now())
	if err != nil {
		t.Fatalf("Finish: %v", err)
	}
	if len(events) != 1 || events[0].Type != EventPhaseFail {
		t.Fatalf("finish events = %+v", events)
	}
	if events[0].Phase != "acfs-install" || events[0].ResumeHint != DefaultResumeHint {
		t.Fatalf("unexpected fail event: %+v", events[0])
	}
	state := parser.State()
	if len(state.Completed) != 1 || state.Completed[0].Phase != "preflight" {
		t.Fatalf("completed phases = %+v", state.Completed)
	}
}

func TestParserFailedDependencyInstall(t *testing.T) {
	t.Parallel()
	events := parseTranscript(t, []string{
		`{"type":"phase.start","phase":"base-packages","name":"Install base packages"}`,
		`{"type":"phase.checkpoint","phase":"base-packages","key":"apt.update","status":"pass"}`,
		`{"type":"phase.checkpoint","phase":"base-packages","key":"apt.install","status":"fail"}`,
		"E: Unable to locate package acfs-required-tool",
	}, 42)
	fail := lastEventOfType(events, EventPhaseFail)
	if fail == nil {
		t.Fatalf("expected phase.fail, got %+v", events)
	}
	if fail.Phase != "base-packages" || fail.RC != 42 {
		t.Fatalf("unexpected fail event: %+v", *fail)
	}
	if len(fail.LastLines) == 0 || !strings.Contains(fail.LastLines[len(fail.LastLines)-1], "Unable to locate") {
		t.Fatalf("last lines missing failure context: %+v", fail.LastLines)
	}
}

func TestParserAlreadyInstalledIdempotentRun(t *testing.T) {
	t.Parallel()
	events := parseTranscript(t, []string{
		"[acfs] phase.start acfs-install Canonical installer",
		"[acfs] checkpoint acfs-install already-installed skip",
		"[acfs] checkpoint acfs-install doctor pass",
		"[acfs] phase.end acfs-install rc=0 durationMs=5",
	}, 0)
	checkpoint := lastEventOfType(events, EventPhaseCheckpoint)
	if checkpoint == nil || checkpoint.Key != "doctor" || checkpoint.Status != CheckpointPass {
		t.Fatalf("unexpected checkpoint: %+v", checkpoint)
	}
	end := lastEventOfType(events, EventPhaseEnd)
	if end == nil || end.Phase != "acfs-install" || end.RC != 0 {
		t.Fatalf("unexpected end event: %+v", end)
	}
}

func TestMarkerMismatchFallsBackToRawLog(t *testing.T) {
	t.Parallel()
	parser := NewParser(ParserConfig{
		RunID:                   "run_corrupt",
		LowConfidenceAfterLines: 3,
	})
	var events []Event
	for _, line := range []string{
		"installer output changed shape",
		"still streaming useful raw log",
		"third line crosses confidence threshold",
	} {
		next, err := parser.Observe(Line{Stream: StreamStdout, Text: line})
		if err != nil {
			t.Fatalf("Observe: %v", err)
		}
		events = append(events, next...)
	}
	confidence := lastEventOfType(events, EventParserConfidence)
	if confidence == nil {
		t.Fatalf("expected parser confidence event: %+v", events)
	}
	if confidence.Confidence != ConfidenceLow || !confidence.RawLogFallback {
		t.Fatalf("unexpected confidence event: %+v", *confidence)
	}
	lineEvents := countEvents(events, EventPhaseLine)
	if lineEvents != 3 {
		t.Fatalf("raw log progress should continue via phase.line, got %d line events", lineEvents)
	}
}

func TestPhaseFailMarkerIsNotDuplicatedAtProcessExit(t *testing.T) {
	t.Parallel()
	parser := NewParser(ParserConfig{RunID: "run_failed_marker"})
	var events []Event
	for _, line := range []string{
		"[acfs] phase.start source-pins Verify bootstrap source pins",
		"[acfs] phase.fail source-pins rc=23",
	} {
		next, err := parser.Observe(Line{Stream: StreamStderr, Text: line})
		if err != nil {
			t.Fatalf("Observe: %v", err)
		}
		events = append(events, next...)
	}
	next, err := parser.Finish(23, time.Now())
	if err != nil {
		t.Fatalf("Finish: %v", err)
	}
	events = append(events, next...)
	if got := countEvents(events, EventPhaseFail); got != 1 {
		t.Fatalf("phase.fail count = %d, want 1; events=%+v", got, events)
	}
}

func TestRunnerPersistsLogAndStreamsEvents(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 5, 4, 12, 0, 0, 0, time.UTC)
	exec := &fakeExecutor{lines: []Line{
		{Stream: StreamStdout, Text: "[acfs] phase.start preflight Verify OS", At: now},
		{Stream: StreamStdout, Text: "[acfs] checkpoint preflight os pass", At: now.Add(time.Millisecond)},
		{Stream: StreamStdout, Text: "[acfs] phase.end preflight rc=0 durationMs=1", At: now.Add(2 * time.Millisecond)},
	}}
	runner := Runner{
		Exec: exec,
		Now:  func() time.Time { return now },
	}
	var events []Event
	result, err := runner.Run(context.Background(), RunRequest{
		RunID:  "run_123",
		Ref:    "v0.7.0",
		LogDir: t.TempDir(),
	}, func(event Event) error {
		events = append(events, event)
		return nil
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if exec.spec.Ref != "v0.7.0" || !strings.Contains(exec.spec.URL, "/v0.7.0/install.sh") {
		t.Fatalf("unexpected command spec: %+v", exec.spec)
	}
	if result.LogPath == "" {
		t.Fatalf("log path should be set")
	}
	data, err := os.ReadFile(result.LogPath)
	if err != nil {
		t.Fatalf("read log: %v", err)
	}
	if !strings.Contains(string(data), "phase.start preflight") {
		t.Fatalf("log missing raw line: %q", data)
	}
	if result.Events != len(events) {
		t.Fatalf("result events = %d, emitted %d", result.Events, len(events))
	}
	if countEvents(events, EventPhaseLine) != len(exec.lines) {
		t.Fatalf("expected one phase.line per raw line: %+v", events)
	}
}

func TestRunnerWritesResumableCheckpoints(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 5, 4, 12, 30, 0, 0, time.UTC)
	exec := &fakeExecutor{lines: []Line{
		{Stream: StreamStdout, Text: "[acfs] phase.start acfs-install Install ACFS", At: now},
		{Stream: StreamStdout, Text: "[acfs] checkpoint acfs-install doctor fail", At: now.Add(time.Millisecond)},
	}}
	sink := &fakeCheckpointSink{}
	runner := Runner{
		Exec:        exec,
		Checkpoints: sink,
		Now:         func() time.Time { return now },
	}
	_, err := runner.Run(context.Background(), RunRequest{
		RunID:     "run_checkpoints",
		ProjectID: "proj_onboard",
		Ref:       "v0.7.0",
		LogDir:    t.TempDir(),
	}, nil)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(sink.requests) < 2 {
		t.Fatalf("checkpoint requests = %+v", sink.requests)
	}
	var failed checkpoints.TransitionRequest
	for _, request := range sink.requests {
		if request.StepID == "acfs-install.doctor" {
			failed = request
			break
		}
	}
	if failed.StepID != "acfs-install.doctor" || failed.Status != checkpoints.StatusFailed {
		t.Fatalf("failed checkpoint request = %+v", failed)
	}
	if failed.ProjectID != "proj_onboard" || failed.ResumeHint != DefaultResumeHint {
		t.Fatalf("checkpoint context = %+v", failed)
	}
}

func TestRunnerRawLogFallbackPreservesRunExitAndResume(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 5, 4, 12, 45, 0, 0, time.UTC)
	lines := make([]Line, 0, DefaultLowConfidenceAfterLines)
	for i := 0; i < DefaultLowConfidenceAfterLines; i++ {
		lines = append(lines, Line{
			Stream: StreamStdout,
			Text:   fmt.Sprintf("installer raw line %02d", i+1),
			At:     now.Add(time.Duration(i) * time.Millisecond),
		})
	}
	exec := &fakeExecutor{
		lines: lines,
		result: CommandResult{
			ExitCode:    17,
			CompletedAt: now.Add(time.Second),
		},
	}
	runner := Runner{
		Exec: exec,
		Now:  func() time.Time { return now },
	}
	var events []Event
	result, err := runner.Run(context.Background(), RunRequest{
		RunID:  "run_raw",
		Ref:    "v0.7.0",
		LogDir: t.TempDir(),
	}, func(event Event) error {
		events = append(events, event)
		return nil
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if result.RunID != "run_raw" || result.ExitCode != 17 || !result.RawLogFallback || result.ResumeHint != DefaultResumeHint {
		t.Fatalf("result = %+v", result)
	}
	if result.Events != len(events) {
		t.Fatalf("result events = %d, emitted %d", result.Events, len(events))
	}
	confidence := lastEventOfType(events, EventParserConfidence)
	if confidence == nil || confidence.RunID != "run_raw" || !confidence.RawLogFallback {
		t.Fatalf("confidence event = %+v", confidence)
	}
	fail := lastEventOfType(events, EventPhaseFail)
	if fail == nil || fail.RunID != "run_raw" || fail.Phase != "bootstrap" || fail.RC != 17 {
		t.Fatalf("fail event = %+v", fail)
	}
	if fail.ResumeHint != DefaultResumeHint || len(fail.LastLines) != DefaultLowConfidenceAfterLines {
		t.Fatalf("fail context = %+v", fail)
	}
}

func TestRunnerFailureTimelineResumesFromLastSuccessfulPhase(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 5, 4, 12, 50, 0, 0, time.UTC)
	exec := &fakeExecutor{
		lines: []Line{
			{Stream: StreamStdout, Text: "[acfs] phase.start preflight Verify OS", At: now},
			{Stream: StreamStdout, Text: "[acfs] phase.end preflight rc=0 durationMs=10", At: now.Add(time.Millisecond)},
			{Stream: StreamStdout, Text: "[acfs] phase.start acfs-install Install ACFS", At: now.Add(2 * time.Millisecond)},
			{Stream: StreamStderr, Text: "network dropped while fetching tool bundle", At: now.Add(3 * time.Millisecond)},
		},
		result: CommandResult{
			ExitCode:    255,
			CompletedAt: now.Add(time.Second),
		},
	}
	nextID := 0
	service := checkpoints.NewService(checkpoints.Config{
		Now: func() time.Time { return now },
		NewID: func() (string, error) {
			nextID++
			return fmt.Sprintf("evt_resume_%d", nextID), nil
		},
	})
	runner := Runner{
		Exec:        exec,
		Checkpoints: service,
		Now:         func() time.Time { return now },
	}
	result, err := runner.Run(context.Background(), RunRequest{
		RunID:     "run_resume",
		ProjectID: "proj_resume",
		Ref:       "v0.7.0",
		LogDir:    t.TempDir(),
	}, nil)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if result.ExitCode != 255 || result.ResumeHint != DefaultResumeHint {
		t.Fatalf("result = %+v", result)
	}
	timeline, err := service.Timeline(context.Background(), "run_resume")
	if err != nil {
		t.Fatalf("Timeline: %v", err)
	}
	preflight, ok := checkpointByStep(timeline.Checkpoints, "preflight")
	if !ok || preflight.Status != checkpoints.StatusSucceeded {
		t.Fatalf("preflight checkpoint = %+v, ok=%v", preflight, ok)
	}
	acfsInstall, ok := checkpointByStep(timeline.Checkpoints, "acfs-install")
	if !ok || acfsInstall.Status != checkpoints.StatusFailed {
		t.Fatalf("acfs-install checkpoint = %+v, ok=%v", acfsInstall, ok)
	}
	if acfsInstall.Attempt != 1 || acfsInstall.ProjectID != "proj_resume" || acfsInstall.ResumeHint != DefaultResumeHint {
		t.Fatalf("acfs-install context = %+v", acfsInstall)
	}
	if acfsInstall.StartedAt == nil || acfsInstall.CompletedAt == nil {
		t.Fatalf("acfs-install timestamps = %+v", acfsInstall)
	}
	if len(timeline.Actions) != 1 || !hasRepairAction(timeline.Actions[0], checkpoints.RepairResumeStep) ||
		!hasRepairAction(timeline.Actions[0], checkpoints.RepairRunACFSDoctor) {
		t.Fatalf("timeline actions = %+v", timeline.Actions)
	}
}

func TestRunnerRejectsUnsafeRefAndRunID(t *testing.T) {
	t.Parallel()
	runner := Runner{Exec: &fakeExecutor{}}
	_, err := runner.Run(context.Background(), RunRequest{RunID: "../bad", Ref: "v0.7.0", LogDir: t.TempDir()}, nil)
	if !errors.Is(err, ErrInvalidRunRequest) {
		t.Fatalf("expected invalid run id, got %v", err)
	}
	_, err = runner.Run(context.Background(), RunRequest{RunID: "run", Ref: "main;rm", LogDir: t.TempDir()}, nil)
	if !errors.Is(err, ErrInvalidRef) {
		t.Fatalf("expected invalid ref, got %v", err)
	}
}

func TestDefaultLogDirShape(t *testing.T) {
	t.Parallel()
	dir, err := DefaultLogDir()
	if err != nil {
		t.Fatalf("DefaultLogDir: %v", err)
	}
	if filepath.Base(dir) != "logs" || filepath.Base(filepath.Dir(dir)) != ".hoopoe" {
		t.Fatalf("unexpected log dir: %s", dir)
	}
}

type fakeExecutor struct {
	lines  []Line
	result CommandResult
	err    error
	spec   CommandSpec
}

type fakeCheckpointSink struct {
	requests []checkpoints.TransitionRequest
}

func (f *fakeCheckpointSink) Transition(_ context.Context, req checkpoints.TransitionRequest) (checkpoints.TransitionResult, error) {
	f.requests = append(f.requests, req)
	return checkpoints.TransitionResult{}, nil
}

func (f *fakeExecutor) Run(_ context.Context, spec CommandSpec, onLine func(Line) error) (CommandResult, error) {
	f.spec = spec
	for _, line := range f.lines {
		if err := onLine(line); err != nil {
			return f.result, err
		}
	}
	if f.result.CompletedAt.IsZero() {
		f.result.CompletedAt = time.Now().UTC()
	}
	return f.result, f.err
}

func parseTranscript(t *testing.T, lines []string, rc int) []Event {
	t.Helper()
	parser := NewParser(ParserConfig{RunID: "run_test"})
	var events []Event
	for i, line := range lines {
		next, err := parser.Observe(Line{Stream: StreamStdout, Offset: int64(i * 10), Text: line})
		if err != nil {
			t.Fatalf("Observe: %v", err)
		}
		events = append(events, next...)
	}
	next, err := parser.Finish(rc, time.Now())
	if err != nil {
		t.Fatalf("Finish: %v", err)
	}
	events = append(events, next...)
	return events
}

func eventTypes(events []Event) []EventType {
	out := make([]EventType, 0, len(events))
	for _, event := range events {
		out = append(out, event.Type)
	}
	return out
}

func countEvents(events []Event, eventType EventType) int {
	total := 0
	for _, event := range events {
		if event.Type == eventType {
			total++
		}
	}
	return total
}

func lastEventOfType(events []Event, eventType EventType) *Event {
	for i := len(events) - 1; i >= 0; i-- {
		if events[i].Type == eventType {
			return &events[i]
		}
	}
	return nil
}

func checkpointByStep(items []checkpoints.Checkpoint, stepID string) (checkpoints.Checkpoint, bool) {
	for _, item := range items {
		if item.StepID == stepID {
			return item, true
		}
	}
	return checkpoints.Checkpoint{}, false
}

func hasRepairAction(hint checkpoints.RepairHint, id checkpoints.RepairActionID) bool {
	for _, action := range hint.Actions {
		if action.ID == id {
			return true
		}
	}
	return false
}

func assertNoLowConfidence(t *testing.T, events []Event) {
	t.Helper()
	for _, event := range events {
		if event.Type == EventParserConfidence || event.Confidence == ConfidenceLow {
			t.Fatalf("unexpected low confidence event: %+v", event)
		}
	}
}
