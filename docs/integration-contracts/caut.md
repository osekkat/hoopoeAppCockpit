# `caut` (coding agent usage tracker) integration contract

> Per-provider **subscription-quota** usage. Backs the top-bar subscription-usage pill (`plan.md` §7.6) and the `watch-safety-thresholds` budget checks (`§8.4`). Subscription quota — **not** API-token quota (Hoopoe is subscription-only; Guardrail 11).

## Source of truth

| Field    | Value                                                |
| -------- | ---------------------------------------------------- |
| Tool     | `caut`                                               |
| Repo     | TBD (canonical: <https://github.com/Dicklesworthstone/...>; pin on real VPS) |
| Observed | Not on PATH on dev box (research-spike 2026-05-02 self-test) — will pin once `hp-r7i` lands |
| Min compatible | TBD                                            |

## Adapter precedence (per `plan.md` §2.3)

1. **`caut usage --json`** — primary read; per-provider quota window (used / remaining / reset_at).
2. **`caut status --json`** — quick read for the top-bar pill (no per-provider breakdown if `usage` too heavy).
3. **No direct provider API** for quota (Guardrail 11) — `caut` is the only path.

## Capability IDs (per `plan.md` §2.8)

| capId                       | Required by                              | Surface                                | Notes                                                        |
| --------------------------- | ---------------------------------------- | -------------------------------------- | ------------------------------------------------------------ |
| `caut.usage.snapshot`       | Top-bar pill, `watch-safety-thresholds`  | `caut usage --json`                    | Cadence: every 60 s in active swarm; every 5 min when idle.  |
| `caut.status.read`          | Adapter probe + UI badge                 | `caut status --json`                   | Lighter than `usage`.                                         |

## Command surfaces (planned, pending VPS-pin)

| Label          | argv (anticipated)                  | Exit | Notes                                                              |
| -------------- | ----------------------------------- | ---- | ------------------------------------------------------------------ |
| `help`         | `caut --help`                       | 0    | Adapter probe.                                                     |
| `usage_json`   | `caut usage --json`                 | 0    | `{providers: {claude: {used, remaining, reset_at, ...}, gpt: ...}}` |
| `status`       | `caut status --json`                | 0    | `{healthy, warnings: [...], top_pressure: {provider, percent}}`    |

These shapes are **placeholders** — pin against the real `caut` once the research-spike VPS is up (`hp-r7i`) and update this contract.

## Failure modes & recovery

| Symptom                                              | Root cause                                       | Hoopoe response                                                        |
| ---------------------------------------------------- | ------------------------------------------------ | ---------------------------------------------------------------------- |
| `caut` not on PATH                                   | ACFS install incomplete                          | Adapter reports `caut.usage.snapshot` missing; UI hides the pill, surfaces in Diagnostics. |
| Quota at 100 %                                       | Subscription window exhausted                    | `tend-swarm` triggers `caam.switch_account` (with approval) or pauses.  |
| Quota update lags real usage                          | `caut` polling cadence vs daemon polling cadence | Adapter trusts `caut`'s `reset_at`; never derives quota from log scraping.|
| Provider API responds with rate-limit despite `caut` showing room | Provider-side burst limits not modeled by `caut` | Combine signals: `caut` + provider 429s — `tend-swarm` reads both.    |

## Authentication / credentials

- `caut` inherits CAAM identity. No separate credentials in Hoopoe.

## Known gotchas (placeholders, pending VPS validation)

See [`../research-spike/gotchas.md`](../research-spike/gotchas.md) (`hp-d54`).

- Subscription windows are provider-specific (Claude Pro: 5 hr; ChatGPT Pro: 24 hr; Gemini Ultra: TBD). Adapter must respect `reset_at` not infer.
- "Subscription-only" position is structurally enforced by `caut` (no API tokens) — don't use raw provider responses to back-fill quota.
- Some providers don't expose remaining-quota via OAuth scope; `caut` may estimate. Adapter must surface `estimated: true` in those cases.

## Test fixtures (placeholders, pending VPS pin)

| Scenario | Fixture path                                                              | Asserts                                                                 |
| -------- | ------------------------------------------------------------------------- | ----------------------------------------------------------------------- |
| `fresh`  | `packages/fixtures/phase0-2026-05-02/scenarios/fresh/snapshot.json`       | All providers < 50 % usage; healthy                                     |
| `active` | `packages/fixtures/phase0-.../scenarios/active/snapshot.json`             | One provider 70-90 % (warning band); others normal                      |
| `failure`| `packages/fixtures/phase0-.../scenarios/failure/snapshot.json`            | One provider at 100 % (exhausted); `tend-swarm` triggers switch-account |

## Adapter notes (Hoopoe Go side)

- Lives at `apps/daemon/internal/adapters/caut/` (Phase 8, bead `hp-ier`).
- Polled by the daemon's `caut.usage.snapshot` job (cadence above); event-pushed to renderer via WS channel `caut_usage_changed`.
- The top-bar pill subscribes to that channel via TanStack Query (read-only).
- `watch-safety-thresholds` (`hp-fb0`) reads `caut` snapshots to decide on switch / pause / halt actions; never makes the decision in the renderer.
