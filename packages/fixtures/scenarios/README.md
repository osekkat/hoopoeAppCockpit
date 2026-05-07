# Mock Flywheel Scenario Goldens

Each active Section 8.8 scenario has a `.goldens/` directory with three frozen artifacts:

Active scenarios currently covered by replay goldens: `healthy-hour`, `idle-but-not-stuck`, `wedged-pane`, `rate-limited-no-caam`, `rate-limited-with-caam`, `stale-reservation`, `commit-burst`, `budget-breach`, `skill-drift`, `missing-tool`, `postcondition-failure`, and `action-arbitration`.

Closure audit `hp-7we`: the canonical no-CAAM rate-limit scenario ID is `rate-limited-no-caam`; all 12 `TENDING_SCENARIOS` IDs are now populated on disk and covered by replay goldens.

- `scenario-source.first-read.json` records the canonical output of `loadTendingScenario()`.
- `event-stream.ndjson` records the instant replay order plus final cursor maps from `startReplay()`.
- `mock-daemon.responses.json` records a fixed set of `MockDaemonClient` responses (`br.list`, `bv.triage`, `ntm.snapshot`, Agent Mail, reservations, logs, auth, and subscribe cursors).

## Canonical-state snapshots (hp-k3u)

Each scenario also carries three canonical-state snapshot fixtures that
back the §8 tending decisions plan.md §8.8 requires (stale-commit
push, budget gating, code-health-driven review flips, build-queue
contention warnings):

- `git-state.json` — HEAD sha + branch + ahead/behind/dirty + uncommitted
  files + stale-push list. Drives push-policy enforcement and the
  stale-commit-push tend job.
- `health-snapshot.json` — verdict + coverage + complexity + hotspot
  count + per-language metrics. Drives the topbar pill and the Hardening
  review-mode flip threshold.
- `build-queue-state.json` — queueDepth + running/queued jobs. Drives
  rate-limit/budget arbitration and swarm-launch build-contention
  warnings.

Shapes are pinned in `packages/fixtures/src/kinds.ts`
(`GitStateSnapshot`, `HealthSnapshotFixture`, `BuildQueueStateSnapshot`).
Per-scenario flavor is asserted in
`packages/fixtures/tests/fixture-quality.test.ts` (e.g., `commit-burst`
must report ≥15 ahead commits; `wedged-pane` must show a long-elapsed
running job; `budget-breach` must show concurrent running jobs).
These fixtures are synthetic — Phase 0 real-VPS captures replace them
once the real ACFS pack lands.

`packages/fixtures/test/golden-replay.test.ts` regenerates the same outputs during test runs, canonicalizes them, and compares bytes against the committed files. Any mismatch means either the scenario pipeline regressed or the scenario contract changed intentionally.

## Canonicalization

The harness sorts JSON object keys and writes JSON with two-space indentation and a trailing newline. NDJSON stream entries are one canonical JSON object per line.

Volatile fields are scrubbed before comparison:

- `rootPath` values under the fixture package become `<fixtures-root>/...`.
- fixture metadata `capturedAt` becomes `<scrubbed-captured-at>`.
- runtime health response `time` is asserted to equal the deterministic
  `MOCK_FLYWHEEL_HEALTH_TIME` constant (hp-2szb); any other ISO at key
  `time` falls back to `<scrubbed-runtime-time>` as a safety net but is
  no longer the expected case for `health()`.
- `Uint8Array` pane logs become base64 envelopes.

Scenario event timestamps are not scrubbed. They are part of the scenario timeline and should fail the golden test when they change.

## Regeneration

Regenerate only when the Mock Flywheel scenario contract changes intentionally:

```bash
scripts/fixtures/regenerate-goldens.sh
```

Then review `git diff -- packages/fixtures/scenarios/*/.goldens/**`. Do not accept broad churn without checking that the changed behavior is intentional. Do not edit scenario JSON or `events.jsonl` as part of a golden-only update.
