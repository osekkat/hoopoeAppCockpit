// `@hoopoe/test-evidence` — `go test -json` NDJSON parser (hp-6sv).
//
// `go test -json ./...` emits an NDJSON stream where each line is one of:
//   {"Time":"...", "Action":"run|pause|cont|pass|fail|skip|output", "Package":"...", "Test":"<name>", "Output":"...", "Elapsed":<sec>}
//
// We aggregate by (Package, Test) and emit one TestResult per leaf test.
// Subtests appear as `Test:"Parent/Child"`; both the parent rollup and
// each child come through, but we only emit the leaves to avoid double-
// counting. Output lines are concatenated into the failure message for
// failed tests.

import type { TestResult, TestStatus } from "./envelope.ts";

interface GoTestEvent {
  Time?: string;
  Action: "run" | "pause" | "cont" | "pass" | "fail" | "skip" | "output" | "bench" | string;
  Package?: string;
  Test?: string;
  Output?: string;
  Elapsed?: number;
}

interface AggregatedTest {
  package: string;
  test: string;
  status?: TestStatus;
  elapsedMs: number;
  outputs: string[];
  hasSubtests: boolean;
}

function statusFromAction(action: string): TestStatus | null {
  if (action === "pass") return "passed";
  if (action === "fail") return "failed";
  if (action === "skip") return "skipped";
  return null;
}

export interface ParsedGoTest {
  testCount: number;
  cases: TestResult[];
  /** Build / package-level errors that aren't tied to a specific test. */
  buildErrors: readonly string[];
}

export function parseGoTestNdjson(ndjson: string): ParsedGoTest {
  const lines = ndjson.split("\n");
  const aggregates = new Map<string, AggregatedTest>();
  const parentNames = new Set<string>();
  const buildErrors: string[] = [];

  for (const raw of lines) {
    const line = raw.trim();
    if (line.length === 0) continue;
    let event: GoTestEvent;
    try {
      event = JSON.parse(line) as GoTestEvent;
    } catch {
      // Bare lines (rare; `go test -json` is normally clean NDJSON) are
      // captured into buildErrors so they aren't silently dropped.
      buildErrors.push(`go-test: malformed JSON line: ${line.slice(0, 200)}`);
      continue;
    }
    if (event.Test === undefined || event.Test.length === 0) {
      // Package-scoped event (build status, top-level pass/fail). Only
      // record the negative ones.
      if (event.Action === "fail" && event.Package !== undefined) {
        buildErrors.push(`go-test: package '${event.Package}' failed`);
      }
      if (event.Action === "output" && event.Output !== undefined && event.Package !== undefined) {
        const out = event.Output.trim();
        if (/^FAIL\s/.test(out) || /\[build failed\]/.test(out)) {
          buildErrors.push(`go-test: ${out}`);
        }
      }
      continue;
    }
    const key = `${event.Package ?? ""}::${event.Test}`;
    let agg = aggregates.get(key);
    if (agg === undefined) {
      agg = {
        package: event.Package ?? "",
        test: event.Test,
        elapsedMs: 0,
        outputs: [],
        hasSubtests: false,
      };
      aggregates.set(key, agg);
    }
    // Track parent → has-subtests relationship.
    const slashIdx = event.Test.indexOf("/");
    if (slashIdx > 0) {
      parentNames.add(`${event.Package ?? ""}::${event.Test.slice(0, slashIdx)}`);
    }
    if (event.Action === "output" && event.Output !== undefined) {
      agg.outputs.push(event.Output);
    }
    const status = statusFromAction(event.Action);
    if (status !== null) {
      agg.status = status;
      if (event.Elapsed !== undefined && Number.isFinite(event.Elapsed)) {
        agg.elapsedMs = Math.max(0, Math.round(event.Elapsed * 1000));
      }
    }
  }

  for (const parentKey of parentNames) {
    const parent = aggregates.get(parentKey);
    if (parent !== undefined) parent.hasSubtests = true;
  }

  const cases: TestResult[] = [];
  for (const agg of aggregates.values()) {
    if (agg.hasSubtests) continue; // emit leaves only
    const status: TestStatus = agg.status ?? "skipped";
    const result: TestResult = {
      name: agg.test,
      file: agg.package,
      status,
      durationMs: agg.elapsedMs,
    };
    if (status === "failed") {
      const tail = agg.outputs.slice(-20).join("").trim();
      if (tail.length > 0) {
        result.errorMessage = tail.slice(0, 4_000);
      }
    }
    cases.push(result);
  }
  cases.sort((a, b) => a.name.localeCompare(b.name));
  return { testCount: cases.length, cases, buildErrors };
}
