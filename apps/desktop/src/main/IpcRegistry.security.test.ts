// hp-rflj DOD: "renderer attempts privileged op → preload rejects + logs
// the attempt as a security event."
//
// The preload's runtime guard is the first line of defense (covered by
// preload.contract.test.ts). This file pins the SECOND line: when a
// dispatch reaches the IpcRegistry with a non-allowlisted / unknown /
// when-clause-blocked command id, the registry emits an
// `IpcSecurityEvent` so higher-level wiring can log it through the
// structured logger (hp-lxs / hp-je1p).
//
// Production wires the sink to the logger at warn level with
// `subsystem: "ipc.security"`. Tests inject a spy.

import { expect, test } from "bun:test";
import {
  IpcRegistry,
  type IpcSecurityEvent,
  type IpcSecurityEventSink,
} from "./IpcRegistry.ts";
import { INTERNAL_IPC_COMMANDS } from "../shared/ipc-contract.ts";

interface SpySink {
  readonly sink: IpcSecurityEventSink;
  readonly events: () => readonly IpcSecurityEvent[];
  readonly reset: () => void;
}

function spy(): SpySink {
  let captured: IpcSecurityEvent[] = [];
  return {
    sink: (event: IpcSecurityEvent) => {
      captured.push(event);
    },
    events: () => captured.slice(),
    reset: () => {
      captured = [];
    },
  };
}

test("dispatch of a command outside the allowlist emits channel-not-allowlisted at the dispatch stage", async () => {
  const s = spy();
  const registry = new IpcRegistry({ onSecurityEvent: s.sink });
  await expect(registry.dispatch("attacker.controlled.method", {})).rejects.toThrow();
  const events = s.events();
  expect(events.length).toBe(1);
  expect(events[0].kind).toBe("channel-not-allowlisted");
  expect(events[0].commandId).toBe("attacker.controlled.method");
  expect(events[0].stage).toBe("dispatch");
});

test("registering a command outside the allowlist emits channel-not-allowlisted at the register stage", () => {
  const s = spy();
  const registry = new IpcRegistry({ onSecurityEvent: s.sink });
  expect(() =>
    registry.register({
      id: "evil.method",
      handler: { handle: async () => null },
    }),
  ).toThrow();
  const events = s.events();
  expect(events.length).toBe(1);
  expect(events[0].kind).toBe("channel-not-allowlisted");
  expect(events[0].commandId).toBe("evil.method");
  expect(events[0].stage).toBe("register");
});

test("dispatch of an allowlisted but unregistered command emits command-not-registered", async () => {
  const s = spy();
  const registry = new IpcRegistry({ onSecurityEvent: s.sink });
  await expect(registry.dispatch(INTERNAL_IPC_COMMANDS.testUnregistered, {})).rejects.toThrow();
  const events = s.events();
  expect(events.length).toBe(1);
  expect(events[0].kind).toBe("command-not-registered");
  expect(events[0].commandId).toBe(INTERNAL_IPC_COMMANDS.testUnregistered);
});

test("dispatch with missing when-clause context emits command-not-eligible with the missing keys", async () => {
  const s = spy();
  const registry = new IpcRegistry({ onSecurityEvent: s.sink });
  registry.register({
    id: INTERNAL_IPC_COMMANDS.testGated,
    handler: { handle: async () => "ok" },
    whenContextKeys: ["mockFlywheel"],
  });
  await expect(
    registry.dispatch(INTERNAL_IPC_COMMANDS.testGated, {}, { mockFlywheel: false }),
  ).rejects.toThrow();
  const events = s.events();
  expect(events.length).toBe(1);
  expect(events[0].kind).toBe("command-not-eligible");
  expect(events[0].missingContextKeys).toEqual(["mockFlywheel"]);
});

test("happy-path dispatch does not emit any security event", async () => {
  const s = spy();
  const registry = new IpcRegistry({ onSecurityEvent: s.sink });
  registry.register({
    id: INTERNAL_IPC_COMMANDS.testHealthy,
    handler: { handle: async () => "ok" },
  });
  const result = await registry.dispatch(INTERNAL_IPC_COMMANDS.testHealthy, {});
  expect(result).toBe("ok");
  expect(s.events().length).toBe(0);
});

test("a sink that throws does not block the registry's own throw", async () => {
  const exploding: IpcSecurityEventSink = () => {
    throw new Error("sink boom");
  };
  const registry = new IpcRegistry({ onSecurityEvent: exploding });
  // The dispatch itself must still throw (registry rejection wins).
  await expect(registry.dispatch("attacker.method", {})).rejects.toThrow();
});

test("registry without a sink remains backward-compatible (no-op on rejection)", async () => {
  const registry = new IpcRegistry();
  await expect(registry.dispatch("attacker.method", {})).rejects.toThrow();
  // No sink, no observable side effect — only the throw.
});
