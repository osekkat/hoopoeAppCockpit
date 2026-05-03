// Hoopoe-owned. Audit trail for security-relevant settings changes (hp-wg5p).
//
// Per the bead's "Audit on change" contract: a defined subset of settings
// (channel, telemetry, mock-flywheel-mode, skill pin, push policy, safety
// preset, model-context policy) MUST write a `setting_changed` audit
// entry on every change. Routine settings (e.g., default editor command)
// MUST NOT write audit entries.
//
// This module is the single enforcement point. It does not decide which
// settings are security-relevant — that policy lives in
// `SECURITY_RELEVANT_SETTING_KEYS` here so it can be searched in CI.
//
// Cross-references:
//   - bead hp-wg5p (this module's spec)
//   - apps/desktop/src/main/SettingsBridge.ts (the persistence layer)
//   - plan.md §10 audit log
//   - plan.md §5 security model — never log secrets
//
// Hard rules:
//   - Audit values are RAW (pre-redaction would lose forensic value); the
//     audit sink is responsible for storage encryption / access control.
//     This module never logs to console, only emits via the configured
//     sink — so the SettingsBridge's logger separation holds.
//   - No setting whose key contains "token", "password", "secret",
//     "passphrase", "bearer", "key" (etc.) may EVER be added to the
//     security-relevant list — those are SECRETS, not settings, and live
//     in `SecretStore.ts`. Adding one fails the build via the
//     `SECRET_KEY_FORBIDDEN_PATTERNS` regex check at module-load time.

/** Setting key paths whose every change MUST emit a `setting_changed`
 *  audit entry. Dotted paths into the SettingsBridge HoopoeSettings shape.
 *  Add to this list when introducing a new security-relevant setting; the
 *  CI lint rule (`scripts/rendererlint/check-renderer-isolation.ts` or
 *  similar) greps for matches against `RELAUNCH_KEYS`-style enforcement. */
export const SECURITY_RELEVANT_SETTING_KEYS: ReadonlySet<string> = new Set<string>([
  "desktop.updateChannel",
  "desktop.serverExposureMode",
  "desktop.telemetryOptIn",
  "desktop.mockFlywheelEnabled",
  "skills.installerPreference",
  "skills.lockfile",
  "project.pushPolicy",
  "project.safetyPreset",
  "project.modelContextPolicy",
  "daemon.tendingEnabled",
  "diagnostics.recoveryShellAccess",
]);

/** Patterns that look like secret-bearing keys. Audit keys matching these
 *  patterns are FORBIDDEN — secrets must use SecretStore, not settings.
 *  Module-load-time guard fires if a security-relevant key matches. */
const SECRET_KEY_FORBIDDEN_PATTERNS: readonly RegExp[] = [
  /token/i,
  /password/i,
  /secret/i,
  /passphrase/i,
  /bearer/i,
  /\.key$/i,
  /apiKey/i,
];

(function assertNoSecretsInAuditList(): void {
  for (const key of SECURITY_RELEVANT_SETTING_KEYS) {
    for (const rx of SECRET_KEY_FORBIDDEN_PATTERNS) {
      if (rx.test(key)) {
        throw new Error(
          `SettingsAuditTrail: '${key}' looks like a secret-bearing key; ` +
            "secrets MUST live in SecretStore, not in the audit-eligible settings list.",
        );
      }
    }
  }
})();

/** A single settings-change candidate. */
export interface SettingsChangeCandidate {
  readonly key: string;
  readonly oldValue: unknown;
  readonly newValue: unknown;
  readonly actor: SettingsActor;
  readonly tier: "user" | "project" | "default" | "env";
  readonly ts?: string;
}

/** Actor descriptor matches plan.md §10 audit-log shape. `kind: 'mock'`
 *  is the kind for mock-flywheel-driven changes (per hp-lddj). */
export interface SettingsActor {
  readonly kind: "user" | "agent" | "mock" | "system";
  readonly id?: string;
  readonly source?: string;
}

/** Persistence-shape audit entry. Adapter sinks (file / SQLite / WS) emit
 *  this exact JSON shape so post-hoc parsers / replay tooling work
 *  uniformly. */
export interface SettingsAuditEntry {
  readonly entry: "setting_changed";
  readonly key: string;
  readonly oldValue: unknown;
  readonly newValue: unknown;
  readonly actor: SettingsActor;
  readonly tier: "user" | "project" | "default" | "env";
  readonly ts: string;
}

/** Sink interface — write a single audit entry (assumed durable). */
export type SettingsAuditSink = (entry: SettingsAuditEntry) => void | Promise<void>;

/** Synchronous-only sink. SettingsBridge requires sync semantics so a sink
 *  failure can synchronously roll back the in-flight setting change before
 *  any disk write happens (hp-6obn at-most-once delivery contract). Callers
 *  with async sinks (network logger / DB) must own their own buffering. */
export type SyncSettingsAuditSink = (entry: SettingsAuditEntry) => void;

/** Transactional batch sink (Phase 1.5 cross-review fix). Receives ALL
 *  entries from a single setUserSettings / setProjectSettings call and
 *  either commits them all or commits none — if the sink throws, no audit
 *  rows are persisted. SettingsBridge prefers this sink over the per-entry
 *  `SyncSettingsAuditSink` because per-entry sinks are inherently
 *  non-transactional (a successful first-call commit cannot be rolled back
 *  if a later call fails). */
export type SyncSettingsAuditBatchSink = (
  entries: readonly SettingsAuditEntry[],
) => void;

/** Defense-in-depth value redactor (hp-6obn + Guardrail 11). Runs against
 *  `oldValue` / `newValue` strings before they reach the sink. The curated
 *  `SECURITY_RELEVANT_SETTING_KEYS` list intentionally excludes secret-bearing
 *  keys (those live in SecretStore), but if a future maintainer ever adds a
 *  key whose value happens to match a secret pattern, this guard catches it
 *  before the audit log persists the leak.
 *
 *  Pattern coverage mirrors hp-je1p's daemon-side redaction (kept in sync
 *  by the cross-package drift check). Each pattern returns the replacement
 *  marker so audit reviewers can see WHICH class fired without reconstructing
 *  the value. */
export interface AuditRedactionPattern {
  readonly id: string;
  readonly regex: RegExp;
  readonly replacement: string;
}

const DEFAULT_AUDIT_REDACTION_PATTERNS: readonly AuditRedactionPattern[] = [
  // JWT-shaped (header.payload.signature with base64url segments).
  {
    id: "jwt",
    regex: /eyJ[A-Za-z0-9_-]{8,}\.[A-Za-z0-9_-]{8,}\.[A-Za-z0-9_-]{8,}/g,
    replacement: "[redacted-jwt]",
  },
  // Hoopoe bearer prefix (used in fixtures + plan.md auth shape).
  {
    id: "hoopoe-bearer",
    regex: /hp-bearer-[A-Za-z0-9_-]{4,}/g,
    replacement: "[redacted-hoopoe-bearer]",
  },
  // Anthropic API keys.
  { id: "anthropic-key", regex: /sk-ant-[A-Za-z0-9_-]{8,}/g, replacement: "[redacted-anthropic-key]" },
  // OpenAI keys.
  { id: "openai-key", regex: /sk-[A-Za-z0-9_-]{20,}/g, replacement: "[redacted-openai-key]" },
  // Google API keys.
  { id: "google-key", regex: /AIza[A-Za-z0-9_-]{16,}/g, replacement: "[redacted-google-key]" },
  // AWS access key id.
  { id: "aws-access", regex: /AKIA[A-Z0-9]{16}/g, replacement: "[redacted-aws-access]" },
  // GitHub PAT.
  { id: "github-pat", regex: /ghp_[A-Za-z0-9]{20,}/g, replacement: "[redacted-github-pat]" },
  // PEM block headers.
  {
    id: "pem-block",
    regex: /-----BEGIN [A-Z ]+ (PRIVATE KEY|CERTIFICATE)-----[\s\S]*?-----END [A-Z ]+ (PRIVATE KEY|CERTIFICATE)-----/g,
    replacement: "[redacted-pem-block]",
  },
  // 12-char Crockford pairing token (per plan.md §5.2).
  {
    id: "pairing-token",
    regex: /\b[ABCDEFGHJKMNPQRSTVWXYZ0-9]{12}\b/g,
    replacement: "[redacted-pairing-token]",
  },
];

/** Run a value through the redactor. Recurses into objects/arrays; passthrough
 *  for booleans/numbers/null. */
export function redactAuditValue(
  value: unknown,
  patterns: readonly AuditRedactionPattern[] = DEFAULT_AUDIT_REDACTION_PATTERNS,
): unknown {
  if (typeof value === "string") {
    let out = value;
    for (const pattern of patterns) {
      out = out.replace(pattern.regex, pattern.replacement);
    }
    return out;
  }
  if (Array.isArray(value)) {
    return value.map((item) => redactAuditValue(item, patterns));
  }
  if (value !== null && typeof value === "object") {
    const out: Record<string, unknown> = {};
    for (const [k, v] of Object.entries(value as Record<string, unknown>)) {
      out[k] = redactAuditValue(v, patterns);
    }
    return out;
  }
  return value;
}

/** Deep-redact an audit entry's `oldValue` and `newValue`. Pure; the result
 *  is a fresh object — the input entry is not mutated. */
export function redactAuditEntry(
  entry: SettingsAuditEntry,
  patterns: readonly AuditRedactionPattern[] = DEFAULT_AUDIT_REDACTION_PATTERNS,
): SettingsAuditEntry {
  return {
    ...entry,
    oldValue: redactAuditValue(entry.oldValue, patterns),
    newValue: redactAuditValue(entry.newValue, patterns),
  };
}

export const auditRedactionInternalsForTesting = {
  patterns: DEFAULT_AUDIT_REDACTION_PATTERNS,
};

export interface AuditChangeOptions {
  /** Override the clock (tests). Default: `() => new Date().toISOString()`. */
  now?: () => string;
}

/** Decide whether a candidate change should write an audit entry. */
export function isSecurityRelevantChange(key: string): boolean {
  return SECURITY_RELEVANT_SETTING_KEYS.has(key);
}

/** Decide-and-emit. Returns the audit entry that was emitted, or null
 *  when the change was routine (no audit). Idempotent: emitting the same
 *  candidate twice writes two entries (audit is append-only by design;
 *  callers must dedupe upstream if needed). */
export async function auditSettingsChange(
  sink: SettingsAuditSink,
  candidate: SettingsChangeCandidate,
  options: AuditChangeOptions = {},
): Promise<SettingsAuditEntry | null> {
  if (!isSecurityRelevantChange(candidate.key)) {
    return null;
  }
  const now = options.now ?? (() => new Date().toISOString());
  const entry: SettingsAuditEntry = {
    entry: "setting_changed",
    key: candidate.key,
    oldValue: candidate.oldValue,
    newValue: candidate.newValue,
    actor: candidate.actor,
    tier: candidate.tier,
    ts: candidate.ts ?? now(),
  };
  await sink(entry);
  return entry;
}

/** Diff two resolved settings trees and emit an audit entry per
 *  security-relevant change. Other changes are silently ignored. Returns
 *  the list of emitted entries.
 *
 *  Used by the SettingsBridge's `recompileAndBroadcast` so every
 *  resolved-tree update gets audited automatically — the renderer never
 *  needs to remember to call this. */
export async function auditResolvedTreeDelta(
  sink: SettingsAuditSink,
  before: Record<string, unknown>,
  after: Record<string, unknown>,
  actor: SettingsActor,
  tier: SettingsChangeCandidate["tier"] = "user",
  options: AuditChangeOptions = {},
): Promise<SettingsAuditEntry[]> {
  const emitted: SettingsAuditEntry[] = [];
  for (const key of SECURITY_RELEVANT_SETTING_KEYS) {
    const oldValue = readDottedKey(before, key);
    const newValue = readDottedKey(after, key);
    if (deepEqual(oldValue, newValue)) continue;
    const candidate: SettingsChangeCandidate = {
      key,
      oldValue,
      newValue,
      actor,
      tier,
    };
    const entry = await auditSettingsChange(sink, candidate, options);
    if (entry) emitted.push(entry);
  }
  return emitted;
}

/** Production batch sink. Appends each entry as a single newline-delimited
 *  JSON line under `<homeDir>/.hoopoe/audit.jsonl` (or whatever path the
 *  composition root chooses). POSIX guarantees `O_APPEND` writes ≤ PIPE_BUF
 *  (4 KiB) are atomic w.r.t. concurrent appenders, so a typical settings
 *  batch (1-10 entries × ~300 bytes) is written all-or-nothing at the file
 *  system level. The parent directory is created on demand.
 *
 *  Throws on disk-write failure; SettingsBridge surfaces the throw as a
 *  `SettingsAuditWriteError` and rolls back the in-flight setting change. */
export function createJsonlBatchAuditSink(input: {
  readonly filePath: string;
}): SyncSettingsAuditBatchSink {
  return (entries) => {
    if (entries.length === 0) return;
    const FS = require("node:fs") as typeof import("node:fs");
    const Path = require("node:path") as typeof import("node:path");
    FS.mkdirSync(Path.dirname(input.filePath), { recursive: true });
    const blob = `${entries.map((entry) => JSON.stringify(entry)).join("\n")}\n`;
    FS.appendFileSync(input.filePath, blob, { encoding: "utf8" });
  };
}

/** Build an in-memory sink — useful for tests + Activity-panel ingestion
 *  where the audit log lives elsewhere on disk. */
export function createInMemoryAuditSink(): {
  sink: SettingsAuditSink;
  drain: () => SettingsAuditEntry[];
  size: () => number;
} {
  const buf: SettingsAuditEntry[] = [];
  return {
    sink: (entry) => {
      buf.push(entry);
    },
    drain: () => buf.splice(0, buf.length),
    size: () => buf.length,
  };
}

export function readDottedKey(obj: Record<string, unknown>, key: string): unknown {
  const parts = key.split(".");
  let cur: unknown = obj;
  for (const p of parts) {
    if (cur === null || cur === undefined || typeof cur !== "object") return undefined;
    cur = (cur as Record<string, unknown>)[p];
  }
  return cur;
}

export function deepEqualForAudit(a: unknown, b: unknown): boolean {
  return deepEqual(a, b);
}

function deepEqual(a: unknown, b: unknown): boolean {
  if (a === b) return true;
  if (a === null || a === undefined || b === null || b === undefined) return false;
  if (typeof a !== typeof b) return false;
  if (typeof a !== "object") return false;
  // Cheap structural compare via JSON; fine for settings values
  // (no functions, no Dates expected at the SettingsBridge boundary).
  try {
    return JSON.stringify(a) === JSON.stringify(b);
  } catch {
    return false;
  }
}
