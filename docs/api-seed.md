# Daemon API Seed

The OpenAPI source of truth lives in `packages/schemas`. This document is the
human-readable seed contract from `plan.md §2.6` and the implementation notes
for the Phase 2/2.5 daemon scaffold.

## Base Rules

- The daemon binds to `127.0.0.1` by default and is reached through the desktop
  SSH tunnel.
- All errors use RFC 7807-style `problem+json`.
- All write actions are typed RPCs. The normal daemon API never exposes
  arbitrary shell execution.
- Event streams use sequence cursors and snapshot-on-reconnect.
- Job, process, and event channels are bounded. Slow consumers receive lag/gap
  repair signals rather than causing unbounded memory growth.

## Seed Endpoints

| Method | Path | Purpose |
| --- | --- | --- |
| `GET` | `/health` | Liveness/readiness probe. |
| `GET` | `/v1/version` | Daemon version, API version, build commit/date, compatibility floor. |
| `GET` | `/v1/capabilities` | Capability registry snapshot gated by tool ids and capability ids. |
| `GET` | `/v1/compatibility` | Capabilities plus schema/migration compatibility. |
| `GET` | `/v1/jobs` | List Hoopoe-owned jobs from the job registry. |
| `GET` | `/v1/jobs/{id}` | Job detail and current lifecycle state. |
| `GET` | `/v1/jobs/{id}/log` | Chunked, offset-addressed job log retrieval. |
| `POST` | `/v1/jobs/{id}/cancel` | Typed cancellation request with audit. |
| `GET` | `/v1/events/sse` | Server-sent event stream for selected channels. |
| `GET` | `/v1/events/ws` | WebSocket event stream with WS-token. |
| `GET` | `/v1/events/replay` | Replay events since a sequence cursor. |
| `GET` | `/v1/security/bind-safety` | Current bind decision and Diagnostics warning payload. |
| `POST` | `/v1/bootstrap/daemon/upgrade` | Verified daemon upgrade orchestration. |

## Event Envelope

```json
{
  "schemaVersion": 1,
  "sequence": 42,
  "channel": "jobs",
  "type": "job.updated",
  "emittedAt": "2026-05-04T00:00:00Z",
  "payload": {}
}
```

Clients reconnect with the last observed sequence per channel. The daemon first
checks whether replay can fill the gap; if not, it emits a gap signal and the
desktop fetches a fresh snapshot for that channel.

## Error Envelope

```json
{
  "type": "https://hoopoe.dev/problems/capability-missing",
  "title": "Required capability is missing",
  "status": 409,
  "detail": "Agent Mail is unavailable for this project.",
  "instance": "/v1/swarm/launch/req-123",
  "code": "capability.agent_mail.missing",
  "severity": "warning",
  "actionability": "configure"
}
```

`detail`, `instance`, and any extension fields are redacted before audit/log
persistence when they may contain project paths, tokens, or user content.

## Compatibility Notes

- Schema versions are real. Bump them and migrate; do not add compatibility
  shims for unused legacy behavior.
- The desktop consumes generated types once `packages/schemas` codegen is in
  the loop. Hand-written shapes must stay temporary and covered by contract
  tests.
- Capability checks gate routes and actions. A parser success is not enough to
  mark a capability available.

## Cross-References

- `plan.md §2.6` — seed REST/WS contract.
- `plan.md §10.1` — backpressure, replay, lag/gap rules.
- `docs/process-manager.md` — job/process model.
- `docs/reconnect-replay.md` — reconnect behavior.
- `docs/security.md` — approvals, audit, bind safety.
