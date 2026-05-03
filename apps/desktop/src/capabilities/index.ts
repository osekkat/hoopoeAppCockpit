// Public entry point for the renderer-side capability gate. UI surfaces
// import named exports from this file; nothing should reach into the
// internal modules directly.

export type {
  ActivityBehavior,
  Capability,
  CapabilityRegistry,
  CapabilityStatus,
  CompatibilityReport,
  DegradedModeContract,
  FeatureCapabilityRequirement,
  FeatureDecision,
  FeatureRender,
  IfMissingOptional,
  IfMissingRequired,
  ToolId,
  ToolReport,
} from "./types.ts";

export {
  decideFeature,
  determineFeature,
  emptyRegistry,
  FEATURE_CATALOG,
  isToolId,
  lookupCapability,
  lookupCapabilityStatus,
  renderBucketFor,
  toolFromCapRef,
} from "./registry.ts";
export type { FeatureId } from "./registry.ts";
