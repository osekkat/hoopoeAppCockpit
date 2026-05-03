import { expect, test } from "bun:test";
import {
  HOOPOE_OPENAPI_VERSION,
  HOOPOE_SCHEMAS_PACKAGE_NAME,
  PROBLEM_JSON_CONTENT_TYPE,
  isProblem,
  type Capability,
  type CapabilityRegistry,
  type CapabilityStatus,
  type CompatibilityReport,
  type DegradedModePolicy,
  type MigrationState,
  type Problem,
  type ToolReport,
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
  expect(isProblem({ type: "x", title: "y", status: "401", code: "z" })).toBe(false);
});

test("CapabilityStatus carries the agreed 5-valued enum", () => {
  // Compile-time check: each literal must be assignable. If openapi-typescript
  // narrows the union, this fails to compile — exactly the drift signal we want.
  const statuses: CapabilityStatus[] = [
    "ok",
    "degraded",
    "missing",
    "blocked-by-policy",
    "untested",
  ];
  expect(statuses).toHaveLength(5);
});

test("Capability + ToolReport accept the §2.8 fixture shape with notes + untested", () => {
  const cap: Capability = {
    status: "untested",
    notes: "daemon could not probe in this snapshot",
  };
  const report: ToolReport = {
    tool: "ntm",
    version: "0.4.2",
    source: "ntm serve",
    capabilities: {
      "ntm.sessions.list": { status: "ok" },
      "ntm.panes.stream": { status: "degraded", fallback: "tmux capture last" },
      "ntm.serve.rest": cap,
    },
    lastCheckedAt: "2026-05-04T00:00:00Z",
    fixturesVersion: "phase0-2026-04-30",
  };
  expect(report.capabilities["ntm.sessions.list"]?.status).toBe("ok");
  expect(report.capabilities["ntm.serve.rest"]?.notes).toContain("could not probe");
});

test("CapabilityRegistry uses object-keyed tools (per WhiteCreek delta B)", () => {
  const reg: CapabilityRegistry = {
    schemaVersion: 1,
    snapshotAt: "2026-05-04T00:00:00Z",
    daemonApiVersion: "0.1.0",
    fixturesVersion: "phase0-2026-04-30",
    tools: {
      git: {
        tool: "git",
        version: "2.45.0",
        source: "CLI",
        capabilities: { "git.status.read": { status: "ok" } },
        lastCheckedAt: "2026-05-04T00:00:00Z",
        fixturesVersion: "phase0-2026-04-30",
      },
    },
  };
  expect(reg.tools.git?.capabilities["git.status.read"]?.status).toBe("ok");
});

test("DegradedModePolicy uses camelCase keys (per WhiteCreek delta 2)", () => {
  const policy: DegradedModePolicy = {
    ifMissingRequired: "block_job",
    ifMissingOptional: "continue_with_warning",
    activityBehavior: "diagnostics_only",
  };
  expect(policy.ifMissingRequired).toBe("block_job");
});

test("MigrationState carries structured + optional phase (per WhiteCreek delta 3)", () => {
  const state: MigrationState = {
    schemaVersion: 7,
    appliedAt: "2026-05-04T00:00:00Z",
    pending: ["foo", "bar"],
    phase: "running",
  };
  expect(state.pending).toHaveLength(2);
  expect(state.phase).toBe("running");
});

test("Project + Bead + Job + Approval entity schemas compile against the §4/§5 shapes", () => {
  // Compile-time + runtime smoke: each entity accepts its required fields and
  // the discriminator-style enums narrow correctly. If openapi-typescript
  // changes a field shape, this test breaks loudly.
  const project: import("./index.ts").Project = {
    schemaVersion: 1,
    id: "proj_01",
    slug: "demo",
    name: "Demo project",
    vpsId: "vps_01",
    repo: { origin: "git@github.com:org/repo.git", branch: "main" },
    lifecycleState: "imported",
  };
  expect(project.lifecycleState).toBe("imported");

  const bead: import("./index.ts").Bead = {
    schemaVersion: 1,
    id: "hp-r3i",
    title: "packages/schemas keystone",
    status: "in_progress",
    priority: 1,
    issueType: "epic",
  };
  expect(bead.id).toBe("hp-r3i");

  const job: import("./index.ts").Job = {
    schemaVersion: 1,
    id: "job_01HXK...",
    type: "build.run",
    status: "running",
  };
  expect(job.status).toBe("running");

  const cmd: import("./index.ts").CommandSpec = {
    kind: "git.push_branch",
    target: { branch: "feature/x" },
    idempotencyKey: "tend:push:feature/x:2026-05-04",
    preconditions: ["branch exists"],
    postconditions: ["origin contains commit"],
  };
  const approval: import("./index.ts").Approval = {
    schemaVersion: 1,
    id: "apv_01",
    state: "pending",
    source: "hoopoe_policy",
    requestedAction: cmd,
    requestActor: { kind: "agent", id: "ag_7" },
    riskClass: "medium",
    scope: "this_bead",
    requestedAt: "2026-05-04T00:00:00Z",
  };
  expect(approval.requestedAction.kind).toBe("git.push_branch");
  expect(approval.source).toBe("hoopoe_policy");
});

test("WsEventEnvelope + WsHeartbeat + WsGap accept the §2.6 wire shapes", () => {
  const envelope: import("./index.ts").WsEventEnvelope = {
    eventId: "evt_01HXK...",
    schemaVersion: 1,
    channel: "project:proj_01",
    type: "bead.changed",
    sequence: 183,
    time: "2026-05-04T00:00:00Z",
    actor: { kind: "agent", id: "ag_123" },
    causationId: "cmd_01HXK...",
    correlationId: "swarm_01HXK...",
    data: { beadId: "hp-r3i", from: "open", to: "in_progress" },
  };
  expect(envelope.sequence).toBe(183);

  const hb: import("./index.ts").WsHeartbeat = {
    channel: "_system",
    type: "heartbeat",
    sequence: 9821,
    time: "2026-05-04T00:00:00Z",
  };
  expect(hb.channel).toBe("_system");

  const gap: import("./index.ts").WsGap = {
    channel: "project:proj_01",
    type: "_gap",
    from: 120,
    to: 183,
    repair: "replayEvents",
  };
  expect(gap.repair).toBe("replayEvents");
});

test("ActionPlan with one git.push_branch action narrows correctly (§8.3.1)", () => {
  const plan: import("./index.ts").ActionPlan = {
    schemaVersion: 1,
    jobId: "tend-swarm",
    runId: "trun_01HXK...",
    summary: "push agent ag_7's branch after passing tests",
    evidenceRefs: ["det_01", "pane_log:ag_7:offsets:1200-1550"],
    actions: [
      {
        kind: "git.push_branch",
        target: { projectId: "proj_01", branch: "feat/ag_7-bead-142" },
        args: { expectedSha: "abc123" },
        idempotencyKey: "tend-swarm:proj_01:push:abc123:2026-05-04",
        preconditions: ["branch exists at expectedSha"],
        postconditions: ["origin contains expectedSha"],
      },
    ],
    riskClass: "medium",
    requiresApproval: false,
  };
  expect(plan.actions[0]?.kind).toBe("git.push_branch");
});

test("CompatibilityReport embeds CapabilityRegistry + structured MigrationState", () => {
  const report: CompatibilityReport = {
    schemaVersion: 1,
    daemonApiVersion: "0.1.0",
    minDesktopVersion: "0.1.0",
    eventSchemaVersions: { "project:abc": 1, _system: 1 },
    migrationState: { schemaVersion: 0, appliedAt: "2026-05-04T00:00:00Z", pending: [] },
    capabilities: {
      schemaVersion: 1,
      snapshotAt: "2026-05-04T00:00:00Z",
      daemonApiVersion: "0.1.0",
      fixturesVersion: "phase0-2026-04-30",
      tools: {},
    },
  };
  expect(report.migrationState.schemaVersion).toBe(0);
  expect(Object.keys(report.capabilities.tools)).toHaveLength(0);
});
