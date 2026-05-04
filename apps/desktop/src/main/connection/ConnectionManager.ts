import { createHash, randomUUID } from "node:crypto";
import { constants as fsConstants, promises as fs } from "node:fs";
import { createServer, type Server, type Socket } from "node:net";
import { dirname, isAbsolute } from "node:path";
import { request as httpRequest } from "node:http";
import { Client, type ConnectConfig } from "ssh2";
import { writeFileStringAtomically } from "../../vendored/t3code/settings/index.ts";

export const TUNNEL_STATES = [
  "unconfigured",
  "ssh_probing",
  "bootstrapping",
  "tunnel_connecting",
  "authenticating",
  "ready",
  "awaiting_network",
  "captive_portal_blocked",
  "degraded",
  "reconnecting",
  "disconnected",
] as const;

export type TunnelState = (typeof TUNNEL_STATES)[number];

export type TunnelTrigger =
  | "profile.saved"
  | "connect.requested"
  | "ssh.probe.ok"
  | "tunnel.opened"
  | "auth.ok"
  | "daemon.health.failed"
  | "tunnel.closed"
  | "macos.wake"
  | "network.changed"
  | "network.online"
  | "network.offline"
  | "network.route_changed"
  | "network.vpn_state_changed"
  | "network.captive_portal_detected"
  | "network.captive_portal_cleared"
  | "bearer.expired"
  | "version.mismatch"
  | "disconnect.requested"
  | "reconnect.retry";

export interface SshProfileInput {
  readonly id?: string;
  readonly name?: string;
  readonly host: string;
  readonly port?: number;
  readonly username: string;
  readonly privateKeyPath: string;
  readonly daemonPort?: number;
  readonly localPortPreference?: number;
}

export interface SshProfile {
  readonly id: string;
  readonly name: string;
  readonly host: string;
  readonly port: number;
  readonly username: string;
  readonly privateKeyPath: string;
  readonly daemonHost: "127.0.0.1";
  readonly daemonPort: number;
  readonly localPortPreference: number | null;
  readonly createdAt: string;
  readonly updatedAt: string;
}

export interface ConnectionFault {
  readonly code: string;
  readonly message: string;
  readonly capturedAt: string;
}

export interface TunnelSnapshot {
  readonly state: TunnelState;
  readonly activeProfileId: string | null;
  readonly localPort: number | null;
  readonly lastFault: ConnectionFault | null;
  readonly reconnectAttempts: number;
  readonly nextRetryAt: string | null;
}

export interface TunnelTransition {
  readonly from: TunnelState;
  readonly to: TunnelState;
  readonly trigger: TunnelTrigger;
  readonly at: string;
  readonly profileId: string | null;
  readonly localPort: number | null;
  readonly fault: ConnectionFault | null;
  readonly reconnectAttempts: number;
}

export interface ConnectionDiagnosticTransition extends TunnelTransition {
  readonly reason: string;
}

export interface ConnectionDiagnosticsSnapshot {
  readonly capturedAt: string;
  readonly current: TunnelSnapshot;
  readonly recentTransitions: readonly ConnectionDiagnosticTransition[];
}

export interface ConnectionNetworkSignal {
  readonly kind: string;
  readonly detail?: Readonly<Record<string, unknown>>;
}

export class ConnectionManagerError extends Error {
  override readonly name = "ConnectionManagerError";
  readonly code: string;
  readonly details: Readonly<Record<string, string>>;

  constructor(code: string, message: string, details: Readonly<Record<string, string>> = {}) {
    super(message);
    this.code = code;
    this.details = details;
  }
}

interface ProfileStoreFile {
  readonly schemaVersion: 1;
  readonly profiles: readonly SshProfile[];
  readonly activeProfileId: string | null;
}

const DEFAULT_DAEMON_PORT = 3779;
const DEFAULT_SSH_PORT = 22;
const PROFILE_ID_RE = /^[A-Za-z0-9._:-]{1,128}$/;
const USERNAME_RE = /^[A-Za-z_][A-Za-z0-9_.-]{0,63}$/;
const HOST_RE = /^[A-Za-z0-9.-]{1,253}$|^\[[0-9A-Fa-f:.]+]$/;

export class SshProfileManager {
  private readonly filePath: string;
  private readonly now: () => string;

  constructor(input: { readonly filePath: string; readonly now?: () => string }) {
    this.filePath = input.filePath;
    this.now = input.now ?? (() => new Date().toISOString());
  }

  async listProfiles(): Promise<readonly SshProfile[]> {
    return (await this.readFile()).profiles;
  }

  async getActiveProfile(): Promise<SshProfile | null> {
    const file = await this.readFile();
    if (file.activeProfileId === null) return null;
    return file.profiles.find((profile) => profile.id === file.activeProfileId) ?? null;
  }

  async saveProfile(input: SshProfileInput): Promise<SshProfile> {
    const file = await this.readFile();
    const now = this.now();
    const existing = input.id
      ? file.profiles.find((profile) => profile.id === input.id)
      : undefined;
    const profile = normalizeProfile(input, existing, now);
    const profiles = upsertProfile(file.profiles, profile);
    await this.writeFile({
      schemaVersion: 1,
      profiles,
      activeProfileId: profile.id,
    });
    return profile;
  }

  async setActiveProfile(id: string | null): Promise<void> {
    const file = await this.readFile();
    if (id !== null && !file.profiles.some((profile) => profile.id === id)) {
      throw new ConnectionManagerError("profile.not-found", `Unknown SSH profile: ${id}`, { id });
    }
    await this.writeFile({ ...file, activeProfileId: id });
  }

  private async readFile(): Promise<ProfileStoreFile> {
    try {
      const raw = await fs.readFile(this.filePath, "utf8");
      const parsed = JSON.parse(raw);
      if (!isProfileStoreFile(parsed)) return emptyProfileFile();
      return parsed;
    } catch (err) {
      if (isErrnoCode(err, "ENOENT")) return emptyProfileFile();
      if (err instanceof SyntaxError) return emptyProfileFile();
      throw err;
    }
  }

  private async writeFile(file: ProfileStoreFile): Promise<void> {
    await fs.mkdir(dirname(this.filePath), { recursive: true });
    writeFileStringAtomically({
      filePath: this.filePath,
      contents: `${JSON.stringify(file, null, 2)}\n`,
    });
  }
}

function emptyProfileFile(): ProfileStoreFile {
  return { schemaVersion: 1, profiles: [], activeProfileId: null };
}

function normalizeProfile(input: SshProfileInput, existing: SshProfile | undefined, now: string): SshProfile {
  const id = input.id?.trim() || existing?.id || `vps-${randomUUID()}`;
  if (!PROFILE_ID_RE.test(id)) {
    throw new ConnectionManagerError("profile.id-invalid", "Profile id contains unsupported characters.", { id });
  }
  const host = input.host.trim();
  if (!HOST_RE.test(host) || host.includes("..") || host.startsWith("-")) {
    throw new ConnectionManagerError("profile.host-invalid", "Host must be a hostname, IPv4, or bracketed IPv6 literal.", { host });
  }
  const username = input.username.trim();
  if (!USERNAME_RE.test(username)) {
    throw new ConnectionManagerError("profile.username-invalid", "Username must be a POSIX-style login name.", { username });
  }
  const privateKeyPath = input.privateKeyPath.trim();
  if (!isAbsolute(privateKeyPath)) {
    throw new ConnectionManagerError("profile.private-key-not-absolute", "Private key path must be absolute.", {
      privateKeyPath,
    });
  }
  const port = normalizePort(input.port ?? existing?.port ?? DEFAULT_SSH_PORT, "profile.port-invalid");
  const daemonPort = normalizePort(
    input.daemonPort ?? existing?.daemonPort ?? DEFAULT_DAEMON_PORT,
    "profile.daemon-port-invalid",
  );
  const preferred = input.localPortPreference ?? existing?.localPortPreference ?? null;
  const localPortPreference =
    preferred === null ? null : normalizePort(preferred, "profile.local-port-invalid");
  const name = (input.name ?? existing?.name ?? `${username}@${host}`).trim();
  return {
    id,
    name,
    host,
    port,
    username,
    privateKeyPath,
    daemonHost: "127.0.0.1",
    daemonPort,
    localPortPreference,
    createdAt: existing?.createdAt ?? now,
    updatedAt: now,
  };
}

function normalizePort(value: number, code: string): number {
  if (!Number.isInteger(value) || value < 1 || value > 65_535) {
    throw new ConnectionManagerError(code, "Port must be an integer in [1, 65535].", {
      port: String(value),
    });
  }
  return value;
}

function upsertProfile(profiles: readonly SshProfile[], profile: SshProfile): readonly SshProfile[] {
  const next = profiles.filter((candidate) => candidate.id !== profile.id);
  return [...next, profile].toSorted((a, b) => a.name.localeCompare(b.name));
}

function isProfileStoreFile(value: unknown): value is ProfileStoreFile {
  if (typeof value !== "object" || value === null) return false;
  const candidate = value as { readonly schemaVersion?: unknown; readonly profiles?: unknown };
  return candidate.schemaVersion === 1 && Array.isArray(candidate.profiles);
}

interface KnownHostFile {
  readonly schemaVersion: 1;
  readonly hosts: Readonly<Record<string, string>>;
}

export type HostVerificationResult =
  | { readonly ok: true; readonly trustedFirstUse: boolean; readonly fingerprint: string }
  | { readonly ok: false; readonly expected: string; readonly actual: string };

export class KnownHostStore {
  private readonly filePath: string;

  constructor(input: { readonly filePath: string }) {
    this.filePath = input.filePath;
  }

  async verifyKey(profile: SshProfile, key: Buffer): Promise<HostVerificationResult> {
    return await this.verifyFingerprint(profile, fingerprintHostKey(key));
  }

  async verifyFingerprint(profile: SshProfile, fingerprint: string): Promise<HostVerificationResult> {
    const key = knownHostKey(profile);
    const file = await this.readFile();
    const expected = file.hosts[key];
    if (expected === undefined) {
      await this.writeFile({
        schemaVersion: 1,
        hosts: { ...file.hosts, [key]: fingerprint },
      });
      return { ok: true, trustedFirstUse: true, fingerprint };
    }
    if (expected !== fingerprint) {
      return { ok: false, expected, actual: fingerprint };
    }
    return { ok: true, trustedFirstUse: false, fingerprint };
  }

  private async readFile(): Promise<KnownHostFile> {
    try {
      const raw = await fs.readFile(this.filePath, "utf8");
      const parsed = JSON.parse(raw);
      if (!isKnownHostFile(parsed)) return emptyKnownHostFile();
      return parsed;
    } catch (err) {
      if (isErrnoCode(err, "ENOENT")) return emptyKnownHostFile();
      if (err instanceof SyntaxError) return emptyKnownHostFile();
      throw err;
    }
  }

  private async writeFile(file: KnownHostFile): Promise<void> {
    await fs.mkdir(dirname(this.filePath), { recursive: true });
    writeFileStringAtomically({
      filePath: this.filePath,
      contents: `${JSON.stringify(file, null, 2)}\n`,
    });
  }
}

function emptyKnownHostFile(): KnownHostFile {
  return { schemaVersion: 1, hosts: {} };
}

function isKnownHostFile(value: unknown): value is KnownHostFile {
  if (typeof value !== "object" || value === null) return false;
  const candidate = value as { readonly schemaVersion?: unknown; readonly hosts?: unknown };
  return candidate.schemaVersion === 1 && typeof candidate.hosts === "object" && candidate.hosts !== null;
}

function knownHostKey(profile: SshProfile): string {
  return `${profile.host}:${profile.port}`;
}

export function fingerprintHostKey(key: Buffer): string {
  return `SHA256:${createHash("sha256").update(key).digest("base64").replace(/=+$/u, "")}`;
}

export interface TunnelHandle {
  readonly profileId: string;
  readonly localPort: number;
  readonly close: () => Promise<void>;
}

export interface TunnelDriver {
  readonly open: (profile: SshProfile) => Promise<TunnelHandle>;
  readonly checkHealth: (handle: TunnelHandle) => Promise<boolean>;
}

export interface Ssh2TunnelDriverOptions {
  readonly knownHosts: KnownHostStore;
  readonly passphraseForProfile?: (profile: SshProfile) => Promise<string | null>;
  readonly heartbeatPath?: string;
  readonly heartbeatTimeoutMs?: number;
}

export class Ssh2TunnelDriver implements TunnelDriver {
  private readonly knownHosts: KnownHostStore;
  private readonly passphraseForProfile: (profile: SshProfile) => Promise<string | null>;
  private readonly heartbeatPath: string;
  private readonly heartbeatTimeoutMs: number;

  constructor(options: Ssh2TunnelDriverOptions) {
    this.knownHosts = options.knownHosts;
    this.passphraseForProfile = options.passphraseForProfile ?? (async () => null);
    this.heartbeatPath = options.heartbeatPath ?? "/health";
    this.heartbeatTimeoutMs = options.heartbeatTimeoutMs ?? 2_000;
  }

  async open(profile: SshProfile): Promise<TunnelHandle> {
    const privateKey = await fs.readFile(profile.privateKeyPath);
    const passphrase = await this.passphraseForProfile(profile);
    const client = new Client();
    const config: ConnectConfig = {
      host: profile.host,
      port: profile.port,
      username: profile.username,
      privateKey,
      readyTimeout: 15_000,
      keepaliveInterval: 10_000,
      keepaliveCountMax: 3,
      hostVerifier: (key: Buffer, verify: (accepted: boolean) => void) => {
        void this.knownHosts
          .verifyKey(profile, key)
          .then((result) => verify(result.ok))
          .catch(() => verify(false));
      },
    };
    if (passphrase !== null) {
      config.passphrase = passphrase;
    }
    const localServer = await new Promise<Server>((resolvePromise, rejectPromise) => {
      const onReady = () => {
        const server = createServer((socket) => {
          forwardSocket(client, profile, socket);
        });
        server.once("error", rejectPromise);
        server.listen(
          { host: "127.0.0.1", port: profile.localPortPreference ?? 0 },
          () => {
            server.off("error", rejectPromise);
            resolvePromise(server);
          },
        );
      };
      client.once("ready", onReady);
      client.once("error", rejectPromise);
      client.connect(config);
    });
    const address = localServer.address();
    const localPort = typeof address === "object" && address !== null ? address.port : null;
    if (localPort === null) {
      throw new ConnectionManagerError("tunnel.local-port-unavailable", "SSH tunnel did not expose a TCP local port.");
    }
    return {
      profileId: profile.id,
      localPort,
      close: async () => {
        await closeServer(localServer);
        client.end();
      },
    };
  }

  async checkHealth(handle: TunnelHandle): Promise<boolean> {
    return await new Promise<boolean>((resolvePromise) => {
      const req = httpRequest(
        {
          host: "127.0.0.1",
          port: handle.localPort,
          path: this.heartbeatPath,
          method: "GET",
          timeout: this.heartbeatTimeoutMs,
        },
        (res) => {
          res.resume();
          resolvePromise((res.statusCode ?? 0) >= 200 && (res.statusCode ?? 0) < 300);
        },
      );
      req.once("timeout", () => {
        req.destroy();
        resolvePromise(false);
      });
      req.once("error", () => resolvePromise(false));
      req.end();
    });
  }
}

function forwardSocket(client: Client, profile: SshProfile, socket: Socket): void {
  client.forwardOut("127.0.0.1", 0, profile.daemonHost, profile.daemonPort, (err, stream) => {
    if (err) {
      socket.destroy(err);
      return;
    }
    socket.pipe(stream).pipe(socket);
  });
}

async function closeServer(server: Server): Promise<void> {
  await new Promise<void>((resolvePromise, rejectPromise) => {
    server.close((err) => {
      if (err) rejectPromise(err);
      else resolvePromise();
    });
  });
}

export interface ConnectionManagerOptions {
  readonly driver: TunnelDriver;
  readonly now?: () => Date;
  readonly jitter?: () => number;
  readonly maxTransitionHistory?: number;
}

export class ConnectionManager {
  private readonly driver: TunnelDriver;
  private readonly now: () => Date;
  private readonly jitter: () => number;
  private readonly maxTransitionHistory: number;
  private currentState: TunnelState = "unconfigured";
  private activeProfile: SshProfile | null = null;
  private handle: TunnelHandle | null = null;
  private fault: ConnectionFault | null = null;
  private attempts = 0;
  private nextRetry: Date | null = null;
  private readonly transitions: TunnelTransition[] = [];

  constructor(options: ConnectionManagerOptions) {
    this.driver = options.driver;
    this.now = options.now ?? (() => new Date());
    this.jitter = options.jitter ?? Math.random;
    this.maxTransitionHistory = options.maxTransitionHistory ?? 50;
  }

  snapshot(): TunnelSnapshot {
    return {
      state: this.currentState,
      activeProfileId: this.activeProfile?.id ?? null,
      localPort: this.handle?.localPort ?? null,
      lastFault: this.fault,
      reconnectAttempts: this.attempts,
      nextRetryAt: this.nextRetry?.toISOString() ?? null,
    };
  }

  transitionHistory(): readonly TunnelTransition[] {
    return this.transitions.slice();
  }

  diagnosticsSnapshot(limit = 20): ConnectionDiagnosticsSnapshot {
    const safeLimit = Number.isInteger(limit) && limit > 0 ? limit : 20;
    const recentTransitions = this.transitions
      .slice(-safeLimit)
      .map((entry): ConnectionDiagnosticTransition => ({
        ...entry,
        reason: entry.fault?.message ?? diagnosticReasonForTrigger(entry.trigger),
      }));
    return {
      capturedAt: this.now().toISOString(),
      current: this.snapshot(),
      recentTransitions,
    };
  }

  async connect(profile: SshProfile): Promise<TunnelSnapshot> {
    if (this.handle !== null) {
      await this.handle.close();
      this.handle = null;
    }
    this.activeProfile = profile;
    this.fault = null;
    this.nextRetry = null;
    this.attempts = 0;
    this.transition("ssh_probing", "connect.requested");
    await this.probePrivateKey(profile);
    this.transition("tunnel_connecting", "ssh.probe.ok");
    try {
      this.handle = await this.driver.open(profile);
      this.transition("authenticating", "tunnel.opened");
      this.transition("ready", "auth.ok");
      return this.snapshot();
    } catch (err) {
      this.markReconnect("tunnel.open-failed", (err as Error).message, "tunnel.closed");
      throw err;
    }
  }

  async retryNow(): Promise<TunnelSnapshot> {
    if (this.activeProfile === null) {
      throw new ConnectionManagerError("profile.none-active", "Cannot reconnect without an active SSH profile.");
    }
    if (this.handle !== null) {
      await this.handle.close();
      this.handle = null;
    }
    this.transition("tunnel_connecting", "reconnect.retry");
    this.handle = await this.driver.open(this.activeProfile);
    this.fault = null;
    this.nextRetry = null;
    this.attempts = 0;
    this.transition("authenticating", "tunnel.opened");
    this.transition("ready", "auth.ok");
    return this.snapshot();
  }

  async checkHealth(): Promise<boolean> {
    if (this.handle === null) return false;
    const ok = await this.driver.checkHealth(this.handle);
    if (!ok) {
      this.markReconnect("daemon.health.failed", "Daemon health check failed.", "daemon.health.failed");
    }
    return ok;
  }

  handleTunnelClosed(message = "SSH tunnel closed."): TunnelSnapshot {
    this.markReconnect("tunnel.closed", message, "tunnel.closed");
    return this.snapshot();
  }

  handleWake(): TunnelSnapshot {
    this.markReconnect("macos.wake", "macOS wake requires tunnel revalidation.", "macos.wake");
    return this.snapshot();
  }

  handleNetworkChange(): TunnelSnapshot {
    this.markReconnect("network.changed", "Network changed; SSH tunnel may be stale.", "network.changed");
    return this.snapshot();
  }

  handleNetworkSignal(signal: ConnectionNetworkSignal): TunnelSnapshot {
    switch (signal.kind) {
      case "network.offline":
        return this.handleNetworkOffline();
      case "network.online":
        return this.handleNetworkOnline();
      case "network.route_changed":
        return this.handleRouteChange();
      case "network.vpn_state_changed":
        return this.handleVpnStateChange(
          signal.detail?.["vpnUp"] === true
            ? "VPN connected. Re-establishing tunnel via VPN route."
            : "VPN state changed. Re-establishing tunnel route.",
        );
      case "network.captive_portal_detected":
        return this.handleCaptivePortalDetected();
      case "network.captive_portal_cleared":
        return this.handleCaptivePortalCleared();
      case "network.changed":
        return this.handleNetworkChange();
      default:
        return this.snapshot();
    }
  }

  handleNetworkOffline(message = "Network unavailable."): TunnelSnapshot {
    this.markPausedNetworkState("awaiting_network", "network.offline", message, "network.offline");
    return this.snapshot();
  }

  handleNetworkOnline(message = "Network back online."): TunnelSnapshot {
    this.markImmediateReconnect("network.online", message, "network.online");
    return this.snapshot();
  }

  handleRouteChange(message = "Network route changed; SSH tunnel may be stale."): TunnelSnapshot {
    this.markImmediateReconnect("network.route_changed", message, "network.route_changed");
    return this.snapshot();
  }

  handleVpnStateChange(message = "VPN state changed. Re-establishing tunnel route."): TunnelSnapshot {
    this.markImmediateReconnect("network.vpn_state_changed", message, "network.vpn_state_changed");
    return this.snapshot();
  }

  handleCaptivePortalDetected(message = "Captive portal detected. Sign in to Wi-Fi to continue."): TunnelSnapshot {
    this.markPausedNetworkState(
      "captive_portal_blocked",
      "network.captive_portal_detected",
      message,
      "network.captive_portal_detected",
    );
    return this.snapshot();
  }

  handleCaptivePortalCleared(message = "Captive portal cleared."): TunnelSnapshot {
    this.markImmediateReconnect("network.captive_portal_cleared", message, "network.captive_portal_cleared");
    return this.snapshot();
  }

  handleBearerExpired(): TunnelSnapshot {
    this.markReconnect("bearer.expired", "Bearer expired; reconnect must refresh session credentials.", "bearer.expired");
    return this.snapshot();
  }

  markVersionMismatch(message: string): TunnelSnapshot {
    this.fault = {
      code: "version.mismatch",
      message,
      capturedAt: this.now().toISOString(),
    };
    this.transition("degraded", "version.mismatch", this.fault);
    return this.snapshot();
  }

  async disconnect(message = "User disconnected."): Promise<TunnelSnapshot> {
    if (this.handle !== null) {
      await this.handle.close();
    }
    this.handle = null;
    this.nextRetry = null;
    this.fault = { code: "disconnect.requested", message, capturedAt: this.now().toISOString() };
    this.transition("disconnected", "disconnect.requested", this.fault);
    return this.snapshot();
  }

  private async probePrivateKey(profile: SshProfile): Promise<void> {
    try {
      await fs.access(profile.privateKeyPath, fsConstants.R_OK);
    } catch (err) {
      throw new ConnectionManagerError(
        "profile.private-key-unreadable",
        `Private key is not readable: ${(err as Error).message}`,
        { privateKeyPath: profile.privateKeyPath },
      );
    }
  }

  private markReconnect(code: string, message: string, trigger: TunnelTrigger): void {
    const staleHandle = this.handle;
    this.handle = null;
    if (staleHandle !== null) {
      void staleHandle.close().catch(() => undefined);
    }
    this.attempts += 1;
    this.fault = { code, message, capturedAt: this.now().toISOString() };
    this.nextRetry = new Date(this.now().getTime() + retryDelayMs(this.attempts, this.jitter()));
    this.transition("reconnecting", trigger, this.fault);
  }

  private markImmediateReconnect(code: string, message: string, trigger: TunnelTrigger): void {
    const staleHandle = this.handle;
    this.handle = null;
    if (staleHandle !== null) {
      void staleHandle.close().catch(() => undefined);
    }
    this.attempts += 1;
    this.fault = { code, message, capturedAt: this.now().toISOString() };
    this.nextRetry = this.now();
    this.transition("reconnecting", trigger, this.fault);
  }

  private markPausedNetworkState(
    state: "awaiting_network" | "captive_portal_blocked",
    code: string,
    message: string,
    trigger: TunnelTrigger,
  ): void {
    const staleHandle = this.handle;
    this.handle = null;
    if (staleHandle !== null) {
      void staleHandle.close().catch(() => undefined);
    }
    this.nextRetry = null;
    this.fault = { code, message, capturedAt: this.now().toISOString() };
    this.transition(state, trigger, this.fault);
  }

  private transition(to: TunnelState, trigger: TunnelTrigger, fault: ConnectionFault | null = this.fault): void {
    const from = this.currentState;
    this.currentState = to;
    const entry: TunnelTransition = {
      from,
      to,
      trigger,
      at: this.now().toISOString(),
      profileId: this.activeProfile?.id ?? null,
      localPort: this.handle?.localPort ?? null,
      fault,
      reconnectAttempts: this.attempts,
    };
    this.transitions.push(entry);
    if (this.transitions.length > this.maxTransitionHistory) {
      this.transitions.splice(0, this.transitions.length - this.maxTransitionHistory);
    }
  }
}

export function retryDelayMs(attempt: number, jitterUnit: number): number {
  const safeAttempt = Math.max(1, attempt);
  const base = Math.min(30_000, 1_000 * 2 ** (safeAttempt - 1));
  const boundedJitter = Math.min(1, Math.max(0, jitterUnit));
  const jitterMultiplier = 0.75 + boundedJitter * 0.5;
  return Math.min(30_000, Math.round(base * jitterMultiplier));
}

function diagnosticReasonForTrigger(trigger: TunnelTrigger): string {
  switch (trigger) {
    case "profile.saved":
      return "SSH profile was saved.";
    case "connect.requested":
      return "User or wizard requested a connection.";
    case "ssh.probe.ok":
      return "SSH profile passed local preflight checks.";
    case "tunnel.opened":
      return "SSH tunnel opened a local daemon port.";
    case "auth.ok":
      return "Daemon bearer and websocket token were accepted.";
    case "daemon.health.failed":
      return "Daemon health check failed.";
    case "tunnel.closed":
      return "SSH tunnel closed.";
    case "macos.wake":
      return "macOS woke from sleep; tunnel must be revalidated.";
    case "network.changed":
      return "Network changed; tunnel may be stale.";
    case "network.online":
      return "Network came back online.";
    case "network.offline":
      return "Network went offline; reconnect paused.";
    case "network.route_changed":
      return "Default route changed; tunnel may be stale.";
    case "network.vpn_state_changed":
      return "VPN state changed; tunnel route must be revalidated.";
    case "network.captive_portal_detected":
      return "Captive portal detected; reconnect paused.";
    case "network.captive_portal_cleared":
      return "Captive portal cleared; reconnect may resume.";
    case "bearer.expired":
      return "Bearer expired and must be refreshed.";
    case "version.mismatch":
      return "Daemon API version is incompatible.";
    case "disconnect.requested":
      return "User requested disconnect.";
    case "reconnect.retry":
      return "Backoff elapsed and reconnect retry started.";
  }
}

function isErrnoCode(err: unknown, code: string): boolean {
  return (
    typeof err === "object" &&
    err !== null &&
    "code" in err &&
    (err as { readonly code: string }).code === code
  );
}
