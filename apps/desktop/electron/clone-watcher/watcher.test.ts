// hp-ndx5 — CloneWatcher tests with injected fs.watch + mock clock + fake probe.

import { afterEach, beforeEach, expect, test } from "bun:test";
import { mkdtempSync, mkdirSync, rmSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { type FSWatcher } from "node:fs";
import { EventEmitter } from "node:events";
import {
  cloneRepoPath,
  ensureCloneState,
  readCloneState,
  type CloneStorageLayout,
  CLEAN_CLONE_STATE,
  type CloneDirtyState,
} from "../clone/index.ts";
import {
  createCloneWatcher,
  createMockClock,
  type CloneWatcherEvent,
} from "./index.ts";

let tempRoot: string;
let layout: CloneStorageLayout;

beforeEach(() => {
  tempRoot = mkdtempSync(join(tmpdir(), "hoopoe-cwatch-"));
  layout = { projectsRoot: tempRoot };
});

afterEach(() => {
  rmSync(tempRoot, { recursive: true, force: true });
});

interface FakeFsWatcher extends EventEmitter {
  closed: boolean;
  triggerChange: (filename: string) => void;
  close: () => void;
}

function createFakeFsWatch() {
  const created: FakeFsWatcher[] = [];
  let nextThrow: Error | null = null;
  function makeWatcher(callback: (event: string, filename: string) => void): FakeFsWatcher {
    const emitter = new EventEmitter() as FakeFsWatcher;
    emitter.closed = false;
    emitter.close = () => { emitter.closed = true; };
    emitter.triggerChange = (filename: string) => callback("change", filename);
    return emitter;
  }
  const fsWatchImpl = ((_path: string, _options: unknown, callback: (event: string, filename: string) => void) => {
    if (nextThrow) {
      const err = nextThrow;
      nextThrow = null;
      throw err;
    }
    const watcher = makeWatcher(callback);
    created.push(watcher);
    return watcher;
  }) as unknown as typeof import("node:fs").watch;
  return {
    fsWatchImpl,
    created,
    setNextThrow: (err: Error) => { nextThrow = err; },
  };
}

function setupClone(projectId: string): void {
  ensureCloneState(layout, { projectId, originRemote: "git@github.com:o/r.git" });
  // Repo dir must exist for the watcher to attach.
  mkdirSync(cloneRepoPath(layout, projectId), { recursive: true });
}

test("CloneWatcher: start emits 'started' + status transitions to running", () => {
  setupClone("p1");
  const events: CloneWatcherEvent[] = [];
  const fake = createFakeFsWatch();
  const clock = createMockClock();
  const watcher = createCloneWatcher({
    projectId: "p1",
    layout,
    fsWatchImpl: fake.fsWatchImpl,
    clock,
    probeImpl: () => CLEAN_CLONE_STATE,
    onEvent: (e) => events.push(e),
  });
  watcher.start();
  expect(watcher.status()).toBe("running");
  expect(events.length).toBe(1);
  expect(events[0]).toEqual({ kind: "started", projectId: "p1" });
});

test("CloneWatcher: fs change debounced into a single dirty probe", () => {
  setupClone("p1");
  const events: CloneWatcherEvent[] = [];
  const fake = createFakeFsWatch();
  const clock = createMockClock();
  let probeCalls = 0;
  const dirty: CloneDirtyState = {
    dirty: true,
    modifiedCount: 1,
    untrackedCount: 0,
    aheadCount: 0,
    behindCount: 0,
  };
  const watcher = createCloneWatcher({
    projectId: "p1",
    layout,
    debounceMs: 100,
    fsWatchImpl: fake.fsWatchImpl,
    clock,
    probeImpl: () => { probeCalls += 1; return dirty; },
    onEvent: (e) => events.push(e),
  });
  watcher.start();
  // Burst of fs events.
  fake.created[0]?.triggerChange("file.ts");
  fake.created[0]?.triggerChange("file.ts");
  fake.created[0]?.triggerChange("file.ts");
  expect(probeCalls).toBe(0); // not yet
  clock.tick(99);
  expect(probeCalls).toBe(0);
  clock.tick(1);
  expect(probeCalls).toBe(1);
  // dirty event emitted with the new state.
  const dirtyEvents = events.filter((e) => e.kind === "dirty");
  expect(dirtyEvents.length).toBe(1);
  expect(dirtyEvents[0]).toEqual({ kind: "dirty", projectId: "p1", state: dirty });
});

test("CloneWatcher: probe updates clone-state.json dirtyState on disk", () => {
  setupClone("p1");
  const fake = createFakeFsWatch();
  const clock = createMockClock();
  const dirty: CloneDirtyState = {
    dirty: true,
    modifiedCount: 2,
    untrackedCount: 1,
    aheadCount: 0,
    behindCount: 0,
  };
  const watcher = createCloneWatcher({
    projectId: "p1",
    layout,
    debounceMs: 50,
    fsWatchImpl: fake.fsWatchImpl,
    clock,
    probeImpl: () => dirty,
  });
  watcher.start();
  watcher.probeNow();
  const persisted = readCloneState(layout, "p1");
  expect(persisted?.dirtyState).toEqual(dirty);
});

test("CloneWatcher: stop cancels pending probe + closes fs handle", () => {
  setupClone("p1");
  const fake = createFakeFsWatch();
  const clock = createMockClock();
  let probeCalls = 0;
  const watcher = createCloneWatcher({
    projectId: "p1",
    layout,
    debounceMs: 100,
    fsWatchImpl: fake.fsWatchImpl,
    clock,
    probeImpl: () => { probeCalls += 1; return CLEAN_CLONE_STATE; },
  });
  watcher.start();
  fake.created[0]?.triggerChange("file.ts");
  watcher.stop();
  clock.tick(500);
  expect(probeCalls).toBe(0);
  expect(fake.created[0]?.closed).toBe(true);
  expect(watcher.status()).toBe("stopped");
});

test("CloneWatcher: fs.watch error → emits error + retries once", () => {
  setupClone("p1");
  const events: CloneWatcherEvent[] = [];
  const fake = createFakeFsWatch();
  const clock = createMockClock();
  fake.setNextThrow(new Error("ENOENT: no such file or directory"));
  const watcher = createCloneWatcher({
    projectId: "p1",
    layout,
    fsWatchImpl: fake.fsWatchImpl,
    clock,
    retryDelayMs: 1_000,
    probeImpl: () => CLEAN_CLONE_STATE,
    onEvent: (e) => events.push(e),
  });
  watcher.start();
  // First attempt failed; status == error; one error event recorded.
  expect(watcher.status()).toBe("error");
  expect(events.filter((e) => e.kind === "error").length).toBe(1);
  // Wait for retry; second attempt succeeds (no more nextThrow).
  clock.tick(1_000);
  expect(watcher.status()).toBe("running");
  expect(events.filter((e) => e.kind === "started").length).toBe(1);
});

test("CloneWatcher: persistent fs.watch failure → 'unrecoverable' stop after one retry", () => {
  setupClone("p1");
  const events: CloneWatcherEvent[] = [];
  const fake = createFakeFsWatch();
  const clock = createMockClock();
  // Throw on every fsWatchImpl call.
  const alwaysFailing = ((_path: string, _options: unknown, _cb: unknown) => {
    throw new Error("EACCES: permission denied");
  }) as unknown as typeof import("node:fs").watch;
  const watcher = createCloneWatcher({
    projectId: "p1",
    layout,
    fsWatchImpl: alwaysFailing,
    clock,
    retryDelayMs: 100,
    probeImpl: () => CLEAN_CLONE_STATE,
    onEvent: (e) => events.push(e),
  });
  watcher.start();
  expect(watcher.status()).toBe("error");
  // Retry fires after retryDelayMs and also fails.
  clock.tick(100);
  expect(watcher.status()).toBe("stopped");
  const stopped = events.find((e) => e.kind === "stopped");
  expect(stopped).toEqual({ kind: "stopped", projectId: "p1", reason: "unrecoverable" });
});

test("CloneWatcher: probe-time error emits error event but doesn't stop the watcher", () => {
  setupClone("p1");
  const events: CloneWatcherEvent[] = [];
  const fake = createFakeFsWatch();
  const clock = createMockClock();
  const watcher = createCloneWatcher({
    projectId: "p1",
    layout,
    fsWatchImpl: fake.fsWatchImpl,
    clock,
    probeImpl: () => { throw new Error("git not installed"); },
    onEvent: (e) => events.push(e),
  });
  watcher.start();
  watcher.probeNow();
  expect(watcher.status()).toBe("running");
  const error = events.find((e) => e.kind === "error");
  expect(error?.kind).toBe("error");
  if (error?.kind === "error") {
    expect(error.code).toBe("probe_failed");
  }
});

test("CloneWatcher: start() is idempotent", () => {
  setupClone("p1");
  const fake = createFakeFsWatch();
  const clock = createMockClock();
  const watcher = createCloneWatcher({
    projectId: "p1",
    layout,
    fsWatchImpl: fake.fsWatchImpl,
    clock,
    probeImpl: () => CLEAN_CLONE_STATE,
  });
  watcher.start();
  watcher.start();
  watcher.start();
  // Only one fs.watch handle should have been created.
  expect(fake.created.length).toBe(1);
});

test("CloneWatcher: stop() is idempotent", () => {
  setupClone("p1");
  const fake = createFakeFsWatch();
  const clock = createMockClock();
  const watcher = createCloneWatcher({
    projectId: "p1",
    layout,
    fsWatchImpl: fake.fsWatchImpl,
    clock,
    probeImpl: () => CLEAN_CLONE_STATE,
  });
  watcher.start();
  watcher.stop();
  watcher.stop();
  watcher.stop();
  expect(watcher.status()).toBe("stopped");
});
