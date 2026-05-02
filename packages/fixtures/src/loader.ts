// `@hoopoe/fixtures` — typed loader (hp-wle stub).
//
// This file is a stub. The full Mock Flywheel loader (with bun-side fs
// reads, sequence-cursor replay, capability-assertion helpers) lands in
// the cross-cutting Mock Flywheel bead `hp-dr8` and the fixture-replay
// harness bead `hp-q3t`.
//
// What we export here:
// - `fixturesRoot()` — resolves the corpus root (overridable via env).
// - `scenarioPath(...)` / `goldenOutputPath(...)` — typed path builders so
//   downstream code never hand-builds string paths and miss a typo.
// - `FixtureNotFoundError` — error class the harness throws on missing
//   files (so callers can branch on stub vs missing).
//
// Cross-references:
// - `packages/fixtures/README.md` — per-scenario + per-golden-output contracts
// - `packages/fixtures/src/kinds.ts` — fixture-kind taxonomy
// - `plan.md` §13 (Mock Flywheel Mode) and §18.3 (adapter contract tests)

import { fileURLToPath } from "node:url";
import { dirname, resolve } from "node:path";
import {
  ADAPTER_SLUGS,
  GOLDEN_OUTPUT_STATES,
  PHASE0_SCENARIOS,
  TENDING_SCENARIOS,
  type AdapterSlug,
  type GoldenOutputState,
  type Phase0ScenarioId,
  type TendingScenarioId,
} from "./kinds.ts";

/** Resolves the corpus root. Defaults to `<this-file>/../..` so the loader
 *  works from inside the package without an env override. Override with
 *  `HOOPOE_FIXTURES_ROOT` for tests that ship their own corpus copies. */
export function fixturesRoot(): string {
  const env = process.env.HOOPOE_FIXTURES_ROOT;
  if (env && env.length > 0) return resolve(env);
  const here = fileURLToPath(new URL(".", import.meta.url));
  return resolve(here, "..");
}

/** Stable corpus tag that matches `phase0-<date>` directory names. */
export const FIXTURES_VERSION = "phase0-2026-05-02";

/** Path to a §8.8 synthetic scenario directory (e.g. `scenarios/healthy-hour/`). */
export function scenarioPath(scenario: TendingScenarioId): string {
  return resolve(fixturesRoot(), "scenarios", scenario);
}

/** Path to a Phase 0 real-VPS scenario directory (e.g. `phase0-2026-05-02/scenarios/fresh/`). */
export function phase0ScenarioPath(scenario: Phase0ScenarioId, version: string = FIXTURES_VERSION): string {
  return resolve(fixturesRoot(), version, "scenarios", scenario);
}

/** Path to a per-adapter golden-output file (e.g. `golden-outputs/br/normal.json`). */
export function goldenOutputPath(adapter: AdapterSlug, state: GoldenOutputState): string {
  return resolve(fixturesRoot(), "golden-outputs", adapter, `${state}.json`);
}

/** Throw on missing fixture. Callers branch on `instanceof FixtureNotFoundError`
 *  to tell "stub not yet pinned" from "real failure". */
export class FixtureNotFoundError extends Error {
  override readonly name = "FixtureNotFoundError";
  readonly path: string;
  readonly hint: string | undefined;
  constructor(path: string, hint?: string) {
    super(`Fixture not found: ${path}${hint ? ` (${hint})` : ""}`);
    this.path = path;
    this.hint = hint;
  }
}

/** Enumerate every required scenario / golden-output combination so a
 *  completeness checker can iterate and assert presence. Used by hp-pl5o. */
export function enumerateRequiredFixtures(): Array<
  | { kind: "tending_scenario"; id: TendingScenarioId; path: string }
  | { kind: "phase0_scenario"; id: Phase0ScenarioId; path: string }
  | { kind: "golden_output"; adapter: AdapterSlug; state: GoldenOutputState; path: string }
> {
  const out: ReturnType<typeof enumerateRequiredFixtures> = [];
  for (const id of TENDING_SCENARIOS) {
    out.push({ kind: "tending_scenario", id, path: scenarioPath(id) });
  }
  for (const id of PHASE0_SCENARIOS) {
    out.push({ kind: "phase0_scenario", id, path: phase0ScenarioPath(id) });
  }
  for (const adapter of ADAPTER_SLUGS) {
    for (const state of GOLDEN_OUTPUT_STATES) {
      out.push({ kind: "golden_output", adapter, state, path: goldenOutputPath(adapter, state) });
    }
  }
  return out;
}

/** Tiny debug helper used by the smoke test in `index.test.ts`. */
export function loaderSelfDescribe(): {
  root: string;
  fixturesVersion: string;
  scenarios: number;
  phase0Scenarios: number;
  goldenOutputs: number;
} {
  return {
    root: fixturesRoot(),
    fixturesVersion: FIXTURES_VERSION,
    scenarios: TENDING_SCENARIOS.length,
    phase0Scenarios: PHASE0_SCENARIOS.length,
    goldenOutputs: ADAPTER_SLUGS.length * GOLDEN_OUTPUT_STATES.length,
  };
}
