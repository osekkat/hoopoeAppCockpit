# Phase 0 fixture pack — `bv` (local stand-in)

Real `bv --robot-*` output captured against the local `hoopoeAppCockpit/.beads/` for adapter contract tests (plan.md §16 / §18.3 / hp-ge0).

## Why this pack exists

The Phase 0 fixture pack at `packages/fixtures/phase0-2026-05-02/scenarios/{fresh,active,failure}/adapters/bv.json` correctly recorded `present: false, skipReason: "missing binary on PATH: bv", captures: {}` because the test VPS (`admin@45.85.250.216`) does not have `bv` installed:

```bash
$ ssh admin@45.85.250.216 'which bv'
zsh:1: command not found: bv
```

This satisfies the original real-VPS pedigree (the snapshot tool faithfully reported what was true on the box) but leaves the `bv` adapter without a real-shape fixture for §18.3 conformance tests. This pack fills that gap with a **local stand-in** capture: real `bv` output against a real `.beads/` database (the live hoopoe project, 261 issues / 88 open / 3 in-progress at capture time).

The capture covers every `--robot-*` flag that bv v0.16.0 exposes, plus `--export-md`. Conformance tests can assert against the JSON shapes here while the same shapes will eventually be re-captured against a real ACFS VPS once `bv` is installed there (sub-bead filed alongside hp-ge0).

## Layout

```
packages/fixtures/phase0-bv/
├── README.md          ← this file
├── manifest.json      ← provenance + per-capture key list
├── replay.test.ts     ← golden-replay test (validates shape invariants)
└── captures/
    ├── robot-triage.json        — `bv --robot-triage` (unified mega-command)
    ├── robot-plan.json          — `bv --robot-plan`
    ├── robot-priority.json      — `bv --robot-priority`
    ├── robot-insights.json      — `bv --robot-insights`
    ├── robot-recipes.json       — `bv --robot-recipes`
    ├── robot-next.json          — `bv --robot-next`
    ├── robot-forecast-all.json  — `bv --robot-forecast all`
    └── export.head.md           — first 200 lines of `bv --export-md`
```

## Substitutions vs the original capture list

The hp-ge0 prompt named two flags that don't exist in `bv` v0.16.0; this pack substitutes the closest real flag and documents the substitution in `manifest.json`:

| Prompt name           | Reality (v0.16.0)                                       |
| --------------------- | ------------------------------------------------------- |
| `--robot-snapshot`    | Doesn't exist — `--robot-triage` is the unified command |
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

## Re-capture against a real ACFS VPS

When `bv` lands on the VPS:

```bash
ssh admin@45.85.250.216
cd ~/Projects/<some-real-project>
bv --robot-triage      > /tmp/robot-triage.json
bv --robot-plan        > /tmp/robot-plan.json
bv --robot-priority    > /tmp/robot-priority.json
bv --robot-insights    > /tmp/robot-insights.json
bv --robot-recipes     > /tmp/robot-recipes.json
bv --robot-next        > /tmp/robot-next.json
bv --robot-forecast all> /tmp/robot-forecast-all.json
bv --export-md /tmp/export.md
# scp the files back, head -200 export.md > export.head.md, swap them
# in here, flip manifest.json's `mode` to "real-vps" + set
# `realVpsAcceptance: true` + record VPS host / project / capturedAt.
```

## Cross-references

- `plan.md` §16 / §18.3 — adapter contract test scope.
- `packages/fixtures/phase0-2026-05-02/` — the original Phase 0 corpus this pack supplements.
- `packages/fixtures/conformance/bv.test.ts` — sibling adapter-conformance harness (uses `golden-outputs/bv/`).
- `packages/fixtures/golden-outputs/bv/normal.json` — pre-existing real `bv --robot-triage` envelope from 2026-05-02 (uses the same capture shape; this pack covers the rest of the robot-* surface).
- bead **hp-ge0** — Phase 0 fixture for `bv` (this pack closes it).
- bead **hp-ie2** — peer parallel for `ntm`.
