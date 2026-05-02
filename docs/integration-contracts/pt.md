# `pt` (process-terminator) integration contract

> Deterministic actuator for killing genuinely wedged processes, used by `watch-safety-thresholds` (`plan.md` §8.4). **Mutating; through `ActionPlan` only.** `pt.kill` is `blocked-by-policy` outside the typed action surface.

## Source of truth

| Field    | Value                                                |
| -------- | ---------------------------------------------------- |
| Tool     | `pt`                                                 |
| Repo     | TBD (canonical Dicklesworthstone repo; pin on VPS)   |
| Observed | Not on PATH on dev box (research-spike 2026-05-02)   |
| Min compatible | TBD                                            |

## Adapter precedence (per `plan.md` §2.3)

1. **`pt kill <target>`** — mutating actuator; ActionPlan-gated.
2. **`pt list --json`** — non-destructive enumeration of candidate processes.
3. **`pt status --json`** — adapter probe.

## Capability IDs (per `plan.md` §2.8)

| capId                       | Required by                              | Surface                                              | Notes                                              |
| --------------------------- | ---------------------------------------- | ---------------------------------------------------- | -------------------------------------------------- |
| `pt.list`                   | Diagnostics + Activity panel             | `pt list --json`                                     | Read-only                                          |
| `pt.kill`                   | `watch-safety-thresholds`                | `pt kill <target> --reason '<text>'`                 | `blocked-by-policy` outside ActionPlan; approval  |
| `pt.status.read`            | Adapter probe                             | `pt status --json`                                   |                                                     |

## Command surfaces (planned — pin on VPS)

| Label    | argv                                            | Exit | Notes                                               |
| -------- | ----------------------------------------------- | ---- | --------------------------------------------------- |
| `help`   | `pt --help`                                     | 0    | Adapter probe.                                       |
| `list`   | `pt list --json`                                | 0    | `[{pid, cmd, age_s, parent, ...}]`                  |
| `status` | `pt status --json`                              | 0    | Quick health.                                        |
| `kill`   | `pt kill <pid|name> --reason '<text>' --json`   | 0/1  | Mutating; never invoked by snapshot.sh.             |

## Failure modes & recovery

| Symptom                                              | Root cause                                       | Hoopoe response                                                        |
| ---------------------------------------------------- | ------------------------------------------------ | ---------------------------------------------------------------------- |
| `pt kill` exit non-zero                              | Permission denied / target gone                  | Surface to Activity panel; do not retry without approval.              |
| Wrong process killed                                  | Stale PID or PID reuse                           | Adapter passes both PID and `cmdline` substring; `pt` should reject if cmdline mismatches. |
| `pt list --json` returns empty                       | Healthy or filter too narrow                     | Treat as "nothing to terminate."                                       |

## Authentication / credentials

- None. Runs as the daemon user; relies on Linux process ownership semantics.
- Audit captures actor, target, reason regardless of activity-panel suppression (Guardrail 10).

## Known gotchas (preliminary)

See [`../research-spike/gotchas.md`](../research-spike/gotchas.md) (`hp-d54`).

- PID reuse: between `pt list` and `pt kill`, the OS may reuse a PID. `pt` SHOULD verify cmdline match before killing; adapter requires both.
- Killing tmux/NTM panes via `pt` rather than NTM is wrong — prefer NTM's `pane.kill` for orchestration; reserve `pt` for true wedged background processes.
- `pt.kill` requires explicit reason text — adapter argv builder rejects missing reason.

## Test fixtures (placeholder — VPS pin)

| Scenario | Fixture path                                                              | Asserts                                                                 |
| -------- | ------------------------------------------------------------------------- | ----------------------------------------------------------------------- |
| `fresh`  | `packages/fixtures/phase0-2026-05-02/scenarios/fresh/snapshot.json`       | `pt list` empty; status healthy.                                        |
| `active` | `packages/fixtures/phase0-.../scenarios/active/snapshot.json`             | `pt list` shows agent processes (informational).                        |
| `failure`| `packages/fixtures/phase0-.../scenarios/failure/snapshot.json`            | One wedged process listed; `pt kill --dry-run` invocation captured.     |

## Adapter notes (Hoopoe Go side)

- Lives at `apps/daemon/internal/adapters/pt/` (Phase 10, bead — TBD; integrated with `watch-safety-thresholds`).
- Argv builder enforces `--reason` and a target safety check (PID + cmdline substring).
- Approval policy: `requires_confirmation` for any `pt.kill` outside an explicit `tend-swarm` ActionPlan; in autopilot mode + low-risk, audit-only.
