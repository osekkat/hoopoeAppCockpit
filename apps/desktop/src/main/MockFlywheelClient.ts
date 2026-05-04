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

type RegisterMockCommand = <Input, Output>(
  id: MockFlywheelCommandId,
  handle: (input: Input) => Output | Promise<Output>,
) => void;

interface ReplayLifecycle {
  readonly session: ReplaySession | null;
  start: () => void;
  cancel: () => void;
  swapScenario: (scenarioId: string, opts: { readonly isAbsolutePath?: boolean }) => string;
  setSpeed: (speed: ReplaySpeed) => ReplaySpeed;
}

/** Register every MockDaemonClient-backed IPC command against the given
 *  IpcRegistry. Returns a teardown handle for tests + hot-reload. */
export function registerMockFlywheelClient(
  options: RegisterMockFlywheelClientOptions,
): RegisteredMockFlywheelClient {
  const { ipcRegistry, client } = options;
  const teardowns: Array<() => void> = [];
  const register = createMockCommandRegistrar(ipcRegistry, teardowns);
  const replay = createReplayLifecycle(client, options);

  registerReadCommands(register, client);
  registerAuthCommands(register, client);
  registerScenarioCommands(register, client, replay);

  replay.start();

  return {
    commandsRegistered: teardowns.length,
    get session() {
      return replay.session;
    },
    unregister: () => {
      replay.cancel();
      unregisterAll(teardowns);
    },
  };
}

function createMockCommandRegistrar(
  ipcRegistry: IpcRegistry,
  teardowns: Array<() => void>,
): RegisterMockCommand {
  return function register<Input, Output>(
    id: MockFlywheelCommandId,
    handle: (input: Input) => Output | Promise<Output>,
  ): void {
    const registration: IpcCommandRegistration<Input, Output> = {
      id,
      handler: { handle },
      whenContextKeys: [WHEN_MOCK_FLYWHEEL],
    };
    const disposed = ipcRegistry.register(registration);
    teardowns.push(disposed.unregister);
  };
}

function registerReadCommands(register: RegisterMockCommand, client: MockDaemonClient): void {
  register<void, ReturnType<MockDaemonClient["health"]>>(MOCK_FLYWHEEL_COMMANDS.health, () =>
    client.health(),
  );
  register<void, ReturnType<MockDaemonClient["version"]>>(
    MOCK_FLYWHEEL_COMMANDS.version,
    () => client.version(),
  );
  register<void, ReturnType<MockDaemonClient["capabilities"]>>(
    MOCK_FLYWHEEL_COMMANDS.capabilities,
    () => client.capabilities(),
  );
  register<void, ReturnType<MockDaemonClient["listProjects"]>>(
    MOCK_FLYWHEEL_COMMANDS.listProjects,
    () => client.listProjects(),
  );
  register<{ projectId: string }, unknown>(MOCK_FLYWHEEL_COMMANDS.getBeads, ({ projectId }) =>
    client.getBeads(projectId),
  );
  register<{ projectId: string }, unknown>(MOCK_FLYWHEEL_COMMANDS.getTriage, ({ projectId }) =>
    client.getTriage(projectId),
  );
  register<{ projectId: string }, unknown>(
    MOCK_FLYWHEEL_COMMANDS.getSwarmSnapshot,
    ({ projectId }) => client.getSwarmSnapshot(projectId),
  );
  register<{ projectId: string }, unknown>(
    MOCK_FLYWHEEL_COMMANDS.getMailDump,
    ({ projectId }) => client.getMailDump(projectId),
  );
  register<{ projectId: string }, unknown>(
    MOCK_FLYWHEEL_COMMANDS.getReservations,
    ({ projectId }) => client.getReservations(projectId),
  );
  register<{ runId: string }, ReturnType<MockDaemonClient["getBuildLog"]>>(
    MOCK_FLYWHEEL_COMMANDS.getBuildLog,
    ({ runId }) => client.getBuildLog(runId),
  );
  register<{ agent: string }, ReturnType<MockDaemonClient["getPaneLog"]>>(
    MOCK_FLYWHEEL_COMMANDS.getPaneLog,
    ({ agent }) => client.getPaneLog(agent),
  );
}

function registerAuthCommands(register: RegisterMockCommand, client: MockDaemonClient): void {
  register<{ pairingToken: string }, ReturnType<MockDaemonClient["exchangePairingForBearer"]>>(
    MOCK_FLYWHEEL_COMMANDS.exchangePairingForBearer,
    (input) => client.exchangePairingForBearer(input),
  );
  register<{ bearerToken: string }, ReturnType<MockDaemonClient["issueWsToken"]>>(
    MOCK_FLYWHEEL_COMMANDS.issueWsToken,
    (input) => client.issueWsToken(input),
  );
}

function registerScenarioCommands(
  register: RegisterMockCommand,
  client: MockDaemonClient,
  replay: ReplayLifecycle,
): void {
  register<void, { scenarioId: string; cursors: Record<string, number> }>(
    MOCK_FLYWHEEL_COMMANDS.scenarioInfo,
    () => ({ scenarioId: client.scenarioId(), cursors: client.currentCursors() }),
  );
  register<{ scenarioId: string; isAbsolutePath?: boolean }, { scenarioId: string }>(
    MOCK_FLYWHEEL_COMMANDS.swapScenario,
    ({ scenarioId, isAbsolutePath }) => {
      const opts = isAbsolutePath !== undefined ? { isAbsolutePath } : {};
      return { scenarioId: replay.swapScenario(scenarioId, opts) };
    },
  );
  register<{ speed: ReplaySpeed }, { speed: ReplaySpeed }>(
    MOCK_FLYWHEEL_COMMANDS.setReplaySpeed,
    ({ speed }) => ({ speed: replay.setSpeed(speed) }),
  );
}

function createReplayLifecycle(
  client: MockDaemonClient,
  options: RegisterMockFlywheelClientOptions,
): ReplayLifecycle {
  let session: ReplaySession | null = null;
  let currentSpeed: ReplaySpeed = options.initialReplaySpeed ?? 1;

  function start(): void {
    if (!options.emitEvent || session !== null) return;
    session = client.subscribe({ speed: currentSpeed }, options.emitEvent);
  }

  function cancel(): void {
    session?.cancel();
    session = null;
  }

  return {
    get session() {
      return session;
    },
    start,
    cancel,
    swapScenario: (scenarioId, opts) => {
      cancel();
      client.swapScenario(scenarioId, opts);
      start();
      return client.scenarioId();
    },
    setSpeed: (speed) => {
      currentSpeed = speed;
      if (options.emitEvent) {
        cancel();
        start();
      }
      return currentSpeed;
    },
  };
}

function unregisterAll(teardowns: Array<() => void>): void {
  while (teardowns.length > 0) {
    const teardown = teardowns.pop();
    if (teardown) teardown();
  }
}
