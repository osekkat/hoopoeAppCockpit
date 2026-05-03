// `@hoopoe/schemas` is the source of truth for Hoopoe's daemon API shapes.
//
// Layout (hp-r3i):
//   - openapi.yaml                        — authoritative OpenAPI 3.1 spec.
//   - src/generated/openapi.ts            — openapi-typescript output (committed; CI gate detects drift).
//   - src/index.ts                        — public entry: re-exports + small runtime helpers.
//   - go/                                 — separate Go module with oapi-codegen output.
//
// Consumers import named types from here, e.g.:
//   import type { components, paths, Problem } from "@hoopoe/schemas";
//
// Runtime helpers live in this file because they need to ship next to the
// types that describe them (and because consumers want them in both the
// renderer and the daemon-shim layer).

export type { components, operations, paths } from "./generated/openapi.ts";

import type { components } from "./generated/openapi.ts";

/**
 * Bare aliases for the most commonly-used component schemas. Keep this list
 * short; full namespace access is `components["schemas"]["..."]`.
 */
export type Problem = components["schemas"]["Problem"];
export type Capability = components["schemas"]["Capability"];
export type CapabilityStatus = components["schemas"]["CapabilityStatus"];
export type ToolId = components["schemas"]["ToolId"];
export type ToolReport = components["schemas"]["ToolReport"];
export type DegradedModePolicy = components["schemas"]["DegradedModePolicy"];
export type CapabilityRegistry = components["schemas"]["CapabilityRegistry"];
export type CompatibilityReport = components["schemas"]["CompatibilityReport"];
export type MigrationState = components["schemas"]["MigrationState"];
export type HealthResponse = components["schemas"]["HealthResponse"];
export type VersionResponse = components["schemas"]["VersionResponse"];
export type Actor = components["schemas"]["Actor"];
export type PageMeta = components["schemas"]["PageMeta"];
export type Cursor = components["schemas"]["Cursor"];

// VPS + Project domain (§4)
export type VpsHost = components["schemas"]["VpsHost"];
export type VpsLifecycleState = components["schemas"]["VpsLifecycleState"];
export type Project = components["schemas"]["Project"];
export type ProjectLifecycleState = components["schemas"]["ProjectLifecycleState"];
export type ProjectRepoRef = components["schemas"]["ProjectRepoRef"];
export type ProjectGate = components["schemas"]["ProjectGate"];
export type ProjectReadiness = components["schemas"]["ProjectReadiness"];

// Plans + Beads (§7.1, §7.2)
export type Plan = components["schemas"]["Plan"];
export type PlanLifecycleState = components["schemas"]["PlanLifecycleState"];
export type PlanQualityScore = components["schemas"]["PlanQualityScore"];
export type Bead = components["schemas"]["Bead"];
export type BeadStatus = components["schemas"]["BeadStatus"];
export type BeadIssueType = components["schemas"]["BeadIssueType"];
export type BeadPriority = components["schemas"]["BeadPriority"];
export type BeadSetQuality = components["schemas"]["BeadSetQuality"];

// Jobs + Artifacts (§2.7)
export type Job = components["schemas"]["Job"];
export type JobStatus = components["schemas"]["JobStatus"];
export type ArtifactRef = components["schemas"]["ArtifactRef"];

// Approvals + CommandSpec (§5.3)
export type Approval = components["schemas"]["Approval"];
export type ApprovalState = components["schemas"]["ApprovalState"];
export type ApprovalRiskClass = components["schemas"]["ApprovalRiskClass"];
export type ApprovalScope = components["schemas"]["ApprovalScope"];
export type ApprovalSource = components["schemas"]["ApprovalSource"];
export type CommandSpec = components["schemas"]["CommandSpec"];

// WS event stream (§2.6)
export type WsEventEnvelope = components["schemas"]["WsEventEnvelope"];
export type WsClientOp = components["schemas"]["WsClientOp"];
export type WsServerMessage = components["schemas"]["WsServerMessage"];
export type WsSubscribeOp = components["schemas"]["WsSubscribeOp"];
export type WsUnsubscribeOp = components["schemas"]["WsUnsubscribeOp"];
export type WsHeartbeat = components["schemas"]["WsHeartbeat"];
export type WsGap = components["schemas"]["WsGap"];
export type WsLag = components["schemas"]["WsLag"];
export type EventReplayResponse = components["schemas"]["EventReplayResponse"];

// ActionPlan (§8.3.1) — closed action set lives in tending-actions.yaml
export type ActionPlan = components["schemas"]["ActionPlan"];
export type Action = components["schemas"]["Action"];
export type ActionKind = components["schemas"]["ActionKind"];

// Swarm + Agent + FileReservation + PaneStreamEvent (§7.3, §8)
export type SwarmSession = components["schemas"]["SwarmSession"];
export type SwarmSessionState = components["schemas"]["SwarmSessionState"];
export type SwarmLaunchSpec = components["schemas"]["SwarmLaunchSpec"];
export type SwarmLaunchComposition = components["schemas"]["SwarmLaunchComposition"];
export type Agent = components["schemas"]["Agent"];
export type AgentState = components["schemas"]["AgentState"];
export type FileReservation = components["schemas"]["FileReservation"];
export type PaneStreamEvent = components["schemas"]["PaneStreamEvent"];

// Budget + build queue policies (§2.7, §8.5)
export type BudgetPolicy = components["schemas"]["BudgetPolicy"];
export type BuildQueuePolicy = components["schemas"]["BuildQueuePolicy"];

// Code health (§7.4, §11)
export type CodeHealthSnapshot = components["schemas"]["CodeHealthSnapshot"];
export type FileHealthMetric = components["schemas"]["FileHealthMetric"];
export type HealthDimension = components["schemas"]["HealthDimension"];

// Provider plugin contract (§6.2, §13) — hp-14zt
// `ProviderPluginContract` is a backward-compatible alias for the hp-r3i name;
// new consumers should prefer `ProviderPluginManifest`.
export type ProviderPluginContract = components["schemas"]["ProviderPluginContract"];
export type ProviderPluginManifest = components["schemas"]["ProviderPluginManifest"];
export type ProviderId = components["schemas"]["ProviderId"];
export type ProviderAuthMode = components["schemas"]["ProviderAuthMode"];
export type ProviderRegion = components["schemas"]["ProviderRegion"];
export type ProviderSize = components["schemas"]["ProviderSize"];
export type ProviderStorageType = components["schemas"]["ProviderStorageType"];
export type ProviderSizeTier = components["schemas"]["ProviderSizeTier"];
export type ProviderCreateInstanceOpts = components["schemas"]["ProviderCreateInstanceOpts"];
export type ProviderInstance = components["schemas"]["ProviderInstance"];
export type ProviderInstanceStatus = components["schemas"]["ProviderInstanceStatus"];
export type ProviderDestroyResult = components["schemas"]["ProviderDestroyResult"];
export type ProviderEstimateCostOpts = components["schemas"]["ProviderEstimateCostOpts"];
export type ProviderCostEstimate = components["schemas"]["ProviderCostEstimate"];
export type ProviderCostLineItem = components["schemas"]["ProviderCostLineItem"];

/** Public package identity. Used in audit + diagnostics. */
export const HOOPOE_SCHEMAS_PACKAGE_NAME = "@hoopoe/schemas";

/** Matches `info.version` in `openapi.yaml`. Bump on any breaking spec change. */
export const HOOPOE_OPENAPI_VERSION = "0.1.0";

/**
 * RFC 7807 problem+json content type. Use when checking `Content-Type` on
 * error responses.
 */
export const PROBLEM_JSON_CONTENT_TYPE = "application/problem+json";

/**
 * Runtime predicate: is this value a Problem? Cheap shape check; use before
 * narrowing without trusting the wire. Returns false for null/undefined.
 */
export function isProblem(value: unknown): value is Problem {
  if (value === null || typeof value !== "object") return false;
  const v = value as Record<string, unknown>;
  return (
    typeof v.type === "string" &&
    typeof v.title === "string" &&
    typeof v.status === "number" &&
    typeof v.code === "string"
  );
}
