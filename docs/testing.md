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

## Cross-references

- `plan.md §10.5` — SLO numeric targets.
- `plan.md §18.1` — milestone acceptance tests (each becomes an `@e2e`
  test).
- `plan.md §18.4` — release smoke (an `@release` tag).
- `packages/slo-targets.yaml` — single SoT.
- `packages/test-evidence/README.md` — envelope schema + reporter API.
- `packages/fixture-replay/README.md` — sibling harness for Phase 0
  fixture-driven tests (hp-q3t).
