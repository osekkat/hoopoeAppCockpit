# Health adapter — Go

> Code-health metrics for Go modules / workspaces (`plan.md` §7.4, §11).
>
> **Status: forward-looking Phase 11 contract.** The adapter sub-package
> (`apps/daemon/internal/adapters/health/go/`) and the
> `packages/fixtures/.../health/go/` fixture trees referenced below **do not
> exist on disk yet.** The current seed implementation lives in
> `apps/daemon/internal/health/health.go` (snapshot envelope + in-process probe
> scaffolding) plus its `health_test.go`. Phase 11 (bead `hp-9uh`) introduces
> the per-language adapter sub-package and the fixture corpus in line with this
> contract.

## Source of truth

| Tool                                | Purpose                  | Notes                                                                 |
| ----------------------------------- | ------------------------ | --------------------------------------------------------------------- |
| `go test -cover -coverprofile=...`  | Coverage                 | Standard tooling; produces `cover.out`.                              |
| `go tool cover -func=cover.out`     | Coverage summary         | Per-function summary.                                                  |
| `go tool cover -html=cover.out`     | Coverage HTML            | UI rendering reference (not consumed by adapter).                     |
| `gocyclo` / `gocognit`              | Complexity               | `gocognit -json .`                                                    |
| `golangci-lint run --out-format json` | Lint + complexity      | Aggregates many linters.                                               |
| `git log --numstat`                 | Churn                    | Same as other adapters.                                                |
| `lizard`                            | Generic complexity        | Cross-lang fallback.                                                   |
| `scc` / `tokei`                     | LOC                      |                                                                        |

## Capability IDs (per `plan.md` §2.8)

| capId                          | Surface                                                                                 | Notes                                                              |
| ------------------------------ | --------------------------------------------------------------------------------------- | ------------------------------------------------------------------ |
| `health.go.coverage`           | `go test -coverprofile=cover.out ./... && go tool cover -func=cover.out`                | Per-function summary                                               |
| `health.go.complexity`         | `gocognit -json .` (or `gocyclo -json .`)                                                | Per-function score                                                  |
| `health.go.churn`              | `git log --numstat -- '*.go'`                                                            |                                                                     |
| `health.go.lint`               | `golangci-lint run --out-format json`                                                    | Combined into finding ledger                                       |

## Command surfaces (planned — pin per project)

| Label                  | argv                                                                                          | Exit | Notes                                                     |
| ---------------------- | --------------------------------------------------------------------------------------------- | ---- | --------------------------------------------------------- |
| `go_test_cover`        | `rch exec -- go test -coverprofile=cover.out -covermode=atomic ./...`                          | 0/1  | `-covermode=atomic` for race-safe coverage.              |
| `go_cover_func`        | `go tool cover -func=cover.out`                                                                | 0    | Per-function summary; adapter parses to JSON.            |
| `gocognit`             | `gocognit -json .`                                                                             | 0    | Per-function complexity.                                  |
| `golangci_lint`        | `rch exec -- golangci-lint run --out-format json ./...`                                        | 0/1  | Aggregated lint output.                                  |

## Failure modes & recovery

| Symptom                                          | Hoopoe response                                                          |
| ------------------------------------------------ | ------------------------------------------------------------------------ |
| `go test` build error                            | Surface compile error; coverage capability marked `degraded`.            |
| `gocognit` not installed                          | Use `gocyclo` fallback; if neither, mark complexity `missing`.            |
| `golangci-lint` config missing                    | Use baked-in default config; surface "no project config" warning.        |
| Coverage `cover.out` missing on success          | Treat as failure; surface in Activity panel.                              |

## Worktree isolation (Guardrail 5)

Health jobs run in `~/.hoopoe/work/<project-id>/health/<run-id>/`. `go build` cache lives in `$GOCACHE` (per-user, not per-worktree) — that's fine, no thrash like cargo.

## Test fixtures

| Scenario | Fixture path                                                                              | Asserts                                          |
| -------- | ----------------------------------------------------------------------------------------- | ------------------------------------------------ |
| `fresh`  | `packages/fixtures/phase0-2026-05-02/scenarios/fresh/health/go/`                          | Hello-world module.                              |
| `active` | `packages/fixtures/phase0-.../scenarios/active/health/go/cover.out`                       | Multi-package, ~70 % coverage.                   |
| `failure`| `packages/fixtures/phase0-.../scenarios/failure/health/go/`                               | Compile error captured.                          |

## Adapter notes (Hoopoe Go side)

- **Today** the only on-disk health code is `apps/daemon/internal/health/health.go`. No per-language adapter sub-packages exist yet.
- **Planned** under bead `hp-9uh` (Phase 11):
  - Adapter at `apps/daemon/internal/adapters/health/go/`.
  - Cover.out parser: `go tool cover -func` is the preferred summary surface; per-line precision is in `cover.out` directly.
  - `golangci-lint` JSON ingested into the §9.3 finding ledger with `source: golangci-lint` stamp.
