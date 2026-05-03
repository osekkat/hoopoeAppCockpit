// schema-versions.test.ts — contract test for §10.3 migration discipline.
//
// Every persisted Hoopoe entity declares a top-level `schemaVersion` field so
// the daemon can run migrations on startup with backup + rollback (`plan.md
// §10.3`). Adding a new persisted entity without a `schemaVersion` is the
// failure mode this test exists to prevent.
//
// What this test does NOT cover (these belong to daemon beads, not hp-r3i):
//   - The actual migration runtime that mutates the daemon's SQLite (§10.3).
//   - Rollback-on-failure behavior of that runtime.
//   - Schema-version compatibility checks across daemon ↔ desktop versions.
// Those tests live in apps/daemon/internal/migration/* once that package
// exists; they'll import the schema-version constants from this package.
//
// What this test DOES cover:
//   - Every persisted entity in openapi.yaml carries a `schemaVersion` field.
//   - The OpenAPI spec's info.version is valid semver.
//   - Generated TS has the `SchemaVersion` alias.

import { expect, test } from "bun:test";
import { readFileSync } from "node:fs";
import { dirname, resolve } from "node:path";
import { fileURLToPath } from "node:url";

import type { components } from "./generated/openapi.ts";
import { HOOPOE_OPENAPI_VERSION } from "./index.ts";

const here = dirname(fileURLToPath(import.meta.url));
const pkgRoot = resolve(here, "..");
const openapiYaml = readFileSync(resolve(pkgRoot, "openapi.yaml"), "utf8");

/**
 * Persisted entities that the daemon stores (SQLite, JSON files, audit log).
 * Every name in this list must have:
 *   1. a top-level `schemaVersion` field in its YAML schema, AND
 *   2. that field listed under `required:`.
 *
 * Adding a new persisted entity? Add it here AND to the OpenAPI spec.
 * Removing one? Make sure no migration depends on it first.
 */
const PERSISTED_ENTITIES = [
  // VPS + Project (§4)
  "VpsHost",
  "Project",
  "ProjectReadiness",
  // Plans + Beads (§7.1, §7.2)
  "Plan",
  "Bead",
  // Jobs + Artifacts (§2.7)
  "Job",
  // Approvals (§5.3)
  "Approval",
  // WS event stream (§2.6) — envelope is persisted in event log
  "WsEventEnvelope",
  // Tending (§8.3.1)
  "ActionPlan",
  // Swarm + Agent + reservations (§7.3, §8)
  "SwarmSession",
  "SwarmLaunchSpec",
  "Agent",
  "FileReservation",
  "PaneStreamEvent",
  // Policy entities (§2.7, §8.5)
  "BudgetPolicy",
  "BuildQueuePolicy",
  // Health snapshots (§7.4)
  "CodeHealthSnapshot",
  "FileHealthMetric",
  // Provider plugin (§13) — hp-14zt replaced thin Contract with rich Manifest;
  // ProviderPluginContract is kept as an alias (`$ref` to ProviderPluginManifest)
  // for backward compatibility with hp-r3i imports, so the schemaVersion gate
  // walks the Manifest definition.
  "ProviderPluginManifest",
  // Capability registry composite (§2.8)
  "CapabilityRegistry",
  // Compatibility report — has its own schemaVersion since /v1/compatibility
  // is the migration-state surface (§10.3).
  "CompatibilityReport",
] as const;

test(`info.version (${HOOPOE_OPENAPI_VERSION}) is valid semver`, () => {
  // Loose semver — major.minor.patch with optional pre-release tag.
  const semverRe = /^\d+\.\d+\.\d+(-[a-z0-9.-]+)?$/;
  expect(HOOPOE_OPENAPI_VERSION).toMatch(semverRe);
  // openapi.yaml's info.version must agree with the constant.
  expect(openapiYaml).toContain(`version: ${HOOPOE_OPENAPI_VERSION}`);
});

test("SchemaVersion type alias is exported from generated TS", () => {
  // Compile-time check: SchemaVersion must be `number` (oapi-codegen treats
  // it as an integer alias, but the TS surface is `number`).
  const v: components["schemas"]["SchemaVersion"] = 1;
  expect(typeof v).toBe("number");
});

for (const entity of PERSISTED_ENTITIES) {
  test(`persisted entity ${entity} declares top-level schemaVersion`, () => {
    // Extract the schema block — from `    EntityName:` (4-space indent) to
    // the next sibling at the same indent. Targeted regex; simpler than a
    // full YAML parser and good enough for a contract test.
    const blockRe = new RegExp(
      `^    ${entity}:\\n((?:      .*\\n|\\s*\\n)+)`,
      "m",
    );
    const match = openapiYaml.match(blockRe);
    if (match === null) {
      throw new Error(
        `entity ${entity} not found in openapi.yaml — add it to PERSISTED_ENTITIES or to the spec`,
      );
    }
    const block = match[1] ?? "";
    expect(block).toContain("schemaVersion:");
    // Required list — single-line form `required: [a, b, c]` or block form.
    expect(/required:.*schemaVersion/s.test(block) || block.includes("- schemaVersion"))
      .toBe(true);
  });
}
