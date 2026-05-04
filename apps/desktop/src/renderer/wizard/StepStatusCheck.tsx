// hp-zsp1 - Wizard status-check step.
//
// This is the renderer contract for step 09. The default bridge reads the
// daemon capability registry; tests can inject the bridge so the component
// stays independent from Electron preload wiring.

import { useState } from "react";
import { Activity, CheckCircle2, Loader2, RefreshCcw, TriangleAlert, XCircle } from "lucide-react";
import type { LucideIcon } from "lucide-react";
import type {
  Capability,
  CapabilityRegistry,
  ToolId,
  ToolReport,
} from "@hoopoe/schemas";

export type StatusCheckSeverity = "ready" | "warning" | "missing";

export interface StatusCheckToolRow {
  readonly id: ToolId;
  readonly label: string;
  readonly status: StatusCheckSeverity;
  readonly version: string;
  readonly capabilityCount: number;
  readonly okCount: number;
  readonly notes: string | null;
}

export interface StatusCheckSubscriptionSummary {
  readonly status: StatusCheckSeverity;
  readonly label: string;
  readonly notes: string | null;
}

export interface StatusCheckResult {
  readonly summary: string;
  readonly checkedAt: string;
  readonly tools: readonly StatusCheckToolRow[];
  readonly subscription: StatusCheckSubscriptionSummary;
}

export interface StatusCheckSelection extends StatusCheckResult {}

export interface StatusCheckCheckpointData extends Record<string, unknown> {
  readonly summary: string;
  readonly checkedAt: string;
  readonly readyTools: readonly string[];
  readonly warningTools: readonly string[];
  readonly missingTools: readonly string[];
  readonly subscriptionStatus: StatusCheckSeverity;
}

export interface StatusCheckBridge {
  readonly load: () => Promise<StatusCheckResult>;
}

export interface StepStatusCheckProps {
  readonly onComplete: (selection: StatusCheckSelection) => void;
  readonly onFailed: (failure: { readonly code: string; readonly message: string }) => void;
  readonly bridge?: StatusCheckBridge;
  readonly initialResult?: StatusCheckResult;
}

const REQUIRED_TOOLS: readonly { readonly id: ToolId; readonly label: string }[] = [
  { id: "git", label: "Git" },
  { id: "br", label: "Beads" },
  { id: "bv", label: "bv graph triage" },
  { id: "ntm", label: "NTM swarm control" },
  { id: "agent_mail", label: "Agent Mail" },
  { id: "caam", label: "CAAM subscriptions" },
  { id: "jsm", label: "jsm skill loader" },
  { id: "jfp", label: "jfp fallback" },
  { id: "oracle", label: "Oracle browser engine" },
  { id: "rch", label: "RCH builds" },
];

const STATUS_ICONS: Record<StatusCheckSeverity, LucideIcon> = {
  ready: CheckCircle2,
  warning: TriangleAlert,
  missing: XCircle,
};

export function StepStatusCheck({
  bridge,
  initialResult,
  onComplete,
  onFailed,
}: StepStatusCheckProps) {
  const resolvedBridge = bridge ?? defaultStatusCheckBridge();
  const [result, setResult] = useState<StatusCheckResult | null>(initialResult ?? null);
  const [running, setRunning] = useState(false);
  const [error, setError] = useState<string | null>(null);

  async function runCheck(): Promise<void> {
    if (!resolvedBridge) {
      const failure = {
        code: "status_check_bridge_unavailable",
        message: "The daemon capability bridge is not wired.",
      };
      setError(failure.message);
      onFailed(failure);
      return;
    }
    setRunning(true);
    setError(null);
    try {
      const next = await resolvedBridge.load();
      setResult(next);
      if (next.tools.some((tool) => tool.status === "missing")) {
        const failure = {
          code: "status_check_missing_tools",
          message: next.summary,
        };
        setError(failure.message);
        onFailed(failure);
        return;
      }
      onComplete(next);
    } catch (err) {
      const failure = {
        code: "status_check_failed",
        message: err instanceof Error ? err.message : String(err),
      };
      setError(failure.message);
      onFailed(failure);
    } finally {
      setRunning(false);
    }
  }

  const counts = result ? summarizeStatusRows(result.tools) : null;

  return (
    <section
      aria-labelledby="hh-wizard-status-check-title"
      className="hh-wizard-step hh-wizard-status-step"
      data-testid="wizard-step-status_check"
    >
      <header className="hh-wizard-step-header">
        <span className="hh-stage-kicker">STEP 09</span>
        <h2 id="hh-wizard-status-check-title">Status check</h2>
        <p>
          Read the daemon capability registry and confirm the toolchain pieces
          Hoopoe needs before opening the cockpit.
        </p>
      </header>

      <div className="hh-wizard-status-summary" data-status={counts?.status ?? "idle"}>
        <Activity size={18} strokeWidth={2.1} />
        <div>
          <strong>{result?.summary ?? "Capability registry not checked yet"}</strong>
          <p>
            {counts
              ? `${counts.ready} ready, ${counts.warning} warnings, ${counts.missing} missing`
              : "Checks the daemon's current capability snapshot."}
          </p>
        </div>
      </div>

      {result ? <StatusToolRows result={result} /> : null}

      {error ? (
        <div className="hh-wizard-resume-banner hh-wizard-resume-banner-inline" role="alert">
          <TriangleAlert size={16} strokeWidth={2.1} />
          <div>
            <strong>Status check needs attention</strong>
            <p>{error}</p>
          </div>
        </div>
      ) : null}

      <div className="hh-wizard-step-actions">
        <button
          className="hh-wizard-primary"
          data-testid="wizard-status-check-run"
          disabled={running}
          onClick={() => void runCheck()}
          type="button"
        >
          {running ? (
            <Loader2 className="hh-spin" size={14} strokeWidth={2.1} />
          ) : (
            <RefreshCcw size={14} strokeWidth={2.1} />
          )}
          {running ? "Checking..." : "Check tools"}
        </button>
      </div>
    </section>
  );
}

function StatusToolRows({ result }: { readonly result: StatusCheckResult }) {
  return (
    <div className="hh-wizard-status-grid" data-testid="wizard-status-check-results">
      {result.tools.map((tool) => (
        <article className="hh-wizard-status-row" data-status={tool.status} key={tool.id}>
          {statusIcon(tool.status)}
          <div>
            <strong>{tool.label}</strong>
            <p>
              {tool.version ? `Version ${tool.version}` : "Version unknown"} -{" "}
              {tool.okCount}/{tool.capabilityCount} capabilities ok
            </p>
            {tool.notes ? <small>{tool.notes}</small> : null}
          </div>
        </article>
      ))}
      <article className="hh-wizard-status-row" data-status={result.subscription.status}>
        {statusIcon(result.subscription.status)}
        <div>
          <strong>{result.subscription.label}</strong>
          <p>{result.subscription.notes ?? "Subscription-backed CLI access is available."}</p>
        </div>
      </article>
    </div>
  );
}

function statusIcon(status: StatusCheckSeverity) {
  const Icon = STATUS_ICONS[status];
  return <Icon size={16} strokeWidth={2.1} />;
}

export function deriveStatusCheckResult(registry: CapabilityRegistry): StatusCheckResult {
  const tools = REQUIRED_TOOLS.map((spec) => buildToolRow(spec.id, spec.label, registry.tools?.[spec.id]));
  const counts = summarizeStatusRows(tools);
  const subscription = buildSubscriptionSummary(registry.tools?.caam);
  const summary =
    counts.missing > 0
      ? `${counts.missing} required tools missing`
      : counts.warning > 0 || subscription.status !== "ready"
        ? "Tool inventory passed with warnings"
        : "Tool inventory passed";
  return {
    summary,
    checkedAt: registry.snapshotAt,
    tools,
    subscription,
  };
}

export function buildStatusCheckCheckpointData(
  selection: StatusCheckSelection,
): StatusCheckCheckpointData {
  return {
    summary: selection.summary,
    checkedAt: selection.checkedAt,
    readyTools: selection.tools.filter((tool) => tool.status === "ready").map((tool) => tool.id),
    warningTools: selection.tools.filter((tool) => tool.status === "warning").map((tool) => tool.id),
    missingTools: selection.tools.filter((tool) => tool.status === "missing").map((tool) => tool.id),
    subscriptionStatus: selection.subscription.status,
  };
}

function buildToolRow(id: ToolId, label: string, report: ToolReport | undefined): StatusCheckToolRow {
  if (!report) {
    return {
      id,
      label,
      status: "missing",
      version: "",
      capabilityCount: 0,
      okCount: 0,
      notes: "Tool report absent from /v1/capabilities.",
    };
  }
  const caps = Object.values(report.capabilities ?? {});
  const status = summarizeCapabilities(caps);
  const note = caps.find((capability) => capability.notes)?.notes ?? null;
  return {
    id,
    label,
    status,
    version: report.version,
    capabilityCount: caps.length,
    okCount: caps.filter((capability) => capability.status === "ok").length,
    notes: note,
  };
}

function summarizeCapabilities(capabilities: readonly Capability[]): StatusCheckSeverity {
  if (capabilities.length === 0) return "missing";
  if (capabilities.every((capability) => capability.status === "ok")) return "ready";
  if (capabilities.some((capability) => capability.status === "ok" || capability.status === "degraded")) {
    return "warning";
  }
  return "missing";
}

function buildSubscriptionSummary(report: ToolReport | undefined): StatusCheckSubscriptionSummary {
  if (!report) {
    return {
      status: "missing",
      label: "CAAM subscription-backed CLI access",
      notes: "CAAM did not report into the capability registry.",
    };
  }
  const status = buildToolRow("caam", "CAAM", report).status;
  return {
    status,
    label: "CAAM subscription-backed CLI access",
    notes:
      status === "ready"
        ? null
        : "Open Settings after onboarding to finish CLI account login or resolve CAAM warnings.",
  };
}

function summarizeStatusRows(rows: readonly StatusCheckToolRow[]): {
  readonly status: "ready" | "warning" | "missing";
  readonly ready: number;
  readonly warning: number;
  readonly missing: number;
} {
  const ready = rows.filter((row) => row.status === "ready").length;
  const warning = rows.filter((row) => row.status === "warning").length;
  const missing = rows.filter((row) => row.status === "missing").length;
  return {
    status: missing > 0 ? "missing" : warning > 0 ? "warning" : "ready",
    ready,
    warning,
    missing,
  };
}

interface DaemonBridgeShape {
  readonly daemon?: {
    readonly request?: <I, O>(method: "capabilities", body: I) => Promise<O>;
  };
}

function defaultStatusCheckBridge(): StatusCheckBridge | null {
  if (typeof window === "undefined") return null;
  const request = (window as Window & { readonly hoopoe?: DaemonBridgeShape }).hoopoe?.daemon?.request;
  if (!request) return null;
  return {
    load: async () => deriveStatusCheckResult(await request<null, CapabilityRegistry>("capabilities", null)),
  };
}
