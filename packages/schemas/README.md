# `@hoopoe/schemas`

> Single source of truth for the Hoopoe daemon ↔ desktop wire contract.

`openapi.yaml` is authoritative. The TypeScript client (`src/generated/openapi.ts`) and Go types (`go/schemas.gen.go`) are **generated** from it; hand-maintained duplicate shapes across `apps/desktop` and `apps/daemon` are forbidden (`plan.md §3 'Shared'`). Drift is policed by a CI gate (`.github/workflows/schemas-codegen-drift.yml`).

## Layout

```text
packages/schemas/
├── openapi.yaml                       # Authoritative OpenAPI 3.1 spec
├── events/
│   └── ws-envelope.schema.json        # Standalone JSON Schema for WS envelope
├── tending-actions.yaml               # Closed action set for ActionPlan (§8.3.1)
├── src/
│   ├── index.ts                       # Public TS entry — re-exports + helpers
│   ├── index.test.ts                  # Smoke tests against generated types
│   ├── schema-versions.test.ts        # §10.3 migration discipline contract
│   └── generated/
│       └── openapi.ts                 # openapi-typescript output (committed)
├── go/
│   ├── go.mod                         # Standalone Go module
│   ├── cfg.yaml                       # oapi-codegen v2 config
│   └── schemas.gen.go                 # oapi-codegen output (committed)
├── scripts/
│   ├── gen-go.ts                      # Pinned oapi-codegen v2.7.0 runner
│   ├── validate-ts-codegen.ts         # TS drift gate
│   └── validate-go-codegen.ts         # Go drift gate
├── package.json
├── tsconfig.json
└── README.md                          # This file
```

## Regenerate

```bash
# From the repo root
bun run --cwd packages/schemas generate         # ts + go
bun run --cwd packages/schemas generate:ts      # ts only (openapi-typescript@7.13.0)
bun run --cwd packages/schemas generate:go      # go only (oapi-codegen@v2.7.0)
```

Both generators are pinned in their script wrappers — a stray `go install ...@latest` won't quietly drift the output between committers. Bumping a generator is a deliberate edit to `scripts/gen-go.ts` or the `bunx --bun openapi-typescript@<version>` invocation in `package.json`.

## Validate (drift gate)

```bash
bun run --cwd packages/schemas validate         # ts + go
bun run --cwd packages/schemas validate:ts
bun run --cwd packages/schemas validate:go
```

Each validator regenerates to a temp file and byte-compares against the committed copy. On drift it writes `*.drift` next to the committed file so you can `diff` locally; CI uploads the same file as a workflow artifact (`schemas-codegen-drift`) for inspection from the Actions log.

The validate scripts also run in CI on every push/PR that touches `packages/schemas/**` (see `.github/workflows/schemas-codegen-drift.yml`).

## Consume from TypeScript

```ts
// In apps/desktop or any other workspace that depends on @hoopoe/schemas
import type {
  CompatibilityReport,
  CapabilityRegistry,
  Project,
  Bead,
  Job,
  Approval,
  WsEventEnvelope,
  ActionPlan,
  Problem,
  paths,
  operations,
} from "@hoopoe/schemas";

import { isProblem, PROBLEM_JSON_CONTENT_TYPE } from "@hoopoe/schemas";
```

Bare aliases are exported for the most-used component schemas (see `src/index.ts`); for deeper access use the full namespace `components["schemas"]["X"]` or `paths["/v1/projects"]["get"]`. If you want a typed `fetch` wrapper on top of `paths`, [`openapi-fetch`](https://github.com/openapi-ts/openapi-typescript/tree/main/packages/openapi-fetch) is the canonical companion (not yet adopted in `apps/desktop`).

## Consume from Go

`packages/schemas/go/` is its own Go module with module path `github.com/hoopoe-cockpit/hoopoe/packages/schemas/go`. Wire it into a consuming module via either pattern:

### Workspace (preferred)

Add a `go.work` at the repo root:

```text
go 1.26

use (
  ./apps/daemon
  ./packages/schemas/go
)
```

Then in your daemon code:

```go
import schemas "github.com/hoopoe-cockpit/hoopoe/packages/schemas/go"

func handleCompatibility(w http.ResponseWriter, r *http.Request) {
  report := schemas.CompatibilityReport{
    SchemaVersion:    1,
    DaemonApiVersion: "0.1.0",
    MinDesktopVersion: "0.1.0",
    EventSchemaVersions: map[string]int{"_system": 1},
    MigrationState: schemas.MigrationState{
      SchemaVersion: 0,
      AppliedAt:     time.Now().UTC(),
      Pending:       []string{},
      Phase:         ptr(schemas.Idle),
    },
    Capabilities: capabilitiesSnapshot,
  }
  // ... encode + write
}
```

### Replace directive (no go.work)

In `apps/daemon/go.mod`:

```text
require github.com/hoopoe-cockpit/hoopoe/packages/schemas/go v0.0.0-00010101000000-000000000000

replace github.com/hoopoe-cockpit/hoopoe/packages/schemas/go => ../../packages/schemas/go
```

The generated Go is types-only — no chi/echo/gin server stubs, no client. The daemon picks its own HTTP framework (chi today via `apps/daemon/internal/api/`) and binds typed handlers to these types itself.

## Add a new entity

1. Add the schema to `openapi.yaml` under `components.schemas:`.
2. If it's persisted (stored in the daemon SQLite, JSONL, or audit log), include a top-level `schemaVersion: { $ref: '#/components/schemas/SchemaVersion' }` field and list it in `required:`. Add the entity name to `PERSISTED_ENTITIES` in `src/schema-versions.test.ts` so the discipline gate catches future regressions.
3. Add at least one path (or wire it into an existing response shape) so `oapi-codegen` will emit it. Standalone schemas that no path references appear in the TS output (openapi-typescript emits everything) but **not** in the Go output (oapi-codegen only emits reachable schemas).
4. Re-export the type from `src/index.ts` if it's part of the consumer-facing surface.
5. Run `bun run --cwd packages/schemas generate` and commit the regenerated `src/generated/openapi.ts` and `go/schemas.gen.go` alongside the spec change.
6. Run `bun run --cwd packages/schemas test` to confirm contract tests pass.

## Conventions (declared once in `openapi.yaml`)

- **Errors** — `application/problem+json` (`#/components/schemas/Problem`) per RFC 7807. `code` is a dotted machine-readable identifier (e.g., `auth.token_expired`, `capability.missing`).
- **Idempotency** — every retryable write accepts `Idempotency-Key` (a stable ULID/UUID); the daemon dedupes within a sliding window (default 24h) and replays the original status + body.
- **Schema versions** — every persisted entity declares `schemaVersion`. Daemon startup runs migrations with backup + rollback (`plan.md §10.3`). Bumping a schema is a deliberate edit + the corresponding migration in the daemon's migration package.
- **Capability gating** — every UI feature gates on capability IDs reported by `/v1/capabilities` (§2.8). Adapter contract tests assert capabilities, not just parser success.
- **WS events** — the WebSocket envelope is also documented as JSON Schema in `events/ws-envelope.schema.json` (sidecar; OpenAPI 3.1 cannot model the event-stream shape adequately for AsyncAPI tooling).
- **ActionPlan kinds** — closed enum mirrored in `tending-actions.yaml`. Adding a new `kind` is a deliberate spec change that lands in **both** files in the same commit; the daemon rejects unknown kinds at validation.

## Sidecars

- **`events/ws-envelope.schema.json`** — standalone JSON Schema for `WsEventEnvelope`, `WsClientOp`, `WsServerMessage`, `WsHeartbeat`, `WsGap`, `WsLag`. Mirrors the same shapes as `components.WsEventEnvelope` etc. so AsyncAPI / replay-tools that don't read OpenAPI can pin against the contract.
- **`tending-actions.yaml`** — authoritative closed list of ActionKind values with per-kind `target` / `args` JSON Schemas, `riskClass`, and `requiresApprovalDefault`. The OpenAPI `ActionKind` enum is a mirror; the spec's contract is "any unknown kind is rejected at validation" — adding a new kind requires a same-commit change to both files.
- **`provider-plugin.yaml`** — authoritative documentation of the 5 typed methods every VPS-provisioning plugin implements (`listRegions`, `listSizes`, `estimateMonthlyCost`, `createInstance`, `destroyInstance`). Per-method JSON Schema fragments `$ref` into `openapi.yaml` component schemas. The Go interface (`schemas.ProviderPlugin`) lives in `go/provider.go`. v1 ships with Contabo only (`hp-9fo`); future Hetzner / DigitalOcean / OVH / Linode plugins implement the same interface.
- **`preload-api.yaml`** — single source of truth for the renderer ↔ preload ↔ main IPC allowlist (hp-rflj). Closed sets of `daemonRequestMethods` (typed RPC over `hoopoe.daemon.request`), `daemonSubscribeTopics` (WS subscriptions), `preloadChannels` (named channel IDs), and `internalCommandPrefixes` (mock-flywheel/internal namespaces). Per-method input/output schemas `$ref` into `openapi.yaml`. Generated TS lives at `apps/desktop/src/shared/ipc-contract.gen.ts`; the manual `ipc-contract.ts` (hp-n5za hardening) stays as the live import surface until the desktop owner switches to re-export from the generated file. The drift gate (`validate:preload`) catches divergence between the YAML and the generated TS.

## Add a new provider plugin

Plugins ship as Go packages compiled into the daemon binary; the registry is open via package-init. To add a new provider (e.g., Hetzner):

1. **Implement the Go interface.** In `apps/daemon/internal/providers/hetzner/`:
   ```go
   import schemas "github.com/hoopoe-cockpit/hoopoe/packages/schemas/go"

   type plugin struct{ /* http client, api token via CAAM, etc. */ }

   var _ schemas.ProviderPlugin = (*plugin)(nil)

   func (p *plugin) Manifest() schemas.ProviderPluginManifest { ... }
   func (p *plugin) ListRegions(ctx context.Context) ([]schemas.ProviderRegion, error) { ... }
   func (p *plugin) ListSizes(ctx context.Context, regionID string) ([]schemas.ProviderSize, error) { ... }
   func (p *plugin) EstimateMonthlyCost(ctx context.Context, opts schemas.ProviderEstimateCostOpts) (*schemas.ProviderCostEstimate, error) { ... }
   func (p *plugin) CreateInstance(ctx context.Context, opts schemas.ProviderCreateInstanceOpts) (*schemas.ProviderInstance, error) { ... }
   func (p *plugin) DestroyInstance(ctx context.Context, instanceID string) (*schemas.ProviderDestroyResult, error) { ... }

   func init() { providers.Register(&plugin{}) }
   ```

2. **Honor the contract invariants** (documented in `go/provider.go` + `provider-plugin.yaml`):
   - `Manifest().Capabilities` declares only the methods you implement; calling an undeclared method returns `ErrProviderMethodUnsupported` (the daemon translates to a `provider.method_unsupported` problem+json).
   - `CreateInstance` MUST clean up on failure — no orphaned billable resources. If the provider partially created an instance, destroy it before returning the error.
   - `DestroyInstance` MUST be idempotent — a second call against an already-destroyed ID returns `&schemas.ProviderDestroyResult{Ok: true, InstanceId: id}`, not an error.
   - `EstimateMonthlyCost` MUST be a pure function for fixed `CatalogVersion` — same `(region, size, bandwidthTBExpected)` always returns the same result while the catalog version is unchanged.
   - The cost-estimate `Breakdown` must sum to `Usd` (the renderer asserts this).

3. **Use CAAM for credentials.** Provider API tokens MUST live in CAAM, never in Hoopoe config (Guardrail 11 generalised: third-party secrets live in CAAM). The plugin's auth bootstrap fetches the token from CAAM at first use.

4. **Redact secrets in logs.** The hp-lxs structured logger has stable pattern IDs for SSH keys (`ssh-pubkey-fingerprint`), provider API tokens (`provider-key-*`), and pairing tokens. Emit log fields per the `loggingFields` declared in `provider-plugin.yaml` for each method; the redactor rewrites secret material before the entry hits a transport.

5. **Update the docs.** If the new plugin opts into a method not yet listed in `provider-plugin.yaml`'s `optionalCapabilities` (e.g., a new `vps.snapshot`), the schema work belongs in a separate bead — coordinate via Agent Mail before extending the closed contract.

6. **Add a contract test.** Mirror `packages/schemas/go/provider_test.go`'s `mockProvider` pattern with your real plugin (or a fixture-backed mock) and assert: ListRegions returns ≥1 region; cost estimate breakdown sums to total; create→destroy is idempotent; manifest capabilities match what's declared.

## Spec version

The OpenAPI `info.version` is the **wire-contract** version (semver). Bump the patch when adding new optional fields or new schemas; bump the minor on backward-compatible additive changes that introduce new endpoints; bump the major on breaking changes. The current version is `0.1.0` (Phase 2 seed contract — pre-stable). The `HOOPOE_OPENAPI_VERSION` constant in `src/index.ts` is kept in sync (a contract test pins the agreement).

## Coordination notes

- **`Capability` shape** is co-owned with `hp-r33` (capability registry); shape changes go through Agent Mail thread `hoopoe-phase2` (msg #145, #153, #154 record the locking discussion).
- **`Job` + `JobStatus` shape** is consumed by `hp-gkk` (job registry); interface bumps coordinate via the same thread.
- **`ActionPlan` + `ActionKind`** is consumed by `hp-209` (ActionPlan executor) and `hp-0d7` (pre-script runner) — both depend on the closed `kind` set staying mirrored between `openapi.yaml` and `tending-actions.yaml`.
- **`CompatibilityReport.migrationState`** is structured (`MigrationState` object: `schemaVersion + appliedAt + pending + optional phase`), agreed with `hp-r33` to support §10.3 Diagnostics ("schema 7 of 9 applied; pending: foo, bar").

## What lives outside this package

- **Daemon migration runtime** (§10.3) — daemon-side; future bead. Imports the `SchemaVersion` constant + per-entity migration scripts.
- **Renderer capability hooks** (`apps/desktop/src/capabilities/`) — owned by `hp-r33`; consumes the generated `Capability` / `ToolReport` types.
- **CommandSpec executor** (`apps/daemon/internal/...`) — owned by `hp-209`; validates against `tending-actions.yaml` per-kind schemas.
- **Provider plugin manifest validator** — owned by Phase 13 work; uses `ProviderPluginContract` shape.
