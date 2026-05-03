import { describe, expect, test } from "bun:test";
import { mkdtempSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { buildCoverageBlock, computeDelta } from "../src/index.ts";

const ISTANBUL = JSON.stringify({
  total: {
    statements: { pct: 92.5 },
    branches: { pct: 88.0 },
    lines: { pct: 91.0 },
    functions: { pct: 95.0 },
  },
});

const ISTANBUL_BASELINE = JSON.stringify({
  total: {
    statements: { pct: 90.0 },
    branches: { pct: 85.0 },
    lines: { pct: 90.0 },
    functions: { pct: 92.0 },
  },
});

const LCOV = `TN:
SF:src/foo.ts
LF:10
LH:8
BRF:4
BRH:3
FNF:2
FNH:2
end_of_record
SF:src/bar.ts
LF:5
LH:5
BRF:0
BRH:0
FNF:1
FNH:1
end_of_record
`;

describe("hp-6sv :: coverage-delta", () => {
  test("loads Istanbul summary JSON", () => {
    const dir = mkdtempSync(join(tmpdir(), "hoopoe-cov-"));
    const path = join(dir, "coverage-summary.json");
    writeFileSync(path, ISTANBUL, "utf8");
    const block = buildCoverageBlock(path);
    expect(block?.statements).toBe(92.5);
    expect(block?.branches).toBe(88);
    expect(block?.lines).toBe(91);
    expect(block?.functions).toBe(95);
  });

  test("loads LCOV summary", () => {
    const dir = mkdtempSync(join(tmpdir(), "hoopoe-cov-"));
    const path = join(dir, "lcov.info");
    writeFileSync(path, LCOV, "utf8");
    const block = buildCoverageBlock(path);
    // 13 / 15 lines hit = 86.67%
    expect(block?.statements).toBeCloseTo(86.67, 1);
    expect(block?.branches).toBe(75);
    expect(block?.functions).toBe(100);
  });

  test("computes delta vs baseline when both files present", () => {
    const dir = mkdtempSync(join(tmpdir(), "hoopoe-cov-"));
    const cur = join(dir, "current.json");
    const base = join(dir, "baseline.json");
    writeFileSync(cur, ISTANBUL, "utf8");
    writeFileSync(base, ISTANBUL_BASELINE, "utf8");
    const block = buildCoverageBlock(cur, base);
    expect(block?.deltaVsMain).toEqual({
      statements: 2.5,
      branches: 3,
      lines: 1,
      functions: 3,
    });
  });

  test("computeDelta is symmetric numerically", () => {
    const a = { statements: 90, branches: 80, lines: 88, functions: 95 };
    const b = { statements: 85, branches: 80, lines: 85, functions: 90 };
    const delta = computeDelta(a, b);
    expect(delta).toEqual({ statements: 5, branches: 0, lines: 3, functions: 5 });
  });

  test("returns null when current file is missing", () => {
    expect(buildCoverageBlock("/no/such/file.json")).toBeNull();
  });
});
