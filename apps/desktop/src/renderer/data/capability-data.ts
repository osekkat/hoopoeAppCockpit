// hp-hle — Renderer-side capability gate for stage routes.
//
// The desktop never assumes a feature exists because a tool is installed
// (plan.md §2.8). Stage routes declare `requiredFeatureIds` against the
// renderer FEATURE_CATALOG, and this module resolves the worst-of
// decision so the route can render available / degraded / unavailable /
// blocked-by-policy.
//
// Daemon RPC: `daemon.request("capabilities", null)` returns the full
// CapabilityRegistry. The query falls back to `emptyRegistry()` when no
// bridge is wired (pre-pairing, jsdom, Mock Flywheel) — the renderer
// then treats every required cap as missing and surfaces an unavailable
// state instead of silently rendering through.

import { useQuery } from "@tanstack/react-query";
import {
  decideFeature,
  emptyRegistry,
  type CapabilityRegistry,
  type FeatureDecision,
  type FeatureId,
  type FeatureRender,
} from "../../capabilities/index.ts";
import type { StageDefinition } from "../stages.ts";

export const CAPABILITY_REGISTRY_STALE_MS = 30_000;

interface RendererDaemonBridge {
  readonly daemon?: {
    readonly request?: (method: string, body: unknown) => Promise<unknown>;
  };
}

function resolveDaemonRequest(): ((method: string, body: unknown) => Promise<unknown>) | null {
  if (typeof window === "undefined") return null;
  const hoopoe = (window as Window & { readonly hoopoe?: RendererDaemonBridge }).hoopoe;
  const request = hoopoe?.daemon?.request;
  return typeof request === "function" ? request : null;
}

/** Fetch the capability registry from the daemon. Returns `null` when no
 *  bridge is wired so the caller falls back to `emptyRegistry()`. */
export async function fetchCapabilityRegistry(): Promise<CapabilityRegistry | null> {
  const request = resolveDaemonRequest();
  if (!request) return null;
  const result = (await request("capabilities", null)) as CapabilityRegistry | null;
  if (!result) return null;
  if (result.schemaVersion !== 1) {
    throw new Error(
      `capabilities: registry schemaVersion=${result.schemaVersion} != expected 1`,
    );
  }
  return result;
}

export function useCapabilityRegistryQuery() {
  return useQuery<CapabilityRegistry>({
    queryKey: ["capability-registry"],
    queryFn: async () => {
      const remote = await fetchCapabilityRegistry();
      return remote ?? emptyRegistry();
    },
    placeholderData: () => emptyRegistry(),
    staleTime: CAPABILITY_REGISTRY_STALE_MS,
  });
}

const RENDER_RANK: Record<FeatureRender, number> = {
  available: 0,
  degraded: 1,
  unavailable: 2,
  "blocked-by-policy": 3,
};

/** Pick the worst (highest-rank) decision in a list. Returns `null` when
 *  the list is empty — callers should treat that as "available". */
export function worstFeatureDecision(
  decisions: readonly FeatureDecision[],
): FeatureDecision | null {
  let worst: FeatureDecision | null = null;
  for (const decision of decisions) {
    if (worst === null) {
      worst = decision;
      continue;
    }
    if (RENDER_RANK[decision.render] > RENDER_RANK[worst.render]) {
      worst = decision;
    }
  }
  return worst;
}

/** Resolve every featureId declared by a stage against the current
 *  registry snapshot. */
export function decideStageFeatures(
  registry: CapabilityRegistry,
  stage: StageDefinition,
): readonly FeatureDecision[] {
  return stage.requiredFeatureIds.map((featureId: FeatureId) =>
    decideFeature(registry, featureId),
  );
}

/** Resolve a stage's gate decision: the worst-of across required
 *  features, or `null` when the stage declares no requirements. */
export function decideStageGate(
  registry: CapabilityRegistry,
  stage: StageDefinition,
): FeatureDecision | null {
  return worstFeatureDecision(decideStageFeatures(registry, stage));
}
