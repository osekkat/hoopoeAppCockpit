import { afterEach, beforeEach, describe, expect, test } from "bun:test";
import { mkdtemp, rm, writeFile } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";
import {
  ConnectionManager,
  ConnectionManagerError,
  KnownHostStore,
  SshProfileManager,
  fingerprintHostKey,
  retryDelayMs,
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
    expect(manager.handleBearerExpired()).toMatchObject({
      state: "reconnecting",
      reconnectAttempts: 3,
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
