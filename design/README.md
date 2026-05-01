# Hoopoe — Design

Pre-Phase-1 visual design artifacts for the Hoopoe cockpit.

## Status

These mockups are **sketches**, not production code. They establish the visual language, the interaction shape of each stage, and the unresolved UX decisions that the plan currently leaves open. They were built outside the repo on `claude.ai/design` and copied in so the work is version-controlled alongside `plan.md`.

When Phase 1 lands and `packages/design-system/` is scaffolded (§12 Phase 1 in `plan.md`), the shared primitives in these files graduate into real components and Storybook stories. See **Migration to Phase 1** below.

## How to view

The mockups run as a single static page — no build, no install. Open it directly:

```bash
open design/mockups/v1/Hoopoe.html
```

This loads React 18 + Babel Standalone from `unpkg`, transpiles the JSX in the browser, and mounts `<App />` against the design tokens. Switch between light/dark themes via the floating "Tweaks" panel (bottom-right). Reset the first-run wizard via the same panel.

The runtime is deliberately a sketching harness, not the production stack — Phase 1 swaps it for Vite + the t3code-vendored Electron lifecycle (§12 Phase 1, Appendix B).

## File map

| File                                  | What it covers                                                                                                                                        | Maps to `plan.md`         |
| ------------------------------------- | ----------------------------------------------------------------------------------------------------------------------------------------------------- | ------------------------- |
| `mockups/v1/Hoopoe.html`              | Entry point. Loads React + Babel from unpkg and mounts `<App />`.                                                                                     | —                         |
| `mockups/v1/macos-window.jsx`         | macOS Tahoe Liquid Glass primitives — `MacWindow`, `MacSidebar`, `MacToolbar`, `MacGlass`, `MacTrafficLights`. Kept minimal; not all of it is used.   | §3 (desktop UI tech)      |
| `mockups/v1/app.jsx`                  | Main shell: `MacShell`, `Sidebar` (projects → plans tree + workspace file-tree toggle), `Toolbar` with stage tabs, `PlanOverview`, design tokens.     | §7 preamble, §7.1, §7.7   |
| `mockups/v1/firstrun.jsx`             | 11-step setup wizard: Welcome → Mode → SSH → Preflight → ACFS → Daemon → Tools → Subscriptions → Oracle → First project → Ready.                      | §6, §12 Phase 3           |
| `mockups/v1/wizard.jsx`               | New-plan flow: Choose (Draft / Import) → Brief → Models (1 primary + up to 3 competing) → Compare → Synthesize → Refine. Plus other stage views.     | §7.1, §12 Phase 5         |
| `mockups/v1/beads.jsx`                | DAG (topological layered layout, SVG arrow edges) + Kanban (5 columns) + bead detail bottom drawer + refinement-rounds modal showing the `br` prompt. | §7.2, §12 Phase 6, 7      |
| `mockups/v1/swarm.jsx`                | Launch panel (auto / manual mode), worker grid, side log panel, recent-events feed.                                                                   | §7.3, §12 Phase 8         |
| `mockups/v1/tweaks-panel.jsx`         | Floating dev-only tweaks panel (theme switch, replay first-run). Not shipped to users.                                                                | —                         |
| `mockups/v1/uploads/`                 | Reference screenshots captured during the design session.                                                                                             | —                         |

## What `v1` does NOT cover

These are deliberately out of scope for v1 mockups; they will be added as the corresponding phases approach:

- **Activity panel (§7.5)** — the cross-stage drawer with Agent Mail timeline + user↔orchestrator chat. Phase 9.
- **Persistent top bar / cockpit chrome (§7.6)** — always-visible project/branch/Git status + tool-health dots + swarm state + beads pulse + code-health pill + subscription-usage pill + Activity panel toggle. Phase 4 lights up a first cut; Phase 11 adds the code-health pill.
- **Diagnostics screen (§10.2)** — repair actions, "Show raw pane" toggle, audit log viewer, daemon/tunnel/NTM/Agent Mail health.
- **Code health Health tab full surface (§7.4.1)** — sortable file table with coverage bars, hotspot scoring, trend sparklines. The `Code Health` tab in `app.jsx` is a placeholder.
- **Hardening review-round UI (§7.4.2)** — active round, prompts, agents involved, live findings, finding ledger, convergence detector. Phase 12.
- **Tending scheduler UI (§8, §12 Phase 10)** — job table, last-run / next-run, decision payloads, pause toggles.
- **Approvals queue UI (§5.3)** — durable approval entities, scope picker, expiry, denial flow, `DCG` verdict ingestion.

## Aesthetic — the load-bearing decisions

Recorded in detail in `DECISIONS.md`. Quick summary:

- **macOS Tahoe / Liquid Glass** chrome — frosted-glass sidebar with vibrancy + saturation, traffic lights, single-pixel borders, big shadow on the window.
- **Hoopoe-russet `#C25A2E` accent** for primary actions and brand-level affordances (the hoopoe's crest).
- **System blue `#0A84FF`** for secondary affordances and selection.
- **SF Pro / SF Mono** typography.
- **Light + dark themes** both first-class.
- **Tabular-numeric stats**, small status dots throughout.

## Migration to Phase 1

When `packages/design-system/` is scaffolded (Phase 1, see `plan.md` §12 and Appendix A):

- Shared primitives (`HoopoeCrest`, `MacGlass`, `MacWindow`, `MacTrafficLights`, status pills, `Light`, model-author dots, `StatusIcon`, `ChatChip`, `Card`, `Stat`, `ActionRow`, `Lifecycle`, `pillBtn` helpers, design tokens) graduate into `packages/design-system/components/` and `packages/design-system/tokens/`.
- Stage-specific mockups become Storybook stories under `packages/design-system/stories/<stage>/v1/`.
- The reusable component set named in §12 Phase 1 (`StageHeader`, `AgentTile`, `BeadCard`, `StatusPill`, `PriorityChip`, `CoverageBar`, `TerminalPane`, `TimelineRow`, `HealthKpiCard`, `ApprovalDialog`, `CommandPalette`) is implemented from these sketches.
- The runtime swaps from React-UMD-via-Babel-standalone to the production stack (Vite + TypeScript + the t3code-vendored Electron lifecycle, see §3 and Appendix B).

`design/mockups/v1/` is preserved as the historical sketching artifact; it's not deleted when the work moves to `packages/`.

## Decisions vs. open questions

`DECISIONS.md` records:
- decisions the design has made that the plan should adopt;
- contradictions between the design and the plan that need a resolution before Phase 8 / Phase 11 ship them.

Read it before changing the mockups or the plan in conflicting ways.
