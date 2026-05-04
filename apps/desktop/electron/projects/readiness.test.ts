import { afterEach, describe, expect, test } from "bun:test";
import { execFileSync } from "node:child_process";
import { mkdirSync, mkdtempSync, rmSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import {
  checkProjectImportedGate,
  initializeHoopoeDir,
  isProjectImported,
} from "./index.ts";

interface SandboxFixture {
  rootPath: string;
  cleanup: () => void;
}

function makeRepo(opts: { withOrigin?: boolean; withAgents?: boolean; withManifest?: boolean } = {}): SandboxFixture {
  const rootPath = mkdtempSync(join(tmpdir(), "hoopoe-hp-ilt-readiness-"));
  execFileSync("git", ["init", "-b", "main"], { cwd: rootPath, stdio: "ignore" });
  execFileSync("git", ["config", "user.email", "test@hoopoe"], { cwd: rootPath, stdio: "ignore" });
  execFileSync("git", ["config", "user.name", "hoopoe-hp-ilt"], { cwd: rootPath, stdio: "ignore" });
  writeFileSync(join(rootPath, "README.md"), "# x\n", "utf8");
  execFileSync("git", ["add", "README.md"], { cwd: rootPath, stdio: "ignore" });
  execFileSync("git", ["commit", "-m", "init"], { cwd: rootPath, stdio: "ignore" });
  if (opts.withOrigin !== false) {
    execFileSync("git", ["remote", "add", "origin", "https://example.invalid/x.git"], {
      cwd: rootPath,
      stdio: "ignore",
    });
  }
  if (opts.withAgents !== false) {
    writeFileSync(join(rootPath, "AGENTS.md"), "# agents\n", "utf8");
  }
  if (opts.withManifest !== false) {
    writeFileSync(join(rootPath, "package.json"), "{}", "utf8");
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
function track(f: SandboxFixture): SandboxFixture {
  sandboxes.push(f);
  return f;
}
afterEach(() => {
  while (sandboxes.length > 0) sandboxes.pop()?.cleanup();
});

describe("hp-ilt :: checkProjectImportedGate", () => {
  test("fully-set-up project satisfies every requirement", () => {
    const f = track(makeRepo());
    initializeHoopoeDir(f.rootPath);
    const report = checkProjectImportedGate(f.rootPath);
    expect(report.gate).toBe("imported");
    expect(report.satisfied).toBe(true);
    for (const req of report.requirements) {
      expect(req.satisfied).toBe(true);
    }
  });

  test("missing AGENTS.md flips agents.md unsatisfied", () => {
    const f = track(makeRepo({ withAgents: false }));
    initializeHoopoeDir(f.rootPath);
    const report = checkProjectImportedGate(f.rootPath);
    expect(report.satisfied).toBe(false);
    const agentsReq = report.requirements.find((r) => r.id === "agents.md");
    expect(agentsReq?.satisfied).toBe(false);
    expect(agentsReq?.note).toContain("AGENTS.md");
  });

  test("missing .hoopoe/ flips hoopoe.dir unsatisfied", () => {
    const f = track(makeRepo());
    const report = checkProjectImportedGate(f.rootPath);
    const hoopoeReq = report.requirements.find((r) => r.id === "hoopoe.dir");
    expect(hoopoeReq?.satisfied).toBe(false);
    expect(report.satisfied).toBe(false);
  });

  test("missing language manifest flips tools.detected unsatisfied (default mode)", () => {
    const f = track(makeRepo({ withManifest: false }));
    initializeHoopoeDir(f.rootPath);
    const report = checkProjectImportedGate(f.rootPath);
    const toolsReq = report.requirements.find((r) => r.id === "tools.detected");
    expect(toolsReq?.satisfied).toBe(false);
    expect(report.satisfied).toBe(false);
  });

  test("allowNoLanguageManifest=true accepts manifest-less repo when AGENTS.md present", () => {
    const f = track(makeRepo({ withManifest: false }));
    initializeHoopoeDir(f.rootPath);
    const report = checkProjectImportedGate(f.rootPath, { allowNoLanguageManifest: true });
    const toolsReq = report.requirements.find((r) => r.id === "tools.detected");
    expect(toolsReq?.satisfied).toBe(true);
    expect(toolsReq?.note).toContain("allowNoLanguageManifest");
  });

  test("missing origin flips git.origin unsatisfied with the §1.1 hint", () => {
    const f = track(makeRepo({ withOrigin: false }));
    initializeHoopoeDir; // no-op; can't init without origin per the lifecycle rule
    const report = checkProjectImportedGate(f.rootPath);
    const originReq = report.requirements.find((r) => r.id === "git.origin");
    expect(originReq?.satisfied).toBe(false);
    expect(originReq?.note).toContain("§1.1");
  });

  test("non-git directory: every git requirement fails", () => {
    const dir = mkdtempSync(join(tmpdir(), "hoopoe-hp-ilt-nonrepo-"));
    track({ rootPath: dir, cleanup: () => rmSync(dir, { recursive: true, force: true }) });
    mkdirSync(dir, { recursive: true }); // already exists; idempotent
    const report = checkProjectImportedGate(dir);
    expect(report.satisfied).toBe(false);
    expect(report.requirements.find((r) => r.id === "git.present")?.satisfied).toBe(false);
    expect(report.requirements.find((r) => r.id === "git.origin")?.satisfied).toBe(false);
    expect(report.requirements.find((r) => r.id === "git.branch")?.satisfied).toBe(false);
  });

  test("isProjectImported short-circuits to bool", () => {
    const ok = track(makeRepo());
    initializeHoopoeDir(ok.rootPath);
    expect(isProjectImported(ok.rootPath)).toBe(true);

    const bad = track(makeRepo({ withAgents: false }));
    initializeHoopoeDir(bad.rootPath);
    expect(isProjectImported(bad.rootPath)).toBe(false);
  });
});
