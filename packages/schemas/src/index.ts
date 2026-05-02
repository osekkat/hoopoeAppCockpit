// `@hoopoe/schemas` is the source of truth for Hoopoe's API shapes.
//
// Phase 2.5 (hp-r3i) lands the real layout:
//   - openapi.yaml — authoritative OpenAPI definition.
//   - generated/ts/ — TS client (consumed by `@hoopoe/desktop`).
//   - generated/go/ — Go types (consumed by `@hoopoe/daemon`).
//
// For hp-xru this file just exposes a constant so the workspace has
// something to type-check.

export const HOOPOE_SCHEMAS_PACKAGE_NAME = "@hoopoe/schemas";
export const HOOPOE_OPENAPI_VERSION = "0.0.0-pre";
