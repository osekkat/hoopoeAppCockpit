import type { ReactNode } from "react";
import type { StageDefinition } from "../stages.ts";

export interface StageHeaderProps {
  readonly stage: StageDefinition;
  readonly projectName: string;
  readonly breadcrumb?: readonly string[];
  readonly actions?: ReactNode;
}

export function StageHeader({
  actions,
  breadcrumb = [],
  projectName,
  stage,
}: StageHeaderProps) {
  const Icon = stage.icon;

  return (
    <header className="hh-stage-header">
      <div className="hh-stage-heading">
        <span className="hh-stage-kicker">
          STAGE {stage.number} {"\u2014"} {stage.verb}
        </span>
        <div className="hh-stage-title-row">
          <span className="hh-stage-icon" aria-hidden="true">
            <Icon size={22} strokeWidth={2.1} />
          </span>
          <h1>{stage.label}</h1>
        </div>
        <nav className="hh-breadcrumb" aria-label="Breadcrumb">
          {[projectName, ...breadcrumb].map((item) => (
            <span key={item}>{item}</span>
          ))}
        </nav>
      </div>
      {actions ? <div className="hh-stage-actions">{actions}</div> : null}
    </header>
  );
}
