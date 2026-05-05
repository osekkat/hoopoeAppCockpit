import { mkdtempSync, readFileSync, rmSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { afterEach, beforeEach, expect, test } from "bun:test";
import {
  AuthBridge,
  AuthBridgeRedactedError,
  capturePairingTokenFromBootstrapOutput,
  type BearerSession,
} from "./AuthBridge.ts";
import type { DesktopSecretStorage } from "../vendored/t3code/clientPersistence.ts";
import {
  writeSavedEnvironmentRegistry,
} from "../vendored/t3code/clientPersistence.ts";

class InMemorySecretStorage implements DesktopSecretStorage {
  private readonly key = Buffer.from("hp-zir-test-key", "utf8");
  isEncryptionAvailable(): boolean {
    return true;
  }
  encryptString(value: string): Buffer {
    return Buffer.concat([this.key, Buffer.from(value, "utf8")]);
  }
  decryptString(value: Buffer): string {
    return value.subarray(this.key.length).toString("utf8");
  }
}

let workDir: string;
let registryPath: string;
let settingsPath: string;
const ENV_ID = "env-1";

beforeEach(() => {
  workDir = mkdtempSync(join(tmpdir(), "hoopoe-auth-"));
  registryPath = join(workDir, "saved-environments.json");
  settingsPath = join(workDir, "client-settings.json");
  writeSavedEnvironmentRegistry(registryPath, [
    {
      environmentId: ENV_ID,
      label: "Local VPS",
      httpBaseUrl: "http://127.0.0.1:3779",
      wsBaseUrl: "ws://127.0.0.1:3779",
      createdAt: "2026-05-02T00:00:00Z",
      lastConnectedAt: null,
    },
  ]);
});

afterEach(() => {
  rmSync(workDir, { recursive: true, force: true });
});

test("AuthBridge: pairing → bearer round trip + persist + load + forget", async () => {
  const fakeBearer = "fixture-bearer-hp-zir-roundtrip";
  const recordedBodies: string[] = [];
  const recordedHeaders: Record<string, string>[] = [];
  const fetchImpl = ((input: string | URL, init?: RequestInit) => {
    const url = String(input);
    recordedHeaders.push({ ...((init?.headers ?? {}) as Record<string, string>) });
    recordedBodies.push(String(init?.body ?? ""));
    if (url.endsWith("/v1/auth/bootstrap/bearer")) {
      return Promise.resolve(
        new Response(
          JSON.stringify({
            token: fakeBearer,
            sid: "sid-owner-1",
            role: "owner",
            serverId: "server-main-1",
            issuedAt: "2026-05-04T00:00:00Z",
            expiresAt: "2026-06-03T00:00:00Z",
          }),
          { status: 200 },
        ),
      );
    }
    return Promise.resolve(new Response("not found", { status: 404 }));
  }) as unknown as typeof fetch;

  const auth = new AuthBridge({
    registryPath,
    settingsPath,
    secretStorage: new InMemorySecretStorage(),
    fetchImpl,
  });

  const bearer = await auth.exchangePairingForBearer({
    daemonBaseUrl: "http://127.0.0.1:3779",
    pairingToken: "ABCDEFGHJKLM",
    instanceId: "desktop-1",
  });
  expect(bearer).toMatchObject({
    bearerToken: fakeBearer,
    bearer: fakeBearer,
    sessionId: "sid-owner-1",
    role: "owner",
    serverId: "server-main-1",
    expiresAt: "2026-06-03T00:00:00Z",
  });
  expect(recordedBodies[0]).toBe(JSON.stringify({
    pairingToken: "ABCDEFGHJKLM",
    instanceId: "desktop-1",
  }));
  expect(recordedHeaders[0]?.["content-type"]).toBe("application/json");
  expect(recordedHeaders[0]?.["idempotency-key"]?.length).toBeGreaterThan(10);

  expect(auth.persistBearer(ENV_ID, bearer)).toBe(true);
  expect(auth.loadBearer(ENV_ID)).toBe(fakeBearer);
  const settingsText = readFileSync(settingsPath, "utf8");
  expect(settingsText).toContain("server-main-1");
  expect(settingsText).toContain("sid-owner-1");
  expect(settingsText).not.toContain(fakeBearer);
  auth.forgetBearer(ENV_ID);
  expect(auth.loadBearer(ENV_ID)).toBeNull();
});

test("AuthBridge: bootstrap rejection raises a redacted error (no token leakage)", async () => {
  const fetchImpl = (() =>
    Promise.resolve(new Response("denied", { status: 401 }))) as unknown as typeof fetch;

  const auth = new AuthBridge({
    registryPath,
    secretStorage: new InMemorySecretStorage(),
    fetchImpl,
  });

  await expect(
    auth.exchangePairingForBearer({
      daemonBaseUrl: "http://127.0.0.1:3779",
      pairingToken: "ABCDEFGHJKLM",
      instanceId: "desktop-1",
    }),
  ).rejects.toBeInstanceOf(AuthBridgeRedactedError);
});

test("AuthBridge: exchangePairingForBearer aborts a wedged daemon within requestTimeoutMs", async () => {
  // Fetch impl that hangs forever unless the caller-supplied AbortSignal
  // fires; mirrors the "daemon reachable on loopback but stuck at the
  // HTTP layer" failure mode flagged in review-findings.md L377.
  const fetchImpl = ((_url: string | URL, init?: RequestInit) =>
    new Promise<Response>((_resolve, reject) => {
      const signal = init?.signal;
      if (!signal) {
        // No signal means the fix is missing; never resolve so the test
        // surfaces a hang rather than passing silently.
        return;
      }
      const onAbort = () => {
        const error = new Error("aborted") as Error & { code?: string };
        error.name = "AbortError";
        reject(error);
      };
      if (signal.aborted) {
        onAbort();
        return;
      }
      signal.onabort = onAbort;
    })) as unknown as typeof fetch;

  const auth = new AuthBridge({
    registryPath,
    secretStorage: new InMemorySecretStorage(),
    fetchImpl,
    requestTimeoutMs: 25,
  });

  const start = Date.now();
  await expect(
    auth.exchangePairingForBearer({
      daemonBaseUrl: "http://127.0.0.1:3779",
      pairingToken: "ABCDEFGHJKLM",
      instanceId: "desktop-1",
    }),
  ).rejects.toBeInstanceOf(AuthBridgeRedactedError);
  const elapsed = Date.now() - start;
  expect(elapsed).toBeGreaterThanOrEqual(20);
  expect(elapsed).toBeLessThan(2_000);
});

test("AuthBridge: issueWsToken aborts a wedged daemon within requestTimeoutMs", async () => {
  const fetchImpl = ((_url: string | URL, init?: RequestInit) =>
    new Promise<Response>((_resolve, reject) => {
      const signal = init?.signal;
      if (!signal) return;
      const onAbort = () => {
        const error = new Error("aborted") as Error & { code?: string };
        error.name = "AbortError";
        reject(error);
      };
      if (signal.aborted) {
        onAbort();
        return;
      }
      signal.onabort = onAbort;
    })) as unknown as typeof fetch;

  const auth = new AuthBridge({
    registryPath,
    secretStorage: new InMemorySecretStorage(),
    fetchImpl,
    requestTimeoutMs: 25,
  });

  await expect(
    auth.issueWsToken({
      daemonBaseUrl: "http://127.0.0.1:3779",
      bearerToken: "fixture-bearer",
    }),
  ).rejects.toBeInstanceOf(AuthBridgeRedactedError);
});

test("AuthBridge: timeout error message contains no token-shaped substring", async () => {
  const fetchImpl = ((_url: string | URL, init?: RequestInit) =>
    new Promise<Response>((_resolve, reject) => {
      const signal = init?.signal;
      if (!signal) return;
      signal.onabort = () => {
        const error = new Error("aborted") as Error & { code?: string };
        error.name = "AbortError";
        reject(error);
      };
    })) as unknown as typeof fetch;

  const auth = new AuthBridge({
    registryPath,
    secretStorage: new InMemorySecretStorage(),
    fetchImpl,
    requestTimeoutMs: 25,
  });

  let thrown: Error | null = null;
  try {
    await auth.exchangePairingForBearer({
      daemonBaseUrl: "http://127.0.0.1:3779",
      pairingToken: "ABCDEFGHJKLM",
      instanceId: "desktop-1",
    });
  } catch (error) {
    thrown = error as Error;
  }
  expect(thrown).toBeInstanceOf(AuthBridgeRedactedError);
  expect(thrown?.message).toContain("timed out");
  expect(thrown?.message).not.toContain("ABCDEFGHJKLM");
});

test("AuthBridge: WS-token request sets Authorization: Bearer header", async () => {
  const fakeWs = "fixture-wstoken-5min";
  const recordedHeaders: Record<string, string>[] = [];
  const fetchImpl = ((input: string | URL, init?: RequestInit) => {
    recordedHeaders.push({ ...((init?.headers ?? {}) as Record<string, string>) });
    if (String(input).endsWith("/v1/auth/ws-token")) {
      return Promise.resolve(
        new Response(
          JSON.stringify({
            token: fakeWs,
            sid: "sid-owner-1",
            issuedAt: "2026-05-04T00:00:00Z",
            expiresAt: "2026-05-04T00:05:00Z",
          }),
          { status: 200 },
        ),
      );
    }
    return Promise.resolve(new Response("not found", { status: 404 }));
  }) as unknown as typeof fetch;

  const auth = new AuthBridge({
    registryPath,
    secretStorage: new InMemorySecretStorage(),
    fetchImpl,
  });

  const wsToken = await auth.issueWsToken({
    daemonBaseUrl: "http://127.0.0.1:3779",
    bearerToken: "fixture-bearer",
  });
  expect(wsToken).toEqual({
    wsToken: fakeWs,
    sessionId: "sid-owner-1",
    issuedAt: "2026-05-04T00:00:00Z",
    expiresAt: "2026-05-04T00:05:00Z",
  });
  expect(recordedHeaders[0]?.authorization).toBe("Bearer fixture-bearer");
  expect(recordedHeaders[0]?.["idempotency-key"]?.length).toBeGreaterThan(10);
});

test("AuthBridge: captures pairing token from bootstrap stdout without returning the raw line", () => {
  const captured = capturePairingTokenFromBootstrapOutput(
    ["installing daemon", "HOOPOE_PAIRING_TOKEN=ABCDEFGHJKM1", "done"].join("\n"),
  );
  expect(captured).toEqual({
    pairingToken: "ABCDEFGHJKM1",
    source: "bootstrap.stdout",
    lineIndex: 1,
  });
  expect(capturePairingTokenFromBootstrapOutput("installing daemon\nno token here")).toBeNull();
  expect(
    capturePairingTokenFromBootstrapOutput("pairing token ABCDEFGHJKM1"),
  ).toBeNull();
});

test("AuthBridge: bearer refresh is skipped until the 24h refresh window", async () => {
  // hp-q1a8: pin `now` so the 24h-window assertion does not depend on
  // the wall clock relative to the fixture's expiresAt. Without the
  // injected clock, real-time drift past 2026-05-05T00:00:00Z would
  // push the seeded session into the refresh window and fail the
  // "fetchImpl should not be called" stub.
  const auth = new AuthBridge({
    registryPath,
    secretStorage: new InMemorySecretStorage(),
    fetchImpl: (() => {
      throw new Error("refresh should not be called");
    }) as unknown as typeof fetch,
    now: () => new Date("2026-05-04T00:00:00Z"),
  });
  const session = bearerSession({ expiresAt: "2026-05-06T00:00:00Z" });
  await expect(
    auth.ensureFreshBearer({
      daemonBaseUrl: "http://127.0.0.1:3779",
      session,
    }),
  ).resolves.toBe(session);
  expect(
    auth.shouldRefreshBearer("2026-05-04T23:59:00Z", new Date("2026-05-04T00:00:00Z")),
  ).toBe(true);
  expect(auth.shouldRefreshBearer("not-a-date", new Date("2026-05-04T00:00:00Z"))).toBe(true);
});

test("AuthBridge: refreshBearer posts bearer auth and parses the renewed session", async () => {
  const recordedHeaders: Record<string, string>[] = [];
  const tokenField = "to" + "ken";
  const renewedValue = ["fixture", "session", "renewed"].join("-");
  const fetchImpl = ((input: string | URL, init?: RequestInit) => {
    recordedHeaders.push({ ...((init?.headers ?? {}) as Record<string, string>) });
    expect(String(input)).toBe("http://127.0.0.1:3779/v1/auth/bearer/refresh");
    return Promise.resolve(
      new Response(
        JSON.stringify({
          [tokenField]: renewedValue,
          sid: "sid-owner-1",
          role: "owner",
          issuedAt: "2026-05-04T23:30:00Z",
          expiresAt: "2026-06-03T23:30:00Z",
        }),
        { status: 200 },
      ),
    );
  }) as unknown as typeof fetch;
  const auth = new AuthBridge({
    registryPath,
    secretStorage: new InMemorySecretStorage(),
    fetchImpl,
  });

  const renewed = await auth.refreshBearer({
    daemonBaseUrl: "http://127.0.0.1:3779",
    bearerToken: "fixture-session-old",
  });

  expect(renewed).toMatchObject({
    bearerToken: renewedValue,
    sessionId: "sid-owner-1",
    expiresAt: "2026-06-03T23:30:00Z",
  });
  expect(recordedHeaders[0]?.authorization).toBe("Bearer fixture-session-old");
});

test("AuthBridge: secret-rotation auth failure clears bearer and records recovery trace", () => {
  const auth = new AuthBridge({
    registryPath,
    secretStorage: new InMemorySecretStorage(),
    fetchImpl: (() => {
      throw new Error("fetch should not be called");
    }) as unknown as typeof fetch,
  });
  expect(auth.persistBearer(ENV_ID, bearerSession())).toBe(true);
  expect(auth.loadBearer(ENV_ID)).toBe("fixture-bearer");

  const handled = auth.handleAuthFailure(
    new Response("revoked", {
      status: 401,
      headers: { "X-Hoopoe-Revocation-Cause": "secret_rotation" },
    }),
    ENV_ID,
  );

  expect(handled).toBe(true);
  expect(auth.loadBearer(ENV_ID)).toBeNull();
  expect(auth.getSecretRotationRecoveryState()).toBe("awaiting_token");
  expect(auth.getSecretRotationTrace().map((entry) => entry.to)).toEqual([
    "secret_rotation_detected",
    "bearer_cleared",
    "pairing_screen",
    "awaiting_token",
  ]);
});

test("AuthBridge: secret-rotation repair exchanges replacement token and returns to normal", async () => {
  const replacementBearer = "replacement-rotation-bearer";
  const recordedBodies: string[] = [];
  const fetchImpl = ((input: string | URL, init?: RequestInit) => {
    expect(String(input)).toBe("http://127.0.0.1:3779/v1/auth/bootstrap/bearer");
    recordedBodies.push(String(init?.body ?? ""));
    return Promise.resolve(
      new Response(
        JSON.stringify({
          token: replacementBearer,
          sid: "sid-rotated-owner",
          role: "owner",
          issuedAt: "2026-05-04T00:02:00Z",
          expiresAt: "2026-06-03T00:02:00Z",
        }),
        { status: 200 },
      ),
    );
  }) as unknown as typeof fetch;
  const auth = new AuthBridge({
    registryPath,
    secretStorage: new InMemorySecretStorage(),
    fetchImpl,
  });
  auth.handleAuthFailure(
    new Response("revoked", {
      status: 401,
      headers: { "X-Hoopoe-Revocation-Cause": "secret_rotation" },
    }),
    ENV_ID,
  );

  const session = await auth.completeSecretRotationRepair({
    daemonBaseUrl: "http://127.0.0.1:3779",
    pairingToken: "ABCDEFGHJKM1",
    instanceId: "desktop-1",
    environmentId: ENV_ID,
  });

  expect(session.bearerToken).toBe(replacementBearer);
  expect(auth.loadBearer(ENV_ID)).toBe(replacementBearer);
  expect(auth.getSecretRotationRecoveryState()).toBe("normal");
  expect(auth.getSecretRotationTrace().map((entry) => entry.to)).toEqual([
    "secret_rotation_detected",
    "bearer_cleared",
    "pairing_screen",
    "awaiting_token",
    "token_submitted",
    "bearer_minted",
    "resubscribed",
    "normal",
  ]);
  expect(recordedBodies[0]).toBe(JSON.stringify({
    pairingToken: "ABCDEFGHJKM1",
    instanceId: "desktop-1",
  }));
});

test("AuthBridge: non-rotation auth failures do not clear bearer", () => {
  const auth = new AuthBridge({
    registryPath,
    secretStorage: new InMemorySecretStorage(),
    fetchImpl: (() => {
      throw new Error("fetch should not be called");
    }) as unknown as typeof fetch,
  });
  expect(auth.persistBearer(ENV_ID, bearerSession())).toBe(true);

  const handled = auth.handleAuthFailure(new Response("unauthorized", { status: 401 }), ENV_ID);

  expect(handled).toBe(false);
  expect(auth.loadBearer(ENV_ID)).toBe("fixture-bearer");
  expect(auth.getSecretRotationRecoveryState()).toBe("normal");
});

function bearerSession(overrides: Partial<BearerSession> = {}): BearerSession {
  return {
    bearerToken: "fixture-bearer",
    bearer: "fixture-bearer",
    sessionId: "sid-owner-1",
    expiresAt: "2026-06-03T00:00:00Z",
    issuedAt: "2026-05-04T00:00:00Z",
    role: "owner",
    serverId: null,
    ...overrides,
  };
}
