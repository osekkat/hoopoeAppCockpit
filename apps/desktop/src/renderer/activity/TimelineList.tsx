// Hoopoe-owned. Activity-drawer timeline list — wraps the design-system
// TimelineRow with an ActivityEvent → TimelineRowProps adapter and adds
// click-to-pivot / right-click context menu hooks.

import { useCallback, useMemo, useState, type CSSProperties } from "react";
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

export function TimelineList({
  emptyReason = "filtered",
  events,
  onEventClick,
  onContextAction,
  onResetFilters,
}: TimelineListProps) {
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
          {...(onEventClick ? { onEventClick } : {})}
          {...(onContextAction ? { onContextAction } : {})}
        />
      ))}
    </div>
  );
}

interface TimelineEntryProps {
  readonly event: ActivityEvent;
  readonly onEventClick?: (event: ActivityEvent, pivot: ActivityPivot | null) => void;
  readonly onContextAction?: (event: ActivityEvent, action: ActivityContextAction) => void;
}

function TimelineEntry({ event, onEventClick, onContextAction }: TimelineEntryProps) {
  const [menuPos, setMenuPos] = useState<{ x: number; y: number } | null>(null);

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
      setMenuPos({ x: e.clientX, y: e.clientY });
    },
    [],
  );

  const handleAction = useCallback(
    (action: ActivityContextAction) => {
      setMenuPos(null);
      if (onContextAction) onContextAction(event, action);
    },
    [event, onContextAction],
  );

  return (
    <>
      <button
        aria-label={model.ariaLabel}
        className="hh-activity-row"
        data-importance={event.importance}
        data-unread={!event.read}
        onClick={handleClick}
        onContextMenu={handleContextMenu}
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
      {menuPos && (
        <ContextMenu
          x={menuPos.x}
          y={menuPos.y}
          onAction={handleAction}
          onDismiss={() => setMenuPos(null)}
        />
      )}
    </>
  );
}

interface ContextMenuProps {
  readonly x: number;
  readonly y: number;
  readonly onAction: (action: ActivityContextAction) => void;
  readonly onDismiss: () => void;
}

const CONTEXT_ACTIONS: ReadonlyArray<{ id: ActivityContextAction; label: string }> = [
  { id: "reply-as-overseer", label: "Reply as human overseer" },
  { id: "broadcast-to-swarm", label: "Broadcast to swarm" },
  { id: "create-bead-from-message", label: "Create bead from message" },
  { id: "mark-acknowledged", label: "Mark acknowledged" },
];

function ContextMenu({ x, y, onAction, onDismiss }: ContextMenuProps) {
  const style: CSSProperties = { position: "fixed", left: x, top: y };
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
        className="hh-activity-context-menu"
        role="menu"
        style={style}
        aria-label="Activity event actions"
      >
        {CONTEXT_ACTIONS.map((a) => (
          <li key={a.id} role="none">
            <button
              className="hh-activity-context-item"
              onClick={() => onAction(a.id)}
              role="menuitem"
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
