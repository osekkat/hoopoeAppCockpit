// hp-o6q — Placeholder card for steps whose body lands in follow-up beads.
//
// Steps 2-10 each have a follow-up bead (filed at hp-o6q close). Until
// those land, the wizard renders this stub so the shell is end-to-end
// navigable: the user can advance through the step list (the stub's
// "Mark complete" button writes a `completed` checkpoint), see the path
// of breadcrumbs, and reach the success screen. Once a real Step
// component lands, swap StepStub for it in WizardShell.

import { Construction } from "lucide-react";
import type { WizardStepId } from "./types.ts";

export interface StepStubProps {
  readonly stepId: WizardStepId;
  readonly stepNumber: number;
  readonly title: string;
  readonly followupBead: string;
  /** Wired to appendCheckpoint({ outcome: "completed" }) so the user
   *  can navigate past the stub during development. */
  readonly onMarkComplete: () => void;
  /** Wired to appendCheckpoint({ outcome: "skipped" }) for the local
   *  demo path where most steps are skipped. */
  readonly onSkip?: () => void;
}

export function StepStub({ followupBead, onMarkComplete, onSkip, stepId, stepNumber, title }: StepStubProps) {
  const stepLabel = String(stepNumber).padStart(2, "0");
  return (
    <section
      aria-labelledby={`hh-wizard-stub-${stepId}`}
      className="hh-wizard-step hh-wizard-step-stub"
      data-testid={`wizard-step-${stepId}`}
    >
      <header className="hh-wizard-step-header">
        <span className="hh-stage-kicker">STEP {stepLabel}</span>
        <h2 id={`hh-wizard-stub-${stepId}`}>{title}</h2>
        <p>
          The body of this step ships in a follow-up bead (
          <code>{followupBead}</code>). The shell renders end-to-end so
          you can verify the checkpoint state machine; click below to
          advance.
        </p>
      </header>
      <div className="hh-wizard-stub-card">
        <Construction size={26} strokeWidth={2.0} />
        <p>This step is intentionally a placeholder until {followupBead} lands.</p>
      </div>
      <div className="hh-wizard-step-actions">
        <button
          className="hh-wizard-continue"
          data-testid={`wizard-step-${stepId}-complete`}
          onClick={onMarkComplete}
          type="button"
        >
          Mark complete (dev)
        </button>
        {onSkip !== undefined ? (
          <button
            className="hh-wizard-secondary"
            data-testid={`wizard-step-${stepId}-skip`}
            onClick={onSkip}
            type="button"
          >
            Skip
          </button>
        ) : null}
      </div>
    </section>
  );
}
