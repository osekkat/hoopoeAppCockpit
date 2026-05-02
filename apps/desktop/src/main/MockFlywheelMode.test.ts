// Hoopoe-owned. Smoke tests for Mock Flywheel Mode wiring (hp-o74).
//
// Asserts:
//   - argv parsing: --mock-flywheel toggles enabled, --scenario / --fixture-path
//     pick the right scenario, default is healthy-hour
//   - createMockFlywheelDaemon throws when mode disabled
//   - registerMockFlywheelClient registers all expected commands and
//     dispatches them through the IpcRegistry returning the same data the
//     direct MockDaemonClient would
//   - subscribe() delivers events at "instant" speed and the cursor map
//     advances per channel
//   - swapScenario re-loads with reset cursors

import { describe, expect, test } from "bun:test";
import { IpcRegistry } from "./IpcRegistry.ts";
import {
  parseArgvForMockFlywheel,
  createMockFlywheelDaemon,
  MOCK_FLYWHEEL_AUTH_TOKENS,
  MOCK_FLYWHEEL_AUDIT_ACTOR,
  MOCK_FLYWHEEL_FLAG,
  FIXTURE_PATH_FLAG,
  SCENARIO_FLAG,
} from "./MockFlywheelMode.ts";
import {
  registerMockFlywheelClient,
  MOCK_FLYWHEEL_COMMANDS,
  WHEN_MOCK_FLYWHEEL,
} from "./MockFlywheelClient.ts";
import type { ReplayEvent } from "@hoopoe/fixtures";

describe("parseArgvForMockFlywheel (hp-o74)", () => {
  test("returns enabled:false when flag absent", () => {
    const r = parseArgvForMockFlywheel({ argv: ["node", "main"] });
    expect(r.enabled).toBe(false);
  });

  test("default scenario is healthy-hour", () => {
    const r = parseArgvForMockFlywheel({ argv: ["node", "main", MOCK_FLYWHEEL_FLAG] });
    expect(r.enabled).toBe(true);
    if (r.enabled) {
      expect(r.scenario).toBe("healthy-hour");
      expect(r.isAbsolutePath).toBe(false);
      expect(r.demoStateDir.endsWith("/.hoopoe/demo/healthy-hour")).toBe(true);
    }
  });

  test("--scenario picks the named scenario", () => {
    const r = parseArgvForMockFlywheel({
      argv: ["node", "main", MOCK_FLYWHEEL_FLAG, SCENARIO_FLAG, "wedged-pane"],
    });
    expect(r.enabled).toBe(true);
    if (r.enabled) {
      expect(r.scenario).toBe("wedged-pane");
      expect(r.isAbsolutePath).toBe(false);
    }
  });

  test("--fixture-path takes precedence over --scenario", () => {
    const r = parseArgvForMockFlywheel({
      argv: [
        "node",
        "main",
        MOCK_FLYWHEEL_FLAG,
        FIXTURE_PATH_FLAG,
        "/tmp/some/path",
        SCENARIO_FLAG,
        "wedged-pane",
      ],
    });
    expect(r.enabled).toBe(true);
    if (r.enabled) {
      expect(r.scenario).toBe("/tmp/some/path");
      expect(r.isAbsolutePath).toBe(true);
    }
  });

  test("demoStateDir respects homedir override", () => {
    const r = parseArgvForMockFlywheel({
      argv: ["node", "main", MOCK_FLYWHEEL_FLAG],
      homedirImpl: () => "/Users/oss",
    });
    expect(r.enabled).toBe(true);
    if (r.enabled) {
      expect(r.demoStateDir).toBe("/Users/oss/.hoopoe/demo/healthy-hour");
    }
  });
});

describe("createMockFlywheelDaemon (hp-o74)", () => {
  test("throws when mode disabled", () => {
    expect(() => createMockFlywheelDaemon({ enabled: false })).toThrow(
      /MockFlywheelMode/,
    );
  });

  test("loads the healthy-hour scenario when enabled", () => {
    const r = parseArgvForMockFlywheel({
      argv: ["node", "main", MOCK_FLYWHEEL_FLAG],
      homedirImpl: () => "/Users/oss",
    });
    expect(r.enabled).toBe(true);
    const client = createMockFlywheelDaemon(r);
    expect(client.scenarioId()).toBe("healthy-hour");
    expect(client.health().environment).toBe("mock-flywheel");
    const projects = client.listProjects();
    expect(projects.length).toBe(1);
    expect(projects[0]?.name).toBe("healthy-hour");
  });
});

describe("auth tokens (hp-o74)", () => {
  test("mock pairing/bearer/ws-token are loud non-real strings", () => {
    expect(MOCK_FLYWHEEL_AUTH_TOKENS.pairingToken).toBe("MOCKMOCKMOCK");
    expect(MOCK_FLYWHEEL_AUTH_TOKENS.bearerToken.startsWith("hp-bearer-mock")).toBe(true);
    expect(MOCK_FLYWHEEL_AUTH_TOKENS.wsToken.startsWith("hp-ws-mock")).toBe(true);
  });

  test("audit actor is unmistakably a mock", () => {
    expect(MOCK_FLYWHEEL_AUDIT_ACTOR.kind).toBe("mock");
    expect(MOCK_FLYWHEEL_AUDIT_ACTOR.source).toBe("mock-flywheel");
  });
});

describe("registerMockFlywheelClient (hp-o74)", () => {
  function setup() {
    const ipc = new IpcRegistry();
    const r = parseArgvForMockFlywheel({
      argv: ["node", "main", MOCK_FLYWHEEL_FLAG],
      homedirImpl: () => "/Users/oss",
    });
    if (!r.enabled) throw new Error("expected enabled");
    const client = createMockFlywheelDaemon(r);
    return { ipc, client };
  }

  test("registers all 16 mock commands", () => {
    const { ipc, client } = setup();
    const handle = registerMockFlywheelClient({ ipcRegistry: ipc, client });
    expect(handle.commandsRegistered).toBe(Object.keys(MOCK_FLYWHEEL_COMMANDS).length);
    expect(ipc.size()).toBe(handle.commandsRegistered);
    handle.unregister();
    expect(ipc.size()).toBe(0);
  });

  test("dispatching health returns the same payload as direct call", async () => {
    const { ipc, client } = setup();
    registerMockFlywheelClient({ ipcRegistry: ipc, client });
    const direct = client.health();
    const dispatched = await ipc.dispatch<undefined, ReturnType<typeof client.health>>(
      MOCK_FLYWHEEL_COMMANDS.health,
      undefined,
      { [WHEN_MOCK_FLYWHEEL]: true },
    );
    expect(dispatched.environment).toBe(direct.environment);
    expect(dispatched.status).toBe(direct.status);
  });

  test("commands are gated on the mockFlywheel context key", async () => {
    const { ipc, client } = setup();
    registerMockFlywheelClient({ ipcRegistry: ipc, client });
    // Without the context key, dispatch should refuse.
    await expect(
      ipc.dispatch(MOCK_FLYWHEEL_COMMANDS.health, undefined, {}),
    ).rejects.toThrow(/unavailable/);
    // With the key, success.
    const ok = await ipc.dispatch(
      MOCK_FLYWHEEL_COMMANDS.health,
      undefined,
      { [WHEN_MOCK_FLYWHEEL]: true },
    );
    expect(ok).toBeDefined();
  });

  test("auth dance works against mock daemon", async () => {
    const { ipc, client } = setup();
    registerMockFlywheelClient({ ipcRegistry: ipc, client });
    const bearer = await ipc.dispatch<{ pairingToken: string }, { bearerToken: string }>(
      MOCK_FLYWHEEL_COMMANDS.exchangePairingForBearer,
      { pairingToken: MOCK_FLYWHEEL_AUTH_TOKENS.pairingToken },
      { [WHEN_MOCK_FLYWHEEL]: true },
    );
    expect(bearer.bearerToken).toBe(MOCK_FLYWHEEL_AUTH_TOKENS.bearerToken);
    const ws = await ipc.dispatch<{ bearerToken: string }, { wsToken: string }>(
      MOCK_FLYWHEEL_COMMANDS.issueWsToken,
      { bearerToken: bearer.bearerToken },
      { [WHEN_MOCK_FLYWHEEL]: true },
    );
    expect(ws.wsToken).toBe(MOCK_FLYWHEEL_AUTH_TOKENS.wsToken);
  });

  test("subscribe delivers fixture events + advances cursors per channel (instant replay)", async () => {
    const { ipc, client } = setup();
    const received: ReplayEvent[] = [];
    const handle = registerMockFlywheelClient({
      ipcRegistry: ipc,
      client,
      emitEvent: (e) => received.push(e),
      initialReplaySpeed: "instant",
    });
    expect(handle.session).not.toBeNull();
    await handle.session?.done;
    // healthy-hour fixture has 4 events. All should land at instant speed.
    expect(received.length).toBe(4);
    // Per-channel cursor map should advance.
    const cursors = client.currentCursors();
    expect(cursors["swarm"]).toBeGreaterThanOrEqual(1);
    handle.unregister();
  });

  test("scenarioInfo dispatch returns scenarioId and cursors", async () => {
    const { ipc, client } = setup();
    registerMockFlywheelClient({ ipcRegistry: ipc, client });
    const info = await ipc.dispatch<undefined, { scenarioId: string; cursors: Record<string, number> }>(
      MOCK_FLYWHEEL_COMMANDS.scenarioInfo,
      undefined,
      { [WHEN_MOCK_FLYWHEEL]: true },
    );
    expect(info.scenarioId).toBe("healthy-hour");
    expect(typeof info.cursors).toBe("object");
  });

  test("swapScenario dispatch swaps the loaded scenario", async () => {
    const { ipc, client } = setup();
    const handle = registerMockFlywheelClient({ ipcRegistry: ipc, client });
    expect(client.scenarioId()).toBe("healthy-hour");
    const result = await ipc.dispatch<{ scenarioId: string }, { scenarioId: string }>(
      MOCK_FLYWHEEL_COMMANDS.swapScenario,
      { scenarioId: "wedged-pane" },
      { [WHEN_MOCK_FLYWHEEL]: true },
    );
    expect(result.scenarioId).toBe("wedged-pane");
    expect(client.scenarioId()).toBe("wedged-pane");
    handle.unregister();
  });
});
