// hp-2n1 — clone state IO tests.

import { afterEach, beforeEach, expect, test } from "bun:test";
import { mkdtempSync, readFileSync, rmSync, writeFileSync, mkdirSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import {
  CloneStateError,
  CLONE_STATE_SCHEMA_VERSION,
  cloneRepoPath,
  cloneStateFilePath,
  emptyCloneState,
  ensureCloneState,
  projectDir,
  readCloneState,
  updateCloneState,
  writeCloneState,
  type CloneStorageLayout,
} from "./index.ts";

let tempRoot: string;
let layout: CloneStorageLayout;

beforeEach(() => {
  tempRoot = mkdtempSync(join(tmpdir(), "hoopoe-clone-state-"));
  layout = { projectsRoot: tempRoot };
});

afterEach(() => {
  rmSync(tempRoot, { recursive: true, force: true });
});

test("projectDir / cloneStateFilePath / cloneRepoPath share a parent", () => {
  expect(projectDir(layout, "proj-A")).toBe(join(tempRoot, "proj-A"));
  expect(cloneStateFilePath(layout, "proj-A")).toBe(join(tempRoot, "proj-A", "clone-state.json"));
  expect(cloneRepoPath(layout, "proj-A")).toBe(join(tempRoot, "proj-A", "repo"));
});

test("ensureSafeProjectId: refuses path-traversal + path separators + leading dot", () => {
  expect(() => projectDir(layout, "../escape")).toThrow(/unsafe_project_id/);
  expect(() => projectDir(layout, "a/b")).toThrow(/unsafe_project_id/);
  expect(() => projectDir(layout, "a\\b")).toThrow(/unsafe_project_id/);
  expect(() => projectDir(layout, ".hidden")).toThrow(/unsafe_project_id/);
  expect(() => projectDir(layout, "")).toThrow(/empty_project_id/);
  // Valid UUID-shaped id is fine.
  expect(() => projectDir(layout, "550e8400-e29b-41d4-a716-446655440000")).not.toThrow();
});

test("readCloneState: returns null for an unknown project", () => {
  expect(readCloneState(layout, "never-written")).toBeNull();
});

test("writeCloneState then readCloneState round-trips the state", () => {
  const fresh = emptyCloneState({
    projectId: "pjt",
    originRemote: "git@github.com:org/repo.git",
    now: () => new Date("2026-05-04T00:00:00Z"),
  });
  writeCloneState(layout, fresh);
  const got = readCloneState(layout, "pjt");
  expect(got).toEqual(fresh);
  // The file is JSON pretty-printed for human inspection — read raw and
  // confirm.
  const raw = readFileSync(cloneStateFilePath(layout, "pjt"), "utf8");
  expect(raw).toContain(`"schemaVersion": ${CLONE_STATE_SCHEMA_VERSION}`);
  expect(raw).toContain(`"originRemote": "git@github.com:org/repo.git"`);
});

test("readCloneState: throws on schema mismatch", () => {
  mkdirSync(projectDir(layout, "bad"), { recursive: true });
  writeFileSync(cloneStateFilePath(layout, "bad"), JSON.stringify({ schemaVersion: 999, state: {} }));
  expect(() => readCloneState(layout, "bad")).toThrow(/schema_mismatch/);
});

test("readCloneState: throws on invalid JSON", () => {
  mkdirSync(projectDir(layout, "garbage"), { recursive: true });
  writeFileSync(cloneStateFilePath(layout, "garbage"), "{not json");
  expect(() => readCloneState(layout, "garbage")).toThrow(/parse_failed/);
});

test("ensureCloneState: idempotent — second call returns the first state", () => {
  const first = ensureCloneState(layout, {
    projectId: "idem",
    originRemote: "https://github.com/org/repo.git",
  });
  const second = ensureCloneState(layout, {
    projectId: "idem",
    originRemote: "https://github.com/SOMEONE_ELSE/repo.git",
  });
  // Second call returns the first persisted value (origin remote unchanged).
  expect(second.originRemote).toBe("https://github.com/org/repo.git");
  expect(second.projectId).toBe(first.projectId);
});

test("updateCloneState: applies the patcher and writes atomically", () => {
  ensureCloneState(layout, { projectId: "u", originRemote: "git@github.com:o/r.git" });
  const updated = updateCloneState(layout, "u", (current) => ({
    ...current,
    syncStatus: "synced",
    lastFetchedSha: "0123456789abcdef0123456789abcdef01234567",
    sizeBytes: 12_345,
    lastSyncedAt: "2026-05-04T01:23:45Z",
  }));
  expect(updated.syncStatus).toBe("synced");
  expect(updated.lastFetchedSha).toBe("0123456789abcdef0123456789abcdef01234567");
  // Round-trip via disk to confirm it actually persisted.
  const fresh = readCloneState(layout, "u");
  expect(fresh?.syncStatus).toBe("synced");
  expect(fresh?.sizeBytes).toBe(12_345);
});

test("updateCloneState: throws when no state has ever been written", () => {
  expect(() => updateCloneState(layout, "missing", (s) => s)).toThrow(/missing_state/);
});

test("updateCloneState: refuses patchers that change projectId", () => {
  ensureCloneState(layout, { projectId: "stable", originRemote: "git@github.com:o/r.git" });
  expect(() => updateCloneState(layout, "stable", (s) => ({ ...s, projectId: "switcheroo" }))).toThrow(
    /projectId_changed/,
  );
});

test("CloneStateError: stable name + readable code", () => {
  const err = new CloneStateError("schema_mismatch", "test");
  expect(err.name).toBe("CloneStateError");
  expect(err.code).toBe("schema_mismatch");
  expect(err.message).toContain("schema_mismatch");
});

test("emptyCloneState: defaults to uncloned + clean", () => {
  const fresh = emptyCloneState({
    projectId: "x",
    originRemote: "git@github.com:o/r.git",
    now: () => new Date("2026-05-04T00:00:00Z"),
  });
  expect(fresh.syncStatus).toBe("uncloned");
  expect(fresh.sizeBytes).toBe(0);
  expect(fresh.dirtyState.dirty).toBe(false);
  expect(fresh.lastError).toBeNull();
  expect(fresh.lastFetchedSha).toBeNull();
  expect(fresh.lastAccessedAt).toBe("2026-05-04T00:00:00.000Z");
});
