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
//   - every command id whose prefix is in `INTERNAL_IPC_COMMAND_PREFIXES`
//     (currently `mock-flywheel.` and `internal.`).
// Adding a new command is a deliberate edit of the contract file.
//
// Validation of `Input`/`Output` shapes against generated TypeScript types
// from `@hoopoe/schemas` is hp-r3i (Phase 2.5). The interim boundary is
// documented + tested here: the unconstrained generics on `dispatch` are
// still a known gap; allowlist + actor + redaction layers now sit above
// this gap so a malicious renderer can't drive arbitrary commands.

import {
  IpcContractError,
  isAllowedRegistryCommandId,
} from "../shared/ipc-contract.ts";

export type WhenContextKeys = ReadonlyArray<string>;

export interface IpcCommandHandler<Input, Output> {
  readonly handle: (input: Input) => Promise<Output> | Output;
}

export interface IpcCommandRegistration<Input, Output> {
  readonly id: string;
  readonly handler: IpcCommandHandler<Input, Output>;
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

export class IpcRegistry {
  private readonly registrations = new Map<string, IpcCommandRegistration<unknown, unknown>>();

  register<Input, Output>(
    registration: IpcCommandRegistration<Input, Output>,
  ): { readonly unregister: () => void } {
    // hp-n5za: refuse registrations outside the contract allowlist.
    // The allowlist is the renderer-facing channel set + named internal
    // prefixes (mock-flywheel.*, internal.*). Adding a new command is a
    // deliberate edit of `src/shared/ipc-contract.ts`.
    if (!isAllowedRegistryCommandId(registration.id)) {
      throw new IpcContractError({ kind: "channel", attempted: registration.id });
    }
    if (this.registrations.has(registration.id)) {
      throw new Error(`IPC command already registered: ${registration.id}`);
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
      throw new IpcContractError({ kind: "channel", attempted: commandId });
    }
    const registration = this.registrations.get(commandId);
    if (!registration) {
      throw new UnknownIpcCommandError(commandId);
    }
    const whenKeys = registration.whenContextKeys ?? [];
    const missing = whenKeys.filter((key) => context[key] !== true);
    if (missing.length > 0) {
      throw new IpcCommandUnavailableError(commandId, missing);
    }
    const handler = registration.handler as IpcCommandHandler<Input, Output>;
    return await handler.handle(input);
  }

  /** Test-only helper for asserting registry contents. */
  size(): number {
    return this.registrations.size;
  }
}
