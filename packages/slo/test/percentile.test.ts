import { describe, expect, test } from "bun:test";
import { percentile, PercentileError } from "../src/index.ts";

describe("hp-5ja :: percentile", () => {
  test("p100 returns the maximum sample", () => {
    expect(percentile([1, 2, 3, 4, 5], 100)).toBe(5);
  });

  test("p0 is rejected (must be > 0)", () => {
    expect(() => percentile([1, 2, 3], 0)).toThrow(PercentileError);
  });

  test("p50 of [1..5] is 3 (the median)", () => {
    expect(percentile([1, 2, 3, 4, 5], 50)).toBe(3);
  });

  test("p95 of 50 evenly-spaced samples interpolates correctly", () => {
    const samples = Array.from({ length: 50 }, (_, i) => i + 1);
    // rank = 95/100 * 49 = 46.55; lower=46 (=47), upper=47 (=48); fraction=0.55
    // value = 47 + (48-47)*0.55 = 47.55
    expect(percentile(samples, 95)).toBeCloseTo(47.55, 5);
  });

  test("single sample short-circuits to that sample for any percentile", () => {
    expect(percentile([42], 1)).toBe(42);
    expect(percentile([42], 99)).toBe(42);
  });

  test("filters non-finite samples before computing", () => {
    expect(percentile([1, Number.NaN, 2, Number.POSITIVE_INFINITY, 3], 50)).toBe(2);
  });

  test("rejects empty samples", () => {
    expect(() => percentile([], 95)).toThrow(PercentileError);
  });

  test("is order-independent", () => {
    const ascending = percentile([1, 2, 3, 4, 5, 6, 7, 8, 9, 10], 90);
    const descending = percentile([10, 9, 8, 7, 6, 5, 4, 3, 2, 1], 90);
    const shuffled = percentile([5, 1, 8, 3, 10, 2, 7, 4, 9, 6], 90);
    expect(ascending).toBe(descending);
    expect(ascending).toBe(shuffled);
  });
});
