// `@hoopoe/problem-types` — types for the canonical hp-g6sp registry.
//
// Mirrors `packages/schemas/problem-types.yaml` exactly. The values
// drive renderer surface routing (hp-8dym) and the daemon's RFC 7807
// envelope shape.

export const PROBLEM_TYPES_SCHEMA_VERSION = 1 as const;

export const PROBLEM_SURFACES = [
  "toast",
  "banner",
  "inline_pill",
  "blocking_modal",
] as const;
export type ProblemSurface = (typeof PROBLEM_SURFACES)[number];

export const PROBLEM_ACTIONABILITIES = [
  "reload",
  "re-pair",
  "edit-deps",
  "switch-account",
  "open-docs",
  "manual",
] as const;
export type ProblemActionability = (typeof PROBLEM_ACTIONABILITIES)[number];

export interface ProblemType {
  /** Stable internal id; the Go ProblemError points at this id and the
   *  registry resolves it to a full envelope. */
  id: string;
  /** Canonical RFC 7807 `type` URL. */
  typeUri: string;
  /** Short, human-readable summary (RFC 7807 `title`). */
  title: string;
  /** HTTP status code. */
  status: number;
  /** Renderer hint per hp-8dym's surface matrix. */
  surface: ProblemSurface;
  /** Suggested actionability the renderer surfaces to the user. */
  actionability: ProblemActionability;
  /** User-facing message; supports `{{var}}` template substitution from
   *  the runtime extensions object. */
  userMessage: string;
  /** Optional template for the RFC 7807 `detail` field. Same templating;
   *  values pass through the daemon redaction layer before serialization. */
  detailTemplate?: string;
}

export interface ProblemRegistry {
  schemaVersion: typeof PROBLEM_TYPES_SCHEMA_VERSION;
  problems: ReadonlyArray<ProblemType>;
  /** Filesystem path the registry was loaded from. */
  sourcePath: string;
}

/** Canonical RFC 7807 envelope shape, with Hoopoe extensions. */
export interface ProblemEnvelope {
  /** RFC 7807 `type` — the registry entry's typeUri. */
  type: string;
  /** RFC 7807 `title`. */
  title: string;
  /** RFC 7807 `status`. */
  status: number;
  /** Optional RFC 7807 `detail` (templated; redacted at the daemon
   *  boundary before serialization). */
  detail?: string;
  /** RFC 7807 `instance` — request-specific identifier (e.g., the
   *  /v1/path that produced the error). */
  instance?: string;
  /** Hoopoe extension: matches the audit log entry for this request. */
  correlation_id?: string;
  /** Hoopoe extension: hp-8dym renderer surface hint. */
  surface: ProblemSurface;
  /** Hoopoe extension: hp-8dym renderer action hint. */
  actionability: ProblemActionability;
  /** Hoopoe extension: rendered user message (templated). */
  user_message: string;
  /** Caller-supplied extension fields (e.g. `cyclePath`, `provider`). */
  [extension: string]: unknown;
}
