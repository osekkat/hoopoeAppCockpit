package acfs

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/hoopoe-cockpit/hoopoe/apps/daemon/internal/onboarding/checkpoints"
	schemas "github.com/hoopoe-cockpit/hoopoe/packages/schemas/go"
)

const DefaultInstallerTimeout = 45 * time.Minute

const acfsCheckpointActorID = "acfs-runner"

var (
	ErrInvalidRunRequest = errors.New("acfs: invalid run request")
	ErrInvalidRef        = errors.New("acfs: invalid ref")
)

type Executor interface {
	Run(ctx context.Context, spec CommandSpec, onLine func(Line) error) (CommandResult, error)
}

type CheckpointSink interface {
	Transition(context.Context, checkpoints.TransitionRequest) (checkpoints.TransitionResult, error)
}

type Runner struct {
	Exec        Executor
	Checkpoints CheckpointSink
	Now         func() time.Time
	Markers     MarkerLibrary
}

func (r Runner) Run(ctx context.Context, req RunRequest, emit func(Event) error) (RunResult, error) {
	if err := ctx.Err(); err != nil {
		return RunResult{}, err
	}
	if r.Exec == nil {
		r.Exec = OSExecutor{}
	}
	now := r.Now
	if now == nil {
		now = time.Now
	}
	runID := strings.TrimSpace(req.RunID)
	if runID == "" {
		runID = fmt.Sprintf("bootstrap-%d", now().UTC().UnixNano())
	}
	if !safeToken(runID) {
		return RunResult{}, fmt.Errorf("%w: run id", ErrInvalidRunRequest)
	}
	ref := strings.TrimSpace(req.Ref)
	if ref == "" {
		ref = DefaultPinnedRef
	}
	if !ValidRef(ref) {
		return RunResult{}, fmt.Errorf("%w: %q", ErrInvalidRef, ref)
	}
	logDir := req.LogDir
	if logDir == "" {
		var err error
		logDir, err = DefaultLogDir()
		if err != nil {
			return RunResult{}, err
		}
	}
	if err := os.MkdirAll(logDir, 0o700); err != nil {
		return RunResult{}, err
	}
	logPath := filepath.Join(logDir, runID+".log")
	logFile, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return RunResult{}, err
	}
	defer logFile.Close()

	started := now().UTC()
	parser := NewParser(ParserConfig{
		RunID:   runID,
		Markers: firstMarkerLibrary(r.Markers, ref),
		Now:     now,
	})
	eventCount := 0
	offset := int64(0)
	emitEvents := func(events []Event) error {
		for _, event := range events {
			if err := r.recordCheckpointEvent(ctx, req.ProjectID, event); err != nil {
				return err
			}
			eventCount++
			if emit != nil {
				if err := emit(event); err != nil {
					return err
				}
			}
		}
		return nil
	}
	spec := DefaultCommandSpec(ref)
	result, runErr := r.Exec.Run(ctx, spec, func(line Line) error {
		if line.At.IsZero() {
			line.At = now().UTC()
		}
		line.Offset = offset
		written, err := logFile.WriteString(line.Text + "\n")
		if err != nil {
			return err
		}
		offset += int64(written)
		events, err := parser.Observe(line)
		if err != nil {
			return err
		}
		return emitEvents(events)
	})
	if syncErr := logFile.Sync(); syncErr != nil && runErr == nil {
		runErr = syncErr
	}
	completed := result.CompletedAt
	if completed.IsZero() {
		completed = now().UTC()
	}
	finishEvents, finishErr := parser.Finish(result.ExitCode, completed)
	if finishErr != nil && runErr == nil {
		runErr = finishErr
	}
	if err := emitEvents(finishEvents); err != nil && runErr == nil {
		runErr = err
	}
	state := parser.State()
	return RunResult{
		RunID:          runID,
		Ref:            ref,
		LogPath:        logPath,
		ExitCode:       result.ExitCode,
		StartedAt:      started,
		CompletedAt:    completed,
		DurationMs:     completed.Sub(started).Milliseconds(),
		Events:         eventCount,
		ParserState:    state,
		RawLogFallback: state.RawLogFallback,
		ResumeHint:     state.ResumeHint,
	}, runErr
}

func (r Runner) recordCheckpointEvent(ctx context.Context, projectID string, event Event) error {
	if r.Checkpoints == nil {
		return nil
	}
	req, ok := checkpointTransitionFromEvent(projectID, event)
	if !ok {
		return nil
	}
	_, err := r.Checkpoints.Transition(ctx, req)
	return err
}

func checkpointTransitionFromEvent(projectID string, event Event) (checkpoints.TransitionRequest, bool) {
	base := checkpoints.TransitionRequest{
		RunID:        event.RunID,
		ProjectID:    strings.TrimSpace(projectID),
		Actor:        schemas.Actor{Kind: schemas.ActorKindSystem, Id: stringPtr(acfsCheckpointActorID)},
		At:           event.At,
		EvidenceRefs: checkpointEvidenceRefs(event),
	}
	if base.RunID == "" {
		return checkpoints.TransitionRequest{}, false
	}
	switch event.Type {
	case EventPhaseStart:
		base.StepID = checkpointStepID(event.Phase, "")
		base.StepLabel = event.Name
		base.Status = checkpoints.StatusRunning
		return base, true
	case EventPhaseCheckpoint:
		base.StepID = checkpointStepID(event.Phase, event.Key)
		base.Status = checkpointStatus(event.Status)
		if event.ResumeHint != "" {
			base.ResumeHint = event.ResumeHint
		}
		if base.Status == checkpoints.StatusFailed {
			base.FailureReason = "ACFS checkpoint reported failure"
			if base.ResumeHint == "" {
				base.ResumeHint = DefaultResumeHint
			}
		}
		return base, true
	case EventPhaseEnd:
		base.StepID = checkpointStepID(event.Phase, "")
		base.Status = checkpoints.StatusSucceeded
		return base, true
	case EventPhaseFail:
		base.StepID = checkpointStepID(event.Phase, "")
		base.Status = checkpoints.StatusFailed
		base.FailureReason = "ACFS phase failed"
		base.ResumeHint = event.ResumeHint
		return base, true
	default:
		return checkpoints.TransitionRequest{}, false
	}
}

func checkpointStatus(status CheckpointStatus) checkpoints.Status {
	switch status {
	case CheckpointPass, CheckpointWarn:
		return checkpoints.StatusSucceeded
	case CheckpointFail:
		return checkpoints.StatusFailed
	case CheckpointSkip:
		return checkpoints.StatusSkipped
	default:
		return checkpoints.StatusPending
	}
}

func checkpointStepID(phase, key string) string {
	phase = strings.TrimSpace(phase)
	if phase == "" {
		phase = "bootstrap"
	}
	key = strings.TrimSpace(key)
	if key == "" {
		return phase
	}
	return phase + "." + key
}

func checkpointEvidenceRefs(event Event) []string {
	if event.Offset <= 0 {
		return []string{"acfs-log:" + event.RunID}
	}
	return []string{"acfs-log:" + event.RunID + ":" + strconv.FormatInt(event.Offset, 10)}
}

func stringPtr(value string) *string {
	return &value
}

func DefaultCommandSpec(ref string) CommandSpec {
	url := fmt.Sprintf("https://raw.githubusercontent.com/Dicklesworthstone/agentic_coding_flywheel_setup/%s/install.sh", ref)
	return CommandSpec{
		Ref:      ref,
		CurlPath: "curl",
		BashPath: "bash",
		URL:      url,
		Timeout:  DefaultInstallerTimeout,
	}
}

func DefaultLogDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("acfs: resolve home dir: %w", err)
	}
	return filepath.Join(home, ".hoopoe", "logs"), nil
}

func ValidRef(ref string) bool {
	if ref == "" || strings.Contains(ref, "..") || strings.ContainsAny(ref, `"'\\ $;&|<>`) {
		return false
	}
	for _, r := range ref {
		switch {
		case r >= 'a' && r <= 'z':
		case r >= 'A' && r <= 'Z':
		case r >= '0' && r <= '9':
		case r == '.', r == '_', r == '-', r == '/':
		default:
			return false
		}
	}
	return true
}

func safeToken(s string) bool {
	if s == "" || len(s) > 128 || strings.Contains(s, "..") {
		return false
	}
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z':
		case r >= 'A' && r <= 'Z':
		case r >= '0' && r <= '9':
		case r == '.', r == '_', r == '-':
		default:
			return false
		}
	}
	return true
}

func firstMarkerLibrary(markers MarkerLibrary, ref string) MarkerLibrary {
	if markers.Ref != "" {
		return markers
	}
	return DefaultMarkerLibrary(ref)
}
