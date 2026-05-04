// hp-o6q — Step 11: Success / VPS Ready.
//
// Terminal step. Shows a "Hoopoe is ready" success card + a CTA into the
// project picker. Renders with the picked-path label in mind so the
// local-demo flow doesn't claim "VPS Ready" — it says "Local demo
// ready" instead.

import { CheckCircle2, Sparkles } from "lucide-react";
import type { WizardPath } from "./types.ts";

export interface Step11SuccessProps {
  /** Which path the user took. Drives the wording. */
  readonly path: WizardPath | null;
  /** Called when the user clicks "Open Hoopoe" (the picker route). */
  readonly onEnterCockpit: () => void;
  /** Called when the user clicks "Restart wizard" (Diagnostics flow). */
  readonly onRestart?: () => void;
}

export function Step11Success({ onEnterCockpit, onRestart, path }: Step11SuccessProps) {
  const isLocalDemo = path === "local_demo";
  const Icon = isLocalDemo ? Sparkles : CheckCircle2;
  return (
    <section
      aria-labelledby="hh-wizard-step-success-title"
      className="hh-wizard-step hh-wizard-step-success"
      data-testid="wizard-step-success"
    >
      <header className="hh-wizard-step-header">
        <span className="hh-stage-kicker">STEP 11</span>
        <h2 id="hh-wizard-step-success-title">
          {isLocalDemo ? "Local demo ready" : "VPS ready"}
        </h2>
        <p>
          {isLocalDemo
            ? "Hoopoe is wired to the Mock Flywheel. Explore the four stages — switch to a real VPS in Settings whenever you're ready."
            : "Your VPS is paired and the Flywheel toolchain is verified. Pick or import a project to start a planning round."}
        </p>
      </header>
      <div className="hh-wizard-success-card" data-local-demo={isLocalDemo}>
        <Icon size={36} strokeWidth={2.0} />
        <div>
          <strong>{isLocalDemo ? "Mock Flywheel" : "All systems go"}</strong>
          <p>
            {isLocalDemo
              ? "Fixtures replay end-to-end; no daemon is paired. The cockpit is fully usable for demos and design feedback."
              : "Daemon ready, capabilities verified, agent CLIs detected, CAAM credentials confirmed."}
          </p>
        </div>
      </div>
      <div className="hh-wizard-step-actions">
        <button
          className="hh-wizard-continue"
          data-testid="wizard-step-success-enter"
          onClick={onEnterCockpit}
          type="button"
        >
          Open Hoopoe
        </button>
        {onRestart !== undefined ? (
          <button
            className="hh-wizard-secondary"
            data-testid="wizard-step-success-restart"
            onClick={onRestart}
            type="button"
          >
            Restart wizard
          </button>
        ) : null}
      </div>
    </section>
  );
}
