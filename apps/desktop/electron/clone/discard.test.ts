// hp-58wp — discardLocalChanges engine tests.
//
// All git invocations are routed through an injected CommandRunner; the
// only filesystem touch is the .git existence check, which we satisfy by
// creating a real temp dir + .git stub. No real clones are made.

import { afterEach, beforeEach, expect, test } from "bun:test";
import { mkdirSync, mkdtempSync, rmSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";

import {
  CloneGitError,
  type CommandRunner,
  discardLocalChanges,
} from "./index.ts";

let tempRoot: string;

beforeEach(() => {
  tempRoot = mkdtempSync(join(tmpdir(), "hoopoe-clone-discard-"));
});

afterEach(() => {
  rmSync(tempRoot, { recursive: true, force: true });
});

interface RunnerCall {
  readonly cmd: string;
  readonly args: readonly string[];
  readonly cwd: string | undefined;
}

function recordingRunner(behaviors: Record<string, { stdout?: string; stderr?: string; exitCode?: number }>): {
  readonly run: CommandRunner;
  readonly calls: RunnerCall[];
} {
  const calls: RunnerCall[] = [];
  const run: CommandRunner = (cmd, args, options) => {
    calls.push({ cmd, args, cwd: options?.cwd });
    // Key by the first two args ("reset --hard" vs "clean -fd" vs "rev-parse").
    const key = `${args[0] ?? ""} ${args[1] ?? ""}`.trim();
    const behavior = behaviors[key] ?? {};
    return {
      stdout: behavior.stdout ?? "",
      stderr: behavior.stderr ?? "",
      exitCode: behavior.exitCode ?? 0,
    };
  };
  return { run, calls };
}

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
  const { run } = recordingRunner({});
  expect(() => discardLocalChanges({ cloneRepoPath: missing, runCommand: run })).toThrow(/clone_missing/);
});

test("discardLocalChanges: refuses paths missing a .git entry", () => {
  const repo = join(tempRoot, "not-a-clone");
  mkdirSync(repo);
  const { run, calls } = recordingRunner({});
  expect(() => discardLocalChanges({ cloneRepoPath: repo, runCommand: run })).toThrow(/clone_missing/);
  expect(calls.length).toBe(0);
});

test("discardLocalChanges: refuses when the path is a file, not a directory", () => {
  const file = join(tempRoot, "file");
  writeFileSync(file, "");
  const { run } = recordingRunner({});
  expect(() => discardLocalChanges({ cloneRepoPath: file, runCommand: run })).toThrow(/clone_missing/);
});

test("discardLocalChanges: runs `git reset --hard @{u}` then `git clean -fd` with explicit argv", () => {
  const repo = makeFakeClone();
  const { run, calls } = recordingRunner({
    "reset --hard": { exitCode: 0 },
    "rev-parse HEAD": { stdout: "0123456789abcdef0123456789abcdef01234567" },
    "clean -fd": { stdout: "Removing dist/\nRemoving notes.tmp\n" },
  });
  const result = discardLocalChanges({ cloneRepoPath: repo, runCommand: run });

  expect(calls[0]).toEqual({ cmd: "git", args: ["reset", "--hard", "@{u}"], cwd: repo });
  expect(calls[1]).toEqual({ cmd: "git", args: ["rev-parse", "HEAD"], cwd: repo });
  expect(calls[2]).toEqual({ cmd: "git", args: ["clean", "-fd"], cwd: repo });
  // No shell, no -x, no user-supplied refs — argv is hermetic.
  expect(result.removedPathCount).toBe(2);
  expect(result.resetToSha).toBe("0123456789abcdef0123456789abcdef01234567");
});

test("discardLocalChanges: short-circuits clean when reset fails", () => {
  const repo = makeFakeClone();
  const { run, calls } = recordingRunner({
    "reset --hard": { exitCode: 128, stderr: "fatal: ambiguous argument '@{u}'" },
  });
  expect(() => discardLocalChanges({ cloneRepoPath: repo, runCommand: run })).toThrow(/no_upstream/);
  // Only the reset call should have been made.
  for (const call of calls) {
    expect(call.args[0]).toBe("reset");
  }
});

test("discardLocalChanges: classifies missing-upstream stderr as `no_upstream`", () => {
  const repo = makeFakeClone();
  const { run } = recordingRunner({
    "reset --hard": {
      exitCode: 128,
      stderr: "fatal: no upstream configured for branch 'feature/foo'",
    },
  });
  try {
    discardLocalChanges({ cloneRepoPath: repo, runCommand: run });
    throw new Error("expected throw");
  } catch (err) {
    expect(err).toBeInstanceOf(CloneGitError);
    expect((err as CloneGitError).code).toBe("no_upstream");
  }
});

test("discardLocalChanges: classifies clean failure as `busy_file` when stderr mentions lstat/permission", () => {
  const repo = makeFakeClone();
  const { run } = recordingRunner({
    "reset --hard": { exitCode: 0 },
    "rev-parse HEAD": { stdout: "0123456789abcdef0123456789abcdef01234567" },
    "clean -fd": {
      exitCode: 1,
      stderr: "warning: could not lstat 'foo/.lock': Operation not permitted",
    },
  });
  try {
    discardLocalChanges({ cloneRepoPath: repo, runCommand: run });
    throw new Error("expected throw");
  } catch (err) {
    expect(err).toBeInstanceOf(CloneGitError);
    expect((err as CloneGitError).code).toBe("busy_file");
  }
});

test("discardLocalChanges: classifies `no space left on device` as disk_full", () => {
  const repo = makeFakeClone();
  const { run } = recordingRunner({
    "reset --hard": { exitCode: 1, stderr: "fatal: write error: No space left on device" },
  });
  expect(() => discardLocalChanges({ cloneRepoPath: repo, runCommand: run })).toThrow(/disk_full/);
});

test("discardLocalChanges: returns -1 removedPathCount when clean stdout is empty", () => {
  const repo = makeFakeClone();
  const { run } = recordingRunner({
    "reset --hard": { exitCode: 0 },
    "rev-parse HEAD": { stdout: "0123456789abcdef0123456789abcdef01234567" },
    "clean -fd": { stdout: "" },
  });
  const result = discardLocalChanges({ cloneRepoPath: repo, runCommand: run });
  expect(result.removedPathCount).toBe(-1);
  expect(result.resetToSha).toBe("0123456789abcdef0123456789abcdef01234567");
});

test("discardLocalChanges: tolerates rev-parse failure (reset succeeded; sha is null)", () => {
  // A failed `git rev-parse HEAD` immediately after a successful reset is
  // a non-fatal post-state read. The discard still succeeded.
  const repo = makeFakeClone();
  const { run } = recordingRunner({
    "reset --hard": { exitCode: 0 },
    "rev-parse HEAD": { exitCode: 1, stderr: "fatal: ambiguous argument 'HEAD'" },
    "clean -fd": { stdout: "" },
  });
  const result = discardLocalChanges({ cloneRepoPath: repo, runCommand: run });
  expect(result.resetToSha).toBeNull();
});

test("discardLocalChanges: argv never includes -x, --interactive, or user-supplied paths", () => {
  // Defense-in-depth: even if a future refactor lets caller-supplied data
  // flow into options, the engine must continue to use a fixed argv shape.
  const repo = makeFakeClone();
  const { run, calls } = recordingRunner({
    "reset --hard": { exitCode: 0 },
    "rev-parse HEAD": { stdout: "0123456789abcdef0123456789abcdef01234567" },
    "clean -fd": { stdout: "" },
  });
  discardLocalChanges({ cloneRepoPath: repo, runCommand: run });
  for (const call of calls) {
    for (const arg of call.args) {
      expect(arg).not.toBe("-x");
      expect(arg).not.toBe("--interactive");
      expect(arg).not.toBe("-i");
      expect(arg.startsWith("--exclude")).toBe(false);
    }
  }
});
