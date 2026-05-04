// schemas-import.test.ts — hp-r3i DOD #9: generated TS client compiles in
// apps/desktop and is consumed in ≥1 IPC handler.
//
// This test:
//   1. Imports types from @hoopoe/schemas (the generated TS client wrapped
//      with handwritten re-exports). Compilation alone proves the types are
//      reachable from apps/desktop's tsconfig.
//   2. Registers an IpcCommandHandler whose Input/Output are typed against
//      the generated `Project` and `CompatibilityReport` shapes, and
//      dispatches it through the real IpcRegistry. This is the ≥1 IPC
//      handler that consumes the generated types.
//   3. Asserts isProblem() correctly narrows wire-shape errors.
//
// When the daemon RPC layer wires up its full typed surface (a follow-up
// bead), real handlers like daemon.compatibility, daemon.projects.list,
// etc. replace this smoke. Until then, this is the proof that generated
// types are consumable end-to-end without drift.

import { test, expect } from "bun:test";
import {
  type CompatibilityReport,
  type Problem,
  type Project,
  isProblem,
  HOOPOE_OPENAPI_VERSION,
  PROBLEM_JSON_CONTENT_TYPE,
} from "@hoopoe/schemas";
import { IpcRegistry } from "./IpcRegistry.ts";
import { INTERNAL_IPC_COMMANDS } from "../shared/ipc-contract.ts";

test("@hoopoe/schemas resolves from apps/desktop", () => {
  expect(HOOPOE_OPENAPI_VERSION).toBe("0.1.0");
  expect(PROBLEM_JSON_CONTENT_TYPE).toBe("application/problem+json");
});

test("Project shape can type an IpcCommandHandler Output", async () => {
  const registry = new IpcRegistry();
  registry.register<{ projectId: string }, Project>({
    id: INTERNAL_IPC_COMMANDS.schemasSmokeProject,
    handler: {
      handle: ({ projectId }) => ({
        schemaVersion: 1,
        id: projectId,
        slug: "demo",
        name: "Demo project",
        vpsId: "vps_01",
        repo: { origin: "git@github.com:org/repo.git", branch: "main" },
        lifecycleState: "imported",
      }),
    },
  });

  const project = await registry.dispatch<{ projectId: string }, Project>(
    INTERNAL_IPC_COMMANDS.schemasSmokeProject,
    { projectId: "proj_01" },
  );
  expect(project.id).toBe("proj_01");
  expect(project.lifecycleState).toBe("imported");
  expect(project.repo.branch).toBe("main");
});

test("CompatibilityReport shape can type an IpcCommandHandler Output", async () => {
  const registry = new IpcRegistry();
  registry.register<void, CompatibilityReport>({
    id: INTERNAL_IPC_COMMANDS.schemasSmokeCompatibility,
    handler: {
      handle: () => ({
        schemaVersion: 1,
        daemonApiVersion: HOOPOE_OPENAPI_VERSION,
        minDesktopVersion: HOOPOE_OPENAPI_VERSION,
        eventSchemaVersions: { _system: 1 },
        migrationState: { schemaVersion: 0, appliedAt: "2026-05-04T00:00:00Z", pending: [] },
        capabilities: {
          schemaVersion: 1,
          snapshotAt: "2026-05-04T00:00:00Z",
          daemonApiVersion: HOOPOE_OPENAPI_VERSION,
          fixturesVersion: "phase0-2026-04-30",
          tools: {},
        },
      }),
    },
  });

  const compat = await registry.dispatch<void, CompatibilityReport>(
    INTERNAL_IPC_COMMANDS.schemasSmokeCompatibility,
    undefined as unknown as void,
  );
  expect(compat.daemonApiVersion).toBe("0.1.0");
  expect(compat.migrationState.schemaVersion).toBe(0);
  expect(compat.capabilities.fixturesVersion).toBe("phase0-2026-04-30");
});

test("isProblem narrows daemon problem+json error responses", () => {
  const wire: unknown = {
    type: "urn:hoopoe:auth/token-expired",
    title: "Bearer token expired",
    status: 401,
    code: "auth.token_expired",
    detail: "Pairing token consumed > 30 days ago; re-pair.",
  };

  if (isProblem(wire)) {
    // Compile-time narrowing: wire is now Problem.
    const problem: Problem = wire;
    expect(problem.code).toBe("auth.token_expired");
    expect(problem.status).toBe(401);
  } else {
    throw new Error("isProblem failed to narrow valid Problem");
  }

  expect(isProblem({ type: "x" })).toBe(false);
  expect(isProblem(null)).toBe(false);
});
