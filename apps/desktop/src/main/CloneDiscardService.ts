// hp-58wp — Moved to `apps/desktop/electron/clone/CloneDiscardService.ts`.
//
// The service lives next to the engine layer it composes
// (`apps/desktop/electron/clone/`) because the desktop tsconfig only
// includes `src/`; reaching from `src/main/` into `electron/` pulls
// additional electron-only files into tsc's project graph and surfaces
// pre-existing latent type errors that aren't in scope for this bead.
//
// This file is intentionally empty so the original path doesn't import
// from `electron/`; remove it after the next clean checkout (rebase /
// prune-untracked) — it has never been committed and exists only as a
// session-local placeholder.
//
// Import the canonical service from
// `electron/clone/CloneDiscardService.ts` instead.

export {};
