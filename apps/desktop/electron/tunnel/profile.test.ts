// hp-e7k — VPS profile tests.

import { afterEach, beforeEach, expect, test } from "bun:test";
import { mkdtempSync, rmSync, statSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import {
  PROFILE_SCHEMA_VERSION,
  VpsProfileError,
  addProfile,
  emptyVpsProfileFile,
  findActiveProfile,
  makeProfile,
  readProfileFile,
  removeProfile,
  setActiveProfile,
  writeProfileFile,
  type ProfileStorage,
} from "./index.ts";

let tempRoot: string;
let storage: ProfileStorage;

beforeEach(() => {
  tempRoot = mkdtempSync(join(tmpdir(), "hoopoe-vpsp-"));
  storage = { profilePath: join(tempRoot, "vps-profiles.json") };
});

afterEach(() => {
  rmSync(tempRoot, { recursive: true, force: true });
});

test("readProfileFile: returns empty file when missing", () => {
  const file = readProfileFile(storage);
  expect(file).toEqual(emptyVpsProfileFile());
  expect(file.schemaVersion).toBe(PROFILE_SCHEMA_VERSION);
});

test("makeProfile: minimal happy path + defaults", () => {
  const profile = makeProfile({
    label: "Production VPS",
    host: "vps.example.com",
    username: "ubuntu",
    privateKeyPath: "~/.ssh/id_ed25519",
    now: () => new Date("2026-05-04T03:00:00Z"),
  });
  expect(profile.label).toBe("Production VPS");
  expect(profile.host).toBe("vps.example.com");
  expect(profile.port).toBe(22);
  expect(profile.preferredLocalPort).toBe(17655);
  expect(profile.daemonBinaryUrl).toBeNull();
  expect(profile.createdAt).toBe("2026-05-04T03:00:00.000Z");
  expect(profile.updatedAt).toBe(profile.createdAt);
  // UUID-shaped id.
  expect(profile.id.length).toBeGreaterThan(0);
});

test("makeProfile: requires non-empty label/host/username/privateKeyPath", () => {
  const baseInput = {
    label: "ok",
    host: "host",
    username: "user",
    privateKeyPath: "key",
  };
  expect(() => makeProfile({ ...baseInput, label: "" })).toThrow(/label is required/);
  expect(() => makeProfile({ ...baseInput, host: "   " })).toThrow(/host is required/);
  expect(() => makeProfile({ ...baseInput, username: "" })).toThrow(/username is required/);
  expect(() => makeProfile({ ...baseInput, privateKeyPath: "" })).toThrow(/privateKeyPath is required/);
});

test("makeProfile: validates port + preferredLocalPort ranges", () => {
  const baseInput = { label: "L", host: "H", username: "U", privateKeyPath: "K" };
  expect(() => makeProfile({ ...baseInput, port: 0 })).toThrow(/port must be 1..65535/);
  expect(() => makeProfile({ ...baseInput, port: 70_000 })).toThrow(/port must be 1..65535/);
  expect(() => makeProfile({ ...baseInput, preferredLocalPort: 80 })).toThrow(/preferredLocalPort must be 1024..65535/);
  expect(() => makeProfile({ ...baseInput, preferredLocalPort: 100_000 })).toThrow(/preferredLocalPort must be 1024..65535/);
});

test("addProfile: appends + setActive flips activeProfileId", () => {
  let file = emptyVpsProfileFile();
  const p1 = makeProfile({ label: "L1", host: "h", username: "u", privateKeyPath: "k", id: "id-1" });
  const p2 = makeProfile({ label: "L2", host: "h", username: "u", privateKeyPath: "k", id: "id-2" });
  file = addProfile(file, p1);
  expect(file.profiles.length).toBe(1);
  expect(file.activeProfileId).toBeNull();
  file = addProfile(file, p2, true);
  expect(file.profiles.length).toBe(2);
  expect(file.activeProfileId).toBe("id-2");
});

test("addProfile: refuses duplicate id", () => {
  const profile = makeProfile({ label: "L", host: "h", username: "u", privateKeyPath: "k", id: "dup" });
  const file = addProfile(emptyVpsProfileFile(), profile);
  expect(() => addProfile(file, profile)).toThrow(/duplicate_id/);
});

test("removeProfile: drops the entry + clears activeProfileId when removing active", () => {
  const p1 = makeProfile({ label: "L1", host: "h", username: "u", privateKeyPath: "k", id: "id-1" });
  let file = addProfile(emptyVpsProfileFile(), p1, true);
  file = removeProfile(file, "id-1");
  expect(file.profiles.length).toBe(0);
  expect(file.activeProfileId).toBeNull();
});

test("removeProfile: refuses unknown profile id", () => {
  expect(() => removeProfile(emptyVpsProfileFile(), "missing")).toThrow(/missing_profile/);
});

test("setActiveProfile: refuses unknown profile id", () => {
  expect(() => setActiveProfile(emptyVpsProfileFile(), "missing")).toThrow(/missing_profile/);
});

test("setActiveProfile: null clears the active profile", () => {
  const p1 = makeProfile({ label: "L", host: "h", username: "u", privateKeyPath: "k", id: "id-1" });
  let file = addProfile(emptyVpsProfileFile(), p1, true);
  file = setActiveProfile(file, null);
  expect(file.activeProfileId).toBeNull();
});

test("findActiveProfile: returns the active VpsProfile or null", () => {
  let file = emptyVpsProfileFile();
  expect(findActiveProfile(file)).toBeNull();
  const p1 = makeProfile({ label: "L", host: "h", username: "u", privateKeyPath: "k", id: "id-1" });
  file = addProfile(file, p1, true);
  expect(findActiveProfile(file)?.id).toBe("id-1");
});

test("readProfileFile + writeProfileFile: round-trip preserves data + 0600 mode", () => {
  const p1 = makeProfile({
    label: "Production",
    host: "vps.example.com",
    username: "ubuntu",
    privateKeyPath: "/Users/me/.ssh/id_ed25519",
    id: "id-1",
  });
  const file = addProfile(emptyVpsProfileFile(), p1, true);
  writeProfileFile(storage, file);
  const got = readProfileFile(storage);
  expect(got.profiles.length).toBe(1);
  expect(got.profiles[0]?.label).toBe("Production");
  expect(got.activeProfileId).toBe("id-1");
  // Profile file holds host + username — should NOT be world-readable.
  const mode = statSync(storage.profilePath).mode & 0o777;
  expect(mode).toBe(0o600);
});

test("readProfileFile: throws on schema mismatch", () => {
  writeProfileFile(storage, { schemaVersion: 99 as 1, profiles: [], activeProfileId: null });
  expect(() => readProfileFile(storage)).toThrow(/schema_mismatch/);
});

test("readProfileFile: throws on invalid JSON", () => {
  // Manually write garbage to the path.
  const fs = require("node:fs") as typeof import("node:fs");
  fs.writeFileSync(storage.profilePath, "{not json");
  expect(() => readProfileFile(storage)).toThrow(/parse_failed/);
});

test("VpsProfileError: stable name + carries code", () => {
  const err = new VpsProfileError("missing_field", "label is required");
  expect(err.name).toBe("VpsProfileError");
  expect(err.code).toBe("missing_field");
});
