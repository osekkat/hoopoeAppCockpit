// hp-411d Phase 1.5 — shared chromium host-readiness check.
//
// Both the desktop smoke suite (apps/desktop/tests/smoke/e2e/) and the
// hp-j30 deeper suite (apps/desktop/tests/e2e/) call into this helper so
// either both run or both skip with the SAME deterministic reason. The
// pre-hp-411d state had hp-j30 skipping while smoke failed 3/3 with
// `libgbm.so.1` ENOENT — exactly the inconsistency this bead exists to
// close.
//
// The check is intentionally Linux-only library probing. macOS / Windows
// hosts always count as ready because Playwright bundles browsers with
// their own Chromium that doesn't share-link against the system libgbm.

import { existsSync } from "node:fs";

/** System libraries Playwright's bundled Chromium dynamically loads on
 *  Linux. If `libgbm.so.1` is missing, Chromium fails to launch with
 *  `error while loading shared libraries`; the test then errors before
 *  any assertion runs. We only check `libgbm` because it is the most
 *  commonly missing dependency on barebones CI/dev hosts (xvfb hosts
 *  usually have the rest). */
const LINUX_CHROMIUM_LIBS: readonly string[] = [
  "/usr/lib/x86_64-linux-gnu/libgbm.so.1",
  "/usr/lib/aarch64-linux-gnu/libgbm.so.1",
  "/usr/lib64/libgbm.so.1",
];

export interface ChromiumHostStatus {
  readonly ready: boolean;
  /** Human-readable reason — populated when `ready === false`. Used by the
   *  e2e orchestrator to print a deterministic skip line and by the spec
   *  files as the `test.skip()` reason. */
  readonly reason: string;
  /** The platform this status was computed against (helpful for log
   *  attachments / structured runner output). */
  readonly platform: NodeJS.Platform;
}

export interface DetectChromiumHostInput {
  readonly platform?: NodeJS.Platform;
  readonly fileExistsImpl?: (path: string) => boolean;
}

/** Returns `{ ready: true }` when the platform isn't Linux OR at least
 *  one of the well-known libgbm paths exists; `{ ready: false, reason }`
 *  otherwise. Pure: `platform` and `fileExistsImpl` are overridable for
 *  unit tests. */
export function detectChromiumHost(input: DetectChromiumHostInput = {}): ChromiumHostStatus {
  const platform = input.platform ?? process.platform;
  const fileExists = input.fileExistsImpl ?? existsSync;

  if (platform !== "linux") {
    return { ready: true, reason: "non-linux platform — Playwright handles browser deps", platform };
  }
  const found = LINUX_CHROMIUM_LIBS.some((path) => fileExists(path));
  if (found) {
    return { ready: true, reason: "libgbm.so.1 present", platform };
  }
  return {
    ready: false,
    reason:
      "libgbm.so.1 not found (looked under /usr/lib/x86_64-linux-gnu, /usr/lib/aarch64-linux-gnu, /usr/lib64). " +
      "Install Playwright host deps via `npx playwright install-deps` (apt: libgbm1 libgtk-3-0 libnss3 libasound2 fonts-liberation xvfb) " +
      "or run on a host that bundles Chromium dependencies. See docs/development/e2e-host-requirements.md.",
    platform,
  };
}

/** Convenience: the global status, computed once per process. Spec files
 *  should call this rather than re-invoking `detectChromiumHost()` to
 *  ensure logs / skip reasons are stable across `test.describe` blocks. */
let cachedStatus: ChromiumHostStatus | null = null;
export function chromiumHostStatus(): ChromiumHostStatus {
  cachedStatus = cachedStatus ?? detectChromiumHost();
  return cachedStatus;
}

export function resetChromiumHostStatusForTesting(): void {
  cachedStatus = null;
}
