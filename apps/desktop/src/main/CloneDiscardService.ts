// Move-redirect placeholder. The canonical service lives at
// `apps/desktop/electron/clone/CloneDiscardService.ts` because the desktop
// tsconfig only includes `src/`; reaching from `src/main/` into `electron/`
// pulls additional electron-only files into tsc's project graph and
// surfaces pre-existing latent type errors that aren't in scope for the
// move bead.
//
// This stub exists so a renderer or main-process import of
// `src/main/CloneDiscardService` from before hp-58wp resolves to a
// well-defined empty module instead of a missing-file error. There is
// no implementation here on purpose.
//
// Provenance:
//   - Originally added in commit b50aced [hp-58wp] add src/main/
//     CloneDiscardService move-redirect stubs.
//   - The earlier comment in this file claimed the placeholder had
//     "never been committed"; that was incorrect — git log shows
//     b50aced. hp-7fa corrected the wording without deleting the file
//     (deletion requires explicit user permission per AGENTS.md
//     RULE 1; reopen a fresh bead with that authorization).
//
// Import the canonical service from
// `electron/clone/CloneDiscardService.ts` instead.

export {};
