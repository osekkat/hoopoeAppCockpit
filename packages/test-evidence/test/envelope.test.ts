import { describe, expect, test } from "bun:test";
import {
  TEST_EVIDENCE_SCHEMA_VERSION,
  buildEnvelope,
  type TestResult,
} from "../src/index.ts";

const baseResults: TestResult[] = [
  { name: "boots", file: "src/x.test.ts", status: "passed", durationMs: 12 },
  { name: "fails", file: "src/y.test.ts", status: "failed", durationMs: 5, errorMessage: "nope" },
];

describe("hp-6sv :: buildEnvelope", () => {
  test("populates required fields and applies defaults for optional ones", () => {
    const env = buildEnvelope({
      gitSha: "deadbeef",
      daemonVersion: "0.0.0",
      runner: "bun-test",
      phase: "phase2",
      results: baseResults,
    });
    expect(env.schemaVersion).toBe(TEST_EVIDENCE_SCHEMA_VERSION);
    expect(env.gitSha).toBe("deadbeef");
    expect(env.daemonVersion).toBe("0.0.0");
    expect(env.runner).toBe("bun-test");
    expect(env.phase).toBe("phase2");
    expect(env.results).toEqual(baseResults);
    expect(env.coverage).toBeNull();
    expect(env.artifacts).toEqual([]);
    expect(env.fixtureScenario).toBeNull();
    expect(env.redactionStats).toEqual({ patternsMatched: {} });
    expect(env.slo).toEqual({ targetsLoaded: 0, breached: [] });
    expect(env.runId).toMatch(/^[0-9a-fA-F-]{36}$|^runid-/);
    expect(env.ts).toMatch(/^\d{4}-\d{2}-\d{2}T/);
  });

  test("preserves explicit runId/ts overrides + dirty marker only when true", () => {
    const explicit = buildEnvelope({
      runId: "fixed-run-id",
      ts: "2026-05-04T00:00:00.000Z",
      gitSha: "abc",
      gitDirty: true,
      daemonVersion: "0.1.0",
      fixtureScenario: "fresh",
      runner: "playwright",
      phase: "phase1",
      results: [],
      slo: { targetsLoaded: 8, breached: [] },
    });
    expect(explicit.runId).toBe("fixed-run-id");
    expect(explicit.ts).toBe("2026-05-04T00:00:00.000Z");
    expect(explicit.fixtureScenario).toBe("fresh");
    expect(explicit.gitDirty).toBe(true);
    expect(explicit.slo.targetsLoaded).toBe(8);

    const clean = buildEnvelope({
      gitSha: "abc",
      gitDirty: false,
      daemonVersion: "0.0.0",
      runner: "go-test",
      phase: "phase2",
      results: [],
    });
    // gitDirty is omitted when false to keep envelopes minimal.
    expect("gitDirty" in clean).toBe(false);
  });
});
