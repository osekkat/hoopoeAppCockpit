# `sbh` (disk-pressure / stale-bytes housekeeper) integration contract

> Stale-artifact cleanup invoked under disk pressure (`plan.md` §8.5). **Mutating** for `cleanup`; through `ActionPlan` only.

## Source of truth

| Field    | Value                                                |
| -------- | ---------------------------------------------------- |
| Tool     | `sbh`                                                |
| Repo     | TBD (canonical Dicklesworthstone repo; pin on VPS)   |
| Observed | Not on PATH on dev box (research-spike 2026-05-02)   |
| Min compatible | TBD                                            |

## Adapter precedence (per `plan.md` §2.3)

1. **`sbh status --json`** — read-only inventory of cleanable artifacts.
2. **`sbh cleanup --dry-run --json`** — preview.
3. **`sbh cleanup --apply`** — mutating; ActionPlan-gated.

## Capability IDs (per `plan.md` §2.8)

| capId                       | Required by                              | Surface                                              | Notes                                              |
| --------------------------- | ---------------------------------------- | ---------------------------------------------------- | -------------------------------------------------- |
| `sbh.status.read`           | Diagnostics tab                           | `sbh status --json`                                  | Read-only                                          |
| `sbh.cleanup.dry_run`       | Cleanup preview                           | `sbh cleanup --dry-run --json`                       | Read-only                                          |
| `sbh.cleanup.apply`         | Disk-pressure recovery                    | `sbh cleanup --apply --json`                         | `blocked-by-policy` outside ActionPlan; approval  |

## Command surfaces (planned — pin on VPS)

| Label             | argv                                  | Exit | Notes                                                          |
| ----------------- | ------------------------------------- | ---- | -------------------------------------------------------------- |
| `help`            | `sbh --help`                          | 0    | Adapter probe.                                                 |
| `status`          | `sbh status --json`                   | 0    | `{cleanable_bytes, by_category: {...}, last_cleanup_ts}`       |
| `cleanup_dry_run` | `sbh cleanup --dry-run --json`        | 0    | Lists candidates by category, never deletes.                   |
| `cleanup_apply`   | `sbh cleanup --apply --json`          | 0    | Mutating; never invoked by snapshot.sh.                        |

## Failure modes & recovery

| Symptom                                              | Root cause                                       | Hoopoe response                                                        |
| ---------------------------------------------------- | ------------------------------------------------ | ---------------------------------------------------------------------- |
| `cleanup --apply` removes more than expected         | Categories misclassified                          | Adapter passes explicit `--category` allowlist; never `--all`.         |
| Cleanup blocked by file in use                       | `lsof` says file held                             | Skip + report in status; never force-delete.                           |
| Disk pressure persists after cleanup                 | True usage exceeds cleanable                      | Surface to user; offer disk-resize / migration runbook.                |

## Authentication / credentials

- None. File-system level cleanup.

## Known gotchas (preliminary)

See [`../research-spike/gotchas.md`](../research-spike/gotchas.md) (`hp-d54`).

- **NEVER call `cleanup --apply` without explicit category list.** `--all` would delete cache + worktrees + build artifacts including in-use ones.
- `sbh` may target `~/.cache/`, `/tmp/`, and `~/.hoopoe/work/` — Hoopoe must reserve current swarm worktrees so `sbh` doesn't sweep them.
- `cleanup` is non-atomic; partial failures land. Adapter must read final `status` after to confirm.

## Test fixtures (placeholder — VPS pin)

| Scenario | Fixture path                                                              | Asserts                                                                 |
| -------- | ------------------------------------------------------------------------- | ----------------------------------------------------------------------- |
| `fresh`  | `packages/fixtures/phase0-2026-05-02/scenarios/fresh/snapshot.json`       | `cleanable_bytes < 1 GB`; healthy.                                      |
| `active` | `packages/fixtures/phase0-.../scenarios/active/snapshot.json`             | `cleanable_bytes 5-20 GB`; cleanup proposal not triggered.              |
| `failure`| `packages/fixtures/phase0-.../scenarios/failure/snapshot.json`            | `cleanable_bytes > 50 GB`; `tend-swarm` cleanup proposal captured.      |

## Adapter notes (Hoopoe Go side)

- Lives at `apps/daemon/internal/adapters/sbh/` (Phase 10).
- ActionPlan: `sbh.cleanup_apply` requires explicit categories; argv builder rejects empty list.
- Approval policy: `requires_confirmation` always (autopilot may pre-approve `--dry-run` only).
