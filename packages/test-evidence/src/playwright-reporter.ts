// `@hoopoe/test-evidence` — Playwright Reporter that emits an evidence envelope (hp-6sv).
//
// Wire this in `playwright.config.ts` only when the env var
// `HOOPOE_TEST_EVIDENCE=1` is set, so the existing CI / dev configs are
// not disturbed:
//
//   import { defineConfig } from "@playwright/test";
//   export default defineConfig({
//     reporter: process.env.HOOPOE_TEST_EVIDENCE === "1"
//       ? [["@hoopoe/test-evidence/playwright-reporter"]]
//       : [["list"]],
//   });
//
// The reporter computes one envelope per Playwright run (across all
// projects) and writes it to `docs/test-evidence/<phase>/<ts>/playwright-<runId>.json`.

import type {
  FullConfig,
  FullResult,
  Reporter,
  Suite,
  TestCase,
  TestResult as PwTestResult,
} from "@playwright/test/reporter";
import { buildEnvelope, type TestResult } from "./envelope.ts";
import { readGitContext } from "./git-context.ts";
import { writeEvidence } from "./writer.ts";
import { parseTags } from "./slo-tags.ts";
import { evaluateAgainst, loadSloTargets, type SloTargets } from "./slo-targets.ts";

interface ReporterOptions {
  /** Override the phase tag (default: env `HOOPOE_TEST_EVIDENCE_PHASE` or `phase2`). */
  phase?: string;
  /** Override repo root (default: cwd). */
  repoRoot?: string;
  /** Override the daemon version string (default: env `HOOPOE_DAEMON_VERSION` or `unknown`). */
  daemonVersion?: string;
  /** Override the fixture scenario id (default: env `HOOPOE_FIXTURE_SCENARIO`). */
  fixtureScenario?: string;
}

function statusFromOutcome(outcome: PwTestResult["status"]): TestResult["status"] {
  if (outcome === "passed") return "passed";
  if (outcome === "skipped") return "skipped";
  return "failed";
}

class HoopoeEvidenceReporter implements Reporter {
  private readonly options: ReporterOptions;
  private readonly results: TestResult[] = [];
  private readonly artifacts: string[] = [];
  private sloTargets: SloTargets | null = null;

  constructor(options: ReporterOptions = {}) {
    this.options = options;
  }

  onBegin(_config: FullConfig, _suite: Suite): void {
    try {
      const opts = this.options.repoRoot !== undefined ? { repoRoot: this.options.repoRoot } : {};
      this.sloTargets = loadSloTargets(opts);
    } catch {
      this.sloTargets = null;
    }
  }

  onTestEnd(test: TestCase, result: PwTestResult): void {
    const parsed = parseTags(test.title);
    const status = statusFromOutcome(result.status);
    const durationMs = Math.max(0, Math.round(result.duration));
    const out: TestResult = {
      name: parsed.cleanName.length > 0 ? parsed.cleanName : test.title,
      file: test.location.file,
      status,
      durationMs,
    };
    if (result.error?.message !== undefined) {
      out.errorMessage = result.error.message;
    }
    if (parsed.sloTarget !== null && this.sloTargets !== null) {
      const target = this.sloTargets.targets[parsed.sloTarget];
      if (target !== undefined) {
        const observed = durationMs;
        const declared = target.target.kind === "boolean"
          ? String(target.target.expected)
          : target.target.declared;
        out.slo = {
          target: target.id,
          declared,
          observed,
          passed: evaluateAgainst(target, observed),
        };
      }
    }
    this.results.push(out);
    for (const att of result.attachments ?? []) {
      if (att.path !== undefined) this.artifacts.push(att.path);
    }
  }

  async onEnd(_result: FullResult): Promise<void> {
    const git = readGitContext(this.options.repoRoot !== undefined ? { cwd: this.options.repoRoot } : {});
    const phase =
      this.options.phase ?? process.env.HOOPOE_TEST_EVIDENCE_PHASE ?? "phase2";
    const daemonVersion =
      this.options.daemonVersion ?? process.env.HOOPOE_DAEMON_VERSION ?? "unknown";
    const fixtureScenario =
      this.options.fixtureScenario ?? process.env.HOOPOE_FIXTURE_SCENARIO ?? null;
    const breaches = this.results
      .filter((r) => r.slo !== undefined && r.slo.passed === false)
      .map((r) => ({
        target: r.slo!.target,
        test: r.name,
        observed: r.slo!.observed,
        declared: r.slo!.declared,
      }));
    const envelope = buildEnvelope({
      gitSha: git.sha,
      gitDirty: git.dirty,
      daemonVersion,
      fixtureScenario,
      runner: "playwright",
      phase,
      results: this.results,
      artifacts: this.artifacts,
      slo: {
        targetsLoaded: this.sloTargets === null ? 0 : Object.keys(this.sloTargets.targets).length,
        breached: breaches,
      },
    });
    const written = await writeEvidence(
      envelope,
      this.options.repoRoot !== undefined ? { repoRoot: this.options.repoRoot } : {},
    );
    process.stdout.write(`[hoopoe-test-evidence] playwright envelope: ${written.relativePath}\n`);
  }
}

// Playwright loads reporters by `default export`.
export default HoopoeEvidenceReporter;
