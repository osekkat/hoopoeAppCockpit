# CAAM (Cross-Agent Account Manager) integration contract

> **Sole credential pathway** for LLM provider accounts (`plan.md` §1.1, §7.3, §8.4). Hoopoe's `tend-swarm` job and the rate-limit recovery action call CAAM to switch the active account; Hoopoe **never** holds provider keys directly (Guardrail 11).

## Source of truth

| Field    | Value                                                                                          |
| -------- | ---------------------------------------------------------------------------------------------- |
| Tool     | `caam`                                                                                         |
| Repo     | <https://github.com/Dicklesworthstone/caam> (canonical; verify on real VPS)                    |
| Observed | `caam 0.1.11 (7c604c4) built on 2026-04-25T01:00:46Z with go1.26.2` (research-spike 2026-05-02) |
| Min compatible | 0.1.10+ (`account-list --json`, `account-status --json`)                                  |

## Adapter precedence (per `plan.md` §2.3)

1. **`caam account-list --json`** — read the configured account inventory.
2. **`caam account-status --json`** — get the currently-active account.
3. **`caam switch-account <id>`** — mutate the active account. **Through `ActionPlan` only**; `blocked-by-policy` outside the typed action surface (`plan.md` §8.3.1).
4. **No direct provider SDK reach** — Guardrail 11.

## Capability IDs (per `plan.md` §2.8)

| capId                       | Required by                              | Surface                                         | Notes                                                        |
| --------------------------- | ---------------------------------------- | ----------------------------------------------- | ------------------------------------------------------------ |
| `caam.accounts.list`        | Settings panel "Accounts" tab            | `caam account-list --json`                      | Returns array of `{id, provider, label, ...}`                |
| `caam.account.status`       | Top-bar provider indicator               | `caam account-status --json`                    | `{active: {provider, id, ...}}`                              |
| `caam.account.switch`       | `tend-swarm` rate-limit recovery         | `caam switch-account <id>`                      | `blocked-by-policy` outside ActionPlan; approval-gated.      |
| `caam.help`                 | Adapter probe                            | `caam --help`                                   | Used at registry probe                                        |

## Command surfaces (observed)

| Label             | argv                                       | Exit | Notes                                                                                |
| ----------------- | ------------------------------------------ | ---- | ------------------------------------------------------------------------------------ |
| `help`            | `caam --help`                              | 0    | Banner + subcommand list. Stable across versions.                                    |
| `accounts_list`   | `caam account-list --json`                 | 0    | Array of accounts. Per-provider provider IDs.                                        |
| `account_status`  | `caam account-status --json`               | 0    | `{active, last_switch, ...}`                                                         |
| `switch_help`     | `caam switch-account --help`               | 0    | Captured for adapter contract; switch is not invoked by snapshot.sh.                 |

## Failure modes & recovery

| Symptom                                                | Root cause                                            | Hoopoe response                                                          |
| ------------------------------------------------------ | ----------------------------------------------------- | ------------------------------------------------------------------------ |
| `account-list --json` returns empty                    | No accounts configured                                 | UI: "No CAAM accounts configured." Block swarm launch with diagnostic.   |
| `switch-account` exit non-zero                         | Account ID invalid, network failure, OAuth expired     | Surface error to Activity panel; do not retry without approval.          |
| `account-status` lags after switch                     | CAAM eventual consistency                              | Re-poll after 1 s; trust `switch-account` exit code as authoritative.    |
| OAuth re-prompt expected                               | Subscription token expired                             | Surface to user with deep-link to Settings → Accounts (re-auth flow).    |

## Authentication / credentials

- CAAM owns the credentials. Hoopoe never reads, copies, or logs them.
- The redaction layer (`hp-je1p`) MUST scrub anything that looks like a CAAM credential token even if it leaks to stderr.

## Known gotchas

See [`../research-spike/gotchas.md`](../research-spike/gotchas.md) (`hp-d54`).

- `caam switch-account` is **mutating, side-effectful, and per-machine** — it changes which credential the next agent CLI session uses. Through `ActionPlan` only.
- The active account state is **not** snapshot-stable across machines; if a teammate switches CAAM accounts on a shared VPS, your next CLI invocation will use their account. Surface in Diagnostics.
- Some CAAM versions cache OAuth tokens for 24 h after switch; immediate re-switch may use the cache rather than re-authing.
- The capability `caam.account.switch` MUST report `blocked-by-policy` in fixtures — adapter contract tests fail otherwise.

## Test fixtures

| Scenario | Fixture path                                                              | Asserts                                                                |
| -------- | ------------------------------------------------------------------------- | ---------------------------------------------------------------------- |
| `fresh`  | `packages/fixtures/phase0-2026-05-02/scenarios/fresh/snapshot.json`       | One account configured (the install user's); active = that account    |
| `active` | `packages/fixtures/phase0-.../scenarios/active/snapshot.json`             | Multiple accounts; active set to one of them                            |
| `failure`| `packages/fixtures/phase0-.../scenarios/failure/snapshot.json`            | OAuth expired; switch-account fails with stderr captured               |

## Adapter notes (Hoopoe Go side)

- Lives at `apps/daemon/internal/adapters/caam/` (Phase 8, bead `hp-ier`).
- Switch operations go through the typed `ActionPlan` (`caam.switch_account`); the daemon (not the agent) executes after approval policy + idempotency check.
- Approval policy: in autopilot mode (`plan.md` §5.3 risk class `low`), tending may switch silently with audit; in supervised mode, switch raises an approval request.
- Audit: every switch records `{from, to, reason, actor, ts}` regardless of activity-panel suppression (Guardrail 10).
