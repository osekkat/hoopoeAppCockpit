// hp-27d3: pure-logic Electron main-process lifecycle wiring.
//
// The companion file `apps/desktop/src/electron-entry.ts` is the
// side-effectful entry that imports `electron` and passes the real
// `app` into `wireMainProcessLifecycle`. Keeping the wiring logic
// here lets bun:test exercise it with a stub `app` — bun:test
// cannot import the `electron` module, so any file that does is
// unreachable from unit tests.

import * as Path from "node:path";

/** Subset of `electron.app` the lifecycle wiring depends on.
 * Declared locally so this module stays Electron-import-free. */
export interface ElectronAppLike {
  readonly requestSingleInstanceLock: () => boolean;
  readonly whenReady: () => Promise<void>;
  readonly on: (
    event: string,
    listener: (...args: unknown[]) => unknown,
  ) => void;
  readonly quit: () => void;
}

/** Subset of `bootstrapDesktop`'s returned handle the lifecycle
 * reaches into. Avoids importing `./main.ts` here (which imports
 * `electron`). */
export interface BootstrapHandleLike {
  readonly shutdown: () => Promise<void>;
}

export interface RendererTarget {
  readonly url: string;
  readonly isDev: boolean;
}

export interface ResolveRendererTargetInput {
  readonly env: NodeJS.ProcessEnv;
  /** Absolute path to the `dist-electron` directory the bundled
   * entry runs from. Used to derive the sibling `dist/index.html`
   * in production. */
  readonly distElectronDir: string;
}

const DEFAULT_DEV_URL = "http://localhost:5173";

/** Resolve the renderer URL the main window loads.
 *
 * Dev mode wins on `HOOPOE_VITE_URL` (explicit override) or when
 * `NODE_ENV=development` is set; otherwise production loads
 * `file://<appRoot>/dist/index.html` as a sibling of the
 * `dist-electron` directory the entry was bundled into. */
export function resolveRendererTarget(
  input: ResolveRendererTargetInput,
): RendererTarget {
  const explicit = input.env.HOOPOE_VITE_URL;
  if (explicit && explicit.length > 0) {
    return { url: explicit, isDev: true };
  }
  if (input.env.NODE_ENV === "development") {
    return { url: DEFAULT_DEV_URL, isDev: true };
  }
  const indexPath = Path.join(
    input.distElectronDir,
    "..",
    "dist",
    "index.html",
  );
  return { url: `file://${indexPath}`, isDev: false };
}

export interface MainWindowFactory {
  (target: RendererTarget): void;
}

export interface WireMainProcessLifecycleInput {
  readonly app: ElectronAppLike;
  readonly platform: NodeJS.Platform;
  readonly target: RendererTarget;
  readonly bootstrap: () => Promise<BootstrapHandleLike>;
  readonly openMainWindow: MainWindowFactory;
  readonly revealExisting: () => boolean;
}

export interface WiredMainProcessLifecycle {
  readonly hasLock: boolean;
  readonly handlers: ReadonlyMap<string, (...args: unknown[]) => unknown>;
  /** Resolves after `whenReady` fires and `bootstrap()` completes.
   * Tests await this to assert the bootstrap → window-open sequence;
   * production code does not need to. */
  readonly readyPromise: Promise<void>;
}

/** Wire the Electron app lifecycle.
 *
 * Returns synchronously — the `whenReady` step runs in the background
 * and is exposed via `readyPromise` for tests.
 *
 * Lifecycle:
 *   - `requestSingleInstanceLock()` returning false → `app.quit()`
 *     and bail without registering listeners (the second instance
 *     has nothing to do; the first instance focuses its window via
 *     the `second-instance` event).
 *   - `whenReady` → `bootstrap()` → `openMainWindow(target)`.
 *   - `second-instance` → `revealExisting()` (focus the running
 *     window without spawning a new one).
 *   - `activate` (macOS dock click) → `revealExisting()` if a
 *     window exists, otherwise `openMainWindow(target)`.
 *   - `window-all-closed` → `app.quit()` on linux/win32; persist
 *     on darwin so the dock click can re-open without a fresh
 *     bootstrap.
 *   - `before-quit` → `bootstrapResult.shutdown()` so the daemon
 *     subprocess + IPC handlers + power assertions are torn down
 *     cleanly.
 */
export function wireMainProcessLifecycle(
  input: WireMainProcessLifecycleInput,
): WiredMainProcessLifecycle {
  const handlers = new Map<string, (...args: unknown[]) => unknown>();

  const hasLock = input.app.requestSingleInstanceLock();
  if (!hasLock) {
    input.app.quit();
    return { hasLock: false, handlers, readyPromise: Promise.resolve() };
  }

  let handle: BootstrapHandleLike | null = null;

  const on = (
    event: string,
    listener: (...args: unknown[]) => unknown,
  ): void => {
    handlers.set(event, listener);
    input.app.on(event, listener);
  };

  on("second-instance", () => {
    input.revealExisting();
  });

  on("activate", () => {
    if (!input.revealExisting()) {
      input.openMainWindow(input.target);
    }
  });

  on("window-all-closed", () => {
    if (input.platform !== "darwin") {
      input.app.quit();
    }
  });

  on("before-quit", async () => {
    if (!handle) return;
    const current = handle;
    handle = null;
    await current.shutdown();
  });

  const readyPromise = input.app.whenReady().then(async () => {
    handle = await input.bootstrap();
    input.openMainWindow(input.target);
  });

  return { hasLock: true, handlers, readyPromise };
}
