// Hoopoe-owned. Wires the MockDaemonClient onto the IpcRegistry so the
// renderer can call mock-mode commands through the same IPC surface the
// real daemon exposes (hp-o74).
//
// This file is the boundary between:
//   - The transport-agnostic `MockDaemonClient` from `@hoopoe/fixtures`
//     (knows about scenarios, replay sessions, mock tokens) AND
//   - The IPC layer (`IpcRegistry`) that the renderer talks to.
//
// Adding a new mock-backed RPC = add one IpcCommandRegistration here.
// The renderer never reaches into `@hoopoe/fixtures` directly.
//
// When the production daemon-RPC client lands, an analogous module will
// register the same command IDs against the real client. The renderer
// is unchanged. (Per the bead: "the renderer cannot tell the difference".)

import type { IpcCommandRegistration, IpcRegistry } from "./IpcRegistry.ts";
import {
  MOCK_FLYWHEEL_COMMANDS,
  type MockFlywheelCommandId,
} from "../shared/ipc-contract.ts";
import type {
  MockDaemonClient,
  ReplayEvent,
  ReplaySession,
  ReplaySpeed,
} from "@hoopoe/fixtures";

/** When-clause key set by `MockFlywheelMode.parseArgvForMockFlywheel().enabled`.
 *  Renderers / commands that should NOT be available in mock mode (e.g.
 *  "push to origin") declare `whenContextKeys: [WHEN_NOT_MOCK_FLYWHEEL]`
 *  so the IpcRegistry hides them automatically. */
export const WHEN_MOCK_FLYWHEEL = "mockFlywheel";
export const WHEN_NOT_MOCK_FLYWHEEL = "notMockFlywheel";

export { MOCK_FLYWHEEL_COMMANDS, type MockFlywheelCommandId };

export interface RegisterMockFlywheelClientOptions {
  ipcRegistry: IpcRegistry;
  client: MockDaemonClient;
  /** Initial replay speed used for default subscriptions. Default: 1×. */
  initialReplaySpeed?: ReplaySpeed;
  /** Sink for events delivered by mock subscriptions. The renderer side
   *  forwards these to the WS-style event channel. Provided by callers
   *  (typically the BackendLifecycle wiring or a Phase 1 subscription
   *  manager); for hp-o74 the test substitutes its own. */
  emitEvent?: (event: ReplayEvent) => void;
}

export interface RegisteredMockFlywheelClient {
  /** Total IPC command registrations made. */
  commandsRegistered: number;
  /** Currently-active subscription session (for tests). */
  session: ReplaySession | null;
  /** Tear down all registrations + cancel any active session. */
  unregister: () => void;
}

/** Register every MockDaemonClient-backed IPC command against the given
 *  IpcRegistry. Returns a teardown handle for tests + hot-reload. */
export function registerMockFlywheelClient(
  options: RegisterMockFlywheelClientOptions,
): RegisteredMockFlywheelClient {
  const { ipcRegistry, client } = options;
  const initialSpeed: ReplaySpeed = options.initialReplaySpeed ?? 1;
  let session: ReplaySession | null = null;
  let currentSpeed: ReplaySpeed = initialSpeed;

  const teardowns: Array<() => void> = [];
  function reg<I, O>(id: string, handle: (input: I) => O | Promise<O>): void {
    const registration: IpcCommandRegistration<I, O> = {
      id,
      handler: { handle },
      whenContextKeys: [WHEN_MOCK_FLYWHEEL],
    };
    const disposed = ipcRegistry.register(registration);
    teardowns.push(disposed.unregister);
  }

  reg<void, ReturnType<MockDaemonClient["health"]>>(MOCK_FLYWHEEL_COMMANDS.health, () =>
    client.health(),
  );
  reg<void, ReturnType<MockDaemonClient["version"]>>(
    MOCK_FLYWHEEL_COMMANDS.version,
    () => client.version(),
  );
  reg<void, ReturnType<MockDaemonClient["capabilities"]>>(
    MOCK_FLYWHEEL_COMMANDS.capabilities,
    () => client.capabilities(),
  );
  reg<void, ReturnType<MockDaemonClient["listProjects"]>>(
    MOCK_FLYWHEEL_COMMANDS.listProjects,
    () => client.listProjects(),
  );
  reg<{ projectId: string }, unknown>(MOCK_FLYWHEEL_COMMANDS.getBeads, ({ projectId }) =>
    client.getBeads(projectId),
  );
  reg<{ projectId: string }, unknown>(MOCK_FLYWHEEL_COMMANDS.getTriage, ({ projectId }) =>
    client.getTriage(projectId),
  );
  reg<{ projectId: string }, unknown>(
    MOCK_FLYWHEEL_COMMANDS.getSwarmSnapshot,
    ({ projectId }) => client.getSwarmSnapshot(projectId),
  );
  reg<{ projectId: string }, unknown>(MOCK_FLYWHEEL_COMMANDS.getMailDump, ({ projectId }) =>
    client.getMailDump(projectId),
  );
  reg<{ projectId: string }, unknown>(
    MOCK_FLYWHEEL_COMMANDS.getReservations,
    ({ projectId }) => client.getReservations(projectId),
  );
  reg<{ runId: string }, ReturnType<MockDaemonClient["getBuildLog"]>>(
    MOCK_FLYWHEEL_COMMANDS.getBuildLog,
    ({ runId }) => client.getBuildLog(runId),
  );
  reg<{ agent: string }, ReturnType<MockDaemonClient["getPaneLog"]>>(
    MOCK_FLYWHEEL_COMMANDS.getPaneLog,
    ({ agent }) => client.getPaneLog(agent),
  );
  reg<{ pairingToken: string }, ReturnType<MockDaemonClient["exchangePairingForBearer"]>>(
    MOCK_FLYWHEEL_COMMANDS.exchangePairingForBearer,
    (input) => client.exchangePairingForBearer(input),
  );
  reg<{ bearerToken: string }, ReturnType<MockDaemonClient["issueWsToken"]>>(
    MOCK_FLYWHEEL_COMMANDS.issueWsToken,
    (input) => client.issueWsToken(input),
  );
  reg<void, { scenarioId: string; cursors: Record<string, number> }>(
    MOCK_FLYWHEEL_COMMANDS.scenarioInfo,
    () => ({ scenarioId: client.scenarioId(), cursors: client.currentCursors() }),
  );
  reg<{ scenarioId: string; isAbsolutePath?: boolean }, { scenarioId: string }>(
    MOCK_FLYWHEEL_COMMANDS.swapScenario,
    ({ scenarioId, isAbsolutePath }) => {
      // Cancel any active subscription before swap; the renderer must
      // resubscribe (which is the right reconnect-replay behavior anyway).
      if (session) {
        session.cancel();
        session = null;
      }
      const opts = isAbsolutePath !== undefined ? { isAbsolutePath } : {};
      client.swapScenario(scenarioId, opts);
      // Auto-restart subscription if the caller had wired emitEvent.
      if (options.emitEvent) {
        session = client.subscribe(
          { speed: currentSpeed },
          options.emitEvent,
        );
      }
      return { scenarioId: client.scenarioId() };
    },
  );
  reg<{ speed: ReplaySpeed }, { speed: ReplaySpeed }>(
    MOCK_FLYWHEEL_COMMANDS.setReplaySpeed,
    ({ speed }) => {
      currentSpeed = speed;
      // Restart the subscription so the new speed takes effect.
      if (options.emitEvent) {
        session?.cancel();
        session = client.subscribe(
          { speed: currentSpeed },
          options.emitEvent,
        );
      }
      return { speed: currentSpeed };
    },
  );

  // Auto-start subscription if the caller wired emitEvent.
  if (options.emitEvent) {
    session = client.subscribe({ speed: currentSpeed }, options.emitEvent);
  }

  const handle: RegisteredMockFlywheelClient = {
    commandsRegistered: teardowns.length,
    get session() {
      return session;
    },
    unregister: () => {
      session?.cancel();
      session = null;
      while (teardowns.length > 0) {
        const t = teardowns.pop();
        if (t) t();
      }
    },
  };
  return handle;
}
