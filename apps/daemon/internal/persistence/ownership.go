// Package persistence declares the canonical-ownership manifest for
// every persistent store the Hoopoe daemon owns or mirrors.
//
// hp-w2zr: plan.md §2.2 says the Hoopoe daemon is an API facade, not
// a new canonical database. The daemon owns Hoopoe job/event/cache/
// preferences/plan/onboarding/health/audit state and explicitly does
// NOT own bead truth, Git truth, NTM session truth, Agent Mail truth,
// file reservation truth, or test-report truth — those come from the
// project's own tools.
//
// The pre-existing code mostly preserves the facade pattern (br is
// CLI-driven, Git is adapter-driven, etc.) but the SQL/JSONL schemas
// did not mechanically tag stores by their source-of-truth role, so
// the boundary was enforced by code review and convention only. This
// package adds a checkable contract: every persistent store the daemon
// touches declares one of four ownership classes, and a registry test
// asserts the manifest covers the canonical store list.
//
// A future drift test (out of scope for this bead's first cut) can
// scan apps/daemon/internal/ for new persistence surfaces and refuse
// any that aren't classified here.
package persistence

import (
	"errors"
	"fmt"
	"sort"
)

// Class is the canonical-ownership classification per plan.md §2.2.
type Class string

const (
	// ClassOwned: state Hoopoe is the canonical source of (no upstream
	// tool owns this — losing the file means losing the truth). Examples:
	// auth pairings/sessions, scheduler state, audit log, approvals.
	ClassOwned Class = "owned"
	// ClassReadModel: cached projection of upstream-canonical state to
	// power UI reads. Source of truth lives in the upstream tool; the
	// projection MUST be reconcilable. Example: .beads/issues.jsonl read
	// by the br adapter, NTM session snapshots.
	ClassReadModel Class = "read_model"
	// ClassCache: TTL-bounded derived data that can be safely deleted —
	// the daemon recomputes on miss. Examples: Git diff snapshots,
	// idempotency keys (recreated on retry), capability probe results.
	ClassCache Class = "cache"
	// ClassExternalToolArtifact: files written by an upstream tool that
	// Hoopoe reads but does not parse-as-canonical. Example: Git pack
	// files when Hoopoe inspects a clone.
	ClassExternalToolArtifact Class = "external_tool_artifact"
)

// validClasses enumerates the legal Class values. Used by Validate to
// reject unknown classes (typos, drift) before they enter the registry.
var validClasses = map[Class]struct{}{
	ClassOwned:                {},
	ClassReadModel:            {},
	ClassCache:                {},
	ClassExternalToolArtifact: {},
}

// StoreEntry is one row in the persistence manifest. Each entry binds a
// repository path (the file backing the store) to its ownership class
// + a short rationale + the canonical owning tool when the class is
// ReadModel/ExternalToolArtifact.
type StoreEntry struct {
	// Name is the human-readable identifier used in errors and logs.
	Name string
	// Package is the Go import path under apps/daemon/internal/ where
	// the store lives. Used by drift tests to detect new persistence
	// surfaces missing from the manifest.
	Package string
	// Class is one of the four canonical ownership classes.
	Class Class
	// PathPattern is the on-disk filename (relative to stateDir) where
	// the store persists. Empty when the store is process-local only.
	PathPattern string
	// CanonicalOwner names the upstream tool when Class is ReadModel
	// or ExternalToolArtifact. Empty for Owned/Cache.
	CanonicalOwner string
	// Rationale explains why this Class applies. Persisted in the
	// manifest so future readers can verify the classification.
	Rationale string
}

// RegisteredStores is the canonical persistence manifest. Every persistent
// store the Hoopoe daemon touches MUST appear here with a Class assignment.
//
// Adding a new store: append an entry here with the import path and
// classification. The registry test below will fail until the entry
// is valid (Class in canonical set, Name + Package non-empty).
//
// Removing or relocating a store: update the entry; do not silently
// drop it from the manifest — drift tests rely on the manifest being
// the authoritative list.
var RegisteredStores = []StoreEntry{
	{
		Name:        "auth.pairing",
		Package:     "apps/daemon/internal/auth",
		Class:       ClassOwned,
		PathPattern: "auth/pairings.jsonl",
		Rationale:   "Daemon-issued pairing tokens are Hoopoe-authoritative — no upstream owns them.",
	},
	{
		Name:        "auth.session",
		Package:     "apps/daemon/internal/auth",
		Class:       ClassOwned,
		PathPattern: "auth/sessions.jsonl",
		Rationale:   "Per-session bearer revocation state is Hoopoe-authoritative (hp-b7rx).",
	},
	{
		Name:        "auth.server_secret",
		Package:     "apps/daemon/internal/auth",
		Class:       ClassOwned,
		PathPattern: "auth/server-secret.json",
		Rationale:   "Daemon signing secret; owned outright.",
	},
	{
		Name:        "approvals.file_store",
		Package:     "apps/daemon/internal/approvals",
		Class:       ClassOwned,
		PathPattern: "approvals/approvals.jsonl",
		Rationale:   "Unified approval queue (hp-rh0w) — Hoopoe policy gates + DCG verdicts + SLB co-signature converge here as canonical Hoopoe state.",
	},
	{
		Name:        "scheduler.state",
		Package:     "apps/daemon/internal/scheduler",
		Class:       ClassOwned,
		PathPattern: "scheduler/state.json",
		Rationale:   "Tending scheduler runs, jobs, leases — Hoopoe is canonical.",
	},
	{
		Name:        "jobs.registry",
		Package:     "apps/daemon/internal/jobs",
		Class:       ClassOwned,
		PathPattern: "jobs/jobs.jsonl",
		Rationale:   "Long-running daemon jobs — Hoopoe-owned execution state.",
	},
	{
		Name:        "jobs.log_store",
		Package:     "apps/daemon/internal/jobs/log",
		Class:       ClassOwned,
		PathPattern: "jobs/logs/<run-id>.jsonl",
		Rationale:   "Per-run job logs — Hoopoe-owned forensic trail.",
	},
	{
		Name:        "onboarding.checkpoints",
		Package:     "apps/daemon/internal/onboarding/checkpoints",
		Class:       ClassOwned,
		PathPattern: "onboarding.sqlite3",
		Rationale:   "Wizard checkpoint state for resume-after-quit — Hoopoe lifecycle, not upstream.",
	},
	{
		Name:        "projects.registry",
		Package:     "apps/daemon/internal/projects",
		Class:       ClassOwned,
		PathPattern: "projects.sqlite3",
		Rationale:   "Hoopoe project registry metadata (slug, vps_id, lifecycle state). Code truth lives at origin (Class=ExternalToolArtifact for the on-disk clone).",
	},
	{
		Name:        "audit.writer",
		Package:     "apps/daemon/internal/audit",
		Class:       ClassOwned,
		PathPattern: "audit/audit.jsonl",
		Rationale:   "Append-only audit log per plan.md §10 / Guardrail 10. Hoopoe-canonical.",
	},
	{
		Name:        "modelcontext.store",
		Package:     "apps/daemon/internal/modelcontext",
		Class:       ClassOwned,
		PathPattern: "modelcontext/<project-id>.jsonl",
		Rationale:   "Per-project model-context snapshots — Hoopoe lifecycle artifact.",
	},
	{
		Name:        "agent.idempotency",
		Package:     "apps/daemon/internal/agent",
		Class:       ClassCache,
		PathPattern: "agent/idempotency.jsonl",
		Rationale:   "Idempotency keys for ActionPlan retries (hp-cjmc). Cache: regenerable on miss; loss prompts a retry rather than corruption.",
	},
	{
		Name:        "beadflow.evidence",
		Package:     "apps/daemon/internal/beadflow",
		Class:       ClassOwned,
		PathPattern: "beadflow/evidence.jsonl",
		Rationale:   "Bead-flow evidence ledger — Hoopoe lifecycle.",
	},
	{
		Name:        "git.local_clone",
		Package:     "apps/daemon/internal/adapters/git",
		Class:       ClassExternalToolArtifact,
		PathPattern: "<projects-root>/<project>/.git",
		CanonicalOwner: "git (origin)",
		Rationale:   "Code truth lives at origin per plan.md §1.1; the VPS clone is a working copy, not the source of truth.",
	},
	{
		Name:        "br.beads_jsonl",
		Package:     "apps/daemon/internal/adapters/br",
		Class:       ClassReadModel,
		PathPattern: ".beads/issues.jsonl",
		CanonicalOwner: "br (beads_rust)",
		Rationale:   "br owns beads canonically; the daemon reads .beads/issues.jsonl as a cold-start projection only.",
	},
}

// Validate returns an error if any RegisteredStore entry has an unknown
// Class, an empty Name, an empty Package, a missing CanonicalOwner when
// Class is ReadModel/ExternalToolArtifact, or duplicate Name/Package
// pairs that would silently shadow each other in lookups.
func Validate() error {
	var errs []error
	seen := make(map[string]struct{}, len(RegisteredStores))
	for _, entry := range RegisteredStores {
		if entry.Name == "" {
			errs = append(errs, fmt.Errorf("persistence manifest entry has empty Name (Package=%q)", entry.Package))
			continue
		}
		if entry.Package == "" {
			errs = append(errs, fmt.Errorf("persistence manifest entry %q has empty Package", entry.Name))
		}
		if _, ok := validClasses[entry.Class]; !ok {
			errs = append(errs, fmt.Errorf("persistence manifest entry %q has unknown Class %q (must be one of: owned, read_model, cache, external_tool_artifact)", entry.Name, entry.Class))
		}
		needsOwner := entry.Class == ClassReadModel || entry.Class == ClassExternalToolArtifact
		if needsOwner && entry.CanonicalOwner == "" {
			errs = append(errs, fmt.Errorf("persistence manifest entry %q (Class=%s) must declare CanonicalOwner", entry.Name, entry.Class))
		}
		if entry.Rationale == "" {
			errs = append(errs, fmt.Errorf("persistence manifest entry %q has empty Rationale (every classification must be defended)", entry.Name))
		}
		key := entry.Name + "|" + entry.Package
		if _, dup := seen[key]; dup {
			errs = append(errs, fmt.Errorf("persistence manifest has duplicate Name+Package pair %q", key))
		}
		seen[key] = struct{}{}
	}
	return errors.Join(errs...)
}

// ByClass returns entries filtered by Class, sorted by Name. Useful
// for follow-up drift tests that want to enumerate only a slice of
// the manifest (e.g., assert every Owned store has a backing test).
func ByClass(class Class) []StoreEntry {
	out := make([]StoreEntry, 0)
	for _, entry := range RegisteredStores {
		if entry.Class == class {
			out = append(out, entry)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}
