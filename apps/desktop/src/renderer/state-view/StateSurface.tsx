import {
  AlertTriangle,
  CircleDashed,
  Info,
  Loader2,
  SearchX,
} from "lucide-react";
import type { ReactNode } from "react";
import "./state-surface.css";

export type StateSurfaceVariant = "empty" | "loading" | "error" | "degraded";
export type StateSurfaceDensity = "regular" | "compact";

export interface StateSurfaceAction {
  readonly label: string;
  readonly icon?: ReactNode;
  readonly onClick?: () => void;
  readonly variant?: "primary" | "secondary";
}

export interface StateSurfaceProps {
  readonly variant: StateSurfaceVariant;
  readonly title: string;
  readonly description: string;
  readonly eyebrow?: string;
  readonly icon?: ReactNode;
  readonly actions?: readonly StateSurfaceAction[];
  readonly density?: StateSurfaceDensity;
  readonly testId?: string;
}

export function StateSurface({
  variant,
  title,
  description,
  eyebrow,
  icon,
  actions = [],
  density = "regular",
  testId,
}: StateSurfaceProps) {
  const role = variant === "error" ? "alert" : "status";
  const ariaLive = variant === "error" ? "assertive" : "polite";

  return (
    <section
      aria-live={ariaLive}
      className="hh-state-surface"
      data-density={density}
      data-testid={testId}
      data-variant={variant}
      role={role}
    >
      <div className="hh-state-surface-icon" aria-hidden="true">
        {icon ?? <DefaultStateIcon variant={variant} />}
      </div>
      <div className="hh-state-surface-body">
        {eyebrow ? <span className="hh-state-surface-eyebrow">{eyebrow}</span> : null}
        <h2>{title}</h2>
        <p>{description}</p>
        {variant === "loading" ? <LoadingSkeleton /> : null}
        {actions.length > 0 ? (
          <div className="hh-state-surface-actions">
            {actions.map((action) => (
              <button
                className="hh-state-surface-action"
                data-action-variant={action.variant ?? "secondary"}
                key={action.label}
                onClick={action.onClick}
                type="button"
              >
                {action.icon ? <span aria-hidden="true">{action.icon}</span> : null}
                <span>{action.label}</span>
              </button>
            ))}
          </div>
        ) : null}
      </div>
    </section>
  );
}

function DefaultStateIcon({ variant }: { readonly variant: StateSurfaceVariant }) {
  if (variant === "loading") {
    return <Loader2 className="hh-state-surface-spin" size={18} strokeWidth={2.1} />;
  }
  if (variant === "error") return <AlertTriangle size={18} strokeWidth={2.1} />;
  if (variant === "degraded") return <Info size={18} strokeWidth={2.1} />;
  if (variant === "empty") return <SearchX size={18} strokeWidth={2.1} />;
  return <CircleDashed size={18} strokeWidth={2.1} />;
}

function LoadingSkeleton() {
  return (
    <div className="hh-state-surface-skeleton" aria-hidden="true">
      <span />
      <span />
      <span />
    </div>
  );
}
