// Hoopoe-owned. Owns BrowserWindow creation, multi-window state, restore on
// dock-click, and first-paint reveal across platforms. Uses the vendored
// t3code `windowReveal.ts` helper for the cross-platform reveal trigger.
//
// Strict CSP and safe webPreferences are enforced here at construction time
// (plan.md §5.4 / Appendix C #2): contextIsolation: true, sandbox: true,
// nodeIntegration: false, no `unsafe-inline` / `unsafe-eval`. The renderer
// reaches the main process only through the typed preload bridge that the
// IpcRegistry powers.

import {
  BrowserWindow,
  app,
  type BrowserWindowConstructorOptions,
  type WebPreferences,
} from "electron";
import { bindFirstRevealTrigger } from "../vendored/t3code/windowReveal.ts";

export interface CreateMainWindowOptions {
  readonly preloadPath: string;
  readonly initialUrl: string;
  /** Strict CSP applied via response headers + a meta fallback. The default
   * forbids inline scripts and external network access except to the
   * SSH-tunneled daemon endpoint passed in here. */
  readonly cspDirective?: string;
  readonly width?: number;
  readonly height?: number;
}

const DEFAULT_WIDTH = 1440;
const DEFAULT_HEIGHT = 900;

const DEFAULT_CSP =
  "default-src 'self'; " +
  "script-src 'self'; " +
  "style-src 'self' 'unsafe-inline'; " +
  "img-src 'self' data: blob:; " +
  "font-src 'self' data:; " +
  "connect-src 'self' http://127.0.0.1:* ws://127.0.0.1:*; " +
  "object-src 'none'; " +
  "base-uri 'none'; " +
  "frame-ancestors 'none'; " +
  "form-action 'none'";

const SAFE_WEB_PREFERENCES: WebPreferences = {
  contextIsolation: true,
  sandbox: true,
  nodeIntegration: false,
  webSecurity: true,
  allowRunningInsecureContent: false,
  spellcheck: false,
};

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

  // Apply CSP to every response so we don't depend on the renderer setting a
  // <meta http-equiv> fallback. The connect-src list pinned to 127.0.0.1:*
  // lets the SSH-tunneled daemon connection through without opening the door
  // to general network egress.
  window.webContents.session.webRequest.onHeadersReceived((details, callback) => {
    const headers = { ...details.responseHeaders };
    headers["Content-Security-Policy"] = [csp];
    callback({ responseHeaders: headers });
  });

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
  electronApp: app,
};
