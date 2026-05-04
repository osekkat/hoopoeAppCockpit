// hp-o6q — Wizard shell.
//
// Composes the per-step components, owns a single WizardReplaySink as
// the source of truth, and renders the stepper sidebar + active step
// body + a small summary footer.
//
// Persistence to client-settings.json happens through a `persist` callback
// the caller supplies. For tests + the local-demo bring-up, the default
// persists nothing (in-memory only); production wires this to the
// SettingsBridge — see hp-rflj follow-up.

import { useCallback, useMemo, useState } from "react";
import { ChevronRight, Circle, CheckCircle2, AlertTriangle, MoreHorizontal } from "lucide-react";
import { Step1PathPicker } from "./Step1PathPicker.tsx";
import { Step11Success } from "./Step11Success.tsx";
import { StepStub } from "./StepStub.tsx";
import { WizardReplaySink } from "./state.ts";
import {
  WIZARD_STEP_IDS,
  type WizardPath,
  type WizardRun,
  type WizardStepId,
} from "./types.ts";
import { applicableStepsFor, computeWizardState } from "./wizard-machine.ts";
import "./WizardShell.css";

export interface WizardShellProps {
  /** Replay sink for the run state. The shell creates a default
   *  in-memory sink when not supplied (used by tests + early bring-up). */
  readonly sink?: WizardReplaySink;
  /** Optional persistence side effect — called after every checkpoint
   *  with the current run. Production wires this to the SettingsBridge.
   *  Default is a no-op so tests don't need to mock the IPC layer. */
  readonly persist?: (run: WizardRun) => void;
  /** Called when the user finishes (clicks "Open Hoopoe" on step 11).
   *  Production wires this to a router redirect into the picker. */
  readonly onComplete?: (path: WizardPath | null) => void;
  /** Optional initial run (used by tests to inject canned checkpoint
   *  histories). When omitted, the shell starts a fresh run. */
  readonly initialRunId?: string;
}

const STEP_TITLES: Record<WizardStepId, string> = {
  path: "Pick a path",
  ssh_key: "SSH key",
  rent_vps: "Rent a VPS (docs)",
  vps_connect: "Connect VPS",
  preflight: "Pre-flight check",
  acfs_install: "Install ACFS",
  reconnect: "Reconnect",
  verify_key: "Verify key",
  status_check: "Status check",
  extensions: "Hoopoe extensions",
  success: "Ready",
};

const STEP_FOLLOWUPS: Record<WizardStepId, string | null> = {
  path: null,
  ssh_key: "hp-o6q-ssh", // filed at hp-o6q close
  rent_vps: "hp-o6q-docs",
  vps_connect: "hp-o6q-vps",
  preflight: "hp-o6q-pre",
  acfs_install: "hp-o6q-acfs",
  reconnect: "hp-o6q-rec",
  verify_key: "hp-o6q-vk",
  status_check: "hp-o6q-stat",
  extensions: "hp-o6q-ext",
  success: null,
};

export function WizardShell({ initialRunId, onComplete, persist, sink: providedSink }: WizardShellProps) {
  const sink = useMemo(() => providedSink ?? new WizardReplaySink(), [providedSink]);

  // Initialize an active run if the sink doesn't have one. The runId is
  // a deterministic timestamp so tests can pass `initialRunId` to pin
  // the value.
  const [, setTick] = useState(0);
  const refreshUi = useCallback(() => setTick((n) => n + 1), []);

  if (sink.active() === null) {
    sink.beginRun({ runId: initialRunId ?? `wizard-${Date.now()}` });
  }
  const run = sink.active()!;
  const computed = computeWizardState(run);

  const recordPathPick = useCallback(
    (path: WizardPath) => {
      sink.recordActivePath(path);
      const next = sink.recordCheckpoint({
        stepId: "path",
        outcome: "completed",
        data: { path },
      });
      persist?.(next);
      refreshUi();
    },
    [persist, refreshUi, sink],
  );

  const advanceFromStub = useCallback(
    (stepId: WizardStepId, outcome: "completed" | "skipped") => {
      const next = sink.recordCheckpoint({ stepId, outcome });
      persist?.(next);
      refreshUi();
    },
    [persist, refreshUi, sink],
  );

  const finish = useCallback(() => {
    const next = sink.recordCheckpoint({ stepId: "success", outcome: "completed" });
    persist?.(next);
    onComplete?.(run.path);
  }, [onComplete, persist, run.path, sink]);

  return (
    <section
      aria-labelledby="hh-wizard-title"
      className="hh-wizard"
      data-testid="wizard"
      data-current-step={computed.currentStep}
      data-terminal={computed.terminal}
    >
      <header className="hh-wizard-header">
        <span className="hh-stage-kicker">STAGE 00 — CONNECT</span>
        <h1 id="hh-wizard-title">First-run wizard</h1>
        <p>Resume from where you left off — checkpoints persist across restarts.</p>
      </header>

      <div className="hh-wizard-layout">
        <Stepper computed={computed} />
        <div className="hh-wizard-body" data-testid="wizard-body">
          {computed.currentStep === "path" ? (
            <PathStep onPick={recordPathPick} value={run.path} />
          ) : computed.currentStep === "success" ? (
            <Step11Success
              onEnterCockpit={finish}
              path={run.path}
            />
          ) : (
            <StubStep
              onAdvance={advanceFromStub}
              path={run.path}
              stepId={computed.currentStep}
            />
          )}
        </div>
      </div>

      {computed.resumable && computed.lastCheckpoint?.failure ? (
        <div className="hh-wizard-resume-banner" data-testid="wizard-resume-banner" role="alert">
          <AlertTriangle size={16} strokeWidth={2.1} />
          <div>
            <strong>Step failed: {computed.lastCheckpoint.failure.code}</strong>
            <p>{computed.lastCheckpoint.failure.message}</p>
          </div>
        </div>
      ) : null}
    </section>
  );
}

function Stepper({ computed }: { readonly computed: ReturnType<typeof computeWizardState> }) {
  const completedSet = new Set(computed.completedSteps);
  return (
    <nav aria-label="Wizard step list" className="hh-wizard-stepper">
      <ol>
        {computed.applicableSteps.map((stepId, idx) => {
          const isCurrent = stepId === computed.currentStep && !computed.terminal;
          const isCompleted = completedSet.has(stepId);
          return (
            <li
              className="hh-wizard-stepper-item"
              data-current={isCurrent}
              data-completed={isCompleted}
              data-testid={`wizard-stepper-${stepId}`}
              key={stepId}
            >
              <span aria-hidden="true" className="hh-wizard-stepper-icon">
                {isCompleted ? (
                  <CheckCircle2 size={14} strokeWidth={2.2} />
                ) : isCurrent ? (
                  <ChevronRight size={14} strokeWidth={2.2} />
                ) : (
                  <Circle size={14} strokeWidth={2.2} />
                )}
              </span>
              <span className="hh-wizard-stepper-step">
                Step {String(idx + 1).padStart(2, "0")}
              </span>
              <strong>{STEP_TITLES[stepId]}</strong>
              {STEP_FOLLOWUPS[stepId] !== null && !isCompleted ? (
                <span aria-label="Follow-up bead" className="hh-wizard-stepper-followup">
                  <MoreHorizontal size={12} strokeWidth={2.2} />
                </span>
              ) : null}
            </li>
          );
        })}
      </ol>
    </nav>
  );
}

interface PathStepProps {
  readonly value: WizardPath | null;
  readonly onPick: (path: WizardPath) => void;
}

function PathStep({ onPick, value }: PathStepProps) {
  const [pending, setPending] = useState<WizardPath | null>(value);
  return (
    <Step1PathPicker
      onChange={setPending}
      onContinue={() => {
        if (pending) onPick(pending);
      }}
      value={pending}
    />
  );
}

interface StubStepProps {
  readonly stepId: WizardStepId;
  readonly path: WizardPath | null;
  readonly onAdvance: (stepId: WizardStepId, outcome: "completed" | "skipped") => void;
}

function StubStep({ onAdvance, path, stepId }: StubStepProps) {
  const stepNumber = WIZARD_STEP_IDS.indexOf(stepId) + 1;
  // The local-demo path skips most steps; surface a Skip button on
  // those so the user can fast-forward to success.
  const allowSkip = path === "local_demo" && stepId !== "ssh_key" && stepId !== "extensions";
  return (
    <StepStub
      followupBead={STEP_FOLLOWUPS[stepId] ?? "(none)"}
      onMarkComplete={() => onAdvance(stepId, "completed")}
      stepId={stepId}
      stepNumber={stepNumber}
      title={STEP_TITLES[stepId]}
      {...(allowSkip ? { onSkip: () => onAdvance(stepId, "skipped") } : {})}
    />
  );
}

export { applicableStepsFor };
