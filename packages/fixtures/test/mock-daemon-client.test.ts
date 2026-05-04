import { describe, expect, test } from "bun:test";
import {
  createMockDaemonClient,
  MOCK_FLYWHEEL_HEALTH_TIME,
} from "../src/index.ts";

describe("mock-flywheel client.health() (hp-2szb)", () => {
  test("returns the deterministic fixed timestamp when no clock is injected", () => {
    // Mock Flywheel Mode is supposed to be deterministic enough for CI
    // and release smoke. With no `options.now` override, every call to
    // health() must return the same fixed ISO string instead of leaking
    // wall-clock state into snapshot tests, fixture consumers, or the
    // committed goldens.
    const client = createMockDaemonClient({ scenarioId: "healthy-hour" });
    const first = client.health();
    const second = client.health();
    expect(first.time).toBe(MOCK_FLYWHEEL_HEALTH_TIME);
    expect(second.time).toBe(MOCK_FLYWHEEL_HEALTH_TIME);
    expect(first.status).toBe("ok");
    expect(first.environment).toBe("mock-flywheel");
  });

  test("uses options.now when injected (epoch ms → ISO)", () => {
    // Tests that need a non-default anchor (e.g. clock-advance scenarios
    // for replay timing) can pass `options.now`; the client converts the
    // returned epoch-ms value into an ISO string.
    const fixedEpochMs = Date.UTC(2027, 0, 15, 12, 30, 45, 250);
    const client = createMockDaemonClient({
      scenarioId: "healthy-hour",
      now: () => fixedEpochMs,
    });
    expect(client.health().time).toBe("2027-01-15T12:30:45.250Z");
  });

  test("multiple clients with different injected clocks do not bleed across", () => {
    // Defense-in-depth: there is no module-global wall-clock state that
    // a second client could observe.
    const earlyClient = createMockDaemonClient({
      scenarioId: "healthy-hour",
      now: () => Date.UTC(2026, 0, 1, 0, 0, 0, 0),
    });
    const lateClient = createMockDaemonClient({
      scenarioId: "healthy-hour",
      now: () => Date.UTC(2099, 11, 31, 23, 59, 59, 999),
    });
    expect(earlyClient.health().time).toBe("2026-01-01T00:00:00.000Z");
    expect(lateClient.health().time).toBe("2099-12-31T23:59:59.999Z");
  });
});
