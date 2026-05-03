// Hoopoe-owned. Renderer + main side redaction primitive. Mirrors the
// daemon-side `apps/daemon/internal/redaction/`. Same pattern IDs, same
// replacement strategies, same TraceEvent shape — drift between them
// fails the CI test in `scripts/redactlint/`.
//
// hp-je1p. The Go redaction package was landed first by another agent
// at commit 3b8c174; this TS mirror was the missing half.

export type Surface = "audit" | "events" | "logger" | string;

export const SurfaceAudit: Surface = "audit";
export const SurfaceEvents: Surface = "events";
export const SurfaceLogger: Surface = "logger";

/** Mirrors `apps/daemon/internal/redaction.TraceEvent`. */
export interface TraceEvent {
  readonly ts: string;
  readonly redactor: string;        // surface or 'adapter:<name>'
  readonly patternId: string;
  readonly context: string;
  readonly bytesRedacted: number;
  readonly count: number;
}

export interface PatternStat {
  readonly patternId: string;
  readonly count: number;
  readonly bytesRedacted: number;
}

export interface StatsSnapshot {
  readonly schemaVersion: 1;
  readonly patterns: PatternStat[];
}

interface RedactionPattern {
  readonly id: string;
  readonly regex: RegExp;
  readonly replace: (match: string) => string;
}

function hashTag(input: string): string {
  let h1 = 2166136261;
  let h2 = 1013904223;
  for (let i = 0; i < input.length; i++) {
    const c = input.charCodeAt(i);
    h1 = Math.imul(h1 ^ c, 16777619);
    h2 = Math.imul(h2 ^ ((c * 31) & 0xffffffff), 16777619);
  }
  const hex = (n: number) => (n >>> 0).toString(16).padStart(8, "0").slice(-4);
  return `sha256:${hex(h1)}${hex(h2)}`;
}

function providerKeyReplace(prefix: string): (match: string) => string {
  return (match: string) => `${prefix}[redacted-${hashTag(match)}]`;
}

function bearerReplace(s: string): string {
  return `Bearer [redacted-${hashTag(s)}]`;
}

function emailLast4(match: string): string {
  const at = match.lastIndexOf("@");
  if (at <= 0) return match;
  const local = match.slice(0, at);
  const domain = match.slice(at);
  const keep = Math.min(4, local.length);
  return `***${local.slice(local.length - keep)}${domain}`;
}

// Pattern order MUST match `apps/daemon/internal/redaction/patterns.go` —
// the drift detector enforces this. Specific patterns run before broad
// ones (e.g., `provider-key-anthropic` before `provider-key-sk`).
function defaultPatterns(): RedactionPattern[] {
  return [
    {
      id: "http-header-authorization",
      regex: /\bAuthorization\s*:\s*[^\r\n]+/gim,
      replace: () => "Authorization: [redacted-header]",
    },
    {
      id: "http-header-cookie",
      regex: /\bCookie\s*:\s*[^\r\n]+/gim,
      replace: () => "Cookie: [redacted-header]",
    },
    {
      id: "http-header-set-cookie",
      regex: /\bSet-Cookie\s*:\s*[^\r\n]+/gim,
      replace: () => "Set-Cookie: [redacted-header]",
    },
    {
      id: "private-key-block",
      regex: /-----BEGIN (?:RSA |OPENSSH |EC |PGP )?PRIVATE KEY(?: BLOCK)?-----[\s\S]*?-----END (?:RSA |OPENSSH |EC |PGP )?PRIVATE KEY(?: BLOCK)?-----/g,
      replace: () => "[private-key-redacted]",
    },
    {
      id: "bearer-hmac",
      regex: /\beyJ[A-Za-z0-9_\-]{10,}\.[A-Za-z0-9_\-]{10,}\.[A-Za-z0-9_\-]{10,}\b/g,
      replace: hashTag,
    },
    {
      id: "bearer-token",
      regex: /\bBearer\s+[A-Za-z0-9._~+/\-]{16,}={0,2}\b/gi,
      replace: bearerReplace,
    },
    {
      id: "provider-key-anthropic",
      regex: /\bsk-ant-[A-Za-z0-9_\-]{16,}\b/g,
      replace: providerKeyReplace("sk-ant-"),
    },
    {
      id: "provider-key-sk",
      regex: /\bsk-[A-Za-z0-9_\-]{20,}\b/g,
      replace: providerKeyReplace("sk-"),
    },
    {
      id: "provider-key-google",
      regex: /\bAIza[A-Za-z0-9_\-]{35}\b/g,
      replace: providerKeyReplace("AIza-"),
    },
    {
      id: "provider-key-aws",
      regex: /\bAKIA[A-Z0-9]{16}\b/g,
      replace: providerKeyReplace("AKIA-"),
    },
    {
      id: "provider-key-github",
      regex: /\bghp_[A-Za-z0-9]{36,}\b/g,
      replace: providerKeyReplace("ghp-"),
    },
    {
      id: "provider-key-slack",
      regex: /\bxox[baprs]-[A-Za-z0-9\-]{10,}\b/g,
      replace: providerKeyReplace("xox-"),
    },
    {
      id: "pairing-token",
      regex: /\bH-[ABCDEFGHJKMNPQRSTVWXYZ0-9]{11}\b/g,
      replace: () => "[pairing-token-redacted]",
    },
    {
      id: "ssh-passphrase",
      regex: /\bpassphrase\s*[:=]\s*\S+/gi,
      replace: () => "passphrase=[redacted]",
    },
    {
      id: "browser-cookie-chatgpt",
      regex: /\b__Secure-next-auth\.session-token\s*=\s*[A-Za-z0-9._\-]+/gi,
      replace: () => "__Secure-next-auth.session-token=[redacted]",
    },
    {
      id: "browser-cookie-claude",
      regex: /\b(?:claude|anthropic)[\w.\-]*session[\w.\-]*\s*=\s*[A-Za-z0-9._\-]+/gi,
      replace: () => "claude-session=[redacted]",
    },
    {
      id: "browser-cookie-oai",
      regex: /\boai[\w.\-]*session[\w.\-]*\s*=\s*[A-Za-z0-9._\-]+/gi,
      replace: () => "oai-session=[redacted]",
    },
    {
      id: "telegram-bot-token",
      regex: /\b\d{8,10}:[A-Za-z0-9_\-]{30,}\b/g,
      replace: () => "[telegram-bot-token-redacted]",
    },
    {
      id: "email-address",
      regex: /\b[A-Za-z0-9._%+\-]+@[A-Za-z0-9.\-]+\.[A-Za-z]{2,}\b/g,
      replace: emailLast4,
    },
    {
      id: "ssh-key-path",
      regex: /(?:^|\s|"|')~?\/?(?:[\w/\-]*?)?\.ssh\/[\w./\-]+/g,
      replace: () => "[ssh-key-path-redacted]",
    },
    {
      id: "shadow-file-path",
      regex: /\/etc\/shadow\b/g,
      replace: () => "[shadow-path-redacted]",
    },
    {
      id: "macos-private-db-path",
      regex: /\/private\/var\/db\/[\w./\-]+/g,
      replace: () => "[macos-private-db-path-redacted]",
    },
    {
      id: "oracle-profile-path",
      regex: /(?:~|\/Users\/[^/\s]+|\/home\/[^/\s]+)\/(?:\.oracle|\.config\/oracle|Library\/Application Support\/oracle)[^\s"']*/g,
      replace: () => "[oracle-profile-path-redacted]",
    },
    {
      id: "user-home-path",
      regex: /(?:~|\/Users\/[^/\s"']+|\/home\/[^/\s"']+)\/(?:[^\s"']+)/g,
      replace: () => "[user-path-redacted]",
    },
    {
      id: "vps-project-path",
      regex: /\/data\/projects\/[^\s"']+/g,
      replace: () => "[project-path-redacted]",
    },
  ];
}

export type RedactValue =
  | string
  | number
  | boolean
  | null
  | RedactValue[]
  | { readonly [key: string]: RedactValue };

export class Redactor {
  private readonly patterns = defaultPatterns();
  private readonly stats: Map<string, { count: number; bytes: number }> = new Map();
  private readonly clock: () => Date;

  constructor(now?: () => Date) {
    this.clock = now ?? (() => new Date());
  }

  /** RedactText scrubs `text` for the given surface + context, recording
   *  stats and returning trace events. Mirrors
   *  `redaction.RedactText(surface, context, text)`. */
  redactText(
    surface: Surface,
    context: string,
    text: string,
  ): { redacted: string; events: TraceEvent[] } {
    if (!text) return { redacted: text, events: [] };
    let out = text;
    const events: TraceEvent[] = [];
    for (const p of this.patterns) {
      p.regex.lastIndex = 0;
      const matches = out.match(p.regex);
      if (!matches || matches.length === 0) continue;
      const bytes = matches.reduce((sum, m) => sum + m.length, 0);
      out = out.replace(p.regex, (m) => p.replace(m));
      const stat = this.stats.get(p.id) ?? { count: 0, bytes: 0 };
      stat.count += matches.length;
      stat.bytes += bytes;
      this.stats.set(p.id, stat);
      events.push({
        ts: this.clock().toISOString(),
        redactor: surface,
        patternId: p.id,
        context,
        bytesRedacted: bytes,
        count: matches.length,
      });
    }
    return { redacted: out, events };
  }

  /** RedactValue walks `value` recursively, redacting strings inside maps
   *  and arrays. Mirrors the Go `RedactValue(surface, context, value)`. */
  redactValue(
    surface: Surface,
    context: string,
    value: RedactValue | undefined,
  ): { redacted: RedactValue | undefined; events: TraceEvent[] } {
    if (value === undefined || value === null) return { redacted: value, events: [] };
    if (typeof value === "string") return this.redactText(surface, context, value);
    if (Array.isArray(value)) {
      const out: RedactValue[] = [];
      const allEvents: TraceEvent[] = [];
      for (let i = 0; i < value.length; i++) {
        const child = value[i];
        const { redacted, events } = this.redactValue(
          surface,
          `${context}[${i}]`,
          child,
        );
        out.push(redacted as RedactValue);
        if (events.length) allEvents.push(...events);
      }
      return { redacted: out, events: allEvents };
    }
    if (typeof value === "object") {
      const out: Record<string, RedactValue> = {};
      const allEvents: TraceEvent[] = [];
      for (const [k, child] of Object.entries(value)) {
        const childContext = context ? `${context}.${k}` : k;
        const { redacted, events } = this.redactValue(
          surface,
          childContext,
          child as RedactValue,
        );
        out[k] = redacted as RedactValue;
        if (events.length) allEvents.push(...events);
      }
      return { redacted: out, events: allEvents };
    }
    return { redacted: value, events: [] };
  }

  /** Returns the list of pattern IDs in firing order. The drift detector
   *  in `scripts/redactlint/` compares this against
   *  `apps/daemon/internal/redaction/patterns.go`. */
  patternIds(): readonly string[] {
    return this.patterns.map((p) => p.id);
  }

  /** Snapshot of current stats. Diagnostics renders this. */
  snapshotStats(): StatsSnapshot {
    const patterns: PatternStat[] = [];
    for (const [id, s] of this.stats.entries()) {
      patterns.push({ patternId: id, count: s.count, bytesRedacted: s.bytes });
    }
    patterns.sort((a, b) => a.patternId.localeCompare(b.patternId));
    return { schemaVersion: 1, patterns };
  }

  /** Reset stats — used by tests to isolate runs. */
  resetStats(): void {
    this.stats.clear();
  }
}
