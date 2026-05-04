import { readFileSync } from "node:fs";
import { describe, expect, test } from "bun:test";
import { goldenOutputPath, type GoldenOutputFixture } from "../src/index.ts";
import { assertToolConformance } from "./harness.ts";

function readGoldenOutput(path: string): GoldenOutputFixture {
  try {
    return JSON.parse(readFileSync(path, "utf8")) as GoldenOutputFixture;
  } catch (error) {
    throw new Error(
      `failed to parse rch unsupported-version golden at ${path}: ${
        error instanceof Error ? error.message : String(error)
      }`,
    );
  }
}

describe("rch adapter contract conformance", () => {
  test("normal, round-trip, negative, and capability cases match the contract", () => {
    assertToolConformance("rch");
  });

  test("unsupported-version fixture degrades rch.run capability", () => {
    const path = goldenOutputPath("rch", "unsupported-version");
    const fixture = readGoldenOutput(path);

    expect(fixture.meta).toMatchObject({
      adapter: "rch",
      state: "unsupported-version",
    });
    expect(fixture.exit).toBe(0);
    expect(fixture.stdoutText).toContain("Posture : unsupported-version");
    expect(fixture.capabilities?.["rch.run"]).toMatchObject({
      status: "degraded",
      notes: "version below supported minimum",
    });
  });
});
