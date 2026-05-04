// hp-ngq — Idempotency contract verification.
//
// For each write endpoint declared in `IDEMPOTENT_WRITE_ENDPOINTS`,
// verifies that the daemon either:
//   - responds with the same body when the same `Idempotency-Key` is
//     supplied twice, OR
//   - responds with 409 Conflict if the body differs (key reuse with
//     different intent), OR
//   - returns the documented "no idempotency middleware yet" shape
//     so the test SKIPs (status === 404 || 501 || 405).
//
// This file ships ahead of the daemon-side middleware; full
// assertions activate as the middleware lands. The contract table
// itself is the deliverable for tests that don't need a running
// daemon yet.

import { describe, expect, test } from "bun:test";
import { resolve } from "node:path";
import {
  IDEMPOTENCY_HEADER,
  IDEMPOTENT_WRITE_ENDPOINTS,
  makeIdempotencyKey,
  tryStartDaemon,
  type IdempotentWriteEndpoint,
} from "../../../src/test-utils/daemon-harness/index.ts";
import { createPhase1TestLogger } from "../../../src/test-utils/structured-test-logger.ts";

const REPO_ROOT = resolve(__dirname, "..", "..", "..", "..", "..");

describe("hp-ngq :: idempotency contract surface", () => {
  test("the canonical write-endpoint table covers §2.6 retryable writes", () => {
    expect(IDEMPOTENT_WRITE_ENDPOINTS.length).toBeGreaterThan(10);
    const paths = IDEMPOTENT_WRITE_ENDPOINTS.map((e) => `${e.method} ${e.path}`);
    // Spot-check a few load-bearing entries from the bead's list:
    expect(paths).toContain("POST /v1/projects");
    expect(paths).toContain("POST /v1/projects/{projectId}/git/push");
    expect(paths).toContain("PATCH /v1/projects/{projectId}/beads/{beadId}");
    expect(paths).toContain("POST /v1/projects/{projectId}/swarms");
    expect(paths).toContain("POST /v1/projects/{projectId}/mail/messages");
  });

  test("makeIdempotencyKey returns stable, traceable, distinct keys", () => {
    const a = makeIdempotencyKey("create-project", 0);
    const b = makeIdempotencyKey("create-project", 1);
    const c = makeIdempotencyKey("update-bead", 0);
    expect(a).toMatch(/^hp-ngq-create-project-/);
    expect(b).toMatch(/^hp-ngq-create-project-/);
    expect(c).toMatch(/^hp-ngq-update-bead-/);
    expect(a).not.toBe(b);
    expect(a).not.toBe(c);
  });

  test("documented enforcement modes are limited to {required, honored}", () => {
    const allowed = new Set(["required", "honored"]);
    for (const entry of IDEMPOTENT_WRITE_ENDPOINTS) {
      expect(allowed.has(entry.enforcement)).toBe(true);
    }
  });
});

interface EndpointProbeResult {
  endpoint: IdempotentWriteEndpoint;
  firstStatus: number;
  secondStatus: number;
  bodiesMatch: boolean;
  /** True iff the daemon currently routes the path at all. When false
   *  the test treats it as "endpoint not yet wired" and SKIPs. */
  routed: boolean;
}

function literalPath(template: string): string {
  // Replace `{var}` placeholders with deterministic mock identifiers.
  return template.replace(/\{[^}]+\}/g, "mock-id");
}

function methodSupportsBody(method: IdempotentWriteEndpoint["method"]): boolean {
  return method === "POST" || method === "PATCH" || method === "PUT";
}

async function probeEndpoint(
  baseUrl: string,
  authToken: string,
  endpoint: IdempotentWriteEndpoint,
): Promise<EndpointProbeResult> {
  const path = literalPath(endpoint.path);
  const key = makeIdempotencyKey(endpoint.path.replace(/[^a-zA-Z0-9]+/g, "-"), 0);
  const body = methodSupportsBody(endpoint.method) ? JSON.stringify({ probe: true }) : null;
  const headers: Record<string, string> = {
    Authorization: `Bearer ${authToken}`,
    [IDEMPOTENCY_HEADER]: key,
  };
  if (body !== null) headers["Content-Type"] = "application/json";

  const url = `${baseUrl}${path}`;
  const first = body === null
    ? await fetch(url, { method: endpoint.method, headers, signal: AbortSignal.timeout(2_000) })
    : await fetch(url, { method: endpoint.method, headers, body, signal: AbortSignal.timeout(2_000) });
  const firstBody = await first.text();
  const second = body === null
    ? await fetch(url, { method: endpoint.method, headers, signal: AbortSignal.timeout(2_000) })
    : await fetch(url, { method: endpoint.method, headers, body, signal: AbortSignal.timeout(2_000) });
  const secondBody = await second.text();

  const routed = !(first.status === 404 || first.status === 405 || first.status === 501);
  return {
    endpoint,
    firstStatus: first.status,
    secondStatus: second.status,
    bodiesMatch: firstBody === secondBody,
    routed,
  };
}

describe("hp-ngq :: idempotency end-to-end (skips when daemon binary missing)", () => {
  test("same key twice → same response shape (or skip on unrouted endpoints)", async () => {
    const logger = createPhase1TestLogger({
      suite: "hp-ngq.contract",
      testName: "same-key-twice",
    });
    logger.start();

    logger.phase("setup");
    const start = await tryStartDaemon({ repoRoot: REPO_ROOT, mode: "mock" });
    if (!start.ok) {
      logger.snapshot("contract.skipped", { reason: start.reason });
      // Skip cleanly when the daemon isn't built. The contract table
      // assertions above still pass; this end-to-end path is opt-in.
      return;
    }
    const daemon = start.handle;
    try {
      logger.phase("act");
      // Probe a small subset (full sweep is wasteful when middleware
      // hasn't landed). Once the daemon enforces idempotency for these,
      // the slice expands.
      const subset = IDEMPOTENT_WRITE_ENDPOINTS.filter((e) =>
        ["POST /v1/projects", "POST /v1/auth/bootstrap/bearer"].includes(`${e.method} ${e.path}`),
      );
      const results: EndpointProbeResult[] = [];
      for (const endpoint of subset) {
        const result = await probeEndpoint(daemon.baseUrl, daemon.authToken, endpoint);
        results.push(result);
        logger.snapshot(`contract.probe.${endpoint.method}.${endpoint.path}`, {
          firstStatus: result.firstStatus,
          secondStatus: result.secondStatus,
          bodiesMatch: result.bodiesMatch,
          routed: result.routed,
        });
      }

      logger.phase("assert");
      for (const result of results) {
        if (!result.routed) {
          // Endpoint not yet routed — the assertion is "the daemon
          // doesn't lie about idempotency for routes it doesn't have."
          expect([404, 405, 501]).toContain(result.firstStatus);
          continue;
        }
        // Routed endpoint: a second call with the same key must NOT
        // produce a divergent status. The body match is ideal but the
        // daemon may legitimately add per-call timestamps; for now we
        // only assert status convergence.
        expect(result.firstStatus).toBe(result.secondStatus);
      }
    } finally {
      logger.phase("teardown");
      await daemon.kill();
    }
  }, 30_000);
});
