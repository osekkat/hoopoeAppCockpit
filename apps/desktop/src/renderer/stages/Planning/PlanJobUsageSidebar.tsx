// hp-pmw — Plan-job usage sidebar.
//
// Renders next to a running plan job: active model / harness / CAAM
// account + elapsed wall-clock + ETA, plus a per-provider window-usage
// list (caut-driven; "unmeasured" when caut isn't reporting).
//
// Per §7.1 'Plan round cost and time discipline' the metric we surface
// is subscription-window usage, NOT token dollars — Hoopoe runs through
// subscription-backed CLIs + Oracle browser mode so token cost is
// off-axis. Rate-limited providers always render in danger color
// regardless of percent.

import { AlertTriangle, BatteryWarning, Gauge, Loader2, ShieldCheck } from "lucide-react";
import {
  activeLineText,
  elapsedMs,
  estimateRemainingMs,
  formatElapsed,
  formatQuotaPercent,
  quotaSeverity,
  rankUsageRows,
  type PlanJobSnapshot,
  type SubscriptionWindowUsage,
} from "./plan-job-usage-model.ts";

export interface PlanJobUsageSidebarProps {
  /** The running (or last-completed) plan job. When null, sidebar
   *  surfaces a friendly "no active plan job" empty state. */
  readonly snapshot: PlanJobSnapshot | null;
  /** Expected number of model calls so the ETA estimator can extrapolate
   *  remaining time. Comes from the planning pipeline config (e.g. 4
   *  candidates + synthesis + 5 refinement rounds = 10). */
  readonly expectedTotalCalls: number;
  /** Per-provider subscription-window usage from caut; "unmeasured" =
   *  null usagePercent. */
  readonly usages: readonly SubscriptionWindowUsage[];
  /** Time injection for stable test rendering. */
  readonly now?: () => Date;
}

export function PlanJobUsageSidebar({
  expectedTotalCalls,
  now,
  snapshot,
  usages,
}: PlanJobUsageSidebarProps) {
  const ranked = rankUsageRows(usages, snapshot?.activeProviderId ?? null);

  return (
    <aside
      aria-labelledby="hh-plan-job-usage-title"
      className="hh-plan-job-usage"
      data-testid="plan-job-usage"
    >
      <header className="hh-plan-job-usage-header">
        <Gauge size={16} strokeWidth={2.1} aria-hidden="true" />
        <div>
          <h3 id="hh-plan-job-usage-title">Plan job</h3>
          <p>Subscription-window usage. Per §13, Hoopoe doesn't bill token dollars.</p>
        </div>
      </header>

      {snapshot ? (
        <ActiveSection
          expectedTotalCalls={expectedTotalCalls}
          {...(now !== undefined ? { now } : {})}
          snapshot={snapshot}
        />
      ) : (
        <p className="hh-plan-job-usage-empty" data-testid="plan-job-usage-empty">
          No active plan job. Generate plans to see live usage.
        </p>
      )}

      {ranked.length > 0 ? (
        <section
          aria-label="Subscription window usage by provider"
          className="hh-plan-job-usage-providers"
          data-testid="plan-job-usage-providers"
        >
          {ranked.map((usage) => {
            const severity = quotaSeverity(usage);
            const isActive = snapshot?.activeProviderId === usage.providerId;
            return (
              <div
                className="hh-plan-job-usage-provider"
                data-active={isActive}
                data-severity={severity}
                data-testid={`plan-job-usage-provider-${usage.providerId}`}
                key={usage.providerId}
              >
                <div className="hh-plan-job-usage-provider-row">
                  <strong>{usage.label}</strong>
                  <span className="hh-plan-job-usage-provider-pct">
                    {formatQuotaPercent(usage.usagePercent)}
                  </span>
                </div>
                <BarMeter severity={severity} percent={usage.usagePercent} />
                {usage.rateLimited ? (
                  <p className="hh-plan-job-usage-provider-note" data-testid={`plan-job-usage-provider-${usage.providerId}-rate`}>
                    <AlertTriangle aria-hidden="true" size={11} strokeWidth={2.2} />
                    Rate-limited — switch CAAM account or wait for the window to roll over.
                  </p>
                ) : usage.usagePercent === null ? (
                  <p className="hh-plan-job-usage-provider-note hh-plan-job-usage-provider-unmeasured">
                    caut isn't reporting usage for this provider.
                  </p>
                ) : usage.resetsAt ? (
                  <p className="hh-plan-job-usage-provider-note">
                    <ShieldCheck aria-hidden="true" size={11} strokeWidth={2.2} />
                    Window resets {formatRelativeFuture(usage.resetsAt, now)}.
                  </p>
                ) : null}
              </div>
            );
          })}
        </section>
      ) : null}
    </aside>
  );
}

interface ActiveSectionProps {
  readonly snapshot: PlanJobSnapshot;
  readonly expectedTotalCalls: number;
  readonly now?: () => Date;
}

function ActiveSection({ expectedTotalCalls, now, snapshot }: ActiveSectionProps) {
  if (snapshot.status !== "running") {
    const elapsed = elapsedMs(snapshot, now);
    return (
      <div
        className="hh-plan-job-usage-active hh-plan-job-usage-terminal"
        data-status={snapshot.status}
        data-testid="plan-job-usage-active"
      >
        <strong>Status: {snapshot.status}</strong>
        <span className="hh-plan-job-usage-elapsed">
          {formatElapsed(elapsed)} total wall-clock across {snapshot.measurements.length} call
          {snapshot.measurements.length === 1 ? "" : "s"}.
        </span>
      </div>
    );
  }

  const text = activeLineText(snapshot, expectedTotalCalls, now);
  const remaining = estimateRemainingMs(snapshot, expectedTotalCalls, now);
  return (
    <div
      className="hh-plan-job-usage-active"
      data-status={snapshot.status}
      data-testid="plan-job-usage-active"
    >
      <Loader2 className="hh-spin" size={14} strokeWidth={2.1} aria-hidden="true" />
      <strong data-testid="plan-job-usage-active-line">{text ?? "Active job…"}</strong>
      {remaining ? (
        <small data-testid="plan-job-usage-eta-confidence">
          ETA confidence: <em>{remaining.confidence}</em>{" "}
          {remaining.source === "fallback" ? "(no calls completed yet)" : null}
          {remaining.source === "single_call" ? "(only 1 call sampled)" : null}
        </small>
      ) : null}
      {expectedTotalCalls > 0 ? (
        <small data-testid="plan-job-usage-progress">
          {snapshot.measurements.length} / {expectedTotalCalls} calls completed
        </small>
      ) : null}
    </div>
  );
}

interface BarMeterProps {
  readonly severity: ReturnType<typeof quotaSeverity>;
  readonly percent: number | null;
}

function BarMeter({ percent, severity }: BarMeterProps) {
  if (percent === null) return null;
  const clamped = Math.max(0, Math.min(100, percent));
  return (
    <div
      aria-hidden="true"
      className="hh-plan-job-usage-bar"
      data-severity={severity}
    >
      <div
        className="hh-plan-job-usage-bar-fill"
        style={{ width: `${clamped}%` }}
      />
    </div>
  );
}

/** "in 4h" / "in 12m" / "now" — minimal future-relative formatter. */
function formatRelativeFuture(value: string, now: () => Date = () => new Date()): string {
  const ts = Date.parse(value);
  if (Number.isNaN(ts)) return "soon";
  const deltaMs = ts - now().getTime();
  if (deltaMs <= 0) return "now";
  const minute = 60 * 1_000;
  const hour = 60 * minute;
  if (deltaMs < minute) return "in <1m";
  if (deltaMs < hour) return `in ${Math.round(deltaMs / minute)}m`;
  if (deltaMs < 24 * hour) return `in ${Math.round(deltaMs / hour)}h`;
  return `in ${Math.round(deltaMs / (24 * hour))}d`;
}

/** Re-export so tests can drive the formatter without crossing module
 *  boundaries. */
export { formatRelativeFuture as _formatRelativeFutureForTest };

// Defensive: callers shouldn't need to install the BatteryWarning icon
// directly, but keeping the import live prevents tree-shake from
// dropping the lucide bundle entry that downstream beads will use for
// the rate-limit modal (hp-fkov surfaces).
export const PLAN_JOB_USAGE_ICONS = { BatteryWarning } as const;
