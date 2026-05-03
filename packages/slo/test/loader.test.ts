import { describe, expect, test } from "bun:test";
import { mkdtempSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join, resolve } from "node:path";
import {
  loadSloTargets,
  SloTargetsError,
} from "../src/index.ts";

const REPO_ROOT = resolve(__dirname, "..", "..", "..");

function writeYaml(text: string): string {
  const dir = mkdtempSync(join(tmpdir(), "hoopoe-slo-loader-"));
  const path = join(dir, "slo.yaml");
  writeFileSync(path, text, "utf8");
  return path;
}

describe("hp-5ja :: loader", () => {
  test("loads packages/slo-targets.yaml and finds the §10.5 ids", () => {
    const targets = loadSloTargets({ repoRoot: REPO_ROOT });
    expect(targets.schemaVersion).toBe(1);
    const ids = targets.targets.map((t) => t.id);
    expect(ids).toContain("desktop.reconnect.p95");
    expect(ids).toContain("dag.usable.500-nodes");
    expect(ids).toContain("job.cancellation.no-orphans");
  });

  test("parses percentile target with seconds suffix into ms threshold", () => {
    const targets = loadSloTargets({ repoRoot: REPO_ROOT });
    const reconnect = targets.targets.find((t) => t.id === "desktop.reconnect.p95");
    expect(reconnect).toBeDefined();
    if (reconnect?.target.kind !== "percentile") throw new Error("expected percentile");
    expect(reconnect.target.percentile).toBe(95);
    expect(reconnect.target.numeric).toBe(10_000);
    expect(reconnect.target.unit).toBe("s");
    expect(reconnect.target.direction).toBe("max");
    expect(reconnect.enforcedIn).toContain("phase2.e2e");
    expect(reconnect.sourceSection).toBe("§10.5");
  });

  test("parses boolean target", () => {
    const targets = loadSloTargets({ repoRoot: REPO_ROOT });
    const dag = targets.targets.find((t) => t.id === "dag.usable.500-nodes");
    expect(dag?.target.kind).toBe("boolean");
    if (dag?.target.kind === "boolean") {
      expect(dag.target.expected).toBe(true);
    }
  });

  test("rejects schemaVersion mismatch", () => {
    const path = writeYaml("schemaVersion: 99\ntargets: []\n");
    expect(() => loadSloTargets({ path })).toThrow(SloTargetsError);
  });

  test("rejects missing target.value", () => {
    const path = writeYaml(
      "schemaVersion: 1\ntargets:\n  - id: bad\n    description: x\n    target:\n      percentile: 95\n      direction: max\n    source_section: '§X'\n    enforced_in: [a]\n",
    );
    expect(() => loadSloTargets({ path })).toThrow(SloTargetsError);
  });

  test("rejects duplicate ids", () => {
    const path = writeYaml(
      [
        "schemaVersion: 1",
        "targets:",
        "  - id: same",
        "    description: a",
        "    target:",
        "      percentile: 95",
        "      value: 10s",
        "      direction: max",
        "    source_section: '§X'",
        "    enforced_in: [a]",
        "  - id: same",
        "    description: b",
        "    target: {boolean: true}",
        "    source_section: '§X'",
        "    enforced_in: [b]",
        "",
      ].join("\n"),
    );
    expect(() => loadSloTargets({ path })).toThrow(SloTargetsError);
  });

  test("rejects unparseable percentile value", () => {
    const path = writeYaml(
      [
        "schemaVersion: 1",
        "targets:",
        "  - id: x",
        "    description: x",
        "    target:",
        "      percentile: 95",
        "      value: 'soon'",
        "      direction: max",
        "    source_section: '§X'",
        "    enforced_in: [a]",
        "",
      ].join("\n"),
    );
    expect(() => loadSloTargets({ path })).toThrow(SloTargetsError);
  });

  test("rejects out-of-range percentile", () => {
    const path = writeYaml(
      [
        "schemaVersion: 1",
        "targets:",
        "  - id: x",
        "    description: x",
        "    target:",
        "      percentile: 0",
        "      value: 10s",
        "      direction: max",
        "    source_section: '§X'",
        "    enforced_in: [a]",
        "",
      ].join("\n"),
    );
    expect(() => loadSloTargets({ path })).toThrow(SloTargetsError);
  });
});
