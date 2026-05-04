import { describe, expect, test } from "bun:test";
import {
  PowerAssertionError,
  PowerAssertionManager,
  registerPowerAssertionIpc,
  type BatterySnapshot,
  type CaffeinateProcess,
  type NativeActivityBridge,
  type PowerAssertionAuditEvent,
  type PowerSaveBlockerLike,
} from "./macPowerAssert.ts";
import { IpcRegistry } from "./IpcRegistry.ts";

function fixedClock(startMs = 0) {
  let nowMs = startMs;
  return {
    now: () => new Date(nowMs),
    advance: (ms: number) => {
      nowMs += ms;
    },
  };
}

function makePowerSaveBlocker(opts: { throwOnStart?: boolean } = {}) {
  const starts: string[] = [];
  const stops: number[] = [];
  let nextID = 1;
  const blocker: PowerSaveBlockerLike = {
    start(type) {
      starts.push(type);
      if (opts.throwOnStart) {
        throw new Error("powerSaveBlocker failed");
      }
      return nextID++;
    },
    stop(id) {
      stops.push(id);
    },
  };
  return { blocker, starts, stops };
}

function makeNativeBridge(opts: { throwOnStart?: boolean } = {}) {
  const begins: Array<{ level: string; reason: string }> = [];
  const ends: Array<string | number> = [];
  const bridge: NativeActivityBridge = {
    beginActivity(input) {
      begins.push(input);
      if (opts.throwOnStart) {
        throw new Error("NSProcessInfo bridge failed");
      }
      return `native-${begins.length}`;
    },
    endActivity(token) {
      ends.push(token);
    },
  };
  return { bridge, begins, ends };
}

function makeCaffeinateSpawner() {
  const spawns: Array<{ pid: number; level: string; reason: string }> = [];
  const kills: Array<string | number | undefined> = [];
  const process: CaffeinateProcess = {
    pid: 123,
    kill(signal) {
      kills.push(signal);
      return true;
    },
  };
  return {
    spawns,
    kills,
    spawner: {
      spawn(input) {
        spawns.push(input);
        return process;
      },
    },
  };
}

function makeBattery(snapshot: BatterySnapshot) {
  return {
    current: () => snapshot,
  };
}

describe("PowerAssertionManager", () => {
  test("acquire uses Electron powerSaveBlocker and release stops it", () => {
    const clock = fixedClock();
    const blocker = makePowerSaveBlocker();
    const audit: PowerAssertionAuditEvent[] = [];
    const manager = new PowerAssertionManager({
      powerSaveBlocker: blocker.blocker,
      now: clock.now,
      audit: (event) => audit.push(event),
      idFactory: () => "pa-1",
    });

    const handle = manager.acquire({ roundId: "round-1", estimatedDurationMs: 60_000 });
    expect(blocker.starts).toEqual(["prevent-display-sleep"]);
    expect(handle.snapshot().heldCount).toBe(1);

    clock.advance(250);
    const snapshot = handle.release("round_complete");
    expect(blocker.stops).toEqual([1]);
    expect(snapshot.active).toBe(false);
    expect(audit.map((event) => event.kind)).toEqual([
      "pro-round.power_acquire",
      "pro-round.power_release",
    ]);
    expect(audit[1]?.heldMs).toBe(250);
  });

  test("powerSaveBlocker failure falls back to NSProcessInfo bridge", () => {
    const blocker = makePowerSaveBlocker({ throwOnStart: true });
    const native = makeNativeBridge();
    const audit: PowerAssertionAuditEvent[] = [];
    const manager = new PowerAssertionManager({
      powerSaveBlocker: blocker.blocker,
      nativeActivity: native.bridge,
      caffeinate: undefined,
      audit: (event) => audit.push(event),
      idFactory: () => "pa-1",
    });

    const handle = manager.acquire({ roundId: "round-1", estimatedDurationMs: 30 * 60_000 });

    expect(handle.snapshot().mechanism).toBe("nsprocessinfo");
    expect(native.begins.length).toBe(1);
    expect(audit.some((event) => event.warningKind === "fallback_used")).toBe(true);
    handle.release("round_complete");
    expect(native.ends).toEqual(["native-1"]);
  });

  test("falls back to caffeinate when Electron and NSProcessInfo fail", () => {
    const blocker = makePowerSaveBlocker({ throwOnStart: true });
    const native = makeNativeBridge({ throwOnStart: true });
    const caffeinate = makeCaffeinateSpawner();
    const manager = new PowerAssertionManager({
      powerSaveBlocker: blocker.blocker,
      nativeActivity: native.bridge,
      caffeinate: caffeinate.spawner,
      pid: 4242,
      idFactory: () => "pa-1",
    });

    const handle = manager.acquire({ roundId: "round-1", estimatedDurationMs: 30 * 60_000 });

    expect(handle.snapshot().mechanism).toBe("caffeinate");
    expect(caffeinate.spawns).toEqual([
      { pid: 4242, level: "app-suspension", reason: "ChatGPT Pro Oracle round in progress" },
    ]);
    handle.release("round_failed");
    expect(caffeinate.kills).toEqual(["SIGTERM"]);
  });

  test("release is refcounted across concurrent Pro rounds", () => {
    const blocker = makePowerSaveBlocker();
    let id = 0;
    const manager = new PowerAssertionManager({
      powerSaveBlocker: blocker.blocker,
      idFactory: () => `pa-${++id}`,
    });

    const r1 = manager.acquire({ roundId: "round-1" });
    const r2 = manager.acquire({ roundId: "round-2" });
    expect(blocker.starts.length).toBe(1);
    expect(manager.snapshot().heldCount).toBe(2);

    r1.release("round_complete");
    expect(manager.snapshot().active).toBe(true);
    expect(blocker.stops).toEqual([]);

    r2.release("round_complete");
    expect(manager.snapshot().active).toBe(false);
    expect(blocker.stops).toEqual([1]);
  });

  test("double release is idempotent and emits a warning", () => {
    const blocker = makePowerSaveBlocker();
    const audit: PowerAssertionAuditEvent[] = [];
    const manager = new PowerAssertionManager({
      powerSaveBlocker: blocker.blocker,
      audit: (event) => audit.push(event),
      idFactory: () => "pa-1",
    });

    const handle = manager.acquire({ roundId: "round-1" });
    handle.release("round_complete");
    handle.release("round_complete");

    expect(blocker.stops).toEqual([1]);
    expect(audit.some((event) => event.warningKind === "double_release")).toBe(true);
  });

  test("settings disable releases active assertions and blocks new acquire", () => {
    const blocker = makePowerSaveBlocker();
    const audit: PowerAssertionAuditEvent[] = [];
    const manager = new PowerAssertionManager({
      powerSaveBlocker: blocker.blocker,
      audit: (event) => audit.push(event),
      idFactory: () => "pa-1",
    });

    const handle = manager.acquire({ roundId: "round-1", estimatedDurationMs: 900_000 });
    const snapshot = manager.setDisabled(true);

    expect(snapshot.active).toBe(false);
    expect(blocker.stops).toEqual([1]);
    expect(audit.at(-1)?.kind).toBe("pro-round.power_release");
    expect(audit.at(-1)?.reason).toBe("user_disabled");
    expect(() => manager.acquire({ roundId: "round-2" })).toThrow(PowerAssertionError);
    expect(handle.release("round_complete").active).toBe(false);
  });

  test("watchdog warns after silence and force-releases after threshold", () => {
    const clock = fixedClock();
    const blocker = makePowerSaveBlocker();
    const audit: PowerAssertionAuditEvent[] = [];
    const manager = new PowerAssertionManager({
      powerSaveBlocker: blocker.blocker,
      now: clock.now,
      audit: (event) => audit.push(event),
      watchdogWarnAfterMs: 100,
      watchdogForceReleaseAfterMs: 200,
      idFactory: () => "pa-1",
    });

    manager.acquire({ roundId: "round-1" });
    clock.advance(120);
    manager.checkWatchdog();
    expect(audit.some((event) => event.warningKind === "silence_threshold")).toBe(true);
    expect(manager.snapshot().active).toBe(true);

    clock.advance(100);
    manager.checkWatchdog();
    expect(manager.snapshot().active).toBe(false);
    expect(audit.some((event) => event.reason === "watchdog_force_release")).toBe(true);
  });

  test("round activity resets watchdog silence", () => {
    const clock = fixedClock();
    const blocker = makePowerSaveBlocker();
    const audit: PowerAssertionAuditEvent[] = [];
    const manager = new PowerAssertionManager({
      powerSaveBlocker: blocker.blocker,
      now: clock.now,
      audit: (event) => audit.push(event),
      watchdogWarnAfterMs: 100,
      watchdogForceReleaseAfterMs: 200,
      idFactory: () => "pa-1",
    });

    manager.acquire({ roundId: "round-1" });
    clock.advance(90);
    manager.recordRoundActivity("round-1");
    clock.advance(90);
    manager.checkWatchdog();

    expect(audit.some((event) => event.warningKind === "silence_threshold")).toBe(false);
    expect(manager.snapshot().active).toBe(true);
  });

  test("low battery downgrades to display-only assertion and logs warning", () => {
    const blocker = makePowerSaveBlocker();
    const audit: PowerAssertionAuditEvent[] = [];
    const manager = new PowerAssertionManager({
      powerSaveBlocker: blocker.blocker,
      battery: makeBattery({ powerSource: "battery", levelPercent: 15, onBattery: true }),
      audit: (event) => audit.push(event),
      idFactory: () => "pa-1",
    });

    const handle = manager.acquire({ roundId: "round-1", estimatedDurationMs: 30 * 60_000 });

    expect(handle.snapshot().level).toBe("display");
    expect(blocker.starts).toEqual(["prevent-display-sleep"]);
    expect(audit.some((event) => event.warningKind === "battery_low_downgrade")).toBe(true);
  });

  test("idle leak guard reports held assertions whose round is no longer active", () => {
    const blocker = makePowerSaveBlocker();
    const manager = new PowerAssertionManager({
      powerSaveBlocker: blocker.blocker,
      idFactory: () => "pa-1",
    });

    manager.acquire({ roundId: "round-1" });
    const warnings = manager.checkIdleLeak([]);

    expect(warnings.length).toBe(1);
    expect(warnings[0]?.warningKind).toBe("leak_detected");
  });

  test("IPC registration exposes acquire, release, and snapshot channels", async () => {
    const blocker = makePowerSaveBlocker();
    const manager = new PowerAssertionManager({
      powerSaveBlocker: blocker.blocker,
      idFactory: () => "pa-1",
    });
    const registry = new IpcRegistry();
    registerPowerAssertionIpc(registry, manager);

    const acquired = await registry.dispatch<
      { roundId: string },
      { assertionId: string | null; heldCount: number }
    >("hoopoe.power.acquire", { roundId: "round-1" });
    expect(acquired.assertionId).toBe("pa-1");
    expect(acquired.heldCount).toBe(1);

    const snapshot = await registry.dispatch<Record<string, never>, { active: boolean }>(
      "hoopoe.power.snapshot",
      {},
    );
    expect(snapshot.active).toBe(true);

    const released = await registry.dispatch<
      { assertionId: string; reason: "round_cancelled" },
      { active: boolean }
    >("hoopoe.power.release", { assertionId: "pa-1", reason: "round_cancelled" });
    expect(released.active).toBe(false);
  });

  test("IPC release refuses unknown release reasons", async () => {
    const registry = new IpcRegistry();
    const manager = new PowerAssertionManager({
      powerSaveBlocker: makePowerSaveBlocker().blocker,
      idFactory: () => "pa-1",
    });
    registerPowerAssertionIpc(registry, manager);
    manager.acquire({ roundId: "round-1" });

    await expect(
      registry.dispatch("hoopoe.power.release", { assertionId: "pa-1", reason: "not-a-reason" }),
    ).rejects.toThrow(PowerAssertionError);
  });
});

test("PowerAssertionManager refuses malformed round IDs", () => {
  const manager = new PowerAssertionManager({
    powerSaveBlocker: makePowerSaveBlocker().blocker,
  });
  expect(() => manager.acquire({ roundId: "../bad" })).toThrow(PowerAssertionError);
});
