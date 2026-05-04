// hp-2qn — Malformed-adapter chaos primitive test.
//
// Verifies that every conformance-covered adapter ships a
// `malformed-json.json` golden fixture and that the bytes really
// don't parse. This guards the substrate the chaos suite uses to
// simulate a malformed adapter — if a fixture ever becomes valid
// JSON by accident, the chaos test would silently no-op.

import { describe, expect, test } from "bun:test";
import { resolve } from "node:path";
import {
  ChaosMalformedAdapterError,
  loadMalformedAdapterFixture,
  parseMalformedFixture,
} from "../../../src/test-utils/chaos/index.ts";

const REPO_ROOT = resolve(__dirname, "..", "..", "..", "..", "..");

// The conformance harness covers these adapters today (hp-8a8); they
// must each ship a malformed-json.json fixture for the chaos suite to
// drive their degraded-mode path. `rch` is intentionally excluded
// because its golden-outputs/ directory is empty by design (the
// conformance harness probes the local rch instead — see
// `EXPECTED_FINDING_IDS.rch` in packages/fixtures/conformance/harness.ts).
const COVERED_ADAPTERS = [
  "br",
  "bv",
  "ntm",
  "agent_mail",
  "git",
  "ru",
  "caam",
  "dcg",
  "caut",
  "health",
  "casr",
  "pt",
  "srp",
  "sbh",
  "ubs",
  "jsm",
  "jfp",
  "oracle",
] as const;

describe("hp-2qn :: malformed-adapter primitive", () => {
  test("every conformance-covered adapter has a malformed-json.json fixture", () => {
    for (const tool of COVERED_ADAPTERS) {
      const fixture = loadMalformedAdapterFixture({ tool, repoRoot: REPO_ROOT });
      expect(fixture.tool).toBe(tool);
      expect(fixture.path.endsWith("malformed-json.json")).toBe(true);
      expect(fixture.rawText.length).toBeGreaterThan(0);
    }
  });

  test("malformed fixtures actually fail to parse cleanly (sanity check)", () => {
    // The fixture envelopes themselves are valid JSON wrappers; the
    // CONTENT they describe (stdout, captures) is what must look
    // malformed to downstream parsers. We check that either:
    //  - the envelope itself doesn't parse, OR
    //  - the envelope parses but its captured-stdout content is
    //    flagged as malformed via the fixture's own metadata
    //    (`meta.state === 'malformed-json'`).
    for (const tool of COVERED_ADAPTERS) {
      const fixture = loadMalformedAdapterFixture({ tool, repoRoot: REPO_ROOT });
      const envelopeParse = parseMalformedFixture(fixture);
      if (envelopeParse.ok === false && envelopeParse.error.includes("parsed cleanly")) {
        // The wrapper parsed, so check the meta.state marker.
        const parsed = JSON.parse(fixture.rawText) as { meta?: { state?: string } };
        expect(parsed.meta?.state).toBe("malformed-json");
      }
    }
  });

  test("throws on unknown tool with helpful message", () => {
    let caught: unknown = null;
    try {
      loadMalformedAdapterFixture({ tool: "does-not-exist", repoRoot: REPO_ROOT });
    } catch (err) {
      caught = err;
    }
    expect(caught).toBeInstanceOf(ChaosMalformedAdapterError);
    expect((caught as Error).message).toContain("does-not-exist");
  });
});
