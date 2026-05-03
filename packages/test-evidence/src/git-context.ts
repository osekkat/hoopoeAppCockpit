// `@hoopoe/test-evidence` — git context for the envelope (hp-6sv).
//
// Reads the current commit sha + dirty-tree status by shelling out to
// `git`. Defensive: if the tree isn't a git repo, returns `unknown`
// rather than throwing — evidence files are still useful in CI when the
// checkout is shallow / detached.

import { spawnSync } from "node:child_process";

export interface GitContext {
  sha: string;
  branch: string | null;
  dirty: boolean;
}

export interface ReadGitContextOptions {
  cwd?: string;
}

function run(cwd: string, args: readonly string[]): { ok: boolean; stdout: string } {
  const result = spawnSync("git", args, { cwd, encoding: "utf8", stdio: ["ignore", "pipe", "pipe"] });
  if (result.status !== 0 || result.error !== undefined) return { ok: false, stdout: "" };
  return { ok: true, stdout: (result.stdout ?? "").trim() };
}

export function readGitContext(options: ReadGitContextOptions = {}): GitContext {
  const cwd = options.cwd ?? process.cwd();
  const sha = run(cwd, ["rev-parse", "HEAD"]);
  if (!sha.ok || sha.stdout.length === 0) {
    return { sha: "unknown", branch: null, dirty: false };
  }
  const branch = run(cwd, ["rev-parse", "--abbrev-ref", "HEAD"]);
  const status = run(cwd, ["status", "--porcelain"]);
  return {
    sha: sha.stdout,
    branch: branch.ok && branch.stdout.length > 0 && branch.stdout !== "HEAD" ? branch.stdout : null,
    dirty: status.ok && status.stdout.length > 0,
  };
}
