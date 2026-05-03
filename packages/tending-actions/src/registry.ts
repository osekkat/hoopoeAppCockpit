// `@hoopoe/tending-actions` — in-process registry (hp-dmz).
//
// Same pattern as `@hoopoe/slo`'s registry: load once, look up by
// kind. `getAction(kind)` throws if the kind is not declared so a
// typo at the call site fails loudly rather than silently no-op.

import {
  loadTendingActions,
  TendingActionsError,
  type LoadTendingActionsOptions,
} from "./loader.ts";
import type { RiskClass, TendingAction, TendingActionsBundle } from "./types.ts";

let activeRegistry: TendingActionsBundle | null = null;

export function useActions(bundle: TendingActionsBundle): void {
  activeRegistry = bundle;
}

export function clearActions(): void {
  activeRegistry = null;
}

export function ensureRegistry(options?: LoadTendingActionsOptions): TendingActionsBundle {
  if (activeRegistry !== null) return activeRegistry;
  activeRegistry = loadTendingActions(options ?? {});
  return activeRegistry;
}

export function getAction(kind: string, options?: LoadTendingActionsOptions): TendingAction {
  const registry = ensureRegistry(options);
  const action = registry.actions.find((a) => a.kind === kind);
  if (action === undefined) {
    const known = registry.actions.map((a) => a.kind).join(", ");
    throw new TendingActionsError(
      registry.sourcePath,
      `unknown action kind '${kind}' — declared kinds: [${known || "(none)"}]`,
    );
  }
  return action;
}

export function listActions(options?: LoadTendingActionsOptions): readonly TendingAction[] {
  return ensureRegistry(options).actions;
}

export function listActionsByRisk(
  riskClass: RiskClass,
  options?: LoadTendingActionsOptions,
): readonly TendingAction[] {
  return listActions(options).filter((a) => a.riskClass === riskClass);
}

export function listActionsRequiringApproval(
  options?: LoadTendingActionsOptions,
): readonly TendingAction[] {
  return listActions(options).filter((a) => a.requiresApprovalDefault);
}
