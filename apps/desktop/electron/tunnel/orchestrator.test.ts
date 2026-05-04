import { beforeEach, expect, test } from "bun:test";
import { TunnelOrchestrator, type OpenTunnel, type ScheduledHandle, type Scheduler } from "./orchestrator.ts";
import { makeProfile, type VpsProfile } from "./index.ts";

let profile: VpsProfile;
let clock: Date;

beforeEach(() => {
  clock = new Date("2026-05-04T04:00:00Z");
  profile = makeProfile({
    id: "profile-1",
    label: "VPS",
    host: "vps.example.test",
    username: "ubuntu",
    privateKeyPath: "/Users/me/.ssh/id_ed25519",
    now: () => clock,
  });
});

test("connect drives probe, bootstrap, tunnel open, auth, heartbeat, then ready", async () => {
  const calls: string[] = [];
  const tunnel = new FakeOpenTunnel(17655);
  const orchestrator = new TunnelOrchestrator({
    tunnel: {
      probe: async () => calls.push("probe"),
      bootstrap: async () => calls.push("bootstrap"),
      open: async () => {
        calls.push("open");
        return tunnel;
      },
    },
    auth: { authenticate: async () => calls.push("auth") },
    heartbeat: { check: async () => (calls.push("heartbeat"), "ok") },
    now: () => clock,
  });

  const snapshot = await orchestrator.connect(profile);

  expect(calls).toEqual(["probe", "bootstrap", "open", "auth", "heartbeat"]);
  expect(snapshot.state).toBe("ready");
  expect(snapshot.activeProfileId).toBe(profile.id);
  expect(snapshot.localPort).toBe(17655);
  expect(snapshot.lastFault).toBeNull();
});

test("probe failure enters reconnecting and scheduled retry can reach ready", async () => {
  let probeFailures = 1;
  const scheduler = new FakeScheduler();
  const orchestrator = new TunnelOrchestrator({
    tunnel: {
      probe: async () => {
        if (probeFailures > 0) {
          probeFailures -= 1;
          throw new Error("ssh refused");
        }
      },
      bootstrap: async () => {},
      open: async () => new FakeOpenTunnel(17655),
    },
    auth: { authenticate: async () => {} },
    heartbeat: { check: async () => "ok" },
    scheduler,
    now: () => clock,
  });

  const failed = await orchestrator.connect(profile);

  expect(failed.state).toBe("reconnecting");
  expect(failed.lastFault?.code).toBe("ssh_unreachable");
  expect(scheduler.scheduled).toHaveLength(1);

  scheduler.runNext();
  await flushAsync();

  expect(orchestrator.snapshot.state).toBe("ready");
  expect(orchestrator.snapshot.localPort).toBe(17655);
});

test("tunnel close from ready schedules a reconnect and clears local port", async () => {
  const scheduler = new FakeScheduler();
  const tunnel = new FakeOpenTunnel(18000);
  const orchestrator = new TunnelOrchestrator({
    tunnel: {
      probe: async () => {},
      bootstrap: async () => {},
      open: async () => tunnel,
    },
    auth: { authenticate: async () => {} },
    heartbeat: { check: async () => "ok" },
    scheduler,
    now: () => clock,
  });
  await orchestrator.connect(profile);

  tunnel.emitClose(new Error("socket closed"));

  expect(orchestrator.snapshot.state).toBe("reconnecting");
  expect(orchestrator.snapshot.localPort).toBeNull();
  expect(orchestrator.snapshot.lastFault?.code).toBe("tunnel_dropped");
  expect(scheduler.scheduled).toHaveLength(1);
});

test("bearer expiry re-authenticates over the existing tunnel", async () => {
  let authCalls = 0;
  const orchestrator = new TunnelOrchestrator({
    tunnel: {
      probe: async () => {},
      bootstrap: async () => {},
      open: async () => new FakeOpenTunnel(17655),
    },
    auth: {
      authenticate: async () => {
        authCalls += 1;
      },
    },
    heartbeat: { check: async () => "ok" },
    now: () => clock,
  });
  await orchestrator.connect(profile);

  const snapshot = await orchestrator.handleBearerExpired();

  expect(authCalls).toBe(2);
  expect(snapshot.state).toBe("ready");
  expect(snapshot.localPort).toBe(17655);
});

test("system sleep closes the tunnel and wake restarts the profile", async () => {
  const tunnels: FakeOpenTunnel[] = [];
  const orchestrator = new TunnelOrchestrator({
    tunnel: {
      probe: async () => {},
      bootstrap: async () => {},
      open: async () => {
        const tunnel = new FakeOpenTunnel(17655 + tunnels.length);
        tunnels.push(tunnel);
        return tunnel;
      },
    },
    auth: { authenticate: async () => {} },
    heartbeat: { check: async () => "ok" },
    now: () => clock,
  });
  await orchestrator.connect(profile);

  const sleeping = await orchestrator.handleSystemSleep();

  expect(sleeping.state).toBe("disconnected");
  expect(tunnels[0].closed).toBe(true);

  const awake = await orchestrator.handleSystemWake();

  expect(awake.state).toBe("ready");
  expect(awake.localPort).toBe(17656);
});

test("heartbeat version mismatch degrades without dropping the tunnel", async () => {
  const tunnel = new FakeOpenTunnel(17655);
  const orchestrator = new TunnelOrchestrator({
    tunnel: {
      probe: async () => {},
      bootstrap: async () => {},
      open: async () => tunnel,
    },
    auth: { authenticate: async () => {} },
    heartbeat: { check: async () => "version_mismatch" },
    now: () => clock,
  });

  const snapshot = await orchestrator.connect(profile);

  expect(snapshot.state).toBe("degraded");
  expect(snapshot.localPort).toBe(17655);
  expect(snapshot.lastFault?.code).toBe("version_incompatible");
  expect(tunnel.closed).toBe(false);
});

class FakeOpenTunnel implements OpenTunnel {
  readonly localPort: number;
  closed = false;
  #onClose: ((reason?: Error) => void) | null = null;

  constructor(localPort: number) {
    this.localPort = localPort;
  }

  onClose(handler: (reason?: Error) => void): void {
    this.#onClose = handler;
  }

  async close(): Promise<void> {
    this.closed = true;
  }

  emitClose(reason?: Error): void {
    this.#onClose?.(reason);
  }
}

class FakeScheduler implements Scheduler {
  readonly scheduled: Array<{ delayMs: number; callback: () => void; handle: FakeScheduledHandle }> = [];

  schedule(delayMs: number, callback: () => void): ScheduledHandle {
    const handle = new FakeScheduledHandle();
    this.scheduled.push({ delayMs, callback, handle });
    return handle;
  }

  runNext(): void {
    const next = this.scheduled.shift();
    if (next && !next.handle.cancelled) {
      next.callback();
    }
  }
}

class FakeScheduledHandle implements ScheduledHandle {
  cancelled = false;

  cancel(): void {
    this.cancelled = true;
  }
}

async function flushAsync(): Promise<void> {
  for (let i = 0; i < 10; i += 1) {
    await Promise.resolve();
  }
}
