// hp-ilt — Phase 4 project lifecycle helpers.
//
// Pure TS; no Electron dependency. The Electron main process AND the
// daemon-side project-registry handler can both call these to:
//
//   - probe a directory for AGENTS.md / README.md / language manifests
//   - check whether a directory is a Git repo + read origin remote +
//     current branch
//   - initialize `.hoopoe/` (project.json + plans/ + skills.lock.json
//     + model-context-policy.json) on a path
//   - detect whether `.beads/` exists, and run `br init` when missing
//     (callers supply the runner so this module stays test-friendly)
//
// All file IO uses `node:fs`; all subprocess invocations go through a
// caller-supplied `runCommand` so tests can inject a fake.

import { execFileSync } from "node:child_process";
import { existsSync, mkdirSync, readFileSync, statSync, writeFileSync } from "node:fs";
import { basename, join, relative, resolve } from "node:path";
import { randomUUID } from "node:crypto";
import {
  PROJECT_JSON_SCHEMA_VERSION,
  SUPPORTED_LANGUAGE_MANIFESTS,
  type DetectedManifest,
  type DetectedToolEnvironment,
  type GitRepoInfo,
  type LanguageManifestName,
  type ProjectJson,
  type ProjectMetadata,
} from "./types.ts";

export class ProjectLifecycleError extends Error {
  override readonly name = "ProjectLifecycleError";
  readonly code: string;
  constructor(code: string, message: string) {
    super(`project-lifecycle (${code}): ${message}`);
    this.code = code;
  }
}

/** Runs a command and returns trimmed stdout. Caller supplies the
 *  runner so tests can inject a fake. Default implementation uses
 *  `execFileSync` with stdio piped. */
export interface CommandRunner {
  (cmd: string, args: readonly string[], options?: { cwd?: string }): { stdout: string; exitCode: number };
}

export const defaultRunCommand: CommandRunner = (cmd, args, options) => {
  try {
    const stdout = execFileSync(cmd, args, {
      cwd: options?.cwd,
      encoding: "utf8",
      stdio: ["ignore", "pipe", "pipe"],
    });
    return { stdout: stdout.trim(), exitCode: 0 };
  } catch (err) {
    const exitCode =
      typeof (err as { status?: unknown }).status === "number"
        ? ((err as { status: number }).status)
        : 1;
    return { stdout: "", exitCode };
  }
};

interface PathCheckOptions {
  /** Custom command runner (tests). */
  runCommand?: CommandRunner;
}

function ensureExistingDir(rootPath: string): void {
  let st;
  try {
    st = statSync(rootPath);
  } catch (err) {
    throw new ProjectLifecycleError("path_missing", `directory not found: ${rootPath}: ${(err as Error).message}`);
  }
  if (!st.isDirectory()) {
    throw new ProjectLifecycleError("path_not_dir", `${rootPath} is not a directory`);
  }
}

/** Detect AGENTS.md / README.md / language manifests within the
 *  project root. Walk is shallow (root + 1 level deep) — we don't
 *  spelunk node_modules / vendor / target. */
export function detectToolEnvironment(rootPath: string): DetectedToolEnvironment {
  const root = resolve(rootPath);
  ensureExistingDir(root);

  const agentsMdRelative = findFirstCaseInsensitive(root, "AGENTS.md");
  const readmeRelative = findFirstCaseInsensitive(root, "README.md");
  const manifests = detectLanguageManifests(root);
  return {
    agentsMdRelative,
    readmeRelative,
    manifests,
    hasBeadsDir: existsSync(join(root, ".beads")),
    hasHoopoeDir: existsSync(join(root, ".hoopoe")),
  };
}

function findFirstCaseInsensitive(root: string, target: string): string | null {
  const candidates = [target, target.toUpperCase(), target.toLowerCase()];
  for (const name of candidates) {
    const path = join(root, name);
    if (existsSync(path)) return relative(root, path);
  }
  return null;
}

function detectLanguageManifests(root: string): DetectedManifest[] {
  const out: DetectedManifest[] = [];
  for (const name of SUPPORTED_LANGUAGE_MANIFESTS) {
    const candidate = join(root, name);
    if (existsSync(candidate)) {
      out.push({ name: name as LanguageManifestName, relativePath: relative(root, candidate) });
    }
  }
  return out;
}

/** Read git repo info: whether it's a git work tree, origin URL,
 *  current branch. Origin is REQUIRED per §1.1; callers branch on
 *  `originRemote === null` to refuse the project. */
export function readGitRepoInfo(rootPath: string, options: PathCheckOptions = {}): GitRepoInfo {
  const root = resolve(rootPath);
  ensureExistingDir(root);
  const run = options.runCommand ?? defaultRunCommand;

  if (!existsSync(join(root, ".git"))) {
    return { isGitRepo: false, originRemote: null, branch: null };
  }

  const remoteResult = run("git", ["remote", "get-url", "origin"], { cwd: root });
  const branchResult = run("git", ["branch", "--show-current"], { cwd: root });
  return {
    isGitRepo: true,
    originRemote: remoteResult.exitCode === 0 && remoteResult.stdout.length > 0 ? remoteResult.stdout : null,
    branch: branchResult.exitCode === 0 && branchResult.stdout.length > 0 ? branchResult.stdout : null,
  };
}

export interface InitializeHoopoeDirOptions {
  /** Override the project id (default: UUID). */
  projectId?: string;
  /** Override the project name (default: basename of rootPath). */
  name?: string;
  /** Override `Date.now()` for tests. */
  now?: () => Date;
  /** Custom command runner (tests). */
  runCommand?: CommandRunner;
}

export interface InitializeHoopoeDirResult {
  hoopoeDir: string;
  projectJsonPath: string;
  metadata: ProjectMetadata;
  /** True if `.hoopoe/` was newly created; false if it already existed. */
  created: boolean;
}

/** Create `.hoopoe/` under `rootPath`, write `project.json`, set up the
 *  empty `plans/` directory + `skills.lock.json` + `model-context-policy.json`
 *  with sensible defaults. Idempotent: re-running leaves an existing
 *  `.hoopoe/` untouched (returns `created: false`). */
export function initializeHoopoeDir(
  rootPath: string,
  options: InitializeHoopoeDirOptions = {},
): InitializeHoopoeDirResult {
  const root = resolve(rootPath);
  ensureExistingDir(root);

  const git = readGitRepoInfo(root, options.runCommand !== undefined ? { runCommand: options.runCommand } : {});
  if (!git.isGitRepo) {
    throw new ProjectLifecycleError("not_git_repo", `${root} is not a git work tree`);
  }
  if (git.originRemote === null) {
    throw new ProjectLifecycleError(
      "missing_origin",
      `${root} has no 'origin' remote — Hoopoe v1 requires an external Git remote per plan.md §1.1`,
    );
  }
  if (git.branch === null) {
    throw new ProjectLifecycleError("detached_head", `${root} is in a detached-HEAD state; check out a branch first`);
  }

  const tools = detectToolEnvironment(root);
  const hoopoeDir = join(root, ".hoopoe");
  const created = !existsSync(hoopoeDir);
  if (created) {
    mkdirSync(hoopoeDir, { recursive: true, mode: 0o755 });
    mkdirSync(join(hoopoeDir, "plans"), { recursive: true, mode: 0o755 });
  }

  const now = options.now ?? (() => new Date());
  const ts = now().toISOString();
  const name = options.name ?? basename(root);
  const id = options.projectId ?? randomUUID();
  const metadata: ProjectMetadata = {
    id,
    name,
    slug: slugify(name),
    rootPath: root,
    originRemote: git.originRemote,
    branch: git.branch,
    state: "imported",
    createdAt: ts,
    updatedAt: ts,
    tools,
  };

  const projectJsonPath = join(hoopoeDir, "project.json");
  if (created || !existsSync(projectJsonPath)) {
    writeProjectJson(projectJsonPath, metadata);
  }

  // Default skills.lock.json (empty pin set; skill loader hp-4d7 fills it).
  const skillsLockPath = join(hoopoeDir, "skills.lock.json");
  if (!existsSync(skillsLockPath)) {
    writeFileSync(
      skillsLockPath,
      `${JSON.stringify({ schemaVersion: 1, pins: {} }, null, 2)}\n`,
      { encoding: "utf8", mode: 0o644 },
    );
  }

  // Default model-context-policy.json (allow-everything until hp-r33's
  // capability registry feeds the policy).
  const policyPath = join(hoopoeDir, "model-context-policy.json");
  if (!existsSync(policyPath)) {
    writeFileSync(
      policyPath,
      `${JSON.stringify(
        {
          schemaVersion: 1,
          contextPolicy: {
            includeAuditLog: false,
            includeFileGlobs: [],
            excludeFileGlobs: [".env*", "**/secrets/**"],
          },
        },
        null,
        2,
      )}\n`,
      { encoding: "utf8", mode: 0o644 },
    );
  }

  return { hoopoeDir, projectJsonPath, metadata, created };
}

/** Atomic write of project.json (write-tmp + rename). */
export function writeProjectJson(projectJsonPath: string, metadata: ProjectMetadata): void {
  const body: ProjectJson = { schemaVersion: PROJECT_JSON_SCHEMA_VERSION, project: metadata };
  const tmp = `${projectJsonPath}.${process.pid}.tmp`;
  writeFileSync(tmp, `${JSON.stringify(body, null, 2)}\n`, { encoding: "utf8", mode: 0o644 });
  // Atomic on POSIX; close enough on Windows (single-disk renames).
  try {
    require("node:fs").renameSync(tmp, projectJsonPath);
  } catch (err) {
    throw new ProjectLifecycleError("rename_failed", `could not rename ${tmp} → ${projectJsonPath}: ${(err as Error).message}`);
  }
}

/** Read project.json from a `.hoopoe/` directory. */
export function readProjectJson(rootPath: string): ProjectJson {
  const path = join(resolve(rootPath), ".hoopoe", "project.json");
  let text: string;
  try {
    text = readFileSync(path, "utf8");
  } catch (err) {
    throw new ProjectLifecycleError("project_json_missing", `${path}: ${(err as Error).message}`);
  }
  let parsed: unknown;
  try {
    parsed = JSON.parse(text);
  } catch (err) {
    throw new ProjectLifecycleError("project_json_invalid", `${path}: ${(err as Error).message}`);
  }
  if (
    typeof parsed !== "object" ||
    parsed === null ||
    (parsed as { schemaVersion?: unknown }).schemaVersion !== PROJECT_JSON_SCHEMA_VERSION
  ) {
    throw new ProjectLifecycleError(
      "project_json_schema",
      `${path}: schemaVersion must be ${PROJECT_JSON_SCHEMA_VERSION}`,
    );
  }
  return parsed as ProjectJson;
}

export interface BeadsInitOptions {
  /** Custom command runner (tests). Default invokes `br init`. */
  runCommand?: CommandRunner;
}

export interface BeadsInitResult {
  /** True if `br init` was actually run; false if `.beads/` already existed. */
  ran: boolean;
  /** Stdout from the `br init` invocation, if it ran. */
  stdout: string;
  /** Exit code from the `br init` invocation, if it ran. */
  exitCode: number;
}

/** Initialize `.beads/` under `rootPath` if it doesn't exist. Returns
 *  `{ ran: false }` when `.beads/` already exists (per the bead's
 *  "br init runs if missing" wording). */
export function initializeBeadsIfMissing(
  rootPath: string,
  options: BeadsInitOptions = {},
): BeadsInitResult {
  const root = resolve(rootPath);
  ensureExistingDir(root);
  if (existsSync(join(root, ".beads"))) {
    return { ran: false, stdout: "", exitCode: 0 };
  }
  const run = options.runCommand ?? defaultRunCommand;
  const result = run("br", ["init"], { cwd: root });
  return { ran: true, stdout: result.stdout, exitCode: result.exitCode };
}

function slugify(name: string): string {
  return name
    .toLowerCase()
    .replace(/[^a-z0-9]+/g, "-")
    .replace(/^-+|-+$/g, "")
    .slice(0, 64);
}
