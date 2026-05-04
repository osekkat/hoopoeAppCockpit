package scheduler

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

type State struct {
	SchemaVersion int               `json:"schemaVersion"`
	Jobs          map[string]Job    `json:"jobs"`
	Runs          map[string]Run    `json:"runs"`
	EventDedupe   map[string]string `json:"eventDedupe"`
	Metrics       Metrics           `json:"metrics"`
}

type Store interface {
	Load(context.Context) (State, error)
	Save(context.Context, State) error
}

type FileStore struct {
	Path string
}

func (s FileStore) Load(ctx context.Context) (State, error) {
	if err := ctx.Err(); err != nil {
		return State{}, err
	}
	if s.Path == "" {
		return State{}, fmt.Errorf("%w: empty store path", ErrInvalidState)
	}
	f, err := os.Open(s.Path)
	if os.IsNotExist(err) {
		return emptyState(), nil
	}
	if err != nil {
		return State{}, err
	}
	defer f.Close()

	var state State
	if err := json.NewDecoder(f).Decode(&state); err != nil {
		return State{}, err
	}
	normalizeState(&state)
	return state, nil
}

func (s FileStore) Save(ctx context.Context, state State) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if s.Path == "" {
		return fmt.Errorf("%w: empty store path", ErrInvalidState)
	}
	normalizeState(&state)
	if err := os.MkdirAll(filepath.Dir(s.Path), 0o700); err != nil {
		return err
	}
	tmp := fmt.Sprintf("%s.tmp.%d", s.Path, time.Now().UnixNano())
	f, err := os.OpenFile(tmp, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return err
	}
	enc := json.NewEncoder(f)
	enc.SetIndent("", "  ")
	if err := enc.Encode(state); err != nil {
		_ = f.Close()
		return err
	}
	if err := f.Sync(); err != nil {
		_ = f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmp, s.Path); err != nil {
		return err
	}
	dir, err := os.Open(filepath.Dir(s.Path))
	if err != nil {
		return fmt.Errorf("sync scheduler state directory: %w", err)
	}
	defer dir.Close()
	_ = dir.Sync()
	return nil
}

type MemoryStore struct {
	mu    sync.Mutex
	state State
}

func NewMemoryStore() *MemoryStore {
	return &MemoryStore{state: emptyState()}
}

func (s *MemoryStore) Load(ctx context.Context) (State, error) {
	if err := ctx.Err(); err != nil {
		return State{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return cloneState(s.state), nil
}

func (s *MemoryStore) Save(ctx context.Context, state State) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	normalizeState(&state)
	s.state = cloneState(state)
	return nil
}

func emptyState() State {
	return State{
		SchemaVersion: SchemaVersion,
		Jobs:          make(map[string]Job),
		Runs:          make(map[string]Run),
		EventDedupe:   make(map[string]string),
	}
}

func normalizeState(state *State) {
	if state.SchemaVersion == 0 {
		state.SchemaVersion = SchemaVersion
	}
	if state.Jobs == nil {
		state.Jobs = make(map[string]Job)
	}
	if state.Runs == nil {
		state.Runs = make(map[string]Run)
	}
	if state.EventDedupe == nil {
		state.EventDedupe = make(map[string]string)
	}
}

func cloneState(state State) State {
	normalizeState(&state)
	out := State{
		SchemaVersion: state.SchemaVersion,
		Jobs:          make(map[string]Job, len(state.Jobs)),
		Runs:          make(map[string]Run, len(state.Runs)),
		EventDedupe:   make(map[string]string, len(state.EventDedupe)),
		Metrics:       state.Metrics,
	}
	for id, job := range state.Jobs {
		out.Jobs[id] = cloneJob(job)
	}
	for id, run := range state.Runs {
		out.Runs[id] = cloneRun(run)
	}
	for key, runID := range state.EventDedupe {
		out.EventDedupe[key] = runID
	}
	return out
}

func cloneJob(job Job) Job {
	job.Definition.EnabledToolsets = append([]string(nil), job.Definition.EnabledToolsets...)
	job.Definition.CapabilitiesRequired = append([]string(nil), job.Definition.CapabilitiesRequired...)
	job.Definition.CapabilitiesOptional = append([]string(nil), job.Definition.CapabilitiesOptional...)
	job.Definition.Skills = append([]string(nil), job.Definition.Skills...)
	if job.NextRunAt != nil {
		value := *job.NextRunAt
		job.NextRunAt = &value
	}
	if job.Lease != nil {
		lease := *job.Lease
		job.Lease = &lease
	}
	if job.DeadLetteredAt != nil {
		value := *job.DeadLetteredAt
		job.DeadLetteredAt = &value
	}
	if job.LastDecision != nil {
		decision := *job.LastDecision
		job.LastDecision = &decision
	}
	job.LastDecisionPayload = cloneAnyMap(job.LastDecisionPayload)
	return job
}

func cloneRun(run Run) Run {
	if run.Trigger.DueAt != nil {
		value := *run.Trigger.DueAt
		run.Trigger.DueAt = &value
	}
	run.Trigger.Data = cloneStringMap(run.Trigger.Data)
	if run.Lease != nil {
		lease := *run.Lease
		run.Lease = &lease
	}
	if run.StartedAt != nil {
		value := *run.StartedAt
		run.StartedAt = &value
	}
	if run.CompletedAt != nil {
		value := *run.CompletedAt
		run.CompletedAt = &value
	}
	if run.Result != nil {
		result := *run.Result
		result.Context = cloneAnyMap(result.Context)
		run.Result = &result
	}
	run.Context = cloneAnyMap(run.Context)
	return run
}

func cloneAnyMap(in map[string]any) map[string]any {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]any, len(in))
	for key, value := range in {
		out[key] = value
	}
	return out
}

func cloneStringMap(in map[string]string) map[string]string {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]string, len(in))
	for key, value := range in {
		out[key] = value
	}
	return out
}

func sortedJobIDs(jobs map[string]Job) []string {
	ids := make([]string, 0, len(jobs))
	for id := range jobs {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}

func validID(id string) bool {
	if id == "" || len(id) > 128 || strings.Contains(id, "..") {
		return false
	}
	for _, r := range id {
		switch {
		case r >= 'a' && r <= 'z':
		case r >= 'A' && r <= 'Z':
		case r >= '0' && r <= '9':
		case r == '_' || r == '-' || r == '.':
		default:
			return false
		}
	}
	return true
}
