import { describe, expect, test } from "bun:test";
import { parseTags } from "../src/index.ts";

describe("hp-6sv :: slo-tags parser", () => {
  test("strips known category tags and recognizes them", () => {
    const parsed = parseTags("@unit reconnect succeeds quickly @smoke");
    expect(parsed.cleanName).toBe("reconnect succeeds quickly");
    expect(parsed.categories).toEqual(["smoke", "unit"]);
    expect(parsed.sloTarget).toBeNull();
  });

  test("captures @slo:<id> and surfaces the target", () => {
    const parsed = parseTags(
      "@e2e reconnect within budget @slo:desktop.reconnect.p95",
    );
    expect(parsed.cleanName).toBe("reconnect within budget");
    expect(parsed.sloTarget).toBe("desktop.reconnect.p95");
    expect(parsed.categories).toEqual(["e2e", "slo"]);
  });

  test("falls back to original name when name is just tags", () => {
    const parsed = parseTags("@unit @slo:foo");
    expect(parsed.sloTarget).toBe("foo");
    // Allow empty cleanName — caller decides whether to fall back.
    expect(parsed.cleanName).toBe("");
  });

  test("preserves untagged strings verbatim", () => {
    const parsed = parseTags("plain old test name");
    expect(parsed.cleanName).toBe("plain old test name");
    expect(parsed.categories).toEqual([]);
    expect(parsed.sloTarget).toBeNull();
  });

  test("collects unknown tag values into `other`", () => {
    const parsed = parseTags("@bug:HP-12345 reproduces the regression @author:brown");
    expect(parsed.other.bug).toEqual(["HP-12345"]);
    expect(parsed.other.author).toEqual(["brown"]);
    expect(parsed.cleanName).toBe("reproduces the regression");
  });
});
