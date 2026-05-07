// hp-27d3 unit tests for the Electron main-process lifecycle wiring.
// Exercises the pure logic in `./ElectronLifecycle.ts` via a stub
// `app`. The real `electron` module is never imported — bun:test
// can't load it.

import { expect, test } from "bun:test";
import {
  resolveRendererTarget,
  wireMainProcessLifecycle,
  type ElectronAppLike,
  type RendererTarget,
} from "./ElectronLifecycle.ts";

// ── stub Electron app ─────────────────────────────────────────────

interface Deferred<T> {
  readonly promise: Promise<T>;
  resolve(value: T): void;
}

function deferred<T>(): Deferred<T> {
  let resolveFn!: (value: T) => void;
  const promise = new Promise<T>((r) => {
    resolveFn = r;
  });
  return { promise, resolve: resolveFn };
}

class StubApp implements ElectronAppLike {
  whenReadyDeferred = deferred<void>();
  listeners = new Map<string, (...args: unknown[]) => unknown>();
  quitCount = 0;
  hasLockResult = true;

  requestSingleInstanceLock = (): boolean => this.hasLockResult;
  whenReady = (): Promise<void> => this.whenReadyDeferred.promise;
  on = (
    event: string,
    listener: (...args: unknown[]) => unknown,
  ): void => {
    this.listeners.set(event, listener);
  };
  quit = (): void => {
    this.quitCount += 1;
  };
}

// ── resolveRendererTarget ────────────────────────────────────────

test("resolveRendererTarget: HOOPOE_VITE_URL wins over NODE_ENV", () => {
  const got = resolveRendererTarget({
    env: { HOOPOE_VITE_URL: "http://127.0.0.1:5173", NODE_ENV: "production" },
    distElectronDir: "/app/dist-electron",
  });
  expect(got.url).toBe("http://127.0.0.1:5173");
  expect(got.isDev).toBe(true);
});

test("resolveRendererTarget: NODE_ENV=development falls back to localhost:5173", () => {
  const got = resolveRendererTarget({
    env: { NODE_ENV: "development" },
    distElectronDir: "/app/dist-electron",
  });
  expect(got.url).toBe("http://localhost:5173");
  expect(got.isDev).toBe(true);
});

test("resolveRendererTarget: production resolves to file://<appRoot>/dist/index.html", () => {
  const got = resolveRendererTarget({
    env: {},
    distElectronDir: "/app/dist-electron",
  });
  expect(got.url).toBe("file:///app/dist/index.html");
  expect(got.isDev).toBe(false);
});

test("resolveRendererTarget: trailing slash on distElectronDir is normalised", () => {
  const got = resolveRendererTarget({
    env: {},
    distElectronDir: "/app/dist-electron/",
  });
  expect(got.url).toBe("file:///app/dist/index.html");
});

test("resolveRendererTarget: empty HOOPOE_VITE_URL falls through (does not match dev mode)", () => {
  const got = resolveRendererTarget({
    env: { HOOPOE_VITE_URL: "", NODE_ENV: "production" },
    distElectronDir: "/app/dist-electron",
  });
  expect(got.url).toBe("file:///app/dist/index.html");
  expect(got.isDev).toBe(false);
});

// ── wireMainProcessLifecycle ─────────────────────────────────────

const TARGET: RendererTarget = { url: "http://localhost:5173", isDev: true };

interface ScenarioOpts {
  readonly hasLock?: boolean;
  readonly platform?: NodeJS.Platform;
  readonly revealResults?: readonly boolean[];
}

interface Scenario {
  readonly stubApp: StubApp;
  readonly wired: ReturnType<typeof wireMainProcessLifecycle>;
  readonly counts: {
    open: number;
    bootstrap: number;
    shutdown: number;
    reveal: number;
  };
}

function scenario(opts: ScenarioOpts = {}): Scenario {
  const stubApp = new StubApp();
  if (opts.hasLock === false) stubApp.hasLockResult = false;
  const counts = { open: 0, bootstrap: 0, shutdown: 0, reveal: 0 };
  const revealResults = opts.revealResults ?? [true];
  const wired = wireMainProcessLifecycle({
    app: stubApp,
    platform: opts.platform ?? "darwin",
    target: TARGET,
    bootstrap: async () => {
      counts.bootstrap += 1;
      return {
        shutdown: async () => {
          counts.shutdown += 1;
        },
      };
    },
    openMainWindow: (target) => {
      expect(target).toBe(TARGET);
      counts.open += 1;
    },
    revealExisting: () => {
      const idx = counts.reveal;
      counts.reveal += 1;
      return revealResults[idx] ?? false;
    },
  });
  return { stubApp, wired, counts };
}

test("whenReady triggers bootstrapDesktop then createMainWindow", async () => {
  const s = scenario();
  expect(s.wired.hasLock).toBe(true);
  expect(s.counts.bootstrap).toBe(0);
  expect(s.counts.open).toBe(0);
  s.stubApp.whenReadyDeferred.resolve();
  await s.wired.readyPromise;
  expect(s.counts.bootstrap).toBe(1);
  expect(s.counts.open).toBe(1);
});

test("window-all-closed quits on linux", async () => {
  const s = scenario({ platform: "linux" });
  s.stubApp.whenReadyDeferred.resolve();
  await s.wired.readyPromise;
  const handler = s.wired.handlers.get("window-all-closed");
  expect(handler).toBeDefined();
  handler!();
  expect(s.stubApp.quitCount).toBe(1);
});

test("window-all-closed quits on win32", async () => {
  const s = scenario({ platform: "win32" });
  s.stubApp.whenReadyDeferred.resolve();
  await s.wired.readyPromise;
  s.wired.handlers.get("window-all-closed")!();
  expect(s.stubApp.quitCount).toBe(1);
});

test("window-all-closed does NOT quit on darwin", async () => {
  const s = scenario({ platform: "darwin" });
  s.stubApp.whenReadyDeferred.resolve();
  await s.wired.readyPromise;
  s.wired.handlers.get("window-all-closed")!();
  expect(s.stubApp.quitCount).toBe(0);
});

test("second-instance focuses the existing window via revealExisting", async () => {
  const s = scenario();
  s.stubApp.whenReadyDeferred.resolve();
  await s.wired.readyPromise;
  s.wired.handlers.get("second-instance")!();
  expect(s.counts.reveal).toBe(1);
});

test("activate on darwin focuses the existing window when revealExisting succeeds", async () => {
  const s = scenario({ platform: "darwin", revealResults: [true] });
  s.stubApp.whenReadyDeferred.resolve();
  await s.wired.readyPromise;
  // initial open from whenReady is the only one; activate hands off
  // to revealExisting which returns true.
  s.wired.handlers.get("activate")!();
  expect(s.counts.open).toBe(1);
  expect(s.counts.reveal).toBe(1);
});

test("activate on darwin re-opens the window when no windows remain", async () => {
  const s = scenario({ platform: "darwin", revealResults: [false] });
  s.stubApp.whenReadyDeferred.resolve();
  await s.wired.readyPromise;
  s.wired.handlers.get("activate")!();
  // revealExisting returned false → openMainWindow fires again.
  expect(s.counts.open).toBe(2);
});

test("before-quit awaits bootstrapResult.shutdown", async () => {
  const s = scenario();
  s.stubApp.whenReadyDeferred.resolve();
  await s.wired.readyPromise;
  await s.wired.handlers.get("before-quit")!();
  expect(s.counts.shutdown).toBe(1);
});

test("before-quit fired twice still only calls shutdown once", async () => {
  const s = scenario();
  s.stubApp.whenReadyDeferred.resolve();
  await s.wired.readyPromise;
  await s.wired.handlers.get("before-quit")!();
  await s.wired.handlers.get("before-quit")!();
  expect(s.counts.shutdown).toBe(1);
});

test("before-quit is a no-op when bootstrap hasn't completed yet", async () => {
  // before-quit can fire before whenReady (e.g., a fast SIGTERM
  // interrupting startup). Asserting we don't throw is the contract.
  const s = scenario();
  await s.wired.handlers.get("before-quit")!();
  expect(s.counts.shutdown).toBe(0);
});

test("second instance: requestSingleInstanceLock=false quits and skips wiring", () => {
  const s = scenario({ hasLock: false });
  expect(s.wired.hasLock).toBe(false);
  expect(s.stubApp.quitCount).toBe(1);
  expect(s.wired.handlers.size).toBe(0);
  expect(s.counts.bootstrap).toBe(0);
});

test("returned handlers map mirrors the listeners attached to app.on", async () => {
  const s = scenario();
  expect(s.wired.handlers.has("second-instance")).toBe(true);
  expect(s.wired.handlers.has("activate")).toBe(true);
  expect(s.wired.handlers.has("window-all-closed")).toBe(true);
  expect(s.wired.handlers.has("before-quit")).toBe(true);
  for (const [event, handler] of s.wired.handlers) {
    expect(s.stubApp.listeners.get(event)).toBe(handler);
  }
});
