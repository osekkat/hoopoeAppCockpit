# Research-spike gotchas + version skew (`hp-d54`)

> Surprises, undocumented behavior, and version skew observed while wrapping ACFS tools. **Read this before writing any adapter** in Phase 2.5+. New gotchas discovered while implementing get appended here in the same PR — do not let lessons rot.

This file is the structured catalog. The narrative goes in [`research-spike-notes.md`](research-spike-notes.md). Per-tool detail lives in [`docs/integration-contracts/<tool>.md`](../integration-contracts/).

---

## Source of entries

- **2026-05-02 local self-test** — gotchas surfaced by `scripts/research-spike/snapshot.sh --self-test` against the dev box. Marked `source: self-test 2026-05-02`.
- **(forthcoming) real-VPS captures** — gotchas surfaced once `hp-r7i` provisions a real ACFS VPS and `hp-jvm` runs the canonical 13-step wizard. Marked `source: vps <id>`.

Each entry has the same shape:

> **Symptom** — what the developer sees that surprises them.
> **Root cause** — why it happens.
> **Mitigation in Hoopoe** — what the adapter / daemon / UI does to handle it.
> **Fixture coverage** — `packages/fixtures/` paths exercising this.
> **References** — integration-contract file(s); cross-cuts to `plan.md` Appendix C guardrails.

---

## Bare-TUI invocations (Guardrail 1 + relatives)

### `bv` without flags launches a blocking interactive TUI

- **Symptom:** Agent session hangs; PTY shows interactive triage UI; nothing parseable on stdout.
- **Root cause:** Bare `bv` is a TUI, not a CLI. The `--robot-*` flags are the JSON surface.
- **Mitigation in Hoopoe:** `scripts/research-spike/snapshot.sh` explicitly invokes only `--robot-help`, `--robot-recipes`, `--robot-triage`, `--robot-plan`, `--robot-insights`, `--robot-priority`, `--robot-diff`. The capability registry pins `bv.tui: blocked-by-policy`. CI lint rule (Phase 1, `hp-rflj`-adjacent): grep for bare `bv` invocations in adapter code → fail build.
- **Fixture coverage:** `packages/fixtures/golden-outputs/bv/normal.json` (real `--robot-triage`); the registry assertion in every scenario's `capabilities.json` enforces `bv.tui` is never `ok`.
- **References:** [`docs/integration-contracts/bv.md`](../integration-contracts/bv.md), `plan.md` Appendix C #1, `AGENTS.md` "bv — Graph-Aware Triage Engine".
- **Source:** self-test 2026-05-02 (rule baked in from day 0; no near-miss yet because the script never invoked it).

### `ntm spawn` without flags is interactive

- **Symptom:** `ntm spawn` blocks waiting for input.
- **Root cause:** Some `ntm` subcommands prompt when not given full argv.
- **Mitigation in Hoopoe:** All NTM mutating actions go through the typed `ActionPlan` (`plan.md` §8.3.1) which constructs full argv with `--non-interactive` semantics. Snapshot script captures `ntm sessions list --json` only.
- **Fixture coverage:** `packages/fixtures/golden-outputs/ntm/normal.json` (real `--robot-snapshot`).
- **References:** [`docs/integration-contracts/ntm.md`](../integration-contracts/ntm.md).
- **Source:** observation from `vibing-with-ntm` skill + AGENTS.md.

### `ru sync` without `--non-interactive` prompts

- **Symptom:** `ru sync` hangs waiting for confirmation.
- **Root cause:** `ru sync` prompts for confirmation on certain repo states (dirty WIP, force operations) when not given `--non-interactive`.
- **Mitigation in Hoopoe:** Adapter argv builder MUST include `--non-interactive` AND `--json` for every sync/status invocation; rejects argv missing them at compile time (Go test).
- **Fixture coverage:** `packages/fixtures/golden-outputs/ru/normal.json` includes `argv: ["ru", "--schema"]` (a non-interactive surface) — the adapter test asserts other surfaces also pass `--non-interactive`.
- **References:** [`docs/integration-contracts/ru.md`](../integration-contracts/ru.md), `plan.md` §17 ru paragraph.
- **Source:** documented in `plan.md` §17.

---

## CLI version probes

### `agent_mail --version` returns the help banner border

- **Symptom:** `probe_version` captures `╔══════════════════════════════════════════════════════════════════════════════╗` as the version string.
- **Root cause:** `agent_mail` does not implement `--version`; the snapshot script's fallback (`--help | head -n 1`) catches the box-drawing border.
- **Mitigation in Hoopoe:** The MCP transport is preferred over the CLI; adapter probes via `mcp__mcp-agent-mail__health_check` (`{status, environment, http_host, http_port, database_url}`) and inspects the MCP server's response, not `agent_mail --version`. Snapshot script keeps the (incorrect) probe so the gotcha is visible in the corpus.
- **Fixture coverage:** `packages/fixtures/golden-outputs/agent_mail/normal.json` shows the help-text capture; `unsupported-version.json` exercises the version-string probe path.
- **References:** [`docs/integration-contracts/agent_mail.md`](../integration-contracts/agent_mail.md), `research-spike-notes.md` §3 "agent_mail".
- **Source:** self-test 2026-05-02.

### Some tools emit version to stderr, others to stdout

- **Symptom:** Adapter parses empty version string; the actual version went to stderr.
- **Root cause:** No CLI-wide convention; `git version` writes to stdout, some Go binaries write banner to stderr.
- **Mitigation in Hoopoe:** `envelope::probe_version` tries `--version` then `-V` then `version` and reads stdout only. If stdout is empty but stderr has it, snapshot envelope still records both — adapter consumers can look at both.
- **Fixture coverage:** `golden-outputs/<tool>/unsupported-version.json` exercises the `--version` probe path with a synthesized `0.0.1` response.
- **References:** [`scripts/research-spike/lib/envelope.sh`](../../scripts/research-spike/lib/envelope.sh) `probe_version`.
- **Source:** general observation.

---

## Output schemas

### `br list --json` returns `{issues, total, ...}`; `br ready --json` returns a top-level array

- **Symptom:** Parser written for `{issues: [...], total: ...}` returns null when fed `br ready` output.
- **Root cause:** The two surfaces have intentionally different shapes; `br list` is paginated, `br ready` is not.
- **Mitigation in Hoopoe:** Adapter has separate parsers for each surface. Type-driven so a wrong schema fails at compile time, not runtime.
- **Fixture coverage:** `golden-outputs/br/normal.json` (`br list` shape) — `br ready` shape lives in `scenarios/healthy-hour/br-list.json` is `br list`-style but each scenario can override; the adapter contract test asserts both shapes are recognized.
- **References:** [`docs/integration-contracts/br.md`](../integration-contracts/br.md) "Read" command surfaces.
- **Source:** self-test 2026-05-02 — surfaced while building snapshot.sh.

### `br schema` is JSON Schema, **not** issue data

- **Symptom:** Adapter that calls `br schema` expecting issues sees no issues.
- **Root cause:** `br schema` returns the JSON Schema documents that `br`'s `--json` output validates against — useful for adapter validation, not issue listing.
- **Mitigation in Hoopoe:** Snapshot envelope captures both; integration contract documents the distinction. Adapter consumers use `br list --json` for issue payloads.
- **Fixture coverage:** `golden-outputs/br/normal.json` has `argv: ["br", "list", ...]` (correct surface).
- **References:** [`docs/integration-contracts/br.md`](../integration-contracts/br.md).
- **Source:** self-test 2026-05-02.

### `bv --robot-insights` keys are PascalCase; other `--robot-*` are snake_case

- **Symptom:** Code looking for `bottlenecks` finds nothing in `--robot-insights`.
- **Root cause:** `--robot-insights` uses `Bottlenecks`, `CriticalPath`, `Stats.PageRank`; `--robot-plan` uses `tracks`, `summary`, `highest_impact`.
- **Mitigation in Hoopoe:** Adapter has per-surface schema; never assumes shape consistency across `--robot-*`.
- **Fixture coverage:** `golden-outputs/bv/normal.json` is `--robot-triage`; per-surface fixtures land as the adapter is built.
- **References:** [`docs/integration-contracts/bv.md`](../integration-contracts/bv.md).
- **Source:** self-test 2026-05-02.

### `ru sync --json` emits NDJSON, not a JSON array

- **Symptom:** Parser fails because output is multiple JSON objects newline-delimited, not `[{...}, {...}]`.
- **Root cause:** `ru` emits one JSON object per repo result; `--json` does not wrap in an array.
- **Mitigation in Hoopoe:** Adapter NDJSON parser per `ru sync` surface. Envelope's NDJSON detection (`scripts/research-spike/lib/envelope.sh` `run_capture`) handles this in fixtures.
- **Fixture coverage:** `golden-outputs/ru/normal.json` is `ru --schema` (single JSON document) — `ru sync` NDJSON fixture lands when real-VPS captures arrive.
- **References:** [`docs/integration-contracts/ru.md`](../integration-contracts/ru.md).
- **Source:** documented in `plan.md` §17.

---

## Interactivity / blocking shapes

### Pane-crash notifications written to disk async

- **Symptom:** UI shows pane "alive" but `.ntm/human_inbox/<date>_<time>_agent_crashed.md` already exists.
- **Root cause:** NTM async-writes crash notes; the snapshot may race between the write and the UI poll.
- **Mitigation in Hoopoe:** Activity panel ingests from Agent Mail (preferred) AND watches `.ntm/human_inbox/` (fallback); idempotent across both sources.
- **Fixture coverage:** `scenarios/wedged-pane/expected-outcome.json` includes `agent.classified_wedged` with the correct sequence.
- **References:** [`docs/integration-contracts/ntm.md`](../integration-contracts/ntm.md), [`docs/integration-contracts/agent_mail.md`](../integration-contracts/agent_mail.md).
- **Source:** observation from `.ntm/human_inbox/` artifacts on dev box (`2026-05-02_21-17-46_agent_crashed.md`).

### `ntm sessions list --json` returns `{sessions: null, count: 0}`, not `{sessions: [], count: 0}`

- **Symptom:** Parser written for `{sessions: []}` errors on `null`.
- **Root cause:** Go-style JSON marshaling of an unallocated slice yields `null`, not `[]`.
- **Mitigation in Hoopoe:** Adapter normalizes `null → []` at the type boundary; downstream code never sees `null` for collections.
- **Fixture coverage:** `golden-outputs/ntm/normal.json` (real capture; both shapes can occur depending on swarm state).
- **References:** [`docs/integration-contracts/ntm.md`](../integration-contracts/ntm.md).
- **Source:** self-test 2026-05-02 (observed `count: 0` with `sessions: null`).

---

## File-system / state-store

### `ru` keeps state in `~/.local/state/ru/**`

- **Symptom:** Hoopoe daemon and `ru` "agree" on a project state but later they disagree; root cause is `ru` mutated its state-store under `~/.local/state/ru/` between calls.
- **Root cause:** `ru` is a fully-featured tool with its own SQLite-ish session store; it's the source of truth for `ru` operations. Touching that store from Hoopoe creates a parallel source of truth (violates `plan.md` §1.1).
- **Mitigation in Hoopoe:** Hoopoe NEVER writes `~/.local/state/ru/`. Read-only via `ru status` only. Diagnostics surface `ru prune --archive` as an explicit user-approved repair (`plan.md` §10.2).
- **Fixture coverage:** `golden-outputs/ru/normal.json` is `--schema`; the adapter test asserts adapter never writes the state directory.
- **References:** [`docs/integration-contracts/ru.md`](../integration-contracts/ru.md), `plan.md` §17 ru paragraph.
- **Source:** documented in `plan.md` §17.

### Worktrees mandatory for health jobs (Guardrail 5)

- **Symptom:** Coverage / lint / format runs interfere with active agent work in the main worktree.
- **Root cause:** Health tools (`vitest --coverage`, `cargo llvm-cov`, `eslint --fix`) modify caches, write reports, sometimes mutate files.
- **Mitigation in Hoopoe:** Health jobs run in `~/.hoopoe/work/<project-id>/health/<run-id>/` via `git worktree add`. Never in the active agent working tree (Guardrail 5).
- **Fixture coverage:** `golden-outputs/health/<lang>/normal.json` (forthcoming as adapters land); the adapter test asserts `cwd` is under `~/.hoopoe/work/`.
- **References:** [`docs/integration-contracts/health/{ts,python,rust,go,generic}.md`](../integration-contracts/health/), `plan.md` Appendix C #5.
- **Source:** `plan.md` Appendix C.

### Cargo `target/` cache thrash on shared boxes

- **Symptom:** Multiple Rust projects on the same box invalidate each other's cargo build cache.
- **Root cause:** Cargo defaults to `<crate>/target/`; sharing across crates mid-flight causes lock contention and cache invalidation.
- **Mitigation in Hoopoe:** Adapter argv builder sets `CARGO_TARGET_DIR=$TMPDIR/rch_target_<basename-of-cwd>` per project. Pattern from AGENTS.md "RCH" section.
- **Fixture coverage:** `golden-outputs/health/rust.md` doc (forthcoming).
- **References:** [`docs/integration-contracts/health/rust.md`](../integration-contracts/health/rust.md), `AGENTS.md` "RCH — Remote Compilation Helper".
- **Source:** `AGENTS.md` cargo section + observed in cross-project rebuilds.

---

## Network / endpoints

### `ntm serve` port discovery is dynamic

- **Symptom:** Daemon connects to the wrong port and fails.
- **Root cause:** `ntm serve` chooses a port at startup (default-deny posture, `plan.md` §2.4); the port is recorded in systemd unit env or a state file, not a fixed value.
- **Mitigation in Hoopoe:** Daemon discovers via systemd or `ntm serve status`; never hard-codes.
- **Fixture coverage:** `golden-outputs/ntm/normal.json` (real `--robot-snapshot`); transport pinning lands when real-VPS captures arrive.
- **References:** [`docs/integration-contracts/ntm.md`](../integration-contracts/ntm.md), `plan.md` §2.4 default-deny.
- **Source:** general observation; pin on real VPS.

### MCP Agent Mail HTTP server is HTTP-only

- **Symptom:** Stdio / SSE attempts hang or fail.
- **Root cause:** The MCP server intentionally disables stdio and SSE transports — HTTP-only (per server log: "Transport is HTTP-only; stdio/SSE are intentionally disabled.").
- **Mitigation in Hoopoe:** Adapter uses MCP HTTP transport on `127.0.0.1:<port>` (default 8765); discovers via `resource://config/environment`.
- **Fixture coverage:** `golden-outputs/agent_mail/normal.json` is `--help`; transport pinning via `health_check` resource read.
- **References:** [`docs/integration-contracts/agent_mail.md`](../integration-contracts/agent_mail.md).
- **Source:** self-test 2026-05-02 (mcp-agent-mail server log line).

---

## Authentication / credentials

### CAAM switch-account is per-machine and side-effectful

- **Symptom:** Switching CAAM on a shared VPS changes which credential the next agent CLI invocation uses; teammates affected.
- **Root cause:** CAAM stores active-account state in user-scoped config; multiple agents on the same machine share it.
- **Mitigation in Hoopoe:** `caam.account.switch` is `blocked-by-policy` outside the typed `ActionPlan` surface; per-VPS coordination via Activity panel + audit log (Guardrail 10) ensures team awareness.
- **Fixture coverage:** `scenarios/rate-limited-with-caam/expected-outcome.json` exercises the switch-account ActionPlan + approval flow.
- **References:** [`docs/integration-contracts/caam.md`](../integration-contracts/caam.md).
- **Source:** documented in `plan.md` §7.3, §8.4.

### No provider API keys anywhere (Guardrail 11)

- **Symptom:** Adapter author writes `OPENAI_API_KEY` config field; CI fails the build.
- **Root cause:** Hoopoe is subscription-only by design — `OPENAI_API_KEY`, `ANTHROPIC_API_KEY`, `GEMINI_API_KEY` config fields and provider SDK imports (`openai`, `@anthropic-ai/sdk`, `@google/generative-ai`) are forbidden in `apps/daemon/` and `apps/desktop/`.
- **Mitigation in Hoopoe:** CI lint rule (`hp-ara`) fails build on import. Adapter contracts ([`oracle.md`](../integration-contracts/oracle.md), [`caam.md`](../integration-contracts/caam.md), [`caut.md`](../integration-contracts/caut.md)) reiterate the position.
- **Fixture coverage:** No fixture should contain those strings — fixture-quality test (`hp-pl5o`) asserts grep is empty.
- **References:** `plan.md` Appendix C #11, AGENTS.md Guardrail 11.
- **Source:** `plan.md` Appendix C, design from day 0.

---

## Output volume / truncation

### `br schema` and `ru --schema` produce multi-KB / multi-100-KB JSON

- **Symptom:** Naive parser blows ARG_MAX or out-of-memory on a buffer-everything strategy.
- **Root cause:** Both surfaces emit substantial documents; `br schema` ~63 KB, `ru --schema` larger.
- **Mitigation in Hoopoe:** Snapshot envelope uses `jq --rawfile` / `--slurpfile` to file-stage rather than argv-stage (`scripts/research-spike/lib/envelope.sh` `run_capture`). Adapter parsers stream when memory matters.
- **Fixture coverage:** `golden-outputs/br/normal.json` is `br list` (smaller); `golden-outputs/ru/normal.json` is `ru --schema` (large) — exercises the high-volume path.
- **References:** [`docs/integration-contracts/br.md`](../integration-contracts/br.md), [`docs/integration-contracts/ru.md`](../integration-contracts/ru.md).
- **Source:** self-test 2026-05-02 (initial snapshot.sh ran into ARG_MAX with `--argjson`; refactored to `--rawfile`/`--slurpfile`).

### `ubs --ci .` scans the whole tree; multi-second on real repos

- **Symptom:** Round-0 review takes longer than expected on monorepos.
- **Root cause:** UBS scans every file by default in `--ci` mode.
- **Mitigation in Hoopoe:** Adapter prefers `ubs <changed-files>` per Round semantics (per `plan.md` §7.4.2 review rounds); whole-tree scan reserved for Round 0 + Round 5 only.
- **Fixture coverage:** `golden-outputs/ubs/normal.json` is `ubs --help` capture (cheap); whole-tree fixtures land per-scenario.
- **References:** [`docs/integration-contracts/ubs.md`](../integration-contracts/ubs.md), AGENTS.md UBS section.
- **Source:** self-test 2026-05-02.

---

## Encoding / locale

### LANG / LC_ALL change CLI output for some tools

- **Symptom:** Adapter parses `git status` correctly in C locale but fails for non-English locales.
- **Root cause:** `git`, `cargo`, and other tools localize messages.
- **Mitigation in Hoopoe:** Daemon sets `LC_ALL=C.UTF-8` before invoking adapters; never inherits user locale for parsing.
- **Fixture coverage:** Fixtures captured under `LC_ALL=C.UTF-8` by convention; adapter test asserts no localized substrings appear.
- **References:** every adapter contract; common pattern.
- **Source:** general practice; pin on real-VPS captures.

---

## Workflow (multi-agent)

### Concurrent build cache contention on `~/.cache/{bun,turbo,yarn}`

- **Symptom:** Multiple agents launching `bun install` or `bun run build` simultaneously corrupt or invalidate caches; builds fail or stall.
- **Root cause:** Shared cache dirs without per-agent isolation.
- **Mitigation in Hoopoe:** Coordinate via Agent Mail `[hoopoe-builds]` thread (per per-pane runbook STEP 9). Prefer per-package builds. Daemon-side build queue (`§2.7`) serializes when configured.
- **Fixture coverage:** `scenarios/healthy-hour/build-logs/` shows passing concurrent builds; future scenarios may exercise contention.
- **References:** AGENTS.md STEP 9 (concurrent build contention), per-pane runbook.
- **Source:** AGENTS.md, observed in this very session.

### Peer agents stage files via `git add .` — shows up in your status

- **Symptom:** `git status` shows files you didn't touch as `Changes to be committed`.
- **Root cause:** A peer agent is mid-commit; their `git add` ran but `git commit` hasn't yet.
- **Mitigation in Hoopoe (per AGENTS.md "Note for Codex/GPT-5.5"):** Treat unfamiliar staged or modified files as your own — never stash, revert, or unstage them. Use `git commit <pathspec>` to limit your commit to your own files.
- **Fixture coverage:** N/A (workflow gotcha, not adapter).
- **References:** AGENTS.md "Note for Codex/GPT-5.5".
- **Source:** AGENTS.md + observed multiple times in this session.

---

## Adding new gotchas

When you hit a surprise during adapter implementation:

1. Append a new entry under the right section here, in the standard shape.
2. If it cross-references a per-tool contract, edit that contract's "Known gotchas" pointer too.
3. If the gotcha is reproducible in a fixture, add the fixture path to "Fixture coverage."
4. Same PR — do not let the lesson slip into someone's head only.

If the gotcha is significant enough to change `plan.md`, propose the edit in the same PR and link from `research-spike-notes.md` "Decisions made on the VPS that should land in the plan."
