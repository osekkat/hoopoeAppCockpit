import { useEffect, type ReactNode } from "react";

export interface ActivityPanelProps {
  readonly open: boolean;
  readonly onClose: () => void;
  readonly icon?: ReactNode;
}

const activityRows = [
  { source: "orchestrator-chat", detail: "Waiting for first project context" },
  { source: "agent-mail", detail: "Inbox sync pending daemon pairing" },
  { source: "audit", detail: "No local actions recorded" },
] as const;

export function ActivityPanel({ open, onClose, icon }: ActivityPanelProps) {
  useEffect(() => {
    if (!open) return;

    const onKeyDown = (event: KeyboardEvent) => {
      if (event.key === "Escape") onClose();
    };

    window.addEventListener("keydown", onKeyDown);
    return () => window.removeEventListener("keydown", onKeyDown);
  }, [onClose, open]);

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
        aria-label="Activity panel"
        aria-modal={open}
        className="hh-activity-panel"
        data-open={open}
        role="dialog"
      >
        <header className="hh-activity-header">
          <div className="hh-activity-title">
            {icon}
            <span>Activity</span>
          </div>
          <button className="hh-text-button" onClick={onClose} type="button">
            Close
          </button>
        </header>

        <div className="hh-activity-thread" role="list">
          {activityRows.map((row) => (
            <article className="hh-activity-row" key={row.source} role="listitem">
              <span>{row.source}</span>
              <p>{row.detail}</p>
            </article>
          ))}
        </div>
      </aside>
    </>
  );
}
