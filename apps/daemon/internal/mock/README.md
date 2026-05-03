# Mock Flywheel Mode

Mock Flywheel Mode boots the daemon from the Phase 0 fixture corpus instead of
probing a live ACFS VPS. It is for development, CI smoke tests, and support
reproduction only; it is not a second source of truth.

Default fixture root:

```text
packages/fixtures/phase0-2026-05-02/scenarios/
```

Supported seed scenarios:

```text
fresh
active
failure
```

Run the mock daemon entry point:

```bash
go run ./cmd/hoopoed-mock --scenario=fresh
```

Equivalent environment:

```bash
HOOPOE_MOCK_SCENARIO=fresh go run ./cmd/hoopoed-mock
```

The Go-side manifest shape is intentionally close to the TypeScript replay
harness: scenario id, fixture root, adapter index path, snapshot path, adapter
list, capturedAt, and fixturesVersion. The daemon exposes that at
`/v1/mock/scenario` and returns raw adapter captures at
`/v1/mock/adapters/{tool}`.

Smoke endpoints:

```text
GET /v1/version
GET /v1/capabilities
GET /v1/jobs
GET /v1/events/replay?channel=_system&sinceSequence=0
GET /v1/mock/scenario
GET /v1/mock/adapters/git
```

All capability reports in mock mode use `source: "fixture"` and preserve
`fixturesVersion` from the scenario snapshot. Missing tools from the real VPS
capture are represented as a deterministic `__probe__` capability with
`status: "missing"` so consumers can distinguish fixture absence from parser
success.
