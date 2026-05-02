# `@hoopoe/fixtures`

Mock Flywheel corpus — JSON snapshots, event streams, pane logs, Agent Mail
messages, file reservations, build/test outputs, and health snapshots
captured against a real ACFS VPS during Phase 0. Used to:

1. Drive **Mock Flywheel Mode** in development (`plan.md §13`) so the
   desktop and daemon can be exercised without a live VPS.
2. Power **adapter contract tests** (`plan.md §18.3`) — golden fixtures
   for normal output, missing tool, unsupported version, malformed JSON,
   timeout, and high-volume output.
3. Feed the **tending evaluation harness** (`plan.md §8.8`) — healthy
   hour, idle-but-not-stuck, wedged pane, rate-limited (with/without
   CAAM), stale reservation, budget breach, skill drift, missing tool,
   postcondition failure, action arbitration.

## Status

Pre-Phase-1 scaffold (hp-xru). The real corpus capture, format, and loader
land in **Phase 0** beads (hp-7cs, hp-6v3, hp-78m, hp-wle, hp-d54,
hp-pl5o).

## Layout (forthcoming)

```text
packages/fixtures/
├── corpus/
│   ├── git/                # git status, diff, log, ru sync/status/list/prune
│   ├── br/                 # br list, ready, show
│   ├── bv/                 # bv --robot-{triage,plan,insights,diff}
│   ├── ntm/                # ntm --robot-snapshot, --robot-status, --robot-tail
│   ├── agent-mail/         # mail dumps, threads, reservations
│   ├── health/             # lizard, language-native coverage/complexity
│   └── tending/            # §8.8 fixtures: healthy/wedged/rate-limited/etc.
├── src/
│   ├── loader.ts           # typed loader + capability assertions
│   └── kinds.ts            # fixture-kind enum + schema bindings
└── README.md
```

## Capability assertions, not just parser success

Per `plan.md §2.8`, a fixture that parses but cannot satisfy the declared
capability **must not** mark the feature as available. Every fixture is
tagged with the adapter capabilities it should produce (or deliberately
fail to produce). Adapter contract tests assert capability presence, not
parser success.
