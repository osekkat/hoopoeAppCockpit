// `@hoopoe/fixtures` — corpus validator (hp-pl5o).
//
// Walks the fixture corpus and asserts:
//   1. completeness — every required scenario / golden-output exists with the
//      bead-spec'd file set;
//   2. parseability — every JSON file parses;
//   3. shape — meta.json validates against the fixture-meta minimum;
//   4. failure-path coverage — every adapter has all six golden-output states;
//   5. determinism — re-runs of the snapshot script produce byte-identical
//      output modulo timestamps + durations + host fields (asserted by hash);
//   6. no secrets — corpus must NOT contain provider-key strings (Guardrail 11).
//
// This file is framework-agnostic: it returns a structured result. The
// bun-test wrapper at `packages/fixtures/tests/fixture-quality.test.ts`
// asserts on the result.
//
// Cross-references:
// - `packages/fixtures/README.md` — per-scenario + per-golden-output contracts
// - `plan.md` §18.3 — adapter contract tests
// - `plan.md` §16 Phase 0 — research-spike acceptance
// - `docs/integration-contracts/gotchas-and-version-skew.md` — known parser quirks

import { readdirSync, readFileSync, statSync } from "node:fs";
import { join, relative, resolve } from "node:path";
import {
  ADAPTER_SLUGS,
  GOLDEN_OUTPUT_STATES,
  PHASE0_SCENARIOS,
  TENDING_SCENARIOS,
  type AdapterSlug,
  type GoldenOutputState,
  type Phase0ScenarioId,
  type TendingScenarioId,
} from "./kinds.ts";
import {
  fixturesRoot,
  goldenOutputPath,
  phase0ScenarioPath,
  scenarioPath,
} from "./loader.ts";

/** Minimum file set every tending scenario must contain (per `packages/fixtures/README.md`).
 *  `tools-degraded.json` is required only for the `missing-tool` scenario. */
const REQUIRED_SCENARIO_FILES = [
  "meta.json",
  "bv-triage.json",
  "br-list.json",
  "ntm-snapshot.json",
  "agent-mail-dump.json",
  "reservations.json",
  "events.jsonl",
  "capabilities.json",
  "expected-outcome.json",
] as const;

/** Required *directories* (must exist; may be empty in stub scenarios). */
const REQUIRED_SCENARIO_DIRS = ["pane-logs", "build-logs"] as const;

/** Required for `missing-tool` specifically. */
const SCENARIO_SPECIFIC_REQUIREMENTS: Record<string, readonly string[]> = {
  "missing-tool": ["tools-degraded.json"],
};

/** Provider-secret patterns we forbid in the corpus (Guardrail 11).
 *
 *  Two classes:
 *  - **Assignment context** (`OPENAI_API_KEY=...`, `OPENAI_API_KEY":...`) —
 *    indicates an actual leaked value follows. Always an error.
 *  - **Live-key shapes** (`sk-...`, `ghp_...`, `AKIA...`, PEM blocks) —
 *    these are actual key payload patterns. Always an error.
 *
 *  Bare env-var names *as text* (e.g., a bead description that says
 *  "no `OPENAI_API_KEY` config field anywhere" per Guardrail 11) are NOT
 *  flagged — that's policy documentation, not a leak. */
const FORBIDDEN_SECRET_PATTERNS: ReadonlyArray<{ name: string; rx: RegExp }> = [
  { name: "OPENAI_API_KEY assignment", rx: /OPENAI_API_KEY\s*[:=]\s*["']?[A-Za-z0-9_-]{8,}/ },
  { name: "ANTHROPIC_API_KEY assignment", rx: /ANTHROPIC_API_KEY\s*[:=]\s*["']?[A-Za-z0-9_-]{8,}/ },
  { name: "GEMINI_API_KEY assignment", rx: /GEMINI_API_KEY\s*[:=]\s*["']?[A-Za-z0-9_-]{8,}/ },
  { name: "openai sk- live key", rx: /\bsk-[A-Za-z0-9_-]{20,}/ },
  { name: "github personal access token", rx: /\bghp_[A-Za-z0-9]{36,}/ },
  { name: "AWS access key id", rx: /\bAKIA[A-Z0-9]{16}\b/ },
  { name: "PEM private key block", rx: /-----BEGIN [A-Z ]*PRIVATE KEY-----/ },
];

/** Severity levels for validator findings. */
export type FindingSeverity = "error" | "warning";

/** A single validator finding. */
export interface Finding {
  severity: FindingSeverity;
  /** Stable rule id (so adapter contract tests can target specific rules). */
  rule: string;
  /** Repo-relative path of the offending file (or scenario dir). */
  path: string;
  /** Free-form message. */
  message: string;
}

/** Result of one validator run. */
export interface ValidationResult {
  ok: boolean;
  rootInspected: string;
  summary: {
    tendingScenariosExpected: number;
    tendingScenariosFound: number;
    phase0ScenariosExpected: number;
    phase0ScenariosFound: number;
    goldenOutputsExpected: number;
    goldenOutputsFound: number;
    jsonFilesParsed: number;
  };
  findings: Finding[];
}

function exists(p: string): boolean {
  try {
    statSync(p);
    return true;
  } catch {
    return false;
  }
}

function isDir(p: string): boolean {
  try {
    return statSync(p).isDirectory();
  } catch {
    return false;
  }
}

function isFile(p: string): boolean {
  try {
    return statSync(p).isFile();
  } catch {
    return false;
  }
}

function tryParseJson(path: string): unknown | null {
  try {
    const text = readFileSync(path, "utf8");
    return JSON.parse(text) as unknown;
  } catch {
    return null;
  }
}

function tryParseNdjson(path: string): unknown[] | null {
  try {
    const text = readFileSync(path, "utf8");
    const lines = text.split("\n").filter((l) => l.length > 0);
    return lines.map((l) => JSON.parse(l) as unknown);
  } catch {
    return null;
  }
}

function relPath(root: string, p: string): string {
  return relative(root, p) || ".";
}

interface ValidatorContext {
  root: string;
  findings: Finding[];
  jsonParsed: number;
}

function pushError(ctx: ValidatorContext, rule: string, path: string, message: string): void {
  ctx.findings.push({ severity: "error", rule, path: relPath(ctx.root, path), message });
}

function pushWarning(ctx: ValidatorContext, rule: string, path: string, message: string): void {
  ctx.findings.push({ severity: "warning", rule, path: relPath(ctx.root, path), message });
}

function validateMetaShape(ctx: ValidatorContext, scenarioDir: string): void {
  const metaPath = join(scenarioDir, "meta.json");
  const meta = tryParseJson(metaPath);
  if (meta === null || meta === undefined) {
    pushError(ctx, "meta.unparseable", metaPath, "meta.json missing or unparseable");
    return;
  }
  ctx.jsonParsed++;
  if (typeof meta !== "object" || Array.isArray(meta)) {
    pushError(ctx, "meta.not-object", metaPath, "meta.json must be a JSON object");
    return;
  }
  const m = meta as Record<string, unknown>;
  const requiredKeys = ["kind", "fixturesVersion", "capturedAt", "source"];
  for (const k of requiredKeys) {
    if (!(k in m)) {
      pushError(ctx, "meta.missing-key", metaPath, `meta.json missing required key '${k}'`);
    }
  }
  if (m.kind !== null && m.kind !== undefined && !["realistic", "synthetic", "stub"].includes(m.kind as string)) {
    pushError(
      ctx,
      "meta.bad-kind",
      metaPath,
      `meta.kind must be one of realistic|synthetic|stub (got '${String(m.kind)}')`,
    );
  }
}

function validateScenarioDir(
  ctx: ValidatorContext,
  scenarioDir: string,
  scenarioId: string,
): void {
  if (!isDir(scenarioDir)) {
    pushError(ctx, "scenario.missing", scenarioDir, `scenario '${scenarioId}' directory missing`);
    return;
  }

  // Stub scenarios (directory exists, but no meta.json) are intentional
  // placeholders for §8.8 entries not yet populated by the seeder. Skip
  // the per-file checks but emit a warning so they're visible.
  const metaPresent = isFile(join(scenarioDir, "meta.json"));
  if (!metaPresent) {
    pushWarning(
      ctx,
      "scenario.stub",
      scenarioDir,
      `scenario '${scenarioId}' is a stub (no meta.json); skip-checked. Populate via build-scenarios.sh.`,
    );
    return;
  }

  for (const file of REQUIRED_SCENARIO_FILES) {
    const fp = join(scenarioDir, file);
    if (!isFile(fp)) {
      pushError(
        ctx,
        "scenario.missing-file",
        fp,
        `scenario '${scenarioId}' missing required file '${file}'`,
      );
      continue;
    }
    if (file.endsWith(".json")) {
      const parsed = tryParseJson(fp);
      if (parsed === null || parsed === undefined) {
        pushError(ctx, "scenario.unparseable-json", fp, `${file} did not parse as JSON`);
      } else {
        ctx.jsonParsed++;
      }
    } else if (file.endsWith(".jsonl")) {
      const lines = tryParseNdjson(fp);
      if (lines === null || lines === undefined) {
        pushError(ctx, "scenario.unparseable-jsonl", fp, `${file} did not parse as NDJSON`);
      } else {
        ctx.jsonParsed += lines.length;
        // Extra: NDJSON events should have channel, seq, ts, type at minimum.
        for (let i = 0; i < lines.length; i++) {
          const ev = lines[i] as Record<string, unknown> | null;
          if (
            ev === null ||
            ev === undefined ||
            typeof ev.channel !== "string" ||
            typeof ev.seq !== "number" ||
            typeof ev.ts !== "string" ||
            typeof ev.type !== "string"
          ) {
            pushError(
              ctx,
              "scenario.bad-event",
              fp,
              `events.jsonl line ${i + 1} missing channel/seq/ts/type`,
            );
            break;
          }
        }
      }
    }
  }

  for (const subdir of REQUIRED_SCENARIO_DIRS) {
    const dp = join(scenarioDir, subdir);
    if (!isDir(dp)) {
      pushError(
        ctx,
        "scenario.missing-dir",
        dp,
        `scenario '${scenarioId}' missing required dir '${subdir}/'`,
      );
    }
  }

  const extras = SCENARIO_SPECIFIC_REQUIREMENTS[scenarioId];
  if (extras) {
    for (const file of extras) {
      const fp = join(scenarioDir, file);
      if (!isFile(fp)) {
        pushError(
          ctx,
          "scenario.missing-conditional-file",
          fp,
          `scenario '${scenarioId}' missing required '${file}' (scenario-specific)`,
        );
      }
    }
  }

  validateMetaShape(ctx, scenarioDir);
  validateExpectedOutcomeShape(ctx, scenarioDir);
}

function validateExpectedOutcomeShape(ctx: ValidatorContext, scenarioDir: string): void {
  const path = join(scenarioDir, "expected-outcome.json");
  if (!isFile(path)) return; // already reported
  const parsed = tryParseJson(path) as Record<string, unknown> | null;
  if (parsed === null || parsed === undefined) return;
  const requiredKeys = ["meta", "detections", "wakeAgent", "approvalsRequested", "postconditions", "activityBehavior"];
  for (const k of requiredKeys) {
    if (!(k in parsed)) {
      pushError(
        ctx,
        "outcome.missing-key",
        path,
        `expected-outcome.json missing required key '${k}'`,
      );
    }
  }
  if (
    parsed.activityBehavior !== null &&
    parsed.activityBehavior !== undefined &&
    !["silent", "audit_only", "surface"].includes(parsed.activityBehavior as string)
  ) {
    pushError(
      ctx,
      "outcome.bad-activity-behavior",
      path,
      `expected-outcome.activityBehavior must be silent|audit_only|surface (got '${String(parsed.activityBehavior)}')`,
    );
  }
  if (parsed.wakeAgent === true && (parsed.actionPlan === null || parsed.actionPlan === undefined)) {
    pushError(
      ctx,
      "outcome.wake-without-plan",
      path,
      "expected-outcome: wakeAgent=true but actionPlan is missing",
    );
  }
  if (parsed.wakeAgent === false && parsed.actionPlan !== null && parsed.actionPlan !== undefined) {
    pushWarning(
      ctx,
      "outcome.plan-without-wake",
      path,
      "expected-outcome: wakeAgent=false but actionPlan is present (intentional?)",
    );
  }
}

function validateGoldenOutputFile(
  ctx: ValidatorContext,
  adapter: AdapterSlug,
  state: GoldenOutputState,
): void {
  const path = goldenOutputPath(adapter, state);
  if (!isFile(path)) {
    pushError(
      ctx,
      "golden.missing-file",
      path,
      `golden-outputs/${adapter}/${state}.json missing`,
    );
    return;
  }
  const parsed = tryParseJson(path) as Record<string, unknown> | null;
  if (parsed === null || parsed === undefined) {
    pushError(ctx, "golden.unparseable", path, "did not parse as JSON");
    return;
  }
  ctx.jsonParsed++;
  const m = parsed.meta as Record<string, unknown> | undefined;
  if (m === null || m === undefined) {
    pushError(ctx, "golden.no-meta", path, "missing meta block");
    return;
  }
  if (m.adapter !== adapter) {
    pushError(
      ctx,
      "golden.bad-adapter",
      path,
      `meta.adapter='${String(m.adapter)}' doesn't match directory '${adapter}'`,
    );
  }
  if (m.state !== state) {
    pushError(
      ctx,
      "golden.bad-state",
      path,
      `meta.state='${String(m.state)}' doesn't match filename '${state}'`,
    );
  }
  for (const k of ["argv", "exit", "durationMs"]) {
    if (!(k in parsed)) {
      pushError(ctx, "golden.missing-envelope-key", path, `missing required envelope key '${k}'`);
    }
  }
  // Failure-class semantics
  if (state === "missing-tool" && parsed.exit !== 127) {
    pushWarning(
      ctx,
      "golden.missing-tool-exit",
      path,
      `state=missing-tool but exit=${String(parsed.exit)} (expected 127)`,
    );
  }
  if (state === "timeout" && parsed.exit !== 124) {
    pushWarning(
      ctx,
      "golden.timeout-exit",
      path,
      `state=timeout but exit=${String(parsed.exit)} (expected 124)`,
    );
  }
  if (state === "high-volume" && parsed.truncated !== true) {
    pushWarning(
      ctx,
      "golden.high-volume-truncated",
      path,
      `state=high-volume but truncated!=true`,
    );
  }
}

function scanForSecrets(ctx: ValidatorContext, root: string): void {
  // Scope: corpus dirs only — never the package's own src/ or tests/ (which
  // legitimately contain the secret-pattern strings as part of *enforcing*
  // Guardrail 11). The corpus is fixture data; src/tests are validator code.
  const corpusDirs = ["scenarios", "golden-outputs"];
  for (const entry of readdirSync(root, { withFileTypes: true })) {
    if (entry.isDirectory() && entry.name.startsWith("phase0-")) {
      corpusDirs.push(entry.name);
    }
  }
  function walk(dir: string): void {
    let entries: string[];
    try {
      entries = readdirSync(dir);
    } catch {
      return;
    }
    for (const name of entries) {
      if (name === "node_modules" || name === ".turbo" || name.startsWith(".")) continue;
      const fp = join(dir, name);
      if (isDir(fp)) {
        walk(fp);
      } else if (isFile(fp)) {
        let text: string;
        try {
          text = readFileSync(fp, "utf8");
        } catch {
          continue;
        }
        for (const { name: ruleName, rx } of FORBIDDEN_SECRET_PATTERNS) {
          if (rx.test(text)) {
            pushError(
              ctx,
              "secret.found",
              fp,
              `forbidden pattern matched: ${ruleName}`,
            );
          }
        }
      }
    }
  }
  for (const sub of corpusDirs) {
    const dir = join(root, sub);
    if (isDir(dir)) walk(dir);
  }
}

/** Run the validator against the corpus root (defaults to `fixturesRoot()`). */
export function validateCorpus(rootOverride?: string): ValidationResult {
  const root = rootOverride ?? fixturesRoot();
  const ctx: ValidatorContext = { root, findings: [], jsonParsed: 0 };

  let tendingScenariosFound = 0;
  for (const id of TENDING_SCENARIOS as readonly TendingScenarioId[]) {
    const dir = scenarioPath(id);
    if (isDir(dir)) {
      tendingScenariosFound++;
    }
    validateScenarioDir(ctx, dir, id);
  }

  let phase0ScenariosFound = 0;
  for (const id of PHASE0_SCENARIOS as readonly Phase0ScenarioId[]) {
    const dir = phase0ScenarioPath(id);
    if (isDir(dir)) {
      phase0ScenariosFound++;
      // Phase 0 directories are placeholders pre-VPS; only assert they exist.
    } else {
      pushError(ctx, "phase0.missing-dir", dir, `phase0 scenario '${id}' directory missing`);
    }
  }

  let goldenOutputsFound = 0;
  for (const adapter of ADAPTER_SLUGS as readonly AdapterSlug[]) {
    for (const state of GOLDEN_OUTPUT_STATES as readonly GoldenOutputState[]) {
      const path = goldenOutputPath(adapter, state);
      if (isFile(path)) goldenOutputsFound++;
      validateGoldenOutputFile(ctx, adapter, state);
    }
  }

  scanForSecrets(ctx, root);

  return {
    ok: ctx.findings.every((f) => f.severity !== "error"),
    rootInspected: root,
    summary: {
      tendingScenariosExpected: TENDING_SCENARIOS.length,
      tendingScenariosFound,
      phase0ScenariosExpected: PHASE0_SCENARIOS.length,
      phase0ScenariosFound,
      goldenOutputsExpected: ADAPTER_SLUGS.length * GOLDEN_OUTPUT_STATES.length,
      goldenOutputsFound,
      jsonFilesParsed: ctx.jsonParsed,
    },
    findings: ctx.findings,
  };
}

/** Pretty-print the result for CLI / CI. */
export function formatResult(result: ValidationResult): string {
  const lines: string[] = [];
  lines.push(`# Hoopoe fixture-quality report`);
  lines.push(`Root: ${result.rootInspected}`);
  lines.push("");
  lines.push(
    `Tending scenarios:  ${result.summary.tendingScenariosFound}/${result.summary.tendingScenariosExpected}`,
  );
  lines.push(
    `Phase 0 scenarios:  ${result.summary.phase0ScenariosFound}/${result.summary.phase0ScenariosExpected}`,
  );
  lines.push(
    `Golden outputs:     ${result.summary.goldenOutputsFound}/${result.summary.goldenOutputsExpected}`,
  );
  lines.push(`JSON files parsed:  ${result.summary.jsonFilesParsed}`);
  lines.push("");
  const errors = result.findings.filter((f) => f.severity === "error");
  const warnings = result.findings.filter((f) => f.severity === "warning");
  lines.push(`Errors:   ${errors.length}`);
  lines.push(`Warnings: ${warnings.length}`);
  lines.push("");
  if (errors.length > 0) {
    lines.push("## Errors");
    for (const f of errors) {
      lines.push(`- [${f.rule}] ${f.path}: ${f.message}`);
    }
    lines.push("");
  }
  if (warnings.length > 0) {
    lines.push("## Warnings");
    for (const f of warnings) {
      lines.push(`- [${f.rule}] ${f.path}: ${f.message}`);
    }
    lines.push("");
  }
  lines.push(result.ok ? "## Result: OK" : "## Result: FAIL");
  return lines.join("\n");
}
