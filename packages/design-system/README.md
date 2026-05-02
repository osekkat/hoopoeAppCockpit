# `@hoopoe/design-system`

Design tokens, Storybook, and reusable React components for the Hoopoe
desktop UI. The cream/dark sidebar, agent-family palette, status pills,
priority chips, and coverage-bar ramp from the v1 mockups
(`design/mockups/v1/`) live here as the canonical token + component
implementation.

## Status

Pre-Phase-1 scaffold (hp-xru). Real tokens + Storybook + component set
land in **hp-k6r** per `plan.md §16` Phase 1 week 1.

## Components (per `plan.md §12 Phase 1`)

The reusable component set targeted for Phase 1:

- `StageHeader` — `STAGE N — VERB` chrome (Planning, Beads, Swarm,
  Hardening).
- `AgentTile` — agent grid card; harness, CAAM account, current bead,
  status, time-on-bead, recent decisions. Critically: **no terminal
  output** in the default UI (`Guardrail 12`).
- `BeadCard` — Kanban bead card; status pill, priority chip, owner,
  files-touched, traceability link.
- `StatusPill`, `PriorityChip`, `CoverageBar` — small atoms.
- `TerminalPane` — used **only** by Diagnostics "Show raw pane" debug
  toggle (`Guardrail 12`); wrapped in xterm.js.
- `TimelineRow` — Activity-panel cross-stage timeline atom.
- `HealthKpiCard`, `ApprovalDialog`, `CommandPalette` — bigger composites.

## Token sources

- `src/tokens/index.ts` — typed token shapes (agent family, status pill
  variants, priority chip variants, coverage ramp).
- The visual palette derives from `design/mockups/v1/` and is locked in
  `design/DECISIONS.md` when a decision conflicts with `plan.md`.
