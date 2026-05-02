// Hoopoe-owned. Isolates demo-mode state from real-mode state (hp-lddj).
//
// Per the bead's "PROJECT STATE ISOLATION" section: local-demo daemon
// writes to ~/.hoopoe/demo/<fixture-id>/ — completely separate from
// ~/.hoopoe/. Switching to a real VPS later does NOT see demo state;
// demo state survives reopens but is wiped on user-confirmed
// 'Switch to real VPS'.
//
// This module owns:
//   - Path resolution: realRoot vs demoRoot per fixture id.
//   - Idempotent ensureDemoRoot (creates the demo dir tree).
//   - wipeDemoRoot (called by the wizard on confirmed switch-to-real).
//   - listDemoRoots (so Diagnostics can show which demos have state).
//
// Pure I/O. Used by LocalDemoBootstrap; not coupled to wizard UI.

import { existsSync, mkdirSync, rmSync, readdirSync, statSync } from "node:fs";
import { homedir } from "node:os";
import { join, resolve } from "node:path";

const DEMO_DIR_NAME = ".hoopoe/demo";
const REAL_DIR_NAME = ".hoopoe";
const REQUIRED_SUBDIRS = ["audit", "events", "scratch"] as const;

export interface IsolatorOptions {
  /** Override homedir (tests). Default: `os.homedir()`. */
  homedirImpl?: () => string;
}

export interface DemoStatePaths {
  /** ~/.hoopoe/demo/<fixture-id>/ */
  readonly demoRoot: string;
  /** ~/.hoopoe/demo/<fixture-id>/audit.jsonl — demo audit log,
   *  separate from the real audit log under ~/.hoopoe/audit.jsonl
   *  (per hp-lddj e2e #5). */
  readonly demoAuditLog: string;
  /** Real-mode root (~/.hoopoe/). Demo writes MUST never touch here. */
  readonly realRoot: string;
}

function resolveHome(options: IsolatorOptions): string {
  return (options.homedirImpl ?? homedir)();
}

/** Compute paths for a given fixture id. Pure: no I/O. */
export function resolveDemoStatePaths(
  fixtureId: string,
  options: IsolatorOptions = {},
): DemoStatePaths {
  const home = resolveHome(options);
  const demoRoot = resolve(home, DEMO_DIR_NAME, fixtureId);
  return {
    demoRoot,
    demoAuditLog: join(demoRoot, "audit.jsonl"),
    realRoot: resolve(home, REAL_DIR_NAME),
  };
}

/** Idempotently create the demo state tree for a fixture. */
export function ensureDemoRoot(
  fixtureId: string,
  options: IsolatorOptions = {},
): DemoStatePaths {
  const paths = resolveDemoStatePaths(fixtureId, options);
  mkdirSync(paths.demoRoot, { recursive: true });
  for (const sub of REQUIRED_SUBDIRS) {
    mkdirSync(join(paths.demoRoot, sub), { recursive: true });
  }
  return paths;
}

export interface WipeResult {
  readonly fixtureId: string;
  readonly demoRoot: string;
  readonly existed: boolean;
}

/** Wipe a fixture's demo state. Called by the wizard's "Switch to real
 *  VPS" path AFTER explicit user confirmation. SAFETY:
 *  - Only ever deletes paths that resolve under ~/.hoopoe/demo/.
 *  - Refuses if the resolved path doesn't have ".hoopoe/demo/" in it. */
export function wipeDemoRoot(
  fixtureId: string,
  options: IsolatorOptions = {},
): WipeResult {
  const paths = resolveDemoStatePaths(fixtureId, options);
  // Defense in depth: never rm anything not under .hoopoe/demo.
  if (!paths.demoRoot.includes(`${DEMO_DIR_NAME}/`)) {
    throw new Error(
      `LocalDemoStateIsolator: refusing to wipe path not under ${DEMO_DIR_NAME}/: ${paths.demoRoot}`,
    );
  }
  // Refuse to wipe if path is not a directory (would be a symlink or
  // file misconfiguration we shouldn't touch).
  let existed = false;
  if (existsSync(paths.demoRoot)) {
    const stat = statSync(paths.demoRoot);
    if (!stat.isDirectory()) {
      throw new Error(
        `LocalDemoStateIsolator: refusing to wipe non-directory: ${paths.demoRoot}`,
      );
    }
    existed = true;
    rmSync(paths.demoRoot, { recursive: true, force: false });
  }
  return { fixtureId, demoRoot: paths.demoRoot, existed };
}

/** List fixture-ids that currently have demo state on disk. */
export function listDemoRoots(options: IsolatorOptions = {}): string[] {
  const home = resolveHome(options);
  const root = resolve(home, DEMO_DIR_NAME);
  if (!existsSync(root)) return [];
  return readdirSync(root)
    .filter((name) => {
      try {
        return statSync(join(root, name)).isDirectory();
      } catch {
        return false;
      }
    })
    .sort();
}
