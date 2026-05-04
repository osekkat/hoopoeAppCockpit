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

// ── Channel names ──────────────────────────────────────────────────────────
//
// hp-n5za: The single source of truth for channel/method/topic names is
// `apps/desktop/src/shared/ipc-contract.ts`. Both this preload and the
// main-side IpcRegistry consume the same constants; a runtime parity
// test fails the build if they ever drift.

const CHANNELS = PRELOAD_IPC_CHANNELS;

// Generic invoke shape — every method round-trips through ipcRenderer.invoke
// with a single args object and returns whatever the main-process handler
// resolves to. The renderer-side typed shapes come from `@hoopoe/schemas`.
async function invoke<I, O>(channel: string, args: I): Promise<O> {
  return (await ipcRenderer.invoke(channel, args)) as O;
}

// Subscribe shape — main process pushes events on a channel; the listener
// fires with the typed payload until `unsubscribe()` is called.
function subscribe<P>(
  channel: string,
  listener: (payload: P) => void,
): () => void {
  const wrapped = (_event: IpcRendererEvent, payload: P) => {
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
    readonly request: <I, O>(method: DaemonRequestMethod, body: I) => Promise<O>;
    readonly subscribe: <P>(
      topic: DaemonSubscribeTopic,
      listener: (payload: P) => void,
    ) => () => void;
  };
  readonly settings: {
    readonly get: <T>() => Promise<T>;
    readonly set: <T>(partial: T) => Promise<void>;
    readonly watch: <T>(listener: (next: T) => void) => () => void;
  };
  readonly keybindings: {
    readonly compile: <I, O>(rules: I) => Promise<O>;
    readonly dispatch: <I, O>(input: I) => Promise<O>;
  };
  readonly approvals: {
    readonly listPending: <O>() => Promise<O>;
    readonly approve: <I, O>(decision: I) => Promise<O>;
    readonly deny: <I, O>(decision: I) => Promise<O>;
    readonly extend: <I, O>(decision: I) => Promise<O>;
  };
  readonly files: {
    readonly openExternal: (url: string) => Promise<void>;
    readonly revealInFinder: (path: string) => Promise<void>;
    readonly ripgrep: <I, O>(query: I) => Promise<O>;
  };
  readonly ssh: {
    readonly listKeys: <O>() => Promise<O>;
    readonly generateKey: <I, O>(input: I) => Promise<O>;
  };
  readonly clone: {
    /** hp-58wp: Discard local changes against a project's local clone.
     *  Runs `git reset --hard @{u}` + `git clean -fd` in main with safe
     *  argv. The renderer never supplies a path; only the projectId is
     *  carried across the IPC boundary. */
    readonly discardLocalChanges: <O>(input: { projectId: string }) => Promise<O>;
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
    readonly setCapOverride: <O>(input: {
      projectId: string;
      capsOverride: { softCapBytes: number; hardCapBytes: number } | null;
    }) => Promise<O>;
  };
  readonly power: {
    /** hp-6gs4: Scoped Mac awake assertion while a ChatGPT Pro Oracle
     *  browser round is actively running. Main owns the OS mechanisms;
     *  renderer passes only round metadata. */
    readonly acquire: <O>(input: {
      roundId: string;
      modelId?: string;
      oracleTopology?: "mac" | "vps";
      estimatedDurationMs?: number;
      reason?: string;
    }) => Promise<O>;
    readonly release: <O>(input: {
      assertionId: string;
      reason?: "round_complete" | "round_failed" | "round_cancelled" | "watchdog_force_release" | "user_disabled" | "shutdown";
    }) => Promise<O>;
    readonly snapshot: <O>() => Promise<O>;
  };
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
    set: (partial) => invoke(CHANNELS.settingsSet, partial),
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
    openExternal: (url) => invoke(CHANNELS.filesOpenExternal, { url }),
    revealInFinder: (path) => invoke(CHANNELS.filesRevealInFinder, { path }),
    ripgrep: (query) => invoke(CHANNELS.filesRipgrep, query),
  },
  ssh: {
    listKeys: () => invoke(CHANNELS.sshListKeys, {}),
    generateKey: (input) => invoke(CHANNELS.sshGenerateKey, input),
  },
  clone: {
    discardLocalChanges: (input) => invoke(CHANNELS.cloneDiscardLocalChanges, input),
    revealInFinder: (input) => invoke(CHANNELS.cloneRevealInFinder, input),
    openInTerminal: (input) => invoke(CHANNELS.cloneOpenInTerminal, input),
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
