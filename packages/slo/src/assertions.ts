// `@hoopoe/slo` — assertion API (hp-5ja).
//
// Three helpers, mirroring the bead's spec:
//
//   expectSloPass(id, samples)            — computes the declared percentile
//                                           and asserts vs target
//   expectSlo(id, samples, {samples})     — same plus minimum sample-count gate
//   expectSloBoolean(id, actual)          — boolean target check
//
// Each helper returns a structured `SloAssertionResult` so test runners
// can record a non-throwing record (used by the test-evidence emitter)
// AND throws on failure so plain `bun:test` consumers see a normal
// assertion failure.

import { percentile } from "./percentile.ts";
import { getTarget } from "./registry.ts";
import type { LoadSloTargetsOptions } from "./loader.ts";
import type { PercentileTarget, SloTarget } from "./types.ts";

export class SloAssertionError extends Error {
  override readonly name = "SloAssertionError";
  readonly targetId: string;
  readonly observed: number;
  readonly threshold: number;
  constructor(message: string, targetId: string, observed: number, threshold: number) {
    super(message);
    this.targetId = targetId;
    this.observed = observed;
    this.threshold = threshold;
  }
}

export interface SloAssertionResult {
  target: SloTarget;
  observed: number;
  threshold: number;
  passed: boolean;
  /** Sample count used for the assertion (1 for boolean checks). */
  sampleCount: number;
  /** Notes carried through to the test-evidence envelope. */
  notes?: string;
}

function ensurePercentile(target: SloTarget): PercentileTarget {
  if (target.target.kind !== "percentile") {
    throw new SloAssertionError(
      `target '${target.id}' is a boolean target — call expectSloBoolean instead`,
      target.id,
      0,
      0,
    );
  }
  return target.target;
}

function compareDirection(observed: number, threshold: number, direction: "max" | "min"): boolean {
  if (direction === "max") return observed <= threshold;
  return observed >= threshold;
}

export interface ExpectSloOptions extends LoadSloTargetsOptions {
  /** Minimum sample count required for the assertion to be valid.
   *  Default: 1 (any non-empty sample list). */
  samples?: number;
}

/** Assert observed samples satisfy the declared percentile target.
 *  Returns a structured result on success; throws SloAssertionError on failure. */
export function expectSlo(
  id: string,
  samples: readonly number[],
  options: ExpectSloOptions = {},
): SloAssertionResult {
  const minSamples = options.samples ?? 1;
  const { samples: _ignored, ...loadOptions } = options;
  const target = getTarget(id, loadOptions);
  const percentileTarget = ensurePercentile(target);
  if (samples.length < minSamples) {
    throw new SloAssertionError(
      `target '${id}' requires at least ${minSamples} sample(s); got ${samples.length}`,
      id,
      0,
      percentileTarget.numeric,
    );
  }
  const observed = percentile(samples, percentileTarget.percentile);
  const passed = compareDirection(observed, percentileTarget.numeric, percentileTarget.direction);
  if (!passed) {
    throw new SloAssertionError(
      `target '${id}' breached: p${percentileTarget.percentile} observed=${observed.toFixed(2)} ` +
        `${percentileTarget.unit === "ms" || percentileTarget.unit === "s" ? "ms" : percentileTarget.unit} ` +
        `vs declared=${percentileTarget.declared} (direction=${percentileTarget.direction}) — ` +
        `${samples.length} samples`,
      id,
      observed,
      percentileTarget.numeric,
    );
  }
  return {
    target,
    observed,
    threshold: percentileTarget.numeric,
    passed: true,
    sampleCount: samples.length,
  };
}

/** Convenience wrapper: alias for `expectSlo(id, samples)` with no
 *  minimum sample count; matches the bead's `expectSloPass` API name. */
export function expectSloPass(
  id: string,
  samples: readonly number[],
  options: LoadSloTargetsOptions = {},
): SloAssertionResult {
  return expectSlo(id, samples, options);
}

/** Assert a boolean target. */
export function expectSloBoolean(
  id: string,
  actual: boolean,
  options: LoadSloTargetsOptions = {},
): SloAssertionResult {
  const target = getTarget(id, options);
  if (target.target.kind !== "boolean") {
    throw new SloAssertionError(
      `target '${id}' is a percentile target — call expectSlo instead`,
      id,
      0,
      0,
    );
  }
  const expected = target.target.expected;
  const passed = actual === expected;
  if (!passed) {
    throw new SloAssertionError(
      `target '${id}' breached: actual=${String(actual)} expected=${String(expected)}`,
      id,
      actual ? 1 : 0,
      expected ? 1 : 0,
    );
  }
  return {
    target,
    observed: actual ? 1 : 0,
    threshold: expected ? 1 : 0,
    passed: true,
    sampleCount: 1,
  };
}
