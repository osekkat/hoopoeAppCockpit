// hp-hle — Capability gate for stage routes.
//
// Each stage declares `requiredFeatureIds` against the renderer
// FEATURE_CATALOG (apps/desktop/src/capabilities/registry.ts). At render
// time, this component resolves the worst-of decision across those
// features and either renders the stage children (available / degraded)
// or replaces them with a state surface (unavailable / blocked-by-policy).
//
// The data fetch lives in `useCapabilityRegistryQuery`; this component
// stays pure so the four states can be tested without TanStack Query
// (per the bun:test no-DOM constraint).

import { AlertTriangle, ShieldOff } from "lucide-react";
import type { ReactNode } from "react";
import type { CapabilityRegistry, FeatureDecision } from "../../capabilities/index.ts";
import { decideStageGate, useCapabilityRegistryQuery } from "../data/capability-data.ts";
import type { StageDefinition } from "../stages.ts";
import { StateSurface } from "../state-view/index.ts";

interface StageGateProps {
  readonly stage: StageDefinition;
  readonly registry: CapabilityRegistry;
  readonly children: ReactNode;
}

/** Pure decision-renderer. Pass a registry snapshot and it resolves the
 *  stage's gate state without touching TanStack Query — tests use this
 *  directly. */
export function StageGate({ stage, registry, children }: StageGateProps) {
  const decision = decideStageGate(registry, stage);
  if (!decision || decision.render === "available") {
    return <>{children}</>;
  }
  if (decision.render === "degraded") {
    return (
      <>
        <StageDegradedBanner stage={stage} decision={decision} />
        {children}
      </>
    );
  }
  if (decision.render === "blocked-by-policy") {
    return <StageBlockedSurface stage={stage} decision={decision} />;
  }
  return <StageUnavailableSurface stage={stage} decision={decision} />;
}

/** Live wrapper that subscribes to the capability registry and renders
 *  the gated stage. Routes use this directly; tests prefer `StageGate`. */
export function ConnectedStageGate({
  stage,
  children,
}: {
  readonly stage: StageDefinition;
  readonly children: ReactNode;
}) {
  const { data: registry } = useCapabilityRegistryQuery();
  if (!registry) return <>{children}</>;
  return (
    <StageGate stage={stage} registry={registry}>
      {children}
    </StageGate>
  );
}

function StageDegradedBanner({
  stage,
  decision,
}: {
  readonly stage: StageDefinition;
  readonly decision: FeatureDecision;
}) {
  return (
    <div
      aria-live="polite"
      className="hh-stage-degraded-banner"
      data-stage={stage.id}
      data-testid={`stage-gate-degraded-${stage.id}`}
      role="status"
    >
      <AlertTriangle aria-hidden="true" size={14} strokeWidth={2.1} />
      <span>
        {stage.label} is running in degraded mode — {decision.degradedReasons.join(", ")}
      </span>
    </div>
  );
}

function StageBlockedSurface({
  stage,
  decision,
}: {
  readonly stage: StageDefinition;
  readonly decision: FeatureDecision;
}) {
  return (
    <StateSurface
      variant="error"
      eyebrow={`${stage.label} blocked`}
      icon={<ShieldOff size={18} strokeWidth={2.1} />}
      title={`${stage.label} is blocked by policy`}
      description="A required capability is blocked by an organization or safety policy. Review the audit log or capability registry in Diagnostics to clear the block."
      details={decision.blockedByPolicy}
      testId={`stage-gate-blocked-${stage.id}`}
      actions={[
        {
          label: "Open Diagnostics",
          href: "diag",
          variant: "primary",
          testId: `stage-gate-blocked-${stage.id}-diagnostics`,
        },
      ]}
    />
  );
}

function StageUnavailableSurface({
  stage,
  decision,
}: {
  readonly stage: StageDefinition;
  readonly decision: FeatureDecision;
}) {
  return (
    <StateSurface
      variant="error"
      eyebrow={`${stage.label} unavailable`}
      icon={<AlertTriangle size={18} strokeWidth={2.1} />}
      title={`${stage.label} cannot run yet`}
      description="A required tool is missing or untested. Run the onboarding wizard or open Diagnostics to install or verify the capability."
      details={decision.missingRequired}
      testId={`stage-gate-unavailable-${stage.id}`}
      actions={[
        {
          label: "Open Diagnostics",
          href: "diag",
          variant: "primary",
          testId: `stage-gate-unavailable-${stage.id}-diagnostics`,
        },
      ]}
    />
  );
}
