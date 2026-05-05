import { readFileSync } from "node:fs";
import { describe, expect, test } from "bun:test";
import { goldenOutputPath, type GoldenOutputFixture } from "../src/index.ts";
import { assertToolConformance } from "./harness.ts";

function readGoldenOutput(path: string): GoldenOutputFixture {
  try {
    return JSON.parse(readFileSync(path, "utf8")) as GoldenOutputFixture;
  } catch (error) {
    throw new Error(
      `failed to parse git timeout golden at ${path}: ${
        error instanceof Error ? error.message : String(error)
      }`,
    );
  }
}

describe("git adapter contract conformance", () => {
  test("normal, round-trip, negative, and capability cases match the contract", () => {
    assertToolConformance("git");
  });

  test("timeout fixture degrades git timeout capability", () => {
    const fixture = readGoldenOutput(goldenOutputPath("git", "timeout"));

    expect(fixture.meta).toMatchObject({
      adapter: "git",
      state: "timeout",
    });
    expect(fixture.argv).toEqual(["git", "--json", "list"]);
    expect(fixture.exit).toBe(124);
    expect(fixture.stderrText).toContain("timeout");
    expect(fixture.capabilities?.["git._timeout"]).toMatchObject({
      status: "degraded",
      notes: "exceeded ENVELOPE_TIMEOUT_S; adapter must surface; do not retry without backoff",
    });
  });
});
