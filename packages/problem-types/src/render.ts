// `@hoopoe/problem-types` — RFC 7807 envelope renderer (hp-g6sp).
//
// Builds the canonical envelope from a registry entry + runtime
// extensions. Tests assert that the daemon's wire format matches what
// `renderProblemEnvelope` would produce for the same id + extensions
// — drift between the two means the daemon is hand-writing JSON
// somewhere it shouldn't.

import { getProblem } from "./registry.ts";
import type { LoadProblemTypesOptions } from "./loader.ts";
import type { ProblemEnvelope } from "./types.ts";

const TEMPLATE_VAR_RE = /\{\{\s*([A-Za-z_][A-Za-z0-9_]*)\s*\}\}/g;

/** Substitute `{{var}}` placeholders using `extensions[var]`. Missing
 *  values stay as-is so the template is preserved verbatim (rather
 *  than producing `undefined` substrings). */
export function renderTemplate(template: string, extensions: Record<string, unknown>): string {
  TEMPLATE_VAR_RE.lastIndex = 0;
  return template.replace(TEMPLATE_VAR_RE, (match, name: string) => {
    const value = extensions[name];
    if (value === undefined || value === null) return match;
    return String(value);
  });
}

export interface RenderOptions extends LoadProblemTypesOptions {
  /** Caller-supplied extension fields used both for templating and as
   *  passthrough on the envelope. Reserved keys (`type`, `title`,
   *  `status`, `detail`, `instance`, `correlation_id`, `surface`,
   *  `actionability`, `user_message`) are silently ignored at the
   *  passthrough layer to prevent extension overrides. */
  extensions?: Record<string, unknown>;
  /** RFC 7807 `instance` — request path or correlation id. */
  instance?: string;
  /** Audit-log correlation id; injected as the `correlation_id`
   *  extension per §10. */
  correlationId?: string;
}

const RESERVED_KEYS: ReadonlySet<string> = new Set([
  "type",
  "title",
  "status",
  "detail",
  "instance",
  "correlation_id",
  "surface",
  "actionability",
  "user_message",
]);

/** Build a problem+json envelope for a known problem-type id. */
export function renderProblemEnvelope(
  id: string,
  options: RenderOptions = {},
): ProblemEnvelope {
  const extensions = options.extensions ?? {};
  const lookupOpts: LoadProblemTypesOptions = {};
  if (options.path !== undefined) lookupOpts.path = options.path;
  if (options.repoRoot !== undefined) lookupOpts.repoRoot = options.repoRoot;
  const problem = getProblem(id, lookupOpts);
  const envelope: ProblemEnvelope = {
    type: problem.typeUri,
    title: problem.title,
    status: problem.status,
    surface: problem.surface,
    actionability: problem.actionability,
    user_message: renderTemplate(problem.userMessage, extensions),
  };
  if (problem.detailTemplate !== undefined) {
    envelope.detail = renderTemplate(problem.detailTemplate, extensions);
  }
  if (options.instance !== undefined) {
    envelope.instance = options.instance;
  }
  if (options.correlationId !== undefined) {
    envelope.correlation_id = options.correlationId;
  }
  for (const [key, value] of Object.entries(extensions)) {
    if (RESERVED_KEYS.has(key)) continue;
    envelope[key] = value;
  }
  return envelope;
}
