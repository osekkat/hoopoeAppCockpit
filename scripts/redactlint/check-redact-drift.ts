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

export interface DriftReport {
  ok: boolean;
  goIds: string[];
  tsIds: string[];
  missingFromTs: string[];
  missingFromGo: string[];
  orderMismatchAt: number; // -1 if order matches
}

export function checkDrift(goSource: string, redactor: Redactor): DriftReport {
  const goIds = parseGoPatternIds(goSource);
  const tsIds = [...redactor.patternIds()];

  const goSet = new Set(goIds);
  const tsSet = new Set(tsIds);

  const missingFromTs = goIds.filter((id) => !tsSet.has(id));
  const missingFromGo = tsIds.filter((id) => !goSet.has(id));

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
    ok: missingFromTs.length === 0 && missingFromGo.length === 0 && orderMismatchAt === -1,
    goIds,
    tsIds,
    missingFromTs,
    missingFromGo,
    orderMismatchAt,
  };
}

function main(): void {
  const source = readFileSync(GO_REDACT_FILE, "utf8");
  const report = checkDrift(source, new Redactor());

  if (report.ok) {
    process.stderr.write(
      `[redactlint] OK — ${report.goIds.length} pattern IDs match between Go and TS.\n`,
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
  process.stderr.write(
    "\nFix: edit BOTH apps/daemon/internal/redaction/patterns.go AND apps/desktop/src/shared/redact/redact.ts in lockstep.\n",
  );
  process.exit(1);
}

if (import.meta.main) {
  main();
}
