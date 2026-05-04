// hp-e7k — VPS profile persistence.
//
// Profiles live in `<HoopoeAppDataRoot>/vps-profiles.json`. Atomic writes
// (write-tmp + rename) so a crash mid-update never leaves a half-written
// file. Same pattern as the clone state + wizard checkpoint stores.
//
// The SSH passphrase + bearer/WS tokens go through SecretStore (Phase 1
// Keychain task) — never written to this file.

import { existsSync, mkdirSync, readFileSync, renameSync, writeFileSync } from "node:fs";
import { dirname, resolve } from "node:path";
import { randomUUID } from "node:crypto";
import {
  PROFILE_SCHEMA_VERSION,
  emptyVpsProfileFile,
  type VpsProfile,
  type VpsProfileFile,
} from "./types.ts";

export class VpsProfileError extends Error {
  override readonly name = "VpsProfileError";
  readonly code: string;
  constructor(code: string, message: string) {
    super(`vps-profile (${code}): ${message}`);
    this.code = code;
  }
}

export interface ProfileStorage {
  /** Absolute path to `vps-profiles.json`. */
  readonly profilePath: string;
}

export function readProfileFile(storage: ProfileStorage): VpsProfileFile {
  if (!existsSync(storage.profilePath)) return emptyVpsProfileFile();
  let text: string;
  try {
    text = readFileSync(storage.profilePath, "utf8");
  } catch (err) {
    throw new VpsProfileError("read_failed", `${storage.profilePath}: ${(err as Error).message}`);
  }
  let parsed: unknown;
  try {
    parsed = JSON.parse(text);
  } catch (err) {
    throw new VpsProfileError("parse_failed", `${storage.profilePath}: ${(err as Error).message}`);
  }
  if (
    typeof parsed !== "object" ||
    parsed === null ||
    (parsed as { schemaVersion?: unknown }).schemaVersion !== PROFILE_SCHEMA_VERSION
  ) {
    throw new VpsProfileError(
      "schema_mismatch",
      `${storage.profilePath}: schemaVersion must be ${PROFILE_SCHEMA_VERSION}`,
    );
  }
  return parsed as VpsProfileFile;
}

export function writeProfileFile(storage: ProfileStorage, file: VpsProfileFile): void {
  const path = resolve(storage.profilePath);
  const parent = dirname(path);
  if (!existsSync(parent)) mkdirSync(parent, { recursive: true, mode: 0o755 });
  const tmp = `${path}.${process.pid}.tmp`;
  writeFileSync(tmp, `${JSON.stringify(file, null, 2)}\n`, { encoding: "utf8", mode: 0o600 });
  try {
    renameSync(tmp, path);
  } catch (err) {
    throw new VpsProfileError("rename_failed", `${tmp} → ${path}: ${(err as Error).message}`);
  }
}

export interface CreateProfileInput {
  readonly label: string;
  readonly host: string;
  readonly port?: number;
  readonly username: string;
  readonly privateKeyPath: string;
  readonly preferredLocalPort?: number;
  readonly daemonBinaryUrl?: string | null;
  readonly id?: string;
  readonly now?: () => Date;
}

/** Build a fresh VpsProfile with sensible defaults + validation. Does
 *  NOT persist — caller composes via `addProfile()`. */
export function makeProfile(input: CreateProfileInput): VpsProfile {
  ensureNonEmpty("label", input.label);
  ensureNonEmpty("host", input.host);
  ensureNonEmpty("username", input.username);
  ensureNonEmpty("privateKeyPath", input.privateKeyPath);
  if (input.port !== undefined && (input.port < 1 || input.port > 65535)) {
    throw new VpsProfileError("invalid_port", `port must be 1..65535; got ${input.port}`);
  }
  if (input.preferredLocalPort !== undefined && (input.preferredLocalPort < 1024 || input.preferredLocalPort > 65535)) {
    throw new VpsProfileError(
      "invalid_local_port",
      `preferredLocalPort must be 1024..65535; got ${input.preferredLocalPort}`,
    );
  }
  const ts = (input.now ?? (() => new Date()))().toISOString();
  return {
    id: input.id ?? randomUUID(),
    label: input.label.trim(),
    host: input.host.trim(),
    port: input.port ?? 22,
    username: input.username.trim(),
    privateKeyPath: input.privateKeyPath.trim(),
    daemonBinaryUrl: input.daemonBinaryUrl ?? null,
    preferredLocalPort: input.preferredLocalPort ?? 17655,
    createdAt: ts,
    updatedAt: ts,
  };
}

/** Add a profile to the file (immutable update). When `setActive` is
 *  true, also flips the activeProfileId to the new profile. */
export function addProfile(file: VpsProfileFile, profile: VpsProfile, setActive = false): VpsProfileFile {
  if (file.profiles.some((p) => p.id === profile.id)) {
    throw new VpsProfileError("duplicate_id", `profile id already exists: ${profile.id}`);
  }
  return {
    schemaVersion: file.schemaVersion,
    profiles: [...file.profiles, profile],
    activeProfileId: setActive ? profile.id : file.activeProfileId,
  };
}

export function removeProfile(file: VpsProfileFile, profileId: string): VpsProfileFile {
  if (!file.profiles.some((p) => p.id === profileId)) {
    throw new VpsProfileError("missing_profile", `unknown profile: ${profileId}`);
  }
  return {
    schemaVersion: file.schemaVersion,
    profiles: file.profiles.filter((p) => p.id !== profileId),
    activeProfileId: file.activeProfileId === profileId ? null : file.activeProfileId,
  };
}

export function setActiveProfile(file: VpsProfileFile, profileId: string | null): VpsProfileFile {
  if (profileId !== null && !file.profiles.some((p) => p.id === profileId)) {
    throw new VpsProfileError("missing_profile", `unknown profile: ${profileId}`);
  }
  return { ...file, activeProfileId: profileId };
}

export function findActiveProfile(file: VpsProfileFile): VpsProfile | null {
  if (!file.activeProfileId) return null;
  return file.profiles.find((p) => p.id === file.activeProfileId) ?? null;
}

function ensureNonEmpty(field: string, value: string): void {
  if (typeof value !== "string" || value.trim().length === 0) {
    throw new VpsProfileError("missing_field", `${field} is required`);
  }
}
