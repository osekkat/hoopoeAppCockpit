// hp-no7e — Stage-data renderer tests pin two contracts:
// 1. Mock-flywheel.* command IDs never reach window.hoopoe.daemon.request,
//    even when a bridge is present. The preload boundary rejects them, so
//    routing them through the bridge would break BeadsStage/SwarmStage in
//    real Electron. Always serve fixture fallback for these IDs.
// 2. Stage queries do NOT cache forever — staleTime is finite so TanStack
//    Query refetches on focus/mount.

import { afterEach, beforeEach, describe, expect, test } from "bun:test";
import {
  loadBeadsStageData,
  loadSwarmStageData,
  STAGE_QUERY_STALE_MS,
} from "./stage-data.ts";

interface DaemonStub {
  readonly requested: Array<{ method: string; body: unknown }>;
  readonly result: unknown;
}

function installDaemonStub(stub: DaemonStub) {
  const globalAny = globalThis as unknown as {
    window?: { hoopoe?: { daemon?: { request?: (method: string, body: unknown) => Promise<unknown> } } };
  };
  globalAny.window = {
    hoopoe: {
      daemon: {
        request: async (method: string, body: unknown) => {
          stub.requested.push({ method, body });
          return stub.result;
        },
      },
    },
  };
}

function clearDaemonStub() {
  const globalAny = globalThis as unknown as { window?: unknown };
  delete globalAny.window;
}

describe("hp-no7e — stage queries are not permanently fresh", () => {
  test("STAGE_QUERY_STALE_MS is finite (was POSITIVE_INFINITY pre-fix)", () => {
    expect(Number.isFinite(STAGE_QUERY_STALE_MS)).toBe(true);
    expect(STAGE_QUERY_STALE_MS).toBeGreaterThan(0);
  });
});

describe("hp-no7e — mock-flywheel.* IDs never reach the daemon bridge", () => {
  beforeEach(() => {
    clearDaemonStub();
  });

  afterEach(() => {
    clearDaemonStub();
  });

  test("loadBeadsStageData uses fixture fallback for mock projects even when a bridge is present", async () => {
    const stub: DaemonStub = { requested: [], result: { issues: [] } };
    installDaemonStub(stub);
    const data = await loadBeadsStageData("local-demo");
    expect(stub.requested).toEqual([]);
    expect(data.source.transport).toBe("fixture-fallback");
  });

  test("loadSwarmStageData uses fixture fallback for mock projects even when a bridge is present", async () => {
    const stub: DaemonStub = { requested: [], result: {} };
    installDaemonStub(stub);
    const data = await loadSwarmStageData("local-demo");
    expect(stub.requested).toEqual([]);
    expect(data.source.transport).toBe("fixture-fallback");
  });

  test("loadBeadsStageData refuses non-mock projects (no real beads.list RPC wired)", async () => {
    const stub: DaemonStub = { requested: [], result: {} };
    installDaemonStub(stub);
    await expect(loadBeadsStageData("real-project")).rejects.toThrow(/main-only mock-flywheel command/);
    expect(stub.requested).toEqual([]);
  });

  test("loadSwarmStageData refuses non-mock projects (no real swarm.snapshot RPC wired)", async () => {
    const stub: DaemonStub = { requested: [], result: {} };
    installDaemonStub(stub);
    await expect(loadSwarmStageData("real-project")).rejects.toThrow(/main-only mock-flywheel command/);
    expect(stub.requested).toEqual([]);
  });
});
