// Originally from github.com/pingdotgg/t3code (MIT License)
// Copyright (c) 2026 T3 Tools Inc.
// Adapted for Hoopoe.
//
// Full MIT license text: vendored/t3code/LICENSE
//
// Lifted from t3code `apps/web/src/keybindings.ts` lines 119–209:
// `evaluateWhenNode`, `matchesWhenClause`, `matchesShortcut`,
// `resolveShortcutCommand`. Browser-isms are removed: `navigator.platform`
// is replaced by an explicit `platform` argument so the same code works in
// Electron's main process and the renderer.

import type {
  KeybindingShortcut,
  KeybindingWhenNode,
  ResolvedKeybindingRule,
  ResolvedKeybindingsConfig,
} from "./types.ts";

export interface ShortcutContext {
  readonly [key: string]: boolean | undefined;
}

export interface ShortcutEventLike {
  readonly key: string;
  readonly metaKey: boolean;
  readonly ctrlKey: boolean;
  readonly shiftKey: boolean;
  readonly altKey: boolean;
}

const isMacPlatform = (platform: string): boolean =>
  platform === "darwin" || platform.toLowerCase().startsWith("mac");

export function evaluateWhenNode(
  node: KeybindingWhenNode,
  context: ShortcutContext,
): boolean {
  switch (node.type) {
    case "identifier":
      if (node.name === "true") return true;
      if (node.name === "false") return false;
      return Boolean(context[node.name]);
    case "not":
      return !evaluateWhenNode(node.node, context);
    case "and":
      return (
        evaluateWhenNode(node.left, context) && evaluateWhenNode(node.right, context)
      );
    case "or":
      return (
        evaluateWhenNode(node.left, context) || evaluateWhenNode(node.right, context)
      );
  }
}

export function matchesWhenClause(
  whenAst: KeybindingWhenNode | undefined,
  context: ShortcutContext,
): boolean {
  if (!whenAst) return true;
  return evaluateWhenNode(whenAst, context);
}

export function matchesShortcut(
  event: ShortcutEventLike,
  shortcut: KeybindingShortcut,
  platform: string,
): boolean {
  const useMetaForMod = isMacPlatform(platform);
  const expectedMeta = shortcut.metaKey || (shortcut.modKey && useMetaForMod);
  const expectedCtrl = shortcut.ctrlKey || (shortcut.modKey && !useMetaForMod);
  if (event.metaKey !== expectedMeta) return false;
  if (event.ctrlKey !== expectedCtrl) return false;
  if (event.shiftKey !== shortcut.shiftKey) return false;
  if (event.altKey !== shortcut.altKey) return false;
  return event.key.toLowerCase() === shortcut.key;
}

/**
 * Iterate the resolved rules in REVERSE order so the LAST rule wins on a
 * conflict (this is the canonical "user override" semantics — entries later
 * in the file shadow earlier ones). Skip rules whose `when` is unsatisfied.
 */
export function resolveShortcutCommand(
  event: ShortcutEventLike,
  keybindings: ResolvedKeybindingsConfig,
  context: ShortcutContext,
  platform: string,
): string | null {
  for (let index = keybindings.length - 1; index >= 0; index -= 1) {
    const binding: ResolvedKeybindingRule | undefined = keybindings[index];
    if (!binding) continue;
    if (!matchesWhenClause(binding.whenAst, context)) continue;
    if (!matchesShortcut(event, binding.shortcut, platform)) continue;
    return binding.command;
  }
  return null;
}
