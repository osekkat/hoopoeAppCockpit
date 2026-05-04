// hp-58wp — Engine-side discard of local changes against the desktop's
// local clone (§7.7 'DIRTY BANNER' destructive action).
//
// Runs `git reset --hard @{u}` followed by `git clean -fd`:
//   - reset moves HEAD + working tree + index back to the upstream-tracked
//     ref. Tracked-modified, staged, and ahead-of-upstream commits all go.
//   - clean removes untracked files and directories. .gitignored files are
//     preserved (use `-x` to nuke those too — deliberately NOT included).
//
// Safety posture per Guardrail 2:
//   - `cloneRepoPath` is validated against the on-disk filesystem; the
//     caller (CloneDiscardService) resolves it from the project registry,
//     never from a renderer-supplied string.
//   - argv is explicit, non-interpolated, no shell.
//   - timeouts default to 60s (slow disks under load) but are bounded.
//
// The two git invocations are sequenced (not chained) so we can map a
// reset failure to `no_upstream` while a clean failure maps to
// `clean_failed` — UI surfaces them differently. A failure in step 1
// short-circuits step 2.

import { existsSync, statSync } from "node:fs";
import { join } from "node:path";

import {
  CloneGitError,
  defaultRunCommand,
  type CommandRunner,
} from "./git.ts";

export interface DiscardLocalChangesInput {
  readonly cloneRepoPath: string;
  readonly runCommand?: CommandRunner;
  readonly timeoutMs?: number;
  readonly env?: NodeJS.ProcessEnv;
}

export interface DiscardLocalChangesResult {
  /** Number of paths reported by `git clean -fd`. Counted from stdout
   *  ("Removing <path>" lines); `-1` when stdout was empty (clean ran but
   *  reported nothing). Used by audit + Activity panel. */
  readonly removedPathCount: number;
  /** SHA the working tree now matches (the upstream ref `@{u}`). */
  readonly resetToSha: string | null;
}

/** Pre-validate the clone path. Refuses non-existent / non-directory /
 *  no-`.git`-subdir targets so a malformed projectId never sweeps over
 *  another directory. */
function validateCloneRepoPath(cloneRepoPath: string): void {
  if (!existsSync(cloneRepoPath)) {
    throw new CloneGitError(
      "clone_missing",
      `clone repo not found at ${cloneRepoPath}`,
      "",
    );
  }
  let stat;
  try {
    stat = statSync(cloneRepoPath);
  } catch (err) {
    throw new CloneGitError(
      "clone_missing",
      `cannot stat ${cloneRepoPath}: ${(err as Error).message}`,
      "",
    );
  }
  if (!stat.isDirectory()) {
    throw new CloneGitError(
      "clone_missing",
      `${cloneRepoPath} is not a directory`,
      "",
    );
  }
  // .git can be a directory (normal clone) or a file (worktrees / submodules).
  // Either is acceptable; absence is not.
  if (!existsSync(join(cloneRepoPath, ".git"))) {
    throw new CloneGitError(
      "clone_missing",
      `${cloneRepoPath} does not look like a git clone (.git is missing)`,
      "",
    );
  }
}

/** Map common reset/clean stderr signatures to typed `CloneGitError` codes
 *  so callers can render an actionable message. The two failure modes that
 *  matter most are:
 *    - reset has no upstream tracking branch → user must pick a branch
 *      (rare; surfaces when a clone is detached or branch tracking lost).
 *    - clean was blocked by a busy/locked file (Windows / IDE lock) →
 *      retry / close the editor.
 *  Everything else lands in the generic fallback. */
function classifyDiscardFailure(
  fallbackCode: "reset_failed" | "clean_failed",
  stderr: string,
): CloneGitError {
  const lower = stderr.toLowerCase();
  if (
    lower.includes("no upstream configured") ||
    lower.includes("no upstream branch") ||
    lower.includes("does not have an upstream branch") ||
    lower.includes("no tracked branch") ||
    lower.includes("ambiguous argument '@{u}'")
  ) {
    return new CloneGitError(
      "no_upstream",
      "current branch has no upstream — cannot reset to @{u}",
      stderr,
    );
  }
  if (
    lower.includes("could not lstat") ||
    lower.includes("would not remove") ||
    lower.includes("permission denied") ||
    lower.includes("operation not permitted")
  ) {
    return new CloneGitError(
      "busy_file",
      "git could not remove a path (file may be locked or owned by another process)",
      stderr,
    );
  }
  if (lower.includes("no space left on device")) {
    return new CloneGitError("disk_full", "disk full while discarding", stderr);
  }
  return new CloneGitError(fallbackCode, stderr || "git command failed", stderr);
}

/** Counts "Removing <path>" lines from `git clean -fd` stdout. Returns -1
 *  when stdout is empty, as a signal that clean ran but reported nothing.
 *  We deliberately don't trust the line count for security decisions; it
 *  drives the audit summary so the user knows roughly how much was wiped. */
function countRemovedPaths(stdout: string): number {
  if (stdout.length === 0) return -1;
  const lines = stdout.split("\n");
  let count = 0;
  for (const line of lines) {
    if (line.startsWith("Removing ")) count += 1;
  }
  return count;
}

/** Run `git reset --hard @{u} && git clean -fd` (sequenced) against the
 *  local clone. Throws CloneGitError on any failure; success returns a
 *  small summary used for audit + UI. */
export function discardLocalChanges(input: DiscardLocalChangesInput): DiscardLocalChangesResult {
  validateCloneRepoPath(input.cloneRepoPath);
  const run = input.runCommand ?? defaultRunCommand;
  const timeoutMs = input.timeoutMs ?? 60_000;
  const env = input.env ?? undefined;

  const reset = run(
    "git",
    ["reset", "--hard", "@{u}"],
    {
      cwd: input.cloneRepoPath,
      timeoutMs,
      ...(env ? { env } : {}),
    },
  );
  if (reset.exitCode !== 0) {
    throw classifyDiscardFailure("reset_failed", reset.stderr);
  }

  // Resolve the upstream SHA after the reset so the audit entry can record
  // exactly what HEAD now points at. A failure here is non-fatal — the
  // reset succeeded and we don't want to pretend it didn't because of a
  // post-state read.
  let resetToSha: string | null = null;
  const headRead = run(
    "git",
    ["rev-parse", "HEAD"],
    { cwd: input.cloneRepoPath, timeoutMs },
  );
  if (headRead.exitCode === 0 && headRead.stdout.length === 40) {
    resetToSha = headRead.stdout;
  }

  const clean = run(
    "git",
    ["clean", "-fd"],
    {
      cwd: input.cloneRepoPath,
      timeoutMs,
      ...(env ? { env } : {}),
    },
  );
  if (clean.exitCode !== 0) {
    throw classifyDiscardFailure("clean_failed", clean.stderr);
  }

  return {
    removedPathCount: countRemovedPaths(clean.stdout),
    resetToSha,
  };
}
