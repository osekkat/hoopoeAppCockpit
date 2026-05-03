// Hoopoe-owned. Production logger for desktop renderer + main. Mirrors the
// Go daemon logger at `apps/daemon/internal/logger/logger.go`.
//
// The renderer's `console.warn` in vendored/t3code/syncShellEnvironment.ts
// is the ONLY tolerated raw-console call in production code (excluded by
// the lint at `scripts/lint-no-raw-logging.ts`). Every other code path
// must use `createLogger(...)` and `child.with({...})`.

import { Redactor, type RedactionEvent, redactLogEntry } from "./redactor.ts";
import {
  ENVELOPE_FIELDS,
  type Actor,
  type EnvelopeField,
  type Level,
  type LogEntry,
  type LogValue,
  levelRank,
} from "./types.ts";

/** Sink for emitted entries. Each transport receives already-redacted
 *  entries; transports MUST NOT do their own redaction. */
export interface Transport {
  emit(entry: LogEntry): void;
}

export interface LoggerOptions {
  readonly component: string;
  readonly minLevel?: Level;
  readonly now?: () => Date;
  readonly redactor?: Redactor;
  readonly transports: readonly Transport[];
}

/** Scoped logger. Use `with(...)` to derive a child carrying additional
 *  envelope fields without mutating the parent. */
export interface Logger {
  level(): Level;
  with(scope: ScopeFields): Logger;
  trace(msg: string, fields?: Record<string, LogValue>): LogEntry;
  debug(msg: string, fields?: Record<string, LogValue>): LogEntry;
  info(msg: string, fields?: Record<string, LogValue>): LogEntry;
  warn(msg: string, fields?: Record<string, LogValue>): LogEntry;
  error(msg: string, fields?: Record<string, LogValue>): LogEntry;
  fatal(msg: string, fields?: Record<string, LogValue>): LogEntry;
}

/** Fields that can be attached via `.with({...})`. Envelope-named keys
 *  populate dedicated columns; everything else flows into `entry.fields`. */
export interface ScopeFields {
  readonly component?: string;
  readonly subsystem?: string;
  readonly correlationId?: string;
  readonly causationId?: string;
  readonly actor?: Actor;
  readonly jobId?: string;
  readonly beadId?: string;
  readonly swarmId?: string;
  readonly planId?: string;
  readonly runId?: string;
  readonly [extra: string]: LogValue | Actor | undefined;
}

interface ResolvedScope {
  component: string;
  subsystem?: string;
  correlationId?: string;
  causationId?: string;
  actor?: Actor;
  jobId?: string;
  beadId?: string;
  swarmId?: string;
  planId?: string;
  runId?: string;
  fields: Record<string, LogValue>;
}

const ENVELOPE_SET: ReadonlySet<string> = new Set(ENVELOPE_FIELDS);

function isEnvelopeField(key: string): key is EnvelopeField {
  return ENVELOPE_SET.has(key);
}

export function createLogger(options: LoggerOptions): Logger {
  const minLevel: Level = options.minLevel ?? "info";
  const now = options.now ?? (() => new Date());
  const redactor = options.redactor ?? new Redactor();
  const transports = options.transports;
  if (transports.length === 0) {
    throw new Error("createLogger: at least one transport is required");
  }

  return buildLogger(
    {
      component: options.component,
      fields: {},
    },
    minLevel,
    now,
    redactor,
    transports,
  );
}

function buildLogger(
  scope: ResolvedScope,
  minLevel: Level,
  now: () => Date,
  redactor: Redactor,
  transports: readonly Transport[],
): Logger {
  function emitAt(level: Level, msg: string, fields?: Record<string, LogValue>): LogEntry {
    if (levelRank(level) < levelRank(minLevel)) {
      return {
        ts: now().toISOString(),
        level,
        msg,
        component: scope.component,
      };
    }
    const callFields = fields ?? {};
    const merged: Record<string, LogValue> = { ...scope.fields };
    for (const [k, v] of Object.entries(callFields)) {
      if (isEnvelopeField(k)) continue; // skip; route via .with()
      if (v === undefined) continue;
      merged[k] = v as LogValue;
    }
    const entry: LogEntry = {
      ts: now().toISOString(),
      level,
      msg,
      component: scope.component,
      ...(scope.subsystem ? { subsystem: scope.subsystem } : {}),
      ...(scope.correlationId ? { correlationId: scope.correlationId } : {}),
      ...(scope.causationId ? { causationId: scope.causationId } : {}),
      ...(scope.actor ? { actor: scope.actor } : {}),
      ...(scope.jobId ? { jobId: scope.jobId } : {}),
      ...(scope.beadId ? { beadId: scope.beadId } : {}),
      ...(scope.swarmId ? { swarmId: scope.swarmId } : {}),
      ...(scope.planId ? { planId: scope.planId } : {}),
      ...(scope.runId ? { runId: scope.runId } : {}),
      ...(Object.keys(merged).length > 0 ? { fields: merged } : {}),
    };
    const { entry: redacted } = redactLogEntry(redactor, entry);
    for (const t of transports) {
      t.emit(redacted);
    }
    return redacted;
  }

  return {
    level: () => minLevel,
    with: (delta: ScopeFields): Logger => {
      const next: ResolvedScope = {
        ...scope,
        fields: { ...scope.fields },
      };
      for (const [k, v] of Object.entries(delta)) {
        if (v === undefined) continue;
        if (k === "component" && typeof v === "string") {
          next.component = v;
        } else if (k === "subsystem" && typeof v === "string") {
          next.subsystem = v;
        } else if (k === "correlationId" && typeof v === "string") {
          next.correlationId = v;
        } else if (k === "causationId" && typeof v === "string") {
          next.causationId = v;
        } else if (k === "actor" && typeof v === "object") {
          next.actor = v as Actor;
        } else if (k === "jobId" && typeof v === "string") {
          next.jobId = v;
        } else if (k === "beadId" && typeof v === "string") {
          next.beadId = v;
        } else if (k === "swarmId" && typeof v === "string") {
          next.swarmId = v;
        } else if (k === "planId" && typeof v === "string") {
          next.planId = v;
        } else if (k === "runId" && typeof v === "string") {
          next.runId = v;
        } else if (!isEnvelopeField(k)) {
          next.fields[k] = v as LogValue;
        }
      }
      return buildLogger(next, minLevel, now, redactor, transports);
    },
    trace: (msg, f) => emitAt("trace", msg, f),
    debug: (msg, f) => emitAt("debug", msg, f),
    info: (msg, f) => emitAt("info", msg, f),
    warn: (msg, f) => emitAt("warn", msg, f),
    error: (msg, f) => emitAt("error", msg, f),
    fatal: (msg, f) => emitAt("fatal", msg, f),
  };
}

/** A no-op logger that satisfies the Logger interface but emits nothing.
 *  Returned by tests/utilities that need a logger handle without wiring
 *  transports. Production code should NEVER reach this. */
export const NOOP_LOGGER: Logger = createLogger({
  component: "test.noop",
  minLevel: "fatal",
  transports: [{ emit() {} }],
});

// Re-export for test convenience.
export { Redactor };
export type { RedactionEvent };
