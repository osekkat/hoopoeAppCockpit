#!/usr/bin/env bun
// hp-6sv — Desktop Bun-test wrapper that emits a test-evidence envelope.
//
// Usage:
//   bun apps/desktop/scripts/test-evidence/run-bun.ts -- <bun test args>
//
// Examples:
//   bun apps/desktop/scripts/test-evidence/run-bun.ts -- src tests/unit/*.test.ts
//   bun apps/desktop/scripts/test-evidence/run-bun.ts -- tests/replay/*.test.ts
//
// Env knobs:
//   HOOPOE_TEST_EVIDENCE_PHASE   — phase tag (default "phase2")
//   HOOPOE_DAEMON_VERSION        — embedded into envelope.daemonVersion
//   HOOPOE_FIXTURE_SCENARIO      — embedded into envelope.fixtureScenario
//   HOOPOE_TEST_EVIDENCE_REPO    — repo root override (default: cwd)
//
// The wrapper exits with the underlying `bun test` exit code so CI sees
// failures normally.

import { mkdtempSync, readFileSync, rmSync } from "node:fs";
import { tmpdir } from "node:os";
import { join, resolve } from "node:path";
import { spawn } from "node:child_process";
import {
  buildEnvelope,
  collectStructuredLogLines,
  computeRedactionStats,
  evaluateAgainst,
  loadSloTargets,
  parseJunitXml,
  parseTags,
  readGitContext,
  sliceForCase,
  writeEvidence,
  type SloRollup,
  type SloTargets,
  type TestResult,
} from "@hoopoe/test-evidence";

interface CliArgs {
  bunArgs: readonly string[];
  repoRoot: string;
  phase: string;
  daemonVersion: string;
  fixtureScenario: string | null;
}

function parseArgs(): CliArgs {
  const argv = process.argv.slice(2);
  const dashIdx = argv.indexOf("--");
  const bunArgs = dashIdx >= 0 ? argv.slice(dashIdx + 1) : argv;
  const repoRoot = process.env.HOOPOE_TEST_EVIDENCE_REPO ?? process.cwd();
  return {
    bunArgs,
    repoRoot,
    phase: process.env.HOOPOE_TEST_EVIDENCE_PHASE ?? "phase2",
    daemonVersion: process.env.HOOPOE_DAEMON_VERSION ?? "unknown",
    fixtureScenario: process.env.HOOPOE_FIXTURE_SCENARIO ?? null,
  };
}

function loadSloOrNull(repoRoot: string): SloTargets | null {
  try {
    return loadSloTargets({ repoRoot });
  } catch {
    return null;
  }
}

function attachSloAndCleanNames(
  results: readonly TestResult[],
  sloTargets: SloTargets | null,
): TestResult[] {
  return results.map((r) => {
    const parsed = parseTags(r.name);
    const cleanName = parsed.cleanName.length > 0 ? parsed.cleanName : r.name;
    const next: TestResult = { ...r, name: cleanName };
    if (parsed.sloTarget !== null && sloTargets !== null) {
      const target = sloTargets.targets[parsed.sloTarget];
      if (target !== undefined) {
        const observed = r.durationMs;
        next.slo = {
          target: target.id,
          declared: target.declared,
          observed,
          passed: evaluateAgainst(target, observed),
        };
      }
    }
    return next;
  });
}

function attachLogSlices(
  results: readonly TestResult[],
  combinedOutput: string,
): TestResult[] {
  const collected = collectStructuredLogLines(combinedOutput);
  return results.map((r) => {
    const slice = sliceForCase(collected, r.name, r.classname);
    if (slice === undefined || slice.length === 0) return r;
    return { ...r, logSlice: slice };
  });
}

function buildSloRollup(results: readonly TestResult[], targetCount: number): SloRollup {
  const breached: SloRollup["breached"] = [];
  for (const r of results) {
    if (r.slo !== undefined && !r.slo.passed) {
      breached.push({
        target: r.slo.target,
        test: r.name,
        observed: r.slo.observed,
        declared: r.slo.declared,
      });
    }
  }
  return { targetsLoaded: targetCount, breached };
}

async function main(): Promise<number> {
  const args = parseArgs();
  if (args.bunArgs.length === 0) {
    process.stderr.write(
      "[run-bun] no bun-test arguments supplied. Pass them after --, e.g. " +
        "`bun run-bun.ts -- src tests/unit/*.test.ts`\n",
    );
    return 2;
  }

  const tmpDir = mkdtempSync(join(tmpdir(), "hoopoe-test-evidence-"));
  const junitPath = join(tmpDir, "bun-junit.xml");

  const child = spawn(
    "bun",
    [
      "test",
      "--reporter=junit",
      `--reporter-outfile=${junitPath}`,
      ...args.bunArgs,
    ],
    { stdio: ["ignore", "pipe", "pipe"], env: { ...process.env, FORCE_COLOR: "0" } },
  );

  let stdout = "";
  let stderr = "";
  child.stdout.on("data", (chunk: Buffer) => {
    const text = chunk.toString("utf8");
    stdout += text;
    process.stdout.write(text);
  });
  child.stderr.on("data", (chunk: Buffer) => {
    const text = chunk.toString("utf8");
    stderr += text;
    process.stderr.write(text);
  });

  const exitCode: number = await new Promise((resolveExit) => {
    child.on("close", (code) => resolveExit(code ?? 1));
  });

  let xml = "";
  try {
    xml = readFileSync(junitPath, "utf8");
  } catch {
    process.stderr.write("[run-bun] WARNING: junit XML missing; envelope will be results-empty\n");
  }
  const parsed = xml.length > 0 ? parseJunitXml(xml) : { testCount: 0, cases: [] };
  const sloTargets = loadSloOrNull(args.repoRoot);
  const enrichedSlo = attachSloAndCleanNames(parsed.cases, sloTargets);
  const enriched = attachLogSlices(enrichedSlo, `${stdout}\n${stderr}`);
  const slo = buildSloRollup(
    enriched,
    sloTargets === null ? 0 : Object.keys(sloTargets.targets).length,
  );
  const git = readGitContext({ cwd: args.repoRoot });
  const envelope = buildEnvelope({
    gitSha: git.sha,
    gitDirty: git.dirty,
    daemonVersion: args.daemonVersion,
    fixtureScenario: args.fixtureScenario,
    runner: "bun-test",
    phase: args.phase,
    results: enriched,
    redactionStats: computeRedactionStats(`${stdout}\n${stderr}`),
    slo,
  });
  const written = await writeEvidence(envelope, { repoRoot: args.repoRoot });

  // Diagnostics line + breach summary so CI logs are useful.
  process.stdout.write(`[run-bun] envelope: ${written.relativePath} (${envelope.results.length} cases)\n`);
  if (slo.breached.length > 0) {
    process.stdout.write(`[run-bun] SLO breaches: ${slo.breached.length}\n`);
    for (const b of slo.breached.slice(0, 10)) {
      process.stdout.write(`  - ${b.target}: ${b.test} observed=${b.observed} declared=${b.declared}\n`);
    }
  }

  rmSync(tmpDir, { recursive: true, force: true });
  return exitCode;
}

const code = await main();
// Match `bun test`'s exit semantics: non-zero on test failure.
process.exit(code);

// Suppress "expression unused" if bundlers complain about the resolve import.
void resolve;
