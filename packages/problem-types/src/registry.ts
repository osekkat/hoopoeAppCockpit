// `@hoopoe/problem-types` — in-process registry (hp-g6sp).
//
// Same pattern as `@hoopoe/slo` and `@hoopoe/tending-actions`: load
// once, look up by id. `getProblem(id)` throws if the id is not
// declared so a typo at the call site fails loudly.

import {
  loadProblemTypes,
  ProblemTypesError,
  type LoadProblemTypesOptions,
} from "./loader.ts";
import type {
  ProblemActionability,
  ProblemRegistry,
  ProblemSurface,
  ProblemType,
} from "./types.ts";

let activeRegistry: ProblemRegistry | null = null;

export function useProblems(registry: ProblemRegistry): void {
  activeRegistry = registry;
}

export function clearProblems(): void {
  activeRegistry = null;
}

export function ensureRegistry(options?: LoadProblemTypesOptions): ProblemRegistry {
  if (activeRegistry !== null) return activeRegistry;
  activeRegistry = loadProblemTypes(options ?? {});
  return activeRegistry;
}

export function getProblem(id: string, options?: LoadProblemTypesOptions): ProblemType {
  const registry = ensureRegistry(options);
  const problem = registry.problems.find((p) => p.id === id);
  if (problem === undefined) {
    const known = registry.problems.map((p) => p.id).join(", ");
    throw new ProblemTypesError(
      registry.sourcePath,
      `unknown problem id '${id}' — declared ids: [${known || "(none)"}]`,
    );
  }
  return problem;
}

export function listProblems(options?: LoadProblemTypesOptions): readonly ProblemType[] {
  return ensureRegistry(options).problems;
}

export function listBySurface(
  surface: ProblemSurface,
  options?: LoadProblemTypesOptions,
): readonly ProblemType[] {
  return listProblems(options).filter((p) => p.surface === surface);
}

export function listByActionability(
  actionability: ProblemActionability,
  options?: LoadProblemTypesOptions,
): readonly ProblemType[] {
  return listProblems(options).filter((p) => p.actionability === actionability);
}

export function listByStatus(
  status: number,
  options?: LoadProblemTypesOptions,
): readonly ProblemType[] {
  return listProblems(options).filter((p) => p.status === status);
}
