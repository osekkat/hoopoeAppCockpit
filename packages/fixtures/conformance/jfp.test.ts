import { describe, test } from "bun:test";
import { assertToolConformance } from "./harness.ts";

describe("jfp adapter contract conformance", () => {
  test("normal, round-trip, negative, and capability cases match the contract", () => {
    assertToolConformance("jfp");
  });
});
