# `phase0-2026-05-02/` — real-VPS fixture corpus (placeholder)

Placeholder for the real-VPS capture corpus. Files here are produced by `scripts/research-spike/snapshot.sh` running on the research-spike VPS (`hp-r7i`) after the canonical 13-step wizard run-through (`hp-jvm`).

Each scenario directory mirrors the `meta.fixturesVersion: phase0-2026-05-02` tag.

## Scenarios (forthcoming)

| Directory          | Source                                                                                  | Status |
| ------------------ | --------------------------------------------------------------------------------------- | ------ |
| `scenarios/fresh/` | `snapshot.sh --scenario fresh` against fresh `acfs onboard`                              | pending hp-r7i |
| `scenarios/active/`| `snapshot.sh --scenario active` mid-swarm                                                | pending hp-r7i |
| `scenarios/failure/`| `snapshot.sh --scenario failure --scenario-notes "<deliberate breakage>"`               | pending hp-r7i |

Each scenario directory shares the per-scenario file contract documented in [`packages/fixtures/README.md`](../README.md) (meta.json, bv-triage.json, br-list.json, ntm-snapshot.json, agent-mail-dump.json, reservations.json, events.jsonl, pane-logs/, build-logs/, capabilities.json, expected-outcome.json).

## Capture recipe

See [`scripts/research-spike/README.md`](../../../scripts/research-spike/README.md) "Running on the research-spike VPS" for the canonical recipe.
