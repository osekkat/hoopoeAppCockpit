# Testing — runners, tags, evidence

> Bead `hp-6sv` — Test runner config + JSON evidence emitter.
> Sibling: `hp-q3t` (fixture-replay harness).

Hoopoe ships three test runners and one shared evidence shape. Every test
run produces a single JSON envelope under
`docs/test-evidence/<phase>/<UTC-timestamp>/<runner>-<runId>.json`. The
envelope is the audit trail — there is no other "logs you have to dig
for" surface.

## Runners

| Runner       | Surface                            | Wrapper                                               |
| ------------ | ---------------------------------- | ----------------------------------------------------- |
| `bun-test`   | desktop unit + integration         | `apps/desktop/scripts/test-evidence/run-bun.ts`       |
| `go-test`    | daemon unit + integration          | `apps/daemon/scripts/test-evidence/run-go.ts`         |
| `playwright` | desktop e2e (Electron + Chromium)  | `@hoopoe/test-evidence/playwright-reporter` (env-gated) |

The bead text references `vitest` for the desktop runner; the actual
desktop runner is **Bun test** (the project chose Bun + Turbo per
`plan.md §3`). `bun test --reporter=junit --reporter-outfile=<path>`
produces JUnit XML that the wrapper post-processes into the envelope.

## Tag taxonomy

Tags live in test names (the `describe` / `test` description). The
`@hoopoe/test-evidence` parser strips them from the envelope's
`result.name`.

| Tag             | Meaning                                                                     |
| --------------- | --------------------------------------------------------------------------- |
| `@unit`         | Pure / in-process / no I/O.                                                 |
| `@integration`  | Touches a real subsystem (DB, FS, child process).                           |
| `@e2e`          | Full app flow (Playwright + Electron).                                      |
| `@chaos`        | Fault-injection — intentionally drops/breaks something.                     |
| `@smoke`        | Minimal "does it boot" suite (release-gate-blocking).                       |
| `@release`      | Release-gate suite (`release_gate` CI job).                                 |
| `@slo:<id>`     | Asserts the test's duration ≤ the SLO target with id `<id>`.                |

`<id>` must match an entry in `packages/slo-targets.yaml`.

## SLO source of truth

`packages/slo-targets.yaml` is the **only** file that carries §10.5
numbers. Plan quality, Diagnostics dashboards, and tests all read from
it. Adding a new SLO target = add a new entry there + tag a test with
`@slo:<id>`.

Example:

```ts
test("reconnects within budget @slo:desktop.reconnect.p95", async () => {
  // ...
});
```

The reporter records `result.slo = { target, declared, observed, passed }`
in the envelope. Tests don't need to assert against the threshold
themselves — the envelope's `slo.breached[]` is non-empty when any
target is crossed, and CI gates on that.

## Evidence layout

```
docs/test-evidence/
└── <phase>/                     # e.g. phase2
    └── 20260504T010203Z/        # UTC timestamp segment (mkdir -p)
        ├── bun-test-<runId>.json
        ├── go-test-<runId>.json
        └── playwright-<runId>.json
```

The envelope schema is documented at
`packages/test-evidence/README.md`. Every field is optional except
`schemaVersion`, `runId`, `ts`, `runner`, `phase`, and `results[]`.

## Running

```bash
# Desktop unit + integration via Bun (writes envelope under docs/test-evidence/)
bun apps/desktop/scripts/test-evidence/run-bun.ts -- src tests/smoke/*.test.ts tests/unit/*.test.ts tests/replay/*.test.ts electron/*.test.ts

# Daemon unit + integration via go test -json
bun apps/daemon/scripts/test-evidence/run-go.ts -- ./...

# Playwright e2e — set HOOPOE_TEST_EVIDENCE=1 and the env-gated reporter
# in playwright.config.ts will be active. (Default config is unchanged.)
HOOPOE_TEST_EVIDENCE=1 bun run --cwd apps/desktop e2e
```

All three wrappers shell out to the underlying runner, capture its
output, parse into `TestResult[]`, attach SLO assertions for tests
tagged `@slo:<id>`, then write the envelope to disk and exit with the
runner's status.

## CI integration (deferred to a follow-up bead)

This commit ships the **measurement** path (envelope written on every
local run). Coverage-delta enforcement, PR-comment summary, and the
release-gate workflow that diffs envelopes against `main` are tracked
separately in the CI bead — they touch `.github/workflows/` which is
shared infra.

## Strategy Matrix

| Surface | Required coverage | Evidence |
| --- | --- | --- |
| Desktop unit | Components, stores, hooks, IPC contracts, renderer hardening. | `bun-test` envelope. |
| Desktop e2e | First-run wizard, Activity panel, stage navigation, Diagnostics, error surfaces. | Playwright envelope, screenshots where relevant. |
| Daemon unit | Adapters, scheduler invariants, auth, jobs, process manager, audit, upgrade, capabilities. | `go-test` envelope. |
| Adapter contracts | Normal, missing tool, unsupported version, malformed JSON, timeout, high-volume output. | Golden fixtures under `docs/integration-contracts/` and `packages/fixtures/`. |
| Tending harness | Healthy hour, idle-not-stuck, wedged pane, rate limit, stale reservation, budget breach, skill drift, missing tool, postcondition failure. | Fixture replay evidence. |
| Chaos | Tunnel drop, daemon restart, disk pressure, slow renderer, malformed adapter output, long scheduler job. | `@chaos` evidence plus replay logs. |
| Release smoke | Install, pair, launch mock swarm, health, review, upgrade, restart/replay, no-secret audit scan. | `@release` evidence under `docs/test-evidence/release/`. |

## Disposable VPS E2E

Real-VPS suites are gated and never run in ordinary local unit loops. A nightly
or pre-release job may provision or attach a disposable Ubuntu VPS, run the
wizard, install ACFS, install the daemon, import a fixture repo, execute the
Phase 3/18 smoke path, then destroy or archive the VPS according to provider
policy.

Required artifacts:

- bootstrap raw log and parsed checkpoint stream;
- `/v1/version`, `/v1/capabilities`, and readiness snapshots;
- `br`, `bv --robot-*`, NTM, Agent Mail, CAAM, caut, rch adapter snapshots;
- audit excerpt with secrets scan result;
- daemon upgrade happy-path or rollback evidence when testing releases.

## Smoke Commands

Use `rch` for Go and heavy test/build work:

```bash
cd apps/daemon
rch exec -- go test ./...
rch exec -- go build ./...
rch exec -- go vet ./...
```

Desktop smoke:

```bash
rch exec -- bun run --cwd apps/desktop typecheck
rch exec -- bun run --cwd apps/desktop test
```

Docs-only changes should at minimum run a no-op docs/link check when one exists,
UBS on changed supported files, and a relevant surface smoke if the docs claim a
specific code contract.

## Cross-references

- `plan.md §10.5` — SLO numeric targets.
- `plan.md §18.1` — milestone acceptance tests (each becomes an `@e2e`
  test).
- `plan.md §18.4` — release smoke (an `@release` tag).
- `packages/slo-targets.yaml` — single SoT.
- `packages/test-evidence/README.md` — envelope schema + reporter API.
- `packages/fixture-replay/README.md` — sibling harness for Phase 0
  fixture-driven tests (hp-q3t).
