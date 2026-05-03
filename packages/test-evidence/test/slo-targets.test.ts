// hp-5ja: this package now delegates to `@hoopoe/slo`. The wrapper here
// preserves the indexed-by-id `targets[id]` shape + `evaluateAgainst`
// helper that the test-evidence reporter and the run-bun wrapper use
// for per-test lookups. Lower-level loader / percentile / assertion
// behavior is exercised in the @hoopoe/slo test suite.

import { describe, expect, test } from "bun:test";
import { resolve } from "node:path";
import {
  evaluateAgainst,
  loadSloTargets,
  SloTargetsError,
} from "../src/index.ts";

const REPO_ROOT = resolve(__dirname, "..", "..", "..");

describe("hp-6sv :: slo-targets adapter (delegates to @hoopoe/slo)", () => {
  test("loadSloTargets exposes an id-indexed lookup", () => {
    const targets = loadSloTargets({ repoRoot: REPO_ROOT });
    expect(targets.schemaVersion).toBe(1);
    const ids = Object.keys(targets.targets);
    expect(ids).toContain("desktop.reconnect.p95");
    expect(ids).toContain("dag.usable.500-nodes");
    expect(ids).toContain("job.cancellation.no-orphans");
    expect(targets.sourcePath.endsWith("slo-targets.yaml")).toBe(true);
  });

  test("evaluateAgainst handles latency_p95 percentile targets", () => {
    const targets = loadSloTargets({ repoRoot: REPO_ROOT });
    const reconnect = targets.targets["desktop.reconnect.p95"];
    expect(reconnect).toBeDefined();
    expect(evaluateAgainst(reconnect!, 9_999)).toBe(true);
    expect(evaluateAgainst(reconnect!, 10_001)).toBe(false);
  });

  test("evaluateAgainst handles boolean targets", () => {
    const targets = loadSloTargets({ repoRoot: REPO_ROOT });
    const dag = targets.targets["dag.usable.500-nodes"];
    expect(dag).toBeDefined();
    expect(evaluateAgainst(dag!, 1)).toBe(true);
    expect(evaluateAgainst(dag!, 0)).toBe(false);
  });

  test("loader propagates SloTargetsError on shape mismatch", () => {
    expect(() => loadSloTargets({ path: "/no/such/slo-targets.yaml" })).toThrow(
      SloTargetsError,
    );
  });
});
