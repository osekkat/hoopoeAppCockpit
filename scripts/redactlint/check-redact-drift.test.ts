import { readFileSync } from "node:fs";
import path from "node:path";
import { describe, expect, test } from "bun:test";
import { Redactor } from "../../apps/desktop/src/shared/redact/redact.ts";
import {
  checkDrift,
  findTsTestReferencedIds,
  parseGoFuzzFixtureIds,
  parseGoPatternIds,
} from "./check-redact-drift.ts";

const GO_REDACT_FILE = path.join(
  process.cwd(),
  "apps/daemon/internal/redaction/patterns.go",
);
const GO_FUZZ_TEST_FILE = path.join(
  process.cwd(),
  "apps/daemon/internal/redaction/redaction_fuzz_test.go",
);
const TS_TEST_FILE = path.join(
  process.cwd(),
  "apps/desktop/src/shared/redact/redact.test.ts",
);

describe("parseGoPatternIds", () => {
  test("extracts every id literal", () => {
    const source = readFileSync(GO_REDACT_FILE, "utf8");
    const ids = parseGoPatternIds(source);
    expect(ids.length).toBeGreaterThan(0);
    // First entry must be http-header-authorization (the canonical
    // hp-je1p package leads with header redaction).
    expect(ids[0]).toBe("http-header-authorization");
  });

  test("synthetic source extracts every id", () => {
    const source = `
      func defaultPatterns() []redactionPattern {
        return []redactionPattern{
          { id: "alpha", regex: nil, replace: nil },
          { id: "beta", regex: nil, replace: nil },
          { id: "gamma", regex: nil, replace: nil },
        }
      }
    `;
    expect(parseGoPatternIds(source)).toEqual(["alpha", "beta", "gamma"]);
  });
});

describe("parseGoFuzzFixtureIds", () => {
  test("extracts every fuzz fixture id literal", () => {
    const source = readFileSync(GO_FUZZ_TEST_FILE, "utf8");
    const ids = parseGoFuzzFixtureIds(source);
    expect(ids.length).toBeGreaterThan(0);
  });
});

describe("findTsTestReferencedIds", () => {
  test("reports IDs present as quoted literals", () => {
    const source = `
      expect(events.find((e) => e.patternId === "provider-key-sk")).toBeDefined();
      expect(events.find((e) => e.patternId === 'browser-cookie-claude-sessionkey')).toBeDefined();
      const ids = ["http-header-authorization", "made-up"];
    `;
    const found = findTsTestReferencedIds(source, [
      "provider-key-sk",
      "browser-cookie-claude-sessionkey",
      "http-header-authorization",
      "missing-pattern",
    ]);
    expect(found.has("provider-key-sk")).toBe(true);
    expect(found.has("browser-cookie-claude-sessionkey")).toBe(true);
    expect(found.has("http-header-authorization")).toBe(true);
    expect(found.has("missing-pattern")).toBe(false);
  });

  test("escapes regex meta-characters in pattern IDs", () => {
    // Pattern IDs are kebab-case in practice but the matcher must not
    // treat hyphens or dots as regex metacharacters by accident.
    const source = `const x = "foo.bar-baz";`;
    const found = findTsTestReferencedIds(source, ["foo.bar-baz"]);
    expect(found.has("foo.bar-baz")).toBe(true);
  });
});

describe("checkDrift (Go ↔ TS)", () => {
  test("Go and TS pattern IDs match exactly + in order (the contract)", () => {
    const goPatterns = readFileSync(GO_REDACT_FILE, "utf8");
    const goFuzzTest = readFileSync(GO_FUZZ_TEST_FILE, "utf8");
    const tsTest = readFileSync(TS_TEST_FILE, "utf8");
    const report = checkDrift(
      { goPatterns, goFuzzTest, tsTest },
      new Redactor(),
    );
    if (!report.ok) {
      const detail = [
        `goIds=${JSON.stringify(report.goIds)}`,
        `tsIds=${JSON.stringify(report.tsIds)}`,
        `missingFromTs=${JSON.stringify(report.missingFromTs)}`,
        `missingFromGo=${JSON.stringify(report.missingFromGo)}`,
        `missingFromGoFuzz=${JSON.stringify(report.missingFromGoFuzz)}`,
        `missingFromTsTests=${JSON.stringify(report.missingFromTsTests)}`,
        `orderMismatchAt=${report.orderMismatchAt}`,
      ].join("\n");
      throw new Error(`redact drift detected:\n${detail}`);
    }
    expect(report.ok).toBe(true);
    // Strict assertions for the test-coverage extension.
    expect(report.missingFromGoFuzz).toEqual([]);
    expect(report.missingFromTsTests).toEqual([]);
  });

  test("synthetic drift fails the check (legacy single-string signature)", () => {
    const goSource = `
      { id: "alpha", regex: nil, replace: nil },
      { id: "beta", regex: nil, replace: nil },
    `;
    // Build a fake Redactor with different IDs by stubbing patternIds().
    // Easier: import a real Redactor and just compare against synthetic Go.
    const realRedactor = new Redactor();
    const report = checkDrift(goSource, realRedactor);
    expect(report.ok).toBe(false);
    expect(report.missingFromGo.length).toBeGreaterThan(0);
  });

  test("missing fuzz fixture is reported as drift", () => {
    const goPatterns = `
      { id: "http-header-authorization", regex: nil, replace: nil },
    `;
    // Empty fuzz fixture file → every Go pattern flagged as missing.
    const tsTest = "";
    const report = checkDrift(
      { goPatterns, goFuzzTest: "", tsTest },
      new Redactor(),
    );
    expect(report.ok).toBe(false);
    // checkDrift only computes missingFromGoFuzz when goFuzzTest is
    // non-empty; with an empty string the check is skipped (legacy
    // contract). The synthetic drift in goPatterns vs TS already drives
    // ok=false above, so just assert the mechanism for skip.
    expect(report.missingFromGoFuzz).toEqual([]);

    // Now exercise it with a non-empty (but mismatched) fuzz file.
    const reportWithFuzz = checkDrift(
      { goPatterns, goFuzzTest: `{ id: "different-pattern" }`, tsTest },
      new Redactor(),
    );
    expect(reportWithFuzz.missingFromGoFuzz).toEqual(["http-header-authorization"]);
  });

  test("missing TS test reference is reported as drift", () => {
    const goPatterns = readFileSync(GO_REDACT_FILE, "utf8");
    const goFuzzTest = readFileSync(GO_FUZZ_TEST_FILE, "utf8");
    // Empty test source → every TS pattern flagged.
    const report = checkDrift(
      { goPatterns, goFuzzTest, tsTest: "// no pattern references here" },
      new Redactor(),
    );
    expect(report.missingFromTsTests.length).toBeGreaterThan(0);
    expect(report.ok).toBe(false);
  });
});
