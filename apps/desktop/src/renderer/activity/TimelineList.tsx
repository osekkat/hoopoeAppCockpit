// Hoopoe-owned. Activity-drawer timeline list — wraps the design-system
// TimelineRow with an ActivityEvent → TimelineRowProps adapter and adds
// click-to-pivot / right-click context menu hooks.

import {
  useCallback,
  useEffect,
  useMemo,
  useRef,
  useState,
  type CSSProperties,
  type KeyboardEvent as ReactKeyboardEvent,
} from "react";
import {
  getTimelineRowModel,
  type TimelineRowProps,
} from "@hoopoe/design-system";
import { FilterX, MessageSquare } from "lucide-react";
import {
  mapToTimelineKind,
  type ActivityEvent,
  type ActivityPivot,
} from "./types.ts";
import { StateSurface } from "../state-view/index.ts";

export interface TimelineListProps {
  readonly events: readonly ActivityEvent[];
  readonly emptyReason?: "no-events" | "filtered";
  readonly onEventClick?: (event: ActivityEvent, pivot: ActivityPivot | null) => void;
  readonly onContextAction?: (event: ActivityEvent, action: ActivityContextAction) => void;
  readonly onResetFilters?: () => void;
}

export type ActivityContextAction =
  | "reply-as-overseer"
  | "broadcast-to-swarm"
  | "create-bead-from-message"
  | "mark-acknowledged";

interface OpenContextMenuState {
  readonly event: ActivityEvent;
  readonly x: number;
  readonly y: number;
}

const ACTIVITY_CONTEXT_MENU_DOM_ID = "hh-activity-context-menu";

export function TimelineList({
  emptyReason = "filtered",
  events,
  onEventClick,
  onContextAction,
  onResetFilters,
}: TimelineListProps) {
  // hp-tg0s: keep ContextMenu state at the parent so AT MOST ONE menu is
  // rendered across the whole timeline. The previous per-row state let two
  // menus stack when the user right-clicked row A then row B without
  // dismissing A. Hooks must be called unconditionally — declare them
  // before the empty-state early return.
  const [openMenu, setOpenMenu] = useState<OpenContextMenuState | null>(null);
  const openerRef = useRef<HTMLButtonElement | null>(null);

  const handleOpenMenu = useCallback(
    (event: ActivityEvent, x: number, y: number, opener: HTMLButtonElement) => {
      openerRef.current = opener;
      setOpenMenu({ event, x, y });
    },
    [],
  );

  const dismissMenu = useCallback(() => {
    const opener = openerRef.current;
    openerRef.current = null;
    setOpenMenu(null);
    opener?.focus();
  }, []);

  const handleAction = useCallback(
    (action: ActivityContextAction) => {
      const target = openMenu;
      openerRef.current = null;
      setOpenMenu(null);
      // Don't restore focus on action — the action handler may itself open
      // another surface (composer, modal, navigation) that owns focus.
      if (target && onContextAction) onContextAction(target.event, action);
    },
    [openMenu, onContextAction],
  );

  if (events.length === 0) {
    const noEvents = emptyReason === "no-events";
    return (
      <StateSurface
        variant="empty"
        density="compact"
        icon={
          noEvents ? (
            <MessageSquare size={18} strokeWidth={2.1} />
          ) : (
            <FilterX size={18} strokeWidth={2.1} />
          )
        }
        title={noEvents ? "No activity yet" : "No matching events"}
        description={
          noEvents
            ? "Agent Mail, reservations, builds, approvals, and orchestrator messages will appear here."
            : "Clear filters to restore the activity timeline."
        }
        details={
          noEvents
            ? ["Use the composer below to contact the orchestrator when a swarm is running."]
            : ["Filters never change canonical activity; they only hide rows in this drawer."]
        }
        actions={
          !noEvents && onResetFilters
            ? [
                {
                  label: "Clear filters",
                  icon: <FilterX size={13} strokeWidth={2.1} />,
                  onClick: onResetFilters,
                  variant: "primary",
                },
              ]
            : []
        }
        testId="activity-timeline-empty"
      />
    );
  }

  return (
    <div className="hh-activity-thread" role="list">
      {events.map((event) => (
        <TimelineEntry
          key={event.id}
          event={event}
          isMenuOpen={openMenu?.event.id === event.id}
          onOpenContextMenu={handleOpenMenu}
          {...(onEventClick ? { onEventClick } : {})}
        />
      ))}
      {openMenu && (
        <ContextMenu
          x={openMenu.x}
          y={openMenu.y}
          onAction={handleAction}
          onDismiss={dismissMenu}
        />
      )}
    </div>
  );
}

interface TimelineEntryProps {
  readonly event: ActivityEvent;
  readonly isMenuOpen: boolean;
  readonly onEventClick?: (event: ActivityEvent, pivot: ActivityPivot | null) => void;
  readonly onOpenContextMenu: (
    event: ActivityEvent,
    x: number,
    y: number,
    opener: HTMLButtonElement,
  ) => void;
}

function TimelineEntry({ event, isMenuOpen, onEventClick, onOpenContextMenu }: TimelineEntryProps) {
  const rowRef = useRef<HTMLButtonElement>(null);

  const props: TimelineRowProps = useMemo(() => {
    return {
      id: event.id,
      kind: mapToTimelineKind(event.kind),
      timestampLabel: formatTimestamp(event.timestamp),
      actor: {
        id: event.actor.id,
        displayName: event.actor.displayName,
        kind: event.actor.kind,
        ...(event.actor.harness ? { harness: event.actor.harness } : {}),
      },
      summary: event.summary,
      ...(event.pills && event.pills.length > 0
        ? { pills: event.pills.map((p) => ({ id: p.id, label: p.label })) }
        : {}),
      ...(event.inlinePreview ? { inlinePreview: event.inlinePreview } : {}),
      unread: !event.read,
    };
  }, [event]);

  const model = getTimelineRowModel(props);

  const handleClick = useCallback(() => {
    if (onEventClick) onEventClick(event, event.pivot ?? null);
  }, [event, onEventClick]);

  const handleContextMenu = useCallback(
    (e: React.MouseEvent<HTMLButtonElement>) => {
      e.preventDefault();
      if (rowRef.current) {
        onOpenContextMenu(event, e.clientX, e.clientY, rowRef.current);
      }
    },
    [event, onOpenContextMenu],
  );

  // hp-r8ch: keyboard-triggered menu opens (Shift+F10 and the dedicated
  // ContextMenu key) anchor at the row's bounding rect rather than the
  // synthetic clientX/Y=0 a KeyboardEvent carries — that pair would
  // detach the fixed-position menu from the focused row entirely.
  const handleKeyDown = useCallback(
    (e: ReactKeyboardEvent<HTMLButtonElement>) => {
      if (!isContextMenuKeyEvent(e.key, e.shiftKey)) return;
      e.preventDefault();
      const node = rowRef.current;
      if (!node) return;
      const rect = node.getBoundingClientRect();
      onOpenContextMenu(event, rect.left, rect.bottom, node);
    },
    [event, onOpenContextMenu],
  );

  return (
    <button
      aria-controls={isMenuOpen ? ACTIVITY_CONTEXT_MENU_DOM_ID : undefined}
      aria-expanded={isMenuOpen}
      aria-haspopup="menu"
      aria-label={model.ariaLabel}
      className="hh-activity-row"
      data-importance={event.importance}
      data-unread={!event.read}
      onClick={handleClick}
      onContextMenu={handleContextMenu}
      onKeyDown={handleKeyDown}
      ref={rowRef}
      role="listitem"
      type="button"
    >
      <span className="hh-activity-row-marker" aria-hidden="true">
        {model.kindMarker}
      </span>
      <span className="hh-activity-row-body">
        <span className="hh-activity-row-meta">
          <strong>{event.actor.displayName}</strong>
          <span className="hh-activity-row-kind">{model.kindLabel}</span>
          <time dateTime={event.timestamp}>{model.timestampLabel}</time>
        </span>
        <p className="hh-activity-row-summary">{event.summary}</p>
        {event.inlinePreview && (
          <p className="hh-activity-row-preview">{event.inlinePreview}</p>
        )}
        {event.pills && event.pills.length > 0 && (
          <span className="hh-activity-row-pills">
            {event.pills.map((p) => (
              <span
                className="hh-activity-row-pill"
                data-tone={p.tone ?? "muted"}
                key={p.id}
              >
                {p.label}
              </span>
            ))}
          </span>
        )}
      </span>
      {!event.read && (
        <span className="hh-activity-row-unread" aria-label="Unread" />
      )}
    </button>
  );
}

interface ContextMenuProps {
  readonly x: number;
  readonly y: number;
  readonly onAction: (action: ActivityContextAction) => void;
  readonly onDismiss: () => void;
}

export const CONTEXT_ACTIONS: ReadonlyArray<{ id: ActivityContextAction; label: string }> = [
  { id: "reply-as-overseer", label: "Reply as human overseer" },
  { id: "broadcast-to-swarm", label: "Broadcast to swarm" },
  { id: "create-bead-from-message", label: "Create bead from message" },
  { id: "mark-acknowledged", label: "Mark acknowledged" },
];

/** Pure WAI-ARIA menu keyboard reducer. Returns the next state given the
 *  current focused index, the total number of items, and the key pressed.
 *
 *  hp-0xm3: extracted as a pure helper so the keyboard contract can be
 *  unit-tested with bun:test (the renderer has no DOM environment) and
 *  so the same logic can be reused if a second menu surface lands.
 *
 *  Pattern reference: https://www.w3.org/WAI/ARIA/apg/patterns/menu/ */
export type ContextMenuKeyAction =
  | { readonly type: "move"; readonly nextIndex: number }
  | { readonly type: "activate"; readonly index: number }
  | { readonly type: "dismiss" }
  | null;

/** hp-r8ch: pure helper for the row's keyboard-trigger detector. The
 *  WAI-ARIA APG menu pattern names two keys for opening a context menu
 *  from a focused element: the dedicated ContextMenu key (where the
 *  hardware exposes one) and Shift+F10 (the reliable cross-platform
 *  fallback). Extracted so the contract is unit-testable without a DOM. */
export function isContextMenuKeyEvent(key: string, shiftKey: boolean): boolean {
  if (key === "ContextMenu") return true;
  if (key === "F10" && shiftKey) return true;
  return false;
}

export function reduceContextMenuKey(
  key: string,
  currentIndex: number,
  total: number,
): ContextMenuKeyAction {
  if (total <= 0) {
    if (key === "Escape" || key === "Tab") return { type: "dismiss" };
    return null;
  }
  switch (key) {
    case "ArrowDown":
      return { type: "move", nextIndex: (currentIndex + 1) % total };
    case "ArrowUp":
      return { type: "move", nextIndex: (currentIndex - 1 + total) % total };
    case "Home":
    case "PageUp":
      return { type: "move", nextIndex: 0 };
    case "End":
    case "PageDown":
      return { type: "move", nextIndex: total - 1 };
    case "Enter":
    case " ":
    case "Spacebar":
      return { type: "activate", index: currentIndex };
    case "Escape":
    case "Tab":
      return { type: "dismiss" };
    default:
      return null;
  }
}

function ContextMenu({ x, y, onAction, onDismiss }: ContextMenuProps) {
  const style: CSSProperties = { position: "fixed", left: x, top: y };
  const [focusedIndex, setFocusedIndex] = useState(0);
  const itemRefs = useRef<Array<HTMLButtonElement | null>>([]);

  // hp-0xm3: focus the first menuitem on mount so keyboard users land
  // inside the menu immediately. Do it after the next paint to let React
  // commit DOM positioning first.
  useEffect(() => {
    const handle = window.requestAnimationFrame(() => {
      itemRefs.current[0]?.focus();
    });
    return () => window.cancelAnimationFrame(handle);
  }, []);

  // Keep the focused element in sync with focusedIndex when the user
  // navigates via Arrow keys.
  useEffect(() => {
    itemRefs.current[focusedIndex]?.focus();
  }, [focusedIndex]);

  const handleKeyDown = (e: ReactKeyboardEvent<HTMLUListElement>) => {
    const action = reduceContextMenuKey(e.key, focusedIndex, CONTEXT_ACTIONS.length);
    if (!action) return;
    e.preventDefault();
    e.stopPropagation();
    if (action.type === "move") {
      setFocusedIndex(action.nextIndex);
      return;
    }
    if (action.type === "activate") {
      const item = CONTEXT_ACTIONS[action.index];
      if (item) onAction(item.id);
      return;
    }
    onDismiss();
  };

  return (
    <>
      <button
        aria-hidden="true"
        className="hh-activity-context-backdrop"
        onClick={onDismiss}
        tabIndex={-1}
        type="button"
      />
      <ul
        aria-label="Activity event actions"
        className="hh-activity-context-menu"
        id={ACTIVITY_CONTEXT_MENU_DOM_ID}
        onKeyDown={handleKeyDown}
        role="menu"
        style={style}
      >
        {CONTEXT_ACTIONS.map((a, i) => (
          <li key={a.id} role="none">
            <button
              className="hh-activity-context-item"
              data-focused={i === focusedIndex ? "true" : undefined}
              onClick={() => onAction(a.id)}
              ref={(el) => {
                itemRefs.current[i] = el;
              }}
              role="menuitem"
              tabIndex={i === focusedIndex ? 0 : -1}
              type="button"
            >
              {a.label}
            </button>
          </li>
        ))}
      </ul>
    </>
  );
}

function formatTimestamp(ts: string): string {
  // Display HH:MM in user-local time. Real wiring uses Intl.DateTimeFormat
  // with the user's locale (settings store); for now use a stable
  // ISO-derived format.
  const d = new Date(ts);
  if (Number.isNaN(d.getTime())) return ts;
  return d.toISOString().slice(11, 16) + "Z";
}
