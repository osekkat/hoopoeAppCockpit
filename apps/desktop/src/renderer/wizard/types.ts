// hp-o6q — Phase 3 first-run wizard shared types.
//
// The wizard wraps the canonical 13-step agent-flywheel.com/wizard flow
// with structured checkpoints (plan.md §6.1). Each step writes a
// CheckpointEvent so the UI can resume from the last completed step
// instead of restarting from scratch when the desktop is killed mid-run.
//
// Step state lives in `client-settings.json` under
// `wizard.<runId>.checkpoints[]`. The state machine in
// `wizard-machine.ts` consumes the checkpoint stream to compute the
// current step. The renderer reads the machine and renders the matching
// Step component.

export const WIZARD_STATE_SCHEMA_VERSION = 1 as const;

/** Eleven user-visible steps from §6.1 + a synthetic 'done' terminal.
 *  Step 3 is provider-side instructions for renting a VPS — it's a
 *  read-only docs card but counts as its own step for the resume flow.
 *  Provider-driven 'Provision new VPS' is post-MVP and is hidden in v1
 *  (the path picker shows it disabled with an explanation). */
export const WIZARD_STEP_IDS = [
  "path",          //  1 Path picker (existing VPS / local demo / provision new)
  "ssh_key",       //  2 Generate or import SSH key
  "rent_vps",      //  3 Provider docs cards (read-only)
  "vps_connect",   //  4 Enter VPS host/port/user; tunnel TOFU
  "preflight",     //  5 /v1/bootstrap/preflight stream
  "acfs_install",  //  6 ACFS installer stream
  "reconnect",     //  7 Auto-reconnect after install
  "verify_key",    //  8 acfs doctor --json
  "status_check",  //  9 Tool inventory + CAAM verification
  "extensions",    // 10 Hoopoe extensions (daemon, oracle, jsm/jfp, GitHub auth)
  "success",       // 11 VPS Ready / Local demo Ready
] as const;
export type WizardStepId = (typeof WIZARD_STEP_IDS)[number];

/** First step a fresh wizard starts on. */
export const FIRST_WIZARD_STEP_ID: WizardStepId = "path";

/** Three top-level paths the user picks at step 1. The choice
 *  determines which subsequent steps are skipped (local-demo skips
 *  4-11; provision-new is post-MVP). */
export const WIZARD_PATHS = ["existing_vps", "local_demo", "provision_new"] as const;
export type WizardPath = (typeof WIZARD_PATHS)[number];

export const CHECKPOINT_OUTCOMES = ["completed", "failed", "skipped"] as const;
export type CheckpointOutcome = (typeof CHECKPOINT_OUTCOMES)[number];

export interface CheckpointFailure {
  /** Stable code so callers can render the right "Resume" CTA. */
  readonly code: string;
  /** Human-readable detail; safe to render verbatim. */
  readonly message: string;
}

export interface CheckpointEvent {
  readonly stepId: WizardStepId;
  readonly outcome: CheckpointOutcome;
  /** RFC3339. */
  readonly recordedAt: string;
  /** Per-step payload — kept loose because each step decides what to
   *  persist (e.g., chosen path on `path`, host/port on `vps_connect`,
   *  selected key fingerprint on `ssh_key`). The state machine doesn't
   *  read into this; consumers do. */
  readonly data?: Record<string, unknown>;
  readonly failure?: CheckpointFailure;
}

export interface WizardRun {
  readonly runId: string;
  /** RFC3339 timestamp of the first checkpoint. */
  readonly startedAt: string;
  /** Path picked at step 1, if reached. The state machine uses this to
   *  compute which steps apply (local_demo skips 4-11). */
  readonly path: WizardPath | null;
  readonly checkpoints: readonly CheckpointEvent[];
}

export interface WizardStateFile {
  readonly schemaVersion: typeof WIZARD_STATE_SCHEMA_VERSION;
  /** Multiple historical runs are kept so Diagnostics can replay any of
   *  them. The active run is `runs[runs.length - 1]`. */
  readonly runs: readonly WizardRun[];
}

/** Empty wizard state (no runs yet). */
export function emptyWizardStateFile(): WizardStateFile {
  return { schemaVersion: WIZARD_STATE_SCHEMA_VERSION, runs: [] };
}
