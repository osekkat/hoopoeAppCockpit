import { afterEach, beforeAll, describe, expect, test } from "bun:test";
import { resolve } from "node:path";
import {
  clearTargets,
  expectSlo,
  expectSloBoolean,
  expectSloPass,
  loadSloTargets,
  SloAssertionError,
  SloTargetsError,
  useTargets,
  getTarget,
  listTargets,
  listTargetsByPhase,
} from "../src/index.ts";

const REPO_ROOT = resolve(__dirname, "..", "..", "..");

beforeAll(() => {
  useTargets(loadSloTargets({ repoRoot: REPO_ROOT }));
});

afterEach(() => {
  // Re-load between tests so any clearTargets() doesn't bleed.
  useTargets(loadSloTargets({ repoRoot: REPO_ROOT }));
});

describe("hp-5ja :: assertions", () => {
  test("expectSloPass passes for samples whose p95 is within the budget", () => {
    // desktop.reconnect.p95 ≤ 10000ms. 50 samples in [1000, 5000] ms.
    const samples = Array.from({ length: 50 }, (_, i) => 1_000 + (i * 80));
    const result = expectSloPass("desktop.reconnect.p95", samples);
    expect(result.passed).toBe(true);
    expect(result.observed).toBeLessThanOrEqual(10_000);
    expect(result.target.id).toBe("desktop.reconnect.p95");
  });

  test("expectSloPass throws for samples whose p95 exceeds the budget", () => {
    // 50 samples in [11_000, 15_000] ms — p95 > 10s budget.
    const samples = Array.from({ length: 50 }, (_, i) => 11_000 + i * 80);
    let caught: unknown = null;
    try {
      expectSloPass("desktop.reconnect.p95", samples);
    } catch (err) {
      caught = err;
    }
    expect(caught).toBeInstanceOf(SloAssertionError);
    if (caught instanceof SloAssertionError) {
      expect(caught.targetId).toBe("desktop.reconnect.p95");
      expect(caught.observed).toBeGreaterThan(10_000);
    }
  });

  test("expectSlo enforces a minimum sample count", () => {
    expect(() =>
      expectSlo("desktop.reconnect.p95", [100, 200], { samples: 10 }),
    ).toThrow(SloAssertionError);
  });

  test("expectSloBoolean handles boolean targets", () => {
    expect(() => expectSloBoolean("dag.usable.500-nodes", true)).not.toThrow();
    expect(() => expectSloBoolean("dag.usable.500-nodes", false)).toThrow(SloAssertionError);
  });

  test("expectSloBoolean refuses percentile targets", () => {
    expect(() => expectSloBoolean("desktop.reconnect.p95", true)).toThrow(SloAssertionError);
  });

  test("expectSloPass refuses boolean targets", () => {
    expect(() => expectSloPass("dag.usable.500-nodes", [1, 2, 3])).toThrow(SloAssertionError);
  });

  test("getTarget throws on unknown id with a helpful message", () => {
    expect(() => getTarget("does.not.exist")).toThrow(SloTargetsError);
  });

  test("listTargets returns the loaded set; listTargetsByPhase filters", () => {
    const all = listTargets();
    expect(all.length).toBeGreaterThan(0);
    const phase2 = listTargetsByPhase("phase2.e2e");
    expect(phase2.some((t) => t.id === "desktop.reconnect.p95")).toBe(true);
    const chaos = listTargetsByPhase("chaos");
    expect(chaos.some((t) => t.id === "job.cancellation.no-orphans")).toBe(true);
    const unknown = listTargetsByPhase("phase999");
    expect(unknown).toEqual([]);
  });

  test("clearTargets isolates registry state between tests", () => {
    clearTargets();
    // After clear, ensureRegistry re-loads — pass repoRoot so the
    // re-load doesn't fall back to a cwd that lacks slo-targets.yaml.
    expect(() => listTargetsByPhase("phase2.e2e", { repoRoot: REPO_ROOT })).not.toThrow();
  });
});
