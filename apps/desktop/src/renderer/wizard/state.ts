// hp-o6q — Wizard run state IO.
//
// The store is in-memory in the renderer; persistence ultimately lives in
// `~/.hoopoe/userdata/client-settings.json` under `wizard.<runId>.checkpoints[]`.
// The renderer goes through the preload bridge (`window.hoopoe.settings.set`)
// to persist; for tests + early bring-up the in-memory store is enough.
//
// This module owns:
//   - Pure helpers to start a new run, append checkpoints, mark a path,
//     and serialize the state file shape.
//   - A small in-memory ReplaySink so the renderer can render before the
//     preload bridge is wired (covered by hp-rflj follow-up).

import {
  CHECKPOINT_OUTCOMES,
  WIZARD_STATE_SCHEMA_VERSION,
  type CheckpointEvent,
  type CheckpointFailure,
  type CheckpointOutcome,
  type WizardPath,
  type WizardRun,
  type WizardStateFile,
  type WizardStepId,
} from "./types.ts";

export class WizardStateError extends Error {
  override readonly name = "WizardStateError";
  readonly code: string;
  constructor(code: string, message: string) {
    super(`wizard-state (${code}): ${message}`);
    this.code = code;
  }
}

export interface StartRunInput {
  readonly runId: string;
  readonly now?: () => Date;
}

/** Fresh wizard run with no checkpoints yet. */
export function startRun(input: StartRunInput): WizardRun {
  if (input.runId.trim().length === 0) {
    throw new WizardStateError("empty_run_id", "runId cannot be empty");
  }
  const ts = (input.now ?? (() => new Date()))().toISOString();
  return {
    runId: input.runId,
    startedAt: ts,
    path: null,
    checkpoints: [],
  };
}

export interface AppendCheckpointInput {
  readonly stepId: WizardStepId;
  readonly outcome: CheckpointOutcome;
  readonly data?: Record<string, unknown>;
  readonly failure?: CheckpointFailure;
  readonly now?: () => Date;
}

/** Append a CheckpointEvent to a run, returning a new WizardRun (no
 *  in-place mutation). */
export function appendCheckpoint(run: WizardRun, input: AppendCheckpointInput): WizardRun {
  if (!CHECKPOINT_OUTCOMES.includes(input.outcome)) {
    throw new WizardStateError(
      "invalid_outcome",
      `outcome must be one of ${CHECKPOINT_OUTCOMES.join(", ")}; got ${JSON.stringify(input.outcome)}`,
    );
  }
  const event: CheckpointEvent = {
    stepId: input.stepId,
    outcome: input.outcome,
    recordedAt: (input.now ?? (() => new Date()))().toISOString(),
    ...(input.data !== undefined ? { data: input.data } : {}),
    ...(input.failure !== undefined ? { failure: input.failure } : {}),
  };
  return { ...run, checkpoints: [...run.checkpoints, event] };
}

/** Record the user's path-picker choice from step 1. The state machine
 *  uses this to decide which subsequent steps apply. */
export function recordPath(run: WizardRun, path: WizardPath): WizardRun {
  return { ...run, path };
}

/** Build a serializable file shape from one or more runs. */
export function toStateFile(runs: readonly WizardRun[]): WizardStateFile {
  return { schemaVersion: WIZARD_STATE_SCHEMA_VERSION, runs };
}

/** Parse a state file shape back into runs. Throws on schema mismatch. */
export function fromStateFile(value: unknown): readonly WizardRun[] {
  if (typeof value !== "object" || value === null) {
    throw new WizardStateError("not_object", "wizard state must be an object");
  }
  const file = value as WizardStateFile;
  if (file.schemaVersion !== WIZARD_STATE_SCHEMA_VERSION) {
    throw new WizardStateError(
      "schema_mismatch",
      `schemaVersion must be ${WIZARD_STATE_SCHEMA_VERSION}; got ${JSON.stringify(file.schemaVersion)}`,
    );
  }
  if (!Array.isArray(file.runs)) {
    throw new WizardStateError("not_array", "runs must be an array");
  }
  return file.runs;
}

/** In-memory replay sink — keeps a single active run plus historical
 *  runs. The renderer wires this to the SettingsBridge so writes are
 *  durable; tests use the in-memory shape directly. */
export class WizardReplaySink {
  private runs: WizardRun[];

  constructor(initial: readonly WizardRun[] = []) {
    this.runs = [...initial];
  }

  list(): readonly WizardRun[] {
    return this.runs;
  }

  active(): WizardRun | null {
    return this.runs.length > 0 ? this.runs[this.runs.length - 1] ?? null : null;
  }

  /** Start a new run. The previous run (if any) becomes historical. */
  beginRun(input: StartRunInput): WizardRun {
    const run = startRun(input);
    this.runs.push(run);
    return run;
  }

  /** Append a checkpoint to the active run. Throws if no run is active. */
  recordCheckpoint(input: AppendCheckpointInput): WizardRun {
    const current = this.active();
    if (!current) {
      throw new WizardStateError("no_active_run", "call beginRun before recordCheckpoint");
    }
    const next = appendCheckpoint(current, input);
    this.replaceActive(next);
    return next;
  }

  /** Update the active run's path. */
  recordActivePath(path: WizardPath): WizardRun {
    const current = this.active();
    if (!current) {
      throw new WizardStateError("no_active_run", "call beginRun before recordActivePath");
    }
    const next = recordPath(current, path);
    this.replaceActive(next);
    return next;
  }

  toFile(): WizardStateFile {
    return toStateFile(this.runs);
  }

  private replaceActive(run: WizardRun): void {
    if (this.runs.length === 0) {
      this.runs.push(run);
      return;
    }
    this.runs[this.runs.length - 1] = run;
  }
}
