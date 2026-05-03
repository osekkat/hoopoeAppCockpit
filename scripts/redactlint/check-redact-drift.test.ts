import { readFileSync } from "node:fs";
import path from "node:path";
import { describe, expect, test } from "bun:test";
import { Redactor } from "../../apps/desktop/src/shared/redact/redact.ts";
import { checkDrift, parseGoPatternIds } from "./check-redact-drift.ts";

const GO_REDACT_FILE = path.join(
  process.cwd(),
  "apps/daemon/internal/redaction/patterns.go",
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

describe("checkDrift (Go ↔ TS)", () => {
  test("Go and TS pattern IDs match exactly + in order (the contract)", () => {
    const source = readFileSync(GO_REDACT_FILE, "utf8");
    const report = checkDrift(source, new Redactor());
    if (!report.ok) {
      const detail = [
        `goIds=${JSON.stringify(report.goIds)}`,
        `tsIds=${JSON.stringify(report.tsIds)}`,
        `missingFromTs=${JSON.stringify(report.missingFromTs)}`,
        `missingFromGo=${JSON.stringify(report.missingFromGo)}`,
        `orderMismatchAt=${report.orderMismatchAt}`,
      ].join("\n");
      throw new Error(`redact drift detected:\n${detail}`);
    }
    expect(report.ok).toBe(true);
  });

  test("synthetic drift fails the check", () => {
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
});
