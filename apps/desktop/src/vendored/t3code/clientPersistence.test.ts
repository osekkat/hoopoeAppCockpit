// Hoopoe-owned tests for the vendored t3code helper in this directory.
// The implementation file (./clientPersistence.ts) carries the MIT
// notice; tests are Hoopoe-authored and not subject to the lift policy.

import { mkdtempSync, rmSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { afterEach, beforeEach, expect, test } from "bun:test";
import {
  readSavedEnvironmentRegistry,
  readSavedEnvironmentSecret,
  writeSavedEnvironmentRegistry,
  writeSavedEnvironmentSecret,
  type DesktopSecretStorage,
} from "./clientPersistence.ts";

let workDir: string;

beforeEach(() => {
  workDir = mkdtempSync(join(tmpdir(), "hoopoe-cp-"));
});

afterEach(() => {
  rmSync(workDir, { recursive: true, force: true });
});

class InMemorySecretStorage implements DesktopSecretStorage {
  private readonly key = Buffer.from("hoopoe-test-key", "utf8");
  isEncryptionAvailable(): boolean {
    return true;
  }
  encryptString(value: string): Buffer {
    const plain = Buffer.from(value, "utf8");
    return Buffer.concat([this.key, plain]);
  }
  decryptString(value: Buffer): string {
    if (!value.subarray(0, this.key.length).equals(this.key)) {
      throw new Error("not encrypted by this storage");
    }
    return value.subarray(this.key.length).toString("utf8");
  }
}

test("clientPersistence: encrypt → write → read → decrypt round trip", () => {
  const registryPath = join(workDir, "saved-environments.json");
  const secretStorage = new InMemorySecretStorage();

  writeSavedEnvironmentRegistry(registryPath, [
    {
      environmentId: "env-1",
      label: "Local VPS",
      httpBaseUrl: "http://127.0.0.1:3779",
      wsBaseUrl: "ws://127.0.0.1:3779",
      createdAt: "2026-05-02T00:00:00Z",
      lastConnectedAt: null,
    },
  ]);

  const fakeBearer = "hp-15s-roundtrip-payload";
  const wrote = writeSavedEnvironmentSecret({
    registryPath,
    environmentId: "env-1",
    secret: fakeBearer,
    secretStorage,
  });
  expect(wrote).toBe(true);

  const decrypted = readSavedEnvironmentSecret({
    registryPath,
    environmentId: "env-1",
    secretStorage,
  });
  expect(decrypted).toBe(fakeBearer);

  const records = readSavedEnvironmentRegistry(registryPath);
  expect(records).toHaveLength(1);
  expect(records[0]?.environmentId).toBe("env-1");
  expect(records[0]?.label).toBe("Local VPS");
});
