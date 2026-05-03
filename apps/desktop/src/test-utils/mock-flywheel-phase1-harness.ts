import {
  listAvailableScenarios,
  type MockDaemonClient,
  type ReplayEvent,
  type TendingScenarioId,
} from "@hoopoe/fixtures";
import { IpcRegistry } from "../main/IpcRegistry.ts";
import {
  MOCK_FLYWHEEL_AUTH_TOKENS,
  MOCK_FLYWHEEL_FLAG,
  SCENARIO_FLAG,
  createMockFlywheelDaemon,
  parseArgvForMockFlywheel,
} from "../main/MockFlywheelMode.ts";
import {
  MOCK_FLYWHEEL_COMMANDS,
  WHEN_MOCK_FLYWHEEL,
  registerMockFlywheelClient,
} from "../main/MockFlywheelClient.ts";

export interface Phase1MockFlywheelHarness {
  readonly client: MockDaemonClient;
  readonly ipcRegistry: IpcRegistry;
  readonly scenarioId: () => string;
  readonly availableScenarioIds: () => readonly string[];
  readonly projectId: () => Promise<string>;
  readonly authRoundTrip: () => Promise<{
    readonly pairingToken: string;
    readonly bearerToken: string;
    readonly wsToken: string;
  }>;
  readonly snapshot: () => Promise<Phase1MockFlywheelSnapshot>;
  readonly collectReplayEvents: (input?: { readonly channel?: string }) => Promise<{
    readonly events: readonly ReplayEvent[];
    readonly cursors: Readonly<Record<string, number>>;
  }>;
  readonly assertReady: () => Promise<Phase1MockFlywheelSnapshot>;
  readonly close: () => void;
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
  const mode = parseArgvForMockFlywheel({
    argv: ["bun", "hoopoe", MOCK_FLYWHEEL_FLAG, SCENARIO_FLAG, scenarioId],
    nodeEnv: "test",
    homedirImpl: () => "/tmp/hoopoe-phase1-mock-flywheel",
  });
  const client = createMockFlywheelDaemon(mode);
  const ipcRegistry = new IpcRegistry();
  const replayEvents: ReplayEvent[] = [];
  const registration = registerMockFlywheelClient({
    ipcRegistry,
    client,
    initialReplaySpeed: "instant",
    emitEvent: (event) => {
      replayEvents.push(event);
    },
  });

  const dispatch = async <Input, Output>(commandId: string, value: Input): Promise<Output> =>
    await ipcRegistry.dispatch<Input, Output>(
      commandId,
      value,
      { [WHEN_MOCK_FLYWHEEL]: true },
    );

  const projectId = async () => {
    const projects = await dispatch<void, ReturnType<MockDaemonClient["listProjects"]>>(
      MOCK_FLYWHEEL_COMMANDS.listProjects,
      undefined,
    );
    const project = projects[0];
    if (!project) {
      throw new Error(`Mock Flywheel scenario ${client.scenarioId()} has no projects`);
    }
    return project.id;
  };

  const snapshot = async (): Promise<Phase1MockFlywheelSnapshot> => {
    const activeProjectId = await projectId();
    const health = await dispatch<void, ReturnType<MockDaemonClient["health"]>>(
      MOCK_FLYWHEEL_COMMANDS.health,
      undefined,
    );
    const projects = await dispatch<void, ReturnType<MockDaemonClient["listProjects"]>>(
      MOCK_FLYWHEEL_COMMANDS.listProjects,
      undefined,
    );
    const capabilities = await dispatch<void, ReturnType<MockDaemonClient["capabilities"]>>(
      MOCK_FLYWHEEL_COMMANDS.capabilities,
      undefined,
    );
    const beads = await dispatch<{ projectId: string }, unknown>(
      MOCK_FLYWHEEL_COMMANDS.getBeads,
      { projectId: activeProjectId },
    );
    const triage = await dispatch<{ projectId: string }, unknown>(
      MOCK_FLYWHEEL_COMMANDS.getTriage,
      { projectId: activeProjectId },
    );
    const swarm = await dispatch<{ projectId: string }, unknown>(
      MOCK_FLYWHEEL_COMMANDS.getSwarmSnapshot,
      { projectId: activeProjectId },
    );
    const buildLog = await dispatch<
      { runId: string },
      ReturnType<MockDaemonClient["getBuildLog"]>
    >(
      MOCK_FLYWHEEL_COMMANDS.getBuildLog,
      { runId: "build-healthy-001" },
    );
    const paneLog = await dispatch<
      { agent: string },
      ReturnType<MockDaemonClient["getPaneLog"]>
    >(
      MOCK_FLYWHEEL_COMMANDS.getPaneLog,
      { agent: "GreenBear" },
    );
    await registration.session?.done;

    return {
      scenarioId: client.scenarioId(),
      healthEnvironment: health.environment,
      projectId: activeProjectId,
      projectCount: projects.length,
      capabilityKeys: Object.keys(capabilities).toSorted(),
      fixtureEventCount: replayEvents.length,
      buildLogCount: buildLog === null ? 0 : 1,
      paneLogCount: paneLog === null ? 0 : 1,
      beadPayloadKind: payloadKind(beads),
      triagePayloadKind: payloadKind(triage),
      swarmPayloadKind: payloadKind(swarm),
    };
  };

  return {
    client,
    ipcRegistry,
    scenarioId: () => client.scenarioId(),
    availableScenarioIds: () => listAvailableScenarios(),
    projectId,
    authRoundTrip: async () => {
      const bearer = await dispatch<{ pairingToken: string }, { bearerToken: string }>(
        MOCK_FLYWHEEL_COMMANDS.exchangePairingForBearer,
        { pairingToken: MOCK_FLYWHEEL_AUTH_TOKENS.pairingToken },
      );
      const ws = await dispatch<{ bearerToken: string }, { wsToken: string }>(
        MOCK_FLYWHEEL_COMMANDS.issueWsToken,
        { bearerToken: bearer.bearerToken },
      );
      return {
        pairingToken: MOCK_FLYWHEEL_AUTH_TOKENS.pairingToken,
        bearerToken: bearer.bearerToken,
        wsToken: ws.wsToken,
      };
    },
    snapshot,
    collectReplayEvents: async (input) => {
      if (input?.channel !== undefined) {
        throw new Error("Phase1MockFlywheelHarness channel filtering is not IPC-exposed yet");
      }
      await registration.session?.done;
      const info = await dispatch<void, { cursors: Record<string, number> }>(
        MOCK_FLYWHEEL_COMMANDS.scenarioInfo,
        undefined,
      );
      return {
        events: replayEvents.slice(),
        cursors: info.cursors,
      };
    },
    assertReady: async () => {
      const state = await snapshot();
      const auth = await dispatch<{ bearerToken: string }, { wsToken: string }>(
        MOCK_FLYWHEEL_COMMANDS.issueWsToken,
        {
          bearerToken: (await dispatch<{ pairingToken: string }, { bearerToken: string }>(
            MOCK_FLYWHEEL_COMMANDS.exchangePairingForBearer,
            { pairingToken: MOCK_FLYWHEEL_AUTH_TOKENS.pairingToken },
          )).bearerToken,
        },
      );
      const replay = await registration.session?.done.then(() => replayEvents.slice()) ?? [];

      if (state.healthEnvironment !== "mock-flywheel") {
        throw new Error(`Expected mock-flywheel health, received ${state.healthEnvironment}`);
      }
      if (state.projectCount < 1 || state.capabilityKeys.length < 1) {
        throw new Error(`Scenario ${state.scenarioId} is missing project/capability fixtures`);
      }
      if (auth.wsToken !== MOCK_FLYWHEEL_AUTH_TOKENS.wsToken || replay.length < 1) {
        throw new Error(`Scenario ${state.scenarioId} failed auth/replay readiness`);
      }

      return state;
    },
    close: () => {
      registration.unregister();
    },
  };
}

export const EXPECTED_PHASE1_MOCK_TOKENS = {
  pairingToken: MOCK_FLYWHEEL_AUTH_TOKENS.pairingToken,
  bearerToken: MOCK_FLYWHEEL_AUTH_TOKENS.bearerToken,
  wsToken: MOCK_FLYWHEEL_AUTH_TOKENS.wsToken,
} as const;

function payloadKind(value: unknown): string {
  if (Array.isArray(value)) return "array";
  if (value === null) return "null";
  return typeof value;
}
