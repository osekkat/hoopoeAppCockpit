// hp-58wp/hp-hde4 — Engine-side guard for the retired "discard local
// changes" action.
//
// The desktop local clone is a read-only sync mirror (Guardrail 3). The
// previous implementation attempted to repair dirty mirrors with
// `git reset --hard @{u}` and `git clean -fd`; hp-hde4 closes that
// exception. Keep this exported function as a compatibility boundary for
// the preload channel, but make it refuse before any git command can run.

import { existsSync, statSync } from "node:fs";
import { join } from "node:path";

import {
  CloneGitError,
  DESKTOP_MIRROR_READ_ONLY_ERROR_CODE,
} from "./git.ts";

export interface DiscardLocalChangesInput {
  readonly cloneRepoPath: string;
}

export interface DiscardLocalChangesResult {
  /** This shape is retained for callers compiled against hp-58wp. The
   *  hp-hde4 guard always throws before returning a result. */
  readonly removedPathCount: number;
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

/** Validate the resolved mirror path, then refuse the retired destructive
 *  repair. Validation preserves the old error specificity for bad project
 *  registry state while guaranteeing no git mutation runs on a valid
 *  desktop mirror. */
export function discardLocalChanges(input: DiscardLocalChangesInput): DiscardLocalChangesResult {
  validateCloneRepoPath(input.cloneRepoPath);
  throw new CloneGitError(
    DESKTOP_MIRROR_READ_ONLY_ERROR_CODE,
    "desktop local clone is read-only; Hoopoe refuses to reset or clean the mirror",
    "",
  );
}
