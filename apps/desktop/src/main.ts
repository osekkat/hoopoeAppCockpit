// Hoopoe desktop main-process entry. By design ≤200 lines per
// plan.md Appendix B "Anti-patterns to refuse" #4 — the heavy logic
// lives in modules under `src/main/`.
//
// This file does only the wiring: app lifecycle hooks, instantiating each
// bridge with a small composition root. Wiring up Electron's app/IPC events
// lands when Phase 1 ships the renderer (hp-z1x). Until then, this file
// exposes a typed `bootstrapDesktop` factory that the test harness (and
// hp-191's smoke build) can call.

import * as FS from "node:fs";
import * as Path from "node:path";
import Electron from "electron";
import { spawnBackend, type BackendHandle } from "./main/BackendLifecycle.ts";
import { createUpdateMachine, type UpdateMachine } from "./main/UpdateMachine.ts";
import {
  IpcRegistry,
  attachIpcRegistryToElectron,
  type AttachIpcRegistryHandle,
  type IpcMainLike,
} from "./main/IpcRegistry.ts";
import {
  SettingsBridge,
  defaultUserSettingsPath,
  projectSettingsPath,
} from "./main/SettingsBridge.ts";
import { createJsonlBatchAuditSink, type SettingsActor } from "./main/SettingsAuditTrail.ts";
import { AuthBridge, type AuthBridgeAuditEvent } from "./main/AuthBridge.ts";
import {
  createNSProcessInfoBridge,
  PowerAssertionManager,
  registerPowerAssertionIpc,
  type NativeActivityBridge,
  type PowerAssertionAuditEvent,
  type PowerSaveBlockerLike,
} from "./main/macPowerAssert.ts";
import { resolveDesktopRuntimeInfo } from "./vendored/t3code/runtimeArch.ts";
import { resolveDefaultDesktopUpdateChannel } from "./vendored/t3code/updateChannels.ts";
import type { DesktopSecretStorage } from "./vendored/t3code/clientPersistence.ts";

/** Production-tier audit sink lives at `<homeDir>/.hoopoe/audit.jsonl`.
 *  Append-only, newline-delimited JSON, atomic at the FS level for typical
 *  settings batches (POSIX `O_APPEND` ≤ PIPE_BUF / 4 KiB). */
export function defaultSettingsAuditPath(homeDir: string): string {
  return Path.join(homeDir, ".hoopoe", "audit.jsonl");
}

function appendPowerAssertionAuditEvent(filePath: string, event: PowerAssertionAuditEvent): void {
  FS.mkdirSync(Path.dirname(filePath), { recursive: true });
  FS.appendFileSync(
    filePath,
    `${JSON.stringify({ entry: "power_assertion", actor: DESKTOP_MAIN_DEFAULT_ACTOR, ...event })}\n`,
    { encoding: "utf8" },
  );
}

/** hp-4qzh: production sink for AuthBridge events shipped in hp-rr9m
 *  (bearer persist / forget / session-metadata / secret-rotation).
 *  Mirrors `appendPowerAssertionAuditEvent` shape so the JSONL file
 *  remains a single audit stream consumers can grep by `entry`. */
function appendAuthBridgeAuditEvent(filePath: string, event: AuthBridgeAuditEvent): void {
  FS.mkdirSync(Path.dirname(filePath), { recursive: true });
  FS.appendFileSync(
    filePath,
    `${JSON.stringify({ entry: "auth_bridge", actor: DESKTOP_MAIN_DEFAULT_ACTOR, ...event })}\n`,
    { encoding: "utf8" },
  );
}

/** Default actor stamped on audit entries when the call site doesn't pass
 *  one — captures the desktop-main composition root as the caller of last
 *  resort. Callers crossing IPC SHOULD pass an explicit actor with the
 *  renderer-window id; reaching this default is itself a finding. */
const DESKTOP_MAIN_DEFAULT_ACTOR: SettingsActor = {
  kind: "system",
  id: "desktop:main",
  source: "ipc",
};

export interface ComposedSettingsBridgeInput {
  readonly homeDir: string;
  readonly projectRoot?: string;
  readonly relaunch?: (reason: string) => void;
}

/** Compose the production SettingsBridge with the durable JSONL audit sink
 *  and the desktop-main default actor. Extracted so `bootstrapDesktop` and
 *  the Phase 1.5 bootstrap-integration test exercise the same wiring. */
export function composeProductionSettingsBridge(
  input: ComposedSettingsBridgeInput,
): SettingsBridge {
  return new SettingsBridge({
    paths: {
      userFile: defaultUserSettingsPath(input.homeDir),
      projectFile: input.projectRoot ? projectSettingsPath(input.projectRoot) : null,
    },
    auditBatchSink: createJsonlBatchAuditSink({
      filePath: defaultSettingsAuditPath(input.homeDir),
    }),
    defaultActor: DESKTOP_MAIN_DEFAULT_ACTOR,
    ...(input.relaunch ? { relaunch: input.relaunch } : {}),
  });
}

export interface ComposedAuthBridgeInput {
  readonly homeDir: string;
  readonly secretStorage: DesktopSecretStorage;
}

/** hp-4qzh: compose the production AuthBridge with the durable JSONL audit
 *  sink and the desktop-main default actor. Extracted so `bootstrapDesktop`
 *  and the bootstrap-integration test exercise the same wiring; mirrors the
 *  composeProductionSettingsBridge / PowerAssertionManager pattern. */
export function composeProductionAuthBridge(input: ComposedAuthBridgeInput): AuthBridge {
  const registryPath = Path.join(input.homeDir, ".hoopoe", "userdata", "saved-environments.json");
  return new AuthBridge({
    registryPath,
    secretStorage: input.secretStorage,
    audit: (event) =>
      appendAuthBridgeAuditEvent(defaultSettingsAuditPath(input.homeDir), event),
  });
}

export interface DesktopBootstrapInput {
  readonly homeDir: string;
  readonly currentAppVersion: string;
  /** Path to the Hoopoe Go daemon binary. Optional so renderer-only
   *  dev sessions (no built daemon yet) can still open a window —
   *  hp-1loj follow-up to hp-27d3. When unset, empty, or whitespace,
   *  bootstrapDesktop skips spawnBackend and notifies the user via
   *  `notifyDaemonSpawnSkipped` (default: console.warn).
   *  See {@link shouldSpawnBackend} for the exact trim/empty rule.
   *  Explicit `| undefined` lets electron-entry pass
   *  `process.env.HOOPOE_DAEMON_BIN` directly under
   *  `exactOptionalPropertyTypes: true`. */
  readonly daemonBinaryPath?: string | undefined;
  readonly secretStorage: DesktopSecretStorage;
  readonly relaunch?: (reason: string) => void;
  /** Optional active-project root for the project-tier settings file. */
  readonly projectRoot?: string;
  /** Test seam — defaults to native `process.platform` / `process.arch`. */
  readonly platform?: NodeJS.Platform;
  readonly processArch?: string;
  readonly runningUnderArm64Translation?: boolean;
  readonly powerSaveBlocker?: PowerSaveBlockerLike;
  readonly nativeActivity?: NativeActivityBridge;
  /** hp-vd9: test seam for the Electron `ipcMain` surface. Defaults to
   *  `electron.ipcMain` in production. Tests inject a stub to assert
   *  every registered command lands a `ipcMain.handle` binding. */
  readonly ipcMain?: IpcMainLike;
  /** hp-1loj test seam: lets tests substitute a stub for the
   *  daemon spawn. Defaults to the real `spawnBackend` from
   *  BackendLifecycle. The injected function only runs when
   *  `shouldSpawnBackend(daemonBinaryPath)` returns true; tests can
   *  use it both to assert call counts and to return a synthetic
   *  BackendHandle without a real subprocess. */
  readonly spawnBackend?: typeof spawnBackend;
  /** hp-1loj test seam: invoked once when bootstrapDesktop skips
   *  spawning the daemon (renderer-only mode). Defaults to a
   *  `console.warn` of {@link DAEMON_SPAWN_SKIPPED_MESSAGE}. */
  readonly notifyDaemonSpawnSkipped?: () => void;
}

export interface DesktopBootstrapHandle {
  readonly settings: SettingsBridge;
  readonly auth: AuthBridge;
  readonly ipc: IpcRegistry;
  readonly updates: UpdateMachine;
  /** Null when bootstrapDesktop ran in renderer-only mode (no
   *  daemon binary path set; see hp-1loj). Daemon-backed RPCs
   *  return errors until the user re-runs with HOOPOE_DAEMON_BIN
   *  pointing at a built binary. */
  readonly backend: BackendHandle | null;
  readonly powerAssertions: PowerAssertionManager;
  readonly shutdown: () => Promise<void>;
}

/** hp-1loj: user-visible warning when bootstrapDesktop skips
 *  spawning the daemon. Exported so test seams + UI surfaces can
 *  match the exact phrasing without re-typing it. */
export const DAEMON_SPAWN_SKIPPED_MESSAGE =
  "[hoopoe] daemon spawn skipped — HOOPOE_DAEMON_BIN unset; running renderer-only dev mode. Daemon-backed features (project/health/swarm RPCs) will return errors until you set HOOPOE_DAEMON_BIN to a built daemon binary.";

/** hp-1loj: returns true when `daemonBinaryPath` looks like a
 *  real path bootstrapDesktop should pass to spawnBackend; false
 *  (skip spawn) when undefined, null, empty, or whitespace-only.
 *
 *  Whitespace is normalised because env vars often carry trailing
 *  spaces or empty values from shell substitution; treating them
 *  as "set" would crash spawn() with ERR_INVALID_ARG_VALUE. The
 *  spawnBackend contract itself stays unchanged — callers that
 *  actually want to spawn must still pass a non-empty path; this
 *  helper is the gate that decides whether to call spawnBackend
 *  at all. */
export function shouldSpawnBackend(
  daemonBinaryPath: string | undefined | null,
): boolean {
  return (
    typeof daemonBinaryPath === "string" &&
    daemonBinaryPath.trim().length > 0
  );
}

/** Compose the desktop main-process modules and start the daemon. The
 * returned handle owns the daemon process lifetime; callers must call
 * `shutdown()` before exiting (Electron `before-quit` hook in production). */
export async function bootstrapDesktop(
  input: DesktopBootstrapInput,
): Promise<DesktopBootstrapHandle> {
  const runtimeInfo = resolveDesktopRuntimeInfo({
    platform: input.platform ?? process.platform,
    processArch: input.processArch ?? process.arch,
    runningUnderArm64Translation: input.runningUnderArm64Translation ?? false,
  });

  const settings = composeProductionSettingsBridge({
    homeDir: input.homeDir,
    ...(input.projectRoot ? { projectRoot: input.projectRoot } : {}),
    ...(input.relaunch ? { relaunch: input.relaunch } : {}),
  });

  const auth = composeProductionAuthBridge({
    homeDir: input.homeDir,
    secretStorage: input.secretStorage,
  });

  const ipc = new IpcRegistry();
  const nativeActivity =
    input.nativeActivity ??
    createNSProcessInfoBridge({ platform: input.platform ?? process.platform });
  const powerAssertions = new PowerAssertionManager({
    powerSaveBlocker: input.powerSaveBlocker ?? Electron.powerSaveBlocker,
    ...(nativeActivity ? { nativeActivity } : {}),
    disabled: settings.resolved().desktop.disablePowerAssertions,
    audit: (event) =>
      appendPowerAssertionAuditEvent(defaultSettingsAuditPath(input.homeDir), event),
  });
  registerPowerAssertionIpc(ipc, powerAssertions);
  // hp-vd9: now that handlers are registered with the IpcRegistry, wire
  // them through `ipcMain.handle` so renderer `ipcRenderer.invoke()`
  // calls actually reach the typed dispatch path. Without this step the
  // registry is just an in-memory map and Electron returns "no handler
  // registered" for every renderer IPC call.
  const ipcAttachment: AttachIpcRegistryHandle = attachIpcRegistryToElectron(ipc, {
    ipcMain: input.ipcMain ?? Electron.ipcMain,
  });
  const powerSettingsSubscription = settings.subscribe((event) => {
    if (event.changedKeys.includes("desktop.disablePowerAssertions")) {
      powerAssertions.setDisabled(event.resolved.desktop.disablePowerAssertions);
    }
  });

  const updates = createUpdateMachine({
    currentVersion: input.currentAppVersion,
    runtimeInfo,
    channel: resolveDefaultDesktopUpdateChannel(input.currentAppVersion),
  });

  // hp-1loj: gate spawnBackend on a real-looking daemonBinaryPath.
  // Empty / whitespace / undefined → skip spawn and notify the user
  // that the cockpit is running renderer-only. The spawnBackend
  // contract itself stays unchanged (empty path is still an error
  // there); the gate is here so callers don't have to duplicate the
  // trim/empty check at every call site.
  let backend: BackendHandle | null = null;
  if (shouldSpawnBackend(input.daemonBinaryPath)) {
    const spawnImpl = input.spawnBackend ?? spawnBackend;
    backend = await spawnImpl({
      daemonBinaryPath: input.daemonBinaryPath!.trim(),
      host: "127.0.0.1",
    });
  } else {
    const notify =
      input.notifyDaemonSpawnSkipped ??
      (() => {
        // eslint-disable-next-line no-console -- intentional user-facing
        // diagnostic on the main-process console; routed through Electron
        // logs in production.
        console.warn(DAEMON_SPAWN_SKIPPED_MESSAGE);
      });
    notify();
  }

  let alreadyShutDown = false;
  return {
    settings,
    auth,
    ipc,
    updates,
    backend,
    powerAssertions,
    shutdown: async () => {
      // hp-1loj: shutdown is idempotent — the lifecycle wires this
      // into `before-quit` which can fire more than once during a
      // fast SIGTERM, and the renderer-only path still owns the
      // bridges/IPC so we must run their teardown exactly once
      // regardless of whether a backend was spawned.
      if (alreadyShutDown) return;
      alreadyShutDown = true;
      powerSettingsSubscription.unsubscribe();
      // hp-vd9: detach ipcMain.handle bindings before tearing down the
      // power assertions so a hot-reload or test re-bootstrap doesn't
      // leak stale handlers (Electron throws on duplicate handle()).
      ipcAttachment.detach();
      powerAssertions.shutdown();
      if (backend !== null) {
        const current = backend;
        backend = null;
        await current.stop();
      }
    },
  };
}

export {
  AuthBridge,
  IpcRegistry,
  SettingsBridge,
  spawnBackend,
  createUpdateMachine,
  defaultUserSettingsPath,
  projectSettingsPath,
};
