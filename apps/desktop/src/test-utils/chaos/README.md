# `chaos` — TS-side fault-injection primitives

> Bead: `hp-2qn` — Phase 2.5 chaos / fault-injection suite.

Substrate for §16 Phase 2.5 chaos tests: process-control signals,
slow-consumer wrappers, controlled disk-pressure, and malformed-
adapter fixtures. Rides on the daemon-spawn harness from hp-ngq
(`apps/desktop/src/test-utils/daemon-harness/`) so chaos tests
spin up a real daemon, induce a fault, observe the response, and
clean up — without re-implementing subprocess management per test.

## Primitives

| Primitive                              | Use                                                                      |
| -------------------------------------- | ------------------------------------------------------------------------ |
| `pauseProcess({pid, label?})`          | SIGSTOP — freeze the target. Idempotent.                                 |
| `resumeProcess({pid, label?})`         | SIGCONT — resume.                                                        |
| `withPaused(target, fn)`               | `pause → run fn → resume` even when fn throws. Use this for transient freezes. |
| `shutdownProcess(target, child, opts)` | SIGTERM with grace, SIGKILL on timeout. Returns exit code.               |
| `slowConsumeWebSocketMessages(ws, …)`  | Subscribe to WS, sleep `delayMs` between events; backpressure simulator. |
| `slowConsumeReadable(stream, …)`       | Same shape over `AsyncIterable<unknown>`.                                |
| `fillDisk({dir, totalBytes, …})`       | Write padding files inside an allow-listed temp dir; returns handle with `release()`. |
| `loadMalformedAdapterFixture({tool})`  | Read the canonical `malformed-json.json` golden fixture for an adapter.  |
| `parseMalformedFixture(fixture)`       | Verify a fixture really is malformed (sanity check for chaos tests).     |

## Defensive behavior

- `pauseProcess` / `resumeProcess` throw `ChaosProcessError` if the
  PID is missing (e.g., the daemon already exited).
- `withPaused` always sends SIGCONT even when the wrapped callback
  throws — the harness can never leave a permanently-frozen daemon.
- `fillDisk` refuses to write outside `/tmp/`, `/var/tmp/`, or
  `/private/tmp/`. Total bytes capped at 1 GiB by default
  (`allowAboveCeiling: true` to override). The returned handle's
  `release()` deletes every file written.
- `loadMalformedAdapterFixture` throws with a precise pointer when
  the corpus doesn't have a `malformed-json.json` for the requested
  tool.

## Quick start

```ts
import {
  pauseProcess,
  resumeProcess,
  withPaused,
  fillDisk,
  loadMalformedAdapterFixture,
  parseMalformedFixture,
  slowConsumeWebSocketMessages,
} from "../../src/test-utils/chaos/index.ts";
import { tryStartDaemon } from "../../src/test-utils/daemon-harness/index.ts";

const start = await tryStartDaemon({ mode: "mock", repoRoot: ROOT });
if (!start.ok) return;
const daemon = start.handle;
try {
  // Freeze the daemon for 2s; observe the renderer's reconnect path.
  await withPaused(
    { pid: daemon.process.pid, label: "hoopoed" },
    () => new Promise((r) => setTimeout(r, 2_000)),
  );

  // Fill 100 MB into the daemon's home dir; observe the §10 disk-pressure path.
  const handle = fillDisk({ dir: daemon.daemonHome, totalBytes: 100 * 1024 * 1024 });
  try {
    // …drive the test scenario…
  } finally {
    handle.release();
  }

  // Verify the malformed-adapter fixture exists for `br`:
  const fixture = loadMalformedAdapterFixture({ tool: "br", repoRoot: ROOT });
  const result = parseMalformedFixture(fixture);
  // result.ok === false; result.error contains the JSON parse error.
} finally {
  await daemon.kill();
}
```

## Out of scope

- **Tunnel-drop simulation** — needs network-layer interrupt
  (`tc qdisc` / `iptables`); a follow-up can plumb that through this
  primitive set.
- **macOS sleep/wake** (hp-nd42) — separate bead, depends on this one.
- **VPS reboot** — full power cycle isn't testable in CI; substrate
  ready for tooling that runs against a real VPS.
- **Daemon-side fault handlers** — the response side of every
  fault (graceful disk-pressure tagging, _lag emission under slow
  consumer, capability registry's `degraded` marker on malformed
  adapter output) is owned by daemon panes. This module is the
  test-side trigger; the response-side assertions live in the chaos
  test files and skip when the response logic isn't wired yet.
