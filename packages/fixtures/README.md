# `@hoopoe/fixtures` — Mock Flywheel corpus

Seed corpus of JSON snapshots, replayable event streams, byte-addressable pane logs, build/test logs, Agent Mail messages, file reservations, capability snapshots, and health snapshots — captured (or stubbed for) the ACFS toolchain so Hoopoe can be developed and demoed without a live VPS.

Used by:

1. **Mock Flywheel Mode** (`plan.md` §13, cross-cutting bead `hp-dr8`) — fixture-backed daemon for dev / CI / demos.
2. **Adapter contract tests** (`plan.md` §18.3) — golden fixtures for normal output + the five failure-class fixtures (missing tool, unsupported version, malformed JSON, timeout, high-volume).
3. **Tending evaluation harness** (`plan.md` §8.8) — 12 scenarios covering healthy / idle / wedged / rate-limited / stale-reservation / commit-burst / budget-breach / skill-drift / missing-tool / postcondition-failure / action-arbitration.

## Status

Seed (`hp-wle`). Real VPS captures land once `hp-r7i` (provision) + `hp-jvm` (manual wizard) are unblocked. Each scenario / golden-output file is one of:

- **Realistic** — captured from `scripts/research-spike/snapshot.sh --self-test` against this dev box on 2026-05-02. The 11 tools present locally have actual output.
- **Synthetic** — hand-written for §8.8 scenarios that need state the dev box cannot exhibit (wedged pane, rate-limited, etc.). Marked `synthetic: true` in metadata.
- **Stub** — schema-valid placeholders waiting for VPS-pinned ground truth. Marked `stub: true` in metadata.

Every fixture's `meta.kind` (`realistic | synthetic | stub`) is asserted by the fixture-quality test bead [`hp-pl5o`](#).

## Layout

```text
packages/fixtures/
├── README.md                      ← you are here
├── package.json                   ← @hoopoe/fixtures (hp-xru scaffold)
├── tsconfig.json
├── src/
│   ├── index.ts                   ← package identity (hp-xru)
│   ├── kinds.ts                   ← fixture-kind taxonomy (hp-wle)
│   ├── loader.ts                  ← typed loader (hp-wle stub; full Mock Flywheel work in hp-dr8)
│   └── index.test.ts
├── scenarios/                     ← §8.8 tending-harness scenarios (synthetic + real)
│   ├── healthy-hour/
│   ├── idle-but-not-stuck/
│   ├── wedged-pane/
│   ├── rate-limited-no-caam/
│   ├── rate-limited-with-caam/
│   ├── stale-reservation/
│   ├── commit-burst/
│   ├── budget-breach/
│   ├── skill-drift/
│   ├── missing-tool/
│   ├── postcondition-failure/
│   └── action-arbitration/
├── phase0-2026-05-02/             ← real-VPS scenarios (fresh / active / failure)
│   └── scenarios/
│       ├── fresh/                  ← captured against acfs-onboard-fresh VPS
│       ├── active/                 ← captured mid-swarm
│       └── failure/                ← captured with deliberate breakage
└── golden-outputs/                ← per-adapter contract-test corpus
    ├── README.md
    ├── br/
    │   ├── normal.json
    │   ├── missing-tool.json
    │   ├── unsupported-version.json
    │   ├── malformed-json.json
    │   ├── timeout.json
    │   └── high-volume.json
    ├── bv/  (same six)
    ├── ntm/ (same six)
    ├── agent_mail/ (same six)
    ├── git/
    ├── ru/
    ├── caam/
    ├── caut/
    ├── dcg/
    ├── ubs/
    ├── jsm/
    ├── jfp/
    ├── oracle/
    ├── pt/
    ├── srp/
    ├── sbh/
    ├── casr/
    └── health/{ts,python,rust,go,generic}/
```

## Per-scenario file contract

Every scenario directory under `scenarios/<id>/` and `phase0-*/scenarios/<id>/` MUST contain:

| File                          | Purpose                                                                                                                  | Required for                          |
| ----------------------------- | ------------------------------------------------------------------------------------------------------------------------ | ------------------------------------- |
| `meta.json`                   | `{kind: 'realistic'|'synthetic'|'stub', scenario: <id>, fixturesVersion, capturedAt, vpsId, source, notes}`              | All scenarios                         |
| `bv-triage.json`              | `bv --robot-triage` output                                                                                                | All                                   |
| `br-list.json`                | `br list --status=open --json --limit 250` output                                                                         | All                                   |
| `ntm-snapshot.json`           | `ntm --robot-snapshot` output                                                                                             | All                                   |
| `agent-mail-dump.json`        | Mail messages dump (per-thread)                                                                                           | All                                   |
| `reservations.json`           | File-reservation listing                                                                                                  | All                                   |
| `events.jsonl`                | Replayable WS events with sequence cursors per channel                                                                    | All                                   |
| `pane-logs/<agent>.bin`       | Byte-addressable PTY captures                                                                                             | All (empty file ok for healthy)       |
| `build-logs/<runId>.txt`      | Test/build output samples                                                                                                  | All (one file ok)                     |
| `capabilities.json`           | Capability registry snapshot (`plan.md` §2.8 shape)                                                                       | All                                   |
| `tools-degraded.json`         | List of degraded/missing capabilities                                                                                      | `missing-tool` scenario specifically  |
| `expected-outcome.json`       | What the tending harness should see: detections emitted, wake/no-wake, ActionPlan shape, approvals requested, postconditions verified | All §8.8 scenarios |

## Per-golden-output file contract

Every `golden-outputs/<adapter>/<state>.json` MUST contain:

```jsonc
{
  "meta": {
    "adapter": "br",
    "state": "normal" | "missing-tool" | "unsupported-version" | "malformed-json" | "timeout" | "high-volume",
    "kind": "realistic" | "synthetic" | "stub",
    "fixturesVersion": "phase0-2026-05-02",
    "capturedAt": "2026-05-02T19:50:00Z",
    "source": "snapshot.sh --self-test (local)" | "real-VPS <vps-id>" | "hand-written"
  },
  "argv": ["br", "list", "--status=open", "--json", "--limit", "250"],
  "exit": 0,
  "stdoutBytes": 861975,
  "stdoutJson": { ... },
  "stdoutText": "...",
  "stderrBytes": 0,
  "stderrText": "",
  "durationMs": 142,
  "capabilities": {
    "br.issues.read": { "status": "ok" }
  }
}
```

The adapter contract test (`plan.md` §18.3, bead `hp-pl5o`) loads each `golden-outputs/<adapter>/<state>.json` and asserts:

- The adapter parses `stdoutJson` / `stdoutText` correctly.
- The adapter reports the same capability state as the fixture's `capabilities` block.
- For `state: malformed-json`, the parser fails *gracefully* (no panic; logs the error; reports `degraded`).
- For `state: missing-tool`, the adapter reports `present: false` and the right `skipReason`.

## Fixtures version

Every fixture is tagged with `meta.fixturesVersion`. The capability registry (`plan.md` §2.8) records the same tag so we know which corpus produced a snapshot.

Current corpus tag: `phase0-2026-05-02`.

## Refresh cadence

- **Real-VPS captures (`phase0-*/`):** refreshed when ACFS gets a major version bump or when an adapter contract test starts failing on a stale fixture.
- **Synthetic scenarios (`scenarios/`):** refreshed when `plan.md` §8.8 list changes, when a new tending action is added, or when an existing one's expected-outcome shifts.
- **Golden outputs:** refreshed alongside per-tool integration contract changes (`docs/integration-contracts/`).

To regenerate from a live VPS:

```bash
# 1. Run snapshot.sh against each scenario (see scripts/research-spike/README.md)
ssh <vps> bash /tmp/hoopoe-snapshot/snapshot.sh --scenario fresh > snapshot.fresh.json
# ... (repeat for active, failure)

# 2. Split each snapshot into per-file pieces:
jq '.captures.bv.captures.robot_triage.stdoutJson' snapshot.fresh.json > \
   packages/fixtures/phase0-2026-05-02/scenarios/fresh/bv-triage.json
# ... (per-tool extraction; helper script TBD in hp-pl5o)

# 3. Commit the changed fixtures.
```

A helper script `packages/fixtures/scripts/regen-from-snapshot.ts` will land with `hp-pl5o`.

## Capability assertions, not just parser success

Per `plan.md` §2.8, a fixture that parses but cannot satisfy the declared capability **must not** mark the feature as available. Every fixture's `capabilities` block is asserted by the contract tests. Don't optimize a fixture just to make a parser pass — fix the parser or the contract.
