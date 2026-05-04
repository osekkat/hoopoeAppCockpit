// hp-2n1 — Subprocess-injectable git wrapper for the local clone.
//
// All git invocations route through `runCommand` so tests can replay
// canned outputs without shelling out. The wrapper is read-only from
// Hoopoe's perspective: it clones, fetches, queries — never commits,
// pushes, branches, resets, cleans, or merges. Mutating the desktop
// local clone is forbidden by Guardrail 3; canonical Git writes run on
// the VPS clone through daemon RPCs.
//
// Subprocess error handling intentionally captures stderr so the renderer
// can surface "Repository not found" / "Permission denied (publickey)"
// directly in the project header instead of a generic "git failed".

import { execFileSync } from "node:child_process";
import { existsSync } from "node:fs";
import { dirname } from "node:path";
import { mkdirSync } from "node:fs";

export interface CommandResult {
  readonly stdout: string;
  readonly stderr: string;
  readonly exitCode: number;
}

export interface CommandOptions {
  readonly cwd?: string;
  readonly timeoutMs?: number;
  readonly env?: NodeJS.ProcessEnv;
}

export interface CommandRunner {
  (
    cmd: string,
    args: readonly string[],
    options?: CommandOptions,
  ): CommandResult;
}

export const DESKTOP_MIRROR_READ_ONLY_ERROR_CODE = "desktop_mirror_read_only";

const DESKTOP_MIRROR_MUTATING_GIT_SUBCOMMANDS: ReadonlySet<string> = new Set([
  "add",
  "am",
  "apply",
  "bisect",
  "branch",
  "checkout",
  "cherry-pick",
  "clean",
  "commit",
  "merge",
  "mv",
  "pull",
  "push",
  "rebase",
  "reset",
  "restore",
  "revert",
  "rm",
  "stash",
  "switch",
  "tag",
  "worktree",
]);

export function assertDesktopMirrorGitReadOnlyCommand(args: readonly string[]): void {
  const subcommand = firstGitSubcommand(args);
  if (subcommand === null) return;
  if (isReadOnlyBranchQuery(args)) return;
  if (!DESKTOP_MIRROR_MUTATING_GIT_SUBCOMMANDS.has(subcommand)) return;

  throw new CloneGitError(
    DESKTOP_MIRROR_READ_ONLY_ERROR_CODE,
    `desktop local clone is read-only; git ${subcommand} must run on the VPS clone through daemon RPCs`,
    "",
  );
}

export function runDesktopMirrorGit(
  run: CommandRunner,
  args: readonly string[],
  options?: CommandOptions,
): CommandResult {
  assertDesktopMirrorGitReadOnlyCommand(args);
  return run("git", args, options);
}

function firstGitSubcommand(args: readonly string[]): string | null {
  for (let i = 0; i < args.length; i += 1) {
    const arg = args[i] ?? "";
    if (arg.length === 0) continue;
    if (arg === "-C" || arg === "-c" || arg === "--git-dir" || arg === "--work-tree" || arg === "--namespace") {
      i += 1;
      continue;
    }
    if (
      arg.startsWith("--git-dir=") ||
      arg.startsWith("--work-tree=") ||
      arg.startsWith("--namespace=")
    ) {
      continue;
    }
    if (arg.startsWith("-")) continue;
    return arg;
  }
  return null;
}

function isReadOnlyBranchQuery(args: readonly string[]): boolean {
  return args.length === 2 && args[0] === "branch" && args[1] === "--show-current";
}

export const defaultRunCommand: CommandRunner = (cmd, args, options) => {
  try {
    const stdout = execFileSync(cmd, args, {
      cwd: options?.cwd,
      encoding: "utf8",
      stdio: ["ignore", "pipe", "pipe"],
      ...(options?.timeoutMs ? { timeout: options.timeoutMs } : {}),
      ...(options?.env ? { env: options.env } : {}),
    });
    return { stdout: stdout.trim(), stderr: "", exitCode: 0 };
  } catch (err) {
    const exitCode = typeof (err as { status?: unknown }).status === "number"
      ? ((err as { status: number }).status)
      : 1;
    const stdout = decodeStdio((err as { stdout?: unknown }).stdout);
    const stderr = decodeStdio((err as { stderr?: unknown }).stderr) || (err as Error).message;
    return { stdout: stdout.trim(), stderr: stderr.trim(), exitCode };
  }
};

function decodeStdio(value: unknown): string {
  if (typeof value === "string") return value;
  if (value instanceof Uint8Array) return Buffer.from(value).toString("utf8");
  return "";
}

export class CloneGitError extends Error {
  override readonly name = "CloneGitError";
  readonly code: string;
  readonly stderr: string;
  constructor(code: string, message: string, stderr: string) {
    super(`clone-git (${code}): ${message}`);
    this.code = code;
    this.stderr = stderr;
  }
}

export interface InitialCloneInput {
  readonly originRemote: string;
  readonly destinationPath: string;
  readonly runCommand?: CommandRunner;
  /** Defaults to 10 minutes — plenty for a typical clone, generous
   *  enough for slow corporate networks. */
  readonly timeoutMs?: number;
  /** Custom env vars (e.g. GIT_SSH_COMMAND for non-default key paths). */
  readonly env?: NodeJS.ProcessEnv;
}

export interface InitialCloneResult {
  readonly destinationPath: string;
  /** Branch checked out by default (HEAD of origin). */
  readonly branch: string | null;
  /** SHA of HEAD after the clone completes. */
  readonly headSha: string | null;
}

/** Run `git clone <origin> <dest>`. The destination must NOT already
 *  exist; that's a user-visible "clone already present" error and the
 *  caller should branch on it (e.g. fetch instead). */
export function initialClone(input: InitialCloneInput): InitialCloneResult {
  const run = input.runCommand ?? defaultRunCommand;
  if (existsSync(input.destinationPath)) {
    throw new CloneGitError(
      "destination_exists",
      `cannot clone into ${input.destinationPath}: directory already exists`,
      "",
    );
  }
  // git insists the parent directory exist for clone.
  const parent = dirname(input.destinationPath);
  if (!existsSync(parent)) {
    mkdirSync(parent, { recursive: true, mode: 0o755 });
  }
  const result = run(
    "git",
    ["clone", "--no-single-branch", input.originRemote, input.destinationPath],
    {
      ...(input.timeoutMs ? { timeoutMs: input.timeoutMs } : { timeoutMs: 600_000 }),
      ...(input.env ? { env: input.env } : {}),
    },
  );
  if (result.exitCode !== 0) {
    throw classifyGitFailure("clone_failed", result.stderr);
  }
  return {
    destinationPath: input.destinationPath,
    branch: readCurrentBranch(input.destinationPath, run),
    headSha: readHeadSha(input.destinationPath, run),
  };
}

export interface FetchAllInput {
  readonly cloneRepoPath: string;
  readonly runCommand?: CommandRunner;
  readonly timeoutMs?: number;
  readonly env?: NodeJS.ProcessEnv;
}

export interface FetchAllResult {
  readonly headSha: string | null;
  readonly branch: string | null;
}

/** Run `git fetch --all --tags --prune` against an existing clone. */
export function fetchAll(input: FetchAllInput): FetchAllResult {
  const run = input.runCommand ?? defaultRunCommand;
  if (!existsSync(input.cloneRepoPath)) {
    throw new CloneGitError(
      "clone_missing",
      `cannot fetch into ${input.cloneRepoPath}: directory does not exist`,
      "",
    );
  }
  const result = runDesktopMirrorGit(
    run,
    ["fetch", "--all", "--tags", "--prune"],
    {
      cwd: input.cloneRepoPath,
      ...(input.timeoutMs ? { timeoutMs: input.timeoutMs } : { timeoutMs: 300_000 }),
      ...(input.env ? { env: input.env } : {}),
    },
  );
  if (result.exitCode !== 0) {
    throw classifyGitFailure("fetch_failed", result.stderr);
  }
  return {
    branch: readCurrentBranch(input.cloneRepoPath, run),
    headSha: readHeadSha(input.cloneRepoPath, run),
  };
}

export function readCurrentBranch(cloneRepoPath: string, runCommand: CommandRunner = defaultRunCommand): string | null {
  const result = runDesktopMirrorGit(runCommand, ["branch", "--show-current"], { cwd: cloneRepoPath });
  if (result.exitCode !== 0) return null;
  return result.stdout.length > 0 ? result.stdout : null;
}

export function readHeadSha(cloneRepoPath: string, runCommand: CommandRunner = defaultRunCommand): string | null {
  const result = runDesktopMirrorGit(runCommand, ["rev-parse", "HEAD"], { cwd: cloneRepoPath });
  if (result.exitCode !== 0) return null;
  return result.stdout.length === 40 ? result.stdout : null;
}

/** Map common git stderr signatures to typed CloneGitError codes so the
 *  UI can render an actionable message instead of raw stderr. */
export function classifyGitFailure(
  fallbackCode:
    | "clone_failed"
    | "fetch_failed",
  stderr: string,
): CloneGitError {
  const lower = stderr.toLowerCase();
  if (
    lower.includes("permission denied (publickey)") ||
    lower.includes("could not read username") ||
    lower.includes("authentication failed") ||
    lower.includes("invalid username or password")
  ) {
    return new CloneGitError(
      "auth_missing",
      "Git authentication failed — check your SSH key or credential helper.",
      stderr,
    );
  }
  if (
    lower.includes("could not resolve host") ||
    lower.includes("connection timed out") ||
    lower.includes("network is unreachable")
  ) {
    return new CloneGitError(
      "network",
      "Network error while reaching the origin remote.",
      stderr,
    );
  }
  if (lower.includes("no space left on device")) {
    return new CloneGitError(
      "disk_full",
      "Disk full while cloning.",
      stderr,
    );
  }
  if (lower.includes("repository not found") || lower.includes("does not exist")) {
    return new CloneGitError(
      "git_failure",
      "Origin remote does not exist or is not visible to your credentials.",
      stderr,
    );
  }
  return new CloneGitError(fallbackCode, stderr || "git command failed", stderr);
}
