// `@hoopoe/slo` — types for the canonical hp-5ja target schema.
//
// Mirrors `packages/slo-targets.yaml` exactly. Adding a new field here
// must come with a YAML schemaVersion bump (see SLO_SCHEMA_VERSION).

export const SLO_SCHEMA_VERSION = 1 as const;

export type Direction = "max" | "min";

/** Latency / duration / counter / percentage SLO. The `value` is parsed
 *  on load into `numericMs`; the canonical representation is always
 *  milliseconds for time-based units, raw numbers for counters. */
export interface PercentileTarget {
  kind: "percentile";
  percentile: number;
  /** Declared as written in YAML — e.g. "10s", "150ms", "85%", "300". */
  declared: string;
  /** Parsed numeric form (ms for s/ms, raw number for unitless / %). */
  numeric: number;
  unit: "ms" | "s" | "%" | "count";
  direction: Direction;
}

/** Pass/fail SLO that asserts a boolean condition. */
export interface BooleanTarget {
  kind: "boolean";
  expected: boolean;
}

export type Target = PercentileTarget | BooleanTarget;

export interface SloTarget {
  /** Stable identifier, referenced from tests + Diagnostics + plan. */
  id: string;
  description: string;
  target: Target;
  /** Authoritative plan.md anchor (e.g. `§10.5`). */
  sourceSection: string;
  /** Phases / suites this target gates. */
  enforcedIn: readonly string[];
}

export interface SloTargets {
  schemaVersion: typeof SLO_SCHEMA_VERSION;
  targets: ReadonlyArray<SloTarget>;
  /** Source path the targets were loaded from (for error messages). */
  sourcePath: string;
}
