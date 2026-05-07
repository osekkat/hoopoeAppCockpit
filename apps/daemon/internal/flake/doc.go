// Package flake owns the cognitive layer above the build/test queue
// (hp-977 + hp-gkk): turning raw test/build failure output into a
// stable fingerprint that the failure ledger keys on, so agents
// stop re-running the same broken test 50× per session and so the
// flake detector can distinguish (a) real-unchanged failures, (b)
// intermittent flakes, and (c) genuinely-new failures.
//
// hp-aa02 engine-first slice: this package starts with the pure
// fingerprint normalizer (no DB, no HTTP, no scheduler integration —
// just `raw log → stable fingerprint string`). The persistence
// layer (failure_ledger table), the per-fingerprint state machine,
// the flake-bead auto-creator, and the tend-swarm "known unchanged
// failure" rail are follow-up cuts on the same bead.
//
// Per plan.md §2.7 "Repeated failure fingerprints" + §8.5 "known
// unchanged failure" + §14 risk #6 "Agents compete for builds/tests".
package flake
