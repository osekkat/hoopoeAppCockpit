import { expect, test } from "bun:test";
import {
  HOOPOE_OPENAPI_VERSION,
  HOOPOE_SCHEMAS_PACKAGE_NAME,
  PROBLEM_JSON_CONTENT_TYPE,
  isProblem,
  type Capability,
  type CompatibilityResponse,
  type Problem,
  type ToolCapabilityRegistry,
} from "./index.ts";

test("schemas package exposes identity + spec version", () => {
  expect(HOOPOE_SCHEMAS_PACKAGE_NAME).toBe("@hoopoe/schemas");
  expect(HOOPOE_OPENAPI_VERSION).toBe("0.1.0");
  expect(PROBLEM_JSON_CONTENT_TYPE).toBe("application/problem+json");
});

test("isProblem accepts valid Problem and rejects junk", () => {
  const valid: Problem = {
    type: "urn:hoopoe:auth/token-expired",
    title: "Bearer token expired",
    status: 401,
    code: "auth.token_expired",
  };
  expect(isProblem(valid)).toBe(true);

  expect(isProblem(null)).toBe(false);
  expect(isProblem(undefined)).toBe(false);
  expect(isProblem("nope")).toBe(false);
  expect(isProblem({ type: "x" })).toBe(false);
  // Wrong status type (string instead of number)
  expect(isProblem({ type: "x", title: "y", status: "401", code: "z" })).toBe(false);
});

test("Capability + ToolCapabilityRegistry types are nominally usable", () => {
  // Compile-time + runtime smoke that the generated types accept the §2.8
  // shape exactly. If openapi-typescript output drifts from the YAML, this
  // test will fail to compile.
  const cap: Capability = { status: "degraded", fallback: "tmux capture last" };
  const reg: ToolCapabilityRegistry = {
    tool: "ntm",
    version: "0.4.2",
    source: "ntm serve",
    capabilities: { "ntm.sessions.list": { status: "ok" }, "ntm.panes.stream": cap },
    lastCheckedAt: "2026-05-04T00:00:00Z",
    fixturesVersion: "phase0-2026-04-30",
  };
  expect(reg.capabilities["ntm.sessions.list"]?.status).toBe("ok");
  expect(reg.capabilities["ntm.panes.stream"]?.fallback).toBe("tmux capture last");
});

test("CompatibilityResponse embeds capability snapshot", () => {
  const compat: CompatibilityResponse = {
    schemaVersion: 1,
    daemonApiVersion: "0.1.0",
    minDesktopVersion: "0.1.0",
    eventSchemaVersions: { "project:abc": 1, _system: 1 },
    migrationState: "idle",
    capabilities: {
      schemaVersion: 1,
      generatedAt: "2026-05-04T00:00:00Z",
      registries: [],
    },
  };
  expect(compat.migrationState).toBe("idle");
  expect(compat.capabilities.registries).toHaveLength(0);
});
