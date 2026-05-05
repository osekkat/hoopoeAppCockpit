// `@hoopoe/fixture-replay` — unit tests for the harness (hp-w8q1 part 1).
//
// hp-w8q1 observed that apps/desktop/tests/replay/* exercises bootMockFlywheel
// behavior at the desktop e2e level, where a regression in the harness can
// hide behind harness lifecycle that the test itself synthesized. These tests
// pin the harness contract directly so callAdapter / markStageReached /
// expectStageReached / expectAdapterCalled / assertNoUnredactedSecrets each
// have at least one independent unit-level assertion that does not depend on
// the apps/desktop replay tests at all.
//
// Loads the real Phase 0 'fresh' scenario from packages/fixtures/phase0-2026-05-02/
// — the goal is to exercise the harness against the same shape it sees in
// production tests, not to replace fixture-quality / golden-replay coverage.

import { describe, expect, test } from "bun:test";
import {
  ALLOWED_SECRET_LITERALS,
  STAGE_ADAPTERS,
  STAGE_IDS,
  assertNoUnredactedSecrets,
  bootMockFlywheel,
  bootScenarioLibrary,
  expectAdapterCalled,
  expectAdapterNotCalled,
  expectStageReached,
  FixtureReplayAssertionError,
  isStageId,
  stageForAdapter,
} from "../src/index.ts";

const PINNED_NOW = () => Date.UTC(2026, 4, 4, 0, 0, 0);

describe("stages.ts", () => {
  test("STAGE_IDS lists exactly the four cockpit stages", () => {
    expect(STAGE_IDS).toEqual(["planning", "beads", "swarm", "hardening"] as const);
  });

  test("STAGE_ADAPTERS maps every stage to a non-empty adapter list", () => {
    for (const stage of STAGE_IDS) {
      expect(STAGE_ADAPTERS[stage].length).toBeGreaterThan(0);
    }
  });

  test("isStageId accepts only the four canonical ids", () => {
    expect(isStageId("planning")).toBe(true);
    expect(isStageId("hardening")).toBe(true);
    expect(isStageId("debugging")).toBe(false);
    expect(isStageId("")).toBe(false);
  });

  test("stageForAdapter returns each declared stage exactly once", () => {
    expect(stageForAdapter("br")).toEqual(["beads"]);
    expect(stageForAdapter("ntm")).toEqual(["swarm"]);
    expect(stageForAdapter("agent_mail")).toEqual(["planning", "swarm"]);
    expect(stageForAdapter("ubs")).toEqual(["hardening"]);
  });

  test("stageForAdapter returns empty for unmapped adapters (e.g. casr)", () => {
    expect(stageForAdapter("casr")).toEqual([]);
    expect(stageForAdapter("rch")).toEqual([]);
  });
});

describe("bootMockFlywheel", () => {
  test("returns a client whose scenarioId matches the requested scenario", () => {
    const client = bootMockFlywheel({ scenario: "fresh", now: PINNED_NOW });
    expect(client.scenarioId()).toBe("fresh");
  });

  test("health() reports the deterministic mock-flywheel envelope", () => {
    const client = bootMockFlywheel({ scenario: "fresh", now: PINNED_NOW });
    const h = client.health();
    expect(h.status).toBe("ok");
    expect(h.environment).toBe("mock-flywheel");
    // Time tracks the injected clock — we asked for the same epoch ms twice.
    expect(h.time).toBe(new Date(PINNED_NOW()).toISOString());
  });

  test("baseline events are deterministic across two boots of the same scenario", () => {
    const a = bootMockFlywheel({ scenario: "fresh", now: PINNED_NOW });
    const b = bootMockFlywheel({ scenario: "fresh", now: PINNED_NOW });
    const eventsA = a.emittedEvents();
    const eventsB = b.emittedEvents();
    expect(eventsA.length).toBeGreaterThan(0);
    expect(eventsA.length).toBe(eventsB.length);
    // Channel/seq/type are derived deterministically from the snapshot;
    // payload is the same captured snapshot data — JSON.stringify is a
    // sufficient byte-identity check.
    expect(JSON.stringify(eventsA)).toBe(JSON.stringify(eventsB));
  });

  test("baseline events have a strictly increasing seq starting at 1", () => {
    const client = bootMockFlywheel({ scenario: "fresh", now: PINNED_NOW });
    const events = client.emittedEvents();
    expect(events[0]?.seq).toBe(1);
    for (let i = 1; i < events.length; i += 1) {
      expect((events[i]?.seq ?? 0) > (events[i - 1]?.seq ?? 0)).toBe(true);
    }
  });

  test("toolPresence reports every declared adapter, including absent ones", () => {
    const client = bootMockFlywheel({ scenario: "fresh", now: PINNED_NOW });
    const declared = client.declaredAdapters();
    const presence = client.toolPresence();
    for (const adapter of declared) {
      expect(adapter in presence).toBe(true);
      expect(typeof presence[adapter]).toBe("boolean");
    }
  });

  test("declaredAdapters is non-empty and includes the canonical core tools", () => {
    const client = bootMockFlywheel({ scenario: "fresh", now: PINNED_NOW });
    const declared = client.declaredAdapters();
    expect(declared).toContain("git");
    expect(declared).toContain("br");
    expect(declared).toContain("bv");
    expect(declared).toContain("ntm");
  });
});

describe("bootScenarioLibrary", () => {
  test("returns one client per Phase 0 scenario by default (fresh, active, failure)", () => {
    const library = bootScenarioLibrary({ now: PINNED_NOW });
    expect(library.length).toBe(3);
    expect(library.map((entry) => entry.scenario).sort()).toEqual([
      "active",
      "failure",
      "fresh",
    ]);
  });

  test("respects the scenarios subset option", () => {
    const library = bootScenarioLibrary({ scenarios: ["fresh"], now: PINNED_NOW });
    expect(library.length).toBe(1);
    expect(library[0]?.scenario).toBe("fresh");
  });
});

describe("ReplayClient.callAdapter", () => {
  test("records a call against an absent adapter and emits adapter.degraded", () => {
    const client = bootMockFlywheel({ scenario: "fresh", now: PINNED_NOW });
    const baselineCount = client.emittedEvents().length;
    // br is in the fresh-VPS snapshot with present=false (binary missing on
    // PATH). callAdapter still returns the ToolCapture so callers can read
    // skipReason without a second lookup; the call goes down the
    // "adapter.degraded" event path.
    expect(client.toolPresence().br).toBe(false);
    const result = client.callAdapter("br", "list");
    expect(result).not.toBeNull();
    expect(result?.present).toBe(false);
    expect(result?.skipReason).toMatch(/br/);
    const calls = client.recordedCalls();
    expect(calls.length).toBe(1);
    expect(calls[0]?.adapter).toBe("br");
    expect(calls[0]?.method).toBe("list");
    expect(calls[0]?.toolPresent).toBe(false);
    const events = client.emittedEvents();
    expect(events.length).toBe(baselineCount + 1);
    const last = events.at(-1);
    expect(last?.type).toBe("adapter.degraded");
    expect(last?.payload).toMatchObject({ tool: "br", method: "list" });
  });

  test("returns null for adapters not in the snapshot at all", () => {
    const client = bootMockFlywheel({ scenario: "fresh", now: PINNED_NOW });
    // Adapters declared in the index but not captured (e.g. rch on this
    // corpus) should report null from callAdapter while still recording
    // the call as toolPresent=false — the "tool genuinely missing"
    // signal that distinguishes index-only declarations from snapshot
    // entries.
    const result = client.callAdapter("does_not_exist_xyz", "anything");
    expect(result).toBeNull();
    expect(client.recordedCalls().at(-1)?.toolPresent).toBe(false);
    expect(client.emittedEvents().at(-1)?.type).toBe("adapter.degraded");
  });

  test("records a call against a present adapter and emits adapter.invoked", () => {
    const client = bootMockFlywheel({ scenario: "fresh", now: PINNED_NOW });
    // git is present on the fresh-VPS scenario.
    expect(client.toolPresence().git).toBe(true);
    const result = client.callAdapter("git", "status");
    expect(result).not.toBeNull();
    expect(result?.tool).toBe("git");
    const events = client.emittedEvents();
    expect(events.at(-1)?.type).toBe("adapter.invoked");
    expect(events.at(-1)?.payload).toMatchObject({ tool: "git", method: "status" });
  });

  test("recording the same adapter twice produces two distinct call records", () => {
    const client = bootMockFlywheel({ scenario: "fresh", now: PINNED_NOW });
    client.callAdapter("br", "list");
    client.callAdapter("br", "ready");
    const calls = client.recordedCalls();
    expect(calls.length).toBe(2);
    expect(calls.map((c) => c.method)).toEqual(["list", "ready"]);
  });

  test("calling a stage-mapped adapter marks the corresponding stage", () => {
    const client = bootMockFlywheel({ scenario: "fresh", now: PINNED_NOW });
    expect(client.reachedStages()).toEqual([]);
    client.callAdapter("br", "list");
    expect(client.reachedStages()).toContain("beads");
  });

  test("calling agent_mail marks both planning and swarm stages", () => {
    const client = bootMockFlywheel({ scenario: "fresh", now: PINNED_NOW });
    client.callAdapter("agent_mail", "fetch_inbox");
    const reached = client.reachedStages();
    expect(reached).toContain("planning");
    expect(reached).toContain("swarm");
  });

  test("calling an unmapped adapter (rch) does NOT mark any stage", () => {
    const client = bootMockFlywheel({ scenario: "fresh", now: PINNED_NOW });
    client.callAdapter("rch", "exec");
    expect(client.reachedStages()).toEqual([]);
  });
});

describe("ReplayClient.markStageReached", () => {
  test("explicitly marks a stage even without an adapter call", () => {
    const client = bootMockFlywheel({ scenario: "fresh", now: PINNED_NOW });
    expect(client.reachedStages()).toEqual([]);
    client.markStageReached("hardening", "explicit");
    expect(client.reachedStages()).toContain("hardening");
  });

  test("is idempotent — repeated calls do not add duplicates", () => {
    const client = bootMockFlywheel({ scenario: "fresh", now: PINNED_NOW });
    client.markStageReached("hardening");
    client.markStageReached("hardening");
    const reached = client.reachedStages();
    expect(reached.filter((s) => s === "hardening").length).toBe(1);
  });
});

describe("ReplayClient.emit", () => {
  test("appends test events with a monotonic seq above the baseline tail", () => {
    const client = bootMockFlywheel({ scenario: "fresh", now: PINNED_NOW });
    const baselineTail = client.emittedEvents().at(-1)?.seq ?? 0;
    // Pass a deliberately too-low seq — the harness should bump it.
    client.emit({
      channel: "audit",
      seq: 1,
      ts: "2026-05-04T12:00:00Z",
      type: "audit.test",
      payload: { test: true },
    });
    const tail = client.emittedEvents().at(-1);
    expect(tail?.type).toBe("audit.test");
    expect((tail?.seq ?? 0) > baselineTail).toBe(true);
  });

  test("preserves caller-supplied seq when it is already monotonic", () => {
    const client = bootMockFlywheel({ scenario: "fresh", now: PINNED_NOW });
    const baselineTail = client.emittedEvents().at(-1)?.seq ?? 0;
    const desired = baselineTail + 1000;
    client.emit({
      channel: "audit",
      seq: desired,
      ts: "2026-05-04T12:00:00Z",
      type: "audit.test",
    });
    expect(client.emittedEvents().at(-1)?.seq).toBe(desired);
  });
});

describe("ReplayClient.close", () => {
  test("subsequent callAdapter throws after close()", () => {
    const client = bootMockFlywheel({ scenario: "fresh", now: PINNED_NOW });
    client.close();
    expect(() => client.callAdapter("br", "list")).toThrow(/after close\(\)/);
  });

  test("close() is idempotent", () => {
    const client = bootMockFlywheel({ scenario: "fresh", now: PINNED_NOW });
    client.close();
    expect(() => client.close()).not.toThrow();
  });
});

describe("expectStageReached", () => {
  test("passes when the stage was reached via callAdapter", () => {
    const client = bootMockFlywheel({ scenario: "fresh", now: PINNED_NOW });
    client.callAdapter("br", "list");
    expect(() => expectStageReached(client, "beads")).not.toThrow();
  });

  test("throws FixtureReplayAssertionError when the stage was not reached", () => {
    const client = bootMockFlywheel({ scenario: "fresh", now: PINNED_NOW });
    expect(() => expectStageReached(client, "beads")).toThrow(FixtureReplayAssertionError);
  });

  test("throws when given an unknown stage id", () => {
    const client = bootMockFlywheel({ scenario: "fresh", now: PINNED_NOW });
    expect(() => expectStageReached(client, "debugging")).toThrow(
      /not a known stage id/,
    );
  });
});

describe("expectAdapterCalled / expectAdapterNotCalled", () => {
  test("expectAdapterCalled passes after the matching call", () => {
    const client = bootMockFlywheel({ scenario: "fresh", now: PINNED_NOW });
    client.callAdapter("br", "list");
    expect(() => expectAdapterCalled(client, "br", "list")).not.toThrow();
  });

  test("expectAdapterCalled throws when the method does not match", () => {
    const client = bootMockFlywheel({ scenario: "fresh", now: PINNED_NOW });
    client.callAdapter("br", "list");
    expect(() => expectAdapterCalled(client, "br", "ready")).toThrow(
      FixtureReplayAssertionError,
    );
  });

  test("expectAdapterNotCalled passes when the call never happened", () => {
    const client = bootMockFlywheel({ scenario: "fresh", now: PINNED_NOW });
    expect(() => expectAdapterNotCalled(client, "br", "list")).not.toThrow();
  });

  test("expectAdapterNotCalled throws after the call happens", () => {
    const client = bootMockFlywheel({ scenario: "fresh", now: PINNED_NOW });
    client.callAdapter("br", "list");
    expect(() => expectAdapterNotCalled(client, "br", "list")).toThrow(
      FixtureReplayAssertionError,
    );
  });
});

describe("assertNoUnredactedSecrets", () => {
  test("passes for the baseline event stream of a fresh scenario", () => {
    const client = bootMockFlywheel({ scenario: "fresh", now: PINNED_NOW });
    const result = assertNoUnredactedSecrets(client.emittedEvents());
    expect(result.findings.length).toBe(0);
    expect(result.events).toBeGreaterThan(0);
  });

  test("ignores allow-listed mock literals (MOCKMOCKMOCK et al.)", () => {
    // Sanity: the allow-list export is non-empty. Without this list the
    // scanner could trip on intentional mock-token shapes the rest of the
    // suite emits as deterministic placeholders.
    expect(ALLOWED_SECRET_LITERALS.length).toBeGreaterThan(0);
    const client = bootMockFlywheel({ scenario: "fresh", now: PINNED_NOW });
    const literal = ALLOWED_SECRET_LITERALS[0] ?? "MOCKMOCKMOCK";
    client.emit({
      channel: "audit",
      seq: 9999,
      ts: "2026-05-04T12:00:00Z",
      type: "audit.test",
      payload: { token: literal },
    });
    expect(() => assertNoUnredactedSecrets(client.emittedEvents())).not.toThrow();
  });

  test("throws when an emitted event carries a high-entropy unredacted secret", () => {
    const client = bootMockFlywheel({ scenario: "fresh", now: PINNED_NOW });
    // Synthesized — not a real key. The `sk-ant-` prefix is a known
    // provider shape the scanner is required to flag.
    const fakeKey = `sk-ant-${"AbCdEf0123456789".repeat(4)}`;
    client.emit({
      channel: "audit",
      seq: 9999,
      ts: "2026-05-04T12:00:00Z",
      type: "audit.test",
      payload: { token: fakeKey },
    });
    expect(() => assertNoUnredactedSecrets(client.emittedEvents())).toThrow(
      FixtureReplayAssertionError,
    );
  });
});
