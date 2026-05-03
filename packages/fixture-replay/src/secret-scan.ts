// `@hoopoe/fixture-replay` — secret-shape scanner for synthesized events (hp-q3t).
//
// Hoopoe's audit pipeline must never let a provider API key, a real bearer
// token, or a long-lived shared-secret leak into the Activity panel or the
// audit log. Replay-driven tests assert that boundary by scanning the
// emitted-events stream with this scanner.
//
// The scanner is intentionally narrow — it looks for shapes that are
// *Hoopoe-specific*:
//   - Provider env-var names that are forbidden anywhere in the codebase
//     (Guardrail 11): OPENAI_API_KEY, ANTHROPIC_API_KEY, GEMINI_API_KEY.
//   - Provider key prefixes: `sk-`, `sk-ant-`, `sk-proj-` (OpenAI / Anthropic).
//   - JWT-shaped tokens (`eyJ...` with two `.` separators).
//   - Bearer / authorization headers verbatim.
//
// The mock auth tokens (`MOCKMOCKMOCK`, `hp-bearer-mock-do-not-trust`,
// `hp-ws-mock-do-not-trust`) are explicitly allow-listed since they are
// loud, non-credential placeholders that several Phase 1 e2e flows pass
// through the renderer; flagging them would force every test to redact.

import type { ReplayEvent } from "@hoopoe/fixtures";

export const ALLOWED_SECRET_LITERALS: readonly string[] = [
  "MOCKMOCKMOCK",
  "hp-bearer-mock-do-not-trust",
  "hp-ws-mock-do-not-trust",
];

export interface SecretFinding {
  /** Index into the events array. */
  eventIndex: number;
  /** `channel:type:seq` triple to identify the event. */
  eventRef: string;
  /** Which detector matched. */
  rule: string;
  /** A short snippet of the matched substring (first 32 chars, key bits redacted). */
  evidence: string;
}

export interface SecretScanResult {
  events: number;
  findings: readonly SecretFinding[];
}

interface ScanRule {
  name: string;
  pattern: RegExp;
}

const RULES: readonly ScanRule[] = [
  // Provider env-var names. Catches accidental config-leak via event payloads.
  { name: "provider-env-var", pattern: /\b(?:OPENAI|ANTHROPIC|GEMINI)_API_KEY\b/g },
  // OpenAI / Anthropic API key prefixes. `sk-ant-` and `sk-proj-` covered by
  // the broader `sk-` pattern; we keep the broader one to also catch test/dev
  // keys that don't follow the latest prefix scheme.
  { name: "openai-key-shape", pattern: /\bsk-[A-Za-z0-9_\-]{20,}\b/g },
  // Anthropic key shape (starts with `sk-ant-` plus a long suffix).
  { name: "anthropic-key-shape", pattern: /\bsk-ant-[A-Za-z0-9_\-]{20,}\b/g },
  // JWT-shaped tokens. Three base64url segments separated by `.`. Suffix
  // bound is loose to catch any shape, then we filter allow-listed values
  // before flagging.
  { name: "jwt-shape", pattern: /\beyJ[A-Za-z0-9_-]{10,}\.[A-Za-z0-9_-]{10,}\.[A-Za-z0-9_-]{10,}\b/g },
  // Authorization / Bearer header shapes.
  { name: "bearer-header", pattern: /\bBearer\s+[A-Za-z0-9._\-]{16,}\b/g },
];

function redactEvidence(match: string): string {
  if (match.length <= 8) return match;
  return `${match.slice(0, 4)}***${match.slice(-4)}`;
}

function isAllowed(match: string): boolean {
  for (const allowed of ALLOWED_SECRET_LITERALS) {
    if (match.includes(allowed)) return true;
  }
  return false;
}

export function scanEventsForSecrets(events: readonly ReplayEvent[]): SecretScanResult {
  const findings: SecretFinding[] = [];
  for (let i = 0; i < events.length; i++) {
    const event = events[i];
    if (event === undefined) continue;
    let serialized: string;
    try {
      serialized = JSON.stringify(event);
    } catch {
      // Circular payloads can't be searched; that's the harness's bug, not
      // a leak — skip rather than throw.
      continue;
    }
    const eventRef = `${event.channel}:${event.type}:${event.seq}`;
    for (const rule of RULES) {
      // RegExp with /g must be reset per-event since exec advances lastIndex.
      rule.pattern.lastIndex = 0;
      let m: RegExpExecArray | null = rule.pattern.exec(serialized);
      while (m !== null) {
        const matched = m[0];
        if (!isAllowed(matched)) {
          findings.push({
            eventIndex: i,
            eventRef,
            rule: rule.name,
            evidence: redactEvidence(matched.slice(0, 32)),
          });
        }
        m = rule.pattern.exec(serialized);
      }
    }
  }
  return { events: events.length, findings };
}
