// `@hoopoe/test-evidence` — public API surface (hp-6sv).
//
// See `packages/test-evidence/README.md` and `docs/testing.md` for usage.

export {
  TEST_EVIDENCE_SCHEMA_VERSION,
  buildEnvelope,
  type BuildEnvelopeInput,
  type CoverageBlock,
  type RedactionStats,
  type RunnerId,
  type SloAssertion,
  type SloRollup,
  type StructuredLogSlice,
  type TestEvidenceEnvelope,
  type TestResult,
  type TestStatus,
} from "./envelope.ts";

export {
  evidencePath,
  timestampSegment,
  writeEvidence,
  type WriteEvidenceOptions,
  type WriteEvidenceResult,
} from "./writer.ts";

export {
  evaluateAgainst,
  loadSloTargets,
  SloTargetsError,
  type LoadSloTargetsOptions,
  type SloTarget,
  type SloTargets,
} from "./slo-targets.ts";

export {
  KNOWN_CATEGORY_TAGS,
  TAG_REGEX,
  parseTags,
  type ParsedTags,
} from "./slo-tags.ts";

export {
  parseJunitXml,
  type ParsedJunit,
} from "./junit-parser.ts";

export {
  parseGoTestNdjson,
  type ParsedGoTest,
} from "./go-json-parser.ts";

export {
  collectStructuredLogLines,
  sliceForCase,
  type CollectedSlices,
  type CollectOptions,
} from "./log-collector.ts";

export {
  buildCoverageBlock,
  computeDelta,
  loadCoverageSummary,
  type LoadCoverageOptions,
} from "./coverage-delta.ts";

export {
  ALLOWED_LITERALS,
  computeRedactionStats,
} from "./redaction.ts";

export {
  readGitContext,
  type GitContext,
  type ReadGitContextOptions,
} from "./git-context.ts";
