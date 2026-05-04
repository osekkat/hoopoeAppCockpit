// hp-ndx5 — Pure debounce primitive (no timers tied to module scope).
//
// The clone watcher fires hundreds of fs events when an editor saves a
// file (write to a temp file, rename, write to .swp, fsync, etc.). We
// collapse them into a single probe call N ms after the last event.
// Tests inject a mock clock so they don't have to wait real time.

export interface Clock {
  /** Returns a timer handle. */
  readonly setTimeout: (callback: () => void, ms: number) => unknown;
  /** Cancels a pending timer. */
  readonly clearTimeout: (handle: unknown) => void;
}

export const realClock: Clock = {
  setTimeout: (cb, ms) => globalThis.setTimeout(cb, ms),
  clearTimeout: (handle) => globalThis.clearTimeout(handle as ReturnType<typeof globalThis.setTimeout>),
};

export interface DebouncedHandle {
  /** Reset the timer; the callback fires `delayMs` after the LAST call. */
  readonly trigger: () => void;
  /** Cancel any pending fire without invoking the callback. */
  readonly cancel: () => void;
  /** True when a fire is pending (between `trigger()` and the callback). */
  readonly pending: () => boolean;
}

export function debounce(
  callback: () => void,
  delayMs: number,
  clock: Clock = realClock,
): DebouncedHandle {
  if (delayMs < 0) throw new Error(`debounce: delayMs must be >= 0; got ${delayMs}`);
  let handle: unknown = null;

  function fire(): void {
    handle = null;
    callback();
  }

  return {
    trigger() {
      if (handle !== null) clock.clearTimeout(handle);
      handle = clock.setTimeout(fire, delayMs);
    },
    cancel() {
      if (handle !== null) {
        clock.clearTimeout(handle);
        handle = null;
      }
    },
    pending() {
      return handle !== null;
    },
  };
}

/** Manual clock for tests. `tick(ms)` fires every timer whose deadline is
 *  <= the cumulative time. Insertion order is preserved. */
export function createMockClock(): Clock & {
  /** Advance time by `ms` and fire any expired callbacks (in scheduled
   *  order). Returns the number of callbacks fired. */
  tick(ms: number): number;
  /** Currently-scheduled callback count. */
  pendingCount(): number;
  /** Discard all pending timers without firing them. */
  reset(): void;
} {
  interface Scheduled {
    readonly id: number;
    readonly deadline: number;
    readonly cb: () => void;
  }
  let now = 0;
  let nextId = 1;
  let scheduled: Scheduled[] = [];

  return {
    setTimeout(cb, ms) {
      const id = nextId++;
      scheduled.push({ id, deadline: now + ms, cb });
      return id;
    },
    clearTimeout(handle) {
      const id = handle as number;
      scheduled = scheduled.filter((s) => s.id !== id);
    },
    tick(ms) {
      now += ms;
      let fired = 0;
      while (true) {
        const ready = scheduled
          .filter((s) => s.deadline <= now)
          .sort((a, b) => a.deadline - b.deadline || a.id - b.id);
        if (ready.length === 0) break;
        const next = ready[0]!;
        scheduled = scheduled.filter((s) => s.id !== next.id);
        next.cb();
        fired += 1;
        // Re-check after firing — the callback might have scheduled more.
      }
      return fired;
    },
    pendingCount() {
      return scheduled.length;
    },
    reset() {
      scheduled = [];
    },
  };
}
