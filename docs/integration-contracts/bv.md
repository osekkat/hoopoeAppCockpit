# `bv` integration contract

> Graph-aware triage engine for `br` projects. **Bare `bv` launches an interactive TUI that blocks the session — never invoke from automation (`plan.md` Appendix C #1).** Hoopoe uses `bv --robot-*` flags only.

## Source of truth

| Field    | Value                                                                |
| -------- | -------------------------------------------------------------------- |
| Tool     | `bv`                                                                 |
| Repo     | Installed alongside `br` (<https://github.com/Dicklesworthstone/beads_rust>) |
| Observed | `bv v0.16.0` (research-spike 2026-05-02)                             |
| Min compatible | 0.15+ (verify `bv --robot-help` advertises the surfaces below) |
| Reference | `AGENTS.md` "bv — Graph-Aware Triage Engine" section                |

## Adapter precedence (per `plan.md` §2.3)

1. **`bv --robot-<verb>`** — only allowed surface. JSON output, deterministic.
2. **`bv --recipe <name> --robot-<verb>`** — pre-filtered triage (`actionable`, `high-impact`, etc.).
3. **`bv --diff-since <ref> --robot-diff`** — point-in-time delta.
4. **`bv --as-of <ref> --robot-insights`** — historical snapshot.
5. **No bare `bv`.** Capability `bv.tui` is `blocked-by-policy` to make the rule visible in the registry.
6. **`.beads/issues.jsonl`** raw read is `bv`'s input (it consumes the same JSONL `br` writes); Hoopoe never edits it directly.

## Capability IDs (per `plan.md` §2.8)

| capId                       | Required by                              | Surface                                              | Status semantics                                            |
| --------------------------- | ---------------------------------------- | ---------------------------------------------------- | ----------------------------------------------------------- |
| `bv.robot.triage`           | Beads stage triage panel                 | `bv --robot-triage`                                  | `ok` when JSON parses + `bottlenecks`/`recommendations` keys present |
| `bv.robot.next`             | "Pick the next task" CTA + recommend hint | `bv --robot-next`                                   | Single top recommendation: `{id, title, score, reasons[], unblocks, claim_command, show_command}` |
| `bv.robot.plan`             | DAG view "ready frontier" highlight      | `bv --robot-plan`                                    | `ok` when `tracks` + `summary.highest_impact` present       |
| `bv.robot.insights`         | DAG view + bottleneck panel              | `bv --robot-insights`                                | `ok` when `Bottlenecks`, `CriticalPath`, `Stats.PageRank` present |
| `bv.robot.diff`             | Bead delta panel                         | `bv --robot-diff --diff-since <ref>`                 | Empty range → `{summary: {...}, ...}` with zeros. **Not advertised by `--robot-help` but works.** |
| `bv.robot.priority`         | Priority misalignment notifier           | `bv --robot-priority`                                | `recommendations: null` is normal — no misalignment. **Not advertised by `--robot-help` but works.** |
| `bv.robot.recipes`          | Recipe picker UI                         | `bv --robot-recipes`                                 | Lists built-in + user recipes. **Not advertised by `--robot-help` but works.** |
| `bv.robot.help`             | Adapter capability probe                 | `bv --robot-help`                                    | Used at probe time; advertises only the 4 "core commands" (triage/next/plan/insights). |
| `bv.export.md`              | Export-to-markdown action                | `bv --recipe <r> --export-md <file>`                 | Writes file; Hoopoe captures the path, never the bytes inline |
| `bv.tui`                    | (deliberately unused)                    | bare `bv`                                            | `blocked-by-policy` (Guardrail 1)                            |

## Command surfaces (observed)

| Label                   | argv                                              | Exit | stdout summary                                                                             |
| ----------------------- | ------------------------------------------------- | ---- | ------------------------------------------------------------------------------------------ |
| `robot_help`            | `bv --robot-help`                                 | 0    | List of `--robot-*` surfaces. Use as the capability probe.                                 |
| `robot_recipes`         | `bv --robot-recipes`                              | 0    | `["actionable", "high-impact", ...]` plus user recipes.                                    |
| `robot_triage`          | `bv --robot-triage`                               | 0    | Top-level: `bottlenecks`, `recommendations`, `summary`. ~hundreds of KB on real graphs.    |
| `robot_plan`            | `bv --robot-plan`                                 | 0    | `{plan: {tracks: [...], items: [...], summary: {highest_impact: <id>}}}`                   |
| `robot_plan_actionable` | `bv --recipe actionable --robot-plan`             | 0    | Same shape; pre-filtered to immediately actionable items.                                  |
| `robot_insights`        | `bv --robot-insights`                             | 0    | `Bottlenecks`, `CriticalPath`, `Cycles`, `Stats.{PageRank, Betweenness, HITS, ...}`        |
| `robot_priority`        | `bv --robot-priority`                             | 0    | `recommendations: null` when nothing is misaligned.                                         |
| `robot_diff`            | `bv --robot-diff --diff-since HEAD~30`            | 0    | `{summary: {new, closed, modified, cycles}}` — `summary` is empty when range has no commits|

## Failure modes & recovery

| Symptom                                                | Root cause                                       | Hoopoe response                                                                |
| ------------------------------------------------------ | ------------------------------------------------ | ------------------------------------------------------------------------------ |
| Bare `bv` invoked → TUI starts                         | Adapter regression                               | **Never allowed.** CI lint rule fails the build (`hp-r33` capability registry). |
| `--robot-diff` with bad `--diff-since`                 | Ref does not exist in repo                       | Capture stderr, surface as user-fixable.                                       |
| Empty `recommendations` in `--robot-priority`          | Healthy graph                                    | Treat as `ok`, suppress notification noise.                                     |
| `Cycles` non-empty                                     | Real bug in deps                                 | Surface as **blocker** in Beads stage; cannot proceed to launch swarm.         |
| Output truncated (over `--max-bytes`)                  | Massive bead graph                               | Adjust `--max-bytes` cap; consider `--export-md` to file instead.              |
| Stale snapshot (after `br update` without `bv` re-run) | `bv` reads from JSONL on each invocation         | No-op; each invocation is fresh. No caching needed at the `bv` adapter.        |

## Authentication / credentials

- None. Local file access only (`.beads/issues.jsonl`).

## Known gotchas

See [`../research-spike/gotchas.md`](../research-spike/gotchas.md) (`hp-d54`). Highlights:

- **NEVER** call bare `bv`. The TUI grabs the terminal and blocks the agent. `--robot-help` is the safe probe.
- `--diff-since` accepts any git ref (tag, SHA, `HEAD~N`). Bad refs print to stderr but exit 0; check stderr after every diff invocation.
- `--robot-insights` Stats keys are PascalCase (`PageRank`, `Betweenness`); other surfaces use snake_case (`bottlenecks`, `recommendations`). Adapter consumers must branch on shape, not assume.
- `--export-md` writes a file; the path is the contract, not the bytes. Capture the file path in the snapshot envelope, not the contents.

## Test fixtures

| Scenario | Fixture path                                                              | Asserts                                                                |
| -------- | ------------------------------------------------------------------------- | ---------------------------------------------------------------------- |
| `fresh`  | `packages/fixtures/phase0-2026-05-02/scenarios/fresh/snapshot.json`       | All `--robot-*` succeed; `Cycles: []`, `recommendations: null`         |
| `active` | `packages/fixtures/phase0-.../scenarios/active/snapshot.json`             | Non-empty `tracks`, populated `bottlenecks`, populated `summary`       |
| `failure`| `packages/fixtures/phase0-.../scenarios/failure/snapshot.json`            | `Cycles` non-empty (deliberate); `--robot-diff` against bad ref captured |

The `bv.tui` capability MUST report `blocked-by-policy` in every fixture — adapter contract tests fail the build if any fixture shows `bv.tui` as anything else.

## Adapter notes (Hoopoe Go side)

- Lives at `apps/daemon/internal/adapters/bv/` (Phase 6, bead `hp-pjp`).
- Each `--robot-*` invocation is a separate adapter method; they share a common envelope wrapper.
- Linter rule (Phase 1, `hp-rflj`-adjacent): grep for `\bbv\b` outside `--robot-*` patterns in `apps/daemon/**/*.go` fails the build.
- Cache lifetime: `bv` is fast and stateless; no caching needed at the daemon. UI-side `TanStack Query` handles staleness.
- The Beads stage subscribes to `bead_set_changed` events; on event, re-runs `--robot-plan` + `--robot-insights` (parallel) and pushes a new snapshot to the renderer.
