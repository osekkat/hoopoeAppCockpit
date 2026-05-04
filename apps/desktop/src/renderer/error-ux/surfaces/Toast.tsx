import { AlertCircle, AlertTriangle, Info, X } from "lucide-react";
import { useEffect, useMemo, useRef } from "react";
import { ariaLiveFor } from "../classification.ts";
import { MAX_VISIBLE_TOASTS } from "../errorBus.ts";
import type { ErrorBus, ErrorSeverity, PublishedError } from "../types.ts";
import { defaultActionLabel } from "../classification.ts";

interface ToastStackProps {
  readonly bus: ErrorBus;
  readonly errors: readonly PublishedError[];
  readonly reducedMotion: boolean;
}

export function ToastStack({ bus, errors, reducedMotion }: ToastStackProps) {
  const visible = useMemo(() => errors.slice(0, MAX_VISIBLE_TOASTS), [errors]);
  const overflow = errors.length - visible.length;

  return (
    <div
      className={`hh-error-toast-stack${reducedMotion ? " hh-error-toast-stack-reduced-motion" : ""}`}
      data-testid="error-toast-stack"
    >
      {visible.map((error) => (
        <ToastItem key={error.id} bus={bus} error={error} />
      ))}
      {overflow > 0 ? (
        <div
          className="hh-error-toast-overflow"
          data-testid="error-toast-overflow"
          role="status"
          aria-live="polite"
        >
          + {overflow} more
        </div>
      ) : null}
    </div>
  );
}

interface ToastItemProps {
  readonly bus: ErrorBus;
  readonly error: PublishedError;
}

function ToastItem({ bus, error }: ToastItemProps) {
  const dismissTimerRef = useRef<number | null>(null);
  const dismissible = error.hints?.dismissible !== false;
  const autoDismissMs = error.hints?.autoDismissMs;

  useEffect(() => {
    if (typeof autoDismissMs !== "number" || autoDismissMs <= 0) return;
    const timer = setTimeout(() => bus.dismiss(error.id), autoDismissMs);
    dismissTimerRef.current = timer as unknown as number;
    return () => {
      clearTimeout(timer);
      dismissTimerRef.current = null;
    };
  }, [autoDismissMs, bus, error.id]);

  return (
    <div
      className={`hh-error-toast hh-error-toast-${error.severity}`}
      data-testid={`error-toast-${error.id}`}
      role={error.severity === "info" ? "status" : "alert"}
      aria-live={ariaLiveFor(error.severity)}
    >
      <span className="hh-error-toast-icon" aria-hidden="true">
        <SeverityIcon severity={error.severity} />
      </span>
      <div className="hh-error-toast-body">
        <div className="hh-error-toast-title">
          {error.envelope.title}
          {error.coalescedCount > 1 ? (
            <span className="hh-error-toast-count" aria-label={`${error.coalescedCount} occurrences`}>
              ×{error.coalescedCount}
            </span>
          ) : null}
        </div>
        {error.envelope.user_message ? (
          <p className="hh-error-toast-detail">{error.envelope.user_message}</p>
        ) : null}
        {error.hints?.primaryActionLabel ? (
          <button type="button" className="hh-error-toast-action">
            {error.hints.primaryActionLabel ?? defaultActionLabel(error.envelope.actionability)}
          </button>
        ) : null}
      </div>
      {dismissible ? (
        <button
          type="button"
          className="hh-error-toast-dismiss"
          onClick={() => bus.dismiss(error.id)}
          aria-label="Dismiss"
        >
          <X size={12} strokeWidth={2.1} />
        </button>
      ) : null}
    </div>
  );
}

function SeverityIcon({ severity }: { readonly severity: ErrorSeverity }) {
  switch (severity) {
    case "info":
      return <Info size={14} strokeWidth={2.1} />;
    case "warning":
      return <AlertTriangle size={14} strokeWidth={2.1} />;
    case "error":
    case "critical":
    case "blocking":
      return <AlertCircle size={14} strokeWidth={2.1} />;
  }
}
