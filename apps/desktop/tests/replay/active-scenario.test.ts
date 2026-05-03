// hp-q3t — Phase 0 fixture-replay e2e: `active` scenario.
//
// "Active" represents a mid-swarm capture taken the same way as `fresh`
// but at a different point in the timeline. The fixture's tool presence
// happens to be identical for now (only git/agent_mail/health on the
// captured VPS), so this test exercises:
//   - explicit `markStageReached` for renderer-only stage transitions
//   - cross-scenario determinism: same scenario id + same boot clock =
//     identical baseline event count
//   - the secret-scrub allow-list (mock auth tokens must not flag)

import { describe, expect, test } from "bun:test";
import {
  ALLOWED_SECRET_LITERALS,
  assertNoUnredactedSecrets,
  bootMockFlywheel,
  expectAdapterCalled,
  expectStageReached,
  getEmittedEvents,
} from "../../src/test-utils/replay/index.ts";
import { createPhase1TestLogger } from "../../src/test-utils/structured-test-logger.ts";

describe("hp-q3t fixture-replay :: active scenario", () => {
  test("two boots with the same clock produce the same baseline event seqs", () => {
    const logger = createPhase1TestLogger({ suite: "hp-q3t.replay.active", testName: "deterministic-baseline" });
    logger.start();
    logger.phase("setup");
    const fixedClock = () => 1_700_000_000_000;

    logger.phase("act");
    const a = bootMockFlywheel({ scenario: "active", now: fixedClock });
    const b = bootMockFlywheel({ scenario: "active", now: fixedClock });

    try {
      const aEvents = getEmittedEvents(a);
      const bEvents = getEmittedEvents(b);
      const aSerialized = JSON.stringify(aEvents);
      const bSerialized = JSON.stringify(bEvents);

      logger.snapshot("active.deterministic", {
        eventCount: aEvents.length,
        equal: aSerialized === bSerialized,
        firstSeq: aEvents[0]?.seq ?? null,
        lastSeq: aEvents.at(-1)?.seq ?? null,
      });

      logger.phase("assert");
      expect(aEvents.length).toBe(bEvents.length);
      expect(aSerialized).toBe(bSerialized);
      expect(aEvents.length).toBeGreaterThan(0);
    } finally {
      logger.phase("teardown");
      a.close();
      b.close();
    }
  });

  test("renderer-only stage transition via markStageReached", () => {
    const logger = createPhase1TestLogger({ suite: "hp-q3t.replay.active", testName: "markStageReached" });
    logger.start();
    logger.phase("setup");
    const client = bootMockFlywheel({ scenario: "active" });
    try {
      logger.phase("act");
      // Hardening doesn't have a present-tool adapter in `active`, but the
      // renderer can still mount the stage shell. `markStageReached` lets
      // tests assert that flow without inventing fake adapter calls.
      client.markStageReached("hardening", "shell-mounted");
      client.callAdapter("git", "log_short");
      client.callAdapter("agent_mail", "fetch_inbox");

      logger.snapshot("active.stages", {
        reached: client.reachedStages(),
        callCount: client.recordedCalls().length,
      });

      logger.phase("assert");
      expectStageReached(client, "hardening");
      expectStageReached(client, "planning");
      expectAdapterCalled(client, "git", "log_short");

      // Calls advance the events list past the baseline.
      expect(client.emittedEvents().length).toBeGreaterThan(client.recordedCalls().length);
    } finally {
      logger.phase("teardown");
      client.close();
    }
  });

  test("mock auth literals do not trip the secret scanner", () => {
    const logger = createPhase1TestLogger({ suite: "hp-q3t.replay.active", testName: "secret-scrub-allow-list" });
    logger.start();
    logger.phase("setup");
    const client = bootMockFlywheel({ scenario: "active" });
    try {
      logger.phase("act");
      client.callAdapter("agent_mail", "exchange_pairing_for_bearer");
      // Inject events shaped like the AuthBridge would emit when running
      // against the mock daemon. The values are the loud, allow-listed
      // mock literals; the scanner must let them through.
      for (const literal of ALLOWED_SECRET_LITERALS) {
        client.emit({
          channel: "auth",
          seq: 0,
          ts: client.scenario().snapshot.meta.capturedAt,
          type: "auth.token-issued",
          payload: { token: literal, kind: "mock" },
        });
      }
      // Also inject a benign event with no secret content.
      client.emit({
        channel: "auth",
        seq: 0,
        ts: client.scenario().snapshot.meta.capturedAt,
        type: "auth.session-bound",
        payload: { sessionId: "active-test-session" },
      });

      logger.phase("assert");
      const events = getEmittedEvents(client);
      const result = assertNoUnredactedSecrets(events);
      logger.snapshot("active.scrub", { eventCount: result.events, findings: result.findings });
      expect(result.findings).toEqual([]);
      expect(result.events).toBe(events.length);
    } finally {
      logger.phase("teardown");
      client.close();
    }
  });
});
