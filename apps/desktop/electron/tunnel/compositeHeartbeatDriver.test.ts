// hp-fkov — CompositeHeartbeatDriver tests.

import { describe, expect, test } from "bun:test";

import {
  CompositeHeartbeatDriver,
  type CompositeHeartbeatAuditEvent,
  type HealthProbe,
  type VersionProbe,
} from "./compositeHeartbeatDriver.ts";
import type { HeartbeatStatus } from "./orchestrator.ts";
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

interface ProbeRecorder {
  readonly health: HealthProbe;
  readonly version: VersionProbe;
  readonly healthCalls: number;
  readonly versionCalls: number;
}

function makeProbes(opts: {
  readonly healthResult?: HeartbeatStatus | Error;
  readonly versionResult?: { compatibility: "compatible" | "version_mismatch"; reportedSchemaVersion: number } | Error;
}): ProbeRecorder {
  let healthCalls = 0;
  let versionCalls = 0;
  const health: HealthProbe = {
    check: async () => {
      healthCalls += 1;
      const r = opts.healthResult ?? "ok";
      if (r instanceof Error) throw r;
      return r;
    },
  };
  const version: VersionProbe = {
    check: async () => {
      versionCalls += 1;
      const r = opts.versionResult ?? { compatibility: "compatible" as const, reportedSchemaVersion: 1 };
      if (r instanceof Error) throw r;
      return r;
    },
  };
  return {
    health,
    version,
    get healthCalls() {
      return healthCalls;
    },
    get versionCalls() {
      return versionCalls;
    },
  };
}

const fixedNow = () => new Date("2026-05-04T01:00:00.000Z");

describe("CompositeHeartbeatDriver.check", () => {
  test("happy path: health ok + version compatible → ok", async () => {
    const probes = makeProbes({});
    const audit: CompositeHeartbeatAuditEvent[] = [];
    const driver = new CompositeHeartbeatDriver({
      health: probes.health,
      version: probes.version,
      audit: (e) => audit.push(e),
      now: fixedNow,
    });
    const result = await driver.check({ profile: PROFILE, localPort: 17655 });
    expect(result).toBe("ok");
    expect(probes.healthCalls).toBe(1);
    expect(probes.versionCalls).toBe(1);
    expect(audit.length).toBe(1);
    expect(audit[0]?.kind).toBe("heartbeat.composite.ok");
    expect(audit[0]?.at).toBe("2026-05-04T01:00:00.000Z");
  });

  test("health ok + version mismatch → version_mismatch with reportedSchemaVersion in audit", async () => {
    const probes = makeProbes({
      versionResult: { compatibility: "version_mismatch", reportedSchemaVersion: 99 },
    });
    const audit: CompositeHeartbeatAuditEvent[] = [];
    const driver = new CompositeHeartbeatDriver({
      health: probes.health,
      version: probes.version,
      audit: (e) => audit.push(e),
    });
    const result = await driver.check({ profile: PROFILE, localPort: 17655 });
    expect(result).toBe("version_mismatch");
    expect(audit.length).toBe(1);
    expect(audit[0]?.kind).toBe("heartbeat.composite.version_mismatch_detected");
    expect(audit[0]?.reportedSchemaVersion).toBe(99);
    expect(audit[0]?.message).toContain("99");
  });

  test("health throws: composite throws + version probe NOT called", async () => {
    const probes = makeProbes({ healthResult: new Error("ECONNREFUSED") });
    const audit: CompositeHeartbeatAuditEvent[] = [];
    const driver = new CompositeHeartbeatDriver({
      health: probes.health,
      version: probes.version,
      audit: (e) => audit.push(e),
    });
    try {
      await driver.check({ profile: PROFILE, localPort: 17655 });
      throw new Error("expected throw");
    } catch (err) {
      expect((err as Error).message).toBe("ECONNREFUSED");
    }
    expect(probes.healthCalls).toBe(1);
    expect(probes.versionCalls).toBe(0);
    expect(audit.length).toBe(1);
    expect(audit[0]?.kind).toBe("heartbeat.composite.health_failed");
    expect(audit[0]?.message).toBe("ECONNREFUSED");
  });

  test("health ok + version throws: composite rethrows the version error", async () => {
    const probes = makeProbes({ versionResult: new Error("version probe blip") });
    const audit: CompositeHeartbeatAuditEvent[] = [];
    const driver = new CompositeHeartbeatDriver({
      health: probes.health,
      version: probes.version,
      audit: (e) => audit.push(e),
    });
    try {
      await driver.check({ profile: PROFILE, localPort: 17655 });
      throw new Error("expected throw");
    } catch (err) {
      expect((err as Error).message).toBe("version probe blip");
    }
    expect(probes.healthCalls).toBe(1);
    expect(probes.versionCalls).toBe(1);
    expect(audit.length).toBe(1);
    expect(audit[0]?.kind).toBe("heartbeat.composite.version_failed");
    expect(audit[0]?.message).toBe("version probe blip");
  });

  test("health returns version_mismatch directly: short-circuits without calling version probe", async () => {
    // Defensive forward-compat: a future health probe could itself
    // detect a schema mismatch (e.g. via a header). The composite
    // surfaces it without re-asking the version probe.
    const probes = makeProbes({ healthResult: "version_mismatch" });
    const audit: CompositeHeartbeatAuditEvent[] = [];
    const driver = new CompositeHeartbeatDriver({
      health: probes.health,
      version: probes.version,
      audit: (e) => audit.push(e),
    });
    const result = await driver.check({ profile: PROFILE, localPort: 17655 });
    expect(result).toBe("version_mismatch");
    expect(probes.versionCalls).toBe(0);
    expect(audit[0]?.kind).toBe("heartbeat.composite.version_mismatch_detected");
    expect(audit[0]?.message).toContain("health probe");
  });

  test("audit-sink that throws does NOT mask the composite verdict", async () => {
    const probes = makeProbes({});
    let auditCalls = 0;
    const driver = new CompositeHeartbeatDriver({
      health: probes.health,
      version: probes.version,
      audit: () => {
        auditCalls += 1;
        throw new Error("audit boom");
      },
    });
    // The composite still returns "ok" even though the audit sink
    // threw — sink failure must not break the probe contract.
    const result = await driver.check({ profile: PROFILE, localPort: 17655 });
    expect(result).toBe("ok");
    expect(auditCalls).toBe(1);
  });

  test("audit-sink throw on health-failed: composite still rethrows the health error", async () => {
    const probes = makeProbes({ healthResult: new Error("ECONNREFUSED") });
    const driver = new CompositeHeartbeatDriver({
      health: probes.health,
      version: probes.version,
      audit: () => {
        throw new Error("audit boom");
      },
    });
    try {
      await driver.check({ profile: PROFILE, localPort: 17655 });
      throw new Error("expected throw");
    } catch (err) {
      // The original health error wins, NOT the audit-sink error.
      expect((err as Error).message).toBe("ECONNREFUSED");
    }
  });

  test("non-Error health failure: stringifies for audit", async () => {
    // Health probe rejects with a string (poor practice, but defensive
    // code paths handle it).
    const health: HealthProbe = {
      check: async () => {
        throw "raw string error";
      },
    };
    const probes = makeProbes({});
    const audit: CompositeHeartbeatAuditEvent[] = [];
    const driver = new CompositeHeartbeatDriver({
      health,
      version: probes.version,
      audit: (e) => audit.push(e),
    });
    try {
      await driver.check({ profile: PROFILE, localPort: 17655 });
      throw new Error("expected throw");
    } catch (err) {
      expect(String(err)).toBe("raw string error");
    }
    expect(audit[0]?.message).toBe("raw string error");
  });

  test("composite passes input through to both probes", async () => {
    let healthInput: { profile: VpsProfile; localPort: number } | null = null;
    let versionInput: { profile: VpsProfile; localPort: number } | null = null;
    const health: HealthProbe = {
      check: async (input) => {
        healthInput = { profile: input.profile, localPort: input.localPort };
        return "ok";
      },
    };
    const version: VersionProbe = {
      check: async (input) => {
        versionInput = { profile: input.profile, localPort: input.localPort };
        return { compatibility: "compatible", reportedSchemaVersion: 1 };
      },
    };
    const driver = new CompositeHeartbeatDriver({
      health,
      version,
      audit: () => undefined,
    });
    await driver.check({ profile: PROFILE, localPort: 24680 });
    expect(healthInput).toEqual({ profile: PROFILE, localPort: 24680 });
    expect(versionInput).toEqual({ profile: PROFILE, localPort: 24680 });
  });

  test("audit timestamps use the injected clock", async () => {
    const probes = makeProbes({});
    const audit: CompositeHeartbeatAuditEvent[] = [];
    const driver = new CompositeHeartbeatDriver({
      health: probes.health,
      version: probes.version,
      audit: (e) => audit.push(e),
      now: () => new Date("2030-12-31T23:59:59.999Z"),
    });
    await driver.check({ profile: PROFILE, localPort: 17655 });
    expect(audit[0]?.at).toBe("2030-12-31T23:59:59.999Z");
  });
});
