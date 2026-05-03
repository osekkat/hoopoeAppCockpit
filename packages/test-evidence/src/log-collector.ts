// `@hoopoe/test-evidence` — collect structured-logger lines (hp-6sv).
//
// The desktop test harness emits one JSON line per assertion / phase via
// `apps/desktop/src/test-utils/structured-logger.ts`. We tail those lines
// from a runner's stdout/stderr stream and bucket them per-test (using
// the `suite` + `testId` fields shared with the structured logger).
//
// The collector is a pure function over the raw line text — no I/O. The
// caller hands us the captured stream content and we return per-test
// log slices.

import type { StructuredLogSlice } from "./envelope.ts";

interface RawStructuredLine {
  ts?: string;
  testId?: string;
  suite?: string;
  step?: string;
  status?: string;
  durationMs?: number;
  errorMessage?: string;
  data?: unknown;
}

export interface CollectedSlices {
  /** Per-test slices keyed by the structured logger's `testId` (a
   *  random UUID per `createStructuredLogger` call). */
  byTestId: Readonly<Record<string, readonly StructuredLogSlice[]>>;
  /** Per-(suite, test) slices keyed by `${suite}::${testName}` — used
   *  when a JUnit testcase doesn't carry the same correlationId but
   *  matches by name. The key is always lower-case. */
  bySuiteAndName: Readonly<Record<string, readonly StructuredLogSlice[]>>;
  /** Total lines parsed (including those that didn't match a known shape). */
  totalLines: number;
  /** Lines that didn't parse as JSON; counted for diagnostics. */
  nonJsonLines: number;
}

export interface CollectOptions {
  /** Cap per-test slice length. Default: 200 (per the bead spec). */
  maxPerTest?: number;
}

const DEFAULT_MAX_PER_TEST = 200;

function isStructuredLogLine(value: unknown): value is RawStructuredLine {
  if (typeof value !== "object" || value === null) return false;
  const v = value as Record<string, unknown>;
  return (
    typeof v.ts === "string" &&
    typeof v.suite === "string" &&
    typeof v.step === "string" &&
    typeof v.status === "string"
  );
}

function toSlice(line: RawStructuredLine): StructuredLogSlice {
  const slice: StructuredLogSlice = {
    ts: line.ts ?? "",
    step: line.step ?? "",
    status: line.status ?? "",
    durationMs: typeof line.durationMs === "number" ? line.durationMs : 0,
  };
  if (typeof line.errorMessage === "string") {
    slice.errorMessage = line.errorMessage;
  }
  if (line.data !== undefined) {
    slice.data = line.data;
  }
  return slice;
}

export function collectStructuredLogLines(
  text: string,
  options: CollectOptions = {},
): CollectedSlices {
  const max = options.maxPerTest ?? DEFAULT_MAX_PER_TEST;
  const byTestId: Record<string, StructuredLogSlice[]> = {};
  const bySuiteAndName: Record<string, StructuredLogSlice[]> = {};
  let totalLines = 0;
  let nonJsonLines = 0;

  for (const raw of text.split("\n")) {
    const line = raw.trim();
    if (line.length === 0) continue;
    totalLines += 1;
    if (line[0] !== "{" && line[0] !== "[") continue;
    let parsed: unknown;
    try {
      parsed = JSON.parse(line);
    } catch {
      nonJsonLines += 1;
      continue;
    }
    if (!isStructuredLogLine(parsed)) continue;
    const slice = toSlice(parsed);
    const testId = parsed.testId;
    if (typeof testId === "string" && testId.length > 0) {
      const arr = byTestId[testId] ?? [];
      arr.push(slice);
      if (arr.length > max) arr.splice(0, arr.length - max);
      byTestId[testId] = arr;
    }
    const suite = parsed.suite;
    if (typeof suite === "string" && typeof parsed.step === "string") {
      const key = `${suite.toLowerCase()}::${slice.step.toLowerCase()}`;
      const arr = bySuiteAndName[key] ?? [];
      arr.push(slice);
      if (arr.length > max) arr.splice(0, arr.length - max);
      bySuiteAndName[key] = arr;
    }
  }
  return { byTestId, bySuiteAndName, totalLines, nonJsonLines };
}

/** Pick the best per-test slice given a JUnit case. Looks up by suite+name
 *  first, falls back to scanning byTestId for matches whose `data` mentions
 *  the test name. Returns `undefined` if no slice matches. */
export function sliceForCase(
  collected: CollectedSlices,
  testName: string,
  classname: string | undefined,
): readonly StructuredLogSlice[] | undefined {
  if (classname !== undefined) {
    const key = `${classname.toLowerCase()}::${testName.toLowerCase()}`;
    const exact = collected.bySuiteAndName[key];
    if (exact !== undefined && exact.length > 0) return exact;
  }
  // Loose match — any suite, same step name.
  for (const key of Object.keys(collected.bySuiteAndName)) {
    if (key.endsWith(`::${testName.toLowerCase()}`)) {
      return collected.bySuiteAndName[key];
    }
  }
  return undefined;
}
