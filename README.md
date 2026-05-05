# Hoopoe

> A macOS desktop cockpit for the Agentic Coding Flywheel.
>
> **Hoopoe is the cockpit, not the engine. The VPS is the execution plane. The existing Flywheel tools remain the source-of-truth systems.**

Hoopoe turns the Agentic Coding Flywheel — a powerful but manual collection of CLIs, tmux sessions, agent prompts, mail messages, bead graphs, and build/test logs — into one staged operational cockpit. It centralizes, visualizes, automates, and audits the workflow without replacing `br`, `bv`, `ntm`, Agent Mail, `rch`, ACFS, CAAM, DCG, CASS, Git, or the agent CLIs. Its job is to make the workflow visible, resumable, safe, and less manually operated.

---

## Status

This repository is now an active Hoopoe monorepo. `plan.md` remains the
authoritative strategy document, while the code and docs tree contain the
living implementation contracts that Appendix A moved out of the plan.

Current authoritative entry points:

| Path | What it is |
| --- | --- |
| `plan.md` | Strategic plan: vision, principles, architecture, decisions, roadmap |
| `AGENTS.md` | Required operating rules for agents working in this repo |
| `apps/desktop/` | Electron + TypeScript desktop cockpit |
| `apps/daemon/` | Go daemon that runs on the VPS and wraps Flywheel tools |
| `packages/` | Shared schemas, fixtures, design-system, and test infrastructure |
| `docs/` | Code-near architecture, security, onboarding, testing, and operations references |
| `design/` | Mockups and design-vs-plan decisions |
| `.beads/` | Bead tracking via `br` |

When `plan.md` and any other document disagree, **`plan.md` wins**.

---

## The product: four stages plus a cross-stage Activity panel

The cockpit is organized as `STAGE N — VERB` chrome that the user navigates between. Most cognitive work happens in Planning and Beads; Swarm and Hardening are mostly machine-tending, review, intervention, and quality convergence.

```
01 Planning      chat-box → 3-4 candidate models → comparative matrix →
                 best-of-all-worlds synthesis → fresh-eyes critique →
                 4-5 refinement rounds → lock

02 Beads         locked plan → br beads with traceability map →
                 polish rounds → Kanban / DAG / Force views

03 Swarm         NTM agent launch with composition picker; bead board +
                 agent grid + Activity panel only (NO terminals by default)

04 Debugging /   code health metrics, review rounds (UBS first, then
   Hardening     specialized audits), finding tracker, convergence detector
```

The **Activity panel** is a cross-stage drawer (not a stage). It overlays whichever stage the user is in and hosts agent ↔ agent mail and the user ↔ orchestrator chat. The orchestrator the user chats with is a literal `orchestrator-chat` tending agent (§7.5 / §8.4) — not a metaphor for daemon code.

A persistent **top bar** is visible across all stages and shows: project / repo / branch, tool health dots, swarm state, beads pulse, code-health pill (coverage / complexity / hotspot count), per-provider subscription-quota usage, and the Activity panel toggle.

---

## Architecture

```
macOS Desktop                                 VPS
┌─────────────────────────┐                  ┌──────────────────────────────┐
│ Hoopoe Electron App     │   SSH tunnel     │ Hoopoe Go Daemon             │
│ - React UI              │ ◄──────────────► │ - REST + SSE/WebSocket        │
│ - TanStack Router/Query │   bearer + WS    │ - Job runner + audit log      │
│ - Local SQLite cache    │   token          │ - Adapters (br, bv, ntm,      │
│ - Sync-driven Git clone │                  │   Agent Mail, git, rch, ...)  │
│   of origin (read-only) │                  │ - Tending scheduler (§8)      │
└─────────────────────────┘                  │ - Build/test queue            │
                                             └────────────┬─────────────────┘
                                                          │
                                                          ▼
                                             ACFS toolchain on VPS:
                                             Claude Code, Codex CLI, Gemini CLI,
                                             NTM/tmux, br/bv, Agent Mail, ru,
                                             rch, CAAM, DCG, CASS, UBS, ...
                                                          │
                                                          ▼
                                                       origin
                                                  (GitHub / GitLab)
                                                          ▲
                                                          │ git fetch (read-only)
                                                          │ from desktop's local
                                                          │ sync-driven mirror
```

**Transport.** SSH tunnel is the v1 default; the daemon binds to `127.0.0.1` on the VPS and the desktop forwards a local port. Three-token auth: pairing → bearer → WS-token. Sequence-cursor + snapshot-on-reconnect makes laptop sleep, daemon restart, and tunnel re-establishment work without state corruption.

**The daemon is an API facade, not a new canonical database.** It owns Hoopoe's job state, event log, read-model cache, plan metadata, onboarding state, health snapshots Hoopoe generates, and audit events. It does *not* own bead truth, Git truth, NTM session truth, Agent Mail truth, file reservation truth, or test-report truth — those come from the project's own tools.

**The desktop's local Git clone is a sync-driven mirror of origin** — never a write target. It powers fast file reads, diffs, blame, and ripgrep. Live VPS-WIP overlays (unpushed commits, modified files) come from daemon RPCs.

---

## Tech stack

### Desktop (`apps/desktop/`, target)

- Electron + TypeScript + React + Vite + Tailwind with custom design tokens
- TanStack Router (typed stage routes), TanStack Query (server cache), Zustand (ephemeral UI state)
- CodeMirror 6 (plan editor), xterm.js (Diagnostics "Show raw pane" debug toggle only), React Flow (DAG)
- macOS Keychain via Electron `safeStorage`; local cache in SQLite or IndexedDB (read-only mirror)
- **Effect framework is NOT adopted** — patterns are lifted into plain TypeScript

### Daemon (`apps/daemon/`, target)

- **Go** (chi/echo HTTP, modernc SQLite, gorilla/nhooyr WebSocket, creack/pty fallback)
- Static cross-compiled binary (single-file deploy over SSH); `Type=notify` systemd integration
- Same family as kubelet, containerd, Tailscale, Caddy, **and NTM** — long-lived control-plane daemons that multiplex subprocesses + expose HTTP/WS

### Shared (`packages/schemas/`, target)

- OpenAPI is the source of truth; generates the TS client and Go types
- No hand-maintained duplicate shape definitions across desktop and daemon

### Source provenance — t3code lift

Desktop scaffolding (Electron lifecycle, auth, settings, keybindings, build pipeline) is vendored from [`github.com/pingdotgg/t3code`](https://github.com/pingdotgg/t3code) (MIT) and adapted for Hoopoe's remote-daemon shape. See `plan.md` Appendix B for the file inventory and `AGENTS.md` for editing rules. T3 Code's 2,175-line `main.ts` is decomposed on day one into `BackendLifecycle`, `UpdateMachine`, `IpcRegistry`, `WindowManager`, `SettingsBridge`, `AuthBridge`.

---

## Subscription model

Hoopoe is **subscription-only** for model access. Every reach into a model goes through one of:

- **Claude Code** (Claude Max → Opus, Sonnet)
- **Codex CLI** (GPT Pro → GPT-5.x and the GPT-5 Pro API counterpart)
- **Gemini CLI** (Gemini Ultra → Gemini 3 Pro with Deep Think)
- **`oracle --engine browser`** (ChatGPT Pro web — the only path to Pro)

There is **no BYOK**, no `OPENAI_API_KEY` / `ANTHROPIC_API_KEY` / `GEMINI_API_KEY` config field anywhere, and no direct provider SDK in `apps/daemon/` or `apps/desktop/`. CI fails the build on import of any provider SDK. CAAM is the sole credential pathway. The user must have at least one qualifying subscription; the onboarding wizard surfaces this requirement up front.

**Indicative monthly cost** (mirrors [agent-flywheel.com](https://agent-flywheel.com/)):

| Line item                                             | Cost (USD/month)        |
| ----------------------------------------------------- | ----------------------- |
| Cloud VPS (64 GB Ubuntu, Contabo or OVH)              | ~$40–56                 |
| Claude Max                                            | $200 (or $400 power)    |
| ChatGPT Pro (drives Oracle for the planning pipeline) | $200                    |
| Gemini Ultra (optional)                               | varies                  |
| **All-in**                                            | **~$440–$656**          |

For comparison, a junior developer is $5,000+/month; under $700 buys a 10+-agent swarm running 24/7.

---

## Tools Hoopoe wraps

Hoopoe is an integrator. It does not reimplement what these tools already do.

**Core flywheel**

- `ACFS` — canonical installer; one-liner streamed via the wizard
- `NTM` — tmux-based agent orchestrator; primary swarm substrate
- `br` (beads_rust) — dependency-aware issue tracking
- `bv` — graph-aware triage engine (PageRank, betweenness, critical path, cycles)
- `Agent Mail` — agent-to-agent messaging + advisory file reservations
- `ru` — multi-project git operations (sync, status, prune)
- `rch` — remote compilation / test offload

**Safety & accounts**

- `DCG` — Destructive Command Guard (verdicts ingested into Hoopoe's approvals queue)
- `SLB` — Simultaneous Launch Button (optional two-person rule)
- `CAAM` — Cross-Agent Account Manager (account inventory + instant switching)
- `casr` — Cross-Agent Session Resumer
- `pt` / `srp` / `sbh` — process termination / system resource protection / disk-pressure cleanup
- `UBS` — Ultimate Bug Scanner (Round 0 in review; default first-pass)

**Skills & planning**

- `vibing-with-ntm` skill — authoritative behavioral spec for swarm tending
- `ntm` skill — tool reference for NTM
- `jsm` (Jeffrey's Skills.md CLI) — preferred skill installer; SHA-256 versioning, cross-device sync
- `jfp` (Jeffrey's Prompts) — free fallback skill installer (ACFS-installed)
- `oracle` — browser-mode harness for ChatGPT Pro web
- `caut` / `rano` — subscription-quota usage tracker / per-call latency observer

**Deliberately not adopted:** direct provider SDKs, OpenCode multi-backend harness, `wa` (WezTerm Automata), `apr` as planning backend (Hoopoe owns its planning pipeline directly).

---

## Repository layout

### Current tree

```
hoopoeAppCockpit/
├── AGENTS.md            # Guidelines for AI coding agents (read first)
├── plan.md              # Strategic plan (authoritative)
├── plan.full.md         # Preserved earlier full version
├── apps/
│   ├── daemon/          # Go daemon and scripts
│   └── desktop/         # Electron desktop app
├── packages/            # Schemas, fixtures, test infra, design-system
├── docs/                # Architecture refs, ADRs, runbooks
├── design/
│   ├── README.md
│   ├── DECISIONS.md     # Design-vs-plan conflicts ledger
│   └── mockups/v1/      # Pre-Phase-1 visual sketches
└── .beads/              # Issue tracking via br
```

### Workspace intent (per `plan.md §12 Phase 1+`)

```
apps/
├── desktop/             # Electron + TS + React desktop app (cockpit UI)
│   └── src/vendored/t3code/  # Lifted from t3code (MIT); do not edit in place
└── daemon/              # Go daemon running on the VPS (API facade over Flywheel)

packages/
├── schemas/             # OpenAPI + shared types (TS client + Go types)
└── design-system/       # Design tokens + Storybook + reusable components

docs/                    # Architecture refs (source-of-truth, security, ADRs)
design/                  # Mockups + DECISIONS ledger (lives across phases)
.beads/                  # Bead tracking
```

---

## Roadmap

Phases are sequenced; do not skip ahead. See `plan.md §12` and `§16` for detail and immediate first engineering tasks, and `plan.md §18.1` for the milestone acceptance tests.

```
Phase 0    Research spike + integration contract
           (real ACFS VPS, JSON snapshots, parser fixtures)

Phase 1    Monorepo + desktop shell + lifted t3code scaffolding
Phase 2    VPS connection, auth, daemon skeleton
Phase 2.5  API/process contract hardening (do BEFORE Phase 3+)

Phase 3    ACFS onboarding and tool inventory
Phase 4    Project registry, Git, desktop local clone
Phase 5    Planning workspace
Phase 6    Bead conversion and quality tracker
Phase 7    Kanban, DAG, Force views
Phase 8    Swarm launch MVP (composition picker, abstracted dashboard, no terminals)
Phase 9    Activity panel and Agent Mail
Phase 10   Tending scheduler + initial job set
Phase 11   Debugging / Hardening: code health metrics
Phase 12   Debugging / Hardening: review rounds and convergence
Phase 13   Provider automation and production polish
```

Phase 0 scaffolding has landed: the integration-contract docs under
`docs/integration-contracts/`, the `scripts/research-spike/` collector +
verifier, the reserved `packages/fixtures/phase0-2026-05-02/` corpus
directory, and the synthetic Mock Flywheel scenarios under
`packages/fixtures/scenarios/` (development substrate — useful for replay
work, not Phase 0 acceptance evidence). The real-VPS acceptance evidence
pack itself is **still pending**: no verifier-passing capture from a real
ACFS-installed VPS has been unpacked into `scenarios/{fresh,active,failure}/`
yet. See `packages/fixtures/phase0-2026-05-02/README.md` for the collector
contract and the open beads tracking it (hp-r7i, hp-jvm, hp-7cs, hp-vtwm).

Phase 1, Phase 1.5, and the early Phase 2 daemon substrate have landed in
this checkout. The remaining beads continue to follow the same roadmap order
and the milestone acceptance tests in `plan.md §18`.

---

## Tending: scheduled jobs + skills, not a bespoke loop

Tending the swarm — detecting idle/wedged/rate-limited agents, recovering stalled beads, pushing stale commits, deciding when to flip into review mode — is implemented as a **scheduler running skill-attached jobs**. There is no hand-coded "operator loop" in Go. Four cleanly separated layers:

```
Layer 1: Scheduler (Go)        cron + interval + event triggers + on-demand
Layer 2: Pre-script (Go)       cheap mechanical reconcile; emits {wakeAgent, context}
Layer 3: Agent runtime         spawns agent with skills loaded; emits typed ActionPlans
Layer 4: Skills (content)      vibing-with-ntm, ntm; pinned via jsm or jfp
```

`wakeAgent: false` keeps healthy hours at zero LLM cost. `[SILENT]` suppresses Activity-panel noise on agent runs that decide nothing was warranted. Audit *always* fires regardless. Mutating actions go through a typed `ActionPlan` (§8.3.1) where the daemon — not the model — is the executor, with policy + idempotency + approvals + postcondition verification against canonical state.

The orchestrator the user chats with in the Activity panel is the literal `orchestrator-chat` tending job — same runtime as scheduled tending, just triggered by user messages instead of a clock.

---

## Non-negotiable guardrails (`plan.md` Appendix C)

1. Do not parse bare `bv` output; use robot surfaces only.
2. Do not expose arbitrary shell execution from the renderer or normal daemon API.
3. Do not let the desktop local clone become a write target.
4. Do not let local SQLite/IndexedDB cache become canonical.
5. Do not run health/coverage jobs inside the active agent working tree by default.
6. Do not make provider automation block existing-VPS onboarding.
7. Do not start a large swarm without showing build/test contention, budget, rate-limit, and ready-frontier warnings.
8. Do not let terminal output be the source of truth for bead/agent/mail state when structured APIs exist.
9. Do not wake tending LLM jobs when deterministic pre-scripts find nothing actionable.
10. Do not suppress audit entries just because a job returned `[SILENT]`.
11. **Do not call provider APIs directly.** No `OPENAI_API_KEY` / `ANTHROPIC_API_KEY` / `GEMINI_API_KEY` env-var references, no SDK imports (`import openai`, `from "@anthropic-ai/sdk"`, `from "@google/generative-ai"`, etc.), and no provider-SDK entries in `package.json` / `go.mod` anywhere in `apps/daemon/` or `apps/desktop/`. Provider-name labels in redaction fixtures and user-facing subscription-status UI are allowed (they're what the redaction layer matches against). CI rule: `scripts/providerlint/check-provider-sdks.ts` enforces this with anchored patterns.
12. **Do not surface raw terminal panes in the default swarm UI.** PTY plumbing exists on the daemon side for tending and forensics; the user-visible Swarm dashboard shows bead state + agent state + Activity panel only.

A failed grep on any of these is a failed PR.

---

## Source-of-truth boundary

When in doubt about who owns what (`plan.md §1.1`):

| Domain                      | Canonical source                                            |
| --------------------------- | ----------------------------------------------------------- |
| Code (canonical)            | origin (GitHub/GitLab/etc.)                                 |
| Code (VPS working state)    | VPS clone at `/data/projects/<project>/`                    |
| Code (desktop sync mirror)  | local clone at `~/Library/Application Support/Hoopoe/...`   |
| Plans                       | markdown files in repo, under `.hoopoe/plans/<plan-id>/`    |
| Beads                       | `br` / `.beads`                                             |
| Bead graph intelligence     | `bv --robot-*`                                              |
| Swarm sessions              | `ntm` + tmux                                                |
| Agent communication         | Agent Mail                                                  |
| Build/test execution        | `rch`, NTM pipelines, language-native runners               |
| Swarm tending methodology   | `ntm` + `vibing-with-ntm` skills (loaded into agents)       |
| Safety approvals            | NTM/DCG/SLB; DCG verdicts ingested into a unified queue     |
| LLM provider credentials    | CAAM (sole credential pathway)                              |
| Subscription usage          | `caut` (per-provider quota)                                 |

Hoopoe maintains a cache and append-only event log, but it must always be able to answer: *"What is true if we ignore the Hoopoe cache and re-read Git, `br`, `bv`, NTM, Agent Mail, and test reports?"* That question guides every integration boundary.

---

## Working in this repo

If you are an AI coding agent or contributor, **read `AGENTS.md` first**. Highlights:

- **No file deletion without express permission.** Even files you yourself created.
- **No script-based code transformations.** Make code changes manually; use parallel subagents for many simple changes.
- **No file proliferation.** Revise existing files in place; the bar for new files is incredibly high.
- **No backwards-compatibility shims** for non-existent legacy behavior. Hoopoe has no users yet — do things the right way with no tech debt.
- **Verify after substantive changes.** Typecheck, lint, test, build the surfaces you touched.
- **Phase 0 fixtures are mandatory** before adapter-dependent feature work for `br`, `bv`, `ntm`, and Agent Mail.
- **Tests assert capabilities, not just parser success.** Every adapter reports `/v1/capabilities`; stage routes are gated on capability IDs.

Issue tracking uses `br` (beads_rust). Coordination uses MCP Agent Mail with file reservations. See `AGENTS.md` for the workflow.

---

## Documentation map

| Question                                              | Where to look                                  |
| ----------------------------------------------------- | ---------------------------------------------- |
| What is Hoopoe and why?                               | `plan.md §0`, `§1`                             |
| How is the system put together?                       | `plan.md §2`, `§3`                             |
| What owns each source of truth and data path?          | `docs/source-of-truth.md`                      |
| How do I get a dev checkout running?                   | `docs/getting-started.md`                      |
| What does the user see and do?                        | `plan.md §7` (the four stages + Activity panel) |
| How does the cockpit reach models?                    | `plan.md §7.1`, `§13`                          |
| How is the swarm tended?                              | `plan.md §8`                                   |
| How does Hardening work?                              | `plan.md §7.4`, `§9`                           |
| Security model, approvals, audit schema               | `docs/security.md`                             |
| First install / VPS onboarding                        | `docs/onboarding.md`, `docs/wizard.md`         |
| Daemon API seed contract                              | `docs/api-seed.md`                             |
| Process and job management                            | `docs/process-manager.md`                      |
| Reconnect and event replay                            | `docs/reconnect-replay.md`                     |
| Daemon upgrade and rollback                           | `docs/upgrade-and-rollback.md`                 |
| Testing and release smoke                             | `docs/testing.md`                              |
| Troubleshooting                                       | `docs/troubleshooting.md`                      |
| Roadmap and milestone acceptance                      | `plan.md §12`, `§16`, `§18`                    |
| Risks and mitigations                                 | `docs/risks.md`, `plan.md §14`                 |
| MVP scope (in/out of scope, deferred)                 | `plan.md §13`                                  |
| t3code lift inventory + anti-patterns to refuse       | `plan.md` Appendix B                           |
| Non-negotiable guardrails                             | `plan.md` Appendix C                           |
| Visual design                                         | `design/mockups/v1/`, `design/DECISIONS.md`    |
| Methodology Hoopoe codifies                           | [agent-flywheel.com](https://agent-flywheel.com/) |

---

## License

Hoopoe's own code is unreleased while the project is pre-beta. The desktop scaffolding lifts files from [`github.com/pingdotgg/t3code`](https://github.com/pingdotgg/t3code) (MIT, Copyright 2026 T3 Tools Inc.); the MIT notice is preserved on every vendored file under `apps/desktop/src/vendored/t3code/` per `plan.md` Appendix B.
