# `@hoopoe/design-system`

Design tokens, Storybook, and reusable React components for the Hoopoe
desktop UI. The cream/dark sidebar, agent-family palette, status pills,
priority chips, and coverage-bar ramp from the v1 mockups
(`design/mockups/v1/`) live here as the canonical token + component
implementation.

## Status

The Phase 1 token layer and dependency-light component primitives are in
place. The package exports typed model helpers plus DOM renderers that keep
Storybook stories and unit tests framework-neutral; React wrappers live in
`apps/desktop` where a stage needs renderer-specific behavior.

## Components (per `plan.md §12 Phase 1`)

The reusable component set currently exported from `@hoopoe/design-system`:

- `StageHeader` — `STAGE N — VERB` chrome (Planning, Beads, Swarm,
  Hardening, Diagnostics), breadcrumb, subtitle, and typed action row.
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

Foundational shell and error surfaces that are renderer-specific
(`ActivityDrawer`, shell `EmptyStage`, and the problem+json toast/banner
hierarchy) live under `apps/desktop/src/renderer/` and consume these tokens.

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

## Usage Examples

```ts
import {
  getStageHeaderModel,
  getStatusPillModel,
  getPriorityChipModel,
  getCoverageBarModel,
} from "@hoopoe/design-system";

const header = getStageHeaderModel({
  stageId: "swarm",
  projectName: "Local demo",
  breadcrumb: ["Swarm"],
  actions: [{ id: "broadcast", label: "Broadcast", tone: "primary" }],
});

const beadState = getStatusPillModel({
  kind: "bead",
  state: "in_progress",
  size: "sm",
});

const priority = getPriorityChipModel({ priority: "p0" });
const coverage = getCoverageBarModel({ percent: 78 });

const summary = [header.kicker, beadState.label, priority.label, coverage.band];
```

```ts
import { buildTerminalPaneAuditEntry, getTerminalPaneModel } from "@hoopoe/design-system";
```

`TerminalPane` is intentionally constrained to the Diagnostics surface. It
requires an audit acknowledgement before the raw pane can be revealed, and it
must not be imported into the default Swarm UI.

## Storybook And Verification

- `src/**/*.stories.ts` contains DOM-rendered stories for tokens and every
  exported component state set.
- `bun run --cwd packages/design-system storybook` is the package-local
  story surface command; it remains dependency-light until the monorepo
  Storybook runner is wired.
- `bun run --cwd packages/design-system test` exercises model invariants,
  accessibility labels, state coverage, command-palette filtering, and
  Guardrail 12 enforcement.
- `bun run --cwd packages/design-system typecheck` and `build` run the
  package TypeScript contract.
