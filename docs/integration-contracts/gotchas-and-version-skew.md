# Gotchas + version skew (`hp-d54`)

> The integration-contracts-angle catalog: every tool quirk an adapter author needs to know **before** writing the parser. Companion to [`../research-spike/gotchas.md`](../research-spike/gotchas.md) (the workflow + setup catalog) — that one is broader, this one is tighter and indexed by the per-tool integration contracts.
>
> When an adapter author hits a new surprise, append it here AND update the relevant per-tool contract's "Known gotchas" section in the same PR. Lessons rot fast — this file is the antidote.

---

## How to use this file

1. Skimming before writing an adapter? Jump to the section for your tool.
2. Hit a new gotcha? Append in the standard shape (symptom / root cause / mitigation / fixture / references).
3. Capability vs install confusion? Read **§2 Capability vs install** first.
4. Looking for the bigger workflow gotchas (concurrent builds, peer staged files, etc.)? See [`../research-spike/gotchas.md`](../research-spike/gotchas.md).

---

## 1. Bare-TUI invocations (Guardrail 1)

### `bv` without flags hangs the agent session

- **Symptom:** Agent PTY shows interactive triage UI; no parseable stdout; eventually the agent's framework times out without a useful response.
- **Root cause:** Bare `bv` is a TUI, not a CLI. The `--robot-*` flags are the JSON surface (`bv --robot-help`, `--robot-triage`, `--robot-plan`, `--robot-insights`, `--robot-priority`, `--robot-diff`, `--robot-recipes`).
- **Mitigation in Hoopoe:** `scripts/research-spike/snapshot.sh capture::bv()` invokes only `--robot-*` surfaces. Capability registry pins `bv.tui: blocked-by-policy`. CI lint rule (`hp-rflj`-adjacent): grep for bare `\bbv\b` outside `--robot-*` patterns in `apps/daemon/**/*.go` fails the build.
- **Fixture coverage:** every scenario's `capabilities.json` asserts `bv.tui != ok`; `golden-outputs/bv/normal.json` is real `--robot-triage` output (~28 KB).
- **References:** [`bv.md`](bv.md), `plan.md` Appendix C #1, AGENTS.md "bv — Graph-Aware Triage Engine".

### `ntm spawn` without flags prompts

- **Symptom:** `ntm spawn` blocks waiting for stdin; agent hangs.
- **Root cause:** Some NTM subcommands are interactive when not given full argv (typically `spawn`, `attach`, `kill` without `--robot-*`).
- **Mitigation in Hoopoe:** All mutating NTM operations go through the typed `ActionPlan` (`plan.md` §8.3.1) — argv is constructed by the daemon, never by the agent free-form. Snapshot script captures only `ntm sessions list --json` + `--robot-*`.
- **Fixture coverage:** `golden-outputs/ntm/normal.json` is `--robot-snapshot`.
- **References:** [`ntm.md`](ntm.md), `vibing-with-ntm` skill.

### `ru sync` without `--non-interactive` prompts

- **Symptom:** `ru sync` blocks on dirty-WIP confirmation prompts.
- **Root cause:** `ru` defaults to interactive confirmation for some workflows.
- **Mitigation in Hoopoe:** Adapter argv builder MUST include `--non-interactive` AND `--json` for every sync/status invocation; rejects argv missing them at compile time (Go test).
- **Fixture coverage:** `golden-outputs/ru/normal.json` (`ru --schema`, non-interactive).
- **References:** [`ru.md`](ru.md), `plan.md` §17 ru paragraph.

---

## 2. Capability vs install (per `plan.md` §2.8)

The single most important distinction for adapter authors. **A tool's binary being on PATH is necessary but not sufficient for a capability to be `ok`.** Mark a capability `ok` only if it actually works on the current VPS at the current version with the current configuration.

### `caut` not on PATH at all on the dev box

- **Symptom:** Top-bar subscription pill never appears; no error; users wonder why.
- **Root cause:** `caut` was not installed on the local self-test box (research-spike 2026-05-02). Adapter probe `command -v caut` returned non-zero → `present: false`.
- **Mitigation in Hoopoe:** Adapter reports `caut.usage.snapshot: missing` in the capability registry. The top-bar pill checks the capability state, not just "is `caut` installed?" — UI hides the pill cleanly + surfaces in Diagnostics. **Never** infer quota from provider 429s as a workaround (Guardrail 11).
- **Fixture coverage:** `golden-outputs/caut/missing-tool.json`; in `scenarios/missing-tool/` (forthcoming, scenario stubbed).
- **References:** [`caut.md`](caut.md), `plan.md` §2.8 capability registry, `plan.md` Appendix C #11.

### `agent_mail` binary present but `--version` returns banner glyph

- **Symptom:** Capability registry shows `version: "╔══════════════════════..."` instead of a semver.
- **Root cause:** `agent_mail` does not implement `--version`; `probe_version`'s fallback (`--help | head -n 1`) caught the box-drawing border. The binary IS present and reachable; it just has no version flag.
- **Mitigation in Hoopoe:** Discriminating probe: prefer the MCP server's `health_check` (`{status, environment, http_host, http_port, database_url}`) and read the server-side version from there. The CLI `--version` is fallback only and adapter logic must not treat the banner string as a semver for compatibility checks.
- **Fixture coverage:** `golden-outputs/agent_mail/normal.json` exhibits the help-text capture; `unsupported-version.json` exercises the version-string probe path.
- **References:** [`agent_mail.md`](agent_mail.md), [`../research-spike/research-spike-notes.md`](../research-spike/research-spike-notes.md) §3 agent_mail.

### `lizard` missing → `health.complexity: missing` even though other complexity tools are installed

- **Symptom:** Health tab shows complexity unavailable even with `gocognit`, `radon`, `clippy::cognitive_complexity` etc. installed.
- **Root cause:** Generic-cross-language complexity capability (`health.generic.complexity`) is keyed off `lizard` specifically; per-language adapters (`health.go.complexity`, `health.python.complexity`) are independent.
- **Mitigation in Hoopoe:** Capability hierarchy is per-language → per-tool; UI rolls up at language level, not tool level. `health.generic.complexity` is only `ok` if `lizard` is present; `health.go.complexity` is `ok` independently if `gocognit` works. Hotspot scoring uses generic when language adapter unavailable.
- **Fixture coverage:** `golden-outputs/health/...` (forthcoming as adapters land); `scenarios/healthy-hour/capabilities.json` shows `health` block.
- **References:** [`health/generic.md`](health/generic.md), [`health/go.md`](health/go.md).

### `dcg` present but verdict transport unverified

- **Symptom:** `dcg --help` works, `dcg status --json` works, but no verdicts ever land in the approvals queue.
- **Root cause:** DCG is a Claude Code hook; verdicts only emit when Claude Code is the active CLI. If the swarm uses Codex CLI / Gemini CLI, DCG fires zero verdicts even though it's "installed."
- **Mitigation in Hoopoe:** Capability `dcg.verdicts.subscribe` reports `untested` until the daemon observes at least one verdict from a Claude Code session. Adapter contract test asserts the registry distinguishes "no verdict because no Claude Code in chain" from "verdict: allowed."
- **Fixture coverage:** `golden-outputs/dcg/normal.json` is `dcg status --json`; verdict-stream fixture pending real-VPS Claude Code session.
- **References:** [`dcg.md`](dcg.md).

### `oracle serve` present but Chrome session expired

- **Symptom:** Planning pipeline hangs; `oracle --remote-host` succeeds but the model returns auth-error HTML.
- **Root cause:** Oracle browser-mode is a Chrome cookie-jar driver. If the user is signed out of `chatgpt.com`, no API call from `oracle` will succeed, but the binary itself is alive.
- **Mitigation in Hoopoe:** Capability `oracle.browser.run` is probed via test-prompt before any planning request; if it fails, fall back to surface "Oracle session expired — re-sign-in to chatgpt.com on your Mac." Daemon never embeds the auth-error HTML in cost ledger.
- **Fixture coverage:** `golden-outputs/oracle/normal.json` (stub), `failure.json` (cookie-expired path forthcoming).
- **References:** [`oracle.md`](oracle.md), `plan.md` §7.1.

### `jsm` 401 → `jfp` fallback (capability source identity matters)

- **Symptom:** Skill installed but tending agents complain about SHA mismatch / drift.
- **Root cause:** `jsm` install with subscription gives SHA-pinned skills; `jfp` fallback gives advisory-versioned skills (no SHA). The skill *content* is the same; the integrity *guarantee* is different. Adapter authors that assume `sha256` is always non-null break.
- **Mitigation in Hoopoe:** `.hoopoe/skills.lock.json` records `source: jsm | jfp` and `sha256: <hex> | null`. Skill loader verify-step branches on `source`. Capability `jsm.skill.verify: ok` and `jfp.skill.verify: degraded` (advisory only) are both legal end states.
- **Fixture coverage:** `golden-outputs/jsm/normal.json` (with SHA), `golden-outputs/jfp/normal.json` (without SHA).
- **References:** [`jsm.md`](jsm.md), [`jfp.md`](jfp.md), `plan.md` §10.3 lock-file.

---

## 3. Parser-output drift across versions

### `br list --json` returns `{issues, total, ...}`; `br ready --json` returns a top-level array

- **Symptom:** Parser written for `{issues: [...], total: ...}` returns null when fed `br ready` output.
- **Root cause:** Different surfaces, intentionally different shapes (`br list` is paginated; `br ready` is not).
- **Mitigation in Hoopoe:** Separate parsers per surface. Type-driven so wrong-schema fails at compile time.
- **Fixture coverage:** `golden-outputs/br/normal.json` covers `br list`; per-surface fixtures land as the adapter expands.
- **References:** [`br.md`](br.md) "Read" command surfaces.

### `bv --robot-insights` PascalCase vs snake_case in other `--robot-*` outputs

- **Symptom:** Code looking for `bottlenecks` in `--robot-insights` finds nothing (it's `Bottlenecks` — capital B).
- **Root cause:** Older codepath in `bv` for `--robot-insights` uses `Bottlenecks`, `CriticalPath`, `Stats.PageRank`; newer surfaces (`--robot-plan`, `--robot-triage`, `--robot-priority`) use snake_case (`tracks`, `summary`, `recommendations`).
- **Mitigation in Hoopoe:** Per-surface schema, never assume shape consistency across `--robot-*`. Pin observed key sets in [`bv.md`](bv.md).
- **Fixture coverage:** `golden-outputs/bv/normal.json` is `--robot-triage`; per-surface fixtures grow as adapter is built.
- **References:** [`bv.md`](bv.md).

### `bv --robot-priority` returns `recommendations: null` (not `[]`) when nothing misaligned

- **Symptom:** Adapter parser written for `recommendations: []` errors on null.
- **Root cause:** Distinguishing "no signal" from "empty signal" is by `null`. Healthy graphs are `null`.
- **Mitigation in Hoopoe:** Adapter normalizes `null → []` at the type boundary; downstream code never sees null for collections.
- **Fixture coverage:** `golden-outputs/bv/normal.json` includes a `null`-recommendations capture (real graph is healthy).
- **References:** [`bv.md`](bv.md), AGENTS.md "Understanding Robot Output".

### `bv --robot-diff --diff-since <bad-ref>` exits 0 with stderr explanation

- **Symptom:** Adapter sees `exit: 0` and trusts the empty payload; downstream "no changes" rendering is wrong.
- **Root cause:** `bv` fails-open on bad refs (treats them as empty range, prints to stderr).
- **Mitigation in Hoopoe:** Always inspect stderr after `--robot-diff`; non-empty stderr means user-visible diagnostic.
- **Fixture coverage:** `golden-outputs/bv/normal.json` (good ref); failure-class capture pending.
- **References:** [`bv.md`](bv.md) "Failure modes".

### `ntm sessions list --json` returns `{sessions: null, count: 0}` not `{sessions: []}`

- **Symptom:** Parser written for `{sessions: []}` errors on `null`.
- **Root cause:** Go-style JSON marshaling of an unallocated slice yields `null`, not `[]`.
- **Mitigation in Hoopoe:** Adapter normalizes `null → []` at the type boundary.
- **Fixture coverage:** `golden-outputs/ntm/normal.json`.
- **References:** [`ntm.md`](ntm.md).

### `ntm --robot-tail` returns bytes, not lines

- **Symptom:** Adapter passes `--lines` style flag (doesn't exist); receives unexpected behavior.
- **Root cause:** `--robot-tail` operates on bytes for byte-addressable replay; `--max-bytes` is the correct cap.
- **Mitigation in Hoopoe:** Argv builder rejects `--lines`-style flags; only `--max-bytes` allowed.
- **Fixture coverage:** `golden-outputs/ntm/high-volume.json` shows `truncated: true`.
- **References:** [`ntm.md`](ntm.md).

### `ru sync --json` is NDJSON (one object per repo), not a JSON array

- **Symptom:** Parser using `JSON.parse(stdout)` errors because there are multiple top-level objects.
- **Root cause:** `ru` emits one JSON object per repo result; `--json` does not wrap in an array.
- **Mitigation in Hoopoe:** Adapter NDJSON parser per `ru sync` surface. `scripts/research-spike/lib/envelope.sh run_capture` detects NDJSON via `try fromjson` per-line and returns an array via `stdoutJson`.
- **Fixture coverage:** Detection covered by envelope unit tests; NDJSON fixture lands when real-VPS captures arrive.
- **References:** [`ru.md`](ru.md), `plan.md` §17.

### `br schema` is JSON Schema, **not** issue data

- **Symptom:** Adapter that calls `br schema` expecting issues sees no issues.
- **Root cause:** `br schema` returns the JSON Schema documents that `br --json` output validates against — useful for adapter validation, not issue listing.
- **Mitigation in Hoopoe:** Snapshot envelope captures both; integration contract names the distinction.
- **Fixture coverage:** `golden-outputs/br/normal.json` uses `br list` (correct).
- **References:** [`br.md`](br.md).

---

## 4. Agent Mail naming + tool renames

### `create_agent_identity` (extended) vs `register_agent` (core)

- **Symptom:** Agent author uses `create_agent_identity` from old docs; tool not found or behaves differently.
- **Root cause:** Two distinct tools cover identity creation. `register_agent` is the canonical core tool — idempotent (reusing the same `name` updates the profile and refreshes `last_active_ts`). `create_agent_identity` is the extended namespace tool — used to mint a brand-new globally-unique identity with no name collision check on existing agents.
- **Mitigation in Hoopoe:**
  - For session boot, prefer `macro_start_session(human_key, program, model, task_description)` — registers + fetches inbox in one call. Auto-generates name (adjective+noun, e.g. `FuchsiaStone`).
  - For controlled identity creation outside `macro_start_session`, use `register_agent` (with optional `name` for idempotency).
  - `create_agent_identity` is reserved for the rare case of forcing a new name when one might collide; not in the hot path.
- **Fixture coverage:** `golden-outputs/agent_mail/normal.json` is `--help`; MCP-tool-shape fixtures land in adapter contract tests.
- **References:** [`agent_mail.md`](agent_mail.md), `mcp-agent-mail` resource `resource://tooling/directory`.

### `human_key` slug derivation is one-way

- **Symptom:** Two agents using slightly different `human_key` strings (`/abs/path` vs `/abs/path/`) end up in *different* projects.
- **Root cause:** `ensure_project` slugifies `human_key` (lowercased, safe characters); trailing slashes / case differences create distinct slugs.
- **Mitigation in Hoopoe:** Daemon canonicalizes `human_key` to the project's absolute path *without* trailing slash; all callers go through the daemon, never raw MCP.
- **Fixture coverage:** `golden-outputs/agent_mail/normal.json`; the canonical `human_key` is `/home/ubuntu/Projects/hoopoeAppCockpit` (no trailing slash).
- **References:** [`agent_mail.md`](agent_mail.md) "ensure_project".

### `file_reservation_paths` accepts globs, not literal paths

- **Symptom:** Reserving `packages/fixtures/scenarios/healthy-hour/meta.json` doesn't conflict with another agent's reservation of `packages/fixtures/**`.
- **Root cause:** Reservations are pattern-based; broader patterns dominate. The conflict resolver compares pattern overlap, not literal path equality.
- **Mitigation in Hoopoe:** Adapter argv builder requires `paths` to be globs (validates with `**` / `*` markers); literal paths get a `**` suffix or `/` prefix automatically when ambiguous. Reservations also have TTLs (`ttl_seconds`) — never `0` (unbounded).
- **Fixture coverage:** `scenarios/wedged-pane/reservations.json` shows holder + pattern.
- **References:** [`agent_mail.md`](agent_mail.md) "file_reservation_paths", AGENTS.md "Reserve files before editing".

### `acknowledge_message` ≠ `mark_message_read`

- **Symptom:** Read-receipt timestamps appear without ack timestamps (or vice versa); other agents wait on an ack that already happened in spirit.
- **Root cause:** `mark_message_read` only sets `read_ts`; `acknowledge_message` (extended) sets both `read_ts` AND `ack_ts`. Messages with `ack_required: true` need the latter.
- **Mitigation in Hoopoe:** Adapter inspects `ack_required` on inbox messages; calls `acknowledge_message` (not `mark_message_read`) for any with `ack_required: true`.
- **Fixture coverage:** `scenarios/healthy-hour/agent-mail-dump.json` shows `ack_required: false` on every message; ack-required scenarios pending.
- **References:** [`agent_mail.md`](agent_mail.md) "Capability IDs".

---

## 5. NTM cursor / sequence semantics

### Sequence cursor is per-channel, not global

- **Symptom:** Replay-after-disconnect produces gaps because the daemon used a single global `seq` cursor.
- **Root cause:** NTM `serve` events have a per-channel sequence cursor (e.g. `swarm`, `agent_state`, `beads`, `caut`, `caam`, `audit`); each channel's `seq` increments independently.
- **Mitigation in Hoopoe:** Daemon stores `{channel, last_seq}` map per WS connection. On reconnect, sends `replayEvents(channel, seq)` per channel, not a single cursor. Hoopoe wraps NTM events in its own envelope with its own per-Hoopoe-channel `seq` so client reconnect is independent of NTM's cursor lifetime.
- **Fixture coverage:** `events.jsonl` in every scenario has per-channel `seq` values (`{"channel":"swarm","seq":1, ...}`, `{"channel":"agent_state","seq":2, ...}`, etc.).
- **References:** [`ntm.md`](ntm.md) "ntm serve (live mode)", `plan.md` §2.6 sequence cursor + replayEvents.

### Hoopoe wraps NTM `seq` with its own cursor (double-cursor pattern)

- **Symptom:** Client expecting `event.seq` from NTM sees Hoopoe's `seq` and disagrees with daemon-side telemetry.
- **Root cause:** Hoopoe daemon does not expose NTM's raw `seq` — it ingests NTM events and re-emits with its own `seq` cursored per Hoopoe channel. NTM's `seq` is preserved in `event.payload.ntmSeq` for forensics, not used by clients.
- **Mitigation in Hoopoe:** Adapter contract test asserts every Hoopoe-emitted event has `seq` (Hoopoe's), and NTM's `seq` is in `payload.ntmSeq` only.
- **Fixture coverage:** `events.jsonl` uses Hoopoe's seq; per-event `payload` would include NTM's `ntmSeq` once real-VPS captures arrive.
- **References:** [`ntm.md`](ntm.md) "Adapter notes", `plan.md` §2.6.

### NTM cursor expiry: replay window is bounded

- **Symptom:** After laptop sleeps overnight, client reconnects with `seq: 12345` but server has GC'd events past `seq: 23000`; replay returns `404 cursor too old`.
- **Root cause:** NTM keeps a bounded ring of events for replay (configurable; default ~24 h or N MiB). Beyond that, snapshot-on-reconnect is the only recovery.
- **Mitigation in Hoopoe:** On `404 cursor too old`, daemon falls back to snapshot (`ntm --robot-snapshot`) + clears the client's per-channel cursor. Client receives a `seq_reset` event so UI can re-render from snapshot. Audit records the reset.
- **Fixture coverage:** Failure-class fixture pending (`scenarios/cursor-stale/` future).
- **References:** [`ntm.md`](ntm.md) "Failure modes", `plan.md` §2.6.

### Pane crash notifications race the snapshot

- **Symptom:** UI shows pane "alive" but `.ntm/human_inbox/<date>_<time>_agent_crashed.md` already exists.
- **Root cause:** NTM async-writes crash notes to `.ntm/human_inbox/`; the snapshot may race between the write and the UI poll.
- **Mitigation in Hoopoe:** Activity panel ingests from Agent Mail (preferred) AND watches `.ntm/human_inbox/` (fallback); idempotent across both sources. Always poll `.ntm/human_inbox/` once after every `--robot-snapshot` to catch races.
- **Fixture coverage:** `scenarios/wedged-pane/expected-outcome.json` includes `agent.classified_wedged` event sequence.
- **References:** [`ntm.md`](ntm.md), [`agent_mail.md`](agent_mail.md).

---

## 6. Snapshot / probing infrastructure

### `--version` probe is ambiguous; falls back catch banner glyphs

- **Symptom:** Capability registry shows "version" strings that are help-banner box-drawing characters or ANSI escapes.
- **Root cause:** Tools without `--version` get probed via `--V` then `version` then `--help | head -n 1` — the last fallback catches the banner border.
- **Mitigation in Hoopoe:** Per-tool integration contract pins the canonical version probe; envelope's generic `probe_version` is best-effort and adapter contract tests assert version strings match expected regex.
- **Fixture coverage:** `golden-outputs/<tool>/unsupported-version.json` exercises the version-string probe path with synthesized `0.0.1`.
- **References:** [`scripts/research-spike/lib/envelope.sh`](../../scripts/research-spike/lib/envelope.sh) `probe_version`.

### `jq --argjson` overflows ARG_MAX on large captures

- **Symptom:** `Argument list too long` from `jq` when assembling `br schema` (~63 KB) or `ru --schema` (large).
- **Root cause:** Passing JSON via `--argjson` puts the value on the command line. ARG_MAX on Linux is typically 2 MiB; a single `br schema` doesn't exceed it but combining several adapter envelopes does.
- **Mitigation in Hoopoe:** `scripts/research-spike/lib/envelope.sh` uses `jq --rawfile` (for raw text) and `--slurpfile` (for parsed JSON), staging via tempfiles instead of argv. This was a real bug found and fixed during hp-6v3.
- **Fixture coverage:** `golden-outputs/<tool>/high-volume.json` documents the truncation contract.
- **References:** `scripts/research-spike/lib/envelope.sh` `run_capture`, `tool_close`.

### Stdout vs stderr discipline differs across tools

- **Symptom:** Adapter parses empty stdout; the actual JSON went to stderr.
- **Root cause:** No CLI-wide convention; some Go binaries emit JSON to stderr (e.g. progress) and prose to stdout, or vice versa.
- **Mitigation in Hoopoe:** Snapshot envelope captures both; adapter contract spec for each tool names which stream is the JSON authoritative source. Default assumption: stdout. If a tool is exception, document it in the per-tool contract.
- **Fixture coverage:** `golden-outputs/<tool>/normal.json` records both streams.
- **References:** every per-tool contract; pattern in [`README.md`](README.md).

### LANG / LC_ALL change CLI output for some tools

- **Symptom:** Adapter parses `git status` messages correctly in C locale, fails for non-English locales.
- **Root cause:** `git`, `cargo`, and other tools localize messages.
- **Mitigation in Hoopoe:** Daemon sets `LC_ALL=C.UTF-8` before invoking adapters; never inherits user locale for parsing. Snapshot script doesn't set this — research-spike captures user's locale to make it visible.
- **Fixture coverage:** Fixtures captured under `LC_ALL=C.UTF-8` in the daemon path (golden-outputs). Research-spike fixtures may show locale variance.
- **References:** every per-tool contract.

### Truncation cap (`ENVELOPE_MAX_BYTES`) bites on `br list --limit 250`

- **Symptom:** A real bead graph over ~250 issues with long descriptions hits 1 MiB cap; envelope reports `truncated: true`.
- **Root cause:** Default cap is 1 MiB. `br list` with `--limit 250 --json` against a real ACFS swarm exceeded it.
- **Mitigation in Hoopoe:** Adapter pages via `--limit` + `--offset` rather than raising `--max-bytes`. Capability registry warns when `truncated: true` shows up in any non-failure-fixture context.
- **Fixture coverage:** `golden-outputs/<tool>/high-volume.json` exercises the truncation path (synthetic 1 MiB).
- **References:** [`br.md`](br.md), `scripts/research-spike/lib/envelope.sh`.

---

## 7. Encoding / locale (small but real)

### `git log --pretty=format:'...'` has no trailing newline

- **Symptom:** Adapter splits stdout on `\n` and last entry merges with EOF.
- **Mitigation in Hoopoe:** Adapter splits and filters empty entries; or uses `%C(reset)%n` explicitly.
- **References:** [`git.md`](git.md).

### `git push -u origin HEAD` writes to stderr on success

- **Symptom:** Adapter treats stderr-non-empty as failure; flags successful pushes.
- **Mitigation in Hoopoe:** Trust git's exit code, never stderr-emptiness.
- **References:** [`git.md`](git.md).

---

## Cross-references

- Workflow + setup gotchas (concurrent builds, peer-staged files, ru state-store, etc.): [`../research-spike/gotchas.md`](../research-spike/gotchas.md).
- Per-tool integration contracts: [`README.md`](README.md) inventory.
- Snapshot envelope contract: [`../../scripts/research-spike/schema/snapshot.schema.json`](../../scripts/research-spike/schema/snapshot.schema.json).
- Capability registry shape: `plan.md` §2.8.
- Hard guardrails: `plan.md` Appendix C, `AGENTS.md` "Hoopoe Non-Negotiable Guardrails".
