// `@hoopoe/fixtures` — Mock Flywheel corpus.
//
// Phase 0 (hp-7cs / hp-6v3 / hp-78m / hp-wle / hp-d54 / hp-pl5o) captures
// real ACFS-VPS JSON snapshots and parser fixtures for Git, beads, `bv`
// triage/plan/insights/diff, NTM, Agent Mail, reservations, and health.
// They feed Mock Flywheel Mode (`plan.md §13`), the daemon's adapter
// contract tests (`plan.md §18.3`), and the tending evaluation harness
// (`plan.md §8.8`).
//
// For hp-xru this file is a placeholder so the workspace has something to
// type-check. The real fixture loader and corpus index land in Phase 0.

export const HOOPOE_FIXTURES_PACKAGE_NAME = "@hoopoe/fixtures";

export type FixtureKind =
  | "git_status"
  | "br_list"
  | "bv_triage"
  | "bv_plan"
  | "bv_insights"
  | "bv_diff"
  | "ntm_snapshot"
  | "agent_mail_dump"
  | "file_reservations"
  | "health_lizard"
  | "ru_sync"
  | "ru_status"
  | "ru_list"
  | "ru_prune";

// Re-exports for the corpus loader + Mock Flywheel replay engine (hp-wle, hp-o74).
// Downstream consumers (apps/desktop, future hp-q3t harness) import from here.
export {
  ADAPTER_SLUGS,
  TENDING_SCENARIOS,
  PHASE0_SCENARIOS,
  GOLDEN_OUTPUT_STATES,
  FIXTURE_KINDS,
  type AdapterSlug,
  type TendingScenarioId,
  type Phase0ScenarioId,
  type GoldenOutputState,
  type FixtureMeta,
  type GoldenOutputMeta,
  type GoldenOutputFixture,
  type InvocationEnvelope,
  type ExpectedOutcome,
} from "./kinds.ts";

export {
  fixturesRoot,
  scenarioPath,
  phase0ScenarioPath,
  goldenOutputPath,
  enumerateRequiredFixtures,
  loaderSelfDescribe,
  FixtureNotFoundError,
  FIXTURES_VERSION,
} from "./loader.ts";

export {
  loadTendingScenario,
  loadPhase0Scenario,
  listAvailableScenarios,
  ScenarioLoadError,
  startReplay,
  deriveCursors,
  createMockDaemonClient,
  deriveScenarioEndCursors,
  MOCK_PAIRING_TOKEN,
  MOCK_BEARER_TOKEN,
  MOCK_WS_TOKEN,
  MOCK_FLYWHEEL_HEALTH_TIME,
  type LoadedScenario,
  type ReplayEvent,
  type PaneLog,
  type BuildLog,
  type ReplaySession,
  type ReplaySpeed,
  type ReplaySubscriber,
  type StartReplayOptions,
  type MockDaemonClient,
  type MockDaemonClientOptions,
  type MockBearerResponse,
  type MockWsTokenResponse,
  type MockPairingToken,
  type MockSubscribeOptions,
} from "../replay/index.ts";

export {
  validateCorpus,
  formatResult,
  type ValidationResult,
  type Finding,
  type FindingSeverity,
} from "./validate.ts";
