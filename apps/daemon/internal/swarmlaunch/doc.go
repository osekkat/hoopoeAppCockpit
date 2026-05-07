// Package swarmlaunch owns the §7.3 swarm-launch sequence
// catalog: the 10 ordered stages, the 6 launch gates that must
// pass before stage 4, the 6 warning categories surfaced before
// spawn, and the 12-item default launch policy applied to each
// agent kickoff.
//
// hp-pux engine-first slice: this package starts with the typed
// Stage / Gate / Warning / PolicyDirective surfaces + the §7.3
// catalogs as a single source of truth. The orchestration runner
// (state machine that walks the stages), the per-gate evaluator,
// the per-warning renderer, the post-commit auto-push hook
// installer, and the kickoff prompt template are follow-up cuts
// on the same bead.
//
// Per plan.md §7.3 (the launch sequence + default policy + push
// hook), §4.2 (the launch gates), §1.1 (canonical wins on
// reconcile), §1.5 (build for restartability — the orchestrator
// resumes mid-sequence on daemon restart).
package swarmlaunch
