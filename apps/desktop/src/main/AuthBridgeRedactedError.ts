// Hoopoe-owned. Redaction-aware error class for AuthBridge.
//
// hp-zir / hp-k9ys: caller-visible AuthBridge error messages must never
// contain credential material. The constructor scans for three classes of
// leakage and throws a meta-error (refusing to construct the
// AuthBridgeRedactedError) if any are found:
//
//   1) shape denylist — known credential prefixes (JWT, Hoopoe bearer /
//      pairing / WS / PAT, GitHub PAT/OAuth, GitLab PAT, Slack, Stripe,
//      AWS, npm, Google OAuth, etc.) and PEM key blocks;
//   2) Authorization-header schemes (`bearer ` / `basic ` followed by a
//      space) — refuses messages that look like a header echo;
//   3) entropy gate — any url-safe substring of length > 16 with Shannon
//      entropy ≥ 4.5 bits/char (catches HMAC-style opaque bearers without
//      a recognizable prefix, which is the AuthBridge case per
//      review-findings.md "AuthBridgeRedactedError token-shape detection
//      is too narrow").
//
// Pure hex strings are allowlisted in the entropy gate because git SHAs
// and HMAC hex digests are bounded at log2(16) = 4 bits/char by their
// 16-character alphabet — naturally excluded by the 4.5 threshold but
// allowlisted explicitly for clarity. UUIDs (with or without dashes) sit
// at ~3.5–4.0 bits/char and likewise stay below the threshold.

// codex-shape-scrub-ok: The literal token-shape strings below ('eyj',
// 'hp-bearer-', 'ghp_', 'akia', etc.) are the *matchers*, not credentials.
// They are intentionally hard-coded so the redactor can detect leakage at
// construction time. This file is the only place these literals should
// appear in the main process. Stored lowercase because we lowercase the
// candidate message before matching — token shapes that are case-sensitive
// in the wild (e.g. AWS `AKIA...`, JWT `eyJ...`) are still matched
// correctly because their case-folded forms remain distinctive.
const TOKEN_SHAPE_PREFIXES: readonly string[] = [
  // Hoopoe ecosystem (legacy fixture + future opaque forms)
  "hp-bearer-",
  "hp-pairing-",
  "hp-wstoken-",
  "hp-pat-",
  "hp-refresh-",
  "hop_",
  "hpat_",
  "hopaq_",
  // JWT (any JWT header begins with base64url-encoded '{"' → 'eyJ')
  "eyj",
  // GitHub fine-grained PATs / OAuth tokens
  "ghp_",
  "gho_",
  "ghu_",
  "ghs_",
  "ghr_",
  // GitLab personal-access tokens
  "glpat-",
  // Slack tokens
  "xoxp-",
  "xoxb-",
  "xoxa-",
  "xoxs-",
  "xoxr-",
  // Stripe API keys
  "sk_live_",
  "sk_test_",
  "pk_live_",
  "pk_test_",
  "rk_live_",
  "rk_test_",
  // AWS access keys (uppercase in the wild — folded to lowercase here)
  "akia",
  "asia",
  "agpa",
  "anpa",
  // npm publish tokens
  "npm_",
  // Generic personal-access-token prefix
  "pat_",
  // Google OAuth refresh / access
  "1//0",
  "ya29.",
  // Sentry user auth
  "sntrys_",
];

const PEM_CREDENTIAL_MARKERS: readonly string[] = [
  "-----BEGIN OPENSSH PRIVATE KEY-----",
  "-----BEGIN ENCRYPTED PRIVATE KEY-----",
  "-----BEGIN RSA PRIVATE KEY-----",
  "-----BEGIN EC PRIVATE KEY-----",
  "-----BEGIN DSA PRIVATE KEY-----",
  "-----BEGIN PRIVATE KEY-----",
  "-----BEGIN PGP PRIVATE KEY BLOCK-----",
];

const AUTHORIZATION_HEADER_SCHEMES: readonly string[] = [
  "bearer ",
  "basic ",
];

const HIGH_ENTROPY_BITS_PER_CHAR_THRESHOLD = 4.5;
const MIN_OPAQUE_TOKEN_LENGTH = 17;
// Second-tier gate: long url-safe substrings with multiple character
// classes are suspect even when entropy dips below 4.5 (e.g. structured
// opaque bearers that repeat a shape but still mix lower / upper / digit
// / `-` / `_`). UUIDs slip below this gate because they only mix digit /
// hex-letter / dash and so register at most 3 classes BUT have entropy
// well below 4.0.
const STRUCTURED_TOKEN_MIN_LENGTH = 32;
const STRUCTURED_TOKEN_MIN_ENTROPY = 4.0;
// 4 classes filters out CamelCase identifiers (lower / upper / digit only —
// 3 classes max) but still catches structured opaque bearers that mix in
// `-` or `_`. UUIDs land below this gate too because they only mix lower
// hex / digit / dash (3 classes) AND have entropy below 4.0.
const STRUCTURED_TOKEN_MIN_CLASSES = 4;

const URL_SAFE_TOKEN_RE = /[A-Za-z0-9_-]{17,}/g;
const PURE_HEX_RE = /^[0-9a-fA-F]+$/;

export interface RedactionViolation {
  readonly kind: "prefix" | "pem-marker" | "auth-scheme" | "entropy";
  readonly detail: string;
}

export function detectTokenShape(message: string): RedactionViolation | null {
  const lower = message.toLowerCase();

  for (const prefix of TOKEN_SHAPE_PREFIXES) {
    if (lower.includes(prefix)) {
      return { kind: "prefix", detail: prefix };
    }
  }

  for (const marker of PEM_CREDENTIAL_MARKERS) {
    if (message.includes(marker)) {
      return { kind: "pem-marker", detail: marker };
    }
  }

  for (const scheme of AUTHORIZATION_HEADER_SCHEMES) {
    if (lower.includes(scheme)) {
      return { kind: "auth-scheme", detail: scheme.trim() };
    }
  }

  const candidates = message.match(URL_SAFE_TOKEN_RE);
  if (candidates !== null) {
    for (const candidate of candidates) {
      if (candidate.length < MIN_OPAQUE_TOKEN_LENGTH) continue;
      if (isAllowlistedOpaqueShape(candidate)) continue;
      const entropy = shannonEntropyBitsPerChar(candidate);
      if (entropy >= HIGH_ENTROPY_BITS_PER_CHAR_THRESHOLD) {
        return {
          kind: "entropy",
          detail: `urlsafe-len=${candidate.length}-entropy>=${HIGH_ENTROPY_BITS_PER_CHAR_THRESHOLD}`,
        };
      }
      if (
        candidate.length >= STRUCTURED_TOKEN_MIN_LENGTH &&
        entropy >= STRUCTURED_TOKEN_MIN_ENTROPY &&
        countCharacterClasses(candidate) >= STRUCTURED_TOKEN_MIN_CLASSES
      ) {
        return {
          kind: "entropy",
          detail: `urlsafe-len=${candidate.length}-classes>=${STRUCTURED_TOKEN_MIN_CLASSES}`,
        };
      }
    }
  }

  return null;
}

function countCharacterClasses(value: string): number {
  let classes = 0;
  if (/[a-z]/.test(value)) classes += 1;
  if (/[A-Z]/.test(value)) classes += 1;
  if (/[0-9]/.test(value)) classes += 1;
  if (value.includes("-")) classes += 1;
  if (value.includes("_")) classes += 1;
  return classes;
}

export function shannonEntropyBitsPerChar(value: string): number {
  if (value.length === 0) return 0;
  const counts = new Map<string, number>();
  for (const ch of value) {
    counts.set(ch, (counts.get(ch) ?? 0) + 1);
  }
  let entropy = 0;
  for (const count of counts.values()) {
    const probability = count / value.length;
    entropy -= probability * Math.log2(probability);
  }
  return entropy;
}

function isAllowlistedOpaqueShape(candidate: string): boolean {
  // Pure hex is bounded at 4 bits/char by the 16-char alphabet (covers
  // git SHAs, HMAC hex digests, content hashes, UUIDs without dashes).
  if (PURE_HEX_RE.test(candidate)) return true;
  return false;
}

export class AuthBridgeRedactedError extends Error {
  constructor(message: string) {
    const violation = detectTokenShape(message);
    if (violation !== null) {
      throw new Error(
        `AuthBridge error message contained a token-like string (${violation.kind}:${violation.detail})`,
      );
    }
    super(message);
    this.name = "AuthBridgeRedactedError";
  }
}
