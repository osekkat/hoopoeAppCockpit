# `@hoopoe/tending-actions`

Typed action surface for ActionPlan: a TypeScript loader + drift
checker for the canonical
[`packages/schemas/tending-actions.yaml`](../schemas/tending-actions.yaml).

> Bead: `hp-dmz` — tending-actions.yaml schema (typed action surface
> for ActionPlan).

## What this is

The YAML at `packages/schemas/tending-actions.yaml` declares the
**closed set** of action `kind`s a tending agent may emit (per
`plan.md §8.3.1`). Every kind ships with:

- a JSON Schema for its `target` payload,
- a JSON Schema for its `args` payload,
- a `riskClass` (`low | medium | high | destructive`),
- a `requiresApprovalDefault` flag,
- declared `preconditions[]` (canonical-state queries the daemon
  evaluates **before** execution), and
- declared `postconditions[]` (queries it evaluates **after**;
  failure emits a follow-up detection per §8.3.1).

Anything else is rejected at validation. The YAML mirrors the
OpenAPI `ActionKind` enum in `packages/schemas/openapi.yaml`; this
package enforces the mirror invariant via `assertActionKindInSync`.

## Quick start

```ts
import {
  loadTendingActions,
  useActions,
  getAction,
  listActionsByRisk,
  assertActionKindInSync,
} from "@hoopoe/tending-actions";

// Boot:
const bundle = loadTendingActions({ repoRoot: process.cwd() });
useActions(bundle);
assertActionKindInSync(bundle); // fails loudly on YAML/OpenAPI drift

// Lookup:
const kill = getAction("agent.kill_wedged_process");
console.log(kill.preconditions, kill.postconditions);

const destructive = listActionsByRisk("high");
```

## Public API

| Helper                                   | Behavior                                                                       |
| ---------------------------------------- | ------------------------------------------------------------------------------ |
| `loadTendingActions({ path?, repoRoot?})`| Read + validate the YAML; throws `TendingActionsError` on shape mismatch.      |
| `useActions(bundle)` / `clearActions()`  | Cache / clear the registry.                                                    |
| `getAction(kind)`                        | Look up an action; throws if not declared.                                     |
| `listActions()`                          | All declared actions in YAML order.                                            |
| `listActionsByRisk(riskClass)`           | Filter by `low | medium | high | destructive`.                                |
| `listActionsRequiringApproval()`         | Actions whose default policy demands explicit human approval.                  |
| `checkActionKindDrift(bundle)`           | Returns `{extraInYaml, missingInYaml, inSync}` against OpenAPI's enum.         |
| `assertActionKindInSync(bundle)`         | Throws on drift; used by the conformance test.                                 |

## Why a separate package

`packages/schemas/` is owned by the OpenAPI keystone (`hp-r3i`); the
loader for one specific YAML lives next to the consumers it serves
(daemon ActionPlan executor, future Diagnostics action-graph viewer)
rather than expanding `packages/schemas/`'s surface. The YAML stays
the source of truth; this package is just the typed view.

## Testing

```bash
rch exec -- bun run --cwd packages/tending-actions test
rch exec -- bun run --cwd packages/tending-actions typecheck
```
