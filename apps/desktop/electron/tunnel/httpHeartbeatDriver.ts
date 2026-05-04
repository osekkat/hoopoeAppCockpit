// hp-fkov — HttpHeartbeatDriver: real HeartbeatDriver implementation
// that probes /v1/health over the SSH-tunnel local port.
//
// Engine-first slice 2 of hp-fkov (the wiring that calls
// `orchestrator.handleHeartbeatTimeout` lives in a follow-up — this
// file is the pure I/O layer with full timeout + parsing + bearer
// support, no FSM coupling).
//
// Production callers:
//   - Connect pipeline once per connect transition (via
//     `orchestrator.heartbeat.check(...)`).
//   - Future periodic timer (separate hp-fkov slice) that calls into
//     this driver every N seconds while the FSM is `ready`.
//
// Both call paths get the same typed errors, so the orchestrator's
// existing `classifyPipelineError` mapping (heartbeat_timeout) does
// the right thing on failure.

import type { HeartbeatDriver, HeartbeatStatus } from "./orchestrator.ts";
import type { VpsProfile } from "./types.ts";

export class HttpHeartbeatError extends Error {
  override readonly name = "HttpHeartbeatError";
  readonly code: HttpHeartbeatErrorCode;
  readonly status: number | null;
  constructor(code: HttpHeartbeatErrorCode, message: string, status: number | null = null) {
    super(message);
    this.code = code;
    this.status = status;
  }
}

export type HttpHeartbeatErrorCode =
  // fetch threw / network unreachable / aborted (timeout flips to this).
  | "network"
  // Daemon returned non-200 (5xx, 4xx without retry intent).
  | "http_status"
  // Body parsing failed (malformed JSON / wrong shape).
  | "malformed_response"
  // Timeout fired before fetch resolved.
  | "timeout";

/** Subset of WHATWG fetch we depend on. Tests inject a stub; production
 *  uses Node 18+'s built-in `fetch`. We avoid pulling in @types/node-
 *  fetch or undici; the structural shape is enough. */
export interface FetchLike {
  (url: string, init: { readonly signal: AbortSignal; readonly headers: Record<string, string> }): Promise<FetchResponse>;
}

export interface FetchResponse {
  readonly ok: boolean;
  readonly status: number;
  text(): Promise<string>;
}

/** HealthResponse fields the driver needs. Mirrors the OpenAPI
 *  `HealthResponse` schema; deliberate subset. */
interface HealthResponse {
  readonly status: "ok" | "degraded" | "draining";
}

const HEALTH_STATUSES: ReadonlySet<string> = new Set(["ok", "degraded", "draining"]);

export interface HttpHeartbeatDriverOptions {
  /** WHATWG fetch. Defaults to globalThis.fetch (Node 18+ / Electron
   *  renderer). Tests inject a stub. */
  readonly fetch?: FetchLike;
  /** Timeout budget per probe. Defaults to 5s — generous enough that
   *  a healthy daemon under load doesn't false-positive, tight enough
   *  that a kernel-frozen socket reports degraded within a single tick
   *  of the orchestrator's reconnect budget. */
  readonly timeoutMs?: number;
  /** Bearer token resolver. Called per-probe (not per-construct) so
   *  bearer rotation lands on the next heartbeat without rebuilding
   *  the driver. Returning `null` skips the Authorization header — the
   *  daemon's /v1/health is `security: []`, so an unauth probe is a
   *  legitimate sanity check before pairing completes. */
  readonly bearer?: () => string | null;
  /** Optional URL builder override (tests). Default builds
   *  `http://127.0.0.1:<localPort>/v1/health`. */
  readonly urlFor?: (input: { readonly profile: VpsProfile; readonly localPort: number }) => string;
}

const DEFAULT_TIMEOUT_MS = 5_000;

export class HttpHeartbeatDriver implements HeartbeatDriver {
  readonly #fetch: FetchLike;
  readonly #timeoutMs: number;
  readonly #bearer: () => string | null;
  readonly #urlFor: (input: { readonly profile: VpsProfile; readonly localPort: number }) => string;

  constructor(options: HttpHeartbeatDriverOptions = {}) {
    const f = options.fetch ?? (globalThis.fetch as unknown as FetchLike | undefined);
    if (!f) {
      throw new Error("HttpHeartbeatDriver: no fetch implementation available (Node 18+ required)");
    }
    this.#fetch = f;
    this.#timeoutMs = options.timeoutMs ?? DEFAULT_TIMEOUT_MS;
    this.#bearer = options.bearer ?? (() => null);
    this.#urlFor = options.urlFor ?? defaultUrlFor;
  }

  async check(input: { readonly profile: VpsProfile; readonly localPort: number }): Promise<HeartbeatStatus> {
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
      // AbortController.abort() flips fetch's promise to rejection. We
      // distinguish timeout from generic network error so the audit
      // trail says exactly which one fired.
      if (controller.signal.aborted) {
        throw new HttpHeartbeatError("timeout", `heartbeat timed out after ${this.#timeoutMs}ms`);
      }
      throw new HttpHeartbeatError("network", errorMessage(err));
    } finally {
      clearTimeout(timer);
    }

    if (!response.ok) {
      throw new HttpHeartbeatError(
        "http_status",
        `heartbeat returned HTTP ${response.status}`,
        response.status,
      );
    }

    let body: string;
    try {
      body = await response.text();
    } catch (err) {
      throw new HttpHeartbeatError("malformed_response", `cannot read body: ${errorMessage(err)}`);
    }

    let parsed: unknown;
    try {
      parsed = JSON.parse(body);
    } catch (err) {
      throw new HttpHeartbeatError("malformed_response", `body is not JSON: ${errorMessage(err)}`);
    }

    if (!isHealthResponse(parsed)) {
      throw new HttpHeartbeatError(
        "malformed_response",
        "body shape does not match HealthResponse",
      );
    }

    // For now, every documented HealthResponse status maps to "ok" —
    // `degraded` and `draining` are the daemon telling us about its
    // own state, but the heartbeat itself succeeded (the daemon
    // process can serve HTTP). The version_mismatch path is wired
    // separately in a follow-up driver that compares /v1/version
    // schemaVersion against the desktop's expected range.
    return "ok";
  }
}

function defaultUrlFor(input: { readonly profile: VpsProfile; readonly localPort: number }): string {
  return `http://127.0.0.1:${input.localPort}/v1/health`;
}

function isHealthResponse(value: unknown): value is HealthResponse {
  if (typeof value !== "object" || value === null) return false;
  const obj = value as Record<string, unknown>;
  return typeof obj["status"] === "string" && HEALTH_STATUSES.has(obj["status"] as string);
}

function errorMessage(err: unknown): string {
  return err instanceof Error ? err.message : String(err);
}
