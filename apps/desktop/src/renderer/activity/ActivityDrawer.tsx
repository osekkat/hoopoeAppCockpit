// Hoopoe-owned. Activity drawer — slide-over from the right edge,
// overlaying whichever stage the user is on. Per plan.md §7.5 + the
// hp-1r4 bead.

import { useEffect, useMemo, useRef, type ReactNode } from "react";
import { ChatInput } from "./ChatInput.tsx";
import { FilterBar } from "./FilterBar.tsx";
import {
  TimelineList,
  type ActivityContextAction,
} from "./TimelineList.tsx";
import { applyFilter, useActivityStore } from "./store.ts";
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
  // Select raw slices and derive the visible list with useMemo. Calling
  // s.visibleEvents() inside the selector returns a fresh array every
  // render, which makes useSyncExternalStore loop infinitely.
  const events = useActivityStore((s) => s.events);
  const filter = useActivityStore((s) => s.filter);
  const visibleEvents = useMemo(() => applyFilter(events, filter), [events, filter]);
  const visibleUnreadCount = useMemo(
    () => visibleEvents.filter((e) => !e.read).length,
    [visibleEvents],
  );
  const unreadCount = useActivityStore((s) => s.unreadCount);
  const toggleCategory = useActivityStore((s) => s.toggleCategory);
  const toggleImportance = useActivityStore((s) => s.toggleImportance);
  const setText = useActivityStore((s) => s.setText);
  const resetFilter = useActivityStore((s) => s.resetFilter);
  const markRead = useActivityStore((s) => s.markRead);
  const markAllRead = useActivityStore((s) => s.markAllRead);
  const closeButtonRef = useRef<HTMLButtonElement>(null);
  const restoreFocusRef = useRef<HTMLElement | null>(null);

  useEffect(() => {
    if (!open) return;
    const onKeyDown = (event: KeyboardEvent) => {
      if (event.key === "Escape") onClose();
    };
    window.addEventListener("keydown", onKeyDown);
    return () => window.removeEventListener("keydown", onKeyDown);
  }, [onClose, open]);

  useEffect(() => {
    if (typeof document === "undefined") return;

    if (open) {
      const activeElement = document.activeElement;
      restoreFocusRef.current = activeElement instanceof HTMLElement ? activeElement : null;
      const focusFrame = window.requestAnimationFrame(() => closeButtonRef.current?.focus());
      return () => window.cancelAnimationFrame(focusFrame);
    }

    const restoreTarget = restoreFocusRef.current;
    restoreFocusRef.current = null;
    if (restoreTarget && document.contains(restoreTarget)) {
      restoreTarget.focus();
    }
  }, [open]);

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
        aria-hidden={!open}
        aria-label="Activity drawer"
        aria-modal={open ? true : undefined}
        className="hh-activity-panel"
        data-open={open}
        inert={!open}
        role="dialog"
      >
        <header className="hh-activity-header">
          <div className="hh-activity-title">
            {icon}
            <span>Activity</span>
            {visibleUnreadCount > 0 && (
              <span
                aria-label={`${visibleUnreadCount} unread`}
                className="hh-activity-unread-badge"
                data-importance={hasUrgentEvents(visibleEvents) ? "urgent" : "info"}
              >
                {visibleUnreadCount}
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
              ref={closeButtonRef}
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
            emptyReason={events.length === 0 ? "no-events" : "filtered"}
            events={visibleEvents}
            onEventClick={handleEventClick}
            onResetFilters={resetFilter}
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
