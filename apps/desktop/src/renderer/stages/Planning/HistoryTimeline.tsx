import {
  CircleDot,
  Columns3,
  GitCompare,
  History as HistoryIcon,
  Lightbulb,
  Lock,
  RefreshCcw,
  Sparkles,
  Unlock,
} from "lucide-react";
import type { PlanHistoryEntry, PlanHistoryKind } from "../../data/plan-data.ts";

const HISTORY_ICON: Record<
  PlanHistoryKind,
  React.ComponentType<{ readonly size?: number; readonly strokeWidth?: number }>
> = {
  plan_created: CircleDot,
  candidate_generated: Sparkles,
  comparative_matrix_built: Columns3,
  synthesis_run: GitCompare,
  fresh_eyes_critique: Lightbulb,
  refinement_round_complete: RefreshCcw,
  plan_locked: Lock,
  plan_unlocked: Unlock,
};

const HISTORY_LABEL: Record<PlanHistoryKind, string> = {
  plan_created: "Plan created",
  candidate_generated: "Candidate generated",
  comparative_matrix_built: "Comparative matrix built",
  synthesis_run: "Synthesis run",
  fresh_eyes_critique: "Fresh-eyes critique",
  refinement_round_complete: "Refinement round complete",
  plan_locked: "Plan locked",
  plan_unlocked: "Plan unlocked",
};

interface HistoryTimelineProps {
  readonly history: readonly PlanHistoryEntry[];
}

function formatTimestamp(ts: string): string {
  try {
    const date = new Date(ts);
    return `${date.toISOString().slice(0, 10)} ${date.toISOString().slice(11, 19)} UTC`;
  } catch {
    return ts;
  }
}

export function HistoryTimeline({ history }: HistoryTimelineProps) {
  if (history.length === 0) {
    return (
      <div className="hh-plan-history-empty" role="status">
        <HistoryIcon size={18} strokeWidth={2.1} />
        <span>No plan history yet.</span>
      </div>
    );
  }

  const sorted = history.toSorted((a, b) => (a.ts < b.ts ? 1 : -1));

  return (
    <section className="hh-plan-history" data-testid="history-timeline">
      <header className="hh-plan-history-header">
        <HistoryIcon size={14} strokeWidth={2.1} />
        <h3>Plan history</h3>
        <span className="hh-plan-history-count">{history.length} events</span>
      </header>
      <ol className="hh-plan-history-list">
        {sorted.map((entry) => {
          const Icon = HISTORY_ICON[entry.kind] ?? CircleDot;
          return (
            <li key={`${entry.ts}-${entry.kind}-${entry.actor}`} className="hh-plan-history-row">
              <span className="hh-plan-history-icon" aria-hidden="true">
                <Icon size={12} strokeWidth={2.1} />
              </span>
              <span className="hh-plan-history-main">
                <span className="hh-plan-history-headline">
                  <strong>{HISTORY_LABEL[entry.kind] ?? entry.kind}</strong>
                  <span className="hh-plan-history-actor">{entry.actor}</span>
                </span>
                <span className="hh-plan-history-meta">
                  <time dateTime={entry.ts}>{formatTimestamp(entry.ts)}</time>
                  {entry.artifact ? <span>· {entry.artifact}</span> : null}
                  {typeof entry.latencyMs === "number" ? (
                    <span>· {(entry.latencyMs / 1000).toFixed(1)}s</span>
                  ) : null}
                  {typeof entry.round === "number" ? <span>· round {entry.round}</span> : null}
                  {typeof entry.version === "number" ? (
                    <span>· v{entry.version}</span>
                  ) : null}
                </span>
                {entry.summary ? (
                  <span className="hh-plan-history-summary">{entry.summary}</span>
                ) : null}
              </span>
            </li>
          );
        })}
      </ol>
    </section>
  );
}
