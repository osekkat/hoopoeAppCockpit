// hp-je1p deliverable. Asserts the Go redaction package
// (`apps/daemon/internal/redaction/patterns.go`) and the TS redact
// package (`apps/desktop/src/shared/redact/`) expose the SAME pattern IDs
// in the SAME order. The bead spec requires the two libraries to stay in
// sync — adding a pattern in one place without the other breaks
// redaction guarantees on whichever surface lags.
//
// This script is consumed two ways:
//   1. Unit test (`check-redact-drift.test.ts`) — executes via `bun test`
//      so a missing-pattern divergence fails CI before any other checks.
//   2. Standalone executable — `bun scripts/redactlint/check-redact-drift.ts`
//      prints a human-readable diff if drift is detected. Wired into
//      `bun run lint`.
//
// How it works:
//   - Imports the TS Redactor directly + reads its patternIds().
//   - Parses the Go patterns.go file for the `id: "..."` token in each
//     pattern struct literal (regex-based; brittle to formatting changes
//     but cheaper than spawning a Go binary just for this).
//
// Adding a new pattern: edit BOTH patterns.go + redact.ts in the same
// commit. The order MUST match between the two files (specific patterns
// before broad ones).

import { readFileSync } from "node:fs";
import path from "node:path";
import { Redactor } from "../../apps/desktop/src/shared/redact/redact.ts";

const REPO_ROOT = process.cwd();
const GO_REDACT_FILE = path.join(REPO_ROOT, "apps/daemon/internal/redaction/patterns.go");
const GO_FUZZ_TEST_FILE = path.join(
  REPO_ROOT,
  "apps/daemon/internal/redaction/redaction_fuzz_test.go",
);
const TS_TEST_FILE = path.join(REPO_ROOT, "apps/desktop/src/shared/redact/redact.test.ts");

/** Parse the pattern IDs from redact.go. The patterns are declared in
 *  `defaultPatterns()` as `{id: "...", regex: ..., replace: ...}` — we
 *  match the literal `id:\s*"..."` tokens within `defaultPatterns`. */
export function parseGoPatternIds(source: string): string[] {
  // Find the `defaultPatterns()` body. Patterns end at the matching `}`
  // for the function. A regex over the whole file is fine because the `id`
  // field name is unique to the redactionPattern struct.
  const ids: string[] = [];
  const re = /\bid:\s*"([^"]+)"/g;
  let match;
  while ((match = re.exec(source)) !== null) {
    ids.push(match[1]);
  }
  return ids;
}

/** Parse pattern IDs that have a fuzz fixture in redaction_fuzz_test.go.
 *  The fixture table uses the same `id: "..."` shape as defaultPatterns
 *  (a `patternFixture` struct literal with an `id` field), so the same
 *  regex applies. Every canonical pattern must have a fuzz fixture so the
 *  regex actually exercises secret inputs — pattern parity without test
 *  parity hides regressions. */
export function parseGoFuzzFixtureIds(source: string): string[] {
  const ids: string[] = [];
  const re = /\bid:\s*"([^"]+)"/g;
  let match;
  while ((match = re.exec(source)) !== null) {
    ids.push(match[1]);
  }
  return ids;
}

/** Return the subset of `canonical` pattern IDs that appear as quoted
 *  string literals (single, double, or backtick) anywhere in the TS test
 *  source. The TS test file mixes individual `test(...)` blocks with
 *  table-driven `test.each(...)` fixtures and a snapshot of `patternIds()`
 *  — any of these counts as evidence the pattern is acknowledged in the
 *  test suite. A pattern that lands in `redact.ts` but never appears in
 *  the test file is the drift signal we want to catch. */
export function findTsTestReferencedIds(
  testSource: string,
  canonical: readonly string[],
): Set<string> {
  const found = new Set<string>();
  for (const id of canonical) {
    const escaped = id.replace(/[.*+?^${}()|[\]\\]/g, "\\$&");
    const re = new RegExp(`["'\`]${escaped}["'\`]`);
    if (re.test(testSource)) {
      found.add(id);
    }
  }
  return found;
}

export interface DriftReport {
  ok: boolean;
  goIds: string[];
  tsIds: string[];
  missingFromTs: string[];
  missingFromGo: string[];
  orderMismatchAt: number; // -1 if order matches
  /** Canonical Go pattern IDs that have no fixture in
   *  `redaction_fuzz_test.go`. A non-empty list means a pattern was added
   *  without test coverage. */
  missingFromGoFuzz: string[];
  /** Canonical TS pattern IDs that don't appear as a string literal in
   *  `redact.test.ts`. Same drift signal on the TS side. */
  missingFromTsTests: string[];
}

export interface CheckDriftSources {
  goPatterns: string;
  goFuzzTest: string;
  tsTest: string;
}

export function checkDrift(
  sources: CheckDriftSources | string,
  redactor: Redactor,
  legacyTsTestSource?: string,
): DriftReport {
  // Backwards-compatible signature: callers that pass a raw goPatterns
  // string + redactor still work; new callers pass the full source bundle.
  const resolved: CheckDriftSources =
    typeof sources === "string"
      ? { goPatterns: sources, goFuzzTest: "", tsTest: legacyTsTestSource ?? "" }
      : sources;

  const goIds = parseGoPatternIds(resolved.goPatterns);
  const tsIds = [...redactor.patternIds()];
  const goFuzzIds = parseGoFuzzFixtureIds(resolved.goFuzzTest);
  const tsTestRefs = findTsTestReferencedIds(resolved.tsTest, tsIds);

  const goSet = new Set(goIds);
  const tsSet = new Set(tsIds);
  const goFuzzSet = new Set(goFuzzIds);

  const missingFromTs = goIds.filter((id) => !tsSet.has(id));
  const missingFromGo = tsIds.filter((id) => !goSet.has(id));
  const missingFromGoFuzz = resolved.goFuzzTest
    ? goIds.filter((id) => !goFuzzSet.has(id))
    : [];
  const missingFromTsTests = resolved.tsTest
    ? tsIds.filter((id) => !tsTestRefs.has(id))
    : [];

  let orderMismatchAt = -1;
  if (missingFromTs.length === 0 && missingFromGo.length === 0) {
    for (let i = 0; i < goIds.length; i++) {
      if (goIds[i] !== tsIds[i]) {
        orderMismatchAt = i;
        break;
      }
    }
  }

  return {
    ok:
      missingFromTs.length === 0 &&
      missingFromGo.length === 0 &&
      orderMismatchAt === -1 &&
      missingFromGoFuzz.length === 0 &&
      missingFromTsTests.length === 0,
    goIds,
    tsIds,
    missingFromTs,
    missingFromGo,
    orderMismatchAt,
    missingFromGoFuzz,
    missingFromTsTests,
  };
}

function main(): void {
  const goPatterns = readFileSync(GO_REDACT_FILE, "utf8");
  const goFuzzTest = readFileSync(GO_FUZZ_TEST_FILE, "utf8");
  const tsTest = readFileSync(TS_TEST_FILE, "utf8");
  const report = checkDrift({ goPatterns, goFuzzTest, tsTest }, new Redactor());

  if (report.ok) {
    process.stderr.write(
      `[redactlint] OK — ${report.goIds.length} pattern IDs match between Go and TS, all covered by tests on both sides.\n`,
    );
    process.exit(0);
  }

  process.stderr.write("[redactlint] DRIFT detected:\n\n");
  process.stderr.write(`Go patterns (${report.goIds.length}):\n`);
  for (const id of report.goIds) process.stderr.write(`  ${id}\n`);
  process.stderr.write(`\nTS patterns (${report.tsIds.length}):\n`);
  for (const id of report.tsIds) process.stderr.write(`  ${id}\n`);

  if (report.missingFromTs.length > 0) {
    process.stderr.write("\nMissing from TS:\n");
    for (const id of report.missingFromTs) process.stderr.write(`  ${id}\n`);
  }
  if (report.missingFromGo.length > 0) {
    process.stderr.write("\nMissing from Go:\n");
    for (const id of report.missingFromGo) process.stderr.write(`  ${id}\n`);
  }
  if (report.orderMismatchAt >= 0) {
    process.stderr.write(
      `\nOrder mismatch at index ${report.orderMismatchAt}: Go="${report.goIds[report.orderMismatchAt]}" TS="${report.tsIds[report.orderMismatchAt]}"\n`,
    );
  }
  if (report.missingFromGoFuzz.length > 0) {
    process.stderr.write("\nMissing from Go fuzz fixtures (redaction_fuzz_test.go patternFixtures):\n");
    for (const id of report.missingFromGoFuzz) process.stderr.write(`  ${id}\n`);
  }
  if (report.missingFromTsTests.length > 0) {
    process.stderr.write("\nMissing from TS test references (redact.test.ts):\n");
    for (const id of report.missingFromTsTests) process.stderr.write(`  ${id}\n`);
  }
  process.stderr.write(
    "\nFix: edit BOTH apps/daemon/internal/redaction/patterns.go AND apps/desktop/src/shared/redact/redact.ts in lockstep, and ensure each new pattern has a fuzz fixture (patternFixtures) on the Go side and a referenced literal on the TS side.\n",
  );
  process.exit(1);
}

if (import.meta.main) {
  main();
}
