// hp-o6q — Step 1: Path picker.
//
// User chooses one of:
//   - Connect existing VPS  (default — happy path for first-run)
//   - Local demo            (skips steps 4-11 — Mock Flywheel mode)
//   - Provision new VPS     (post-MVP, hidden in v1; surfaced disabled
//                            with a tooltip explaining provider plugins
//                            ship later per Guardrail 6)

import { Cloud, Layers, Sparkles } from "lucide-react";
import type { WizardPath } from "./types.ts";

export interface Step1PathPickerProps {
  /** Currently-selected path (controlled). When null, no card is
   *  highlighted. */
  readonly value: WizardPath | null;
  /** Called when the user picks a path. */
  readonly onChange: (path: WizardPath) => void;
  /** Called when the user clicks Continue. The shell wires this to
   *  appendCheckpoint({ stepId: "path", outcome: "completed", ... }). */
  readonly onContinue: () => void;
}

interface PathDefinition {
  readonly id: WizardPath;
  readonly title: string;
  readonly description: string;
  readonly icon: typeof Cloud;
  readonly disabled?: true;
  readonly disabledReason?: string;
}

const PATH_DEFINITIONS: readonly PathDefinition[] = [
  {
    id: "existing_vps",
    title: "Connect existing VPS",
    description:
      "Pair Hoopoe with a VPS you've already provisioned (Contabo, OVH, etc.). Required for real-VPS swarms.",
    icon: Cloud,
  },
  {
    id: "local_demo",
    title: "Local demo (Mock Flywheel)",
    description:
      "Skip the VPS setup and explore Hoopoe against pre-recorded fixtures. Great for evaluation; switch to existing-VPS later.",
    icon: Sparkles,
  },
  {
    id: "provision_new",
    title: "Provision new VPS (post-MVP)",
    description:
      "Provider-driven VPS provisioning ships after the existing-VPS path is solid (plan.md Guardrail 6).",
    icon: Layers,
    disabled: true,
    disabledReason: "Provider plugins ship in Phase 13 — pick another path for now.",
  },
];

export function Step1PathPicker({ onChange, onContinue, value }: Step1PathPickerProps) {
  return (
    <section
      aria-labelledby="hh-wizard-step-path-title"
      className="hh-wizard-step"
      data-testid="wizard-step-path"
    >
      <header className="hh-wizard-step-header">
        <span className="hh-stage-kicker">STEP 01</span>
        <h2 id="hh-wizard-step-path-title">Pick a path</h2>
        <p>
          Hoopoe is the cockpit. Connect a VPS so the engine can run, or pick the
          local demo to explore the cockpit against fixtures.
        </p>
      </header>
      <div className="hh-wizard-path-grid">
        {PATH_DEFINITIONS.map((path) => {
          const Icon = path.icon;
          const selected = value === path.id;
          const disabled = path.disabled === true;
          return (
            <button
              aria-disabled={disabled}
              aria-label={path.title}
              aria-pressed={selected}
              className="hh-wizard-path-card"
              data-disabled={disabled}
              data-selected={selected}
              data-testid={`wizard-path-${path.id}`}
              disabled={disabled}
              key={path.id}
              onClick={() => {
                if (!disabled) onChange(path.id);
              }}
              type="button"
            >
              <Icon size={18} strokeWidth={2.1} />
              <div>
                <strong>{path.title}</strong>
                <p>{path.description}</p>
                {path.disabledReason !== undefined ? (
                  <small className="hh-wizard-path-disabled-reason">{path.disabledReason}</small>
                ) : null}
              </div>
            </button>
          );
        })}
      </div>
      <div className="hh-wizard-step-actions">
        <button
          className="hh-wizard-continue"
          data-testid="wizard-step-path-continue"
          disabled={value === null}
          onClick={onContinue}
          type="button"
        >
          Continue
        </button>
      </div>
    </section>
  );
}
