# `@hoopoe/test-evidence`

JSON evidence envelope emitter shared by Bun test, `go test`, and
Playwright. One shape across all three runners; every test run writes
exactly one envelope under
`docs/test-evidence/<phase>/<UTC-timestamp>/<runner>-<runId>.json`.

> Bead: `hp-6sv` — Test runner config + JSON evidence emitter.

See also: `docs/testing.md` for the user-facing tag taxonomy + run
commands, and `packages/slo-targets.yaml` for the §10.5 SLO source of
truth.

## Public API

```ts
import {
  buildEnvelope,
  writeEvidence,
  evidencePath,
  loadSloTargets,
  evaluateAgainst,
  parseTags,
  parseJunitXml,
  parseGoTestNdjson,
  collectStructuredLogLines,
  sliceForCase,
  buildCoverageBlock,
  computeDelta,
  computeRedactionStats,
  readGitContext,
  TEST_EVIDENCE_SCHEMA_VERSION,
  type TestEvidenceEnvelope,
  type TestResult,
  type SloTarget,
} from "@hoopoe/test-evidence";

import HoopoeReporter from "@hoopoe/test-evidence/playwright-reporter";
```

## Envelope schema (v1)

```ts
interface TestEvidenceEnvelope {
  schemaVersion: 1;
  runId: string;                  // UUID
  ts: string;                     // RFC3339
  gitSha: string;                 // HEAD or "unknown"
  gitDirty?: boolean;             // working tree had uncommitted edits
  daemonVersion: string;          // env or "unknown"
  fixtureScenario: string | null; // "fresh" | "active" | "failure" | null
  runner: "bun-test" | "vitest" | "go-test" | "playwright";
  phase: string;                  // "phase1" | "phase2" | …
  results: TestResult[];
  coverage: CoverageBlock | null;
  artifacts: string[];
  redactionStats: { patternsMatched: Record<string, number> };
  slo: { targetsLoaded: number; breached: SloBreach[] };
}

interface TestResult {
  name: string;
  file: string;
  status: "passed" | "failed" | "skipped";
  durationMs: number;
  slo?: { target: string; declared: string; observed: number; passed: boolean };
  logSlice?: StructuredLogSlice[]; // last 200 entries from the structured logger
  errorMessage?: string;
  classname?: string;
}
```

The full type set lives in `src/envelope.ts`.

## Writing an envelope

```ts
const env = buildEnvelope({
  gitSha: readGitContext().sha,
  daemonVersion: process.env.HOOPOE_DAEMON_VERSION ?? "unknown",
  runner: "bun-test",
  phase: "phase2",
  results: parseJunitXml(xml).cases,
  redactionStats: computeRedactionStats(JSON.stringify(rawLogs)),
});
const written = await writeEvidence(env, { repoRoot });
console.log(written.relativePath);
// → docs/test-evidence/phase2/20260504T010203Z/bun-test-<uuid>.json
```

## Wiring Playwright (env-gated)

`playwright.config.ts` (no behavior change unless `HOOPOE_TEST_EVIDENCE=1`):

```ts
import { defineConfig } from "@playwright/test";
export default defineConfig({
  reporter:
    process.env.HOOPOE_TEST_EVIDENCE === "1"
      ? [["@hoopoe/test-evidence/playwright-reporter"]]
      : [["list"]],
});
```

Set `HOOPOE_TEST_EVIDENCE_PHASE=phase2` and `HOOPOE_FIXTURE_SCENARIO=fresh`
to have those propagate into the envelope.

## Why a separate package

Shared between the desktop, daemon, and root scripts directories — and
referenced by `playwright.config.ts` (root) plus the per-runner
wrappers. Keeping it in `packages/` avoids duplicating envelope
construction across three call sites.

## Testing

```bash
rch exec -- bun run --cwd packages/test-evidence test
rch exec -- bun run --cwd packages/test-evidence typecheck
```
