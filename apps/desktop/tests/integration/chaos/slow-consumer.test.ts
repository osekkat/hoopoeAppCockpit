// hp-2qn — Slow-consumer primitive test.
//
// Drives the slow-consumer wrapper against a synthetic AsyncIterable
// to verify the per-item delay + maxItems cap + timeout. The
// WebSocket flavor is exercised by integration tests against a live
// daemon; the readable-stream flavor is testable in pure TS.

import { describe, expect, test } from "bun:test";
import { slowConsumeReadable } from "../../../src/test-utils/chaos/index.ts";

async function* numberStream(n: number, intervalMs: number = 0): AsyncGenerator<number> {
  for (let i = 0; i < n; i++) {
    if (intervalMs > 0) await new Promise((r) => setTimeout(r, intervalMs));
    yield i;
  }
}

describe("hp-2qn :: slow-consumer primitive", () => {
  test("applies the per-item delay between consumed values", async () => {
    const start = Date.now();
    const collected = await slowConsumeReadable(numberStream(5), { delayMs: 50 });
    const elapsed = Date.now() - start;
    expect(collected).toEqual([0, 1, 2, 3, 4]);
    // 5 items × 50ms = 250ms expected; allow some slack for scheduler.
    expect(elapsed).toBeGreaterThanOrEqual(200);
  });

  test("respects maxItems cap", async () => {
    const collected = await slowConsumeReadable(numberStream(100), {
      delayMs: 1,
      maxItems: 5,
    });
    expect(collected.length).toBe(5);
    expect(collected).toEqual([0, 1, 2, 3, 4]);
  });

  test("respects timeoutMs cap", async () => {
    // 100 items, 100ms delay each → 10s; cap at 200ms.
    const start = Date.now();
    const collected = await slowConsumeReadable(numberStream(100), {
      delayMs: 100,
      timeoutMs: 200,
    });
    const elapsed = Date.now() - start;
    expect(elapsed).toBeLessThan(2_000);
    // Should have collected far fewer than 100 items.
    expect(collected.length).toBeLessThan(10);
  });

  test("returns immediately on empty source", async () => {
    const collected = await slowConsumeReadable(numberStream(0), { delayMs: 50 });
    expect(collected).toEqual([]);
  });
});
