import { describe, expect, test } from "bun:test";
import { mkdtempSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join, resolve } from "node:path";
import {
  assertActionKindInSync,
  checkActionKindDrift,
  loadTendingActions,
  TendingActionsError,
} from "../src/index.ts";

const REPO_ROOT = resolve(__dirname, "..", "..", "..");

function makeOpenapiYaml(enumLines: readonly string[]): string {
  return [
    "openapi: 3.0.0",
    "components:",
    "  schemas:",
    "    ActionKind:",
    "      type: string",
    "      enum:",
    ...enumLines.map((v) => `        - ${v}`),
    "    NextThing:",
    "      type: object",
    "",
  ].join("\n");
}

function makeBundleYaml(kinds: readonly string[]): string {
  const actions = kinds
    .map((k) =>
      [
        `  ${k}:`,
        "    description: x",
        "    riskClass: low",
        "    requiresApprovalDefault: false",
        "    target: {}",
        "    args: {}",
        "    preconditions: []",
        "    postconditions: ['ok']",
      ].join("\n"),
    )
    .join("\n");
  return [
    "schemaVersion: 1",
    "mirrorsOpenapiSchema: ActionKind",
    "mirrorsOpenapiYaml: ./openapi.yaml",
    "actions:",
    actions,
    "",
  ].join("\n");
}

describe("hp-dmz :: drift checker", () => {
  test("real repo: tending-actions.yaml is in sync with openapi.yaml's ActionKind", () => {
    const bundle = loadTendingActions({ repoRoot: REPO_ROOT });
    const report = checkActionKindDrift(bundle);
    expect(report.inSync).toBe(true);
    expect(report.extraInYaml).toEqual([]);
    expect(report.missingInYaml).toEqual([]);
  });

  test("assertActionKindInSync passes on the real repo", () => {
    const bundle = loadTendingActions({ repoRoot: REPO_ROOT });
    expect(() => assertActionKindInSync(bundle)).not.toThrow();
  });

  test("detects extra-in-YAML drift", () => {
    const dir = mkdtempSync(join(tmpdir(), "hoopoe-tending-drift-"));
    const openapi = join(dir, "openapi.yaml");
    const bundlePath = join(dir, "tending-actions.yaml");
    writeFileSync(openapi, makeOpenapiYaml(["a.one", "a.two"]), "utf8");
    writeFileSync(bundlePath, makeBundleYaml(["a.one", "a.two", "a.extra"]), "utf8");
    const bundle = loadTendingActions({ path: bundlePath });
    const report = checkActionKindDrift(bundle);
    expect(report.inSync).toBe(false);
    expect(report.extraInYaml).toEqual(["a.extra"]);
    expect(report.missingInYaml).toEqual([]);
  });

  test("detects missing-in-YAML drift", () => {
    const dir = mkdtempSync(join(tmpdir(), "hoopoe-tending-drift-"));
    const openapi = join(dir, "openapi.yaml");
    const bundlePath = join(dir, "tending-actions.yaml");
    writeFileSync(openapi, makeOpenapiYaml(["a.one", "a.two", "a.three"]), "utf8");
    writeFileSync(bundlePath, makeBundleYaml(["a.one", "a.two"]), "utf8");
    const bundle = loadTendingActions({ path: bundlePath });
    const report = checkActionKindDrift(bundle);
    expect(report.inSync).toBe(false);
    expect(report.extraInYaml).toEqual([]);
    expect(report.missingInYaml).toEqual(["a.three"]);
  });

  test("assertActionKindInSync throws TendingActionsError on drift", () => {
    const dir = mkdtempSync(join(tmpdir(), "hoopoe-tending-drift-"));
    const openapi = join(dir, "openapi.yaml");
    const bundlePath = join(dir, "tending-actions.yaml");
    writeFileSync(openapi, makeOpenapiYaml(["a.one"]), "utf8");
    writeFileSync(bundlePath, makeBundleYaml(["a.two"]), "utf8");
    const bundle = loadTendingActions({ path: bundlePath });
    expect(() => assertActionKindInSync(bundle)).toThrow(TendingActionsError);
  });
});
