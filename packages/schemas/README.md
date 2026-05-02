# `@hoopoe/schemas`

OpenAPI source of truth for Hoopoe's daemon API. Generates a TypeScript
client (consumed by `@hoopoe/desktop`) and Go types (consumed by
`@hoopoe/daemon`) so the two surfaces never drift (`plan.md §3`, `§2.6`).

## Status

Pre-Phase-1 scaffold (hp-xru). The seed REST/WS contract from `plan.md §2.6`
and the TS+Go codegen pipeline land in **hp-r3i** (Phase 2.5) before any
Planning/Beads UI depends on daemon behavior (per `plan.md §16` Phase 2.5
"do BEFORE Phase 3+").

## Convention

- `openapi.yaml` is authoritative. Hand-maintained duplicate shape
  definitions across desktop and daemon are forbidden.
- All write endpoints accept an `Idempotency-Key` header; all errors use
  RFC 7807 `problem+json`; all state-changing calls write audit entries.
- `/v1/capabilities` (Phase 2.5) reports adapter capability IDs; stage
  routes are gated on these IDs, not on optimistic "tool is installed."

## What lands when

- **hp-xru** (this scaffold) — package layout, placeholder `openapi.yaml`,
  smoke test.
- **hp-r3i** (Phase 2.5) — seed contract, TS client + Go types codegen, CI
  schema-validation gate.
- **hp-r33** (Phase 2.5) — capability registry + degraded-mode contract.
