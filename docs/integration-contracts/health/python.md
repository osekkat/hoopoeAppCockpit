# Health adapter — Python

> Code-health metrics for Python projects (`plan.md` §7.4, §11).

## Source of truth

| Tool                  | Purpose                            | Notes                                                                 |
| --------------------- | ---------------------------------- | --------------------------------------------------------------------- |
| `pytest --cov`        | Coverage                           | Via `pytest-cov`; JSON via `--cov-report=json:coverage.json`.         |
| `coverage` (raw)      | Coverage (when not using pytest)   | `coverage run -m unittest && coverage json -o coverage.json`         |
| `radon`               | Cyclomatic complexity              | `radon cc -j .` returns JSON.                                         |
| `lizard`              | Cross-lang complexity              | Used when `radon` not present.                                        |
| `ruff check --format=json` | Lint                          | Fast, includes complexity rules.                                      |
| `git log --numstat`   | Churn                              | Same as TS adapter.                                                   |
| `tokei` / `scc`       | LOC                                |                                                                        |

## Capability IDs (per `plan.md` §2.8)

| capId                          | Surface                                              | Notes                                                              |
| ------------------------------ | ---------------------------------------------------- | ------------------------------------------------------------------ |
| `health.python.coverage`       | `pytest --cov --cov-report=json:coverage.json`       | Per-file coverage                                                  |
| `health.python.complexity`     | `radon cc -j .` (or `lizard` fallback)               | Per-function complexity                                            |
| `health.python.churn`          | `git log --numstat -- '*.py'`                        |                                                                     |
| `health.python.lint`           | `ruff check --format=json`                           | Often subsumes complexity                                          |

## Command surfaces (planned — pin per project)

| Label              | argv                                                              | Exit | Notes                                                     |
| ------------------ | ----------------------------------------------------------------- | ---- | --------------------------------------------------------- |
| `pytest_cov`       | `uv run pytest --cov --cov-report=json:coverage.json`             | 0/1  | `uv run` per ACFS toolchain; falls back to `python -m pytest`. |
| `radon_cc`         | `radon cc -j .`                                                   | 0    | JSON output.                                              |
| `ruff_check`       | `ruff check --format=json .`                                      | 0/1  | Lint; combined into finding ledger.                       |
| `lizard`           | `lizard --xml`                                                    | 0    | Cross-lang fallback for complexity.                       |

## Failure modes & recovery

| Symptom                                | Hoopoe response                                                          |
| -------------------------------------- | ------------------------------------------------------------------------ |
| `pytest` not configured                | Fall back to `coverage` raw; if neither, mark coverage `missing`.        |
| Test execution hangs                   | Per-test timeout; surface to Activity panel.                              |
| `radon` not installed                  | Use `lizard --xml` and parse XML (parser ships in adapter).              |
| Virtual env mismatch                    | Adapter uses project's `.venv` / `pyproject.toml` resolver via `uv`.     |

## Worktree isolation (Guardrail 5)

Same as TS — `~/.hoopoe/work/<project-id>/health/<run-id>/`.

## Test fixtures

| Scenario | Fixture path                                                                              | Asserts                                          |
| -------- | ----------------------------------------------------------------------------------------- | ------------------------------------------------ |
| `fresh`  | `packages/fixtures/phase0-2026-05-02/scenarios/fresh/health/python/`                      | Project skeleton; no tests yet.                  |
| `active` | `packages/fixtures/phase0-.../scenarios/active/health/python/coverage.json`               | 70 % coverage, 2-3 hotspots.                     |
| `failure`| `packages/fixtures/phase0-.../scenarios/failure/health/python/`                           | Test execution timeout captured.                 |

## Adapter notes (Hoopoe Go side)

- Lives at `apps/daemon/internal/adapters/health/python/` (Phase 11, bead `hp-9uh`).
- Same scoring + persistence path as TS.
