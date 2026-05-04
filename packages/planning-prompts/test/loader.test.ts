import { describe, expect, test } from "bun:test";
import { mkdtempSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join, resolve } from "node:path";
import {
  loadManifest,
  loadPlanningPrompts,
  loadPromptFile,
  PlanningPromptsError,
  validatePromptAgainstManifest,
} from "../src/index.ts";

const REPO_ROOT = resolve(__dirname, "..", "..", "..");

describe("hp-3ab :: planning prompt loader", () => {
  test("loads the canonical manifest and all 9 prompt steps", () => {
    const manifest = loadManifest({ repoRoot: REPO_ROOT });
    expect(manifest.schemaVersion).toBe(1);
    expect(manifest.prompts.map((prompt) => prompt.id)).toEqual([
      "clarifying-questions",
      "take-first-shot",
      "candidate-draft",
      "comparative-matrix",
      "synthesis-best-of-all-worlds",
      "fresh-eyes-critique",
      "refinement-round-N",
      "quality-evaluator",
      "lock-readiness",
    ]);

    const prompts = loadPlanningPrompts({ repoRoot: REPO_ROOT });
    expect(prompts).toHaveLength(9);
    expect(prompts.every((prompt) => prompt.frontmatter.owner === "planning-pipeline")).toBe(true);
  });

  test("manifest hashes match prompt frontmatter and prompt body", () => {
    const manifest = loadManifest({ repoRoot: REPO_ROOT });
    for (const entry of manifest.prompts) {
      const prompt = validatePromptAgainstManifest(manifest.sourcePath, entry);
      expect(prompt.frontmatter.hash).toBe(entry.hash);
    }
  });

  test("rejects malformed frontmatter", () => {
    const dir = mkdtempSync(join(tmpdir(), "hoopoe-planning-prompts-"));
    const path = join(dir, "bad.md");
    writeFileSync(path, "---\nid: bad\nversion: nope\n---\nBody\n", "utf8");
    expect(() => loadPromptFile(path)).toThrow(PlanningPromptsError);
  });

  test("rejects manifest schema drift", () => {
    const dir = mkdtempSync(join(tmpdir(), "hoopoe-planning-prompts-"));
    const path = join(dir, "manifest.json");
    writeFileSync(path, JSON.stringify({ schemaVersion: 99, prompts: [] }), "utf8");
    expect(() => loadManifest({ manifestPath: path })).toThrow(PlanningPromptsError);
  });
});
