import { AlertCircle, AlertTriangle, Info, X } from "lucide-react";
import { useMemo } from "react";
import { ariaLiveFor, defaultActionLabel } from "../classification.ts";
import { MAX_VISIBLE_BANNERS } from "../errorBus.ts";
import type { ErrorBus, ErrorSeverity, PublishedError } from "../types.ts";

interface BannerStackProps {
  readonly bus: ErrorBus;
  readonly errors: readonly PublishedError[];
}

export function BannerStack({ bus, errors }: BannerStackProps) {
  const visible = useMemo(() => errors.slice(0, MAX_VISIBLE_BANNERS), [errors]);
  const overflow = errors.length - visible.length;

  if (errors.length === 0) return null;

  return (
    <div className="hh-error-banner-stack" data-testid="error-banner-stack">
      {visible.map((error) => (
        <BannerItem key={error.id} bus={bus} error={error} />
      ))}
      {overflow > 0 ? (
        <button
          type="button"
          className="hh-error-banner-overflow"
          data-testid="error-banner-overflow"
        >
          Show all banners ({overflow} more)
        </button>
      ) : null}
    </div>
  );
}

interface BannerItemProps {
  readonly bus: ErrorBus;
  readonly error: PublishedError;
}

function BannerItem({ bus, error }: BannerItemProps) {
  const dismissible = error.hints?.dismissible !== false;
  const ariaLabel = `${error.severity} — ${error.envelope.title}`;

  return (
    <div
      className={`hh-error-banner hh-error-banner-${error.severity}`}
      data-testid={`error-banner-${error.id}`}
      role="region"
      aria-label={ariaLabel}
      aria-live={ariaLiveFor(error.severity)}
    >
      <span className="hh-error-banner-icon" aria-hidden="true">
        <SeverityIcon severity={error.severity} />
      </span>
      <div className="hh-error-banner-body">
        <strong className="hh-error-banner-title">{error.envelope.title}</strong>
        {error.envelope.user_message ? (
          <span className="hh-error-banner-detail">{error.envelope.user_message}</span>
        ) : null}
      </div>
      <div className="hh-error-banner-actions">
        {error.hints?.primaryActionLabel || error.envelope.actionability !== "manual" ? (
          <button type="button" className="hh-error-banner-action">
            {error.hints?.primaryActionLabel ?? defaultActionLabel(error.envelope.actionability)}
          </button>
        ) : null}
        {dismissible ? (
          <button
            type="button"
            className="hh-error-banner-dismiss"
            onClick={() => bus.dismiss(error.id)}
            aria-label="Dismiss"
          >
            <X size={13} strokeWidth={2.1} />
          </button>
        ) : null}
      </div>
    </div>
  );
}

function SeverityIcon({ severity }: { readonly severity: ErrorSeverity }) {
  switch (severity) {
    case "info":
      return <Info size={15} strokeWidth={2.1} />;
    case "warning":
      return <AlertTriangle size={15} strokeWidth={2.1} />;
    case "error":
    case "critical":
    case "blocking":
      return <AlertCircle size={15} strokeWidth={2.1} />;
  }
}
