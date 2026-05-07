// Package diagnostics owns the Diagnostics-screen repair-action
// catalog (§10.2 of plan.md). Each repair surfaces in the
// Diagnostics screen, is wired to a typed daemon RPC, audits its
// invocation, gates destructive operations behind confirmation +
// approval, and reports the result with enough context for the
// user to verify it actually worked.
//
// hp-6d7 engine-first slice: this package starts with the typed
// RepairAction surface + the §10.2 catalog of 12 entries as a
// single source of truth. The handler wiring (typed RPC handlers,
// confirmation/approval flow, impact-warning copy, audit emission,
// E2E tests for force-release / restart-daemon / re-run-acfs-doctor)
// is follow-up cuts on the same bead.
//
// The existing onboarding repair catalog at
// `apps/daemon/internal/onboarding/checkpoints/repair.go` is
// scoped to the wizard flow (resume_step / skip_step / view_logs +
// a small overlap with diagnostics actions). This package owns the
// broader §10.2 surface; future work can coalesce the two into one
// rendered list at the Diagnostics-screen boundary.
//
// Per plan.md §10.2 (the table) + §5.3 (destructive operations
// require approval) + §10.3 (schema migrations + rollback used by
// daemon restart).
package diagnostics
