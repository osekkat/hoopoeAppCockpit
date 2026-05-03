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
      return;
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
        aria-label="Command palette"
        aria-modal="true"
        className="hh-command-palette"
        role="dialog"
      >
        <header className="hh-command-palette-header">
          <h2 className="hh-command-palette-title">Command palette</h2>
          <button
            aria-label="Close command palette"
            className="hh-command-palette-close"
            onClick={onClose}
            type="button"
          >
            Close
          </button>
        </header>
        <input
          aria-label="Search commands"
          className="hh-command-palette-input"
          onChange={(event) => setQuery(event.currentTarget.value)}
          placeholder={model.placeholder}
          ref={inputRef}
          type="search"
          value={query}
        />
        <div
          aria-label="Matched commands"
          className="hh-command-palette-list"
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
