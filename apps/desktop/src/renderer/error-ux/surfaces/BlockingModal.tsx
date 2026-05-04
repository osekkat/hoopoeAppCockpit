import { AlertCircle } from "lucide-react";
import { useCallback, useEffect, useRef } from "react";
import { ariaLiveFor, defaultActionLabel } from "../classification.ts";
import type { ErrorBus, PublishedError } from "../types.ts";

interface BlockingModalProps {
  readonly bus: ErrorBus;
  /** First modal in the queue is rendered. */
  readonly errors: readonly PublishedError[];
}

const FOCUSABLE_SELECTOR =
  'button:not([disabled]), [href], input:not([disabled]), select:not([disabled]), textarea:not([disabled]), [tabindex]:not([tabindex="-1"])';

export function BlockingModal({ bus, errors }: BlockingModalProps) {
  const top = errors[0];
  const ref = useRef<HTMLDivElement | null>(null);
  const triggerRef = useRef<Element | null>(null);

  const dismiss = useCallback(() => {
    if (!top) return;
    if (top.hints?.dismissible === false) return;
    bus.dismiss(top.id);
  }, [bus, top]);

  useEffect(() => {
    if (!top) return;
    triggerRef.current = document.activeElement;
    const node = ref.current;
    if (!node) return;
    const focusables = node.querySelectorAll<HTMLElement>(FOCUSABLE_SELECTOR);
    focusables[0]?.focus();

    function handleKey(event: KeyboardEvent) {
      if (event.key === "Escape") {
        if (top?.hints?.dismissible !== false) {
          event.preventDefault();
          dismiss();
        }
        return;
      }
      if (event.key === "Tab" && node) {
        const items = node.querySelectorAll<HTMLElement>(FOCUSABLE_SELECTOR);
        if (items.length === 0) return;
        const first = items[0];
        const last = items[items.length - 1];
        if (event.shiftKey && document.activeElement === first) {
          event.preventDefault();
          last?.focus();
        } else if (!event.shiftKey && document.activeElement === last) {
          event.preventDefault();
          first?.focus();
        }
      }
    }

    document.addEventListener("keydown", handleKey);
    return () => {
      document.removeEventListener("keydown", handleKey);
      const trigger = triggerRef.current;
      if (trigger instanceof HTMLElement) {
        trigger.focus();
      }
    };
  }, [top, dismiss]);

  if (!top) return null;

  const dismissible = top.hints?.dismissible !== false;

  return (
    <div
      className="hh-error-modal-backdrop"
      data-testid={`error-modal-backdrop-${top.id}`}
      onClick={(event) => {
        if (event.target === event.currentTarget && dismissible) dismiss();
      }}
    >
      <div
        ref={ref}
        className={`hh-error-modal hh-error-modal-${top.severity}`}
        data-testid={`error-modal-${top.id}`}
        role="dialog"
        aria-modal="true"
        aria-labelledby={`error-modal-title-${top.id}`}
        aria-describedby={`error-modal-detail-${top.id}`}
        aria-live={ariaLiveFor(top.severity)}
      >
        <div className="hh-error-modal-header">
          <span className="hh-error-modal-icon" aria-hidden="true">
            <AlertCircle size={16} strokeWidth={2.1} />
          </span>
          <h2 id={`error-modal-title-${top.id}`} className="hh-error-modal-title">
            {top.envelope.title}
          </h2>
        </div>
        <div id={`error-modal-detail-${top.id}`} className="hh-error-modal-body">
          {top.envelope.user_message ? <p>{top.envelope.user_message}</p> : null}
          {top.envelope.detail && top.envelope.detail !== top.envelope.user_message ? (
            <p className="hh-error-modal-extra">{top.envelope.detail}</p>
          ) : null}
        </div>
        <div className="hh-error-modal-actions">
          {dismissible ? (
            <button
              type="button"
              className="hh-error-modal-secondary"
              onClick={dismiss}
              data-testid={`error-modal-cancel-${top.id}`}
            >
              Cancel
            </button>
          ) : null}
          <button
            type="button"
            className="hh-error-modal-primary"
            data-testid={`error-modal-primary-${top.id}`}
          >
            {top.hints?.primaryActionLabel ?? defaultActionLabel(top.envelope.actionability)}
          </button>
        </div>
      </div>
    </div>
  );
}
