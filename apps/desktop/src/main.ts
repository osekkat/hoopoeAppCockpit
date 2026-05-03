// Hoopoe desktop main-process entry. By design ≤200 lines per
// plan.md Appendix B "Anti-patterns to refuse" #4 — the heavy logic
// lives in modules under `src/main/`.
//
// This file does only the wiring: app lifecycle hooks, instantiating each
// bridge with a small composition root. Wiring up Electron's app/IPC events
// lands when Phase 1 ships the renderer (hp-z1x). Until then, this file
// exposes a typed `bootstrapDesktop` factory that the test harness (and
// hp-191's smoke build) can call.

import * as Path from "node:path";
import { spawnBackend, type BackendHandle } from "./main/BackendLifecycle.ts";
import { createUpdateMachine, type UpdateMachine } from "./main/UpdateMachine.ts";
import { IpcRegistry } from "./main/IpcRegistry.ts";
import {
  SettingsBridge,
  defaultUserSettingsPath,
  projectSettingsPath,
} from "./main/SettingsBridge.ts";
import {
  createJsonlBatchAuditSink,
  type SettingsActor,
} from "./main/SettingsAuditTrail.ts";
import { AuthBridge } from "./main/AuthBridge.ts";
import { resolveDesktopRuntimeInfo } from "./vendored/t3code/runtimeArch.ts";
import {
  resolveDefaultDesktopUpdateChannel,
} from "./vendored/t3code/updateChannels.ts";
import type { DesktopSecretStorage } from "./vendored/t3code/clientPersistence.ts";

/** Production-tier audit sink lives at `<homeDir>/.hoopoe/audit.jsonl`.
 *  Append-only, newline-delimited JSON, atomic at the FS level for typical
 *  settings batches (POSIX `O_APPEND` ≤ PIPE_BUF / 4 KiB). */
export function defaultSettingsAuditPath(homeDir: string): string {
  return Path.join(homeDir, ".hoopoe", "audit.jsonl");
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

export interface DesktopBootstrapInput {
  readonly homeDir: string;
  readonly currentAppVersion: string;
  readonly daemonBinaryPath: string;
  readonly secretStorage: DesktopSecretStorage;
  readonly relaunch?: (reason: string) => void;
  /** Optional active-project root for the project-tier settings file. */
  readonly projectRoot?: string;
  /** Test seam — defaults to native `process.platform` / `process.arch`. */
  readonly platform?: NodeJS.Platform;
  readonly processArch?: string;
  readonly runningUnderArm64Translation?: boolean;
}

export interface DesktopBootstrapHandle {
  readonly settings: SettingsBridge;
  readonly auth: AuthBridge;
  readonly ipc: IpcRegistry;
  readonly updates: UpdateMachine;
  readonly backend: BackendHandle;
  readonly shutdown: () => Promise<void>;
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

  const registryPath = Path.join(input.homeDir, ".hoopoe", "userdata", "saved-environments.json");
  const auth = new AuthBridge({
    registryPath,
    secretStorage: input.secretStorage,
  });

  const ipc = new IpcRegistry();

  const updates = createUpdateMachine({
    currentVersion: input.currentAppVersion,
    runtimeInfo,
    channel: resolveDefaultDesktopUpdateChannel(input.currentAppVersion),
  });

  const backend = await spawnBackend({
    daemonBinaryPath: input.daemonBinaryPath,
    host: "127.0.0.1",
  });

  return {
    settings,
    auth,
    ipc,
    updates,
    backend,
    shutdown: async () => {
      await backend.stop();
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
