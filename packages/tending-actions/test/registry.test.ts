import { afterEach, beforeAll, describe, expect, test } from "bun:test";
import { resolve } from "node:path";
import {
  clearActions,
  getAction,
  listActions,
  listActionsByRisk,
  listActionsRequiringApproval,
  loadTendingActions,
  TendingActionsError,
  useActions,
} from "../src/index.ts";

const REPO_ROOT = resolve(__dirname, "..", "..", "..");

beforeAll(() => {
  useActions(loadTendingActions({ repoRoot: REPO_ROOT }));
});

afterEach(() => {
  useActions(loadTendingActions({ repoRoot: REPO_ROOT }));
});

describe("hp-dmz :: registry", () => {
  test("getAction returns the declared action by kind", () => {
    const action = getAction("agent.kill_wedged_process");
    expect(action.kind).toBe("agent.kill_wedged_process");
    expect(action.riskClass).toBe("high");
    expect(action.preconditions.length).toBeGreaterThan(0);
    expect(action.postconditions.length).toBeGreaterThan(0);
  });

  test("getAction throws on unknown kind with helpful message", () => {
    let caught: unknown = null;
    try {
      getAction("does.not.exist");
    } catch (err) {
      caught = err;
    }
    expect(caught).toBeInstanceOf(TendingActionsError);
    expect((caught as Error).message).toContain("does.not.exist");
  });

  test("listActionsByRisk filters correctly", () => {
    const high = listActionsByRisk("high");
    const lowKinds = listActionsByRisk("low").map((a) => a.kind);
    expect(high.some((a) => a.kind === "agent.kill_wedged_process")).toBe(true);
    expect(high.some((a) => a.kind === "swarm.halt")).toBe(true);
    expect(lowKinds).toContain("agent.ask_status");
    expect(lowKinds).toContain("agent.pause");
  });

  test("listActionsRequiringApproval returns the destructive defaults", () => {
    const requiring = listActionsRequiringApproval();
    const kinds = requiring.map((a) => a.kind);
    expect(kinds).toContain("agent.kill_wedged_process");
    expect(kinds).toContain("reservation.force_release");
    expect(kinds).toContain("swarm.halt");
    expect(kinds).toContain("review.propose_flip");
    expect(kinds).not.toContain("agent.ask_status");
  });

  test("clearActions isolates registry state", () => {
    clearActions();
    expect(() => listActions({ repoRoot: REPO_ROOT })).not.toThrow();
  });
});
