// hp-si53 — macOS network monitor tests.

import { describe, expect, test } from "bun:test";

import {
  APPLE_CAPTIVE_PORTAL_SUCCESS_HTML,
  classifyCaptivePortalResponse,
  coalesceNetworkSignals,
  detectMacNetworkSignals,
  installMacNetworkMonitor,
  networkSignalMessage,
  parseAirportNetwork,
  parseDefaultRoute,
  parseInterfaceList,
  parseWifiDevice,
  probeCaptivePortal,
  tunnelEventForNetworkSignal,
  vpnInterfacesFromList,
  type ElectronNetLike,
  type MacNetworkSnapshot,
  type NetworkMonitorScheduler,
  type NetworkSignal,
} from "./networkMonitor.ts";

const FIXED_NOW = () => new Date("2026-05-04T07:00:00.000Z");

describe("hp-si53 :: parser helpers", () => {
  test("parseDefaultRoute: extracts interface + gateway from route output", () => {
    expect(parseDefaultRoute(`
   route to: default
destination: default
       mask: default
    gateway: 192.168.1.1
  interface: en0
      flags: <UP,GATEWAY,DONE,STATIC,PRCLONING>
`)).toEqual({ interfaceName: "en0", gateway: "192.168.1.1" });
    expect(parseDefaultRoute("route: writing to routing socket: not in table")).toBeNull();
  });

  test("parseWifiDevice: finds Wi-Fi hardware device from networksetup listing", () => {
    expect(parseWifiDevice(`
Hardware Port: Ethernet
Device: en7
Ethernet Address: 00:00:00:00:00:01

Hardware Port: Wi-Fi
Device: en0
Ethernet Address: 00:00:00:00:00:02
`)).toBe("en0");
  });

  test("parseAirportNetwork: extracts current SSID and treats not-associated as null", () => {
    expect(parseAirportNetwork("Current Wi-Fi Network: HomeWifi\n")).toBe("HomeWifi");
    expect(parseAirportNetwork("You are not associated with an AirPort network.\n")).toBeNull();
  });

  test("parseInterfaceList + vpnInterfacesFromList: detects tun/utun/ppp/ipsec devices", () => {
    expect(parseInterfaceList("lo0 gif0 stf0 en0 awdl0 utun0 utun3 ppp1 ipsec0\n")).toEqual([
      "lo0",
      "gif0",
      "stf0",
      "en0",
      "awdl0",
      "utun0",
      "utun3",
      "ppp1",
      "ipsec0",
    ]);
    expect(vpnInterfacesFromList("lo0 en0 utun3 utun0 ppp1 ipsec0\n")).toEqual([
      "ipsec0",
      "ppp1",
      "utun0",
      "utun3",
    ]);
  });
});

describe("hp-si53 :: signal detection", () => {
  test("detectMacNetworkSignals: route, SSID, and VPN deltas are structured", () => {
    const before: MacNetworkSnapshot = {
      capturedAt: "2026-05-04T07:00:00.000Z",
      online: true,
      defaultRoute: { interfaceName: "en0", gateway: "192.168.1.1" },
      ssid: "HomeWifi",
      vpnInterfaces: [],
    };
    const after: MacNetworkSnapshot = {
      capturedAt: "2026-05-04T07:00:05.000Z",
      online: true,
      defaultRoute: { interfaceName: "en1", gateway: "10.0.0.1" },
      ssid: "CafeWifi",
      vpnInterfaces: ["utun0"],
    };

    const signals = detectMacNetworkSignals(before, after);

    expect(signals.map((signal) => signal.kind)).toEqual([
      "network.route_changed",
      "network.ssid_changed",
      "network.vpn_state_changed",
    ]);
    expect(signals[0]?.detail).toMatchObject({ fromInterface: "en0", toInterface: "en1" });
    expect(signals[1]?.detail).toMatchObject({ fromSsid: "HomeWifi", toSsid: "CafeWifi" });
    expect(signals[2]?.detail).toMatchObject({ vpnUp: true });
  });

  test("detectMacNetworkSignals: net.online false/true emits offline/online", () => {
    const base: MacNetworkSnapshot = {
      capturedAt: "2026-05-04T07:00:00.000Z",
      online: true,
      defaultRoute: null,
      ssid: null,
      vpnInterfaces: [],
    };
    const offline = { ...base, capturedAt: "2026-05-04T07:00:05.000Z", online: false };
    const online = { ...base, capturedAt: "2026-05-04T07:00:10.000Z", online: true };

    expect(detectMacNetworkSignals(base, offline).map((signal) => signal.kind)).toEqual(["network.offline"]);
    expect(detectMacNetworkSignals(offline, online).map((signal) => signal.kind)).toEqual(["network.online"]);
  });

  test("coalesceNetworkSignals: flap storms collapse to one deterministic transition", () => {
    const signals: NetworkSignal[] = [
      { kind: "network.offline", capturedAt: "2026-05-04T07:00:00.000Z" },
      { kind: "network.online", capturedAt: "2026-05-04T07:00:00.100Z" },
      { kind: "network.route_changed", capturedAt: "2026-05-04T07:00:00.300Z" },
      { kind: "network.vpn_state_changed", capturedAt: "2026-05-04T07:00:00.600Z" },
      { kind: "network.route_changed", capturedAt: "2026-05-04T07:00:00.799Z" },
    ];

    const coalesced = coalesceNetworkSignals(signals, 1_000);

    expect(coalesced).toHaveLength(1);
    expect(coalesced[0]?.kind).toBe("network.route_changed");
    expect(coalesced[0]?.detail?.["coalescedKinds"]).toEqual(signals.map((signal) => signal.kind));
  });

  test("tunnelEventForNetworkSignal: maps only FSM-relevant signals", () => {
    expect(tunnelEventForNetworkSignal({ kind: "network.route_changed", capturedAt: FIXED_NOW().toISOString() })).toBe("network_route_changed");
    expect(tunnelEventForNetworkSignal({ kind: "network.ssid_changed", capturedAt: FIXED_NOW().toISOString() })).toBeNull();
    expect(networkSignalMessage({ kind: "network.captive_portal_detected", capturedAt: FIXED_NOW().toISOString() })).toContain("Captive portal");
  });
});

describe("hp-si53 :: captive portal probe", () => {
  test("classifyCaptivePortalResponse: Apple success HTML is open, other 2xx HTML is captive", () => {
    expect(
      classifyCaptivePortalResponse({
        status: 200,
        body: `\n${APPLE_CAPTIVE_PORTAL_SUCCESS_HTML.toLowerCase()}\n`,
      }),
    ).toBe("open");
    expect(classifyCaptivePortalResponse({ status: 200, body: "<html>coffee shop login</html>" })).toBe("captive");
    expect(classifyCaptivePortalResponse({ status: 302, body: "" })).toBe("inconclusive");
  });

  test("probeCaptivePortal: emits typed signals only for conclusive responses", async () => {
    const captive = await probeCaptivePortal({
      now: FIXED_NOW,
      fetcher: async () => ({
        status: 200,
        text: async () => "<html>coffee shop login</html>",
      }),
    });
    expect(captive.classification).toBe("captive");
    expect(captive.signal?.kind).toBe("network.captive_portal_detected");

    const open = await probeCaptivePortal({
      now: FIXED_NOW,
      fetcher: async () => ({
        status: 200,
        text: async () => APPLE_CAPTIVE_PORTAL_SUCCESS_HTML,
      }),
    });
    expect(open.classification).toBe("open");
    expect(open.signal?.kind).toBe("network.captive_portal_cleared");

    const timeout = await probeCaptivePortal({
      now: FIXED_NOW,
      fetcher: async () => {
        throw new Error("timeout");
      },
    });
    expect(timeout).toEqual({ classification: "inconclusive", signal: null });
  });
});

describe("hp-si53 :: installMacNetworkMonitor", () => {
  test("net.online events are debounced and forwarded to the orchestrator", async () => {
    const net = new FakeNet();
    const scheduler = new FakeScheduler();
    const observed: NetworkSignal[] = [];
    const orchestrated: NetworkSignal[] = [];

    const handle = installMacNetworkMonitor({
      net,
      scheduler,
      now: FIXED_NOW,
      platform: "linux",
      debounceWindowMs: 1_000,
      onSignal: (signal) => observed.push(signal),
      orchestrator: { handleNetworkSignal: (signal) => orchestrated.push(signal) },
    });

    net.emit("offline");
    net.emit("online");
    expect(observed).toHaveLength(0);

    scheduler.advance(1_000);
    await flushAsync();
    expect(observed).toHaveLength(1);
    expect(observed[0]?.kind).toBe("network.changed");
    expect(orchestrated.map((signal) => signal.kind)).toEqual(["network.changed"]);

    handle.uninstall();
    net.emit("offline");
    scheduler.advance(1_000);
    expect(observed).toHaveLength(1);
  });

  test("darwin polling emits route changes from command snapshots", async () => {
    const scheduler = new FakeScheduler();
    const observed: NetworkSignal[] = [];
    let routeInterface = "en0";
    let gateway = "192.168.1.1";

    installMacNetworkMonitor({
      scheduler,
      platform: "darwin",
      pollIntervalMs: 5_000,
      debounceWindowMs: 1_000,
      now: FIXED_NOW,
      onSignal: (signal) => observed.push(signal),
      runCommand: async (command, args) => {
        if (command === "networksetup" && args[0] === "-listallhardwareports") {
          return { stdout: "Hardware Port: Wi-Fi\nDevice: en0\n" };
        }
        if (command === "networksetup") {
          return { stdout: "Current Wi-Fi Network: HomeWifi\n" };
        }
        if (command === "ifconfig") return { stdout: "lo0 en0\n" };
        if (command === "route") {
          return { stdout: `gateway: ${gateway}\ninterface: ${routeInterface}\n` };
        }
        throw new Error(`unexpected command ${command}`);
      },
    });

    await flushAsync();
    routeInterface = "en1";
    gateway = "10.0.0.1";
    scheduler.tickInterval();
    await flushAsync();
    scheduler.advance(1_000);
    await flushAsync();

    expect(observed.map((signal) => signal.kind)).toEqual(["network.route_changed"]);
    expect(observed[0]?.detail).toMatchObject({ fromInterface: "en0", toInterface: "en1" });
  });
});

async function flushAsync(): Promise<void> {
  for (let i = 0; i < 10; i += 1) {
    await Promise.resolve();
  }
}

class FakeNet implements ElectronNetLike {
  readonly listeners = new Map<"online" | "offline", Set<() => void>>();

  on(event: "online" | "offline", handler: () => void): void {
    const set = this.listeners.get(event) ?? new Set<() => void>();
    set.add(handler);
    this.listeners.set(event, set);
  }

  off(event: "online" | "offline", handler: () => void): void {
    this.listeners.get(event)?.delete(handler);
  }

  emit(event: "online" | "offline"): void {
    for (const handler of this.listeners.get(event) ?? []) {
      handler();
    }
  }
}

class FakeScheduler implements NetworkMonitorScheduler {
  private nowMs = 0;
  private nextId = 1;
  private readonly timeouts = new Map<number, { at: number; callback: () => void }>();
  private readonly intervals = new Map<number, { intervalMs: number; callback: () => void }>();

  setTimeout(callback: () => void, delayMs: number): unknown {
    const id = this.nextId;
    this.nextId += 1;
    this.timeouts.set(id, { at: this.nowMs + delayMs, callback });
    return id;
  }

  clearTimeout(handle: unknown): void {
    this.timeouts.delete(Number(handle));
  }

  setInterval(callback: () => void, intervalMs: number): unknown {
    const id = this.nextId;
    this.nextId += 1;
    this.intervals.set(id, { intervalMs, callback });
    return id;
  }

  clearInterval(handle: unknown): void {
    this.intervals.delete(Number(handle));
  }

  advance(deltaMs: number): void {
    this.nowMs += deltaMs;
    for (const [id, timeout] of [...this.timeouts]) {
      if (timeout.at <= this.nowMs) {
        this.timeouts.delete(id);
        timeout.callback();
      }
    }
  }

  tickInterval(): void {
    for (const interval of this.intervals.values()) {
      void interval.intervalMs;
      interval.callback();
    }
  }
}
