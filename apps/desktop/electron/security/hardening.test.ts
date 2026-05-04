import { expect, test } from "bun:test";
import {
  ALLOWED_NAVIGATION_ORIGINS,
  DEFAULT_CSP,
  HARDENING_RESPONSE_HEADERS,
  SAFE_WEB_PREFERENCES,
} from "../../src/main/window-policy.ts";
import {
  isCacheKeyAllowed,
  parseCsp,
  validateCachePayload,
  validateRendererHardening,
} from "./hardening.ts";

test("validateRendererHardening: accepts the production window policy", () => {
  const findings = validateRendererHardening({
    webPreferences: SAFE_WEB_PREFERENCES as Record<string, unknown>,
    csp: DEFAULT_CSP,
    hardeningHeaders: HARDENING_RESPONSE_HEADERS,
    navigationOrigins: ALLOWED_NAVIGATION_ORIGINS,
  });
  expect(findings).toEqual([]);
});

test("validateRendererHardening: rejects unsafe renderer flags", () => {
  const findings = validateRendererHardening({
    webPreferences: {
      ...SAFE_WEB_PREFERENCES,
      contextIsolation: false,
      sandbox: false,
      nodeIntegration: true,
      allowRunningInsecureContent: true,
      enableBlinkFeatures: "SharedArrayBuffer",
    } as Record<string, unknown>,
    csp: DEFAULT_CSP,
    hardeningHeaders: HARDENING_RESPONSE_HEADERS,
    navigationOrigins: ALLOWED_NAVIGATION_ORIGINS,
  });
  expect(findings.map((finding) => finding.id)).toContain("webPreferences.contextIsolation");
  expect(findings.map((finding) => finding.id)).toContain("webPreferences.sandbox");
  expect(findings.map((finding) => finding.id)).toContain("webPreferences.nodeIntegration");
  expect(findings.map((finding) => finding.id)).toContain("webPreferences.allowRunningInsecureContent");
  expect(findings.map((finding) => finding.id)).toContain("webPreferences.enableBlinkFeatures");
});

test("validateRendererHardening: rejects public connect-src and inline script CSP", () => {
  const findings = validateRendererHardening({
    webPreferences: SAFE_WEB_PREFERENCES as Record<string, unknown>,
    csp: [
      "default-src 'self'",
      "script-src 'self' 'unsafe-inline'",
      "connect-src 'self' https://daemon.example.com:* *",
      "img-src 'self' https:",
      "object-src 'none'",
      "base-uri 'self'",
      "form-action 'self'",
      "frame-ancestors 'none'",
    ].join("; "),
    hardeningHeaders: HARDENING_RESPONSE_HEADERS,
    navigationOrigins: [...ALLOWED_NAVIGATION_ORIGINS, "https://daemon.example.com"],
  });
  expect(findings.map((finding) => finding.id)).toContain("csp.script-src.'unsafe-inline'");
  expect(findings.map((finding) => finding.id)).toContain("csp.connect-src.public");
  expect(findings.map((finding) => finding.id)).toContain("csp.img-src.remote");
  expect(findings.map((finding) => finding.id)).toContain("navigation.origin");
});

test("validateRendererHardening: pins required hardening headers", () => {
  const findings = validateRendererHardening({
    webPreferences: SAFE_WEB_PREFERENCES as Record<string, unknown>,
    csp: DEFAULT_CSP,
    hardeningHeaders: {
      "X-Content-Type-Options": "sniff",
      "X-Frame-Options": "SAMEORIGIN",
      "Referrer-Policy": "origin",
    },
    navigationOrigins: ALLOWED_NAVIGATION_ORIGINS,
  });
  expect(findings.map((finding) => finding.id)).toEqual([
    "headers.xcto",
    "headers.xfo",
    "headers.referrer",
  ]);
});

test("validateCachePayload: refuses secrets in desktop cache keys or values", () => {
  const providerKey = ["OPENAI", "API", "KEY"].join("_");
  const result = validateCachePayload({
    projectId: "proj_01",
    view: { stageId: "plan" },
    auth: {
      bearerToken: "Bearer abc.def.ghi",
      sshPassphrase: "correct horse",
    },
    provider: {
      [providerKey]: `${providerKey}=sk-test`,
    },
  });
  expect(result.ok).toBe(false);
  expect(result.findings.map((finding) => finding.id)).toContain("cache.key.secret");
  expect(result.findings.map((finding) => finding.id)).toContain("cache.value.secret");
});

test("validateCachePayload: allows ordinary renderer read-model fields", () => {
  const result = validateCachePayload({
    projectId: "proj_01",
    lastStageId: "swarm",
    panelWidths: { activity: 420 },
    cloneState: { branch: "main", lastFetchedSha: "a".repeat(40) },
  });
  expect(result).toEqual({ ok: true, findings: [] });
  expect(isCacheKeyAllowed("bearerToken")).toBe(false);
  expect(isCacheKeyAllowed("lastFetchedSha")).toBe(true);
});

test("parseCsp: exposes directives for targeted checks", () => {
  const parsed = parseCsp(DEFAULT_CSP);
  expect(parsed.get("script-src")).toContain("'self'");
  expect(parsed.get("connect-src")).toContain("http://127.0.0.1:*");
});
