import { randomUUID } from "node:crypto";

export type StructuredTestStatus = "started" | "passed" | "failed" | "skipped";

export type StructuredJsonPrimitive = string | number | boolean | null;
export type StructuredJsonValue =
  | StructuredJsonPrimitive
  | StructuredJsonValue[]
  | { readonly [key: string]: StructuredJsonValue };

export interface StructuredTestLogLine {
  readonly ts: string;
  readonly testId: string;
  readonly suite: string;
  readonly step: string;
  readonly durationMs: number;
  readonly status: StructuredTestStatus;
  readonly errorMessage?: string;
  readonly data?: StructuredJsonValue;
}

export interface StructuredLoggerInput {
  readonly suite: string;
  readonly testId: string;
  readonly correlationId?: string;
  readonly now?: () => Date;
  readonly emit?: (line: StructuredTestLogLine) => void;
}

export interface StructuredTestLogger {
  readonly correlationId: string;
  readonly record: (
    step: string,
    status: StructuredTestStatus,
    input?: { readonly durationMs?: number; readonly data?: unknown; readonly error?: unknown },
  ) => StructuredTestLogLine;
  readonly step: <T>(
    step: string,
    action: () => Promise<T>,
    data?: unknown,
  ) => Promise<T>;
  readonly stepSync: <T>(
    step: string,
    action: () => T,
    data?: unknown,
  ) => T;
  readonly entries: () => readonly StructuredTestLogLine[];
  readonly jsonLines: () => string;
}

export function createStructuredLogger(input: StructuredLoggerInput): StructuredTestLogger {
  const correlationId = input.correlationId ?? randomUUID();
  const now = input.now ?? (() => new Date());
  const emit =
    input.emit ??
    ((line: StructuredTestLogLine) => {
      process.stderr.write(`${JSON.stringify(line)}\n`);
    });
  const entries: StructuredTestLogLine[] = [];

  const record: StructuredTestLogger["record"] = (step, status, eventInput = {}) => {
    const line: StructuredTestLogLine = {
      ts: now().toISOString(),
      testId: input.testId,
      suite: input.suite,
      step,
      durationMs: Math.max(0, Math.round(eventInput.durationMs ?? 0)),
      status,
      ...(eventInput.error !== undefined
        ? { errorMessage: errorMessage(eventInput.error) }
        : {}),
      ...(eventInput.data !== undefined ? { data: sanitizeForStructuredLog(eventInput.data) } : {}),
    };
    entries.push(line);
    emit(line);
    return line;
  };

  return {
    correlationId,
    record,
    step: async (step, action, data) => {
      record(step, "started", { data });
      const started = performance.now();
      try {
        const result = await action();
        record(step, "passed", { durationMs: performance.now() - started });
        return result;
      } catch (error) {
        record(step, "failed", { durationMs: performance.now() - started, error });
        throw error;
      }
    },
    stepSync: (step, action, data) => {
      record(step, "started", { data });
      const started = performance.now();
      try {
        const result = action();
        record(step, "passed", { durationMs: performance.now() - started });
        return result;
      } catch (error) {
        record(step, "failed", { durationMs: performance.now() - started, error });
        throw error;
      }
    },
    entries: () => entries.slice(),
    jsonLines: () => entries.map((entry) => JSON.stringify(entry)).join("\n"),
  };
}

export function sanitizeForStructuredLog(value: unknown, depth = 0): StructuredJsonValue {
  if (depth > 6) return "[MaxDepth]";
  if (value === null) return null;
  if (typeof value === "string" || typeof value === "boolean") return value;
  if (typeof value === "number") return Number.isFinite(value) ? value : String(value);
  if (typeof value === "bigint") return value.toString();
  if (value instanceof Date) return value.toISOString();
  if (value instanceof Uint8Array) {
    return { type: "Uint8Array", byteLength: value.byteLength };
  }
  if (Array.isArray(value)) {
    return value.map((item) => sanitizeForStructuredLog(item, depth + 1));
  }
  if (typeof value === "object") {
    const out: Record<string, StructuredJsonValue> = {};
    for (const [key, child] of Object.entries(value as Record<string, unknown>)) {
      if (child === undefined || typeof child === "function" || typeof child === "symbol") {
        continue;
      }
      out[key] = sanitizeForStructuredLog(child, depth + 1);
    }
    return out;
  }
  return String(value);
}

function errorMessage(error: unknown): string {
  return error instanceof Error ? error.message : String(error);
}
