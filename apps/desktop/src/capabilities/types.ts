// Hoopoe-owned. Renderer-side mirror of the daemon's CapabilityRegistry
// shape. Placeholder until packages/schemas (hp-r3i) emits generated types
// from openapi.yaml. When that happens, replace these declarations with a
// re-export from @hoopoe/schemas — the field names and string unions here
// are intentionally byte-identical with the agreed shape so the swap is
// mechanical.
//
// Cross-references:
//   - plan.md §2.6, §2.8 — daemon API + capability registry shape
//   - apps/daemon/internal/capabilities/types.go — Go side
//   - hoopoe-phase2 thread (msg id 145) — agreed proposal

/** Closed enumeration of canonical-state tools per plan.md §2.8. */
export type ToolId =
  | "ntm"
  | "br"
  | "bv"
  | "agent_mail"
  | "git"
  | "ru"
  | "caam"
  | "caut"
  | "dcg"
  | "casr"
  | "pt"
  | "srp"
  | "sbh"
  | "ubs"
  | "jsm"
  | "jfp"
  | "oracle"
  | "health_ts"
  | "health_py"
  | "health_rs"
  | "health_go"
  | "health_generic";

/** Five storage states. The renderer maps `untested → unavailable` for the
 *  user-visible bucket; Diagnostics keeps the distinction. */
export type CapabilityStatus =
  | "ok"
  | "degraded"
  | "missing"
  | "blocked-by-policy"
  | "untested";

/** One capId result inside a ToolReport. */
export interface Capability {
  readonly status: CapabilityStatus;
  readonly fallback?: string;
  readonly transport?: string;
  readonly notes?: string;
}

/** One tool's slice of the registry (per plan.md §2.8). */
export interface ToolReport {
  readonly tool: ToolId;
  readonly version: string;
  readonly source: string;
  readonly capabilities: Readonly<Record<string, Capability>>;
  readonly lastCheckedAt: string;
  readonly fixturesVersion: string;
}

/** GET /v1/capabilities response. */
export interface CapabilityRegistry {
  readonly schemaVersion: 1;
  readonly snapshotAt: string;
  readonly daemonApiVersion: string;
  readonly fixturesVersion: string;
  readonly tools: Readonly<Partial<Record<ToolId, ToolReport>>>;
}

export type IfMissingRequired = "block_job" | "run_read_only" | "emit_diagnostic";
export type IfMissingOptional = "continue_with_warning" | "suppress_related_detections";
export type ActivityBehavior = "silent" | "diagnostics_only" | "activity_panel_warning";

export interface DegradedModeContract {
  readonly ifMissingRequired: IfMissingRequired;
  readonly ifMissingOptional: IfMissingOptional;
  readonly activityBehavior: ActivityBehavior;
}

/** What every gated UI feature declares — the renderer never hard-codes a
 *  version check. */
export interface FeatureCapabilityRequirement {
  readonly featureId: string;
  readonly capabilitiesRequired: readonly string[];
  readonly capabilitiesOptional: readonly string[];
  readonly degradedMode: DegradedModeContract;
}

/** Renderer-visible bucket. UI components key off this — `untested` resolves
 *  to `unavailable` for non-Diagnostics surfaces. */
export type FeatureRender = "available" | "degraded" | "unavailable" | "blocked-by-policy";

/** Resolved decision for a feature against the current registry snapshot. */
export interface FeatureDecision {
  readonly featureId: string;
  readonly render: FeatureRender;
  readonly missingRequired: readonly string[];
  readonly missingOptional: readonly string[];
  readonly blockedByPolicy: readonly string[];
  readonly degradedReasons: readonly string[];
  readonly contractAction: IfMissingRequired;
  readonly optionalAction: IfMissingOptional;
  readonly activityBehavior: ActivityBehavior;
}

/** GET /v1/compatibility — embeds the registry. */
export interface CompatibilityReport {
  readonly schemaVersion: 1;
  readonly daemonApiVersion: string;
  readonly minDesktopVersion: string;
  readonly eventSchemaVersions: Readonly<Record<string, number>>;
  readonly migrationState: {
    readonly schemaVersion: number;
    readonly appliedAt: string;
    readonly pending: readonly string[];
    readonly phase?: "idle" | "running" | "failed" | "rolled_back";
  };
  readonly capabilities: CapabilityRegistry;
  readonly unsupportedClientWarnings?: readonly string[];
}
