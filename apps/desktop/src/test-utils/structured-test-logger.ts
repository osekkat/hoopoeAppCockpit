import { randomUUID } from "node:crypto";

export type Phase1TestPhase = "setup" | "act" | "assert" | "teardown";
export type Phase1TestResult = "passed" | "failed" | "skipped";

export type JsonPrimitive = string | number | boolean | null;
export type JsonValue = JsonPrimitive | JsonValue[] | { readonly [key: string]: JsonValue };

export interface StructuredTestLogEntry {
  readonly ts: string;
  readonly correlationId: string;
  readonly component: "test.phase1";
  readonly suite: string;
  readonly test: string;
  readonly phase?: Phase1TestPhase;
  readonly event: string;
  readonly data?: JsonValue;
}

export interface Phase1StructuredTestLogger {
  readonly correlationId: string;
  readonly start: (data?: unknown) => StructuredTestLogEntry;
  readonly phase: (phase: Phase1TestPhase, data?: unknown) => StructuredTestLogEntry;
  readonly snapshot: (label: string, value: unknown) => StructuredTestLogEntry;
  readonly assertion: (name: string, data?: unknown) => StructuredTestLogEntry;
  readonly end: (result: Phase1TestResult, data?: unknown) => StructuredTestLogEntry;
  readonly entries: () => readonly StructuredTestLogEntry[];
  readonly lastEntries: (count?: number) => readonly StructuredTestLogEntry[];
  readonly jsonLines: () => string;
}

export interface CreatePhase1TestLoggerInput {
  readonly suite: string;
  readonly testName: string;
  readonly correlationId?: string;
  readonly now?: () => Date;
}

export function createPhase1TestLogger(
  input: CreatePhase1TestLoggerInput,
): Phase1StructuredTestLogger {
  const correlationId = input.correlationId ?? randomUUID();
  const now = input.now ?? (() => new Date());
  const entries: StructuredTestLogEntry[] = [];
  let currentPhase: Phase1TestPhase | undefined;

  const write = (event: string, data?: unknown): StructuredTestLogEntry => {
    const base = {
      ts: now().toISOString(),
      correlationId,
      component: "test.phase1" as const,
      suite: input.suite,
      test: input.testName,
      event,
    };
    const entry: StructuredTestLogEntry = {
      ...base,
      ...(currentPhase !== undefined ? { phase: currentPhase } : {}),
      ...(data !== undefined ? { data: sanitizeForJson(data) } : {}),
    };
    entries.push(entry);
    return entry;
  };

  return {
    correlationId,
    start: (data) => write("test.start", data),
    phase: (phase, data) => {
      currentPhase = phase;
      return write("test.phase", data);
    },
    snapshot: (label, value) => write("test.snapshot", { label, value }),
    assertion: (name, data) => write("test.assertion", { name, ...(dataObject(data) ?? {}) }),
    end: (result, data) => write("test.end", { result, ...(dataObject(data) ?? {}) }),
    entries: () => entries.slice(),
    lastEntries: (count = 200) => entries.slice(Math.max(0, entries.length - count)),
    jsonLines: () => entries.map((entry) => JSON.stringify(entry)).join("\n"),
  };
}

export function assertNoProductionEndpoints(input: {
  readonly urls: Iterable<string>;
  readonly allowedHosts?: readonly string[];
}): void {
  const allowedHosts = new Set([
    "127.0.0.1",
    "::1",
    "localhost",
    "mock-flywheel",
    ...(input.allowedHosts ?? []),
  ]);
  const blocked: string[] = [];

  for (const raw of input.urls) {
    let parsed: URL;
    try {
      parsed = new URL(raw);
    } catch {
      blocked.push(raw);
      continue;
    }

    if (parsed.protocol === "fixture:" || parsed.protocol === "file:") {
      continue;
    }

    if (
      (parsed.protocol === "http:" || parsed.protocol === "https:") &&
      !allowedHosts.has(parsed.hostname)
    ) {
      blocked.push(raw);
    }
  }

  if (blocked.length > 0) {
    throw new Error(`Phase 1 tests must not contact production endpoints: ${blocked.join(", ")}`);
  }
}

export function sanitizeForJson(value: unknown, depth = 0): JsonValue {
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
    return value.map((item) => sanitizeForJson(item, depth + 1));
  }
  if (typeof value === "object") {
    const out: Record<string, JsonValue> = {};
    for (const [key, child] of Object.entries(value as Record<string, unknown>)) {
      if (child === undefined || typeof child === "function" || typeof child === "symbol") {
        continue;
      }
      out[key] = sanitizeForJson(child, depth + 1);
    }
    return out;
  }
  return String(value);
}

function dataObject(value: unknown): Record<string, unknown> | null {
  return value !== null && typeof value === "object" && !Array.isArray(value)
    ? (value as Record<string, unknown>)
    : value === undefined
      ? null
      : { value };
}
