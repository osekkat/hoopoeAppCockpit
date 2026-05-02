# `golden-outputs/` — adapter contract test corpus

One file per `(adapter, state)` pair. `state` is one of: `normal`, `missing-tool`, `unsupported-version`, `malformed-json`, `timeout`, `high-volume` (per `plan.md` §18.3).

Adapter contract tests load `golden-outputs/<adapter>/<state>.json` and assert:

- The adapter parses `stdoutJson` / `stdoutText` correctly.
- The adapter reports the same capability state as the fixture's `capabilities` block.
- For `state: malformed-json`, the parser fails *gracefully* (no panic; logs the error; reports `degraded`).
- For `state: missing-tool`, the adapter reports `present: false` and the right `skipReason`.
- For `state: timeout`, the adapter surfaces the timeout without retrying without backoff.
- For `state: high-volume`, the adapter respects pagination contracts (`--limit` / `--offset`) and never raises `--max-bytes` past the schema cap.

`normal.json` files are realistic when the source snapshot has a capture for them; otherwise they are stubs marked `meta.kind: stub`. The five failure-class fixtures are always `synthetic` (real systems don't exhibit them on demand).

Regenerate via:

```bash
scripts/snapshot/seed/build-golden-outputs.sh \
  --snapshot /tmp/hoopoe-snapshot-full.json \
  --fixtures-version phase0-2026-05-02
```
