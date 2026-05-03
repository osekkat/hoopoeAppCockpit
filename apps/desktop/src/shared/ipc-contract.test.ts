// hp-n5za — IPC contract source-of-truth tests.
//
// Validates the runtime guards against the static literal arrays so a
// future refactor of the contract file keeps the type-narrowing helpers
// in sync.

import { expect, test } from "bun:test";
import {
  DAEMON_REQUEST_METHODS,
  DAEMON_SUBSCRIBE_TOPICS,
  INTERNAL_IPC_COMMAND_PREFIXES,
  IpcContractError,
  PRELOAD_IPC_CHANNELS,
  isAllowedRegistryCommandId,
  isDaemonRequestMethod,
  isDaemonSubscribeTopic,
  isPreloadIpcChannel,
} from "./ipc-contract.ts";

test("DAEMON_REQUEST_METHODS: every entry is a non-empty string", () => {
  expect(DAEMON_REQUEST_METHODS.length).toBeGreaterThan(0);
  for (const method of DAEMON_REQUEST_METHODS) {
    expect(typeof method).toBe("string");
    expect(method.length).toBeGreaterThan(0);
  }
});

test("DAEMON_REQUEST_METHODS: no duplicates", () => {
  const set = new Set(DAEMON_REQUEST_METHODS);
  expect(set.size).toBe(DAEMON_REQUEST_METHODS.length);
});

test("isDaemonRequestMethod: accepts every listed method, rejects others", () => {
  for (const method of DAEMON_REQUEST_METHODS) {
    expect(isDaemonRequestMethod(method)).toBe(true);
  }
  for (const reject of [
    "settings.delete",
    "audit.read",
    "redaction.disable",
    "",
    "PING",
    "ping ", // trailing space
    null,
    undefined,
    42,
  ]) {
    expect(isDaemonRequestMethod(reject)).toBe(false);
  }
});

test("DAEMON_SUBSCRIBE_TOPICS: no duplicates and excludes internal-only topics", () => {
  const set = new Set(DAEMON_SUBSCRIBE_TOPICS);
  expect(set.size).toBe(DAEMON_SUBSCRIBE_TOPICS.length);
  // Defense check: internal-only topics MUST NOT be in the renderer-facing set.
  for (const forbidden of [
    "events.audit",
    "events.redaction",
    "events.settings.tier-merge",
    "internal.heartbeat",
  ]) {
    expect(set.has(forbidden as never)).toBe(false);
  }
});

test("isDaemonSubscribeTopic: accepts every listed topic, rejects others", () => {
  for (const topic of DAEMON_SUBSCRIBE_TOPICS) {
    expect(isDaemonSubscribeTopic(topic)).toBe(true);
  }
  for (const reject of ["events.audit", "", "*", null, 0]) {
    expect(isDaemonSubscribeTopic(reject)).toBe(false);
  }
});

test("PRELOAD_IPC_CHANNELS: every value starts with `hoopoe.`", () => {
  for (const channel of Object.values(PRELOAD_IPC_CHANNELS)) {
    expect(channel.startsWith("hoopoe.")).toBe(true);
  }
});

test("isPreloadIpcChannel: accepts only enumerated values", () => {
  for (const channel of Object.values(PRELOAD_IPC_CHANNELS)) {
    expect(isPreloadIpcChannel(channel)).toBe(true);
  }
  expect(isPreloadIpcChannel("hoopoe.daemon.evil")).toBe(false);
  expect(isPreloadIpcChannel("hoopoe.")).toBe(false);
  expect(isPreloadIpcChannel("")).toBe(false);
});

test("isAllowedRegistryCommandId: preload channels are always allowed", () => {
  for (const channel of Object.values(PRELOAD_IPC_CHANNELS)) {
    expect(isAllowedRegistryCommandId(channel)).toBe(true);
  }
});

test("isAllowedRegistryCommandId: every listed prefix gates a namespace", () => {
  for (const prefix of INTERNAL_IPC_COMMAND_PREFIXES) {
    expect(isAllowedRegistryCommandId(`${prefix}example`)).toBe(true);
    expect(isAllowedRegistryCommandId(`${prefix}deeply.nested.id`)).toBe(true);
  }
});

test("isAllowedRegistryCommandId: rejects ids outside the allowlist", () => {
  const reject = [
    "evil.command",
    "swarm.kick",
    "hoopoe.daemon.evil", // looks like a channel but isn't enumerated
    "internalNoPrefix", // missing trailing dot
    "",
  ];
  for (const id of reject) {
    expect(isAllowedRegistryCommandId(id)).toBe(false);
  }
});

test("IpcContractError: carries kind + attempted in the message", () => {
  const err = new IpcContractError({ kind: "method", attempted: "evil.method" });
  expect(err.name).toBe("IpcContractError");
  expect(err.kind).toBe("method");
  expect(err.attempted).toBe("evil.method");
  expect(err.message).toContain("method");
  expect(err.message).toContain("evil.method");
  expect(err.message).toContain("ipc-contract.ts");
});

test("Method names are stable canonical kebab/dot identifiers (no whitespace, no slashes)", () => {
  for (const method of DAEMON_REQUEST_METHODS) {
    expect(method).toMatch(/^[a-z][a-zA-Z0-9.-]*$/);
  }
  for (const topic of DAEMON_SUBSCRIBE_TOPICS) {
    expect(topic).toMatch(/^events\.[a-z]+$/);
  }
});
