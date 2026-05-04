# `@hoopoe/problem-types`

RFC 7807 problem-type registry + canonical envelope renderer +
contract-test assertion helpers. Daemon-side handlers and
renderer-side surface routing (hp-8dym) both consume from here so
the wire format never drifts.

> Bead: `hp-g6sp` — RFC 7807 problem+json producer infrastructure.

## What this is

`packages/schemas/problem-types.yaml` is the single source of truth
for every error the daemon emits. Each entry declares:

- `id` — stable internal id (referenced from Go ProblemError + tests)
- `type_uri` — canonical RFC 7807 `type` URL
- `title` — short human-readable summary
- `status` — HTTP status code
- `surface` — renderer hint: `toast | banner | inline_pill | blocking_modal`
- `actionability` — suggested user action: `reload | re-pair | edit-deps | switch-account | open-docs | manual`
- `user_message` — text shown to the user (supports `{{var}}` templating)
- `detail_template?` — optional template for RFC 7807 `detail` (templated; redacted at the daemon boundary)

Adding a new problem type = add an entry here AND wrap the Go error
with `ProblemTypeID() string` returning the id.

## Quick start

```ts
import {
  loadProblemTypes,
  useProblems,
  renderProblemEnvelope,
  assertResponseMatchesRegistry,
} from "@hoopoe/problem-types";

// Boot:
useProblems(loadProblemTypes({ repoRoot: process.cwd() }));

// Build a canonical envelope from an id + runtime extensions:
const envelope = renderProblemEnvelope("bead.cycle-detected", {
  extensions: { cyclePath: ["hp-a", "hp-b", "hp-a"] },
  correlationId: "audit-abc-123",
  instance: "/v1/projects/proj-1/beads/hp-a/deps",
});
// envelope.type === "https://hoopoe.io/problems/bead/cycle-detected"
// envelope.user_message === 'Adding this dependency would create a cycle: hp-a,hp-b,hp-a'

// Contract test (against a live daemon):
const response = await fetch(`${baseUrl}/v1/projects/x/beads/y/deps`, {
  method: "POST",
  body: JSON.stringify({ blocks: "z" }),
});
await assertResponseMatchesRegistry(response, "bead.cycle-detected");
```

## API

| Helper                                              | Behavior                                                                       |
| --------------------------------------------------- | ------------------------------------------------------------------------------ |
| `loadProblemTypes({ path?, repoRoot? })`            | Read + validate the YAML; throws `ProblemTypesError` on shape mismatch.        |
| `useProblems(registry)` / `clearProblems()`         | Cache / clear the registry.                                                    |
| `getProblem(id)`                                    | Look up an entry by id; throws if unknown.                                     |
| `listProblems()`                                    | All declared problem types in YAML order.                                      |
| `listBySurface(s)` / `listByActionability(a)`       | Filter by enum value.                                                          |
| `listByStatus(n)`                                   | Filter by HTTP status code.                                                    |
| `renderProblemEnvelope(id, { extensions, … })`      | Build the canonical RFC 7807 envelope.                                         |
| `renderTemplate(template, extensions)`              | Lower-level template substitution (keeps `{{var}}` literal on missing values). |
| `assertResponseIsProblemJson(response)`             | HTTP-level shape check (Content-Type + 4xx/5xx status).                        |
| `assertProblemMatchesRegistry(envelope, id)`        | Verify type/title/status/surface/actionability against the registry entry.     |
| `assertResponseMatchesRegistry(response, id)`       | One-shot: parse + assert.                                                      |

## Why a separate package

`packages/schemas/` owns the OpenAPI codegen (RedMountain's hp-r3i);
the loader for one specific YAML lives next to its consumers
(daemon ProblemError middleware, renderer hp-8dym, contract tests)
rather than expanding the schemas package's surface. Same pattern
as `@hoopoe/slo`, `@hoopoe/tending-actions`, and `@hoopoe/test-evidence`.

## Out of scope (separate beads)

- **Daemon-side Go ProblemError interface + chi/echo middleware** — daemon panes own the producer.
- **Per-endpoint auto-generated contract tests** — depend on the daemon emitting problem+json; the assertion helpers shipped here are the substrate.
- **Diagnostics 'recent problems' panel** — renderer UI work.
- **CI gate** — "endpoint adds error path without registry entry" — natural fit alongside the daemon-side middleware bead.

## Testing

```bash
rch exec -- bun run --cwd packages/problem-types test
rch exec -- bun run --cwd packages/problem-types typecheck
```
