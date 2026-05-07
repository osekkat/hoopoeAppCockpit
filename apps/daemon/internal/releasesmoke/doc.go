// Package releasesmoke owns the §18.4 pre-release smoke gate
// catalog. Every Hoopoe beta release must pass these 10 checks
// before publish; CI runs the full suite as a hard gate and
// failures block release.
//
// hp-f99 engine-first slice: this package starts with the typed
// SmokeCheck surface + the §18.4 catalog of 10 checks as the
// single source of truth. The runner harness, per-check
// assertions, evidence emission to docs/test-evidence/release-<v>/,
// and CI workflow wiring are follow-up cuts on the same bead.
//
// Distinct from `apps/daemon/internal/release` which owns daemon
// release-artifact verification (signatures, SLSA provenance,
// SBOM). That package answers "is this binary signed and provably
// built from this commit"; this one answers "does the cockpit
// behave correctly end-to-end before we let users install it".
//
// Per plan.md §18.4 (release smoke checks) + §10.5 (SLO targets
// referenced by checks 6 and 7) + §10.4 (export/restore semantics
// covered by check 10).
package releasesmoke
