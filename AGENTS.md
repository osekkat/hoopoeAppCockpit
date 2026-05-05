# AGENTS.md — Hoopoe

> Guidelines for AI coding agents working in the Hoopoe codebase.
> **`plan.md` is the authoritative strategic document.** When this file and `plan.md` disagree, `plan.md` wins; fix this file.

---

## RULE 0 - THE FUNDAMENTAL OVERRIDE PREROGATIVE

If I tell you to do something, even if it goes against what follows below, YOU MUST LISTEN TO ME. I AM IN CHARGE, NOT YOU.

---

## RULE NUMBER 1: NO FILE DELETION

**YOU ARE NEVER ALLOWED TO DELETE A FILE WITHOUT EXPRESS PERMISSION.** Even a new file that you yourself created, such as a test code file. You have a horrible track record of deleting critically important files or otherwise throwing away tons of expensive work. As a result, you have permanently lost any and all rights to determine that a file or folder should be deleted.

**YOU MUST ALWAYS ASK AND RECEIVE CLEAR, WRITTEN PERMISSION BEFORE EVER DELETING A FILE OR FOLDER OF ANY KIND.**

---

## Repository Reality Check (Authoritative)

This repository is **Hoopoe**, a macOS Electron desktop **cockpit** for the Agentic Coding Flywheel (ACFS). It is not the engine — it is the cockpit that wraps existing Flywheel tools (`br`, `bv`, `ntm`, Agent Mail, ACFS, CAAM, DCG, CASS, Git, agent CLIs) without replacing them. See `plan.md §0 Executive thesis` and `§1.1 Preserve native sources of truth`.

### Current state (early — pre-Phase-1)

This checkout is in the planning phase. As of this writing the only authoritative artifacts are:

- `plan.md` — strategic plan (vision, principles, architecture, decisions, roadmap). Read it before non-trivial work.
- `plan.full.md` — preserved earlier full version.
- `design/mockups/v1/` — pre-Phase-1 visual sketches.
- `design/DECISIONS.md` — design-vs-plan conflicts ledger.
- `.beads/` — bead tracking via `br`.

The monorepo layout described below (`apps/desktop`, `apps/daemon`, `packages/schemas`, `packages/design-system`) is the **target** structure per `plan.md §12 Phase 1`. It does not exist on disk yet. Do not invent code paths that aren't there; if a task needs scaffolding, it is Phase 1 work and should be discussed first.

### Target workspace shape (forward-looking, per plan.md)

| Path                                | Purpose                                                                                       |
| ----------------------------------- | --------------------------------------------------------------------------------------------- |
| `apps/desktop/`                     | Electron + TypeScript + React desktop app (the cockpit UI the user sees)                      |
| `apps/daemon/`                      | Go daemon that runs on the user's VPS (the API facade over the Flywheel toolchain)            |
| `packages/schemas/`                 | OpenAPI + shared types; generates a TS client and Go types so daemon and desktop never drift  |
| `packages/design-system/`           | Design tokens + Storybook + reusable components (`StageHeader`, `BeadCard`, `AgentTile`, ...) |
| `apps/desktop/src/vendored/t3code/` | Code lifted from `github.com/pingdotgg/t3code` (MIT) per Appendix B; **do not edit in place** |
| `docs/`                             | Architecture refs (`source-of-truth.md`, `security.md`, `process-manager.md`, ADRs)           |
| `design/mockups/v1/`                | Pre-Phase-1 visual sketches; design choices in `design/DECISIONS.md`                          |
| `.beads/`                           | Bead tracking (managed by `br`)                                                               |

If any later section in this file references repository structure that does not yet exist, treat `plan.md` and the on-disk tree as authoritative.

---

## Irreversible Git & Filesystem Actions — DO NOT EVER BREAK GLASS

1. **Absolutely forbidden commands:** `git reset --hard`, `git clean -fd`, `rm -rf`, or any command that can delete or overwrite code/data must never be run unless the user explicitly provides the exact command and states, in the same message, that they understand and want the irreversible consequences.
2. **No guessing:** If there is any uncertainty about what a command might delete or overwrite, stop immediately and ask the user for specific approval. "I think it's safe" is never acceptable.
3. **Safer alternatives first:** When cleanup or rollbacks are needed, request permission to use non-destructive options (`git status`, `git diff`, `git stash`, copying to backups) before ever considering a destructive command.
4. **Mandatory explicit plan:** Even after explicit user authorization, restate the command verbatim, list exactly what will be affected, and wait for a confirmation that your understanding is correct. Only then may you execute it — if anything remains ambiguous, refuse and escalate.
5. **Document the confirmation:** When running any approved destructive command, record (in the session notes / final response) the exact user text that authorized it, the command actually run, and the execution time. If that record is absent, the operation did not happen.

## Hoopoe Non-Negotiable Guardrails (from plan.md Appendix C)

Internalize these. They are load-bearing decisions and a failed grep on any of them is a failed PR.

1. **Do not parse bare `bv` output; use robot surfaces only.** Bare `bv` launches a TUI and blocks; always use `bv --robot-*`.
2. **Do not expose arbitrary shell execution from the renderer or normal daemon API.** All project-level commands flow through typed daemon RPCs (§5.3).
3. **Do not let the desktop local clone become a write target.** It is a read-only sync-driven mirror of origin (§1.7, §7.7). Staging, committing, branching, merging, pushing all go through the daemon.
4. **Do not let local SQLite/IndexedDB cache become canonical.** Caches are reconciled against canonical tool state (§2.2). Canonical wins.
5. **Do not run health/coverage jobs inside the active agent working tree by default.** Health jobs run in a dedicated worktree under `~/.hoopoe/work/<project-id>/health/<run-id>/` (§7.4.1).
6. **Do not make provider automation block existing-VPS onboarding.** Existing-VPS first; provider plugins (Contabo/OVH/etc.) ship later (§6.2).
7. **Do not start a large swarm without showing build/test contention, budget, rate-limit, and ready-frontier warnings.**
8. **Do not let terminal output be the source of truth for bead/agent/mail state when structured APIs exist.** Read from `br`, `bv`, NTM, Agent Mail (§1.3).
9. **Do not wake tending LLM jobs when deterministic pre-scripts find nothing actionable.** `wakeAgent: false` is the default for healthy ticks (§8.3).
10. **Do not suppress audit entries just because a job returned `[SILENT]`.** Activity panel suppresses; audit log always records (§8.3, §10).
11. **Do not call provider APIs directly.** Every model reach goes through Claude Code / Codex CLI / Gemini CLI (subscription-backed CLIs) or `oracle --engine browser` (ChatGPT Pro web). Specifically forbidden in `apps/daemon/` and `apps/desktop/`:
    - **(a) Env-var references**: `OPENAI_API_KEY`, `ANTHROPIC_API_KEY`, `GEMINI_API_KEY` (or any other provider API-key env var) in code, config-loading, or `.env*` files.
    - **(b) SDK imports**: `import openai`, `from "openai"`, `from '@anthropic-ai/sdk'`, `from '@google/generative-ai'`, `require("openai")`, `import "github.com/sashabaranov/go-openai"`, or any equivalent that pulls a provider SDK into the runtime.
    - **(c) Manifest entries**: those SDKs in `package.json` (deps/devDeps), `go.mod`, or any vendored copy.

    **Allowed**: provider-name labels (`openai`, `anthropic`, `claude`, `gemini`) appearing in redaction fixtures (`apps/daemon/internal/redaction/`), test data, code comments, docs, and user-facing subscription-status UI strings — these are intentional matches that the redaction layer must detect, not direct API calls.

    **CI gate**: a raw substring grep for `openai` is too broad and would fail on the benign label hits above. The canonical CI check uses anchored patterns (env-var names + import-context regexes + manifest entries) — see `scripts/providerlint/check-provider-sdks.ts` (§5.1, §7.1, §13).
12. **Do not surface raw terminal panes in the default swarm UI.** PTY plumbing exists on the daemon side for tending and forensics; the user-visible Swarm dashboard shows bead state + agent state + Activity panel only. Terminal scrollback is reachable from Diagnostics behind an explicit, audited "Show raw pane" toggle, never from the default agent grid (§7.3).

### Plus the seven product principles (plan.md §1)

- **§1.1** Preserve native sources of truth (wrap, don't replace).
- **§1.2** Desktop is not the orchestrator of record — VPS daemon owns long-running jobs.
- **§1.3** Robot/API surfaces first, shell parsing last.
- **§1.4** Every automation must be inspectable (audit who/what/why/when).
- **§1.5** Build for restartability.
- **§1.6** Make the first successful run boring (existing-VPS first).
- **§1.7** Sync-driven mirrors allowed; parallel sources of truth not.
- **§1.8** Tend agents with skill-attached jobs, not bespoke loops — `vibing-with-ntm` is loaded into agent context at runtime, never reimplemented in Go.

---

## Toolchain

Hoopoe is a polyglot monorepo. Use the right toolchain for the surface you're touching.

### Desktop (`apps/desktop/`)

- **Runtime:** Electron + TypeScript + React + Vite + Tailwind (custom design tokens).
- **Package manager:** Bun + Turbo workspaces.
- **State / routing / editing:** TanStack Router (typed stage routes), TanStack Query (server cache), Zustand (ephemeral UI state), CodeMirror 6 (plan editor), xterm.js (Diagnostics "Show raw pane" debug toggle only — see Guardrail 12), React Flow (DAG).
- **Secrets:** macOS Keychain via Electron `safeStorage`. Local cache in SQLite or IndexedDB (read-only mirror, never canonical — Guardrail 4).
- **Effect framework is NOT adopted.** T3 Code's server is Effect-everywhere; Hoopoe lifts patterns (PubSub change streams, atomic file writes, sequence cursors, semaphore-guarded ops) in plain TypeScript at lower cognitive cost. See plan.md §3.

### Daemon (`apps/daemon/`)

- **Runtime:** Go (chi/echo HTTP, modernc SQLite, gorilla/nhooyr WebSocket, creack/pty fallback).
- **Why Go:** static cross-compiled binary (single-file deploy over SSH), `Type=notify` systemd integration, mature long-lived control-plane idioms (kubelet, containerd, Tailscale, Caddy). Same family as NTM (also Go). See plan.md §3.
- **Greenfield.** Auth/settings/protocol _shapes_ are taken from t3code; the implementation is ours.

### Shared (`packages/schemas/`)

- OpenAPI is the source of truth; generates the TS client and Go types.
- **No hand-maintained duplicate shape definitions** across desktop and daemon.

### Source provenance: the t3code lift

Desktop scaffolding (Electron lifecycle, auth, settings, keybindings, build pipeline) is **vendored from `github.com/pingdotgg/t3code` (MIT)**, adapted for Hoopoe's remote-daemon shape. See plan.md Appendix B for the full file inventory.

- Lifted files land under `apps/desktop/src/vendored/t3code/` with the MIT notice preserved at the top of each file.
- **Do not edit `vendored/` files in place** except for mechanical mass renames (e.g., `T3CODE_*` → `HOOPOE_*`, branding strings). Adaptations live in our own files that import from `vendored/`. Keep the diff against upstream small enough to re-merge later.
- T3 Code's 2,175-line `apps/desktop/src/main.ts` must be **decomposed on day one** into `BackendLifecycle`, `UpdateMachine`, `IpcRegistry`, `WindowManager`, `SettingsBridge`, `AuthBridge` — do not inherit the monolith.
- See plan.md Appendix B "Anti-patterns to refuse" before writing new code in vendored areas.

---

## Code Editing Discipline

### No Script-Based Changes

**NEVER** run a script that processes/changes code files in this repo. Brittle regex-based transformations create far more problems than they solve.

- **Always make code changes manually**, even when there are many instances.
- For many simple changes: use parallel subagents.
- For subtle/complex changes: do them methodically yourself.

### No File Proliferation

If you want to change something or add a feature, **revise existing code files in place**.

**NEVER** create variations like:

- `mainV2.ts`
- `daemon_improved.go`
- `BackendLifecycle.enhanced.ts`

New files are reserved for **genuinely new functionality** that makes zero sense to include in any existing file. The bar for creating new files is **incredibly high**.

---

## Backwards Compatibility

Hoopoe is in early development with no users. Do things the **RIGHT** way with **NO TECH DEBT**.

- Never create "compatibility shims" or feature flags for non-existent legacy behavior.
- Never create wrapper functions for deprecated APIs.
- Just fix the code directly.
- Exception: API/event/config schema versions are real — bump them and migrate, don't pretend they aren't there. See plan.md §10.3.

---

## Verification After Changes (CRITICAL)

**After any substantive code changes, you MUST verify no errors were introduced.** The exact commands depend on the surface; the on-disk `package.json` / `Cargo.toml` / `go.mod` / Turbo config is authoritative when these examples drift.

### Desktop / TypeScript surface

```bash
bun run typecheck
bun run lint
bun run test
bun run build
```

### Daemon / Go surface

```bash
cd apps/daemon
go build ./...
go vet ./...
golangci-lint run
go test ./...
```

### Shared schemas

```bash
bun run --cwd packages/schemas generate   # regenerate TS client + Go types
bun run --cwd packages/schemas validate
```

If you see errors, **carefully understand and resolve each issue.** Read sufficient context to fix them the RIGHT way — silencing a lint or skipping a test without root-causing it is a failure.

---

## Testing

Hoopoe must be testable without a real VPS. **Mock Flywheel Mode** is required for development and demos (plan.md §13): a fixture-backed daemon/adapter layer that replays canonical-state snapshots, event streams, pane logs, Agent Mail messages, reservations, build/test outputs, and health snapshots.

### Test categories to expect

| Surface                    | Tests                                                                                                                                                                                                                |
| -------------------------- | -------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| Desktop unit               | Component tests, store/hook tests, schema validation                                                                                                                                                                 |
| Daemon unit                | Adapter parsers, scheduler invariants (§8.3.2), action executor, capability registry                                                                                                                                 |
| Adapter contract tests     | Golden fixtures for normal output, missing tool, unsupported version, malformed JSON, timeout, high-volume output (plan.md §18.3)                                                                                    |
| Tending evaluation harness | Required fixtures from §8.8: healthy hour, idle-but-not-stuck, wedged pane, rate-limited (with/without CAAM), stale reservation, budget breach, skill drift, missing tool, postcondition failure, action arbitration |
| Integration / E2E          | Phase 0 fixtures snapshotting Git, beads, `bv`, NTM, Agent Mail, reservations, health (plan.md §16)                                                                                                                  |
| Chaos / fault-injection    | Tunnel drop, daemon restart, disk pressure, slow renderer, malformed adapter output, long-running scheduler job (plan.md §10.5)                                                                                      |

### Tests assert capabilities, not just parser success

A fixture that parses but cannot satisfy the declared capability must not mark the feature as available (plan.md §2.8). Every adapter reports `/v1/capabilities` and stage routes are gated on capability IDs, not on optimistic "tool is installed."

### Phase 0 is mandatory before adapter-dependent feature work

For `br`, `bv`, `ntm`, and Agent Mail, Phase 0 fixtures captured against a real ACFS VPS are mandatory before features depend on the adapter (plan.md §16, §18.3). Do not write an adapter from imagined output shapes.

---

## Architecture Reference (per plan.md)

### Strategic constraint

> **Hoopoe is the cockpit, not the engine. The VPS is the execution plane. The existing Flywheel tools remain the source-of-truth systems.**

### Four stages + Activity panel

The product is `STAGE N — VERB` chrome:

```
01 Planning      — chat-box → 3-4 candidate models → comparative matrix → synthesis → fresh-eyes critique → 4-5 refinement rounds
02 Beads         — locked plan → `br` beads with traceability map, polish rounds, Kanban/DAG
03 Swarm         — NTM agent launch with composition picker; bead board + agent grid + Activity panel only (NO terminals — Guardrail 12)
04 Debugging /
   Hardening    — code health metrics, review rounds (UBS first), finding tracker, convergence detector
```

The **Activity panel** is a cross-stage drawer (not a stage); it hosts agent↔agent mail and user↔orchestrator chat. The orchestrator the user chats with is the literal `orchestrator-chat` tending agent (plan.md §7.5, §8.4).

### System architecture

`Client → Tunnel → Daemon → Toolchain.` SSH tunnel is the v1 default; mTLS direct mode is post-v1. Three-token auth: pairing → bearer → WS-token. Sequence-cursor + snapshot-on-reconnect. See plan.md §2.

### The daemon is an API facade, not a new canonical database

The daemon owns Hoopoe job state, event log, read-model cache, UI preferences, plan metadata Hoopoe creates, onboarding state, health snapshots Hoopoe generates, workflow audit events. It does **not** own bead truth, Git truth, NTM session truth, Agent Mail truth, file reservation truth, or test-report truth — those come from the project's own tools (plan.md §2.2, §1.1).

### Tending: scheduler + skill-attached jobs (plan.md §1.8, §8)

Four cleanly-separated layers:

```
Layer 1: Scheduler (Go)               — cron + interval + event triggers + on-demand
Layer 2: Pre-script (Go, per job)     — cheap mechanical reconcile; emits {wakeAgent: bool, context: {...}}
Layer 3: Agent runtime                — spawns agent with skills loaded; emits typed ActionPlans
Layer 4: Skills (content)             — vibing-with-ntm, ntm; pinned via `jsm` (preferred) or `jfp` (fallback)
```

`wakeAgent: false` keeps healthy hours at zero LLM cost; `[SILENT]` keeps the Activity panel quiet; **audit always fires regardless** (Guardrail 10). Mutating actions go through a typed `ActionPlan` (§8.3.1); the daemon — not the model — is the executor, with policy + idempotency + approvals + postcondition verification against canonical state.

### Subscription-only model access (plan.md §7.1, §13, Guardrail 11)

Every model reach goes through one of: Claude Code (Claude Max), Codex CLI (GPT Pro), Gemini CLI (Gemini Ultra), or `oracle --engine browser` (ChatGPT Pro web). **No BYOK. No direct provider APIs. No `OPENAI_API_KEY` / `ANTHROPIC_API_KEY` / `GEMINI_API_KEY` config field anywhere.** CI runs `scripts/providerlint/check-provider-sdks.ts` which scans `apps/daemon/` and `apps/desktop/` for the three categories above (env-var references in code/config, SDK imports in import-context, manifest entries in `package.json`/`go.mod`). Provider-name labels in redaction fixtures and user-facing subscription UI strings are explicitly allowed.

### Local code clone (plan.md §7.7)

The desktop maintains a sync-driven Git clone of each project at `~/Library/Application Support/Hoopoe/projects/<project-id>/repo/`, fetched **from origin** (not from the VPS). It powers fast file reads, diffs, blame, ripgrep. Live VPS-WIP overlay (unpushed commits, modified files) comes from daemon RPCs. The clone is **never** a write target through Hoopoe (Guardrail 3).

### Push policy (plan.md §7.3)

Origin is the canonical source of truth for code. Agents push their bead's branch after every commit (or at minimum after every successful test run). The daemon enforces this via a post-commit auto-push hook installed at swarm-launch time, with audit on every push attempt and surfacing of push failures in the Activity panel.

### Source of truth table (plan.md §1.1)

When in doubt about who owns what:

- **Code (canonical):** origin (GitHub/GitLab/etc.).
- **Code (VPS working state):** VPS clone at `/data/projects/<project>/`.
- **Code (desktop sync mirror):** local clone at `~/Library/Application Support/Hoopoe/projects/<project-id>/repo/` (read-only).
- **Plans:** markdown files in repo, under `.hoopoe/plans/<plan-id>/`.
- **Beads:** `br` / `.beads`.
- **Bead graph intelligence:** `bv --robot-*`.
- **Swarm sessions:** `ntm` + tmux.
- **Agent communication:** Agent Mail.
- **Build/test execution:** `rch`, NTM pipelines, language-native runners.
- **Swarm tending methodology:** `ntm` + `vibing-with-ntm` skills, loaded into tending agents at runtime — **not reimplemented in Go**.
- **Safety approvals:** NTM/DCG/SLB. DCG verdicts are _ingested_ into Hoopoe's unified approvals queue, not run as a parallel guard.
- **LLM provider account credentials:** CAAM (sole credential pathway).
- **Subscription usage:** `caut` (per-provider quota).

---

## Roadmap (plan.md §12)

The current immediate work is **Phase 0** (research spike on a real ACFS VPS). Subsequent phases are sequenced — do not skip ahead. See plan.md §16 for immediate first engineering tasks.

```
Phase 0  — Research spike + integration contract (real ACFS VPS, JSON snapshots, parser fixtures)
Phase 1  — Monorepo + desktop shell + lifted t3code scaffolding
Phase 2  — VPS connection, auth, daemon skeleton
Phase 2.5 — API/process contract hardening (do BEFORE Phase 3+)
Phase 3  — ACFS onboarding and tool inventory
Phase 4  — Project registry, Git, desktop local clone
Phase 5  — Planning workspace
Phase 6  — Bead conversion and quality tracker
Phase 7  — Kanban, DAG, Force views
Phase 8  — Swarm launch MVP (composition picker, abstracted dashboard, no terminals)
Phase 9  — Activity panel and Agent Mail
Phase 10 — Tending scheduler + initial job set
Phase 11 — Debugging / Hardening: code health metrics
Phase 12 — Debugging / Hardening: review rounds and convergence
Phase 13 — Provider automation and production polish
```

---

## CI/CD Pipeline

CI exists. Three workflows live in `.github/workflows/`:

- **`ci.yml`** — PR-blocking gate on push to `master` and pull requests to `master`. Two jobs on `ubuntu-latest`:
  - `lint-typecheck-test`: `bun install --frozen-lockfile`, then `bun run lint`, `bun run typecheck`, `bun run test`.
  - `e2e` (depends on the lint job): installs Playwright + system deps, then `bun run e2e` (smoke + hp-j30 suites). Uploads HTML reports as artifacts on every run.
- **`release.yml`** — tag-triggered (`v*.*.*`) + `workflow_dispatch` (channel: stable | nightly). macOS-only (`macos-14`). Three sequential jobs: `preflight` (lint + typecheck + test + version metadata) → `build` (matrix: macOS arm64 + macOS x64; signed/notarized DMG via `bun scripts/build-desktop-artifact.ts`; required secrets: `CSC_LINK`, `CSC_KEY_PASSWORD`, `APPLE_API_KEY`, `APPLE_API_KEY_ID`, `APPLE_API_ISSUER`) → `release` (publishes the GitHub Release with DMGs + blockmaps + electron-updater manifests, using `GH_TOKEN`). Originally lifted from t3code (commit `460d9c3`) and adapted for Hoopoe's Mac-only v1.
- **`schemas-codegen-drift.yml`** — focused codegen-drift gate, path-filtered to `packages/schemas/**` and `apps/desktop/src/shared/ipc-contract.gen.ts`. Validates TS codegen, Go codegen, and the preload-API contract drift; runs schemas typecheck + tests; vets and builds the generated Go module. Surfaces `*.drift` artifacts on failure.

The provider-SDK-import lint rule (Guardrail 11) **is** a hard CI gate today — `bun run lint` chains `lint:provider-sdks`, `lint:renderer-isolation`, `lint:codex-shape-scrub`, `lint:no-raw-logging`, `lint:redact-drift` before `oxlint`, and `bun run test` runs the corresponding `*.test.ts` for each lint script before `turbo run test`. All five gates run in `ci.yml` and `release.yml`'s preflight.

The daemon's `go vet ./...` and `go test ./...` reach CI through Turbo: `apps/daemon/package.json` exposes them as `typecheck` and `test`, and `bun run typecheck` / `bun run test` fan out via `turbo run typecheck` / `turbo run test`.

Open follow-ups (per plan.md §11, Appendix B):

- `golangci-lint run` is documented under "Verification After Changes" but is not wired into any workflow yet. Add it to `ci.yml` (or the daemon's Turbo `lint` task) so PRs fail on the same lint rules a developer runs locally.
- Capability tests need to assert `/v1/capabilities` (plan.md §2.8), not just parser success — partial coverage today; expand as Phase 3 ACFS onboarding adapters land.

---

## Release Process

The desktop release surface has landed (Phase 1). The daemon-binary release surface is still forward-looking (Phase 2).

**Shipped — desktop (`release.yml`):**

- macOS signed/notarized DMG (`arm64` + `x64`) built by `bun scripts/build-desktop-artifact.ts`.
- `electron-updater` against GitHub Releases. Stable channel triggers on `v*.*.*` tag pushes; the `nightly` channel is reachable via `workflow_dispatch` and produces `0.0.0-nightly.<YYYYMMDD>.<run>` versions marked as prereleases.
- Required secrets: `CSC_LINK`, `CSC_KEY_PASSWORD`, `APPLE_API_KEY`, `APPLE_API_KEY_ID`, `APPLE_API_ISSUER`, and a `GH_TOKEN` for publishing. The build job hard-fails if signing/notarization secrets are missing.
- Publishes `*.dmg`, `*.zip`, `*.blockmap`, and the `*-mac.yml` electron-updater manifests; `release-publish/*-mac.yml` is suffixed with the architecture for the x64 variant so both feeds coexist.

**Forward-looking — daemon (per plan.md §11):**

- Single static Go binary, signed release URL, checksum + signature + provenance attestation verified at install. SBOM + minimum compatible desktop/API version recorded in the release manifest.
- Tool version pinning: ACFS/tool versions recorded; warn on unsupported versions; user controls upgrade.

---

## MCP Agent Mail — Multi-Agent Coordination

A mail-like layer that lets coding agents coordinate asynchronously via MCP tools and resources. Provides identities, inbox/outbox, searchable threads, and advisory file reservations with human-auditable artifacts in Git.

### Why It's Useful

- **Prevents conflicts:** Explicit file reservations (leases) for files/globs.
- **Token-efficient:** Messages stored in per-project archive, not in context.
- **Quick reads:** `resource://inbox/...`, `resource://thread/...`.

### Same Repository Workflow

1. **Register identity:**

   ```
   ensure_project(project_key=<abs-path>)
   register_agent(project_key, program, model)
   ```

2. **Reserve files before editing:**

   ```
   file_reservation_paths(project_key, agent_name, ["apps/desktop/src/**"], ttl_seconds=3600, exclusive=true)
   ```

3. **Communicate with threads:**

   ```
   send_message(..., thread_id="FEAT-123")
   fetch_inbox(project_key, agent_name)
   acknowledge_message(project_key, agent_name, message_id)
   ```

4. **Quick reads:**
   ```
   resource://inbox/{Agent}?project=<abs-path>&limit=20
   resource://thread/{id}?project=<abs-path>&include_bodies=true
   ```

### Macros vs Granular Tools

- **Prefer macros for speed:** `macro_start_session`, `macro_prepare_thread`, `macro_file_reservation_cycle`, `macro_contact_handshake`.
- **Use granular tools for control:** `register_agent`, `file_reservation_paths`, `send_message`, `fetch_inbox`, `acknowledge_message`.

### Common Pitfalls

- `"from_agent not registered"`: Always `register_agent` in the correct `project_key` first.
- `"FILE_RESERVATION_CONFLICT"`: Adjust patterns, wait for expiry, or use non-exclusive reservation.
- **Auth errors:** If JWT+JWKS enabled, include bearer token with matching `kid`.

---

## Beads (br) — Dependency-Aware Issue Tracking

Beads provides a lightweight, dependency-aware issue database and CLI (`br` — beads_rust) for selecting "ready work," setting priorities, and tracking status. It complements MCP Agent Mail's messaging and file reservations.

**Important:** `br` is non-invasive — it NEVER runs git commands automatically. You must manually commit changes after `br sync --flush-only`.

### Conventions

- **Single source of truth:** Beads for task status/priority/dependencies; Agent Mail for conversation and audit.
- **Shared identifiers:** Use Beads issue ID (e.g., `br-123`) as Mail `thread_id` and prefix subjects with `[br-123]`.
- **Reservations:** When starting a task, call `file_reservation_paths()` with the issue ID in `reason`.

### Typical Agent Flow

1. **Pick ready work (Beads):**

   ```bash
   br ready --json  # Choose highest priority, no blockers
   ```

2. **Reserve edit surface (Mail):**

   ```
   file_reservation_paths(project_key, agent_name, ["apps/desktop/src/**"], ttl_seconds=3600, exclusive=true, reason="br-123")
   ```

3. **Announce start (Mail):**

   ```
   send_message(..., thread_id="br-123", subject="[br-123] Start: <title>", ack_required=true)
   ```

4. **Work and update:** Reply in-thread with progress.

5. **Complete and release:**
   ```bash
   br close 123 --reason "Completed"
   br sync --flush-only  # Export to JSONL (no git operations)
   ```
   ```
   release_file_reservations(project_key, agent_name, paths=["apps/desktop/src/**"])
   ```
   Final Mail reply: `[br-123] Completed` with summary.

### Mapping Cheat Sheet

| Concept                   | Value                             |
| ------------------------- | --------------------------------- |
| Mail `thread_id`          | `br-###`                          |
| Mail subject              | `[br-###] ...`                    |
| File reservation `reason` | `br-###`                          |
| Commit messages           | Include `br-###` for traceability |

---

## bv — Graph-Aware Triage Engine

bv is a graph-aware triage engine for Beads projects (`.beads/beads.jsonl`). It computes PageRank, betweenness, critical path, cycles, HITS, eigenvector, and k-core metrics deterministically.

**Scope boundary:** bv handles _what to work on_ (triage, priority, planning). For agent-to-agent coordination (messaging, work claiming, file reservations), use MCP Agent Mail.

**CRITICAL: Never run bare `bv` in agent sessions (Guardrail 1).** Bare `bv` launches an interactive TUI that blocks your session.

Use robot-mode flags only, and verify supported commands in your installed version:

```bash
bv --robot-help
```

### The Workflow: Start With Triage

Start with this sequence:

- `bv --recipe actionable --robot-plan` to get immediately actionable work tracks
- `bv --robot-priority` to detect priority/impact mismatches
- `bv --robot-insights` for deep graph bottleneck analysis

```bash
bv --recipe actionable --robot-plan
bv --robot-priority
bv --robot-insights
```

### Command Reference

**Planning & Priority:**
| Command | Returns |
|---------|---------|
| `--robot-plan` | Parallel execution tracks with `unblocks` lists |
| `--robot-priority` | Priority misalignment detection with confidence |

**Graph Analysis:**
| Command | Returns |
|---------|---------|
| `--robot-insights` | Full metrics: PageRank, betweenness, HITS, eigenvector, critical path, cycles, k-core, articulation points, slack |

**History & Change Tracking:**
| Command | Returns |
|---------|---------|
| `--robot-diff --diff-since <ref>` | Changes since ref: new/closed/modified issues, cycles |
| `--as-of <ref>` | Point-in-time graph view at a historical revision/date |

**Recipes & Reporting:**
| Command | Returns |
|---------|---------|
| `--robot-recipes` | Available built-in/user/project recipe names |
| `--recipe <name>` / `-r <name>` | Apply recipe prefilter before robot command |
| `--export-md <file>` | Markdown report export with Mermaid visualizations |

### Scoping & Filtering

```bash
bv --robot-insights --as-of HEAD~30          # Historical point-in-time
bv --recipe actionable --robot-plan          # Pre-filter: ready to work
bv --recipe high-impact --robot-plan         # Pre-filter: top PageRank
bv --diff-since HEAD~30 --robot-diff         # Graph delta since historical ref
```

### Understanding Robot Output

**`--robot-plan` output:**

- `tracks` — independent work streams safe for parallel execution
- `items` — actionable issues in each track
- `summary.highest_impact` — best first target

**`--robot-priority` output:**

- `recommendations` — may be `null` when no reprioritization is needed
- `summary` — total issues scanned and recommendation counts

**`--robot-insights` output:**

- `Bottlenecks` / `CriticalPath` / `Cycles` — structural blockers
- `Stats.PageRank` / `Stats.Betweenness` / `Stats.TopologicalOrder` — ranking + traversal signals

### jq Quick Reference

```bash
bv --recipe actionable --robot-plan | jq '.plan.summary'   # Action summary
bv --robot-priority | jq '.recommendations[0]'             # Top reprioritization suggestion
bv --robot-plan | jq '.plan.summary.highest_impact'        # Best unblock target
bv --robot-insights | jq '.Bottlenecks[:5]'                # Top graph bottlenecks
bv --robot-insights | jq '.Cycles'                         # Circular deps (must fix!)
bv --diff-since HEAD~30 --robot-diff | jq '.summary'       # Health trend and churn
```

---

## UBS — Ultimate Bug Scanner

**Golden Rule:** `ubs <changed-files>` before every commit. Exit 0 = safe. Exit >0 = fix & re-run.

UBS is also the default Round 0 scanner in the Debugging / Hardening review rounds (plan.md §7.4.2, §9.2). Findings flow into the §9.3 finding ledger with `source: ubs` stamped on each one for cross-tool deduping.

### Commands

```bash
ubs file.ts file2.go                    # Specific files (< 1s) — USE THIS
ubs $(git diff --name-only --cached)    # Staged files — before commit
ubs --only=ts,go src/                   # Language filter (3-5x faster)
ubs --ci --fail-on-warning .            # CI mode — before PR
ubs .                                   # Whole project
```

### Output Format

```
Warning  Category (N errors)
    file.ts:42:5 - Issue description
    Suggested fix
Exit code: 1
```

### Fix Workflow

1. Read finding → category + fix suggestion.
2. Navigate `file:line:col` → view context.
3. Verify real issue (not false positive).
4. Fix root cause (not symptom).
5. Re-run `ubs <file>` → exit 0.
6. Commit.

### Bug Severity

- **Critical (always fix):** Data races, SQL/command injection, path traversal, unsafe deserialization, secrets leaking into logs/audit/cache (Guardrail 11), arbitrary shell exposure from the renderer or daemon API (Guardrail 2), writes to the local clone (Guardrail 3).
- **Important (production):** Unhandled promise rejections in TS, panics in Go, resource leaks (file handles, goroutines, child-process orphans, unclosed PTYs), missing `context.Context` cancellation paths, missing redaction at audit/log boundaries, unbounded channels / `PubSub.unbounded` (plan.md §14 risks), missing idempotency keys on retryable write endpoints.
- **Contextual (judgment):** TODO/FIXME, `console.log` / `fmt.Println` debugging, dead code, overly broad `any` in TS, ignored errors in Go (`_ =`).

---

## RCH — Remote Compilation Helper

RCH offloads compilation/test commands to a fleet of remote workers instead of building locally. It is most relevant for daemon (`go build`, `go test`) and any heavy desktop builds (large `bun run build` invocations) when many agents run simultaneously.

```bash
rch exec -- go build ./...
rch exec -- go test ./...
rch exec -- bun run build
```

Quick commands:

```bash
rch doctor                    # Health check
rch workers probe --all       # Test connectivity
rch status                    # Overview of current state
rch queue                     # See active/waiting builds
```

If rch or its workers are unavailable, it fails open — builds run locally.

In Hoopoe itself, `rch` is _also_ one of the canonical build/test execution sources of truth (see plan.md §1.1 source table) — the daemon's build queue (§2.7) prefers `rch` when configured.

---

## ast-grep vs ripgrep

**Use `ast-grep` when structure matters.** It parses code and matches AST nodes, ignoring comments/strings, and can **safely rewrite** code.

- Refactors/codemods: rename APIs, change import forms.
- Policy checks: enforce patterns across a repo.
- Editor/automation: LSP mode, `--json` output.

**Use `ripgrep` when text is enough.** Fastest way to grep literals/regex.

- Recon: find strings, TODOs, log lines, config values.
- Pre-filter: narrow candidate files before ast-grep.

### Rule of Thumb

- Need correctness or **applying changes** → `ast-grep`.
- Need raw speed or **hunting text** → `rg`.
- Often combine: `rg` to shortlist files, then `ast-grep` to match/modify.

### Examples for Hoopoe's stack

```bash
# Find structured TS code (ignores comments)
ast-grep run -l TypeScript -p 'function $NAME($$$ARGS) { $$$BODY }'

# Find every IPC handler registration
ast-grep run -l TypeScript -p 'ipcMain.handle($CHAN, $$$)'

# Quick textual hunt
rg -n 'console.log' -t ts
rg -n 'OPENAI_API_KEY|ANTHROPIC_API_KEY|GEMINI_API_KEY'   # Guardrail 11 grep — must be empty

# Combine
rg -l -t go 'exec.Command' | xargs ast-grep run -l Go -p 'exec.Command($$$)' --json
```

---

## Morph Warp Grep — AI-Powered Code Search

**Use `mcp__morph-mcp__warp_grep` for exploratory "how does X work?" questions.** An AI agent expands your query, greps the codebase, reads relevant files, and returns precise line ranges with full context.

**Use `ripgrep` for targeted searches.** When you know exactly what you're looking for.

**Use `ast-grep` for structural patterns.** When you need AST precision for matching/rewriting.

### When to Use What

| Scenario                                         | Tool        | Why                                                               |
| ------------------------------------------------ | ----------- | ----------------------------------------------------------------- |
| "How does the tending scheduler dispatch a job?" | `warp_grep` | Exploratory; need cross-module flow before reading files manually |
| "Where is the ActionPlan validator implemented?" | `warp_grep` | Need to discover authoritative module + call sites                |
| "Find all uses of `safeStorage.encryptString`"   | `ripgrep`   | Targeted literal search                                           |
| "Find files importing `@anthropic-ai/sdk`"       | `ripgrep`   | Guardrail check (must be empty)                                   |
| "Replace all `oracle.exec` with `oracle.run`"    | `ast-grep`  | Structural refactor                                               |

### warp_grep Usage

```
mcp__morph-mcp__warp_grep(
  repoPath: "/Users/osekkat/hoopoeAppCockpit",
  query: "How does the desktop's local clone sync with origin and surface VPS WIP overlays?"
)
```

Returns structured results with file paths, line ranges, and extracted code snippets.

### Anti-Patterns

- **Don't** use `warp_grep` to find a specific function name → use `ripgrep`.
- **Don't** use `ripgrep` to understand "how does X work" → wastes time with manual reads.
- **Don't** use `ripgrep` for codemods → risks collateral edits.

<!-- bv-agent-instructions-v1 -->

---

## Beads Workflow Integration

This project uses [beads_rust](https://github.com/Dicklesworthstone/beads_rust) (`br`) for issue tracking. Issues are stored in `.beads/` and tracked in git.

**Important:** `br` is non-invasive — it NEVER executes git commands. After `br sync --flush-only`, you must manually run `git add .beads/ && git commit`.

### Essential Commands

```bash
# View issues (launches TUI - avoid in automated sessions)
bv

# CLI commands for agents (use these instead)
br ready              # Show issues ready to work (no blockers)
br list --status=open # All open issues
br show <id>          # Full issue details with dependencies
br create --title="..." --type=task --priority=2
br update <id> --status=in_progress
br close <id> --reason "Completed"
br close <id1> <id2>  # Close multiple issues at once
br sync --flush-only  # Export to JSONL (NO git operations)
```

### Workflow Pattern

1. **Start**: Run `br ready` to find actionable work.
2. **Claim**: Use `br update <id> --status=in_progress`.
3. **Work**: Implement the task.
4. **Complete**: Use `br close <id>`.
5. **Sync**: Run `br sync --flush-only` then manually commit.

### Key Concepts

- **Dependencies**: Issues can block other issues. `br ready` shows only unblocked work.
- **Priority**: P0=critical, P1=high, P2=medium, P3=low, P4=backlog (use numbers, not words).
- **Types**: task, bug, feature, epic, question, docs.
- **Blocking**: `br dep add <issue> <depends-on>` to add dependencies.

### Session Protocol

**Before ending any session, run this checklist:**

```bash
git status              # Check what changed
git add <files>         # Stage code changes
br sync --flush-only    # Export beads to JSONL
git add .beads/         # Stage beads changes
git commit -m "..."     # Commit everything together
git push                # Push to remote
```

### Best Practices

- Check `br ready` at session start to find available work.
- Update status as you work (in_progress → closed).
- Create new issues with `br create` when you discover tasks.
- Use descriptive titles and set appropriate priority/type.
- Always `br sync --flush-only && git add .beads/` before ending session.

<!-- end-bv-agent-instructions -->

## Landing the Plane (Session Completion)

**When ending a work session**, you MUST complete ALL steps below.

**MANDATORY WORKFLOW:**

1. **File issues for remaining work** — Create issues for anything that needs follow-up.
2. **Run quality gates** (if code changed) — Tests, linters, builds (per the "Verification After Changes" section above).
3. **Update issue status** — Close finished work, update in-progress items.
4. **Sync beads** — `br sync --flush-only` to export to JSONL.
5. **Hand off** — Provide context for next session.

---

## Note for Codex/GPT-5.5

You constantly bother me and stop working with concerned questions that look similar to this:

```
Unexpected changes (need guidance)

- Working tree still shows edits I did not make in apps/desktop/src/main/BackendLifecycle.ts, apps/daemon/internal/auth/session.go. Please advise whether to keep/commit/revert these before any further work. I did not touch them.

Next steps (pick one)

1. Decide how to handle the unrelated modified files above so we can resume cleanly.
2. ...
```

NEVER EVER DO THAT AGAIN. The answer is literally ALWAYS the same: those are changes created by the potentially dozen of other agents working on the project at the same time. This is not only a common occurrence, it happens multiple times PER MINUTE. The way to deal with it is simple: you NEVER, under ANY CIRCUMSTANCE, stash, revert, overwrite, or otherwise disturb in ANY way the work of other agents. Just treat those changes identically to changes that you yourself made. Just fool yourself into thinking YOU made the changes and simply don't recall it for some reason.

---

## Note on Built-in TODO Functionality

If I ask you to explicitly use your built-in TODO functionality, don't complain about this and say you need to use beads. You can use built-in TODOs if I tell you specifically to do so. Always comply with such orders.
