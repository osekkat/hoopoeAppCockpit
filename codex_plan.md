# Hoopoe Implementation Plan

Hoopoe is an Electron desktop macOS app that centralizes and automates the Agentic Coding Flywheel workflow. It replaces manually coordinating `br`, `bv`, `ntm`, Agent Mail, model CLIs, and terminal sessions with a single GUI control center.

The actual flywheel execution runs on a VPS. The desktop app connects to the VPS and acts as a dashboard, project manager, operator console, and automation controller.

## Reference Sources

- Agent Flywheel complete guide: https://agent-flywheel.com/complete-guide
- `repo_updater`: https://github.com/Dicklesworthstone/repo_updater
- `ntm`: https://github.com/Dicklesworthstone/ntm
- `agentic_coding_flywheel_setup`: https://github.com/Dicklesworthstone/agentic_coding_flywheel_setup
- `beads_rust`: https://github.com/Dicklesworthstone/beads_rust
- Agent Mail: https://github.com/Dicklesworthstone/mcp_agent_mail
- Beads workflow skill: https://jeffreys-skills.md/skills/beads-workflow
- Vibing with NTM skill: https://jeffreys-skills.md/skills/vibing-with-ntm

## Core Architecture

Hoopoe should be built as a two-plane system.

| Layer | Runs Where | Responsibility |
|---|---:|---|
| Electron macOS app | User's Mac | UI, SSH pairing, credentials, dashboards, operator controls |
| Hoopoe VPS daemon | VPS | Stable API wrapper around `br`, `bv`, `ntm`, Agent Mail, git, coverage tools |
| Agentic tooling | VPS | Actual execution: Claude Code, Codex, Gemini CLI, tmux sessions, Beads, Agent Mail |
| Project repos | VPS | Git-backed source trees with `.beads/`, plans, logs, metrics, artifacts |

The desktop app should not scrape terminal panes as its primary integration. SSH should be used for bootstrap, tunnel setup, and emergency fallback. Normal operation should go through a localhost-bound daemon on the VPS reached through an SSH tunnel.

`ntm` already exposes robot snapshots, REST/SSE/WebSocket-style automation, and `ntm serve`, so Hoopoe should consume those surfaces where available instead of reimplementing tmux orchestration.

## Product Principle

Hoopoe should not replace the flywheel tools. It should coordinate them.

The Flywheel methodology separates plan space, bead space, and code space:

- Plan space: architecture, workflows, tradeoffs, and product intent are shaped in markdown.
- Bead space: the markdown plan is converted into executable, dependency-aware work units.
- Code space: agents implement, review, test, and harden the project.

Hoopoe's job is to make these stages visible, stateful, resumable, and less manually operated.

## Recommended Stack

Desktop app:

- Electron
- TypeScript
- React
- Vite
- Tailwind or plain CSS modules, depending on preferred design control
- SQLite for local app cache/state
- macOS Keychain via `keytar` or equivalent for secrets
- `ssh2` or a mature SSH tunnel library for pairing and tunnel management

VPS daemon:

- Go if single-binary deployment and process supervision are the priority
- TypeScript/Node if sharing types and code with the Electron app is more important
- HTTP API bound to `127.0.0.1`
- Server-sent events or WebSocket stream for live updates
- Local SQLite state for daemon jobs and cached snapshots

The daemon should be the only Hoopoe component that shells out to `br`, `bv`, `ntm`, git, test runners, coverage tools, and model CLIs.

## Persistent Data Model

Create a Hoopoe metadata directory inside each project:

```text
.hoopoe/
  project.json
  plans/
    plan-001.md
    plan-001.meta.json
  jobs/
    job-*.json
  metrics/
    coverage-*.json
    complexity-*.json
  swarm/
    sessions.json
```

Core entities:

| Entity | Key Fields |
|---|---|
| `VpsHost` | host, user, SSH fingerprint, setup status, tool versions |
| `Project` | id, repo URL, branch, local VPS path, git status, active plan |
| `Plan` | id, file path, state, source model outputs, refinement rounds, linked bead IDs |
| `Bead` | id, title, status, priority, dependencies, labels, owner, plan id |
| `SwarmSession` | ntm name, project id, agents, launch prompt, mode, status |
| `Agent` | name, tool, model, pane id, current bead, tokens, runtime, health |
| `MailThread` | thread id, bead id, participants, latest message, urgency |
| `HealthSnapshot` | coverage, tests, complexity, churn, hotspots |
| `Job` | command, status, logs, started/ended timestamps, artifact paths |

Plan states should be explicit:

```text
draft
competing_plans_running
synthesis_ready
refining
plan_finalized
bead_conversion_running
beads_created
beads_polishing
beads_finalized
swarm_running
review_rounds
completed
```

## Stage 1: First-Install VPS Onboarding

Build a guided setup wizard.

1. Choose provider mode: support existing VPS first, provider automation later.
2. Collect SSH host, user, and key.
3. Verify OS, CPU, RAM, disk, git, curl, and tmux.
4. Install the Agentic Coding Flywheel stack from `agentic_coding_flywheel_setup`.
5. Install the Hoopoe daemon as a `systemd` user or system service.
6. Start Agent Mail and verify health.
7. Run version probes for `br`, `bv`, `ntm`, Claude Code, Codex, Gemini CLI, and `ru`.

The setup repo describes itself as bootstrapping a fresh Ubuntu VPS into a multi-agent AI development environment with coding agents, session management, safety tools, and coordination infrastructure.

### Setup API

The desktop app should expose setup as a step-by-step checklist, while the daemon or bootstrap script records machine-readable results:

```json
{
  "host": "vps-1",
  "status": "ready",
  "checks": {
    "ssh": "ok",
    "os": "ubuntu-24.04",
    "disk_free_gb": 180,
    "tmux": "3.4",
    "br": "0.1.45",
    "bv": "0.16.0",
    "ntm": "installed",
    "agent_mail": "healthy"
  }
}
```

## Stage 2: Project Creation And Import

Support three flows.

| Flow | Behavior |
|---|---|
| New repo | Create directory on VPS, `git init`, seed `AGENTS.md`, initialize Hoopoe metadata |
| Clone repo | Clone from GitHub, detect stack, run setup checks |
| Existing VPS repo | Select path, validate git repo, initialize missing `.hoopoe/` and `.beads/` |

Use `repo_updater` for multi-repo sync/status because it has JSON and non-interactive modes intended for automation.

Project checks:

- Is it a git repo?
- Is the working tree clean?
- Is `.beads/` present?
- Is `AGENTS.md` present?
- Are package/test commands detectable?
- Is Agent Mail configured for this project?
- Is `ntm` aware of any existing session for this repo?

## Stage 3: Plan Creation

Implement plan creation as a job pipeline, not as a chat box only.

Pipeline:

1. User enters rough idea, constraints, desired stack, target users, known risks.
2. Hoopoe launches competing model jobs: GPT, Claude, Gemini, optionally Grok if configured.
3. Store each model output as a plan candidate.
4. Run synthesis prompt to produce a hybrid master plan.
5. Run refinement rounds until changes become incremental.
6. Mark the plan `plan_finalized`.

The Flywheel workflow recommends competing frontier models, synthesis into a "best of all worlds" plan, and repeated refinement before bead conversion.

### Plan UI

The Plans page should show:

- Plan cards with title, status, line count, bead count, linked project, and last activity.
- Plan editor with markdown preview.
- Candidate plan comparison.
- Refinement round history.
- Model output artifacts.
- "Convert to beads" action once finalized.

### Plan Quality Tracker

Track quality signals:

- Has user intent been captured?
- Are user workflows explicit?
- Are major architectural decisions justified?
- Are failure modes covered?
- Are testing obligations included?
- Are rollout/deployment constraints covered?
- Are dependencies and sequencing clear?
- Is the plan self-contained enough for agents?

## Stage 4: Plan To Beads

When the plan is finalized, trigger a controlled conversion job.

1. Ensure `br init` has run.
2. Launch a high-reasoning agent on the VPS with the plan-to-beads prompt.
3. Require all bead creation/modification through `br`.
4. Run `br sync --flush-only`.
5. Snapshot `.beads/issues.jsonl`.
6. Link created beads back to the plan metadata.

`br` stores primary data in SQLite with Git-friendly JSONL export in `.beads/issues.jsonl`, so Hoopoe should treat the CLI and JSONL export as the stable integration layer instead of mutating SQLite directly.

### Bead Schema In Hoopoe

Hoopoe should normalize beads into its own read model:

```ts
type Bead = {
  id: string;
  title: string;
  description: string;
  status: "open" | "in_progress" | "review" | "closed" | "blocked";
  priority: 0 | 1 | 2 | 3;
  type: "feature" | "bug" | "task" | "test" | "docs" | "refactor";
  labels: string[];
  dependencies: string[];
  dependents: string[];
  assignee?: string;
  planId?: string;
  coverage?: number;
  complexity?: number;
  updatedAt: string;
};
```

## Stage 5: Bead Visualization And Curation

Build two primary views.

| View | Data Source |
|---|---|
| Kanban | `br list`, `.beads/issues.jsonl`, status/priority fields |
| DAG | `bv --robot-insights`, `bv --robot-plan`, dependency edges |

Use `bv --robot-*` only for machine reads. Bare `bv` launches an interactive TUI and should not be used from automated app flows.

### Curation Features

- Detect missing dependencies.
- Detect cycles.
- Detect oversized beads.
- Detect duplicate beads.
- Show ready, blocked, in progress, review, and closed lanes.
- Show priority.
- Show graph-theory priority: PageRank, betweenness, critical path, unblock count.
- Let the user accept automated polish recommendations.

### Bead Polish Rounds

Each polish round should create an audit artifact:

```text
.hoopoe/plans/plan-001.bead-polish-round-003.md
```

Each round should record:

- Model/tool used
- Recommendations
- Accepted changes
- Rejected changes
- New beads
- Removed or merged beads
- Dependency changes
- Remaining concerns

## Stage 6: Swarm Launch

Swarm launch should be template-driven.

Inputs:

```text
project
plan/bead scope
agent mix: cc/cod/gmi
mode: implementation | review | test-hardening | ui-polish
start staggering
max concurrent builds
token/budget limit
```

Use `ntm spawn` for launch, `ntm add` for scaling, `ntm status` or robot snapshot for status, and `ntm send` for prompts. `ntm` supports mixed Claude/Codex/Gemini swarms and named tmux sessions.

### Default Launch Policy

- Stagger agents by at least 30 seconds.
- Force initial prompt to include `AGENTS.md` reread.
- Require Agent Mail registration.
- Require `bv --robot-triage` before claim.
- Require `br update --status in_progress` for claimed work.
- Require file reservation before edits.
- Require progress mail in the bead thread.
- Avoid concurrent builds for the same project unless explicitly allowed.
- Periodically clear stale build artifacts.

### Launch Prompt Template

```text
Reread AGENTS.md so it is fresh in your mind.

You are part of a Hoopoe-managed Agentic Coding Flywheel swarm.

Use Agent Mail for coordination.
Use br for bead status.
Use bv --robot-triage to choose high-impact ready work.
Reserve files before editing.
Mark a bead in_progress before starting.
Send a message when blocked, when handing off, and when ready for review.

Do not run bare bv. Use only bv --robot-* commands.
Do not start expensive builds if another agent is already building the same project.
When you finish work, self-review with fresh eyes before closing or handing off.
```

## Stage 7: Swarm Monitoring

Build the Swarm page around a live agent grid.

| Card Field | Source |
|---|---|
| Agent name/model | `ntm`, Agent Mail registration |
| Status | `ntm status`, pane heartbeat |
| Current bead | `br`, Agent Mail thread id, prompt metadata |
| Runtime | daemon process/session timestamps |
| Tokens/spend | CLI logs where available, estimated fallback |
| CPU/RAM | VPS process telemetry |
| Last action | pane tail or `ntm` event stream |
| Health | idle, working, rate-limited, wedged, review-needed |

Operator actions:

- Send marching orders.
- Pause/resume agent.
- Kill/restart pane.
- Reassign bead.
- Flip swarm to review-only.
- Trigger fresh-eyes review.
- Run stuck-agent recovery.
- Clear stale build artifacts.
- Open full terminal view.

### Stuck Swarm Detection

Detect:

- Multiple agents claiming the same bead.
- Agent idle beyond threshold.
- Agent rate-limited.
- Pane stopped producing output.
- Bead in progress with no recent activity.
- Repeated failed test loop.
- Agent after context compaction no longer following AGENTS.md.
- Strategic drift: many commits but remaining beads no longer close the product gap.

Recommended actions:

- Send reread-AGENTS prompt.
- Send `bv --robot-triage` prompt.
- Ask for explicit status.
- Reclaim stale bead.
- Split blocked bead.
- Stop and run reality-check planning.

## Stage 8: Agent Mail Activity

Agent Mail should be a first-class Activity tab.

Use it for:

- Bead-thread timeline.
- File reservations.
- Urgent messages.
- ACK-needed messages.
- Cross-agent decisions.
- Human overseer broadcasts.

Agent Mail provides identities, inbox/outbox, searchable history, and advisory file reservation leases backed by Git and SQLite.

### Activity Page

The Activity page should show:

- Unified recent messages.
- Filters by bead, agent, urgency, project, and thread.
- File reservation conflicts.
- Agent check-ins.
- Review requests.
- Blocked work.
- Human broadcasts.

The app should allow the user to reply as the human overseer and broadcast instructions to the whole swarm.

## Stage 9: Code Health

Implement a metrics runner with language-specific adapters.

Minimum MVP:

| Metric | How |
|---|---|
| LOC | `tokei` or `cloc` |
| Unit/integration/e2e counts | parse package scripts, test reports, Playwright/Jest/Vitest/etc. |
| Coverage | consume `lcov`, `coverage-summary.json`, `cobertura.xml`, `go test -coverprofile`, `cargo llvm-cov` |
| Complexity | `lizard` initially, later language-native tools |
| Churn | git diff/log |
| Hotspots | complexity x churn x low coverage |

Run health snapshots after commits, swarm rounds, and manual refresh. Do not block the UI on full test suites; queue them as background jobs on the VPS.

### Code Health Page

Show:

- Written files
- Average coverage
- Average complexity
- Hotspots
- Per-file table
- Owner/agent attribution
- Churn
- Coverage trend
- Complexity trend
- Test counts by type

The app should be able to create new beads from code health findings.

## Automation Loops

Hoopoe should implement an operator loop that runs every few minutes during active swarms.

Loop responsibilities:

1. Refresh `ntm` status.
2. Refresh `br`/`bv` state.
3. Refresh Agent Mail.
4. Detect stale in-progress beads.
5. Detect idle or wedged agents.
6. Send prompts to agents that need input.
7. Detect review saturation.
8. Trigger next review or hardening round if needed.
9. Clear stale build artifacts when disk pressure rises.
10. Write an append-only event log.

The loop should be visible and user-controllable. Users should see what Hoopoe did and why.

## Security Model

Security requirements:

- Store SSH keys and tokens in macOS Keychain.
- Never log raw API keys, SSH private keys, OAuth tokens, or model CLI auth tokens.
- Pin SSH host fingerprints.
- Bind VPS daemon to localhost only by default.
- Use SSH tunnel from desktop app to daemon.
- Require explicit user approval for destructive operations.
- Keep an audit log for commands sent through Hoopoe.
- Support read-only dashboard mode.
- Provide emergency disconnect and stop-swarm controls.

## Command Execution Model

All long-running daemon operations should be jobs.

```ts
type Job = {
  id: string;
  projectId?: string;
  kind:
    | "vps_setup"
    | "repo_clone"
    | "plan_generate"
    | "plan_refine"
    | "bead_convert"
    | "bead_polish"
    | "swarm_launch"
    | "swarm_prompt"
    | "health_snapshot"
    | "test_run";
  status: "queued" | "running" | "succeeded" | "failed" | "cancelled";
  commandPreview: string;
  startedAt?: string;
  endedAt?: string;
  logsPath: string;
  artifactPaths: string[];
};
```

The app should stream job logs live and keep artifacts attached to the relevant project, plan, bead, or swarm session.

## UI Structure

Main navigation:

- Plans
- Beads
- Activity
- Swarm
- Code Health
- Settings

Secondary tools:

- VPS setup
- NTM sessions
- Agent Mail
- CAAM/accounts
- DCG/safety guards
- Repos

The visual design should be utilitarian and dense. This is an operational cockpit, not a marketing site.

## MVP Milestones

### MVP 0: Thin Vertical Slice

- Electron shell
- SSH pairing
- VPS daemon install
- Tunnel setup
- Run remote command
- Show logs
- Project list from VPS

### MVP 1: Project And Beads

- Project registry
- `br` integration
- Kanban view
- Bead detail drawer
- `bv --robot-triage` panel
- Basic DAG view

### MVP 2: Plans

- Plan editor/import
- Plan state machine
- Plan refinement artifacts
- Plan-to-beads job
- Plan/bead linking

### MVP 3: Swarm Control

- `ntm` launch
- Agent grid
- Pane status
- Prompt dispatch
- Session stop/restart
- Stale agent detection

### MVP 4: Agent Mail

- Thread timeline
- File reservations
- Urgent messages
- Overseer broadcast
- Bead-thread linking

### MVP 5: Code Health

- Coverage ingestion
- Complexity table
- Hotspots
- Trend history
- Create bead from hotspot

### MVP 6: Automation Loops

- Stale bead detection
- Review rounds
- Convergence detection
- Stuck-agent recovery
- Artifact cleanup
- Operator event log

### MVP 7: Distribution

- Signed macOS app
- Auto-update
- Crash reports
- Encrypted secrets
- Onboarding docs

## Testing Plan

Desktop app:

- Unit tests for state machines and API clients.
- Component tests for major views.
- Playwright or equivalent E2E against a mocked daemon.

Daemon:

- Unit tests for command parsing and normalization.
- Integration tests with fixture repos.
- Golden tests for `br`, `bv`, `ntm`, and Agent Mail output parsers.
- Job lifecycle tests.
- Recovery tests for interrupted jobs.

End-to-end:

- Spin up disposable Ubuntu VPS or local VM.
- Install stack.
- Import fixture repo.
- Create plan.
- Convert to beads.
- Launch tiny swarm with dry-run agents or mock CLIs.
- Ingest Agent Mail messages.
- Generate code health snapshot.

## Highest-Risk Areas

The main risks are not Electron UI work. They are orchestration reliability, credential handling, model CLI instability, long-running process recovery, and trustworthy state reconciliation across `br`, `bv`, `ntm`, Agent Mail, git, and shell logs.

Design the daemon around idempotent jobs, append-only event logs, and periodic reconciliation. Hoopoe should always be able to answer:

> What is true if I ignore my cache and re-read the repo/tool state?

That question should guide every integration boundary.

## Suggested First Engineering Tasks

1. Scaffold Electron + React + TypeScript app.
2. Scaffold VPS daemon with `/health`, `/version`, `/events`, and `/jobs`.
3. Implement SSH tunnel manager in the desktop app.
4. Implement daemon bootstrap over SSH.
5. Add project discovery endpoint.
6. Add `br` parser and `.beads/issues.jsonl` importer.
7. Add `bv --robot-triage` endpoint.
8. Build Beads Kanban page.
9. Add `ntm status` and `ntm --robot-snapshot` ingestion.
10. Build Swarm grid prototype.

