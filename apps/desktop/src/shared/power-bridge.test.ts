import { describe, expect, test } from "bun:test";
import {
  PowerAcquireRateLimitError,
  createPowerAcquireRateLimiter,
  createPowerBridge,
} from "./power-bridge.ts";

const SNAPSHOT = {
  active: true,
  assertionId: "pa-1",
  mechanism: "powersaveblocker",
  level: "app-suspension",
  ownerRoundIds: ["round-1"],
  heldCount: 1,
  acquiredAt: "2026-05-04T00:00:00.000Z",
} as const;

describe("power acquire rate limiter", () => {
  test("refuses acquire attempts above the per-window limit", () => {
    let now = 1_000;
    const limiter = createPowerAcquireRateLimiter({
      maxAttempts: 2,
      windowMs: 1_000,
      now: () => now,
    });

    limiter.assertAllowed();
    limiter.assertAllowed();

    let thrown: unknown = null;
    try {
      limiter.assertAllowed();
    } catch (error) {
      thrown = error;
    }

    expect(thrown).toBeInstanceOf(PowerAcquireRateLimitError);
    expect((thrown as PowerAcquireRateLimitError).retryAfterMs).toBe(1_000);

    now += 1_000;
    expect(() => limiter.assertAllowed()).not.toThrow();
  });

  test("power bridge rejects rate-limited acquire before IPC", async () => {
    let now = 5_000;
    const calls: Array<{ channel: string; args: unknown }> = [];
    const bridge = createPowerBridge(async (channel, args) => {
      calls.push({ channel, args });
      return SNAPSHOT;
    }, {
      rateLimiter: createPowerAcquireRateLimiter({
        maxAttempts: 1,
        windowMs: 10_000,
        now: () => now,
      }),
    });

    await bridge.acquire({ roundId: "round-1" });

    await expect(bridge.acquire({ roundId: "round-2" })).rejects.toThrow(
      PowerAcquireRateLimitError,
    );
    expect(calls.map((call) => call.channel)).toEqual(["hoopoe.power.acquire"]);

    await bridge.release({ assertionId: "pa-1", reason: "round_complete" });
    await bridge.snapshot();
    expect(calls.map((call) => call.channel)).toEqual([
      "hoopoe.power.acquire",
      "hoopoe.power.release",
      "hoopoe.power.snapshot",
    ]);

    now += 10_000;
    await bridge.acquire({ roundId: "round-3" });
    expect(calls.at(-1)).toMatchObject({
      channel: "hoopoe.power.acquire",
      args: { roundId: "round-3" },
    });
  });
});
