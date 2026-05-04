// hp-o90 — Bootstrap-step bridge protocol types.
//
// These shapes describe the function-call protocol the wizard's
// bootstrap stream steps (apps/desktop/src/renderer/wizard/StepBootstrapStream.tsx)
// invoke through `window.hoopoe.bootstrap`. They live in `shared/` so
// the preload (`apps/desktop/electron/preload.ts`) can declare them on
// `HoopoeBridge` and the renderer can import them from the same source
// — no more parallel `BootstrapBridgeShape` cast that bypasses the
// typed `Window.hoopoe` declaration.
//
// The runtime daemon endpoints
// (POST /v1/bootstrap/preflight, /acfs/start, etc.) are still
// "intentionally not yet wired" per hp-9z45; preload registers handlers
// once those endpoints land. Until then `window.hoopoe.bootstrap` is
// undefined and `getDefaultBridge()` returns null defensively.

/** Steps the bootstrap stream UI can run. Must remain a strict subset
 *  of `WizardStepId` (apps/desktop/src/renderer/wizard/types.ts); the
 *  type-level assertion in StepBootstrapStream pins the relationship. */
export type BootstrapStepId =
  | "preflight"
  | "acfs_install"
  | "reconnect"
  | "verify_key";

export type BootstrapPhaseStatus =
  | "pending"
  | "running"
  | "passed"
  | "warning"
  | "failed"
  | "skipped";

export interface BootstrapDoctorCheck {
  readonly id: string;
  readonly label: string;
  readonly status: "ok" | "warn" | "fail";
  readonly detail?: string;
}

export interface BootstrapStreamEvent {
  readonly seq: number;
  readonly phaseId: string;
  readonly label: string;
  readonly status: BootstrapPhaseStatus;
  readonly message?: string;
  readonly detail?: string;
  readonly evidenceRef?: string;
  readonly doctor?: readonly BootstrapDoctorCheck[];
  readonly recordedAt?: string;
}

export interface BootstrapStepRunInput {
  readonly runId: string;
  readonly stepId: BootstrapStepId;
  readonly sinceSeq?: number;
}

export type BootstrapEventSink = (event: BootstrapStreamEvent) => void;

export interface BootstrapStepResult {
  readonly outcome: "completed" | "failed";
  readonly summary: string;
  readonly events: readonly BootstrapStreamEvent[];
  readonly evidenceRefs?: readonly string[];
  readonly resumeHint?: string;
}

/** The bridge surface published on `window.hoopoe.bootstrap`. Each
 *  field is optional so the UI can render "Waiting for the bootstrap
 *  preload bridge." until preload registers a handler. */
export interface BootstrapStepBridge {
  readonly preflight?: (
    input: BootstrapStepRunInput,
    sink: BootstrapEventSink,
  ) => Promise<BootstrapStepResult>;
  readonly acfsInstall?: (
    input: BootstrapStepRunInput,
    sink: BootstrapEventSink,
  ) => Promise<BootstrapStepResult>;
  readonly reconnect?: (
    input: BootstrapStepRunInput,
    sink: BootstrapEventSink,
  ) => Promise<BootstrapStepResult>;
  readonly verifyKey?: (
    input: BootstrapStepRunInput,
    sink: BootstrapEventSink,
  ) => Promise<BootstrapStepResult>;
}
