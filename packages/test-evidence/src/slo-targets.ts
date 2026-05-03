// `@hoopoe/test-evidence` — SLO target loader (hp-5ja migration).
//
// The canonical SLO library lives at `@hoopoe/slo` (hp-5ja) — this
// module is a thin adapter that exposes the index-by-id `targets[id]`
// shape that the test-evidence reporter and the run-bun wrapper use
// for per-test lookups. Consumers wanting the richer assertion API
// (`expectSlo`, `expectSloPass`, `expectSloBoolean`, `percentile`)
// should import from `@hoopoe/slo` directly.

import {
  loadSloTargets as loadCanonicalTargets,
  SloTargetsError,
  type LoadSloTargetsOptions,
  type SloTarget,
} from "@hoopoe/slo";

export { SloTargetsError };
export type { LoadSloTargetsOptions, SloTarget };

export interface SloTargets {
  schemaVersion: 1;
  /** Per-id lookup table built from the canonical
   *  `SloTargets.targets` array. */
  targets: Readonly<Record<string, SloTarget>>;
  /** Filesystem path the YAML was loaded from. */
  sourcePath: string;
}

export function loadSloTargets(options: LoadSloTargetsOptions = {}): SloTargets {
  const canonical = loadCanonicalTargets(options);
  const indexed: Record<string, SloTarget> = {};
  for (const target of canonical.targets) {
    indexed[target.id] = target;
  }
  return { schemaVersion: 1, targets: indexed, sourcePath: canonical.sourcePath };
}

/** Evaluate a single observed value against a target. Percentile targets
 *  use `observed <= numeric` (or `>=` when `direction === "min"`).
 *  Boolean targets compare `observed >= 1` against the declared expectation.
 *
 *  For percentile assertions over a sample array, prefer
 *  `expectSlo`/`expectSloPass` from `@hoopoe/slo` — they compute the
 *  declared percentile from the samples and emit richer error messages. */
export function evaluateAgainst(target: SloTarget, observed: number): boolean {
  if (target.target.kind === "boolean") {
    const obs = observed >= 1;
    return obs === target.target.expected;
  }
  const t = target.target;
  if (t.direction === "max") return observed <= t.numeric;
  return observed >= t.numeric;
}
