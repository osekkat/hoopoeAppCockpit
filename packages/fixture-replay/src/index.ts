// `@hoopoe/fixture-replay` — public API surface (hp-q3t).
//
// Tests import from this entrypoint only:
//
//   import {
//     bootMockFlywheel,
//     expectStageReached,
//     expectAdapterCalled,
//     getEmittedEvents,
//     assertNoUnredactedSecrets,
//   } from "@hoopoe/fixture-replay";
//
// See packages/fixture-replay/README.md for the full usage story.

export {
  bootMockFlywheel,
  bootScenarioLibrary,
  type BootMockFlywheelOptions,
  type BootScenarioLibraryOptions,
} from "./boot.ts";

export {
  createReplayClient,
  type AdapterId,
  type AdapterCallRecord,
  type ReplayClient,
} from "./client.ts";

export {
  loadPhase0Snapshot,
  isPhase0ScenarioId,
  SnapshotLoadError,
  type AdapterIndex,
  type CapabilityDescriptor,
  type InvocationEnvelope,
  type LoadedPhase0Scenario,
  type LoadPhase0Options,
  type PrepareStatus,
  type Snapshot,
  type SnapshotMeta,
  type ToolCapture,
} from "./snapshot-loader.ts";

export {
  STAGE_ADAPTERS,
  STAGE_IDS,
  isStageId,
  stageForAdapter,
  type StageId,
} from "./stages.ts";

export {
  synthesizeBaselineEvents,
  type BaselineEventPayload,
} from "./events.ts";

export {
  scanEventsForSecrets,
  ALLOWED_SECRET_LITERALS,
  type SecretFinding,
  type SecretScanResult,
} from "./secret-scan.ts";

export {
  assertNoUnredactedSecrets,
  expectAdapterCalled,
  expectAdapterNotCalled,
  expectStageReached,
  getEmittedEvents,
  FixtureReplayAssertionError,
} from "./assertions.ts";
