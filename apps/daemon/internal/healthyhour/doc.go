// Package healthyhour owns the §8.6 healthy-hour invariant
// assertion: a healthy hour of swarm operation produces near-zero
// LLM cost (wakeAgent:false on every pre-script) and near-zero
// panel noise ([SILENT] on agent runs that decide nothing was
// warranted). Audit always fires regardless (Guardrail 10).
//
// hp-ilrr engine-first slice: this package starts with the typed
// scheduler-metric row + pre-script/agent-run outcome enums + the
// pure-function CheckInvariants validator that takes a 60-minute
// window of metric rows and returns a typed list of violations.
// The instrumentation wiring (scheduler emitting rows into
// scheduler_metrics SQLite table per §8.3.2), the chaos / fault-
// injection scenarios, the per-job pre-script unit tests, and
// the test-evidence emission to docs/test-evidence/healthy-hour/
// are follow-up cuts on the same bead.
//
// Distinct from `apps/daemon/internal/tendingeval` (hp-rnn) which
// owns the §8.8 fixture catalog itself. This package owns the
// invariant validator hp-rnn's healthy_hour fixture dispatches
// over: hp-rnn declares "this fixture is a healthy hour"; this
// package declares "here is what a healthy hour's metrics must
// look like". The two coordinate via the FixtureID stamp.
//
// Per plan.md §8.6 (the invariants), §8.3.2 (scheduler metrics +
// audit-on-every-tick), §8.7 (cost ceilings only hold if the
// deterministic floor holds), Guardrail 9 (don't wake tending
// LLM jobs when deterministic pre-scripts find nothing actionable),
// Guardrail 10 (don't suppress audit entries just because a job
// returned [SILENT]).
package healthyhour
