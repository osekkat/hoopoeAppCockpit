# Phase 0 fixture pack — `bv` (real VPS)

Real `bv --robot-*` output captured over SSH from the ACFS VPS for adapter contract tests (plan.md §16 / §18.3 / hp-ge0 / hp-kmrc).

## Why this pack exists

The original Phase 0 fixture pack at `packages/fixtures/phase0-2026-05-02/scenarios/{fresh,active,failure}/adapters/bv.json` correctly recorded `present: false, skipReason: "missing binary on PATH: bv", captures: {}` because the test VPS (`admin@45.85.250.216`) did not expose `bv` on the default PATH at that time:

```bash
$ ssh admin@45.85.250.216 'which bv'
zsh:1: command not found: bv
```

hp-kmrc found that `bv v0.16.0` is installed at `/home/admin/.local/bin/bv`. The capture command prepends `/home/admin/.local/bin` and records output from the real VPS project `/home/admin/Projects/nexusAudio` (`https://github.com/osekkat/nexusAudio`, git HEAD `82367437c261a52958e442dbc1202e3aaa22e684`, 167 beads / 4 open / 0 in-progress at capture time).

The capture covers every `--robot-*` flag that bv v0.16.0 exposes, plus `--export-md`. It also records the requested `bv --robot-snapshot` attempt as unsupported, because that flag does not exist in bv v0.16.0.

## Layout

```
packages/fixtures/phase0-bv/
├── README.md          ← this file
├── manifest.json      ← provenance + per-capture key list
├── replay.test.ts     ← golden-replay test (validates shape invariants)
└── captures/
    ├── robot-triage.json        — `bv --robot-triage` (unified mega-command)
    ├── robot-snapshot.unsupported.json — `bv --robot-snapshot` unsupported flag envelope
    ├── robot-plan.json          — `bv --robot-plan`
    ├── robot-priority.json      — `bv --robot-priority`
    ├── robot-insights.json      — `bv --robot-insights`
    ├── robot-recipes.json       — `bv --robot-recipes`
    ├── robot-next.json          — `bv --robot-next`
    ├── robot-forecast-all.json  — `bv --robot-forecast all`
    └── export.head.md           — first 200 lines of `bv --export-md`
```

## Unsupported requested flag

The hp-kmrc prompt requested `bv --robot-snapshot`. The real VPS tool is `bv v0.16.0`, whose `bv --robot-help` output advertises `--robot-triage`, `--robot-next`, `--robot-plan`, and `--robot-insights`; `--robot-snapshot` exits 1 with `unknown flag: --robot-snapshot`.

| Prompt name           | Reality (v0.16.0)                                       |
| --------------------- | ------------------------------------------------------- |
| `--robot-snapshot`    | Doesn't exist — captured as `robot-snapshot.unsupported.json`; `--robot-triage` is the unified command |
| `--robot-export-md`   | Doesn't exist — `--export-md <path>` writes Markdown    |

## Replay test

`replay.test.ts` is a golden-replay test that:

1. Reads each capture file in `captures/`.
2. Asserts each JSON capture parses successfully.
3. Asserts each capture's top-level key set matches `manifest.json` (catches schema drift in bv output).
4. Asserts a small set of structural invariants per command (e.g. `triage.quick_ref.top_picks` is a non-empty array, `recipes` is a non-empty array, `recommendations` is an array, `forecast_count` matches `forecasts.length`).
5. Validates the export.head.md starts with the canonical `# Beads Export` header.

Run via:

```bash
rch exec -- bun test packages/fixtures/phase0-bv/replay.test.ts
```

## Re-capture

```bash
ssh admin@45.85.250.216
export PATH=/home/admin/.local/bin:$PATH
cd /home/admin/Projects/nexusAudio
bv --robot-triage      > /tmp/robot-triage.json
bv --robot-snapshot    > /tmp/robot-snapshot.json  # expected unsupported in bv v0.16.0
bv --robot-plan        > /tmp/robot-plan.json
bv --robot-priority    > /tmp/robot-priority.json
bv --robot-insights    > /tmp/robot-insights.json
bv --robot-recipes     > /tmp/robot-recipes.json
bv --robot-next        > /tmp/robot-next.json
bv --robot-forecast all> /tmp/robot-forecast-all.json
bv --export-md /tmp/export.md
# scp the files back, head -200 export.md > export.head.md, swap them
# in here, and update manifest.json's captureSource + topLevelKeys.
```

## Cross-references

- `plan.md` §16 / §18.3 — adapter contract test scope.
- `packages/fixtures/phase0-2026-05-02/` — the original Phase 0 corpus this pack supplements.
- `packages/fixtures/conformance/bv.test.ts` — sibling adapter-conformance harness (uses `golden-outputs/bv/`).
- `packages/fixtures/golden-outputs/bv/normal.json` — pre-existing real `bv --robot-triage` envelope from 2026-05-02 (uses the same capture shape; this pack covers the rest of the robot-* surface).
- bead **hp-ge0** — Phase 0 fixture for `bv` (this pack closes it).
- bead **hp-ie2** — peer parallel for `ntm`.
