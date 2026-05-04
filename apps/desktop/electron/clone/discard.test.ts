// hp-58wp/hp-hde4 — discardLocalChanges engine tests.
//
// The retired discard engine still validates registry-resolved clone
// paths, then refuses the operation before any git reset/clean can run.

import { afterEach, beforeEach, expect, test } from "bun:test";
import { mkdirSync, mkdtempSync, rmSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";

import {
  CloneGitError,
  DESKTOP_MIRROR_READ_ONLY_ERROR_CODE,
  discardLocalChanges,
} from "./index.ts";

let tempRoot: string;

beforeEach(() => {
  tempRoot = mkdtempSync(join(tmpdir(), "hoopoe-clone-discard-"));
});

afterEach(() => {
  rmSync(tempRoot, { recursive: true, force: true });
});

function makeFakeClone(): string {
  const repo = join(tempRoot, "repo");
  mkdirSync(repo, { recursive: true });
  // .git can be a directory or a file; an empty file satisfies the
  // existsSync check the engine uses.
  writeFileSync(join(repo, ".git"), "gitdir: /not/used\n");
  return repo;
}

test("discardLocalChanges: refuses non-existent clone paths", () => {
  const missing = join(tempRoot, "does-not-exist");
  expect(() => discardLocalChanges({ cloneRepoPath: missing })).toThrow(/clone_missing/);
});

test("discardLocalChanges: refuses paths missing a .git entry", () => {
  const repo = join(tempRoot, "not-a-clone");
  mkdirSync(repo);
  expect(() => discardLocalChanges({ cloneRepoPath: repo })).toThrow(/clone_missing/);
});

test("discardLocalChanges: refuses when the path is a file, not a directory", () => {
  const file = join(tempRoot, "file");
  writeFileSync(file, "");
  expect(() => discardLocalChanges({ cloneRepoPath: file })).toThrow(/clone_missing/);
});

test("discardLocalChanges: rejects valid mirrors with the read-only guard", () => {
  const repo = makeFakeClone();
  try {
    discardLocalChanges({ cloneRepoPath: repo });
    throw new Error("expected throw");
  } catch (err) {
    expect(err).toBeInstanceOf(CloneGitError);
    expect((err as CloneGitError).code).toBe(DESKTOP_MIRROR_READ_ONLY_ERROR_CODE);
    expect((err as CloneGitError).message).toContain("read-only");
  }
});
