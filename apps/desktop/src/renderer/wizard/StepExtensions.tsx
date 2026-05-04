// hp-zsp1 - Wizard extensions step.
//
// Step 10 collects the final Hoopoe-specific readiness checks. The daemon is
// still the execution plane; this renderer component consumes a typed bridge
// and checkpoints the deterministic result.

import { useState } from "react";
import { CheckCircle2, Loader2, PackageCheck, RefreshCcw, TriangleAlert, XCircle } from "lucide-react";
import type { LucideIcon } from "lucide-react";
import type { CapabilityRegistry, ToolId, ToolReport } from "@hoopoe/schemas";

export type ExtensionStepStatus = "ready" | "warning" | "missing";

export interface ExtensionSubstep {
  readonly id: "daemon_service" | "oracle_browser" | "skill_loader" | "github_auth";
  readonly label: string;
  readonly description: string;
  readonly status: ExtensionStepStatus;
  readonly evidence: string;
}

export interface ExtensionsResult {
  readonly summary: string;
  readonly checkedAt: string;
  readonly substeps: readonly ExtensionSubstep[];
}

export interface ExtensionsSelection extends ExtensionsResult {}

export interface ExtensionsCheckpointData extends Record<string, unknown> {
  readonly summary: string;
  readonly checkedAt: string;
  readonly readySubsteps: readonly string[];
  readonly warningSubsteps: readonly string[];
  readonly missingSubsteps: readonly string[];
}

export interface ExtensionsBridge {
  readonly verify: () => Promise<ExtensionsResult>;
}

export interface StepExtensionsProps {
  readonly onComplete: (selection: ExtensionsSelection) => void;
  readonly onFailed: (failure: { readonly code: string; readonly message: string }) => void;
  readonly bridge?: ExtensionsBridge;
  readonly initialResult?: ExtensionsResult;
}

const EXTENSION_STATUS_ICONS: Record<ExtensionStepStatus, LucideIcon> = {
  ready: CheckCircle2,
  warning: TriangleAlert,
  missing: XCircle,
};

export function StepExtensions({
  bridge,
  initialResult,
  onComplete,
  onFailed,
}: StepExtensionsProps) {
  const resolvedBridge = bridge ?? defaultExtensionsBridge();
  const [result, setResult] = useState<ExtensionsResult | null>(initialResult ?? null);
  const [running, setRunning] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const counts = result ? summarizeExtensions(result.substeps) : null;

  async function runVerify(): Promise<void> {
    if (!resolvedBridge) {
      const failure = {
        code: "extensions_bridge_unavailable",
        message: "The extensions verification bridge is not wired.",
      };
      setError(failure.message);
      onFailed(failure);
      return;
    }
    setRunning(true);
    setError(null);
    try {
      const next = await resolvedBridge.verify();
      setResult(next);
      if (next.substeps.some((substep) => substep.status === "missing")) {
        const failure = {
          code: "extensions_missing_required_step",
          message: next.summary,
        };
        setError(failure.message);
        onFailed(failure);
        return;
      }
      onComplete(next);
    } catch (err) {
      const failure = {
        code: "extensions_check_failed",
        message: err instanceof Error ? err.message : String(err),
      };
      setError(failure.message);
      onFailed(failure);
    } finally {
      setRunning(false);
    }
  }

  return (
    <section
      aria-labelledby="hh-wizard-extensions-title"
      className="hh-wizard-step hh-wizard-extensions-step"
      data-testid="wizard-step-extensions"
    >
      <header className="hh-wizard-step-header">
        <span className="hh-stage-kicker">STEP 10</span>
        <h2 id="hh-wizard-extensions-title">Hoopoe extensions</h2>
        <p>
          Verify the daemon service, browser-engine access, skill loader, and
          GitHub auth surfaces that Hoopoe layers on top of ACFS.
        </p>
      </header>

      <div className="hh-wizard-status-summary" data-status={counts?.status ?? "idle"}>
        <PackageCheck size={18} strokeWidth={2.1} />
        <div>
          <strong>{result?.summary ?? "Extensions not verified yet"}</strong>
          <p>
            {counts
              ? `${counts.ready} ready, ${counts.warning} warnings, ${counts.missing} missing`
              : "Checks the daemon service, browser engine, skill loader, and Git auth."}
          </p>
        </div>
      </div>

      {result ? (
        <div className="hh-wizard-status-grid" data-testid="wizard-extensions-results">
          {result.substeps.map((substep) => (
            <article className="hh-wizard-status-row" data-status={substep.status} key={substep.id}>
              {statusIcon(substep.status)}
              <div>
                <strong>{substep.label}</strong>
                <p>{substep.description}</p>
                <small>{substep.evidence}</small>
              </div>
            </article>
          ))}
        </div>
      ) : null}

      {error ? (
        <div className="hh-wizard-resume-banner hh-wizard-resume-banner-inline" role="alert">
          <TriangleAlert size={16} strokeWidth={2.1} />
          <div>
            <strong>Extensions need attention</strong>
            <p>{error}</p>
          </div>
        </div>
      ) : null}

      <div className="hh-wizard-step-actions">
        <button
          className="hh-wizard-primary"
          data-testid="wizard-extensions-run"
          disabled={running}
          onClick={() => void runVerify()}
          type="button"
        >
          {running ? (
            <Loader2 className="hh-spin" size={14} strokeWidth={2.1} />
          ) : (
            <RefreshCcw size={14} strokeWidth={2.1} />
          )}
          {running ? "Verifying..." : "Verify extensions"}
        </button>
      </div>
    </section>
  );
}

function statusIcon(status: ExtensionStepStatus) {
  const Icon = EXTENSION_STATUS_ICONS[status];
  return <Icon size={16} strokeWidth={2.1} />;
}

export function deriveExtensionsResult(input: {
  readonly capabilities: CapabilityRegistry;
  readonly healthOk: boolean;
  readonly version: string;
}): ExtensionsResult {
  const { capabilities, healthOk, version } = input;
  const jsm = toolStatus(capabilities.tools?.jsm);
  const jfp = toolStatus(capabilities.tools?.jfp);
  const skillLoaderStatus =
    jsm === "ready" ? "ready" : jfp === "ready" ? "warning" : jsm === "warning" || jfp === "warning" ? "warning" : "missing";
  const substeps: readonly ExtensionSubstep[] = [
    {
      id: "daemon_service",
      label: "Daemon service",
      description: "Hoopoe daemon answers health/version checks.",
      status: healthOk ? "ready" : "missing",
      evidence: version ? `Daemon version ${version}` : "Daemon version unavailable.",
    },
    {
      id: "oracle_browser",
      label: "Oracle browser engine",
      description: "ChatGPT Pro browser-engine route is visible to the daemon.",
      status: toolStatus(capabilities.tools?.oracle),
      evidence: evidenceForTool("oracle", capabilities.tools?.oracle),
    },
    {
      id: "skill_loader",
      label: "Skill loader",
      description: "jsm is preferred; jfp fallback is acceptable with a warning.",
      status: skillLoaderStatus,
      evidence:
        skillLoaderStatus === "ready"
          ? evidenceForTool("jsm", capabilities.tools?.jsm)
          : skillLoaderStatus === "warning"
            ? "jfp fallback or degraded skill-loader capability is available."
            : "Neither jsm nor jfp reported a usable capability.",
    },
    {
      id: "github_auth",
      label: "GitHub auth",
      description: "Git transport is available for daemon-mediated push/pull flows.",
      status: toolStatus(capabilities.tools?.git),
      evidence: evidenceForTool("git", capabilities.tools?.git),
    },
  ];
  const counts = summarizeExtensions(substeps);
  return {
    summary:
      counts.missing > 0
        ? `${counts.missing} extension checks missing`
        : counts.warning > 0
          ? "Extensions verified with warnings"
          : "Extensions verified",
    checkedAt: capabilities.snapshotAt,
    substeps,
  };
}

export function buildExtensionsCheckpointData(
  selection: ExtensionsSelection,
): ExtensionsCheckpointData {
  return {
    summary: selection.summary,
    checkedAt: selection.checkedAt,
    readySubsteps: selection.substeps.filter((step) => step.status === "ready").map((step) => step.id),
    warningSubsteps: selection.substeps.filter((step) => step.status === "warning").map((step) => step.id),
    missingSubsteps: selection.substeps.filter((step) => step.status === "missing").map((step) => step.id),
  };
}

function toolStatus(report: ToolReport | undefined): ExtensionStepStatus {
  if (!report) return "missing";
  const statuses = Object.values(report.capabilities ?? {}).map((capability) => capability.status);
  if (statuses.length === 0) return "missing";
  if (statuses.every((status) => status === "ok")) return "ready";
  if (statuses.some((status) => status === "ok" || status === "degraded")) return "warning";
  return "missing";
}

function evidenceForTool(tool: ToolId, report: ToolReport | undefined): string {
  if (!report) return `${tool} report absent from /v1/capabilities.`;
  const ok = Object.values(report.capabilities ?? {}).filter((capability) => capability.status === "ok").length;
  return `${tool} ${report.version || "version unknown"}: ${ok}/${Object.keys(report.capabilities ?? {}).length} capabilities ok.`;
}

function summarizeExtensions(substeps: readonly ExtensionSubstep[]): {
  readonly status: "ready" | "warning" | "missing";
  readonly ready: number;
  readonly warning: number;
  readonly missing: number;
} {
  const ready = substeps.filter((substep) => substep.status === "ready").length;
  const warning = substeps.filter((substep) => substep.status === "warning").length;
  const missing = substeps.filter((substep) => substep.status === "missing").length;
  return {
    status: missing > 0 ? "missing" : warning > 0 ? "warning" : "ready",
    ready,
    warning,
    missing,
  };
}

interface DaemonBridgeShape {
  readonly daemon?: {
    readonly request?: <I, O>(method: "health" | "version" | "capabilities", body: I) => Promise<O>;
  };
}

interface HealthShape {
  readonly status?: string;
}

interface VersionShape {
  readonly version?: string;
}

function defaultExtensionsBridge(): ExtensionsBridge | null {
  if (typeof window === "undefined") return null;
  const request = (window as Window & { readonly hoopoe?: DaemonBridgeShape }).hoopoe?.daemon?.request;
  if (!request) return null;
  return {
    verify: async () => {
      const [health, version, capabilities] = await Promise.all([
        request<null, HealthShape>("health", null),
        request<null, VersionShape>("version", null),
        request<null, CapabilityRegistry>("capabilities", null),
      ]);
      return deriveExtensionsResult({
        capabilities,
        healthOk: health.status !== "unhealthy",
        version: version.version ?? "",
      });
    },
  };
}
