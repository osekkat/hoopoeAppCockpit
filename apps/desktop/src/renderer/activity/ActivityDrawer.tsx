// Hoopoe-owned. Activity drawer — slide-over from the right edge,
// overlaying whichever stage the user is on. Per plan.md §7.5 + the
// hp-1r4 bead.

import { useEffect, type ReactNode } from "react";
import { ChatInput } from "./ChatInput.tsx";
import { FilterBar } from "./FilterBar.tsx";
import {
  TimelineList,
  type ActivityContextAction,
} from "./TimelineList.tsx";
import { useActivityStore } from "./store.ts";
import type { ActivityEvent, ActivityPivot } from "./types.ts";

export interface ActivityDrawerProps {
  readonly open: boolean;
  readonly onClose: () => void;
  readonly icon?: ReactNode;
  readonly onPivot?: (event: ActivityEvent, pivot: ActivityPivot | null) => void;
  readonly onContextAction?: (
    event: ActivityEvent,
    action: ActivityContextAction,
  ) => void;
  readonly onChatSubmit?: (text: string) => void;
}

export function ActivityDrawer({
  open,
  onClose,
  icon,
  onPivot,
  onContextAction,
  onChatSubmit,
}: ActivityDrawerProps) {
  const visibleEvents = useActivityStore((s) => s.visibleEvents());
  const filter = useActivityStore((s) => s.filter);
  const unreadCount = useActivityStore((s) => s.unreadCount);
  const toggleCategory = useActivityStore((s) => s.toggleCategory);
  const toggleImportance = useActivityStore((s) => s.toggleImportance);
  const setText = useActivityStore((s) => s.setText);
  const resetFilter = useActivityStore((s) => s.resetFilter);
  const markRead = useActivityStore((s) => s.markRead);
  const markAllRead = useActivityStore((s) => s.markAllRead);

  useEffect(() => {
    if (!open) return;
    const onKeyDown = (event: KeyboardEvent) => {
      if (event.key === "Escape") onClose();
    };
    window.addEventListener("keydown", onKeyDown);
    return () => window.removeEventListener("keydown", onKeyDown);
  }, [onClose, open]);

  const handleEventClick = (event: ActivityEvent, pivot: ActivityPivot | null) => {
    markRead(event.id);
    if (onPivot) onPivot(event, pivot);
  };

  return (
    <>
      <button
        aria-hidden={!open}
        className="hh-activity-backdrop"
        data-open={open}
        onClick={onClose}
        tabIndex={open ? 0 : -1}
        type="button"
      />
      <aside
        aria-label="Activity drawer"
        aria-modal={open}
        className="hh-activity-panel"
        data-open={open}
        role="dialog"
      >
        <header className="hh-activity-header">
          <div className="hh-activity-title">
            {icon}
            <span>Activity</span>
            {unreadCount > 0 && (
              <span
                aria-label={`${unreadCount} unread`}
                className="hh-activity-unread-badge"
                data-importance={hasUrgentEvents(visibleEvents) ? "urgent" : "info"}
              >
                {unreadCount}
              </span>
            )}
          </div>
          <div className="hh-activity-header-actions">
            {unreadCount > 0 && (
              <button
                className="hh-text-button"
                onClick={markAllRead}
                type="button"
              >
                Mark all read
              </button>
            )}
            <button
              aria-label="Close Activity drawer"
              className="hh-text-button"
              onClick={onClose}
              type="button"
            >
              Close
            </button>
          </div>
        </header>

        <FilterBar
          filter={filter}
          onToggleCategory={toggleCategory}
          onToggleImportance={toggleImportance}
          onSetText={setText}
          onReset={resetFilter}
        />

        <div className="hh-activity-thread-wrap">
          <TimelineList
            events={visibleEvents}
            onEventClick={handleEventClick}
            {...(onContextAction ? { onContextAction } : {})}
          />
        </div>

        <ChatInput
          onSubmit={(text) => {
            if (onChatSubmit) onChatSubmit(text);
          }}
        />
      </aside>
    </>
  );
}

function hasUrgentEvents(events: readonly ActivityEvent[]): boolean {
  return events.some((e) => !e.read && e.importance === "urgent");
}
