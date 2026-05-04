import { afterEach, beforeAll, describe, expect, test } from "bun:test";
import { resolve } from "node:path";
import {
  clearProblems,
  getProblem,
  listByActionability,
  listByStatus,
  listBySurface,
  listProblems,
  loadProblemTypes,
  ProblemTypesError,
  useProblems,
} from "../src/index.ts";

const REPO_ROOT = resolve(__dirname, "..", "..", "..");

beforeAll(() => {
  useProblems(loadProblemTypes({ repoRoot: REPO_ROOT }));
});

afterEach(() => {
  useProblems(loadProblemTypes({ repoRoot: REPO_ROOT }));
});

describe("hp-g6sp :: registry", () => {
  test("getProblem returns the declared entry by id", () => {
    const cycle = getProblem("bead.cycle-detected");
    expect(cycle.id).toBe("bead.cycle-detected");
    expect(cycle.status).toBe(422);
    expect(cycle.surface).toBe("banner");
    expect(cycle.actionability).toBe("edit-deps");
    expect(cycle.userMessage).toContain("{{cyclePath}}");
  });

  test("getProblem throws on unknown id with helpful message", () => {
    let caught: unknown = null;
    try {
      getProblem("does.not.exist");
    } catch (err) {
      caught = err;
    }
    expect(caught).toBeInstanceOf(ProblemTypesError);
    expect((caught as Error).message).toContain("does.not.exist");
  });

  test("listBySurface filters correctly", () => {
    const blockers = listBySurface("blocking_modal");
    expect(blockers.length).toBeGreaterThan(0);
    expect(blockers.every((p) => p.surface === "blocking_modal")).toBe(true);
    expect(blockers.some((p) => p.id === "auth.pairing.token-expired")).toBe(true);
  });

  test("listByActionability filters correctly", () => {
    const rePair = listByActionability("re-pair");
    expect(rePair.length).toBeGreaterThan(0);
    expect(rePair.every((p) => p.actionability === "re-pair")).toBe(true);
    expect(rePair.some((p) => p.id === "auth.bearer.expired")).toBe(true);
  });

  test("listByStatus filters correctly", () => {
    const conflicts = listByStatus(409);
    expect(conflicts.length).toBeGreaterThan(0);
    expect(conflicts.every((p) => p.status === 409)).toBe(true);
  });

  test("clearProblems isolates registry state", () => {
    clearProblems();
    expect(() => listProblems({ repoRoot: REPO_ROOT })).not.toThrow();
  });
});
