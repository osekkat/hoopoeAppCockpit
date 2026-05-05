// hp-n5za — IPC contract source-of-truth tests.
//
// Validates the runtime guards against the static literal arrays so a
// future refactor of the contract file keeps the type-narrowing helpers
// in sync.

import { expect, test } from "bun:test";
import {
  DAEMON_REQUEST_METHODS,
  DAEMON_SUBSCRIBE_TOPICS,
  INTERNAL_IPC_COMMANDS,
  IpcContractError,
  MOCK_FLYWHEEL_COMMANDS,
  PRELOAD_CHANNELS_WITHOUT_DIRECT_CONTRACT,
  PRELOAD_IPC_CHANNELS,
  PRELOAD_IPC_CHANNEL_CONTRACTS,
  isAllowedRegistryCommandId,
  isDaemonRequestMethod,
  isDaemonSubscribeTopic,
  isInternalIpcCommand,
  isPreloadIpcChannel,
  preloadChannelRequiresDirectContract,
  type PreloadIpcChannelKey,
} from "./ipc-contract.ts";
import {
  DAEMON_REQUEST_METHODS as GENERATED_DAEMON_REQUEST_METHODS,
  DAEMON_SUBSCRIBE_TOPICS as GENERATED_DAEMON_SUBSCRIBE_TOPICS,
  INTERNAL_IPC_COMMANDS as GENERATED_INTERNAL_IPC_COMMANDS,
  MOCK_FLYWHEEL_COMMANDS as GENERATED_MOCK_FLYWHEEL_COMMANDS,
  PRELOAD_IPC_CHANNELS as GENERATED_PRELOAD_IPC_CHANNELS,
  PRELOAD_IPC_CHANNEL_CONTRACTS as GENERATED_PRELOAD_IPC_CHANNEL_CONTRACTS,
} from "./ipc-contract.gen.ts";

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

test("generated preload contract mirrors manual ipc-contract.ts", () => {
  expect(DAEMON_REQUEST_METHODS).toEqual(GENERATED_DAEMON_REQUEST_METHODS);
  expect(DAEMON_SUBSCRIBE_TOPICS).toEqual(GENERATED_DAEMON_SUBSCRIBE_TOPICS);
  expect(PRELOAD_IPC_CHANNELS).toEqual(GENERATED_PRELOAD_IPC_CHANNELS);
  expect(PRELOAD_IPC_CHANNEL_CONTRACTS).toEqual(
    GENERATED_PRELOAD_IPC_CHANNEL_CONTRACTS,
  );
  expect(MOCK_FLYWHEEL_COMMANDS).toEqual(GENERATED_MOCK_FLYWHEEL_COMMANDS);
  expect(INTERNAL_IPC_COMMANDS).toEqual(GENERATED_INTERNAL_IPC_COMMANDS);
});

test("power assertion preload contracts pin payload and response type names", () => {
  expect(PRELOAD_IPC_CHANNEL_CONTRACTS.powerAcquire).toEqual({
    channel: PRELOAD_IPC_CHANNELS.powerAcquire,
    input: "PowerAssertionAcquireInput",
    output: "PowerAssertionSnapshot",
  });
  expect(PRELOAD_IPC_CHANNEL_CONTRACTS.powerRelease).toEqual({
    channel: PRELOAD_IPC_CHANNELS.powerRelease,
    input: "PowerAssertionReleaseInput",
    output: "PowerAssertionSnapshot",
  });
  expect(PRELOAD_IPC_CHANNEL_CONTRACTS.powerSnapshot).toEqual({
    channel: PRELOAD_IPC_CHANNELS.powerSnapshot,
    input: "EmptyObject",
    output: "PowerAssertionSnapshot",
  });
  for (const contract of Object.values(PRELOAD_IPC_CHANNEL_CONTRACTS)) {
    expect(isPreloadIpcChannel(contract.channel)).toBe(true);
  }
});

test("hp-3zc: every invoke-style preload channel has a direct contract entry", () => {
  // Coverage gate: any new channel added to PRELOAD_IPC_CHANNELS must either
  // (a) declare a contract entry here, or (b) opt out via
  // PRELOAD_CHANNELS_WITHOUT_DIRECT_CONTRACT (which is reserved for the
  // multiplexers and the subscribe-only watch channel).
  const missing: PreloadIpcChannelKey[] = [];
  for (const channelKey of Object.keys(PRELOAD_IPC_CHANNELS) as PreloadIpcChannelKey[]) {
    if (!preloadChannelRequiresDirectContract(channelKey)) continue;
    if (!(channelKey in PRELOAD_IPC_CHANNEL_CONTRACTS)) {
      missing.push(channelKey);
    }
  }
  expect(missing).toEqual([]);
});

test("hp-3zc: opt-out list only contains multiplexers + watch channel", () => {
  // The codegen mirrors this set in
  // packages/schemas/scripts/gen-preload-contract.ts; the two MUST agree or
  // a future channel can sneak in without a contract entry. Encode the
  // intent here so a refactor of either side trips the test.
  expect(new Set(PRELOAD_CHANNELS_WITHOUT_DIRECT_CONTRACT)).toEqual(
    new Set(["daemonRequest", "daemonSubscribe", "daemonUnsubscribe", "settingsWatch"]),
  );
});

test("hp-3zc: every direct contract entry's channel matches its key + lives in PRELOAD_IPC_CHANNELS", () => {
  for (const [key, contract] of Object.entries(PRELOAD_IPC_CHANNEL_CONTRACTS)) {
    expect(PRELOAD_IPC_CHANNELS[key as PreloadIpcChannelKey]).toBe(contract.channel);
    expect(typeof contract.input).toBe("string");
    expect(contract.input.length).toBeGreaterThan(0);
    expect(typeof contract.output).toBe("string");
    expect(contract.output.length).toBeGreaterThan(0);
  }
});

test("isInternalIpcCommand: accepts only explicit internal manifest commands", () => {
  for (const command of [
    ...Object.values(INTERNAL_IPC_COMMANDS),
    ...Object.values(MOCK_FLYWHEEL_COMMANDS),
  ]) {
    expect(isInternalIpcCommand(command)).toBe(true);
    expect(isAllowedRegistryCommandId(command)).toBe(true);
  }
  expect(isInternalIpcCommand("internal.anything")).toBe(false);
  expect(isInternalIpcCommand("mock-flywheel.anything")).toBe(false);
});

test("isAllowedRegistryCommandId: rejects ids outside the allowlist", () => {
  const reject = [
    "evil.command",
    "swarm.kick",
    "hoopoe.daemon.evil", // looks like a channel but isn't enumerated
    "internalNoPrefix", // missing trailing dot
    "internal.anything", // prefix alone is not enough
    "mock-flywheel.anything", // prefix alone is not enough
    "mock-flywheel.", // empty suffix and not in the manifest
    "internal.../../etc/passwd",
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
    // Topic names start with `events.` and may have additional dotted
    // segments (e.g. `events.clone.dirty`). Each segment is lowercase
    // alphanumeric.
    expect(topic).toMatch(/^events\.[a-z][a-z0-9]*(?:\.[a-z][a-z0-9]*)*$/);
  }
});
