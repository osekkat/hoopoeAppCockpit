# Agent Mail (MCP) integration contract

> Async coordination layer for the swarm: identities, inbox/outbox, threads, file reservations. Hoopoe surfaces this in the **Activity panel** (`plan.md` §7.5) and uses it for tending-job coordination (§8).

## Source of truth

| Field    | Value                                                              |
| -------- | ------------------------------------------------------------------ |
| Tool     | `mcp_agent_mail` (MCP server) + `agent_mail`/`agent-mail`/`am` CLI |
| Repo     | <https://github.com/Dicklesworthstone/mcp_agent_mail>              |
| Observed | CLI present locally but `--version` prints help banner; MCP server confirmed (`mcp__mcp-agent-mail__health_check` returned `{"status":"ok","environment":"development","http_host":"127.0.0.1","http_port":8765}`) |
| Min compatible | TBD — pin once a real research-spike VPS is up           |
| Reference | `AGENTS.md` "MCP Agent Mail" section                              |

## Adapter precedence (per `plan.md` §2.3)

1. **MCP HTTP transport** (preferred) — via the MCP server endpoint. Same surface that agents use in their own sessions.
2. **`agent_mail` CLI** — fallback for cases where MCP not running. Subcommands mirror MCP tool names.
3. **`agent-mail` archive on disk** — read-only fallback for forensics/diagnostics. Per-project archive under `<project>/messages/` and `<project>/agents/`.

## Capability IDs (per `plan.md` §2.8)

| capId                                | Required by                                | Surface                                                        | Notes                                                          |
| ------------------------------------ | ------------------------------------------ | -------------------------------------------------------------- | -------------------------------------------------------------- |
| `agent_mail.project.ensure`          | First-run wizard, project import           | `ensure_project(human_key=<abs-path>)`                         | Idempotent.                                                    |
| `agent_mail.agent.register`          | Agent identity                             | `register_agent(project_key, program, model, name?)`           | Auto-generates name if not given (adjective+noun).              |
| `agent_mail.messages.read`           | Activity panel timeline                    | `fetch_inbox(project_key, agent_name, limit, since_ts)`        | Polled or pushed via MCP subscription                          |
| `agent_mail.messages.send`           | Tending: orchestrator-chat / cross-agent   | `send_message(project_key, sender_name, to, subject, body_md)` | Markdown body. Threading via `thread_id`.                      |
| `agent_mail.messages.reply`          | Thread replies                             | `reply_message(project_key, message_id, sender_name, body_md)` | Inherits thread_id.                                             |
| `agent_mail.messages.ack`            | `ack_required=true` messages               | `acknowledge_message(project_key, agent_name, message_id)`     | Sets read+ack timestamps.                                       |
| `agent_mail.reservations.list`       | File-collision UI                          | resource://reservations or `list_reservations` (extended)      | Adapter probes via extended-tools listing                       |
| `agent_mail.reservations.create`     | Pre-edit lock (per agent)                  | `file_reservation_paths(...)`                                  | Exclusive vs shared; TTL-bounded                                |
| `agent_mail.reservations.release`    | Post-bead release                          | `release_file_reservations(project_key, agent_name, paths?)`   | Idempotent                                                      |
| `agent_mail.reservations.force_release` | Stale-reservation recovery (tending)    | `force_release_file_reservation(...)`                          | Approval-gated; ActionPlan only                                 |
| `agent_mail.precommit.install`       | Pre-commit guard                           | `install_precommit_guard(project_key)`                         | Optional; per-project hook                                      |
| `agent_mail.macros.start_session`    | Agent boot                                 | `macro_start_session(...)`                                     | One-call register + inbox                                       |
| `agent_mail.macros.thread`           | Thread-prep                                | `macro_prepare_thread(...)`                                    | Aligns context                                                  |
| `agent_mail.macros.handshake`        | Contact handshake                          | `macro_contact_handshake(...)`                                  | When projects use `contacts_only` policy                         |

## Command surfaces (observed)

### MCP tool calls (preferred)

| Tool                              | Arguments (key)                                                                | Returns (shape)                                                                  |
| --------------------------------- | ------------------------------------------------------------------------------ | -------------------------------------------------------------------------------- |
| `ensure_project`                  | `human_key`                                                                    | `{id, slug, human_key, created_at}`                                              |
| `register_agent`                  | `project_key, program, model, name?, task_description?`                        | `{id, name, program, model, project_id, ...}`                                    |
| `macro_start_session`             | `human_key, program, model, task_description`                                  | `{project, agent, file_reservations: {granted, conflicts}, inbox: [...]}`        |
| `fetch_inbox`                     | `project_key, agent_name, limit?, since_ts?, urgent_only?, include_bodies?`    | `[{id, subject, from, created_ts, importance, ack_required, body_md?}, ...]`     |
| `send_message`                    | `project_key, sender_name, to[], subject, body_md, thread_id?, importance?, ack_required?` | `{deliveries: [{project, payload: {id, ...}}], count, attachments}`              |
| `reply_message`                   | `project_key, message_id, sender_name, body_md, to?, cc?`                      | Same envelope as `send_message`                                                  |
| `acknowledge_message`             | `project_key, agent_name, message_id`                                          | `{message_id, read_ts, ack_ts}`                                                  |
| `file_reservation_paths`          | `project_key, agent_name, paths[], ttl_seconds, exclusive, reason`             | `{granted: [...], conflicts: [{path, holders: [{agent, ...}]}]}`                 |
| `release_file_reservations`       | `project_key, agent_name, paths?`                                              | `{released: <n>, released_at}`                                                   |
| `mark_message_read`               | `project_key, agent_name, message_id`                                          | `{message_id, read, read_at}`                                                    |
| `health_check`                    | (none)                                                                         | `{status, environment, http_host, http_port, database_url}`                      |
| `list_extended_tools`             | (none)                                                                         | `{total, by_category: {...}, tools: [{name, category, description}, ...]}`       |
| `call_extended_tool`              | `tool_name, arguments`                                                         | Tool-specific result                                                              |

### MCP resources

| URI                                                            | Returns                                                          |
| -------------------------------------------------------------- | ---------------------------------------------------------------- |
| `resource://config/environment`                                | Server env (HTTP host/port, DB URL)                              |
| `resource://projects`                                          | All projects in creation order                                   |
| `resource://agents/<project_slug>`                             | All agents in a project                                          |
| `resource://inbox/<agent_name>?project=<key>&limit=<n>`        | Quick inbox read (no mutation)                                   |
| `resource://thread/<id>?project=<key>&include_bodies=true`     | Quick thread read                                                |
| `resource://tooling/{directory,schemas,metrics,locks}`         | Diagnostic / discovery                                           |

### CLI fallback

`agent_mail --help` is the only baseline-stable command observed (CLI shape may vary by installation; pin once VPS-validated). Prefer MCP.

## Failure modes & recovery

| Symptom                                              | Root cause                                       | Hoopoe response                                                        |
| ---------------------------------------------------- | ------------------------------------------------ | ---------------------------------------------------------------------- |
| `from_agent not registered`                          | `register_agent` not called in this `project_key` | Adapter retries `register_agent` then resends.                         |
| `FILE_RESERVATION_CONFLICT`                          | Peer holds overlapping pattern                    | Surface conflicting holder; offer narrow scope, wait, or escalate.    |
| MCP HTTP `503`                                       | Server restart                                    | Backoff (250ms / 500ms / 1s); retry up to 30s.                         |
| Auth error (JWT/JWKS, when enabled)                  | Bearer token invalid                              | Re-issue from daemon; never log token.                                 |
| Inbox unbounded growth                               | Reader fell behind                                | `fetch_inbox` with `since_ts` cursor; never `limit=∞`.                 |
| Stale reservation past TTL                           | Agent died before release                         | `tend-swarm` job's `force_release` action (`ActionPlan` + approval).   |

## Authentication / credentials

- MCP server local: typically no auth (loopback).
- MCP server JWT/JWKS: when configured, daemon presents a bearer token signed with the project's MCP key (`AGENTS.md` "Common Pitfalls — Auth errors").
- Per-agent identities are mail's source of truth — Hoopoe never invents an `agent_name` outside `register_agent`.

## Known gotchas

See [`../research-spike/gotchas.md`](../research-spike/gotchas.md) (`hp-d54`).

- `agent_mail --version` is not standard — `probe_version` falls back to `--help` and may capture banner glyphs as "version." Adapter must use a discriminating probe.
- Reservation `paths` are **glob patterns**, not literal paths. `packages/fixtures/**` reserves the whole subtree; `packages/fixtures/phase0-*/snapshot.json` is much narrower.
- `auto_contact_if_blocked=true` will auto-issue a contact request when policy blocks — useful for first-time peers, surprising on repeat sends.
- `attachments_policy=auto` will inline small images (WebP-converted). Large attachments referenced as paths.
- Threads are first-class but **subjects** drift; agents should use `thread_id` to correlate, not subject substrings.

## Test fixtures

| Scenario | Fixture path                                                              | Asserts                                                                |
| -------- | ------------------------------------------------------------------------- | ---------------------------------------------------------------------- |
| `fresh`  | `packages/fixtures/phase0-2026-05-02/scenarios/fresh/snapshot.json`       | Empty inbox, no reservations, server `health_check` OK                 |
| `active` | `packages/fixtures/phase0-.../scenarios/active/snapshot.json`             | Multi-thread inbox, 2+ reservations (one with conflict), ack-required  |
| `failure`| `packages/fixtures/phase0-.../scenarios/failure/snapshot.json`            | Stale reservation past TTL, MCP 503 captured, conflict force-release   |

## Adapter notes (Hoopoe Go side)

- Lives at `apps/daemon/internal/adapters/agentmail/` (Phase 9, bead `hp-ay4`).
- MCP transport: HTTP POST `application/json` with JSON-RPC 2.0 envelope. Long-poll or SSE for inbox subscriptions.
- Activity timeline ingestion: every `send_message` event mapped to an Activity entry (`plan.md` §7.5, bead `hp-3se`).
- Force-release flow: requires user approval surfaced through the unified approvals queue (`plan.md` §5.3); never silently force-released by tending alone.
- Pre-commit guard (`install_precommit_guard`) is **off by default** for Hoopoe development; opt-in.
