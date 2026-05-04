// hp-2n1 — Atomic read/write of `clone-state.json`.
//
// The state file lives next to the clone directory at
// `<HoopoeAppDataRoot>/projects/<projectId>/clone-state.json` (NOT inside
// the clone dir, so a `git clean -fd` against the clone can never wipe
// the cached state). Writes are atomic (write-tmp + rename) so a crash
// in the middle of an update never leaves a half-written JSON file.

import { existsSync, mkdirSync, readFileSync, renameSync, writeFileSync } from "node:fs";
import { dirname, join, resolve } from "node:path";
import {
  CLONE_STATE_SCHEMA_VERSION,
  emptyCloneState,
  type CloneState,
  type CloneStateFile,
} from "./types.ts";

export class CloneStateError extends Error {
  override readonly name = "CloneStateError";
  readonly code: string;
  constructor(code: string, message: string) {
    super(`clone-state (${code}): ${message}`);
    this.code = code;
  }
}

export interface CloneStorageLayout {
  /** Root for all per-project clones, e.g.
   *  `~/Library/Application Support/Hoopoe/projects/`. */
  readonly projectsRoot: string;
}

/** Returns `<projectsRoot>/<projectId>/`. Doesn't create directories. */
export function projectDir(layout: CloneStorageLayout, projectId: string): string {
  ensureSafeProjectId(projectId);
  return resolve(layout.projectsRoot, projectId);
}

/** Returns `<projectsRoot>/<projectId>/clone-state.json`. Doesn't read or
 *  create. */
export function cloneStateFilePath(layout: CloneStorageLayout, projectId: string): string {
  return join(projectDir(layout, projectId), "clone-state.json");
}

/** Returns `<projectsRoot>/<projectId>/repo/` — where the actual git
 *  clone lives. Sibling of `clone-state.json` so a destructive `git
 *  clean -fd` against `repo/` cannot touch the state file. */
export function cloneRepoPath(layout: CloneStorageLayout, projectId: string): string {
  return join(projectDir(layout, projectId), "repo");
}

/** Read the state file; returns `null` when no state has ever been
 *  written for this project (first-run case). Throws on schema mismatch
 *  so callers don't silently consume stale shapes. */
export function readCloneState(
  layout: CloneStorageLayout,
  projectId: string,
): CloneState | null {
  const path = cloneStateFilePath(layout, projectId);
  if (!existsSync(path)) return null;
  let text: string;
  try {
    text = readFileSync(path, "utf8");
  } catch (err) {
    throw new CloneStateError(
      "read_failed",
      `cannot read ${path}: ${(err as Error).message}`,
    );
  }
  let parsed: unknown;
  try {
    parsed = JSON.parse(text);
  } catch (err) {
    throw new CloneStateError(
      "parse_failed",
      `${path}: ${(err as Error).message}`,
    );
  }
  if (
    typeof parsed !== "object" ||
    parsed === null ||
    (parsed as { schemaVersion?: unknown }).schemaVersion !== CLONE_STATE_SCHEMA_VERSION
  ) {
    throw new CloneStateError(
      "schema_mismatch",
      `${path}: schemaVersion must be ${CLONE_STATE_SCHEMA_VERSION}`,
    );
  }
  return (parsed as CloneStateFile).state;
}

/** Atomic write of the state file. Creates the project dir if missing. */
export function writeCloneState(
  layout: CloneStorageLayout,
  state: CloneState,
): void {
  const path = cloneStateFilePath(layout, state.projectId);
  const parent = dirname(path);
  if (!existsSync(parent)) {
    mkdirSync(parent, { recursive: true, mode: 0o755 });
  }
  const body: CloneStateFile = { schemaVersion: CLONE_STATE_SCHEMA_VERSION, state };
  const tmp = `${path}.${process.pid}.tmp`;
  writeFileSync(tmp, `${JSON.stringify(body, null, 2)}\n`, { encoding: "utf8", mode: 0o644 });
  try {
    renameSync(tmp, path);
  } catch (err) {
    throw new CloneStateError(
      "rename_failed",
      `could not rename ${tmp} → ${path}: ${(err as Error).message}`,
    );
  }
}

/** Idempotent — return existing state if present, otherwise initialize a
 *  fresh `uncloned` state for the project and persist it. Used at project-
 *  registration time so the clone manager always has a state file to
 *  update. */
export function ensureCloneState(
  layout: CloneStorageLayout,
  input: { readonly projectId: string; readonly originRemote: string; readonly now?: () => Date },
): CloneState {
  const existing = readCloneState(layout, input.projectId);
  if (existing) return existing;
  const fresh = emptyCloneState(input);
  writeCloneState(layout, fresh);
  return fresh;
}

/** Functional updater: read, mutate via the patcher, write atomically.
 *  Patcher returns a new state object (do NOT mutate the input). */
export function updateCloneState(
  layout: CloneStorageLayout,
  projectId: string,
  patcher: (current: CloneState) => CloneState,
): CloneState {
  const current = readCloneState(layout, projectId);
  if (!current) {
    throw new CloneStateError(
      "missing_state",
      `cannot update unknown clone state for project ${projectId}; call ensureCloneState first`,
    );
  }
  const next = patcher(current);
  if (next.projectId !== current.projectId) {
    throw new CloneStateError(
      "projectId_changed",
      `patcher must not change projectId (${current.projectId} → ${next.projectId})`,
    );
  }
  writeCloneState(layout, next);
  return next;
}

/** Refuse projectIds that contain path separators or `..` segments — they
 *  would break out of `<projectsRoot>` when joined. The id should be a
 *  UUID per `ProjectMetadata.id`; this is defense-in-depth in case a
 *  daemon-side bug ever lets a hand-typed slug through. */
function ensureSafeProjectId(projectId: string): void {
  if (projectId.length === 0) {
    throw new CloneStateError("empty_project_id", "projectId is empty");
  }
  if (projectId.includes("/") || projectId.includes("\\") || projectId.includes("..") || projectId.startsWith(".")) {
    throw new CloneStateError(
      "unsafe_project_id",
      `projectId ${JSON.stringify(projectId)} contains unsafe characters`,
    );
  }
}
