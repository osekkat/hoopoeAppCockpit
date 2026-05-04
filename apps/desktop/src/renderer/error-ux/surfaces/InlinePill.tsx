import { AlertTriangle, ChevronRight } from "lucide-react";
import { useMemo } from "react";
import type { PublishedError } from "../types.ts";

interface InlinePillProps {
  /** Bus errors filtered to inline_pill surface. */
  readonly errors: readonly PublishedError[];
  /** Capability ID this pill covers. */
  readonly capabilityId: string;
  /** Optional source filter when multiple features share a capability. */
  readonly source?: string;
  /** What the pill should look like when no degraded state is active. */
  readonly children: React.ReactNode;
  /** Open Diagnostics with capability anchor. */
  readonly onOpenDiagnostics?: (capabilityId: string) => void;
}

/** Rendered by feature components in place of an interactive control
 *  that depends on a degraded capability. When the bus carries an
 *  `inline_pill` error matching {capabilityId, source?}, replace the
 *  children with a tooltip-bearing pill that links to Diagnostics. */
export function InlinePill({
  errors,
  capabilityId,
  source,
  children,
  onOpenDiagnostics,
}: InlinePillProps) {
  const match = useMemo(
    () =>
      errors.find(
        (error) =>
          error.surface === "inline_pill" &&
          error.hints?.capabilityId === capabilityId &&
          (source === undefined || error.source === source),
      ),
    [errors, capabilityId, source],
  );

  if (!match) {
    return <>{children}</>;
  }

  const tooltip = `${match.envelope.title}. ${match.envelope.user_message}`;

  return (
    <button
      type="button"
      className="hh-error-inline-pill"
      data-testid={`error-inline-pill-${capabilityId}`}
      data-capability={capabilityId}
      title={tooltip}
      aria-label={tooltip}
      onClick={() => onOpenDiagnostics?.(capabilityId)}
    >
      <AlertTriangle size={11} strokeWidth={2.1} />
      <span className="hh-error-inline-pill-label">{match.envelope.title}</span>
      <ChevronRight size={10} strokeWidth={2.1} />
    </button>
  );
}
