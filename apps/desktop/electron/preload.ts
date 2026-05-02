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

// ── Channel names ──────────────────────────────────────────────────────────
//
// One TS literal type per method so handler-side validation can pin down
// the channel string at compile time. The renderer consumes these via the
// generated `@hoopoe/schemas` client (Phase 2.5); this preload file only
// adapts the channel→method shape.

const CHANNELS = {
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
} as const satisfies Record<string, `hoopoe.${string}`>;

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
    readonly request: <I, O>(method: string, body: I) => Promise<O>;
    readonly subscribe: <P>(topic: string, listener: (payload: P) => void) => () => void;
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
}

export const hoopoeBridge: HoopoeBridge = {
  daemon: {
    request: (method, body) =>
      invoke(CHANNELS.daemonRequest, { method, body }),
    subscribe: (topic, listener) => {
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
