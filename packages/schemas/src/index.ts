// `@hoopoe/schemas` is the source of truth for Hoopoe's daemon API shapes.
//
// Layout (hp-r3i):
//   - openapi.yaml                        — authoritative OpenAPI 3.1 spec.
//   - src/generated/openapi.ts            — openapi-typescript output (committed; CI gate detects drift).
//   - src/index.ts                        — public entry: re-exports + small runtime helpers.
//   - go/                                 — separate Go module with oapi-codegen output.
//
// Consumers import named types from here, e.g.:
//   import type { components, paths, Problem } from "@hoopoe/schemas";
//
// Runtime helpers live in this file because they need to ship next to the
// types that describe them (and because consumers want them in both the
// renderer and the daemon-shim layer).

export type { components, operations, paths } from "./generated/openapi.ts";

import type { components } from "./generated/openapi.ts";

/**
 * Bare aliases for the most commonly-used component schemas. Keep this list
 * short; full namespace access is `components["schemas"]["..."]`.
 */
export type Problem = components["schemas"]["Problem"];
export type Capability = components["schemas"]["Capability"];
export type CapabilityStatus = components["schemas"]["CapabilityStatus"];
export type ToolId = components["schemas"]["ToolId"];
export type ToolReport = components["schemas"]["ToolReport"];
export type DegradedModePolicy = components["schemas"]["DegradedModePolicy"];
export type CapabilityRegistry = components["schemas"]["CapabilityRegistry"];
export type CompatibilityReport = components["schemas"]["CompatibilityReport"];
export type MigrationState = components["schemas"]["MigrationState"];
export type HealthResponse = components["schemas"]["HealthResponse"];
export type VersionResponse = components["schemas"]["VersionResponse"];
export type Actor = components["schemas"]["Actor"];
export type PageMeta = components["schemas"]["PageMeta"];

/** Public package identity. Used in audit + diagnostics. */
export const HOOPOE_SCHEMAS_PACKAGE_NAME = "@hoopoe/schemas";

/** Matches `info.version` in `openapi.yaml`. Bump on any breaking spec change. */
export const HOOPOE_OPENAPI_VERSION = "0.1.0";

/**
 * RFC 7807 problem+json content type. Use when checking `Content-Type` on
 * error responses.
 */
export const PROBLEM_JSON_CONTENT_TYPE = "application/problem+json";

/**
 * Runtime predicate: is this value a Problem? Cheap shape check; use before
 * narrowing without trusting the wire. Returns false for null/undefined.
 */
export function isProblem(value: unknown): value is Problem {
  if (value === null || typeof value !== "object") return false;
  const v = value as Record<string, unknown>;
  return (
    typeof v.type === "string" &&
    typeof v.title === "string" &&
    typeof v.status === "number" &&
    typeof v.code === "string"
  );
}
