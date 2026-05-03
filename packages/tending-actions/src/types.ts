// `@hoopoe/tending-actions` — types for the canonical hp-dmz schema.
//
// Mirrors `packages/schemas/tending-actions.yaml` exactly. The
// `mirrorsOpenapiSchema: ActionKind` invariant is asserted via the
// drift checker in `validate.ts` — every YAML key must equal an
// `ActionKind` enum value in `openapi.yaml` and vice versa.

export const TENDING_ACTIONS_SCHEMA_VERSION = 1 as const;

export type RiskClass = "low" | "medium" | "high" | "destructive";

/** JSON-Schema fragment as written in the YAML. We keep it loosely
 *  typed at this layer; the daemon's ActionPlan executor (hp-209)
 *  binds it to a real validator (`Ajv` or hand-written). */
export type JsonSchemaFragment = Record<string, unknown>;

export interface TendingAction {
  /** Closed-set action `kind` (matches OpenAPI's `ActionKind` enum). */
  kind: string;
  description: string;
  riskClass: RiskClass;
  /** Default for `Action.requiresApproval` — policy may override per
   *  project. */
  requiresApprovalDefault: boolean;
  /** JSON Schema for the action's `target` payload. */
  target: JsonSchemaFragment;
  /** JSON Schema for the action's `args` payload. */
  args: JsonSchemaFragment;
  /** Canonical-state predicates the daemon evaluates BEFORE execution.
   *  Empty array means "no preconditions declared" (still valid). */
  preconditions: readonly string[];
  /** Canonical-state predicates the daemon evaluates AFTER execution.
   *  A failed postcondition emits a follow-up detection (per §8.3.1). */
  postconditions: readonly string[];
}

export interface TendingActionsBundle {
  schemaVersion: typeof TENDING_ACTIONS_SCHEMA_VERSION;
  /** Names of OpenAPI artifacts the YAML mirrors — used by the drift
   *  checker to know which file/enum to read. */
  mirrorsOpenapiSchema: string;
  mirrorsOpenapiYaml: string;
  actions: ReadonlyArray<TendingAction>;
  /** Source file the bundle was loaded from. */
  sourcePath: string;
}
