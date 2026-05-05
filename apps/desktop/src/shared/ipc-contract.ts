// hp-n5za Phase 1.5 — single-source-of-truth IPC contract.
//
// Renderer-facing IPC surface is a CLOSED set: every method/topic the
// renderer can drive is enumerated here, and both the preload boundary
// (apps/desktop/electron/preload.ts) and the main-side IpcRegistry
// (apps/desktop/src/main/IpcRegistry.ts) consume this file. Disagreement
// between the two surfaces is caught by a runtime parity test
// (apps/desktop/src/shared/ipc-contract.test.ts) and surfaces as a build
// failure rather than a security gap.
//
// THIS FILE IS A MANUAL CONST UNTIL hp-r3i (packages/schemas / OpenAPI
// codegen) lands. When hp-r3i ships, the literal arrays here are
// regenerated from packages/schemas/preload-api.yaml; the parity test
// stays as-is and just verifies the generated set matches what main
// has actually registered.
//
// Cross-references:
//   - review-findings.md "Renderer preload — [HIGH] no allowlist gate on
//     daemon.request" / "...daemon.subscribe" / "[HIGH] IPC dispatch
//     type-safety is illusory pending hp-r3i schemas"
//   - bead hp-rflj (renderer hardening parent — preload typed API)
//   - bead hp-r3i  (Phase 2.5 schemas — replaces this manual list)
//
// Threat model: any string the renderer constructs reaches main process.
// Without this allowlist, a buggy or malicious renderer could drive
// daemon-internal methods (audit, redaction, settings hot-reload) the
// renderer has no business observing or invoking. The preload boundary
// is enforced first; main-side IpcRegistry enforces again as
// defense-in-depth (in case preload is ever bypassed, e.g. via a
// future renderer change that imports `electron` directly).

// ── 1. Daemon RPC methods (renderer ↔ preload ↔ main ↔ daemon) ────────────
//
// These are method NAMES carried in the body of `hoopoe.daemon.request`
// IPC calls. Each one corresponds to a single daemon HTTP/gRPC method.
// Adding one is a SECURITY-RELEVANT change — every entry expands the
// surface area the renderer can reach.

export const DAEMON_REQUEST_METHODS = [
  // Health / capabilities — read-only.
  "ping",
  "health",
  "version",
  "capabilities",
  // Auth handshake — required for first-run pairing + WS-token issuance.
  "auth.exchangePairingForBearer",
  "auth.issueWsToken",
  // Settings RPC — read tier-resolved + write user/project tier.
  "settings.get",
  "settings.set",
  // Project / bead read surface.
  "projects.list",
  // hp-ilt: Project lifecycle. Daemon-side handlers persist the new project
  // in the SQLite registry and call the lifecycle helpers in
  // `apps/desktop/electron/projects/` (shared between daemon + main).
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
  // Approvals — the renderer-driven decision surface (hp-v0g).
  "approvals.list",
  "approvals.approve",
  "approvals.deny",
  "approvals.extend",
  // hp-m79e: ConnectionManager FSM snapshot for the renderer's
  // ConnectionStatus pill + ToolHealthPill VPS dot. Lives on top of
  // the hp-e7k FSM via the hp-fkov orchestrator.
  "tunnel.snapshot",
] as const;

export type DaemonRequestMethod = (typeof DAEMON_REQUEST_METHODS)[number];

const DAEMON_REQUEST_METHOD_SET: ReadonlySet<string> = new Set(DAEMON_REQUEST_METHODS);

export function isDaemonRequestMethod(value: unknown): value is DaemonRequestMethod {
  return typeof value === "string" && DAEMON_REQUEST_METHOD_SET.has(value);
}

// ── 2. Daemon subscription topics (renderer ↔ preload ↔ main ↔ daemon WS) ─
//
// Topics the renderer is ENTITLED to observe. Internal-only topics
// (audit_log, redaction_trace, settings.tier-merge) are intentionally
// absent — those flow only inside main. Adding one expands the
// renderer's read surface.

export const DAEMON_SUBSCRIBE_TOPICS = [
  // Activity / Swarm event stream.
  "events.swarm",
  "events.beads",
  "events.activity",
  "events.health",
  "events.tend",
  "events.approvals",
  // Mock Flywheel replay events (hp-o74).
  "events.replay",
  // Settings change notifications (resolved tree only — never raw
  // tier deltas, which the renderer can't see anyway via the
  // SettingsBridge subscriber surface).
  "events.settings",
  // hp-ndx5: Per-project local-clone dirty state (modified/untracked/
  // ahead/behind counts) emitted by the desktop main-process clone
  // watcher whenever a debounced fs event triggers a fresh probe.
  "events.clone.dirty",
  // hp-m79e: ConnectionManager FSM transitions emitted by the
  // tunnel orchestrator (hp-fkov) so the ConnectionStatus pill
  // renders live state without polling.
  "events.tunnel",
] as const;

export type DaemonSubscribeTopic = (typeof DAEMON_SUBSCRIBE_TOPICS)[number];

const DAEMON_SUBSCRIBE_TOPIC_SET: ReadonlySet<string> = new Set(DAEMON_SUBSCRIBE_TOPICS);

export function isDaemonSubscribeTopic(value: unknown): value is DaemonSubscribeTopic {
  return typeof value === "string" && DAEMON_SUBSCRIBE_TOPIC_SET.has(value);
}

// ── 3. Preload IPC channel allowlist (preload ↔ main) ─────────────────────
//
// One channel per direct preload method (NOT the daemon-RPC methods,
// which all multiplex over `hoopoe.daemon.request`). The preload uses
// these channel names to invoke the IpcRegistry; main uses them to
// register handlers. They are kept here so the parity test catches drift.

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
  // hp-pl8h: SSH key wizard step. listKeys → reads ~/.ssh/ for *.pub
  // entries; generateKey → shell-out to `ssh-keygen -t ed25519` with
  // EXPLICIT argv (no shell, no user-controlled path). The renderer
  // supplies a runId; main derives the key file path from it so a
  // malicious renderer can never write outside ~/.ssh/ or inject
  // ssh-keygen flags.
  sshListKeys: "hoopoe.ssh.listKeys",
  sshGenerateKey: "hoopoe.ssh.generateKey",
  // hp-58wp/hp-hde4: Legacy local-clone discard channel. Guardrail 3
  // forbids mutating the desktop read-only mirror, so main now validates
  // projectId/clone-state, emits audit, and refuses with an explicit
  // read-only error. Git writes must run on the VPS clone via daemon RPCs.
  cloneDiscardLocalChanges: "hoopoe.clone.discard-local-changes",
  // hp-5bhy: Three more clone-action channels backing CloneSettingsCard
  // (hp-1fd1). Same safety posture as cloneDiscardLocalChanges — the
  // renderer carries only the projectId; main resolves the clone path
  // and audits every invocation regardless of outcome.
  cloneRevealInFinder: "hoopoe.clone.reveal-in-finder",
  cloneOpenInTerminal: "hoopoe.clone.open-in-terminal",
  cloneSetCapOverride: "hoopoe.clone.set-cap-override",
  // hp-6gs4: ChatGPT Pro Oracle rounds run through the user's Mac-side
  // browser session in v1. These channels let the Plan workspace acquire
  // and release a scoped main-process power assertion while a Pro round is
  // actually running. Renderer input carries only round metadata; main owns
  // the OS-level mechanisms (powerSaveBlocker / NSProcessInfo / caffeinate).
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

export type EmptyObject = Record<string, never>;

// ── 3a. Direct preload channel payload types ──────────────────────────────
//
// hp-3zc: every invoke-style direct preload channel exposed through
// `window.hoopoe.<group>.<method>` carries a typed input + output that the
// main-side IpcRegistry handler is required to validate at the boundary
// (`apps/desktop/src/main/IpcRegistry.ts:182-194` rejects renderer-facing
// registrations without `validateInput`/`validateOutput`).
//
// The interfaces below are the SOURCE OF TRUTH for the contract type names
// listed in `PRELOAD_IPC_CHANNEL_CONTRACTS` below. Validators registered by
// each domain owner (settings/keybindings/approvals/files/ssh/clone) must
// narrow the untrusted renderer payload to one of these shapes — concrete
// per-domain wiring lands in the follow-up beads filed alongside this
// commit (hp-3zc-{settings,keybindings,approvals,files,ssh,clone}).
//
// Where a domain already exports a richer type elsewhere
// (e.g. `SshKeyService.ListedSshKey`, `keybindings/types.KeybindingRule`),
// the per-domain follow-up either makes that file re-import from here OR
// keeps its own type and structurally satisfies the contract. The contract
// names here are the boundary; nothing else may cross.

// Power assertion (hp-6gs4 / hp-ey3) ----------------------------------------

export type PowerAssertionLevel = "display" | "app-suspension" | "system";

export type PowerAssertionMechanism = "powersaveblocker" | "nsprocessinfo" | "caffeinate";

export type PowerAssertionReleaseReason =
  | "round_complete"
  | "round_failed"
  | "round_cancelled"
  | "watchdog_force_release"
  | "user_disabled"
  | "shutdown";

export interface PowerAssertionAcquireInput {
  readonly roundId: string;
  readonly modelId?: string;
  readonly oracleTopology?: "mac" | "vps";
  readonly estimatedDurationMs?: number;
  readonly reason?: string;
}

export interface PowerAssertionReleaseInput {
  readonly assertionId: string;
  readonly reason?: PowerAssertionReleaseReason;
}

export interface PowerAssertionSnapshot {
  readonly active: boolean;
  readonly assertionId: string | null;
  readonly mechanism: PowerAssertionMechanism | null;
  readonly level: PowerAssertionLevel | null;
  readonly ownerRoundIds: readonly string[];
  readonly heldCount: number;
  readonly acquiredAt: string | null;
}

// Settings (settingsGet / settingsSet) -------------------------------------

export type SettingsScope = "user" | "project" | "merged";
export type SettingsTier = "user" | "project";

export interface SettingsGetInput {
  readonly scope?: SettingsScope;
}

/** Tier-resolved settings tree. Keys/values are intentionally open-ended at
 *  the boundary; the renderer narrows with `HoopoeSettings` from
 *  `apps/desktop/src/main/SettingsBridge.ts`. */
export type SettingsGetOutput = Readonly<Record<string, unknown>>;

export interface SettingsSetInput {
  readonly tier: SettingsTier;
  readonly key: string;
  readonly value: unknown;
}

export interface SettingsSetOutput {
  readonly ok: boolean;
}

// Keybindings (keybindingsCompile / keybindingsDispatch) -------------------

export interface KeybindingsCompileInput {
  /** Unparsed rule list as authored by the user. The compile handler
   *  delegates to `apps/desktop/src/vendored/t3code/keybindings/parser.ts`
   *  which narrows + validates each rule. */
  readonly rules: ReadonlyArray<unknown>;
}

export interface KeybindingsCompileError {
  readonly ruleIndex: number;
  readonly code: string;
  readonly message: string;
}

export interface KeybindingsCompileOutput {
  readonly resolved: ReadonlyArray<unknown>;
  readonly errors: ReadonlyArray<KeybindingsCompileError>;
}

export interface KeybindingsDispatchInput {
  readonly key: string;
  readonly modifiers?: ReadonlyArray<string>;
  readonly contextKeys?: ReadonlyArray<string>;
}

export interface KeybindingsDispatchOutput {
  readonly handled: boolean;
  readonly commandId?: string;
  readonly error?: { readonly code: string; readonly message: string };
}

// Approvals (approvalsList / approvalsApprove / approvalsDeny / approvalsExtend) -

export interface ApprovalsListInput {
  readonly projectId: string;
}

export type ApprovalStatus = "pending" | "approved" | "denied" | "expired";

export interface ApprovalSummary {
  readonly approvalId: string;
  readonly projectId: string;
  readonly status: ApprovalStatus;
  readonly createdAt: string;
  readonly expiresAt?: string;
  readonly note?: string;
}

export interface ApprovalsListOutput {
  readonly items: ReadonlyArray<ApprovalSummary>;
}

export interface ApprovalsDecisionInput {
  readonly projectId: string;
  readonly approvalId: string;
  readonly note?: string;
}

export type ApprovalsDecisionOutput = ApprovalSummary;

export interface ApprovalsExtendInput extends ApprovalsDecisionInput {
  /** Bounded by the daemon RPC contract (`approvals.extend`: 60–86400). */
  readonly additionalSeconds: number;
}

// Files (filesOpenExternal / filesRevealInFinder / filesRipgrep) -----------

export interface FilesOpenExternalInput {
  readonly url: string;
}

export interface FilesRevealInFinderInput {
  readonly path: string;
}

export interface FilesRipgrepInput {
  readonly projectId: string;
  readonly query: string;
  readonly globs?: ReadonlyArray<string>;
  readonly caseSensitive?: boolean;
  readonly maxMatches?: number;
}

export interface FilesRipgrepHit {
  readonly path: string;
  readonly line: number;
  readonly column?: number;
  readonly preview: string;
}

export interface FilesRipgrepOutput {
  readonly hits: ReadonlyArray<FilesRipgrepHit>;
  readonly truncated: boolean;
}

// SSH (sshListKeys / sshGenerateKey) ---------------------------------------
//
// hp-pl8h: the renderer NEVER supplies a file path or comment string
// verbatim — main derives both from the runId so a malicious renderer
// cannot write outside `~/.ssh/` or inject ssh-keygen flags. The contract
// reflects that: input is `runId` + optional comment, NOT a free-form
// path/argv.

export type SshKeyAlgorithm = "ed25519" | "rsa" | "ecdsa" | "dsa";

export interface SshKeyDescriptor {
  readonly name: string;
  readonly path: string;
  readonly algorithm: SshKeyAlgorithm;
  readonly fingerprint: string;
}

export interface SshListKeysOutput {
  readonly keys: ReadonlyArray<SshKeyDescriptor>;
}

export interface SshGenerateKeyInput {
  readonly runId: string;
  readonly comment?: string;
}

export interface SshGenerateKeyOutput {
  readonly key: SshKeyDescriptor;
}

// Clone (cloneDiscardLocalChanges / cloneRevealInFinder / cloneOpenInTerminal /
//        cloneSetCapOverride) -------------------------------------------------
//
// hp-58wp/hp-5bhy: the renderer carries ONLY the projectId; main resolves
// the local-clone path from the project registry. Audit fires on every
// invocation regardless of outcome (Guardrail 10).

export interface CloneProjectIdInput {
  readonly projectId: string;
}

export interface CloneCapsOverride {
  readonly softCapBytes: number;
  readonly hardCapBytes: number;
}

export interface CloneSetCapOverrideInput {
  readonly projectId: string;
  /** `null` clears the per-project override and falls back to the global
   *  cap config. */
  readonly capsOverride: CloneCapsOverride | null;
}

export interface CloneSetCapOverrideOutput {
  readonly projectId: string;
  readonly capsOverride: CloneCapsOverride | null;
}

export type CloneDiscardOutcome = "refused-readonly" | "discarded";

export interface CloneDiscardOutput {
  readonly projectId: string;
  /** Guardrail 3 makes "refused-readonly" the steady-state outcome — the
   *  desktop local clone is a sync-driven mirror, not a write target. */
  readonly outcome: CloneDiscardOutcome;
  readonly auditId: string;
}

// ── Channel ↔ contract registry ───────────────────────────────────────────

export const PRELOAD_IPC_CHANNEL_CONTRACTS = {
  settingsGet: {
    channel: PRELOAD_IPC_CHANNELS.settingsGet,
    input: "SettingsGetInput",
    output: "SettingsGetOutput",
  },
  settingsSet: {
    channel: PRELOAD_IPC_CHANNELS.settingsSet,
    input: "SettingsSetInput",
    output: "SettingsSetOutput",
  },
  keybindingsCompile: {
    channel: PRELOAD_IPC_CHANNELS.keybindingsCompile,
    input: "KeybindingsCompileInput",
    output: "KeybindingsCompileOutput",
  },
  keybindingsDispatch: {
    channel: PRELOAD_IPC_CHANNELS.keybindingsDispatch,
    input: "KeybindingsDispatchInput",
    output: "KeybindingsDispatchOutput",
  },
  approvalsList: {
    channel: PRELOAD_IPC_CHANNELS.approvalsList,
    input: "ApprovalsListInput",
    output: "ApprovalsListOutput",
  },
  approvalsApprove: {
    channel: PRELOAD_IPC_CHANNELS.approvalsApprove,
    input: "ApprovalsDecisionInput",
    output: "ApprovalsDecisionOutput",
  },
  approvalsDeny: {
    channel: PRELOAD_IPC_CHANNELS.approvalsDeny,
    input: "ApprovalsDecisionInput",
    output: "ApprovalsDecisionOutput",
  },
  approvalsExtend: {
    channel: PRELOAD_IPC_CHANNELS.approvalsExtend,
    input: "ApprovalsExtendInput",
    output: "ApprovalsDecisionOutput",
  },
  filesOpenExternal: {
    channel: PRELOAD_IPC_CHANNELS.filesOpenExternal,
    input: "FilesOpenExternalInput",
    output: "EmptyObject",
  },
  filesRevealInFinder: {
    channel: PRELOAD_IPC_CHANNELS.filesRevealInFinder,
    input: "FilesRevealInFinderInput",
    output: "EmptyObject",
  },
  filesRipgrep: {
    channel: PRELOAD_IPC_CHANNELS.filesRipgrep,
    input: "FilesRipgrepInput",
    output: "FilesRipgrepOutput",
  },
  sshListKeys: {
    channel: PRELOAD_IPC_CHANNELS.sshListKeys,
    input: "EmptyObject",
    output: "SshListKeysOutput",
  },
  sshGenerateKey: {
    channel: PRELOAD_IPC_CHANNELS.sshGenerateKey,
    input: "SshGenerateKeyInput",
    output: "SshGenerateKeyOutput",
  },
  cloneDiscardLocalChanges: {
    channel: PRELOAD_IPC_CHANNELS.cloneDiscardLocalChanges,
    input: "CloneProjectIdInput",
    output: "CloneDiscardOutput",
  },
  cloneRevealInFinder: {
    channel: PRELOAD_IPC_CHANNELS.cloneRevealInFinder,
    input: "CloneProjectIdInput",
    output: "EmptyObject",
  },
  cloneOpenInTerminal: {
    channel: PRELOAD_IPC_CHANNELS.cloneOpenInTerminal,
    input: "CloneProjectIdInput",
    output: "EmptyObject",
  },
  cloneSetCapOverride: {
    channel: PRELOAD_IPC_CHANNELS.cloneSetCapOverride,
    input: "CloneSetCapOverrideInput",
    output: "CloneSetCapOverrideOutput",
  },
  powerAcquire: {
    channel: PRELOAD_IPC_CHANNELS.powerAcquire,
    input: "PowerAssertionAcquireInput",
    output: "PowerAssertionSnapshot",
  },
  powerRelease: {
    channel: PRELOAD_IPC_CHANNELS.powerRelease,
    input: "PowerAssertionReleaseInput",
    output: "PowerAssertionSnapshot",
  },
  powerSnapshot: {
    channel: PRELOAD_IPC_CHANNELS.powerSnapshot,
    input: "EmptyObject",
    output: "PowerAssertionSnapshot",
  },
} as const satisfies Record<
  string,
  {
    readonly channel: PreloadIpcChannelValue;
    readonly input: string;
    readonly output: string;
  }
>;

/** Channels that are excluded from the direct contract section because they
 *  are not invoke-style request/response. The codegen + parity tests use
 *  this set to verify completeness of `PRELOAD_IPC_CHANNEL_CONTRACTS`. */
export const PRELOAD_CHANNELS_WITHOUT_DIRECT_CONTRACT = [
  "daemonRequest",
  "daemonSubscribe",
  "daemonUnsubscribe",
  "settingsWatch",
] as const satisfies ReadonlyArray<PreloadIpcChannelKey>;

const PRELOAD_CHANNELS_WITHOUT_DIRECT_CONTRACT_SET: ReadonlySet<string> = new Set(
  PRELOAD_CHANNELS_WITHOUT_DIRECT_CONTRACT,
);

/** True when the given channel key MUST appear in
 *  `PRELOAD_IPC_CHANNEL_CONTRACTS`. */
export function preloadChannelRequiresDirectContract(
  channelKey: PreloadIpcChannelKey,
): boolean {
  return !PRELOAD_CHANNELS_WITHOUT_DIRECT_CONTRACT_SET.has(channelKey);
}

// ── 4. IpcContract error type ─────────────────────────────────────────────
//
// Thrown by both preload (renderer-side) and IpcRegistry (main-side) when
// an unknown method/topic/channel is requested. Distinguishable from
// network/RPC errors so the renderer can surface "preload contract
// violation — file a bug" rather than "daemon unreachable."

export class IpcContractError extends Error {
  readonly kind: "method" | "topic" | "channel";
  readonly attempted: string;
  constructor(input: { kind: IpcContractError["kind"]; attempted: string }) {
    super(
      `IPC contract violation: unknown ${input.kind} ${JSON.stringify(input.attempted)}. ` +
        "If this is a legitimate addition, update apps/desktop/src/shared/ipc-contract.ts.",
    );
    this.name = "IpcContractError";
    this.kind = input.kind;
    this.attempted = input.attempted;
  }
}

// ── 5. Internal command-id manifest (main-process IpcRegistry) ────────────
//
// IpcRegistry hosts more than just the renderer-facing channels. Every
// main-only command ID must be listed here so adding a new privileged
// internal surface is a deliberate contract edit instead of a prefix match.
//
// Keep this list short. Each entry is a security review burden.

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
  issueWsSession: "mock-flywheel.auth.issue-ws-session",
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

export function isAllowedRegistryCommandId(commandId: string): boolean {
  if (typeof commandId !== "string" || commandId.length === 0) return false;
  if (PRELOAD_IPC_CHANNEL_VALUES.has(commandId)) return true;
  return INTERNAL_IPC_COMMAND_VALUES.has(commandId);
}
