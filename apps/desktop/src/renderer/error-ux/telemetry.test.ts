import { describe, expect, test } from "bun:test";
import {
  buildTelemetryEvent,
  InMemoryTelemetrySink,
  recordTelemetry,
  TELEMETRY_ALLOWED_KEYS,
} from "./telemetry.ts";
import type { PublishedError } from "./types.ts";

function publishedError(overrides?: Partial<PublishedError>): PublishedError {
  return {
    id: "err-42",
    source: "plan-generate",
    severity: "warning",
    surface: "banner",
    publishedAt: 0,
    coalescedCount: 1,
    envelope: {
      type: "https://hoopoe.io/problems/plan-generate-failed",
      title: "Plan generation failed",
      status: 422,
      surface: "banner",
      actionability: "open-docs",
      user_message: "We couldn't generate a candidate plan. See docs for retry steps.",
      detail: "Underlying CLI returned exit code 7 with stderr: secret-stderr-string",
      instance: "/v1/plans/secret-plan-id/generate",
      correlation_id: "01JEMC4Z6XR0V8X5C9YV0RXB7H",
      cyclePath: ["secret-bead-id-1", "secret-bead-id-2"],
    },
    context: { planId: "secret-plan-id", projectPath: "/Users/secret/path" },
    ...overrides,
  };
}

const fixedClock = () => new Date("2026-05-04T01:23:45.678Z");

describe("hp-8dym :: telemetry", () => {
  test("buildTelemetryEvent contains only allowed keys (no PII fields)", () => {
    const event = buildTelemetryEvent(publishedError(), fixedClock);
    const keys = Object.keys(event);
    for (const key of keys) {
      expect(TELEMETRY_ALLOWED_KEYS.has(key)).toBe(true);
    }
    expect(keys.toSorted()).toEqual(
      ["actionability", "problemType", "severity", "source", "surface", "ts", "type"].toSorted(),
    );
  });

  test("buildTelemetryEvent never copies envelope.detail / instance / extensions or context", () => {
    const event = buildTelemetryEvent(publishedError(), fixedClock);
    const json = JSON.stringify(event);
    expect(json).not.toContain("secret-stderr-string");
    expect(json).not.toContain("secret-plan-id");
    expect(json).not.toContain("secret-bead-id-1");
    expect(json).not.toContain("01JEMC4Z6XR0V8X5C9YV0RXB7H");
    expect(json).not.toContain("/Users/secret/path");
  });

  test("recordTelemetry skips writing when telemetry is disabled", () => {
    const sink = new InMemoryTelemetrySink();
    const result = recordTelemetry(publishedError(), {
      enabled: false,
      sink,
      clock: fixedClock,
    });
    expect(result).toBeNull();
    expect(sink.drain()).toHaveLength(0);
  });

  test("recordTelemetry writes a redacted event when enabled", () => {
    const sink = new InMemoryTelemetrySink();
    const event = recordTelemetry(publishedError(), {
      enabled: true,
      sink,
      clock: fixedClock,
    });
    expect(event).not.toBeNull();
    expect(sink.drain()).toHaveLength(1);
    const written = sink.drain()[0]!;
    expect(written.problemType).toBe("https://hoopoe.io/problems/plan-generate-failed");
    expect(written.severity).toBe("warning");
    expect(written.actionability).toBe("open-docs");
    expect(written.surface).toBe("banner");
    expect(written.source).toBe("plan-generate");
    expect(written.ts).toBe("2026-05-04T01:23:45.678Z");
  });
});
