// Hoopoe-owned. Owns BrowserWindow creation, multi-window state, restore on
// dock-click, and first-paint reveal across platforms. Uses the vendored
// t3code `windowReveal.ts` helper for the cross-platform reveal trigger.
// Renderer hardening (hp-rflj) constants live in `./window-policy.ts` so
// they can be unit-tested without importing `electron` — see that file for
// the canonical CSP + webPreferences definitions.

import {
  BrowserWindow,
  type BrowserWindowConstructorOptions,
} from "electron";
import { bindFirstRevealTrigger } from "../vendored/t3code/windowReveal.ts";
import {
  ALLOWED_NAVIGATION_ORIGINS,
  DEFAULT_CSP,
  DEFAULT_HEIGHT,
  DEFAULT_WIDTH,
  HARDENING_RESPONSE_HEADERS,
  SAFE_WEB_PREFERENCES,
  isAllowedNavigationUrl,
} from "./window-policy.ts";

export {
  ALLOWED_NAVIGATION_ORIGINS,
  DEFAULT_CSP,
  HARDENING_RESPONSE_HEADERS,
  SAFE_WEB_PREFERENCES,
  isAllowedNavigationUrl,
};

export interface CreateMainWindowOptions {
  readonly preloadPath: string;
  readonly initialUrl: string;
  /** Strict CSP applied via response headers. The default forbids inline
   * scripts, eval, and external network access except to the SSH-tunneled
   * daemon endpoint (loopback + websocket). */
  readonly cspDirective?: string;
  readonly width?: number;
  readonly height?: number;
}

const knownWindows = new Set<BrowserWindow>();

export function createMainWindow(options: CreateMainWindowOptions): BrowserWindow {
  const csp = options.cspDirective ?? DEFAULT_CSP;
  const ctorOptions: BrowserWindowConstructorOptions = {
    width: options.width ?? DEFAULT_WIDTH,
    height: options.height ?? DEFAULT_HEIGHT,
    show: false,
    backgroundColor: "#0E0E10",
    webPreferences: {
      ...SAFE_WEB_PREFERENCES,
      preload: options.preloadPath,
    },
  };
  const window = new BrowserWindow(ctorOptions);
  knownWindows.add(window);
  window.once("closed", () => {
    knownWindows.delete(window);
  });

  applyResponseHeaders(window, csp);
  applyNavigationGuards(window);

  // Cross-platform first-paint reveal — see vendored/t3code/windowReveal.ts.
  bindFirstRevealTrigger(
    [
      (listener) => window.once("ready-to-show", listener),
      (listener) => window.webContents.once("did-finish-load", listener),
    ],
    () => window.show(),
  );

  void window.loadURL(options.initialUrl);
  return window;
}

/** Apply CSP + a few hardening response headers to every response. The CSP
 * is applied here (not via <meta>) so the renderer can't disable it by
 * removing the meta tag. */
function applyResponseHeaders(window: BrowserWindow, csp: string): void {
  window.webContents.session.webRequest.onHeadersReceived((details, callback) => {
    const headers = { ...details.responseHeaders };
    headers["Content-Security-Policy"] = [csp];
    for (const [key, value] of Object.entries(HARDENING_RESPONSE_HEADERS)) {
      headers[key] = [value];
    }
    callback({ responseHeaders: headers });
  });
}

/** Block navigation away from the loopback origin and refuse `window.open()`
 * outright — `{ action: "deny" }` is the safe default. Routing external URLs
 * to the OS browser is wired through `window.hoopoe.files.openExternal`
 * (preload bridge → main IpcRegistry → `electron.shell.openExternal`) so
 * the renderer can't inherit the preload + privileges via a new window. */
function applyNavigationGuards(window: BrowserWindow): void {
  window.webContents.on("will-navigate", (event, url) => {
    if (!isAllowedNavigationUrl(url)) {
      event.preventDefault();
    }
  });
  window.webContents.setWindowOpenHandler(() => {
    return { action: "deny" };
  });
}

export function listKnownWindows(): readonly BrowserWindow[] {
  return Array.from(knownWindows);
}

/** macOS: re-show the existing window on dock-click instead of creating a
 * second one. Wired up by main.ts via `app.on("activate", ...)`. */
export function revealExistingWindowOnActivate(): boolean {
  const allWindows = BrowserWindow.getAllWindows();
  if (allWindows.length === 0) return false;
  const target = allWindows[0];
  if (!target) return false;
  if (target.isMinimized()) target.restore();
  target.show();
  target.focus();
  return true;
}

/** Closes every Hoopoe-owned window. Called by main.ts in shutdown paths. */
export function closeAllWindows(): void {
  for (const window of knownWindows) {
    if (!window.isDestroyed()) {
      window.close();
    }
  }
  knownWindows.clear();
}

export const windowManagerInternalsForTesting = {
  defaultCsp: DEFAULT_CSP,
  safeWebPreferences: SAFE_WEB_PREFERENCES,
  allowedNavigationOrigins: ALLOWED_NAVIGATION_ORIGINS,
  isAllowedNavigationUrl,
};
