import { afterEach, beforeEach, describe, expect, test } from "bun:test";
import { chmod, mkdir, mkdtemp, rm, writeFile } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";
import {
  ConnectionManager,
  ConnectionManagerError,
  KnownHostStore,
  SshProfileManager,
  fingerprintHostKey,
  retryDelayMs,
  type KnownHostAuditEvent,
  type SshProfile,
  type TunnelDriver,
  type TunnelHandle,
} from "./ConnectionManager.ts";

let workDir: string;

beforeEach(async () => {
  workDir = await mkdtemp(join(tmpdir(), "hoopoe-connection-"));
});

afterEach(async () => {
  await rm(workDir, { recursive: true, force: true });
});

function fixtureProfile(privateKeyPath: string): SshProfile {
  return {
    id: "vps-main",
    name: "Main VPS",
    host: "vps.example.com",
    port: 22,
    username: "ubuntu",
    privateKeyPath,
    daemonHost: "127.0.0.1",
    daemonPort: 3779,
    localPortPreference: null,
    createdAt: "2026-05-04T00:00:00.000Z",
    updatedAt: "2026-05-04T00:00:00.000Z",
  };
}

class FakeTunnelDriver implements TunnelDriver {
  openCount = 0;
  health = true;
  lastHandle: TunnelHandle | null = null;
  closed = 0;

  async open(profile: SshProfile): Promise<TunnelHandle> {
    this.openCount += 1;
    const handle: TunnelHandle = {
      profileId: profile.id,
      localPort: 18_000 + this.openCount,
      close: async () => {
        this.closed += 1;
      },
    };
    this.lastHandle = handle;
    return handle;
  }

  async checkHealth(): Promise<boolean> {
    return this.health;
  }
}

describe("hp-e7k :: SshProfileManager", () => {
  test("stores normalized profiles durably and marks the saved profile active", async () => {
    const keyPath = join(workDir, "id_ed25519");
    const manager = new SshProfileManager({
      filePath: join(workDir, "profiles.json"),
      now: () => "2026-05-04T01:02:03.000Z",
    });

    const profile = await manager.saveProfile({
      id: "vps-prod",
      host: "example.internal",
      username: "ubuntu",
      privateKeyPath: keyPath,
      daemonPort: 4_443,
    });

    expect(profile).toMatchObject({
      id: "vps-prod",
      name: "ubuntu@example.internal",
      host: "example.internal",
      port: 22,
      daemonHost: "127.0.0.1",
      daemonPort: 4_443,
      localPortPreference: null,
    });
    expect(await manager.getActiveProfile()).toMatchObject({ id: "vps-prod" });

    const reopened = new SshProfileManager({ filePath: join(workDir, "profiles.json") });
    expect((await reopened.listProfiles()).map((p) => p.id)).toEqual(["vps-prod"]);
    expect(await reopened.getActiveProfile()).toMatchObject({ id: "vps-prod" });
  });

  test("rejects invalid hostnames and relative private-key paths", async () => {
    const manager = new SshProfileManager({ filePath: join(workDir, "profiles.json") });
    await expect(
      manager.saveProfile({
        host: "../bad",
        username: "ubuntu",
        privateKeyPath: join(workDir, "id_ed25519"),
      }),
    ).rejects.toMatchObject({ code: "profile.host-invalid" });
    await expect(
      manager.saveProfile({
        host: "vps.example.com",
        username: "ubuntu",
        privateKeyPath: "id_ed25519",
      }),
    ).rejects.toMatchObject({ code: "profile.private-key-not-absolute" });
  });

  test("rejects malformed profile stores instead of silently resetting them", async () => {
    const filePath = join(workDir, "profiles.json");
    const manager = new SshProfileManager({ filePath });

    await writeFile(filePath, "{", "utf8");
    await expect(manager.listProfiles()).rejects.toMatchObject({
      code: "profile.store-malformed",
      details: { filePath, reason: "invalid JSON" },
    });

    await writeFile(
      filePath,
      JSON.stringify({ schemaVersion: 1, profiles: [], activeProfileId: "missing-profile" }),
      "utf8",
    );
    await expect(manager.listProfiles()).rejects.toMatchObject({
      code: "profile.store-malformed",
      details: {
        filePath,
        reason: "expected schemaVersion 1 with valid profiles and activeProfileId",
      },
    });
  });
});

describe("hp-e7k :: KnownHostStore", () => {
  test("trusts first use, accepts the same fingerprint, and rejects changed host keys", async () => {
    const store = new KnownHostStore({ filePath: join(workDir, "known_hosts.json") });
    const profile = fixtureProfile(join(workDir, "id_ed25519"));
    const firstKey = Buffer.from("host-key-one", "utf8");
    const secondKey = Buffer.from("host-key-two", "utf8");
    const firstFingerprint = fingerprintHostKey(firstKey);

    expect(await store.verifyKey(profile, firstKey)).toEqual({
      ok: true,
      trustedFirstUse: true,
      fingerprint: firstFingerprint,
    });
    expect(await store.verifyKey(profile, firstKey)).toEqual({
      ok: true,
      trustedFirstUse: false,
      fingerprint: firstFingerprint,
    });
    expect(await store.verifyKey(profile, secondKey)).toEqual({
      ok: false,
      expected: firstFingerprint,
      actual: fingerprintHostKey(secondKey),
    });
  });

  test("rejects malformed known-host stores instead of silently trusting first use", async () => {
    const filePath = join(workDir, "known_hosts.json");
    const store = new KnownHostStore({ filePath });
    const profile = fixtureProfile(join(workDir, "id_ed25519"));

    await writeFile(filePath, "{", "utf8");
    await expect(store.verifyFingerprint(profile, "SHA256:first")).rejects.toMatchObject({
      code: "known-hosts.store-malformed",
      details: { filePath, reason: "invalid JSON" },
    });

    await writeFile(
      filePath,
      JSON.stringify({ schemaVersion: 1, hosts: { "vps.example.com:22": 42 } }),
      "utf8",
    );
    await expect(store.verifyFingerprint(profile, "SHA256:first")).rejects.toMatchObject({
      code: "known-hosts.store-malformed",
      details: {
        filePath,
        reason: "expected schemaVersion 1 with a string fingerprint map",
      },
    });
  });

  test("hp-ejx7: emits audit events for trust, re-verify, and mismatch decisions", async () => {
    const audit: KnownHostAuditEvent[] = [];
    const store = new KnownHostStore({
      filePath: join(workDir, "known_hosts.json"),
      audit: (event) => audit.push(event),
      now: () => new Date("2026-05-04T01:02:03.456Z"),
    });
    const profile = fixtureProfile(join(workDir, "id_ed25519"));
    const firstKey = Buffer.from("host-key-one", "utf8");
    const secondKey = Buffer.from("host-key-two", "utf8");
    const firstFingerprint = fingerprintHostKey(firstKey);
    const secondFingerprint = fingerprintHostKey(secondKey);

    await store.verifyKey(profile, firstKey); // first use
    await store.verifyKey(profile, firstKey); // re-verify
    await store.verifyKey(profile, secondKey); // mismatch

    expect(audit).toEqual([
      {
        kind: "tunnel.known-host.trusted_first_use",
        at: "2026-05-04T01:02:03.456Z",
        profileId: profile.id,
        host: profile.host,
        port: profile.port,
        fingerprint: firstFingerprint,
      },
      {
        kind: "tunnel.known-host.verified_existing",
        at: "2026-05-04T01:02:03.456Z",
        profileId: profile.id,
        host: profile.host,
        port: profile.port,
        fingerprint: firstFingerprint,
      },
      {
        kind: "tunnel.known-host.mismatch_refused",
        at: "2026-05-04T01:02:03.456Z",
        profileId: profile.id,
        host: profile.host,
        port: profile.port,
        expected: firstFingerprint,
        actual: secondFingerprint,
      },
    ]);
  });

  test("hp-ejx7: emits store_read_failed before re-throwing on malformed store", async () => {
    const filePath = join(workDir, "known_hosts.json");
    const audit: KnownHostAuditEvent[] = [];
    const store = new KnownHostStore({
      filePath,
      audit: (event) => audit.push(event),
      now: () => new Date("2026-05-04T01:02:03.456Z"),
    });
    const profile = fixtureProfile(join(workDir, "id_ed25519"));

    await writeFile(filePath, "{", "utf8");
    await expect(store.verifyFingerprint(profile, "SHA256:first")).rejects.toMatchObject({
      code: "known-hosts.store-malformed",
    });

    expect(audit).toHaveLength(1);
    expect(audit[0]).toMatchObject({
      kind: "tunnel.known-host.store_read_failed",
      profileId: profile.id,
      host: profile.host,
      port: profile.port,
      errorCode: "known-hosts.store-malformed",
    });
    // The redacted message is the throw text from malformedStoreError —
    // matches the human-readable form, not the bare code, since
    // ConnectionManagerError.message is what the catch handler sees.
    expect(audit[0]!.errorMessage).toContain("invalid JSON");
  });

  test("hp-ejx7: emits store_write_failed before re-throwing on first-use persist error", async () => {
    const audit: KnownHostAuditEvent[] = [];
    // Force write failure by chmod'ing the parent directory read-only
    // AFTER ensuring read returns empty (ENOENT). On the next write
    // attempt, mkdir succeeds (dir already exists) but
    // writeFileStringAtomically cannot create the temp file.
    const roDir = join(workDir, "ro");
    await mkdir(roDir);
    const filePath = join(roDir, "known_hosts.json");
    await chmod(roDir, 0o500);
    try {
      const store = new KnownHostStore({
        filePath,
        audit: (event) => audit.push(event),
        now: () => new Date("2026-05-04T01:02:03.456Z"),
      });
      const profile = fixtureProfile(join(workDir, "id_ed25519"));
      const firstKey = Buffer.from("host-key-one", "utf8");
      const fingerprint = fingerprintHostKey(firstKey);

      await expect(store.verifyKey(profile, firstKey)).rejects.toBeDefined();

      expect(audit).toHaveLength(1);
      expect(audit[0]).toMatchObject({
        kind: "tunnel.known-host.store_write_failed",
        profileId: profile.id,
        host: profile.host,
        port: profile.port,
        fingerprint,
      });
      expect(typeof audit[0]!.errorCode).toBe("string");
      expect(typeof audit[0]!.errorMessage).toBe("string");
    } finally {
      // Restore writable mode so afterEach cleanup can delete the dir.
      await chmod(roDir, 0o700);
    }
  });

  test("hp-ejx7: a throwing audit sink does not derail trust decisions", async () => {
    const store = new KnownHostStore({
      filePath: join(workDir, "known_hosts.json"),
      audit: () => {
        throw new Error("audit sink exploded");
      },
    });
    const profile = fixtureProfile(join(workDir, "id_ed25519"));
    const key = Buffer.from("host-key-one", "utf8");

    // The store's own contract is the trust decision. A throwing sink
    // must not flip a successful trusted-first-use into a refusal.
    const result = await store.verifyKey(profile, key);
    expect(result).toEqual({
      ok: true,
      trustedFirstUse: true,
      fingerprint: fingerprintHostKey(key),
    });
  });
});

describe("hp-e7k :: ConnectionManager FSM", () => {
  test("connects through probe, tunnel, authentication, and ready states", async () => {
    const keyPath = join(workDir, "id_ed25519");
    await writeFile(keyPath, "PRIVATE\n");
    const driver = new FakeTunnelDriver();
    const manager = new ConnectionManager({
      driver,
      now: () => new Date("2026-05-04T02:00:00.000Z"),
      jitter: () => 0.5,
    });

    const snapshot = await manager.connect(fixtureProfile(keyPath));

    expect(snapshot).toMatchObject({
      state: "ready",
      activeProfileId: "vps-main",
      localPort: 18_001,
      reconnectAttempts: 0,
      nextRetryAt: null,
    });
    expect(manager.transitionHistory().map((entry) => entry.to)).toEqual([
      "ssh_probing",
      "tunnel_connecting",
      "authenticating",
      "ready",
    ]);
  });

  test("records health failures as bounded reconnect attempts and retryNow returns to ready", async () => {
    const keyPath = join(workDir, "id_ed25519");
    await writeFile(keyPath, "PRIVATE\n");
    const driver = new FakeTunnelDriver();
    const manager = new ConnectionManager({
      driver,
      now: () => new Date("2026-05-04T03:00:00.000Z"),
      jitter: () => 0.5,
    });
    await manager.connect(fixtureProfile(keyPath));

    driver.health = false;
    expect(await manager.checkHealth()).toBe(false);
    expect(manager.snapshot()).toMatchObject({
      state: "reconnecting",
      reconnectAttempts: 1,
      nextRetryAt: "2026-05-04T03:00:01.000Z",
      lastFault: { code: "daemon.health.failed" },
    });

    const retried = await manager.retryNow();
    expect(retried).toMatchObject({
      state: "ready",
      localPort: 18_002,
      reconnectAttempts: 0,
      nextRetryAt: null,
      lastFault: null,
    });
  });

  test("sleep, network, bearer, version, and disconnect triggers are explicit", async () => {
    const keyPath = join(workDir, "id_ed25519");
    await writeFile(keyPath, "PRIVATE\n");
    const driver = new FakeTunnelDriver();
    const manager = new ConnectionManager({
      driver,
      now: () => new Date("2026-05-04T04:00:00.000Z"),
      jitter: () => 0,
    });
    await manager.connect(fixtureProfile(keyPath));

    expect(manager.handleWake()).toMatchObject({ state: "reconnecting", reconnectAttempts: 1 });
    expect(manager.handleNetworkChange()).toMatchObject({ state: "reconnecting", reconnectAttempts: 2 });
    expect(manager.handleRouteChange()).toMatchObject({
      state: "reconnecting",
      reconnectAttempts: 3,
      nextRetryAt: "2026-05-04T04:00:00.000Z",
      lastFault: { code: "network.route_changed" },
    });
    expect(manager.handleVpnStateChange()).toMatchObject({
      state: "reconnecting",
      reconnectAttempts: 4,
      nextRetryAt: "2026-05-04T04:00:00.000Z",
      lastFault: { code: "network.vpn_state_changed" },
    });
    expect(manager.handleBearerExpired()).toMatchObject({
      state: "reconnecting",
      reconnectAttempts: 5,
      lastFault: { code: "bearer.expired" },
    });
    expect(manager.markVersionMismatch("daemon API is too old")).toMatchObject({
      state: "degraded",
      lastFault: { code: "version.mismatch" },
    });
    expect(await manager.disconnect()).toMatchObject({
      state: "disconnected",
      activeProfileId: "vps-main",
      localPort: null,
    });
    expect(driver.closed).toBe(1);
    expect(manager.transitionHistory().map((entry) => entry.trigger)).toContain("version.mismatch");
  });

  test("network offline and captive portal states pause retries until recovery", async () => {
    const keyPath = join(workDir, "id_ed25519");
    await writeFile(keyPath, "PRIVATE\n");
    const driver = new FakeTunnelDriver();
    const manager = new ConnectionManager({
      driver,
      now: () => new Date("2026-05-04T04:30:00.000Z"),
      jitter: () => 0,
    });
    await manager.connect(fixtureProfile(keyPath));

    expect(manager.handleNetworkOffline()).toMatchObject({
      state: "awaiting_network",
      localPort: null,
      nextRetryAt: null,
      lastFault: { code: "network.offline" },
    });
    expect(manager.handleNetworkOnline()).toMatchObject({
      state: "reconnecting",
      reconnectAttempts: 1,
      nextRetryAt: "2026-05-04T04:30:00.000Z",
      lastFault: { code: "network.online" },
    });

    expect(manager.handleCaptivePortalDetected()).toMatchObject({
      state: "captive_portal_blocked",
      localPort: null,
      nextRetryAt: null,
      lastFault: { code: "network.captive_portal_detected" },
    });
    expect(manager.handleCaptivePortalCleared()).toMatchObject({
      state: "reconnecting",
      reconnectAttempts: 2,
      nextRetryAt: "2026-05-04T04:30:00.000Z",
      lastFault: { code: "network.captive_portal_cleared" },
    });
    expect(manager.transitionHistory().map((entry) => entry.trigger)).toContain("network.captive_portal_detected");
  });

  test("handleNetworkSignal dispatches macOS monitor signals into explicit triggers", async () => {
    const keyPath = join(workDir, "id_ed25519");
    await writeFile(keyPath, "PRIVATE\n");
    const driver = new FakeTunnelDriver();
    const manager = new ConnectionManager({
      driver,
      now: () => new Date("2026-05-04T04:45:00.000Z"),
      jitter: () => 0,
    });
    await manager.connect(fixtureProfile(keyPath));

    expect(manager.handleNetworkSignal({ kind: "network.vpn_state_changed", detail: { vpnUp: true } })).toMatchObject({
      state: "reconnecting",
      lastFault: {
        code: "network.vpn_state_changed",
        message: "VPN connected. Re-establishing tunnel via VPN route.",
      },
    });
    expect(manager.handleNetworkSignal({ kind: "network.ssid_changed" })).toMatchObject({
      state: "reconnecting",
      lastFault: { code: "network.vpn_state_changed" },
    });
  });

  test("diagnosticsSnapshot exposes current state and reasoned transitions", async () => {
    const keyPath = join(workDir, "id_ed25519");
    await writeFile(keyPath, "PRIVATE\n");
    const driver = new FakeTunnelDriver();
    const manager = new ConnectionManager({
      driver,
      now: () => new Date("2026-05-04T05:00:00.000Z"),
      jitter: () => 0.5,
    });
    await manager.connect(fixtureProfile(keyPath));

    driver.health = false;
    await manager.checkHealth();

    const diagnostics = manager.diagnosticsSnapshot();

    expect(diagnostics.capturedAt).toBe("2026-05-04T05:00:00.000Z");
    expect(diagnostics.current).toMatchObject({
      state: "reconnecting",
      activeProfileId: "vps-main",
      localPort: null,
      reconnectAttempts: 1,
    });
    expect(diagnostics.recentTransitions.map((entry) => entry.trigger)).toEqual([
      "connect.requested",
      "ssh.probe.ok",
      "tunnel.opened",
      "auth.ok",
      "daemon.health.failed",
    ]);
    expect(diagnostics.recentTransitions.at(-1)).toMatchObject({
      to: "reconnecting",
      reason: "Daemon health check failed.",
      fault: { code: "daemon.health.failed" },
    });
  });

  test("diagnosticsSnapshot defaults to the last 20 transitions", async () => {
    const keyPath = join(workDir, "id_ed25519");
    await writeFile(keyPath, "PRIVATE\n");
    const driver = new FakeTunnelDriver();
    const manager = new ConnectionManager({
      driver,
      now: () => new Date("2026-05-04T06:00:00.000Z"),
      jitter: () => 0,
    });
    await manager.connect(fixtureProfile(keyPath));

    for (let i = 0; i < 25; i += 1) {
      manager.handleNetworkChange();
    }

    const diagnostics = manager.diagnosticsSnapshot();
    expect(diagnostics.recentTransitions).toHaveLength(20);
    expect(diagnostics.recentTransitions.every((entry) => entry.trigger === "network.changed")).toBe(true);
    expect(diagnostics.recentTransitions.every((entry) => entry.reason === "Network changed; SSH tunnel may be stale.")).toBe(true);
    expect(manager.diagnosticsSnapshot(1).recentTransitions).toHaveLength(1);
    expect(manager.diagnosticsSnapshot(0).recentTransitions).toHaveLength(20);
  });

  test("missing private keys fail before opening a tunnel", async () => {
    const driver = new FakeTunnelDriver();
    const manager = new ConnectionManager({ driver });
    await expect(manager.connect(fixtureProfile(join(workDir, "missing")))).rejects.toBeInstanceOf(
      ConnectionManagerError,
    );
    expect(driver.openCount).toBe(0);
  });
});

test("hp-e7k :: retryDelayMs applies exponential backoff, jitter, and 30s cap", () => {
  expect(retryDelayMs(1, 0.5)).toBe(1_000);
  expect(retryDelayMs(2, 0)).toBe(1_500);
  expect(retryDelayMs(8, 1)).toBe(30_000);
});
