// Hoopoe-owned. Renderer hardening policy constants — extracted from
// `WindowManager.ts` so they can be unit-tested without importing from
// `electron` (Bun's `bun:test` runner can't load Electron's CJS module
// outside an Electron runtime; `WindowManager.ts` itself only loads inside
// the real Electron main process). Both files share these constants;
// WindowManager re-exports them so existing call sites stay stable.
//
// See `WindowManager.ts` for the BrowserWindow construction that consumes
// these constants and the navigation-guard wiring.

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
  "file://",
];

export function isAllowedNavigationUrl(url: string): boolean {
  return ALLOWED_NAVIGATION_ORIGINS.some((origin) => url.startsWith(origin));
}

/** Hardening response headers applied alongside CSP on every response. */
export const HARDENING_RESPONSE_HEADERS: Record<string, string> = {
  "X-Content-Type-Options": "nosniff",
  "X-Frame-Options": "DENY",
  "Referrer-Policy": "no-referrer",
};
