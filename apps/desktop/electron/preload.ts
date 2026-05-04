// Hoopoe-owned. Electron preload bridge — the ONLY surface the renderer
// uses to reach the main process. Per hp-rflj (§5.4):
//   - the renderer runs with `contextIsolation: true`, `sandbox: true`,
//     `nodeIntegration: false`. It cannot `require()` Node modules.
//   - the only route into main-process privileges is `window.hoopoe`,
//     exposed here via `contextBridge.exposeInMainWorld`.
//   - every method routes through `ipcRenderer.invoke` to a typed channel
//     name; main-process IPC handlers in `apps/desktop/src/main/IpcRegistry.ts`
//     validate input/output against `@hoopoe/schemas`.
//
// What is NOT exposed:
//   - direct fs / net / child_process access
//   - the bearer token, pairing token, WS-token, SSH passphrase
//   - arbitrary shell execution (Guardrail #2)
//   - `process`, `require`, or any Node global
//
// Adding a method to the bridge is a deliberate act: schema in
// `packages/schemas/preload-api.yaml` (lands in hp-r3i) → typed handler
// registered with IpcRegistry → method here → typed shape on
// `window.hoopoe`. Skipping any step fails the renderer-isolation lint
// (`scripts/rendererlint/check-renderer-isolation.ts`).

import { randomUUID } from "node:crypto";
import { contextBridge, ipcRenderer, type IpcRendererEvent } from "electron";
import {
  IpcContractError,
  PRELOAD_IPC_CHANNELS,
  isDaemonRequestMethod,
  isDaemonSubscribeTopic,
  type DaemonRequestMethod,
  type DaemonSubscribeTopic,
} from "../src/shared/ipc-contract.ts";
import type { BootstrapStepBridge } from "../src/shared/bootstrap-bridge.ts";

// ── Channel names ──────────────────────────────────────────────────────────
//
// hp-n5za: The single source of truth for channel/method/topic names is
// `apps/desktop/src/shared/ipc-contract.ts`. Both this preload and the
// main-side IpcRegistry consume the same constants; a runtime parity
// test fails the build if they ever drift.

const CHANNELS = PRELOAD_IPC_CHANNELS;

// Every method round-trips through ipcRenderer.invoke with a single args
// object. Concrete request/response validation happens in main-process
// IpcRegistry handlers; preload exposes unknown wire values until a caller
// narrows them with the generated schema types.
async function invoke(channel: string, args: unknown): Promise<unknown> {
  return await ipcRenderer.invoke(channel, args);
}

async function invokeVoid(channel: string, args: unknown): Promise<void> {
  await invoke(channel, args);
}

// Subscribe shape — main process pushes events on a channel; the listener
// fires with the typed payload until `unsubscribe()` is called.
function subscribe(
  channel: string,
  listener: (payload: unknown) => void,
): () => void {
  const wrapped = (_event: IpcRendererEvent, payload: unknown) => {
    listener(payload);
  };
  ipcRenderer.on(channel, wrapped);
  return () => {
    ipcRenderer.off(channel, wrapped);
  };
}

// ── window.hoopoe shape ───────────────────────────────────────────────────

export interface HoopoeBridge {
  readonly daemon: {
    /** hp-n5za: `method` is statically constrained to the allowlist in
     *  `src/shared/ipc-contract.ts`. Unknown methods are rejected at the
     *  preload boundary BEFORE main IPC sees them. */
    readonly request: (method: DaemonRequestMethod, body: unknown) => Promise<unknown>;
    readonly subscribe: (
      topic: DaemonSubscribeTopic,
      listener: (payload: unknown) => void,
    ) => () => void;
  };
  readonly settings: {
    readonly get: () => Promise<unknown>;
    readonly set: (partial: unknown) => Promise<void>;
    readonly watch: (listener: (next: unknown) => void) => () => void;
  };
  readonly keybindings: {
    readonly compile: (rules: unknown) => Promise<unknown>;
    readonly dispatch: (input: unknown) => Promise<unknown>;
  };
  readonly approvals: {
    readonly listPending: () => Promise<unknown>;
    readonly approve: (decision: unknown) => Promise<unknown>;
    readonly deny: (decision: unknown) => Promise<unknown>;
    readonly extend: (decision: unknown) => Promise<unknown>;
  };
  readonly files: {
    readonly openExternal: (url: string) => Promise<void>;
    readonly revealInFinder: (path: string) => Promise<void>;
    readonly ripgrep: (query: unknown) => Promise<unknown>;
  };
  readonly ssh: {
    readonly listKeys: () => Promise<unknown>;
    readonly generateKey: (input: unknown) => Promise<unknown>;
  };
  readonly clone: {
    /** hp-58wp/hp-hde4: Legacy discard channel. Main validates the
     *  projectId/clone-state, emits audit, and refuses because the
     *  desktop local clone is a read-only mirror. */
    readonly discardLocalChanges: (input: { projectId: string }) => Promise<unknown>;
    /** hp-5bhy: Reveal the project's local clone in Finder. Main
     *  resolves the path from the project registry; renderer carries
     *  only the projectId. */
    readonly revealInFinder: (input: { projectId: string }) => Promise<void>;
    /** hp-5bhy: Open the project's local clone in the user's default
     *  terminal app. Main resolves the path; safe argv. */
    readonly openInTerminal: (input: { projectId: string }) => Promise<void>;
    /** hp-5bhy: Persist a per-project cap override into clone-state.json.
     *  Pass `capsOverride: null` to clear the override and fall back to
     *  the global cap config. */
    readonly setCapOverride: (input: {
      projectId: string;
      capsOverride: { softCapBytes: number; hardCapBytes: number } | null;
    }) => Promise<unknown>;
  };
  readonly power: {
    /** hp-6gs4: Scoped Mac awake assertion while a ChatGPT Pro Oracle
     *  browser round is actively running. Main owns the OS mechanisms;
     *  renderer passes only round metadata. */
    readonly acquire: (input: {
      roundId: string;
      modelId?: string;
      oracleTopology?: "mac" | "vps";
      estimatedDurationMs?: number;
      reason?: string;
    }) => Promise<unknown>;
    readonly release: (input: {
      assertionId: string;
      reason?: "round_complete" | "round_failed" | "round_cancelled" | "watchdog_force_release" | "user_disabled" | "shutdown";
    }) => Promise<unknown>;
    readonly snapshot: () => Promise<unknown>;
  };
  /** hp-9z45 + hp-o90: Wizard bootstrap stream steps. Optional —
   *  daemon endpoints (POST /v1/bootstrap/preflight, /acfs/start,
   *  reconnect, verify-key) are still planned/stub routes. Preload
   *  will register handlers and add the corresponding entries to
   *  PRELOAD_IPC_CHANNELS once the daemon endpoints land; the renderer
   *  surface (`apps/desktop/src/renderer/wizard/StepBootstrapStream.tsx`)
   *  reads this property today and degrades to a "Waiting for the
   *  bootstrap preload bridge." message until then. */
  readonly bootstrap?: BootstrapStepBridge;
}

export const hoopoeBridge: HoopoeBridge = {
  daemon: {
    request: (method, body) => {
      // hp-n5za defense-in-depth: even though the type system constrains
      // `method` to DaemonRequestMethod, a non-TS renderer (e.g., a future
      // bundle from a third-party renderer plugin) could still pass an
      // arbitrary string. Runtime check refuses unknown methods before
      // they reach main.
      if (!isDaemonRequestMethod(method)) {
        return Promise.reject(new IpcContractError({ kind: "method", attempted: String(method) }));
      }
      return invoke(CHANNELS.daemonRequest, { method, body });
    },
    subscribe: (topic, listener) => {
      if (!isDaemonSubscribeTopic(topic)) {
        throw new IpcContractError({ kind: "topic", attempted: String(topic) });
      }
      // Subscription IDs are crypto-random (RFC 4122 v4 UUID) so a malicious
      // or buggy renderer can't predict/collide channel names. The `topic`
      // is included for diagnostics only — the actual channel is bound to
      // the random suffix, not the topic.
      const subscriptionId = randomUUID();
      const channel = `${CHANNELS.daemonSubscribe}.${subscriptionId}`;
      void invoke(CHANNELS.daemonSubscribe, { topic, subscriptionId });
      const unsubscribe = subscribe(channel, listener);
      return () => {
        unsubscribe();
        void invoke(CHANNELS.daemonUnsubscribe, { subscriptionId });
      };
    },
  },
  settings: {
    get: () => invoke(CHANNELS.settingsGet, {}),
    set: (partial) => invokeVoid(CHANNELS.settingsSet, partial),
    watch: (listener) => subscribe(CHANNELS.settingsWatch, listener),
  },
  keybindings: {
    compile: (rules) => invoke(CHANNELS.keybindingsCompile, rules),
    dispatch: (input) => invoke(CHANNELS.keybindingsDispatch, input),
  },
  approvals: {
    listPending: () => invoke(CHANNELS.approvalsList, {}),
    approve: (decision) => invoke(CHANNELS.approvalsApprove, decision),
    deny: (decision) => invoke(CHANNELS.approvalsDeny, decision),
    extend: (decision) => invoke(CHANNELS.approvalsExtend, decision),
  },
  files: {
    openExternal: (url) => invokeVoid(CHANNELS.filesOpenExternal, { url }),
    revealInFinder: (path) => invokeVoid(CHANNELS.filesRevealInFinder, { path }),
    ripgrep: (query) => invoke(CHANNELS.filesRipgrep, query),
  },
  ssh: {
    listKeys: () => invoke(CHANNELS.sshListKeys, {}),
    generateKey: (input) => invoke(CHANNELS.sshGenerateKey, input),
  },
  clone: {
    discardLocalChanges: (input) => invoke(CHANNELS.cloneDiscardLocalChanges, input),
    revealInFinder: (input) => invokeVoid(CHANNELS.cloneRevealInFinder, input),
    openInTerminal: (input) => invokeVoid(CHANNELS.cloneOpenInTerminal, input),
    setCapOverride: (input) => invoke(CHANNELS.cloneSetCapOverride, input),
  },
  power: {
    acquire: (input) => invoke(CHANNELS.powerAcquire, input),
    release: (input) => invoke(CHANNELS.powerRelease, input),
    snapshot: () => invoke(CHANNELS.powerSnapshot, {}),
  },
};

// ── Channel-name export for IpcRegistry handler registration ──────────────
//
// The main-process bootstrap reads this object and registers a handler for
// every channel via IpcRegistry. The set of legal channels is therefore
// the intersection of what's exposed here and what main has registered;
// the renderer cannot reach a channel that isn't in this list.

export const HOOPOE_PRELOAD_CHANNELS = CHANNELS;

// ── Install on window.hoopoe ──────────────────────────────────────────────

contextBridge.exposeInMainWorld("hoopoe", hoopoeBridge);

declare global {
  interface Window {
    readonly hoopoe: HoopoeBridge;
  }
}
