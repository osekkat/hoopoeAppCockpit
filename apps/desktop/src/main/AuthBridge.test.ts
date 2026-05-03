import { mkdtempSync, rmSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { afterEach, beforeEach, expect, test } from "bun:test";
import { AuthBridge, AuthBridgeRedactedError } from "./AuthBridge.ts";
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
const ENV_ID = "env-1";

beforeEach(() => {
  workDir = mkdtempSync(join(tmpdir(), "hoopoe-auth-"));
  registryPath = join(workDir, "saved-environments.json");
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
  const fetchImpl = ((input: string | URL) => {
    const url = String(input);
    if (url.endsWith("/v1/auth/bootstrap/bearer")) {
      return Promise.resolve(
        new Response(JSON.stringify({ bearerToken: fakeBearer }), { status: 200 }),
      );
    }
    return Promise.resolve(new Response("not found", { status: 404 }));
  }) as unknown as typeof fetch;

  const auth = new AuthBridge({
    registryPath,
    secretStorage: new InMemorySecretStorage(),
    fetchImpl,
  });

  const bearer = await auth.exchangePairingForBearer({
    daemonBaseUrl: "http://127.0.0.1:3779",
    pairingToken: "ABCDEFGHJKLM",
  });
  expect(bearer).toBe(fakeBearer);

  expect(auth.persistBearer(ENV_ID, bearer)).toBe(true);
  expect(auth.loadBearer(ENV_ID)).toBe(fakeBearer);
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
      signal.addEventListener("abort", onAbort, { once: true });
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
      signal.addEventListener("abort", onAbort, { once: true });
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
      signal.addEventListener(
        "abort",
        () => {
          const error = new Error("aborted") as Error & { code?: string };
          error.name = "AbortError";
          reject(error);
        },
        { once: true },
      );
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
        new Response(JSON.stringify({ wsToken: fakeWs }), { status: 200 }),
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
  expect(wsToken).toBe(fakeWs);
  expect(recordedHeaders[0]?.authorization).toBe("Bearer fixture-bearer");
});
