// `apps/desktop/src/test-utils/replay` — re-exports for Phase 0 fixture-replay
// harness consumers in this workspace (hp-q3t).
//
// Tests under `apps/desktop/tests/replay/` import from here so the desktop
// package owns the surface its tests touch (no deep imports into a sibling
// package). The actual implementation lives in `@hoopoe/fixture-replay`.

export {
  ALLOWED_SECRET_LITERALS,
  FixtureReplayAssertionError,
  STAGE_ADAPTERS,
  STAGE_IDS,
  SnapshotLoadError,
  assertNoUnredactedSecrets,
  bootMockFlywheel,
  bootScenarioLibrary,
  createReplayClient,
  expectAdapterCalled,
  expectAdapterNotCalled,
  expectStageReached,
  getEmittedEvents,
  isPhase0ScenarioId,
  isStageId,
  loadPhase0Snapshot,
  scanEventsForSecrets,
  stageForAdapter,
  synthesizeBaselineEvents,
  type AdapterCallRecord,
  type AdapterId,
  type AdapterIndex,
  type BaselineEventPayload,
  type BootMockFlywheelOptions,
  type BootScenarioLibraryOptions,
  type CapabilityDescriptor,
  type InvocationEnvelope,
  type LoadedPhase0Scenario,
  type LoadPhase0Options,
  type PrepareStatus,
  type ReplayClient,
  type SecretFinding,
  type SecretScanResult,
  type Snapshot,
  type SnapshotMeta,
  type StageId,
  type ToolCapture,
} from "@hoopoe/fixture-replay";
