# Health adapter — TypeScript / JavaScript

> Code-health metrics for TS/JS projects (`plan.md` §7.4, §11). Coverage, complexity, churn, hotspots.

## Source of truth

| Tool                | Purpose                          | Notes                                                                 |
| ------------------- | -------------------------------- | --------------------------------------------------------------------- |
| `vitest --coverage` | Coverage (preferred for Vite)    | JSON via `--coverage.reporter=json-summary`; per-file via `json`.     |
| `c8`                | Coverage (fallback for non-Vite) | `c8 --reporter=json-summary -- bun test`                              |
| `eslint --format=json` | Complexity + lint               | `complexity` rule + `max-lines-per-function`.                         |
| `complexity-report` | Cyclomatic complexity (legacy)   | Optional; ESLint complexity is usually enough.                        |
| `lizard`            | Generic complexity (cross-lang)  | Same binary used in `health/generic.md`.                              |
| `git log --numstat` | Churn (lines changed per file)   | Used by hotspot scoring.                                              |
| `tokei` / `scc`     | Lines of code per file/dir       | Optional; informational.                                               |
| `bun run test`      | Test execution                   | Hoopoe wraps with `rch exec`; emits JUnit/JSON.                       |

## Capability IDs (per `plan.md` §2.8)

| capId                       | Surface                                              | Notes                                                              |
| --------------------------- | ---------------------------------------------------- | ------------------------------------------------------------------ |
| `health.ts.coverage`        | `vitest --coverage --coverage.reporter=json-summary` | Returns `{total: {lines: {pct, ...}, ...}, perFile: {...}}`        |
| `health.ts.complexity`      | `eslint --format=json --rule '{"complexity": ["warn", 10]}'` | One issue per over-complex function                          |
| `health.ts.churn`           | `git log --since=<date> --numstat -- '*.ts' '*.tsx'` | Aggregated per file                                                |
| `health.ts.lint`            | `eslint --format=json`                              | Distinct from complexity; combined with finding ledger             |

## Command surfaces (planned — pin per project)

| Label                  | argv                                                                   | Exit | Notes                                                          |
| ---------------------- | ---------------------------------------------------------------------- | ---- | -------------------------------------------------------------- |
| `vitest_coverage`      | `bun run vitest --coverage --coverage.reporter=json-summary --reporter=json` | 0/1 | Per-file coverage in `coverage/coverage-summary.json`.        |
| `c8_coverage`          | `bunx c8 --reporter=json-summary -- bun test`                          | 0/1  | Fallback for non-Vite repos.                                   |
| `eslint_complexity`    | `bunx eslint --format=json --rule '{"complexity": ["warn", 10]}'`      | 0/1  | Top per-function complexity.                                    |
| `tokei`                | `tokei --output json`                                                  | 0    | Optional informational LOC metrics.                            |
| `git_churn`            | `git log --since='30 days ago' --pretty=format: --numstat -- '*.ts' '*.tsx'` | 0 | Adapter sums by file.                                       |

## Hotspot scoring (per `plan.md` §11)

`hotspot_score = normalize(churn_30d) × normalize(complexity) × (1 − coverage)`

Top-N hotspots feed the Health tab (`hp-gmm`) and can be turned into beads via "Create bead from hotspot" action.

## Failure modes & recovery

| Symptom                                | Hoopoe response                                                          |
| -------------------------------------- | ------------------------------------------------------------------------ |
| `vitest` not configured                | Fall back to `c8` + `bun test`; if still fails, mark `coverage` `degraded`. |
| ESLint config missing                  | Use baked-in default config (`eslint:recommended` + `complexity: 10`).   |
| Out-of-memory on large monorepo        | Per-package scan (parallel via `rch`); never whole-repo `eslint .`.       |
| Coverage report files not produced     | Treat as failure; surface in Activity panel.                              |

## Worktree isolation (Guardrail 5)

Health jobs run in `~/.hoopoe/work/<project-id>/health/<run-id>/` via `git worktree add` — **never** in the active agent working tree.

## Test fixtures

| Scenario | Fixture path                                                                              | Asserts                                          |
| -------- | ----------------------------------------------------------------------------------------- | ------------------------------------------------ |
| `fresh`  | `packages/fixtures/phase0-2026-05-02/scenarios/fresh/health/ts/`                          | Empty / not-yet-tested project.                  |
| `active` | `packages/fixtures/phase0-.../scenarios/active/health/ts/coverage-summary.json`           | Realistic 60-80 % coverage, 3-5 hotspots.        |
| `failure`| `packages/fixtures/phase0-.../scenarios/failure/health/ts/`                               | Coverage report missing; ESLint OOM captured.    |

## Adapter notes (Hoopoe Go side)

- Lives at `apps/daemon/internal/adapters/health/ts/` (Phase 11, bead `hp-9uh`).
- Output normalized to the snapshot envelope's `health` capture.
- Hotspot scoring lives in `apps/daemon/internal/health/scoring.go` — language-agnostic.
- Persistence: snapshots stored in `apps/daemon/internal/health/store/` per project + per run-id (`hp-3at`).
