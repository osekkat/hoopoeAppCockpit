# Hoopoe — Implementation Plan

## North star

A Mac desktop app that turns the Agentic Coding Flywheel from a stack of CLIs into one staged surface. Five numbered screens — **Plans → Beads → Activity → Swarm → Code Health** — each framed as a control-panel stage. All execution runs on a user-owned VPS the app provisions and SSHes into; the desktop is the cockpit, never the engine. The user spends 85% of their time in Stages 1–2; Stages 3–5 are machine-tending. **Build order should mirror that distribution.**

## Architecture

Three components, one responsibility each:

1. **Hoopoe Desktop** (Electron, Mac-first). React + TypeScript + Tailwind. Thick UI, thin local cache. Holds one secret locally: VPS connection credentials in macOS Keychain. Renders state owned by the server.
2. **Hoopoe Server** (Rust daemon on the VPS). Long-running process that wraps every CLI in the ACFS stack — `br`, `bv`, `ntm`, `caam`, `dcg`, `cass`, Agent Mail MCP. Exposes JSON-RPC over HTTPS plus a single persistent WebSocket for live data. Owns SQLite state, owns the git repos on disk, owns all PTY multiplexing into tmux. **Single source of truth.**
3. **The ACFS CLIs themselves** — installed by the bootstrap script, never reimplemented. The server shells out and parses.

**Why a daemon and not "just SSH and shell"?** Three things bite hard:
- **PTY mirroring** for 12 live tmux panes onto an Electron grid needs a stable multiplexer. SSH-per-pane doesn't scale past 4–5.
- **Subscriptions**: "tell me when bead bd-1a2b transitions to in_progress" needs a server that watches state and fans out events. Polling `br list` from 5 panes 1×/sec is unworkable.
- **Cold starts**: opening the Beads tab can't trigger 5 fresh CLI invocations. The daemon caches.

Desktop ↔ server is one mTLS-pinned HTTPS connection plus one WebSocket.

## Tech stack

| Layer | Choice | Why |
|---|---|---|
| Desktop shell | Electron 38+ | Native menubar, mature signing/notarize |
| UI | React 19 + TypeScript | Standard |
| Styling | Tailwind 4 + custom design tokens | Reference design has a distinctive cream-on-dark palette — token discipline, not Tailwind defaults |
| Server cache | TanStack Query | Built-in subscriptions + invalidation |
| UI state | Zustand | Ephemeral state, no boilerplate |
| Routing | TanStack Router | Type-safe, fits the 5-stage layout |
| Graph | React Flow (DAG) + hand-rolled Kanban | DAG needs a real graph engine; Kanban is simpler bespoke |
| Terminal panes | xterm.js bridged over WebSocket | Faithful agent-pane rendering |
| Markdown editor | CodeMirror 6 (Markdown + Vim opt-in) | Plan editing |
| Provisioning SSH | `ssh2` (Node) in Electron main | Used once during bootstrap; afterward HTTPS |
| Server daemon | **Rust** (axum + tokio) | Same toolchain as `br`/`bv`/`ntm`; great at long-lived process + subprocess management |
| Server state | SQLite (rusqlite) + JSONL event log | Same pattern `br` uses — durable, portable, syncs cleanly |
| Server PTY | `portable-pty` | Mature |
| Auth | mTLS client cert + 24h bearer token | Cert pinned at provisioning |
| Bootstrap | cloud-init + bash installer | Provider-agnostic |

**Rust on the server, not Node**: long-running process, runs subprocesses, multiplexes PTYs, cohabits the same VPS as the rest of the (Rust) ACFS stack. One toolchain on the box; no `node_modules` next to a 12-agent swarm fighting for inodes. Desktop stays in TS because it's a UI app.

## Design system

The reference screenshots are doing real work and need to be encoded as tokens, not reproduced as one-offs. Stand up a `packages/design-system` workspace in week 1 — by week 3 hand-typed Tailwind in the route files will diverge.

Core elements:
- **Two-zone shell**: black sidebar (~`#0E0B08`), warm cream content (~`#F2EBDA`). The sidebar carries Hoopoe's identity.
- **Stage chrome**: every primary screen leads with `STAGE N — VERB` in tracked uppercase, then the H1.
- **Agent identity tiles**: 2-letter codes (`C1`, `X2`, `G1`, `U1`, `A1`, `P1`) colored by family — Claude orange, Codex green, Gemini blue, Cursor gray, Aider purple, Amp red. Reusable as 32px tiles in headers and 20px chips inline.
- **Status pills**: `CONVERTED`, `DRAFT`, `PLANNING`, `URGENT`. Bordered, all-caps, never bold.
- **Priority chips**: `P0` red · `P1` orange · `P2` muted · `P3` gray.
- **Coverage bars**: 4-stop ramp red → amber → olive → green.
- **Numbered nav**: `01 Plans`, `02 Beads`, … with kbd hint icons.

## The five stages

### Stage 1 — Plans

**UI**: grid of plan cards (state pill, line count, bead count, age). Clicking one opens a 3-pane layout: CodeMirror editor on the left, "drafts and merges" sidebar on the right, action rail on top (`Generate with Claude/GPT/Gemini`, `Compare drafts`, `Synthesize`, `Refine round N`, `Convert to beads`).

**Server endpoints** (BYOK API keys held server-side, never on the desktop after first paste):
- `plan.create(project_id, title, source = blank|import|paste)`
- `plan.draft(plan_id, model_set, rough_idea)` — fans out to Anthropic/OpenAI/Google APIs in parallel
- `plan.synthesize(plan_id, draft_ids[])` — single deep-think call producing the hybrid master plan
- `plan.refine(plan_id, instructions, model)` — iterative
- `plan.lock(plan_id)` — required before `plan.convert_to_beads`

**State machine**: `draft → drafts_generated → synthesized → refining → locked → converted`

**Storage**: `<project>/plans/<plan-id>/` with `plan.md`, `drafts/<model>.md`, `synthesis.md`, `history.jsonl`. Git-tracked.

This is the screen the user lives in. Disproportionate polish here.

### Stage 2 — Beads

**UI**: tab bar `Kanban` / `DAG` / `Force`, filter chips `all/P0/P1/P2`, **Launch swarm** CTA top-right (the visual pivot to Stage 4). Five columns (Backlog, Ready, In Progress, Review, Closed). Cards show id, priority, title, blocker count, coverage when present, and the working agent's chip.

**DAG view**: same beads laid out by dependency. Cycles flagged red, the `bv ready` set highlighted.

**Server endpoints** (wrap `br` and `bv`):
- `bead.list(project_id, filters)` → `br list --json`
- `bead.graph(project_id)` → `bv graph --json`
- `bead.update(bead_id, status?, priority?, deps?)` → `br update`
- `bead.from_plan(plan_id)` → invokes the `beads-workflow` skill flow on the server with the plan as input
- `bead.score(plan_id)` — quality tracker (see below)
- `swarm.launch_proposal(plan_id)` — preview a reasonable swarm composition

**Bead-quality tracker** (side panel that scores the bead set):
- granularity (no bead > ~800 expected diff lines)
- dependency density (orphan detection)
- plan coverage (every plan section has ≥ 1 bead)
- ready-set size (≥ 4 P0/P1 beads ready to start)

Each refinement round, the score updates. When it stabilizes, **Launch swarm**.

### Stage 3 — Activity

**UI**: vertical timeline of Agent Mail messages. Each row: sender chip → target chip, bead pill, optional `URGENT`, title, body, timestamp. System messages (rate limits, swarm events) styled distinctly.

**Server**: WebSocket subscription `activity.stream(project_id)` proxies the Agent Mail MCP and persists.

Two interactions matter: click a bead pill to pivot to its Stage-2 detail; click an agent chip to pivot to its Stage-4 tile.

### Stage 4 — Swarm

**UI**: grid of agent tiles. Each tile: agent chip + model, status (`Working` / `Idle` / `In review` / `Awaiting review` / `Rate limited` / `Error`), current bead, CPU sparkline, RAM, dollar spend, and a live xterm.js mirror of the tmux pane. Top bar: aggregates (`Working 5/12`, `Tokens 24h`, `Spend`, `Velocity LOC/d`).

**Server endpoints**:
- `swarm.compose(project_id, {claude:4, codex:4, gemini:2, ...})` → `ntm` spins the tmux session
- `swarm.status()` — periodic snapshot
- `swarm.pane_stream(agent_id)` — WebSocket to the live PTY (the heaviest lift in the app)
- `swarm.message(agent_id, text)` — pipe a marching-orders prompt
- `swarm.kill(agent_id)` / `swarm.restart(agent_id)` / `swarm.broadcast(text)`
- `swarm.tend()` — runs the orchestrator loop with the universal-starting-prompt every 4 minutes

**PTY streaming strategy** (the hardest engineering problem here):
- Server runs a per-pane reader buffering tmux output to a 50KB ring.
- WebSocket streams diffs at ~10Hz; full ring on (re)attach.
- xterm.js feeds raw bytes; no client-side parsing.

**Cost guardrails**: per-agent budget caps configured at compose time. Server kills the pane if budget exceeds; CAAM swaps accounts on per-account rate-limit hits.

### Stage 5 — Code health

**UI**: 4 KPI cards (Written files, Avg coverage, Avg complexity, Hot spots), then a sortable table: file, LOC, CX, coverage bar, churn, owner agent. Hot rows clickable into the bead they should generate.

**Server endpoints**:
- `health.scan(project_id)` — language-appropriate analyzers:
  - TS/JS: vitest --coverage + ts-complexity / lizard
  - Rust: cargo llvm-cov + lizard
  - Python: coverage.py + lizard
  - Go: go test -cover + gocyclo
- `health.file_metrics(project_id, file)`
- `health.auto_file_bead(project_id, file)` — if `CX≥20` or `cov<60%`, generate a bead via `br`

Run on every push to main and after each swarm round.

## First-run / VPS provisioning

The make-or-break moment. If it doesn't "just work," the app dies in the box.

1. **Welcome** — "Hoopoe runs your agent swarm on a VPS you own." Two paths: `Provision new` or `Connect existing`.
2. **Provider** — Hetzner / DigitalOcean / Linode / Vultr / "Other (BYO)". Abstract behind `provider.create_instance({size, region, image})`.
3. **Auth** — paste provider API token; store in Keychain; sanity-check by listing regions.
4. **Spec** — opinionated default: AMD 8-core / 16GB RAM / 200GB SSD / Ubuntu 24.04. Region picker. Show $/month estimate.
5. **Provisioning** — create instance, wait for IP, generate ed25519 keypair (private to Keychain), upload public key.
6. **Bootstrap** — SSH in, push `bootstrap.sh`, execute. Script:
   - apt update; install build-essential, git, tmux, python3, sqlite3
   - install Rust toolchain
   - clone `agentic_coding_flywheel_setup` and run its installer
   - download signed Hoopoe Server binary, install
   - create `hoopoe` user + systemd unit
   - generate mTLS cert pair, return client cert to desktop
   - start daemon on 443 (or 8443 if taken)
7. **Handshake** — desktop connects via HTTPS using the cert, verifies version.
8. **API keys** — paste Anthropic/OpenAI/Google keys (used for plan generation; swarm uses CAAM-managed CLI logins).
9. **CAAM** — optional walk-through to add Claude Max / GPT Pro / Gemini Ultra accounts.

Target: ≤ 8 minutes wall clock. Each step is a checkpoint; failures resume from the last checkpoint, not the top.

## Auth model

- **Initial bootstrap**: ed25519 key in Keychain; pubkey on the VPS.
- **Steady state**: mTLS — server cert + pinned client cert. Both rotated yearly.
- **API token**: 24h bearer on top of mTLS for per-request auth and easy revocation.
- **CAAM creds, API keys**: never leave the VPS once configured. Desktop sends once, forgets.
- **Audit log**: every server action → `~/.hoopoe/audit.jsonl`.

## Phased delivery (~5 months for v1)

**Phase 0 — Foundation (3 wk).** Electron + React + TS + Tailwind + design tokens. Sidebar shell, 5 routed empty stages, design-system package, Keychain wired. *Exit*: design review against the screenshots passes.

**Phase 1 — VPS provisioning + bootstrap (3 wk).** Hetzner-only provider abstraction. cloud-init + `bootstrap.sh`. Server skeleton (axum hello world, mTLS, version handshake). Resume-on-failure. *Exit*: fresh Hetzner account → connected daemon in ≤ 8 min, twice.

**Phase 2 — Project + Plans (4 wk, the core).** Project CRUD, repo clone/import. Plan CRUD, multi-LLM draft fan-out, synthesizer, refine loop. Markdown editor with diff between drafts. *Exit*: 1-paragraph idea → locked synthesized plan, end-to-end.

**Phase 3 — Beads (4 wk).** `br`/`bv` integration. Kanban + DAG + quality tracker. `plan.convert_to_beads`. *Exit*: locked plan → bead set on canvas with deps; quality score updates on edit.

**Phase 4 — Swarm + Activity (6 wk, hardest).** NTM integration, tmux composer. PTY ↔ WebSocket. Agent grid with xterm.js mirrors. Agent Mail MCP → activity feed. Looper marching orders. CAAM rotation. Cost caps. *Exit*: launch a 6-agent swarm on a real bead set; watch it work an hour without intervention.

**Phase 5 — Code health (3 wk).** Per-language analyzers + adapters. KPI cards + table. Auto-bead-file on threshold breach. *Exit*: swarm round ends → health view updates → low-coverage file becomes a bead.

**Phase 6 — Polish, sign, ship (3 wk).** Empty/error/loading states. Onboarding tour. ⌘K palette. Notarization, electron-updater. Crash reporter. *Exit*: stranger installs, provisions, ships a small project unaided.

Buffer ~2 weeks of slip across the program.

## Risks and how to defuse them

1. **PTY-over-WS fidelity.** Wrong terminal output is a credibility killer. *Defuse*: on day 1 of Phase 4 ship a single-pane proof-of-concept; don't wait until the grid is done.
2. **Provisioning failure modes are infinite.** *Defuse*: ship Hetzner-only; gate other providers behind a flag; lean on cloud-init.
3. **Cost runaway.** A 12-agent swarm can burn $200/hr unsupervised. *Defuse*: per-agent budgets are non-optional; default $5/hr cap, alarm at 70%.
4. **CLI tool drift.** `br`/`bv`/`ntm` JSON formats can change. *Defuse*: pin versions on the server; auto-update with user opt-in; integration-test every wrapper endpoint.
5. **Plan-quality flywheel.** Bad master plans → bad beads → bad swarm work. *Defuse*: version-control every prompt as a diffable artifact; treat the synthesizer prompt as a real product.
6. **Mac-only scope.** Limits beta. *Defuse*: keep all renderer code OS-agnostic so Win/Linux is a build target, not a rewrite.
7. **WS drops on laptop sleep.** *Defuse*: idempotent reconnect with last-event-id replay from the server's event log.
8. **VPS sprawl.** Users will forget instances. *Defuse*: surface "your VPSes" with last-seen + est. monthly cost; one-click teardown.

## Open questions

1. **Mac-only forever, or roadmap to Win/Linux?** Affects whether we use Mac-only Electron features.
2. **BYOK or hosted relay?** Strong recommendation: BYOK in v1 (no prompt-handling liability).
3. **Single VPS per user, or per project?** Per-user cheaper, per-project isolates blast radius. Default per-user, allow per-project as a switch.
4. **Pricing model**: free + BYO-VPS, or do we want to be the one selling VPS time? Determines auth/billing surface.
5. **Distribution**: notarized DMG via website, or Mac App Store? MAS sandboxing breaks the SSH bootstrap → recommend self-hosted DMG with Sparkle.

## What to build first, this week

A walking-skeleton in this exact order:

1. Empty Electron app with the design-system tokens, dark sidebar, and 5 routed empty stages (no data).
2. A "STAGE 0 — CONNECT" preflight that takes a hostname + ssh key and runs `bootstrap.sh` against an existing Ubuntu 24.04 VPS you set up by hand.
3. Hoopoe Server v0: axum + a `version` endpoint and a `plan.list` returning `[]`.
4. Stage 1 plumbed end-to-end with one mock plan.

Once those four are alive in one process, every subsequent feature is "fill in a slice." Until then you're guessing.
