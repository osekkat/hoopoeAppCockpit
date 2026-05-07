// hp-1loj: tests for the daemon-spawn gate added to bootstrapDesktop.
//
// The gate decides whether bootstrapDesktop calls spawnBackend at
// all. hp-27d3's first cut unconditionally called spawnBackend with
// an empty path when HOOPOE_DAEMON_BIN was unset, which crashed
// child_process.spawn() with ERR_INVALID_ARG_VALUE during whenReady
// and prevented the renderer-only dev workflow from opening a
// window. hp-1loj wires the gate so empty / whitespace / undefined
// values produce a clean console.warn and a null backend handle.
//
// Tests target the pure helper (`shouldSpawnBackend`) for the input
// matrix and `bootstrapDesktop` itself with a stub spawnBackend +
// stub Electron seams to assert the integration honours the gate.

import { mkdtempSync, rmSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import type { ChildProcess } from "node:child_process";
import { afterEach, beforeEach, expect, test } from "bun:test";
import {
  DAEMON_SPAWN_SKIPPED_MESSAGE,
  bootstrapDesktop,
  shouldSpawnBackend,
  type DesktopBootstrapInput,
} from "./main.ts";
import type {
  BackendHandle,
  BackendSpawnOptions,
} from "./main/BackendLifecycle.ts";
import type { IpcMainLike } from "./main/IpcRegistry.ts";
import type { PowerSaveBlockerLike } from "./main/macPowerAssert.ts";
import type { DesktopSecretStorage } from "./vendored/t3code/clientPersistence.ts";

class InMemorySecretStorage implements DesktopSecretStorage {
  isEncryptionAvailable(): boolean {
    return true;
  }
  encryptString(value: string): Buffer {
    return Buffer.from(value, "utf8");
  }
  decryptString(value: Buffer): string {
    return value.toString("utf8");
  }
}

const stubIpcMain: IpcMainLike = {
  handle() {},
  removeHandler() {},
};

const stubPowerSaveBlocker: PowerSaveBlockerLike = {
  start: () => 1,
  stop: () => true,
  isStarted: () => false,
};

interface SpawnSpy {
  readonly calls: BackendSpawnOptions[];
  readonly stoppedHandles: number[];
  readonly spawnImpl: NonNullable<DesktopBootstrapInput["spawnBackend"]>;
}

function buildSpawnSpy(): SpawnSpy {
  const calls: BackendSpawnOptions[] = [];
  const stoppedHandles: number[] = [];
  const spy: SpawnSpy = {
    calls,
    stoppedHandles,
    spawnImpl: async (options) => {
      const id = calls.length;
      calls.push(options);
      const handle: BackendHandle = {
        port: 30000 + id,
        baseUrl: `http://stub:${30000 + id}`,
        // The lifecycle never reads .child outside of stop, so this
        // synthetic ChildProcess satisfies the type without owning a
        // real subprocess.
        child: { kill() {} } as unknown as ChildProcess,
        async stop() {
          stoppedHandles.push(id);
        },
      };
      return handle;
    },
  };
  return spy;
}

function inputBase(homeDir: string, spy: SpawnSpy): DesktopBootstrapInput {
  return {
    homeDir,
    currentAppVersion: "0.0.0-hp-1loj-test",
    secretStorage: new InMemorySecretStorage(),
    spawnBackend: spy.spawnImpl,
    notifyDaemonSpawnSkipped: () => {
      // suppressed in tests; spy on call counts via the count below
    },
    powerSaveBlocker: stubPowerSaveBlocker,
    ipcMain: stubIpcMain,
    platform: "linux",
  };
}

let homeDir: string;
beforeEach(() => {
  homeDir = mkdtempSync(join(tmpdir(), "hoopoe-bootstrap-gate-"));
});
afterEach(() => {
  rmSync(homeDir, { recursive: true, force: true });
});

// ── shouldSpawnBackend (pure helper) ─────────────────────────────

test("shouldSpawnBackend: undefined returns false", () => {
  expect(shouldSpawnBackend(undefined)).toBe(false);
});

test("shouldSpawnBackend: null returns false", () => {
  expect(shouldSpawnBackend(null)).toBe(false);
});

test("shouldSpawnBackend: empty string returns false", () => {
  expect(shouldSpawnBackend("")).toBe(false);
});

test("shouldSpawnBackend: whitespace-only returns false", () => {
  expect(shouldSpawnBackend(" ")).toBe(false);
  expect(shouldSpawnBackend("\t")).toBe(false);
  expect(shouldSpawnBackend("  \n  ")).toBe(false);
});

test("shouldSpawnBackend: real path returns true", () => {
  expect(shouldSpawnBackend("/path/to/hoopoed")).toBe(true);
  expect(shouldSpawnBackend("hoopoed")).toBe(true);
  expect(shouldSpawnBackend("./relative/hoopoed")).toBe(true);
});

test("shouldSpawnBackend: leading/trailing whitespace around a real path still returns true", () => {
  expect(shouldSpawnBackend("  /path/to/hoopoed  ")).toBe(true);
});

// ── DAEMON_SPAWN_SKIPPED_MESSAGE ─────────────────────────────────

test("DAEMON_SPAWN_SKIPPED_MESSAGE: names the env var and renderer-only mode", () => {
  expect(DAEMON_SPAWN_SKIPPED_MESSAGE).toContain("HOOPOE_DAEMON_BIN");
  expect(DAEMON_SPAWN_SKIPPED_MESSAGE).toContain("renderer-only");
  // The message starts with the project tag so console-grepping is easy.
  expect(DAEMON_SPAWN_SKIPPED_MESSAGE.startsWith("[hoopoe]")).toBe(true);
});

// ── bootstrapDesktop daemon-gate integration ─────────────────────

test("bootstrapDesktop: daemonBinaryPath unset -> spawnBackend NOT called, backend is null", async () => {
  const spy = buildSpawnSpy();
  let skipNotifications = 0;
  const handle = await bootstrapDesktop({
    ...inputBase(homeDir, spy),
    notifyDaemonSpawnSkipped: () => {
      skipNotifications += 1;
    },
  });
  expect(spy.calls).toHaveLength(0);
  expect(handle.backend).toBeNull();
  expect(skipNotifications).toBe(1);
  await handle.shutdown();
});

test("bootstrapDesktop: daemonBinaryPath empty string -> spawnBackend NOT called", async () => {
  const spy = buildSpawnSpy();
  const handle = await bootstrapDesktop({
    ...inputBase(homeDir, spy),
    daemonBinaryPath: "",
  });
  expect(spy.calls).toHaveLength(0);
  expect(handle.backend).toBeNull();
  await handle.shutdown();
});

test("bootstrapDesktop: daemonBinaryPath whitespace-only -> spawnBackend NOT called (treated as unset after trim)", async () => {
  const spy = buildSpawnSpy();
  const handle = await bootstrapDesktop({
    ...inputBase(homeDir, spy),
    daemonBinaryPath: "  \n\t ",
  });
  expect(spy.calls).toHaveLength(0);
  expect(handle.backend).toBeNull();
  await handle.shutdown();
});

test("bootstrapDesktop: real daemonBinaryPath -> spawnBackend IS called with the trimmed path", async () => {
  const spy = buildSpawnSpy();
  let skipNotifications = 0;
  const handle = await bootstrapDesktop({
    ...inputBase(homeDir, spy),
    daemonBinaryPath: "  /path/to/hoopoed  ",
    notifyDaemonSpawnSkipped: () => {
      skipNotifications += 1;
    },
  });
  expect(spy.calls).toHaveLength(1);
  expect(spy.calls[0]?.daemonBinaryPath).toBe("/path/to/hoopoed");
  expect(handle.backend).not.toBeNull();
  expect(handle.backend?.port).toBe(30000);
  expect(skipNotifications).toBe(0);
  await handle.shutdown();
  expect(spy.stoppedHandles).toEqual([0]);
});

test("bootstrapDesktop: shutdown is idempotent in the absent-backend path", async () => {
  const spy = buildSpawnSpy();
  const handle = await bootstrapDesktop(inputBase(homeDir, spy));
  await handle.shutdown();
  await handle.shutdown();
  await handle.shutdown();
  // The gate skipped spawn, so nothing was started — but the
  // bridges + ipcAttachment + powerAssertions still got composed,
  // and shutdown must run their teardown exactly once. The
  // observable here is: no exception is raised on the repeat calls.
  expect(spy.calls).toHaveLength(0);
});

test("bootstrapDesktop: shutdown is idempotent in the present-backend path (backend.stop called once)", async () => {
  const spy = buildSpawnSpy();
  const handle = await bootstrapDesktop({
    ...inputBase(homeDir, spy),
    daemonBinaryPath: "/path/to/hoopoed",
  });
  await handle.shutdown();
  await handle.shutdown();
  await handle.shutdown();
  // Only one stop call lands even if shutdown is invoked repeatedly
  // (e.g. before-quit firing more than once during a fast SIGTERM).
  expect(spy.stoppedHandles).toEqual([0]);
});
