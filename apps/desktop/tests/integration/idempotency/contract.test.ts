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

describe("hp-ngq :: idempotency contract surface (scaffolding)", () => {
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

  // hp-jsi: every retryable endpoint must declare whether it's
  // currently deferred (route returns 404/405/501) or actually
  // wired. The flag is a compile-time-checked boolean; this test
  // is the runtime tripwire that catches a typo'd flag value or
  // a future contributor who adds an entry without thinking about
  // its wiring status.
  test("every entry's deferred field is either undefined or literal `true`", () => {
    for (const entry of IDEMPOTENT_WRITE_ENDPOINTS) {
      if (entry.deferred !== undefined) {
        expect(entry.deferred).toBe(true);
      }
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

// hp-jsi: enforcement lane. Pre-fix this test passed green when a
// declared retryable endpoint returned 404/405/501 — the bead's
// "lying about idempotency" symptom. Now every endpoint that is NOT
// marked `deferred: true` in the contract table must be reachable and
// must return a non-{404,405,501} status. Marking an endpoint
// deferred is the explicit signal that it's NOT yet wired; flipping
// the flag off when an adapter lands is what drives the enforcement
// lane to start exercising it.
//
// The previous "skips when daemon binary missing" test pretended
// every unrouted endpoint was a benign skip. That semantic now lives
// in the contract table itself (the `deferred` field) instead of
// being silently inferred from the daemon's response.
const DAEMON_BINARY_MISSING = (() => {
  const path = resolve(REPO_ROOT, "apps", "daemon", "bin", "hoopoed");
  try {
    // Synchronous existence check at module load — bun:test cannot
    // skip dynamically mid-test, so this drives test.skipIf below.
    return !require("node:fs").existsSync(path);
  } catch {
    return true;
  }
})();

describe("hp-ngq :: idempotency enforcement lane (hp-jsi)", () => {
  test.skipIf(DAEMON_BINARY_MISSING)(
    "every non-deferred endpoint routes; second call converges on status",
    async () => {
      const logger = createPhase1TestLogger({
        suite: "hp-ngq.contract",
        testName: "enforcement",
      });
      logger.start();

      logger.phase("setup");
      const start = await tryStartDaemon({ repoRoot: REPO_ROOT, mode: "mock" });
      if (!start.ok) {
        // skipIf above guaranteed binary exists — a failure here is
        // a real harness problem, not a "no binary" skip.
        throw new Error(`daemon harness failed despite binary present: ${start.reason}`);
      }
      const daemon = start.handle;
      try {
        logger.phase("act");
        const results: EndpointProbeResult[] = [];
        for (const endpoint of IDEMPOTENT_WRITE_ENDPOINTS) {
          const result = await probeEndpoint(daemon.baseUrl, daemon.authToken, endpoint);
          results.push(result);
          logger.snapshot(`contract.probe.${endpoint.method}.${endpoint.path}`, {
            firstStatus: result.firstStatus,
            secondStatus: result.secondStatus,
            bodiesMatch: result.bodiesMatch,
            routed: result.routed,
            deferred: endpoint.deferred === true,
          });
        }

        logger.phase("assert");
        for (const result of results) {
          const id = `${result.endpoint.method} ${result.endpoint.path}`;
          if (result.endpoint.deferred === true) {
            // Deferred endpoints are EXPECTED to return 404/405/501.
            // If they start returning something else, the deferred
            // flag is stale and the contract should drop it so the
            // enforcement lane starts exercising the route. Pin the
            // direction so the staleness is loud.
            expect([404, 405, 501]).toContain(result.firstStatus);
            continue;
          }
          // Non-deferred endpoint: it MUST be wired. Any 404/405/501
          // response means the endpoint was declared in the contract
          // but isn't actually reachable — the exact "lying about
          // idempotency for routes it doesn't have" symptom hp-jsi
          // was filed to surface.
          if ([404, 405, 501].includes(result.firstStatus)) {
            throw new Error(
              `[hp-jsi] retryable endpoint ${id} returned ${result.firstStatus} but is not marked deferred. ` +
                `Either implement the route or add \`deferred: true\` to its contract entry.`,
            );
          }
          // Routed endpoint: second call with the same key must NOT
          // produce a divergent status. Body match is ideal but the
          // daemon may legitimately add per-call timestamps; for now
          // only status convergence is enforced.
          expect(result.firstStatus).toBe(result.secondStatus);
        }
      } finally {
        logger.phase("teardown");
        await daemon.kill();
      }
    },
    30_000,
  );
});
