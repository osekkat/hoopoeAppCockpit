// `@hoopoe/test-evidence` — test-run evidence envelope (hp-6sv).
//
// One JSON shape, written by every runner. The envelope is the test log;
// there is no other "logs you have to dig for" surface.
//
// Cross-references:
//   - hp-6sv DOD ("REPORTERS" block).
//   - plan.md §10.5 (SLO targets) — `slo` block carries observed-vs-target.
//   - plan.md §18 (acceptance / release smoke) — these envelopes are the
//     audit trail per release.
//
// Schema version is bumped (per plan.md §10.3 schema-version discipline)
// when a field is renamed / removed / re-typed; additive optional fields
// do NOT bump.

import { randomUUID as nodeRandomUUID } from "node:crypto";

export const TEST_EVIDENCE_SCHEMA_VERSION = 1 as const;

export type TestStatus = "passed" | "failed" | "skipped";

export type RunnerId = "bun-test" | "vitest" | "go-test" | "playwright";

export interface SloAssertion {
  /** Stable id from `packages/slo-targets.yaml`. */
  target: string;
  /** Target value as declared in YAML (e.g. "10000ms p95", "true"). */
  declared: string;
  /** Numerically observed value in the same unit as `declared`. */
  observed: number;
  /** Whether observed satisfies declared. */
  passed: boolean;
  /** Free-form note for context (e.g. "n=50 samples"). */
  notes?: string;
}

export interface StructuredLogSlice {
  /** Raw structured-logger line (1:1 with `StructuredTestLogLine` from
   *  `apps/desktop/src/test-utils/structured-logger.ts`). Held as a
   *  pre-parsed object so the envelope is one well-formed JSON document. */
  ts: string;
  step: string;
  status: string;
  durationMs: number;
  errorMessage?: string;
  data?: unknown;
}

export interface TestResult {
  /** Test name (the `it`/`test` description). */
  name: string;
  /** Source file the test came from. */
  file: string;
  status: TestStatus;
  durationMs: number;
  /** SLO assertion if the test name carried `@slo:<targetId>`. */
  slo?: SloAssertion;
  /** Tail of the structured-logger output for this test (last 200 entries). */
  logSlice?: readonly StructuredLogSlice[];
  /** Failure message + stack snippet if `status === 'failed'`. */
  errorMessage?: string;
  /** Raw assertion / test class for debug. */
  classname?: string;
}

export interface CoverageBlock {
  statements: number;
  branches: number;
  lines: number;
  functions: number;
  /** Delta vs the baseline (typically `main` branch coverage on this commit). */
  deltaVsMain?: {
    statements: number;
    branches: number;
    lines: number;
    functions: number;
  };
}

export interface RedactionStats {
  /** Map of detector name → count of matches scrubbed before serializing. */
  patternsMatched: Record<string, number>;
}

export interface SloRollup {
  /** Total target lines loaded from the SLO file. */
  targetsLoaded: number;
  /** Tests that crossed their declared SLO. */
  breached: ReadonlyArray<{
    target: string;
    test: string;
    observed: number;
    declared: string;
  }>;
}

export interface TestEvidenceEnvelope {
  schemaVersion: typeof TEST_EVIDENCE_SCHEMA_VERSION;
  /** Stable per-run UUID. */
  runId: string;
  /** RFC3339 capture timestamp. */
  ts: string;
  /** Current HEAD sha (or `unknown` if not in a git work tree). */
  gitSha: string;
  /** Optional dirty-tree marker (the working tree had uncommitted edits). */
  gitDirty?: boolean;
  /** Daemon binary version, OR the desktop package version, OR `unknown`. */
  daemonVersion: string;
  /** Mock Flywheel scenario id when running fixture-driven tests. */
  fixtureScenario: string | null;
  runner: RunnerId;
  /** Phase tag ("phase1", "phase2", "phase2.5", …) for evidence directory layout. */
  phase: string;
  results: readonly TestResult[];
  coverage: CoverageBlock | null;
  artifacts: readonly string[];
  redactionStats: RedactionStats;
  slo: SloRollup;
}

export interface BuildEnvelopeInput {
  runId?: string;
  ts?: string;
  gitSha: string;
  gitDirty?: boolean;
  daemonVersion: string;
  fixtureScenario?: string | null;
  runner: RunnerId;
  phase: string;
  results: readonly TestResult[];
  coverage?: CoverageBlock | null;
  artifacts?: readonly string[];
  redactionStats?: RedactionStats;
  slo?: SloRollup;
}

/** Build an envelope from collected pieces. Defaults `runId` and `ts` so
 *  callers don't have to plumb either down. */
export function buildEnvelope(input: BuildEnvelopeInput): TestEvidenceEnvelope {
  const env: TestEvidenceEnvelope = {
    schemaVersion: TEST_EVIDENCE_SCHEMA_VERSION,
    runId: input.runId ?? cryptoRandomUUID(),
    ts: input.ts ?? new Date().toISOString(),
    gitSha: input.gitSha,
    daemonVersion: input.daemonVersion,
    fixtureScenario: input.fixtureScenario ?? null,
    runner: input.runner,
    phase: input.phase,
    results: input.results,
    coverage: input.coverage ?? null,
    artifacts: input.artifacts ?? [],
    redactionStats: input.redactionStats ?? { patternsMatched: {} },
    slo: input.slo ?? { targetsLoaded: 0, breached: [] },
  };
  if (input.gitDirty === true) {
    return { ...env, gitDirty: true };
  }
  return env;
}

function cryptoRandomUUID(): string {
  return nodeRandomUUID();
}
