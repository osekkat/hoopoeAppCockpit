// Package tendingjobs owns the §8.4 initial tending-job set
// catalog: the seven default jobs (tend-swarm,
// watch-safety-thresholds, push-stale-commits, snapshot-health,
// drift-check, review-readiness-check, orchestrator-chat) the
// scheduler launches at swarm-active time.
//
// hp-fb0 engine-first slice: this package starts with the typed
// JobSpec surface + the §8.4 catalog of 7 entries as a single
// source of truth. The per-job pre-script Go implementations
// (one file per job under this package), the prompt templates
// each ships with, and the scheduler-side dispatch wiring are
// follow-up cuts on the same bead.
//
// Per plan.md §8.4 (the job definitions), §8.5 (what pre-scripts
// cover), §8.6 (healthy-hour invariants — hp-ilrr's validator
// keys on these jobs), §8.8 (evaluation fixtures — hp-rnn's
// catalog dispatches over these jobs), §7.3 (push policy),
// §7.5 (orchestrator-chat realization).
package tendingjobs
