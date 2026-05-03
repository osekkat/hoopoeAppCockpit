// `@hoopoe/test-evidence` — pre-serialize redaction stats (hp-6sv).
//
// Counts how many secret-shape matches a payload contains. The harness
// uses this on the JSON-serialized envelope just before disk write so
// `redactionStats.patternsMatched` always reflects what was scrubbed.
// We only count — actual redaction belongs in the structured logger
// (which already redacts on emission).
//
// Patterns kept in sync with `@hoopoe/fixture-replay`'s `secret-scan.ts`:
//   - provider env-var names
//   - OpenAI / Anthropic key prefixes
//   - JWT-shaped tokens
//   - Bearer / Authorization headers

import type { RedactionStats } from "./envelope.ts";

const RULES: ReadonlyArray<readonly [string, RegExp]> = [
  ["provider-env-var", /\b(?:OPENAI|ANTHROPIC|GEMINI)_API_KEY\b/g],
  ["openai-key-shape", /\bsk-[A-Za-z0-9_\-]{20,}\b/g],
  ["anthropic-key-shape", /\bsk-ant-[A-Za-z0-9_\-]{20,}\b/g],
  ["jwt-shape", /\beyJ[A-Za-z0-9_-]{10,}\.[A-Za-z0-9_-]{10,}\.[A-Za-z0-9_-]{10,}\b/g],
  ["bearer-header", /\bBearer\s+[A-Za-z0-9._\-]{16,}\b/g],
];

export const ALLOWED_LITERALS: readonly string[] = [
  "MOCKMOCKMOCK",
  "hp-bearer-mock-do-not-trust",
  "hp-ws-mock-do-not-trust",
];

/** Count secret-shape matches in `text`, excluding allow-listed mock
 *  literals. Returns the totals as a `RedactionStats` block. */
export function computeRedactionStats(text: string): RedactionStats {
  const stats: Record<string, number> = {};
  for (const [name, pattern] of RULES) {
    pattern.lastIndex = 0;
    let count = 0;
    let m: RegExpExecArray | null = pattern.exec(text);
    while (m !== null) {
      const matched = m[0];
      if (!ALLOWED_LITERALS.some((literal) => matched.includes(literal))) {
        count += 1;
      }
      m = pattern.exec(text);
    }
    if (count > 0) stats[name] = count;
  }
  return { patternsMatched: stats };
}
