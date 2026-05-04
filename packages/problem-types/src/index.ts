// `@hoopoe/problem-types` — public API (hp-g6sp).
//
// One YAML, one closed registry, one envelope shape. Daemon-side
// ProblemError implementations and renderer-side surface routing
// (hp-8dym) both consume from here so the wire format never drifts.

export {
  loadProblemTypes,
  ProblemTypesError,
  type LoadProblemTypesOptions,
} from "./loader.ts";

export {
  clearProblems,
  ensureRegistry,
  getProblem,
  listByActionability,
  listByStatus,
  listBySurface,
  listProblems,
  useProblems,
} from "./registry.ts";

export {
  renderProblemEnvelope,
  renderTemplate,
  type RenderOptions,
} from "./render.ts";

export {
  PROBLEM_JSON_CONTENT_TYPE,
  ProblemAssertionError,
  assertProblemMatchesRegistry,
  assertResponseIsProblemJson,
  assertResponseMatchesRegistry,
} from "./assertions.ts";

export {
  PROBLEM_ACTIONABILITIES,
  PROBLEM_SURFACES,
  PROBLEM_TYPES_SCHEMA_VERSION,
  type ProblemActionability,
  type ProblemEnvelope,
  type ProblemRegistry,
  type ProblemSurface,
  type ProblemType,
} from "./types.ts";
