// `@hoopoe/slo` — public API (hp-5ja).
//
// One YAML, three asserts, one Diagnostics surface. Tests / dashboards
// / lint rules import from here; the canonical YAML lives at
// `packages/slo-targets.yaml`.

export {
  loadSloTargets,
  SloTargetsError,
  type LoadSloTargetsOptions,
} from "./loader.ts";

export {
  PercentileError,
  percentile,
} from "./percentile.ts";

export {
  clearTargets,
  ensureRegistry,
  getTarget,
  listTargets,
  listTargetsByPhase,
  useTargets,
} from "./registry.ts";

export {
  SloAssertionError,
  expectSlo,
  expectSloBoolean,
  expectSloPass,
  type ExpectSloOptions,
  type SloAssertionResult,
} from "./assertions.ts";

export {
  SLO_SCHEMA_VERSION,
  type BooleanTarget,
  type Direction,
  type PercentileTarget,
  type SloTarget,
  type SloTargets,
  type Target,
} from "./types.ts";
