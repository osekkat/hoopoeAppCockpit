# `daemon-harness` — TS-side daemon-spawn harness

> Bead: `hp-ngq` — Idempotency-key tests for retried writes through
> simulated tunnel drop.

Spawns the Go daemon binary (`apps/daemon/bin/hoopoed`, default `--mock`
mode for deterministic fixture-backed runs), waits for `/v1/health` to
return 200, and yields a `DaemonHandle` so integration tests can drive
HTTP / WS endpoints from TypeScript.

## Why a TS harness

The hp-ngq idempotency assertions are inherently HTTP-level: same key
twice → same response, mid-request abort + retry with same key →
exactly one effect. Authoring them in TS keeps them next to the rest of
the desktop integration suite (`apps/desktop/tests/integration/`) and
sidesteps cross-pane Go ownership. Daemon-side idempotency middleware
itself remains owned by the daemon panes — this harness is the test
substrate.

## Usage

```ts
import {
  tryStartDaemon,
  IDEMPOTENT_WRITE_ENDPOINTS,
  makeIdempotencyKey,
  IDEMPOTENCY_HEADER,
} from "../../src/test-utils/daemon-harness/index.ts";

const start = await tryStartDaemon({ mode: "mock", repoRoot: ROOT });
if (!start.ok) {
  // Skip the suite gracefully — the daemon binary isn't built on this host.
  return;
}
const daemon = start.handle;
try {
  const key = makeIdempotencyKey("create-project", 0);
  const a = await fetch(`${daemon.baseUrl}/v1/projects`, {
    method: "POST",
    headers: {
      "Content-Type": "application/json",
      Authorization: `Bearer ${daemon.authToken}`,
      [IDEMPOTENCY_HEADER]: key,
    },
    body: JSON.stringify({ name: "my-project" }),
  });
  const b = await fetch(`${daemon.baseUrl}/v1/projects`, {
    method: "POST",
    headers: { /* same headers */ },
    body: JSON.stringify({ name: "my-project" }),
  });
  // The daemon must return either:
  //   - same status + same body (replay-as-success), OR
  //   - 409 Conflict if the request body differs from the original
  //     (different intent reusing the same key).
  // ...
} finally {
  await daemon.kill();
}
```

## Defensive behavior

- `tryStartDaemon` returns `{ ok: false, reason: '...' }` when the
  binary is missing, so tests `it.skip()` cleanly on hosts that
  haven't built the daemon (e.g. desktop-only CI lanes).
- Random ephemeral ports avoid collisions when multiple suites run
  in parallel.
- `kill()` SIGTERMs first, falls back to SIGKILL after 500 ms.
- `daemonHome` defaults to a per-PID temp directory under
  `/tmp/hoopoe-harness-<pid>-<port>/` so concurrent runs don't share
  state.

## Idempotency-contract surface (`idempotency-contract.ts`)

`IDEMPOTENT_WRITE_ENDPOINTS` lists every write endpoint that must
honor an `Idempotency-Key` header per `plan.md §2.7`. Each entry
declares its `enforcement` mode (`required` → 400 if header absent,
`honored` → no-op when absent). Tests iterate over this table; the
renderer can also import the same list to ensure every write call
sets the header before retrying through a reconnect.

## What's deferred

- **Daemon-side idempotency middleware** — implementation belongs to
  the daemon panes. Tests in `apps/desktop/tests/integration/idempotency/`
  document the contract and skip when the daemon doesn't yet enforce
  it; they will activate as the middleware lands.
- **Tunnel-drop simulation harness** — the bead's "simulated tunnel
  drop" requires interrupting an in-flight request mid-stream. The
  current harness exposes the daemon `process` handle so a follow-up
  can pause/resume the network path via `iptables` / `tc` /
  process-level pause; the contract test placeholder demonstrates the
  surface.
