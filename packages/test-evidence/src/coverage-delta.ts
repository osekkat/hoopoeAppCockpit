// `@hoopoe/test-evidence` — coverage-delta utility (hp-6sv).
//
// Reads a coverage summary file (vitest / Bun coverage `coverage-summary.json`
// shape, or LCOV) and either:
//   - returns the absolute coverage block, or
//   - subtracts a baseline (typically `main` branch coverage) and returns the
//     delta block.
//
// We support two input formats:
//   1. The Istanbul-flavored `coverage-summary.json` shape:
//        { "total": { "statements": {pct}, "branches": {pct}, ... }, "<file>": {...} }
//   2. Plain LCOV (line records with `LF:` / `LH:` / `BRF:` / `BRH:` /
//      `FNF:` / `FNH:`). When LCOV is the input, branches/lines/etc. are
//      computed from the file-level totals.

import { readFileSync } from "node:fs";
import type { CoverageBlock } from "./envelope.ts";

export interface LoadCoverageOptions {
  /** Path to either a `coverage-summary.json` or a `.lcov` / `lcov.info` file. */
  path: string;
}

interface IstanbulSummary {
  total?: {
    statements?: { pct?: number };
    branches?: { pct?: number };
    lines?: { pct?: number };
    functions?: { pct?: number };
  };
}

function loadIstanbul(text: string): CoverageBlock | null {
  let parsed: IstanbulSummary;
  try {
    parsed = JSON.parse(text) as IstanbulSummary;
  } catch {
    return null;
  }
  const total = parsed.total;
  if (total === undefined) return null;
  return {
    statements: total.statements?.pct ?? 0,
    branches: total.branches?.pct ?? 0,
    lines: total.lines?.pct ?? 0,
    functions: total.functions?.pct ?? 0,
  };
}

function loadLcov(text: string): CoverageBlock | null {
  let lf = 0;
  let lh = 0;
  let brf = 0;
  let brh = 0;
  let fnf = 0;
  let fnh = 0;
  let recordsSeen = 0;
  for (const line of text.split("\n")) {
    const trimmed = line.trim();
    if (trimmed.length === 0) continue;
    const colonIdx = trimmed.indexOf(":");
    if (colonIdx <= 0) continue;
    const key = trimmed.slice(0, colonIdx);
    const value = trimmed.slice(colonIdx + 1);
    const numeric = Number(value);
    if (!Number.isFinite(numeric)) continue;
    if (key === "LF") lf += numeric;
    else if (key === "LH") lh += numeric;
    else if (key === "BRF") brf += numeric;
    else if (key === "BRH") brh += numeric;
    else if (key === "FNF") fnf += numeric;
    else if (key === "FNH") fnh += numeric;
    else if (key === "end_of_record") recordsSeen += 1;
  }
  if (recordsSeen === 0 && lf === 0) return null;
  // LCOV doesn't separate "statements" from "lines"; we report the same
  // value for both so downstream consumers don't have to special-case.
  const statements = lf === 0 ? 0 : (lh / lf) * 100;
  const branches = brf === 0 ? 0 : (brh / brf) * 100;
  const functions = fnf === 0 ? 0 : (fnh / fnf) * 100;
  return {
    statements,
    branches,
    lines: statements,
    functions,
  };
}

/** Load a coverage summary from disk. Auto-detects Istanbul JSON vs LCOV. */
export function loadCoverageSummary(options: LoadCoverageOptions): CoverageBlock | null {
  let text: string;
  try {
    text = readFileSync(options.path, "utf8");
  } catch {
    return null;
  }
  const trimmed = text.trim();
  if (trimmed.startsWith("{")) {
    return loadIstanbul(text);
  }
  return loadLcov(text);
}

/** Compute `current - baseline` per metric. Both blocks must be present. */
export function computeDelta(
  current: CoverageBlock,
  baseline: CoverageBlock,
): NonNullable<CoverageBlock["deltaVsMain"]> {
  return {
    statements: round2(current.statements - baseline.statements),
    branches: round2(current.branches - baseline.branches),
    lines: round2(current.lines - baseline.lines),
    functions: round2(current.functions - baseline.functions),
  };
}

function round2(value: number): number {
  return Math.round(value * 100) / 100;
}

/** Convenience: load both files and return a CoverageBlock with the delta
 *  embedded. Returns `null` if `currentPath` cannot be read. */
export function buildCoverageBlock(
  currentPath: string,
  baselinePath?: string,
): CoverageBlock | null {
  const current = loadCoverageSummary({ path: currentPath });
  if (current === null) return null;
  if (baselinePath === undefined) return current;
  const baseline = loadCoverageSummary({ path: baselinePath });
  if (baseline === null) return current;
  return { ...current, deltaVsMain: computeDelta(current, baseline) };
}
