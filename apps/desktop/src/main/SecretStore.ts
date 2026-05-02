// Hoopoe-owned. Typed wrapper around Electron's `safeStorage` for the
// daemon bearer token, optional SSH key passphrase, and Oracle remote-token
// (Phase 5+). Persistence model: a single JSON file at
// `<userdata>/secrets.json` mapping keyString → base64(encryptedBuffer),
// updated via tempfile-and-rename so a crash mid-write leaves either the
// previous file intact or no file at all (never a half-written state).
//
// Hard rules:
//   - NEVER log decrypted values. The audit-log redaction layer (hp-je1p)
//     covers structured logs; this module upholds the contract by simply
//     not echoing any plaintext into the logger pathway.
//   - safeStorage requires the macOS account to be unlocked at first use.
//     If `isEncryptionAvailable()` returns false (login-keychain locked,
//     headless CI without the keychain entitlement, etc.) we fall back to
//     an in-memory cache + log a single warning, so the app still works
//     for the current session.
//   - Connection-profile ID (vpsId / keyId) is the stable key prefix; it
//     survives renames and lets multi-VPS state migrate cleanly when
//     ADR-0001 lifts.

import * as FS from "node:fs";
import * as Path from "node:path";
import type { DesktopSecretStorage } from "../vendored/t3code/clientPersistence.ts";

export type SecretKey =
  | { readonly kind: "bearer"; readonly vpsId: string }
  | { readonly kind: "ssh-passphrase"; readonly keyId: string }
  | { readonly kind: "oracle-remote-token" };

export interface SecretStoreLogger {
  readonly warn: (message: string, meta?: Record<string, unknown>) => void;
}

const noopLogger: SecretStoreLogger = { warn() {} };

export interface SecretStoreOptions {
  readonly storagePath: string;
  readonly secretStorage: DesktopSecretStorage;
  readonly logger?: SecretStoreLogger;
}

interface EncryptedBlob {
  readonly [keyString: string]: string;
}

export function serializeSecretKey(key: SecretKey): string {
  switch (key.kind) {
    case "bearer":
      return `bearer:${key.vpsId}`;
    case "ssh-passphrase":
      return `ssh-passphrase:${key.keyId}`;
    case "oracle-remote-token":
      return "oracle-remote-token";
  }
}

export class SecretStore {
  private readonly storagePath: string;
  private readonly secretStorage: DesktopSecretStorage;
  private readonly logger: SecretStoreLogger;
  private readonly inMemoryFallback = new Map<string, string>();
  private warnedFallback = false;

  constructor(options: SecretStoreOptions) {
    this.storagePath = options.storagePath;
    this.secretStorage = options.secretStorage;
    this.logger = options.logger ?? noopLogger;
  }

  has(key: SecretKey): boolean {
    return this.get(key) !== null;
  }

  set(key: SecretKey, value: string): boolean {
    const keyString = serializeSecretKey(key);
    if (!this.secretStorage.isEncryptionAvailable()) {
      this.warnFallbackOnce();
      this.inMemoryFallback.set(keyString, value);
      return false;
    }
    const blob = this.readBlob();
    const encrypted = this.secretStorage.encryptString(value).toString("base64");
    const next: EncryptedBlob = { ...blob, [keyString]: encrypted };
    this.writeBlobAtomically(next);
    return true;
  }

  get(key: SecretKey): string | null {
    const keyString = serializeSecretKey(key);
    if (!this.secretStorage.isEncryptionAvailable()) {
      this.warnFallbackOnce();
      return this.inMemoryFallback.get(keyString) ?? null;
    }
    const blob = this.readBlob();
    const encoded = blob[keyString];
    if (!encoded) return null;
    try {
      return this.secretStorage.decryptString(Buffer.from(encoded, "base64"));
    } catch {
      // The blob was encrypted by a different keychain identity (user
      // migrated machines, or the macOS account changed). Treat as absent
      // — the next set() will overwrite. Caller is responsible for
      // re-issuing the secret.
      return null;
    }
  }

  delete(key: SecretKey): void {
    const keyString = serializeSecretKey(key);
    if (!this.secretStorage.isEncryptionAvailable()) {
      this.inMemoryFallback.delete(keyString);
      return;
    }
    const blob = this.readBlob();
    if (!(keyString in blob)) return;
    const next: EncryptedBlob = { ...blob };
    delete (next as Record<string, string>)[keyString];
    this.writeBlobAtomically(next);
  }

  /** List the currently-stored key strings. Returned in insertion order from
   * the on-disk file. Plaintext values are never returned by this method —
   * use `get(key)` for explicit per-secret reads. */
  listStoredKeys(): readonly string[] {
    if (!this.secretStorage.isEncryptionAvailable()) {
      return Array.from(this.inMemoryFallback.keys());
    }
    const blob = this.readBlob();
    return Object.keys(blob);
  }

  private readBlob(): EncryptedBlob {
    try {
      if (!FS.existsSync(this.storagePath)) return {};
      const raw = FS.readFileSync(this.storagePath, "utf8");
      const parsed = JSON.parse(raw);
      if (parsed === null || typeof parsed !== "object" || Array.isArray(parsed)) return {};
      const result: Record<string, string> = {};
      for (const [k, v] of Object.entries(parsed as Record<string, unknown>)) {
        if (typeof v === "string") result[k] = v;
      }
      return result;
    } catch {
      return {};
    }
  }

  private writeBlobAtomically(blob: EncryptedBlob): void {
    const directory = Path.dirname(this.storagePath);
    const tempPath = `${this.storagePath}.${process.pid}.${Date.now()}.tmp`;
    FS.mkdirSync(directory, { recursive: true });
    FS.writeFileSync(tempPath, `${JSON.stringify(blob, null, 2)}\n`, "utf8");
    FS.renameSync(tempPath, this.storagePath);
  }

  private warnFallbackOnce(): void {
    if (this.warnedFallback) return;
    this.warnedFallback = true;
    this.logger.warn("secret-store.encryption-unavailable", {
      hint: "macOS Keychain unlock pending; secrets held in memory for this session.",
    });
  }
}

/** Default-resolved storage path under `~/.hoopoe/userdata/`. Composes with
 * `defaultSettingsBridgePaths(homeDir)` so all userdata lands in one place. */
export function defaultSecretStorePath(homeDir: string): string {
  return Path.join(homeDir, ".hoopoe", "userdata", "secrets.json");
}
