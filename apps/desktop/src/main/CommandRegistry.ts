// Hoopoe-owned. Renderer-side command registry that powers ⌘K, the keybindings
// dispatcher (`apps/desktop/src/main/keybindings/`), and per-agent / per-
// tending-job commands registered at runtime. This is the addition over
// t3code's implicit string-switch IPC dispatch (Appendix B "Anti-patterns to
// refuse" #6).
//
// Distinct from `IpcRegistry` (which is an Electron `ipcMain.handle` typed
// wrapper for the renderer→main bridge). Keyboard shortcuts dispatch into
// the renderer's CommandRegistry first; only commands that actually need
// main-process privileges hop the IPC bridge.
//
// Closes Appendix B anti-pattern #7: unknown when-clause context keys must
// fail loudly at parse time. The set of allowed context keys is registered
// up front (`registerContextKey`) and validation happens when keybindings
// are compiled.

export type CommandHandler<Input = unknown, Output = unknown> =
  (input: Input) => Promise<Output> | Output;

export interface CommandRegistration<Input = unknown, Output = unknown> {
  readonly id: string;
  readonly handler: CommandHandler<Input, Output>;
  /** Default label for ⌘K palette listings; renderers may override per
   * locale. Optional — commands not surfaced in the palette can omit it. */
  readonly title?: string;
  readonly category?: string;
}

export class UnknownCommandError extends Error {
  readonly commandId: string;
  constructor(commandId: string) {
    super(`Unknown command: ${commandId}`);
    this.name = "UnknownCommandError";
    this.commandId = commandId;
  }
}

export class UnknownContextKeyError extends Error {
  readonly contextKey: string;
  constructor(contextKey: string) {
    super(
      `Unknown when-clause context key: "${contextKey}". Register it via registerContextKey() before parsing keybindings.`,
    );
    this.name = "UnknownContextKeyError";
    this.contextKey = contextKey;
  }
}

export class CommandRegistry {
  private readonly commands = new Map<string, CommandRegistration<unknown, unknown>>();
  private readonly contextKeys = new Set<string>();

  registerCommand<Input, Output>(
    registration: CommandRegistration<Input, Output>,
  ): { readonly unregister: () => void } {
    if (this.commands.has(registration.id)) {
      throw new Error(`Command already registered: ${registration.id}`);
    }
    this.commands.set(
      registration.id,
      registration as CommandRegistration<unknown, unknown>,
    );
    return {
      unregister: () => {
        this.commands.delete(registration.id);
      },
    };
  }

  registerContextKey(key: string): void {
    this.contextKeys.add(key);
  }

  registerContextKeys(keys: ReadonlyArray<string>): void {
    for (const key of keys) {
      this.contextKeys.add(key);
    }
  }

  hasCommand(commandId: string): boolean {
    return this.commands.has(commandId);
  }

  knownContextKeys(): ReadonlyArray<string> {
    return Array.from(this.contextKeys).toSorted();
  }

  /** True / false short-circuit identifiers that the evaluator handles
   * directly without consulting the registered key set. */
  isWellKnownIdentifier(identifier: string): boolean {
    return identifier === "true" || identifier === "false";
  }

  /** Throws UnknownContextKeyError if the identifier is not registered.
   * Used by the keybindings compiler to fail loud on typos. */
  validateContextKey(identifier: string): void {
    if (this.isWellKnownIdentifier(identifier)) return;
    if (!this.contextKeys.has(identifier)) {
      throw new UnknownContextKeyError(identifier);
    }
  }

  async dispatch<Input, Output>(commandId: string, input: Input): Promise<Output> {
    const registration = this.commands.get(commandId);
    if (!registration) {
      throw new UnknownCommandError(commandId);
    }
    const handler = registration.handler as CommandHandler<Input, Output>;
    return await handler(input);
  }

  /** Snapshot of currently-registered commands for ⌘K palette use. */
  listCommands(): ReadonlyArray<CommandRegistration<unknown, unknown>> {
    return Array.from(this.commands.values()).toSorted((a, b) =>
      a.id.localeCompare(b.id),
    );
  }

  /** Test-only helper. */
  size(): number {
    return this.commands.size;
  }
}
