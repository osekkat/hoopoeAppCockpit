# Capability registry & degraded-mode contract

Hoopoe gates every UI feature and tending job by **capabilities**, not by
optimistic assumptions about installed tool versions. A fixture that *parses*
must not silently mark a feature available if the underlying capability is
missing, blocked, or only partially supported (`plan.md` §2.8).

This document is the contract reference for `hp-r33`: capability registry +
degraded-mode contract. The on-disk implementation:

| Surface | Path | Owner |
| --- | --- | --- |
| Daemon registry (Go) | `apps/daemon/internal/capabilities/` | Hoopoe daemon |
| Daemon HTTP handlers | `apps/daemon/internal/capabilities/http.go` | Hoopoe daemon |
| Renderer gate (TS) | `apps/desktop/src/capabilities/` | Hoopoe desktop |
| Wire schema (OpenAPI) | `packages/schemas/openapi.yaml` (hp-r3i) | `@hoopoe/schemas` |
| Fixture corpus | `packages/fixtures/scenarios/*/capabilities.json` | `@hoopoe/fixtures` |

The Go and TS implementations are intentionally byte-identical in shape; the
OpenAPI spec is the source of truth once `hp-r3i` lands codegen, at which
point both implementations re-export from generated artifacts.

## Endpoints

### `GET /v1/capabilities`

Returns a `CapabilityRegistry` snapshot:

```json
{
  "schemaVersion": 1,
  "snapshotAt": "2026-05-02T23:29:34Z",
  "daemonApiVersion": "0.1.0",
  "fixturesVersion": "phase0-2026-05-02",
  "tools": {
    "git": {
      "tool": "git",
      "version": "2.40.0",
      "source": "CLI",
      "lastCheckedAt": "2026-05-02T23:29:34Z",
      "fixturesVersion": "phase0-2026-05-02",
      "capabilities": {
        "git.status.read": { "status": "ok" },
        "git.push": {
          "status": "blocked-by-policy",
          "notes": "snapshot scripts never push"
        }
      }
    },
    "br": { "...": "..." }
  }
}
```

`tools` is keyed by `ToolId` (closed enum); the `capabilities` map inside
each `ToolReport` is keyed by the **fully-qualified** capability ID
(`git.status.read`, not `status.read`). The fixture format is the inner
`capabilities` block per tool — the daemon wraps it with version/source/
last-checked metadata when serving from a fixture-backed boot.

### `GET /v1/compatibility`

Composes the registry with daemon API version, minimum desktop version,
event schema versions, and migration state:

```json
{
  "schemaVersion": 1,
  "daemonApiVersion": "0.1.0",
  "minDesktopVersion": "0.1.0",
  "eventSchemaVersions": { "project": 1, "swarm": 1 },
  "migrationState": {
    "schemaVersion": 7,
    "appliedAt": "2026-05-02T23:00:00Z",
    "pending": [],
    "phase": "idle"
  },
  "capabilities": { "...": "...embedded full registry..." }
}
```

`migrationState.phase` is optional — the structured fields are load-bearing.
Diagnostics top-bar uses `phase` for a quick verdict; richer surfaces consume
the structured triple.

## Capability schema

```ts
type ToolId =
  | "ntm" | "br" | "bv" | "agent_mail" | "git" | "ru"
  | "caam" | "caut" | "dcg" | "casr"
  | "pt" | "srp" | "sbh"
  | "ubs" | "jsm" | "jfp" | "oracle"
  | "health_ts" | "health_py" | "health_rs" | "health_go" | "health_generic";

type CapabilityStatus =
  | "ok"             // capability fully available
  | "degraded"       // available with a fallback in use
  | "missing"        // probe ran, capability absent
  | "blocked-by-policy"  // capability disabled by daemon policy
  | "untested";      // probe did not run; treat as unavailable

interface Capability {
  status: CapabilityStatus;
  fallback?: string;   // human-readable; "tmux capture-pane" etc.
  transport?: string;  // "websocket" | "http" | "sse" | "stdio" | "fixture"
  notes?: string;      // free-text diagnostic
}

interface ToolReport {
  tool: ToolId;
  version: string;          // semver; "" if unknown
  source: string;           // "ntm serve" | "--robot-* JSON" | "CLI" | "fixture"
  capabilities: Record<string, Capability>;  // keyed by full capId
  lastCheckedAt: string;    // RFC3339
  fixturesVersion: string;
}
```

### Status semantics

- **`ok`** — fully available; no fallback.
- **`degraded`** — available, but a fallback path is in use. Adapter records
  the fallback in `Capability.fallback`. Renderer shows a degraded badge.
- **`missing`** — probe ran and the capability is not available (binary
  absent, version too old, subcommand missing). Renderer shows unavailable.
- **`blocked-by-policy`** — capability is disabled by daemon configuration
  (e.g., `git.push` in a read-only mirror, `caam.account.switch` in a
  snapshot-only environment). Renderer shows a policy badge distinct from
  missing.
- **`untested`** — probe was *not run* (e.g., snapshot script skipped this
  tool). Renderer maps to unavailable but Diagnostics keeps the distinction
  so users know a reprobe could change the verdict.

The renderer collapses these five storage states into four UI buckets:

| Storage status | Renderer bucket |
| --- | --- |
| `ok` | `available` |
| `degraded` | `degraded` |
| `missing`, `untested` | `unavailable` |
| `blocked-by-policy` | `blocked-by-policy` |

Diagnostics surfaces always show the underlying storage status.

## Degraded-mode contract

Each gated UI feature and tending job declares a
`FeatureCapabilityRequirement`:

```ts
interface FeatureCapabilityRequirement {
  featureId: string;                       // 'swarm.bead.push-branch', etc.
  capabilitiesRequired: string[];          // fully-qualified capIds
  capabilitiesOptional: string[];
  degradedMode: {
    ifMissingRequired:
      | "block_job"          // refuse to run; surface error
      | "run_read_only"      // execute in read-only mode
      | "emit_diagnostic";   // log to Diagnostics, do not panel-warn
    ifMissingOptional:
      | "continue_with_warning"
      | "suppress_related_detections";
    activityBehavior:
      | "silent"
      | "diagnostics_only"
      | "activity_panel_warning";
  };
}
```

### Resolution priority

When `Determine` (Go) / `determineFeature` (TS) resolves a feature against
the registry:

1. Any required capability with status `blocked-by-policy` →
   `render = "blocked-by-policy"`. Render bucket cannot be downgraded by
   later checks.
2. Any required capability `missing` or `untested` →
   `render = "unavailable"` (unless already blocked-by-policy).
3. Any required *or* optional capability `degraded` →
   `render = "degraded"` (unless already unavailable / blocked).
4. Otherwise `render = "available"`.

The `contractAction` in the returned `FeatureDecision` reflects the
requirement's `ifMissingRequired` directly — it is the policy the daemon
applies when executing the feature, independent of the render bucket.

### Activity-panel behavior

The `activityBehavior` field controls the user-visible noise level when a
feature runs degraded or unavailable:

- `silent` — never surface in Activity panel; audit log always records.
- `diagnostics_only` — surface in Diagnostics tab but not the Activity drawer.
- `activity_panel_warning` — surface in the Activity drawer with a warning
  badge.

Audit logging is **always** independent of `activityBehavior` (Guardrail 10
in `AGENTS.md`).

## Adapter contract: fixture parses ≠ feature available

The load-bearing §2.8 contract: a fixture that *parses* successfully must
not mark a feature available if the underlying capability is missing,
blocked, untested, or degraded. Adapter contract tests assert capability
state, not just parser success.

The Phase 0 corpus in `packages/fixtures/scenarios/*/capabilities.json` is
the source of truth for this. Spot-check coverage by scenario:

| Scenario | What the registry asserts |
| --- | --- |
| `healthy-hour` | All canonical capabilities `ok`; `git.push` and `caam.account.switch` `blocked-by-policy`; `health.coverage` and `dcg.verdicts.subscribe` `untested`. |
| `idle-but-not-stuck` | Same as healthy-hour; tending pre-script returns `wakeAgent: false`. |
| `wedged-pane` | `ntm.robot.snapshot` `ok` but pane evidence triggers tending action plan. |
| `rate-limited-with-caam` | `caam.account.switch` `ok`; ActionPlan emits switch. |
| `rate-limited-no-caam` | `caam.account.switch` `missing`; degraded path engages without proposing CAAM action. |

The adapter tests in `apps/daemon/internal/capabilities/registry_test.go`
include explicit cases for: normal output, missing tool, unsupported version,
malformed output, timeout, and high-volume output (per `plan.md` §18.3).

## Capability ID namespaces

Authoritative list (extend by adding fixtures + adapter coverage; the names
are renderer-stable contracts, not implementation details):

```
ntm.sessions.list, ntm.panes.stream, ntm.approvals.list,
ntm.robot.snapshot, ntm.robot.status, ntm.robot.tail, ntm.swarm.halt,
ntm.serve.rest

br.issues.read, br.issues.update, br.dep.add, br.sync.flush_only

bv.robot.triage, bv.robot.plan, bv.robot.insights, bv.robot.diff,
bv.tui  (always blocked-by-policy — Guardrail 1)

agent_mail.messages.read, agent_mail.messages.send,
agent_mail.reservations.list

git.status.read, git.diff.read, git.push, git.unpushed.list

ru.sync.dry_run, ru.status.read, ru.list.paths,
ru.prune.dry_run, ru.schema

caam.accounts.list, caam.account.switch
caut.usage.snapshot
dcg.verdicts.subscribe
casr.session.resume

pt.kill, srp.signals.read, sbh.cleanup

ubs.scan
jsm.skill.install, jsm.skill.verify, jsm.skill.list, jfp.skill.install
oracle.serve.status, oracle.browser.run

health.coverage, health.complexity, health.churn  (under health_<lang>)
```

## Renderer feature catalog

Every gated UI surface declares its requirement in
`apps/desktop/src/capabilities/registry.ts` (`FEATURE_CATALOG`). Adding a
gated feature is a *single edit* to that file; the calling component
imports the catalog and calls `decideFeature(registry, featureId)`.

Examples shipped at registry land time:

- `swarm.bead.push-branch` — requires `git.status.read` + `git.push`.
- `bead.kanban.refresh` — requires `br.issues.read`; optional `bv.robot.triage`.
- `swarm.dashboard.live` — requires `ntm.robot.snapshot`; optional
  `ntm.panes.stream`.
- `approvals.dcg.subscribe` — requires `dcg.verdicts.subscribe`.
- `tending.watch-safety-thresholds` — requires `ntm.sessions.list`;
  optional `ntm.swarm.halt`.
- `activity.mail.send` — requires `agent_mail.messages.send`.

## How to add a new capability

1. Add the capability ID to the relevant adapter's probe in
   `apps/daemon/internal/capabilities/` (or in the upstream adapter for the
   tool — the registry is just a composition layer).
2. Add the same capId to `packages/fixtures/scenarios/*/capabilities.json`
   for every scenario that exercises it.
3. Add adapter contract tests asserting the capability under
   normal/missing/unsupported/malformed/timeout/high-volume conditions.
4. If a UI feature should depend on it, declare a
   `FeatureCapabilityRequirement` in the renderer's `FEATURE_CATALOG`.
5. Update this docs page's namespace list.

## How to add a new ToolId

OpenAPI's enum is closed. Adding a new tool id (or a new `health_<lang>`)
requires a coordinated edit to:

- `packages/schemas/openapi.yaml` (`ToolId` enum) — `hp-r3i` owner.
- `apps/daemon/internal/capabilities/types.go` (`KnownClosedTools`).
- `apps/desktop/src/capabilities/types.ts` + `registry.ts`
  (`KNOWN_CLOSED_TOOLS`).
- `packages/fixtures/scenarios/*/capabilities.json` if a corpus entry is
  needed.

Bump `CapabilityRegistry.schemaVersion` if the *shape* changes (per
`plan.md` §10.3). Adding tools/capabilities does not bump the schema; only
field renames or removals do.
