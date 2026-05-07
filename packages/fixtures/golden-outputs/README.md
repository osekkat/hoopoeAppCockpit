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

### `argv` semantics (synthetic vs. realistic)

The `argv` field carries a **stable shape placeholder**, not necessarily a command Hoopoe actually shells out to. Failure-class fixtures (`missing-tool`, `unsupported-version`, `malformed-json`, `timeout`, `high-volume`) often use the same `["<tool>", "--json", "list"]` triple across adapters so the fixture corpus is uniform and easy to scan; the real adapter call paths live in `docs/integration-contracts/<tool>.md`. **Do not read `argv` as the canonical adapter invocation** — for example, `git/timeout.json#argv` is `["git", "--json", "list"]` even though the real Hoopoe invocation under timeout is `git status --porcelain=v2 --branch` (or any of the other commands enumerated in `docs/integration-contracts/git.md`). Similarly `bv/timeout.json#argv` carries `["bv", "--json", "list"]` as a placeholder; bare `bv list` is forbidden by Guardrail 1, and the real bv calls go through `--robot-*` surfaces.

What the fixture *does* pin authoritatively for failure-class states:

- `meta.state` (the failure class)
- `exit` + `stderrText` (the byte-level outcome the adapter must classify)
- `capabilities` (the per-capability contract the adapter must report)
- `stdoutText` for `malformed-json` and `high-volume` (the parser must fail gracefully on these bytes)

`argv` is a label for the slot, not a contract. Tests that pin `argv` byte-exactly should keep that pin (so a future fixture refresh that drops the field is caught), but should not be read as proof that the daemon shells out to that exact command.

Regenerate via:

```bash
scripts/snapshot/seed/build-golden-outputs.sh \
  --snapshot /tmp/hoopoe-snapshot-full.json \
  --fixtures-version phase0-2026-05-02
```
