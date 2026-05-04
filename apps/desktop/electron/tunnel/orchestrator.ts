import { dispatch, type DispatchContext } from "./fsm.ts";
import {
  INITIAL_TUNNEL_SNAPSHOT,
  type TunnelSnapshot,
  type VpsProfile,
} from "./types.ts";

export interface OpenTunnel {
  readonly localPort: number;
  onClose(handler: (reason?: Error) => void): void;
  close(): Promise<void>;
}

export interface TunnelDriver {
  probe(profile: VpsProfile): Promise<void>;
  bootstrap(profile: VpsProfile): Promise<void>;
  open(profile: VpsProfile): Promise<OpenTunnel>;
}

export interface AuthDriver {
  authenticate(input: { readonly profile: VpsProfile; readonly localPort: number }): Promise<void>;
}

export type HeartbeatStatus = "ok" | "version_mismatch";

export interface HeartbeatDriver {
  check(input: { readonly profile: VpsProfile; readonly localPort: number }): Promise<HeartbeatStatus>;
}

export interface ScheduledHandle {
  cancel(): void;
}

export interface Scheduler {
  schedule(delayMs: number, callback: () => void): ScheduledHandle;
}

export interface TunnelOrchestratorOptions {
  readonly tunnel: TunnelDriver;
  readonly auth: AuthDriver;
  readonly heartbeat: HeartbeatDriver;
  readonly scheduler?: Scheduler;
  readonly now?: () => Date;
  readonly onSnapshot?: (snapshot: TunnelSnapshot) => void;
}

export class TunnelOrchestrator {
  #snapshot: TunnelSnapshot = INITIAL_TUNNEL_SNAPSHOT;
  #profile: VpsProfile | null = null;
  #currentTunnel: OpenTunnel | null = null;
  #reconnectTimer: ScheduledHandle | null = null;
  #connectGeneration = 0;
  readonly #opts: TunnelOrchestratorOptions;

  constructor(opts: TunnelOrchestratorOptions) {
    this.#opts = opts;
  }

  get snapshot(): TunnelSnapshot {
    return this.#snapshot;
  }

  async connect(profile: VpsProfile): Promise<TunnelSnapshot> {
    this.#profile = profile;
    this.#cancelReconnect();
    this.#connectGeneration += 1;
    const generation = this.#connectGeneration;
    this.#transition("profile_set", { profileId: profile.id });
    await this.#runConnectPipeline(profile, generation);
    return this.#snapshot;
  }

  async disconnect(): Promise<TunnelSnapshot> {
    this.#connectGeneration += 1;
    this.#cancelReconnect();
    await this.#closeCurrentTunnel();
    this.#transition("user_disconnect");
    return this.#snapshot;
  }

  async handleSystemSleep(): Promise<TunnelSnapshot> {
    this.#connectGeneration += 1;
    this.#cancelReconnect();
    await this.#closeCurrentTunnel();
    this.#transition("system_sleep");
    return this.#snapshot;
  }

  async handleSystemWake(): Promise<TunnelSnapshot> {
    if (!this.#profile) {
      return this.#snapshot;
    }
    this.#transition("system_wake");
    return this.connect(this.#profile);
  }

  handleTunnelClosed(reason?: Error): TunnelSnapshot {
    void this.#closeCurrentTunnel();
    this.#transition("tunnel_closed", reason ? { faultMessage: reason.message } : {});
    this.#scheduleReconnect();
    return this.#snapshot;
  }

  handleHeartbeatTimeout(message = "Heartbeat timed out"): TunnelSnapshot {
    void this.#closeCurrentTunnel();
    this.#transition("heartbeat_timeout", { faultMessage: message });
    this.#scheduleReconnect();
    return this.#snapshot;
  }

  async handleBearerExpired(): Promise<TunnelSnapshot> {
    if (!this.#profile || this.#currentTunnel === null) {
      return this.#snapshot;
    }
    this.#transition("bearer_expired");
    try {
      await this.#opts.auth.authenticate({
        profile: this.#profile,
        localPort: this.#currentTunnel.localPort,
      });
      this.#transition("auth_succeeded", { localPort: this.#currentTunnel.localPort });
    } catch (err) {
      this.#transition("auth_failed", { faultMessage: errorMessage(err) });
    }
    return this.#snapshot;
  }

  async #runConnectPipeline(profile: VpsProfile, generation: number): Promise<void> {
    try {
      await this.#opts.tunnel.probe(profile);
      if (!this.#isCurrent(generation)) return;
      this.#transition("ssh_probe_succeeded");

      await this.#opts.tunnel.bootstrap(profile);
      if (!this.#isCurrent(generation)) return;
      this.#transition("bootstrap_succeeded");

      const opened = await this.#opts.tunnel.open(profile);
      if (!this.#isCurrent(generation)) {
        await opened.close();
        return;
      }
      this.#currentTunnel = opened;
      opened.onClose((reason) => {
        if (this.#currentTunnel === opened) {
          this.handleTunnelClosed(reason);
        }
      });
      this.#transition("tunnel_opened", { localPort: opened.localPort });

      await this.#opts.auth.authenticate({ profile, localPort: opened.localPort });
      if (!this.#isCurrent(generation)) return;
      this.#transition("auth_succeeded", { localPort: opened.localPort });

      const heartbeat = await this.#opts.heartbeat.check({ profile, localPort: opened.localPort });
      if (!this.#isCurrent(generation)) return;
      if (heartbeat === "version_mismatch") {
        this.#transition("version_mismatch", { faultMessage: "Daemon API version mismatch" });
      } else {
        this.#transition("heartbeat_ok");
      }
    } catch (err) {
      if (!this.#isCurrent(generation)) return;
      this.#classifyPipelineError(err);
      this.#scheduleReconnect();
    }
  }

  #classifyPipelineError(err: unknown): void {
    const message = errorMessage(err);
    switch (this.#snapshot.state) {
      case "ssh_probing":
        this.#transition("ssh_probe_failed", { faultMessage: message });
        break;
      case "bootstrapping":
        this.#transition("bootstrap_failed", { faultMessage: message });
        break;
      case "tunnel_connecting":
        this.#transition("tunnel_closed", { faultMessage: message });
        break;
      case "authenticating":
        this.#transition("auth_failed", { faultMessage: message });
        break;
      default:
        this.#transition("heartbeat_timeout", { faultMessage: message });
        break;
    }
  }

  #transition(event: Parameters<typeof dispatch>[1], ctx: DispatchContext = {}): void {
    const nextCtx: DispatchContext = this.#opts.now ? { ...ctx, now: this.#opts.now } : ctx;
    this.#snapshot = dispatch(this.#snapshot, event, nextCtx).snapshot;
    this.#opts.onSnapshot?.(this.#snapshot);
  }

  #scheduleReconnect(): void {
    if (this.#snapshot.state !== "reconnecting" || !this.#snapshot.nextRetryAt || !this.#profile) {
      return;
    }
    this.#cancelReconnect();
    const target = Date.parse(this.#snapshot.nextRetryAt);
    const now = this.#opts.now?.().getTime() ?? Date.now();
    const delayMs = Math.max(0, target - now);
    const profile = this.#profile;
    this.#reconnectTimer = (this.#opts.scheduler ?? realScheduler).schedule(delayMs, () => {
      this.#transition("backoff_elapsed");
      void this.connect(profile);
    });
  }

  #cancelReconnect(): void {
    this.#reconnectTimer?.cancel();
    this.#reconnectTimer = null;
  }

  async #closeCurrentTunnel(): Promise<void> {
    const tunnel = this.#currentTunnel;
    this.#currentTunnel = null;
    if (tunnel) {
      await tunnel.close();
    }
  }

  #isCurrent(generation: number): boolean {
    return generation === this.#connectGeneration;
  }
}

const realScheduler: Scheduler = {
  schedule(delayMs, callback) {
    const handle = setTimeout(callback, delayMs);
    return { cancel: () => clearTimeout(handle) };
  },
};

function errorMessage(err: unknown): string {
  return err instanceof Error ? err.message : String(err);
}
