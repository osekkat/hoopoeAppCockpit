// state.go — surface migration state into the wire shape consumed by
// `/v1/compatibility` (`schemas.MigrationState` mirror in the
// capabilities package's hand-rolled types).
//
// The capabilities package's `StaticCompatibilityComposer` is the
// "always-idle, schemaVersion=0, no pending" baseline used by the
// daemon scaffolding. Once this package wires in, the daemon swaps
// the static composer for one that consults a `*Runner` to compute
// real values:
//
//   - schemaVersion: highest applied migration id (0 when fresh).
//   - appliedAt: timestamp of the most recent applied row.
//   - pending: descriptions of every registered migration whose id
//     is > schemaVersion.
//   - phase: idle | running | failed | rolled_back, transitioned by
//     the runner via SetPhase as Run() executes.
//
// We intentionally don't reach for the generated schemas.MigrationState
// type from packages/schemas/go because this package's caller (the
// capabilities composer) is hand-rolled with a structurally-identical
// shape. When that file flips to the generated types, this package's
// State is byte-equivalent and the integration is a one-line swap.
package migrations

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"
)

// Phase mirrors the optional phase field on schemas.MigrationState.
type Phase string

const (
	PhaseIdle      Phase = "idle"
	PhaseRunning   Phase = "running"
	PhaseFailed    Phase = "failed"
	PhaseRollback  Phase = "rolled_back"
)

// State is the JSON-ready snapshot that maps 1:1 to
// `components.schemas.MigrationState` in `packages/schemas/openapi.yaml`.
type State struct {
	SchemaVersion int       `json:"schemaVersion"`
	AppliedAt     time.Time `json:"appliedAt"`
	Pending       []string  `json:"pending"`
	Phase         Phase     `json:"phase,omitempty"`
}

// StateProvider is what the daemon's CompatibilityComposer consumes. It
// returns the live state on demand. Implementations are free to cache,
// but the runner produces a fresh read on every call (it's cheap).
type StateProvider interface {
	State(ctx context.Context) (State, error)
	SetPhase(phase Phase) // for tests + runner-driven phase transitions
}

// RunnerStateProvider implements StateProvider on top of a *Runner.
// Production daemons hold a single instance and pass it to the
// CompatibilityComposer.
type RunnerStateProvider struct {
	runner *Runner
	mu     sync.Mutex
	phase  Phase
}

// NewRunnerStateProvider returns a provider initialized in `idle`.
func NewRunnerStateProvider(runner *Runner) *RunnerStateProvider {
	if runner == nil {
		// The runner is required; we return a non-nil provider that
		// always errors out on State() so callers fail fast in dev.
		return &RunnerStateProvider{phase: PhaseIdle}
	}
	return &RunnerStateProvider{
		runner: runner,
		phase:  PhaseIdle,
	}
}

// SetPhase transitions the reported phase. Callers (typically the runner
// itself, or the daemon's startup code wrapping Run() with phase
// transitions) own the lifecycle.
func (p *RunnerStateProvider) SetPhase(phase Phase) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.phase = phase
}

// State returns the live snapshot. SchemaVersion + AppliedAt come from
// the meta table; Pending comes from the registry diffed against the
// applied set.
func (p *RunnerStateProvider) State(ctx context.Context) (State, error) {
	if p.runner == nil {
		return State{}, errors.New("migrations: state provider has nil runner")
	}

	current, err := p.runner.CurrentVersion(ctx)
	if err != nil {
		return State{}, fmt.Errorf("migrations: state read current version: %w", err)
	}

	pending, err := p.runner.Pending(ctx)
	if err != nil {
		return State{}, fmt.Errorf("migrations: state list pending: %w", err)
	}
	pendingDescriptions := make([]string, 0, len(pending))
	for _, m := range pending {
		pendingDescriptions = append(pendingDescriptions,
			fmt.Sprintf("%04d %s", m.ID(), m.Description()))
	}

	appliedAt, err := p.lastAppliedAt(ctx, current)
	if err != nil {
		return State{}, err
	}

	p.mu.Lock()
	phase := p.phase
	p.mu.Unlock()

	// `pending == [] AND phase == idle` is the steady state. Compute the
	// derived phase if the caller hasn't explicitly set one — keeps the
	// composer honest even when someone forgets to flip the phase.
	if phase == "" {
		if len(pendingDescriptions) == 0 {
			phase = PhaseIdle
		} else {
			phase = PhaseIdle // we haven't started yet; pending is just informational
		}
	}

	return State{
		SchemaVersion: current,
		AppliedAt:     appliedAt,
		Pending:       pendingDescriptions,
		Phase:         phase,
	}, nil
}

// lastAppliedAt reads the applied_at of the row with the highest id, or
// returns the zero time when no migrations have been applied.
func (p *RunnerStateProvider) lastAppliedAt(ctx context.Context, current int) (time.Time, error) {
	if current == 0 {
		return time.Time{}, nil
	}
	var raw string
	err := p.runner.db.QueryRowContext(ctx,
		"SELECT applied_at FROM "+MetaTableName+" WHERE id = ?",
		current,
	).Scan(&raw)
	if err != nil {
		return time.Time{}, fmt.Errorf("migrations: read applied_at for %d: %w", current, err)
	}
	t, err := time.Parse(time.RFC3339Nano, raw)
	if err != nil {
		// Fall back to RFC3339 for older rows (pre-nanosecond).
		t, err = time.Parse(time.RFC3339, raw)
		if err != nil {
			return time.Time{}, fmt.Errorf("migrations: parse applied_at %q: %w", raw, err)
		}
	}
	return t, nil
}

// StaticStateProvider is a fixed-state provider used by tests + the
// daemon's pre-runner scaffolding. Always returns the same State on
// every call.
type StaticStateProvider struct {
	Snapshot State
}

func (s *StaticStateProvider) State(_ context.Context) (State, error) { return s.Snapshot, nil }
func (s *StaticStateProvider) SetPhase(phase Phase) {
	s.Snapshot.Phase = phase
}
