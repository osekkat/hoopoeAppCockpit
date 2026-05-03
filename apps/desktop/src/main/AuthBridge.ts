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

import {
  readSavedEnvironmentSecret,
  writeSavedEnvironmentSecret,
  removeSavedEnvironmentSecret,
  type DesktopSecretStorage,
} from "../vendored/t3code/clientPersistence.ts";
import { AuthBridgeRedactedError } from "./AuthBridgeRedactedError.ts";

export { AuthBridgeRedactedError };

export interface AuthBridgeOptions {
  readonly registryPath: string;
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

export interface ExchangePairingForBearerInput {
  readonly daemonBaseUrl: string;
  readonly pairingToken: string;
}

export interface IssueWsTokenInput {
  readonly daemonBaseUrl: string;
  readonly bearerToken: string;
}

export class AuthBridge {
  private readonly fetchImpl: typeof fetch;
  private readonly options: AuthBridgeOptions;
  private readonly requestTimeoutMs: number;
  constructor(options: AuthBridgeOptions) {
    this.options = options;
    this.fetchImpl = options.fetchImpl ?? fetch;
    this.requestTimeoutMs = options.requestTimeoutMs ?? DEFAULT_REQUEST_TIMEOUT_MS;
  }

  async exchangePairingForBearer(input: ExchangePairingForBearerInput): Promise<string> {
    const response = await this.fetchWithTimeout(
      new URL("/v1/auth/bootstrap/bearer", input.daemonBaseUrl).toString(),
      {
        method: "POST",
        headers: { "content-type": "application/json" },
        body: JSON.stringify({ pairingToken: input.pairingToken }),
      },
      "bootstrap",
    );
    if (!response.ok) {
      throw new AuthBridgeRedactedError(
        `Bootstrap rejected pairing token (status ${response.status}).`,
      );
    }
    const payload = (await response.json()) as { bearerToken?: unknown };
    if (typeof payload.bearerToken !== "string" || payload.bearerToken.length === 0) {
      throw new AuthBridgeRedactedError("Bootstrap response missing bearerToken.");
    }
    return payload.bearerToken;
  }

  async issueWsToken(input: IssueWsTokenInput): Promise<string> {
    const response = await this.fetchWithTimeout(
      new URL("/v1/auth/ws-token", input.daemonBaseUrl).toString(),
      {
        method: "POST",
        headers: {
          authorization: `Bearer ${input.bearerToken}`,
          "content-type": "application/json",
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
    const payload = (await response.json()) as { wsToken?: unknown };
    if (typeof payload.wsToken !== "string" || payload.wsToken.length === 0) {
      throw new AuthBridgeRedactedError("WS-token response missing wsToken.");
    }
    return payload.wsToken;
  }

  /** Wrap fetch with an AbortController so a wedged daemon at the HTTP
   *  layer (busy-loop, blocked downstream, never replies) can't hang the
   *  pairing wizard forever. Translates `AbortError` into a redacted
   *  AuthBridgeRedactedError with a non-token message so callers can
   *  distinguish timeout from network failure or status rejection. */
  private async fetchWithTimeout(
    url: string,
    init: RequestInit,
    requestKind: "bootstrap" | "ws-token",
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

  persistBearer(environmentId: string, bearerToken: string): boolean {
    return writeSavedEnvironmentSecret({
      registryPath: this.options.registryPath,
      environmentId,
      secret: bearerToken,
      secretStorage: this.options.secretStorage,
    });
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
}

function isAbortError(error: unknown): boolean {
  if (!(error instanceof Error)) return false;
  if (error.name === "AbortError") return true;
  const code = (error as unknown as { code?: unknown }).code;
  return typeof code === "string" && code === "ABORT_ERR";
}
