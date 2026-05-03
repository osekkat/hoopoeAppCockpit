// `@hoopoe/tending-actions` — public API (hp-dmz).
//
// One YAML, one closed action surface, one drift gate. Daemon's
// ActionPlan executor (hp-209) and tests both load via this module so
// `tending-actions.yaml` and OpenAPI's `ActionKind` enum can never
// silently drift.

export {
  loadTendingActions,
  TendingActionsError,
  type LoadTendingActionsOptions,
} from "./loader.ts";

export {
  clearActions,
  ensureRegistry,
  getAction,
  listActions,
  listActionsByRisk,
  listActionsRequiringApproval,
  useActions,
} from "./registry.ts";

export {
  assertActionKindInSync,
  checkActionKindDrift,
  type CheckDriftOptions,
  type DriftReport,
} from "./validate.ts";

export {
  TENDING_ACTIONS_SCHEMA_VERSION,
  type JsonSchemaFragment,
  type RiskClass,
  type TendingAction,
  type TendingActionsBundle,
} from "./types.ts";
