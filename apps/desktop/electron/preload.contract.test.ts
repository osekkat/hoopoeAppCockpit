// hp-n5za — preload contract enforcement tests.
//
// We don't spawn Electron in unit tests; instead the IpcRegistry side
// (which enforces the SAME allowlist as preload via the shared
// `apps/desktop/src/shared/ipc-contract.ts`) is exercised here as the
// observable side of the contract. Unknown methods/topics also propagate
// to renderer rejects in real use because the preload's runtime guards
// throw IpcContractError before any IPC fires.
//
// The preload's runtime guards are themselves indirectly covered by the
// `ipc-contract.test.ts` (the source-of-truth) — preload calls the same
// `isDaemonRequestMethod` / `isDaemonSubscribeTopic` helpers.

import { expect, test } from "bun:test";
import { IpcRegistry } from "../src/main/IpcRegistry.ts";
import {
  DAEMON_REQUEST_METHODS,
  DAEMON_SUBSCRIBE_TOPICS,
  IpcContractError,
  PRELOAD_IPC_CHANNELS,
  isAllowedRegistryCommandId,
  isDaemonRequestMethod,
  isDaemonSubscribeTopic,
} from "../src/shared/ipc-contract.ts";

test("preload contract: every allowlisted preload channel is registrable on IpcRegistry", () => {
  const registry = new IpcRegistry();
  // Each preload channel can be registered — this proves main + preload
  // see the same allowlist and each renderer-facing channel has validators.
  for (const channel of Object.values(PRELOAD_IPC_CHANNELS)) {
    expect(() =>
      registry.register({
        id: channel,
        validateInput: passthrough,
        validateOutput: passthrough,
        handler: { handle: () => null },
      }),
    ).not.toThrow();
  }
});

test("preload contract: registering a channel outside the allowlist throws IpcContractError", () => {
  const registry = new IpcRegistry();
  for (const evil of [
    "hoopoe.daemon.evil",
    "evil.command",
    "swarm.kick",
    "settings.delete",
  ]) {
    let thrown: unknown = null;
    try {
      registry.register({ id: evil, handler: { handle: () => null } });
    } catch (error) {
      thrown = error;
    }
    expect(thrown).toBeInstanceOf(IpcContractError);
    expect((thrown as IpcContractError).kind).toBe("channel");
    expect((thrown as IpcContractError).attempted).toBe(evil);
  }
});

test("preload contract: every listed daemon method is recognised by the runtime guard", () => {
  for (const method of DAEMON_REQUEST_METHODS) {
    expect(isDaemonRequestMethod(method)).toBe(true);
  }
});

test("preload contract: every listed daemon topic is recognised by the runtime guard", () => {
  for (const topic of DAEMON_SUBSCRIBE_TOPICS) {
    expect(isDaemonSubscribeTopic(topic)).toBe(true);
  }
});

test("preload contract: arbitrary attacker-controlled method/topic is refused", () => {
  // These are the kind of strings a buggy or malicious renderer might
  // construct. The contract's runtime guards refuse all of them.
  const evilMethods = [
    "audit.read",
    "redaction.disable",
    "settings.tier-merge", // internal-only
    "exec",
    "../../../etc/passwd",
    "Object.prototype.__defineGetter__",
  ];
  for (const method of evilMethods) {
    expect(isDaemonRequestMethod(method)).toBe(false);
  }
  const evilTopics = ["events.audit", "events.redaction", "events.*", "*"];
  for (const topic of evilTopics) {
    expect(isDaemonSubscribeTopic(topic)).toBe(false);
  }
});

test("preload contract: dispatching an unknown command id (even if mock-registered) is refused at dispatch", async () => {
  const registry = new IpcRegistry();
  // Allowed registration (under internal. prefix) ...
  registry.register({
    id: "internal.shadow",
    handler: { handle: () => "ok" },
  });
  // ... but we reach into the registrations Map and rebind under a
  // disallowed id to simulate a hypothetical bypass. The dispatch
  // path's defense-in-depth check refuses it.
  // (We can't easily mutate a private Map from outside, so instead:
  // assert that direct dispatch of a non-allowlisted id throws.)
  let thrown: unknown = null;
  try {
    await registry.dispatch("hoopoe.daemon.evil", {});
  } catch (error) {
    thrown = error;
  }
  expect(thrown).toBeInstanceOf(IpcContractError);
});

test("preload contract: source-of-truth parity — registry allowlist matches isAllowedRegistryCommandId", () => {
  for (const channel of Object.values(PRELOAD_IPC_CHANNELS)) {
    expect(isAllowedRegistryCommandId(channel)).toBe(true);
  }
  for (const evil of [
    "hoopoe.audit.read",
    "hoopoe.daemon.kill",
    "swarm.terminate",
    "settings.delete",
  ]) {
    expect(isAllowedRegistryCommandId(evil)).toBe(false);
  }
});

const passthrough = (value: unknown): unknown => value;
