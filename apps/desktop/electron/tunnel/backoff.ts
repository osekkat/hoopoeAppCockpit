// hp-e7k — Reconnect backoff with jitter (§2.5).
//
// Plan.md mandates a 30s cap with jitter so a wave of clients reconnecting
// after a network blip don't dogpile the daemon. Standard exponential
// backoff: base * 2^attempt, clipped to a max, then ±25% jitter.

export interface BackoffConfig {
  /** First attempt's deadline in ms. Default 500ms. */
  readonly baseMs: number;
  /** Hard cap on a single delay. Default 30 000ms (§2.5). */
  readonly maxMs: number;
  /** Jitter ratio; final delay is in [delay * (1-jitter), delay * (1+jitter)].
   *  Default 0.25. */
  readonly jitter: number;
  /** Random source — tests inject a deterministic stream so assertions
   *  on jitter aren't flaky. */
  readonly random?: () => number;
}

export const DEFAULT_BACKOFF: BackoffConfig = {
  baseMs: 500,
  maxMs: 30_000,
  jitter: 0.25,
};

/** Compute the delay (ms) for `attempt` (0-indexed). The first reconnect
 *  attempt uses `baseMs` ± jitter; subsequent attempts double until they
 *  hit `maxMs`. */
export function computeBackoffMs(attempt: number, config: BackoffConfig = DEFAULT_BACKOFF): number {
  if (attempt < 0) throw new Error(`computeBackoffMs: attempt must be >= 0; got ${attempt}`);
  if (config.baseMs <= 0) throw new Error(`computeBackoffMs: baseMs must be > 0; got ${config.baseMs}`);
  if (config.maxMs < config.baseMs) {
    throw new Error(`computeBackoffMs: maxMs (${config.maxMs}) must be >= baseMs (${config.baseMs})`);
  }
  if (config.jitter < 0 || config.jitter >= 1) {
    throw new Error(`computeBackoffMs: jitter must be in [0, 1); got ${config.jitter}`);
  }
  // Use Math.pow rather than `<<` so we don't blow up at attempt > 30.
  const exponential = Math.min(config.maxMs, config.baseMs * Math.pow(2, attempt));
  const random = config.random ?? Math.random;
  // Map random in [0, 1) to a multiplier in [1 - jitter, 1 + jitter).
  const multiplier = 1 - config.jitter + random() * (config.jitter * 2);
  // Clamp post-jitter so we never violate the maxMs ceiling.
  return Math.min(config.maxMs, Math.round(exponential * multiplier));
}

/** Returns the sequence of delays for attempts 0..n-1 — useful for tests
 *  that want to assert a curve without driving the FSM. */
export function backoffSequence(count: number, config: BackoffConfig = DEFAULT_BACKOFF): readonly number[] {
  const out: number[] = [];
  for (let i = 0; i < count; i += 1) {
    out.push(computeBackoffMs(i, config));
  }
  return out;
}
