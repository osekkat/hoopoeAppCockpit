import { expect, test } from "bun:test";
import {
  IpcCommandUnavailableError,
  IpcPayloadValidationError,
  IpcRegistry,
  MissingIpcValidatorError,
  UnknownIpcCommandError,
  attachIpcRegistryToElectron,
  type IpcMainLike,
  type IpcValueValidator,
} from "./IpcRegistry.ts";
import {
  INTERNAL_IPC_COMMANDS,
  PRELOAD_IPC_CHANNELS,
} from "../shared/ipc-contract.ts";

// Test fixtures use explicit main-only command IDs from the IPC contract
// manifest so they exercise the same registration path as production code.

test("IpcRegistry: register + dispatch returns the handler's result", async () => {
  const registry = new IpcRegistry();
  registry.register({
    id: INTERNAL_IPC_COMMANDS.swarmSendMarchingOrders,
    handler: {
      handle: (input: { readonly orderText: string }) =>
        Promise.resolve({ accepted: true, length: input.orderText.length }),
    },
  });
  const result = await registry.dispatch<
    { readonly orderText: string },
    { readonly accepted: boolean; readonly length: number }
  >(INTERNAL_IPC_COMMANDS.swarmSendMarchingOrders, { orderText: "stand down" });
  expect(result.accepted).toBe(true);
  expect(result.length).toBe(10);
});

test("IpcRegistry: dispatching unknown command throws UnknownIpcCommandError", async () => {
  const registry = new IpcRegistry();
  await expect(
    registry.dispatch(INTERNAL_IPC_COMMANDS.testUnregistered, {}),
  ).rejects.toBeInstanceOf(UnknownIpcCommandError);
});

test("IpcRegistry: when-clause context keys must all be true", async () => {
  const registry = new IpcRegistry();
  registry.register({
    id: INTERNAL_IPC_COMMANDS.approvalConfirm,
    whenContextKeys: ["activeApproval", "userIsOwner"],
    handler: { handle: () => "approved" },
  });

  await expect(
    registry.dispatch(INTERNAL_IPC_COMMANDS.approvalConfirm, {}, { activeApproval: true }),
  ).rejects.toBeInstanceOf(IpcCommandUnavailableError);

  const ok = await registry.dispatch<unknown, string>(
    INTERNAL_IPC_COMMANDS.approvalConfirm,
    {},
    { activeApproval: true, userIsOwner: true },
  );
  expect(ok).toBe("approved");
});

test("IpcRegistry: enabledCommands honors when-clause filtering", () => {
  const registry = new IpcRegistry();
  registry.register({
    id: INTERNAL_IPC_COMMANDS.testAlways,
    handler: { handle: () => 1 },
  });
  registry.register({
    id: INTERNAL_IPC_COMMANDS.testNeedsAuth,
    whenContextKeys: ["authenticated"],
    handler: { handle: () => 2 },
  });

  expect(registry.enabledCommands({})).toEqual([INTERNAL_IPC_COMMANDS.testAlways]);
  expect(registry.enabledCommands({ authenticated: true })).toEqual([
    INTERNAL_IPC_COMMANDS.testAlways,
    INTERNAL_IPC_COMMANDS.testNeedsAuth,
  ]);
});

test("IpcRegistry: registering same id twice fails fast", () => {
  const registry = new IpcRegistry();
  registry.register({ id: INTERNAL_IPC_COMMANDS.testDuplicate, handler: { handle: () => null } });
  expect(() =>
    registry.register({ id: INTERNAL_IPC_COMMANDS.testDuplicate, handler: { handle: () => null } }),
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

// hp-vd9 — attachIpcRegistryToElectron tests.

interface RecordedHandle {
  readonly channel: string;
  readonly listener: (event: unknown, ...args: unknown[]) => Promise<unknown> | unknown;
}

function makeIpcMainStub(): {
  readonly ipcMain: IpcMainLike;
  readonly handles: RecordedHandle[];
  readonly removed: string[];
} {
  const handles: RecordedHandle[] = [];
  const removed: string[] = [];
  const ipcMain: IpcMainLike = {
    handle(channel, listener) {
      handles.push({ channel, listener });
    },
    removeHandler(channel) {
      removed.push(channel);
    },
  };
  return { ipcMain, handles, removed };
}

test("attachIpcRegistryToElectron: registers an ipcMain.handle for every registered command", () => {
  const registry = new IpcRegistry();
  registry.register({
    id: INTERNAL_IPC_COMMANDS.swarmSendMarchingOrders,
    handler: { handle: () => ({ ok: true }) },
  });
  registry.register({
    id: PRELOAD_IPC_CHANNELS.filesOpenExternal,
    validateInput: passthrough,
    validateOutput: passthrough,
    handler: { handle: () => ({}) },
  });
  const { ipcMain, handles } = makeIpcMainStub();

  const handle = attachIpcRegistryToElectron(registry, { ipcMain });

  expect(handles.map((h) => h.channel).toSorted()).toEqual(
    [
      INTERNAL_IPC_COMMANDS.swarmSendMarchingOrders,
      PRELOAD_IPC_CHANNELS.filesOpenExternal,
    ].toSorted(),
  );
  expect(handle.attachedCommandIds.length).toBe(2);
});

test("attachIpcRegistryToElectron: ipcMain handler delegates to registry.dispatch with the wire payload", async () => {
  const registry = new IpcRegistry();
  registry.register({
    id: INTERNAL_IPC_COMMANDS.swarmSendMarchingOrders,
    handler: {
      handle: (input: { readonly orderText: string }) =>
        Promise.resolve({ accepted: true, len: input.orderText.length }),
    },
  });
  const { ipcMain, handles } = makeIpcMainStub();
  attachIpcRegistryToElectron(registry, { ipcMain });

  // Simulate Electron invoking the registered handler with (event, payload).
  const handler = handles[0]!.listener;
  const result = (await handler(/* event */ {}, { orderText: "stand down" })) as {
    accepted: boolean;
    len: number;
  };

  expect(result).toEqual({ accepted: true, len: 10 });
});

test("attachIpcRegistryToElectron: detach() removes every handler this attach call installed", () => {
  const registry = new IpcRegistry();
  registry.register({
    id: INTERNAL_IPC_COMMANDS.swarmSendMarchingOrders,
    handler: { handle: () => null },
  });
  registry.register({
    id: INTERNAL_IPC_COMMANDS.testShadow,
    handler: { handle: () => null },
  });
  const { ipcMain, removed } = makeIpcMainStub();
  const handle = attachIpcRegistryToElectron(registry, { ipcMain });

  handle.detach();

  expect(removed.toSorted()).toEqual(
    [
      INTERNAL_IPC_COMMANDS.swarmSendMarchingOrders,
      INTERNAL_IPC_COMMANDS.testShadow,
    ].toSorted(),
  );

  // Second detach is a no-op.
  handle.detach();
  expect(removed.length).toBe(2);
});

test("attachIpcRegistryToElectron: removeHandler errors during detach are swallowed (idempotent)", () => {
  const registry = new IpcRegistry();
  registry.register({
    id: INTERNAL_IPC_COMMANDS.swarmSendMarchingOrders,
    handler: { handle: () => null },
  });
  const ipcMain: IpcMainLike = {
    handle: () => undefined,
    removeHandler: () => {
      throw new Error("removeHandler boom");
    },
  };
  const handle = attachIpcRegistryToElectron(registry, { ipcMain });

  expect(() => handle.detach()).not.toThrow();
});

test("attachIpcRegistryToElectron: snapshot is taken at attach time — later registrations are not visible", () => {
  const registry = new IpcRegistry();
  registry.register({
    id: INTERNAL_IPC_COMMANDS.swarmSendMarchingOrders,
    handler: { handle: () => null },
  });
  const { ipcMain, handles } = makeIpcMainStub();
  attachIpcRegistryToElectron(registry, { ipcMain });
  expect(handles.length).toBe(1);

  // Register a second command AFTER attach.
  registry.register({
    id: INTERNAL_IPC_COMMANDS.testShadow,
    handler: { handle: () => null },
  });
  // The second handler is NOT auto-wired — caller must re-attach.
  expect(handles.length).toBe(1);
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
