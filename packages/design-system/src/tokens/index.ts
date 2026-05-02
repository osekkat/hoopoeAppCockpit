// Hoopoe design tokens — placeholder shapes only for hp-xru.
//
// The real token set (cream/dark sidebar, agent-family palette, status
// pills, priority chips, coverage-bar ramp) lands in hp-k6r per
// `plan.md §16` Phase 1 week 1 and is consumed by Tailwind config in
// `@hoopoe/desktop`. The shapes below are scaffolding so other packages
// can compile against typed token references before the real values land.

export type AgentFamily = "claude" | "codex" | "gemini" | "oracle";

export type StatusPillVariant =
  | "open"
  | "ready"
  | "in_progress"
  | "blocked"
  | "review"
  | "closed";

export type PriorityChipVariant = "p0" | "p1" | "p2" | "p3" | "p4";

export interface CoverageRampStop {
  readonly threshold: number;
  readonly label: "low" | "medium" | "high";
}

export const COVERAGE_RAMP: ReadonlyArray<CoverageRampStop> = [
  { threshold: 0, label: "low" },
  { threshold: 60, label: "medium" },
  { threshold: 85, label: "high" },
];
