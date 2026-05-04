import {
  PRELOAD_IPC_CHANNELS,
  type PowerAssertionAcquireInput,
  type PowerAssertionReleaseInput,
  type PowerAssertionSnapshot,
} from "./ipc-contract.ts";

export const POWER_ACQUIRE_RATE_LIMIT_MAX = 12;
export const POWER_ACQUIRE_RATE_LIMIT_WINDOW_MS = 60_000;

export interface PowerAcquireRateLimiter {
  assertAllowed(): void;
}

export interface PowerAcquireRateLimiterOptions {
  readonly maxAttempts?: number;
  readonly windowMs?: number;
  readonly now?: () => number;
}

export class PowerAcquireRateLimitError extends Error {
  readonly code = "power_acquire_rate_limited";
  readonly retryAfterMs: number;

  constructor(retryAfterMs: number) {
    super(`power assertion acquire rate limit exceeded; retry after ${retryAfterMs}ms`);
    this.name = "PowerAcquireRateLimitError";
    this.retryAfterMs = retryAfterMs;
  }
}

export function createPowerAcquireRateLimiter(
  options: PowerAcquireRateLimiterOptions = {},
): PowerAcquireRateLimiter {
  const maxAttempts = positiveInteger(
    options.maxAttempts ?? POWER_ACQUIRE_RATE_LIMIT_MAX,
    "maxAttempts",
  );
  const windowMs = positiveInteger(
    options.windowMs ?? POWER_ACQUIRE_RATE_LIMIT_WINDOW_MS,
    "windowMs",
  );
  const now = options.now ?? (() => Date.now());
  const attempts: number[] = [];

  return {
    assertAllowed(): void {
      const current = now();
      pruneAttempts(attempts, current, windowMs);
      if (attempts.length >= maxAttempts) {
        const oldest = attempts[0] ?? current;
        const retryAfterMs = Math.max(1, Math.ceil(windowMs - (current - oldest)));
        throw new PowerAcquireRateLimitError(retryAfterMs);
      }
      attempts.push(current);
    },
  };
}

export type PreloadInvoke = (
  channel: string,
  args: unknown,
) => Promise<unknown>;

export interface PowerBridge {
  readonly acquire: (
    input: PowerAssertionAcquireInput,
  ) => Promise<PowerAssertionSnapshot>;
  readonly release: (
    input: PowerAssertionReleaseInput,
  ) => Promise<PowerAssertionSnapshot>;
  readonly snapshot: () => Promise<PowerAssertionSnapshot>;
}

export interface CreatePowerBridgeOptions {
  readonly rateLimiter?: PowerAcquireRateLimiter;
}

export function createPowerBridge(
  invoke: PreloadInvoke,
  options: CreatePowerBridgeOptions = {},
): PowerBridge {
  const rateLimiter = options.rateLimiter ?? createPowerAcquireRateLimiter();
  return {
    acquire: async (input) => {
      rateLimiter.assertAllowed();
      return (await invoke(
        PRELOAD_IPC_CHANNELS.powerAcquire,
        input,
      )) as PowerAssertionSnapshot;
    },
    release: (input) =>
      invoke(PRELOAD_IPC_CHANNELS.powerRelease, input) as Promise<PowerAssertionSnapshot>,
    snapshot: () =>
      invoke(PRELOAD_IPC_CHANNELS.powerSnapshot, {}) as Promise<PowerAssertionSnapshot>,
  };
}

function pruneAttempts(
  attempts: number[],
  current: number,
  windowMs: number,
): void {
  while (attempts.length > 0 && current - attempts[0]! >= windowMs) {
    attempts.shift();
  }
}

function positiveInteger(value: number, field: string): number {
  if (!Number.isInteger(value) || value < 1) {
    throw new Error(`${field} must be a positive integer`);
  }
  return value;
}
