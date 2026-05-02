// `@hoopoe/fixtures/replay` — time-stepped event-stream replayer (hp-o74).
//
// Drives a fixture scenario's events.jsonl through a subscriber as if it
// were a live WS event stream from the daemon. Honors:
//   - per-channel sequence cursors (so reconnect-replay works)
//   - configurable acceleration (1×, 10×, 100×, instant)
//   - cancellation
//   - subscribe-from-cursor semantics (the snapshot-on-reconnect protocol
//     in plan.md §2.6 is exercised against fixtures, not just real WS)
//
// Pure: no Electron / Node-process assumptions. The MockFlywheelMode
// integration in apps/desktop/src/main/ binds this to the IPC layer.

import type { ReplayEvent } from "./scenario-source.ts";

export type ReplaySpeed = 1 | 10 | 100 | "instant";

export interface ReplaySubscriber {
  /** Called for every event delivered. */
  onEvent: (event: ReplayEvent) => void | Promise<void>;
  /** Called once when the stream completes (reached end-of-events.jsonl). */
  onEnd?: () => void;
  /** Called if the stream is canceled before completion. */
  onCancel?: () => void;
}

export interface ReplaySession {
  /** Cancel this session. Idempotent. */
  cancel: () => void;
  /** Promise that resolves when the session completes (end or cancel). */
  done: Promise<void>;
  /** Snapshot of cursors per channel as of the last delivered event. */
  cursors: () => Record<string, number>;
}

export interface StartReplayOptions {
  events: ReplayEvent[];
  subscriber: ReplaySubscriber;
  speed?: ReplaySpeed;
  /** Channel → cursor map. Events with seq <= cursor on a given channel are
   *  skipped (snapshot-replay semantics: reconnect resumes after the cursor). */
  fromCursors?: Record<string, number>;
  /** Override the clock (for tests). Returns ms since epoch. */
  now?: () => number;
  /** Override the sleeper (for tests). Default sets timeouts in real time. */
  sleep?: (ms: number) => Promise<void>;
}

const defaultSleep = (ms: number): Promise<void> =>
  new Promise((resolve) => {
    setTimeout(resolve, ms);
  });

const parseTs = (ts: string): number => {
  const t = Date.parse(ts);
  return Number.isFinite(t) ? t : 0;
};

/** Start a replay session. Returns a handle the caller can cancel. */
export function startReplay(options: StartReplayOptions): ReplaySession {
  const { events, subscriber } = options;
  const speed = options.speed ?? 1;
  const fromCursors = options.fromCursors ?? {};
  const now = options.now ?? (() => Date.now());
  const sleep = options.sleep ?? defaultSleep;

  // Filter events by per-channel cursor up-front.
  const filtered = events
    .filter((e) => {
      const cursor = fromCursors[e.channel] ?? 0;
      return e.seq > cursor;
    })
    // Stable order: by ts then by seq within channel (events.jsonl already
    // emits in causal order; we re-sort defensively).
    .sort((a, b) => parseTs(a.ts) - parseTs(b.ts) || a.seq - b.seq);

  const cursors: Record<string, number> = { ...fromCursors };
  let canceled = false;
  let resolveDone: () => void = () => {};
  const done = new Promise<void>((resolve) => {
    resolveDone = resolve;
  });

  async function run(): Promise<void> {
    const first = filtered[0];
    if (first === undefined) {
      subscriber.onEnd?.();
      resolveDone();
      return;
    }
    const startWall = now();
    const startTs = parseTs(first.ts);

    for (const event of filtered) {
      if (canceled) {
        subscriber.onCancel?.();
        resolveDone();
        return;
      }
      if (speed !== "instant") {
        const eventOffset = parseTs(event.ts) - startTs;
        const wallOffset = now() - startWall;
        const targetWallOffset = Math.floor(eventOffset / speed);
        const sleepMs = targetWallOffset - wallOffset;
        if (sleepMs > 0) await sleep(sleepMs);
      }
      if (canceled) {
        subscriber.onCancel?.();
        resolveDone();
        return;
      }
      cursors[event.channel] = Math.max(cursors[event.channel] ?? 0, event.seq);
      try {
        await subscriber.onEvent(event);
      } catch (err) {
        // Subscriber errors abort the stream loudly — they're a bug in the
        // consumer, not in the fixture.
        if (typeof console !== "undefined") {
          // eslint-disable-next-line no-console
          console.error("ReplaySession subscriber threw", err);
        }
        subscriber.onCancel?.();
        resolveDone();
        return;
      }
    }
    subscriber.onEnd?.();
    resolveDone();
  }

  // Don't await the run; return immediately.
  void run();

  return {
    cancel: () => {
      canceled = true;
    },
    done,
    cursors: () => ({ ...cursors }),
  };
}

/** Synthesize a snapshot-on-reconnect: returns the per-channel last-seq map
 *  derived from a list of events. Callers pass this back to `startReplay`'s
 *  `fromCursors` to skip already-delivered events on reconnect. */
export function deriveCursors(events: ReplayEvent[]): Record<string, number> {
  const cursors: Record<string, number> = {};
  for (const e of events) {
    cursors[e.channel] = Math.max(cursors[e.channel] ?? 0, e.seq);
  }
  return cursors;
}
