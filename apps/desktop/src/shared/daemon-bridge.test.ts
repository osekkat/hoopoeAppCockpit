import { describe, expect, test } from "bun:test";
import {
  DaemonSubscribeError,
  createDaemonBridge,
  type DaemonInvoke,
  type DaemonRendererSubscribe,
} from "./daemon-bridge.ts";
import {
  IpcContractError,
  PRELOAD_IPC_CHANNELS,
} from "./ipc-contract.ts";

interface InvokeCall {
  readonly channel: string;
  readonly args: unknown;
}

interface ListenerEntry {
  readonly channel: string;
  readonly listener: (payload: unknown) => void;
}

function makeStubSubscribe() {
  const listeners = new Map<string, (payload: unknown) => void>();
  const subscribe: DaemonRendererSubscribe = (channel, listener) => {
    listeners.set(channel, listener);
    return () => listeners.delete(channel);
  };
  return {
    subscribe,
    listeners,
    push(channel: string, payload: unknown): void {
      const listener = listeners.get(channel);
      if (!listener) throw new Error(`no listener registered for ${channel}`);
      listener(payload);
    },
  };
}

function counterIds(): () => string {
  let n = 0;
  return () => `id-${++n}`;
}

describe("daemon bridge — request", () => {
  test("rejects unknown methods at the preload boundary before invoking main", async () => {
    const calls: InvokeCall[] = [];
    const invoke: DaemonInvoke = async (channel, args) => {
      calls.push({ channel, args });
      return null;
    };
    const stub = makeStubSubscribe();
    const bridge = createDaemonBridge(invoke, stub.subscribe, {
      subscriptionId: counterIds(),
    });

    let thrown: unknown = null;
    try {
      await bridge.request("evil.method" as never, { x: 1 });
    } catch (error) {
      thrown = error;
    }

    expect(thrown).toBeInstanceOf(IpcContractError);
    expect((thrown as IpcContractError).kind).toBe("method");
    expect(calls).toHaveLength(0);
  });

  test("forwards allowed methods through invoke with the daemonRequest channel", async () => {
    const calls: InvokeCall[] = [];
    const invoke: DaemonInvoke = async (channel, args) => {
      calls.push({ channel, args });
      return { ok: true };
    };
    const stub = makeStubSubscribe();
    const bridge = createDaemonBridge(invoke, stub.subscribe, {
      subscriptionId: counterIds(),
    });

    const result = await bridge.request("tunnel.snapshot", null);

    expect(result).toEqual({ ok: true });
    expect(calls).toEqual([
      {
        channel: PRELOAD_IPC_CHANNELS.daemonRequest,
        args: { method: "tunnel.snapshot", body: null },
      },
    ]);
  });
});

describe("daemon bridge — subscribe (hp-7bj1 race + error surface)", () => {
  test("installs renderer listener BEFORE telling main to start the stream", () => {
    const events: string[] = [];
    const subscribeStub: DaemonRendererSubscribe = (channel, listener) => {
      events.push(`listener:${channel}`);
      return () => {
        events.push(`off:${channel}`);
        void listener;
      };
    };
    const invoke: DaemonInvoke = async (channel) => {
      events.push(`invoke:${channel}`);
      return null;
    };
    const bridge = createDaemonBridge(invoke, subscribeStub, {
      subscriptionId: counterIds(),
    });

    const unsubscribe = bridge.subscribe("events.tunnel", () => undefined);

    // The renderer-side listener must register first; the main-side
    // invoke is microtask-queued after.
    expect(events[0]).toBe(
      `listener:${PRELOAD_IPC_CHANNELS.daemonSubscribe}.id-1`,
    );
    expect(events[1]).toBe(`invoke:${PRELOAD_IPC_CHANNELS.daemonSubscribe}`);

    unsubscribe();
  });

  test("delivers a fast first event emitted before the invoke promise resolves", async () => {
    const stub = makeStubSubscribe();
    let resolveInvoke: (() => void) | null = null;
    const invokePromise = new Promise<void>((resolve) => {
      resolveInvoke = resolve;
    });
    const invoke: DaemonInvoke = async (channel) => {
      if (channel === PRELOAD_IPC_CHANNELS.daemonSubscribe) await invokePromise;
      return null;
    };
    const bridge = createDaemonBridge(invoke, stub.subscribe, {
      subscriptionId: counterIds(),
    });

    const received: unknown[] = [];
    bridge.subscribe("events.tunnel", (payload) => received.push(payload));

    // Push an event BEFORE the invoke promise resolves. This is the
    // "fast first event" race the previous fire-and-forget order
    // exposed: with listener-first the renderer must catch it.
    stub.push(`${PRELOAD_IPC_CHANNELS.daemonSubscribe}.id-1`, { ok: true });
    expect(received).toEqual([{ ok: true }]);

    resolveInvoke!();
    await Promise.resolve();
  });

  test("surfaces a rejected main-side subscribe registration via options.onError", async () => {
    const stub = makeStubSubscribe();
    const subscribeFailure = new Error("topic not allowed by main");
    const invoke: DaemonInvoke = async (channel) => {
      if (channel === PRELOAD_IPC_CHANNELS.daemonSubscribe) {
        throw subscribeFailure;
      }
      return null;
    };
    const bridge = createDaemonBridge(invoke, stub.subscribe, {
      subscriptionId: counterIds(),
    });

    const errors: DaemonSubscribeError[] = [];
    const unsubscribe = bridge.subscribe(
      "events.tunnel",
      () => undefined,
      { onError: (error) => errors.push(error) },
    );

    // Let the rejected promise propagate through the catch handler.
    await new Promise<void>((resolve) => setTimeout(resolve, 0));

    expect(errors).toHaveLength(1);
    expect(errors[0]).toBeInstanceOf(DaemonSubscribeError);
    expect(errors[0]!.code).toBe("subscribe_failed");
    expect(errors[0]!.cause).toBe(subscribeFailure);

    unsubscribe();
  });

  test("surfaces a rejected main-side unsubscribe via options.onError", async () => {
    const stub = makeStubSubscribe();
    const unsubscribeFailure = new Error("daemon teardown blew up");
    const invoke: DaemonInvoke = async (channel) => {
      if (channel === PRELOAD_IPC_CHANNELS.daemonUnsubscribe) {
        throw unsubscribeFailure;
      }
      return null;
    };
    const bridge = createDaemonBridge(invoke, stub.subscribe, {
      subscriptionId: counterIds(),
    });

    const errors: DaemonSubscribeError[] = [];
    const unsubscribe = bridge.subscribe(
      "events.tunnel",
      () => undefined,
      { onError: (error) => errors.push(error) },
    );

    unsubscribe();
    await new Promise<void>((resolve) => setTimeout(resolve, 0));

    expect(errors).toHaveLength(1);
    expect(errors[0]!.code).toBe("unsubscribe_failed");
    expect(errors[0]!.cause).toBe(unsubscribeFailure);
  });

  test("rejects unknown topics synchronously without invoking main", () => {
    const calls: InvokeCall[] = [];
    const invoke: DaemonInvoke = async (channel, args) => {
      calls.push({ channel, args });
      return null;
    };
    const stub = makeStubSubscribe();
    const bridge = createDaemonBridge(invoke, stub.subscribe, {
      subscriptionId: counterIds(),
    });

    let thrown: unknown = null;
    try {
      bridge.subscribe("events.evil" as never, () => undefined);
    } catch (error) {
      thrown = error;
    }

    expect(thrown).toBeInstanceOf(IpcContractError);
    expect((thrown as IpcContractError).kind).toBe("topic");
    expect(calls).toHaveLength(0);
    expect(stub.listeners.size).toBe(0);
  });

  test("uses a unique channel per subscription so a fast unsubscribe doesn't kill a sibling", () => {
    const channels: string[] = [];
    const subscribeStub: DaemonRendererSubscribe = (channel) => {
      channels.push(channel);
      return () => undefined;
    };
    const invoke: DaemonInvoke = async () => null;
    const bridge = createDaemonBridge(invoke, subscribeStub, {
      subscriptionId: counterIds(),
    });

    bridge.subscribe("events.tunnel", () => undefined);
    bridge.subscribe("events.tunnel", () => undefined);

    expect(channels).toEqual([
      `${PRELOAD_IPC_CHANNELS.daemonSubscribe}.id-1`,
      `${PRELOAD_IPC_CHANNELS.daemonSubscribe}.id-2`,
    ]);
  });

  test("subscribe/unsubscribe failures are silently swallowed when no onError is provided", async () => {
    const stub = makeStubSubscribe();
    const invoke: DaemonInvoke = async () => {
      throw new Error("main rejected");
    };
    const bridge = createDaemonBridge(invoke, stub.subscribe, {
      subscriptionId: counterIds(),
    });

    // No options — onError is absent. The bridge must NOT throw or
    // surface an unhandled-promise rejection at the call site, because
    // the public subscribe contract is synchronous.
    const unsubscribe = bridge.subscribe("events.tunnel", () => undefined);
    expect(typeof unsubscribe).toBe("function");

    unsubscribe();
    await new Promise<void>((resolve) => setTimeout(resolve, 0));
    // No assertion to fail — we just want to confirm no thrown error
    // escaped the synchronous boundary.
  });
});

describe("daemon bridge — DaemonSubscribeError", () => {
  test("preserves the underlying cause and uses a stable code", () => {
    const cause = new Error("root reason");
    const error = new DaemonSubscribeError("subscribe_failed", cause);

    expect(error.code).toBe("subscribe_failed");
    expect(error.cause).toBe(cause);
    expect(error.message).toContain("subscribe failed");
    expect(error.message).toContain("root reason");
    expect(error).toBeInstanceOf(Error);
  });

  test("stringifies non-Error causes via String(...)", () => {
    const error = new DaemonSubscribeError("unsubscribe_failed", { not: "an error" });
    expect(error.message).toContain("unsubscribe failed");
    expect(error.message).toContain("[object Object]");
  });
});

describe("daemon bridge — defaults", () => {
  test("missing subscriptionId provider throws on first subscribe", () => {
    const stub = makeStubSubscribe();
    const invoke: DaemonInvoke = async () => null;
    const bridge = createDaemonBridge(invoke, stub.subscribe);

    let thrown: unknown = null;
    try {
      bridge.subscribe("events.tunnel", () => undefined);
    } catch (error) {
      thrown = error;
    }

    expect(thrown).toBeInstanceOf(Error);
    expect((thrown as Error).message).toContain("subscriptionId");
  });
});
