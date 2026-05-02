# DCG (Destructive Command Guard) integration contract

> Claude Code hook that intercepts dangerous commands. **Verdicts are ingested into Hoopoe's unified approvals queue (`plan.md` §5.3); DCG is not run as a parallel guard.** Hoopoe never bypasses DCG; we surface its decisions and require user approval where needed.

## Source of truth

| Field    | Value                                                       |
| -------- | ----------------------------------------------------------- |
| Tool     | `dcg`                                                       |
| Repo     | <https://github.com/Dicklesworthstone/dcg> (verify on VPS)  |
| Observed | `0.5.0` (research-spike 2026-05-02)                         |
| Skill    | `dcg` (loaded by Claude Code automatically when installed)  |

## Adapter precedence (per `plan.md` §2.3)

1. **DCG verdict stream** — primary. Subscribe via the hook's emit channel (file/socket/log line; pin per VPS).
2. **`dcg --help` / `dcg status --json`** — adapter probe.
3. **No reimplementation** — Hoopoe does not duplicate DCG logic in Go (Guardrail). DCG is the canonical destructive-command judge.

## Capability IDs (per `plan.md` §2.8)

| capId                          | Required by                              | Surface                                              | Notes                                                            |
| ------------------------------ | ---------------------------------------- | ---------------------------------------------------- | ---------------------------------------------------------------- |
| `dcg.verdicts.subscribe`       | Approvals queue                          | hook emit channel (file/socket/log)                  | Pin transport on real VPS                                        |
| `dcg.status.read`              | Adapter probe + UI badge                 | `dcg status --json`                                  | Quick health check                                                |
| `dcg.help`                     | Adapter probe                             | `dcg --help`                                         | Stable across versions                                            |

## Verdict shape (planned; pin on VPS)

```json
{
  "verdict_id": "dcg-<ulid>",
  "verdict": "blocked" | "requires_confirmation" | "allowed",
  "command": {
    "argv": ["rm", "-rf", "/"],
    "cwd": "/data/projects/foo",
    "actor": {"agent": "ClaudeCode", "session": "..."}
  },
  "reasoning": "rm -rf with absolute path; root-targeting",
  "policy_class": "destructive-fs",
  "ts": "2026-05-03T10:12:14Z"
}
```

These keys are placeholders until the real VPS provides ground truth. Adapter contract tests assert the keys above plus a `schemaVersion` field.

## Command surfaces (observed)

| Label              | argv                                       | Exit | Notes                                                          |
| ------------------ | ------------------------------------------ | ---- | -------------------------------------------------------------- |
| `help`             | `dcg --help`                               | 0    | Banner + subcommand list. Captured at probe time.              |
| `status`           | `dcg status --json`                        | 0    | Quick health snapshot.                                          |

## Failure modes & recovery

| Symptom                                              | Root cause                                       | Hoopoe response                                                        |
| ---------------------------------------------------- | ------------------------------------------------ | ---------------------------------------------------------------------- |
| Verdict stream stalls                                | DCG hook crash                                    | Adapter detects via heartbeat; surface "DCG offline" warning; **block** swarm-mutating actions. |
| `verdict: blocked` for an action Hoopoe wants to run | DCG policy disallows                              | **Never bypass.** Surface to user; raise approval request via §5.3.    |
| `verdict: requires_confirmation`                     | Sensitive but user-overridable                    | Add to approvals queue; default-deny if user idle > 5 min.             |
| `dcg status` exit non-zero                           | Hook misconfigured                                | Adapter reports `dcg.verdicts.subscribe` missing; UI flags Diagnostics.|

## Authentication / credentials

- None. DCG is a Claude Code hook; runs as the user.

## Known gotchas (preliminary)

See [`../research-spike/gotchas.md`](../research-spike/gotchas.md) (`hp-d54`).

- Verdict transport (file vs socket vs log) varies by DCG version — pin on real VPS.
- DCG runs **inside** Claude Code's hook chain; if Claude Code is not the active CLI (e.g. Codex CLI session), DCG won't fire. Approvals queue must distinguish "no DCG verdict because no Claude Code in chain" from "DCG verdict: allowed."
- Bypassing DCG via `--no-verify` or skipping the hook is **forbidden** in Hoopoe (AGENTS.md "Note for Codex/GPT-5.5"). Adapter contract tests assert no such bypass exists.

## Test fixtures (placeholder; pin on VPS)

| Scenario | Fixture path                                                              | Asserts                                                                 |
| -------- | ------------------------------------------------------------------------- | ----------------------------------------------------------------------- |
| `fresh`  | `packages/fixtures/phase0-2026-05-02/scenarios/fresh/snapshot.json`       | DCG present; status healthy; no verdicts pending.                       |
| `active` | `packages/fixtures/phase0-.../scenarios/active/snapshot.json`             | At least one `requires_confirmation` verdict in the queue.              |
| `failure`| `packages/fixtures/phase0-.../scenarios/failure/snapshot.json`            | One `blocked` verdict; one stalled stream (DCG offline).                |

## Adapter notes (Hoopoe Go side)

- Lives at `apps/daemon/internal/adapters/dcg/` (Phase 8, bead — TBD; folded into approvals work).
- Subscription is a long-lived goroutine; reconnect with sequence-cursor on disconnect.
- Verdicts feed `apps/daemon/internal/approvals/queue.go` (the §5.3 unified queue). DCG verdicts get `source: dcg` stamped for cross-tool deduping.
- The renderer never sees raw DCG output; only the normalized approval entry.
