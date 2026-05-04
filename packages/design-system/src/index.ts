// `@hoopoe/design-system` — design tokens, Storybook, and reusable
// components (`StageHeader`, `BeadCard`, `AgentTile`, `StatusPill`,
// `PriorityChip`, `CoverageBar`, `TerminalPane`, `TimelineRow`,
// `HealthKpiCard`, `ApprovalDialog`, `CommandPalette`).
//
// The package exports dependency-light TypeScript models plus DOM renderers
// for Storybook and tests. React wrappers live in the desktop app where a
// stage needs framework-specific behavior.

export * from "./tokens/index.ts";
export * from "./components/index.ts";

export const HOOPOE_DESIGN_SYSTEM_PACKAGE_NAME = "@hoopoe/design-system";
