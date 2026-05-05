// Hoopoe-owned. Replaces t3code's implicit string-switch IPC dispatch
// (Appendix B "Anti-patterns to refuse" #6) with a typed command registry.
// The same registry powers Electron `ipcMain.handle` IPC and the renderer-
// side ⌘K command palette (palette wiring lands in a later bead).
//
// hp-n5za hardening: `register()` and `dispatch()` consult the shared
// `apps/desktop/src/shared/ipc-contract.ts` allowlist — unknown command
// IDs are refused (defense-in-depth in case the preload boundary is ever
// bypassed). The allowlist permits:
//   - every value in `PRELOAD_IPC_CHANNELS` (the renderer-facing surface),
//   - every command id in the explicit internal IPC manifest.
// Adding a new command is a deliberate edit of the contract file.
//
// hp-ifq hardening: renderer-facing preload channels must register runtime
// input/output validators. Internal command ids may still use typed handlers
// without validators because they are not reachable through the preload bridge.

import {
  IpcContractError,
  isAllowedRegistryCommandId,
  isPreloadIpcChannel,
} from "../shared/ipc-contract.ts";

export type WhenContextKeys = ReadonlyArray<string>;

export interface IpcCommandHandler<Input, Output> {
  readonly handle: (input: Input) => Promise<Output> | Output;
}

export type IpcValueValidator<T> = (value: unknown) => T;
export type IpcPayloadValidationPhase = "input" | "output";

export interface IpcCommandRegistration<Input, Output> {
  readonly id: string;
  readonly handler: IpcCommandHandler<Input, Output>;
  /** Required for renderer-facing `hoopoe.*` preload channels. Validates and
   * narrows the untrusted renderer payload before the handler runs. */
  readonly validateInput?: IpcValueValidator<Input>;
  /** Required for renderer-facing `hoopoe.*` preload channels. Validates the
   * handler's returned wire shape before it crosses back into the renderer. */
  readonly validateOutput?: IpcValueValidator<Output>;
  /** When-clause: keys that must all be true in the dispatch context for the
   * handler to be eligible. Undefined / empty means "always eligible". */
  readonly whenContextKeys?: WhenContextKeys;
}

export interface IpcDispatchContext {
  readonly [key: string]: boolean;
}

export class UnknownIpcCommandError extends Error {
  readonly commandId: string;
  constructor(commandId: string) {
    super(`Unknown IPC command: ${commandId}`);
    this.name = "UnknownIpcCommandError";
    this.commandId = commandId;
  }
}

export class IpcCommandUnavailableError extends Error {
  readonly commandId: string;
  readonly missingContextKeys: ReadonlyArray<string>;
  constructor(commandId: string, missingContextKeys: ReadonlyArray<string>) {
    super(
      `IPC command "${commandId}" is unavailable: missing context keys [${missingContextKeys.join(", ")}]`,
    );
    this.name = "IpcCommandUnavailableError";
    this.commandId = commandId;
    this.missingContextKeys = missingContextKeys;
  }
}

export class MissingIpcValidatorError extends Error {
  readonly commandId: string;
  readonly missing: readonly IpcPayloadValidationPhase[];
  constructor(commandId: string, missing: readonly IpcPayloadValidationPhase[]) {
    super(
      `IPC command "${commandId}" is renderer-facing and must register ${missing.join(
        " and ",
      )} validator${missing.length === 1 ? "" : "s"}`,
    );
    this.name = "MissingIpcValidatorError";
    this.commandId = commandId;
    this.missing = missing;
  }
}

export class IpcPayloadValidationError extends Error {
  readonly commandId: string;
  readonly phase: IpcPayloadValidationPhase;
  override readonly cause: unknown;

  constructor(commandId: string, phase: IpcPayloadValidationPhase, cause: unknown) {
    const causeMessage = cause instanceof Error ? cause.message : String(cause);
    super(`IPC ${phase} validation failed for "${commandId}": ${causeMessage}`);
    this.name = "IpcPayloadValidationError";
    this.commandId = commandId;
    this.phase = phase;
    this.cause = cause;
  }
}

/**
 * Security-event payload emitted whenever the registry rejects a command —
 * either because the id isn't in the contract allowlist (`channel-not-allowlisted`),
 * an unknown command was dispatched (`command-not-registered`), or the
 * when-clause context guard failed (`command-not-eligible`).
 *
 * Per hp-rflj: "renderer attempts privileged op → preload rejects + logs the
 * attempt as a security event." The preload boundary is the first line of
 * defense; this is the second line (the registry sees what got past the
 * preload) and the one that has access to a logger / audit sink.
 *
 * Higher-level wiring (BackendLifecycle / main bootstrap) attaches a callback
 * that pipes these events into the structured logger at warn level with
 * `subsystem: "ipc.security"`.
 */
export type IpcSecurityEventKind =
  | "channel-not-allowlisted"
  | "command-not-registered"
  | "command-not-eligible"
  | "preload-channel-missing-validator"
  | "payload-validation-failed";

export interface IpcSecurityEvent {
  readonly kind: IpcSecurityEventKind;
  readonly commandId: string;
  readonly missingContextKeys?: readonly string[];
  readonly missingValidators?: readonly IpcPayloadValidationPhase[];
  readonly payloadPhase?: IpcPayloadValidationPhase;
  readonly stage: "register" | "dispatch";
}

export type IpcSecurityEventSink = (event: IpcSecurityEvent) => void;

export interface IpcRegistryOptions {
  /** Called whenever a registration or dispatch is refused. The sink MUST
   *  NOT throw — it is invoked just before the registry rethrows the error
   *  to the caller. Wire it to a structured logger in production; tests
   *  inject a spy. */
  readonly onSecurityEvent?: IpcSecurityEventSink;
}

export class IpcRegistry {
  private readonly registrations = new Map<string, IpcCommandRegistration<unknown, unknown>>();
  private readonly onSecurityEvent: IpcSecurityEventSink | undefined;

  constructor(options: IpcRegistryOptions = {}) {
    this.onSecurityEvent = options.onSecurityEvent;
  }

  private emitSecurityEvent(event: IpcSecurityEvent): void {
    if (!this.onSecurityEvent) return;
    try {
      this.onSecurityEvent(event);
    } catch {
      // Defensive: a sink that throws cannot block the throw the caller
      // is expecting. Swallow — production wiring uses a logger that
      // doesn't throw.
    }
  }

  register<Input, Output>(
    registration: IpcCommandRegistration<Input, Output>,
  ): { readonly unregister: () => void } {
    // hp-n5za: refuse registrations outside the contract allowlist.
    // The allowlist is the renderer-facing channel set + explicit internal
    // command manifest. Adding a new command is a deliberate edit of
    // `src/shared/ipc-contract.ts`.
    if (!isAllowedRegistryCommandId(registration.id)) {
      this.emitSecurityEvent({
        kind: "channel-not-allowlisted",
        commandId: registration.id,
        stage: "register",
      });
      throw new IpcContractError({ kind: "channel", attempted: registration.id });
    }
    if (this.registrations.has(registration.id)) {
      throw new Error(`IPC command already registered: ${registration.id}`);
    }
    const missingValidators: IpcPayloadValidationPhase[] = [];
    if (isPreloadIpcChannel(registration.id)) {
      if (!registration.validateInput) missingValidators.push("input");
      if (!registration.validateOutput) missingValidators.push("output");
    }
    if (missingValidators.length > 0) {
      this.emitSecurityEvent({
        kind: "preload-channel-missing-validator",
        commandId: registration.id,
        missingValidators,
        stage: "register",
      });
      throw new MissingIpcValidatorError(registration.id, missingValidators);
    }
    this.registrations.set(
      registration.id,
      registration as IpcCommandRegistration<unknown, unknown>,
    );
    return {
      unregister: () => {
        this.registrations.delete(registration.id);
      },
    };
  }

  has(commandId: string): boolean {
    return this.registrations.has(commandId);
  }

  /** Returns command ids whose when-clause context keys are all satisfied. */
  enabledCommands(context: IpcDispatchContext): readonly string[] {
    const enabled: string[] = [];
    for (const [id, registration] of this.registrations) {
      const whenKeys = registration.whenContextKeys ?? [];
      if (whenKeys.every((key) => context[key] === true)) {
        enabled.push(id);
      }
    }
    return enabled;
  }

  async dispatch<Input, Output>(
    commandId: string,
    input: Input,
    context: IpcDispatchContext = {},
  ): Promise<Output> {
    // hp-n5za defense-in-depth: even if a registration somehow exists for
    // a non-allowlisted ID (shouldn't, since `register()` blocks it), the
    // dispatch path independently checks. Two locks > one lock.
    if (!isAllowedRegistryCommandId(commandId)) {
      this.emitSecurityEvent({
        kind: "channel-not-allowlisted",
        commandId,
        stage: "dispatch",
      });
      throw new IpcContractError({ kind: "channel", attempted: commandId });
    }
    const registration = this.registrations.get(commandId);
    if (!registration) {
      this.emitSecurityEvent({
        kind: "command-not-registered",
        commandId,
        stage: "dispatch",
      });
      throw new UnknownIpcCommandError(commandId);
    }
    const whenKeys = registration.whenContextKeys ?? [];
    const missing = whenKeys.filter((key) => context[key] !== true);
    if (missing.length > 0) {
      this.emitSecurityEvent({
        kind: "command-not-eligible",
        commandId,
        missingContextKeys: missing,
        stage: "dispatch",
      });
      throw new IpcCommandUnavailableError(commandId, missing);
    }
    const inputForHandler = this.validatePayload(
      commandId,
      "input",
      registration.validateInput,
      input,
    );
    const handler = registration.handler as IpcCommandHandler<unknown, unknown>;
    const output = await handler.handle(inputForHandler);
    return this.validatePayload(
      commandId,
      "output",
      registration.validateOutput,
      output,
    ) as Output;
  }

  private validatePayload<T>(
    commandId: string,
    phase: IpcPayloadValidationPhase,
    validator: IpcValueValidator<T> | undefined,
    value: unknown,
  ): T | unknown {
    if (!validator) return value;
    try {
      return validator(value);
    } catch (error) {
      this.emitSecurityEvent({
        kind: "payload-validation-failed",
        commandId,
        payloadPhase: phase,
        stage: "dispatch",
      });
      throw new IpcPayloadValidationError(commandId, phase, error);
    }
  }

  /** Test-only helper for asserting registry contents. */
  size(): number {
    return this.registrations.size;
  }

  /** hp-vd9: snapshot of currently-registered command ids. Used by
   *  `attachIpcRegistryToElectron` to wire `ipcMain.handle` for each
   *  command at attach time. New registrations after attach require
   *  explicit re-attach (the production composition root registers all
   *  handlers BEFORE bootstrap calls the attacher; tests follow the
   *  same order). */
  registeredCommandIds(): readonly string[] {
    return Array.from(this.registrations.keys());
  }
}

/** hp-vd9: minimal Electron `ipcMain` surface the registry needs. The real
 *  Electron `IpcMain` matches this; tests inject a stub. We deliberately
 *  do not depend on the full `IpcMain` interface because importing
 *  `electron` from this file would prevent bun:test from loading it. */
export interface IpcMainLike {
  handle(channel: string, listener: (event: unknown, ...args: unknown[]) => Promise<unknown> | unknown): void;
  removeHandler(channel: string): void;
}

export interface AttachIpcRegistryOptions {
  /** Override `electron.ipcMain` for tests. */
  readonly ipcMain: IpcMainLike;
  /** Optional dispatch context (e.g. for when-clause gating). */
  readonly context?: IpcDispatchContext;
}

export interface AttachIpcRegistryHandle {
  readonly attachedCommandIds: readonly string[];
  /** Remove every `ipcMain.handle` binding this attach call installed.
   *  Idempotent — calling twice is safe. */
  readonly detach: () => void;
}

/** hp-vd9: wire each registered command in `ipc` to a matching
 *  `ipcMain.handle(commandId, ...)` so renderer `ipcRenderer.invoke()`
 *  calls reach the registry's typed dispatch path. Without this step
 *  the IpcRegistry is just an in-memory map and renderer calls hit
 *  Electron's "no handler registered" default error.
 *
 *  Call AFTER all production handlers have registered (e.g., after
 *  `registerPowerAssertionIpc(ipc, ...)` in bootstrapDesktop). The
 *  attacher takes a snapshot of registered ids at call time; commands
 *  registered later won't be visible to the renderer until the next
 *  attach pass. The `shutdown()` flow MUST call the returned
 *  `detach()` so `ipcMain.handle` doesn't leak across hot-reload or
 *  test runs. */
export function attachIpcRegistryToElectron(
  ipc: IpcRegistry,
  options: AttachIpcRegistryOptions,
): AttachIpcRegistryHandle {
  const ids = ipc.registeredCommandIds();
  const context = options.context ?? {};
  for (const commandId of ids) {
    options.ipcMain.handle(commandId, async (_event: unknown, payload: unknown) => {
      return await ipc.dispatch(commandId, payload, context);
    });
  }
  let detached = false;
  return {
    attachedCommandIds: ids,
    detach: () => {
      if (detached) return;
      detached = true;
      for (const commandId of ids) {
        try {
          options.ipcMain.removeHandler(commandId);
        } catch {
          // removeHandler can throw if the handler was already removed
          // (e.g., a hot-reload partial flow). Swallow — detach is
          // idempotent by contract.
        }
      }
    },
  };
}
