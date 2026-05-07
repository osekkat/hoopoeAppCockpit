// hp-ngq — Daemon-spawn harness smoke test.
//
// Boots the Go daemon binary in mock mode, verifies /v1/health, then
// kills it. When the binary isn't built (desktop-only CI lanes), the
// test must report SKIPPED — not a green pass via early-return.
//
// hp-nu6: prior to this fix the test pretended to "pass" when the
// daemon binary was missing: tryStartDaemon returned ok:false, the
// test logged a "harness.skipped" snapshot, asserted start.reason
// contained "not found", and returned. CI then reported the suite
// as green even though the daemon never spawned. That's a
// false-confidence signal — a CI lane could flip to no-binary
// configuration and the test would silently keep "passing" without
// exercising shutdown or /v1/health at all.
//
// Use bun:test's `test.skipIf` so the absence of the binary is a
// statically visible SKIP in CI output, not a hidden green pass.

import { describe, expect, test } from "bun:test";
import { existsSync } from "node:fs";
import { resolve } from "node:path";
import { tryStartDaemon } from "../../../src/test-utils/daemon-harness/index.ts";
import { createPhase1TestLogger } from "../../../src/test-utils/structured-test-logger.ts";

const REPO_ROOT = resolve(__dirname, "..", "..", "..", "..", "..");
const DAEMON_BINARY = resolve(REPO_ROOT, "apps", "daemon", "bin", "hoopoed");
const daemonBinaryMissing = !existsSync(DAEMON_BINARY);

describe("hp-ngq :: daemon-harness smoke", () => {
  test.skipIf(daemonBinaryMissing)(
    "spawns daemon, /v1/health returns 200, kill is idempotent",
    async () => {
      const logger = createPhase1TestLogger({
        suite: "hp-ngq.harness-smoke",
        testName: "spawn-health-kill",
      });
      logger.start();

      logger.phase("setup");
      const start = await tryStartDaemon({ repoRoot: REPO_ROOT, mode: "mock" });
      // The skipIf above guarantees the binary exists when we reach this
      // line; if start.ok is still false, that's a real harness failure
      // (port collision, env mismatch, immediate crash) — fail loudly
      // instead of recording a benign snapshot.
      if (!start.ok) {
        throw new Error(`daemon harness failed despite binary present: ${start.reason}`);
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
    },
    15_000,
  );
});
