import { readFileSync, readdirSync } from "node:fs";
import { basename, join, resolve } from "node:path";
import type { PlanningPrompt, PromptRegressionResult, RegressionFixture } from "./types.ts";

function isObject(value: unknown): value is Record<string, unknown> {
  return typeof value === "object" && value !== null && !Array.isArray(value);
}

function parseFixture(path: string): RegressionFixture {
  let parsed: unknown;
  try {
    parsed = JSON.parse(readFileSync(path, "utf8")) as unknown;
  } catch (err) {
    throw new Error(`fixture ${path}: failed to parse JSON: ${(err as Error).message}`, {
      cause: err,
    });
  }
  if (!isObject(parsed)) {
    throw new Error(`fixture ${path}: root must be object`);
  }
  if (typeof parsed.promptId !== "string") {
    throw new Error(`fixture ${path}: promptId must be string`);
  }
  if (typeof parsed.pipelineVersion !== "string") {
    throw new Error(`fixture ${path}: pipelineVersion must be string`);
  }
  if (!isObject(parsed.inputShape)) {
    throw new Error(`fixture ${path}: inputShape must be object`);
  }
  if (!isObject(parsed.expectedOutputSchema)) {
    throw new Error(`fixture ${path}: expectedOutputSchema must be object`);
  }
  if (
    !Array.isArray(parsed.sampleAcceptableOutputs) ||
    parsed.sampleAcceptableOutputs.some((item) => typeof item !== "string")
  ) {
    throw new Error(`fixture ${path}: sampleAcceptableOutputs must be string[]`);
  }
  return parsed as unknown as RegressionFixture;
}

export function loadRegressionFixtures(repoRoot = process.cwd()): RegressionFixture[] {
  const dir = resolve(repoRoot, "packages", "fixtures", "planning-prompt-regression");
  return readdirSync(dir)
    .filter((name) => name.endsWith(".json"))
    .toSorted()
    .map((name) => parseFixture(join(dir, name)));
}

export function runDeterministicRegression(
  prompt: PlanningPrompt,
  fixture: RegressionFixture,
  fixturePath = `${fixture.promptId}.json`,
): PromptRegressionResult {
  const diagnostics: string[] = [];
  if (prompt.frontmatter.id !== fixture.promptId) {
    diagnostics.push(
      `fixture promptId '${fixture.promptId}' does not match prompt '${prompt.frontmatter.id}'`,
    );
  }
  if (!prompt.frontmatter.appliesToPipelineVersions.includes(fixture.pipelineVersion)) {
    diagnostics.push(`pipeline version '${fixture.pipelineVersion}' is not in prompt frontmatter`);
  }
  for (const key of Object.keys(fixture.inputShape)) {
    if (!prompt.body.includes(`{{${key}}}`)) {
      diagnostics.push(`prompt body does not reference input variable {{${key}}}`);
    }
  }
  for (const sample of fixture.sampleAcceptableOutputs) {
    if (sample.trim().length === 0) {
      diagnostics.push("sample acceptable output must not be empty");
    }
  }
  if (basename(fixturePath, ".json") !== fixture.promptId) {
    diagnostics.push(`fixture filename should be ${fixture.promptId}.json`);
  }
  return { promptId: fixture.promptId, fixturePath, ok: diagnostics.length === 0, diagnostics };
}
