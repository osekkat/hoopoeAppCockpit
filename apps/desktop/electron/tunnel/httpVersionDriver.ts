// hp-fkov — HttpVersionDriver: probes /v1/version + reports compatibility.
//
// Engine-first slice 4 of hp-fkov. The orchestrator's HeartbeatDriver
// returns `"version_mismatch"` when the daemon's wire schema doesn't
// match what the desktop was built against. The HttpHeartbeatDriver
// (slice 2) deliberately leaves that detection to a separate probe so
// /v1/health stays cheap (no version lookup on every tick); this file
// is that probe.
//
// Production wiring composes the two: a `CompositeHeartbeatDriver`
// (future slice or BackendLifecycle glue) calls /v1/health as the
// frequent tick + /v1/version once on connect (or at a coarser
// cadence post-ready). The version probe is intentionally NOT
// scheduled on every tick — schema drift is rare and the daemon
// process being alive (the heartbeat answer) is the question that
// drives reconnect decisions.

import type { VpsProfile } from "./types.ts";
import type { FetchLike, FetchResponse } from "./httpHeartbeatDriver.ts";
import type { HeartbeatDriver, HeartbeatStatus } from "./orchestrator.ts";

export class HttpVersionError extends Error {
  override readonly name = "HttpVersionError";
  readonly code: HttpVersionErrorCode;
  readonly status: number | null;
  constructor(code: HttpVersionErrorCode, message: string, status: number | null = null) {
    super(message);
    this.code = code;
    this.status = status;
  }
}

export type HttpVersionErrorCode =
  | "network"
  | "http_status"
  | "malformed_response"
  | "timeout";

/** Compatibility verdict.
 *
 * `compatible`: the daemon's `schemaVersion` is within the desktop's
 *   accepted range — heartbeat returns "ok".
 * `version_mismatch`: the daemon reports a schemaVersion outside the
 *   range — heartbeat caller should route to the orchestrator's
 *   `version_mismatch` transition (downgrade UI to a "daemon is on
 *   schema vN; desktop expects vM" state until the user upgrades).
 */
export type VersionCompatibility = "compatible" | "version_mismatch";

/** Subset of OpenAPI VersionResponse the driver consumes. */
interface VersionResponse {
  readonly schemaVersion: number;
  readonly daemon?: {
    readonly version?: string;
    readonly commit?: string;
    readonly channel?: string;
  };
}

export interface HttpVersionDriverOptions {
  /** Schema versions the desktop accepts. Must include at least one
   *  entry. The driver returns "compatible" iff the daemon's reported
   *  schemaVersion is in this set. Wildcard support is intentionally
   *  absent — every accepted schema bump should be deliberate. */
  readonly acceptedSchemaVersions: ReadonlyArray<number>;
  /** WHATWG fetch (or stub in tests). Defaults to globalThis.fetch. */
  readonly fetch?: FetchLike;
  /** Timeout budget. Defaults to 5s — same as the heartbeat driver
   *  since the cost profile is identical (both are GET /v1/<path>
   *  over the local-port loopback). */
  readonly timeoutMs?: number;
  /** Bearer resolver, called per-probe. /v1/version is `security: []`
   *  per the OpenAPI spec, so an unauth probe is fine — but if the
   *  daemon's deployment configures auth, the bearer flows through
   *  the same way as the heartbeat probe. */
  readonly bearer?: () => string | null;
  /** URL builder override (tests). Default builds
   *  `http://127.0.0.1:<localPort>/v1/version`. */
  readonly urlFor?: (input: { readonly profile: VpsProfile; readonly localPort: number }) => string;
}

const DEFAULT_TIMEOUT_MS = 5_000;

export interface HttpVersionProbeResult {
  readonly compatibility: VersionCompatibility;
  readonly reportedSchemaVersion: number;
  readonly daemonVersion?: string;
  readonly commit?: string;
  readonly channel?: string;
}

export interface VersionProbeDriver {
  check(input: { readonly profile: VpsProfile; readonly localPort: number }): Promise<HttpVersionProbeResult>;
}

export interface VersionedHeartbeatDriverOptions {
  readonly health: HeartbeatDriver;
  readonly version: VersionProbeDriver;
}

/** HeartbeatDriver wrapper used by the connect pipeline.
 *
 * The cheap /v1/health probe proves the tunnel and daemon process are
 * reachable. The /v1/version probe then maps schema drift into the FSM's
 * non-blocking `version_mismatch` transition instead of treating it like a
 * dropped tunnel.
 */
export class VersionedHeartbeatDriver implements HeartbeatDriver {
  readonly #health: HeartbeatDriver;
  readonly #version: VersionProbeDriver;

  constructor(options: VersionedHeartbeatDriverOptions) {
    this.#health = options.health;
    this.#version = options.version;
  }

  async check(input: { readonly profile: VpsProfile; readonly localPort: number }): Promise<HeartbeatStatus> {
    const health = await this.#health.check(input);
    if (health === "version_mismatch") {
      return health;
    }
    const version = await this.#version.check(input);
    return version.compatibility === "compatible" ? "ok" : "version_mismatch";
  }
}

export class HttpVersionDriver {
  readonly #fetch: FetchLike;
  readonly #timeoutMs: number;
  readonly #bearer: () => string | null;
  readonly #urlFor: (input: { readonly profile: VpsProfile; readonly localPort: number }) => string;
  readonly #accepted: ReadonlySet<number>;

  constructor(options: HttpVersionDriverOptions) {
    if (!options.acceptedSchemaVersions || options.acceptedSchemaVersions.length === 0) {
      throw new Error("HttpVersionDriver: acceptedSchemaVersions must be non-empty");
    }
    const f = options.fetch ?? (globalThis.fetch as unknown as FetchLike | undefined);
    if (!f) {
      throw new Error("HttpVersionDriver: no fetch implementation available (Node 18+ required)");
    }
    this.#fetch = f;
    this.#timeoutMs = options.timeoutMs ?? DEFAULT_TIMEOUT_MS;
    this.#bearer = options.bearer ?? (() => null);
    this.#urlFor = options.urlFor ?? defaultUrlFor;
    this.#accepted = new Set(options.acceptedSchemaVersions);
  }

  async check(input: { readonly profile: VpsProfile; readonly localPort: number }): Promise<HttpVersionProbeResult> {
    const url = this.#urlFor(input);
    const headers: Record<string, string> = { Accept: "application/json" };
    const bearer = this.#bearer();
    if (bearer) {
      headers["Authorization"] = `Bearer ${bearer}`;
    }

    const controller = new AbortController();
    const timer = setTimeout(() => controller.abort(), this.#timeoutMs);

    let response: FetchResponse;
    try {
      response = await this.#fetch(url, { signal: controller.signal, headers });
    } catch (err) {
      if (controller.signal.aborted) {
        throw new HttpVersionError("timeout", `version probe timed out after ${this.#timeoutMs}ms`);
      }
      throw new HttpVersionError("network", errorMessage(err));
    } finally {
      clearTimeout(timer);
    }

    if (!response.ok) {
      throw new HttpVersionError(
        "http_status",
        `version probe returned HTTP ${response.status}`,
        response.status,
      );
    }

    let body: string;
    try {
      body = await response.text();
    } catch (err) {
      throw new HttpVersionError("malformed_response", `cannot read body: ${errorMessage(err)}`);
    }

    let parsed: unknown;
    try {
      parsed = JSON.parse(body);
    } catch (err) {
      throw new HttpVersionError("malformed_response", `body is not JSON: ${errorMessage(err)}`);
    }

    if (!isVersionResponse(parsed)) {
      throw new HttpVersionError(
        "malformed_response",
        "body shape does not match VersionResponse",
      );
    }

    const reportedSchemaVersion = parsed.schemaVersion;
    const compatibility: VersionCompatibility = this.#accepted.has(reportedSchemaVersion)
      ? "compatible"
      : "version_mismatch";

    const result: HttpVersionProbeResult = {
      compatibility,
      reportedSchemaVersion,
      ...(parsed.daemon?.version ? { daemonVersion: parsed.daemon.version } : {}),
      ...(parsed.daemon?.commit ? { commit: parsed.daemon.commit } : {}),
      ...(parsed.daemon?.channel ? { channel: parsed.daemon.channel } : {}),
    };
    return result;
  }
}

function defaultUrlFor(input: { readonly profile: VpsProfile; readonly localPort: number }): string {
  return `http://127.0.0.1:${input.localPort}/v1/version`;
}

function isVersionResponse(value: unknown): value is VersionResponse {
  if (typeof value !== "object" || value === null) return false;
  const obj = value as Record<string, unknown>;
  if (typeof obj["schemaVersion"] !== "number") return false;
  if (!Number.isInteger(obj["schemaVersion"])) return false;
  if ((obj["schemaVersion"] as number) < 1) return false;
  // daemon block is optional but if present must be an object
  if (obj["daemon"] !== undefined) {
    if (typeof obj["daemon"] !== "object" || obj["daemon"] === null) return false;
    const d = obj["daemon"] as Record<string, unknown>;
    if (d["version"] !== undefined && typeof d["version"] !== "string") return false;
    if (d["commit"] !== undefined && typeof d["commit"] !== "string") return false;
    if (d["channel"] !== undefined && typeof d["channel"] !== "string") return false;
  }
  return true;
}

function errorMessage(err: unknown): string {
  return err instanceof Error ? err.message : String(err);
}
