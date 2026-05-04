// hp-2n1 — dirty-state detector tests.

import { expect, test } from "bun:test";
import { CLEAN_CLONE_STATE, parsePorcelain, probeDirtyState } from "./index.ts";

test("parsePorcelain: empty stdout = clean", () => {
  expect(parsePorcelain("")).toEqual(CLEAN_CLONE_STATE);
});

test("parsePorcelain: branch line with no ahead/behind = clean", () => {
  expect(parsePorcelain("## main...origin/main\n")).toEqual(CLEAN_CLONE_STATE);
});

test("parsePorcelain: ahead-by-3 = dirty (local commits)", () => {
  const got = parsePorcelain("## main...origin/main [ahead 3]\n");
  expect(got).toEqual({
    dirty: true,
    modifiedCount: 0,
    untrackedCount: 0,
    aheadCount: 3,
    behindCount: 0,
  });
});

test("parsePorcelain: behind-by-2 alone is NOT dirty (banner uses 'dirty' for divergence FROM the user)", () => {
  // Behind = upstream has new commits the user hasn't pulled. That's a
  // sync state, not dirty edits. We still surface behindCount so the
  // banner can mention "5 new commits on origin" but `dirty` stays false.
  const got = parsePorcelain("## main...origin/main [behind 2]\n");
  expect(got.dirty).toBe(false);
  expect(got.behindCount).toBe(2);
});

test("parsePorcelain: ahead AND behind populates both counts", () => {
  const got = parsePorcelain("## main...origin/main [ahead 1, behind 4]\n");
  expect(got.aheadCount).toBe(1);
  expect(got.behindCount).toBe(4);
  expect(got.dirty).toBe(true); // because aheadCount > 0
});

test("parsePorcelain: counts untracked entries from `??`", () => {
  const stdout = [
    "## main...origin/main",
    "?? new-file.txt",
    "?? another-new-file.md",
    "?? subdir/",
  ].join("\n");
  const got = parsePorcelain(stdout);
  expect(got.untrackedCount).toBe(3);
  expect(got.modifiedCount).toBe(0);
  expect(got.dirty).toBe(true);
});

test("parsePorcelain: counts modified/added/deleted/renamed as one bucket", () => {
  const stdout = [
    "## main...origin/main",
    " M src/foo.ts",      // modified, unstaged
    "M  src/bar.ts",      // modified, staged
    "MM src/baz.ts",      // staged + further unstaged mods
    "A  src/new.ts",      // added
    "D  src/gone.ts",     // deleted
    "R  src/from.ts -> src/to.ts", // renamed
  ].join("\n");
  const got = parsePorcelain(stdout);
  expect(got.modifiedCount).toBe(6);
  expect(got.dirty).toBe(true);
});

test("parsePorcelain: handles trailing newline gracefully", () => {
  expect(parsePorcelain("## main...origin/main\n M file.ts\n")).toEqual({
    dirty: true,
    modifiedCount: 1,
    untrackedCount: 0,
    aheadCount: 0,
    behindCount: 0,
  });
});

test("probeDirtyState: returns CLEAN_CLONE_STATE on git failure (so the banner doesn't false-positive)", () => {
  const got = probeDirtyState({
    cloneRepoPath: "/dev/null/no-such-dir",
    runCommand: () => ({ stdout: "", stderr: "fatal: not a git repo", exitCode: 128 }),
  });
  expect(got).toEqual(CLEAN_CLONE_STATE);
});

test("probeDirtyState: parses successful porcelain output", () => {
  const got = probeDirtyState({
    cloneRepoPath: "/tmp/fake",
    runCommand: () => ({
      stdout: "## main...origin/main [ahead 1]\n M README.md\n?? scratch.txt\n",
      stderr: "",
      exitCode: 0,
    }),
  });
  expect(got).toEqual({
    dirty: true,
    modifiedCount: 1,
    untrackedCount: 1,
    aheadCount: 1,
    behindCount: 0,
  });
});

test("probeDirtyState: passes the right git args", () => {
  const calls: Array<{ readonly cmd: string; readonly args: readonly string[]; readonly cwd: string | undefined }> = [];
  probeDirtyState({
    cloneRepoPath: "/tmp/fake",
    runCommand: (cmd, args, options) => {
      calls.push({ cmd, args, cwd: options?.cwd });
      return { stdout: "## main...origin/main\n", stderr: "", exitCode: 0 };
    },
  });
  expect(calls[0]?.cmd).toBe("git");
  expect(calls[0]?.args).toEqual(["status", "--porcelain=v1", "-uall", "--branch"]);
  expect(calls[0]?.cwd).toBe("/tmp/fake");
});
