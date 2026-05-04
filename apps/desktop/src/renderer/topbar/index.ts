// hp-4ya — public exports for the cockpit top-bar.
//
// Existing project switcher lives alongside; the new exports cover the
// five Phase 4 pills wired in RootLayout.

export { ProjectSwitcher, ProjectRunningPill } from "./ProjectSwitcher.tsx";
export {
  formatRelativeActivation,
  isProjectSwarmRunning,
  normalizeSearchText,
  projectMatchesSearch,
  routeForStage,
  splitProjectSections,
  type ProjectSections,
} from "./project-switcher-model.ts";

export {
  BeadsPulsePill,
  CodeHealthPill,
  PowerAssertionPill,
  SubscriptionPill,
  SwarmStatePill,
  ToolHealthPill,
  powerAssertionAria,
} from "./TopbarPills.tsx";

export {
  codeHealthAria,
  dotClass,
  seedBeadsPulse,
  seedCodeHealth,
  seedSubscriptionUsage,
  seedSwarmState,
  seedToolHealth,
  subscriptionAria,
  toolHealthAria,
  useBeadsPulseQuery,
  useCodeHealthQuery,
  useSubscriptionUsageQuery,
  useSwarmStateQuery,
  useToolHealthQuery,
  type BeadsPulse,
  type CodeHealthSummary,
  type HealthDot,
  type SubscriptionProviderUsage,
  type SubscriptionUsageSummary,
  type SwarmStateSummary,
  type ToolHealthSnapshot,
} from "./topbar-data.ts";
