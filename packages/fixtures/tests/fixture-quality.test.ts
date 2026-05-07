// `@hoopoe/fixtures` — fixture-quality test suite (hp-pl5o).
//
// Asserts every Phase 0 fixture is correct, comprehensive, and free of
// secrets. The corpus is the foundation downstream Mock Flywheel + adapter
// contract tests build on; if the fixtures rot, every later test sits on
// rotten ground.
//
// What this enforces:
//   1. completeness — every required scenario / golden-output exists
//   2. parseability — every JSON parses; events.jsonl is valid NDJSON
//   3. shape — meta.json + expected-outcome.json + golden-output meta
//   4. failure-path coverage — every adapter has all 6 states with the
//      right exit semantics (missing-tool=127, timeout=124, high-volume
//      truncated=true)
//   5. no provider secrets (Guardrail 11)
//
// Run: bun test packages/fixtures/tests/fixture-quality.test.ts
//
// Cross-references:
// - bead hp-pl5o
// - packages/fixtures/src/validate.ts
// - plan.md §18.3 adapter contract tests
// - plan.md §16 Phase 0 acceptance

import { describe, expect, test } from "bun:test";
import { existsSync, readFileSync } from "node:fs";
import { join } from "node:path";
import { formatResult, validateCorpus } from "../src/validate.ts";
import {
  ADAPTER_SLUGS,
  GOLDEN_OUTPUT_STATES,
  TENDING_SCENARIOS,
  PHASE0_SCENARIOS,
} from "../src/kinds.ts";
import { fixturesRoot } from "../src/loader.ts";

const result = validateCorpus();
const FIXTURES_ROOT = fixturesRoot();

describe("fixture corpus quality (hp-pl5o)", () => {
  test("validator reaches ok=true with no error-severity findings", () => {
    if (!result.ok) {
      // Print the report so CI logs surface the offending entries.
      // eslint-disable-next-line no-console
      console.error(formatResult(result));
    }
    expect(result.ok).toBe(true);
  });

  test("all 12 §8.8 tending scenarios are populated", () => {
    expect(result.summary.tendingScenariosFound).toBe(TENDING_SCENARIOS.length);
    expect(result.summary.tendingScenariosExpected).toBe(TENDING_SCENARIOS.length);
    expect(result.findings.filter((f) => f.rule === "scenario.stub")).toEqual([]);
  });

  test("all 18 adapters × 6 golden-output states present (108 fixtures)", () => {
    const expected = ADAPTER_SLUGS.length * GOLDEN_OUTPUT_STATES.length;
    expect(result.summary.goldenOutputsFound).toBe(expected);
    expect(result.summary.goldenOutputsExpected).toBe(expected);
  });

  test("Phase 0 real-VPS scenario directories exist (placeholders OK)", () => {
    expect(result.summary.phase0ScenariosFound).toBe(PHASE0_SCENARIOS.length);
  });

  test("at least one JSON parsed per scenario file × scenarios + golden outputs", () => {
    // Conservative: 9 JSON files per fully-populated scenario × 5, plus
    // 102 golden outputs = ~147 JSON parses minimum.
    expect(result.summary.jsonFilesParsed).toBeGreaterThanOrEqual(120);
  });

  test("no error findings about forbidden provider-secret patterns (Guardrail 11)", () => {
    const secretFindings = result.findings.filter((f) => f.rule === "secret.found");
    if (secretFindings.length > 0) {
      // eslint-disable-next-line no-console
      console.error(secretFindings);
    }
    expect(secretFindings.length).toBe(0);
  });

  test("missing-tool golden outputs exit 127", () => {
    const wrong = result.findings.filter((f) => f.rule === "golden.missing-tool-exit");
    expect(wrong).toEqual([]);
  });

  test("timeout golden outputs exit 124", () => {
    const wrong = result.findings.filter((f) => f.rule === "golden.timeout-exit");
    expect(wrong).toEqual([]);
  });

  test("high-volume golden outputs are truncated:true", () => {
    const wrong = result.findings.filter((f) => f.rule === "golden.high-volume-truncated");
    expect(wrong).toEqual([]);
  });

  test("expected-outcome contracts are coherent (no wake-without-plan)", () => {
    const wrong = result.findings.filter((f) => f.rule === "outcome.wake-without-plan");
    expect(wrong).toEqual([]);
  });

  test("every populated scenario has the required directory siblings (pane-logs/, build-logs/)", () => {
    const wrong = result.findings.filter((f) => f.rule === "scenario.missing-dir");
    expect(wrong).toEqual([]);
  });

  test("every populated scenario events.jsonl line has channel/seq/ts/type", () => {
    const wrong = result.findings.filter((f) => f.rule === "scenario.bad-event");
    expect(wrong).toEqual([]);
  });

  test("all 12 §8.8 scenarios appear in the kinds taxonomy (typo guard)", () => {
    expect(TENDING_SCENARIOS.length).toBe(12);
    for (const id of TENDING_SCENARIOS) {
      expect(typeof id).toBe("string");
    }
  });

  test("all 18 adapter slugs appear in the kinds taxonomy", () => {
    expect(ADAPTER_SLUGS.length).toBe(18);
  });
});

// hp-k3u: every §8.8 scenario must carry a Git state, health snapshot,
// and build-queue state fixture. Without these the §8 tending decisions
// (stale-commit push, budget gating, code-health-driven review flips,
// build-queue contention warnings) can pass replay without exercising
// the canonical inputs plan.md §8.8 requires.
describe("§8.8 canonical-state snapshot fixtures (hp-k3u)", () => {
  const REQUIRED_SNAPSHOT_FILES = [
    "git-state.json",
    "health-snapshot.json",
    "build-queue-state.json",
  ] as const;

  for (const scenarioId of TENDING_SCENARIOS) {
    test(`${scenarioId}: has git/health/build-queue snapshots`, () => {
      for (const filename of REQUIRED_SNAPSHOT_FILES) {
        const path = join(FIXTURES_ROOT, "scenarios", scenarioId, filename);
        expect(existsSync(path)).toBe(true);
        const raw = readFileSync(path, "utf8");
        const parsed = JSON.parse(raw) as { meta?: { scenario?: string; kind?: string } };
        expect(parsed.meta?.scenario).toBe(scenarioId);
        expect(parsed.meta?.kind).toMatch(/^(synthetic|realistic|stub)$/);
      }
    });
  }

  test("git-state.json shape: every scenario reports head + branch + ahead/behind/dirty", () => {
    for (const scenarioId of TENDING_SCENARIOS) {
      const path = join(FIXTURES_ROOT, "scenarios", scenarioId, "git-state.json");
      const parsed = JSON.parse(readFileSync(path, "utf8")) as Record<string, unknown>;
      expect(typeof parsed.head).toBe("string");
      expect((parsed.head as string).length).toBeGreaterThanOrEqual(7);
      expect(typeof parsed.branch).toBe("string");
      expect(typeof parsed.ahead).toBe("number");
      expect(typeof parsed.behind).toBe("number");
      expect(typeof parsed.dirty).toBe("boolean");
      expect(Array.isArray(parsed.uncommittedFiles)).toBe(true);
      expect(Array.isArray(parsed.stalePushes)).toBe(true);
    }
  });

  test("health-snapshot.json shape: verdict ∈ healthy|warning|critical|unknown", () => {
    const validVerdicts = new Set(["healthy", "warning", "critical", "unknown"]);
    for (const scenarioId of TENDING_SCENARIOS) {
      const path = join(FIXTURES_ROOT, "scenarios", scenarioId, "health-snapshot.json");
      const parsed = JSON.parse(readFileSync(path, "utf8")) as Record<string, unknown>;
      expect(validVerdicts.has(parsed.verdict as string)).toBe(true);
      expect(parsed.coveragePercent === null || typeof parsed.coveragePercent === "number").toBe(true);
      expect(parsed.avgComplexity === null || typeof parsed.avgComplexity === "number").toBe(true);
      expect(typeof parsed.hotspotCount).toBe("number");
      expect(Array.isArray(parsed.perLanguage)).toBe(true);
    }
  });

  test("build-queue-state.json shape: queueDepth + running[] + queued[] arrays", () => {
    for (const scenarioId of TENDING_SCENARIOS) {
      const path = join(FIXTURES_ROOT, "scenarios", scenarioId, "build-queue-state.json");
      const parsed = JSON.parse(readFileSync(path, "utf8")) as Record<string, unknown>;
      expect(typeof parsed.queueDepth).toBe("number");
      expect(Array.isArray(parsed.running)).toBe(true);
      expect(Array.isArray(parsed.queued)).toBe(true);
    }
  });

  test("scenario-flavor: budget-breach has running jobs (drives swarm.halt)", () => {
    const path = join(FIXTURES_ROOT, "scenarios", "budget-breach", "build-queue-state.json");
    const parsed = JSON.parse(readFileSync(path, "utf8")) as { running: unknown[] };
    expect(parsed.running.length).toBeGreaterThan(0);
  });

  test("scenario-flavor: commit-burst has queued runs (drives test-run collapse)", () => {
    const path = join(FIXTURES_ROOT, "scenarios", "commit-burst", "build-queue-state.json");
    const parsed = JSON.parse(readFileSync(path, "utf8")) as { queued: unknown[] };
    expect(parsed.queued.length).toBeGreaterThan(0);
  });

  test("scenario-flavor: wedged-pane has a long-elapsed running job (drives kill_wedged_process)", () => {
    const path = join(FIXTURES_ROOT, "scenarios", "wedged-pane", "build-queue-state.json");
    const parsed = JSON.parse(readFileSync(path, "utf8")) as {
      running: { elapsedMinutes: number }[];
    };
    expect(parsed.running.length).toBe(1);
    expect(parsed.running[0]!.elapsedMinutes).toBeGreaterThanOrEqual(30);
  });

  test("scenario-flavor: missing-tool reports verdict=unknown (snapshot can't run)", () => {
    const path = join(FIXTURES_ROOT, "scenarios", "missing-tool", "health-snapshot.json");
    const parsed = JSON.parse(readFileSync(path, "utf8")) as { verdict: string };
    expect(parsed.verdict).toBe("unknown");
  });

  test("scenario-flavor: commit-burst has 15 unpushed commits (drives stale-commit push)", () => {
    const path = join(FIXTURES_ROOT, "scenarios", "commit-burst", "git-state.json");
    const parsed = JSON.parse(readFileSync(path, "utf8")) as { ahead: number };
    expect(parsed.ahead).toBe(15);
  });

  test("scenario-flavor: postcondition-failure has dirty working tree (rollback in flight)", () => {
    const path = join(FIXTURES_ROOT, "scenarios", "postcondition-failure", "git-state.json");
    const parsed = JSON.parse(readFileSync(path, "utf8")) as {
      dirty: boolean;
      uncommittedFiles: string[];
    };
    expect(parsed.dirty).toBe(true);
    expect(parsed.uncommittedFiles.length).toBeGreaterThan(0);
  });
});
