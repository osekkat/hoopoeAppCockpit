import { ArrowLeft, ArrowRight, CheckCircle2, Compass, X } from "lucide-react";
import {
  ONBOARDING_TOUR_STEP_IDS,
  nextOnboardingTourStepId,
  previousOnboardingTourStepId,
  type OnboardingTourStepId,
} from "../store.ts";

interface OnboardingTourStep {
  readonly id: OnboardingTourStepId;
  readonly eyebrow: string;
  readonly title: string;
  readonly body: string;
  readonly callouts: readonly string[];
}

export interface OnboardingTourProps {
  readonly open: boolean;
  readonly stepId: OnboardingTourStepId;
  readonly onBack: () => void;
  readonly onClose: () => void;
  readonly onComplete: () => void;
  readonly onNext: () => void;
  readonly onSkip: () => void;
}

export const ONBOARDING_TOUR_STEPS: readonly OnboardingTourStep[] = [
  {
    id: "topbar",
    eyebrow: "Cockpit chrome",
    title: "Read the project at a glance",
    body: "The top bar carries the current project, tunnel health, tool status, swarm pulse, code health, and subscription usage.",
    callouts: ["Project", "Tunnel", "Tools", "Swarm", "Beads", "Code health", "Subscriptions"],
  },
  {
    id: "activity",
    eyebrow: "Activity",
    title: "Keep the audit trail open",
    body: "The Activity drawer is the shared place for agent mail, automation events, approvals, and user-to-orchestrator messages.",
    callouts: ["Mail", "Reservations", "Builds", "Approvals", "Review findings"],
  },
  {
    id: "stages",
    eyebrow: "Stages",
    title: "Move through the four-stage cockpit",
    body: "Planning, Beads, Swarm, and Hardening are persistent workspaces over the same canonical project state.",
    callouts: ["Planning", "Beads", "Swarm", "Hardening", "Diagnostics"],
  },
  {
    id: "planning",
    eyebrow: "Stage 01",
    title: "Draft and lock the plan",
    body: "Planning collects the intake, candidate model outputs, comparison matrix, synthesis, and locked plan artifact.",
    callouts: ["Intake", "Candidates", "Matrix", "Synthesis", "Locked plan"],
  },
  {
    id: "beads",
    eyebrow: "Stage 02",
    title: "Convert work into traceable beads",
    body: "The Beads stage shows the br board, DAG, ready frontier, dependencies, and plan-to-bead traceability.",
    callouts: ["Ready frontier", "DAG", "Traceability", "Polish rounds"],
  },
  {
    id: "swarm",
    eyebrow: "Stage 03",
    title: "Launch agents without raw panes by default",
    body: "The Swarm stage centers bead state, agent state, and activity. Raw terminal panes stay behind Diagnostics.",
    callouts: ["Composition", "Agent grid", "Bead board", "Activity"],
  },
  {
    id: "hardening",
    eyebrow: "Stage 04",
    title: "Review until convergence is clear",
    body: "Hardening tracks review rounds, findings, code-health snapshots, and the convergence detector before final landing.",
    callouts: ["UBS", "Review rounds", "Finding tracker", "Convergence"],
  },
];

const stepsById = new Map(ONBOARDING_TOUR_STEPS.map((step) => [step.id, step]));

export function OnboardingTour({
  onBack,
  onClose,
  onComplete,
  onNext,
  onSkip,
  open,
  stepId,
}: OnboardingTourProps) {
  if (!open) return null;

  const step = stepsById.get(stepId) ?? ONBOARDING_TOUR_STEPS[0]!;
  const index = ONBOARDING_TOUR_STEP_IDS.indexOf(step.id);
  const current = index + 1;
  const total = ONBOARDING_TOUR_STEPS.length;
  const hasPrevious = previousOnboardingTourStepId(step.id) !== step.id;
  const hasNext = nextOnboardingTourStepId(step.id) !== step.id;

  return (
    <div className="hh-onboarding-backdrop" data-testid="onboarding-tour">
      <section
        aria-labelledby="hh-onboarding-tour-title"
        aria-modal="true"
        className="hh-onboarding-tour"
        data-step={step.id}
        role="dialog"
      >
        <header className="hh-onboarding-tour-header">
          <div>
            <span className="hh-stage-kicker">{step.eyebrow}</span>
            <h2 id="hh-onboarding-tour-title">{step.title}</h2>
          </div>
          <button
            aria-label="Close onboarding tour"
            className="hh-icon-button"
            data-testid="onboarding-tour-close"
            onClick={onClose}
            type="button"
          >
            <X size={16} strokeWidth={2.1} />
          </button>
        </header>

        <p className="hh-onboarding-tour-body">{step.body}</p>

        <div aria-label="Tour progress" className="hh-onboarding-progress">
          {ONBOARDING_TOUR_STEPS.map((item) => (
            <span
              aria-label={`${item.title}${item.id === step.id ? " current" : ""}`}
              data-current={item.id === step.id}
              key={item.id}
            />
          ))}
        </div>

        <div className="hh-onboarding-callouts" data-testid="onboarding-tour-callouts">
          {step.callouts.map((callout) => (
            <span key={callout}>{callout}</span>
          ))}
        </div>

        <footer className="hh-onboarding-tour-footer">
          <span className="hh-onboarding-tour-count">
            <Compass size={14} strokeWidth={2.1} />
            Step {current} of {total}
          </span>
          <div className="hh-onboarding-tour-actions">
            <button className="hh-onboarding-secondary" data-testid="onboarding-tour-skip" onClick={onSkip} type="button">
              Skip
            </button>
            <button
              className="hh-onboarding-secondary"
              data-testid="onboarding-tour-back"
              disabled={!hasPrevious}
              onClick={onBack}
              type="button"
            >
              <ArrowLeft size={14} strokeWidth={2.1} />
              Back
            </button>
            <button
              className="hh-onboarding-primary"
              data-testid="onboarding-tour-next"
              onClick={hasNext ? onNext : onComplete}
              type="button"
            >
              {hasNext ? (
                <>
                  Next
                  <ArrowRight size={14} strokeWidth={2.1} />
                </>
              ) : (
                <>
                  Finish
                  <CheckCircle2 size={14} strokeWidth={2.1} />
                </>
              )}
            </button>
          </div>
        </footer>
      </section>
    </div>
  );
}
