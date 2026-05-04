// hp-70yz — dirty-state store tests.

import { afterEach, beforeEach, expect, test } from "bun:test";
import {
  CLEAN_DIRTY_STATE,
  selectDirtyState,
  selectUpdatedAt,
  subscribeToCloneDirtyEvents,
  useDirtyStore,
} from "./index.ts";

beforeEach(() => {
  useDirtyStore.getState().clear();
  delete (globalThis as { window?: unknown }).window;
});

afterEach(() => {
  useDirtyStore.getState().clear();
  delete (globalThis as { window?: unknown }).window;
});

test("recordEvent + selectDirtyState round-trip", () => {
  useDirtyStore.getState().recordEvent("p1", {
    dirty: true,
    modifiedCount: 2,
    untrackedCount: 1,
    aheadCount: 0,
    behindCount: 0,
  });
  const state = selectDirtyState(useDirtyStore.getState(), "p1");
  expect(state?.dirty).toBe(true);
  expect(state?.modifiedCount).toBe(2);
});

test("selectDirtyState: returns null for unknown project + null projectId", () => {
  expect(selectDirtyState(useDirtyStore.getState(), "unknown")).toBeNull();
  expect(selectDirtyState(useDirtyStore.getState(), null)).toBeNull();
});

test("recordEvent: empty projectId is silently dropped", () => {
  useDirtyStore.getState().recordEvent("", CLEAN_DIRTY_STATE);
  expect(Object.keys(useDirtyStore.getState().entries)).toEqual([]);
});

test("recordEvent: stamps an updatedAt that selectUpdatedAt returns", () => {
  useDirtyStore.getState().recordEvent("p1", CLEAN_DIRTY_STATE, () => new Date("2026-05-04T03:00:00Z"));
  expect(selectUpdatedAt(useDirtyStore.getState(), "p1")).toBe("2026-05-04T03:00:00.000Z");
});

test("forget: drops the entry", () => {
  useDirtyStore.getState().recordEvent("p1", CLEAN_DIRTY_STATE);
  useDirtyStore.getState().forget("p1");
  expect(selectDirtyState(useDirtyStore.getState(), "p1")).toBeNull();
});

test("forget: missing project is a no-op", () => {
  expect(() => useDirtyStore.getState().forget("never-added")).not.toThrow();
});

test("clear: wipes everything", () => {
  useDirtyStore.getState().recordEvent("a", CLEAN_DIRTY_STATE);
  useDirtyStore.getState().recordEvent("b", CLEAN_DIRTY_STATE);
  useDirtyStore.getState().clear();
  expect(Object.keys(useDirtyStore.getState().entries)).toEqual([]);
});

test("subscribeToCloneDirtyEvents: returns no-op when no window", () => {
  const unsub = subscribeToCloneDirtyEvents(useDirtyStore.getState().recordEvent);
  expect(typeof unsub).toBe("function");
  expect(() => unsub()).not.toThrow();
});

test("subscribeToCloneDirtyEvents: returns no-op when no daemon bridge", () => {
  (globalThis as { window?: unknown }).window = {};
  const unsub = subscribeToCloneDirtyEvents(useDirtyStore.getState().recordEvent);
  expect(typeof unsub).toBe("function");
});

test("subscribeToCloneDirtyEvents: wires bridge subscription + records events", () => {
  let listener: ((payload: unknown) => void) | null = null;
  let unsubCalls = 0;
  (globalThis as { window?: unknown }).window = {
    hoopoe: {
      daemon: {
        subscribe: (topic: string, handler: (payload: unknown) => void) => {
          expect(topic).toBe("events.clone.dirty");
          listener = handler;
          return () => { unsubCalls += 1; };
        },
      },
    },
  };
  const unsub = subscribeToCloneDirtyEvents(useDirtyStore.getState().recordEvent);
  // Drive a payload through the bridge.
  listener!({
    projectId: "p1",
    state: { dirty: true, modifiedCount: 1, untrackedCount: 0, aheadCount: 0, behindCount: 0 },
  });
  expect(selectDirtyState(useDirtyStore.getState(), "p1")?.dirty).toBe(true);
  unsub();
  expect(unsubCalls).toBe(1);
});

test("subscribeToCloneDirtyEvents: ignores malformed payloads", () => {
  let listener: ((payload: unknown) => void) | null = null;
  (globalThis as { window?: unknown }).window = {
    hoopoe: {
      daemon: {
        subscribe: (_topic: string, handler: (payload: unknown) => void) => {
          listener = handler;
          return () => undefined;
        },
      },
    },
  };
  subscribeToCloneDirtyEvents(useDirtyStore.getState().recordEvent);
  listener!(null);
  listener!("garbage");
  listener!({ projectId: "" });
  listener!({ projectId: "p1" }); // missing state
  listener!({ projectId: "p1", state: null });
  expect(Object.keys(useDirtyStore.getState().entries)).toEqual([]);
});
