#!/usr/bin/env bun
// hp-411d Phase 1.5 — root `bun run e2e` orchestrator.
//
// Replaces the missing root `bun run e2e` script with a real runner that:
//   1. Detects whether the host can launch Playwright's bundled Chromium
//      (Linux: probe libgbm.so.1; macOS/Windows: always ready). If not,
//      emit a SINGLE deterministic skip line for both suites — never
//      one-fails-while-the-other-skips.
//   2. When ready: run the smoke suite (apps/desktop/tests/smoke/e2e/),
//      then the hp-j30 deeper suite. Surface aggregate pass/fail to the
//      shell exit code.
//   3. When not ready: print a structured skip envelope (suite, reason,
//      install hint) and exit 0 so CI on a barebones host stays green
//      while still telling humans what's missing.
//
// Acceptance from hp-411d: `bun run e2e` exists at repo root, runs both
// suites, and reports deterministic skip-or-pass. The CI host installs
// Playwright deps via .github/workflows/ci.yml; local dev hosts that
// don't have them skip.

import { spawnSync } from "node:child_process";
import * as Path from "node:path";
import { fileURLToPath } from "node:url";
import { detectChromiumHost } from "../../apps/desktop/src/test-utils/chromium-host.ts";

interface SuiteSpec {
  readonly name: string;
  readonly cwd: string;
  readonly args: readonly string[];
}

interface SuiteResult {
  readonly suite: string;
  readonly outcome: "passed" | "failed" | "skipped";
  readonly exitCode: number;
  readonly reason?: string;
}

const repoRoot = Path.resolve(Path.dirname(fileURLToPath(import.meta.url)), "..", "..");

const SUITES: readonly SuiteSpec[] = [
  {
    name: "desktop-smoke",
    cwd: repoRoot,
    args: ["x", "playwright", "test", "-c", "playwright.config.ts"],
  },
  {
    name: "hp-j30-desktop-shell",
    cwd: Path.join(repoRoot, "apps", "desktop"),
    args: ["x", "playwright", "test", "-c", "tests/e2e/hp-j30.playwright.config.ts"],
  },
];

function emit(envelope: Record<string, unknown>): void {
  process.stdout.write(`${JSON.stringify(envelope)}\n`);
}

function runSuite(suite: SuiteSpec): SuiteResult {
  emit({ event: "e2e.suite.start", suite: suite.name, cwd: suite.cwd });
  const result = spawnSync("bun", suite.args, {
    cwd: suite.cwd,
    stdio: "inherit",
    env: { ...process.env },
  });
  if (result.error) {
    emit({
      event: "e2e.suite.error",
      suite: suite.name,
      message: result.error.message,
    });
    return { suite: suite.name, outcome: "failed", exitCode: 1, reason: result.error.message };
  }
  const exitCode = result.status ?? 1;
  const outcome: SuiteResult["outcome"] = exitCode === 0 ? "passed" : "failed";
  emit({ event: "e2e.suite.end", suite: suite.name, outcome, exitCode });
  return { suite: suite.name, outcome, exitCode };
}

function main(): number {
  const hostStatus = detectChromiumHost();
  emit({
    event: "e2e.host.detected",
    platform: hostStatus.platform,
    ready: hostStatus.ready,
    reason: hostStatus.reason,
  });

  if (!hostStatus.ready) {
    // Skip-with-message for every suite — same reason for both — and exit
    // 0 so CI on a barebones host stays green. Humans see the install hint.
    for (const suite of SUITES) {
      emit({
        event: "e2e.suite.skipped",
        suite: suite.name,
        reason: hostStatus.reason,
      });
    }
    emit({
      event: "e2e.summary",
      total: SUITES.length,
      passed: 0,
      failed: 0,
      skipped: SUITES.length,
      hostReady: false,
    });
    return 0;
  }

  const results: SuiteResult[] = [];
  let firstFailureCode = 0;
  for (const suite of SUITES) {
    const result = runSuite(suite);
    results.push(result);
    if (result.outcome === "failed" && firstFailureCode === 0) {
      firstFailureCode = result.exitCode;
    }
  }

  emit({
    event: "e2e.summary",
    total: results.length,
    passed: results.filter((r) => r.outcome === "passed").length,
    failed: results.filter((r) => r.outcome === "failed").length,
    skipped: results.filter((r) => r.outcome === "skipped").length,
    hostReady: true,
  });

  return firstFailureCode;
}

process.exit(main());
