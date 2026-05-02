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

function readDottedKey(obj: Record<string, unknown>, key: string): unknown {
  const parts = key.split(".");
  let cur: unknown = obj;
  for (const p of parts) {
    if (cur == null || typeof cur !== "object") return undefined;
    cur = (cur as Record<string, unknown>)[p];
  }
  return cur;
}

function deepEqual(a: unknown, b: unknown): boolean {
  if (a === b) return true;
  if (a == null || b == null) return false;
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
