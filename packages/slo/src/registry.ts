// `@hoopoe/slo` — in-process target registry (hp-5ja).
//
// Tests/Diagnostics call `useTargets(loadSloTargets({...}))` once at
// boot and then look up targets by id without re-reading YAML each time.
// `getTarget(id)` throws if the id isn't declared, so a typo in the test
// fails loudly rather than silently no-oping.

import {
  loadSloTargets,
  SloTargetsError,
  type LoadSloTargetsOptions,
} from "./loader.ts";
import type { SloTarget, SloTargets } from "./types.ts";

let activeRegistry: SloTargets | null = null;

export function useTargets(targets: SloTargets): void {
  activeRegistry = targets;
}

export function clearTargets(): void {
  activeRegistry = null;
}

/** Returns the active registry, loading it from the default location on
 *  first call. Subsequent calls use the cached instance. */
export function ensureRegistry(options?: LoadSloTargetsOptions): SloTargets {
  if (activeRegistry !== null) return activeRegistry;
  activeRegistry = loadSloTargets(options ?? {});
  return activeRegistry;
}

export function getTarget(id: string, options?: LoadSloTargetsOptions): SloTarget {
  const registry = ensureRegistry(options);
  const target = registry.targets.find((t) => t.id === id);
  if (target === undefined) {
    const known = registry.targets.map((t) => t.id).join(", ");
    throw new SloTargetsError(
      registry.sourcePath,
      `unknown target id '${id}' — declared ids: [${known || "(none)"}]`,
    );
  }
  return target;
}

export function listTargets(options?: LoadSloTargetsOptions): readonly SloTarget[] {
  return ensureRegistry(options).targets;
}

export function listTargetsByPhase(
  phase: string,
  options?: LoadSloTargetsOptions,
): readonly SloTarget[] {
  return listTargets(options).filter((t) => t.enforcedIn.includes(phase));
}
