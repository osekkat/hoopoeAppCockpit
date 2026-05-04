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
  // hp-58wp: Local-clone destructive action. Runs `git reset --hard @{u}`
  // followed by `git clean -fd` against the project's local clone in the
  // main process. The renderer NEVER supplies the clone path or argv —
  // main resolves the path from the project registry and invokes git
  // with explicit, non-interpolated argv (Guardrail 2). Audit fires on
  // every invocation regardless of outcome (Guardrail 10).
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

// ── 5. Internal command-id namespaces (main-process IpcRegistry) ──────────
//
// IpcRegistry hosts more than just the renderer-facing channels — it also
// hosts internal command IDs (e.g. "mock-flywheel.health", which is wired
// in MockFlywheelClient.ts). The allowlist enforced on `register()` is
// the union of the channel values above PLUS these internal namespaces.
// Each namespace is a string PREFIX; everything under it is allowed.
//
// Keep this list short. Each prefix is a security review burden.

export const INTERNAL_IPC_COMMAND_PREFIXES = [
  "mock-flywheel.",
  "internal.",
] as const;

export type InternalIpcCommandPrefix = (typeof INTERNAL_IPC_COMMAND_PREFIXES)[number];

/** Sane shape for the suffix after `mock-flywheel.` / `internal.`. Refuses
 *  empty suffix, control chars, whitespace, path-traversal segments, and
 *  unicode that isn't lowercase ASCII / digits / dots / hyphens. The fuzz
 *  harness (preload.fuzz.test.ts P5) found pre-fix `startsWith` was happy
 *  with `"mock-flywheel.\nfoo"`, `"internal.../../etc/passwd"`, and
 *  `"mock-flywheel."` (just the prefix) — all defense-in-depth holes
 *  closed here. */
const ALLOWED_INTERNAL_SUFFIX = /^[a-z][a-zA-Z0-9.-]*$/;

export function isAllowedRegistryCommandId(commandId: string): boolean {
  if (typeof commandId !== "string" || commandId.length === 0) return false;
  if (PRELOAD_IPC_CHANNEL_VALUES.has(commandId)) return true;
  for (const prefix of INTERNAL_IPC_COMMAND_PREFIXES) {
    if (!commandId.startsWith(prefix)) continue;
    const suffix = commandId.slice(prefix.length);
    if (suffix.length === 0) return false;
    if (suffix.includes("..")) return false; // path-traversal guard
    return ALLOWED_INTERNAL_SUFFIX.test(suffix);
  }
  return false;
}
