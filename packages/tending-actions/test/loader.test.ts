import { describe, expect, test } from "bun:test";
import { mkdtempSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join, resolve } from "node:path";
import {
  loadTendingActions,
  TendingActionsError,
} from "../src/index.ts";

const REPO_ROOT = resolve(__dirname, "..", "..", "..");

function writeYaml(text: string): string {
  const dir = mkdtempSync(join(tmpdir(), "hoopoe-tending-actions-"));
  const path = join(dir, "tending-actions.yaml");
  writeFileSync(path, text, "utf8");
  return path;
}

describe("hp-dmz :: loader", () => {
  test("loads packages/schemas/tending-actions.yaml from repo root", () => {
    const bundle = loadTendingActions({ repoRoot: REPO_ROOT });
    expect(bundle.schemaVersion).toBe(1);
    expect(bundle.mirrorsOpenapiSchema).toBe("ActionKind");
    expect(bundle.mirrorsOpenapiYaml).toBe("./openapi.yaml");
    expect(bundle.actions.length).toBe(11);
  });

  test("every canonical §8.3.1 action kind is present", () => {
    const bundle = loadTendingActions({ repoRoot: REPO_ROOT });
    const kinds = new Set(bundle.actions.map((a) => a.kind));
    for (const expected of [
      "agent.ask_status",
      "agent.send_marching_orders",
      "agent.pause",
      "agent.kill_wedged_process",
      "reservation.force_release",
      "caam.switch_account",
      "casr.resume_session",
      "git.push_branch",
      "swarm.halt",
      "review.propose_flip",
      "bead.create_blocker",
    ]) {
      expect(kinds.has(expected)).toBe(true);
    }
  });

  test("each action carries preconditions and postconditions arrays", () => {
    const bundle = loadTendingActions({ repoRoot: REPO_ROOT });
    for (const action of bundle.actions) {
      expect(Array.isArray(action.preconditions)).toBe(true);
      expect(Array.isArray(action.postconditions)).toBe(true);
      expect(action.postconditions.length).toBeGreaterThan(0);
    }
  });

  test("destructive actions default to requiring approval", () => {
    const bundle = loadTendingActions({ repoRoot: REPO_ROOT });
    const kill = bundle.actions.find((a) => a.kind === "agent.kill_wedged_process");
    const halt = bundle.actions.find((a) => a.kind === "swarm.halt");
    const release = bundle.actions.find((a) => a.kind === "reservation.force_release");
    expect(kill?.requiresApprovalDefault).toBe(true);
    expect(halt?.requiresApprovalDefault).toBe(true);
    expect(release?.requiresApprovalDefault).toBe(true);
  });

  test("rejects schemaVersion mismatch", () => {
    const path = writeYaml(
      [
        "schemaVersion: 99",
        "mirrorsOpenapiSchema: ActionKind",
        "mirrorsOpenapiYaml: ./openapi.yaml",
        "actions: {}",
      ].join("\n"),
    );
    expect(() => loadTendingActions({ path })).toThrow(TendingActionsError);
  });

  test("rejects unknown riskClass", () => {
    const path = writeYaml(
      [
        "schemaVersion: 1",
        "mirrorsOpenapiSchema: ActionKind",
        "mirrorsOpenapiYaml: ./openapi.yaml",
        "actions:",
        "  bad.action:",
        "    description: x",
        "    riskClass: nuclear",
        "    requiresApprovalDefault: true",
        "    target: {}",
        "    args: {}",
        "",
      ].join("\n"),
    );
    expect(() => loadTendingActions({ path })).toThrow(TendingActionsError);
  });

  test("rejects non-array preconditions", () => {
    const path = writeYaml(
      [
        "schemaVersion: 1",
        "mirrorsOpenapiSchema: ActionKind",
        "mirrorsOpenapiYaml: ./openapi.yaml",
        "actions:",
        "  bad.action:",
        "    description: x",
        "    riskClass: low",
        "    requiresApprovalDefault: false",
        "    target: {}",
        "    args: {}",
        "    preconditions: 'not a list'",
        "",
      ].join("\n"),
    );
    expect(() => loadTendingActions({ path })).toThrow(TendingActionsError);
  });

  test("rejects non-string entry inside postconditions", () => {
    const path = writeYaml(
      [
        "schemaVersion: 1",
        "mirrorsOpenapiSchema: ActionKind",
        "mirrorsOpenapiYaml: ./openapi.yaml",
        "actions:",
        "  bad.action:",
        "    description: x",
        "    riskClass: low",
        "    requiresApprovalDefault: false",
        "    target: {}",
        "    args: {}",
        "    postconditions: [42]",
        "",
      ].join("\n"),
    );
    expect(() => loadTendingActions({ path })).toThrow(TendingActionsError);
  });
});
