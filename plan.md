# Hoopoe — Implementation Plan

> Strategic plan: vision, principles, architecture, decisions, roadmap.
> Schemas, API surfaces, design components, and process detail belong in code (`packages/schemas/`, `packages/design-system/`, `docs/`) — see Appendix A.
> Desktop lifecycle, auth, settings, keybindings, and build-pipeline scaffolding are lifted from `github.com/pingdotgg/t3code` (MIT) — see Appendix B.
> Earlier full version preserved at `plan.full.md`.

---

## 0. Executive thesis

Hoopoe is a macOS Electron desktop app that turns the Agentic Coding Flywheel from a powerful but manual collection of CLIs, tmux sessions, agent prompts, mail messages, bead graphs, and build/test logs into one staged operational cockpit.

The strategic design constraint is non-negotiable:

> **Hoopoe is the cockpit, not the engine. The VPS is the execution plane. The existing Flywheel tools remain the source-of-truth systems.**

Hoopoe should centralize, visualize, automate, and audit the Flywheel. It should not replace `br`, `bv`, `ntm`, Agent Mail, `rch`, ACFS, CAAM, DCG, CASS, Git, or the agent CLIs. Its job is to make the workflow visible, resumable, safe, and less manually operated.

The product is organized into five numbered stages:

```text
01 Plans
02 Beads
03 Activity
04 Swarm
05 Code Health
```

The user spends most of their meaningful cognitive effort in **Plans** and **Beads**. The later stages are mostly machine-tending, review, intervention, and quality convergence. The engineering roadmap mirrors that distribution: cockpit + setup first, then plan creation and bead curation, before pouring months into perfect swarm telemetry.

---

## 1. Product principles

### 1.1 Preserve native sources of truth

Hoopoe must never become a fragile parallel database that silently diverges from the tools agents actually use.

| Domain | Canonical source | Hoopoe role |
|---|---|---|
| Code | Git repo on VPS | status, commits, diffs, safety gates |
| Plans | Markdown files in repo | editor, versioning, synthesis artifacts, state metadata |
| Beads | `br` / `.beads` | command wrapper, read model, kanban/DAG visualization |
| Bead graph intelligence | `bv --robot-*` | triage panels, graph metrics, launch readiness |
| Swarm sessions | `ntm` + tmux | launch, observe, send, recover, checkpoint |
| Agent communication | Agent Mail | timeline, threads, reservations, overseer broadcast |
| Build/test execution | `rch`, test runners, job queue | dedupe, throttle, stream logs, record artifacts |
| Code health | coverage/complexity reports + Git | normalize snapshots and trends |
| Safety approvals | NTM/DCG/SLB/policy tools | surface state, require approvals, audit decisions |

Hoopoe maintains a cache and append-only event log, but it should always be able to answer:

> What is true if we ignore the Hoopoe cache and re-read Git, `br`, `bv`, NTM, Agent Mail, and test reports?

That question guides every integration boundary.

### 1.2 The desktop app is not the orchestrator of record

Electron can sleep, crash, lose Wi-Fi, or be closed. The swarm must continue. All long-running jobs, operator loops, state reconciliation, and review cycles run on the VPS under the Hoopoe daemon and/or NTM. The desktop reconnects, replays events, and rehydrates UI state. It does not own the running swarm.

### 1.3 Use robot/API surfaces first, shell parsing last

Integration precedence:

1. Official REST/SSE/WebSocket/OpenAPI surfaces, especially NTM `serve`.
2. Tool-provided robot/JSON output: `ntm --robot-*`, `bv --robot-*`, `br --json`, `ru --json`.
3. Stable repo files such as `.beads/issues.jsonl`, plan metadata, health report files.
4. Direct SQLite reads only when documented and read-only.
5. Human CLI output parsing only as a fallback, behind tests and version pins.

Bare `bv` is never invoked from automation — it launches an interactive TUI. Hoopoe uses only `bv --robot-*` surfaces for machine reads.

### 1.4 Every automation must be inspectable

Hoopoe should never feel like hidden magic. Every meaningful automated action produces an event capturing who/what triggered it, which command or API call ran, why, what input scope, what output, and which artifact stores the evidence. This makes the tool trustworthy when it reopens a stalled bead, kills a wedged pane, force-releases a stale reservation, queues a build, or asks agents to re-read `AGENTS.md`.

### 1.5 Build for restartability

Every stage is restartable from artifacts: plans are markdown plus history files; beads live in `.beads`; swarms have NTM sessions/checkpoints/logs/timelines; code health has persisted snapshots; jobs have status, logs, inputs, and outputs. A user can close Hoopoe, reopen it, and understand exactly where the project is.

### 1.6 Make the first successful run boring

The first install and first project launch optimize for reliability over maximal automation. Provider-provisioned VPS creation is valuable, but the MVP supports existing-VPS connection first and adds provider plugins later. Full provider automation does not block the core product from working.

---

## 2. System architecture

Use a **Client → Tunnel → Daemon → Toolchain** architecture.

```text
macOS Desktop
┌──────────────────────────────────────────────────────────────┐
│ Hoopoe Electron App                                           │
│  - React UI                                                   │
│  - local encrypted settings                                   │
│  - macOS Keychain integration                                 │
│  - SSH profile + tunnel manager                               │
│  - local event cache / reconnect cursor                       │
│  - optional emergency terminal                                │
└──────────────────────────────┬───────────────────────────────┘
                               │
                               │ SSH tunnel by default
                               │ optional pinned mTLS direct mode later
                               ▼
VPS localhost
┌──────────────────────────────────────────────────────────────┐
│ Hoopoe VPS Daemon                                             │
│  - REST API + SSE/WebSocket event stream                      │
│  - job runner and append-only audit log                       │
│  - adapters around ACFS, br, bv, ntm, Agent Mail, git, rch    │
│  - cached read models and reconciliation loops                │
│  - PTY/tmux/NTM pane streaming bridge                         │
│  - operator loop and build/test queue                         │
└──────────────────────────────┬───────────────────────────────┘
                               │
                               ▼
Existing Flywheel stack on VPS
┌──────────────────────────────────────────────────────────────┐
│ ACFS-installed tooling                                        │
│  - Claude Code, Codex CLI, Gemini CLI                         │
│  - NTM / tmux                                                 │
│  - br / bv                                                    │
│  - Agent Mail                                                 │
│  - ru, rch, CAAM, DCG, CASS, UBS, language runtimes           │
│  - project Git repos under /data/projects                     │
└──────────────────────────────────────────────────────────────┘
```

### 2.1 Why the VPS daemon is necessary

Direct SSH command execution from Electron is good enough for bootstrap but not for the product. The daemon is required because Hoopoe needs:

- stable APIs instead of ad hoc command strings;
- background jobs that survive Electron restarts;
- periodic reconciliation and operator loops;
- low-latency event fanout;
- PTY/tmux/NTM pane streaming;
- command allowlisting and audit logs;
- cached read models so tab navigation does not run five expensive CLI commands;
- centralized build/test throttling;
- version compatibility checks;
- replayable event history after laptop sleep.

### 2.2 The daemon is an API facade, not a new canonical database

The daemon owns: Hoopoe job state, event log, read-model cache, UI-only preferences, plan metadata Hoopoe creates, onboarding state, health snapshots Hoopoe generates, workflow audit events.

The daemon does **not** own: bead truth, Git truth, NTM session truth, Agent Mail truth, file reservation truth, or test-report truth when those come from the project's own tools.

For each dashboard load, the daemon reconciles its cache against canonical state. Caches can be stale; canonical tool state wins.

### 2.3 Integration hierarchy

Adapters are implemented in this order:

```text
NTMAdapter
  1. ntm serve REST/SSE/WS
  2. ntm --robot-snapshot / --robot-status / --robot-tail
  3. tmux capture-pane fallback

BrAdapter
  1. br commands with --json where available
  2. .beads/issues.jsonl read model
  3. read-only SQLite fallback if documented

BvAdapter
  1. bv --robot-triage
  2. bv --robot-plan
  3. bv --robot-insights
  4. bv --robot-diff

AgentMailAdapter
  1. Agent Mail MCP/API surfaces
  2. NTM mail/locks surfaces
  3. read-only DB/file fallback if documented

GitAdapter
  1. git plumbing commands
  2. ru --json for multi-repo workflows

HealthAdapter
  1. project-native reports
  2. language-native commands
  3. generic analyzers (lizard/tokei/cloc/scc)
```

### 2.4 Default network posture

**Default mode:** daemon binds to `127.0.0.1` on the VPS; Electron creates an SSH tunnel; API calls go to `localhost:<forwarded-port>`; no public daemon port is exposed.

**Advanced mode (later):** daemon may expose HTTPS on a public or private interface; mTLS client certificates are pinned during provisioning; firewall rules restrict access; bearer token still required on top of mTLS.

This gives the security benefits of SSH-tunneled localhost by default while leaving room for provider-managed or team scenarios later.

---

## 3. Tech stack

### Desktop
- Electron + TypeScript + React + Vite + Tailwind with custom design tokens
- TanStack Router (typed stage routes), TanStack Query (server cache), Zustand (ephemeral UI state)
- CodeMirror 6 for plan editing, xterm.js for pane mirroring, React Flow for DAG (Cytoscape only if needed)
- macOS Keychain via keytar; local cache in SQLite or IndexedDB

### VPS daemon
- **Go** (chi/echo, modernc SQLite, gorilla/nhooyr WebSocket, creack/pty fallback)
- Rationale: static cross-compiled binary (single-file deploy over SSH, no native-module rebuild on target), `Type=notify` systemd integration, strong concurrency primitives for goroutine-per-PTY-stream + WS fanout, no `node_modules` competing with the user's project for VPS inodes/disk, lower baseline memory than Node/Bun for long-lived processes, mature production debugging (`pprof`/`delve`/`strace`).
- **Toolchain landscape** (verified 2026-04-29):
  - `NTM` — **Go** (1.25+, charmbracelet bubbletea/bubbles ecosystem). The largest integration surface; same-language adapter writing means we can read NTM source as canonical reference and copy request/response types straight into Hoopoe's adapter.
  - `beads_rust` (`br`) — **Rust**, published as a library on crates.io with `pub mod {model, storage, sync, format, ...}`. Hoopoe consumes via `br --json`; the library is available if we ever want in-process embedding.
  - `Agent Mail` (`mcp_agent_mail`) — **Python** (3.12+, fastmcp + FastAPI). MCP/HTTP regardless of daemon language.
- Genre fit: long-lived control-plane daemons that multiplex subprocesses + expose HTTP/WS on Linux servers (kubelet, containerd, Tailscale, Caddy, Consul, **and NTM**) are Go. Hoopoe is structurally a member of that family.
- Rust is a credible alternative if the team strongly prefers it. The only direct technical advantage Rust unlocks is in-process embedding of `beads_rust`, which is "nice but not decisive" — `br --json` covers the integration boundary cleanly. Node/Bun is acceptable for prototypes; t3code proves it works for local-host servers, but the deployment costs (Bun runtime + native modules + node_modules on the VPS) are real for Hoopoe's remote-daemon shape.

### Shared
- OpenAPI in `packages/schemas/`, generated TS client + Go types
- No hand-maintained duplicate shape definitions across desktop and daemon

### Desktop layer source provenance
- The Electron lifecycle, auth, settings, keybindings, and build-pipeline scaffolding are **vendored from `github.com/pingdotgg/t3code` (MIT)**, adapted for Hoopoe's remote-daemon shape. T3 Code's `apps/desktop/` is ~plain TypeScript with only 3 Effect-framework references (vs. 5,283 in their server), making it cleanly portable. Full file inventory in Appendix B.
- **Effect framework is *not* adopted.** Their server is Effect-everywhere; we adopt their *patterns* (PubSub change streams, atomic file writes, file-watch debounce, sequence cursors, semaphore-guarded ops) in plain TypeScript at lower cognitive cost.
- The Go daemon is greenfield. Auth/settings/protocol *shapes* are taken from t3code; the implementation is ours.

Detailed library tables and rationale in `packages/schemas/README.md` and `apps/*/README.md` at scaffold time.

---

## 4. Domain model

### 4.1 Lifecycle states (project)

```text
unconfigured
  → vps_ready
  → tools_installed
  → project_imported
  → planning
  → plan_finalized
  → beads_created
  → beads_finalized
  → swarm_running
  → review_rounds
  → quality_gates
  → completed
```

Plan and swarm sub-states are derivable from canonical sources (`br list`, NTM session state, plan metadata) and live in `packages/schemas/`, not here.

### 4.2 Gate invariants

Before advancing from one stage to the next, these must be true:

| Gate | Must be true |
|---|---|
| VPS ready | SSH verified, daemon reachable, ACFS installed or intentionally skipped, tool versions recorded |
| Project ready | Git repo present, branch known, `AGENTS.md` present, `.hoopoe` initialized, tool detection done |
| Plan locked | plan self-contained, major decisions explicit, testing strategy present, unresolved decisions listed or accepted |
| Beads created | `br` contains beads linked to plan, `.beads/issues.jsonl` flushed, conversion artifacts saved |
| Beads finalized | plan-to-bead coverage checked, dependencies checked, ready set sufficient, bead clarity/testability acceptable |
| Launch ready | NTM healthy, Agent Mail healthy, `bv --robot-*` healthy, `br ready --json` nonempty or intentionally scoped, build queue policy set |
| Review ready | implementation beads closed or intentionally deferred, no obvious stuck in-progress beads, review prompts available |
| Ship ready | tests/builds pass or exceptions documented, code health gates pass or follow-up beads exist, Git/beads synced |

Entity schemas (Project, Plan, Bead, SwarmSession, Agent, FileReservation, BudgetPolicy, etc.) live in `packages/schemas/`.

---

## 5. Security model

### 5.1 Secrets

- **Local Mac:** SSH private key referenced or generated and stored in macOS Keychain; provider API tokens (if used); local connection-profile tokens; optional client-cert material for advanced mode.
- **VPS:** agent CLI credentials handled by ACFS/CAAM; planning API keys if user opts into server-side planning; daemon auth token; encrypted config files; redacted audit logs.

The desktop never stores raw model API keys longer than necessary if the user routes planning through the desktop instead of the VPS. The UI offers an explicit choice between "Store on VPS" (recommended) and "Desktop-only API calls" (privacy-max).

### 5.2 Auth model: pairing → bearer → WS-token

Adopt the three-token shape from t3code, adapted for Hoopoe's SSH-tunnel transport:

1. **Pairing token** (12-char Crockford alphabet, no `0/I/O` confusables) — issued by the daemon at first start (and re-issuable via `hoopoe auth pairing create`). Persisted in the daemon's append-only event log + seeded as a single-use in-memory grant so first launch works before the desktop has any credentials. Single-use; consumed atomically at `/api/auth/bootstrap/bearer`.
2. **Bearer session token** (HMAC-signed `base64url(claims).base64url(sig)`, 30-day TTL) — minted by consuming the pairing token over the SSH tunnel. Persisted on desktop via Electron `safeStorage.encryptString`. Used for all HTTP calls.
3. **WS token** (5-min, stateless HMAC over the daemon's signing secret, with `sid` claim that's looked up against the bearer table) — issued just-in-time from `/api/auth/ws-token` immediately before a WebSocket connect, passed as `?wsToken=...`. Used for WebSocket only.

Roles: `owner` (mints/revokes pairing tokens, manages sessions) vs. `client` (read/write data only). Single signing secret on the daemon; rotating it revokes everything.

CLI surface on the daemon: `hoopoe auth {pairing,session} {create,list,revoke}` — same shape as t3code's `t3 auth` for operator familiarity.

**Bootstrap auth flow.** During VPS onboarding, `bootstrap.sh` starts the daemon, which prints its initial pairing token to stdout. The desktop captures it through the SSH session (the SSH channel is already authenticated) and immediately exchanges it over the tunnel for a 30-day bearer. No QR code, no out-of-band transfer — the SSH session *is* the trusted bootstrap channel.

**For local demo mode** (when daemon and desktop run on the same Mac for development), use t3code's **FD-3 envelope pattern**: the desktop spawns the daemon with `stdio: ['ignore', 'pipe', 'pipe', 'pipe']`, writes a per-launch JSON envelope `{port, token, ...}` to FD 3, and closes it. Secrets never appear in `ps`, env, or argv.

**Steady state.** SSH tunnel + bearer (HTTP) + WS-token (WebSocket) + reconnect cursor on the event stream. mTLS-direct mode is optional for advanced/team scenarios; SSH tunneling is the v1 default.

### 5.3 Command safety

The daemon never exposes arbitrary shell execution as a normal API. It executes typed commands through a policy layer: command path must be inside a registered project or approved tool path; destructive Git/filesystem operations require explicit approval; `sudo` requires setup mode or explicit approval; build/test commands go through the build queue; secrets are redacted before storage and streaming; every command writes an audit event.

Human approval is required before destructive operations (deleting projects, force-pushing, hard resets over active swarm work, killing swarms, exposing daemon ports, raising budget caps, importing provider credentials, running unrecognized custom scripts). The exact list is curated per feature in `docs/security.md`.

---

## 6. First install and VPS onboarding

### 6.1 First-run wizard (Stage 0 — Connect)

Steps:

1. Explain that Hoopoe controls a user-owned VPS.
2. Choose path: connect existing VPS, provision new, or local demo.
3. Configure SSH (generate or import key, paste host/user, verify fingerprint).
4. Run preflight (OS version, CPU/RAM/disk, network, base tools, permissions).
5. Install or verify ACFS.
6. Install or update Hoopoe daemon.
7. Start daemon as systemd service.
8. Establish tunnel.
9. Run tool inventory.
10. Configure optional credentials (planning keys, CAAM accounts, GitHub auth).
11. Show "VPS Ready."

### 6.2 Existing VPS first, provider automation second

The MVP supports **existing VPS** first because it is easiest to make reliable and fastest to debug. Provider automation is designed from day one but ships after the tunnel/daemon/tooling path works. Recommended rollout: existing VPS → one provider plugin (the team's preferred provider) → additional providers → one-click teardown and cost inventory.

Provider plugin contract (in `packages/schemas/`): `listRegions`, `listSizes`, `createInstance`, `destroyInstance`, `estimateMonthlyCost`.

### 6.3 Bootstrap flow

```text
1. verify OS and basic dependencies
2. install missing base packages
3. install or verify ACFS
4. run ACFS doctor/inventory
5. install Hoopoe daemon binary
6. create daemon config and signing secret
7. install systemd unit (Type=notify) and start daemon
8. daemon emits initial pairing token to stdout (captured by SSH)
9. desktop opens SSH tunnel, exchanges pairing token → 30d bearer
10. bearer persisted in macOS Keychain via safeStorage
11. version handshake; print machine-readable result JSON
```

The wizard streams logs and shows structured checkpoint cards. Failures resume from checkpoints rather than starting from scratch.

---

## 7. The five stages — strategic intent

UI specs, component inventories, and detailed view layouts live in `packages/design-system/`. This section captures only the strategic intent and the load-bearing decisions per stage.

### 7.1 Plans

**Purpose.** Plans are the highest-leverage artifact in the system. The Plan workspace is a first-class product, not a textarea plus "generate" button. The goal: turn a rough idea or existing-codebase feature request into a deeply reasoned, self-contained markdown plan that can survive conversion into beads without losing architecture, tests, user workflows, or edge cases.

**Modes.**
- **Import an existing plan:** paste/select markdown; Hoopoe runs a quality review before conversion.
- **Create from rough idea:** competing-models pipeline below.
- **Extend existing codebase:** agents inspect README, AGENTS.md, architecture docs, package files, tests, existing beads, and current health hot spots before generating implementation-aware plans.

**Pipeline (from rough idea):**

```text
rough_idea
  ├─ candidate_plan_claude
  ├─ candidate_plan_codex_or_openai
  ├─ candidate_plan_gemini
  └─ optional_candidate_other
        ↓
  comparative_matrix
        ↓
  synthesized_master_plan
        ↓
  fresh_eyes_critique
        ↓
  refinement_round_1
        ↓
  refinement_round_2
        ↓
  lock_or_continue
```

**Where planning runs.** Default: planning runs on the VPS daemon as jobs so artifacts land inside the project and survive desktop disconnects. Three options: server-side API mode (BYOK keys on VPS), server-side CLI mode (configured Claude/Codex/Gemini CLIs), desktop-only API mode (Electron calls APIs and writes artifacts to VPS). The UI must make credential location explicit.

**Quality dimensions** (deterministic + model-based, scored as guidance, not truth): intent clarity, architecture specificity, workflow coverage, implementation detail, testing specificity, risk coverage, bead readiness.

**Locking.** Writes final `plan.md`, creates a snapshot hash, requires unresolved decisions to be accepted or resolved, marks metadata `locked`, enables "Convert to beads." Amendments to a locked plan create a new version and can trigger bead delta analysis.

### 7.2 Beads

**Purpose.** The bead stage prevents the plan-bead gap. It must ensure the final plan becomes dependency-aware, self-contained, testable work units that agents can execute without improvising major architecture.

**Conversion workflow** (every step audited):

```text
1. verify plan is locked or ask for override
2. verify br is initialized
3. create conversion job
4. start a high-reasoning conversion agent/job
5. use beads-workflow instructions
6. create/update beads through br only
7. run br sync --flush-only
8. snapshot .beads/issues.jsonl
9. build plan-to-bead traceability map
10. run bv robot triage/insights
11. show quality score and recommended polish round
```

**Plan-to-bead traceability.** Every conversion produces `traceability.json` mapping each bead to the plan sections it implements, with coverage status, test obligations, unmapped sections, orphans, oversized beads, duplicate candidates. This is what lets the user ask "which plan section is this bead?" and "which sections have no beads?"

**Polish rounds** (each round is a tracked job with its own artifact):

```text
Round 1: plan coverage
Round 2: dependency correctness
Round 3: granularity and split/merge
Round 4: test obligations and acceptance criteria
Round 5: parallel execution tracks
Round 6: fresh-eyes review of bead graph
```

**Quality dimensions:** plan coverage, dependency correctness, granularity, ready-set size, testability, duplicate risk, parallelism, context richness.

**Views.** Kanban (execution state), DAG (dependency structure), Force (cluster/hotspot exploration). Bead detail drawer covers overview, full context, dependencies, plan traceability, mail thread, files/reservations, tests/health, commits, review findings, audit history.

### 7.3 Activity

**Purpose.** Activity is the coordination ledger. It combines Agent Mail, NTM events, bead updates, file reservations, build/test events, and orchestrator interventions into one readable timeline.

**Event categories.** agent registered; mail sent/received/urgent; bead claimed/status changed; file reserved/renewed/released/conflicted; build/test started/completed/failed; rate limit detected; pane wedged; orchestrator intervention; review request/finding; commit created; health snapshot updated.

**Interactions.** Click a bead pill → bead detail. Click an agent chip → swarm tile. Click a file path → reservation view. Reply as human overseer; broadcast to swarm; create bead from message; mark acknowledged.

**File reservations are advisory, not hard locks** — Hoopoe surfaces stale reservations and conflict warnings without pretending the GUI can prevent every file edit.

### 7.4 Swarm

**Purpose.** Mission control. Launches agents through NTM, shows live status, exposes logs/panes, tracks costs and rate limits, lets the user intervene without dropping into raw tmux.

**Default launch policy.** Stagger starts by ≥30 seconds; force `AGENTS.md` and README reread; require Agent Mail registration; require `bv --robot-triage` and `br ready --json` before claiming work; mark claimed beads `in_progress`; reserve files before edits; include bead ID in mail subjects, reservation reasons, and commit messages; use `rch` for builds/tests when configured; never invoke bare `bv`; avoid concurrent builds for same project; self-review with fresh eyes before review/close; report blockers quickly; do not wait in communication purgatory.

**Launch sequence.** Reconcile project state → verify launch gates → show warnings (dirty Git, stale reservations, no ready beads, low disk, missing Agent Mail) → create swarm spec and audit event → call NTM spawn/add → stagger agent starts → send kickoff prompt → start event subscriptions → start operator loop → show swarm dashboard.

**Kickoff prompt template** (this is product surface — versioned and regression-tested):

```text
Reread AGENTS.md and README.md completely so the operating contract is fresh.

You are part of a Hoopoe-managed Agentic Coding Flywheel swarm.
Project: {{projectName}}
Project path: {{projectPath}}
Current mode: {{mode}}
Plan: {{planTitle}}
Bead scope: {{beadScope}}

Rules:
- Coordinate through Agent Mail.
- Register/check your inbox before starting work.
- Use bv --robot-triage and br ready --json before selecting work.
- Do not run bare bv.
- Claim exactly one bead before editing, then mark it in_progress with br.
- Use bead IDs in Agent Mail thread IDs, subjects, file reservation reasons, and commit messages.
- Reserve files through Agent Mail before editing.
- Avoid duplicate work and avoid stepping on active reservations.
- Use rch for builds/tests when available and respect the shared build queue.
- Report blockers quickly.
- Do not get stuck in communication purgatory; if unblocked, choose useful ready work.
- When finished, run tests appropriate to the bead, self-review with fresh eyes, then move to review or close according to project policy.
```

**PTY and pane streaming strategy.** Terminal fidelity is a major credibility risk.

- Preferred: NTM WebSocket/robot-tail surfaces; subscribe to structured pane output and status events; render in xterm.js.
- Fallback: daemon uses tmux capture loop, per-pane 50–100KB ring buffer, initial ring on attach, 5–10Hz diff streaming, cursor-based reconnect.
- Do NOT make one SSH connection per pane. Do NOT poll every pane aggressively from Electron. Do NOT parse terminal output as the source of truth when NTM/Agent Mail/`br` already know the state. Terminal content is observability and manual-intervention surface, not canonical state.

**Cost and rate-limit guardrails.** Per-agent and per-swarm budget caps with alert/hard-stop thresholds; rate-limit detection via CLI status messages, NTM events, long-no-output heuristics, and CAAM if configured. Show estimates as estimates — avoid false precision.

### 7.5 Code Health

**Purpose.** Code Health turns "agents are coding" into measurable quality and feeds findings back into beads.

**Adapters per ecosystem:** TS/JS (vitest/jest + lcov + lizard/ts-complexity), Python (pytest + coverage.py + radon/lizard), Rust (cargo test + cargo llvm-cov + lizard), Go (go test -cover + gocyclo/lizard), generic (configurable shell + lizard/scc/tokei/cloc).

**Hotspot scoring** is a weighted sum of high complexity, low coverage, high churn, recent agent changes, failed tests nearby, review findings nearby, and critical-path bead linkage. Default thresholds: complexity ≥ 20, coverage < 60%. Configurable per project.

**Output.** KPI cards (written files, average coverage, average complexity, hotspot count) + sortable file table (LOC, complexity, coverage bar, churn, owner agent, linked bead, hotspot reasons) + quick action to create a bead from a hotspot. Snapshots run on every push to main and after each swarm round.

---

## 8. Operator loop

### 8.1 Purpose

The operator loop is the machine-tending brain. It runs on the VPS every few minutes during active swarms (default 4-minute cadence), is visible, configurable, and auditable.

### 8.2 Algorithm

```text
for each active swarm:
  acquire loop lock
  create operator tick event

  reconcile:
    ntm snapshot/status
    br bead states
    bv robot triage/plan/insights
    Agent Mail inbox/activity/reservations
    Git status
    build/test queue state
    health snapshot if cheap
    disk usage/artifact pressure

  detect:
    idle agents
    wedged panes
    rate-limited agents
    duplicate bead claims
    in-progress beads with no activity
    stale reservations
    build/test contention
    repeated failed test loops
    dirty Git risk
    agents not using Agent Mail
    agents not updating br
    strategic drift
    review saturation
    low disk

  decide:
    no-op
    send marching orders
    ask agent for status
    send AGENTS.md reread prompt
    assign ready bead
    reopen stalled bead
    split blocker bead
    release stale reservation
    pause/requeue build
    run artifact cleanup
    start review round
    alert human

  act with audit events
  release loop lock
```

### 8.3 Stalled bead detection

A bead is a stalled candidate when status is `in_progress`, the owner is idle/wedged/stopped/rate-limited, no Agent Mail activity for the bead in N minutes, no pane output from the owner in N minutes, no file modification in N minutes, no test/build activity related to it, the reservation has expired, and repeated loop ticks produced no progress.

Actions by severity: ask owner for status → reread AGENTS.md → ask owner to proceed/help/release → reopen if owner is gone → force-release stale reservations with audit note → create blocker bead → reassign → alert human for destructive/ambiguous cases.

### 8.4 Build/test contention control

Centralize expensive commands. Agents request tests/builds; the daemon queues and dedupes; identical commands reuse recent results when safe; `rch` is preferred when configured; UI shows queue and currently running jobs; orchestrator warns agents when contention is high; stale-artifact cleanup runs under disk pressure.

### 8.5 Strategic drift detection

A swarm can be busy but no longer closing the product gap. Hoopoe flags drift when there are many commits but few beads closed; lots of low-priority work while P0/P1 critical path is unchanged; repeated review findings in the same domain; code health worsens while beads close; agents create many new beads without closing old ones; user-defined success criteria remain unmapped.

Actions: stop/slow swarm; run reality-check review; generate drift report; create or revise beads; ask human to approve a new plan/bead refinement round.

---

## 9. Review and hardening

### 9.1 Transition into review mode

When implementation beads are done or nearly done, Hoopoe proposes review mode. Prerequisites: no obvious active implementation beads without owner; all P0/P1 ready beads either closed, in review, or intentionally deferred; Git status understood; latest health snapshot available; build/test queue not overloaded.

### 9.2 Review rounds

```text
Round 1: original-agent self-review
Round 2: cross-agent review
Round 3: fresh-eyes new-session review
Round 4: random code exploration
Round 5: hotspot-targeted review
Round 6: test/coverage hardening
Round 7: UI/UX polish if applicable
Round 8: security/performance/deadlock/mock-code specialized skills if applicable
Round 9: final landing checklist
```

Each round writes an artifact recording: model/tool used, prompts, agents involved, findings, fixes, new beads, false positives, test/health deltas, cost/time summary.

### 9.3 Finding lifecycle

```text
new
  → triaged
  → fix_now
  → new_bead
  → false_positive
  → needs_human
  → closed
```

Every finding resolves to one of: fixed immediately, converted to a bead, attached as blocker to an existing bead, rejected as false positive with note, or escalated to human decision.

### 9.4 Convergence detector

Track findings per round, severe findings per round, duplicate findings, fixes per round, new beads per round, test failures fixed, coverage delta, complexity delta, cost/time per useful finding.

```text
not_started
  → high_yield
  → medium_yield
  → low_yield
  → saturated
  → final_gate_ready
```

A round is "saturated" when new useful findings are low relative to cost and effort, and remaining findings are mostly duplicates, low severity, or already tracked as beads.

### 9.5 Specialized audits

When review saturation is reached but the user wants further hardening, Hoopoe offers targeted skills/workflows: mock-code finder, deadlock/concurrency finder, security audit for SaaS, performance profiling, project reality check, reasoning-mode analysis, golden artifact testing, fuzzing, e2e testing with logging/no mocks, UI polish review. Each audit creates beads instead of free-floating todos.

---

## 10. Observability and recovery

Every meaningful daemon action writes to `~/.hoopoe/audit.jsonl` with actor, project, action, reason, command preview, result, and artifact pointers. Each entry carries a monotonic `seq` number per project (not just `time`) so multi-process actors — operator loop + user actions + adapter callbacks all writing concurrently — order deterministically under clock skew.

**Sequence-cursor + snapshot-on-reconnect** (pattern lifted from t3code's `OrchestrationRecoveryCoordinator`). Every WebSocket event carries `sequence: NonNegativeInt` per channel. The desktop tracks `latestSequence` + `highestObservedSequence` per channel. On disconnect or detected gap, it calls `replayEvents(channel, sinceSequence)` against the daemon's append-only log and merges results idempotently. Subscribe-RPCs return a snapshot first, then live deltas, on the same stream — there is no separate "snapshot" vs "subscribe" path. This is what makes laptop sleep, daemon restart, and tunnel re-establishment work without state corruption.

A Recovery/Diagnostics screen exposes daemon status, tunnel status, NTM sessions, active and stuck jobs, stale locks, last operator ticks, tool versions, disk pressure, recent audit events, and repair actions.

Audit log schema and replay protocol in `docs/security.md`.

---

## 11. Packaging and updates

**Build pipeline** (lifted from t3code's `scripts/build-desktop-artifact.ts` + `.github/workflows/release.yml`). The orchestrator stages all dist artifacts to a temp directory, **synthesizes a self-contained `package.json`** resolving the workspace catalog into concrete versions, runs `bun install --production` there, then invokes `bunx electron-builder` against the staged dir. The staged `package.json` is the source of build truth, not the repo's — this avoids electron-builder's flaky monorepo workspace support.

**Desktop distribution.** macOS signed and notarized DMG (arm64 + x64) for v1. `electron-updater` against GitHub Releases, with channel selection (`latest` vs `nightly`) per-user via `desktop-settings.json`. 15-second startup delay before first update check, 4-hour poll. `--publish never` on builder, manual upload in a separate CI step (more control). For local update testing, use a mock-update-server pattern (`scripts/mock-update-server.ts`).

**Daemon distribution.** Single static Go binary, downloaded via signed release URL, verified by checksum. Upgrade flow: download → verify → `stop service` → backup config/db → install → start → verify `/v1/version` → run compatibility checks. Desktop detects daemon version mismatch and offers upgrade.

**Tool version pinning.** Record ACFS/tool versions, warn on unsupported versions, allow user to pin/upgrade, run adapter contract tests against pinned versions, show drift in settings.

**Cross-platform stance.** Mac-only for v1. The lifted build matrix supports Linux AppImage and Windows NSIS; we keep those code paths but don't ship those targets in v1.

---

## 12. Milestone roadmap

### Phase 0 — Research spike and integration contract

Goal: prove the stack can be read and controlled from code.

Deliverables: test VPS with ACFS; NTM server running; sample project with `br`/`bv`; one script that produces a full machine-readable JSON snapshot covering Git, beads, `bv` triage, NTM session, Agent Mail messages/reservations, and health metrics; documented command/API contracts; parser fixtures.

**Exit:** one command produces a reliable machine-readable project snapshot.

### Phase 1 — Monorepo + desktop shell + lifted scaffolding

Initialize Hoopoe monorepo (Turbo + Bun workspaces, `apps/{desktop, daemon}`, `packages/{schemas, design-system}`).

**Vendor from t3code (MIT, see Appendix B), adapt for Hoopoe:**
- Build pipeline: `scripts/build-desktop-artifact.ts`, `.github/workflows/release.yml`, `scripts/mock-update-server.ts`. Strip Win/Linux from CI matrix; keep code paths.
- Desktop lifecycle: `apps/desktop/src/{clientPersistence, backendPort, backendReadiness, serverListeningDetector, desktopSettings, updateMachine, updateChannels, updateState, runtimeArch, syncShellEnvironment, windowReveal, confirmDialog, appBranding}.ts`. Rebrand. Rewire backend lifecycle to launch the Go daemon binary instead of `process.execPath` with `ELECTRON_RUN_AS_NODE`. Drop the `ELECTRON_RUN_AS_NODE` codepath.
- Decompose t3code's 2,175-line `apps/desktop/src/main.ts` into `BackendLifecycle`, `UpdateMachine`, `IpcRegistry`, `WindowManager`, `SettingsBridge`, `AuthBridge` modules from day one — do not inherit the monolith.
- Settings system: three-store split (`~/.hoopoe/userdata/{daemon-settings.json, desktop-settings.json, client-settings.json}`) with hot-reload + atomic write + PubSub change stream + `relaunchDesktopApp(reason)` for restart-on-toggle. Schema-validated reads.
- Keybindings: `~/.hoopoe/keybindings.json` array of `{key, command, when}`, recursive-descent parser for `when`, AST compiled and shipped to client over WS, last-rule-wins, file-watch-debounce. **Add a real command registry** (`commandRegistry.register(id, handler, {whenContextKeys})`) — t3code's implicit string-switch dispatch won't scale to per-agent commands.

**Build greenfield:**
- Design tokens (cream/dark sidebar, agent-family color palette, status pills, priority chips, coverage-bar ramp).
- Five-stage routed shell with `STAGE N — VERB` chrome.
- Reusable components: `StageHeader`, `AgentTile`, `BeadCard`, `StatusPill`, `PriorityChip`, `CoverageBar`, `TerminalPane`, `TimelineRow`, `HealthKpiCard`, `ApprovalDialog`, `CommandPalette`.
- ⌘K command palette with the registry from above.
- macOS Keychain integration via Electron `safeStorage`.

**Exit:** visual review against reference design passes; app can navigate stages; `bun run dist:desktop:dmg:arm64` produces a signed/notarized DMG; auto-update flow works against `mock-update-server`; settings hot-reload demonstrated; ⌘K palette opens.

### Phase 2 — VPS connection, auth, and daemon skeleton

**Daemon (Go, greenfield):**
- HTTP/WS scaffolding with chi/echo + gorilla/nhooyr.
- `/health`, `/v1/version`, `/v1/events` (SSE + WebSocket), `/v1/jobs`.
- `BootstrapCredentialService`: 12-char Crockford pairing tokens (in-memory single-use grant + JSONL persistence). Re-issue via `hoopoe auth pairing create`.
- `SessionCredentialService`: HMAC-signed bearer tokens (30d), HMAC WS tokens (5min, stateless with `sid` claim). Single signing secret in `ServerSecretStore` (32 bytes random).
- `hoopoe auth {pairing,session} {create,list,revoke}` CLI.
- `Type=notify` systemd integration (sd_notify ready signal).
- Sequence-cursor on every WS event + `replayEvents(channel, sinceSequence)` endpoint.
- Bound channels everywhere — no unbounded PubSub (anti-pattern from t3code investigation).

**Desktop (lifted + adapted):**
- SSH profile manager + tunnel manager (`ssh2` in main process).
- Auth bridge: capture pairing token from bootstrap stdout, exchange over tunnel for bearer, persist via `safeStorage.encryptString`.
- WS-token issuance just-in-time before each WebSocket connect.
- Reconnect-resubscribe + sequence-gap detection (`OrchestrationRecoveryCoordinator` shape).

**Bootstrap:** `bootstrap.sh` over SSH installs daemon binary, configures systemd, starts service, prints initial pairing token to stdout.

**Exit:** connect to existing VPS → daemon installed → bearer issued → WS tunnel up → stream a remote job log → simulate disconnect, watch sequence-gap replay close the gap with no UI corruption.

### Phase 3 — ACFS onboarding and tool inventory

Setup wizard; preflight checks; ACFS install/doctor integration; resumable setup checkpoints; tool inventory screen; daemon upgrade flow.

**Exit:** fresh supported VPS reaches "ready" state from Hoopoe.

### Phase 4 — Project registry and Git

Create/import/clone project; project readiness checks; `.hoopoe` initialization; Git status top bar; AGENTS.md detection/editor link; `br` initialization check; `ru --json` multi-repo support.

**Exit:** user can open a repo-backed project and see accurate Git/tool state.

### Phase 5 — Plans workspace

Plan cards; CodeMirror plan editor; artifact rail; import/create flows; multi-model candidate jobs; synthesis and refinement artifacts; plan quality tracker; lock plan action.

**Exit:** one-paragraph idea can become a locked plan with candidate/synthesis artifacts.

### Phase 6 — Bead conversion and quality tracker

`br` adapter; plan-to-beads job; `br sync --flush-only`; traceability map; bead quality tracker; polish round jobs; `bv` adapter.

**Exit:** locked plan converts into real `br` beads with dependencies and traceability.

### Phase 7 — Kanban, DAG, Force views

Kanban columns/cards; bead drawer; DAG graph; Force graph; filters; dependency editing; cycle warnings; critical path and ready frontier; `bv --robot-triage` panel.

**Exit:** user can curate beads visually and understand graph state without a terminal.

### Phase 8 — Swarm launch MVP

Swarm composition form; NTM launch integration; staggered kickoff; launch prompt renderer; agent grid; basic agent status; terminal/log tail; send/broadcast/interrupt/stop.

**Exit:** launch and observe a mixed small swarm against a real bead set.

### Phase 9 — Activity and Agent Mail

Timeline; mail ingestion; reservation view; urgent alerts; overseer compose; bead/agent pivot links; conflict/stale reservation warnings.

**Exit:** user can coordinate the swarm from Hoopoe without opening Agent Mail manually.

### Phase 10 — Operator loop

4-minute loop; idle/wedged/rate-limit detection; stale bead detection; stale reservation detection; build contention detection; disk cleanup; marching orders; audit event visibility.

**Exit:** Hoopoe can tend a real swarm for an hour with visible, explainable interventions.

### Phase 11 — Code Health

Health adapter discovery; test/coverage/complexity parsing; health snapshots; KPI cards; file health table; hotspot scoring; create bead from hotspot; trends.

**Exit:** swarm round updates health; low-coverage/high-complexity files can become beads.

### Phase 12 — Review mode and convergence

Review-only swarm mode; review round jobs; fresh-eyes prompts; cross-agent review; finding tracker; finding-to-bead conversion; convergence dashboard; final landing checklist.

**Exit:** completed implementation transitions into structured review/hardening and reaches final gate.

### Phase 13 — Provider automation and production polish

One provider plugin (Hetzner first); cost estimate and teardown; polished empty/loading/error states; onboarding tour; diagnostics screen; crash reports opt-in; daemon upgrade system end-to-end; documentation and demo project.

(Signed/notarized DMG and auto-update infrastructure are already in place from Phase 1's lift — this phase is about polish, error UX, and provider automation, not building the release pipeline.)

**Exit:** a new user can install Hoopoe, connect/provision a VPS, import a project, create a plan, convert beads, launch agents, monitor review, and land a small project.

---

## 13. MVP scope

### Must include

Electron app with five-stage shell and design system; existing-VPS connection; daemon install over SSH; SSH tunnel and event stream; ACFS install/doctor/tool inventory; project import/create; plan import/create/editor; plan-to-beads conversion through `br`; bead Kanban and basic DAG; `bv --robot-triage` panel; NTM swarm launch; agent grid with status and log tail; Agent Mail timeline; basic operator loop for idle/stale agents; basic code health scan; audit log.

### Can defer

Multi-provider automatic VPS provisioning; perfect PTY fidelity for all panes; direct mTLS public daemon mode; full spend precision; advanced Force graph interactions; full language coverage; CASS/CM deep memory workflows; collaborative multi-user teams; hosted relay/cloud sync; Mac App Store distribution.

### Walking skeleton order

1. Desktop shell with design tokens and five routes.
2. Existing-VPS SSH connection and tunnel.
3. Daemon `/version` and `/events`.
4. Remote job runner with log streaming.
5. Project discovery.
6. `br` list adapter and Kanban.
7. `bv --robot-triage` endpoint.
8. Plan editor/import.
9. Convert-to-beads job.
10. NTM status ingestion.
11. Swarm grid prototype.

This order ensures every later feature is a vertical slice, not a pile of disconnected UI.

---

## 14. Risks and mitigations

**PTY streaming fidelity fails.** Prototype a single pane early; use NTM stream/robot surfaces first; keep tmux capture fallback; use ring buffers and reconnect cursors; treat terminal output as observability, not canonical state.

**Tool output drift breaks adapters.** Prefer robot/API/JSON surfaces; pin versions; golden tests; tool inventory and compatibility warnings; user-controlled updates.

**Hoopoe cache diverges from canonical state.** Periodic reconciliation; canonical tool state wins; explicit stale-cache indicators; "reload from tools" action; source-of-truth table in docs.

**First install is brittle.** Existing VPS first; checkpointed setup; clear logs plus structured steps; one provider only after manual path works; diagnostics and resume.

**Costs run away.** Budget caps; alert thresholds; rate-limit detection; CAAM integration when configured; stop/pause policies; spend estimates labeled clearly.

**Agents compete for builds/tests.** Build queue; `rch` preference; dedupe repeated commands; operator warnings; disk pressure cleanup.

**Stale agents hold beads/reservations hostage.** Stalled bead detection; stale reservation detector; forced release with audit; reopen/reassign workflows; review of in-progress age.

**Unsafe commands accidentally exposed.** Typed command specs; allowlist; path sandboxing; approval gates; DCG/NTM safety checks; audit log; no arbitrary shell API.

**Planning quality is weak.** Competing model candidates; synthesis artifacts; quality tracker; fresh-eyes review; lock gate; bead traceability.

**Users trust subjective scores too much.** Label scores as decision aids; show underlying evidence; allow override; keep canonical artifacts visible.

**Laptop sleep breaks perception of reliability.** VPS owns jobs/loops; event replay; pane ring buffers; reconnect UI; no swarm dependency on Electron process.

**Lifted code carries Codex-shaped assumptions.** The t3code desktop layer was written for a chat/agent product, not a staged-cockpit product. Subtle assumptions (thread-centric data shapes, "provider" abstractions, message-list virtualization) may leak through scrubbing. Mitigation: scrub aggressively in Phase 1, write integration tests against Hoopoe's own flows immediately, refactor anything that still says `thread`/`provider`/`chat` after week 2.

**Upstream t3code drift.** They ship fixes (auth hardening, updater improvements, lifecycle bug fixes) we'd want. We can't cleanly merge because we've stripped/refactored half the surface. Mitigation: pin a t3code commit at lift time; quarterly review of their CHANGELOG for security-relevant fixes; cherry-pick deliberately, not automatically.

**`PubSub.unbounded` patterns leak through.** T3code uses `PubSub.unbounded` everywhere — a memory-leak landmine when slow consumers sit on fast producers (terminal output, activity stream). Hoopoe's daemon must bound channels at design time. Catch via load tests with a wedged consumer.

---

## 15. Definition of success

A successful Hoopoe session looks like this:

1. User opens the app.
2. Hoopoe reconnects to VPS and project.
3. Top bar shows repo, branch, cleanliness, tool health, and swarm state.
4. Plans show current plan artifacts and status.
5. Beads show ready, blocked, critical, stale, and in-review work.
6. User launches a mixed NTM swarm.
7. Agents read `AGENTS.md`, register with Agent Mail, use `bv`, claim beads through `br`, reserve files, implement, test, and report.
8. Hoopoe streams live agent state, mail, reservations, build/test events, and graph changes.
9. The operator loop nudges idle/stuck/rate-limited agents and explains every intervention.
10. Code Health updates after commits/rounds.
11. Review mode runs fresh-eyes and cross-agent reviews.
12. Findings become fixes or beads.
13. Convergence is visible.
14. The session lands with synced beads, clean Git, passing tests or documented exceptions, and a restartable audit trail.

That is the actual product: not a pretty wrapper around terminals, but a reliable cockpit for **planning, bead graph curation, swarm tending, and quality convergence**.

---

## 16. Immediate first engineering tasks

**Phase 0 (parallel) — Research spike on real ACFS VPS.** ~3 days.
1. Stand up an Ubuntu 24.04 VPS with ACFS installed (Hetzner / DigitalOcean / existing).
2. Write a script that produces one machine-readable JSON snapshot covering: Git status, `br list --json`, `bv --robot-triage`, `bv --robot-plan`, `bv --robot-insights`, `ntm --robot-snapshot`, Agent Mail dump, file reservations, lizard health.
3. Capture parser fixtures for every output format. Document any drift from expected shapes.
4. Identify gotchas (TUI invocations, undocumented flags, version skew) before writing adapters.

**Phase 1 (week 1) — Vendor + scaffold.**
1. `git clone github.com/pingdotgg/t3code /tmp/t3code-pinned`. Pin commit SHA in `docs/source-provenance.md`.
2. Initialize Hoopoe monorepo: Turbo + Bun workspaces, `apps/{desktop, daemon}`, `packages/{schemas, design-system}`.
3. Vendor t3code build pipeline (`scripts/build-desktop-artifact.ts`, `.github/workflows/release.yml`, `scripts/mock-update-server.ts`). Strip Win/Linux from CI matrix. Verify a hello-world Electron app produces a signed/notarized DMG end-to-end.
4. Vendor t3code desktop lifecycle files (Appendix B). Decompose `main.ts` into `BackendLifecycle/UpdateMachine/IpcRegistry/WindowManager/SettingsBridge/AuthBridge` modules. Rebrand. Strip Codex-specific code.
5. Create `packages/design-system/` with tokens (cream/dark sidebar, agent palette, status pills, priority chips, coverage ramp).
6. Build the five-stage routed shell with `STAGE N — VERB` chrome and the reusable component set.

**Phase 1 (week 2) — Settings, keybindings, command palette.**
1. Vendor settings system (three-store split, hot-reload, atomic write, PubSub change stream).
2. Vendor keybindings (file-watch + AST + last-rule-wins). Add the command registry layer (`commandRegistry.register` — our addition over t3code).
3. Wire ⌘K palette against the registry.
4. Smoke test: app boots, navigates stages, settings hot-reloads, palette opens.

**Phase 2 (weeks 3–4) — Auth, tunnel, daemon skeleton.**
1. Daemon: chi/echo HTTP, gorilla WS, `/health`, `/v1/version`, `/v1/events`, `/v1/jobs`. Bind 127.0.0.1.
2. Daemon auth: `BootstrapCredentialService` (12-char pairing tokens) + `SessionCredentialService` (HMAC bearer + WS token). `hoopoe auth` CLI.
3. Daemon: sequence-cursor on every WS event + `replayEvents` endpoint. Bound channels.
4. systemd unit (Type=notify).
5. Desktop: SSH tunnel manager (`ssh2`), AuthBridge, WS-token-on-connect, reconnect-resubscribe loop.
6. `bootstrap.sh` over SSH: install daemon, configure systemd, start, print pairing token.
7. End-to-end smoke: cold VPS → `bootstrap.sh` → bearer issued → WS tunnel up → stream a `tool_inventory` job log.

Do not start with provider automation, spend charts, or polished graph animations. The first milestone is a working cockpit connected to a real VPS daemon with one real project and one real tool adapter.

---

## 17. References

- Agentic Coding Flywheel methodology: `agent-flywheel.com/complete-guide`
- ACFS setup: `github.com/Dicklesworthstone/agentic_coding_flywheel_setup`
- NTM: `github.com/Dicklesworthstone/ntm`
- Beads Rust: `github.com/Dicklesworthstone/beads_rust`
- Repo Updater: `github.com/Dicklesworthstone/repo_updater`
- Agent Mail: `github.com/Dicklesworthstone/mcp_agent_mail`
- Beads workflow skill: `jeffreys-skills.md/skills/beads-workflow`
- Vibing with NTM skill: `jeffreys-skills.md/skills/vibing-with-ntm`

Phase 0 must verify actual installed command names, output formats, version compatibility, and exact API surfaces on a fresh ACFS VPS before downstream phases assume them.

---

## Appendix A — Where the operational details live now

The full earlier version is preserved at `plan.full.md`. Detail moved out of this document:

| Cut from | New home |
|---|---|
| Repo workspace structure | top-level `README.md` at scaffold time |
| Persistent data layout (`/data/projects/...`, `~/.hoopoe/`, `~/Library/Application Support/Hoopoe/`) | `docs/source-of-truth.md` |
| Entity schemas (`VpsHost`, `Project`, `Plan`, `Bead`, `SwarmSession`, `Agent`, `FileReservation`, `BudgetPolicy`, `BuildQueuePolicy`, `SwarmLaunchSpec`) | `packages/schemas/` (TS + Go generated) |
| `CommandSpec`, full approval-checkpoint matrix, audit-log schema | `docs/security.md` |
| Tool-inventory JSON schema | `packages/schemas/` |
| `project.json`, full readiness-check list | `packages/schemas/` + `docs/onboarding.md` |
| Plans/Beads/Activity/Swarm/Code Health UI mockups, columns, drawers, KPI cards | `packages/design-system/` (Storybook + tokens) |
| `PlanQualityScore`, `BeadSetQuality`, `CodeHealthSnapshot`, `FileHealthMetric` | `packages/schemas/` |
| Daemon REST endpoint list | `packages/schemas/openapi.yaml` |
| `Job` model, lifecycle, kind enum | `packages/schemas/` |
| Design-system component inventory (`StageHeader`, `BeadCard`, `AgentTile`, etc.) | `packages/design-system/README.md` |
| Testing strategy detail (desktop tests, daemon tests, integration scenarios, E2E disposable VPS, smoke checks) | `docs/testing.md` |
| Provider plugin contract | `packages/schemas/` |
| Pane stream event types | `packages/schemas/` |

Schemas, API contracts, and component inventories belong in source code so the type system and tests catch drift. The plan reserves itself for vision, decisions, and roadmap.

---

## Appendix B — T3 Code lift inventory

Source: `github.com/pingdotgg/t3code`, MIT License (Copyright 2026 T3 Tools Inc.). Pin a specific commit SHA in `docs/source-provenance.md` at lift time. MIT requires only that the copyright notice be preserved in any substantial portion of the source.

**Vendoring layout.** Lifted files land under `apps/desktop/src/vendored/t3code/` with MIT notice preserved at the top of each file. Adaptations (rebranding, rewiring) happen in our own files that import from `vendored/`. Do not edit `vendored/` files in place except for mechanical mass renames — keep the diff against upstream small enough to re-merge later if needed.

### Files lifted

| Source (`/tmp/t3code-pinned/`) | Hoopoe target | Adaptation |
|---|---|---|
| `apps/desktop/src/clientPersistence.ts` | `apps/desktop/src/vendored/t3code/clientPersistence.ts` | Replace `T3CODE_*` env keys with `HOOPOE_*`; keep `safeStorage.encryptString` logic intact |
| `apps/desktop/src/backendPort.ts` | same | Default port → Hoopoe's |
| `apps/desktop/src/backendReadiness.ts` | same | Replace t3code stdout signature with Hoopoe daemon's "listening on" line |
| `apps/desktop/src/serverListeningDetector.ts` | same | Update parser regex |
| `apps/desktop/src/desktopSettings.ts` | same | Schema swap (HoopoeDesktopSettings) |
| `apps/desktop/src/updateMachine.ts` | same | None |
| `apps/desktop/src/updateChannels.ts` | same | None |
| `apps/desktop/src/updateState.ts` | same | None |
| `apps/desktop/src/runtimeArch.ts` | same | None |
| `apps/desktop/src/syncShellEnvironment.ts` | same | None |
| `apps/desktop/src/windowReveal.ts` | same | None |
| `apps/desktop/src/confirmDialog.ts` | same | None |
| `apps/desktop/src/appBranding.ts` | same | Replace branding strings |
| `apps/desktop/src/main.ts` (2,175 lines) | **decomposed** into `BackendLifecycle.ts`, `UpdateMachine.ts`, `IpcRegistry.ts`, `WindowManager.ts`, `SettingsBridge.ts`, `AuthBridge.ts` under `apps/desktop/src/main/` | Major: split monolith on day one. Drop `ELECTRON_RUN_AS_NODE` codepath. Replace `process.execPath`-as-Node trick with launching Hoopoe's Go daemon binary. Drop the local-pairing-token-via-FD-3 path for v1 (not needed when daemon is remote); keep the code for future local-demo-mode |
| `scripts/build-desktop-artifact.ts` | `scripts/build-desktop-artifact.ts` | Strip Linux/Windows targets from default; keep code paths under flags. Replace `@t3tools` → `@hoopoe` |
| `scripts/mock-update-server.ts` | same | None |
| `scripts/release-smoke.ts` | same | None |
| `.github/workflows/release.yml` | same | Strip Linux + Windows matrix entries (keep mac arm64 + x64). Update secrets (CSC_LINK, APPLE_API_KEY, GH_TOKEN). Rename workflows |
| `.github/workflows/*.yml` (typecheck, lint, test) | same | Adapt to Hoopoe's command names |
| `apps/desktop/scripts/{dev-electron, start-electron, smoke-test}.mjs` | same | None |
| `electron-builder` config (in `apps/desktop/package.json` / `build.yml`) | same | Update `productName`, `appId`, signing identity |

### Patterns lifted (re-implemented, not copied)

| Pattern | Source reference | Hoopoe location |
|---|---|---|
| Two-token auth (pairing → bearer → WS-token) | `apps/server/src/auth/Layers/{Bootstrap,Session}CredentialService.ts` | `apps/daemon/internal/auth/` (Go, greenfield) |
| Settings three-store split + hot reload + PubSub | `apps/server/src/serverSettings.ts` | `apps/daemon/internal/settings/` (Go) + `apps/desktop/src/main/SettingsBridge.ts` |
| Keybindings AST + file watch + last-rule-wins | `apps/server/src/keybindings.ts`, `apps/web/src/keybindings.ts` | `apps/desktop/src/main/keybindings/` (TS, no Effect) |
| Sequence-cursor + snapshot-on-reconnect | `apps/web/src/orchestrationRecovery.ts`, `apps/web/src/rpc/wsTransport.ts` | `apps/desktop/src/renderer/sync/` (TS) + `apps/daemon/internal/events/` (Go) |
| FD-3 bootstrap envelope (local demo mode only) | `apps/desktop/src/main.ts:1395-1413` | `apps/desktop/src/main/BackendLifecycle.ts` (deferred to local-demo path) |
| Atomic file write (tempfile + rename) | `apps/server/src/atomicWrite.ts` | `apps/daemon/internal/storage/` (Go: `os.Rename` + `Sync`) + `apps/desktop/src/main/atomicWrite.ts` (TS) |
| `auth pairing/session create|list|revoke` CLI shape | `apps/server/src/cli.ts:809-969` | `apps/daemon/cmd/hoopoe/auth.go` |

### Files we explicitly do NOT lift

- `apps/server/src/` (entire TypeScript server) — Hoopoe daemon is Go.
- `apps/web/` (chat-centric UI) — Hoopoe's renderer is purpose-built.
- `apps/marketing/` — irrelevant.
- `packages/effect-acp/` — Agent Client Protocol; Hoopoe wraps CLIs, not ACP-speaking agents.
- `packages/effect-codex-app-server/` — OpenAI-internal protocol.
- `packages/contracts/` — would have to be rewritten as Go-readable schemas; recreate from scratch in `packages/schemas/` instead.
- Effect framework wholesale — adopt patterns in plain TypeScript.

### Anti-patterns to refuse

From t3code, learned not-to-copy:

1. **`PubSub.unbounded` everywhere** — bound all channels in the daemon.
2. **Terminal history as one big string blob on `terminal.open`** — chunk it.
3. **Silent client-side message caps** (`MAX_THREAD_MESSAGES = 2_000`) — virtualize or show "showing latest N."
4. **2,175-line `main.ts` monolith** — decompose on day one, not someday.
5. **No port-conflict resolution** — implement `findOpenPort(preferred)` in `BackendLifecycle.ts`.
6. **Implicit command dispatch via string-switch** — build a real command registry from day one.
7. **Unknown `when`-clause context keys evaluate to `false`** — validate keys against a known set at parse time, fail loudly on typos.

### License attribution

Add to top of every vendored file:

```text
// Originally from github.com/pingdotgg/t3code (MIT License)
// Copyright (c) 2026 T3 Tools Inc.
// Adapted for Hoopoe.
//
// Full MIT license text: vendored/t3code/LICENSE
```

Copy `t3code/LICENSE` to `apps/desktop/src/vendored/t3code/LICENSE`. Document the lift in the project root `NOTICE` file.
