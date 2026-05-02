# `@hoopoe/design-system`

Design tokens, Storybook, and reusable React components for the Hoopoe
desktop UI. The cream/dark sidebar, agent-family palette, status pills,
priority chips, and coverage-bar ramp from the v1 mockups
(`design/mockups/v1/`) live here as the canonical token + component
implementation.

## Status

`hp-k6r` installs the Phase 1 token layer. Component primitives land in
follow-on beads, starting with `hp-i62`.

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

## Token Sources

- `src/tokens/index.ts` is the source of truth for colors, typography,
  spacing, radii, shadows, status tones, priority chips, agent-family
  tones, tool-health dots, and the coverage ramp.
- `src/tokens/theme.css` exposes the same values as CSS variables, with
  dark mode as the default `:root` theme and a deferred light-theme hook
  under `:root[data-theme="light"]`.
- `src/tokens/tokens.stories.ts` renders the token showcase for the
  Storybook surface without requiring component-library dependencies yet.
- `apps/desktop/tailwind.config.ts` consumes `tailwindTokenTheme` from
  this package so app styling derives from the same token source.

## Component Primitives

- `src/components/status-pill.ts` provides the `StatusPill` primitive
  model and DOM renderer for all bead, job, approval, capability, and
  tool-health states. It is dependency-free until the React wrapper layer
  lands in the broader component-set bead.
