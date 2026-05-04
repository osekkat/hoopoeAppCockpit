// hp-rflj renderer-hardening assertions. Tests target `./window-policy.ts`
// (the Electron-free constants) so bun:test can load them without spinning
// up an Electron runtime — `./WindowManager.ts` itself imports `electron`
// and is only loadable inside the real main process.

import { expect, test } from "bun:test";
import {
  ALLOWED_NAVIGATION_ORIGINS,
  DEFAULT_CSP,
  HARDENING_RESPONSE_HEADERS,
  SAFE_WEB_PREFERENCES,
  isAllowedNavigationUrl,
  navigationPolicyForInitialUrl,
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
