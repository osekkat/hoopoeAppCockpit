# `oracle` integration contract

> The harness Hoopoe uses to reach **ChatGPT Pro web** from the planning pipeline (`plan.md` §7.1). Browser-mode automation drives a logged-in `chatgpt.com` session — the only way to use a ChatGPT Pro subscription (no API equivalent, no key). Used **only** for ChatGPT Pro; Claude / Codex / Gemini reach their subscriptions through native CLIs.

## Source of truth

| Field    | Value                                                       |
| -------- | ----------------------------------------------------------- |
| Tool     | `oracle`                                                    |
| Repo     | <https://github.com/steipete/oracle> (MIT)                  |
| Observed | Not on PATH on dev box (research-spike 2026-05-02; macOS-only host typically) |
| Min compatible | TBD (pin on macOS user host)                          |

## Adapter precedence (per `plan.md` §2.3, §7.1)

1. **MVP topology:** `oracle serve` runs on the **user's Mac** (Chrome already signed in to ChatGPT Pro); the VPS daemon calls it via `--remote-host` over the SSH tunnel (reverse direction).
2. **Direct CLI invocation** (when running on the same host as Chrome): `oracle --engine browser --model <m> --prompt <text> --file <path> --write-output <path>`.
3. **VPS-resident Oracle** (post-MVP option) — would require Chrome + ChatGPT Pro session on the VPS, less convenient.
4. **No reimplementation** — Hoopoe shells out; never touches the browser machinery directly.

## Capability IDs (per `plan.md` §2.8)

| capId                       | Required by                              | Surface                                              | Notes                                              |
| --------------------------- | ---------------------------------------- | ---------------------------------------------------- | -------------------------------------------------- |
| `oracle.serve.status`       | Adapter probe + Diagnostics              | `oracle serve status` (or HTTP /health)              | Pin on VPS                                         |
| `oracle.browser.run`        | Planning pipeline (Phase 5)              | `oracle --engine browser --model <m> --prompt <p>`   | macOS user host only in MVP                        |
| `oracle.remote.invoke`      | VPS → Mac reverse-RPC                    | `oracle --remote-host <addr> --remote-token <tok>`   | Reverse-tunnel topology                             |
| `oracle.help`               | Adapter probe                             | `oracle --help`                                      |                                                     |

## Command surfaces (planned — pin on macOS host)

| Label             | argv                                                                 | Exit | Notes                                                          |
| ----------------- | -------------------------------------------------------------------- | ---- | -------------------------------------------------------------- |
| `help`            | `oracle --help`                                                      | 0    | Adapter probe.                                                  |
| `serve_help`      | `oracle serve --help`                                                | 0    | Captured for adapter contract.                                  |
| `serve_status`    | `oracle serve status`                                                | 0    | `{healthy, model, last_request_ts}`                             |
| `remote_help`     | `oracle --remote-host --help`                                        | 0    | Captured for adapter contract.                                  |
| `browser_run`     | `oracle --engine browser --model gpt-5.4-pro --prompt … --file … --write-output …` | 0/1 | Mutating-ish (sends LLM request). Not run by snapshot.sh. |

## Output shape

`--write-output <path>` writes the model response to a file. Adapter reads from that path; never inlines the bytes through stdout (large responses).

```jsonc
{
  "request": { "model": "gpt-5.4-pro", "prompt_hash": "<sha>", "files": [...] },
  "response": { "text": "...", "tokens_in": 1024, "tokens_out": 4096, "duration_ms": 18420 },
  "session": { "browser_session_id": "...", "page_url": "..." }
}
```

Schema TBD; pin on first VPS+Mac integration test.

## Failure modes & recovery

| Symptom                                              | Root cause                                       | Hoopoe response                                                        |
| ---------------------------------------------------- | ------------------------------------------------ | ---------------------------------------------------------------------- |
| `oracle serve` not running                           | User closed it / Mac restarted                    | Adapter detects via probe; surface "Oracle offline — start `oracle serve` on your Mac." |
| Chrome session lost cookies                           | User logged out of ChatGPT                        | `oracle` returns auth error; surface deep-link to chat.openai.com.     |
| `oracle --remote-host` connection refused            | SSH reverse tunnel down                           | Daemon retries SSH tunnel; surface in Activity panel.                  |
| Long Pro run times out                                | ChatGPT Pro browser session quirks                 | `oracle` auto-reattaches per its own logic; adapter just forwards exit. |
| `--write-output` file missing on success              | Race or path issue                                 | Treat as failure; user sees "no artifact written" diagnostic.          |

## Authentication / credentials

- ChatGPT Pro authentication lives in the user's Chrome cookie jar — Hoopoe **never** touches.
- `oracle --remote-token` is a per-session token issued by `oracle serve`; daemon stores in keychain-equivalent.
- No CAAM, no provider key.

## Known gotchas

See [`../research-spike/gotchas.md`](../research-spike/gotchas.md) (`hp-d54`).

- Browser-mode requires a real Chrome with a real signed-in profile. CI cannot run `oracle.browser.run`; CI must mock.
- `oracle` may auto-reattach when ChatGPT Pro disconnects mid-response; adapter must wait for `oracle`'s exit code, not impose its own timeout below ~10 min.
- `--write-output` is the contract; **never** parse stdout for the response. Adapter reads the file.
- ChatGPT Pro web has different rate limits than the API (which doesn't exist for Pro). Surface "subscription quota" via `caut` instead of inferring from Oracle 429s.
- macOS-only in MVP — VPS Phase 5 daemon calls Mac Oracle via `--remote-host`. Linux desktop port is post-MVP.

## Test fixtures (placeholder — pin on macOS host)

| Scenario | Fixture path                                                              | Asserts                                                                 |
| -------- | ------------------------------------------------------------------------- | ----------------------------------------------------------------------- |
| `fresh`  | `packages/fixtures/phase0-2026-05-02/scenarios/fresh/snapshot.json`       | `oracle serve` running on Mac; status healthy.                          |
| `active` | `packages/fixtures/phase0-.../scenarios/active/snapshot.json`             | Mid-pipeline planning request in flight.                                |
| `failure`| `packages/fixtures/phase0-.../scenarios/failure/snapshot.json`            | Chrome cookies invalid; auth error captured.                            |

## Adapter notes (Hoopoe Go side)

- Lives at `apps/daemon/internal/adapters/oracle/` (Phase 5, bead `hp-xr8`).
- Reverse-tunnel topology: VPS daemon initiates SSH tunnel to the Mac to expose Oracle's local port to the daemon. The daemon then HTTP-POSTs requests.
- Per-request artifact: write to `~/.hoopoe/work/<project>/planning/<request-id>/output.txt`; daemon ingests for plan.md cost ledger + artifact rail.
- Provider lockdown (Guardrail 11): grep for `openai|anthropic|google` SDK imports in this adapter must return empty.
