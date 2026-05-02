# `rch` (Remote Compilation Helper) integration contract

> Build offload to remote workers; fails open to local execution when workers unavailable. Hoopoe wraps build/test commands with `rch exec --` so dispatch is consistent and instrumented (`plan.md` §7.3, §8.5, §17). Also one of the canonical build/test execution sources of truth (`§1.1` table).

## Source of truth

| Field    | Value                                                            |
| -------- | ---------------------------------------------------------------- |
| Tool     | `rch`                                                            |
| Repo     | TBD (canonical Dicklesworthstone repo; pin on VPS)               |
| Observed | `rch 1.0.13` (research-spike 2026-05-02 dev box)                 |
| Min compatible | 1.0+                                                       |
| Reference | `AGENTS.md` "RCH — Remote Compilation Helper" section          |

## Adapter precedence (per `plan.md` §2.3, §1.1)

1. **`rch exec -- <cmd>`** — wraps any compilation/test command. Routes to remote worker if configured + healthy; else local.
2. **`rch doctor` / `rch workers probe --all` / `rch status` / `rch queue`** — diagnostics.
3. **Direct `bun run` / `go test` / `cargo build`** as last resort if `rch` not installed.

## Capability IDs (per `plan.md` §2.8)

| capId                       | Required by                              | Surface                                              | Notes                                              |
| --------------------------- | ---------------------------------------- | ---------------------------------------------------- | -------------------------------------------------- |
| `rch.exec`                  | Build/test queue                         | `rch exec -- <cmd>`                                  | Pass-through wrapper; never modifies cmd output    |
| `rch.doctor`                | Diagnostics                               | `rch doctor`                                         | Health snapshot                                     |
| `rch.workers.probe`         | Diagnostics                               | `rch workers probe --all`                            | Remote-worker connectivity                          |
| `rch.status.read`           | Build queue UI                           | `rch status` / `rch queue`                           | Active + waiting builds                             |
| `rch.help`                  | Adapter probe                             | `rch --help`                                         |                                                     |

## Command surfaces (observed)

| Label             | argv                                          | Exit                  | Notes                                                          |
| ----------------- | --------------------------------------------- | --------------------- | -------------------------------------------------------------- |
| `help`            | `rch --help`                                  | 0                     | Adapter probe.                                                 |
| `version`         | `rch --version`                               | 0                     | `rch 1.0.13` observed.                                         |
| `exec`            | `rch exec -- <cmd> [args...]`                 | passthrough            | Exit code = wrapped command's exit code.                       |
| `doctor`          | `rch doctor`                                  | 0/1                   | Health summary (workers, daemon, queue).                        |
| `status`          | `rch status`                                  | 0                     | Active builds.                                                  |
| `queue`           | `rch queue`                                   | 0                     | Waiting builds.                                                 |
| `workers_probe`   | `rch workers probe --all`                     | 0/1                   | Per-worker connectivity check.                                  |

## Failure modes & recovery

| Symptom                                              | Root cause                                       | Hoopoe response                                                        |
| ---------------------------------------------------- | ------------------------------------------------ | ---------------------------------------------------------------------- |
| `rch exec` warning "non-compilation command"         | Wrapped non-build command                         | Cosmetic; ignore. The wrapper still runs the command locally.          |
| All workers unhealthy                                | Worker daemons offline                            | Fail-open to local; surface "remote build offline" badge in Diagnostics. |
| `rch doctor` exit non-zero                           | Local rch daemon issue                            | Surface to Diagnostics; user runs `rch doctor` interactively.           |
| Build queue backed up                                | More work than capacity                           | Show queue depth in Build Queue panel; user may pause new launches.    |
| Network flake during exec                            | Worker connection dropped                         | rch retries internally; adapter forwards final exit code.              |

## Authentication / credentials

- Per-worker SSH keys (managed by `rch workers add`). Hoopoe never reads or rotates worker creds.

## Known gotchas

See [`../research-spike/gotchas.md`](../research-spike/gotchas.md) (`hp-d54`).

- **Always wrap with `rch exec --`** even when no remote workers are configured — fail-open is fine and the dispatch is instrumented.
- Set `RCH_VISIBILITY=verbose` for slow-build investigation.
- `cargo` builds need `CARGO_TARGET_DIR` set per-cwd to avoid cross-build cache thrash:
  `rch exec -- env CARGO_TARGET_DIR=$TMPDIR/rch_target_$(basename $PWD) cargo check --workspace --all-targets`
- Local fail-open is **expected** in dev — don't treat the warning as an error.
- Concurrent agent builds collide on `~/.cache/bun`, `~/.cache/turbo`. Coordinate via `[hoopoe-builds]` Agent Mail thread (per per-pane runbook).

## Test fixtures

| Scenario | Fixture path                                                              | Asserts                                                                 |
| -------- | ------------------------------------------------------------------------- | ----------------------------------------------------------------------- |
| `fresh`  | `packages/fixtures/phase0-2026-05-02/scenarios/fresh/snapshot.json`       | `rch doctor` healthy; queue empty.                                      |
| `active` | `packages/fixtures/phase0-.../scenarios/active/snapshot.json`             | Active build in flight; queue depth ≥ 1.                                |
| `failure`| `packages/fixtures/phase0-.../scenarios/failure/snapshot.json`            | Worker connectivity failure; fail-open captured.                        |

## Adapter notes (Hoopoe Go side)

- Lives at `apps/daemon/internal/adapters/rch/` (Phase 8, integrated with daemon Build Queue / `§2.7`).
- Build queue (`hp-gkk`): jobs go through `rch exec --` for instrumentation; adapter records exit code, duration, output path.
- Adapter does **not** parse build output — it forwards exit code; specialized parsers (test-result reader, lint adapter) consume the output downstream.
