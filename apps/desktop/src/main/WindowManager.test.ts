// hp-rflj renderer-hardening assertions. Tests target `./window-policy.ts`
// (the Electron-free constants) so bun:test can load them without spinning
// up an Electron runtime — `./WindowManager.ts` itself imports `electron`
// and is only loadable inside the real main process.

import { expect, test } from "bun:test";
import {
  ALLOWED_NAVIGATION_ORIGINS,
  DEFAULT_CSP,
  DEV_CSP_FOR_VITE,
  HARDENING_RESPONSE_HEADERS,
  SAFE_WEB_PREFERENCES,
  isAllowedNavigationUrl,
  navigationPolicyForInitialUrl,
  selectCsp,
} from "./window-policy.ts";

// ── Strict webPreferences (hp-rflj) ───────────────────────────────────────

test("window-policy: SAFE_WEB_PREFERENCES enforces every renderer-hardening flag", () => {
  expect(SAFE_WEB_PREFERENCES.contextIsolation).toBe(true);
  expect(SAFE_WEB_PREFERENCES.sandbox).toBe(true);
  expect(SAFE_WEB_PREFERENCES.nodeIntegration).toBe(false);
  expect(SAFE_WEB_PREFERENCES.nodeIntegrationInWorker).toBe(false);
  expect(SAFE_WEB_PREFERENCES.nodeIntegrationInSubFrames).toBe(false);
  expect(SAFE_WEB_PREFERENCES.webSecurity).toBe(true);
  expect(SAFE_WEB_PREFERENCES.allowRunningInsecureContent).toBe(false);
  expect(SAFE_WEB_PREFERENCES.experimentalFeatures).toBe(false);
  expect(SAFE_WEB_PREFERENCES.enableBlinkFeatures).toBe("");
  expect(SAFE_WEB_PREFERENCES.spellcheck).toBe(false);
});

// ── Strict CSP (hp-rflj) ──────────────────────────────────────────────────

test("window-policy: DEFAULT_CSP forbids inline scripts (no 'unsafe-inline' / 'unsafe-eval' on script-src)", () => {
  const scriptSrcMatch = /script-src ([^;]+);/.exec(DEFAULT_CSP);
  expect(scriptSrcMatch).not.toBeNull();
  const scriptSrc = scriptSrcMatch?.[1] ?? "";
  expect(scriptSrc).not.toContain("'unsafe-inline'");
  expect(scriptSrc).not.toContain("'unsafe-eval'");
  expect(DEFAULT_CSP).toContain("default-src 'self'");
  expect(DEFAULT_CSP).toContain("object-src 'none'");
  expect(DEFAULT_CSP).toContain("base-uri 'self'");
  expect(DEFAULT_CSP).toContain("frame-ancestors 'none'");
  expect(DEFAULT_CSP).toContain("form-action 'self'");
});

test("window-policy: DEFAULT_CSP allows the SSH-tunneled loopback daemon connection only", () => {
  expect(DEFAULT_CSP).toContain("connect-src 'self'");
  expect(DEFAULT_CSP).toContain("http://127.0.0.1:*");
  expect(DEFAULT_CSP).toContain("ws://127.0.0.1:*");
  // No public origins.
  expect(DEFAULT_CSP).not.toMatch(/https?:\/\/(?!127\.0\.0\.1|localhost)/);
});

// ── Hardening response headers ────────────────────────────────────────────

test("window-policy: hardening response headers cover XCTO, XFO, Referrer-Policy", () => {
  expect(HARDENING_RESPONSE_HEADERS["X-Content-Type-Options"]).toBe("nosniff");
  expect(HARDENING_RESPONSE_HEADERS["X-Frame-Options"]).toBe("DENY");
  expect(HARDENING_RESPONSE_HEADERS["Referrer-Policy"]).toBe("no-referrer");
});

// ── Navigation guards ─────────────────────────────────────────────────────

test("window-policy: isAllowedNavigationUrl approves only the initial loopback origin", () => {
  const localhostPolicy = navigationPolicyForInitialUrl("http://localhost:3779/index.html");
  const ipv4Policy = navigationPolicyForInitialUrl("http://127.0.0.1:3779/index.html");

  expect(isAllowedNavigationUrl("http://localhost:3779/index.html", localhostPolicy)).toBe(true);
  expect(isAllowedNavigationUrl("http://localhost:3779/assets/index.js", localhostPolicy)).toBe(true);
  expect(isAllowedNavigationUrl("http://localhost:3780/index.html", localhostPolicy)).toBe(false);
  expect(isAllowedNavigationUrl("http://127.0.0.1:3779/index.html", localhostPolicy)).toBe(false);
  expect(isAllowedNavigationUrl("http://localhost:3779/index.html", ipv4Policy)).toBe(false);
  expect(isAllowedNavigationUrl("http://127.0.0.1:3779/index.html", ipv4Policy)).toBe(true);
  expect(isAllowedNavigationUrl("http://127.0.0.1:3780/index.html", ipv4Policy)).toBe(false);
  expect(isAllowedNavigationUrl("http://127.0.0.1:3779/")).toBe(false);
  expect(isAllowedNavigationUrl("http://localhost.evil.example/")).toBe(false);
  expect(isAllowedNavigationUrl("http://127.0.0.1.evil.example/")).toBe(false);
  expect(isAllowedNavigationUrl("https://127.0.0.1.evil.example/")).toBe(false);
  expect(isAllowedNavigationUrl("http://localhost@evil.example/")).toBe(false);
  expect(isAllowedNavigationUrl("https://[::1]:3779/index.html")).toBe(false);
  expect(isAllowedNavigationUrl("https://[::ffff:127.0.0.1]:3779/index.html")).toBe(false);
});

test("window-policy: isAllowedNavigationUrl approves only app-root file URLs", () => {
  const policy = navigationPolicyForInitialUrl("file:///opt/hoopoe/dist/index.html");
  expect(isAllowedNavigationUrl("file:///opt/hoopoe/dist/index.html", policy)).toBe(true);
  expect(isAllowedNavigationUrl("file:///opt/hoopoe/dist/assets/index.js", policy)).toBe(true);
  expect(isAllowedNavigationUrl("file:///opt/hoopoe/dist/../evil.html", policy)).toBe(false);
  expect(isAllowedNavigationUrl("file:///tmp/evil.html", policy)).toBe(false);
  expect(isAllowedNavigationUrl("file:///Users/ubuntu/Downloads/evil.html", policy)).toBe(false);
  expect(isAllowedNavigationUrl("file:///opt/hoopoe/dist/index.html")).toBe(false);
});

test("window-policy: isAllowedNavigationUrl rejects external origins + dangerous schemes", () => {
  expect(isAllowedNavigationUrl("https://evil.example.com/")).toBe(false);
  expect(isAllowedNavigationUrl("http://example.com/")).toBe(false);
  expect(isAllowedNavigationUrl("javascript:throw 1")).toBe(false);
  expect(isAllowedNavigationUrl("data:text/html,<script>")).toBe(false);
});

test("window-policy: ALLOWED_NAVIGATION_ORIGINS pins to loopback origins", () => {
  expect(ALLOWED_NAVIGATION_ORIGINS).toContain("http://127.0.0.1");
  expect(ALLOWED_NAVIGATION_ORIGINS).toContain("https://127.0.0.1");
  expect(ALLOWED_NAVIGATION_ORIGINS).toContain("http://localhost");
  expect(ALLOWED_NAVIGATION_ORIGINS).not.toContain("file://");
  // No public origin sneaks in: every entry must be loopback.
  for (const origin of ALLOWED_NAVIGATION_ORIGINS) {
    const isLoopback = origin.includes("127.0.0.1") || origin.includes("localhost");
    expect(isLoopback).toBe(true);
  }
});

// ── hp-iq8f: dev-mode CSP + selector ──────────────────────────────────────

test("window-policy: DEFAULT_CSP MUST NOT contain 'unsafe-inline' or 'unsafe-eval' on script-src (prod-strict invariant)", () => {
  // Regression guard. The dev relaxation in DEV_CSP_FOR_VITE must
  // NEVER bleed into the prod CSP. Renderer isolation (Guardrail 2
  // spirit) depends on this. style-src `'unsafe-inline'` is the
  // documented Tailwind-runtime exception (see comment on
  // DEFAULT_CSP); script-src must stay locked.
  const scriptSrcMatch = /script-src ([^;]+);/.exec(DEFAULT_CSP);
  expect(scriptSrcMatch).not.toBeNull();
  const scriptSrc = scriptSrcMatch?.[1] ?? "";
  expect(scriptSrc).not.toContain("'unsafe-inline'");
  expect(scriptSrc).not.toContain("'unsafe-eval'");
  expect(scriptSrc).not.toContain("127.0.0.1:5173");
});

test("window-policy: DEV_CSP_FOR_VITE allows the inline preamble + eval-based Fast Refresh", () => {
  const scriptSrcMatch = /script-src ([^;]+);/.exec(DEV_CSP_FOR_VITE);
  expect(scriptSrcMatch).not.toBeNull();
  const scriptSrc = scriptSrcMatch?.[1] ?? "";
  expect(scriptSrc).toContain("'self'");
  expect(scriptSrc).toContain("'unsafe-inline'");
  expect(scriptSrc).toContain("'unsafe-eval'");
  expect(scriptSrc).toContain("http://127.0.0.1:5173");
});

test("window-policy: DEV_CSP_FOR_VITE permits Vite asset/style fetches from the dev server", () => {
  expect(DEV_CSP_FOR_VITE).toMatch(/style-src[^;]+http:\/\/127\.0\.0\.1:5173/);
  expect(DEV_CSP_FOR_VITE).toMatch(/img-src[^;]+http:\/\/127\.0\.0\.1:5173/);
  expect(DEV_CSP_FOR_VITE).toMatch(/font-src[^;]+http:\/\/127\.0\.0\.1:5173/);
  expect(DEV_CSP_FOR_VITE).toMatch(/default-src[^;]+http:\/\/127\.0\.0\.1:5173/);
});

test("window-policy: DEV_CSP_FOR_VITE keeps object-src / base-uri / frame-ancestors strict", () => {
  expect(DEV_CSP_FOR_VITE).toContain("object-src 'none'");
  expect(DEV_CSP_FOR_VITE).toContain("base-uri 'self'");
  expect(DEV_CSP_FOR_VITE).toContain("frame-ancestors 'none'");
  expect(DEV_CSP_FOR_VITE).toContain("form-action 'self'");
});

test("window-policy: DEV_CSP_FOR_VITE connect-src already permits the HMR websocket (loopback ws)", () => {
  // Vite's HMR connects to ws://127.0.0.1:5173. The shared
  // connect-src list already covers ws://127.0.0.1:* so HMR works
  // without further relaxation.
  expect(DEV_CSP_FOR_VITE).toMatch(/connect-src[^;]+ws:\/\/127\.0\.0\.1:\*/);
});

test("selectCsp: HOOPOE_VITE_URL unset returns the strict DEFAULT_CSP", () => {
  expect(selectCsp({})).toBe(DEFAULT_CSP);
});

test("selectCsp: HOOPOE_VITE_URL='' returns the strict DEFAULT_CSP", () => {
  expect(selectCsp({ HOOPOE_VITE_URL: "" })).toBe(DEFAULT_CSP);
});

test("selectCsp: whitespace-only HOOPOE_VITE_URL still returns DEFAULT_CSP (treated as unset after trim)", () => {
  expect(selectCsp({ HOOPOE_VITE_URL: "   \n\t " })).toBe(DEFAULT_CSP);
});

test("selectCsp: real HOOPOE_VITE_URL returns DEV_CSP_FOR_VITE", () => {
  expect(
    selectCsp({ HOOPOE_VITE_URL: "http://127.0.0.1:5173" }),
  ).toBe(DEV_CSP_FOR_VITE);
});

test("selectCsp: leading/trailing whitespace around a real URL still returns DEV_CSP_FOR_VITE", () => {
  expect(
    selectCsp({ HOOPOE_VITE_URL: "  http://127.0.0.1:5173  " }),
  ).toBe(DEV_CSP_FOR_VITE);
});
