// Hoopoe-owned. Renderer hardening policy constants — extracted from
// `WindowManager.ts` so they can be unit-tested without importing from
// `electron` (Bun's `bun:test` runner can't load Electron's CJS module
// outside an Electron runtime; `WindowManager.ts` itself only loads inside
// the real Electron main process). Both files share these constants;
// WindowManager re-exports them so existing call sites stay stable.
//
// See `WindowManager.ts` for the BrowserWindow construction that consumes
// these constants and the navigation-guard wiring.

import * as FS from "node:fs";
import * as Path from "node:path";
import { fileURLToPath } from "node:url";
import type { WebPreferences } from "electron";

export const DEFAULT_WIDTH = 1440;
export const DEFAULT_HEIGHT = 900;

/**
 * Strict CSP per hp-rflj. Notes on each directive:
 *   default-src 'self'      — fetches default to same origin.
 *   script-src 'self'       — no inline, no eval, no external scripts.
 *   style-src 'self'        — design system ships compiled stylesheets.
 *                              `'unsafe-inline'` retained for now because
 *                              Tailwind's runtime + dynamic theme variables
 *                              emit inline styles; nonce-based hardening is
 *                              tracked in §5.4 for a follow-up.
 *   connect-src 'self' …    — the SSH-tunneled loopback HTTPS+WS endpoints
 *                              (127.0.0.1:* / localhost:*) are the only
 *                              external connections allowed.
 *   img-src 'self' data:    — design tokens + base64 data URIs only.
 *   font-src 'self'         — bundled fonts; no Google Fonts etc.
 *   object-src 'none'       — no <object>/<embed>.
 *   base-uri 'self'         — block <base href> hijacking.
 *   form-action 'self'      — forms can't post offsite.
 *   frame-ancestors 'none'  — Hoopoe is never embedded in another frame.
 */
export const DEFAULT_CSP =
  "default-src 'self'; " +
  "script-src 'self'; " +
  "style-src 'self' 'unsafe-inline'; " +
  "img-src 'self' data:; " +
  "font-src 'self'; " +
  "connect-src 'self' http://127.0.0.1:* http://localhost:* ws://127.0.0.1:* ws://localhost:* wss://127.0.0.1:* wss://localhost:*; " +
  "object-src 'none'; " +
  "base-uri 'self'; " +
  "form-action 'self'; " +
  "frame-ancestors 'none'";

/**
 * hp-iq8f: dev-only CSP applied when the renderer loads from Vite
 * (HOOPOE_VITE_URL set). The Vite + @vitejs/plugin-react dev
 * pipeline injects an inline preamble script and uses `eval`-based
 * Fast Refresh; both are blocked by the strict prod CSP, which
 * causes plugin-react to throw "can't detect preamble" at the top
 * of the first .tsx import and leaves the renderer with a black
 * screen because React never mounts.
 *
 * Loosenings vs DEFAULT_CSP, scoped to dev:
 *   default-src + script-src + style-src + img-src + font-src
 *     each gain http://127.0.0.1:5173 so module/asset fetches
 *     from the Vite dev server resolve.
 *   script-src additionally gains 'unsafe-inline' (React preamble)
 *     and 'unsafe-eval' (Fast Refresh's eval-based HMR).
 *
 * Hardening that STAYS strict in dev (not relaxed):
 *   object-src 'none'    — no plugins.
 *   base-uri 'self'      — no <base href> hijack.
 *   form-action 'self'   — forms still bound to self.
 *   frame-ancestors 'none' — never embedded.
 *   connect-src          — already permits ws://127.0.0.1:* /
 *                          loopback, so HMR works without further
 *                          relaxation.
 *
 * Selection is gated on HOOPOE_VITE_URL via {@link selectCsp};
 * production builds (no env var) keep DEFAULT_CSP, so this dev
 * relaxation never reaches a user-shipped DMG.
 */
export const DEV_CSP_FOR_VITE =
  "default-src 'self' http://127.0.0.1:5173; " +
  "script-src 'self' 'unsafe-inline' 'unsafe-eval' http://127.0.0.1:5173; " +
  "style-src 'self' 'unsafe-inline' http://127.0.0.1:5173; " +
  "img-src 'self' data: http://127.0.0.1:5173; " +
  "font-src 'self' http://127.0.0.1:5173; " +
  "connect-src 'self' http://127.0.0.1:* http://localhost:* ws://127.0.0.1:* ws://localhost:* wss://127.0.0.1:* wss://localhost:*; " +
  "object-src 'none'; " +
  "base-uri 'self'; " +
  "form-action 'self'; " +
  "frame-ancestors 'none'";

/**
 * hp-iq8f: pick the CSP at startup based on whether the renderer
 * is loading from Vite. When `HOOPOE_VITE_URL` is set to a real
 * URL (non-empty after trim), returns {@link DEV_CSP_FOR_VITE};
 * otherwise returns the strict {@link DEFAULT_CSP}. The trim/empty
 * rule mirrors hp-1loj's `shouldSpawnBackend` so dev-mode toggles
 * stay consistent across the entry-point seams.
 */
export function selectCsp(env: NodeJS.ProcessEnv): string {
  const viteUrl = env.HOOPOE_VITE_URL;
  if (typeof viteUrl === "string" && viteUrl.trim().length > 0) {
    return DEV_CSP_FOR_VITE;
  }
  return DEFAULT_CSP;
}

export const SAFE_WEB_PREFERENCES: WebPreferences = {
  contextIsolation: true,
  sandbox: true,
  nodeIntegration: false,
  nodeIntegrationInWorker: false,
  nodeIntegrationInSubFrames: false,
  webSecurity: true,
  allowRunningInsecureContent: false,
  experimentalFeatures: false,
  enableBlinkFeatures: "",
  spellcheck: false,
};

export const ALLOWED_NAVIGATION_ORIGINS = [
  "http://127.0.0.1",
  "http://localhost",
  "https://127.0.0.1",
  "https://localhost",
];

export interface NavigationPolicy {
  readonly appFileRootPath?: string;
  readonly loopbackOrigin?: string;
}

export function navigationPolicyForInitialUrl(initialUrl: string): NavigationPolicy {
  const parsed = parseUrl(initialUrl);
  if (!parsed) return {};
  if (isAllowedLoopbackUrl(parsed)) return { loopbackOrigin: parsed.origin };
  if (parsed.protocol !== "file:") return {};
  const initialFilePath = filePathFromUrl(parsed);
  if (!initialFilePath) return {};
  return { appFileRootPath: canonicalPath(Path.dirname(initialFilePath)) };
}

export function isAllowedNavigationUrl(url: string, policy: NavigationPolicy = {}): boolean {
  const parsed = parseUrl(url);
  if (!parsed) return false;
  if (isAllowedLoopbackUrl(parsed)) return parsed.origin === policy.loopbackOrigin;
  if (parsed.protocol !== "file:") return false;

  const appRoot = policy.appFileRootPath;
  if (!appRoot) return false;
  const targetPath = filePathFromUrl(parsed);
  if (!targetPath) return false;
  return isPathInsideRoot(canonicalPath(targetPath), canonicalPath(appRoot));
}

function parseUrl(value: string): URL | null {
  try {
    return new URL(value);
  } catch {
    return null;
  }
}

function isAllowedLoopbackUrl(url: URL): boolean {
  if (url.protocol !== "http:" && url.protocol !== "https:") return false;
  return url.hostname === "127.0.0.1" || url.hostname === "localhost";
}

function filePathFromUrl(url: URL): string | null {
  try {
    return fileURLToPath(url);
  } catch {
    return null;
  }
}

function canonicalPath(filePath: string): string {
  const resolved = Path.resolve(filePath);
  try {
    return FS.realpathSync.native(resolved);
  } catch {
    return resolved;
  }
}

function isPathInsideRoot(filePath: string, rootPath: string): boolean {
  const relative = Path.relative(rootPath, filePath);
  return (
    relative === "" ||
    (relative !== "" && !relative.startsWith("..") && !Path.isAbsolute(relative))
  );
}

/** Hardening response headers applied alongside CSP on every response. */
export const HARDENING_RESPONSE_HEADERS: Record<string, string> = {
  "X-Content-Type-Options": "nosniff",
  "X-Frame-Options": "DENY",
  "Referrer-Policy": "no-referrer",
};
