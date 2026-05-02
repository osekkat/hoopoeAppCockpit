// `@hoopoe/fixtures/replay` — public entrypoint (hp-o74).
//
// Re-exports the surface the desktop main process and tests consume.

export {
  loadTendingScenario,
  loadPhase0Scenario,
  listAvailableScenarios,
  ScenarioLoadError,
  type LoadedScenario,
  type ReplayEvent,
  type PaneLog,
  type BuildLog,
} from "./scenario-source.ts";

export {
  startReplay,
  deriveCursors,
  type ReplaySession,
  type ReplaySpeed,
  type ReplaySubscriber,
  type StartReplayOptions,
} from "./event-stream.ts";

export {
  createMockDaemonClient,
  deriveScenarioEndCursors,
  MOCK_PAIRING_TOKEN,
  MOCK_BEARER_TOKEN,
  MOCK_WS_TOKEN,
  type MockDaemonClient,
  type MockDaemonClientOptions,
  type MockBearerResponse,
  type MockWsTokenResponse,
  type MockPairingToken,
  type MockSubscribeOptions,
} from "./mock-daemon-client.ts";
