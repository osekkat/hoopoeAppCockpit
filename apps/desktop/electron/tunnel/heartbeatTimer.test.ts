// hp-fkov — HeartbeatTimer tests.

import { describe, expect, test } from "bun:test";

import { HeartbeatTimer, type HeartbeatTimerAuditEvent, type IntervalScheduler, type ScheduledHandle } from "./heartbeatTimer.ts";
import type { HeartbeatDriver, HeartbeatStatus } from "./orchestrator.ts";
import type { VpsProfile } from "./types.ts";

const PROFILE: VpsProfile = {
  id: "profile-1",
  schemaVersion: 1,
  alias: "test",
  host: "vps.example.com",
  port: 22,
  username: "agent",
  privateKeyPath: "/home/test/.ssh/id_ed25519",
  knownHostFingerprint: null,
  daemonReleaseChannel: "stable",
  preferredLocalPort: 17655,
  createdAt: "2026-05-04T01:00:00.000Z",
  lastUsedAt: null,
};

// ── Test scheduler — explicit "fire next callback" control ───────────────

interface FakeScheduler extends IntervalScheduler {
  /** Pending callbacks waiting for `fireNext`. */
  readonly pending: Array<{ delayMs: number; callback: () => void; cancelled: boolean }>;
  /** Fire the next scheduled callback. Returns false if none queued. */
  fireNext(): boolean;
  /** Cancel everything. */
  cancelAll(): void;
}

function makeScheduler(): FakeScheduler {
  const pending: Array<{ delayMs: number; callback: () => void; cancelled: boolean }> = [];
  return {
    pending,
    schedule(delayMs, callback): ScheduledHandle {
      const entry = { delayMs, callback, cancelled: false };
      pending.push(entry);
      return {
        cancel: () => {
          entry.cancelled = true;
        },
      };
    },
    fireNext(): boolean {
      while (pending.length > 0) {
        const entry = pending.shift()!;
        if (!entry.cancelled) {
          entry.callback();
          return true;
        }
      }
      return false;
    },
    cancelAll() {
      for (const entry of pending) entry.cancelled = true;
      pending.length = 0;
    },
  };
}

// ── Test heartbeat driver — script of responses ───────────────────────────

interface ScriptedDriver extends HeartbeatDriver {
  readonly callCount: () => number;
  readonly lastCall: () => { profile: VpsProfile; localPort: number } | null;
}

function makeDriver(
  responses: ReadonlyArray<HeartbeatStatus | Error>,
): ScriptedDriver {
  let i = 0;
  let callCount = 0;
  let last: { profile: VpsProfile; localPort: number } | null = null;
  return {
    async check(input) {
      callCount += 1;
      last = { profile: input.profile, localPort: input.localPort };
      const next = responses[i++] ?? "ok";
      if (next instanceof Error) throw next;
      return next;
    },
    callCount: () => callCount,
    lastCall: () => last,
  };
}

interface OrchestratorRecorder {
  readonly orchestrator: { handleHeartbeatTimeout(message?: string): unknown };
  readonly calls: Array<string | undefined>;
}

function makeOrchestrator(opts: { throwOnHandle?: boolean } = {}): OrchestratorRecorder {
  const calls: Array<string | undefined> = [];
  return {
    orchestrator: {
      handleHeartbeatTimeout(message) {
        calls.push(message);
        if (opts.throwOnHandle) throw new Error("orchestrator handler boom");
      },
    },
    calls,
  };
}

const fixedNow = () => new Date("2026-05-04T01:00:00.000Z");

// Wait helper: yield enough event-loop turns for an async probe to land.
const flushAsync = () => new Promise<void>((r) => setTimeout(r, 5));

// ── Tests ────────────────────────────────────────────────────────────────

describe("HeartbeatTimer.start / stop", () => {
  test("start: schedules the first probe + emits timer_started audit", () => {
    const scheduler = makeScheduler();
    const driver = makeDriver(["ok"]);
    const orch = makeOrchestrator();
    const audit: HeartbeatTimerAuditEvent[] = [];
    const timer = new HeartbeatTimer({
      heartbeat: driver,
      orchestrator: orch.orchestrator,
      audit: (e) => audit.push(e),
      scheduler,
      intervalMs: 5000,
      now: fixedNow,
    });
    timer.start({ profile: PROFILE, localPort: 17655 });
    expect(scheduler.pending.length).toBe(1);
    expect(scheduler.pending[0]?.delayMs).toBe(5000);
    expect(audit.length).toBe(1);
    expect(audit[0]?.kind).toBe("heartbeat.timer_started");
    expect(audit[0]?.message).toContain("5000ms");
    expect(audit[0]?.message).toContain("17655");
    expect(timer.running).toBe(true);
  });

  test("happy tick: probe succeeds → tick_ok audit + reschedules", async () => {
    const scheduler = makeScheduler();
    const driver = makeDriver(["ok", "ok", "ok"]);
    const orch = makeOrchestrator();
    const audit: HeartbeatTimerAuditEvent[] = [];
    const timer = new HeartbeatTimer({
      heartbeat: driver,
      orchestrator: orch.orchestrator,
      audit: (e) => audit.push(e),
      scheduler,
      intervalMs: 1000,
    });
    timer.start({ profile: PROFILE, localPort: 17655 });

    scheduler.fireNext();
    await flushAsync();

    expect(driver.callCount()).toBe(1);
    expect(audit.map((e) => e.kind)).toEqual([
      "heartbeat.timer_started",
      "heartbeat.tick_started",
      "heartbeat.tick_ok",
    ]);
    expect(scheduler.pending.length).toBe(1); // next tick scheduled
    expect(orch.calls).toEqual([]);
  });

  test("driver call: passes through profile + localPort", async () => {
    const scheduler = makeScheduler();
    const driver = makeDriver(["ok"]);
    const orch = makeOrchestrator();
    const timer = new HeartbeatTimer({
      heartbeat: driver,
      orchestrator: orch.orchestrator,
      audit: () => undefined,
      scheduler,
    });
    timer.start({ profile: PROFILE, localPort: 23456 });
    scheduler.fireNext();
    await flushAsync();
    expect(driver.lastCall()).toEqual({ profile: PROFILE, localPort: 23456 });
  });

  test("multiple ticks: ok → ok → ok keeps rescheduling", async () => {
    const scheduler = makeScheduler();
    const driver = makeDriver(["ok", "ok", "ok"]);
    const orch = makeOrchestrator();
    const audit: HeartbeatTimerAuditEvent[] = [];
    const timer = new HeartbeatTimer({
      heartbeat: driver,
      orchestrator: orch.orchestrator,
      audit: (e) => audit.push(e),
      scheduler,
    });
    timer.start({ profile: PROFILE, localPort: 17655 });

    scheduler.fireNext();
    await flushAsync();
    scheduler.fireNext();
    await flushAsync();
    scheduler.fireNext();
    await flushAsync();

    expect(driver.callCount()).toBe(3);
    expect(audit.filter((e) => e.kind === "heartbeat.tick_ok").length).toBe(3);
    expect(orch.calls).toEqual([]);
  });

  test("driver throws: tick_failed audit + handleHeartbeatTimeout invoked + timer stops", async () => {
    const scheduler = makeScheduler();
    const driver = makeDriver([new Error("ECONNREFUSED")]);
    const orch = makeOrchestrator();
    const audit: HeartbeatTimerAuditEvent[] = [];
    const timer = new HeartbeatTimer({
      heartbeat: driver,
      orchestrator: orch.orchestrator,
      audit: (e) => audit.push(e),
      scheduler,
    });
    timer.start({ profile: PROFILE, localPort: 17655 });

    scheduler.fireNext();
    await flushAsync();

    expect(audit.map((e) => e.kind)).toEqual([
      "heartbeat.timer_started",
      "heartbeat.tick_started",
      "heartbeat.tick_failed",
    ]);
    expect(audit[2]?.message).toBe("ECONNREFUSED");
    expect(orch.calls).toEqual(["ECONNREFUSED"]);
    // No reschedule on failure — orchestrator's handleHeartbeatTimeout
    // transitions to reconnecting; a fresh start() lands via the FSM
    // snapshot subscription.
    expect(scheduler.pending.length).toBe(0);
  });

  test("version_mismatch: tick_failed + handleHeartbeatTimeout with version message", async () => {
    const scheduler = makeScheduler();
    const driver = makeDriver(["version_mismatch"]);
    const orch = makeOrchestrator();
    const audit: HeartbeatTimerAuditEvent[] = [];
    const timer = new HeartbeatTimer({
      heartbeat: driver,
      orchestrator: orch.orchestrator,
      audit: (e) => audit.push(e),
      scheduler,
    });
    timer.start({ profile: PROFILE, localPort: 17655 });

    scheduler.fireNext();
    await flushAsync();

    expect(orch.calls).toEqual(["Daemon API version mismatch"]);
    expect(audit.find((e) => e.kind === "heartbeat.tick_failed")?.message).toBe(
      "Daemon API version mismatch",
    );
  });

  test("stop: cancels pending probe + emits timer_stopped audit", () => {
    const scheduler = makeScheduler();
    const driver = makeDriver(["ok"]);
    const orch = makeOrchestrator();
    const audit: HeartbeatTimerAuditEvent[] = [];
    const timer = new HeartbeatTimer({
      heartbeat: driver,
      orchestrator: orch.orchestrator,
      audit: (e) => audit.push(e),
      scheduler,
    });
    timer.start({ profile: PROFILE, localPort: 17655 });
    expect(scheduler.pending.length).toBe(1);

    timer.stop();

    expect(scheduler.pending[0]?.cancelled).toBe(true);
    expect(audit.map((e) => e.kind)).toEqual([
      "heartbeat.timer_started",
      "heartbeat.timer_stopped",
    ]);
    expect(timer.running).toBe(false);
  });

  test("stop while idle: no-op, no audit", () => {
    const scheduler = makeScheduler();
    const driver = makeDriver([]);
    const orch = makeOrchestrator();
    const audit: HeartbeatTimerAuditEvent[] = [];
    const timer = new HeartbeatTimer({
      heartbeat: driver,
      orchestrator: orch.orchestrator,
      audit: (e) => audit.push(e),
      scheduler,
    });
    timer.stop();
    expect(audit.length).toBe(0);
  });

  test("stop during in-flight probe: result is discarded (no orchestrator call)", async () => {
    // Slow probe: start, fire scheduler, then stop BEFORE the probe
    // resolves. The orchestrator must NOT see a stale tick result.
    const scheduler = makeScheduler();
    let resolveProbe!: (status: HeartbeatStatus) => void;
    const driver: HeartbeatDriver = {
      check: () =>
        new Promise<HeartbeatStatus>((r) => {
          resolveProbe = r;
        }),
    };
    const orch = makeOrchestrator();
    const timer = new HeartbeatTimer({
      heartbeat: driver,
      orchestrator: orch.orchestrator,
      audit: () => undefined,
      scheduler,
    });
    timer.start({ profile: PROFILE, localPort: 17655 });
    scheduler.fireNext();
    // The probe is now pending. Stop the timer before resolving it.
    timer.stop();
    resolveProbe("ok");
    await flushAsync();
    // Timer was stopped → result discarded → no reschedule, no orch call.
    expect(scheduler.pending.length).toBe(0);
    expect(orch.calls).toEqual([]);
  });

  test("stop during in-flight probe with failure: orchestrator NOT called", async () => {
    // Symmetric: even a failure result that arrives after stop must
    // be silently discarded.
    const scheduler = makeScheduler();
    let rejectProbe!: (err: Error) => void;
    const driver: HeartbeatDriver = {
      check: () =>
        new Promise<HeartbeatStatus>((_, rej) => {
          rejectProbe = rej;
        }),
    };
    const orch = makeOrchestrator();
    const timer = new HeartbeatTimer({
      heartbeat: driver,
      orchestrator: orch.orchestrator,
      audit: () => undefined,
      scheduler,
    });
    timer.start({ profile: PROFILE, localPort: 17655 });
    scheduler.fireNext();
    timer.stop();
    rejectProbe(new Error("late failure"));
    await flushAsync();
    expect(orch.calls).toEqual([]);
  });

  test("start while running: rebinds + cancels old + reschedules", () => {
    // After reconnect the local port may change. Calling start with
    // a new (profile, localPort) must abandon the old binding cleanly.
    const scheduler = makeScheduler();
    const driver = makeDriver([]);
    const orch = makeOrchestrator();
    const timer = new HeartbeatTimer({
      heartbeat: driver,
      orchestrator: orch.orchestrator,
      audit: () => undefined,
      scheduler,
    });
    timer.start({ profile: PROFILE, localPort: 17655 });
    const firstHandle = scheduler.pending[0]!;
    timer.start({ profile: PROFILE, localPort: 22222 });
    // First handle was cancelled.
    expect(firstHandle.cancelled).toBe(true);
    // Two handles total in the queue (the cancelled first + a fresh one).
    expect(scheduler.pending.length).toBe(2);
  });

  test("start with new port mid-flight: in-flight probe result is discarded", async () => {
    // start() on the SAME profile but a new port should treat any
    // in-flight probe as stale.
    const scheduler = makeScheduler();
    let resolveFirst!: (status: HeartbeatStatus) => void;
    let probeIndex = 0;
    const driver: HeartbeatDriver = {
      check: () => {
        probeIndex += 1;
        if (probeIndex === 1) {
          return new Promise<HeartbeatStatus>((r) => {
            resolveFirst = r;
          });
        }
        return Promise.resolve("ok");
      },
    };
    const orch = makeOrchestrator();
    const timer = new HeartbeatTimer({
      heartbeat: driver,
      orchestrator: orch.orchestrator,
      audit: () => undefined,
      scheduler,
    });
    timer.start({ profile: PROFILE, localPort: 17655 });
    scheduler.fireNext(); // probe 1 in flight
    timer.start({ profile: PROFILE, localPort: 22222 });
    resolveFirst("ok"); // late result
    await flushAsync();
    // No reschedule from the stale probe.
    expect(orch.calls).toEqual([]);
  });

  test("audit-sink that throws does NOT stop the timer", async () => {
    let throws = 0;
    const scheduler = makeScheduler();
    const driver = makeDriver(["ok", "ok"]);
    const orch = makeOrchestrator();
    const timer = new HeartbeatTimer({
      heartbeat: driver,
      orchestrator: orch.orchestrator,
      audit: () => {
        throws += 1;
        throw new Error("audit boom");
      },
      scheduler,
    });
    timer.start({ profile: PROFILE, localPort: 17655 });
    scheduler.fireNext();
    await flushAsync();
    scheduler.fireNext();
    await flushAsync();
    expect(driver.callCount()).toBe(2);
    expect(throws).toBeGreaterThan(0);
  });

  test("orchestrator handleHeartbeatTimeout throws: timer survives", async () => {
    // A buggy orchestrator handler must not corrupt the timer's state.
    const scheduler = makeScheduler();
    const driver = makeDriver([new Error("boom")]);
    const orch = makeOrchestrator({ throwOnHandle: true });
    const audit: HeartbeatTimerAuditEvent[] = [];
    const timer = new HeartbeatTimer({
      heartbeat: driver,
      orchestrator: orch.orchestrator,
      audit: (e) => audit.push(e),
      scheduler,
    });
    timer.start({ profile: PROFILE, localPort: 17655 });
    scheduler.fireNext();
    await flushAsync();
    // Audit still recorded the failure.
    expect(audit.find((e) => e.kind === "heartbeat.tick_failed")).toBeDefined();
    // Subsequent stop() is well-formed.
    expect(() => timer.stop()).not.toThrow();
  });

  test("default intervalMs: 30s", () => {
    const scheduler = makeScheduler();
    const timer = new HeartbeatTimer({
      heartbeat: makeDriver([]),
      orchestrator: makeOrchestrator().orchestrator,
      audit: () => undefined,
      scheduler,
    });
    timer.start({ profile: PROFILE, localPort: 17655 });
    expect(scheduler.pending[0]?.delayMs).toBe(30_000);
  });

  test("custom intervalMs: respected", () => {
    const scheduler = makeScheduler();
    const timer = new HeartbeatTimer({
      heartbeat: makeDriver([]),
      orchestrator: makeOrchestrator().orchestrator,
      audit: () => undefined,
      scheduler,
      intervalMs: 1234,
    });
    timer.start({ profile: PROFILE, localPort: 17655 });
    expect(scheduler.pending[0]?.delayMs).toBe(1234);
  });

  test("running flag: false → start → true → stop → false", () => {
    const scheduler = makeScheduler();
    const timer = new HeartbeatTimer({
      heartbeat: makeDriver([]),
      orchestrator: makeOrchestrator().orchestrator,
      audit: () => undefined,
      scheduler,
    });
    expect(timer.running).toBe(false);
    timer.start({ profile: PROFILE, localPort: 17655 });
    expect(timer.running).toBe(true);
    timer.stop();
    expect(timer.running).toBe(false);
  });
});
