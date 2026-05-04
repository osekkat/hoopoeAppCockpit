// hp-2n1 — Disk size + cap enforcement for the local clone.
//
// Per plan.md §7.7:
//   - Soft cap (default 2 GB): warning surfaces in the settings card; user
//     gets the option to clear blobs older than N days.
//   - Hard cap (default 5 GB): refuses further fetches until cleared or
//     the cap is raised.
//
// The size walker uses `fs.statSync` recursively. For the v1 clone-size
// numbers (~ tens to low hundreds of MB), this is fast enough; for big
// monorepos we'll switch to `du` later. The path is symlink-safe — we
// don't follow symlinks out of the clone tree.

import { existsSync, readdirSync, statSync, type Dirent } from "node:fs";
import { join, resolve } from "node:path";
import { CloneStateError } from "./state.ts";
import { DEFAULT_CLONE_CAPS, type CloneCapConfig } from "./types.ts";

export type CapVerdict =
  | "ok"
  | "soft_cap_exceeded"
  | "hard_cap_exceeded";

export interface CapEvaluation {
  readonly verdict: CapVerdict;
  readonly sizeBytes: number;
  readonly caps: CloneCapConfig;
  /** When `verdict !== "ok"`, the renderer message includes how far over
   *  the offending cap the clone is (in bytes; UI formats to MB/GB). */
  readonly excessBytes: number;
  /** True iff the verdict refuses fetches (i.e. hard cap). The fetcher
   *  consults this before invoking git. */
  readonly fetchAllowed: boolean;
}

/** Recursive directory size in bytes. Skips symlinks (they could point
 *  out of the tree and cause infinite recursion). Returns 0 if `root`
 *  doesn't exist (uncloned project case). */
export function directorySizeBytes(root: string): number {
  const r = resolve(root);
  if (!existsSync(r)) return 0;
  let total = 0;
  // Stack-based walk avoids deep recursion on monorepo trees.
  const stack: string[] = [r];
  while (stack.length > 0) {
    const dir = stack.pop()!;
    let entries: Dirent[];
    try {
      // The string-overload of readdirSync({ withFileTypes: true })
      // returns Dirent[] (not Dirent<NonSharedBuffer>[] which the
      // overload-resolution-via-ReturnType incorrectly inferred).
      entries = readdirSync(dir, { withFileTypes: true }) as Dirent[];
    } catch (err) {
      // Permission denied / ENOENT race: skip this directory rather than
      // crash the size walk. Future calls re-attempt.
      if (isExpectedWalkError(err)) continue;
      throw err;
    }
    for (const entry of entries) {
      const child = join(dir, entry.name);
      if (entry.isSymbolicLink()) continue;
      if (entry.isDirectory()) {
        stack.push(child);
        continue;
      }
      try {
        const st = statSync(child);
        total += st.size;
      } catch (err) {
        if (isExpectedWalkError(err)) continue;
        throw err;
      }
    }
  }
  return total;
}

/** Evaluate caps for a clone of the given size. Pass the per-project cap
 *  override if one is set on `CloneState.capsOverride`; falls back to
 *  the defaults otherwise. */
export function evaluateCaps(
  sizeBytes: number,
  capsOverride: CloneCapConfig | null = null,
): CapEvaluation {
  const caps = capsOverride ?? DEFAULT_CLONE_CAPS;
  validateCaps(caps);
  if (sizeBytes >= caps.hardCapBytes) {
    return {
      verdict: "hard_cap_exceeded",
      sizeBytes,
      caps,
      excessBytes: sizeBytes - caps.hardCapBytes,
      fetchAllowed: false,
    };
  }
  if (sizeBytes >= caps.softCapBytes) {
    return {
      verdict: "soft_cap_exceeded",
      sizeBytes,
      caps,
      excessBytes: sizeBytes - caps.softCapBytes,
      fetchAllowed: true,
    };
  }
  return {
    verdict: "ok",
    sizeBytes,
    caps,
    excessBytes: 0,
    fetchAllowed: true,
  };
}

/** Caps that can't be the wrong way round and are non-zero. The settings
 *  surface should refuse user input that violates this; the runtime
 *  guard is defense-in-depth. */
export function validateCaps(caps: CloneCapConfig): void {
  if (caps.softCapBytes <= 0 || caps.hardCapBytes <= 0) {
    throw new CloneStateError("invalid_caps", "caps must be positive");
  }
  if (caps.hardCapBytes < caps.softCapBytes) {
    throw new CloneStateError(
      "invalid_caps",
      `hardCapBytes (${caps.hardCapBytes}) must be >= softCapBytes (${caps.softCapBytes})`,
    );
  }
}

function isExpectedWalkError(err: unknown): boolean {
  const code = (err as { code?: string } | null)?.code;
  return code === "ENOENT" || code === "EACCES" || code === "EPERM";
}
