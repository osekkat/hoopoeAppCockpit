import { describe, expect, test } from "bun:test";
import { ALLOWED_LITERALS, computeRedactionStats } from "../src/index.ts";

describe("hp-6sv :: redaction", () => {
  test("counts provider env-var leaks", () => {
    const text = "user set OPENAI_API_KEY in shell; also exported ANTHROPIC_API_KEY";
    const stats = computeRedactionStats(text);
    expect(stats.patternsMatched["provider-env-var"]).toBe(2);
  });

  test("counts OpenAI-shaped keys but skips short prefixes", () => {
    const stats = computeRedactionStats("token=sk-abcdefghijklmnopqrstuvwxy");
    expect(stats.patternsMatched["openai-key-shape"]).toBe(1);
  });

  test("counts JWT-shaped tokens", () => {
    const text = "Bearer eyJhbGciOiJIUzI1NiJ9.eyJzdWIiOiIxMjM0NTY3ODkwIn0.abcdefghij1234567890";
    const stats = computeRedactionStats(text);
    expect(stats.patternsMatched["jwt-shape"] ?? 0).toBeGreaterThanOrEqual(1);
    expect(stats.patternsMatched["bearer-header"] ?? 0).toBeGreaterThanOrEqual(1);
  });

  test("skips allow-listed mock literals", () => {
    const text = ALLOWED_LITERALS.map((l) => `Bearer ${l}-suffix-padding-padding`).join(" ");
    const stats = computeRedactionStats(text);
    // None of the bearer tokens should trip the bearer-header rule because
    // they all CONTAIN one of the allow-listed mock literals.
    expect(stats.patternsMatched["bearer-header"] ?? 0).toBe(0);
  });

  test("returns empty patternsMatched on clean text", () => {
    const stats = computeRedactionStats("nothing to see here");
    expect(stats.patternsMatched).toEqual({});
  });
});
