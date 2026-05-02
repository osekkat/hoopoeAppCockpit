# `srp` (System Resource Protection) integration contract

> Disk / CPU / load signals for `watch-safety-thresholds` (`plan.md` §8.4). Read-only diagnostic source.

## Source of truth

| Field    | Value                                                |
| -------- | ---------------------------------------------------- |
| Tool     | `srp`                                                |
| Repo     | TBD (canonical Dicklesworthstone repo; pin on VPS)   |
| Observed | Not on PATH on dev box (research-spike 2026-05-02)   |
| Min compatible | TBD                                            |

## Adapter precedence (per `plan.md` §2.3)

1. **`srp signals --json`** — primary read.
2. **`srp status --json`** — adapter probe.

## Capability IDs (per `plan.md` §2.8)

| capId                       | Required by                              | Surface                                              | Notes                                              |
| --------------------------- | ---------------------------------------- | ---------------------------------------------------- | -------------------------------------------------- |
| `srp.signals.read`          | `watch-safety-thresholds`                | `srp signals --json`                                 | Polled ~5 s under active swarm                     |
| `srp.status.read`           | Adapter probe                             | `srp status --json`                                  |                                                     |

## Command surfaces (planned — pin on VPS)

| Label     | argv                          | Exit | Returns                                                                  |
| --------- | ----------------------------- | ---- | ------------------------------------------------------------------------ |
| `help`    | `srp --help`                  | 0    | Adapter probe.                                                            |
| `signals` | `srp signals --json`          | 0    | `{cpu: {load1, load5, load15}, mem: {used_mb, free_mb}, disk: {/data: {free_gb, percent}, ...}, swap: {...}}` |
| `status`  | `srp status --json`           | 0    | `{healthy, warnings: [...]}`                                             |

## Failure modes & recovery

| Symptom                                              | Root cause                                       | Hoopoe response                                                        |
| ---------------------------------------------------- | ------------------------------------------------ | ---------------------------------------------------------------------- |
| `srp` not on PATH                                    | ACFS install incomplete                          | Adapter falls back to `/proc/loadavg`, `/proc/meminfo`, `df`; reports `degraded`. |
| Signals stale                                         | `srp` daemon paused                              | Adapter detects via timestamp; restart with approval.                  |
| Disk >90 % alert                                      | Usual — fixture corpus, build artifacts          | `tend-swarm` triggers `sbh.cleanup` proposal (with approval).          |

## Authentication / credentials

- None.

## Known gotchas (preliminary)

See [`../research-spike/gotchas.md`](../research-spike/gotchas.md) (`hp-d54`).

- `srp` thresholds are configurable; Hoopoe must read the *current threshold* with the signals so `watch-safety-thresholds` doesn't double-judge.
- Disk metrics are per-mount; adapter must enumerate `/data`, `/var/log`, `~/.hoopoe/` separately.

## Test fixtures (placeholder — VPS pin)

| Scenario | Fixture path                                                              | Asserts                                                                 |
| -------- | ------------------------------------------------------------------------- | ----------------------------------------------------------------------- |
| `fresh`  | `packages/fixtures/phase0-2026-05-02/scenarios/fresh/snapshot.json`       | All signals healthy.                                                    |
| `active` | `packages/fixtures/phase0-.../scenarios/active/snapshot.json`             | Load 1.5–4.0 (active swarm); disk normal.                                |
| `failure`| `packages/fixtures/phase0-.../scenarios/failure/snapshot.json`            | Disk >90 %; `tend-swarm` proposal captured.                              |

## Adapter notes (Hoopoe Go side)

- Lives at `apps/daemon/internal/adapters/srp/` (Phase 10, integrated with `watch-safety-thresholds`).
- Polling: 5 s under active swarm, 30 s when idle.
- Read-only adapter; never mutates.
