# Health adapter — generic (cross-language)

> Catch-all for languages without a dedicated adapter (Ruby, Elixir, Java, C/C++, Swift, shell, multi-language repos). Scopes: complexity (`lizard`), LOC (`scc` / `tokei`), churn (`git log --numstat`). No coverage by default — coverage is language-specific and lands in the per-language adapters.

## Source of truth

| Tool                    | Purpose                          | Notes                                                                 |
| ----------------------- | -------------------------------- | --------------------------------------------------------------------- |
| `lizard`                | Cyclomatic complexity (cross-lang) | Supports C/C++, Java, JS, Python, Ruby, PHP, Swift, Scala, etc.       |
| `scc`                   | LOC + complexity hint            | Fast; produces JSON.                                                  |
| `tokei`                 | LOC                              | Alternative to scc; JSON output.                                      |
| `git log --numstat`     | Churn                            | Language-agnostic.                                                    |
| `ubs <files>`           | Bug findings                     | See [`../ubs.md`](../ubs.md); covers many languages.                  |
| `cloc`                  | LOC (legacy fallback)            | Slower than scc/tokei.                                                 |

## Capability IDs (per `plan.md` §2.8)

| capId                          | Surface                                              | Notes                                                              |
| ------------------------------ | ---------------------------------------------------- | ------------------------------------------------------------------ |
| `health.generic.complexity`    | `lizard --xml` (parsed) or `lizard --json`           | Per-function complexity for any supported language                 |
| `health.generic.loc`           | `scc --format json --no-cocomo` / `tokei --output json` | LOC per language                                              |
| `health.generic.churn`         | `git log --numstat`                                  | Aggregated per file                                                |
| `health.generic.coverage`      | (intentionally `missing`)                            | Use language-specific adapter for coverage                         |

## Command surfaces (observed)

| Label    | argv                                          | Exit | Notes                                                          |
| -------- | --------------------------------------------- | ---- | -------------------------------------------------------------- |
| `lizard` | `lizard --xml`                                | 0    | XML output; adapter parses. JSON via `--csv` patches.          |
| `scc`    | `scc --format json --no-cocomo`               | 0    | Returns array of per-language records.                          |
| `tokei`  | `tokei --output json`                         | 0    | Per-language structured.                                        |
| `cloc`   | `cloc --json .`                                | 0    | Slower; informational fallback.                                |

## Failure modes & recovery

| Symptom                                | Hoopoe response                                                          |
| -------------------------------------- | ------------------------------------------------------------------------ |
| `lizard` not installed                 | Mark complexity `missing`; UI hides the metric.                           |
| `scc` and `tokei` both missing          | Mark LOC `missing`.                                                       |
| Lizard XML parse error                 | Bubble; capability `degraded`.                                            |
| Symlink loops in scan                  | Use `--exclude` and `-Z` (lizard `--exclude-pattern`).                    |

## Worktree isolation (Guardrail 5)

`~/.hoopoe/work/<project-id>/health/<run-id>/` like other health adapters.

## Test fixtures

| Scenario | Fixture path                                                                              | Asserts                                          |
| -------- | ----------------------------------------------------------------------------------------- | ------------------------------------------------ |
| `fresh`  | `packages/fixtures/phase0-2026-05-02/scenarios/fresh/health/generic/`                     | Multi-lang skeleton; baseline LOC + complexity.  |
| `active` | `packages/fixtures/phase0-.../scenarios/active/health/generic/scc.json`                   | Real multi-lang repo; populated metrics.         |
| `failure`| `packages/fixtures/phase0-.../scenarios/failure/health/generic/`                          | All tools missing → `missing` reported.          |

## Adapter notes (Hoopoe Go side)

- Lives at `apps/daemon/internal/adapters/health/generic/` (Phase 11, bead `hp-9uh`).
- Always-on for any project (even when a language-specific adapter is also engaged).
- Hotspot scoring uses generic complexity + churn when language-specific complexity unavailable.
