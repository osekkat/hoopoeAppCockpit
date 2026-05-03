import { describe, test } from "bun:test";
import { assertToolConformance } from "./harness.ts";

// hp-8a8: single-tool conformance for `health`. Per-language split
// (`health_ts`, `health_python`, `health_rust`, `health_go`,
// `health_generic`) is deferred until per-language fixtures land
// alongside the real-VPS pinning bead (hp-r7i). The capability
// registry already namespaces with `health_<lang>` (per hp-r33), so
// this test asserts on the umbrella adapter today and a follow-up
// will fan out per-language assertions.
describe("health adapter contract conformance", () => {
  test("normal, round-trip, negative, and capability cases match the contract", () => {
    assertToolConformance("health");
  });
});
