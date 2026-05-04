// hp-o6q — Pure step state machine for the first-run wizard.
//
// The machine is a fold over a CheckpointEvent stream. Given a run, it
// computes:
//   - Which step is currently active (the next un-completed step).
//   - Whether the wizard has reached `success`.
//   - Whether the active step is in a `failed` state and needs Resume.
//   - The set of steps the picked path applies to (local-demo skips the
//     VPS-only steps; existing-VPS uses every step).
//
// All inputs/outputs are plain data — no React, no IPC. The renderer
// wires `WizardShell` to a single source-of-truth WizardRun and reads
// `computeWizardState()` to drive its rendering.

import {
  FIRST_WIZARD_STEP_ID,
  WIZARD_STEP_IDS,
  type CheckpointEvent,
  type WizardPath,
  type WizardRun,
  type WizardStepId,
} from "./types.ts";

/** Steps gated by the wizard path. existing_vps + provision_new run
 *  every step; local_demo skips the VPS-only ones. */
const APPLICABLE_STEPS: Record<WizardPath, ReadonlySet<WizardStepId>> = {
  existing_vps: new Set(WIZARD_STEP_IDS),
  provision_new: new Set(WIZARD_STEP_IDS),
  local_demo: new Set([
    "path",
    "ssh_key",  // optional but the wizard still surfaces it
    "extensions",
    "success",
  ]),
};

export interface WizardComputedState {
  /** Step the renderer should display. When `terminal` is true, this is
   *  always `success`. */
  readonly currentStep: WizardStepId;
  /** True iff the user has reached the success step. */
  readonly terminal: boolean;
  /** True iff the most recent checkpoint for `currentStep` is `failed`
   *  — the renderer renders a Resume CTA in this case. */
  readonly resumable: boolean;
  /** Last checkpoint event for the current step, when one exists. */
  readonly lastCheckpoint: CheckpointEvent | null;
  /** Steps the picked path will visit, in order (always includes
   *  `success`). When no path has been picked yet, returns just
   *  ["path", "success"]. */
  readonly applicableSteps: readonly WizardStepId[];
  /** Steps already completed (in canonical step order). */
  readonly completedSteps: readonly WizardStepId[];
}

/** Compute the wizard's current state from a run + the path the user
 *  picked at step 1 (when known). The machine never assumes a path; if
 *  the user picks one, the state file should record it via
 *  `recordPath()` from `state.ts` before this is called. */
export function computeWizardState(run: WizardRun): WizardComputedState {
  const applicable = applicableStepsFor(run.path);

  // Walk the canonical step order; the current step is the first
  // applicable step whose latest checkpoint is NOT `completed` /
  // `skipped`. Failed checkpoints stop progression — that step becomes
  // the current step and is marked resumable.
  const completed: WizardStepId[] = [];
  let currentStep: WizardStepId = FIRST_WIZARD_STEP_ID;
  let resumable = false;

  for (const stepId of applicable) {
    const last = lastCheckpointForStep(run.checkpoints, stepId);
    if (!last) {
      currentStep = stepId;
      resumable = false;
      break;
    }
    if (last.outcome === "completed" || last.outcome === "skipped") {
      completed.push(stepId);
      continue;
    }
    // Failed: stop here; this step is the current resumable step.
    currentStep = stepId;
    resumable = true;
    break;
  }

  // If we walked every applicable step without breaking, the wizard is
  // terminal — every step (including success) is completed.
  const allCompleted = completed.length === applicable.length;
  if (allCompleted) {
    currentStep = "success";
  }

  const lastCheckpoint = lastCheckpointForStep(run.checkpoints, currentStep);

  return {
    currentStep,
    terminal: allCompleted || currentStep === "success",
    resumable,
    lastCheckpoint,
    applicableSteps: applicable,
    completedSteps: completed,
  };
}

/** Returns the canonical step order filtered to only the steps the
 *  picked path will visit. When `path` is null (user hasn't picked
 *  yet), returns just `[path, success]` so the renderer can show a
 *  minimal sidebar. */
export function applicableStepsFor(path: WizardPath | null): readonly WizardStepId[] {
  if (path === null) return ["path", "success"];
  const set = APPLICABLE_STEPS[path];
  return WIZARD_STEP_IDS.filter((id) => set.has(id));
}

/** Latest checkpoint for a given step, or null. */
export function lastCheckpointForStep(
  checkpoints: readonly CheckpointEvent[],
  stepId: WizardStepId,
): CheckpointEvent | null {
  for (let i = checkpoints.length - 1; i >= 0; i -= 1) {
    const event = checkpoints[i];
    if (event && event.stepId === stepId) return event;
  }
  return null;
}

/** Helper for test fixtures — returns the next step id after `stepId`
 *  in the canonical order, or null if `stepId` is the last. Doesn't
 *  consult applicability; consumers (and the rendering layer) filter
 *  via `applicableStepsFor` separately. */
export function canonicalNextStep(stepId: WizardStepId): WizardStepId | null {
  const idx = WIZARD_STEP_IDS.indexOf(stepId);
  if (idx === -1 || idx === WIZARD_STEP_IDS.length - 1) return null;
  return WIZARD_STEP_IDS[idx + 1] ?? null;
}
