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
  /** hp-q1a8: clock provider for the bearer-refresh-window check.
   *  Defaults to `() => new Date()`. Tests inject a fixed clock so the
   *  24h window assertion does not depend on wall-clock-vs-fixture
   *  drift. */
  readonly now?: () => Date;
  /** hp-rr9m: optional audit sink. Production wiring routes these
   *  events into the structured logger; tests inject a spy. The sink
   *  MUST NOT throw — the bridge swallows sink errors so a bad logger
   *  cannot demote a successful persist into a perceived failure. */
  readonly audit?: AuthBridgeAuditSink;
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

export type SecretRotationRecoveryState =
  | "normal"
  | "secret_rotation_detected"
  | "bearer_cleared"
  | "pairing_screen"
  | "awaiting_token"
  | "token_submitted"
  | "bearer_minted"
  | "resubscribed";

export interface SecretRotationTransition {
  readonly from: SecretRotationRecoveryState;
  readonly to: SecretRotationRecoveryState;
  readonly reason: string;
  readonly at: string;
}

export interface CompleteSecretRotationRepairInput extends ExchangePairingForBearerInput {
  readonly environmentId: string;
}

/** hp-rr9m: events emitted by AuthBridge so the audit log can show
 *  bearer persist / forget / session-metadata / secret-rotation
 *  side effects that were previously invisible. Token material is
 *  never on these payloads — only environment id, session id (if a
 *  session was provided), expiresAt (a public ISO date), serverId
 *  (an opaque identifier exposed elsewhere in the API), and error
 *  codes/messages routed through `redactAuthError`. */
export type AuthBridgeAuditEvent =
  | {
      readonly kind: "auth.bearer_persisted";
      readonly at: string;
      readonly environmentId: string;
      readonly sessionId: string | null;
      readonly expiresAt: string | null;
      /** False when secret storage encryption is unavailable or the
       *  saved-environment registry has no record for `environmentId`. */
      readonly persisted: boolean;
    }
  | {
      readonly kind: "auth.bearer_persist_failed";
      readonly at: string;
      readonly environmentId: string;
      readonly errorCode: string;
      readonly errorMessage: string;
    }
  | {
      readonly kind: "auth.session_metadata_written";
      readonly at: string;
      readonly environmentId: string;
      readonly sessionId: string;
      readonly expiresAt: string;
      readonly serverId: string | null;
    }
  | {
      readonly kind: "auth.bearer_forgotten";
      readonly at: string;
      readonly environmentId: string;
    }
  | {
      readonly kind: "auth.bearer_forget_failed";
      readonly at: string;
      readonly environmentId: string;
      readonly errorCode: string;
      readonly errorMessage: string;
    }
  | {
      readonly kind: "auth.secret_rotation_transition";
      readonly at: string;
      readonly from: SecretRotationRecoveryState;
      readonly to: SecretRotationRecoveryState;
      readonly reason: string;
    };

export type AuthBridgeAuditSink = (event: AuthBridgeAuditEvent) => void;

export class AuthBridge {
  private readonly fetchImpl: typeof fetch;
  private readonly options: AuthBridgeOptions;
  private readonly requestTimeoutMs: number;
  private readonly refreshWindowMs: number;
  private readonly nowProvider: () => Date;
  private readonly auditSink: AuthBridgeAuditSink | undefined;
  private secretRotationState: SecretRotationRecoveryState = "normal";
  private readonly secretRotationTrace: SecretRotationTransition[] = [];
  constructor(options: AuthBridgeOptions) {
    this.options = options;
    this.fetchImpl = options.fetchImpl ?? fetch;
    this.requestTimeoutMs = options.requestTimeoutMs ?? DEFAULT_REQUEST_TIMEOUT_MS;
    this.refreshWindowMs = DEFAULT_BEARER_REFRESH_WINDOW_MS;
    this.nowProvider = options.now ?? (() => new Date());
    this.auditSink = options.audit;
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

  shouldRefreshBearer(expiresAt: string, now: Date = this.nowProvider()): boolean {
    const expiresAtMs = Date.parse(expiresAt);
    if (!Number.isFinite(expiresAtMs)) return true;
    return expiresAtMs - now.getTime() <= this.refreshWindowMs;
  }

  getSecretRotationRecoveryState(): SecretRotationRecoveryState {
    return this.secretRotationState;
  }

  getSecretRotationTrace(): readonly SecretRotationTransition[] {
    return [...this.secretRotationTrace];
  }

  handleAuthFailure(response: Response, environmentId: string): boolean {
    if (!isSecretRotationRevocation(response)) {
      return false;
    }
    this.transitionSecretRotation("secret_rotation_detected", "daemon reported secret rotation");
    this.forgetBearer(environmentId);
    this.transitionSecretRotation("bearer_cleared", "persisted bearer cleared");
    this.transitionSecretRotation("pairing_screen", "renderer must show in-app pairing flow");
    this.transitionSecretRotation("awaiting_token", "waiting for replacement pairing token");
    return true;
  }

  async completeSecretRotationRepair(
    input: CompleteSecretRotationRepairInput,
  ): Promise<BearerSession> {
    if (this.secretRotationState !== "awaiting_token") {
      throw new AuthBridgeRedactedError("Secret rotation repair requires awaiting_token state.");
    }
    this.transitionSecretRotation("token_submitted", "replacement pairing token submitted");
    const session = await this.exchangePairingForBearer(input);
    if (!this.persistBearer(input.environmentId, session)) {
      throw new AuthBridgeRedactedError("Secret rotation repair could not persist bearer.");
    }
    this.transitionSecretRotation("bearer_minted", "replacement bearer minted");
    this.transitionSecretRotation("resubscribed", "event subscriptions may replay from sequence cursors");
    this.transitionSecretRotation("normal", "secret rotation recovery complete");
    return session;
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
    let persisted: boolean;
    try {
      persisted = writeSavedEnvironmentSecret({
        registryPath: this.options.registryPath,
        environmentId,
        secret: bearerToken,
        secretStorage: this.options.secretStorage,
      });
    } catch (err) {
      this.emitAudit({
        kind: "auth.bearer_persist_failed",
        at: this.nowProvider().toISOString(),
        environmentId,
        ...redactAuthError(err),
      });
      throw err;
    }
    this.emitAudit({
      kind: "auth.bearer_persisted",
      at: this.nowProvider().toISOString(),
      environmentId,
      sessionId: typeof bearer === "string" ? null : bearer.sessionId,
      expiresAt: typeof bearer === "string" ? null : bearer.expiresAt,
      persisted,
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
    try {
      removeSavedEnvironmentSecret({
        registryPath: this.options.registryPath,
        environmentId,
      });
    } catch (err) {
      this.emitAudit({
        kind: "auth.bearer_forget_failed",
        at: this.nowProvider().toISOString(),
        environmentId,
        ...redactAuthError(err),
      });
      throw err;
    }
    this.emitAudit({
      kind: "auth.bearer_forgotten",
      at: this.nowProvider().toISOString(),
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
    this.emitAudit({
      kind: "auth.session_metadata_written",
      at: this.nowProvider().toISOString(),
      environmentId,
      sessionId: session.sessionId,
      expiresAt: session.expiresAt,
      serverId: session.serverId,
    });
  }

  private transitionSecretRotation(to: SecretRotationRecoveryState, reason: string): void {
    const from = this.secretRotationState;
    if (from === to) return;
    this.secretRotationState = to;
    const at = this.nowProvider().toISOString();
    this.secretRotationTrace.push({
      from,
      to,
      reason,
      at,
    });
    this.emitAudit({
      kind: "auth.secret_rotation_transition",
      at,
      from,
      to,
      reason,
    });
  }

  private emitAudit(event: AuthBridgeAuditEvent): void {
    if (!this.auditSink) return;
    try {
      this.auditSink(event);
    } catch {
      // Defensive: a sink that throws cannot block credential lifecycle
      // operations. Drop the event silently — production wiring uses a
      // logger that doesn't throw.
    }
  }
}

/** Pull a redacted (code, message) pair off an arbitrary thrown value
 *  for inclusion in audit events. AuthBridgeRedactedError already
 *  carries a redacted message (token material scrubbed); other errors
 *  use the constructor name as a stable, low-cardinality discriminator. */
function redactAuthError(err: unknown): { errorCode: string; errorMessage: string } {
  if (err instanceof AuthBridgeRedactedError) {
    return { errorCode: "AuthBridgeRedactedError", errorMessage: err.message };
  }
  if (err instanceof Error) {
    return { errorCode: err.name || "Error", errorMessage: err.message };
  }
  return { errorCode: "unknown", errorMessage: String(err) };
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

function isSecretRotationRevocation(response: Response): boolean {
  if (response.status !== 401) return false;
  return response.headers.get("X-Hoopoe-Revocation-Cause") === "secret_rotation";
}

function isAbortError(error: unknown): boolean {
  if (!(error instanceof Error)) return false;
  if (error.name === "AbortError") return true;
  const code = (error as unknown as { code?: unknown }).code;
  return typeof code === "string" && code === "ABORT_ERR";
}
