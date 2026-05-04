// Hoopoe-owned. Renderer-side capability gate. The desktop never assumes a
// feature exists because a tool is installed (plan.md §2.8). Every UI button
// declares a `FeatureCapabilityRequirement` and the registry resolves it
// against the daemon's `/v1/capabilities` snapshot.
//
// This file is the renderer mirror of `apps/daemon/internal/capabilities`.
// The daemon is the source of truth — but the renderer needs synchronous
// gating for pre-render UI, so a copy of the resolution logic lives here.
// Keep the two implementations behaviorally identical; the Go tests in
// `apps/daemon/internal/capabilities/registry_test.go` are the contract.

import type {
  Capability,
  CapabilityRegistry,
  CapabilityStatus,
  FeatureCapabilityRequirement,
  FeatureDecision,
  FeatureRender,
  ToolId,
} from "./types.ts";

const KNOWN_CLOSED_TOOLS: readonly ToolId[] = [
  "ntm",
  "br",
  "bv",
  "agent_mail",
  "git",
  "ru",
  "caam",
  "caut",
  "dcg",
  "casr",
  "pt",
  "srp",
  "sbh",
  "ubs",
  "jsm",
  "jfp",
  "oracle",
  "rch",
  "rano",
  "health_ts",
  "health_py",
  "health_rs",
  "health_go",
  "health_generic",
];

const KNOWN_TOOLS_SET: ReadonlySet<string> = new Set(KNOWN_CLOSED_TOOLS);

export function isToolId(value: string): value is ToolId {
  return KNOWN_TOOLS_SET.has(value);
}

/** Parse the tool prefix from a fully-qualified capability reference like
 *  `git.status.read`. Returns `null` if the prefix isn't a known ToolId. The
 *  capability ID inside the ToolReport.capabilities map IS the full ref —
 *  this function does not strip the prefix. */
export function toolFromCapRef(ref: string): ToolId | null {
  const dot = ref.indexOf(".");
  if (dot <= 0 || dot === ref.length - 1) return null;
  const prefix = ref.slice(0, dot);
  return isToolId(prefix) ? prefix : null;
}

/** Look up a single capability by its fully-qualified reference. Returns
 *  `null` for unknown tool / unknown capId. */
export function lookupCapability(
  registry: CapabilityRegistry,
  capRef: string,
): Capability | null {
  const tool = toolFromCapRef(capRef);
  if (!tool) return null;
  const report = registry.tools[tool];
  if (!report) return null;
  return report.capabilities[capRef] ?? null;
}

export function lookupCapabilityStatus(
  registry: CapabilityRegistry,
  capRef: string,
): CapabilityStatus {
  const cap = lookupCapability(registry, capRef);
  return cap?.status ?? "missing";
}

/** Resolve a feature requirement against a registry snapshot. Mirrors
 *  `apps/daemon/internal/capabilities.Determine` exactly:
 *
 *   1. Any required cap blocked-by-policy → render = `blocked-by-policy`.
 *   2. Any required cap missing/untested → render = `unavailable`.
 *   3. Any required or optional cap degraded → render = `degraded`.
 *   4. Otherwise render = `available`.
 *
 *  Required-blocked outranks required-missing; required-missing outranks
 *  degraded; degraded outranks available.
 */
export function determineFeature(
  registry: CapabilityRegistry,
  requirement: FeatureCapabilityRequirement,
): FeatureDecision {
  let render: FeatureRender = "available";
  const missingRequired: string[] = [];
  const missingOptional: string[] = [];
  const blockedByPolicy: string[] = [];
  const degradedReasons: string[] = [];

  for (const ref of requirement.capabilitiesRequired) {
    const status = lookupCapabilityStatus(registry, ref);
    if (status === "blocked-by-policy") {
      blockedByPolicy.push(ref);
      render = "blocked-by-policy";
      continue;
    }
    if (status === "missing" || status === "untested") {
      missingRequired.push(ref);
      if (render !== "blocked-by-policy") render = "unavailable";
      continue;
    }
    if (status === "degraded") {
      degradedReasons.push(ref);
      if (render === "available") render = "degraded";
    }
  }

  for (const ref of requirement.capabilitiesOptional) {
    const status = lookupCapabilityStatus(registry, ref);
    if (status === "missing" || status === "untested") {
      missingOptional.push(ref);
      continue;
    }
    if (status === "degraded") {
      degradedReasons.push(ref);
      if (render === "available") render = "degraded";
    }
  }

  return {
    featureId: requirement.featureId,
    render,
    missingRequired: [...missingRequired].sort(),
    missingOptional: [...missingOptional].sort(),
    blockedByPolicy: [...blockedByPolicy].sort(),
    degradedReasons: [...degradedReasons].sort(),
    contractAction: requirement.degradedMode.ifMissingRequired,
    optionalAction: requirement.degradedMode.ifMissingOptional,
    activityBehavior: requirement.degradedMode.activityBehavior,
  };
}

/** Maps the storage `CapabilityStatus` to the renderer's user-visible four
 *  buckets. `untested` maps to `unavailable` (Diagnostics distinguishes the
 *  underlying status). */
export function renderBucketFor(status: CapabilityStatus): FeatureRender {
  switch (status) {
    case "ok":
      return "available";
    case "degraded":
      return "degraded";
    case "blocked-by-policy":
      return "blocked-by-policy";
    case "missing":
    case "untested":
      return "unavailable";
  }
}

/** Empty registry (used at app boot before /v1/capabilities is fetched). */
export function emptyRegistry(): CapabilityRegistry {
  return {
    schemaVersion: 1,
    snapshotAt: new Date(0).toISOString(),
    daemonApiVersion: "",
    fixturesVersion: "",
    tools: {},
  };
}

/** A renderer-side feature catalog. UI surfaces import this so they have a
 *  single declarative source of "what this button needs to work."
 *
 *  Adding a new gated feature: append an entry below. UI code calls
 *  `determineFeature(registry, FEATURE_CATALOG[featureId])` and renders
 *  according to the returned decision.
 *
 *  This catalog is intentionally exhaustive at definition time so a CI
 *  test (apps/desktop/src/capabilities/registry.test.ts) can spot-check
 *  that no UI feature flag drifts away from declared capabilities. */
export const FEATURE_CATALOG: Readonly<Record<string, FeatureCapabilityRequirement>> = {
  "swarm.bead.push-branch": {
    featureId: "swarm.bead.push-branch",
    capabilitiesRequired: ["git.status.read", "git.push"],
    capabilitiesOptional: [],
    degradedMode: {
      ifMissingRequired: "block_job",
      ifMissingOptional: "continue_with_warning",
      activityBehavior: "activity_panel_warning",
    },
  },
  "bead.kanban.refresh": {
    featureId: "bead.kanban.refresh",
    capabilitiesRequired: ["br.issues.read"],
    capabilitiesOptional: ["bv.robot.triage"],
    degradedMode: {
      ifMissingRequired: "run_read_only",
      ifMissingOptional: "continue_with_warning",
      activityBehavior: "activity_panel_warning",
    },
  },
  "swarm.dashboard.live": {
    featureId: "swarm.dashboard.live",
    capabilitiesRequired: ["ntm.robot.snapshot"],
    capabilitiesOptional: ["ntm.panes.stream"],
    degradedMode: {
      ifMissingRequired: "emit_diagnostic",
      ifMissingOptional: "continue_with_warning",
      activityBehavior: "diagnostics_only",
    },
  },
  "approvals.dcg.subscribe": {
    featureId: "approvals.dcg.subscribe",
    capabilitiesRequired: ["dcg.verdicts.subscribe"],
    capabilitiesOptional: [],
    degradedMode: {
      ifMissingRequired: "emit_diagnostic",
      ifMissingOptional: "continue_with_warning",
      activityBehavior: "diagnostics_only",
    },
  },
  "tending.watch-safety-thresholds": {
    featureId: "tending.watch-safety-thresholds",
    capabilitiesRequired: ["ntm.sessions.list"],
    capabilitiesOptional: ["ntm.swarm.halt"],
    degradedMode: {
      ifMissingRequired: "emit_diagnostic",
      ifMissingOptional: "continue_with_warning",
      activityBehavior: "diagnostics_only",
    },
  },
  "activity.mail.send": {
    featureId: "activity.mail.send",
    capabilitiesRequired: ["agent_mail.messages.send"],
    capabilitiesOptional: ["agent_mail.reservations.list"],
    degradedMode: {
      ifMissingRequired: "block_job",
      ifMissingOptional: "continue_with_warning",
      activityBehavior: "activity_panel_warning",
    },
  },
};

export type FeatureId = keyof typeof FEATURE_CATALOG;

/** Resolve a feature by id against the current registry. Throws if the
 *  featureId is not in the catalog — UI authors must declare features
 *  centrally rather than passing inline strings. */
export function decideFeature(
  registry: CapabilityRegistry,
  featureId: FeatureId,
): FeatureDecision {
  const requirement = FEATURE_CATALOG[featureId];
  if (!requirement) {
    throw new Error(`capabilities: unknown featureId ${featureId}`);
  }
  return determineFeature(registry, requirement);
}
