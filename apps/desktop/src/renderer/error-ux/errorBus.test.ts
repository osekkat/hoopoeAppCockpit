import { afterEach, beforeEach, describe, expect, test } from "bun:test";
import { COALESCE_WINDOW_MS, createErrorBus } from "./errorBus.ts";
import type { ErrorPayload, ProblemEnvelope, PublishedError } from "./types.ts";

const baseEnvelope: ProblemEnvelope = {
  type: "https://hoopoe.io/problems/auth/bearer-expired",
  title: "Bearer token has expired",
  status: 401,
  surface: "blocking_modal",
  actionability: "re-pair",
  user_message: "Your session is no longer valid. Re-pair to continue.",
};

function makePayload(overrides?: Partial<ErrorPayload>): ErrorPayload {
  return {
    source: "swarm-launch",
    envelope: baseEnvelope,
    ...overrides,
  };
}

describe("hp-8dym :: errorBus", () => {
  let bus = createErrorBus();

  beforeEach(() => {
    bus = createErrorBus();
  });

  afterEach(() => {
    bus.reset();
  });

  test("publish returns a stable id and exposes the error in snapshot order", () => {
    const id1 = bus.publish(makePayload({ source: "a" }));
    const id2 = bus.publish(
      makePayload({ source: "b", envelope: { ...baseEnvelope, type: "https://hoopoe.io/problems/x" } }),
    );

    const errors = bus.getSnapshot().errors;
    expect(errors).toHaveLength(2);
    expect(errors[0]?.id).toBe(id1);
    expect(errors[1]?.id).toBe(id2);
  });

  test("publish derives severity from envelope.status and routes to envelope.surface by default", () => {
    bus.publish(
      makePayload({
        envelope: {
          ...baseEnvelope,
          status: 500,
          surface: "banner",
          actionability: "manual",
        },
      }),
    );
    const [error] = bus.getSnapshot().errors as readonly PublishedError[];
    expect(error?.severity).toBe("error");
    expect(error?.surface).toBe("banner");
  });

  test("publish honors explicit severity + surface override", () => {
    bus.publish(
      makePayload({
        severity: "critical",
        surfaceOverride: "toast",
      }),
    );
    const [error] = bus.getSnapshot().errors as readonly PublishedError[];
    expect(error?.severity).toBe("critical");
    expect(error?.surface).toBe("toast");
  });

  test("identical (source + envelope.type) within COALESCE_WINDOW_MS coalesces into one entry", () => {
    bus.publish(makePayload());
    bus.publish(makePayload());
    bus.publish(makePayload());

    const errors = bus.getSnapshot().errors;
    expect(errors).toHaveLength(1);
    expect(errors[0]?.coalescedCount).toBe(3);
  });

  test("distinct errors do not coalesce", () => {
    bus.publish(makePayload({ source: "a" }));
    bus.publish(makePayload({ source: "b" }));

    const errors = bus.getSnapshot().errors;
    expect(errors).toHaveLength(2);
    expect(errors.every((entry) => entry.coalescedCount === 1)).toBe(true);
  });

  test("dismiss removes only the specified id", () => {
    const id1 = bus.publish(makePayload({ source: "a" }));
    bus.publish(makePayload({ source: "b" }));

    bus.dismiss(id1);

    const errors = bus.getSnapshot().errors;
    expect(errors).toHaveLength(1);
    expect(errors[0]?.source).toBe("b");
  });

  test("dismissAll empties the bus", () => {
    bus.publish(makePayload({ source: "a" }));
    bus.publish(makePayload({ source: "b" }));
    bus.dismissAll();
    expect(bus.getSnapshot().errors).toHaveLength(0);
  });

  test("subscribe receives an immediate snapshot then incremental updates; unsubscribe stops updates", () => {
    const calls: number[] = [];
    const unsubscribe = bus.subscribe((errors) => {
      calls.push(errors.length);
    });

    bus.publish(makePayload({ source: "x" }));
    bus.publish(
      makePayload({ source: "y", envelope: { ...baseEnvelope, type: "https://hoopoe.io/problems/y" } }),
    );

    unsubscribe();
    bus.publish(makePayload({ source: "z" }));

    expect(calls).toEqual([0, 1, 2]);
  });

  test("default dismissibility is true except for blocking-modal + blocking severity", () => {
    bus.publish(
      makePayload({
        severity: "blocking",
        surfaceOverride: "blocking_modal",
      }),
    );
    const [blocking] = bus.getSnapshot().errors;
    expect(blocking?.hints?.dismissible).toBe(false);

    bus.publish(makePayload({ source: "info-source", severity: "info", surfaceOverride: "toast" }));
    const errors = bus.getSnapshot().errors;
    const info = errors.find((entry) => entry.source === "info-source");
    expect(info?.hints?.dismissible).toBe(true);
  });

  test("toast surface gets a default autoDismissMs derived from severity; non-toast surfaces don't", () => {
    bus.publish(
      makePayload({
        source: "toast-source",
        severity: "warning",
        surfaceOverride: "toast",
      }),
    );
    bus.publish(
      makePayload({
        source: "banner-source",
        envelope: { ...baseEnvelope, type: "https://hoopoe.io/problems/banner" },
        surfaceOverride: "banner",
      }),
    );
    const errors = bus.getSnapshot().errors;
    const toast = errors.find((entry) => entry.source === "toast-source");
    const banner = errors.find((entry) => entry.source === "banner-source");
    expect(toast?.hints?.autoDismissMs).toBe(7_000);
    expect(banner?.hints?.autoDismissMs).toBeUndefined();
  });

  test("explicit autoDismissMs is clamped into [1000, 30000]", () => {
    bus.publish(
      makePayload({
        source: "fast",
        surfaceOverride: "toast",
        hints: { autoDismissMs: 100 },
      }),
    );
    bus.publish(
      makePayload({
        source: "slow",
        envelope: { ...baseEnvelope, type: "https://hoopoe.io/problems/slow" },
        surfaceOverride: "toast",
        hints: { autoDismissMs: 100_000 },
      }),
    );
    const errors = bus.getSnapshot().errors;
    expect(errors.find((entry) => entry.source === "fast")?.hints?.autoDismissMs).toBe(1000);
    expect(errors.find((entry) => entry.source === "slow")?.hints?.autoDismissMs).toBe(30_000);
  });

  test("COALESCE_WINDOW_MS exposes the documented 2-second coalesce window", () => {
    expect(COALESCE_WINDOW_MS).toBe(2_000);
  });
});
