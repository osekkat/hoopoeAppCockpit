import { hoopoeTokens, statusTones } from "../../tokens/index.ts";

export type CommandPaletteCategory =
  | "Project"
  | "Plan"
  | "Beads"
  | "Swarm"
  | "Activity"
  | "Diagnostics"
  | "Help"
  | "Window";

export interface CommandPaletteCommand {
  readonly id: string;
  readonly title: string;
  readonly category: CommandPaletteCategory;
  readonly description?: string;
  readonly defaultKeybinding?: string;
  readonly whenContextKeys?: readonly string[];
}

export interface CommandPaletteProps {
  readonly commands: readonly CommandPaletteCommand[];
  readonly query: string;
  readonly context: Readonly<Record<string, boolean>>;
  readonly knownContextKeys?: readonly string[];
  readonly recentCommandIds?: readonly string[];
  readonly activeCommandId?: string | null;
  readonly maxResults?: number;
  readonly placeholder?: string;
  readonly onQueryChange?: (query: string) => void;
  readonly onExecute?: (command: CommandPaletteCommand, event: MouseEvent | KeyboardEvent) => void;
  readonly onClose?: (event: MouseEvent | KeyboardEvent) => void;
}

export interface CommandPaletteMatchRange {
  readonly start: number;
  readonly end: number;
}

export interface CommandPaletteItemModel {
  readonly command: CommandPaletteCommand;
  readonly score: number;
  readonly recentRank: number | null;
  readonly active: boolean;
  readonly titleMatchRanges: readonly CommandPaletteMatchRange[];
  readonly idMatchRanges: readonly CommandPaletteMatchRange[];
  readonly descriptionMatchRanges: readonly CommandPaletteMatchRange[];
}

export interface CommandPaletteModel {
  readonly query: string;
  readonly normalizedQuery: string;
  readonly placeholder: string;
  readonly items: readonly CommandPaletteItemModel[];
  readonly activeCommand: CommandPaletteCommand | null;
  readonly emptyState: "no-commands" | "no-matches" | null;
  readonly filteredCommandIds: readonly string[];
  readonly className: string;
  readonly style: Readonly<Record<string, string>>;
}

export class UnknownCommandPaletteContextKeyError extends Error {
  readonly contextKey: string;

  constructor(contextKey: string) {
    super(`Unknown command-palette context key: ${contextKey}`);
    this.name = "UnknownCommandPaletteContextKeyError";
    this.contextKey = contextKey;
  }
}

export function getCommandPaletteModel(props: CommandPaletteProps): CommandPaletteModel {
  const query = props.query.trim();
  const normalizedQuery = normalize(query);
  const knownKeys = new Set(props.knownContextKeys ?? Object.keys(props.context));
  const recentRankById = new Map(
    (props.recentCommandIds ?? []).map((id, index) => [id, index] as const),
  );
  const visibleCommands: CommandPaletteCommand[] = [];
  const filteredCommandIds: string[] = [];

  for (const command of props.commands) {
    if (isCommandEnabled(command, props.context, knownKeys)) {
      visibleCommands.push(command);
    } else {
      filteredCommandIds.push(command.id);
    }
  }

  const items = rankCommands(visibleCommands, normalizedQuery, recentRankById)
    .slice(0, props.maxResults ?? 12)
    .map((item, index) => ({
      command: item.command,
      score: item.score,
      recentRank: item.recentRank,
      titleMatchRanges: item.titleMatchRanges,
      idMatchRanges: item.idMatchRanges,
      descriptionMatchRanges: item.descriptionMatchRanges,
      active:
        props.activeCommandId === undefined || props.activeCommandId === null
          ? index === 0
          : item.command.id === props.activeCommandId,
    }));

  const activeCommand = items.find((item) => item.active)?.command ?? null;
  const emptyState =
    props.commands.length === 0 ? "no-commands" : items.length === 0 ? "no-matches" : null;

  return {
    query: props.query,
    normalizedQuery,
    placeholder: props.placeholder ?? "Search commands",
    items,
    activeCommand,
    emptyState,
    filteredCommandIds,
    className: "hoopoe-command-palette",
    style: commandPaletteStyle(),
  };
}

export function moveCommandPaletteSelection(
  model: CommandPaletteModel,
  direction: "next" | "previous",
): string | null {
  if (model.items.length === 0) {
    return null;
  }

  const activeIndex = model.items.findIndex((item) => item.active);
  const fallbackIndex = direction === "next" ? 0 : model.items.length - 1;
  const currentIndex = activeIndex === -1 ? fallbackIndex : activeIndex;
  const nextIndex =
    direction === "next"
      ? (currentIndex + 1) % model.items.length
      : (currentIndex - 1 + model.items.length) % model.items.length;

  return model.items[nextIndex]?.command.id ?? null;
}

export function renderCommandPaletteElement(
  props: CommandPaletteProps,
  ownerDocument: Document = document,
): HTMLElement {
  const model = getCommandPaletteModel(props);
  const shell = ownerDocument.createElement("section");
  const panel = ownerDocument.createElement("div");
  const header = ownerDocument.createElement("header");
  const title = ownerDocument.createElement("h2");
  const closeButton = ownerDocument.createElement("button");
  const input = ownerDocument.createElement("input");
  const list = ownerDocument.createElement("div");

  shell.className = model.className;
  shell.setAttribute("role", "dialog");
  shell.setAttribute("aria-modal", "true");
  shell.setAttribute("aria-label", "Command palette");
  assignStyle(shell, model.style);

  assignStyle(panel, panelStyle());

  header.style.display = "grid";
  header.style.gridTemplateColumns = "1fr auto";
  header.style.alignItems = "center";
  header.style.gap = hoopoeTokens.spacing[3];

  title.textContent = "Command Palette";
  assignStyle(title, headingStyle());

  closeButton.type = "button";
  closeButton.textContent = "Close";
  closeButton.setAttribute("aria-label", "Close command palette");
  assignStyle(closeButton, quietButtonStyle());
  closeButton.addEventListener("click", (event) => props.onClose?.(event));

  input.type = "search";
  input.value = model.query;
  input.placeholder = model.placeholder;
  input.setAttribute("aria-label", "Search commands");
  assignStyle(input, searchInputStyle());
  input.addEventListener("input", (event) => {
    props.onQueryChange?.((event.currentTarget as HTMLInputElement).value);
  });
  input.addEventListener("keydown", (event) => {
    if (event.key === "Escape") {
      props.onClose?.(event);
    }
    if (event.key === "Enter" && model.activeCommand !== null) {
      props.onExecute?.(model.activeCommand, event);
    }
  });

  list.setAttribute("role", "listbox");
  list.setAttribute("aria-label", "Matched commands");
  assignStyle(list, listStyle());

  if (model.emptyState !== null) {
    list.append(renderEmptyState(model.emptyState, ownerDocument));
  } else {
    for (const item of model.items) {
      list.append(renderCommandItem(item, props.onExecute, ownerDocument));
    }
  }

  header.append(title, closeButton);
  panel.append(header, input, list);
  shell.append(panel);

  return shell;
}

function isCommandEnabled(
  command: CommandPaletteCommand,
  context: Readonly<Record<string, boolean>>,
  knownKeys: ReadonlySet<string>,
): boolean {
  for (const key of command.whenContextKeys ?? []) {
    if (!knownKeys.has(key)) {
      throw new UnknownCommandPaletteContextKeyError(key);
    }

    if (context[key] !== true) {
      return false;
    }
  }

  return true;
}

function rankCommands(
  commands: readonly CommandPaletteCommand[],
  normalizedQuery: string,
  recentRankById: ReadonlyMap<string, number>,
): readonly Omit<CommandPaletteItemModel, "active">[] {
  const ranked: Omit<CommandPaletteItemModel, "active">[] = [];

  for (const command of commands) {
    const recentRank = recentRankById.get(command.id) ?? null;
    const match = getBestCommandMatch(command, normalizedQuery);

    if (normalizedQuery !== "" && match.score === 0) {
      continue;
    }

    ranked.push({
      command,
      score: match.score + recentScoreBonus(recentRank),
      recentRank,
      titleMatchRanges: match.titleMatchRanges,
      idMatchRanges: match.idMatchRanges,
      descriptionMatchRanges: match.descriptionMatchRanges,
    });
  }

  return ranked.toSorted(compareCommandItems);
}

function getBestCommandMatch(
  command: CommandPaletteCommand,
  normalizedQuery: string,
): Pick<
  CommandPaletteItemModel,
  "score" | "titleMatchRanges" | "idMatchRanges" | "descriptionMatchRanges"
> {
  if (normalizedQuery === "") {
    return {
      score: 0,
      titleMatchRanges: [],
      idMatchRanges: [],
      descriptionMatchRanges: [],
    };
  }

  const titleMatch = fuzzyMatch(normalizedQuery, command.title);
  const idMatch = fuzzyMatch(normalizedQuery, command.id);
  const descriptionMatch = fuzzyMatch(normalizedQuery, command.description ?? "");
  const titleScore = weightedMatchScore(titleMatch.score, 80);
  const idScore = weightedMatchScore(idMatch.score, 60);
  const descriptionScore = weightedMatchScore(descriptionMatch.score, 20);

  return {
    score: Math.max(titleScore, idScore, descriptionScore),
    titleMatchRanges: titleMatch.ranges,
    idMatchRanges: idMatch.ranges,
    descriptionMatchRanges: descriptionMatch.ranges,
  };
}

function weightedMatchScore(score: number, weight: number): number {
  return score === 0 ? 0 : score + weight;
}

function fuzzyMatch(
  normalizedQuery: string,
  value: string,
): { readonly score: number; readonly ranges: readonly CommandPaletteMatchRange[] } {
  const normalized = normalizeWithIndexMap(value);
  const normalizedValue = normalized.value;

  if (normalizedQuery === "" || normalizedValue === "") {
    return { score: 0, ranges: [] };
  }

  const exactIndex = normalizedValue.indexOf(normalizedQuery);
  if (exactIndex !== -1) {
    const score = exactIndex === 0 ? 900 : 700 - exactIndex;
    const originalStart = normalized.indexMap[exactIndex] ?? 0;
    const originalEnd =
      (normalized.indexMap[exactIndex + normalizedQuery.length - 1] ?? originalStart) + 1;

    return {
      score,
      ranges: [{ start: originalStart, end: originalEnd }],
    };
  }

  const ranges: CommandPaletteMatchRange[] = [];
  let queryIndex = 0;
  let firstMatch = -1;
  let lastMatch = -1;
  let gaps = 0;

  for (let valueIndex = 0; valueIndex < normalizedValue.length; valueIndex += 1) {
    if (normalizedValue[valueIndex] !== normalizedQuery[queryIndex]) {
      continue;
    }

    if (firstMatch === -1) {
      firstMatch = valueIndex;
    }
    if (lastMatch !== -1 && valueIndex > lastMatch + 1) {
      gaps += valueIndex - lastMatch - 1;
    }

    const originalIndex = normalized.indexMap[valueIndex] ?? valueIndex;
    ranges.push({ start: originalIndex, end: originalIndex + 1 });
    lastMatch = valueIndex;
    queryIndex += 1;

    if (queryIndex === normalizedQuery.length) {
      const compactness = Math.max(0, 180 - gaps * 8);
      const prefixBonus = firstMatch === 0 ? 90 : Math.max(0, 40 - firstMatch);
      return { score: compactness + prefixBonus, ranges };
    }
  }

  return { score: 0, ranges: [] };
}

function compareCommandItems(
  a: Omit<CommandPaletteItemModel, "active">,
  b: Omit<CommandPaletteItemModel, "active">,
): number {
  const scoreDelta = b.score - a.score;
  if (scoreDelta !== 0) {
    return scoreDelta;
  }

  const aRecent = a.recentRank ?? Number.MAX_SAFE_INTEGER;
  const bRecent = b.recentRank ?? Number.MAX_SAFE_INTEGER;
  if (aRecent !== bRecent) {
    return aRecent - bRecent;
  }

  const categoryDelta = a.command.category.localeCompare(b.command.category);
  if (categoryDelta !== 0) {
    return categoryDelta;
  }

  return a.command.title.localeCompare(b.command.title);
}

function recentScoreBonus(recentRank: number | null): number {
  return recentRank === null ? 0 : Math.max(0, 120 - recentRank * 20);
}

function renderCommandItem(
  item: CommandPaletteItemModel,
  onExecute: CommandPaletteProps["onExecute"],
  ownerDocument: Document,
): HTMLElement {
  const button = ownerDocument.createElement("button");
  const main = ownerDocument.createElement("span");
  const title = ownerDocument.createElement("span");
  const description = ownerDocument.createElement("span");
  const meta = ownerDocument.createElement("span");
  const category = ownerDocument.createElement("span");
  const keybinding = ownerDocument.createElement("kbd");

  button.type = "button";
  button.dataset.commandId = item.command.id;
  button.setAttribute("role", "option");
  button.setAttribute("aria-selected", item.active ? "true" : "false");
  assignStyle(button, itemStyle(item.active));
  button.addEventListener("click", (event) => onExecute?.(item.command, event));

  assignStyle(main, itemMainStyle());
  assignStyle(title, itemTitleStyle());
  appendHighlightedText(title, item.command.title, item.titleMatchRanges, ownerDocument);

  description.textContent = item.command.description ?? item.command.id;
  assignStyle(description, itemDescriptionStyle());

  category.textContent = item.command.category;
  assignStyle(category, categoryStyle());

  keybinding.textContent = item.command.defaultKeybinding ?? "";
  keybinding.hidden = item.command.defaultKeybinding === undefined;
  assignStyle(keybinding, keybindingStyle());

  meta.append(category, keybinding);
  assignStyle(meta, itemMetaStyle());

  main.append(title, description);
  button.append(main, meta);

  return button;
}

function renderEmptyState(
  emptyState: NonNullable<CommandPaletteModel["emptyState"]>,
  ownerDocument: Document,
): HTMLElement {
  const wrapper = ownerDocument.createElement("div");
  wrapper.textContent =
    emptyState === "no-commands" ? "No commands registered" : "No commands match this context";
  assignStyle(wrapper, emptyStateStyle());
  return wrapper;
}

function appendHighlightedText(
  element: HTMLElement,
  text: string,
  ranges: readonly CommandPaletteMatchRange[],
  ownerDocument: Document,
): void {
  if (ranges.length === 0) {
    element.textContent = text;
    return;
  }

  const mergedRanges = mergeRanges(ranges);
  let cursor = 0;

  for (const range of mergedRanges) {
    if (range.start > cursor) {
      element.append(ownerDocument.createTextNode(text.slice(cursor, range.start)));
    }

    const mark = ownerDocument.createElement("mark");
    mark.textContent = text.slice(range.start, range.end);
    assignStyle(mark, markStyle());
    element.append(mark);
    cursor = range.end;
  }

  if (cursor < text.length) {
    element.append(ownerDocument.createTextNode(text.slice(cursor)));
  }
}

function mergeRanges(
  ranges: readonly CommandPaletteMatchRange[],
): readonly CommandPaletteMatchRange[] {
  const sorted = [...ranges].toSorted((a, b) => a.start - b.start);
  const merged: CommandPaletteMatchRange[] = [];

  for (const range of sorted) {
    const last = merged.at(-1);

    if (last === undefined || range.start > last.end) {
      merged.push(range);
      continue;
    }

    merged[merged.length - 1] = {
      start: last.start,
      end: Math.max(last.end, range.end),
    };
  }

  return merged;
}

function normalize(value: string): string {
  return value.toLowerCase().replace(/\s+/g, "");
}

function normalizeWithIndexMap(value: string): {
  readonly value: string;
  readonly indexMap: readonly number[];
} {
  let normalized = "";
  const indexMap: number[] = [];

  for (let index = 0; index < value.length; index += 1) {
    const character = value[index] ?? "";

    if (/\s/.test(character)) {
      continue;
    }

    normalized += character.toLowerCase();
    indexMap.push(index);
  }

  return { value: normalized, indexMap };
}

function commandPaletteStyle(): Readonly<Record<string, string>> {
  return {
    position: "fixed",
    inset: "0",
    display: "grid",
    placeItems: "start center",
    padding: "72px 24px 24px",
    background: "rgba(15, 15, 17, 0.62)",
    color: hoopoeTokens.color.surface.dark.text,
    fontFamily: hoopoeTokens.typography.sans.join(", "),
    letterSpacing: "0",
  };
}

function panelStyle(): Readonly<Record<string, string>> {
  return {
    display: "grid",
    gap: hoopoeTokens.spacing[3],
    width: "min(720px, 100%)",
    padding: hoopoeTokens.spacing[3],
    borderRadius: hoopoeTokens.radius.lg,
    border: `1px solid ${hoopoeTokens.color.surface.dark.border}`,
    background: hoopoeTokens.color.surface.dark.panel,
    boxShadow: hoopoeTokens.shadow.glass,
  };
}

function headingStyle(): Readonly<Record<string, string>> {
  return {
    margin: "0",
    color: hoopoeTokens.color.surface.dark.text,
    fontSize: "14px",
    fontWeight: "750",
    lineHeight: "1.2",
    letterSpacing: "0",
  };
}

function quietButtonStyle(): Readonly<Record<string, string>> {
  return {
    minHeight: "28px",
    padding: "4px 8px",
    borderRadius: hoopoeTokens.radius.md,
    border: `1px solid ${hoopoeTokens.color.surface.dark.borderSoft}`,
    background: "transparent",
    color: hoopoeTokens.color.surface.dark.textDim,
    font: "inherit",
    fontSize: "12px",
    fontWeight: "650",
    lineHeight: "1.2",
    cursor: "pointer",
  };
}

function searchInputStyle(): Readonly<Record<string, string>> {
  return {
    width: "100%",
    minHeight: "42px",
    padding: "8px 12px",
    borderRadius: hoopoeTokens.radius.lg,
    border: `1px solid ${hoopoeTokens.color.surface.dark.border}`,
    background: hoopoeTokens.color.surface.dark.panelAlt,
    color: hoopoeTokens.color.surface.dark.text,
    font: "inherit",
    fontSize: "15px",
    lineHeight: "1.2",
    letterSpacing: "0",
    outline: "none",
  };
}

function listStyle(): Readonly<Record<string, string>> {
  return {
    display: "grid",
    gap: hoopoeTokens.spacing[1],
    maxHeight: "440px",
    overflow: "auto",
  };
}

function itemStyle(active: boolean): Readonly<Record<string, string>> {
  return {
    display: "grid",
    gridTemplateColumns: "minmax(0, 1fr) auto",
    alignItems: "center",
    gap: hoopoeTokens.spacing[3],
    width: "100%",
    minHeight: "56px",
    padding: "8px 10px",
    borderRadius: hoopoeTokens.radius.lg,
    border: `1px solid ${active ? statusTones.ready.border : "transparent"}`,
    background: active ? statusTones.ready.bg : "transparent",
    color: active ? statusTones.ready.fg : hoopoeTokens.color.surface.dark.text,
    font: "inherit",
    textAlign: "left",
    cursor: "pointer",
  };
}

function itemMainStyle(): Readonly<Record<string, string>> {
  return {
    display: "grid",
    gap: hoopoeTokens.spacing[0.5],
    minWidth: "0",
  };
}

function itemTitleStyle(): Readonly<Record<string, string>> {
  return {
    color: "inherit",
    fontSize: "13px",
    fontWeight: "750",
    lineHeight: "1.25",
    letterSpacing: "0",
    overflow: "hidden",
    textOverflow: "ellipsis",
    whiteSpace: "nowrap",
  };
}

function itemDescriptionStyle(): Readonly<Record<string, string>> {
  return {
    color: hoopoeTokens.color.surface.dark.textDim,
    fontSize: "12px",
    fontWeight: "500",
    lineHeight: "1.3",
    letterSpacing: "0",
    overflow: "hidden",
    textOverflow: "ellipsis",
    whiteSpace: "nowrap",
  };
}

function itemMetaStyle(): Readonly<Record<string, string>> {
  return {
    display: "inline-flex",
    alignItems: "center",
    justifyContent: "end",
    gap: hoopoeTokens.spacing[2],
    minWidth: "0",
  };
}

function categoryStyle(): Readonly<Record<string, string>> {
  return {
    color: hoopoeTokens.color.surface.dark.textDim,
    fontSize: "11px",
    fontWeight: "700",
    lineHeight: "1",
    letterSpacing: "0",
    whiteSpace: "nowrap",
  };
}

function keybindingStyle(): Readonly<Record<string, string>> {
  return {
    display: "inline-grid",
    placeItems: "center",
    minHeight: "22px",
    minWidth: "48px",
    padding: "2px 6px",
    borderRadius: hoopoeTokens.radius.md,
    border: `1px solid ${hoopoeTokens.color.surface.dark.borderSoft}`,
    background: hoopoeTokens.color.surface.dark.base,
    color: hoopoeTokens.color.surface.dark.textDim,
    fontFamily: hoopoeTokens.typography.mono.join(", "),
    fontSize: "11px",
    fontWeight: "650",
    lineHeight: "1",
    letterSpacing: "0",
  };
}

function emptyStateStyle(): Readonly<Record<string, string>> {
  return {
    display: "grid",
    placeItems: "center",
    minHeight: "120px",
    borderRadius: hoopoeTokens.radius.lg,
    border: `1px solid ${hoopoeTokens.color.surface.dark.borderSoft}`,
    color: hoopoeTokens.color.surface.dark.textDim,
    fontSize: "13px",
    fontWeight: "650",
    lineHeight: "1.3",
    letterSpacing: "0",
  };
}

function markStyle(): Readonly<Record<string, string>> {
  return {
    padding: "0",
    borderRadius: hoopoeTokens.radius.sm,
    background: statusTones.waiting_approval.bg,
    color: statusTones.waiting_approval.fg,
  };
}

function assignStyle(element: HTMLElement, styles: Readonly<Record<string, string>>): void {
  for (const [name, value] of Object.entries(styles)) {
    element.style.setProperty(kebabCase(name), value);
  }
}

function kebabCase(value: string): string {
  return value.replace(/[A-Z]/g, (match) => `-${match.toLowerCase()}`);
}
