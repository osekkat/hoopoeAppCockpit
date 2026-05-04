// hp-2n1 — Dirty-state detector for the local clone.
//
// Per plan.md §7.7 'DIRTY BANNER':
//   "A file watcher on the local clone detects modifications, untracked
//    files, or local commits. When detected, project header shows yellow
//    banner: 'Local clone has unsaved changes — Hoopoe ignores local
//    edits.'"
//
// The detector parses `git status --porcelain=v1 -uall --branch` so we
// get untracked files + ahead/behind in one round trip. The watcher
// (chokidar in main process — wired up by the BackendLifecycle) calls
// `probeDirtyState()` after debounced filesystem events; the result lands
// in `CloneState.dirtyState` via `updateCloneState()`.

import { defaultRunCommand, type CommandRunner } from "./git.ts";
import { CLEAN_CLONE_STATE, type CloneDirtyState } from "./types.ts";

export interface ProbeDirtyStateInput {
  readonly cloneRepoPath: string;
  readonly runCommand?: CommandRunner;
  readonly timeoutMs?: number;
}

/** Run `git status --porcelain=v1 -uall --branch` and parse it into a
 *  `CloneDirtyState`. Returns CLEAN_CLONE_STATE when git fails so the
 *  banner doesn't false-positive on transient errors — caller can
 *  separately surface persistent failures via the sync-status pill. */
export function probeDirtyState(input: ProbeDirtyStateInput): CloneDirtyState {
  const run = input.runCommand ?? defaultRunCommand;
  const result = run(
    "git",
    ["status", "--porcelain=v1", "-uall", "--branch"],
    {
      cwd: input.cloneRepoPath,
      ...(input.timeoutMs ? { timeoutMs: input.timeoutMs } : { timeoutMs: 30_000 }),
    },
  );
  if (result.exitCode !== 0) {
    return CLEAN_CLONE_STATE;
  }
  return parsePorcelain(result.stdout);
}

/** Pure parser; exported for unit tests. Input is the stdout of
 *  `git status --porcelain=v1 -uall --branch`. */
export function parsePorcelain(stdout: string): CloneDirtyState {
  let modifiedCount = 0;
  let untrackedCount = 0;
  let aheadCount = 0;
  let behindCount = 0;

  const lines = stdout.length === 0 ? [] : stdout.split("\n");
  for (const line of lines) {
    if (line.length === 0) continue;
    if (line.startsWith("##")) {
      const ahead = /\bahead (\d+)\b/.exec(line);
      const behind = /\bbehind (\d+)\b/.exec(line);
      if (ahead && ahead[1] !== undefined) aheadCount += Number(ahead[1]);
      if (behind && behind[1] !== undefined) behindCount += Number(behind[1]);
      continue;
    }
    if (line.startsWith("??")) {
      untrackedCount += 1;
      continue;
    }
    // Anything else (' M', 'M ', 'MM', 'A ', 'D ', etc.) is a tracked
    // change — modified, added, deleted, renamed, etc. We collapse all of
    // those into "modifiedCount" because the banner doesn't differentiate.
    modifiedCount += 1;
  }

  const dirty = modifiedCount > 0 || untrackedCount > 0 || aheadCount > 0;
  return { dirty, modifiedCount, untrackedCount, aheadCount, behindCount };
}
