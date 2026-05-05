// Hoopoe-owned. Factory for the `daemon` block of the preload bridge —
// extracted from `apps/desktop/electron/preload.ts` so the subscribe race
// + error-surfacing semantics from hp-7bj1 can be unit-tested without
// spawning Electron's contextBridge.
//
// hp-n5za still owns the channel/method/topic allowlist via
// `./ipc-contract.ts`; this file only wires the renderer-facing call
// shapes onto `invoke` + `subscribe` primitives.

import {
  IpcContractError,
  PRELOAD_IPC_CHANNELS,
  isDaemonRequestMethod,
  isDaemonSubscribeTopic,
  type DaemonRequestMethod,
  type DaemonSubscribeTopic,
} from "./ipc-contract.ts";

export type DaemonInvoke = (channel: string, args: unknown) => Promise<unknown>;

/** Renderer-side listener registration: install `listener` for `channel`,
 *  return a function that removes the listener. The factory does not own
 *  the underlying `ipcRenderer.on`/`off` calls — preload supplies them. */
export type DaemonRendererSubscribe = (
  channel: string,
  listener: (payload: unknown) => void,
) => () => void;

export type DaemonSubscriptionIdProvider = () => string;

/** Bag of error reasons surfaced through `DaemonSubscribeOptions.onError`.
 *  Surfacing via callback (rather than throwing) keeps the public
 *  `subscribe` signature synchronous for existing callers — hp-7bj1's
 *  acceptance criterion was to make subscribe/unsubscribe failures
 *  observable, not to force every caller to handle them. */
export type DaemonSubscribeErrorCode = "subscribe_failed" | "unsubscribe_failed";

export class DaemonSubscribeError extends Error {
  readonly code: DaemonSubscribeErrorCode;
  override readonly cause: unknown;

  constructor(code: DaemonSubscribeErrorCode, cause: unknown) {
    const message = cause instanceof Error ? cause.message : String(cause);
    super(`daemon ${code === "subscribe_failed" ? "subscribe" : "unsubscribe"} failed: ${message}`);
    this.name = "DaemonSubscribeError";
    this.code = code;
    this.cause = cause;
  }
}

export interface DaemonSubscribeOptions {
  /** Invoked when main-process registration (or teardown) of the
   *  subscription rejects. Without this hook the bridge previously
   *  swallowed the rejection (`void invoke(...)`), so the renderer
   *  thought it was subscribed even when main refused. */
  readonly onError?: (error: DaemonSubscribeError) => void;
}

export interface DaemonBridge {
  readonly request: (method: DaemonRequestMethod, body: unknown) => Promise<unknown>;
  readonly subscribe: (
    topic: DaemonSubscribeTopic,
    listener: (payload: unknown) => void,
    options?: DaemonSubscribeOptions,
  ) => () => void;
}

export interface CreateDaemonBridgeOptions {
  /** Source of per-subscription IDs. Production uses
   *  `crypto.randomUUID`; tests inject a deterministic counter. */
  readonly subscriptionId?: DaemonSubscriptionIdProvider;
}

export function createDaemonBridge(
  invoke: DaemonInvoke,
  rendererSubscribe: DaemonRendererSubscribe,
  options: CreateDaemonBridgeOptions = {},
): DaemonBridge {
  const nextSubscriptionId = options.subscriptionId ?? throwingDefaultProvider;
  return {
    request: (method, body) => {
      // hp-n5za defense-in-depth: refuse unknown methods before reaching
      // main IPC. Static types don't bind a non-TS renderer.
      if (!isDaemonRequestMethod(method)) {
        return Promise.reject(
          new IpcContractError({ kind: "method", attempted: String(method) }),
        );
      }
      return invoke(PRELOAD_IPC_CHANNELS.daemonRequest, { method, body });
    },
    subscribe: (topic, listener, opts) => {
      if (!isDaemonSubscribeTopic(topic)) {
        throw new IpcContractError({ kind: "topic", attempted: String(topic) });
      }

      const subscriptionId = nextSubscriptionId();
      const channel = `${PRELOAD_IPC_CHANNELS.daemonSubscribe}.${subscriptionId}`;

      // hp-7bj1: install the renderer-side listener BEFORE telling main to
      // start the stream. Even if Electron's cross-process IPC ordering
      // makes "fast first event before listener attaches" practically
      // impossible today, listener-first removes the question and matches
      // the sequence-cursor / snapshot-on-reconnect contract documented in
      // README and plan.md §2 / §7.7.
      const removeListener = rendererSubscribe(channel, listener);

      // hp-7bj1: surface main-side rejection. Previously `void invoke(...)`
      // hid the rejection so the renderer believed it was subscribed even
      // when main refused (e.g., topic allowlist drift, daemon stream
      // attach error). The optional onError hook lets renderer state
      // observe the failure without breaking callers that don't pass it.
      invoke(PRELOAD_IPC_CHANNELS.daemonSubscribe, { topic, subscriptionId }).catch(
        (cause: unknown) => {
          opts?.onError?.(new DaemonSubscribeError("subscribe_failed", cause));
        },
      );

      return () => {
        removeListener();
        invoke(PRELOAD_IPC_CHANNELS.daemonUnsubscribe, { subscriptionId }).catch(
          (cause: unknown) => {
            opts?.onError?.(new DaemonSubscribeError("unsubscribe_failed", cause));
          },
        );
      };
    },
  };
}

function throwingDefaultProvider(): never {
  // Forces production preload to inject a real UUID source. Tests that
  // forget to inject one get an obvious error rather than a stable
  // "undefined" suffix that would silently make every channel name
  // collide.
  throw new Error("createDaemonBridge requires options.subscriptionId");
}
