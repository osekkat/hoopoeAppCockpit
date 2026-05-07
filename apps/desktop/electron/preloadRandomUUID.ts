// hp-r1tk: Web Crypto wrapper for the Electron sandboxed preload.
//
// Why this exists: the previous preload imported `randomUUID` from
// `node:crypto`, but Electron's sandboxed preload context does NOT
// expose Node builtins (the sandbox flag explicitly strips them).
// Result: `Error: module not found: node:crypto` at preload load
// time, contextBridge.exposeInMainWorld never runs, and
// `window.hoopoe` is undefined in the renderer.
//
// Web Crypto's `crypto.randomUUID()` is available in sandboxed
// preloads since Chromium 92 / Electron 14, returns an RFC 4122 v4
// UUID, and is the documented replacement for `node:crypto`'s
// randomUUID in renderer / preload contexts.
//
// This file is a separate module (not inline in preload.ts) so
// bun:test can load it without triggering preload.ts's top-level
// `contextBridge.exposeInMainWorld(...)` side effect — that call
// throws in test contexts and would block the unit test that
// verifies the v4-UUID shape contract.

/** Returns an RFC 4122 v4 UUID via the Web Crypto API exposed on
 * `globalThis.crypto`. Used as the `subscriptionId` factory for
 * `createDaemonBridge`. */
export function randomUUID(): string {
  return globalThis.crypto.randomUUID();
}
