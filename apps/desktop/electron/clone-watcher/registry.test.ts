// hp-ndx5 — CloneWatcherRegistry tests.

import { afterEach, beforeEach, expect, test } from "bun:test";
import { mkdtempSync, mkdirSync, rmSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { EventEmitter } from "node:events";
import {
  cloneRepoPath,
  ensureCloneState,
  CLEAN_CLONE_STATE,
  type CloneStorageLayout,
} from "../clone/index.ts";
import {
  createCloneWatcherRegistry,
  createMockClock,
  type CloneWatcherEvent,
} from "./index.ts";

let tempRoot: string;
let layout: CloneStorageLayout;

beforeEach(() => {
  tempRoot = mkdtempSync(join(tmpdir(), "hoopoe-cwreg-"));
  layout = { projectsRoot: tempRoot };
});

afterEach(() => {
  rmSync(tempRoot, { recursive: true, force: true });
});

function fsWatchStub() {
  const created: Array<EventEmitter & { closed: boolean; close: () => void }> = [];
  const impl = ((_path: string, _options: unknown, _cb: unknown) => {
    const emitter = new EventEmitter() as EventEmitter & { closed: boolean; close: () => void };
    emitter.closed = false;
    emitter.close = () => { emitter.closed = true; };
    created.push(emitter);
    return emitter;
  }) as unknown as typeof import("node:fs").watch;
  return { impl, created };
}

function setupClone(projectId: string): void {
  ensureCloneState(layout, { projectId, originRemote: "git@github.com:o/r.git" });
  mkdirSync(cloneRepoPath(layout, projectId), { recursive: true });
}

test("registry.add: starts the watcher + returns it", () => {
  setupClone("p1");
  const registry = createCloneWatcherRegistry();
  const fake = fsWatchStub();
  const clock = createMockClock();
  const watcher = registry.add({
    projectId: "p1",
    layout,
    fsWatchImpl: fake.impl,
    clock,
    probeImpl: () => CLEAN_CLONE_STATE,
  });
  expect(watcher.status()).toBe("running");
  expect(registry.list().length).toBe(1);
});

test("registry.add: idempotent — second add for same projectId returns the existing watcher", () => {
  setupClone("p1");
  const registry = createCloneWatcherRegistry();
  const fake = fsWatchStub();
  const clock = createMockClock();
  const w1 = registry.add({ projectId: "p1", layout, fsWatchImpl: fake.impl, clock, probeImpl: () => CLEAN_CLONE_STATE });
  const w2 = registry.add({ projectId: "p1", layout, fsWatchImpl: fake.impl, clock, probeImpl: () => CLEAN_CLONE_STATE });
  expect(w1).toBe(w2);
  expect(fake.created.length).toBe(1); // only one fs handle
});

test("registry.add: multiple projects keep independent watchers", () => {
  setupClone("p1");
  setupClone("p2");
  setupClone("p3");
  const registry = createCloneWatcherRegistry();
  const fake = fsWatchStub();
  const clock = createMockClock();
  for (const id of ["p1", "p2", "p3"]) {
    registry.add({ projectId: id, layout, fsWatchImpl: fake.impl, clock, probeImpl: () => CLEAN_CLONE_STATE });
  }
  expect(registry.list().length).toBe(3);
  expect(fake.created.length).toBe(3);
});

test("registry.remove: stops + drops the watcher", () => {
  setupClone("p1");
  const registry = createCloneWatcherRegistry();
  const fake = fsWatchStub();
  const clock = createMockClock();
  registry.add({ projectId: "p1", layout, fsWatchImpl: fake.impl, clock, probeImpl: () => CLEAN_CLONE_STATE });
  registry.remove("p1");
  expect(registry.list().length).toBe(0);
  expect(fake.created[0]?.closed).toBe(true);
});

test("registry.remove: missing projectId is a no-op", () => {
  const registry = createCloneWatcherRegistry();
  expect(() => registry.remove("never-added")).not.toThrow();
});

test("registry.stopAll: closes every watcher", () => {
  setupClone("p1");
  setupClone("p2");
  const registry = createCloneWatcherRegistry();
  const fake = fsWatchStub();
  const clock = createMockClock();
  registry.add({ projectId: "p1", layout, fsWatchImpl: fake.impl, clock, probeImpl: () => CLEAN_CLONE_STATE });
  registry.add({ projectId: "p2", layout, fsWatchImpl: fake.impl, clock, probeImpl: () => CLEAN_CLONE_STATE });
  registry.stopAll();
  expect(registry.list().length).toBe(0);
  expect(fake.created.every((w) => w.closed)).toBe(true);
});

test("registry.subscribe: forwards events from every watcher", () => {
  setupClone("p1");
  setupClone("p2");
  const registry = createCloneWatcherRegistry();
  const fake = fsWatchStub();
  const clock = createMockClock();
  const events: CloneWatcherEvent[] = [];
  const unsubscribe = registry.subscribe((e) => events.push(e));
  registry.add({ projectId: "p1", layout, fsWatchImpl: fake.impl, clock, probeImpl: () => CLEAN_CLONE_STATE });
  registry.add({ projectId: "p2", layout, fsWatchImpl: fake.impl, clock, probeImpl: () => CLEAN_CLONE_STATE });
  // Two `started` events — one per watcher.
  expect(events.filter((e) => e.kind === "started").map((e) => e.projectId).sort()).toEqual(["p1", "p2"]);
  unsubscribe();
  registry.add({ projectId: "p3", layout, fsWatchImpl: fake.impl, clock, probeImpl: () => CLEAN_CLONE_STATE });
  // After unsubscribe, no new events recorded.
  expect(events.filter((e) => e.kind === "started").length).toBe(2);
});

test("registry.subscribe: a throwing listener doesn't break the broadcast", () => {
  setupClone("p1");
  const registry = createCloneWatcherRegistry();
  const fake = fsWatchStub();
  const clock = createMockClock();
  const safeEvents: CloneWatcherEvent[] = [];
  registry.subscribe(() => { throw new Error("listener bug"); });
  registry.subscribe((e) => safeEvents.push(e));
  registry.add({ projectId: "p1", layout, fsWatchImpl: fake.impl, clock, probeImpl: () => CLEAN_CLONE_STATE });
  expect(safeEvents.length).toBe(1);
});
