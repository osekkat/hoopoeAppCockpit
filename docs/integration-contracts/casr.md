# `casr` (Cross-Agent Session Resumer) integration contract

> Converts in-flight sessions across CLIs/providers; backs cross-provider recovery (`plan.md` §7.3, §8.4). **Post-MVP** for v1: the `tend-swarm` job's `casr.resume_session` action lands in Phase 10/11; v1 ships without auto-resume.

## Source of truth

| Field    | Value                                                |
| -------- | ---------------------------------------------------- |
| Tool     | `casr`                                               |
| Repo     | TBD (canonical: <https://github.com/Dicklesworthstone/...>; pin on VPS) |
| Observed | Not on PATH on dev box (research-spike 2026-05-02)   |
| Min compatible | TBD                                            |

## Adapter precedence (per `plan.md` §2.3)

1. **`casr session resume <session-id>`** — primary mutating surface; through `ActionPlan` only.
2. **`casr session list --json`** — read-only enumeration of resumable sessions.
3. **`casr status --json`** — adapter probe + health badge.

## Capability IDs (per `plan.md` §2.8)

| capId                       | Required by                              | Surface                                              | Notes                                              |
| --------------------------- | ---------------------------------------- | ---------------------------------------------------- | -------------------------------------------------- |
| `casr.session.list`         | Recovery picker UI                       | `casr session list --json`                           | Read-only                                          |
| `casr.session.resume`       | Tending: `casr.resume_session` action    | `casr session resume <id> --target-cli <cli>`        | `blocked-by-policy` outside ActionPlan; approval  |
| `casr.status.read`          | Adapter probe                            | `casr status --json`                                 |                                                     |

## Command surfaces (placeholder — pin on VPS)

| Label       | argv (anticipated)                          | Exit | Notes                                               |
| ----------- | ------------------------------------------- | ---- | --------------------------------------------------- |
| `help`      | `casr --help`                               | 0    | Probe.                                              |
| `status`    | `casr status --json`                        | 0    | `{healthy, sessions_resumable: <n>}`                |
| `list`      | `casr session list --json`                  | 0    | Array of resumable sessions.                        |
| `resume`    | `casr session resume <id> --target-cli <c>` | 0/4  | 4 on policy block; mutating.                        |

## Failure modes & recovery

| Symptom                                              | Root cause                                       | Hoopoe response                                                        |
| ---------------------------------------------------- | ------------------------------------------------ | ---------------------------------------------------------------------- |
| `casr` not on PATH                                   | ACFS install incomplete                          | Adapter reports `casr.session.resume` missing; UI hides action.         |
| `resume` fails with rate limit                       | Target provider also rate-limited                | Try next provider in CAAM rotation; surface to Activity panel.         |
| Session ID stale                                      | Session GC'd before resume                       | Re-list; mark stale in UI.                                              |

## Authentication / credentials

- Inherits CAAM identity for the target CLI.

## Known gotchas (placeholders)

See [`../research-spike/gotchas.md`](../research-spike/gotchas.md) (`hp-d54`).

- Cross-provider resume may lose context — different CLIs have different context windows + tool semantics. `casr` does its best; surface lossiness in UI.
- The `casr.session.resume` action is **always** approval-gated in v1.x — auto-resume lands later (Phase 11+).

## Test fixtures (placeholder — VPS pin)

| Scenario | Fixture path                                                              | Asserts                                                                 |
| -------- | ------------------------------------------------------------------------- | ----------------------------------------------------------------------- |
| `fresh`  | `packages/fixtures/phase0-2026-05-02/scenarios/fresh/snapshot.json`       | `casr` present, no resumable sessions.                                  |
| `active` | `packages/fixtures/phase0-.../scenarios/active/snapshot.json`             | 1+ resumable session listed.                                            |
| `failure`| `packages/fixtures/phase0-.../scenarios/failure/snapshot.json`            | Resume attempt fails with stale session.                                |

## Adapter notes (Hoopoe Go side)

- Lives at `apps/daemon/internal/adapters/casr/` (Phase 8/11, bead — TBD).
- Action: `casr.resume_session` in the typed action surface (`tending-actions.yaml`).
- Approval policy: always `requires_confirmation` in v1.
