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
import { formatResult, validateCorpus } from "../src/validate.ts";
import {
  ADAPTER_SLUGS,
  GOLDEN_OUTPUT_STATES,
  TENDING_SCENARIOS,
  PHASE0_SCENARIOS,
} from "../src/kinds.ts";

const result = validateCorpus();

describe("fixture corpus quality (hp-pl5o)", () => {
  test("validator reaches ok=true with no error-severity findings", () => {
    if (!result.ok) {
      // Print the report so CI logs surface the offending entries.
      // eslint-disable-next-line no-console
      console.error(formatResult(result));
    }
    expect(result.ok).toBe(true);
  });

  test("at least 5 §8.8 tending scenarios are populated", () => {
    // hp-wle ships healthy-hour, idle-but-not-stuck, wedged-pane,
    // rate-limited-no-caam, rate-limited-with-caam. The remaining 7 §8.8
    // scenarios may be stubbed by directory only and will be filled in
    // follow-up beads.
    expect(result.summary.tendingScenariosFound).toBeGreaterThanOrEqual(5);
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
