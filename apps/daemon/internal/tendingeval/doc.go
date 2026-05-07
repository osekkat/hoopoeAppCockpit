// Package tendingeval owns the §8.8 tending evaluation fixture
// catalog: 12 named scenarios that the harness replays against
// the Mock Flywheel substrate to assert the §8 tending architecture
// behaves correctly across the full healthy-and-unhealthy spectrum.
//
// hp-rnn engine-first slice: this package starts with the typed
// Fixture surface + the §8.8 catalog of 12 entries as a single
// source of truth. The replay harness, the per-fixture assertion
// runners (detections / wake-no-wake / ActionPlan validity /
// approvals / postconditions / Activity / audit / cost+noise
// counters), and the new-action-type coverage gate are follow-up
// cuts on the same bead.
//
// Per plan.md §8.8 (the fixture list + assertion catalog),
// §8.6 (healthy-hour cost+noise invariants — hp-ilrr is the
// dedicated bead for that subset), §8.3.1 (typed ActionPlan
// executor), §13 (Mock Flywheel Mode is the substrate).
package tendingeval
