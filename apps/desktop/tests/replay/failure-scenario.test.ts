// hp-q3t — Phase 0 fixture-replay e2e: `failure` scenario.
//
// "Failure" was captured against a degraded VPS where:
//   - br/bv/ntm and most other tools were not present (skipReason set).
//   - `health` reported `lizard not on PATH`.
//
// This test exercises the missing-tool / degraded-tool flow that the
// renderer Activity panel must surface (plan.md §8.8 missing-tool
// scenario, in spirit) plus the secret-scanner's behavior on a fixture
// rich with command output.

import { describe, expect, test } from "bun:test";
import {
  FixtureReplayAssertionError,
  assertNoUnredactedSecrets,
  bootMockFlywheel,
  bootScenarioLibrary,
  expectAdapterCalled,
  expectStageReached,
  getEmittedEvents,
} from "../../src/test-utils/replay/index.ts";
import { createPhase1TestLogger } from "../../src/test-utils/structured-test-logger.ts";

describe("hp-q3t fixture-replay :: failure scenario", () => {
  test("missing-tool calls record degraded events without throwing", () => {
    const logger = createPhase1TestLogger({ suite: "hp-q3t.replay.failure", testName: "missing-tool" });
    logger.start();
    logger.phase("setup");
    const client = bootMockFlywheel({ scenario: "failure" });
    try {
      logger.phase("act");
      const brCapture = client.callAdapter("br", "list");
      const bvCapture = client.callAdapter("bv", "robot_triage");
      const ntmCapture = client.callAdapter("ntm", "snapshot");
      const healthCapture = client.callAdapter("health", "lizard");

      logger.snapshot("failure.calls", {
        brPresent: brCapture?.present ?? null,
        bvPresent: bvCapture?.present ?? null,
        ntmPresent: ntmCapture?.present ?? null,
        healthErrors: healthCapture?.errors ?? null,
      });

      logger.phase("assert");
      expect(brCapture?.present).toBe(false);
      expect(brCapture?.skipReason).toBe("missing binary on PATH: br");
      expect(bvCapture?.present).toBe(false);
      expect(ntmCapture?.present).toBe(false);
      // health is present but reports an error (`lizard not on PATH`).
      expect(healthCapture?.present).toBe(true);
      expect(healthCapture?.errors).toContain("lizard not on PATH");

      expectAdapterCalled(client, "br", "list");
      expectAdapterCalled(client, "bv", "robot_triage");

      // Events from the four calls land at the tail of the event log.
      const tail = getEmittedEvents(client).slice(-4);
      expect(tail.map((e) => e.type)).toEqual([
        "adapter.degraded",
        "adapter.degraded",
        "adapter.degraded",
        "adapter.invoked",
      ]);
      // The br degraded event carries the skipReason from the snapshot.
      expect(tail[0]?.payload).toMatchObject({
        tool: "br",
        method: "list",
        reason: "missing binary on PATH: br",
      });

      expectStageReached(client, "beads");
      expectStageReached(client, "swarm");
      expectStageReached(client, "hardening");
    } finally {
      logger.phase("teardown");
      client.close();
    }
  });

  test("expectStageReached throws with a useful diagnostic when the stage is unreached", () => {
    const logger = createPhase1TestLogger({ suite: "hp-q3t.replay.failure", testName: "stage-unreached" });
    logger.start();
    logger.phase("setup");
    const client = bootMockFlywheel({ scenario: "failure" });
    try {
      logger.phase("act");
      // No adapter calls and no markStageReached.
      let caught: unknown = null;
      try {
        expectStageReached(client, "planning");
      } catch (err) {
        caught = err;
      }

      logger.snapshot("failure.unreached", { error: String(caught ?? "") });

      logger.phase("assert");
      expect(caught).toBeInstanceOf(FixtureReplayAssertionError);
      const message = (caught as Error).message;
      expect(message).toContain("planning");
      expect(message).toContain("scenario 'failure'");
    } finally {
      logger.phase("teardown");
      client.close();
    }
  });

  test("scenario library boots all three Phase 0 scenarios in one call", () => {
    const logger = createPhase1TestLogger({ suite: "hp-q3t.replay.failure", testName: "scenario-library" });
    logger.start();
    logger.phase("setup");

    logger.phase("act");
    const library = bootScenarioLibrary({});
    try {
      const ids = library.map((entry) => entry.scenario);
      const baselineCounts = library.map((entry) => entry.client.emittedEvents().length);

      logger.snapshot("failure.library", { ids, baselineCounts });

      logger.phase("assert");
      expect(ids).toEqual(["fresh", "active", "failure"]);
      // Each baseline is non-empty.
      for (const count of baselineCounts) {
        expect(count).toBeGreaterThan(0);
      }
      // Every loaded client should pass the secret scrub against its
      // synthesized baseline events — fixtures must be redacted at
      // capture time.
      for (const { scenario, client } of library) {
        const result = assertNoUnredactedSecrets(client.emittedEvents());
        expect(result.findings).toEqual([]);
        expect(client.scenarioId()).toBe(scenario);
      }
    } finally {
      logger.phase("teardown");
      for (const { client } of library) {
        client.close();
      }
    }
  });
});
