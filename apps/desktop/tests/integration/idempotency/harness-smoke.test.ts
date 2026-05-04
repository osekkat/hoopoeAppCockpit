// hp-ngq — Daemon-spawn harness smoke test.
//
// Boots the Go daemon binary in mock mode, verifies /v1/health, then
// kills it. Skips gracefully when the binary isn't built so this
// suite doesn't break desktop-only CI lanes.

import { describe, expect, test } from "bun:test";
import { resolve } from "node:path";
import { tryStartDaemon } from "../../../src/test-utils/daemon-harness/index.ts";
import { createPhase1TestLogger } from "../../../src/test-utils/structured-test-logger.ts";

const REPO_ROOT = resolve(__dirname, "..", "..", "..", "..", "..");

describe("hp-ngq :: daemon-harness smoke", () => {
  test("spawns daemon, /v1/health returns 200, kill is idempotent", async () => {
    const logger = createPhase1TestLogger({
      suite: "hp-ngq.harness-smoke",
      testName: "spawn-health-kill",
    });
    logger.start();

    logger.phase("setup");
    const start = await tryStartDaemon({ repoRoot: REPO_ROOT, mode: "mock" });
    if (!start.ok) {
      logger.snapshot("harness.skipped", { reason: start.reason });
      // Bun's `test.skip` doesn't have a runtime equivalent on a
      // dynamic skip — we record the skip in the logger and return
      // early. The test stays "passed" with a snapshot indicating it
      // didn't actually run.
      expect(start.reason).toContain("not found");
      return;
    }
    const daemon = start.handle;
    try {
      logger.snapshot("harness.started", {
        port: daemon.port,
        baseUrl: daemon.baseUrl,
        daemonHome: daemon.daemonHome,
      });

      logger.phase("act");
      const health = await fetch(`${daemon.baseUrl}/v1/health`, {
        signal: AbortSignal.timeout(2_000),
      });
      const body = await health.text();
      logger.snapshot("harness.health", {
        status: health.status,
        bodyBytes: body.length,
        bodyPreview: body.slice(0, 200),
      });

      logger.phase("assert");
      expect(health.status).toBe(200);
      expect(daemon.baseUrl).toMatch(/^http:\/\/127\.0\.0\.1:\d+$/);
      expect(daemon.port).toBeGreaterThan(1024);
      expect(daemon.authToken.length).toBeGreaterThan(0);
    } finally {
      logger.phase("teardown");
      await daemon.kill();
      // Idempotent: a second kill must not throw.
      await daemon.kill();
    }
  }, 15_000);
});
