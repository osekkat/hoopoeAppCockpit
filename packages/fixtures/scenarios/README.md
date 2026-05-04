# Mock Flywheel Scenario Goldens

Each active Section 8.8 scenario has a `.goldens/` directory with three frozen artifacts:

- `scenario-source.first-read.json` records the canonical output of `loadTendingScenario()`.
- `event-stream.ndjson` records the instant replay order plus final cursor maps from `startReplay()`.
- `mock-daemon.responses.json` records a fixed set of `MockDaemonClient` responses (`br.list`, `bv.triage`, `ntm.snapshot`, Agent Mail, reservations, logs, auth, and subscribe cursors).

`packages/fixtures/test/golden-replay.test.ts` regenerates the same outputs during test runs, canonicalizes them, and compares bytes against the committed files. Any mismatch means either the scenario pipeline regressed or the scenario contract changed intentionally.

## Canonicalization

The harness sorts JSON object keys and writes JSON with two-space indentation and a trailing newline. NDJSON stream entries are one canonical JSON object per line.

Volatile fields are scrubbed before comparison:

- `rootPath` values under the fixture package become `<fixtures-root>/...`.
- fixture metadata `capturedAt` becomes `<scrubbed-captured-at>`.
- runtime health response `time` becomes `<scrubbed-runtime-time>`.
- `Uint8Array` pane logs become base64 envelopes.

Scenario event timestamps are not scrubbed. They are part of the scenario timeline and should fail the golden test when they change.

## Regeneration

Regenerate only when the Mock Flywheel scenario contract changes intentionally:

```bash
scripts/fixtures/regenerate-goldens.sh
```

Then review `git diff -- packages/fixtures/scenarios/*/.goldens/**`. Do not accept broad churn without checking that the changed behavior is intentional. Do not edit scenario JSON or `events.jsonl` as part of a golden-only update.
