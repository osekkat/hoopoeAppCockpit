// codex-shape-scrub-ok: This file deliberately mints synthetic token-shaped
// strings to exercise the redactor; nothing here is a real credential. The
// expected outputs are the literal `[REDACTED]` placeholder, never the
// minted shape itself.

import { expect, test } from "bun:test";
import {
  AuthBridgeRedactedError,
  detectTokenShape,
  shannonEntropyBitsPerChar,
} from "./AuthBridgeRedactedError.ts";

const REDACTED = "[REDACTED]";

const MUST_REDACT_SHAPES: ReadonlyArray<{
  readonly label: string;
  readonly message: string;
}> = [
  { label: "JWT (eyJ-prefixed)", message: "got token eyJhbGciOiJIUzI1NiJ9 from server" },
  { label: "Hoopoe bearer fixture", message: "value=hp-bearer-1234567890ABCDEF" },
  { label: "Hoopoe pairing fixture", message: "rejected hp-pairing-ABCDEFGHJKLM" },
  { label: "Hoopoe ws-token fixture", message: "wsToken=hp-wstoken-XYZ789" },
  { label: "Hoopoe PAT fixture", message: "pat=hp-pat-personal-access-1" },
  { label: "Hoopoe new-prefix bearer", message: "got hop_2QmRGz5Y8K9JX1aB3cD4eF6gH7iJ8" },
  { label: "Hoopoe PAT new-prefix", message: "stored hpat_AbCdEf123456_GhIjKlMnOpQr" },
  { label: "GitHub PAT (40 char)", message: "header set ghp_abcdef1234567890ABCDEFGHIJKL12345678" },
  { label: "GitHub OAuth", message: "issued gho_xyzabcdef1234567890XYZABCDEFGHIJKL12" },
  { label: "GitHub server token", message: "ghs_abcdef1234567890ABCDEFGHIJKL12345678" },
  { label: "GitLab PAT", message: "private glpat-abc123def456ghi789jkl0" },
  { label: "Slack user token", message: "got xoxp-1234567890-1234567890-1234567890-abcdef" },
  { label: "Slack bot token", message: "got xoxb-1234567890-abc-DEF" },
  { label: "Stripe live key", message: "stripe sk_live_TYooMQauvdEDq54NiTphI7jx" },
  { label: "Stripe test key", message: "stripe sk_test_4eC39HqLyjWDarjtT1zdp7dc" },
  { label: "AWS access key id", message: "aws creds AKIAIOSFODNN7EXAMPLE" },
  { label: "AWS session credentials", message: "aws ASIAIOSFODNN7EXAMPLE" },
  { label: "npm publish token", message: "npm_abcdef1234567890ABCDEFGHIJKL" },
  { label: "Google OAuth refresh", message: "refresh 1//0g_RaW-QwErTyUiOpAsDfGhJkL" },
  { label: "Google OAuth access", message: "access ya29.A0ARrdaM-XYZ123abcDEF456ghi" },
  { label: "Sentry user auth token", message: "got sntrys_abcdef1234567890ABCDEF1234567890" },
  { label: "OpenSSH private key block", message: "key=-----BEGIN OPENSSH PRIVATE KEY-----..." },
  { label: "PEM RSA private key", message: "-----BEGIN RSA PRIVATE KEY-----" },
  { label: "PEM PGP private key", message: "-----BEGIN PGP PRIVATE KEY BLOCK-----" },
  { label: "Authorization Bearer header", message: "got header Authorization: Bearer abc.xyz" },
  { label: "Authorization Basic header", message: "header was Authorization: Basic ZGVtbw" },
  {
    label: "opaque entropy-only bearer",
    message: "rejected: Z9YQ4uV3pBmK1rN2sT8gH7jL5wF0xC6dE",
  },
  {
    label: "base64url HMAC bearer (no prefix)",
    message: "received X9pVMOJ8RpbiJxF6kj9KqI0V67G8Y8aBcDeFgHiJkLmN",
  },
  {
    label: "long base64url with mixed chars",
    message: "credential aB-cD_eF1234567890XyZ-aBcDeF1234567890",
  },
];

const MUST_NOT_REDACT_MESSAGES: ReadonlyArray<{
  readonly label: string;
  readonly message: string;
}> = [
  { label: "empty string", message: "" },
  { label: "current bootstrap rejection", message: "Bootstrap rejected pairing token (status 401)." },
  { label: "current ws-token rejection", message: "WS-token request rejected (status 502)." },
  {
    label: "current bootstrap missing field",
    message: "Bootstrap response missing bearerToken.",
  },
  {
    label: "git SHA in error",
    message: "found commit 1a2b3c4d5e6f7a8b9c0d1e2f3a4b5c6d7e8f9a0b in history",
  },
  { label: "short git SHA", message: "rebased onto 1a2b3c4 cleanly" },
  { label: "HMAC hex digest", message: "checksum mismatch: ABCDEF0123456789ABCDEF0123456789" },
  {
    label: "absolute file path",
    message: "could not read /home/user/Projects/hoopoe-cockpit/apps/desktop/src/main/AuthBridge.ts",
  },
  {
    label: "branch name",
    message: "branch feature/auth-improvements is behind origin/main",
  },
  {
    label: "version string",
    message: "version 1.2.3-beta.4+commit.abc1234 is out of date",
  },
  { label: "ISO timestamp", message: "rejected at 2026-05-02T22:08:31.810355+00:00" },
  { label: "bead id", message: "see hp-2qgx for context" },
  { label: "environment id", message: "environmentId env-abc-1 not found" },
  { label: "loopback URL", message: "daemon at http://127.0.0.1:3779 is unreachable" },
  { label: "status code phrase", message: "Status code 401 (Unauthorized)" },
  { label: "Crockford pairing format", message: "Pairing token expected 12 Crockford chars" },
  { label: "lowercase low-entropy id", message: "wsTokenForServiceAccountTimedOut" },
  { label: "punctuated UUID", message: "uuid 550e8400-e29b-41d4-a716-446655440000 not found" },
];

function constructorThrows(message: string): boolean {
  try {
    new AuthBridgeRedactedError(message);
    return false;
  } catch {
    return true;
  }
}

for (const { label, message } of MUST_REDACT_SHAPES) {
  test(`AuthBridgeRedactedError refuses: ${label}`, () => {
    expect(constructorThrows(message)).toBe(true);
    const violation = detectTokenShape(message);
    expect(violation).not.toBeNull();
    expect(REDACTED).toBe("[REDACTED]");
  });
}

for (const { label, message } of MUST_NOT_REDACT_MESSAGES) {
  test(`AuthBridgeRedactedError accepts: ${label}`, () => {
    expect(constructorThrows(message)).toBe(false);
    const error = new AuthBridgeRedactedError(message);
    expect(error).toBeInstanceOf(AuthBridgeRedactedError);
    expect(error.message).toBe(message);
    expect(detectTokenShape(message)).toBeNull();
  });
}

test("shannonEntropyBitsPerChar: empty string is zero", () => {
  expect(shannonEntropyBitsPerChar("")).toBe(0);
});

test("shannonEntropyBitsPerChar: repeated single char is zero", () => {
  expect(shannonEntropyBitsPerChar("aaaaaaaaaaaaaaaa")).toBe(0);
});

test("shannonEntropyBitsPerChar: 16-char hex is bounded at 4 bits", () => {
  expect(shannonEntropyBitsPerChar("0123456789abcdef")).toBeCloseTo(4, 5);
});

test("shannonEntropyBitsPerChar: random url-safe string exceeds 4.5 bits", () => {
  const candidate = "AbCdEfGhIjKlMnOpQrStUvWxYz0123_-";
  expect(shannonEntropyBitsPerChar(candidate)).toBeGreaterThanOrEqual(4.5);
});

test("property: random base64url strings of length 32 are always redacted", () => {
  const alphabet = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789-_";
  let seed = 0xC0FFEE;
  const next = () => {
    seed = (seed * 1664525 + 1013904223) >>> 0;
    return seed;
  };
  const failures: string[] = [];
  for (let trial = 0; trial < 64; trial += 1) {
    let candidate = "";
    for (let i = 0; i < 32; i += 1) {
      candidate += alphabet[next() % alphabet.length];
    }
    const message = `received ${candidate} from upstream`;
    if (!constructorThrows(message)) {
      failures.push(candidate);
    }
  }
  expect(failures).toEqual([]);
});

test("property: pure hex digests up to 64 chars are NEVER redacted", () => {
  const hex = "0123456789abcdef";
  let seed = 0xBEEF;
  const next = () => {
    seed = (seed * 1664525 + 1013904223) >>> 0;
    return seed;
  };
  const falsePositives: string[] = [];
  for (const length of [16, 24, 32, 40, 48, 56, 64] as const) {
    for (let trial = 0; trial < 8; trial += 1) {
      let candidate = "";
      for (let i = 0; i < length; i += 1) {
        candidate += hex[next() % hex.length];
      }
      if (constructorThrows(`commit ${candidate} found`)) {
        falsePositives.push(candidate);
      }
    }
  }
  expect(falsePositives).toEqual([]);
});

test("property: long file paths are NEVER redacted", () => {
  const segments = [
    "home", "user", "Projects", "hoopoeAppCockpit", "apps", "desktop",
    "src", "main", "AuthBridgeRedactedError", "ts",
  ];
  const falsePositives: string[] = [];
  for (let i = 1; i <= segments.length; i += 1) {
    const path = "/" + segments.slice(0, i).join("/");
    if (constructorThrows(`failed to read ${path}`)) {
      falsePositives.push(path);
    }
  }
  expect(falsePositives).toEqual([]);
});

test("property: low-entropy long strings are NEVER redacted", () => {
  const samples = [
    "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
    "abcabcabcabcabcabcabcabcabcabcabc",
    "1212121212121212121212121212121212",
    "wsTokenForServiceAccountTimedOutAfter30Seconds",
  ];
  const falsePositives: string[] = [];
  for (const candidate of samples) {
    if (constructorThrows(`value ${candidate} reported`)) {
      falsePositives.push(candidate);
    }
  }
  expect(falsePositives).toEqual([]);
});

test("AuthBridgeRedactedError preserves message identity for safe inputs", () => {
  const safe = "Bootstrap rejected pairing token (status 401).";
  const error = new AuthBridgeRedactedError(safe);
  expect(error.message).toBe(safe);
  expect(error.name).toBe("AuthBridgeRedactedError");
});

test("violation report carries the kind and a non-token detail", () => {
  const jwtViolation = detectTokenShape("got eyJabc def from peer");
  expect(jwtViolation?.kind).toBe("prefix");
  expect(jwtViolation?.detail).toBe("eyj");

  const pemViolation = detectTokenShape("-----BEGIN OPENSSH PRIVATE KEY-----");
  expect(pemViolation?.kind).toBe("pem-marker");

  const headerViolation = detectTokenShape("Authorization: Bearer abc");
  expect(headerViolation?.kind).toBe("auth-scheme");

  const entropyViolation = detectTokenShape("opaque Z9YQ4uV3pBmK1rN2sT8gH7jL5wF0xC6dE");
  expect(entropyViolation?.kind).toBe("entropy");
});
