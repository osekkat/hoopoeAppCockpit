// Hoopoe-owned. Adapter layer that turns a `~/.hoopoe/keybindings.json`
// file into a working dispatch surface, on top of the vendored t3code
// parser/evaluator (`apps/desktop/src/vendored/t3code/keybindings/`).
//
// Responsibilities:
//   1. Read the JSON file (atomic read, malformed → fall back to defaults
//      with a structured warning, never crash the process).
//   2. Compile each rule via the vendored parser; reject rules whose
//      shortcut or `when` clause fails to parse.
//   3. Validate every `when`-clause identifier against the
//      CommandRegistry's known context keys — typos throw
//      `UnknownContextKeyError` rather than silently evaluating to false
//      (Appendix B anti-pattern #7).
//   4. Validate every command id is registered with the CommandRegistry —
//      unknown commands log a warning so users see typos in their config.
//   5. Watch the file with a 100 ms debounce so a save in `$EDITOR`
//      reloads without restart.
//   6. Resolve a keyboard event to a command via the vendored
//      evaluator's last-rule-wins semantics, then dispatch through the
//      CommandRegistry.

import * as FS from "node:fs";
import {
  resolveShortcutCommand,
  type ShortcutContext,
  type ShortcutEventLike,
} from "../../vendored/t3code/keybindings/evaluator.ts";
import {
  compileResolvedKeybindingsConfig,
} from "../../vendored/t3code/keybindings/parser.ts";
import type {
  KeybindingRule,
  KeybindingsConfig,
  KeybindingWhenNode,
  ResolvedKeybindingsConfig,
} from "../../vendored/t3code/keybindings/types.ts";
import type { CommandRegistry } from "../CommandRegistry.ts";
import { HOOPOE_CONTEXT_KEYS } from "./contextKeys.ts";

export const DEFAULT_KEYBINDINGS: ReadonlyArray<KeybindingRule> = [
  { key: "cmd+k", command: "command-palette.open" },
  { key: "cmd+shift+p", command: "command-palette.open" },
  { key: "cmd+1", command: "stage.planning" },
  { key: "cmd+2", command: "stage.beads" },
  { key: "cmd+3", command: "stage.swarm" },
  { key: "cmd+4", command: "stage.harden" },
  { key: "cmd+/", command: "activity.toggle" },
];

export interface KeybindingsManagerOptions {
  readonly configPath: string;
  readonly registry: CommandRegistry;
  readonly logger?: KeybindingsManagerLogger;
  readonly platform?: string;
  readonly debounceMs?: number;
  readonly defaults?: ReadonlyArray<KeybindingRule>;
}

export interface KeybindingsManagerLogger {
  readonly warn: (message: string, meta?: Record<string, unknown>) => void;
  readonly info: (message: string, meta?: Record<string, unknown>) => void;
}

const noopLogger: KeybindingsManagerLogger = { warn() {}, info() {} };
const DEFAULT_DEBOUNCE_MS = 100;

export type KeybindingsChangeListener = (
  resolved: ResolvedKeybindingsConfig,
) => void;

export class KeybindingsManager {
  private readonly options: KeybindingsManagerOptions;
  private readonly logger: KeybindingsManagerLogger;
  private resolved: ResolvedKeybindingsConfig;
  private readonly listeners = new Set<KeybindingsChangeListener>();
  private watcher: FS.FSWatcher | null = null;
  private debounceTimer: NodeJS.Timeout | null = null;

  constructor(options: KeybindingsManagerOptions) {
    this.options = options;
    this.logger = options.logger ?? noopLogger;
    options.registry.registerContextKeys(HOOPOE_CONTEXT_KEYS);
    this.resolved = this.loadAndCompile();
  }

  current(): ResolvedKeybindingsConfig {
    return this.resolved;
  }

  /** Dispatch a keyboard event through the resolved config. Returns the
   * command id that fired (after CommandRegistry dispatch), or null when
   * the event matched no rule. */
  async dispatch(
    event: ShortcutEventLike,
    context: ShortcutContext,
  ): Promise<string | null> {
    const platform = this.options.platform ?? process.platform;
    const command = resolveShortcutCommand(event, this.resolved, context, platform);
    if (!command) return null;
    if (!this.options.registry.hasCommand(command)) {
      this.logger.warn("keybindings.command-not-registered", { command });
      return null;
    }
    await this.options.registry.dispatch(command, event);
    return command;
  }

  subscribe(listener: KeybindingsChangeListener): { readonly unsubscribe: () => void } {
    this.listeners.add(listener);
    return {
      unsubscribe: () => {
        this.listeners.delete(listener);
      },
    };
  }

  /** Start watching the config file for changes. The 100 ms debounce
   * matches t3code's chosen value (don't tune without measuring). */
  startWatching(): void {
    if (this.watcher) return;
    if (!FS.existsSync(this.options.configPath)) return;
    try {
      this.watcher = FS.watch(this.options.configPath, () => {
        this.scheduleReload();
      });
    } catch (error) {
      this.logger.warn("keybindings.watch-failed", {
        error: error instanceof Error ? error.message : String(error),
      });
    }
  }

  stopWatching(): void {
    if (this.debounceTimer) {
      clearTimeout(this.debounceTimer);
      this.debounceTimer = null;
    }
    if (this.watcher) {
      this.watcher.close();
      this.watcher = null;
    }
  }

  /** Test seam — synchronous reload. Production code uses startWatching(). */
  reloadNow(): void {
    this.resolved = this.loadAndCompile();
    for (const listener of this.listeners) {
      try {
        listener(this.resolved);
      } catch {
        // Don't let a bad listener poison the bus.
      }
    }
  }

  private scheduleReload(): void {
    const debounceMs = this.options.debounceMs ?? DEFAULT_DEBOUNCE_MS;
    if (this.debounceTimer) clearTimeout(this.debounceTimer);
    this.debounceTimer = setTimeout(() => {
      this.debounceTimer = null;
      this.reloadNow();
    }, debounceMs);
  }

  private loadAndCompile(): ResolvedKeybindingsConfig {
    const userConfig = this.readUserConfig();
    const merged = [...(this.options.defaults ?? DEFAULT_KEYBINDINGS), ...userConfig];
    this.assertContextKeysAreKnown(merged);
    return compileResolvedKeybindingsConfig(merged);
  }

  private readUserConfig(): KeybindingsConfig {
    try {
      if (!FS.existsSync(this.options.configPath)) return [];
      const raw = FS.readFileSync(this.options.configPath, "utf8");
      const parsed = JSON.parse(raw);
      if (!Array.isArray(parsed)) {
        this.logger.warn("keybindings.malformed", { detail: "expected an array" });
        return [];
      }
      const result: KeybindingRule[] = [];
      for (const entry of parsed) {
        if (entry === null || typeof entry !== "object") continue;
        const obj = entry as Record<string, unknown>;
        if (typeof obj.key !== "string" || typeof obj.command !== "string") continue;
        const rule: KeybindingRule = { key: obj.key, command: obj.command };
        if (typeof obj.when === "string") {
          result.push({ ...rule, when: obj.when });
        } else {
          result.push(rule);
        }
      }
      return result;
    } catch (error) {
      this.logger.warn("keybindings.read-failed", {
        error: error instanceof Error ? error.message : String(error),
      });
      return [];
    }
  }

  private assertContextKeysAreKnown(rules: ReadonlyArray<KeybindingRule>): void {
    const registry = this.options.registry;
    for (const rule of rules) {
      if (rule.when === undefined) continue;
      const tokens = collectIdentifiers(rule.when);
      for (const identifier of tokens) {
        registry.validateContextKey(identifier);
      }
    }
  }
}

/** Best-effort identifier extraction over the raw `when` string — we only
 * need to throw on unknown keys, so a regex scan suffices. The recursive-
 * descent parser provides the canonical AST for evaluation. */
function collectIdentifiers(expression: string): readonly string[] {
  const matches = expression.match(/[A-Za-z_][A-Za-z0-9_.-]*/g);
  return matches ? Array.from(new Set(matches)) : [];
}

export function collectAstIdentifiers(node: KeybindingWhenNode): readonly string[] {
  const out = new Set<string>();
  const visit = (n: KeybindingWhenNode) => {
    switch (n.type) {
      case "identifier":
        out.add(n.name);
        return;
      case "not":
        visit(n.node);
        return;
      case "and":
      case "or":
        visit(n.left);
        visit(n.right);
        return;
    }
  };
  visit(node);
  return Array.from(out);
}
