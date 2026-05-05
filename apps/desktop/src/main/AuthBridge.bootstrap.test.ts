// hp-4qzh — bootstrap-integration test for the AuthBridge audit-sink
// wiring shipped in hp-rr9m (commit c98dc10).
//
// hp-rr9m added an optional `audit: AuthBridgeAuditSink` to AuthBridge
// but bootstrapDesktop did not pass it, so credential-lifecycle events
// (bearer persist / forget / session-metadata / secret-rotation) never
// fired in production. Direct unit tests in AuthBridge.test.ts could
// pass with injected sinks while the shipped app remained silent.
//
// This test exercises the SAME composition factory that bootstrapDesktop
// uses (`composeProductionAuthBridge`), persists a bearer, and asserts
// the matching audit row landed on disk in the JSONL audit file. No
// daemon or fetch mocks needed — the factory is isolated from the rest
// of bootstrap so the security-relevant wiring can be verified without
// spawning the backend binary.

import { mkdtempSync, readFileSync, rmSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { afterEach, beforeEach, expect, test } from "bun:test";
import {
  composeProductionAuthBridge,
  defaultSettingsAuditPath,
} from "../main.ts";
import type { DesktopSecretStorage } from "../vendored/t3code/clientPersistence.ts";
import { writeSavedEnvironmentRegistry } from "../vendored/t3code/clientPersistence.ts";

class InMemorySecretStorage implements DesktopSecretStorage {
  private readonly key = Buffer.from("hp-4qzh-test-key", "utf8");
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

let homeDir: string;
const ENV_ID = "env-1";

beforeEach(() => {
  homeDir = mkdtempSync(join(tmpdir(), "hoopoe-auth-bootstrap-"));
  // The composition factory routes registryPath into <homeDir>/.hoopoe/
  // userdata/saved-environments.json. Pre-seed an entry so persistBearer
  // has somewhere to land its encryptedBearerToken (writeSavedEnvironmentSecret
  // returns false when no record matches the env id, which would still
  // emit an audit row — but the happy-path test reads cleaner with a
  // pre-seeded record).
  const registryPath = join(homeDir, ".hoopoe", "userdata", "saved-environments.json");
  writeSavedEnvironmentRegistry(registryPath, [
    {
      environmentId: ENV_ID,
      label: "Local VPS",
      httpBaseUrl: "http://127.0.0.1:3779",
      wsBaseUrl: "ws://127.0.0.1:3779",
      createdAt: "2026-05-04T00:00:00Z",
      lastConnectedAt: null,
    },
  ]);
});

afterEach(() => {
  rmSync(homeDir, { recursive: true, force: true });
});

test("composeProductionAuthBridge: persistBearer writes one auth_bridge audit row to the durable JSONL", () => {
  const auth = composeProductionAuthBridge({
    homeDir,
    secretStorage: new InMemorySecretStorage(),
  });

  expect(
    auth.persistBearer(ENV_ID, {
      bearerToken: "fixture-bearer",
      bearer: "fixture-bearer",
      sessionId: "sid-owner-1",
      expiresAt: "2026-06-03T00:00:00Z",
      issuedAt: "2026-05-04T00:00:00Z",
      role: "owner",
      serverId: null,
    }),
  ).toBe(true);

  const auditPath = defaultSettingsAuditPath(homeDir);
  const blob = readFileSync(auditPath, "utf8");
  const lines = blob.trim().split("\n").filter((line) => line.length > 0);

  // Two audit rows on a happy persist: bearer_persisted +
  // session_metadata_written. settings-path is undefined in the
  // composition factory (registryPath is the only persistence target),
  // so persistSessionMetadata short-circuits before emitting — only
  // bearer_persisted lands. If a future refactor wires settingsPath in
  // the composition factory the second row should appear here too.
  expect(lines).toHaveLength(1);

  const entry = JSON.parse(lines[0]!);
  expect(entry.entry).toBe("auth_bridge");
  expect(entry.kind).toBe("auth.bearer_persisted");
  expect(entry.environmentId).toBe(ENV_ID);
  expect(entry.persisted).toBe(true);
  expect(entry.sessionId).toBe("sid-owner-1");
  expect(entry.expiresAt).toBe("2026-06-03T00:00:00Z");
  expect(entry.actor).toEqual({ kind: "system", id: "desktop:main", source: "ipc" });
  // Token material must not appear anywhere on the audit row.
  expect(JSON.stringify(entry)).not.toContain("fixture-bearer");
});

test("composeProductionAuthBridge: forgetBearer writes one auth_bridge audit row", () => {
  const auth = composeProductionAuthBridge({
    homeDir,
    secretStorage: new InMemorySecretStorage(),
  });

  // Persist first so we have something to forget — that emits a row;
  // we read after forget and look for the trailing entry.
  auth.persistBearer(ENV_ID, "raw-bearer-string-only");
  auth.forgetBearer(ENV_ID);

  const auditPath = defaultSettingsAuditPath(homeDir);
  const blob = readFileSync(auditPath, "utf8");
  const lines = blob.trim().split("\n").filter((line) => line.length > 0);
  expect(lines.length).toBeGreaterThanOrEqual(2);

  const last = JSON.parse(lines.at(-1)!);
  expect(last.entry).toBe("auth_bridge");
  expect(last.kind).toBe("auth.bearer_forgotten");
  expect(last.environmentId).toBe(ENV_ID);
  expect(JSON.stringify(last)).not.toContain("raw-bearer-string-only");
});
