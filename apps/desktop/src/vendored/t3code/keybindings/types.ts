// Originally from github.com/pingdotgg/t3code (MIT License)
// Copyright (c) 2026 T3 Tools Inc.
// Adapted for Hoopoe.
//
// Full MIT license text: vendored/t3code/LICENSE
//
// Type-only carve-out of t3code's `packages/contracts/src/keybindings.ts`
// (Effect.Schema-based) reduced to plain TypeScript shapes. The structural
// surface is unchanged; only the Effect.Schema decoration is gone.

export const MAX_KEYBINDING_VALUE_LENGTH = 64;
export const MAX_KEYBINDING_WHEN_LENGTH = 256;
export const MAX_WHEN_EXPRESSION_DEPTH = 64;
export const MAX_KEYBINDINGS_COUNT = 256;

/** A user-authored keybinding entry from `~/.hoopoe/keybindings.json`. */
export interface KeybindingRule {
  readonly key: string;
  readonly command: string;
  readonly when?: string;
}

/** A parsed key combination — `cmd+shift+p` decomposed into modifier flags
 * and the trailing literal key. */
export interface KeybindingShortcut {
  readonly key: string;
  readonly metaKey: boolean;
  readonly ctrlKey: boolean;
  readonly shiftKey: boolean;
  readonly altKey: boolean;
  /** `mod` is platform-dependent — meta on macOS, ctrl elsewhere. Recorded
   * separately from concrete `metaKey` / `ctrlKey` so platform resolution
   * happens at evaluation time, not at parse time. */
  readonly modKey: boolean;
}

/** AST node for a parsed `when` clause. The grammar is:
 *   expr   = or
 *   or     = and ('||' and)*
 *   and    = unary ('&&' unary)*
 *   unary  = '!'* primary
 *   primary= identifier | '(' or ')' */
export type KeybindingWhenNode =
  | { readonly type: "identifier"; readonly name: string }
  | { readonly type: "not"; readonly node: KeybindingWhenNode }
  | { readonly type: "and"; readonly left: KeybindingWhenNode; readonly right: KeybindingWhenNode }
  | { readonly type: "or"; readonly left: KeybindingWhenNode; readonly right: KeybindingWhenNode };

/** Compiled keybinding ready for dispatch — shortcut decomposed, AST built. */
export interface ResolvedKeybindingRule {
  readonly command: string;
  readonly shortcut: KeybindingShortcut;
  readonly whenAst?: KeybindingWhenNode;
}

export type ResolvedKeybindingsConfig = ReadonlyArray<ResolvedKeybindingRule>;
export type KeybindingsConfig = ReadonlyArray<KeybindingRule>;
