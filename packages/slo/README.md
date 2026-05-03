# `@hoopoe/slo`

Single-source-of-truth SLO library for the §10.5 numeric targets, with a
percentile-aware assertion API. Tests + Diagnostics dashboards both
read the same `packages/slo-targets.yaml` so plan and runtime never
disagree about what `p95 ≤ 1s` means.

> Bead: `hp-5ja` — SLO assertion library + canonical YAML.

## Quick start

```ts
import {
  expectSloPass,
  expectSlo,
  expectSloBoolean,
  loadSloTargets,
  useTargets,
} from "@hoopoe/slo";

// (Optional) preload the registry at the top of the test file:
useTargets(loadSloTargets({ repoRoot: process.cwd() }));

// Percentile-target assertion:
const observedSamples = await measureReconnectMs({ runs: 50 });
expectSloPass("desktop.reconnect.p95", observedSamples);

// Same with a minimum-sample-count gate:
expectSlo("bead.kanban.load.1k.p95", observedSamples, { samples: 100 });

// Boolean-target assertion:
expectSloBoolean("dag.usable.500-nodes", await dagIsUsableAt500Nodes());
```

## Canonical YAML schema

```yaml
schemaVersion: 1
targets:
  - id: desktop.reconnect.p95
    description: Desktop reconnect after laptop wake, p95 after network returns
    target:
      percentile: 95
      value: 10s              # accepts `<n>ms`, `<n>s`, `<n>%`, or bare `<n>`
      direction: max          # `max` = observed ≤ value; `min` = observed ≥ value
    source_section: "§10.5"
    enforced_in: [phase2.e2e, chaos]

  - id: dag.usable.500-nodes
    description: DAG view is usable (scrollable + < 500ms layout) at 500 nodes
    target:
      boolean: true
    source_section: "§10.5"
    enforced_in: [phase7.e2e]
```

## API

| Helper                                            | Behavior                                                                    |
| ------------------------------------------------- | --------------------------------------------------------------------------- |
| `loadSloTargets({ path?, repoRoot? })`            | Read + validate the YAML; throws `SloTargetsError` on shape mismatch.        |
| `useTargets(targets)`                             | Cache the loaded registry for `getTarget` / `expectSlo*` lookups.            |
| `getTarget(id)`                                   | Look up a target; throws if `id` is not declared.                            |
| `listTargets()` / `listTargetsByPhase(phase)`     | Enumerate targets (optionally filtered by `enforced_in`).                    |
| `expectSloPass(id, samples)`                      | Compute the declared percentile from samples; throw on breach.               |
| `expectSlo(id, samples, { samples: minN })`       | Same plus minimum-sample-count gate.                                         |
| `expectSloBoolean(id, actual)`                    | Boolean-target check.                                                        |
| `percentile(samples, p)`                          | Linear-interpolation percentile (the same routine the assertions use).       |

## Determinism

`percentile` uses the textbook linear-interpolation method (sort, rank
= p/100 × (n−1), interpolate). Same input → same output, byte-for-byte.

Assertions never mutate the registry. Loading the YAML is side-effect
free; the only state the package holds is the cached registry inside
`useTargets`.

## Why a separate package

`@hoopoe/test-evidence` (hp-6sv) consumes this; the Diagnostics SLO
screen will too; the Go-side `internal/slo` package (deferred to a
follow-up bead) will mirror the same assertion shape. Keeping the SoT
in `@hoopoe/slo` avoids three packages each carrying a half-baked YAML
loader.

## Deferred (separate beads)

- **Go-side `internal/slo` package** — assertion mirror for daemon
  tests. The YAML is language-agnostic so a Go loader can consume it
  cleanly.
- **Diagnostics SLO screen** — renderer UI rendering one row per
  target, observed-vs-declared.
- **Lint rule** — "every SLO mention in code references an ID in
  `packages/slo-targets.yaml`" — easier to land alongside the
  `packages/schemas` codegen drift gate.

## Testing

```bash
rch exec -- bun run --cwd packages/slo test
rch exec -- bun run --cwd packages/slo typecheck
```
