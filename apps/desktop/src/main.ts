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
import { SettingsBridge, defaultSettingsBridgePaths } from "./main/SettingsBridge.ts";
import { AuthBridge } from "./main/AuthBridge.ts";
import { resolveDesktopRuntimeInfo } from "./vendored/t3code/runtimeArch.ts";
import {
  resolveDefaultDesktopUpdateChannel,
} from "./vendored/t3code/updateChannels.ts";
import type { DesktopSecretStorage } from "./vendored/t3code/clientPersistence.ts";

export interface DesktopBootstrapInput {
  readonly homeDir: string;
  readonly currentAppVersion: string;
  readonly daemonBinaryPath: string;
  readonly secretStorage: DesktopSecretStorage;
  readonly relaunch?: (reason: string) => void;
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

  const paths = defaultSettingsBridgePaths(input.homeDir);
  const settings = new SettingsBridge({
    paths,
    currentAppVersion: input.currentAppVersion,
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
  defaultSettingsBridgePaths,
};
