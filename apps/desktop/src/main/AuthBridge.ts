// Hoopoe-owned. Implements the three-token auth dance from plan.md §5.2:
//   pairing token (12-char Crockford, single-use)
//     → 30-day bearer (HMAC, persisted via safeStorage.encryptString)
//     → 5-min WebSocket token (HMAC, stateless, issued just-in-time)
//
// Hard rule: tokens of any kind must NEVER be logged, broadcast on the
// PubSub change stream, included in error messages, or stored unencrypted.
// The redaction layer (hp-je1p) covers structured logs; this module
// upholds the contract at the boundary by only ever returning bearer
// values to callers and never echoing them back into a logger.

import { randomUUID } from "node:crypto";
import {
  readClientSettings,
  readSavedEnvironmentSecret,
  writeClientSettings,
  writeSavedEnvironmentSecret,
  removeSavedEnvironmentSecret,
  type DesktopSecretStorage,
} from "../vendored/t3code/clientPersistence.ts";
import { AuthBridgeRedactedError } from "./AuthBridgeRedactedError.ts";

export { AuthBridgeRedactedError };

export interface AuthBridgeOptions {
  readonly registryPath: string;
  readonly settingsPath?: string;
  readonly secretStorage: DesktopSecretStorage;
  readonly fetchImpl?: typeof fetch;
  /** Per-request abort timeout for the bootstrap and ws-token fetches.
   *  Defaults to 10 000 ms — long enough for a slow loopback handshake,
   *  short enough that a wedged daemon can't hang the pairing wizard
   *  forever. Compare `vendored/t3code/backendReadiness.ts` which uses
   *  the same `AbortController` pattern for the readiness probe. */
  readonly requestTimeoutMs?: number;
}

const DEFAULT_REQUEST_TIMEOUT_MS = 10_000;
const DEFAULT_BEARER_REFRESH_WINDOW_MS = 24 * 60 * 60 * 1_000;
const PAIRING_TOKEN_RE = /^[1-9A-HJKMNPQRSTVWXYZ]{12}$/u;

export interface CapturedPairingToken {
  readonly pairingToken: string;
  readonly source: "bootstrap.stdout";
  readonly lineIndex: number;
}

export interface BearerSession {
  readonly bearerToken: string;
  readonly bearer: string;
  readonly sessionId: string;
  readonly expiresAt: string;
  readonly issuedAt: string | null;
  readonly role: string | null;
  readonly serverId: string | null;
}

export interface WsTokenSession {
  readonly wsToken: string;
  readonly sessionId: string | null;
  readonly expiresAt: string;
  readonly issuedAt: string | null;
}

export interface ExchangePairingForBearerInput {
  readonly daemonBaseUrl: string;
  readonly pairingToken: string;
  readonly instanceId: string;
}

export interface IssueWsTokenInput {
  readonly daemonBaseUrl: string;
  readonly bearerToken: string;
}

export interface RefreshBearerInput {
  readonly daemonBaseUrl: string;
  readonly bearerToken: string;
}

export interface EnsureFreshBearerInput {
  readonly daemonBaseUrl: string;
  readonly session: BearerSession;
}

export class AuthBridge {
  private readonly fetchImpl: typeof fetch;
  private readonly options: AuthBridgeOptions;
  private readonly requestTimeoutMs: number;
  private readonly refreshWindowMs: number;
  constructor(options: AuthBridgeOptions) {
    this.options = options;
    this.fetchImpl = options.fetchImpl ?? fetch;
    this.requestTimeoutMs = options.requestTimeoutMs ?? DEFAULT_REQUEST_TIMEOUT_MS;
    this.refreshWindowMs = DEFAULT_BEARER_REFRESH_WINDOW_MS;
  }

  async exchangePairingForBearer(input: ExchangePairingForBearerInput): Promise<BearerSession> {
    const response = await this.fetchWithTimeout(
      new URL("/v1/auth/bootstrap/bearer", input.daemonBaseUrl).toString(),
      {
        method: "POST",
        headers: writeHeaders(),
        body: JSON.stringify({ pairingToken: input.pairingToken, instanceId: input.instanceId }),
      },
      "bootstrap",
    );
    if (!response.ok) {
      throw new AuthBridgeRedactedError(
        `Bootstrap rejected pairing token (status ${response.status}).`,
      );
    }
    return parseBearerSession(await parseJsonPayload(response, "Bootstrap"), "Bootstrap");
  }

  async refreshBearer(input: RefreshBearerInput): Promise<BearerSession> {
    const response = await this.fetchWithTimeout(
      new URL("/v1/auth/bearer/refresh", input.daemonBaseUrl).toString(),
      {
        method: "POST",
        headers: {
          ...writeHeaders(),
          authorization: `Bearer ${input.bearerToken}`,
        },
        body: "{}",
      },
      "bearer-refresh",
    );
    if (!response.ok) {
      throw new AuthBridgeRedactedError(
        `Bearer refresh request rejected (status ${response.status}).`,
      );
    }
    return parseBearerSession(await parseJsonPayload(response, "Bearer refresh"), "Bearer refresh");
  }

  async ensureFreshBearer(input: EnsureFreshBearerInput): Promise<BearerSession> {
    if (!this.shouldRefreshBearer(input.session.expiresAt)) {
      return input.session;
    }
    return await this.refreshBearer({
      daemonBaseUrl: input.daemonBaseUrl,
      bearerToken: input.session.bearerToken,
    });
  }

  shouldRefreshBearer(expiresAt: string, now: Date = new Date()): boolean {
    const expiresAtMs = Date.parse(expiresAt);
    if (!Number.isFinite(expiresAtMs)) return true;
    return expiresAtMs - now.getTime() <= this.refreshWindowMs;
  }

  async issueWsToken(input: IssueWsTokenInput): Promise<WsTokenSession> {
    const response = await this.fetchWithTimeout(
      new URL("/v1/auth/ws-token", input.daemonBaseUrl).toString(),
      {
        method: "POST",
        headers: {
          ...writeHeaders(),
          authorization: `Bearer ${input.bearerToken}`,
        },
        body: "{}",
      },
      "ws-token",
    );
    if (!response.ok) {
      throw new AuthBridgeRedactedError(
        `WS-token request rejected (status ${response.status}).`,
      );
    }
    return parseWsTokenSession(await parseJsonPayload(response, "WS-token"));
  }

  /** Wrap fetch with an AbortController so a wedged daemon at the HTTP
   *  layer (busy-loop, blocked downstream, never replies) can't hang the
   *  pairing wizard forever. Translates `AbortError` into a redacted
   *  AuthBridgeRedactedError with a non-token message so callers can
   *  distinguish timeout from network failure or status rejection. */
  private async fetchWithTimeout(
    url: string,
    init: RequestInit,
    requestKind: "bootstrap" | "bearer-refresh" | "ws-token",
  ): Promise<Response> {
    const controller = new AbortController();
    const timer = setTimeout(() => controller.abort(), this.requestTimeoutMs);
    try {
      return await this.fetchImpl(url, { ...init, signal: controller.signal });
    } catch (error) {
      if (isAbortError(error)) {
        throw new AuthBridgeRedactedError(
          `Auth ${requestKind} request timed out after ${this.requestTimeoutMs} ms.`,
        );
      }
      throw error;
    } finally {
      clearTimeout(timer);
    }
  }

  persistBearer(environmentId: string, bearer: string | BearerSession): boolean {
    const bearerToken = typeof bearer === "string" ? bearer : bearer.bearerToken;
    const persisted = writeSavedEnvironmentSecret({
      registryPath: this.options.registryPath,
      environmentId,
      secret: bearerToken,
      secretStorage: this.options.secretStorage,
    });
    if (persisted && typeof bearer !== "string") {
      this.persistSessionMetadata(environmentId, bearer);
    }
    return persisted;
  }

  loadBearer(environmentId: string): string | null {
    return readSavedEnvironmentSecret({
      registryPath: this.options.registryPath,
      environmentId,
      secretStorage: this.options.secretStorage,
    });
  }

  forgetBearer(environmentId: string): void {
    removeSavedEnvironmentSecret({
      registryPath: this.options.registryPath,
      environmentId,
    });
  }

  private persistSessionMetadata(environmentId: string, session: BearerSession): void {
    const settingsPath = this.options.settingsPath;
    if (!settingsPath) return;

    const current = readClientSettings(settingsPath) ?? {};
    const authSettings = isRecord(current.auth) ? current.auth : {};
    const sessions = isRecord(authSettings.sessions) ? authSettings.sessions : {};
    const metadata: Record<string, string> = {
      sessionId: session.sessionId,
      expiresAt: session.expiresAt,
    };
    if (session.serverId !== null) {
      metadata.serverId = session.serverId;
    }

    writeClientSettings(settingsPath, {
      ...current,
      auth: {
        ...authSettings,
        sessions: {
          ...sessions,
          [environmentId]: metadata,
        },
      },
    });
  }
}

export function capturePairingTokenFromBootstrapOutput(stdout: string): CapturedPairingToken | null {
  const lines = stdout.split(/\r?\n/u);
  for (const [lineIndex, line] of lines.entries()) {
    const [key, value] = splitEnvAssignment(line);
    if (key !== "HOOPOE_PAIRING_TOKEN") continue;
    const pairingToken = normalizePairingToken(value);
    if (pairingToken === null) return null;
    return { pairingToken, source: "bootstrap.stdout", lineIndex };
  }
  return null;
}

function writeHeaders(): Record<string, string> {
  return {
    "content-type": "application/json",
    "idempotency-key": randomUUID(),
  };
}

async function parseJsonPayload(response: Response, label: string): Promise<Record<string, unknown>> {
  try {
    const payload = await response.json();
    if (typeof payload === "object" && payload !== null && !Array.isArray(payload)) {
      return payload as Record<string, unknown>;
    }
  } catch {
    // Fall through to the redacted boundary error below.
  }
  throw new AuthBridgeRedactedError(`${label} response was not a JSON object.`);
}

function parseBearerSession(payload: Record<string, unknown>, label: string): BearerSession {
  const bearerToken = stringField(payload, ["token", "bearerToken", "bearer"]);
  const sessionId = stringField(payload, ["sid", "sessionId"]);
  const expiresAt = stringField(payload, ["expiresAt"]);
  if (bearerToken === null) {
    throw new AuthBridgeRedactedError(`${label} response missing token.`);
  }
  if (sessionId === null) {
    throw new AuthBridgeRedactedError(`${label} response missing session id.`);
  }
  if (expiresAt === null) {
    throw new AuthBridgeRedactedError(`${label} response missing expiresAt.`);
  }
  return {
    bearerToken,
    bearer: bearerToken,
    sessionId,
    expiresAt,
    issuedAt: stringField(payload, ["issuedAt"]),
    role: stringField(payload, ["role"]),
    serverId: stringField(payload, ["serverId"]),
  };
}

function parseWsTokenSession(payload: Record<string, unknown>): WsTokenSession {
  const wsToken = stringField(payload, ["token", "wsToken"]);
  const expiresAt = stringField(payload, ["expiresAt"]);
  if (wsToken === null) {
    throw new AuthBridgeRedactedError("WS-token response missing token.");
  }
  if (expiresAt === null) {
    throw new AuthBridgeRedactedError("WS-token response missing expiresAt.");
  }
  return {
    wsToken,
    sessionId: stringField(payload, ["sid", "sessionId"]),
    expiresAt,
    issuedAt: stringField(payload, ["issuedAt"]),
  };
}

function stringField(payload: Record<string, unknown>, names: readonly string[]): string | null {
  for (const name of names) {
    const value = payload[name];
    if (typeof value === "string" && value.length > 0) return value;
  }
  return null;
}

function splitEnvAssignment(line: string): readonly [string, string] {
  const trimmed = line.trim();
  const index = trimmed.indexOf("=");
  if (index <= 0) return ["", ""];
  return [trimmed.slice(0, index), trimmed.slice(index + 1)];
}

function normalizePairingToken(value: string): string | null {
  const normalized = value.trim().replace(/[-\s]/g, "").toUpperCase();
  return PAIRING_TOKEN_RE.test(normalized) ? normalized : null;
}

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === "object" && value !== null && !Array.isArray(value);
}

function isAbortError(error: unknown): boolean {
  if (!(error instanceof Error)) return false;
  if (error.name === "AbortError") return true;
  const code = (error as unknown as { code?: unknown }).code;
  return typeof code === "string" && code === "ABORT_ERR";
}
