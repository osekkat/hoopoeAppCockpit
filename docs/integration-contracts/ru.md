# `ru` (Repo Updater) integration contract

> **Adopted narrowly** — only the read/sync/list/prune surfaces are wired into Hoopoe's `GitAdapter` (`plan.md` §2.3, §17). The richer `ru review` / `ru agent-sweep` / `ru ai-sync` / `ru dep-update` workflows are **deliberately not adopted at runtime** because they collide with `§7.4` / `§8` / `§9` (parallel session-state would violate `§1.1`). Read `ru`'s patterns; do not invoke them.

## Source of truth

| Field    | Value                                                            |
| -------- | ---------------------------------------------------------------- |
| Tool     | `ru` (Repo Updater)                                              |
| Repo     | <https://github.com/Dicklesworthstone/repo_updater>              |
| Observed | `ru version 1.3.1` (research-spike 2026-05-02)                   |
| Min compatible | 1.2+ (`--schema`, `robot-docs`, `--non-interactive` flags) |
| State store | `~/.local/state/ru/**` — Hoopoe **does not write** here       |

## Adapter precedence (per `plan.md` §2.3)

1. **`ru sync --json --non-interactive --dry-run`** — sync-time read; never blocks waiting for input.
2. **`ru status --no-fetch --json`** — fast multi-repo status without hitting origin.
3. **`ru list --paths`** — repo enumeration.
4. **`ru prune --archive` / `ru prune --dry-run`** — Diagnostics-tab repair (`§10.2`); destructive `--archive` only on user request.
5. **`ru --schema`** — adapter validation reference.
6. **`ru robot-docs`** — auto-generated docs of `--robot-*` surfaces.
7. **`ru --robot-spawn/send/wait/activity/status/interrupt`** — NTM-aware robot mode. Hoopoe **does not invoke** these; we use NTM directly. Listed here so adapters know they exist.

## Capability IDs (per `plan.md` §2.8)

| capId                  | Required by                              | Surface                                              | Status semantics                                        |
| ---------------------- | ---------------------------------------- | ---------------------------------------------------- | ------------------------------------------------------- |
| `ru.sync.dry_run`      | Phase 4 multi-repo status                | `ru sync --dry-run --json --non-interactive`         | `--non-interactive` is mandatory; without it `ru` may prompt |
| `ru.status.read`       | Top-bar + Activity (multi-repo case)     | `ru status --no-fetch --json`                        | `--no-fetch` keeps it offline-fast                      |
| `ru.list.paths`        | Project enumeration                       | `ru list --paths`                                    | One path per line                                       |
| `ru.prune.dry_run`     | Diagnostics repair preview                | `ru prune --dry-run`                                 | Lists candidates; never deletes                          |
| `ru.prune.archive`     | Diagnostics repair (with approval)        | `ru prune --archive`                                 | `blocked-by-policy` outside Diagnostics tab              |
| `ru.schema`            | Adapter validation                        | `ru --schema`                                        | Multi-KB JSON; stream-parse if memory matters            |
| `ru.robot.docs`        | Diagnostics deep-link                     | `ru robot-docs`                                      | Returns Markdown                                         |
| `ru.review`            | (deliberately not adopted)                | `ru review`                                          | `blocked-by-policy` — clashes with §7.4/§9 finding flow |
| `ru.agent_sweep`       | (deliberately not adopted)                | `ru agent-sweep`                                     | `blocked-by-policy` — clashes with §8 tending           |
| `ru.ai_sync`           | (deliberately not adopted)                | `ru ai-sync`                                         | `blocked-by-policy`                                      |
| `ru.dep_update`        | (deliberately not adopted)                | `ru dep-update`                                      | `blocked-by-policy`                                      |

## Command surfaces (observed)

| Label              | argv                                            | Exit | Notes                                                           |
| ------------------ | ----------------------------------------------- | ---- | --------------------------------------------------------------- |
| `help`             | `ru --help`                                     | 0    | Long banner; lists all subcommands including the adopted ones.  |
| `schema`           | `ru --schema`                                   | 0    | JSON Schema document (multi-KB).                                 |
| `robot_docs`       | `ru robot-docs`                                 | 0    | Markdown describing `--robot-*` surfaces.                        |
| `sync_dry_run`     | `ru sync --dry-run --json --non-interactive`    | 0    | NDJSON per-repo result (`ru` emits one JSON line per repo).      |
| `status`           | `ru status --no-fetch --json`                   | 0    | Per-repo status (clean, dirty, unpushed, etc.).                  |
| `list_paths`       | `ru list --paths`                               | 0    | Newline-delimited absolute paths.                                |
| `prune_dry_run`    | `ru prune --dry-run`                            | 0    | Lists prune candidates; safe.                                     |

## Failure modes & recovery

| Symptom                                                | Root cause                                            | Hoopoe response                                                          |
| ------------------------------------------------------ | ----------------------------------------------------- | ------------------------------------------------------------------------ |
| `ru sync` hangs                                         | `--non-interactive` missing; `ru` prompts             | Always include `--non-interactive`. Adapter rejects argv missing it.     |
| `ru status` returns stale data                          | `--no-fetch` was set                                  | Expected. For fresh, drop `--no-fetch` (slower).                         |
| State-store corruption (`~/.local/state/ru/`)           | Disk full or process killed mid-write                  | Surface to Diagnostics; user runs `ru prune --archive` with approval.    |
| `ru --schema` truncated                                 | Adapter `--max-bytes` too low                         | Raise cap on this single capture; default is fine for most.              |
| Missing repo in `ru list --paths`                       | Repo not under one of `ru`'s configured roots         | Adapter falls back to per-project `git` adapter for that repo.            |

## Authentication / credentials

- `ru` inherits SSH/HTTPS credentials from the user (same as `git`).
- No CAAM involvement.

## Known gotchas

See [`../research-spike/gotchas.md`](../research-spike/gotchas.md) (`hp-d54`).

- **Always `--non-interactive`** — `ru sync` will prompt without it, blocking any agent session.
- **State store at `~/.local/state/ru/`** — touching this from Hoopoe creates a parallel source of truth (violates `§1.1`). Read-only via `ru status` only.
- `ru sync --json` emits **NDJSON** (one JSON object per repo), not a single JSON array. Parsers must handle line-delimited.
- The richer subcommands (`review`, `agent-sweep`, `ai-sync`, `dep-update`) are intentionally `blocked-by-policy` in Hoopoe — they have their own session-state model that doesn't compose with beads + Activity panel.
- `ru` predates Hoopoe but its `~17,700 lines of worked Bash` (per `plan.md` §17) are the *reference implementation* for several Go adapters: backoff, NTM-state mapping, blocking-prompt risk classification, secret-scan patterns, GraphQL alias batching. Read those code paths during the corresponding phases.

## Test fixtures

| Scenario | Fixture path                                                              | Asserts                                                                 |
| -------- | ------------------------------------------------------------------------- | ----------------------------------------------------------------------- |
| `fresh`  | `packages/fixtures/phase0-2026-05-02/scenarios/fresh/snapshot.json`       | `sync_dry_run` reports zero pending; clean status                       |
| `active` | `packages/fixtures/phase0-.../scenarios/active/snapshot.json`             | Multi-repo `status` with mix of clean/dirty/unpushed                    |
| `failure`| `packages/fixtures/phase0-.../scenarios/failure/snapshot.json`            | `prune_dry_run` shows non-empty list; state-store partial-write captured|

## Adapter notes (Hoopoe Go side)

- Lives at `apps/daemon/internal/adapters/git/ru/` (Phase 4, bead `hp-w8m`).
- Argv builder enforces `--non-interactive` and `--json` for every sync/status invocation; rejects argv missing them at compile time (Go test).
- The blocked-by-policy capabilities (`ru.review`, etc.) are stored in the registry but the adapter has **no execution path** for them — the registry surfaces "feature exists in tool but Hoopoe routes through bead/Activity flow instead."
- Patterns to **port**, not invoke: backoff (`apps/daemon/internal/scheduler/backoff.go`), risk-classification (`apps/daemon/internal/safety/risk.go`), secret-scan (`apps/daemon/internal/redaction/secrets.go`).
