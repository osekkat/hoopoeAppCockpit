import { describe, expect, test } from "bun:test";
import { existsSync, mkdtempSync } from "node:fs";
import { tmpdir } from "node:os";
import { join, resolve } from "node:path";
import { spawnSync } from "node:child_process";

const repoRoot = resolve(import.meta.dir, "../..");
const collector = join(repoRoot, "scripts/research-spike/collect-evidence-pack.sh");
const verifier = join(repoRoot, "scripts/research-spike/verify-evidence-pack.sh");
const mockCorpus = join(repoRoot, "packages/fixtures/scenarios/healthy-hour");

describe("hp-vtwm evidence pack scripts", () => {
  test("collector builds a mock-driven evidence pack that verifier accepts", () => {
    const outputDir = mkdtempSync(join(tmpdir(), "hoopoe-evidence-pack-test-"));

    const collect = spawnSync(
      "bash",
      [
        collector,
        "--mock-corpus-dir",
        mockCorpus,
        "--output-dir",
        outputDir,
        "--project-dir",
        repoRoot,
        "--vps-id",
        "mock-vps",
        "--wizard-command",
        "printf 'mock 13-step wizard transcript for healthy-hour\\n'",
      ],
      {
        cwd: repoRoot,
        encoding: "utf8",
        env: {
          ...process.env,
          HOOPOE_COLLECT_VERSION_TIMEOUT_S: "1",
        },
      },
    );

    expect(collect.status).toBe(0);
    const tarPath = evidenceTarPath(collect.stdout);
    expect(tarPath).not.toBeNull();
    expect(existsSync(tarPath!)).toBe(true);

    const verify = spawnSync("bash", [verifier, tarPath!], {
      cwd: repoRoot,
      encoding: "utf8",
    });

    expect(verify.status).toBe(0);
    expect(verify.stdout).toContain("PASS hp-vtwm evidence pack verifier");
    expect(verify.stdout).toContain("[PASS] secret scan clean");
  }, 30_000);
});

function evidenceTarPath(stdout: string): string | null {
  for (const line of stdout.split("\n")) {
    if (line.startsWith("EVIDENCE_TAR=")) {
      return line.slice("EVIDENCE_TAR=".length).trim();
    }
  }
  return null;
}
