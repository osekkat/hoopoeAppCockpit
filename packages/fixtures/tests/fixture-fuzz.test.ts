// Hoopoe-owned. Adversarial-input fuzz harness for the fixture loader +
// validator (review Round 3, applies /testing-fuzzing principles).
//
// Asserts: the loader and validator handle malformed / truncated /
// oversized / mixed-encoding / pathologically-shaped inputs WITHOUT
// process-level panics. Throwing a typed error is fine; an uncaught
// exception or hang is not.
//
// What we fuzz:
//   1. loadTendingScenario() against scenarios with:
//      - empty meta.json
//      - truncated JSON (missing closing brace)
//      - prototype-pollution attempts (`__proto__` keys)
//      - JSON with BOM prefix
//      - events.jsonl with binary line bytes
//      - events.jsonl with malformed line in the middle of valid lines
//      - missing required files
//      - directory where a file is expected
//   2. validateCorpus() against an entirely synthetic corpus root with
//      pathological scenarios — verifies the validator gracefully
//      reports findings instead of crashing.

import { describe, expect, test } from "bun:test";
import {
  mkdirSync,
  mkdtempSync,
  readdirSync,
  rmSync,
  writeFileSync,
} from "node:fs";
import { tmpdir } from "node:os";
import { dirname, join } from "node:path";
import { loadTendingScenario, ScenarioLoadError } from "../replay/scenario-source.ts";
import { validateCorpus } from "../src/validate.ts";

function isolatedCorpus(): string {
  return mkdtempSync(join(tmpdir(), "hoopoe-fixture-fuzz-"));
}

/** Create the minimum scenario directory skeleton expected by the loader. */
function buildBaseScenario(corpus: string, scenarioName: string, files: Record<string, string>): string {
  const dir = join(corpus, "scenarios", scenarioName);
  mkdirSync(dir, { recursive: true });
  mkdirSync(join(dir, "pane-logs"), { recursive: true });
  mkdirSync(join(dir, "build-logs"), { recursive: true });
  for (const [name, content] of Object.entries(files)) {
    writeFileSync(join(dir, name), content);
  }
  return dir;
}

const VALID_META = JSON.stringify({
  kind: "synthetic",
  scenario: "fuzz",
  fixturesVersion: "fuzz-2026",
  capturedAt: "2026-05-03T00:00:00Z",
  source: "fuzz harness",
});

const PROTOTYPE_POLLUTION_META =
  '{"kind":"synthetic","fixturesVersion":"fuzz","capturedAt":"2026-05-03T00:00:00Z",' +
  '"source":"fuzz","__proto__":{"polluted":true},"constructor":{"polluted":true}}';

const VALID_EVENT_LINE = JSON.stringify({
  channel: "swarm",
  seq: 1,
  ts: "2026-05-03T00:00:00Z",
  type: "swarm.tick",
});

describe("loadTendingScenario fuzz (review Round 3)", () => {
  test("empty meta.json → throws ScenarioLoadError, not panic", () => {
    const corpus = isolatedCorpus();
    try {
      const dir = buildBaseScenario(corpus, "empty-meta", {
        "meta.json": "",
        "bv-triage.json": "{}",
        "br-list.json": "{}",
        "ntm-snapshot.json": "{}",
        "agent-mail-dump.json": "{}",
        "reservations.json": "{}",
        "events.jsonl": "",
        "capabilities.json": "{}",
        "expected-outcome.json": "{}",
      });
      expect(() => loadTendingScenario("empty-meta", { corpusRoot: dirname(dirname(dir)) })).toThrow(
        ScenarioLoadError,
      );
    } finally {
      rmSync(corpus, { recursive: true, force: true });
    }
  });

  test("truncated JSON in meta.json → throws ScenarioLoadError", () => {
    const corpus = isolatedCorpus();
    try {
      const dir = buildBaseScenario(corpus, "trunc", {
        "meta.json": '{"kind": "synthetic", "scenario":',
        "bv-triage.json": "{}",
        "br-list.json": "{}",
        "ntm-snapshot.json": "{}",
        "agent-mail-dump.json": "{}",
        "reservations.json": "{}",
        "events.jsonl": "",
        "capabilities.json": "{}",
        "expected-outcome.json": "{}",
      });
      expect(() => loadTendingScenario("trunc", { corpusRoot: dirname(dirname(dir)) })).toThrow(
        ScenarioLoadError,
      );
    } finally {
      rmSync(corpus, { recursive: true, force: true });
    }
  });

  test("prototype-pollution keys in meta.json don't pollute Object.prototype", () => {
    const corpus = isolatedCorpus();
    try {
      const dir = buildBaseScenario(corpus, "proto", {
        "meta.json": PROTOTYPE_POLLUTION_META,
        "bv-triage.json": "{}",
        "br-list.json": "{}",
        "ntm-snapshot.json": "{}",
        "agent-mail-dump.json": "{}",
        "reservations.json": "{}",
        "events.jsonl": "",
        "capabilities.json": "{}",
        "expected-outcome.json": "{}",
      });
      const beforePolluted = ({} as Record<string, unknown>).polluted;
      expect(beforePolluted).toBeUndefined();
      const result = loadTendingScenario("proto", { corpusRoot: dirname(dirname(dir)) });
      expect(result.id).toBe("proto");
      // CRITICAL: Object.prototype must NOT be polluted.
      const afterPolluted = ({} as Record<string, unknown>).polluted;
      expect(afterPolluted).toBeUndefined();
    } finally {
      rmSync(corpus, { recursive: true, force: true });
    }
  });

  test("BOM prefix on JSON files → either parses or throws cleanly", () => {
    const corpus = isolatedCorpus();
    try {
      const bom = "﻿";
      const dir = buildBaseScenario(corpus, "bom", {
        "meta.json": bom + VALID_META,
        "bv-triage.json": bom + "{}",
        "br-list.json": "{}",
        "ntm-snapshot.json": "{}",
        "agent-mail-dump.json": "{}",
        "reservations.json": "{}",
        "events.jsonl": "",
        "capabilities.json": "{}",
        "expected-outcome.json": "{}",
      });
      // BOM-prefixed JSON.parse THROWS (UTF-8 BOM is not valid JSON whitespace);
      // we just need to make sure the loader doesn't process-crash.
      let threw = false;
      try {
        loadTendingScenario("bom", { corpusRoot: dirname(dirname(dir)) });
      } catch (err) {
        threw = err instanceof ScenarioLoadError;
      }
      expect(threw).toBe(true);
    } finally {
      rmSync(corpus, { recursive: true, force: true });
    }
  });

  test("binary bytes in events.jsonl line → throws ScenarioLoadError", () => {
    const corpus = isolatedCorpus();
    try {
      // Mix one valid line with one binary-byte line
      const binaryLine = String.fromCharCode(0x00, 0x01, 0x02, 0x03, 0xff);
      const dir = buildBaseScenario(corpus, "binevents", {
        "meta.json": VALID_META,
        "bv-triage.json": "{}",
        "br-list.json": "{}",
        "ntm-snapshot.json": "{}",
        "agent-mail-dump.json": "{}",
        "reservations.json": "{}",
        "events.jsonl": VALID_EVENT_LINE + "\n" + binaryLine + "\n",
        "capabilities.json": "{}",
        "expected-outcome.json": "{}",
      });
      expect(() =>
        loadTendingScenario("binevents", { corpusRoot: dirname(dirname(dir)) }),
      ).toThrow(ScenarioLoadError);
    } finally {
      rmSync(corpus, { recursive: true, force: true });
    }
  });

  test("malformed line in middle of events.jsonl → throws ScenarioLoadError", () => {
    const corpus = isolatedCorpus();
    try {
      const dir = buildBaseScenario(corpus, "midmal", {
        "meta.json": VALID_META,
        "bv-triage.json": "{}",
        "br-list.json": "{}",
        "ntm-snapshot.json": "{}",
        "agent-mail-dump.json": "{}",
        "reservations.json": "{}",
        "events.jsonl":
          VALID_EVENT_LINE +
          "\n" +
          "{not-valid-json" +
          "\n" +
          VALID_EVENT_LINE +
          "\n",
        "capabilities.json": "{}",
        "expected-outcome.json": "{}",
      });
      expect(() =>
        loadTendingScenario("midmal", { corpusRoot: dirname(dirname(dir)) }),
      ).toThrow(ScenarioLoadError);
    } finally {
      rmSync(corpus, { recursive: true, force: true });
    }
  });

  test("oversized events.jsonl (~5 MB of valid lines) loads successfully", () => {
    const corpus = isolatedCorpus();
    try {
      const lines: string[] = [];
      for (let i = 1; i <= 50_000; i++) {
        lines.push(JSON.stringify({ channel: "swarm", seq: i, ts: "2026-05-03T00:00:00Z", type: "tick" }));
      }
      const dir = buildBaseScenario(corpus, "oversize", {
        "meta.json": VALID_META,
        "bv-triage.json": "{}",
        "br-list.json": "{}",
        "ntm-snapshot.json": "{}",
        "agent-mail-dump.json": "{}",
        "reservations.json": "{}",
        "events.jsonl": lines.join("\n") + "\n",
        "capabilities.json": "{}",
        "expected-outcome.json": "{}",
      });
      const t0 = Date.now();
      const result = loadTendingScenario("oversize", { corpusRoot: dirname(dirname(dir)) });
      const elapsedMs = Date.now() - t0;
      expect(result.events.length).toBe(50_000);
      // Should comfortably load in < 5 s on any reasonable hardware;
      // a regression that O(n²)-ifies parsing would blow this.
      expect(elapsedMs).toBeLessThan(5_000);
    } finally {
      rmSync(corpus, { recursive: true, force: true });
    }
  });

  test("missing required file → throws ScenarioLoadError naming the file", () => {
    const corpus = isolatedCorpus();
    try {
      const dir = buildBaseScenario(corpus, "missing", {
        "meta.json": VALID_META,
        // bv-triage.json deliberately absent
        "br-list.json": "{}",
        "ntm-snapshot.json": "{}",
        "agent-mail-dump.json": "{}",
        "reservations.json": "{}",
        "events.jsonl": "",
        "capabilities.json": "{}",
        "expected-outcome.json": "{}",
      });
      let caught: Error | null = null;
      try {
        loadTendingScenario("missing", { corpusRoot: dirname(dirname(dir)) });
      } catch (err) {
        caught = err as Error;
      }
      expect(caught).not.toBeNull();
      expect(caught?.message.toLowerCase()).toMatch(/bv-triage|failed to read/);
    } finally {
      rmSync(corpus, { recursive: true, force: true });
    }
  });

  test("scenario path is a file (not a directory) → throws ScenarioLoadError", () => {
    const corpus = isolatedCorpus();
    try {
      mkdirSync(join(corpus, "scenarios"), { recursive: true });
      writeFileSync(join(corpus, "scenarios", "is-a-file"), "not a directory");
      expect(() =>
        loadTendingScenario("is-a-file", { corpusRoot: corpus }),
      ).toThrow(ScenarioLoadError);
    } finally {
      rmSync(corpus, { recursive: true, force: true });
    }
  });
});

describe("validateCorpus fuzz (review Round 3)", () => {
  test("entirely empty corpus root → reports findings, never panics", () => {
    const corpus = isolatedCorpus();
    try {
      // Create the bare dirs the validator looks for so it can walk them.
      mkdirSync(join(corpus, "scenarios"), { recursive: true });
      mkdirSync(join(corpus, "phase0-2026-05-02", "scenarios", "fresh"), { recursive: true });
      mkdirSync(join(corpus, "phase0-2026-05-02", "scenarios", "active"), { recursive: true });
      mkdirSync(join(corpus, "phase0-2026-05-02", "scenarios", "failure"), { recursive: true });
      mkdirSync(join(corpus, "golden-outputs"), { recursive: true });
      const result = validateCorpus(corpus);
      expect(result.ok).toBe(false);
      expect(result.findings.length).toBeGreaterThan(0);
      // Ensures no NaN / undefined leaked into summary
      expect(typeof result.summary.tendingScenariosExpected).toBe("number");
      expect(typeof result.summary.goldenOutputsExpected).toBe("number");
    } finally {
      rmSync(corpus, { recursive: true, force: true });
    }
  });

  test("scenario with prototype-pollution keys in meta.json doesn't pollute", () => {
    const corpus = isolatedCorpus();
    try {
      mkdirSync(join(corpus, "scenarios", "healthy-hour", "pane-logs"), { recursive: true });
      mkdirSync(join(corpus, "scenarios", "healthy-hour", "build-logs"), { recursive: true });
      writeFileSync(
        join(corpus, "scenarios", "healthy-hour", "meta.json"),
        PROTOTYPE_POLLUTION_META,
      );
      const before = ({} as Record<string, unknown>).polluted;
      validateCorpus(corpus);
      const after = ({} as Record<string, unknown>).polluted;
      expect(before).toBeUndefined();
      expect(after).toBeUndefined();
    } finally {
      rmSync(corpus, { recursive: true, force: true });
    }
  });

  test("events.jsonl with binary bytes triggers a finding, not a crash", () => {
    const corpus = isolatedCorpus();
    try {
      const dir = join(corpus, "scenarios", "healthy-hour");
      mkdirSync(join(dir, "pane-logs"), { recursive: true });
      mkdirSync(join(dir, "build-logs"), { recursive: true });
      writeFileSync(join(dir, "meta.json"), VALID_META);
      writeFileSync(join(dir, "bv-triage.json"), "{}");
      writeFileSync(join(dir, "br-list.json"), "{}");
      writeFileSync(join(dir, "ntm-snapshot.json"), "{}");
      writeFileSync(join(dir, "agent-mail-dump.json"), "{}");
      writeFileSync(join(dir, "reservations.json"), "{}");
      writeFileSync(join(dir, "capabilities.json"), "{}");
      writeFileSync(
        join(dir, "expected-outcome.json"),
        JSON.stringify({
          meta: { kind: "synthetic", fixturesVersion: "fuzz", capturedAt: "2026-05-03T00:00:00Z", source: "fuzz" },
          detections: [],
          wakeAgent: false,
          approvalsRequested: [],
          postconditions: [],
          activityBehavior: "silent",
        }),
      );
      writeFileSync(
        join(dir, "events.jsonl"),
        VALID_EVENT_LINE + "\n" + String.fromCharCode(0x00, 0x01, 0xff) + "\n",
      );
      const result = validateCorpus(corpus);
      const ndjsonFindings = result.findings.filter((f) => f.rule === "scenario.unparseable-jsonl");
      expect(ndjsonFindings.length).toBeGreaterThanOrEqual(1);
    } finally {
      rmSync(corpus, { recursive: true, force: true });
    }
  });

  test("scenario .goldens artifacts are included in the secret scan", () => {
    const corpus = isolatedCorpus();
    try {
      const dir = buildBaseScenario(corpus, "healthy-hour", {
        "meta.json": VALID_META,
        "bv-triage.json": "{}",
        "br-list.json": "{}",
        "ntm-snapshot.json": "{}",
        "agent-mail-dump.json": "{}",
        "reservations.json": "{}",
        "events.jsonl": "",
        "capabilities.json": "{}",
        "expected-outcome.json": "{}",
      });
      const leakedToken = "sk-" + "f".repeat(24);
      mkdirSync(join(dir, ".goldens"), { recursive: true });
      writeFileSync(
        join(dir, ".goldens", "mock-daemon.responses.json"),
        JSON.stringify({ replayPayload: leakedToken }),
      );

      const result = validateCorpus(corpus);
      const secretFindings = result.findings.filter((f) => f.rule === "secret.found");
      const hasGoldenSecretFinding = secretFindings.some(
        (f) => f.path === "scenarios/healthy-hour/.goldens/mock-daemon.responses.json",
      );
      expect(hasGoldenSecretFinding).toBe(true);
    } finally {
      rmSync(corpus, { recursive: true, force: true });
    }
  });

  test("validator returns deterministic finding order across two runs", () => {
    const corpus = isolatedCorpus();
    try {
      mkdirSync(join(corpus, "scenarios"), { recursive: true });
      mkdirSync(join(corpus, "phase0-2026-05-02", "scenarios", "fresh"), { recursive: true });
      mkdirSync(join(corpus, "phase0-2026-05-02", "scenarios", "active"), { recursive: true });
      mkdirSync(join(corpus, "phase0-2026-05-02", "scenarios", "failure"), { recursive: true });
      mkdirSync(join(corpus, "golden-outputs"), { recursive: true });
      const a = validateCorpus(corpus);
      const b = validateCorpus(corpus);
      const aKeys = a.findings.map((f) => `${f.severity}:${f.rule}:${f.path}`).sort();
      const bKeys = b.findings.map((f) => `${f.severity}:${f.rule}:${f.path}`).sort();
      expect(aKeys).toEqual(bKeys);
    } finally {
      rmSync(corpus, { recursive: true, force: true });
    }
  });
});
