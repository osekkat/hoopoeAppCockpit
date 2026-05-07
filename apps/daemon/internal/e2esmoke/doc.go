// Package e2esmoke owns the §18.2 end-to-end smoke-scenario
// catalog: the 16-step user journey from clean macOS to a landed
// small project that runs before every beta release as a CI gate.
//
// hp-so6 engine-first slice: this package starts with the typed
// ScenarioStep surface + the §18.2 catalog of 16 entries as a
// single source of truth. The harness runner (real-VPS variant +
// mock-flywheel variant for nightly CI), the per-step evidence
// emission to docs/test-evidence/e2e-smoke/, and the pre-release
// gate wiring are follow-up cuts on the same bead.
//
// Distinct from `apps/daemon/internal/releasesmoke` (hp-f99) which
// owns the §18.4 per-release smoke checks. §18.4 is "10 invariants
// the cockpit must hold on each release"; §18.2 is "a single
// 16-step journey through the cockpit". Both run before every
// beta release; they cover different surfaces.
//
// Per plan.md §18.2 (the scenario) + §13 (Mock Flywheel Mode is
// the testable substrate for the mock variant) + §1.6 (boring
// first run — the scenario asserts the existing-VPS path stays
// boring after every release).
package e2esmoke
