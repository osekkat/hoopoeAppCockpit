# NTM integration contract

> Multi-pane tmux orchestration for the agent swarm. NTM owns Stage 03 (Swarm) — `plan.md` §7.3. The desktop renders **bead state + agent state + Activity panel**; raw terminal scrollback is hidden behind a Diagnostics-only "Show raw pane" toggle (Guardrail 12).

## Source of truth

| Field    | Value                                                |
| -------- | ---------------------------------------------------- |
| Tool     | `ntm`                                                |
| Repo     | <https://github.com/Dicklesworthstone/ntm>           |
| Observed | `ntm version 1.7.0` (research-spike 2026-05-02)      |
| Min compatible | 1.5+ (`--robot-snapshot/status/tail` assumed)  |
| Skills   | `ntm`, `vibing-with-ntm` (loaded into tending agents at runtime — `plan.md` §17, §1.8) |

## Adapter precedence (per `plan.md` §2.3)

1. **`ntm serve` REST/SSE/WS** — preferred when running. Persistent connection; sequence-cursored events.
2. **`ntm --robot-snapshot` / `--robot-status` / `--robot-tail`** — fallback when `ntm serve` not available; snapshot-style.
3. **`tmux capture-pane`** — last-resort PTY scraper. Adapter must mark output as `fallback-mode` (per `plan.md` §18.3).
4. **No bare `ntm` interactive subcommands** in automation; some `ntm` subcommands are interactive — check `--help` before adding.

## Capability IDs (per `plan.md` §2.8)

| capId                       | Required by                              | Surface                                  | Notes                                                       |
| --------------------------- | ---------------------------------------- | ---------------------------------------- | ----------------------------------------------------------- |
| `ntm.sessions.list`         | Swarm dashboard project picker           | `ntm sessions list --json`               | Returns `{sessions: null, count: 0}` when no swarms running |
| `ntm.serve.rest`            | Live event stream                        | HTTP REST against `ntm serve`            | `untested` until probe; transport: `http`                   |
| `ntm.serve.sse`             | Live pane stream                         | SSE channel from `ntm serve`             | Preferred for `panes.stream`                                |
| `ntm.serve.ws`              | Live pane stream (alt)                   | WebSocket channel from `ntm serve`       | Use when SSE blocked by intermediate proxies                |
| `ntm.robot.snapshot`        | Swarm dashboard cold-start               | `ntm --robot-snapshot`                   | One-shot full state                                          |
| `ntm.robot.status`          | Swarm dashboard refresh                  | `ntm --robot-status`                     | Lighter than snapshot; just statuses                        |
| `ntm.robot.tail`            | "Show raw pane" Diagnostics toggle       | `ntm --robot-tail --max-bytes <n>`       | Bound by `--max-bytes`; tag `high-volume`                   |
| `ntm.swarm.halt`            | Emergency stop                           | (mutating; via daemon `ActionPlan`)      | `blocked-by-policy` outside ActionPlan                       |
| `ntm.spawn`                 | Stage 03 launch                          | (mutating; via daemon job)               | Launches via `ntm spawn` or REST equivalent                 |
| `ntm.send_marching_orders`  | Tending: `agent.send_marching_orders`    | (mutating; via daemon `ActionPlan`)      | Strict typed surface; never free-form shell                 |
| `ntm.pane.kill`             | Wedged-pane recovery                     | (mutating; ActionPlan)                   | Approval required for `tend-swarm`                          |
| `ntm.approvals.list`        | Approvals queue                          | `ntm --robot-approvals` (TBD per probe)  | Used by `plan.md` §5.3                                      |

## Command surfaces (observed)

| Label             | argv                                            | Exit | Notes                                                                          |
| ----------------- | ----------------------------------------------- | ---- | ------------------------------------------------------------------------------ |
| `help`            | `ntm --help`                                    | 0    | Banner + subcommand list. Used as fallback probe when `--robot-help` missing.  |
| `robot_help`      | `ntm --robot-help`                              | 0/2  | May not exist on older versions; mark `robot.help` capability as `untested`.   |
| `sessions_list`   | `ntm sessions list --json`                      | 0    | Empty: `{sessions: null, count: 0}`. Healthy.                                  |
| `robot_snapshot`  | `ntm --robot-snapshot`                          | 0    | Full state object: sessions → panes → agents → state.                          |
| `robot_status`    | `ntm --robot-status`                            | 0    | Lighter; per-pane status only.                                                  |
| `robot_tail`      | `ntm --robot-tail --max-bytes 8192`             | 0    | Tail output up to byte cap. Use byte ranges, not line counts.                  |

### `ntm serve` (live mode)

When `ntm serve` is running on the VPS:

- Discoverable via `ntm serve status` (or systemd unit). Default bind: `127.0.0.1:<port>`; port chosen at startup (`§2.4` default-deny posture).
- Hoopoe daemon connects locally (same VPS); no public binding.
- REST: `GET /v1/sessions`, `GET /v1/panes/<id>/state`.
- SSE: `GET /v1/panes/<id>/stream` — emits `{seq, ts, kind, payload}`.
- WS: `ws://127.0.0.1:<port>/v1/events` — same envelope; sequence cursor on every event.
- **Hoopoe wraps every NTM event with its own `seq` cursor on top of NTM's** so reconnect-replay is per-Hoopoe-channel, not per-NTM-channel.

## Failure modes & recovery

| Symptom                                              | Root cause                                       | Hoopoe response                                                        |
| ---------------------------------------------------- | ------------------------------------------------ | ---------------------------------------------------------------------- |
| `--robot-snapshot` succeeds but pane state stale     | NTM serve died; tmux still alive                  | Detect via `--robot-status` mismatch; restart `ntm serve` via systemd. |
| `tmux: server not found`                             | tmux killed                                       | Adapter reports `degraded`; user prompted to restart NTM.              |
| Pane "no longer exists" notification                 | Agent crashed or pane killed externally           | Catch via `agent.crashed` hook (see `.ntm/human_inbox/`).               |
| `ntm spawn` succeeds but agent never registers       | Agent crashed before mail registration            | Wait 30s then mark agent as `dead`; recovery via `casr` (Phase 8.4).   |
| `ntm --robot-tail` returns truncated                 | Output > byte cap                                 | Adjust cap or follow byte-offset (§2.7 sequence cursor).               |
| `ntm serve` not on listening port                    | Service down or wrong port                        | Fall back to `--robot-*`; surface "live mode unavailable" diagnostic.  |

## Authentication / credentials

- `ntm` itself: none.
- `ntm serve` REST/WS: bearer token issued at serve startup (capture via systemd unit env). Hoopoe daemon stores in keychain-equivalent.
- Agent CLIs spawned by NTM inherit credentials from CAAM (`plan.md` §7.3, §8.4).

## Known gotchas

See [`../research-spike/gotchas.md`](../research-spike/gotchas.md) (`hp-d54`).

- `ntm sessions list --json` empty payload is `{sessions: null, count: 0}`, not `{sessions: [], count: 0}` — adapter must handle both.
- `ntm --robot-tail` returns **bytes**, not lines. The byte offset semantics matter for replay.
- Pane crash notifications land in `.ntm/human_inbox/<date>_<time>_agent_crashed.md` — `agent_mail` is also notified (preferred path).
- Some `ntm` subcommands are interactive (`ntm spawn` without flags prompts). Always use `--robot-*` or full-flag forms.
- `vibing-with-ntm` skill is the source of behavioral truth (`plan.md` §17 closing); when this contract and the skill disagree, the skill wins.

## Test fixtures

| Scenario | Fixture path                                                              | Asserts                                                                |
| -------- | ------------------------------------------------------------------------- | ---------------------------------------------------------------------- |
| `fresh`  | `packages/fixtures/phase0-2026-05-02/scenarios/fresh/snapshot.json`       | `sessions: null`, no panes, `ntm serve` not running                    |
| `active` | `packages/fixtures/phase0-.../scenarios/active/snapshot.json`             | 2+ sessions, 5+ panes, mix of `IDLE/TYPING/THINKING/TOOL_USE` states   |
| `failure`| `packages/fixtures/phase0-.../scenarios/failure/snapshot.json`            | One pane in `wedged` (SIGSTOP'd in fixture build), `agent.crashed` event |

Adapter contract tests must assert that `ntm.serve.sse` is `untested` until a live `ntm serve` is running — capability is not just "binary on PATH."

## Adapter notes (Hoopoe Go side)

- Lives at `apps/daemon/internal/adapters/ntm/` (Phase 8, bead `hp-g1j`).
- Live-mode: long-lived SSE/WS goroutine per pane; bounded channels; reconnect on tunnel drop with sequence-cursor replay.
- Snapshot-mode: per-tick poll (configurable cadence; default 2 s under active swarm, 10 s when idle).
- All mutating operations go through `ActionPlan` (`plan.md` §8.3.1). Never expose raw `ntm` exec from the renderer.
- PTY data: stored in `~/.hoopoe/work/<project>/swarm/<run-id>/panes/<agent>.bin` (byte-addressable; `plan.md` §7.3).
- Audit: every spawn / kill / send-marching-orders writes to the audit log (`plan.md` §10) regardless of activity-panel suppression.
