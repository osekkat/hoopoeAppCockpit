import { describe, expect, test } from "bun:test";
import { join, resolve } from "node:path";
import {
  loadPlanningPrompts,
  loadRegressionFixtures,
  runDeterministicRegression,
} from "../src/index.ts";

const REPO_ROOT = resolve(__dirname, "..", "..", "..");

describe("hp-3ab :: deterministic prompt regression fixtures", () => {
  test("every canonical prompt has a matching fixture", () => {
    const prompts = loadPlanningPrompts({ repoRoot: REPO_ROOT });
    const fixtures = loadRegressionFixtures(REPO_ROOT);
    expect(fixtures.map((fixture) => fixture.promptId).toSorted()).toEqual(
      prompts.map((prompt) => prompt.frontmatter.id).toSorted(),
    );
  });

  test("deterministic mock regression passes for every fixture", () => {
    const prompts = new Map(
      loadPlanningPrompts({ repoRoot: REPO_ROOT }).map((prompt) => [prompt.frontmatter.id, prompt]),
    );
    for (const fixture of loadRegressionFixtures(REPO_ROOT)) {
      const prompt = prompts.get(fixture.promptId);
      if (prompt === undefined) {
        throw new Error(`missing prompt for fixture ${fixture.promptId}`);
      }
      const result = runDeterministicRegression(
        prompt,
        fixture,
        join(
          REPO_ROOT,
          "packages",
          "fixtures",
          "planning-prompt-regression",
          `${fixture.promptId}.json`,
        ),
      );
      expect(result.diagnostics).toEqual([]);
      expect(result.ok).toBe(true);
    }
  });
});
