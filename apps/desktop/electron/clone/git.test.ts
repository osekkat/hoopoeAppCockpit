// hp-2n1 — git wrapper tests using injected CommandRunner.

import { afterEach, beforeEach, expect, test } from "bun:test";
import { mkdtempSync, rmSync, mkdirSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import {
  classifyGitFailure,
  CloneGitError,
  fetchAll,
  initialClone,
  readCurrentBranch,
  readHeadSha,
  type CommandRunner,
} from "./index.ts";

let tempRoot: string;

beforeEach(() => {
  tempRoot = mkdtempSync(join(tmpdir(), "hoopoe-clone-git-"));
});

afterEach(() => {
  rmSync(tempRoot, { recursive: true, force: true });
});

function recordingRunner(behaviors: Record<string, { stdout?: string; stderr?: string; exitCode?: number }>): {
  readonly run: CommandRunner;
  readonly calls: Array<{ readonly cmd: string; readonly args: readonly string[]; readonly cwd: string | undefined }>;
} {
  const calls: Array<{ readonly cmd: string; readonly args: readonly string[]; readonly cwd: string | undefined }> = [];
  const run: CommandRunner = (cmd, args, options) => {
    calls.push({ cmd, args, cwd: options?.cwd });
    const key = args[0] ?? "";
    const behavior = behaviors[key] ?? {};
    return {
      stdout: behavior.stdout ?? "",
      stderr: behavior.stderr ?? "",
      exitCode: behavior.exitCode ?? 0,
    };
  };
  return { run, calls };
}

test("initialClone: passes --no-single-branch and the right args; reports head + branch", () => {
  const dest = join(tempRoot, "child", "repo");
  // Parent dir doesn't exist yet — clone should create it.
  const { run, calls } = recordingRunner({
    clone: { exitCode: 0 },
    branch: { stdout: "main" },
    "rev-parse": { stdout: "0123456789abcdef0123456789abcdef01234567" },
  });
  const out = initialClone({
    originRemote: "git@github.com:org/repo.git",
    destinationPath: dest,
    runCommand: run,
  });
  expect(calls[0]?.cmd).toBe("git");
  expect(calls[0]?.args).toEqual([
    "clone",
    "--no-single-branch",
    "git@github.com:org/repo.git",
    dest,
  ]);
  expect(out.branch).toBe("main");
  expect(out.headSha).toBe("0123456789abcdef0123456789abcdef01234567");
});

test("initialClone: refuses to clone into an existing directory", () => {
  const dest = join(tempRoot, "exists");
  mkdirSync(dest);
  const { run } = recordingRunner({});
  expect(() => initialClone({
    originRemote: "git@github.com:org/repo.git",
    destinationPath: dest,
    runCommand: run,
  })).toThrow(/destination_exists/);
});

test("initialClone: classifies auth failures from stderr", () => {
  const dest = join(tempRoot, "authfail");
  const { run } = recordingRunner({
    clone: {
      exitCode: 128,
      stderr: "git@github.com: Permission denied (publickey).\nfatal: Could not read from remote repository.",
    },
  });
  try {
    initialClone({
      originRemote: "git@github.com:org/private.git",
      destinationPath: dest,
      runCommand: run,
    });
    throw new Error("should have thrown");
  } catch (err) {
    expect(err).toBeInstanceOf(CloneGitError);
    expect((err as CloneGitError).code).toBe("auth_missing");
  }
});

test("fetchAll: invokes git fetch --all --tags --prune in the clone dir", () => {
  mkdirSync(join(tempRoot, "repo"));
  const { run, calls } = recordingRunner({
    fetch: { exitCode: 0 },
    branch: { stdout: "feature/x" },
    "rev-parse": { stdout: "deadbeefdeadbeefdeadbeefdeadbeefdeadbeef" },
  });
  const out = fetchAll({ cloneRepoPath: join(tempRoot, "repo"), runCommand: run });
  expect(calls[0]?.args).toEqual(["fetch", "--all", "--tags", "--prune"]);
  expect(calls[0]?.cwd).toBe(join(tempRoot, "repo"));
  expect(out.branch).toBe("feature/x");
  expect(out.headSha).toBe("deadbeefdeadbeefdeadbeefdeadbeefdeadbeef");
});

test("fetchAll: refuses when the clone dir doesn't exist", () => {
  const { run } = recordingRunner({});
  expect(() => fetchAll({
    cloneRepoPath: join(tempRoot, "no-such-clone"),
    runCommand: run,
  })).toThrow(/clone_missing/);
});

test("fetchAll: classifies network errors", () => {
  mkdirSync(join(tempRoot, "repo"));
  const { run } = recordingRunner({
    fetch: {
      exitCode: 128,
      stderr: "fatal: unable to access 'https://example.invalid/repo.git/': Could not resolve host: example.invalid",
    },
  });
  try {
    fetchAll({ cloneRepoPath: join(tempRoot, "repo"), runCommand: run });
    throw new Error("should have thrown");
  } catch (err) {
    expect((err as CloneGitError).code).toBe("network");
  }
});

test("readCurrentBranch / readHeadSha: tolerate failure with null", () => {
  mkdirSync(join(tempRoot, "repo"));
  const { run } = recordingRunner({
    branch: { exitCode: 1 },
    "rev-parse": { exitCode: 1 },
  });
  expect(readCurrentBranch(join(tempRoot, "repo"), run)).toBeNull();
  expect(readHeadSha(join(tempRoot, "repo"), run)).toBeNull();
});

test("readHeadSha: rejects malformed SHAs", () => {
  mkdirSync(join(tempRoot, "repo"));
  const { run } = recordingRunner({ "rev-parse": { stdout: "not-a-sha" } });
  expect(readHeadSha(join(tempRoot, "repo"), run)).toBeNull();
});

test("classifyGitFailure: covers disk_full and repo-not-found", () => {
  expect(classifyGitFailure("clone_failed", "fatal: write error: No space left on device").code).toBe("disk_full");
  expect(classifyGitFailure("clone_failed", "ERROR: Repository not found.").code).toBe("git_failure");
  expect(classifyGitFailure("clone_failed", "fatal: ...does not exist").code).toBe("git_failure");
});

test("classifyGitFailure: falls back to fetch_failed when nothing matches", () => {
  const err = classifyGitFailure("fetch_failed", "fatal: some new error we don't yet recognize");
  expect(err.code).toBe("fetch_failed");
  expect(err.stderr).toContain("don't yet recognize");
});

test("classifyGitFailure: empty stderr falls back to a generic message", () => {
  const err = classifyGitFailure("clone_failed", "");
  expect(err.code).toBe("clone_failed");
  expect(err.message).toContain("git command failed");
});
