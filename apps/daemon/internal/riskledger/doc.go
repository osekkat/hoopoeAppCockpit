// Package riskledger owns the §14 named-risk catalog: one entry
// per plan.md §14 risk, each with the mitigation strategy from the
// plan, the verification fixture that asserts the mitigation, and
// the phase-epic bead that owns the implementation.
//
// hp-c2ox engine-first slice: this package starts with the typed
// Risk surface + the §14 catalog of 14 entries as a single source
// of truth. The chaos / per-phase fixtures, the docs/risks.md
// cross-reference, and the CI gate that fails loudly when a named
// risk's mitigation regresses are follow-up cuts on the same bead.
//
// The historical hp-n5k bead lumped all §14 risks as a single
// "risk mitigation" task; this catalog is the explicit
// decomposition the bead requested. New risks discovered post-launch
// get added with `DiscoveredIn` set so reviewers can see when each
// row entered the ledger.
//
// Per plan.md §14 (the risks list) + §1.4 (every automation must
// be inspectable — the catalog itself is inspectable evidence of
// the mitigation map).
package riskledger
