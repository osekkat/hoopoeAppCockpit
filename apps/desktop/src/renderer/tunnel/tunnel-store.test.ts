// hp-m79e — tunnel store tests.

import { afterEach, beforeEach, expect, test } from "bun:test";
import {
  INITIAL_TUNNEL_SNAPSHOT,
  hydrateFromBridge,
  selectTunnelSnapshot,
  selectVpsHealthDot,
  subscribeToTunnelEvents,
  useTunnelStore,
  type TunnelSnapshot,
} from "./index.ts";

const FIXED_NOW = () => new Date("2026-05-04T03:00:00.000Z");

const READY_SNAPSHOT: TunnelSnapshot = {
  state: "ready",
  activeProfileId: "profile-1",
  localPort: 17655,
  lastFault: null,
  reconnectAttempts: 0,
  nextRetryAt: null,
};

const RECONNECTING_SNAPSHOT: TunnelSnapshot = {
  state: "reconnecting",
  activeProfileId: "profile-1",
  localPort: null,
  lastFault: { code: "tunnel_dropped", message: "Tunnel closed unexpectedly", capturedAt: "2026-05-04T03:00:00.000Z" },
  reconnectAttempts: 2,
  nextRetryAt: "2026-05-04T03:00:30.000Z",
};

beforeEach(() => {
  useTunnelStore.getState().clear();
  delete (globalThis as { window?: unknown }).window;
});

afterEach(() => {
  useTunnelStore.getState().clear();
  delete (globalThis as { window?: unknown }).window;
});

test("default store: matches INITIAL_TUNNEL_SNAPSHOT", () => {
  const snap = selectTunnelSnapshot(useTunnelStore.getState());
  expect(snap).toEqual(INITIAL_TUNNEL_SNAPSHOT);
  expect(useTunnelStore.getState().receivedAt).toBeNull();
});

test("recordEvent: replaces snapshot + stamps receivedAt", () => {
  useTunnelStore.getState().recordEvent(READY_SNAPSHOT, FIXED_NOW);
  const after = useTunnelStore.getState();
  expect(selectTunnelSnapshot(after)).toEqual(READY_SNAPSHOT);
  expect(after.receivedAt).toBe("2026-05-04T03:00:00.000Z");
});

test("recordEvent: rejects malformed payloads (no state mutation)", () => {
  useTunnelStore.getState().recordEvent({ state: "bogus" } as unknown as TunnelSnapshot);
  expect(selectTunnelSnapshot(useTunnelStore.getState())).toEqual(INITIAL_TUNNEL_SNAPSHOT);
  // Garbage shapes also rejected.
  useTunnelStore.getState().recordEvent({ state: "ready", reconnectAttempts: "not a number" } as unknown as TunnelSnapshot);
  expect(selectTunnelSnapshot(useTunnelStore.getState())).toEqual(INITIAL_TUNNEL_SNAPSHOT);
});

test("recordEvent: malformed lastFault is rejected", () => {
  const broken: TunnelSnapshot = {
    ...RECONNECTING_SNAPSHOT,
    lastFault: { code: "x", message: 42 as unknown as string, capturedAt: "2026-05-04T03:00:00.000Z" },
  };
  useTunnelStore.getState().recordEvent(broken);
  expect(selectTunnelSnapshot(useTunnelStore.getState())).toEqual(INITIAL_TUNNEL_SNAPSHOT);
});

test("clear: resets to initial + drops receivedAt", () => {
  useTunnelStore.getState().recordEvent(READY_SNAPSHOT);
  useTunnelStore.getState().clear();
  expect(selectTunnelSnapshot(useTunnelStore.getState())).toEqual(INITIAL_TUNNEL_SNAPSHOT);
  expect(useTunnelStore.getState().receivedAt).toBeNull();
});

test("hydrateFromBridge: silent when no window", async () => {
  await hydrateFromBridge(useTunnelStore.getState().recordEvent);
  expect(selectTunnelSnapshot(useTunnelStore.getState())).toEqual(INITIAL_TUNNEL_SNAPSHOT);
});

test("hydrateFromBridge: silent when daemon.request is missing", async () => {
  (globalThis as { window?: unknown }).window = { hoopoe: {} };
  await hydrateFromBridge(useTunnelStore.getState().recordEvent);
  expect(selectTunnelSnapshot(useTunnelStore.getState())).toEqual(INITIAL_TUNNEL_SNAPSHOT);
});

test("hydrateFromBridge: invokes tunnel.snapshot + records the result", async () => {
  let called: { method: string; body: unknown } | null = null;
  (globalThis as { window?: unknown }).window = {
    hoopoe: {
      daemon: {
        request: async (method: string, body: unknown) => {
          called = { method, body };
          return READY_SNAPSHOT;
        },
      },
    },
  };
  await hydrateFromBridge(useTunnelStore.getState().recordEvent);
  expect(called).toEqual({ method: "tunnel.snapshot", body: null });
  expect(selectTunnelSnapshot(useTunnelStore.getState())).toEqual(READY_SNAPSHOT);
});

test("hydrateFromBridge: swallows errors", async () => {
  (globalThis as { window?: unknown }).window = {
    hoopoe: {
      daemon: {
        request: async () => {
          throw new Error("daemon unreachable");
        },
      },
    },
  };
  await hydrateFromBridge(useTunnelStore.getState().recordEvent);
  expect(selectTunnelSnapshot(useTunnelStore.getState())).toEqual(INITIAL_TUNNEL_SNAPSHOT);
});

test("subscribeToTunnelEvents: returns no-op when bridge missing", () => {
  const unsub = subscribeToTunnelEvents(useTunnelStore.getState().recordEvent);
  expect(typeof unsub).toBe("function");
  expect(() => unsub()).not.toThrow();
});

test("selectVpsHealthDot: returns unknown until the first snapshot lands (receivedAt === null)", () => {
  // Default store has receivedAt === null even though the snapshot
  // shape says "unconfigured" — we prefer reporting `unknown` over
  // `tunnelHealthDot('unconfigured')` so the topbar renders grey
  // instead of a misleading green/yellow before the orchestrator
  // pushes its first state.
  expect(selectVpsHealthDot(useTunnelStore.getState())).toBe("unknown");
});

test("selectVpsHealthDot: reflects the live FSM state after the first event lands", () => {
  useTunnelStore.getState().recordEvent(READY_SNAPSHOT, FIXED_NOW);
  expect(selectVpsHealthDot(useTunnelStore.getState())).toBe("healthy");

  useTunnelStore.getState().recordEvent(RECONNECTING_SNAPSHOT, FIXED_NOW);
  // tunnelHealthDot maps reconnecting → offline.
  expect(selectVpsHealthDot(useTunnelStore.getState())).toBe("offline");

  // Reset clears receivedAt → back to unknown.
  useTunnelStore.getState().clear();
  expect(selectVpsHealthDot(useTunnelStore.getState())).toBe("unknown");
});

test("subscribeToTunnelEvents: wires events.tunnel + records valid payloads", () => {
  let listener: ((payload: unknown) => void) | null = null;
  let unsubCount = 0;
  (globalThis as { window?: unknown }).window = {
    hoopoe: {
      daemon: {
        subscribe: (topic: string, cb: (payload: unknown) => void) => {
          expect(topic).toBe("events.tunnel");
          listener = cb;
          return () => { unsubCount += 1; };
        },
      },
    },
  };
  const unsub = subscribeToTunnelEvents(useTunnelStore.getState().recordEvent);
  listener!(RECONNECTING_SNAPSHOT);
  expect(selectTunnelSnapshot(useTunnelStore.getState())).toEqual(RECONNECTING_SNAPSHOT);
  // Garbage payload is silently dropped.
  listener!({ random: "garbage" });
  expect(selectTunnelSnapshot(useTunnelStore.getState())).toEqual(RECONNECTING_SNAPSHOT);
  unsub();
  expect(unsubCount).toBe(1);
});
