# `@hoopoe/fixture-replay`

Deterministic, in-process replay of the Hoopoe Phase 0 fixture corpus for
fast unit + e2e tests that don't touch a real VPS.

> Bead: `hp-q3t` — Fixture-replay harness over Mock Flywheel Mode.
> Companion: `hp-dr8` (the Mock Flywheel daemon-side wiring) + `hp-o74`
> (Phase 1 desktop boot against a fixture corpus).

## What this package does

Boots a `ReplayClient` that mirrors the daemon RPC surface (`plan.md` §2.6)
against a frozen Phase 0 snapshot under
`packages/fixtures/phase0-2026-05-02/scenarios/<id>/`. Everything is read
from disk once at boot and held in memory for the lifetime of the test.

Three scenarios ship today:

| Scenario  | Backed by                                                        | What it exercises                                                                              |
| --------- | ---------------------------------------------------------------- | ---------------------------------------------------------------------------------------------- |
| `fresh`   | `phase0-2026-05-02/scenarios/fresh/snapshot.json`                | A freshly-bootstrapped VPS — only `git`, `agent_mail`, and `health` are present.               |
| `active`  | `phase0-2026-05-02/scenarios/active/snapshot.json`               | An "active swarm" capture — same tool presence as `fresh`; mid-swarm context for stage walks.  |
| `failure` | `phase0-2026-05-02/scenarios/failure/snapshot.json`              | A degraded capture — `health` reports `lizard not on PATH`; missing-tool flows are exercised.  |

A scenario id is the only required input:

```ts
import {
  bootMockFlywheel,
  expectStageReached,
  expectAdapterCalled,
  getEmittedEvents,
  assertNoUnredactedSecrets,
} from "@hoopoe/fixture-replay";

const client = bootMockFlywheel({ scenario: "fresh" });

client.callAdapter("git", "status");
client.callAdapter("agent_mail", "fetch_inbox");

expectAdapterCalled(client, "git", "status");
expectStageReached(client, "planning");

const events = getEmittedEvents(client);
assertNoUnredactedSecrets(events);

client.close();
```

## API summary

```
bootMockFlywheel({ scenario, rootPath?, now? }) → ReplayClient
bootScenarioLibrary({ scenarios?, rootPath?, now? }) → [{ scenario, client }, …]
```

A `ReplayClient` exposes:

| Method                                | Returns                                        | Notes                                                              |
| ------------------------------------- | ---------------------------------------------- | ------------------------------------------------------------------ |
| `scenarioId()`                        | `string`                                       | The Phase 0 scenario id.                                           |
| `health()`                            | `{ status, environment: "mock-flywheel", time }` | Mirrors `/v1/health`.                                              |
| `capabilities()`                      | `Record<string, CapabilityDescriptor>`         | Flattened `<adapter>.<feature>` keys.                              |
| `toolPresence()`                      | `Record<string, boolean>`                      | Includes adapters declared in `adapter-index.json` but absent.     |
| `declaredAdapters()`                  | `readonly string[]`                            | The adapter contract for this scenario.                            |
| `callAdapter(adapter, method)`        | `ToolCapture | null`                           | Records the call, emits an `adapter.invoked` / `adapter.degraded`. |
| `getAdapterInvocation(adapter, name)` | `InvocationEnvelope | null`                    | Read raw captured argv/exit/stdout. Does NOT record a call.        |
| `markStageReached(stage, reason?)`    | `void`                                         | Force a stage marker without an adapter call.                      |
| `reachedStages()`                     | `readonly StageId[]`                           | All stages reached so far.                                         |
| `emit(event)`                         | `void`                                         | Append an event to the in-memory log.                              |
| `emittedEvents()`                     | `readonly ReplayEvent[]`                       | Baseline (synthesized) + test-driven events.                       |
| `recordedCalls()`                     | `readonly AdapterCallRecord[]`                 | Every `callAdapter` invocation.                                    |
| `close()`                             | `void`                                         | Idempotent.                                                        |

Assertion helpers:

| Helper                                            | Throws on                                                |
| ------------------------------------------------- | -------------------------------------------------------- |
| `expectStageReached(client, stage)`               | Stage not reached and no adapter mapped to it was called. |
| `expectAdapterCalled(client, adapter, method)`    | No matching `callAdapter` recorded.                       |
| `expectAdapterNotCalled(client, adapter, method)` | A matching `callAdapter` was recorded.                    |
| `getEmittedEvents(client)`                        | (never throws — pure accessor)                            |
| `assertNoUnredactedSecrets(events)`               | Any unallow-listed secret-shape match in event payloads.  |

## Stage taxonomy (`StageId`)

```
planning   → oracle, agent_mail
beads      → br, bv
swarm      → ntm, caam, agent_mail
hardening  → ubs, health
```

`agent_mail` shows up in two stages on purpose — it powers both the
user↔orchestrator chat (planning) and the agent↔agent mail panel (swarm).
Tests that need a narrow stage match should assert on the specific
adapter via `expectAdapterCalled`.

## Determinism guarantees

Same scenario id + same fixture corpus on disk = byte-identical client state
at boot:

- The snapshot is read once and held immutable.
- Baseline events are derived in alphabetical (tool, invocation) order so
  the seq numbering is stable across runs.
- The `now` override lets tests pin the wall clock if `tickMs` deltas
  matter for assertions.

## Running the tests

```bash
# All replay tests in the desktop workspace:
rch exec -- bun run --cwd apps/desktop test:replay

# Just one suite:
rch exec -- bun run --cwd apps/desktop e2e:replay -- tests/replay/fresh-scenario.test.ts
```

The replay tests are also folded into the desktop package's default
`test` script and the monorepo's `turbo run test` matrix.

## Why a separate package (instead of growing `@hoopoe/fixtures`)

`@hoopoe/fixtures` owns the corpus and the loader for §8.8 tending
scenarios (whose on-disk shape is `meta.json` + `events.jsonl` + per-source
JSON). The Phase 0 corpus uses a different shape (`snapshot.json` +
`adapters/<tool>.json`) produced by `scripts/research-spike/snapshot.sh`.
Keeping the Phase 0 reader and the assertion helpers in a separate
package avoids tangling the corpus loader with test ergonomics, and lets
`hp-dr8` (Mock Flywheel daemon side) iterate on the canonical loader
shape independently.
