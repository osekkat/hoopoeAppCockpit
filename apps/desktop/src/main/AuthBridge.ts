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

export interface AuthBridgeOptions {
  readonly registryPath: string;
  readonly secretStorage: DesktopSecretStorage;
  readonly fetchImpl?: typeof fetch;
}

export interface ExchangePairingForBearerInput {
  readonly daemonBaseUrl: string;
  readonly pairingToken: string;
}

export interface IssueWsTokenInput {
  readonly daemonBaseUrl: string;
  readonly bearerToken: string;
}

export class AuthBridgeRedactedError extends Error {
  constructor(message: string) {
    // Caller-visible message must never contain a token; this guard catches
    // accidental concatenation at construction time.
    if (message.includes("eyJ") || message.includes("hp-bearer-")) {
      throw new Error("AuthBridge error message contained a token-like string");
    }
    super(message);
    this.name = "AuthBridgeRedactedError";
  }
}

export class AuthBridge {
  private readonly fetchImpl: typeof fetch;
  private readonly options: AuthBridgeOptions;
  constructor(options: AuthBridgeOptions) {
    this.options = options;
    this.fetchImpl = options.fetchImpl ?? fetch;
  }

  async exchangePairingForBearer(input: ExchangePairingForBearerInput): Promise<string> {
    const response = await this.fetchImpl(
      new URL("/v1/auth/bootstrap/bearer", input.daemonBaseUrl).toString(),
      {
        method: "POST",
        headers: { "content-type": "application/json" },
        body: JSON.stringify({ pairingToken: input.pairingToken }),
      },
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
    const response = await this.fetchImpl(
      new URL("/v1/auth/ws-token", input.daemonBaseUrl).toString(),
      {
        method: "POST",
        headers: {
          authorization: `Bearer ${input.bearerToken}`,
          "content-type": "application/json",
        },
        body: "{}",
      },
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
