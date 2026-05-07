// Package exportbundle owns the §10.4 export-bundle manifest +
// the §10.3 retention-policy catalog. The export bundle is the
// `.tar.zst` artifact `hoopoe export project <id>` produces; the
// retention catalog is the per-domain TTL table that drives both
// the bundle's data slice and the daemon's local retention pruner.
//
// hp-o42 engine-first slice: this package starts with the typed
// BundleSection + BundleManifest + RetentionPolicy surface and
// the §10.4/§10.3 catalogs as a single source of truth. The
// bundle writer (tar.zst streaming, redaction, hashing), the
// restore validator (schema-version + hash + compatibility
// checks), and the retention pruner are follow-up cuts on the
// same bead.
//
// Distinct from `apps/daemon/internal/migrations/backup.go`
// which owns daemon SQLite migration backups (single-file
// before-migration snapshot). This package owns the user-facing
// project export bundle (multi-section archive).
//
// Per plan.md §10.3 (retention + migrations) + §10.4 (backup +
// export + restore) + §1.1 (every section listed here is sourced
// from the canonical tool, never from the Hoopoe cache —
// Guardrail 4).
package exportbundle
