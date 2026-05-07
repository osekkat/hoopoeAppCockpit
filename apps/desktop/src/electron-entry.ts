// hp-27d3: top-level Electron main-process entrypoint.
//
// The build pipeline emits this file as
// `apps/desktop/dist-electron/electron-entry.js`; package.json's
// `"main"` field points at it so `electron apps/desktop` (or
// `electron <path-to-built-entry>`) finds the lifecycle wiring.
//
// This file is intentionally thin and side-effectful: it builds
// production secret storage, resolves the renderer URL, and hands
// the real `electron.app` to `wireMainProcessLifecycle`. All
// testable logic lives in `./main/ElectronLifecycle.ts` and
// `./main.ts` (`bootstrapDesktop`) — bun:test can exercise both
// without an Electron runtime.

import * as OS from "node:os";
import * as Path from "node:path";
import { fileURLToPath } from "node:url";
import { app, safeStorage } from "electron";
import { bootstrapDesktop } from "./main.ts";
import {
  createMainWindow,
  revealExistingWindowOnActivate,
} from "./main/WindowManager.ts";
import {
  resolveRendererTarget,
  wireMainProcessLifecycle,
  type ElectronAppLike,
} from "./main/ElectronLifecycle.ts";
import type { DesktopSecretStorage } from "./vendored/t3code/clientPersistence.ts";

// Resolve this file's directory at runtime. tsdown emits ESM, so
// `import.meta.url` is the right hook; `__dirname` is not auto-
// defined under ESM.
const __filename = fileURLToPath(import.meta.url);
const __dirname = Path.dirname(__filename);

// Production secret storage backed by Electron's `safeStorage`
// (Keychain on macOS via `osCryptAsync`). When safeStorage is
// unavailable (rare — headless CI without a keychain), AuthBridge's
// fallback path applies; see hp-rr9m.
const productionSecretStorage: DesktopSecretStorage = {
  isEncryptionAvailable: () => safeStorage.isEncryptionAvailable(),
  encryptString: (value) => safeStorage.encryptString(value),
  decryptString: (value) => safeStorage.decryptString(value),
};

const target = resolveRendererTarget({
  env: process.env,
  distElectronDir: __dirname,
});

const preloadPath = Path.join(__dirname, "preload.js");

// Electron's `app.on` uses heavily-overloaded literal event-name
// types that don't conform structurally to the simpler `(event:
// string, ...) => void` shape in `ElectronAppLike`. The adapter
// narrows the surface to the four methods the lifecycle uses.
const electronAppAdapter: ElectronAppLike = {
  requestSingleInstanceLock: () => app.requestSingleInstanceLock(),
  whenReady: () => app.whenReady(),
  on: (event, listener) => {
    app.on(event as Parameters<typeof app.on>[0], listener);
  },
  quit: () => app.quit(),
};

wireMainProcessLifecycle({
  app: electronAppAdapter,
  platform: process.platform,
  target,
  bootstrap: () =>
    bootstrapDesktop({
      homeDir: OS.homedir(),
      currentAppVersion: app.getVersion(),
      // The daemon binary path is provisioned by hp-2eg6's verifier
      // before v1; today the env override is the only knob, and an
      // empty value falls through to spawnBackend's documented error
      // path so the failure is visible rather than silent.
      daemonBinaryPath: process.env.HOOPOE_DAEMON_BIN ?? "",
      secretStorage: productionSecretStorage,
    }),
  openMainWindow: (resolved) => {
    createMainWindow({
      preloadPath,
      initialUrl: resolved.url,
    });
  },
  revealExisting: () => revealExistingWindowOnActivate(),
});
