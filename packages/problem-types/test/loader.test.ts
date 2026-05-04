import { describe, expect, test } from "bun:test";
import { mkdtempSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join, resolve } from "node:path";
import {
  loadProblemTypes,
  PROBLEM_ACTIONABILITIES,
  PROBLEM_SURFACES,
  ProblemTypesError,
} from "../src/index.ts";

const REPO_ROOT = resolve(__dirname, "..", "..", "..");

function writeYaml(text: string): string {
  const dir = mkdtempSync(join(tmpdir(), "hoopoe-problem-types-"));
  const path = join(dir, "problem-types.yaml");
  writeFileSync(path, text, "utf8");
  return path;
}

describe("hp-g6sp :: loader", () => {
  test("loads packages/schemas/problem-types.yaml from repo root", () => {
    const registry = loadProblemTypes({ repoRoot: REPO_ROOT });
    expect(registry.schemaVersion).toBe(1);
    expect(registry.problems.length).toBeGreaterThan(20);
    expect(registry.sourcePath.endsWith("problem-types.yaml")).toBe(true);
  });

  test("every entry has well-formed type_uri, status, surface, actionability", () => {
    const registry = loadProblemTypes({ repoRoot: REPO_ROOT });
    for (const problem of registry.problems) {
      expect(problem.typeUri.startsWith("https://hoopoe.io/problems/")).toBe(true);
      expect(problem.status).toBeGreaterThanOrEqual(400);
      expect(problem.status).toBeLessThan(600);
      expect(PROBLEM_SURFACES).toContain(problem.surface);
      expect(PROBLEM_ACTIONABILITIES).toContain(problem.actionability);
      expect(problem.userMessage.length).toBeGreaterThan(0);
    }
  });

  test("every surface enum value has at least one entry", () => {
    const registry = loadProblemTypes({ repoRoot: REPO_ROOT });
    for (const surface of PROBLEM_SURFACES) {
      const matches = registry.problems.filter((p) => p.surface === surface);
      expect(matches.length).toBeGreaterThan(0);
    }
  });

  test("every actionability enum value has at least one entry", () => {
    const registry = loadProblemTypes({ repoRoot: REPO_ROOT });
    for (const actionability of PROBLEM_ACTIONABILITIES) {
      const matches = registry.problems.filter((p) => p.actionability === actionability);
      expect(matches.length).toBeGreaterThan(0);
    }
  });

  test("contains the canonical default fallbacks (not-found, internal-error, bad-request)", () => {
    const registry = loadProblemTypes({ repoRoot: REPO_ROOT });
    const ids = new Set(registry.problems.map((p) => p.id));
    expect(ids.has("not-found")).toBe(true);
    expect(ids.has("internal-error")).toBe(true);
    expect(ids.has("bad-request")).toBe(true);
  });

  test("rejects schemaVersion mismatch", () => {
    const path = writeYaml("schemaVersion: 99\nproblems: []\n");
    expect(() => loadProblemTypes({ path })).toThrow(ProblemTypesError);
  });

  test("rejects malformed type_uri", () => {
    const path = writeYaml(
      [
        "schemaVersion: 1",
        "problems:",
        "  - id: bad",
        "    type_uri: not-a-url",
        "    title: Bad",
        "    status: 400",
        "    surface: toast",
        "    actionability: reload",
        "    user_message: x",
        "",
      ].join("\n"),
    );
    expect(() => loadProblemTypes({ path })).toThrow(ProblemTypesError);
  });

  test("rejects out-of-range status code", () => {
    const path = writeYaml(
      [
        "schemaVersion: 1",
        "problems:",
        "  - id: bad",
        "    type_uri: https://hoopoe.io/problems/bad",
        "    title: Bad",
        "    status: 999",
        "    surface: toast",
        "    actionability: reload",
        "    user_message: x",
        "",
      ].join("\n"),
    );
    expect(() => loadProblemTypes({ path })).toThrow(ProblemTypesError);
  });

  test("rejects unknown surface enum value", () => {
    const path = writeYaml(
      [
        "schemaVersion: 1",
        "problems:",
        "  - id: bad",
        "    type_uri: https://hoopoe.io/problems/bad",
        "    title: Bad",
        "    status: 400",
        "    surface: floating-popup",
        "    actionability: reload",
        "    user_message: x",
        "",
      ].join("\n"),
    );
    expect(() => loadProblemTypes({ path })).toThrow(ProblemTypesError);
  });

  test("rejects duplicate ids", () => {
    const path = writeYaml(
      [
        "schemaVersion: 1",
        "problems:",
        "  - id: same",
        "    type_uri: https://hoopoe.io/problems/a",
        "    title: A",
        "    status: 400",
        "    surface: toast",
        "    actionability: reload",
        "    user_message: x",
        "  - id: same",
        "    type_uri: https://hoopoe.io/problems/b",
        "    title: B",
        "    status: 400",
        "    surface: toast",
        "    actionability: reload",
        "    user_message: x",
        "",
      ].join("\n"),
    );
    expect(() => loadProblemTypes({ path })).toThrow(ProblemTypesError);
  });

  test("rejects duplicate type_uri", () => {
    const path = writeYaml(
      [
        "schemaVersion: 1",
        "problems:",
        "  - id: a",
        "    type_uri: https://hoopoe.io/problems/dup",
        "    title: A",
        "    status: 400",
        "    surface: toast",
        "    actionability: reload",
        "    user_message: x",
        "  - id: b",
        "    type_uri: https://hoopoe.io/problems/dup",
        "    title: B",
        "    status: 400",
        "    surface: toast",
        "    actionability: reload",
        "    user_message: x",
        "",
      ].join("\n"),
    );
    expect(() => loadProblemTypes({ path })).toThrow(ProblemTypesError);
  });
});
