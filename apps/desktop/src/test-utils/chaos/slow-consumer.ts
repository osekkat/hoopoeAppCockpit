// hp-2qn — Slow-consumer chaos primitive.
//
// Wraps an async iteration / WebSocket-style consumer with a per-event
// delay so chaos tests can drive the §10.1 backpressure path without
// hand-rolling timing per test. Two flavors:
//
//   slowConsumeWebSocketMessages(ws, {delayMs, maxMessages?})
//     — listens to `message` events, sleeps `delayMs` per event,
//       returns the messages collected before the loop hits
//       maxMessages or the socket closes.
//
//   slowConsumeReadable(stream, {delayMs, maxItems?})
//     — for any AsyncIterable<unknown>: same shape.
//
// Both are pure consumers — they don't ack/nack on their own; the
// daemon's _lag emission is asserted at a higher level by the chaos
// test suite.

export interface SlowConsumeOptions {
  /** Delay applied between each consumed item, in ms. */
  delayMs: number;
  /** Stop after this many items. Default: unlimited. */
  maxItems?: number;
  /** Hard ceiling on total wall-clock duration, in ms. Default: 30s. */
  timeoutMs?: number;
}

interface MinimalWebSocket {
  addEventListener(type: "message" | "close" | "error", listener: (event: { data: unknown }) => void): void;
  removeEventListener(type: "message" | "close" | "error", listener: (event: { data: unknown }) => void): void;
  close?: () => void;
}

function sleep(ms: number, signal?: AbortSignal): Promise<void> {
  return new Promise<void>((resolveFn, rejectFn) => {
    const timer = setTimeout(resolveFn, ms);
    if (signal !== undefined) {
      signal.addEventListener("abort", () => {
        clearTimeout(timer);
        rejectFn(new Error("aborted"));
      });
    }
  });
}

/** Consume WS messages with a per-event delay; collected payloads are
 *  returned. When the socket closes or `maxItems` is reached, the loop
 *  exits. `timeoutMs` is the upper bound on wall-clock duration. */
export async function slowConsumeWebSocketMessages(
  ws: MinimalWebSocket,
  options: SlowConsumeOptions,
): Promise<readonly unknown[]> {
  const collected: unknown[] = [];
  let closed = false;
  const queue: unknown[] = [];
  let resolveNext: (() => void) | null = null;
  const onMessage = (event: { data: unknown }): void => {
    queue.push(event.data);
    if (resolveNext !== null) {
      const r = resolveNext;
      resolveNext = null;
      r();
    }
  };
  const onClose = (): void => {
    closed = true;
    if (resolveNext !== null) {
      const r = resolveNext;
      resolveNext = null;
      r();
    }
  };
  ws.addEventListener("message", onMessage);
  ws.addEventListener("close", onClose);
  ws.addEventListener("error", onClose);
  const controller = new AbortController();
  const timeoutMs = options.timeoutMs ?? 30_000;
  const timeout = setTimeout(() => controller.abort(), timeoutMs);
  try {
    while (!controller.signal.aborted) {
      if (queue.length === 0) {
        if (closed) break;
        await new Promise<void>((r) => {
          resolveNext = r;
        });
        if (closed && queue.length === 0) break;
      }
      const next = queue.shift();
      collected.push(next);
      if (options.maxItems !== undefined && collected.length >= options.maxItems) break;
      try {
        await sleep(options.delayMs, controller.signal);
      } catch {
        break;
      }
    }
  } finally {
    clearTimeout(timeout);
    ws.removeEventListener("message", onMessage);
    ws.removeEventListener("close", onClose);
    ws.removeEventListener("error", onClose);
  }
  return collected;
}

/** Consume any AsyncIterable with the same per-item delay semantics. */
export async function slowConsumeReadable<T>(
  source: AsyncIterable<T>,
  options: SlowConsumeOptions,
): Promise<readonly T[]> {
  const collected: T[] = [];
  const controller = new AbortController();
  const timeoutMs = options.timeoutMs ?? 30_000;
  const timeout = setTimeout(() => controller.abort(), timeoutMs);
  try {
    for await (const item of source) {
      if (controller.signal.aborted) break;
      collected.push(item);
      if (options.maxItems !== undefined && collected.length >= options.maxItems) break;
      try {
        await sleep(options.delayMs, controller.signal);
      } catch {
        break;
      }
    }
  } finally {
    clearTimeout(timeout);
  }
  return collected;
}
