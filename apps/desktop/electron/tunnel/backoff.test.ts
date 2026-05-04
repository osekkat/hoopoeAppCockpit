// hp-e7k — backoff tests.

import { expect, test } from "bun:test";
import { DEFAULT_BACKOFF, backoffSequence, computeBackoffMs } from "./index.ts";

test("computeBackoffMs: 0 attempts returns ~baseMs (within jitter window)", () => {
  // Use deterministic random=0.5 → multiplier == 1, so we get exact baseMs.
  const value = computeBackoffMs(0, { ...DEFAULT_BACKOFF, random: () => 0.5 });
  expect(value).toBe(500);
});

test("computeBackoffMs: doubles per attempt (no jitter)", () => {
  const noJitter = { ...DEFAULT_BACKOFF, jitter: 0, random: () => 0.5 };
  expect(computeBackoffMs(0, noJitter)).toBe(500);
  expect(computeBackoffMs(1, noJitter)).toBe(1000);
  expect(computeBackoffMs(2, noJitter)).toBe(2000);
  expect(computeBackoffMs(3, noJitter)).toBe(4000);
  expect(computeBackoffMs(4, noJitter)).toBe(8000);
  expect(computeBackoffMs(5, noJitter)).toBe(16000);
  expect(computeBackoffMs(6, noJitter)).toBe(30000); // capped
  expect(computeBackoffMs(7, noJitter)).toBe(30000);
  expect(computeBackoffMs(50, noJitter)).toBe(30000);
});

test("computeBackoffMs: jitter window is ±25% of baseline", () => {
  const fixedRandom = (value: number) => () => value;
  // random=0 → multiplier = 1 - jitter (25% below); random≈1 → multiplier ≈ 1 + jitter (25% above).
  const low = computeBackoffMs(0, { ...DEFAULT_BACKOFF, random: fixedRandom(0) });
  const high = computeBackoffMs(0, { ...DEFAULT_BACKOFF, random: fixedRandom(0.999_999) });
  expect(low).toBe(Math.round(500 * 0.75));      // 375
  expect(high).toBeGreaterThan(low);
  expect(high).toBeLessThanOrEqual(Math.round(500 * 1.25)); // 625
});

test("computeBackoffMs: clamps to maxMs even after jitter", () => {
  // attempt 100 → exponential is huge; jitter shouldn't push above maxMs.
  const value = computeBackoffMs(100, { ...DEFAULT_BACKOFF, random: () => 0.999_999 });
  expect(value).toBeLessThanOrEqual(DEFAULT_BACKOFF.maxMs);
});

test("computeBackoffMs: refuses negative attempts + invalid config", () => {
  expect(() => computeBackoffMs(-1)).toThrow(/attempt must be >= 0/);
  expect(() => computeBackoffMs(0, { baseMs: 0, maxMs: 100, jitter: 0 })).toThrow(/baseMs must be > 0/);
  expect(() => computeBackoffMs(0, { baseMs: 100, maxMs: 50, jitter: 0 })).toThrow(/maxMs/);
  expect(() => computeBackoffMs(0, { baseMs: 100, maxMs: 200, jitter: 1 })).toThrow(/jitter/);
  expect(() => computeBackoffMs(0, { baseMs: 100, maxMs: 200, jitter: -0.1 })).toThrow(/jitter/);
});

test("backoffSequence: produces exactly N delays in attempt order", () => {
  const seq = backoffSequence(5, { ...DEFAULT_BACKOFF, jitter: 0, random: () => 0.5 });
  expect(seq).toEqual([500, 1000, 2000, 4000, 8000]);
});

test("DEFAULT_BACKOFF: 30s cap per plan.md §2.5", () => {
  expect(DEFAULT_BACKOFF.maxMs).toBe(30_000);
});
