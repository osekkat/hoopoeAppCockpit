import { CircleDot, Lock, Pencil } from "lucide-react";
import { planLockStateLabel, type PlanSummary } from "../../data/plan-data.ts";

interface PlanListProps {
  readonly plans: readonly PlanSummary[];
  readonly activePlanId: string | null;
  readonly onSelectPlan: (planId: string) => void;
}

export function PlanList({ plans, activePlanId, onSelectPlan }: PlanListProps) {
  return (
    <nav className="hh-plan-list" aria-label="Plans for this project">
      <header className="hh-plan-list-header">
        <span className="hh-plan-list-kicker">PROJECT PLANS</span>
        <span className="hh-plan-list-count">{plans.length}</span>
      </header>
      <ul className="hh-plan-list-items">
        {plans.map((plan) => {
          const isActive = plan.planId === activePlanId;
          const lockBadge = plan.lockState === "locked" ? Lock : Pencil;
          const LockIcon = lockBadge;
          return (
            <li key={plan.planId}>
              <button
                type="button"
                className={`hh-plan-list-row${isActive ? " hh-plan-list-row-active" : ""}`}
                onClick={() => onSelectPlan(plan.planId)}
                aria-current={isActive ? "true" : undefined}
                data-testid={`plan-list-row-${plan.planId}`}
              >
                <span className="hh-plan-list-row-icon" aria-hidden="true">
                  <CircleDot size={13} strokeWidth={2.1} />
                </span>
                <span className="hh-plan-list-row-main">
                  <span className="hh-plan-list-row-title">{plan.title}</span>
                  {plan.summary ? (
                    <span className="hh-plan-list-row-summary">{plan.summary}</span>
                  ) : null}
                </span>
                <span className="hh-plan-list-row-meta">
                  <span
                    className={`hh-plan-list-row-state hh-plan-state-${plan.lockState}`}
                    aria-label={`Lock state: ${plan.lockState}`}
                  >
                    <LockIcon size={11} strokeWidth={2.1} />
                    <span>{planLockStateLabel(plan.lockState)}</span>
                  </span>
                  <span className="hh-plan-list-row-version">v{plan.version}</span>
                </span>
              </button>
            </li>
          );
        })}
      </ul>
    </nav>
  );
}
