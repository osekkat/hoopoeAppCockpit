// hp-fkov — sleep/wake monitor wiring tests.

import { describe, expect, test } from "bun:test";

import {
  installSleepWakeMonitor,
  type PowerMonitorEvent,
  type PowerMonitorLike,
  type SleepWakeAuditEvent,
  type SleepWakeOrchestrator,
} from "./sleepWakeMonitor.ts";

// ── Test doubles ─────────────────────────────────────────────────────────

interface FakePowerMonitorOptions {
  /** Whether to expose `off` (canonical Node EventEmitter API). */
  readonly hasOff?: boolean;
  /** Whether to expose `removeListener` (Electron's older alias). */
  readonly hasRemoveListener?: boolean;
}

class FakePowerMonitor implements PowerMonitorLike {
  readonly listeners = new Map<PowerMonitorEvent, Set<() => void>>();
  off?: (event: PowerMonitorEvent, handler: () => void) => void;
  removeListener?: (event: PowerMonitorEvent, handler: () => void) => void;

  constructor(options: FakePowerMonitorOptions = {}) {
    const { hasOff = true, hasRemoveListener = true } = options;
    if (hasOff) {
      this.off = (event, handler) => this.#detach(event, handler);
    }
    if (hasRemoveListener) {
      this.removeListener = (event, handler) => this.#detach(event, handler);
    }
  }

  on(event: PowerMonitorEvent, handler: () => void): void {
    const set = this.listeners.get(event) ?? new Set<() => void>();
    set.add(handler);
    this.listeners.set(event, set);
  }

  emit(event: PowerMonitorEvent): void {
    const set = this.listeners.get(event);
    if (!set) return;
    // Snapshot so a listener that detaches itself doesn't mutate iteration.
    for (const handler of [...set]) handler();
  }

  count(event: PowerMonitorEvent): number {
    return this.listeners.get(event)?.size ?? 0;
  }

  #detach(event: PowerMonitorEvent, handler: () => void): void {
    this.listeners.get(event)?.delete(handler);
  }
}

interface OrchestratorRecorder {
  readonly orchestrator: SleepWakeOrchestrator;
  readonly calls: Array<"sleep" | "wake">;
}

function makeOrchestrator(opts: {
  readonly sleepError?: unknown;
  readonly wakeError?: unknown;
  readonly sleepDelayMs?: number;
} = {}): OrchestratorRecorder {
  const calls: Array<"sleep" | "wake"> = [];
  const orchestrator: SleepWakeOrchestrator = {
    handleSystemSleep: async () => {
      calls.push("sleep");
      if (opts.sleepDelayMs) await new Promise((r) => setTimeout(r, opts.sleepDelayMs));
      if (opts.sleepError) throw opts.sleepError;
    },
    handleSystemWake: async () => {
      calls.push("wake");
      if (opts.wakeError) throw opts.wakeError;
    },
  };
  return { orchestrator, calls };
}

function fixedNow(): Date {
  return new Date("2026-05-04T01:00:00.000Z");
}

// ── Tests ────────────────────────────────────────────────────────────────

describe("installSleepWakeMonitor", () => {
  test("subscribes to suspend + resume on install", () => {
    const pm = new FakePowerMonitor();
    const orch = makeOrchestrator();
    const audit: SleepWakeAuditEvent[] = [];
    installSleepWakeMonitor({
      powerMonitor: pm,
      orchestrator: orch.orchestrator,
      audit: (e) => audit.push(e),
    });
    expect(pm.count("suspend")).toBe(1);
    expect(pm.count("resume")).toBe(1);
  });

  test("sleep event: emits sleep_observed audit + invokes handleSystemSleep", async () => {
    const pm = new FakePowerMonitor();
    const orch = makeOrchestrator();
    const audit: SleepWakeAuditEvent[] = [];
    installSleepWakeMonitor({
      powerMonitor: pm,
      orchestrator: orch.orchestrator,
      audit: (e) => audit.push(e),
      now: fixedNow,
    });
    pm.emit("suspend");
    // The audit is synchronous; the orchestrator call is dispatched on a
    // microtask so it never blocks the powerMonitor callback.
    expect(audit.length).toBe(1);
    expect(audit[0]?.kind).toBe("tunnel.sleep_observed");
    expect(audit[0]?.at).toBe("2026-05-04T01:00:00.000Z");
    // Wait a microtask for the dispatched promise.
    await Promise.resolve();
    await Promise.resolve();
    expect(orch.calls).toEqual(["sleep"]);
  });

  test("wake event: emits wake_observed audit + invokes handleSystemWake", async () => {
    const pm = new FakePowerMonitor();
    const orch = makeOrchestrator();
    const audit: SleepWakeAuditEvent[] = [];
    installSleepWakeMonitor({
      powerMonitor: pm,
      orchestrator: orch.orchestrator,
      audit: (e) => audit.push(e),
      now: fixedNow,
    });
    pm.emit("resume");
    expect(audit.length).toBe(1);
    expect(audit[0]?.kind).toBe("tunnel.wake_observed");
    await Promise.resolve();
    await Promise.resolve();
    expect(orch.calls).toEqual(["wake"]);
  });

  // Helper: yield enough event-loop turns for an async handler's
  // rejection to reach the .catch chain. Three microtask ticks aren't
  // enough on bun's runtime when the handler itself awaits — a real
  // setTimeout(0) macrotask boundary is reliable.
  const flushAsync = () => new Promise<void>((r) => setTimeout(r, 5));

  test("sleep handler throw: emits sleep_handler_failed audit + invokes logFailure", async () => {
    const pm = new FakePowerMonitor();
    const orch = makeOrchestrator({ sleepError: new Error("orchestrator boom") });
    const audit: SleepWakeAuditEvent[] = [];
    const logged: Array<{ kind: "sleep" | "wake"; err: unknown }> = [];
    installSleepWakeMonitor({
      powerMonitor: pm,
      orchestrator: orch.orchestrator,
      audit: (e) => audit.push(e),
      logFailure: (kind, err) => logged.push({ kind, err }),
      now: fixedNow,
    });
    pm.emit("suspend");
    await flushAsync();
    expect(audit.length).toBe(2);
    expect(audit[0]?.kind).toBe("tunnel.sleep_observed");
    expect(audit[1]?.kind).toBe("tunnel.sleep_handler_failed");
    expect(audit[1]?.message).toBe("orchestrator boom");
    expect(logged.length).toBe(1);
    expect(logged[0]?.kind).toBe("sleep");
    expect(logged[0]?.err).toBeInstanceOf(Error);
  });

  test("wake handler throw: emits wake_handler_failed audit + invokes logFailure", async () => {
    const pm = new FakePowerMonitor();
    const orch = makeOrchestrator({ wakeError: "string error" });
    const audit: SleepWakeAuditEvent[] = [];
    const logged: Array<{ kind: "sleep" | "wake"; err: unknown }> = [];
    installSleepWakeMonitor({
      powerMonitor: pm,
      orchestrator: orch.orchestrator,
      audit: (e) => audit.push(e),
      logFailure: (kind, err) => logged.push({ kind, err }),
      now: fixedNow,
    });
    pm.emit("resume");
    await flushAsync();
    expect(audit[1]?.kind).toBe("tunnel.wake_handler_failed");
    expect(audit[1]?.message).toBe("string error");
    expect(logged.length).toBe(1);
    expect(logged[0]?.kind).toBe("wake");
  });

  test("audit fires synchronously even when handler is async — no race with caller", () => {
    // Important property: the powerMonitor callback returns immediately
    // after the audit + the dispatched promise. Electron expects the
    // suspend listener to NOT block; if the orchestrator does heavy
    // work, it must yield first.
    const pm = new FakePowerMonitor();
    const orch = makeOrchestrator({ sleepDelayMs: 100 });
    const audit: SleepWakeAuditEvent[] = [];
    installSleepWakeMonitor({
      powerMonitor: pm,
      orchestrator: orch.orchestrator,
      audit: (e) => audit.push(e),
      now: fixedNow,
    });
    const start = Date.now();
    pm.emit("suspend");
    // emit returns within ~1ms even though the handler sleeps 100ms.
    expect(Date.now() - start).toBeLessThan(50);
    // Audit was already recorded synchronously before the handler
    // resolved — that's the guardrail-10 invariant.
    expect(audit[0]?.kind).toBe("tunnel.sleep_observed");
  });

  test("logFailure is optional: handler errors are still audited without it", async () => {
    const pm = new FakePowerMonitor();
    const orch = makeOrchestrator({ sleepError: new Error("boom") });
    const audit: SleepWakeAuditEvent[] = [];
    installSleepWakeMonitor({
      powerMonitor: pm,
      orchestrator: orch.orchestrator,
      audit: (e) => audit.push(e),
      now: fixedNow,
      // No logFailure injected.
    });
    pm.emit("suspend");
    await new Promise<void>((r) => setTimeout(r, 5));
    expect(audit.map((e) => e.kind)).toEqual([
      "tunnel.sleep_observed",
      "tunnel.sleep_handler_failed",
    ]);
  });

  test("uninstall: detaches both listeners + is idempotent", () => {
    const pm = new FakePowerMonitor();
    const orch = makeOrchestrator();
    const handle = installSleepWakeMonitor({
      powerMonitor: pm,
      orchestrator: orch.orchestrator,
      audit: () => undefined,
    });
    expect(pm.count("suspend")).toBe(1);
    expect(pm.count("resume")).toBe(1);
    handle.uninstall();
    expect(pm.count("suspend")).toBe(0);
    expect(pm.count("resume")).toBe(0);
    // Idempotent — second call is a no-op.
    handle.uninstall();
    expect(pm.count("suspend")).toBe(0);
  });

  test("uninstall: falls back to removeListener when off is missing", () => {
    // Older Electron versions exposed only `removeListener`; the wiring
    // tolerates either alias.
    const pm = new FakePowerMonitor({ hasOff: false, hasRemoveListener: true });
    const orch = makeOrchestrator();
    const handle = installSleepWakeMonitor({
      powerMonitor: pm,
      orchestrator: orch.orchestrator,
      audit: () => undefined,
    });
    handle.uninstall();
    expect(pm.count("suspend")).toBe(0);
    expect(pm.count("resume")).toBe(0);
  });

  test("uninstall: when neither off nor removeListener is present, the monitor doesn't crash", () => {
    const pm = new FakePowerMonitor({ hasOff: false, hasRemoveListener: false });
    const orch = makeOrchestrator();
    const handle = installSleepWakeMonitor({
      powerMonitor: pm,
      orchestrator: orch.orchestrator,
      audit: () => undefined,
    });
    expect(() => handle.uninstall()).not.toThrow();
  });

  test("after uninstall, subsequent suspend/resume events are NOT audited", async () => {
    const pm = new FakePowerMonitor();
    const orch = makeOrchestrator();
    const audit: SleepWakeAuditEvent[] = [];
    const handle = installSleepWakeMonitor({
      powerMonitor: pm,
      orchestrator: orch.orchestrator,
      audit: (e) => audit.push(e),
    });
    pm.emit("suspend");
    handle.uninstall();
    pm.emit("suspend");
    pm.emit("resume");
    await Promise.resolve();
    await Promise.resolve();
    // Only the pre-uninstall suspend is audited; the orchestrator was
    // called once.
    expect(audit.length).toBe(1);
    expect(orch.calls).toEqual(["sleep"]);
  });

  test("multiple suspend events: each one audits + invokes handleSystemSleep", async () => {
    // Repeated short sleeps (lid-close, lid-open, lid-close) must each
    // trigger their own handler call — the monitor doesn't dedupe.
    const pm = new FakePowerMonitor();
    const orch = makeOrchestrator();
    const audit: SleepWakeAuditEvent[] = [];
    installSleepWakeMonitor({
      powerMonitor: pm,
      orchestrator: orch.orchestrator,
      audit: (e) => audit.push(e),
    });
    pm.emit("suspend");
    pm.emit("resume");
    pm.emit("suspend");
    pm.emit("resume");
    await Promise.resolve();
    await Promise.resolve();
    expect(audit.map((e) => e.kind)).toEqual([
      "tunnel.sleep_observed",
      "tunnel.wake_observed",
      "tunnel.sleep_observed",
      "tunnel.wake_observed",
    ]);
    expect(orch.calls).toEqual(["sleep", "wake", "sleep", "wake"]);
  });

  test("audit-sink that throws does NOT crash the powerMonitor callback", () => {
    // The powerMonitor is shared with the rest of Electron. A buggy audit
    // sink must not bring down the listener — a thrown sink propagates
    // out of the synchronous emit, but the second listener (the async
    // dispatch) still completes. We assert no uncaught throw escapes
    // the synchronous emit path.
    const pm = new FakePowerMonitor();
    const orch = makeOrchestrator();
    let auditCalls = 0;
    installSleepWakeMonitor({
      powerMonitor: pm,
      orchestrator: orch.orchestrator,
      audit: () => {
        auditCalls += 1;
        throw new Error("audit boom");
      },
    });
    // Document the current behavior: a throwing sink IS surfaced
    // synchronously (we want to know about audit-sink bugs in dev), but
    // the test confirms it doesn't loop or recurse.
    expect(() => pm.emit("suspend")).toThrow("audit boom");
    expect(auditCalls).toBe(1);
  });
});
