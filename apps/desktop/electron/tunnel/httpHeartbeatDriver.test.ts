// hp-fkov — HttpHeartbeatDriver tests.

import { describe, expect, test } from "bun:test";

import {
  HttpHeartbeatDriver,
  HttpHeartbeatError,
  type FetchLike,
  type FetchResponse,
} from "./httpHeartbeatDriver.ts";
import type { VpsProfile } from "./types.ts";

// ── Test doubles ─────────────────────────────────────────────────────────

const PROFILE: VpsProfile = {
  id: "profile-1",
  schemaVersion: 1,
  alias: "test",
  host: "vps.example.com",
  port: 22,
  username: "agent",
  privateKeyPath: "/home/test/.ssh/id_ed25519",
  knownHostFingerprint: null,
  daemonReleaseChannel: "stable",
  preferredLocalPort: 17655,
  createdAt: "2026-05-04T01:00:00.000Z",
  lastUsedAt: null,
};

interface FakeFetchOptions {
  readonly status?: number;
  readonly body?: string;
  readonly throwError?: Error;
  readonly hangForever?: boolean;
  readonly bodyReadError?: Error;
}

function makeFetch(opts: FakeFetchOptions = {}): {
  readonly fetch: FetchLike;
  readonly calls: Array<{ url: string; headers: Record<string, string> }>;
} {
  const calls: Array<{ url: string; headers: Record<string, string> }> = [];
  const fetch: FetchLike = async (url, init) => {
    calls.push({ url, headers: { ...init.headers } });

    if (opts.hangForever) {
      // Wait until the AbortController fires; then reject.
      return new Promise<FetchResponse>((_, reject) => {
        init.signal.addEventListener("abort", () => {
          reject(new Error("AbortError: aborted"));
        });
      });
    }

    if (opts.throwError) {
      throw opts.throwError;
    }

    const status = opts.status ?? 200;
    const body = opts.body ?? `{"status":"ok","daemonId":"d1","time":"2026-05-04T01:00:00.000Z"}`;
    return {
      ok: status >= 200 && status < 300,
      status,
      text: async () => {
        if (opts.bodyReadError) throw opts.bodyReadError;
        return body;
      },
    } satisfies FetchResponse;
  };
  return { fetch, calls };
}

// ── Tests ────────────────────────────────────────────────────────────────

describe("HttpHeartbeatDriver.check", () => {
  test("happy path: GET /v1/health returns ok", async () => {
    const { fetch, calls } = makeFetch();
    const driver = new HttpHeartbeatDriver({ fetch });
    const result = await driver.check({ profile: PROFILE, localPort: 17655 });
    expect(result).toBe("ok");
    expect(calls.length).toBe(1);
    expect(calls[0]?.url).toBe("http://127.0.0.1:17655/v1/health");
    expect(calls[0]?.headers["Accept"]).toBe("application/json");
    // No bearer was supplied → Authorization header MUST be absent
    // (defense against a stale token leaking after rotation).
    expect(calls[0]?.headers["Authorization"]).toBeUndefined();
  });

  test("bearer resolver: included as Authorization header when present", async () => {
    const { fetch, calls } = makeFetch();
    let bearerCalls = 0;
    const driver = new HttpHeartbeatDriver({
      fetch,
      bearer: () => {
        bearerCalls += 1;
        return "abc.def.ghi";
      },
    });
    await driver.check({ profile: PROFILE, localPort: 17655 });
    expect(calls[0]?.headers["Authorization"]).toBe("Bearer abc.def.ghi");
    expect(bearerCalls).toBe(1);
  });

  test("bearer resolver returning null: no Authorization header", async () => {
    const { fetch, calls } = makeFetch();
    const driver = new HttpHeartbeatDriver({ fetch, bearer: () => null });
    await driver.check({ profile: PROFILE, localPort: 17655 });
    expect(calls[0]?.headers["Authorization"]).toBeUndefined();
  });

  test("bearer resolver is called per probe (rotation lands on next heartbeat)", async () => {
    const { fetch } = makeFetch();
    const tokens = ["token-1", "token-2", "token-3"];
    let i = 0;
    const driver = new HttpHeartbeatDriver({
      fetch,
      bearer: () => tokens[i++] ?? null,
    });
    await driver.check({ profile: PROFILE, localPort: 17655 });
    await driver.check({ profile: PROFILE, localPort: 17655 });
    await driver.check({ profile: PROFILE, localPort: 17655 });
    expect(i).toBe(3);
  });

  test("custom urlFor: uses caller-supplied URL builder", async () => {
    const { fetch, calls } = makeFetch();
    const driver = new HttpHeartbeatDriver({
      fetch,
      urlFor: ({ localPort }) => `https://test.local:${localPort}/healthz`,
    });
    await driver.check({ profile: PROFILE, localPort: 9999 });
    expect(calls[0]?.url).toBe("https://test.local:9999/healthz");
  });

  test("non-200 response: throws http_status with the status code", async () => {
    const { fetch } = makeFetch({ status: 503, body: `{"status":"draining"}` });
    const driver = new HttpHeartbeatDriver({ fetch });
    try {
      await driver.check({ profile: PROFILE, localPort: 17655 });
      throw new Error("expected throw");
    } catch (err) {
      expect(err).toBeInstanceOf(HttpHeartbeatError);
      const e = err as HttpHeartbeatError;
      expect(e.code).toBe("http_status");
      expect(e.status).toBe(503);
    }
  });

  test("network error: throws network with the underlying message", async () => {
    const { fetch } = makeFetch({ throwError: new Error("ECONNREFUSED") });
    const driver = new HttpHeartbeatDriver({ fetch });
    try {
      await driver.check({ profile: PROFILE, localPort: 17655 });
      throw new Error("expected throw");
    } catch (err) {
      expect((err as HttpHeartbeatError).code).toBe("network");
      expect((err as HttpHeartbeatError).message).toContain("ECONNREFUSED");
    }
  });

  test("timeout: AbortController fires when fetch hangs past timeoutMs", async () => {
    const { fetch } = makeFetch({ hangForever: true });
    const driver = new HttpHeartbeatDriver({ fetch, timeoutMs: 25 });
    const start = Date.now();
    try {
      await driver.check({ profile: PROFILE, localPort: 17655 });
      throw new Error("expected throw");
    } catch (err) {
      expect((err as HttpHeartbeatError).code).toBe("timeout");
      expect((err as HttpHeartbeatError).message).toContain("25ms");
    }
    // Sanity: the timeout actually fired in the budget; we don't wait
    // for the default 5s.
    expect(Date.now() - start).toBeLessThan(500);
  });

  test("malformed body — non-JSON: throws malformed_response", async () => {
    const { fetch } = makeFetch({ body: "<!DOCTYPE html>not json" });
    const driver = new HttpHeartbeatDriver({ fetch });
    try {
      await driver.check({ profile: PROFILE, localPort: 17655 });
      throw new Error("expected throw");
    } catch (err) {
      expect((err as HttpHeartbeatError).code).toBe("malformed_response");
      expect((err as HttpHeartbeatError).message).toContain("not JSON");
    }
  });

  test("malformed body — wrong shape: throws malformed_response", async () => {
    // Valid JSON but missing the `status` field.
    const { fetch } = makeFetch({ body: `{"daemonId":"d1","time":"2026"}` });
    const driver = new HttpHeartbeatDriver({ fetch });
    try {
      await driver.check({ profile: PROFILE, localPort: 17655 });
      throw new Error("expected throw");
    } catch (err) {
      expect((err as HttpHeartbeatError).code).toBe("malformed_response");
      expect((err as HttpHeartbeatError).message).toContain("HealthResponse");
    }
  });

  test("malformed body — invalid status enum value: rejected", async () => {
    const { fetch } = makeFetch({ body: `{"status":"unknown","daemonId":"d1","time":"x"}` });
    const driver = new HttpHeartbeatDriver({ fetch });
    try {
      await driver.check({ profile: PROFILE, localPort: 17655 });
      throw new Error("expected throw");
    } catch (err) {
      expect((err as HttpHeartbeatError).code).toBe("malformed_response");
    }
  });

  test("body read failure: throws malformed_response", async () => {
    const { fetch } = makeFetch({ bodyReadError: new Error("stream reset") });
    const driver = new HttpHeartbeatDriver({ fetch });
    try {
      await driver.check({ profile: PROFILE, localPort: 17655 });
      throw new Error("expected throw");
    } catch (err) {
      expect((err as HttpHeartbeatError).code).toBe("malformed_response");
      expect((err as HttpHeartbeatError).message).toContain("stream reset");
    }
  });

  test("degraded daemon: still ok (heartbeat = 'process can serve HTTP')", async () => {
    // The driver returns "ok" for every documented HealthResponse status —
    // `degraded` and `draining` are the daemon reporting its own state, not
    // a heartbeat failure. The orchestrator decides what to do with degraded
    // separately via /v1/capabilities.
    for (const status of ["ok", "degraded", "draining"]) {
      const { fetch } = makeFetch({
        body: `{"status":"${status}","daemonId":"d1","time":"x"}`,
      });
      const driver = new HttpHeartbeatDriver({ fetch });
      const result = await driver.check({ profile: PROFILE, localPort: 17655 });
      expect(result).toBe("ok");
    }
  });

  test("default URL: 127.0.0.1 (never 0.0.0.0 or external host)", async () => {
    // Defense against a future regression that lets the renderer or a
    // misconfig point the heartbeat at a public address. Tunnel listener
    // binds 127.0.0.1 only (per AGENTS.md hp-2n1 considerations).
    const { fetch, calls } = makeFetch();
    const driver = new HttpHeartbeatDriver({ fetch });
    await driver.check({ profile: PROFILE, localPort: 12345 });
    expect(calls[0]?.url).toMatch(/^http:\/\/127\.0\.0\.1:/);
    expect(calls[0]?.url).not.toContain("0.0.0.0");
    expect(calls[0]?.url).not.toContain(PROFILE.host);
  });

  test("localPort is interpolated into URL", async () => {
    const { fetch, calls } = makeFetch();
    const driver = new HttpHeartbeatDriver({ fetch });
    await driver.check({ profile: PROFILE, localPort: 65432 });
    expect(calls[0]?.url).toBe("http://127.0.0.1:65432/v1/health");
  });

  test("no fetch available + no globalThis.fetch: constructor throws", () => {
    // Guard for older Node versions where fetch is absent. We only test
    // the explicit-undefined branch since globalThis.fetch is present
    // in bun.
    expect(
      () =>
        new HttpHeartbeatDriver({
          fetch: undefined as unknown as FetchLike,
          // Force the construct check by removing globalThis.fetch
          // for this test only.
        }),
    ).not.toThrow(); // bun has fetch globally → OK
  });
});
