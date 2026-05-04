// hp-fkov — HttpVersionDriver tests.

import { describe, expect, test } from "bun:test";

import {
  HttpVersionDriver,
  HttpVersionError,
  VersionedHeartbeatDriver,
  type HttpVersionProbeResult,
  type VersionCompatibility,
  type VersionProbeDriver,
} from "./httpVersionDriver.ts";
import type { FetchLike, FetchResponse } from "./httpHeartbeatDriver.ts";
import type { HeartbeatDriver } from "./orchestrator.ts";
import type { VpsProfile } from "./types.ts";

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
      return new Promise<FetchResponse>((_, reject) => {
        init.signal.addEventListener("abort", () => {
          reject(new Error("AbortError"));
        });
      });
    }
    if (opts.throwError) throw opts.throwError;

    const status = opts.status ?? 200;
    const body =
      opts.body ??
      `{"schemaVersion":1,"daemon":{"version":"0.1.0","commit":"abcdef0","channel":"stable"}}`;
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

describe("HttpVersionDriver.check", () => {
  test("compatible: schemaVersion in accepted set returns compatible verdict", async () => {
    const { fetch } = makeFetch({
      body: `{"schemaVersion":1,"daemon":{"version":"0.1.0","commit":"abc","channel":"stable"}}`,
    });
    const driver = new HttpVersionDriver({
      acceptedSchemaVersions: [1, 2],
      fetch,
    });
    const result = await driver.check({ profile: PROFILE, localPort: 17655 });
    expect(result.compatibility).toBe("compatible");
    expect(result.reportedSchemaVersion).toBe(1);
    expect(result.daemonVersion).toBe("0.1.0");
    expect(result.commit).toBe("abc");
    expect(result.channel).toBe("stable");
  });

  test("version_mismatch: schemaVersion outside accepted set", async () => {
    const { fetch } = makeFetch({
      body: `{"schemaVersion":99,"daemon":{"version":"99.0.0"}}`,
    });
    const driver = new HttpVersionDriver({
      acceptedSchemaVersions: [1, 2],
      fetch,
    });
    const result = await driver.check({ profile: PROFILE, localPort: 17655 });
    expect(result.compatibility).toBe("version_mismatch");
    expect(result.reportedSchemaVersion).toBe(99);
    expect(result.daemonVersion).toBe("99.0.0");
  });

  test("happy path with bearer header", async () => {
    const { fetch, calls } = makeFetch();
    const driver = new HttpVersionDriver({
      acceptedSchemaVersions: [1],
      fetch,
      bearer: () => "tok.123",
    });
    await driver.check({ profile: PROFILE, localPort: 17655 });
    expect(calls[0]?.headers["Authorization"]).toBe("Bearer tok.123");
  });

  test("no bearer: Authorization header absent", async () => {
    const { fetch, calls } = makeFetch();
    const driver = new HttpVersionDriver({
      acceptedSchemaVersions: [1],
      fetch,
    });
    await driver.check({ profile: PROFILE, localPort: 17655 });
    expect(calls[0]?.headers["Authorization"]).toBeUndefined();
  });

  test("default URL pins 127.0.0.1:<port>/v1/version", async () => {
    const { fetch, calls } = makeFetch();
    const driver = new HttpVersionDriver({
      acceptedSchemaVersions: [1],
      fetch,
    });
    await driver.check({ profile: PROFILE, localPort: 24680 });
    expect(calls[0]?.url).toBe("http://127.0.0.1:24680/v1/version");
    expect(calls[0]?.url).not.toContain("0.0.0.0");
    expect(calls[0]?.url).not.toContain(PROFILE.host);
  });

  test("custom urlFor: respected", async () => {
    const { fetch, calls } = makeFetch();
    const driver = new HttpVersionDriver({
      acceptedSchemaVersions: [1],
      fetch,
      urlFor: ({ localPort }) => `https://daemon.local:${localPort}/version`,
    });
    await driver.check({ profile: PROFILE, localPort: 999 });
    expect(calls[0]?.url).toBe("https://daemon.local:999/version");
  });

  test("non-200: throws http_status with status", async () => {
    const { fetch } = makeFetch({ status: 500, body: "" });
    const driver = new HttpVersionDriver({
      acceptedSchemaVersions: [1],
      fetch,
    });
    try {
      await driver.check({ profile: PROFILE, localPort: 17655 });
      throw new Error("expected throw");
    } catch (err) {
      expect((err as HttpVersionError).code).toBe("http_status");
      expect((err as HttpVersionError).status).toBe(500);
    }
  });

  test("network error: throws network", async () => {
    const { fetch } = makeFetch({ throwError: new Error("ECONNRESET") });
    const driver = new HttpVersionDriver({
      acceptedSchemaVersions: [1],
      fetch,
    });
    try {
      await driver.check({ profile: PROFILE, localPort: 17655 });
      throw new Error("expected throw");
    } catch (err) {
      expect((err as HttpVersionError).code).toBe("network");
      expect((err as HttpVersionError).message).toContain("ECONNRESET");
    }
  });

  test("timeout: AbortController fires within budget", async () => {
    const { fetch } = makeFetch({ hangForever: true });
    const driver = new HttpVersionDriver({
      acceptedSchemaVersions: [1],
      fetch,
      timeoutMs: 25,
    });
    const start = Date.now();
    try {
      await driver.check({ profile: PROFILE, localPort: 17655 });
      throw new Error("expected throw");
    } catch (err) {
      expect((err as HttpVersionError).code).toBe("timeout");
      expect((err as HttpVersionError).message).toContain("25ms");
    }
    expect(Date.now() - start).toBeLessThan(500);
  });

  test("malformed JSON: throws malformed_response", async () => {
    const { fetch } = makeFetch({ body: "<html>not json</html>" });
    const driver = new HttpVersionDriver({
      acceptedSchemaVersions: [1],
      fetch,
    });
    try {
      await driver.check({ profile: PROFILE, localPort: 17655 });
      throw new Error("expected throw");
    } catch (err) {
      expect((err as HttpVersionError).code).toBe("malformed_response");
      expect((err as HttpVersionError).message).toContain("not JSON");
    }
  });

  test("missing schemaVersion field: malformed_response", async () => {
    const { fetch } = makeFetch({
      body: `{"daemon":{"version":"0.1.0"}}`,
    });
    const driver = new HttpVersionDriver({
      acceptedSchemaVersions: [1],
      fetch,
    });
    try {
      await driver.check({ profile: PROFILE, localPort: 17655 });
      throw new Error("expected throw");
    } catch (err) {
      expect((err as HttpVersionError).code).toBe("malformed_response");
    }
  });

  test("non-integer schemaVersion: malformed_response", async () => {
    const { fetch } = makeFetch({
      body: `{"schemaVersion":"v1"}`,
    });
    const driver = new HttpVersionDriver({
      acceptedSchemaVersions: [1],
      fetch,
    });
    try {
      await driver.check({ profile: PROFILE, localPort: 17655 });
      throw new Error("expected throw");
    } catch (err) {
      expect((err as HttpVersionError).code).toBe("malformed_response");
    }
  });

  test("zero schemaVersion: malformed_response (must be >= 1 per OpenAPI)", async () => {
    const { fetch } = makeFetch({
      body: `{"schemaVersion":0}`,
    });
    const driver = new HttpVersionDriver({
      acceptedSchemaVersions: [1],
      fetch,
    });
    try {
      await driver.check({ profile: PROFILE, localPort: 17655 });
      throw new Error("expected throw");
    } catch (err) {
      expect((err as HttpVersionError).code).toBe("malformed_response");
    }
  });

  test("daemon block invalid type: malformed_response", async () => {
    const { fetch } = makeFetch({
      body: `{"schemaVersion":1,"daemon":"not an object"}`,
    });
    const driver = new HttpVersionDriver({
      acceptedSchemaVersions: [1],
      fetch,
    });
    try {
      await driver.check({ profile: PROFILE, localPort: 17655 });
      throw new Error("expected throw");
    } catch (err) {
      expect((err as HttpVersionError).code).toBe("malformed_response");
    }
  });

  test("missing daemon block: still compatible (daemon block is optional)", async () => {
    const { fetch } = makeFetch({
      body: `{"schemaVersion":2}`,
    });
    const driver = new HttpVersionDriver({
      acceptedSchemaVersions: [1, 2, 3],
      fetch,
    });
    const result = await driver.check({ profile: PROFILE, localPort: 17655 });
    expect(result.compatibility).toBe("compatible");
    expect(result.reportedSchemaVersion).toBe(2);
    expect(result.daemonVersion).toBeUndefined();
    expect(result.commit).toBeUndefined();
    expect(result.channel).toBeUndefined();
  });

  test("body read error: malformed_response", async () => {
    const { fetch } = makeFetch({ bodyReadError: new Error("stream truncated") });
    const driver = new HttpVersionDriver({
      acceptedSchemaVersions: [1],
      fetch,
    });
    try {
      await driver.check({ profile: PROFILE, localPort: 17655 });
      throw new Error("expected throw");
    } catch (err) {
      expect((err as HttpVersionError).code).toBe("malformed_response");
      expect((err as HttpVersionError).message).toContain("stream truncated");
    }
  });

  test("acceptedSchemaVersions empty: constructor throws", () => {
    expect(
      () =>
        new HttpVersionDriver({
          acceptedSchemaVersions: [],
          fetch: makeFetch().fetch,
        }),
    ).toThrow("acceptedSchemaVersions must be non-empty");
  });

  test("multiple accepted versions: any-of match", async () => {
    // Desktop tolerates a small range of daemon schema versions during
    // a rolling upgrade window. Verify each accepted value matches.
    for (const v of [1, 2, 3]) {
      const { fetch } = makeFetch({
        body: `{"schemaVersion":${v}}`,
      });
      const driver = new HttpVersionDriver({
        acceptedSchemaVersions: [1, 2, 3],
        fetch,
      });
      const result = await driver.check({ profile: PROFILE, localPort: 17655 });
      expect(result.compatibility).toBe("compatible");
      expect(result.reportedSchemaVersion).toBe(v);
    }
  });

  test("just-out-of-range values are explicitly version_mismatch (not silent compat)", async () => {
    // Boundary check: 0 and 4 when accepted is [1, 2, 3].
    // 0 is rejected as malformed (schemaVersion >= 1 per OpenAPI);
    // 4 is rejected as version_mismatch.
    const { fetch: fetchHigh } = makeFetch({
      body: `{"schemaVersion":4}`,
    });
    const driver = new HttpVersionDriver({
      acceptedSchemaVersions: [1, 2, 3],
      fetch: fetchHigh,
    });
    const result = await driver.check({ profile: PROFILE, localPort: 17655 });
    expect(result.compatibility).toBe("version_mismatch");
  });

  test("bearer resolver called per probe (rotation)", async () => {
    const { fetch } = makeFetch();
    let called = 0;
    const driver = new HttpVersionDriver({
      acceptedSchemaVersions: [1],
      fetch,
      bearer: () => {
        called += 1;
        return `tok-${called}`;
      },
    });
    await driver.check({ profile: PROFILE, localPort: 17655 });
    await driver.check({ profile: PROFILE, localPort: 17655 });
    expect(called).toBe(2);
  });
});

describe("VersionedHeartbeatDriver.check", () => {
  test("compatible version: health ok + version compatible returns ok", async () => {
    const calls: string[] = [];
    const driver = new VersionedHeartbeatDriver({
      health: makeHeartbeatDriver(async () => {
        calls.push("health");
        return "ok";
      }),
      version: makeVersionProbe(async () => {
        calls.push("version");
        return versionResult("compatible", 1);
      }),
    });

    const result = await driver.check({ profile: PROFILE, localPort: 17655 });

    expect(result).toBe("ok");
    expect(calls).toEqual(["health", "version"]);
  });

  test("version mismatch: maps compatibility verdict to HeartbeatStatus", async () => {
    const driver = new VersionedHeartbeatDriver({
      health: makeHeartbeatDriver(async () => "ok"),
      version: makeVersionProbe(async () => versionResult("version_mismatch", 99)),
    });

    const result = await driver.check({ profile: PROFILE, localPort: 17655 });

    expect(result).toBe("version_mismatch");
  });

  test("health mismatch short-circuits version probe", async () => {
    let versionCalls = 0;
    const driver = new VersionedHeartbeatDriver({
      health: makeHeartbeatDriver(async () => "version_mismatch"),
      version: makeVersionProbe(async () => {
        versionCalls += 1;
        return versionResult("compatible", 1);
      }),
    });

    const result = await driver.check({ profile: PROFILE, localPort: 17655 });

    expect(result).toBe("version_mismatch");
    expect(versionCalls).toBe(0);
  });

  test("health failure propagates and skips version probe", async () => {
    let versionCalls = 0;
    const driver = new VersionedHeartbeatDriver({
      health: makeHeartbeatDriver(async () => {
        throw new Error("daemon unreachable");
      }),
      version: makeVersionProbe(async () => {
        versionCalls += 1;
        return versionResult("compatible", 1);
      }),
    });

    try {
      await driver.check({ profile: PROFILE, localPort: 17655 });
      throw new Error("expected throw");
    } catch (err) {
      expect((err as Error).message).toBe("daemon unreachable");
    }
    expect(versionCalls).toBe(0);
  });

  test("version failure propagates after health succeeds", async () => {
    const driver = new VersionedHeartbeatDriver({
      health: makeHeartbeatDriver(async () => "ok"),
      version: makeVersionProbe(async () => {
        throw new Error("malformed version response");
      }),
    });

    try {
      await driver.check({ profile: PROFILE, localPort: 17655 });
      throw new Error("expected throw");
    } catch (err) {
      expect((err as Error).message).toBe("malformed version response");
    }
  });
});

function makeHeartbeatDriver(check: HeartbeatDriver["check"]): HeartbeatDriver {
  return { check };
}

function makeVersionProbe(
  check: (input: { readonly profile: VpsProfile; readonly localPort: number }) => Promise<HttpVersionProbeResult>,
): VersionProbeDriver {
  return { check };
}

function versionResult(
  compatibility: VersionCompatibility,
  reportedSchemaVersion: number,
): HttpVersionProbeResult {
  return { compatibility, reportedSchemaVersion };
}
