// hp-ndx5 — Shared types for the desktop clone watcher.
//
// Watcher lifecycle:
//   created → starting → running → stopping → stopped
//                                           → error  → starting (after retry)
//
// Events (emitted via the optional onEvent listener; the registry forwards
// them to the renderer via events.clone.dirty IPC topic):
//   - "started" — fs.watch handle is up.
//   - "dirty"   — debounced probe finished; payload carries the new
//                 CloneDirtyState delta.
//   - "error"   — fs.watch threw or probeDirtyState raised; the watcher
//                 attempts a single recreate after WATCHER_RETRY_DELAY_MS.
//   - "stopped" — explicit stop() or unrecoverable error.

import type { CloneDirtyState } from "../clone/types.ts";

export type WatcherStatus = "created" | "starting" | "running" | "stopping" | "stopped" | "error";

export type CloneWatcherEvent =
  | { readonly kind: "started"; readonly projectId: string }
  | { readonly kind: "dirty"; readonly projectId: string; readonly state: CloneDirtyState }
  | { readonly kind: "error"; readonly projectId: string; readonly code: string; readonly message: string }
  | { readonly kind: "stopped"; readonly projectId: string; readonly reason: "explicit" | "unrecoverable" };

export type CloneWatcherListener = (event: CloneWatcherEvent) => void;

/** Default debounce window per plan.md §7.7. Editor saves often produce
 *  a burst of fs events (write tmp / rename / fsync); 500ms collapses
 *  the burst into a single probe. */
export const DEFAULT_DEBOUNCE_MS = 500;

/** When fs.watch errors, wait this long before recreating. Avoids hot
 *  loops when the clone path is briefly unavailable (network drive
 *  unmount, sandbox revoke, etc.). */
export const WATCHER_RETRY_DELAY_MS = 2_000;
