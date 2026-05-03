import { describe, expect, test } from "bun:test";
import { mkdtempSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join, resolve } from "node:path";
import {
  evaluateAgainst,
  loadSloTargets,
  SloTargetsError,
} from "../src/index.ts";

const REPO_ROOT = resolve(__dirname, "..", "..", "..");

describe("hp-6sv :: slo-targets loader", () => {
  test("loads packages/slo-targets.yaml from the repo root", () => {
    const targets = loadSloTargets({ repoRoot: REPO_ROOT });
    expect(targets.schemaVersion).toBe(1);
    const ids = Object.keys(targets.targets);
    expect(ids).toContain("desktop.reconnect.p95");
    expect(ids).toContain("dag.usable.500-nodes");
    expect(ids).toContain("job.cancellation.no-orphans");
  });

  test("parses latency_p95 with ms suffix into ms threshold", () => {
    const targets = loadSloTargets({ repoRoot: REPO_ROOT });
    const reconnect = targets.targets["desktop.reconnect.p95"];
    expect(reconnect).toBeDefined();
    expect(reconnect?.kind).toBe("latency_p95");
    expect(reconnect?.threshold).toBe(10_000);
    expect(reconnect?.declared).toBe("10000ms");
  });

  test("parses boolean targets to 1/0", () => {
    const targets = loadSloTargets({ repoRoot: REPO_ROOT });
    const dag = targets.targets["dag.usable.500-nodes"];
    expect(dag?.kind).toBe("boolean");
    expect(dag?.threshold).toBe(1);
  });

  test("evaluateAgainst behaves correctly for latency vs boolean", () => {
    const targets = loadSloTargets({ repoRoot: REPO_ROOT });
    const reconnect = targets.targets["desktop.reconnect.p95"];
    const dag = targets.targets["dag.usable.500-nodes"];
    expect(reconnect && evaluateAgainst(reconnect, 9_999)).toBe(true);
    expect(reconnect && evaluateAgainst(reconnect, 10_001)).toBe(false);
    expect(dag && evaluateAgainst(dag, 1)).toBe(true);
    expect(dag && evaluateAgainst(dag, 0)).toBe(false);
  });

  test("throws SloTargetsError on schemaVersion mismatch", () => {
    const dir = mkdtempSync(join(tmpdir(), "hoopoe-slo-"));
    const path = join(dir, "slo.yaml");
    writeFileSync(path, "schemaVersion: 99\ntargets:\n", "utf8");
    try {
      loadSloTargets({ path });
      throw new Error("expected throw");
    } catch (err) {
      expect(err).toBeInstanceOf(SloTargetsError);
      expect((err as Error).message).toContain("schemaVersion");
    }
  });

  test("rejects unknown kind", () => {
    const dir = mkdtempSync(join(tmpdir(), "hoopoe-slo-"));
    const path = join(dir, "slo.yaml");
    writeFileSync(
      path,
      "schemaVersion: 1\ntargets:\n  bad:\n    kind: nonsense\n    declared: 10ms\n    description: x\n",
      "utf8",
    );
    expect(() => loadSloTargets({ path })).toThrow(SloTargetsError);
  });

  test("rejects malformed declared latency", () => {
    const dir = mkdtempSync(join(tmpdir(), "hoopoe-slo-"));
    const path = join(dir, "slo.yaml");
    writeFileSync(
      path,
      "schemaVersion: 1\ntargets:\n  bad:\n    kind: latency_p95\n    declared: 'soon'\n    description: x\n",
      "utf8",
    );
    expect(() => loadSloTargets({ path })).toThrow(SloTargetsError);
  });
});
