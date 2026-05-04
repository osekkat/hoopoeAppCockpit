// hp-llpe - Electron renderer, cache, and transport hardening checks.
//
// This module is deliberately Electron-free so policy can be asserted in Bun
// tests and reused by future main-process wiring without loading Electron.

export type FindingSeverity = "error" | "warning";

export interface SecurityFinding {
  readonly id: string;
  readonly severity: FindingSeverity;
  readonly message: string;
}

export interface RendererHardeningInput {
  readonly webPreferences: Record<string, unknown>;
  readonly csp: string;
  readonly hardeningHeaders: Record<string, string | undefined>;
  readonly navigationOrigins: readonly string[];
}

export interface CacheValidationResult {
  readonly ok: boolean;
  readonly findings: readonly SecurityFinding[];
}

const TRUE_PREFS = [
  "contextIsolation",
  "sandbox",
  "webSecurity",
] as const;

const FALSE_PREFS = [
  "nodeIntegration",
  "nodeIntegrationInWorker",
  "nodeIntegrationInSubFrames",
  "allowRunningInsecureContent",
  "experimentalFeatures",
] as const;

const LOOPBACK_CONNECT_SOURCES = new Set([
  "'self'",
  "http://127.0.0.1:*",
  "http://localhost:*",
  "ws://127.0.0.1:*",
  "ws://localhost:*",
  "wss://127.0.0.1:*",
  "wss://localhost:*",
]);

const PROVIDER_API_KEY_NAMES = ["OPENAI", "ANTHROPIC", "GEMINI"].map(
  (provider) => `${provider}_API_KEY`,
);

const SENSITIVE_CACHE_KEY_RE = /(?:bearer|pairing|passphrase|privatekey|api[_-]?key|credential|cookie|secret|keychain|safeStorage|wsToken|accessToken|refreshToken)/i;
const SENSITIVE_CACHE_VALUE_RE = new RegExp(
  `(?:Bearer\\s+[A-Za-z0-9._~+/-]+|HOOPOE_PAIRING_TOKEN=|(?:${PROVIDER_API_KEY_NAMES.map(escapeRegExp).join("|")})=|-----BEGIN [A-Z ]*PRIVATE KEY-----)`,
);

export function validateRendererHardening(input: RendererHardeningInput): readonly SecurityFinding[] {
  const findings: SecurityFinding[] = [];
  for (const key of TRUE_PREFS) {
    if (input.webPreferences[key] !== true) {
      findings.push(error(`webPreferences.${key}`, `${key} must be true`));
    }
  }
  for (const key of FALSE_PREFS) {
    if (input.webPreferences[key] !== false) {
      findings.push(error(`webPreferences.${key}`, `${key} must be false`));
    }
  }
  if (input.webPreferences.enableBlinkFeatures !== "") {
    findings.push(error("webPreferences.enableBlinkFeatures", "enableBlinkFeatures must be empty"));
  }

  const csp = parseCsp(input.csp);
  expectDirective(csp, "default-src", ["'self'"], findings);
  expectDirective(csp, "object-src", ["'none'"], findings);
  expectDirective(csp, "base-uri", ["'self'"], findings);
  expectDirective(csp, "form-action", ["'self'"], findings);
  expectDirective(csp, "frame-ancestors", ["'none'"], findings);

  const scriptSrc = csp.get("script-src") ?? [];
  if (!scriptSrc.includes("'self'")) {
    findings.push(error("csp.script-src.self", "script-src must include 'self'"));
  }
  for (const forbidden of ["'unsafe-inline'", "'unsafe-eval'", "*"]) {
    if (scriptSrc.includes(forbidden)) {
      findings.push(error("csp.script-src." + forbidden, `script-src must not include ${forbidden}`));
    }
  }

  const connectSrc = csp.get("connect-src") ?? [];
  if (connectSrc.length === 0) {
    findings.push(error("csp.connect-src.missing", "connect-src is required"));
  }
  for (const source of connectSrc) {
    if (!LOOPBACK_CONNECT_SOURCES.has(source)) {
      findings.push(error("csp.connect-src.public", `connect-src source ${source} is not loopback/self`));
    }
  }

  const imageSrc = csp.get("img-src") ?? [];
  if (imageSrc.includes("http:") || imageSrc.includes("https:") || imageSrc.includes("*")) {
    findings.push(error("csp.img-src.remote", "img-src must not allow remote images"));
  }

  if (input.hardeningHeaders["X-Content-Type-Options"] !== "nosniff") {
    findings.push(error("headers.xcto", "X-Content-Type-Options must be nosniff"));
  }
  if (input.hardeningHeaders["X-Frame-Options"] !== "DENY") {
    findings.push(error("headers.xfo", "X-Frame-Options must be DENY"));
  }
  if (input.hardeningHeaders["Referrer-Policy"] !== "no-referrer") {
    findings.push(error("headers.referrer", "Referrer-Policy must be no-referrer"));
  }

  for (const origin of input.navigationOrigins) {
    if (!isLoopbackOrFileOrigin(origin)) {
      findings.push(error("navigation.origin", `navigation origin ${origin} is not loopback/file`));
    }
  }
  return findings;
}

export function validateCachePayload(payload: unknown): CacheValidationResult {
  const findings: SecurityFinding[] = [];
  visitCachePayload(payload, "$", findings, 0);
  return { ok: findings.length === 0, findings };
}

export function isCacheKeyAllowed(key: string): boolean {
  return !SENSITIVE_CACHE_KEY_RE.test(key);
}

export function parseCsp(csp: string): Map<string, readonly string[]> {
  const directives = new Map<string, readonly string[]>();
  for (const part of csp.split(";")) {
    const trimmed = part.trim();
    if (trimmed === "") continue;
    const tokens = trimmed.split(/\s+/);
    const name = tokens[0];
    if (name === undefined || name === "") continue;
    const values = tokens.slice(1);
    directives.set(name.toLowerCase(), values);
  }
  return directives;
}

function expectDirective(
  csp: ReadonlyMap<string, readonly string[]>,
  directive: string,
  expected: readonly string[],
  findings: SecurityFinding[],
): void {
  const actual = csp.get(directive);
  if (!actual || expected.some((value) => !actual.includes(value))) {
    findings.push(error("csp." + directive, `${directive} must include ${expected.join(" ")}`));
  }
}

function visitCachePayload(
  value: unknown,
  path: string,
  findings: SecurityFinding[],
  depth: number,
): void {
  if (depth > 16) {
    findings.push(error("cache.depth", `${path} exceeds supported cache nesting depth`));
    return;
  }
  if (typeof value === "string") {
    if (SENSITIVE_CACHE_VALUE_RE.test(value)) {
      findings.push(error("cache.value.secret", `${path} contains a secret-looking value`));
    }
    return;
  }
  if (Array.isArray(value)) {
    value.forEach((item, index) => visitCachePayload(item, `${path}[${index}]`, findings, depth + 1));
    return;
  }
  if (!value || typeof value !== "object") {
    return;
  }
  for (const [key, child] of Object.entries(value as Record<string, unknown>)) {
    const childPath = `${path}.${key}`;
    if (!isCacheKeyAllowed(key)) {
      findings.push(error("cache.key.secret", `${childPath} must not be written to desktop cache`));
    }
    visitCachePayload(child, childPath, findings, depth + 1);
  }
}

function isLoopbackOrFileOrigin(origin: string): boolean {
  if (origin === "file://") return true;
  try {
    const parsed = new URL(origin);
    return (
      (parsed.protocol === "http:" || parsed.protocol === "https:") &&
      (parsed.hostname === "127.0.0.1" || parsed.hostname === "localhost" || parsed.hostname === "::1")
    );
  } catch {
    return false;
  }
}

function escapeRegExp(value: string): string {
  return value.replace(/[.*+?^${}()|[\]\\]/g, "\\$&");
}

function error(id: string, message: string): SecurityFinding {
  return { id, severity: "error", message };
}
