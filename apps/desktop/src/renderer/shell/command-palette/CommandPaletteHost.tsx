// Hoopoe-owned. React host for the design-system CommandPalette primitive.
//
// The design-system component (`@hoopoe/design-system`) ships pure model
// functions (`getCommandPaletteModel`, `moveCommandPaletteSelection`) plus
// a vanilla DOM renderer. We reuse the model functions — which carry the
// fuzzy ranking, when-clause filtering, and recent-command logic — and
// render the JSX ourselves so React stays in charge of focus, keystrokes,
// and lifecycle.

import {
  getCommandPaletteModel,
  moveCommandPaletteSelection,
  type CommandPaletteCommand,
  type CommandPaletteItemModel,
  type CommandPaletteMatchRange,
} from "@hoopoe/design-system";
import { useNavigate } from "@tanstack/react-router";
import * as React from "react";
import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import {
  buildShellCommands,
  buildShellPaletteContext,
  SHELL_PALETTE_CONTEXT_KEYS,
  type ShellCommand,
  type ShellCommandContext,
} from "./commands.ts";
import { routeForStage } from "../../topbar/project-switcher-model.ts";
import { stageForPathname, type ShellRouteId } from "../../stages.ts";
import { useShellUiStore } from "../../store.ts";

const RECENT_LIMIT = 5;
const VISIBLE_RESULT_LIMIT = 8;

// Stable DOM ids so the search input's `aria-activedescendant` can point
// at exactly the option the model considers active (review-finding p3:
// "CommandPalette host lacks modal focus containment"). Screen readers
// announce the active option as the user arrows up/down.
const COMMAND_PALETTE_LISTBOX_ID = "hh-command-palette-listbox";
const COMMAND_PALETTE_LABEL_ID = "hh-command-palette-title";
const optionIdForCommand = (commandId: string): string =>
  `hh-command-palette-option-${commandId.replace(/[^a-zA-Z0-9_-]/g, "_")}`;

export interface CommandPaletteHostProps {
  readonly open: boolean;
  readonly projectId: string | undefined;
  readonly pathname: string;
  readonly onClose: () => void;
}

export function CommandPaletteHost({
  open,
  projectId,
  pathname,
  onClose,
}: CommandPaletteHostProps): React.ReactElement | null {
  const navigate = useNavigate();
  const activityPanelOpen = useShellUiStore((state) => state.activityPanelOpen);
  const setActivityPanelOpen = useShellUiStore((state) => state.setActivityPanelOpen);
  const toggleActivityPanel = useShellUiStore((state) => state.toggleActivityPanel);
  const setProjectSwitcherOpen = useShellUiStore((state) => state.setProjectSwitcherOpen);
  const recordRecentCommand = useShellUiStore((state) => state.recordRecentCommand);
  const recentCommandIds = useShellUiStore((state) => state.recentCommandIds);

  const [query, setQuery] = useState("");
  const [activeCommandId, setActiveCommandId] = useState<string | null>(null);
  const inputRef = useRef<HTMLInputElement | null>(null);
  // Trigger element captured at open time so we can restore focus on close
  // (review-finding p3: "CommandPalette host lacks modal focus containment").
  // Without this, Escape returns the user to document.body and screen readers
  // lose place.
  const triggerRef = useRef<HTMLElement | null>(null);
  // Modal subtree root — referenced by the focus-trap logic to discover
  // the first/last focusable descendants on Tab/Shift+Tab.
  const modalRef = useRef<HTMLElement | null>(null);
  const closeButtonRef = useRef<HTMLButtonElement | null>(null);

  const commands = useMemo<readonly ShellCommand[]>(() => buildShellCommands(), []);
  const activeStageId: ShellRouteId | undefined = useMemo(
    () => stageForPathname(pathname)?.id,
    [pathname],
  );
  const context = useMemo(
    () =>
      buildShellPaletteContext({
        projectId,
        activeStageId,
        activityPanelOpen,
      }),
    [activeStageId, activityPanelOpen, projectId],
  );

  const model = useMemo(
    () =>
      getCommandPaletteModel({
        commands,
        query,
        context,
        knownContextKeys: SHELL_PALETTE_CONTEXT_KEYS,
        recentCommandIds,
        activeCommandId,
        maxResults: VISIBLE_RESULT_LIMIT,
        placeholder: "Search commands",
      }),
    [activeCommandId, commands, context, query, recentCommandIds],
  );

  useEffect(() => {
    if (!open) {
      setQuery("");
      setActiveCommandId(null);
      // Restore focus to whatever invoked the palette (typically the
      // top-bar Command button). Guard against the trigger having been
      // removed from the DOM in the interim.
      const trigger = triggerRef.current;
      triggerRef.current = null;
      if (
        trigger &&
        typeof trigger.focus === "function" &&
        document.contains(trigger)
      ) {
        trigger.focus();
      }
      return;
    }
    // Capture the active element AT OPEN TIME so Escape (or selection)
    // returns focus to the right control.
    const activeOnOpen = document.activeElement;
    if (
      activeOnOpen instanceof HTMLElement &&
      typeof activeOnOpen.focus === "function"
    ) {
      triggerRef.current = activeOnOpen;
    }
    const id = window.requestAnimationFrame(() => {
      inputRef.current?.focus();
      inputRef.current?.select();
    });
    return () => window.cancelAnimationFrame(id);
  }, [open]);

  useEffect(() => {
    if (model.activeCommand === null) {
      if (activeCommandId !== null) setActiveCommandId(null);
    } else if (activeCommandId !== model.activeCommand.id) {
      setActiveCommandId(model.activeCommand.id);
    }
  }, [activeCommandId, model.activeCommand]);

  const executionContext = useMemo<ShellCommandContext>(
    () => ({
      projectId,
      navigateToStage: (targetProjectId, stageId) => {
        navigate({
          to: routeForStage(stageId),
          params: { projectId: targetProjectId },
        });
      },
      openProjectPicker: () => setProjectSwitcherOpen(true),
      toggleActivityPanel,
      setActivityPanelOpen,
      closeCommandPalette: onClose,
    }),
    [
      navigate,
      onClose,
      projectId,
      setActivityPanelOpen,
      setProjectSwitcherOpen,
      toggleActivityPanel,
    ],
  );

  const executeCommand = useCallback(
    (command: CommandPaletteCommand) => {
      const shellCommand = commands.find((entry) => entry.id === command.id);
      if (!shellCommand) return;
      recordRecentCommand(shellCommand.id);
      shellCommand.execute(executionContext);
    },
    [commands, executionContext, recordRecentCommand],
  );

  const onKeyDown = useCallback(
    (event: React.KeyboardEvent<HTMLDivElement>) => {
      if (event.key === "Escape") {
        event.preventDefault();
        onClose();
        return;
      }
      if (event.key === "ArrowDown") {
        event.preventDefault();
        const next = moveCommandPaletteSelection(model, "next");
        if (next !== null) setActiveCommandId(next);
        return;
      }
      if (event.key === "ArrowUp") {
        event.preventDefault();
        const previous = moveCommandPaletteSelection(model, "previous");
        if (previous !== null) setActiveCommandId(previous);
        return;
      }
      if (event.key === "Enter" && model.activeCommand !== null) {
        event.preventDefault();
        executeCommand(model.activeCommand);
        return;
      }
      // Focus trap: Tab and Shift+Tab cycle within the modal. Without
      // this, native Tab order takes the user back into the underlying
      // app surface even though the dialog is `aria-modal="true"` —
      // screen readers and keyboard users would experience a broken
      // modal contract. Native cycling works between input and the
      // focused option button + Close button; we manually wrap the
      // edges (first→last on Shift+Tab, last→first on Tab).
      if (event.key !== "Tab") return;
      const root = modalRef.current;
      if (!root) return;
      const focusables = collectFocusable(root);
      if (focusables.length === 0) return;
      const first = focusables[0]!;
      const last = focusables[focusables.length - 1]!;
      const active = document.activeElement;
      if (event.shiftKey) {
        if (active === first || !root.contains(active)) {
          event.preventDefault();
          last.focus();
        }
      } else {
        if (active === last || !root.contains(active)) {
          event.preventDefault();
          first.focus();
        }
      }
    },
    [executeCommand, model, onClose],
  );

  if (!open) return null;

  return (
    <div
      aria-hidden={false}
      className="hh-command-palette-overlay"
      data-open="true"
      onKeyDown={onKeyDown}
    >
      <button
        aria-label="Close command palette"
        className="hh-command-palette-backdrop"
        onClick={onClose}
        tabIndex={-1}
        type="button"
      />
      <section
        aria-labelledby={COMMAND_PALETTE_LABEL_ID}
        aria-modal="true"
        className="hh-command-palette"
        ref={modalRef}
        role="dialog"
      >
        <header className="hh-command-palette-header">
          <h2 className="hh-command-palette-title" id={COMMAND_PALETTE_LABEL_ID}>
            Command palette
          </h2>
          <button
            aria-label="Close command palette"
            className="hh-command-palette-close"
            onClick={onClose}
            ref={closeButtonRef}
            type="button"
          >
            Close
          </button>
        </header>
        <input
          aria-activedescendant={
            model.activeCommand !== null
              ? optionIdForCommand(model.activeCommand.id)
              : undefined
          }
          aria-autocomplete="list"
          aria-controls={COMMAND_PALETTE_LISTBOX_ID}
          aria-label="Search commands"
          className="hh-command-palette-input"
          onChange={(event) => setQuery(event.currentTarget.value)}
          placeholder={model.placeholder}
          ref={inputRef}
          role="combobox"
          aria-expanded={model.items.length > 0}
          type="search"
          value={query}
        />
        <div
          aria-label="Matched commands"
          className="hh-command-palette-list"
          id={COMMAND_PALETTE_LISTBOX_ID}
          role="listbox"
        >
          {model.emptyState !== null ? (
            <div className="hh-command-palette-empty">
              {model.emptyState === "no-commands"
                ? "No commands registered"
                : "No commands match this context"}
            </div>
          ) : (
            model.items.map((item) => (
              <CommandPaletteItem
                item={item}
                key={item.command.id}
                onExecute={executeCommand}
                onPointerEnter={setActiveCommandId}
              />
            ))
          )}
        </div>
      </section>
    </div>
  );
}

interface CommandPaletteItemProps {
  readonly item: CommandPaletteItemModel;
  readonly onExecute: (command: CommandPaletteCommand) => void;
  readonly onPointerEnter: (commandId: string) => void;
}

function CommandPaletteItem({ item, onExecute, onPointerEnter }: CommandPaletteItemProps) {
  return (
    <button
      aria-selected={item.active}
      className="hh-command-palette-item"
      data-active={item.active}
      data-command-id={item.command.id}
      id={optionIdForCommand(item.command.id)}
      onClick={() => onExecute(item.command)}
      onPointerEnter={() => onPointerEnter(item.command.id)}
      role="option"
      type="button"
    >
      <span className="hh-command-palette-item-main">
        <span className="hh-command-palette-item-title">
          {renderHighlighted(item.command.title, item.titleMatchRanges)}
        </span>
        <span className="hh-command-palette-item-description">
          {item.command.description ?? item.command.id}
        </span>
      </span>
      <span className="hh-command-palette-item-meta">
        <span className="hh-command-palette-item-category">{item.command.category}</span>
        {item.command.defaultKeybinding ? (
          <kbd className="hh-command-palette-item-kbd">{item.command.defaultKeybinding}</kbd>
        ) : null}
      </span>
    </button>
  );
}

function renderHighlighted(
  text: string,
  ranges: readonly CommandPaletteMatchRange[],
): React.ReactElement[] {
  if (ranges.length === 0) {
    return [<span key="text">{text}</span>];
  }

  const merged = [...ranges].toSorted((a, b) => a.start - b.start);
  const fragments: React.ReactElement[] = [];
  let cursor = 0;

  merged.forEach((range, index) => {
    if (range.start > cursor) {
      fragments.push(<span key={`text-${index}`}>{text.slice(cursor, range.start)}</span>);
    }
    fragments.push(
      <mark className="hh-command-palette-mark" key={`mark-${index}`}>
        {text.slice(range.start, range.end)}
      </mark>,
    );
    cursor = range.end;
  });

  if (cursor < text.length) {
    fragments.push(<span key="text-tail">{text.slice(cursor)}</span>);
  }

  return fragments;
}

export const COMMAND_PALETTE_RECENT_LIMIT = RECENT_LIMIT;

/** Walk the modal subtree for elements that should receive Tab focus.
 *  Mirrors the WAI-ARIA "tabbable" definition: anchors / buttons / inputs /
 *  selects / textareas / `[tabindex]` not equal to `-1`, excluding `disabled`
 *  controls. Used by the focus trap to determine the wrap targets on
 *  Tab / Shift+Tab. Pure DOM read; no side effects. */
function collectFocusable(root: HTMLElement): HTMLElement[] {
  const selector = [
    "a[href]",
    "button:not([disabled])",
    "input:not([disabled])",
    "select:not([disabled])",
    "textarea:not([disabled])",
    "[tabindex]",
  ].join(",");
  const all = Array.from(root.querySelectorAll<HTMLElement>(selector));
  return all.filter((node) => {
    if (node.hasAttribute("disabled")) return false;
    const tabIndexAttr = node.getAttribute("tabindex");
    if (tabIndexAttr !== null && Number.parseInt(tabIndexAttr, 10) < 0) {
      return false;
    }
    if (node.getAttribute("aria-hidden") === "true") return false;
    return true;
  });
}
