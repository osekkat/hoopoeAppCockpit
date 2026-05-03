#!/usr/bin/env bun
// hp-6sv — Daemon `go test -json` wrapper that emits a test-evidence envelope.
//
// Usage:
//   bun apps/daemon/scripts/test-evidence/run-go.ts -- <go test args>
//
// Examples:
//   bun apps/daemon/scripts/test-evidence/run-go.ts -- ./...
//   bun apps/daemon/scripts/test-evidence/run-go.ts -- -run TestRegistry ./internal/capabilities/
//
// Env knobs:
//   HOOPOE_TEST_EVIDENCE_PHASE   — phase tag (default "phase2")
//   HOOPOE_DAEMON_VERSION        — embedded into envelope.daemonVersion
//   HOOPOE_FIXTURE_SCENARIO      — embedded into envelope.fixtureScenario
//   HOOPOE_TEST_EVIDENCE_REPO    — repo root override (default: cwd)
//
// If `go` isn't on PATH or `apps/daemon/` doesn't compile, the wrapper
// still emits a `runner=go-test, status=skipped` envelope so the
// evidence directory is consistent.

import { spawn } from "node:child_process";
import { existsSync } from "node:fs";
import { join, resolve } from "node:path";
import {
  buildEnvelope,
  computeRedactionStats,
  parseGoTestNdjson,
  readGitContext,
  writeEvidence,
  type RunnerId,
  type TestResult,
} from "@hoopoe/test-evidence";

interface CliArgs {
  goArgs: readonly string[];
  repoRoot: string;
  phase: string;
  daemonVersion: string;
  fixtureScenario: string | null;
  daemonDir: string;
}

const RUNNER: RunnerId = "go-test";

function parseArgs(): CliArgs {
  const argv = process.argv.slice(2);
  const dashIdx = argv.indexOf("--");
  const goArgs = dashIdx >= 0 ? argv.slice(dashIdx + 1) : argv;
  const repoRoot = process.env.HOOPOE_TEST_EVIDENCE_REPO ?? process.cwd();
  return {
    goArgs: goArgs.length > 0 ? goArgs : ["./..."],
    repoRoot,
    phase: process.env.HOOPOE_TEST_EVIDENCE_PHASE ?? "phase2",
    daemonVersion: process.env.HOOPOE_DAEMON_VERSION ?? "unknown",
    fixtureScenario: process.env.HOOPOE_FIXTURE_SCENARIO ?? null,
    daemonDir: resolve(repoRoot, "apps", "daemon"),
  };
}

interface RunSummary {
  exitCode: number;
  results: TestResult[];
  buildErrors: readonly string[];
  rawNdjson: string;
  rawStderr: string;
  skipReason?: string;
}

function commandAvailable(cmd: string): boolean {
  const probe = require("node:child_process").spawnSync(cmd, ["version"], {
    stdio: "ignore",
  });
  return probe.status === 0 || probe.status === 1; // `go version` succeeds; some tools return 1 on bare invocation
}

async function runGoTest(args: CliArgs): Promise<RunSummary> {
  if (!existsSync(args.daemonDir)) {
    return {
      exitCode: 0,
      results: [],
      buildErrors: [],
      rawNdjson: "",
      rawStderr: "",
      skipReason: `apps/daemon/ does not exist at ${args.daemonDir}`,
    };
  }
  if (!existsSync(join(args.daemonDir, "go.mod"))) {
    return {
      exitCode: 0,
      results: [],
      buildErrors: [],
      rawNdjson: "",
      rawStderr: "",
      skipReason: "apps/daemon/go.mod missing — daemon not initialized yet",
    };
  }
  if (!commandAvailable("go")) {
    return {
      exitCode: 0,
      results: [],
      buildErrors: [],
      rawNdjson: "",
      rawStderr: "",
      skipReason: "`go` binary not on PATH",
    };
  }

  const child = spawn("go", ["test", "-json", ...args.goArgs], {
    cwd: args.daemonDir,
    stdio: ["ignore", "pipe", "pipe"],
    env: { ...process.env },
  });

  let ndjson = "";
  let stderr = "";
  child.stdout.on("data", (chunk: Buffer) => {
    const text = chunk.toString("utf8");
    ndjson += text;
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
  const parsed = parseGoTestNdjson(ndjson);
  return {
    exitCode,
    results: parsed.cases,
    buildErrors: parsed.buildErrors,
    rawNdjson: ndjson,
    rawStderr: stderr,
  };
}

async function main(): Promise<number> {
  const args = parseArgs();
  const summary = await runGoTest(args);
  const git = readGitContext({ cwd: args.repoRoot });
  const envelope = buildEnvelope({
    gitSha: git.sha,
    gitDirty: git.dirty,
    daemonVersion: args.daemonVersion,
    fixtureScenario: args.fixtureScenario,
    runner: RUNNER,
    phase: args.phase,
    results: summary.skipReason
      ? [
          {
            name: `(daemon) ${summary.skipReason}`,
            file: args.daemonDir,
            status: "skipped",
            durationMs: 0,
          },
        ]
      : summary.results,
    redactionStats: computeRedactionStats(`${summary.rawNdjson}\n${summary.rawStderr}`),
    artifacts: summary.buildErrors.length > 0 ? [`build-errors=${summary.buildErrors.length}`] : [],
  });
  const written = await writeEvidence(envelope, { repoRoot: args.repoRoot });
  process.stdout.write(
    `[run-go] envelope: ${written.relativePath} (${envelope.results.length} cases${
      summary.skipReason ? `, skipped: ${summary.skipReason}` : ""
    })\n`,
  );
  if (summary.buildErrors.length > 0) {
    process.stderr.write(`[run-go] build-errors:\n`);
    for (const e of summary.buildErrors.slice(0, 5)) process.stderr.write(`  - ${e}\n`);
  }
  return summary.exitCode;
}

const code = await main();
process.exit(code);
