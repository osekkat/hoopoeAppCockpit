// `@hoopoe/design-system` — design tokens, Storybook, and reusable
// components (`StageHeader`, `BeadCard`, `AgentTile`, `StatusPill`,
// `PriorityChip`, `CoverageBar`, `TerminalPane`, `TimelineRow`,
// `HealthKpiCard`, `ApprovalDialog`, `CommandPalette`).
//
// Phase 1 / hp-k6r builds out the real token set and components. For
// hp-xru this index is a placeholder that re-exports the typed token
// shapes from `./tokens` so consumers can import paths exist now.

export * from "./tokens/index.ts";

export const HOOPOE_DESIGN_SYSTEM_PACKAGE_NAME = "@hoopoe/design-system";
