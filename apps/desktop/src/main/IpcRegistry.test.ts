import { expect, test } from "bun:test";
import {
  IpcCommandUnavailableError,
  IpcPayloadValidationError,
  IpcRegistry,
  MissingIpcValidatorError,
  UnknownIpcCommandError,
  type IpcValueValidator,
} from "./IpcRegistry.ts";
import { PRELOAD_IPC_CHANNELS } from "../shared/ipc-contract.ts";

// hp-n5za: test fixtures use the `internal.` prefix from the IPC contract
// allowlist (apps/desktop/src/shared/ipc-contract.ts) so they exercise the
// same registration path as production code, including the allowlist
// enforcement.

test("IpcRegistry: register + dispatch returns the handler's result", async () => {
  const registry = new IpcRegistry();
  registry.register({
    id: "internal.swarm-send-marching-orders",
    handler: {
      handle: (input: { readonly orderText: string }) =>
        Promise.resolve({ accepted: true, length: input.orderText.length }),
    },
  });
  const result = await registry.dispatch<
    { readonly orderText: string },
    { readonly accepted: boolean; readonly length: number }
  >("internal.swarm-send-marching-orders", { orderText: "stand down" });
  expect(result.accepted).toBe(true);
  expect(result.length).toBe(10);
});

test("IpcRegistry: dispatching unknown command throws UnknownIpcCommandError", async () => {
  const registry = new IpcRegistry();
  await expect(
    registry.dispatch("internal.nope", {}),
  ).rejects.toBeInstanceOf(UnknownIpcCommandError);
});

test("IpcRegistry: when-clause context keys must all be true", async () => {
  const registry = new IpcRegistry();
  registry.register({
    id: "internal.approval-confirm",
    whenContextKeys: ["activeApproval", "userIsOwner"],
    handler: { handle: () => "approved" },
  });

  await expect(
    registry.dispatch("internal.approval-confirm", {}, { activeApproval: true }),
  ).rejects.toBeInstanceOf(IpcCommandUnavailableError);

  const ok = await registry.dispatch<unknown, string>(
    "internal.approval-confirm",
    {},
    { activeApproval: true, userIsOwner: true },
  );
  expect(ok).toBe("approved");
});

test("IpcRegistry: enabledCommands honors when-clause filtering", () => {
  const registry = new IpcRegistry();
  registry.register({
    id: "internal.always",
    handler: { handle: () => 1 },
  });
  registry.register({
    id: "internal.needs-auth",
    whenContextKeys: ["authenticated"],
    handler: { handle: () => 2 },
  });

  expect(registry.enabledCommands({})).toEqual(["internal.always"]);
  expect(registry.enabledCommands({ authenticated: true })).toEqual([
    "internal.always",
    "internal.needs-auth",
  ]);
});

test("IpcRegistry: registering same id twice fails fast", () => {
  const registry = new IpcRegistry();
  registry.register({ id: "internal.x", handler: { handle: () => null } });
  expect(() =>
    registry.register({ id: "internal.x", handler: { handle: () => null } }),
  ).toThrow();
});

test("IpcRegistry: renderer-facing preload channels require input and output validators", () => {
  const registry = new IpcRegistry();

  expect(() =>
    registry.register({
      id: PRELOAD_IPC_CHANNELS.filesOpenExternal,
      handler: { handle: () => null },
    }),
  ).toThrow(MissingIpcValidatorError);

  expect(() =>
    registry.register({
      id: PRELOAD_IPC_CHANNELS.filesOpenExternal,
      validateInput: passthrough,
      handler: { handle: () => null },
    }),
  ).toThrow(MissingIpcValidatorError);
});

test("IpcRegistry: input validation runs before renderer-facing handlers", async () => {
  const registry = new IpcRegistry();
  let calls = 0;
  registry.register<{ url: string }, { ok: true }>({
    id: PRELOAD_IPC_CHANNELS.filesOpenExternal,
    validateInput: (value) => {
      if (!isRecord(value) || typeof value.url !== "string") {
        throw new Error("url required");
      }
      return { url: value.url };
    },
    validateOutput: expectOk,
    handler: {
      handle: () => {
        calls++;
        return { ok: true };
      },
    },
  });

  await expect(
    registry.dispatch(PRELOAD_IPC_CHANNELS.filesOpenExternal, { path: "/tmp/x" }),
  ).rejects.toBeInstanceOf(IpcPayloadValidationError);
  expect(calls).toBe(0);
});

test("IpcRegistry: output validation catches renderer-facing handler drift", async () => {
  const registry = new IpcRegistry();
  registry.register<{ url: string }, { ok: true }>({
    id: PRELOAD_IPC_CHANNELS.filesOpenExternal,
    validateInput: (value) => {
      if (!isRecord(value) || typeof value.url !== "string") {
        throw new Error("url required");
      }
      return { url: value.url };
    },
    validateOutput: expectOk,
    handler: {
      handle: () => ({ ok: false }) as unknown as { ok: true },
    },
  });

  await expect(
    registry.dispatch(PRELOAD_IPC_CHANNELS.filesOpenExternal, { url: "https://example.com" }),
  ).rejects.toBeInstanceOf(IpcPayloadValidationError);
});

const passthrough: IpcValueValidator<unknown> = (value) => value;

function expectOk(value: unknown): { ok: true } {
  if (!isRecord(value) || value.ok !== true) {
    throw new Error("ok:true required");
  }
  return { ok: true };
}

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === "object" && value !== null && !Array.isArray(value);
}
