# Health adapter — Rust

> Code-health metrics for Rust crates / workspaces (`plan.md` §7.4, §11).

## Source of truth

| Tool                   | Purpose                  | Notes                                                                 |
| ---------------------- | ------------------------ | --------------------------------------------------------------------- |
| `cargo llvm-cov`       | Coverage (preferred)     | `cargo llvm-cov --workspace --json --output-path coverage.json`       |
| `cargo tarpaulin`      | Coverage (fallback)      | `cargo tarpaulin --workspace --out Json -o coverage.json`             |
| `cargo clippy`         | Lint + complexity        | `cargo clippy --workspace --message-format=json -- -W clippy::cognitive_complexity` |
| `lizard`               | Generic complexity       | Cross-lang fallback                                                   |
| `git log --numstat`    | Churn                    | Same as other adapters                                                |
| `cargo nextest`        | Test execution            | Per ACFS recommendation; emits machine-readable test list             |
| `tokei` / `scc`        | LOC                      |                                                                        |

## Capability IDs (per `plan.md` §2.8)

| capId                          | Surface                                                                | Notes                                                              |
| ------------------------------ | ---------------------------------------------------------------------- | ------------------------------------------------------------------ |
| `health.rust.coverage`         | `cargo llvm-cov --workspace --json`                                    | Per-file + per-region                                              |
| `health.rust.complexity`       | `cargo clippy --workspace --message-format=json` + clippy lints        | `cognitive_complexity` lint                                        |
| `health.rust.churn`            | `git log --numstat -- '*.rs'`                                          |                                                                     |
| `health.rust.lint`             | `cargo clippy` JSON messages                                           | Combined into finding ledger                                       |

## Command surfaces (planned — pin per project)

| Label              | argv                                                                                       | Exit | Notes                                                     |
| ------------------ | ------------------------------------------------------------------------------------------ | ---- | --------------------------------------------------------- |
| `cargo_llvm_cov`   | `rch exec -- env CARGO_TARGET_DIR=$TMPDIR/rch_target_$PROJECT cargo llvm-cov --workspace --json` | 0/1 | `CARGO_TARGET_DIR` per project to avoid cache thrash. |
| `cargo_clippy`     | `rch exec -- env CARGO_TARGET_DIR=... cargo clippy --workspace --message-format=json -- -W clippy::cognitive_complexity` | 0/1 | JSON message format. |
| `cargo_nextest`    | `rch exec -- env CARGO_TARGET_DIR=... cargo nextest run --workspace --message-format json` | 0/1 | Test execution.                                          |

## Failure modes & recovery

| Symptom                                                     | Hoopoe response                                                          |
| ----------------------------------------------------------- | ------------------------------------------------------------------------ |
| `cargo llvm-cov` not installed (`cargo install cargo-llvm-cov`) | Adapter falls back to `tarpaulin`; if neither, marks coverage `missing`. |
| Workspace builds invalidated by sibling cargo run           | `CARGO_TARGET_DIR` per-project enforced by argv builder.                  |
| OOM on large workspace                                       | Per-crate scan; never whole-workspace at once on shared boxes.            |
| Compiler errors block coverage                              | Surface compile errors first; coverage capability marked `degraded`.     |

## Worktree isolation (Guardrail 5)

Health jobs run in `~/.hoopoe/work/<project-id>/health/<run-id>/`. Cargo's target dir lives outside the worktree to keep the worktree disposable.

## Test fixtures

| Scenario | Fixture path                                                                              | Asserts                                          |
| -------- | ----------------------------------------------------------------------------------------- | ------------------------------------------------ |
| `fresh`  | `packages/fixtures/phase0-2026-05-02/scenarios/fresh/health/rust/`                        | Hello-world crate; coverage baseline.            |
| `active` | `packages/fixtures/phase0-.../scenarios/active/health/rust/coverage.json`                 | Multi-crate workspace, ≥ 1 hotspot.              |
| `failure`| `packages/fixtures/phase0-.../scenarios/failure/health/rust/`                             | Compile error blocks coverage.                   |

## Adapter notes (Hoopoe Go side)

- Lives at `apps/daemon/internal/adapters/health/rust/` (Phase 11, bead `hp-9uh`).
- Argv builder enforces `CARGO_TARGET_DIR` and `--message-format=json` for cargo invocations.
- For very large Rust workspaces, the daemon's job runner uses `rch exec` to offload (matches `AGENTS.md` rch pattern).
