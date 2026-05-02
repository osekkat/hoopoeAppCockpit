import {
  MOCK_BEARER_TOKEN,
  MOCK_PAIRING_TOKEN,
  MOCK_WS_TOKEN,
  createMockDaemonClient,
  listAvailableScenarios,
  loadTendingScenario,
  type MockDaemonClient,
  type ReplayEvent,
  type TendingScenarioId,
} from "@hoopoe/fixtures";

export interface Phase1MockFlywheelHarness {
  readonly client: MockDaemonClient;
  readonly scenarioId: () => string;
  readonly availableScenarioIds: () => readonly string[];
  readonly projectId: () => string;
  readonly authRoundTrip: () => {
    readonly pairingToken: string;
    readonly bearerToken: string;
    readonly wsToken: string;
  };
  readonly snapshot: () => Phase1MockFlywheelSnapshot;
  readonly collectReplayEvents: (input?: { readonly channel?: string }) => Promise<{
    readonly events: readonly ReplayEvent[];
    readonly cursors: Readonly<Record<string, number>>;
  }>;
  readonly assertReady: () => Promise<Phase1MockFlywheelSnapshot>;
}

export interface Phase1MockFlywheelSnapshot {
  readonly scenarioId: string;
  readonly healthEnvironment: string;
  readonly projectId: string;
  readonly projectCount: number;
  readonly capabilityKeys: readonly string[];
  readonly fixtureEventCount: number;
  readonly buildLogCount: number;
  readonly paneLogCount: number;
  readonly beadPayloadKind: string;
  readonly triagePayloadKind: string;
  readonly swarmPayloadKind: string;
}

export function createPhase1MockFlywheelHarness(input?: {
  readonly scenarioId?: TendingScenarioId | string;
}): Phase1MockFlywheelHarness {
  const scenarioId = input?.scenarioId ?? "healthy-hour";
  const client = createMockDaemonClient({ scenarioId, speed: "instant" });

  const projectId = () => {
    const project = client.listProjects()[0];
    if (!project) {
      throw new Error(`Mock Flywheel scenario ${client.scenarioId()} has no projects`);
    }
    return project.id;
  };

  const snapshot = (): Phase1MockFlywheelSnapshot => {
    const loaded = loadTendingScenario(client.scenarioId());
    const activeProjectId = projectId();

    return {
      scenarioId: client.scenarioId(),
      healthEnvironment: client.health().environment,
      projectId: activeProjectId,
      projectCount: client.listProjects().length,
      capabilityKeys: Object.keys(client.capabilities()).toSorted(),
      fixtureEventCount: loaded.events.length,
      buildLogCount: loaded.buildLogs.length,
      paneLogCount: loaded.paneLogs.length,
      beadPayloadKind: payloadKind(client.getBeads(activeProjectId)),
      triagePayloadKind: payloadKind(client.getTriage(activeProjectId)),
      swarmPayloadKind: payloadKind(client.getSwarmSnapshot(activeProjectId)),
    };
  };

  return {
    client,
    scenarioId: () => client.scenarioId(),
    availableScenarioIds: () => listAvailableScenarios(),
    projectId,
    authRoundTrip: () => {
      const bearer = client.exchangePairingForBearer({ pairingToken: MOCK_PAIRING_TOKEN });
      const ws = client.issueWsToken({ bearerToken: bearer.bearerToken });
      return {
        pairingToken: MOCK_PAIRING_TOKEN,
        bearerToken: bearer.bearerToken,
        wsToken: ws.wsToken,
      };
    },
    snapshot,
    collectReplayEvents: async (input) => {
      const events: ReplayEvent[] = [];
      const session = client.subscribe(
        {
          speed: "instant",
          fromCursors: {},
          ...(input?.channel !== undefined ? { channel: input.channel } : {}),
        },
        (event) => {
          events.push(event);
        },
      );
      await session.done;
      return {
        events,
        cursors: session.cursors(),
      };
    },
    assertReady: async () => {
      const state = snapshot();
      const auth = client.issueWsToken({
        bearerToken: client.exchangePairingForBearer({ pairingToken: MOCK_PAIRING_TOKEN })
          .bearerToken,
      });
      const replay = await new Promise<readonly ReplayEvent[]>((resolve) => {
        const events: ReplayEvent[] = [];
        const session = client.subscribe({ speed: "instant" }, (event) => {
          events.push(event);
        });
        void session.done.then(() => {
          resolve(events);
        });
      });

      if (state.healthEnvironment !== "mock-flywheel") {
        throw new Error(`Expected mock-flywheel health, received ${state.healthEnvironment}`);
      }
      if (state.projectCount < 1 || state.capabilityKeys.length < 1) {
        throw new Error(`Scenario ${state.scenarioId} is missing project/capability fixtures`);
      }
      if (auth.wsToken !== MOCK_WS_TOKEN || replay.length < 1) {
        throw new Error(`Scenario ${state.scenarioId} failed auth/replay readiness`);
      }

      return state;
    },
  };
}

export const EXPECTED_PHASE1_MOCK_TOKENS = {
  pairingToken: MOCK_PAIRING_TOKEN,
  bearerToken: MOCK_BEARER_TOKEN,
  wsToken: MOCK_WS_TOKEN,
} as const;

function payloadKind(value: unknown): string {
  if (Array.isArray(value)) return "array";
  if (value === null) return "null";
  return typeof value;
}
