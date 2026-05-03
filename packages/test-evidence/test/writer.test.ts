import { describe, expect, test } from "bun:test";
import { mkdtempSync, readFileSync, statSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import {
  buildEnvelope,
  evidencePath,
  timestampSegment,
  writeEvidence,
} from "../src/index.ts";

describe("hp-6sv :: writer", () => {
  test("timestampSegment formats UTC zulu", () => {
    const seg = timestampSegment(new Date("2026-05-04T01:23:45.000Z"));
    expect(seg).toBe("20260504T012345Z");
  });

  test("evidencePath builds the canonical layout", () => {
    const env = buildEnvelope({
      runId: "abc-123",
      ts: "2026-05-04T01:23:45.000Z",
      gitSha: "x",
      daemonVersion: "y",
      runner: "bun-test",
      phase: "phase2",
      results: [],
    });
    const { relative, absolute } = evidencePath(env, { repoRoot: "/repo" });
    expect(relative).toBe("docs/test-evidence/phase2/20260504T012345Z/bun-test-abc-123.json");
    expect(absolute).toBe("/repo/docs/test-evidence/phase2/20260504T012345Z/bun-test-abc-123.json");
  });

  test("writeEvidence writes a parseable JSON file under repoRoot", async () => {
    const repoRoot = mkdtempSync(join(tmpdir(), "hoopoe-evidence-writer-"));
    const env = buildEnvelope({
      runId: "writer-test",
      ts: "2026-05-04T01:23:45.000Z",
      gitSha: "deadbeef",
      daemonVersion: "0.0.0",
      runner: "go-test",
      phase: "phase-test",
      results: [
        { name: "ok", file: "internal/x/x_test.go", status: "passed", durationMs: 1 },
      ],
    });
    const written = await writeEvidence(env, { repoRoot });
    expect(written.relativePath).toBe(
      "docs/test-evidence/phase-test/20260504T012345Z/go-test-writer-test.json",
    );
    const stat = statSync(written.path);
    expect(stat.isFile()).toBe(true);
    expect(stat.size).toBe(written.bytes);
    const parsed = JSON.parse(readFileSync(written.path, "utf8"));
    expect(parsed.schemaVersion).toBe(1);
    expect(parsed.runId).toBe("writer-test");
    expect(parsed.results).toHaveLength(1);
  });
});
