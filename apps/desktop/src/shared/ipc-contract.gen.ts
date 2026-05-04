/**
 * GENERATED — DO NOT EDIT.
 *
 * Source: packages/schemas/preload-api.yaml (schemaVersion 1).
 * Generator: packages/schemas/scripts/gen-preload-contract.ts.
 * Drift gate: packages/schemas/scripts/validate-preload-codegen.ts (CI).
 *
 * This file mirrors the renderer ↔ preload ↔ main IPC allowlist that lives
 * authoritatively in the YAML. The hand-rolled apps/desktop/src/shared/
 * ipc-contract.ts (hp-n5za hardening) pre-dates this generator; the parity
 * test in that directory enforces that the two cannot drift. When the
 * desktop owner switches the manual file to import from this one, the
 * manual file becomes a thin shim.
 *
 * Threat model: every entry here expands the renderer's reach. Adding one
 * is a security-relevant change. Review the bead in the YAML entry before
 * extending.
 */

export const DAEMON_REQUEST_METHODS = [
  "ping",
  "health",
  "version",
  "capabilities",
  "auth.exchangePairingForBearer",
  "auth.issueWsToken",
  "settings.get",
  "settings.set",
  "projects.list",
  "projects.create",
  "projects.import",
  "projects.clone",
  "projects.readiness",
  "beads.get",
  "triage.get",
  "swarm.snapshot",
  "mail.dump",
  "reservations.list",
  "build-log.get",
  "pane-log.get",
  "approvals.list",
  "approvals.approve",
  "approvals.deny",
  "approvals.extend",
  "tunnel.snapshot",
] as const;

export type DaemonRequestMethod = (typeof DAEMON_REQUEST_METHODS)[number];

const DAEMON_REQUEST_METHOD_SET: ReadonlySet<string> = new Set(DAEMON_REQUEST_METHODS);

export function isDaemonRequestMethod(value: unknown): value is DaemonRequestMethod {
  return typeof value === "string" && DAEMON_REQUEST_METHOD_SET.has(value);
}

export const DAEMON_SUBSCRIBE_TOPICS = [
  "events.swarm",
  "events.beads",
  "events.activity",
  "events.health",
  "events.tend",
  "events.approvals",
  "events.replay",
  "events.settings",
  "events.clone.dirty",
  "events.tunnel",
] as const;

export type DaemonSubscribeTopic = (typeof DAEMON_SUBSCRIBE_TOPICS)[number];

const DAEMON_SUBSCRIBE_TOPIC_SET: ReadonlySet<string> = new Set(DAEMON_SUBSCRIBE_TOPICS);

export function isDaemonSubscribeTopic(value: unknown): value is DaemonSubscribeTopic {
  return typeof value === "string" && DAEMON_SUBSCRIBE_TOPIC_SET.has(value);
}

export const PRELOAD_IPC_CHANNELS = {
  daemonRequest: "hoopoe.daemon.request",
  daemonSubscribe: "hoopoe.daemon.subscribe",
  daemonUnsubscribe: "hoopoe.daemon.unsubscribe",
  settingsGet: "hoopoe.settings.get",
  settingsSet: "hoopoe.settings.set",
  settingsWatch: "hoopoe.settings.watch",
  keybindingsCompile: "hoopoe.keybindings.compile",
  keybindingsDispatch: "hoopoe.keybindings.dispatch",
  approvalsList: "hoopoe.approvals.list-pending",
  approvalsApprove: "hoopoe.approvals.approve",
  approvalsDeny: "hoopoe.approvals.deny",
  approvalsExtend: "hoopoe.approvals.extend",
  filesOpenExternal: "hoopoe.files.open-external",
  filesRevealInFinder: "hoopoe.files.reveal-in-finder",
  filesRipgrep: "hoopoe.files.ripgrep",
  sshListKeys: "hoopoe.ssh.listKeys",
  sshGenerateKey: "hoopoe.ssh.generateKey",
  cloneDiscardLocalChanges: "hoopoe.clone.discard-local-changes",
  cloneRevealInFinder: "hoopoe.clone.reveal-in-finder",
  cloneOpenInTerminal: "hoopoe.clone.open-in-terminal",
  cloneSetCapOverride: "hoopoe.clone.set-cap-override",
  powerAcquire: "hoopoe.power.acquire",
  powerRelease: "hoopoe.power.release",
  powerSnapshot: "hoopoe.power.snapshot",
} as const satisfies Record<string, `hoopoe.${string}`>;

export type PreloadIpcChannelKey = keyof typeof PRELOAD_IPC_CHANNELS;
export type PreloadIpcChannelValue =
  (typeof PRELOAD_IPC_CHANNELS)[PreloadIpcChannelKey];

const PRELOAD_IPC_CHANNEL_VALUES: ReadonlySet<string> = new Set(
  Object.values(PRELOAD_IPC_CHANNELS),
);

export function isPreloadIpcChannel(value: unknown): value is PreloadIpcChannelValue {
  return typeof value === "string" && PRELOAD_IPC_CHANNEL_VALUES.has(value);
}

export const MOCK_FLYWHEEL_COMMANDS = {
  health: "mock-flywheel.health",
  version: "mock-flywheel.version",
  capabilities: "mock-flywheel.capabilities",
  listProjects: "mock-flywheel.projects.list",
  getBeads: "mock-flywheel.beads.get",
  getTriage: "mock-flywheel.triage.get",
  getSwarmSnapshot: "mock-flywheel.swarm.snapshot",
  getMailDump: "mock-flywheel.mail.dump",
  getReservations: "mock-flywheel.reservations.list",
  getBuildLog: "mock-flywheel.build-log.get",
  getPaneLog: "mock-flywheel.pane-log.get",
  exchangePairingForBearer: "mock-flywheel.auth.exchangePairing",
  issueWsToken: "mock-flywheel.auth.issueWsToken",
  scenarioInfo: "mock-flywheel.scenario.info",
  swapScenario: "mock-flywheel.scenario.swap",
  setReplaySpeed: "mock-flywheel.replay.setSpeed",
} as const satisfies Record<string, `mock-flywheel.${string}`>;

export type MockFlywheelCommandId =
  (typeof MOCK_FLYWHEEL_COMMANDS)[keyof typeof MOCK_FLYWHEEL_COMMANDS];

export const INTERNAL_IPC_COMMANDS = {
  schemasSmokeProject: "internal.schemas-smoke.project",
  schemasSmokeCompatibility: "internal.schemas-smoke.compatibility",
  swarmSendMarchingOrders: "internal.swarm-send-marching-orders",
  approvalConfirm: "internal.approval-confirm",
  testAlways: "internal.test.always",
  testNeedsAuth: "internal.test.needs-auth",
  testDuplicate: "internal.test.duplicate",
  testGated: "internal.test.gated",
  testHealthy: "internal.test.healthy",
  testShadow: "internal.test.shadow",
  testUnregistered: "internal.test.unregistered",
  fuzzEcho: "internal.fuzz-echo",
} as const satisfies Record<string, `internal.${string}`>;

export type InternalIpcCommandId =
  | (typeof INTERNAL_IPC_COMMANDS)[keyof typeof INTERNAL_IPC_COMMANDS]
  | MockFlywheelCommandId;

const INTERNAL_IPC_COMMAND_VALUES: ReadonlySet<string> = new Set([
  ...Object.values(INTERNAL_IPC_COMMANDS),
  ...Object.values(MOCK_FLYWHEEL_COMMANDS),
]);

export function isInternalIpcCommand(value: unknown): value is InternalIpcCommandId {
  return typeof value === "string" && INTERNAL_IPC_COMMAND_VALUES.has(value);
}
