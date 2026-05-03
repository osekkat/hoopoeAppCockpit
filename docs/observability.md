# Observability — structured logging & redaction

The Hoopoe structured logger is the **only** sanctioned logging path in
production code. Every desktop renderer, desktop main process, and daemon
subsystem emits the same JSON envelope, runs the same redaction patterns
before buffering, and supports the same transport set (file, stderr/journald,
console, in-memory test capture).

`hp-lxs` deliverable. This file is the authoritative reference; if other
documents disagree, this file wins (file an issue).

## On-disk implementation

| Surface | Path |
| --- | --- |
| Daemon redaction primitive (Go) | `apps/daemon/internal/redact/` |
| Daemon logger (Go) | `apps/daemon/internal/logger/{types,redactor,logger,transport}.go` |
| Desktop redaction primitive (TS) | `apps/desktop/src/shared/redact/` |
| Desktop logger (TS) | `apps/desktop/src/shared/logger/` |
| CI lint (raw logging) | `scripts/loggerlint/check-no-raw-logging.{ts,test.ts}` |
| CI lint (redact drift Go ↔ TS) | `scripts/redactlint/check-redact-drift.{ts,test.ts}` |
| Authoritative reference | `docs/observability.md` (this file) |

The redaction primitive (Go `internal/redact` + TS `shared/redact`) is
deliberately separable from the logger so non-logger consumers — the audit
log writer (hp-g73), the WebSocket event fan-out, adapter capture pipes
(br/bv/ntm/agent_mail/oracle CLI stdout/stderr), and the renderer-side
preload boundary — can import it directly without depending on the logger.
The drift-detection lint pins Go and TS pattern IDs to byte-equivalence.

## The canonical envelope

Every log line — daemon, desktop, test — encodes to JSON with these fields:

```json
{
  "ts": "2026-05-04T00:00:00.000Z",
  "level": "info",
  "msg": "ping",
  "component": "daemon.api",
  "subsystem": "v1.health",
  "correlationId": "corr-abc",
  "causationId": "cmd-xyz",
  "actor": { "kind": "system", "id": "scheduler" },
  "jobId": "job-42",
  "beadId": "hp-r33",
  "swarmId": "sw-1",
  "planId": "pl-2",
  "runId": "run-3",
  "fields": { "path": "/v1/health", "status": 200 }
}
```

### Required fields

- `ts` — RFC3339 timestamp, UTC, with sub-second precision (millisecond on
  desktop, nanosecond on daemon).
- `level` — one of `trace | debug | info | warn | error | fatal`.
- `msg` — short human-readable message; redacted before emission.
- `component` — coarse origin tag (`daemon.api`, `desktop.main`,
  `desktop.renderer`, `test.<phase>`, etc.). Used by audit-log filters and
  Diagnostics tabs.

### Optional envelope columns (omitted when empty)

| Field | When set |
| --- | --- |
| `subsystem` | Narrower bucket within a component (e.g., `v1.health`). |
| `correlationId` | Set by request-scoped context (HTTP/WS handler, tending job). |
| `causationId` | Command/event/job that triggered this log. |
| `actor` | Who/what produced the entry: `{kind, id}`. |
| `jobId`, `beadId`, `swarmId`, `planId`, `runId` | Domain-scoped identifiers. |

### Free-form fields

`fields` is the catch-all for structured key/value data not covered by the
envelope columns. It runs through redaction recursively, including arrays
and nested maps. Don't put secrets in here — defense-in-depth, not a
substitute for proper handling.

### Levels

| Level | When to use |
| --- | --- |
| `trace` | Per-operation chatter (each redaction firing, parser steps). |
| `debug` | Developer-mode detail; off in production by default. |
| `info` | Normal operational events. |
| `warn` | Recoverable problems; user/operator should know. |
| `error` | Operation failed; specific request/job didn't complete. |
| `fatal` | Daemon/process-wide failure. **Does not call `os.Exit` from inside the logger** — exit policy is the caller's. |

`Level.Rank()` (Go) / `levelRank(level)` (TS) gives a total order for
filtering.

## Scoping with `with(...)` / `With(...)`

Loggers compose via scope inheritance:

**Go:**

```go
root := logger.New(logger.Config{Component: logger.ComponentDaemonAPI, ...})
reqLogger := root.With(
    logger.Field{"correlationId", reqID},
    logger.Field{"jobId", jobID},
)
reqLogger.Info("starting", logger.Field{"path", req.URL.Path})
```

**TS:**

```ts
const root = createLogger({ component: Component.DesktopMain, ... });
const scoped = root.with({ correlationId: reqId, jobId: jobId });
scoped.info("starting", { path: req.url });
```

Field names matching the envelope (component / subsystem / correlationId /
causationId / actor / jobId / beadId / swarmId / planId / runId) populate
the dedicated columns. Everything else lands in `fields`. Scope-via-call
rules:

- `root.with(...)` creates a NEW logger; the parent is unaffected.
- Per-call fields (`info("msg", { foo: "bar" })`) merge into `entry.fields`.
- Per-call fields targeted at envelope keys are **dropped silently** —
  envelope columns can only be set via `with(...)`. (Deliberate: it
  prevents accidental cross-request mixing.)

## Redaction (runs BEFORE entries are buffered)

Every `msg` and every value inside `fields` (recursively) passes through
the redactor. Transports receive already-scrubbed entries.

Pattern table — pattern IDs are stable contracts; downstream tools (audit
replay, redaction-event auditing) reference them by name.

| Pattern ID | Trigger | Replacement |
| --- | --- | --- |
| `private-key-block` | `-----BEGIN ... PRIVATE KEY-----` blocks (RSA/OpenSSH/EC/DSA/PGP, with optional `BLOCK` suffix) | `[private-key-redacted]` |
| `bearer-hmac` | JWT-shaped `eyJ...` tokens | `sha256:<8 hex>` (hash tag for correlation) |
| `provider-key-anthropic` | `sk-ant-...` (matches before `sk-`) | `sk-ant-[redacted-sha256:<8>]` |
| `provider-key-openai` | `sk-...` (24+ chars) | `sk-[redacted-sha256:<8>]` |
| `provider-key-google` | `AIza...` (39 chars) | `AIza-[redacted-sha256:<8>]` |
| `provider-key-aws` | `AKIA...` (20 chars, uppercase) | `AKIA-[redacted-sha256:<8>]` |
| `provider-key-github` | `ghp_...` (40+ chars) | `ghp-[redacted-sha256:<8>]` |
| `provider-key-slack` | `xox[baprs]-...` | `xox-[redacted-sha256:<8>]` |
| `pairing-token` | Hoopoe 12-char Crockford pairing tokens (`H-<11>`) | `[pairing-token-redacted]` |
| `http-authorization-header` | HTTP `Authorization: ...` line (case-insensitive) | `Authorization: [redacted-header]` |
| `http-cookie-header` | HTTP `Set-Cookie: ...` line | `Set-Cookie: [redacted-header]` |
| `http-cookie-header-request` | HTTP `Cookie: ...` line | `Cookie: [redacted-header]` |
| `ssh-passphrase` | `passphrase=...` / `passphrase: ...` (case-insensitive) | `passphrase=[redacted]` |
| `chatgpt-cookie` | `__Secure-next-auth.session-token=...` | `__Secure-next-auth.session-token=[redacted]` |
| `claude-ai-cookie` | `sessionKey=sk-ant-...` (claude.ai web session) | `sessionKey=[redacted]` |
| `openai-com-cookie` | `__Host-next-auth.csrf-token=...` (openai.com web session) | `__Host-next-auth.csrf-token=[redacted]` |
| `telegram-bot-token` | `<8-10 digits>:<30+ chars>` | `[telegram-bot-token-redacted]` |
| `email-address` | RFC 5321 email | `***<last4>@<domain>` |
| `ssh-key-path` | `~/.ssh/...` and `.ssh/...` paths | `[ssh-key-path-redacted]` |
| `shadow-file-path` | `/etc/shadow` | `[shadow-path-redacted]` |
| `macos-keychain-path` | `/private/var/db/...` | `[keychain-path-redacted]` |
| `oracle-profile-path` | `~/.config/oracle/profiles/...` | `[oracle-profile-path-redacted]` |

### Redaction is auditable

`Redact()` returns the redacted text plus a slice of `Event` records:
`{pattern, count, bytesRedacted}`. Callers can emit a trace-level entry
recording that redaction happened (and which sub-pattern matched) without
leaking the secret. Useful for Diagnostics / hp-1wg8 audit-log explorer.

The structured trace event shape (mirrored across Go and TS):

```ts
interface RedactionTraceEvent {
  ts: string;                  // RFC3339
  redactor: string;            // 'audit' | 'events' | 'logger' | 'adapter:<name>'
  patternId: string;           // e.g., 'bearer-hmac'
  context: string;             // dotted field path, e.g., 'audit.command_preview'
  bytesRedacted: number;
}
```

Trace events are themselves redacted (no leaked secret content) — the
event carries metadata, not payload.

The `Stats` accumulator (`internal/redact.Stats` / `RedactionStats`) keeps
running totals per pattern. Diagnostics renders the breakdown so operators
can verify redaction is firing in production.

### Order matters

Specific patterns run before broader ones — `provider-key-anthropic`
(`sk-ant-...`) runs before `provider-key-openai` (`sk-...`) so the longer
prefix wins. Don't reorder without a corresponding test update.

### Daemon vs. desktop hash parity

The Go daemon uses `crypto/sha256` for hash tags. The desktop runs in a
context where synchronous SHA-256 isn't ergonomic (browser SubtleCrypto is
async); the renderer uses an FNV-1a fold and tags it `sha256:<hex>` for
envelope-shape parity. The hashes will not match across desktop/daemon
for the same secret — this is a known asymmetry, acceptable because
secrets never need to be correlated *across* surfaces (secrets in renderer
logs are different incidents from secrets in daemon logs by definition).

If cross-surface correlation becomes load-bearing, swap the renderer to
`crypto.subtle` + an async redaction pipe.

## Transports

| Transport | Where | Behavior |
| --- | --- | --- |
| **CaptureTransport** | Tests + Diagnostics | In-memory ring (default 200 entries). Dumps `JSONLines()` on test failure. |
| **WriterTransport / StreamTransport** | Daemon stderr (journald), desktop main stdout | One JSON line per entry. |
| **FileTransport (Go)** | Daemon production | Daily-rotated file at `<dir>/<component>-<YYYY-MM-DD>.log` (UTC). |
| **RendererConsoleTransport (TS)** | Desktop renderer | Routes to `console.{debug, info, warn, error}` so DevTools shows colored levels. |
| **NullTransport** | No-op fallback | Drops every entry. Used by `NOOP_LOGGER`. |

Production wiring (forward-looking):

- **Daemon**: `WriterTransport(stderr)` + `FileTransport("~/.hoopoe/logs", "daemon.<subsystem>")`.
- **Desktop main**: `WriterTransport(stdout)` + `FileTransport("~/Library/Application Support/Hoopoe/logs", "desktop.main")` (TS file transport pending — currently the desktop ships only the renderer/stream variants; main-process file rotation lands when the desktop wiring task does).
- **Desktop renderer**: `RendererConsoleTransport()` only — file emission is the main-process job.
- **Tests**: `CaptureTransport` with `JSONLines()` dumped on failure. The existing test-utils logger (`apps/desktop/src/test-utils/structured-logger.ts`) is a higher-level test reporter; new tests should use the canonical logger.

## CI lint: no raw logging

`scripts/loggerlint/check-no-raw-logging.ts` walks `apps/desktop/src` and
`apps/daemon`, flagging:

- TS: `console.log`, `console.info`, `console.warn`, `console.error`,
  `console.debug`.
- Go: `fmt.Println`, `fmt.Printf`, `fmt.Print`, `log.Println`, `log.Printf`,
  `log.Print`, `log.Fatalf`, `log.Fatal`, `log.Panicln`, `log.Panicf`.

Allowlist:

- `vendored/` subtree (upstream code).
- `*.test.ts`, `_test.go`.
- `apps/daemon/cmd/{hoopoe,hoopoed,hoopoed-mock}/main.go` — fatal-startup
  paths before the logger is wired.
- `apps/desktop/src/shared/logger/transport.ts` — `RendererConsoleTransport`
  is supposed to call `console.*`.

Wired into `bun run lint` and `bun run test` at the repo root.

## How to add a new redaction pattern

1. Edit `apps/daemon/internal/logger/redactor.go` and
   `apps/desktop/src/shared/logger/redactor.ts` together. Same id, same
   regex, same replacement strategy.
2. Add a test for the new pattern in BOTH `redactor_test.go` and
   `redactor.test.ts`. Include at least one positive case and one
   negative case (similar text that must NOT be redacted).
3. Update the table in this file.
4. If the pattern targets an external system (e.g., a new provider's API
   key shape), bump the envelope schema version is **not** required —
   pattern additions are forward-compatible. Renames/removals require a
   schema bump per `plan.md` §10.3.

## Cross-references

- `plan.md` §1.4 — every meaningful action is inspectable; logs are half
  of that surface.
- `plan.md` §5.4 — daemon redaction layer (logs + audit + streamed events).
- `plan.md` §10 — audit log shape; uses the same envelope.
- `plan.md` §10.3 — retention/compaction defaults.
- `hp-g73` — audit log infrastructure (consumes this envelope).
- `hp-je1p` — daemon redaction layer (depends on this bead).
- `hp-1wg8` — Diagnostics audit-log explorer UI.
- `hp-r33` — capability registry (`Capability.notes` flows through the
  redactor before being surfaced in Diagnostics).
