import { describe, expect, test } from "bun:test";
import { collectStructuredLogLines, sliceForCase } from "../src/index.ts";

function line(suite: string, step: string, status: string, extra: Record<string, unknown> = {}): string {
  return JSON.stringify({
    ts: "2026-05-04T00:00:00.000Z",
    testId: extra.testId ?? "00000000-0000-0000-0000-000000000000",
    suite,
    step,
    durationMs: 1,
    status,
    ...extra,
  });
}

describe("hp-6sv :: log-collector", () => {
  test("buckets per-test slices by testId and by suite::step", () => {
    const text = [
      line("hp-j30.unit", "lifecycle", "started", { testId: "tid-1" }),
      line("hp-j30.unit", "lifecycle", "passed", { testId: "tid-1", durationMs: 12 }),
      line("hp-j30.unit", "settings-bridge.atomic-write", "started", { testId: "tid-2" }),
      line("hp-j30.unit", "settings-bridge.atomic-write", "passed", { testId: "tid-2", durationMs: 3 }),
      "non-json garbage",
      "",
    ].join("\n");
    const collected = collectStructuredLogLines(text);
    expect(Object.keys(collected.byTestId).sort()).toEqual(["tid-1", "tid-2"]);
    expect(collected.byTestId["tid-1"]?.length).toBe(2);
    expect(collected.bySuiteAndName["hp-j30.unit::lifecycle"]?.length).toBe(2);
    expect(collected.totalLines).toBeGreaterThan(0);
    expect(collected.nonJsonLines).toBe(0); // non-json line started with non-{ char and was skipped before parse
  });

  test("caps per-test slice length at maxPerTest", () => {
    const lines: string[] = [];
    for (let i = 0; i < 250; i++) {
      lines.push(line("burst", "step", "started", { testId: "tid-burst", durationMs: i }));
    }
    const collected = collectStructuredLogLines(lines.join("\n"), { maxPerTest: 50 });
    expect(collected.byTestId["tid-burst"]?.length).toBe(50);
  });

  test("sliceForCase returns the slice for a known suite+name pair, else undefined", () => {
    const text = [
      line("hp-j30.unit", "boots", "passed", { testId: "tid-3" }),
    ].join("\n");
    const collected = collectStructuredLogLines(text);
    const found = sliceForCase(collected, "boots", "hp-j30.unit");
    expect(found?.length).toBe(1);
    const missing = sliceForCase(collected, "nope", "hp-j30.unit");
    expect(missing).toBeUndefined();
  });
});
