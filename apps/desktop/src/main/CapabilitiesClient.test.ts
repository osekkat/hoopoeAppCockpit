import { describe, expect, test } from "bun:test";
import { CapabilitiesClient, type CapabilitiesFetcher } from "./CapabilitiesClient.ts";
import type { CapabilityRegistry, CompatibilityReport } from "../capabilities/index.ts";

const T0 = "2026-05-04T00:00:00Z";

const REGISTRY: CapabilityRegistry = {
  schemaVersion: 1,
  snapshotAt: T0,
  daemonApiVersion: "0.1.0",
  fixturesVersion: "phase0-test",
  tools: {
    git: {
      tool: "git",
      version: "2.40.0",
      source: "CLI",
      lastCheckedAt: T0,
      fixturesVersion: "phase0-test",
      capabilities: {
        "git.status.read": { status: "ok" },
        "git.push": { status: "blocked-by-policy", notes: "snapshot scripts never push" },
      },
    },
    br: {
      tool: "br",
      version: "0.5.0",
      source: "CLI",
      lastCheckedAt: T0,
      fixturesVersion: "phase0-test",
      capabilities: {
        "br.issues.read": { status: "ok" },
      },
    },
    bv: {
      tool: "bv",
      version: "1.0.0",
      source: "CLI",
      lastCheckedAt: T0,
      fixturesVersion: "phase0-test",
      capabilities: {
        "bv.robot.triage": { status: "degraded", fallback: "older binary" },
      },
    },
    ntm: {
      tool: "ntm",
      version: "1.0.0",
      source: "ntm serve",
      lastCheckedAt: T0,
      fixturesVersion: "phase0-test",
      capabilities: {
        "ntm.sessions.list": { status: "ok" },
        "ntm.robot.snapshot": { status: "ok" },
        "ntm.panes.stream": { status: "missing" },
        "ntm.swarm.halt": { status: "missing" },
      },
    },
    agent_mail: {
      tool: "agent_mail",
      version: "1.0.0",
      source: "MCP",
      lastCheckedAt: T0,
      fixturesVersion: "phase0-test",
      capabilities: {
        "agent_mail.messages.send": { status: "ok" },
      },
    },
    dcg: {
      tool: "dcg",
      version: "1.0.0",
      source: "CLI",
      lastCheckedAt: T0,
      fixturesVersion: "phase0-test",
      capabilities: {
        "dcg.verdicts.subscribe": { status: "untested" },
      },
    },
  },
};

interface SpyFetcher {
  readonly fetcher: CapabilitiesFetcher;
  readonly callCount: () => number;
  readonly lastPath: () => string | null;
  readonly setNextBody: (body: unknown, init?: ResponseInit) => void;
}

/** Build a fetcher that produces a FRESH `Response` on every call.
 *  Response bodies are single-use; reusing one across calls trips
 *  `ERR_BODY_ALREADY_USED` (caught by the ensureFresh refresh test). */
function spyFetcher(initialBody: unknown, initialInit?: ResponseInit): SpyFetcher {
  let calls = 0;
  let lastPath: string | null = null;
  let body: unknown = initialBody;
  let init: ResponseInit | undefined = initialInit;
  return {
    fetcher: async (path: string) => {
      calls += 1;
      lastPath = path;
      return jsonResponse(body, init);
    },
    callCount: () => calls,
    lastPath: () => lastPath,
    setNextBody: (b, i) => {
      body = b;
      init = i;
    },
  };
}

function jsonResponse(body: unknown, init?: ResponseInit): Response {
  return new Response(JSON.stringify(body), {
    status: init?.status ?? 200,
    headers: { "Content-Type": "application/json", ...init?.headers },
  });
}

/** Single-call fetcher for tests that don't need to refetch. */
function singleResponseFetcher(res: Response): CapabilitiesFetcher {
  return async () => res;
}

describe("CapabilitiesClient", () => {
  test("snapshot starts as the empty registry until refresh", () => {
    const spy = spyFetcher(REGISTRY);
    const client = new CapabilitiesClient({ fetcher: spy.fetcher });
    expect(client.snapshot().daemonApiVersion).toBe("");
    expect(spy.callCount()).toBe(0);
  });

  test("refresh fetches /v1/capabilities and caches the result", async () => {
    const spy = spyFetcher(REGISTRY);
    const client = new CapabilitiesClient({ fetcher: spy.fetcher });
    const got = await client.refresh();
    expect(got.daemonApiVersion).toBe("0.1.0");
    expect(spy.callCount()).toBe(1);
    expect(spy.lastPath()).toBe("/v1/capabilities");
    expect(client.snapshot()).toBe(got);
  });

  test("ensureFresh skips refresh when cache is younger than maxStaleMs", async () => {
    let nowMs = 1_000;
    const spy = spyFetcher(REGISTRY);
    const client = new CapabilitiesClient({
      fetcher: spy.fetcher,
      now: () => new Date(nowMs),
      maxStaleMs: 5_000,
    });
    await client.refresh();
    expect(spy.callCount()).toBe(1);
    nowMs += 1_000;
    await client.ensureFresh();
    expect(spy.callCount()).toBe(1); // cached
    nowMs += 5_000;
    await client.ensureFresh();
    expect(spy.callCount()).toBe(2); // stale, refetched
  });

  test("concurrent refresh calls coalesce into a single fetch", async () => {
    let resolveResp: (r: Response) => void = () => {};
    const pending = new Promise<Response>((resolve) => {
      resolveResp = resolve;
    });
    const spy: SpyFetcher = {
      fetcher: async () => pending,
      callCount: () => 1,
      lastPath: () => "/v1/capabilities",
      setNextResponse: () => {},
    };
    const client = new CapabilitiesClient({ fetcher: spy.fetcher });
    const a = client.refresh();
    const b = client.refresh();
    resolveResp(jsonResponse(REGISTRY));
    const [r1, r2] = await Promise.all([a, b]);
    expect(r1).toBe(r2);
  });

  test("non-200 response throws", async () => {
    const fetcher = singleResponseFetcher(new Response("nope", { status: 503 }));
    const client = new CapabilitiesClient({ fetcher });
    await expect(client.refresh()).rejects.toThrow(/503/);
  });

  test("schemaVersion mismatch throws (forward-compat guard)", async () => {
    const spy = spyFetcher({ ...REGISTRY, schemaVersion: 2 });
    const client = new CapabilitiesClient({ fetcher: spy.fetcher });
    await expect(client.refresh()).rejects.toThrow(/schemaVersion/);
  });

  test("decide() resolves catalog features against the cache", async () => {
    const spy = spyFetcher(REGISTRY);
    const client = new CapabilitiesClient({ fetcher: spy.fetcher });
    await client.refresh();

    // git.push is blocked-by-policy in the fixture → push-branch render =
    // blocked-by-policy.
    const decision = client.decide("swarm.bead.push-branch");
    expect(decision.render).toBe("blocked-by-policy");
    expect(decision.blockedByPolicy).toEqual(["git.push"]);
  });

  test("decide() picks degraded for bv.robot.triage degradation", async () => {
    const spy = spyFetcher(REGISTRY);
    const client = new CapabilitiesClient({ fetcher: spy.fetcher });
    await client.refresh();
    // bead.kanban.refresh requires br.issues.read (ok) + optional
    // bv.robot.triage (degraded). Optional degradation downgrades render.
    const decision = client.decide("bead.kanban.refresh");
    expect(decision.render).toBe("degraded");
    expect(decision.degradedReasons).toEqual(["bv.robot.triage"]);
  });

  test("decide() picks unavailable when required capability is missing", async () => {
    const spy = spyFetcher(REGISTRY);
    const client = new CapabilitiesClient({ fetcher: spy.fetcher });
    await client.refresh();
    // approvals.dcg.subscribe requires dcg.verdicts.subscribe (untested →
    // unavailable bucket).
    const decision = client.decide("approvals.dcg.subscribe");
    expect(decision.render).toBe("unavailable");
    expect(decision.missingRequired).toEqual(["dcg.verdicts.subscribe"]);
  });

  test("decideAll() returns one decision per catalog entry", async () => {
    const spy = spyFetcher(REGISTRY);
    const client = new CapabilitiesClient({ fetcher: spy.fetcher });
    await client.refresh();
    const decisions = client.decideAll();
    expect(decisions.length).toBeGreaterThan(0);
    // Every decision references a known FEATURE_CATALOG id.
    for (const d of decisions) {
      expect(typeof d.featureId).toBe("string");
      expect(d.render).toMatch(/^(available|degraded|unavailable|blocked-by-policy)$/);
    }
  });

  test("fetchCompatibility hits /v1/compatibility and decodes", async () => {
    const compat: CompatibilityReport = {
      schemaVersion: 1,
      daemonApiVersion: "0.1.0",
      minDesktopVersion: "0.0.0",
      eventSchemaVersions: { _system: 1, project: 1, swarm: 1 },
      migrationState: { schemaVersion: 1, appliedAt: T0, pending: [], phase: "idle" },
      capabilities: REGISTRY,
    };
    const spy = spyFetcher(compat);
    const client = new CapabilitiesClient({ fetcher: spy.fetcher });
    const got = await client.fetchCompatibility();
    expect(got.minDesktopVersion).toBe("0.0.0");
    expect(got.migrationState.phase).toBe("idle");
    expect(spy.lastPath()).toBe("/v1/compatibility");
  });

  test("onSnapshot hook fires after a successful refresh", async () => {
    const seen: CapabilityRegistry[] = [];
    const spy = spyFetcher(REGISTRY);
    const client = new CapabilitiesClient({
      fetcher: spy.fetcher,
      onSnapshot: (r) => seen.push(r),
    });
    await client.refresh();
    expect(seen.length).toBe(1);
    expect(seen[0].daemonApiVersion).toBe("0.1.0");
  });
});
