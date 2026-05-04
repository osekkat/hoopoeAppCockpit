// hp-si53 — macOS network-change signal sources for the tunnel FSM.
//
// This module is the side-effect boundary for Wi-Fi / route / VPN /
// captive-portal detection. Production wiring injects Electron's `net`
// object, command execution, and the tunnel orchestrator. Tests exercise
// the parsers and debounce behavior without touching host networking.

import { execFile } from "node:child_process";
import { promisify } from "node:util";

import type { TunnelEvent } from "./types.ts";

const execFileAsync = promisify(execFile);

export const CAPTIVE_PORTAL_PROBE_URL = "http://captive.apple.com/hotspot-detect.html";
export const APPLE_CAPTIVE_PORTAL_SUCCESS_HTML =
  "<HTML><HEAD><TITLE>Success</TITLE></HEAD><BODY>Success</BODY></HTML>";

export type NetworkSignalKind =
  | "network.changed"
  | "network.offline"
  | "network.online"
  | "network.route_changed"
  | "network.ssid_changed"
  | "network.vpn_state_changed"
  | "network.captive_portal_detected"
  | "network.captive_portal_cleared";

export type NetworkSignalDetailValue =
  | string
  | number
  | boolean
  | null
  | readonly string[];

export interface NetworkSignal {
  readonly kind: NetworkSignalKind;
  readonly capturedAt: string;
  readonly detail?: Readonly<Record<string, NetworkSignalDetailValue>>;
}

export interface DefaultRouteSnapshot {
  readonly interfaceName: string;
  readonly gateway: string;
}

export interface MacNetworkSnapshot {
  readonly capturedAt: string;
  readonly online: boolean | null;
  readonly defaultRoute: DefaultRouteSnapshot | null;
  readonly ssid: string | null;
  readonly vpnInterfaces: readonly string[];
}

export type CaptivePortalClassification = "open" | "captive" | "inconclusive";

export interface CaptivePortalProbeResult {
  readonly classification: CaptivePortalClassification;
  readonly signal: NetworkSignal | null;
}

export interface CaptivePortalResponse {
  readonly status: number;
  text(): Promise<string>;
}

export type CaptivePortalFetcher = (
  url: string,
  init?: { readonly signal?: AbortSignal },
) => Promise<CaptivePortalResponse>;

export type CommandRunner = (
  command: string,
  args: readonly string[],
) => Promise<{ readonly stdout: string }>;

export interface NetworkMonitorScheduler {
  setTimeout(callback: () => void, delayMs: number): unknown;
  clearTimeout(handle: unknown): void;
  setInterval(callback: () => void, intervalMs: number): unknown;
  clearInterval(handle: unknown): void;
}

export type NetworkSignalSink = (signal: NetworkSignal) => void | Promise<void>;

export interface NetworkSignalOrchestrator {
  handleNetworkSignal(signal: NetworkSignal): unknown;
}

export interface ElectronNetLike {
  readonly online?: boolean;
  isOnline?(): boolean;
  on?(event: "online" | "offline", handler: () => void): void;
  off?(event: "online" | "offline", handler: () => void): void;
  removeListener?(event: "online" | "offline", handler: () => void): void;
}

export interface NetworkMonitorOptions {
  readonly onSignal?: NetworkSignalSink;
  readonly orchestrator?: NetworkSignalOrchestrator;
  readonly net?: ElectronNetLike;
  readonly runCommand?: CommandRunner;
  readonly fetcher?: CaptivePortalFetcher;
  readonly scheduler?: NetworkMonitorScheduler;
  readonly now?: () => Date;
  readonly platform?: NodeJS.Platform;
  readonly pollIntervalMs?: number;
  readonly debounceWindowMs?: number;
  readonly wifiInterface?: string;
  readonly logFailure?: (err: unknown) => void;
}

export interface NetworkMonitorHandle {
  uninstall(): void;
}

export function parseDefaultRoute(output: string): DefaultRouteSnapshot | null {
  let interfaceName: string | null = null;
  let gateway: string | null = null;
  for (const line of output.split(/\r?\n/u)) {
    const separator = line.indexOf(":");
    if (separator === -1) continue;
    const key = line.slice(0, separator).trim();
    const value = line.slice(separator + 1).trim();
    if (!value) continue;
    if (key === "interface") interfaceName = value;
    if (key === "gateway") gateway = value;
  }
  if (!interfaceName || !gateway) return null;
  return { interfaceName, gateway };
}

export function parseWifiDevice(output: string): string | null {
  const blocks = output.split(/\n\s*\n/u);
  for (const block of blocks) {
    if (!/Hardware Port:\s*(Wi-Fi|AirPort)/iu.test(block)) continue;
    const match = block.match(/^\s*Device:\s*(\S+)\s*$/imu);
    return match?.[1] ?? null;
  }
  return null;
}

export function parseAirportNetwork(output: string): string | null {
  const match = output.match(/Current Wi-Fi Network:\s*(.+?)\s*$/imu);
  if (!match?.[1]) return null;
  const ssid = match[1].trim();
  if (!ssid || /not associated|off|unavailable/iu.test(ssid)) return null;
  return ssid;
}

export function parseInterfaceList(output: string): readonly string[] {
  return output
    .split(/\s+/u)
    .map((iface) => iface.trim())
    .filter(Boolean);
}

export function vpnInterfacesFromList(output: string): readonly string[] {
  return parseInterfaceList(output)
    .filter((iface) => /^(?:u?tun|ppp|ipsec)\d+$/iu.test(iface))
    .toSorted((a, b) => a.localeCompare(b));
}

export function hasRouteChanged(
  previous: DefaultRouteSnapshot | null,
  next: DefaultRouteSnapshot | null,
): boolean {
  if (previous === null && next === null) return false;
  if (previous === null || next === null) return true;
  return previous.interfaceName !== next.interfaceName || previous.gateway !== next.gateway;
}

export function hasSsidChanged(previous: string | null, next: string | null): boolean {
  return previous !== next;
}

export function hasVpnStateChanged(
  previous: readonly string[],
  next: readonly string[],
): boolean {
  return previous.join("\0") !== next.join("\0");
}

export function detectMacNetworkSignals(
  previous: MacNetworkSnapshot,
  next: MacNetworkSnapshot,
): readonly NetworkSignal[] {
  const signals: NetworkSignal[] = [];
  if (previous.online === true && next.online === false) {
    signals.push({ kind: "network.offline", capturedAt: next.capturedAt });
  } else if (previous.online === false && next.online === true) {
    signals.push({ kind: "network.online", capturedAt: next.capturedAt });
  }

  if (hasRouteChanged(previous.defaultRoute, next.defaultRoute)) {
    signals.push({
      kind: "network.route_changed",
      capturedAt: next.capturedAt,
      detail: {
        fromInterface: previous.defaultRoute?.interfaceName ?? null,
        toInterface: next.defaultRoute?.interfaceName ?? null,
        fromGateway: previous.defaultRoute?.gateway ?? null,
        toGateway: next.defaultRoute?.gateway ?? null,
      },
    });
  }

  if (hasSsidChanged(previous.ssid, next.ssid)) {
    signals.push({
      kind: "network.ssid_changed",
      capturedAt: next.capturedAt,
      detail: {
        fromSsid: previous.ssid,
        toSsid: next.ssid,
      },
    });
  }

  if (hasVpnStateChanged(previous.vpnInterfaces, next.vpnInterfaces)) {
    signals.push({
      kind: "network.vpn_state_changed",
      capturedAt: next.capturedAt,
      detail: {
        fromVpnInterfaces: previous.vpnInterfaces,
        toVpnInterfaces: next.vpnInterfaces,
        vpnUp: next.vpnInterfaces.length > previous.vpnInterfaces.length,
      },
    });
  }

  return signals;
}

export function coalesceNetworkSignals(
  signals: readonly NetworkSignal[],
  windowMs = 1_000,
): readonly NetworkSignal[] {
  if (signals.length <= 1) return signals;
  const sorted = [...signals].toSorted((a, b) => Date.parse(a.capturedAt) - Date.parse(b.capturedAt));
  const out: NetworkSignal[] = [];
  let group: NetworkSignal[] = [];
  let groupStart = Number.NaN;

  for (const signal of sorted) {
    const ts = Date.parse(signal.capturedAt);
    if (group.length === 0 || ts - groupStart < windowMs) {
      if (group.length === 0) groupStart = ts;
      group.push(signal);
      continue;
    }
    out.push(collapseSignalGroup(group));
    group = [signal];
    groupStart = ts;
  }
  if (group.length > 0) out.push(collapseSignalGroup(group));
  return out;
}

export function classifyCaptivePortalResponse(input: {
  readonly status: number;
  readonly body: string;
}): CaptivePortalClassification {
  if (input.status < 200 || input.status >= 300) return "inconclusive";
  return normalizeHtml(input.body) === normalizeHtml(APPLE_CAPTIVE_PORTAL_SUCCESS_HTML)
    ? "open"
    : "captive";
}

export async function probeCaptivePortal(input: {
  readonly fetcher?: CaptivePortalFetcher;
  readonly now?: () => Date;
  readonly url?: string;
  readonly timeoutMs?: number;
} = {}): Promise<CaptivePortalProbeResult> {
  const fetcher = input.fetcher ?? defaultFetcher;
  const now = input.now ?? (() => new Date());
  const url = input.url ?? CAPTIVE_PORTAL_PROBE_URL;
  const controller = new AbortController();
  const timeout = setTimeout(() => controller.abort(), input.timeoutMs ?? 3_000);
  try {
    const response = await fetcher(url, { signal: controller.signal });
    const body = await response.text();
    const classification = classifyCaptivePortalResponse({ status: response.status, body });
    return {
      classification,
      signal: captivePortalSignalForClassification(classification, now),
    };
  } catch {
    return { classification: "inconclusive", signal: null };
  } finally {
    clearTimeout(timeout);
  }
}

export function tunnelEventForNetworkSignal(signal: NetworkSignal): TunnelEvent | null {
  switch (signal.kind) {
    case "network.changed":
      return "network_changed";
    case "network.offline":
      return "network_offline";
    case "network.online":
      return "network_online";
    case "network.route_changed":
      return "network_route_changed";
    case "network.vpn_state_changed":
      return "network_vpn_state_changed";
    case "network.captive_portal_detected":
      return "network_captive_portal_detected";
    case "network.captive_portal_cleared":
      return "network_captive_portal_cleared";
    case "network.ssid_changed":
      return null;
  }
}

export function networkSignalMessage(signal: NetworkSignal): string {
  switch (signal.kind) {
    case "network.offline":
      return "Network unavailable.";
    case "network.online":
      return "Network back online.";
    case "network.route_changed":
      return "Network route changed; SSH tunnel may be stale.";
    case "network.ssid_changed":
      return "Wi-Fi network changed.";
    case "network.vpn_state_changed":
      return signal.detail?.["vpnUp"] === true
        ? "VPN connected. Re-establishing tunnel via VPN route."
        : "VPN state changed. Re-establishing tunnel route.";
    case "network.captive_portal_detected":
      return "Captive portal detected. Sign in to Wi-Fi to continue.";
    case "network.captive_portal_cleared":
      return "Captive portal cleared.";
    case "network.changed":
      return "Network changed; SSH tunnel may be stale.";
  }
}

export async function sampleMacNetworkState(input: {
  readonly runCommand?: CommandRunner;
  readonly now?: () => Date;
  readonly online?: boolean | null;
  readonly wifiInterface?: string;
} = {}): Promise<MacNetworkSnapshot> {
  const runCommand = input.runCommand ?? defaultRunCommand;
  const now = input.now ?? (() => new Date());
  const wifiInterface = input.wifiInterface ?? (await resolveWifiInterface(runCommand));
  const [route, airport, ifconfig] = await Promise.all([
    runOptional(runCommand, "route", ["-n", "get", "default"]),
    wifiInterface
      ? runOptional(runCommand, "networksetup", ["-getairportnetwork", wifiInterface])
      : Promise.resolve(null),
    runOptional(runCommand, "ifconfig", ["-l"]),
  ]);

  return {
    capturedAt: now().toISOString(),
    online: input.online ?? null,
    defaultRoute: route === null ? null : parseDefaultRoute(route),
    ssid: airport === null ? null : parseAirportNetwork(airport),
    vpnInterfaces: ifconfig === null ? [] : vpnInterfacesFromList(ifconfig),
  };
}

export function installMacNetworkMonitor(opts: NetworkMonitorOptions): NetworkMonitorHandle {
  const scheduler = opts.scheduler ?? realScheduler;
  const now = opts.now ?? (() => new Date());
  const platform = opts.platform ?? process.platform;
  const pollIntervalMs = opts.pollIntervalMs ?? 5_000;
  const debounceWindowMs = opts.debounceWindowMs ?? 1_000;
  const dispatchSignal = createDebouncedSignalEmitter({
    emit: async (signal) => {
      await opts.onSignal?.(signal);
      await Promise.resolve(opts.orchestrator?.handleNetworkSignal(signal));
    },
    scheduler,
    windowMs: debounceWindowMs,
  });

  const onOffline = () => {
    dispatchSignal({ kind: "network.offline", capturedAt: now().toISOString() });
  };
  const onOnline = () => {
    dispatchSignal({ kind: "network.online", capturedAt: now().toISOString() });
  };

  opts.net?.on?.("offline", onOffline);
  opts.net?.on?.("online", onOnline);

  let previous: MacNetworkSnapshot | null = null;
  let pollHandle: unknown = null;
  let stopped = false;

  const poll = () => {
    if (stopped) return;
    const online = readNetOnline(opts.net);
    void sampleMacNetworkState({
      ...(opts.runCommand ? { runCommand: opts.runCommand } : {}),
      now,
      online,
      ...(opts.wifiInterface ? { wifiInterface: opts.wifiInterface } : {}),
    }).then((next) => {
      if (previous !== null) {
        for (const signal of detectMacNetworkSignals(previous, next)) {
          dispatchSignal(signal);
        }
      }
      previous = next;
    }).catch((err: unknown) => {
      opts.logFailure?.(err);
    });
  };

  if (platform === "darwin") {
    poll();
    pollHandle = scheduler.setInterval(poll, pollIntervalMs);
  }

  return {
    uninstall() {
      if (stopped) return;
      stopped = true;
      detachNet(opts.net, "offline", onOffline);
      detachNet(opts.net, "online", onOnline);
      if (pollHandle !== null) scheduler.clearInterval(pollHandle);
      dispatchSignal.flush();
    },
  };
}

interface DebouncedSignalEmitter {
  (signal: NetworkSignal): void;
  flush(): void;
}

function createDebouncedSignalEmitter(input: {
  readonly emit: NetworkSignalSink;
  readonly scheduler: NetworkMonitorScheduler;
  readonly windowMs: number;
}): DebouncedSignalEmitter {
  let queue: NetworkSignal[] = [];
  let timer: unknown = null;

  const flush = () => {
    if (timer !== null) {
      input.scheduler.clearTimeout(timer);
      timer = null;
    }
    const signals = coalesceNetworkSignals(queue, input.windowMs);
    queue = [];
    for (const signal of signals) {
      void Promise.resolve(input.emit(signal));
    }
  };

  const enqueue = ((signal: NetworkSignal) => {
    queue.push(signal);
    if (timer !== null) return;
    timer = input.scheduler.setTimeout(flush, input.windowMs);
  }) as DebouncedSignalEmitter;
  enqueue.flush = flush;
  return enqueue;
}

function collapseSignalGroup(group: readonly NetworkSignal[]): NetworkSignal {
  const kinds = group.map((signal) => signal.kind);
  const latest = group[group.length - 1]!;
  const latestCaptive = [...group]
    .reverse()
    .find((signal) => signal.kind === "network.captive_portal_detected" || signal.kind === "network.captive_portal_cleared");
  if (latestCaptive) return mergeSignalDetails(latestCaptive, kinds);

  const latestConnectivity = [...group]
    .reverse()
    .find((signal) => signal.kind === "network.offline" || signal.kind === "network.online");
  const hasOfflineAndOnline =
    kinds.includes("network.offline") && kinds.includes("network.online");
  if (latestConnectivity?.kind === "network.offline") return mergeSignalDetails(latestConnectivity, kinds);

  const latestRoute = [...group].reverse().find((signal) => signal.kind === "network.route_changed");
  if (latestRoute) return mergeSignalDetails(latestRoute, kinds);

  const latestVpn = [...group].reverse().find((signal) => signal.kind === "network.vpn_state_changed");
  if (latestVpn) return mergeSignalDetails(latestVpn, kinds);

  if (hasOfflineAndOnline) {
    return {
      kind: "network.changed",
      capturedAt: latest.capturedAt,
      detail: { coalescedKinds: kinds },
    };
  }

  if (latestConnectivity) return mergeSignalDetails(latestConnectivity, kinds);

  return mergeSignalDetails(latest, kinds);
}

function mergeSignalDetails(signal: NetworkSignal, coalescedKinds: readonly string[]): NetworkSignal {
  return {
    ...signal,
    detail: {
      ...(signal.detail ?? {}),
      coalescedKinds,
    },
  };
}

function captivePortalSignalForClassification(
  classification: CaptivePortalClassification,
  now: () => Date,
): NetworkSignal | null {
  if (classification === "captive") {
    return {
      kind: "network.captive_portal_detected",
      capturedAt: now().toISOString(),
    };
  }
  if (classification === "open") {
    return {
      kind: "network.captive_portal_cleared",
      capturedAt: now().toISOString(),
    };
  }
  return null;
}

async function resolveWifiInterface(runCommand: CommandRunner): Promise<string | null> {
  const output = await runOptional(runCommand, "networksetup", ["-listallhardwareports"]);
  if (output === null) return "en0";
  return parseWifiDevice(output) ?? "en0";
}

async function runOptional(
  runCommand: CommandRunner,
  command: string,
  args: readonly string[],
): Promise<string | null> {
  try {
    return (await runCommand(command, args)).stdout;
  } catch {
    return null;
  }
}

async function defaultRunCommand(
  command: string,
  args: readonly string[],
): Promise<{ readonly stdout: string }> {
  const result = await execFileAsync(command, [...args], { timeout: 2_000 });
  return { stdout: String(result.stdout ?? "") };
}

async function defaultFetcher(
  url: string,
  init?: { readonly signal?: AbortSignal },
): Promise<CaptivePortalResponse> {
  if (!init?.signal) {
    throw new Error("Captive portal fetch requires an AbortSignal.");
  }
  const response = await fetch(url, { signal: init.signal });
  return {
    status: response.status,
    text: () => response.text(),
  };
}

function normalizeHtml(value: string): string {
  return value.replace(/\s+/gu, "").toLowerCase();
}

function readNetOnline(net: ElectronNetLike | undefined): boolean | null {
  if (!net) return null;
  if (typeof net.isOnline === "function") return net.isOnline();
  if (typeof net.online === "boolean") return net.online;
  return null;
}

function detachNet(
  net: ElectronNetLike | undefined,
  event: "online" | "offline",
  handler: () => void,
): void {
  if (!net) return;
  if (typeof net.off === "function") {
    net.off(event, handler);
    return;
  }
  if (typeof net.removeListener === "function") {
    net.removeListener(event, handler);
  }
}

const realScheduler: NetworkMonitorScheduler = {
  setTimeout(callback, delayMs) {
    return setTimeout(callback, delayMs);
  },
  clearTimeout(handle) {
    clearTimeout(handle as ReturnType<typeof setTimeout>);
  },
  setInterval(callback, intervalMs) {
    return setInterval(callback, intervalMs);
  },
  clearInterval(handle) {
    clearInterval(handle as ReturnType<typeof setInterval>);
  },
};
