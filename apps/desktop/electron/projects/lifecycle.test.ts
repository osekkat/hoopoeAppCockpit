import { afterEach, describe, expect, test } from "bun:test";
import { execFileSync } from "node:child_process";
import { existsSync, mkdirSync, mkdtempSync, readdirSync, readFileSync, rmSync, statSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { dirname, extname, join, resolve } from "node:path";
import {
  ProjectLifecycleError,
  detectToolEnvironment,
  initializeBeadsIfMissing,
  initializeHoopoeDir,
  readGitRepoInfo,
  readProjectJson,
  type CommandRunner,
} from "./index.ts";

interface SandboxFixture {
  rootPath: string;
  cleanup: () => void;
}

function createGitRepo(opts: { withOrigin?: boolean; withBranch?: boolean } = {}): SandboxFixture {
  const rootPath = mkdtempSync(join(tmpdir(), "hoopoe-hp-ilt-"));
  execFileSync("git", ["init", "-b", "main"], { cwd: rootPath, stdio: "ignore" });
  // git init -b main may not work on older gits; ensure the branch exists.
  // Make a commit so the branch is materialized.
  execFileSync("git", ["config", "user.email", "test@hoopoe"], { cwd: rootPath, stdio: "ignore" });
  execFileSync("git", ["config", "user.name", "hoopoe-hp-ilt"], { cwd: rootPath, stdio: "ignore" });
  if (opts.withBranch !== false) {
    writeFileSync(join(rootPath, "README.md"), "# fixture\n", "utf8");
    execFileSync("git", ["add", "README.md"], { cwd: rootPath, stdio: "ignore" });
    execFileSync("git", ["commit", "-m", "init"], { cwd: rootPath, stdio: "ignore" });
  }
  if (opts.withOrigin !== false) {
    execFileSync("git", ["remote", "add", "origin", "https://example.invalid/hp-ilt.git"], {
      cwd: rootPath,
      stdio: "ignore",
    });
  }
  return {
    rootPath,
    cleanup: () => {
      try {
        rmSync(rootPath, { recursive: true, force: true });
      } catch {
        // best effort
      }
    },
  };
}

const sandboxes: SandboxFixture[] = [];
function track(fixture: SandboxFixture): SandboxFixture {
  sandboxes.push(fixture);
  return fixture;
}

afterEach(() => {
  while (sandboxes.length > 0) {
    sandboxes.pop()?.cleanup();
  }
});

describe("hp-ilt :: detectToolEnvironment", () => {
  test("finds AGENTS.md, README.md, package.json at root", () => {
    const f = track(createGitRepo());
    writeFileSync(join(f.rootPath, "AGENTS.md"), "# agents\n", "utf8");
    writeFileSync(join(f.rootPath, "package.json"), "{}", "utf8");
    const env = detectToolEnvironment(f.rootPath);
    expect(env.agentsMdRelative).toBe("AGENTS.md");
    expect(env.readmeRelative).toBe("README.md");
    expect(env.manifests.map((m) => m.name)).toContain("package.json");
    expect(env.hasBeadsDir).toBe(false);
    expect(env.hasHoopoeDir).toBe(false);
  });

  test("returns null for missing AGENTS.md and empty manifests", () => {
    const f = track(createGitRepo());
    rmSync(join(f.rootPath, "README.md"), { force: true });
    const env = detectToolEnvironment(f.rootPath);
    expect(env.agentsMdRelative).toBeNull();
    expect(env.readmeRelative).toBeNull();
    expect(env.manifests).toEqual([]);
  });

  test("throws on missing or non-directory path", () => {
    expect(() => detectToolEnvironment("/no/such/dir")).toThrow(ProjectLifecycleError);
  });
});

describe("hp-ilt :: readGitRepoInfo", () => {
  test("captures isGitRepo, originRemote, branch on a real git repo", () => {
    const f = track(createGitRepo());
    const info = readGitRepoInfo(f.rootPath);
    expect(info.isGitRepo).toBe(true);
    expect(info.originRemote).toBe("https://example.invalid/hp-ilt.git");
    expect(info.branch).toBe("main");
  });

  test("originRemote=null when no origin configured", () => {
    const f = track(createGitRepo({ withOrigin: false }));
    const info = readGitRepoInfo(f.rootPath);
    expect(info.isGitRepo).toBe(true);
    expect(info.originRemote).toBeNull();
    expect(info.branch).toBe("main");
  });

  test("isGitRepo=false on a non-git directory", () => {
    const dir = mkdtempSync(join(tmpdir(), "hoopoe-hp-ilt-nonrepo-"));
    track({ rootPath: dir, cleanup: () => rmSync(dir, { recursive: true, force: true }) });
    const info = readGitRepoInfo(dir);
    expect(info.isGitRepo).toBe(false);
    expect(info.originRemote).toBeNull();
    expect(info.branch).toBeNull();
  });
});

describe("hp-ilt :: initializeHoopoeDir", () => {
  test("creates .hoopoe/, project.json, plans/, skills.lock.json, model-context-policy.json", () => {
    const f = track(createGitRepo());
    const result = initializeHoopoeDir(f.rootPath, { now: () => new Date("2026-05-04T00:00:00Z") });
    expect(result.created).toBe(true);
    expect(existsSync(result.hoopoeDir)).toBe(true);
    expect(existsSync(join(result.hoopoeDir, "plans"))).toBe(true);
    expect(existsSync(join(result.hoopoeDir, "skills.lock.json"))).toBe(true);
    expect(existsSync(join(result.hoopoeDir, "model-context-policy.json"))).toBe(true);
    expect(statSync(result.projectJsonPath).isFile()).toBe(true);

    const parsed = readProjectJson(f.rootPath);
    expect(parsed.schemaVersion).toBe(1);
    expect(parsed.project.id).toMatch(/^[0-9a-fA-F-]{36}$/);
    expect(parsed.project.name.length).toBeGreaterThan(0);
    expect(parsed.project.slug).toMatch(/^[a-z0-9-]+$/);
    expect(parsed.project.originRemote).toBe("https://example.invalid/hp-ilt.git");
    expect(parsed.project.branch).toBe("main");
    expect(parsed.project.state).toBe("imported");
    expect(parsed.project.tools.hasHoopoeDir).toBe(false); // captured BEFORE creating .hoopoe/
  });

  test("idempotent: re-running leaves existing .hoopoe/ untouched", () => {
    const f = track(createGitRepo());
    const first = initializeHoopoeDir(f.rootPath, { projectId: "fixed-id" });
    const originalContent = readFileSync(first.projectJsonPath, "utf8");
    const second = initializeHoopoeDir(f.rootPath, { projectId: "different-id" });
    expect(second.created).toBe(false);
    const afterContent = readFileSync(second.projectJsonPath, "utf8");
    expect(afterContent).toBe(originalContent);
  });

  test("refuses non-git directories", () => {
    const dir = mkdtempSync(join(tmpdir(), "hoopoe-hp-ilt-nonrepo-"));
    track({ rootPath: dir, cleanup: () => rmSync(dir, { recursive: true, force: true }) });
    expect(() => initializeHoopoeDir(dir)).toThrow(ProjectLifecycleError);
  });

  test("refuses repos with no origin (plan.md §1.1)", () => {
    const f = track(createGitRepo({ withOrigin: false }));
    let caught: unknown = null;
    try {
      initializeHoopoeDir(f.rootPath);
    } catch (err) {
      caught = err;
    }
    expect(caught).toBeInstanceOf(ProjectLifecycleError);
    expect((caught as ProjectLifecycleError).code).toBe("missing_origin");
  });

  test("respects override projectId + name", () => {
    const f = track(createGitRepo());
    const result = initializeHoopoeDir(f.rootPath, { projectId: "custom-id", name: "My Project" });
    expect(result.metadata.id).toBe("custom-id");
    expect(result.metadata.name).toBe("My Project");
    expect(result.metadata.slug).toBe("my-project");
  });
});

describe("hp-ilt :: initializeBeadsIfMissing", () => {
  test("returns ran=false when .beads/ already exists", () => {
    const f = track(createGitRepo());
    mkdirSync(join(f.rootPath, ".beads"));
    const result = initializeBeadsIfMissing(f.rootPath);
    expect(result.ran).toBe(false);
  });

  test("calls runCommand with `br init` when .beads/ missing", () => {
    const f = track(createGitRepo());
    let capturedCmd = "";
    let capturedArgs: readonly string[] = [];
    const runner: CommandRunner = (cmd, args) => {
      capturedCmd = cmd;
      capturedArgs = args;
      return { stdout: "br init complete", exitCode: 0 };
    };
    const result = initializeBeadsIfMissing(f.rootPath, { runCommand: runner });
    expect(result.ran).toBe(true);
    expect(capturedCmd).toBe("br");
    expect(capturedArgs).toEqual(["init"]);
    expect(result.exitCode).toBe(0);
  });
});

// hp-qk8: lock in the source-of-truth boundary documented in plan.md
// §1.1 / §5.3. The Phase 4 lifecycle helpers in this directory mutate
// repo state on disk (run `git`, write `.hoopoe/project.json`, run
// `br init`) and intentionally duplicate the canonical daemon-side
// implementation in apps/daemon/internal/projects/. To keep them from
// becoming a parallel mutation surface, the renderer (where the user
// triggers project import) must NEVER reach in directly — every
// project mutation goes through a typed daemon RPC. These tests
// regression-guard that boundary by walking every TS/TSX file under
// apps/desktop/src/ (renderer + main IPC bridge code that the renderer
// can reach via preload) and asserting no production import resolves
// into apps/desktop/electron/projects/. Doc references in comments are
// allowed; only `import` / `require` / dynamic-import statements fail.
describe("hp-qk8 :: production-bundle boundary", () => {
  const repoRoot = resolve(__dirname, "..", "..", "..", "..");
  const desktopSrc = resolve(repoRoot, "apps", "desktop", "src");
  const electronProjectsRel = "apps/desktop/electron/projects";

  function walkTsFiles(root: string): string[] {
    const out: string[] = [];
    const stack = [root];
    while (stack.length > 0) {
      const dir = stack.pop()!;
      let entries: ReturnType<typeof readdirSync>;
      try {
        entries = readdirSync(dir, { withFileTypes: true });
      } catch {
        continue;
      }
      for (const entry of entries) {
        const full = join(dir, entry.name);
        if (entry.isDirectory()) {
          if (entry.name === "node_modules" || entry.name === "dist" || entry.name === "dist-electron") continue;
          stack.push(full);
          continue;
        }
        const ext = extname(entry.name);
        if (ext !== ".ts" && ext !== ".tsx" && ext !== ".mts" && ext !== ".cts") continue;
        out.push(full);
      }
    }
    return out;
  }

  function isImportLine(line: string): boolean {
    const trimmed = line.trim();
    if (trimmed.startsWith("//") || trimmed.startsWith("*") || trimmed.startsWith("/*")) return false;
    return (
      /^\s*import\b/.test(trimmed) ||
      /^\s*export\s+\*\s+from\b/.test(trimmed) ||
      /^\s*export\s+\{[^}]*\}\s+from\b/.test(trimmed) ||
      /\brequire\(/.test(trimmed) ||
      /\bimport\s*\(/.test(trimmed)
    );
  }

  function resolveImportTarget(fromFile: string, spec: string): string | null {
    if (!spec.startsWith(".") && !spec.startsWith("/")) return null;
    const base = spec.startsWith("/") ? spec : resolve(dirname(fromFile), spec);
    return base;
  }

  test("no apps/desktop/src/** TS/TSX file imports from apps/desktop/electron/projects/", () => {
    const offenders: { file: string; line: number; spec: string }[] = [];
    for (const file of walkTsFiles(desktopSrc)) {
      const source = readFileSync(file, "utf8");
      const lines = source.split("\n");
      for (let i = 0; i < lines.length; i++) {
        const line = lines[i];
        if (!isImportLine(line)) continue;
        const match = line.match(/(?:from|require\(|import\(|require\s*\(\s*)\s*["'`]([^"'`]+)["'`]/);
        if (!match) continue;
        const spec = match[1];
        const resolved = resolveImportTarget(file, spec);
        if (resolved === null) continue;
        const electronProjectsAbs = resolve(repoRoot, electronProjectsRel);
        if (!resolved.startsWith(electronProjectsAbs)) continue;
        offenders.push({ file: file.slice(repoRoot.length + 1), line: i + 1, spec });
      }
    }
    if (offenders.length > 0) {
      const list = offenders.map((o) => `  ${o.file}:${o.line}  ${o.spec}`).join("\n");
      throw new Error(
        "hp-qk8 boundary violation: apps/desktop/src/ must request project import/readiness through the daemon RPC, not import the local lifecycle helpers directly. Offenders:\n" +
          list,
      );
    }
  });

  test("apps/desktop/electron/projects/ exists and exports the lifecycle helpers it claims to (anchor for the boundary)", () => {
    // Sanity anchor: if someone deletes/renames lifecycle.ts the boundary
    // test above could pass vacuously (no offenders because the target
    // moved). Pin the on-disk path so the boundary test stays meaningful.
    const lifecyclePath = resolve(repoRoot, electronProjectsRel, "lifecycle.ts");
    expect(existsSync(lifecyclePath)).toBe(true);
    const source = readFileSync(lifecyclePath, "utf8");
    expect(source.includes("export function initializeHoopoeDir")).toBe(true);
    expect(source.includes("export function initializeBeadsIfMissing")).toBe(true);
    expect(source.includes("export function readGitRepoInfo")).toBe(true);
  });
});
