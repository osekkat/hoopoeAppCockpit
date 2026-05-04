// hp-9z45 — Wizard bootstrap stream steps.
//
// These components replace the preflight / ACFS install / reconnect /
// verify-key StepStubs with typed, checkpoint-producing UI. The daemon
// endpoints are still exposed as planned/stub routes, so production
// wiring is intentionally behind `window.hoopoe.bootstrap`.

import { useState } from "react";
import {
  Activity,
  CheckCircle2,
  CircleDashed,
  Loader2,
  RefreshCcw,
  ShieldCheck,
  TerminalSquare,
  XCircle,
} from "lucide-react";
import type { WizardStepId } from "./types.ts";

export type BootstrapStepId = Extract<
  WizardStepId,
  "preflight" | "acfs_install" | "reconnect" | "verify_key"
>;

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

export interface BootstrapStepSelection {
  readonly stepId: BootstrapStepId;
  readonly summary: string;
  readonly terminalSeq: number;
  readonly phases: readonly BootstrapStreamEvent[];
  readonly evidenceRefs: readonly string[];
  readonly resumeHint?: string;
}

export interface BootstrapStepCheckpointData extends Record<string, unknown> {
  readonly stepId: BootstrapStepId;
  readonly summary: string;
  readonly terminalSeq: number;
  readonly phaseCount: number;
  readonly failedPhaseIds: readonly string[];
  readonly warningPhaseIds: readonly string[];
  readonly evidenceRefs: readonly string[];
  readonly resumeHint: string | null;
}

export interface BootstrapStepFailure {
  readonly code: string;
  readonly message: string;
}

interface BootstrapStepConfig {
  readonly title: string;
  readonly kicker: string;
  readonly description: string;
  readonly endpointLabel: string;
  readonly primaryLabel: string;
  readonly successLabel: string;
  readonly emptyLabel: string;
  readonly method: keyof BootstrapStepBridge;
  readonly Icon: typeof Activity;
}

export const BOOTSTRAP_STEP_CONFIGS: Record<BootstrapStepId, BootstrapStepConfig> = {
  preflight: {
    title: "Pre-flight checks",
    kicker: "STEP 05",
    description:
      "Run the cheap VPS checks before touching ACFS: SSH identity, writable workspace, required tool paths, and basic daemon reachability.",
    endpointLabel: "POST /v1/bootstrap/preflight",
    primaryLabel: "Run pre-flight",
    successLabel: "Pre-flight passed",
    emptyLabel: "No pre-flight checks have streamed yet.",
    method: "preflight",
    Icon: ShieldCheck,
  },
  acfs_install: {
    title: "Install ACFS",
    kicker: "STEP 06",
    description:
      "Stream the structured ACFS installer phases and preserve the raw parser evidence for Diagnostics.",
    endpointLabel: "POST /v1/bootstrap/acfs/start",
    primaryLabel: "Install ACFS",
    successLabel: "ACFS installed",
    emptyLabel: "Installer phases will appear here as the daemon parses them.",
    method: "acfsInstall",
    Icon: TerminalSquare,
  },
  reconnect: {
    title: "Reconnect after install",
    kicker: "STEP 07",
    description:
      "Drop the bootstrap tunnel, reconnect through the installed daemon, and replay missed events with the current sequence cursor.",
    endpointLabel: "tunnel reconnect + /v1/events/replay",
    primaryLabel: "Reconnect",
    successLabel: "Reconnected",
    emptyLabel: "Reconnect events will appear here.",
    method: "reconnect",
    Icon: RefreshCcw,
  },
  verify_key: {
    title: "Verify ACFS key",
    kicker: "STEP 08",
    description:
      "Render the structured acfs doctor output and checkpoint the evidence refs used to prove the VPS is ready for the cockpit.",
    endpointLabel: "acfs doctor --json",
    primaryLabel: "Verify key",
    successLabel: "Key verified",
    emptyLabel: "Doctor checks will appear here after verification starts.",
    method: "verifyKey",
    Icon: Activity,
  },
};

export interface StepBootstrapStreamProps {
  readonly stepId: BootstrapStepId;
  readonly runId: string;
  readonly onComplete: (selection: BootstrapStepSelection) => void;
  readonly onFailed: (failure: BootstrapStepFailure) => void;
  readonly bridge?: BootstrapStepBridge;
  readonly initialEvents?: readonly BootstrapStreamEvent[];
}

export function StepBootstrapStream({
  bridge,
  initialEvents = [],
  onComplete,
  onFailed,
  runId,
  stepId,
}: StepBootstrapStreamProps) {
  const config = BOOTSTRAP_STEP_CONFIGS[stepId];
  const resolvedBridge = bridge ?? getDefaultBridge();
  const [events, setEvents] = useState<readonly BootstrapStreamEvent[]>(initialEvents);
  const [running, setRunning] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const summary = summarizeBootstrapEvents(events);
  const Icon = config.Icon;

  async function handleRun(): Promise<void> {
    setRunning(true);
    setError(null);
    setEvents([]);
    const collected: BootstrapStreamEvent[] = [];
    try {
      const result = await invokeBootstrapStep(
        resolvedBridge,
        { runId, stepId },
        (event) => {
          collected.push(event);
          setEvents([...collected]);
        },
      );
      const phases = result.events.length > 0 ? result.events : collected;
      setEvents(phases);
      if (result.outcome === "failed") {
        const failure = buildBootstrapFailure(stepId, result.summary, phases);
        setError(failure.message);
        onFailed(failure);
        return;
      }
      onComplete({
        stepId,
        summary: result.summary,
        terminalSeq: summarizeBootstrapEvents(phases).terminalSeq,
        phases,
        evidenceRefs: result.evidenceRefs ?? evidenceRefsFromEvents(phases),
        ...(result.resumeHint ? { resumeHint: result.resumeHint } : {}),
      });
    } catch (err) {
      const failure = buildBootstrapFailure(stepId, (err as Error).message, collected);
      setError(failure.message);
      onFailed(failure);
    } finally {
      setRunning(false);
    }
  }

  return (
    <section
      aria-labelledby={`hh-step-${stepId}-title`}
      className="hh-wizard-step hh-wizard-bootstrap-step"
      data-bootstrap-step={stepId}
      data-testid={`wizard-step-${stepId}`}
    >
      <header className="hh-wizard-step-header">
        <span className="hh-stage-kicker">{config.kicker}</span>
        <h2 id={`hh-step-${stepId}-title`}>{config.title}</h2>
        <p>{config.description}</p>
      </header>

      <div className="hh-wizard-bootstrap-summary" data-status={summary.status}>
        <Icon size={18} strokeWidth={2.1} />
        <div>
          <strong>{summary.status === "passed" ? config.successLabel : config.endpointLabel}</strong>
          <p>
            {summary.total === 0
              ? config.emptyLabel
              : `${summary.passed} passed, ${summary.warning} warnings, ${summary.failed} failed`}
          </p>
        </div>
      </div>

      <BootstrapPhaseList events={events} />

      {error ? (
        <div className="hh-wizard-resume-banner hh-wizard-resume-banner-inline" role="alert">
          <XCircle size={16} strokeWidth={2.1} />
          <div>
            <strong>Bootstrap step failed</strong>
            <p>{error}</p>
          </div>
        </div>
      ) : null}

      <div className="hh-wizard-step-actions">
        <button
          className="hh-wizard-primary"
          data-testid={`wizard-${stepId}-run`}
          disabled={running}
          onClick={() => void handleRun()}
          type="button"
        >
          {running ? (
            <Loader2 className="hh-spin" size={14} strokeWidth={2.1} />
          ) : (
            <Icon size={14} strokeWidth={2.1} />
          )}
          {running ? "Streaming..." : config.primaryLabel}
        </button>
        {resolvedBridge ? null : (
          <span className="hh-wizard-bootstrap-bridge-note">
            Waiting for the bootstrap preload bridge.
          </span>
        )}
      </div>
    </section>
  );
}

export function StepPreflight(props: Omit<StepBootstrapStreamProps, "stepId">) {
  return <StepBootstrapStream {...props} stepId="preflight" />;
}

export function StepAcfsInstall(props: Omit<StepBootstrapStreamProps, "stepId">) {
  return <StepBootstrapStream {...props} stepId="acfs_install" />;
}

export function StepReconnect(props: Omit<StepBootstrapStreamProps, "stepId">) {
  return <StepBootstrapStream {...props} stepId="reconnect" />;
}

export function StepVerifyKey(props: Omit<StepBootstrapStreamProps, "stepId">) {
  return <StepBootstrapStream {...props} stepId="verify_key" />;
}

function BootstrapPhaseList({ events }: { readonly events: readonly BootstrapStreamEvent[] }) {
  if (events.length === 0) {
    return (
      <ol className="hh-wizard-bootstrap-phases" data-empty="true">
        <li>
          <CircleDashed size={15} strokeWidth={2.1} />
          <span>Waiting for stream</span>
        </li>
      </ol>
    );
  }
  return (
    <ol className="hh-wizard-bootstrap-phases">
      {events.map((event) => (
        <li data-status={event.status} key={`${event.seq}:${event.phaseId}`}>
          {iconForStatus(event.status)}
          <div>
            <strong>{event.label}</strong>
            {event.message ? <p>{event.message}</p> : null}
            {event.detail ? <p>{event.detail}</p> : null}
            {event.evidenceRef ? <code>{event.evidenceRef}</code> : null}
            {event.doctor ? <DoctorChecks checks={event.doctor} /> : null}
          </div>
        </li>
      ))}
    </ol>
  );
}

function DoctorChecks({ checks }: { readonly checks: readonly BootstrapDoctorCheck[] }) {
  return (
    <dl className="hh-wizard-bootstrap-doctor">
      {checks.map((check) => (
        <div data-status={check.status} key={check.id}>
          <dt>{check.label}</dt>
          <dd>{check.detail ?? check.status}</dd>
        </div>
      ))}
    </dl>
  );
}

function iconForStatus(status: BootstrapPhaseStatus) {
  switch (status) {
    case "passed":
      return <CheckCircle2 size={15} strokeWidth={2.1} />;
    case "failed":
      return <XCircle size={15} strokeWidth={2.1} />;
    case "running":
      return <Loader2 className="hh-spin" size={15} strokeWidth={2.1} />;
    case "warning":
      return <Activity size={15} strokeWidth={2.1} />;
    case "pending":
    case "skipped":
      return <CircleDashed size={15} strokeWidth={2.1} />;
  }
}

export function isBootstrapStepId(stepId: WizardStepId): stepId is BootstrapStepId {
  return stepId === "preflight" || stepId === "acfs_install" || stepId === "reconnect" || stepId === "verify_key";
}

export function summarizeBootstrapEvents(events: readonly BootstrapStreamEvent[]): {
  readonly status: "idle" | "running" | "passed" | "warning" | "failed";
  readonly total: number;
  readonly passed: number;
  readonly warning: number;
  readonly failed: number;
  readonly terminalSeq: number;
} {
  let passed = 0;
  let warning = 0;
  let failed = 0;
  let running = 0;
  let terminalSeq = 0;
  for (const event of events) {
    terminalSeq = Math.max(terminalSeq, event.seq);
    if (event.status === "passed") passed += 1;
    if (event.status === "warning") warning += 1;
    if (event.status === "failed") failed += 1;
    if (event.status === "running" || event.status === "pending") running += 1;
  }
  const total = events.length;
  const status =
    total === 0
      ? "idle"
      : failed > 0
        ? "failed"
        : running > 0
          ? "running"
          : warning > 0
            ? "warning"
            : "passed";
  return { status, total, passed, warning, failed, terminalSeq };
}

export function buildBootstrapCheckpointData(
  selection: BootstrapStepSelection,
): BootstrapStepCheckpointData {
  const failedPhaseIds = selection.phases
    .filter((phase) => phase.status === "failed")
    .map((phase) => phase.phaseId);
  const warningPhaseIds = selection.phases
    .filter((phase) => phase.status === "warning")
    .map((phase) => phase.phaseId);
  return {
    stepId: selection.stepId,
    summary: selection.summary,
    terminalSeq: selection.terminalSeq,
    phaseCount: selection.phases.length,
    failedPhaseIds,
    warningPhaseIds,
    evidenceRefs: selection.evidenceRefs,
    resumeHint: selection.resumeHint ?? null,
  };
}

export function buildBootstrapFailure(
  stepId: BootstrapStepId,
  message: string,
  events: readonly BootstrapStreamEvent[],
): BootstrapStepFailure {
  const failed = events.findLast((event) => event.status === "failed");
  const detail = failed?.message ?? failed?.detail ?? message;
  return {
    code: `bootstrap_${stepId}_failed`,
    message: detail || `${BOOTSTRAP_STEP_CONFIGS[stepId].title} failed.`,
  };
}

export async function invokeBootstrapStep(
  bridge: BootstrapStepBridge | null,
  input: BootstrapStepRunInput,
  sink: BootstrapEventSink,
): Promise<BootstrapStepResult> {
  const method = BOOTSTRAP_STEP_CONFIGS[input.stepId].method;
  const action = bridge?.[method];
  if (!action) {
    throw new Error(`${BOOTSTRAP_STEP_CONFIGS[input.stepId].endpointLabel} bridge is not wired.`);
  }
  return await action(input, sink);
}

function evidenceRefsFromEvents(events: readonly BootstrapStreamEvent[]): readonly string[] {
  return events
    .map((event) => event.evidenceRef)
    .filter((ref): ref is string => typeof ref === "string" && ref.length > 0);
}

interface BootstrapBridgeShape {
  readonly bootstrap?: BootstrapStepBridge;
}

function getDefaultBridge(): BootstrapStepBridge | null {
  if (typeof window === "undefined") return null;
  const bootstrap = (window as Window & { readonly hoopoe?: BootstrapBridgeShape }).hoopoe?.bootstrap;
  if (!bootstrap || typeof bootstrap !== "object") return null;
  return bootstrap;
}
