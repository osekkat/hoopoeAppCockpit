# Implementation plan for Hoopoe: an Electron control center for the Agentic Coding Flywheel

Hoopoe should not try to replace the Flywheel tools. It should wrap them, preserve their native sources of truth, and make the full loop observable, repeatable, and safe from one desktop UI. The core loop you described maps cleanly onto the Flywheel guide’s stages: detailed markdown plan, conversion into self-contained beads with dependencies and test obligations, swarm launch, human tending, review/testing/hardening, and final quality gates. ([Agent Flywheel][1])

The most important architectural choice is this: **Electron is only the cockpit. The VPS is the execution plane.** ACFS already bootstraps a VPS with language runtimes, Claude/Codex/Gemini CLIs, NTM, Agent Mail, `br`, `bv`, `ru`, and other stack tools, so Hoopoe should install, verify, and operate that stack rather than reimplementing it. ([GitHub][2])

---

## 1. Product shape

Build Hoopoe as five stage-based workspaces, matching the reference design:

1. **Plans**
   Create/import plan files, run multi-model planning rounds, synthesize master plans, track plan state.

2. **Beads**
   Convert plans into `br` beads, visualize bead readiness, polish dependencies, inspect missing context/testing obligations.

3. **Activity**
   Show Agent Mail messages, bead-linked threads, file reservations, warnings, stuck-agent alerts, human overseer messages.

4. **Swarm**
   Launch and monitor Claude Code, Codex, and Gemini CLI agents through NTM. Show agent status, logs, current bead, model, runtime, resource usage, token estimates, rate limits, and health.

5. **Code Health**
   Aggregate tests, coverage, cyclomatic complexity, lint/build status, hot spots, churn, and unresolved quality gates.

This keeps the UI close to the methodology while making each stage actionable instead of merely informational.

---

## 2. High-level architecture

Use a **three-layer architecture**:

```text
macOS Electron App
  ├─ React UI
  ├─ local encrypted settings
  ├─ SSH key + tunnel manager
  └─ local event cache

SSH tunnel / WebSocket / command channel
  └─ no public inbound VPS port required by default

VPS Hoopoe Agent
  ├─ wraps ACFS, br, bv, ntm, ru, Agent Mail, git, coverage tools
  ├─ streams logs/events
  ├─ runs jobs
  ├─ maintains UI cache DB
  └─ never replaces .beads, git, Agent Mail, or NTM state
```

### Why add a VPS-side Hoopoe Agent?

Electron could run every command over SSH directly, but that gets brittle fast. A lightweight sidecar on the VPS gives you:

- stable REST/WebSocket/SSE APIs for the desktop app;
- reliable background jobs that survive Electron restarts;
- command allowlisting and audit logs;
- local access to NTM’s own robot surfaces and REST/WebSocket API;
- a single place to normalize `br`, `bv`, `ntm`, Agent Mail, coverage, and git data.

NTM already exposes machine-readable robot surfaces, REST, SSE, WebSocket, and OpenAPI surfaces, so the Hoopoe Agent should consume those first before falling back to CLI parsing. ([GitHub][3])

---

## 3. Technology stack

### Desktop app

Use:

- **Electron + TypeScript**
- **React + Vite**
- **TanStack Router** for stage-based routing
- **TanStack Query** for server state
- **Zustand** for local UI state
- **React Flow or Cytoscape.js** for DAG visualization
- **xterm.js** for embedded terminal/log panes
- **Keychain integration** for SSH key passphrases, provider tokens, and API keys
- **SQLite or IndexedDB** for local cache and offline UI restore

### VPS Hoopoe Agent

Use one of these two stacks:

**Preferred:** Go
Good for long-running daemon, command execution, WebSockets, static binary packaging, systemd integration, and SSH-friendly deployment.

**Acceptable:** Bun/TypeScript
Good if you want to share types with the Electron app and move quickly, especially since ACFS installs Bun. ([GitHub][2])

For v1, I would use **Go for the VPS daemon** and **TypeScript for the desktop**.

### Shared schema

Define shared OpenAPI + JSON Schema types for:

- Project
- Plan
- Bead
- BeadGraph
- Swarm
- Agent
- AgentLogEvent
- AgentMailMessage
- FileReservation
- CodeHealthSnapshot
- Job
- JobEvent
- QualityGate
- ReviewRound

Generate TypeScript client types from the VPS agent’s OpenAPI spec.

---

## 4. Sources of truth

Do **not** invent a new canonical database for the workflow. Hoopoe should cache and present state, but the real state should stay where the Flywheel tools expect it.

| Domain              | Source of truth                               | Hoopoe behavior                         |
| ------------------- | --------------------------------------------- | --------------------------------------- |
| Project code        | Git repo on VPS                               | read status, run commands, show commits |
| Plans               | repo files under `.hoopoe/plans/` or `plans/` | edit/import/export, commit with repo    |
| Plan metadata       | `.hoopoe/hoopoe.json` plus Git                | track state, plan-to-bead mapping       |
| Beads               | `.beads/` managed by `br`                     | call `br`, never mutate DB directly     |
| Bead graph metrics  | `bv --robot-*` output                         | cache for UI graph/triage               |
| Agent sessions      | NTM/tmux                                      | call NTM API/robot surfaces             |
| Agent messages      | Agent Mail                                    | read via NTM/Agent Mail surfaces        |
| File reservations   | Agent Mail                                    | display and alert on stale reservations |
| Build/test results  | job logs + health snapshots                   | cache, compare over time                |
| Coverage/complexity | generated reports                             | normalize into Code Health              |

`br` is explicitly designed as a local-first, repo-resident issue tracker with SQLite plus JSONL export and machine-readable output, so Hoopoe should treat the `br` CLI as the write API. ([GitHub][4])

---

## 5. Project lifecycle state machine

Every project should have a lifecycle status, and every plan should have its own status.

### Project states

```text
unconfigured
  → vps_ready
  → tools_installed
  → project_imported
  → planning
  → beads_created
  → beads_polishing
  → swarm_ready
  → swarm_running
  → review_running
  → quality_gates
  → done
```

### Plan states

```text
draft
  → multi_model_expansion
  → synthesis
  → refinement_round_n
  → plan_final
  → bead_conversion_started
  → beads_created
  → bead_polish_round_n
  → beads_final
  → implementation_active
  → review_active
  → complete
```

Store this in a versioned repo file:

```json
{
  "schemaVersion": 1,
  "projectId": "cass-memory",
  "plans": [
    {
      "id": "plan-001",
      "path": "plans/cass-memory-core.md",
      "title": "CASS Memory — Core Architecture",
      "state": "beads_final",
      "createdAt": "2026-04-29T00:00:00Z",
      "updatedAt": "2026-04-29T00:00:00Z",
      "beadIds": ["br-a1b2", "br-c3d4"],
      "rounds": [
        {
          "type": "planning_refinement",
          "round": 1,
          "models": ["claude", "codex", "gemini"],
          "outputPath": ".hoopoe/runs/plan-001/round-001/"
        }
      ]
    }
  ]
}
```

---

## 6. First-install and VPS onboarding

### 6.1 Desktop first-run wizard

The first-run flow should be:

1. Welcome: explain that Hoopoe is a desktop control center, but agents run on a VPS.
2. Choose path:
   - “I already have a VPS”
   - “Guide me through creating one”
   - “Use local machine for demo only”

3. Generate or import SSH key.
4. Test SSH connection.
5. Install or verify ACFS.
6. Run `acfs doctor`.
7. Install Hoopoe Agent.
8. Start Hoopoe Agent as a systemd user service.
9. Create SSH tunnel.
10. Show “VPS Ready” dashboard.

ACFS has an idempotent installer and tracks phases in `~/.acfs/state.json`, so Hoopoe can safely re-run or resume installation instead of treating interruption as fatal. ([GitHub][2])

### 6.2 VPS provisioning guidance

For v1, do **guided provisioning**, not full provider automation. Ask the user to create a VPS, then paste IP/hostname and SSH details. Full API-based provisioning can come later.

The wizard should show:

- recommended CPU/RAM/disk tiers;
- Ubuntu requirement;
- SSH key setup;
- firewall note;
- snapshot recommendation;
- estimated install steps;
- current progress log.

### 6.3 ACFS install adapter

Create an adapter in Hoopoe Agent:

```ts
interface AcfsAdapter {
  preflight(): Promise<PreflightReport>;
  install(
    mode: "safe" | "vibe",
    options: InstallOptions,
  ): AsyncIterable<JobEvent>;
  doctor(): Promise<DoctorReport>;
  update(options: UpdateOptions): AsyncIterable<JobEvent>;
  info(): Promise<AcfsInfo>;
}
```

Use `acfs info --json`, `acfs doctor`, `acfs update`, and installer logs where available. The ACFS README describes `acfs info`, `acfs doctor`, `acfs newproj`, and update commands as scriptable management surfaces. ([GitHub][2])

---

## 7. Project setup and Git integration

### 7.1 Project registry

The desktop app should maintain a local list of connected VPS projects:

```json
{
  "projects": [
    {
      "id": "cass-memory",
      "name": "CASS Memory System",
      "remotePath": "/data/projects/cass-memory",
      "repoUrl": "git@github.com:org/repo.git",
      "branch": "main",
      "vpsId": "vps-prod-1",
      "lastOpenedAt": "..."
    }
  ]
}
```

On the VPS, project metadata lives in:

```text
/data/projects/<project>/
  .git/
  .beads/
  AGENTS.md
  .hoopoe/
    hoopoe.json
    plans/
    runs/
    snapshots/
    health/
```

The Flywheel guide’s VPS environment uses `/data/projects` as the workspace and shows `acfs newproj` creating a Git repo, `.beads`, Claude settings, `AGENTS.md`, and `.gitignore`, so the project setup flow should align with that layout. ([Agent Flywheel][1])

### 7.2 New project

Implementation:

1. User enters name, repo visibility, stack, description.
2. Hoopoe runs `acfs newproj` or equivalent.
3. Hoopoe initializes `.hoopoe`.
4. Hoopoe creates initial `AGENTS.md` if absent.
5. Hoopoe verifies:
   - Git repo exists.
   - `br` is initialized.
   - `AGENTS.md` exists.
   - NTM can see the project.
   - Agent Mail can register project key.

### 7.3 Existing project import

Implementation:

1. User enters Git URL or selects existing VPS path.
2. Hoopoe clones or registers it.
3. Run tool detection:
   - Git status
   - language/package manager
   - test commands
   - coverage commands
   - `br` status
   - `AGENTS.md` presence
   - NTM dependency check

4. Offer to initialize missing pieces.

### 7.4 Repo synchronization

Use `ru` where appropriate for multi-repo sync and JSON status, because it is designed for automation with JSON output, non-interactive mode, and meaningful exit codes. ([GitHub][5])

For a single project, direct Git commands are fine, but always normalize into a common `RepoStatus` object:

```ts
type RepoStatus = {
  clean: boolean;
  branch: string;
  ahead: number;
  behind: number;
  hasConflicts: boolean;
  untrackedCount: number;
  modifiedCount: number;
  lastCommitSha: string;
  lastCommitMessage: string;
};
```

---

## 8. Plan workspace

The Plan workspace is where Hoopoe can provide the biggest leverage. The Flywheel guide emphasizes that planning is where whole-system reasoning is still cheap and that plan-space is where you catch architecture problems before expensive implementation. ([Agent Flywheel][1])

### 8.1 Plan creation modes

Support three modes:

1. **Import existing plan**
   - User selects or pastes a markdown file.
   - Hoopoe stores it under `.hoopoe/plans/` or links to existing path.
   - User marks it as draft/final.

2. **Create from rough idea**
   - User writes raw project idea.
   - Hoopoe launches several planning agents/models.
   - Each produces a detailed candidate plan.
   - A synthesis agent merges them into a master plan.

3. **Extend existing codebase**
   - User selects repo and writes feature goal.
   - Agents inspect codebase first.
   - Agents produce implementation-aware plan.
   - Plan includes migration, compatibility, testing, and rollout sections.

### 8.2 Multi-model planning job

Represent the planning workflow as a job DAG:

```text
rough_idea
  ├─ claude_plan
  ├─ codex_plan
  ├─ gemini_plan
  └─ optional_external_model_plan
        ↓
  comparative_review
        ↓
  synthesized_master_plan
        ↓
  refinement_round_1
        ↓
  refinement_round_2
        ↓
  final_plan
```

Each node writes artifacts:

```text
.hoopoe/runs/plan-001/
  round-000-input.md
  claude-candidate.md
  codex-candidate.md
  gemini-candidate.md
  synthesis.md
  critique-round-001.md
  final.md
  manifest.json
```

### 8.3 Plan editor

Implement:

- markdown editor with outline;
- diff between versions;
- “ask model to critique this section” action;
- “run refinement round” action;
- “extract unresolved decisions” action;
- “ready to convert to beads” gate.

### 8.4 Plan quality gates

Before conversion to beads, check:

- Does plan describe user workflows?
- Does plan describe architecture?
- Does plan define data models?
- Does plan include error handling?
- Does plan include testing strategy?
- Does plan identify risky areas?
- Does plan specify migration/deployment steps?
- Does plan include success criteria?
- Does plan mention observability/logging?
- Does plan have enough detail for agents to execute without improvising major architecture?

Show this as a scorecard, but do **not** pretend it is objective truth. It is a decision aid.

---

## 9. Bead conversion workspace

The Flywheel guide warns about the “plan-bead gap”: after plan revision, the workflow must explicitly transition to actual bead creation, and the beads must carry the plan’s details into executable work units. ([Agent Flywheel][1])

### 9.1 Conversion workflow

When the user clicks **Convert to beads**:

1. Create a conversion job.
2. Spawn a conversion agent with the `beads-workflow` skill instructions.
3. The agent must use `br` to create and modify beads.
4. Hoopoe records every bead created during the job.
5. Run `br sync --flush-only`.
6. Commit `.beads/` changes optionally, depending on user settings.

The public skill metadata for `beads-workflow` describes converting markdown plans into dependency-aware beads with `br`, including bead polishing before implementation. ([Jeffrey's Skills.md][6])

### 9.2 Plan-to-bead traceability

Create a mapping file:

```json
{
  "planId": "plan-001",
  "sourcePlanPath": ".hoopoe/plans/plan-001.md",
  "beads": [
    {
      "beadId": "br-a1b2",
      "planSections": ["3.2 Storage", "7.1 Migration"],
      "coverage": "full",
      "notes": "Covers schema migration and rollback path"
    }
  ],
  "unmappedPlanSections": [],
  "orphanBeads": []
}
```

This lets the UI show:

- plan sections with no bead;
- beads with no clear plan source;
- overloaded beads that should be split;
- missing test obligations;
- missing dependencies.

### 9.3 Bead polish rounds

The guide says strong beads need complete coverage, explicit dependencies, and testing obligations; it also cautions against single-pass bead creation. ([Agent Flywheel][1])

Implement dedicated polish rounds:

```text
Round 1: completeness
Round 2: dependency correctness
Round 3: test obligations
Round 4: implementation context
Round 5: parallelism / de-bottlenecking
```

Each round should produce:

- changed bead IDs;
- added dependencies;
- removed dependencies;
- split/merged beads;
- newly created beads;
- remaining concerns.

### 9.4 Bead readiness score

For each bead:

```ts
type BeadReadiness = {
  hasClearTitle: boolean;
  hasContext: boolean;
  hasAcceptanceCriteria: boolean;
  hasTestObligations: boolean;
  hasDependenciesReviewed: boolean;
  isActionable: boolean;
  isTooLarge: boolean;
  isTooVague: boolean;
  score: number;
};
```

Show this as a UI badge, but keep `br` status as canonical.

---

## 10. Bead visualization

### 10.1 Kanban view

Columns:

- Backlog
- Ready
- In Progress
- Review
- Closed
- Stalled
- Blocked

Card fields:

- bead ID
- title
- priority
- type
- assignee/agent
- dependency count
- dependents count
- test obligation badge
- coverage/quality badge
- last activity timestamp
- linked Agent Mail thread count

Actions:

- open details
- mark ready/in progress/review/closed/reopen
- assign to agent
- add dependency
- split bead
- create follow-up bead
- send to Agent Mail thread
- reserve files
- view related commits

### 10.2 DAG view

Use `bv` data to render:

- dependency edges;
- critical path;
- PageRank/betweenness/HITS/eigenvector metrics;
- cycle warnings;
- blocked nodes;
- unblocked frontier;
- suggested parallel tracks.

The guide describes `bv` as the graph-theory compass that computes dependency-aware metrics such as PageRank, betweenness, HITS, critical path, and cycle detection; it also exposes `--robot-*` commands for agent-safe output. ([Agent Flywheel][1])

### 10.3 Bead detail panel

Tabs:

1. **Overview**: title, status, priority, type.
2. **Context**: full bead description/comments.
3. **Dependencies**: upstream/downstream graph.
4. **Tests**: expected tests and latest results.
5. **Activity**: Agent Mail threads, status changes, comments.
6. **Files**: reservations, touched files, commits.
7. **Review**: review findings and follow-up beads.

---

## 11. Swarm launch and orchestration

NTM is the right backend for this because it handles named tmux sessions, labeled agent panes, work triage, Agent Mail coordination, file reservations, safety policy, durable state, robot output, REST/WebSocket APIs, and checkpoints. ([GitHub][3])

### 11.1 Swarm launch form

Fields:

- Project
- Plan/bead scope
- Agent mix:
  - Claude Code count
  - Codex count
  - Gemini count

- Mode:
  - implementation
  - review-only
  - test-hardening
  - planning
  - bead-polishing

- Start policy:
  - stagger interval
  - max concurrent build/test jobs
  - one git committer agent
  - rate-limit backoff behavior

- Prompt template
- Safety level:
  - safe
  - normal
  - high-autonomy

- Build/test routing:
  - require `rch`
  - local fallback allowed or disallowed

### 11.2 Swarm launch implementation

For a project-level swarm:

```text
1. Verify clean-ish repo state or warn user.
2. Verify AGENTS.md exists.
3. Verify br and bv are healthy.
4. Verify Agent Mail project registration.
5. Verify NTM dependencies.
6. Verify no stale build artifacts are consuming disk.
7. Create NTM session.
8. Spawn agents with labels.
9. Stagger startup.
10. Send kickoff packet.
11. Start orchestrator loop.
12. Start event stream to UI.
```

The Flywheel guide’s first-10-minutes sequence is: create agent terminals, send marching orders staggered, have agents read `AGENTS.md`, join Agent Mail, learn conventions, and use `bv --robot-triage` plus `br ready --json` to choose work. ([Agent Flywheel][1])

### 11.3 Kickoff prompt template

Hoopoe should render a project-specific kickoff prompt:

```text
Reread AGENTS.md and README.md completely.

You are part of the Hoopoe-managed swarm for project: {{projectName}}.
Project path: {{projectPath}}.
Current plan: {{planTitle}}.
Current mode: {{mode}}.

Rules:
- Coordinate through Agent Mail.
- Use bead IDs in Agent Mail thread IDs, subjects, file reservation reasons, and commit messages.
- Use bv --robot-triage and br ready --json before choosing work.
- Mark claimed beads in progress.
- Reserve files before editing.
- Use rch for builds/tests.
- Avoid duplicate work.
- Report blockers quickly.
- Do not wait in communication purgatory; choose useful work when unblocked.
- After finishing a bead, self-review with fresh eyes before closing.
```

Bead IDs should be used as anchors across Agent Mail threads, reservations, and commits to create a unified audit trail. ([Agent Flywheel][1])

### 11.4 Orchestrator loop

Implement a Hoopoe Orchestrator as a VPS job, not a hidden Electron timer.

Default loop every 4 minutes:

```text
for each active swarm:
  snapshot = ntm robot snapshot
  beadGraph = bv --robot-triage
  ready = br ready --json
  mail = agent mail inbox/activity
  locks = file reservations
  disk = disk usage
  tests = active rch/build jobs

  detect:
    idle agents
    rate-limited agents
    wedged panes
    stale in-progress beads
    stale file reservations
    duplicate bead claims
    build contention
    low disk
    repeated test failures
    agents not using mail
    agents not updating br status

  act:
    send fresh marching orders
    restart or interrupt wedged panes
    re-open stalled beads
    release stale reservations
    tell idle agents next work frontier
    pause builds/tests if contention is high
    trigger cleanup of stale artifacts
    request self-review / cross-review
```

The `vibing-with-ntm` skill metadata explicitly covers tending NTM swarms, orchestrator loops, recovery of stuck or rate-limited panes, review-only mode, and coordination through Agent Mail, Beads, and BV. ([Jeffrey's Skills.md][7])

### 11.5 Stalled bead detection

A bead is “stalled” when:

- status is `in_progress`;
- no Agent Mail activity for that bead in N minutes;
- no file changes by assigned agent in N minutes;
- no NTM pane output in N minutes;
- agent is dead/rate-limited/wedged;
- no related commit/test event exists.

Action options:

- mark back to open/ready;
- create a blocker bead;
- assign to another agent;
- ask current agent for status;
- force-release file reservations;
- flag human intervention.

The guide’s stuck-swarm diagnosis explicitly calls out in-progress beads that sit too long and recommends checking Agent Mail, reclaiming the bead, or splitting blockers into clearer beads. ([Agent Flywheel][1])

---

## 12. Swarm dashboard

### 12.1 Agent cards

Each card should show:

- agent name
- model/provider
- NTM pane ID
- status:
  - working
  - idle
  - awaiting review
  - rate limited
  - wedged
  - stopped
  - error

- current bead
- runtime
- last output timestamp
- CPU/RAM
- token estimate
- spend estimate
- active file reservations
- recent log tail
- current command
- last mail event

### 12.2 Agent actions

- Send message
- Interrupt
- Restart
- Ask for summary
- Ask to reread `AGENTS.md`
- Assign bead
- Put into review-only mode
- Release reservations
- Open full terminal
- Open NTM pane
- Kill agent

### 12.3 Global swarm metrics

- active/total agents
- open/ready/in-progress/review/closed bead counts
- average bead close time
- current critical path
- tokens/spend estimate
- test pass/fail trend
- build queue length
- disk free
- stale artifacts size
- Agent Mail unread/urgent count
- rate-limit events

---

## 13. Agent Mail activity workspace

The guide treats Agent Mail, Beads, and `bv` as a single coordination system: Beads are durable issue state, Agent Mail is the communication layer, and `bv` is the graph-theory triage layer. ([Agent Flywheel][1])

### 13.1 Timeline view

Show events in chronological order:

- agent registered
- bead claimed
- mail sent
- file reserved
- reservation renewed
- reservation released
- build started
- test passed/failed
- bead moved to review
- review comment
- bead closed
- commit created
- orchestrator intervention

### 13.2 Filters

- by agent
- by bead
- by priority
- by thread
- by file path
- by event type
- by urgency
- by time range

### 13.3 Human overseer compose box

The user should be able to:

- send to one agent;
- send to all agents;
- send to agents working on a bead;
- send to review agents only;
- attach selected bead context;
- attach latest `bv --robot-triage`;
- attach code health summary.

### 13.4 Reservation view

Show:

- path glob
- agent
- bead ID
- exclusive/shared
- TTL
- age
- last renewal
- conflict risk
- release/renew actions

Agent Mail’s advisory file reservations with TTL are important because they let agents coordinate without rigid locks that deadlock when an agent dies. ([Agent Flywheel][1])

---

## 14. Code Health workspace

The Code Health workspace should combine static and dynamic metrics. It should not be one global hardcoded tool because projects vary widely.

### 14.1 Health adapters

Create language/tool adapters:

```ts
interface HealthAdapter {
  detect(projectPath: string): Promise<DetectionResult>;
  testCommands(): Promise<CommandSpec[]>;
  coverageCommands(): Promise<CommandSpec[]>;
  complexityCommands(): Promise<CommandSpec[]>;
  parseReports(): Promise<CodeHealthSnapshot>;
}
```

Initial adapters:

- TypeScript/JavaScript:
  - package manager detection
  - Vitest/Jest coverage
  - Playwright/Cypress e2e detection
  - ESLint complexity rules

- Python:
  - pytest
  - coverage.py
  - radon complexity

- Rust:
  - cargo test
  - cargo tarpaulin or grcov where available

- Go:
  - go test
  - go coverage

- Generic:
  - lizard for complexity
  - cloc/scc/tokei for LOC
  - ripgrep-based test discovery

### 14.2 Health metrics

Show:

- written files
- test files
- unit test count
- integration test count
- e2e test count
- latest test status
- coverage by file
- average coverage
- uncovered hot spots
- cyclomatic complexity
- churn
- flaky test warnings
- build/lint status
- UBS findings
- unresolved review findings

The Flywheel guide’s landing checklist includes tests, linters, builds, issue status updates, `br sync --flush-only`, Git commit/push, and final verification. ([Agent Flywheel][1])

### 14.3 Build/test contention control

All build/test commands should run through an internal queue, with `rch` as the default execution path when configured. The guide calls out `rch` as remote build offloading and notes that it helps prevent heavy CPU work from degrading the swarm box. ([Agent Flywheel][1])

Implement:

```ts
type BuildQueuePolicy = {
  maxConcurrentBuilds: number;
  maxConcurrentTests: number;
  preferRch: boolean;
  allowLocalFallback: boolean;
  cooldownAfterFailureSec: number;
};
```

Orchestrator actions:

- tell agents not to run duplicate builds;
- queue test jobs centrally;
- dedupe identical test commands;
- cache recent test results;
- stream results to agent mail when useful.

---

## 15. Review workflow

After all actionable beads are done, Hoopoe should transition the swarm into review mode.

### 15.1 Review rounds

```text
Round 1: self-review by original agents
Round 2: cross-agent review
Round 3: random code exploration
Round 4: targeted hot-spot review
Round 5: test and coverage hardening
Round 6: UI/UX polish, if applicable
Round 7: final landing checklist
```

The guide’s workflow explicitly includes agents reviewing, testing, and hardening through self-review, cross-agent review, random exploration, coverage, and UI/UX polish until reviews come back clean. ([Agent Flywheel][1])

### 15.2 Review convergence detector

Track:

- bugs found per round;
- severity of bugs;
- new beads created per round;
- test failures fixed;
- coverage delta;
- repeated findings;
- token/spend per finding.

Convergence state:

```text
not_started
  → high_yield
  → medium_yield
  → low_yield
  → saturated
  → final_gate_ready
```

When saturated, Hoopoe asks the user whether to:

- land the project;
- run one more review round;
- run specialized audits;
- create follow-up beads;
- pause the swarm.

### 15.3 Finding-to-bead conversion

Every review finding should become one of:

- closed as false positive;
- fixed immediately under current bead;
- new bead;
- blocker on existing bead;
- human decision needed.

This keeps the next swarm restartable from `br`, `AGENTS.md`, Git, and Agent Mail instead of human memory.

---

## 16. Safety and security

### 16.1 Secrets

Store locally:

- SSH keys in macOS Keychain or encrypted file with Keychain-protected key;
- VPS connection profiles;
- provider API tokens, only when provider provisioning is implemented;
- LLM/API credentials only when absolutely necessary.

Store on VPS:

- agent CLI credentials using existing ACFS/CLI mechanisms;
- Hoopoe Agent auth token under `~/.hoopoe/config.json`;
- audit logs under `~/.hoopoe/logs`.

### 16.2 Command execution safety

The Hoopoe Agent should not expose arbitrary shell execution to the UI by default. Use typed command specs:

```ts
type CommandSpec = {
  id: string;
  name: string;
  executable: "br" | "bv" | "ntm" | "git" | "acfs" | "ru" | "rch" | "custom";
  args: string[];
  cwd: string;
  timeoutSec: number;
  requiresApproval: boolean;
  redacts: string[];
};
```

Rules:

- path must be inside registered project root unless explicitly approved;
- destructive Git/filesystem commands require approval;
- build/test commands go through queue;
- all stdout/stderr is captured;
- secrets are redacted before streaming to UI;
- every command has an audit event.

The Flywheel toolchain includes DCG as a mechanical destructive-command guard and SLB as optional two-person rule guardrails, so Hoopoe should surface their status and alerts rather than bypassing them. ([Agent Flywheel][1])

### 16.3 Human approval checkpoints

Require explicit confirmation for:

- deleting project directory;
- force-pushing;
- resetting hard;
- force-releasing many file reservations;
- killing entire swarm;
- reinstalling ACFS;
- changing agent autonomy mode;
- exposing any public port;
- installing unverified third-party tools.

---

## 17. VPS Hoopoe Agent API

### 17.1 Core endpoints

```http
GET  /v1/health
GET  /v1/system/info
POST /v1/system/acfs/install
GET  /v1/system/acfs/doctor
POST /v1/system/acfs/update

GET  /v1/projects
POST /v1/projects
GET  /v1/projects/:id
GET  /v1/projects/:id/repo-status
POST /v1/projects/:id/sync

GET  /v1/projects/:id/plans
POST /v1/projects/:id/plans
GET  /v1/projects/:id/plans/:planId
PATCH /v1/projects/:id/plans/:planId
POST /v1/projects/:id/plans/:planId/refine
POST /v1/projects/:id/plans/:planId/convert-to-beads

GET  /v1/projects/:id/beads
GET  /v1/projects/:id/beads/:beadId
PATCH /v1/projects/:id/beads/:beadId
GET  /v1/projects/:id/bead-graph
GET  /v1/projects/:id/bv/triage

POST /v1/projects/:id/swarms
GET  /v1/projects/:id/swarms
GET  /v1/projects/:id/swarms/:swarmId
POST /v1/projects/:id/swarms/:swarmId/send
POST /v1/projects/:id/swarms/:swarmId/stop
POST /v1/projects/:id/swarms/:swarmId/review-mode

GET  /v1/projects/:id/activity
GET  /v1/projects/:id/mail
POST /v1/projects/:id/mail

GET  /v1/projects/:id/code-health
POST /v1/projects/:id/code-health/run

GET  /v1/jobs/:jobId
GET  /v1/jobs/:jobId/events
POST /v1/jobs/:jobId/cancel
```

### 17.2 Event stream

Use WebSocket or SSE:

```ts
type HoopoeEvent =
  | { type: "job.started"; job: Job }
  | {
      type: "job.log";
      jobId: string;
      stream: "stdout" | "stderr";
      text: string;
    }
  | { type: "job.finished"; jobId: string; result: JobResult }
  | { type: "bead.updated"; projectId: string; bead: Bead }
  | { type: "agent.status"; swarmId: string; agent: Agent }
  | { type: "agent.log"; swarmId: string; agentId: string; text: string }
  | { type: "mail.message"; projectId: string; message: AgentMailMessage }
  | {
      type: "reservation.updated";
      projectId: string;
      reservation: FileReservation;
    }
  | { type: "health.updated"; projectId: string; snapshot: CodeHealthSnapshot }
  | { type: "orchestrator.alert"; swarmId: string; alert: Alert };
```

---

## 18. Desktop UI implementation plan

### 18.1 Main layout

Match your reference design:

```text
┌────────────────────────────────────────────────────────────┐
│ repo/project · branch · git clean/dirty · LOC · commits    │
├───────────────┬────────────────────────────────────────────┤
│ Sidebar       │ Current Stage View                         │
│               │                                            │
│ Project card  │ Plans / Beads / Activity / Swarm / Health  │
│ Workflow nav  │                                            │
│ Tools nav     │                                            │
│ VPS status    │                                            │
└───────────────┴────────────────────────────────────────────┘
```

### 18.2 Top bar

Show:

- GitHub/repo path
- branch
- clean/dirty
- last commit age
- connected VPS
- SSH status
- swarm online count
- command palette button

### 18.3 Command palette

Include:

- “Run ACFS doctor”
- “Open terminal”
- “Create plan”
- “Convert plan to beads”
- “Launch swarm”
- “Run review round”
- “Run code health scan”
- “Show stuck beads”
- “Show rate-limited agents”
- “Sync repo”
- “Commit beads”
- “Open NTM dashboard”
- “Send broadcast”

### 18.4 Notifications

Alert types:

- urgent Agent Mail
- stale in-progress bead
- rate-limited agent
- build queue blocked
- disk low
- repeated test failure
- dependency cycle
- uncommitted `.beads` changes
- dirty repo before launch
- ACFS tool drift
- NTM unavailable

---

## 19. Data normalization layer

Build adapters, not one-off command calls.

```ts
interface BrAdapter {
  list(project: ProjectRef): Promise<Bead[]>;
  ready(project: ProjectRef): Promise<Bead[]>;
  update(project: ProjectRef, beadId: string, patch: BeadPatch): Promise<Bead>;
  syncFlushOnly(project: ProjectRef): Promise<void>;
}

interface BvAdapter {
  triage(project: ProjectRef): Promise<TriageReport>;
  graph(project: ProjectRef): Promise<BeadGraph>;
  next(project: ProjectRef): Promise<NextWorkRecommendation>;
}

interface NtmAdapter {
  listSessions(): Promise<NtmSession[]>;
  spawnSwarm(project: ProjectRef, spec: SwarmSpec): Promise<Swarm>;
  snapshot(swarm: SwarmRef): Promise<SwarmSnapshot>;
  send(swarm: SwarmRef, target: SendTarget, message: string): Promise<void>;
  interrupt(swarm: SwarmRef, target: SendTarget): Promise<void>;
}

interface AgentMailAdapter {
  messages(
    project: ProjectRef,
    filter: MailFilter,
  ): Promise<AgentMailMessage[]>;
  send(project: ProjectRef, message: MailDraft): Promise<void>;
  reservations(project: ProjectRef): Promise<FileReservation[]>;
}
```

This is where you insulate Hoopoe from future CLI changes.

---

## 20. Milestones

### Milestone 0 — Research spike

Goal: prove you can read and control the stack from code.

Deliverables:

- run ACFS on a test VPS;
- start NTM server;
- call NTM robot/API endpoints;
- run `br` and `bv` commands on a sample repo;
- read Agent Mail activity;
- launch a small swarm manually;
- document every command and output format;
- identify which outputs are stable JSON versus human text.

Exit criteria:

- one script can produce a JSON snapshot of projects, beads, graph, swarm, mail, and health.

---

### Milestone 1 — Electron shell and VPS connection

Deliverables:

- Electron app with sidebar/topbar;
- connection profile manager;
- SSH key generation/import;
- SSH connectivity test;
- SSH tunnel manager;
- local settings store;
- basic terminal pane;
- “VPS connected” status.

Exit criteria:

- user can connect to an existing VPS and run a harmless remote health check from the UI.

---

### Milestone 2 — Hoopoe Agent daemon

Deliverables:

- Go daemon;
- systemd install script;
- health endpoint;
- auth token;
- WebSocket/SSE event stream;
- typed command runner;
- audit log;
- job runner;
- log streaming;
- OpenAPI spec.

Exit criteria:

- Electron can install/start the daemon over SSH and stream a remote job log.

---

### Milestone 3 — ACFS onboarding

Deliverables:

- first-run wizard;
- ACFS preflight/install/doctor/update integration;
- progress view;
- resumable install detection;
- tool inventory;
- agent CLI credential checklist.

Exit criteria:

- fresh VPS can reach “tools installed and verified” state from Hoopoe.

---

### Milestone 4 — Project registry

Deliverables:

- create project;
- import existing project;
- clone repo;
- detect Git status;
- initialize `.hoopoe`;
- detect `AGENTS.md`;
- detect `br`;
- run `acfs newproj` where appropriate;
- project switcher.

Exit criteria:

- user can open a repo-backed project and see Git/branch/clean status.

---

### Milestone 5 — Plans workspace

Deliverables:

- plan list/cards;
- markdown editor;
- import/export;
- plan states;
- version snapshots;
- multi-model planning job skeleton;
- refinement round job;
- plan quality checklist.

Exit criteria:

- user can create/import a plan, refine it, and mark it final.

---

### Milestone 6 — Bead conversion

Deliverables:

- convert plan to beads job;
- `br` adapter;
- `bv` adapter;
- bead list;
- bead detail panel;
- plan-to-bead mapping;
- bead polish jobs;
- `br sync --flush-only`;
- uncommitted bead changes warning.

Exit criteria:

- user can convert a plan into real `br` beads and see them in Hoopoe.

---

### Milestone 7 — Kanban and DAG

Deliverables:

- Kanban board;
- DAG graph;
- priority filters;
- status filters;
- dependency edit actions;
- cycle warnings;
- critical path highlight;
- `bv --robot-triage` panel.

Exit criteria:

- user can understand what is ready, blocked, critical, stale, or in review without opening a terminal.

---

### Milestone 8 — Swarm launch

Deliverables:

- swarm spec form;
- NTM spawn integration;
- staggered launch;
- kickoff prompt renderer;
- swarm event stream;
- agent cards;
- log tails;
- send/interrupt/restart controls.

Exit criteria:

- user can launch a mixed swarm and watch live agent activity in the GUI.

---

### Milestone 9 — Orchestrator loop

Deliverables:

- 4-minute operator tick;
- idle detection;
- stale bead detection;
- rate-limit detection;
- stuck pane detection;
- stale reservation detection;
- build contention detection;
- disk cleanup;
- auto-marching-orders;
- intervention audit log.

Exit criteria:

- the swarm can be tended from Hoopoe, with human-visible interventions and no hidden magic.

---

### Milestone 10 — Activity and Agent Mail

Deliverables:

- message timeline;
- bead-thread linking;
- reservation view;
- urgent message alerts;
- overseer compose box;
- per-agent inbox state;
- file reservation conflict warnings.

Exit criteria:

- user can coordinate agents without leaving the desktop app.

---

### Milestone 11 — Code Health

Deliverables:

- language detection;
- test command detection;
- coverage parsing;
- complexity parsing;
- health snapshots;
- hot spots table;
- build/test queue;
- `rch` integration;
- trend charts.

Exit criteria:

- user can see tests, coverage, complexity, churn, and build health for the current repo.

---

### Milestone 12 — Review mode and convergence

Deliverables:

- review-only swarm mode;
- fresh-eyes prompts;
- cross-agent review;
- finding tracker;
- finding-to-bead conversion;
- convergence dashboard;
- final landing checklist.

Exit criteria:

- when implementation beads are done, Hoopoe can guide the project into review/hardening and decide when the process is saturated.

---

### Milestone 13 — Packaging and production hardening

Deliverables:

- macOS signing and notarization;
- auto-update;
- crash reporting with opt-in;
- encrypted settings migration;
- daemon version compatibility checks;
- telemetry opt-in/out;
- backup/restore for `.hoopoe`;
- documentation;
- sample project demo.

Exit criteria:

- a new user can install Hoopoe, connect a VPS, install tooling, import a project, create beads, launch agents, and monitor quality.

---

## 21. MVP scope

The best MVP is not the whole beautiful dashboard. It is the smallest version that actually centralizes the Flywheel.

### MVP must include

- Electron app with VPS connection.
- Hoopoe Agent daemon.
- ACFS install/doctor integration.
- Project import/create.
- Plan import/create.
- Convert plan to beads via `br`.
- Bead Kanban.
- `bv --robot-triage` display.
- NTM swarm launch.
- Agent grid with logs/status.
- Agent Mail activity feed.
- Basic code health: tests, coverage, complexity.
- Orchestrator tick that handles idle agents and stale beads.

### MVP can defer

- provider API-based VPS creation;
- polished multi-model planning UX;
- advanced DAG graph editing;
- spend/token precision;
- fully generic coverage support for every language;
- deep CASS/CM memory workflows;
- collaborative multi-user mode;
- hosted cloud sync.

---

## 22. Key risks and mitigations

### Risk: CLI output formats change

Mitigation: use robot/API surfaces wherever possible; put every integration behind adapters; snapshot-test command outputs.

### Risk: direct DB reads corrupt assumptions

Mitigation: use `br` as the write API and only read direct files/DB when documented and safe.

### Risk: agents compete for builds/tests

Mitigation: central build queue, `rch` default, dedupe repeated commands, orchestrator warnings.

### Risk: too much hidden automation

Mitigation: every Hoopoe action becomes an event with command, reason, actor, timestamp, and result.

### Risk: stale agents hold work hostage

Mitigation: stalled bead detector, TTL reservation monitor, re-open workflow, force-release with audit note.

### Risk: app disconnects mid-swarm

Mitigation: orchestrator runs on VPS; Electron only reconnects to the event stream.

### Risk: unsafe command execution

Mitigation: allowlisted command specs, approval gates, path sandboxing, DCG/SLB status surfaced in UI.

### Risk: users think the GUI state is canonical

Mitigation: always show the backing source: Git, `.beads`, NTM session, Agent Mail thread, or health report.

---

## 23. Suggested repository structure

```text
hoopoe/
  apps/
    desktop/
      src/
        main/
        renderer/
        preload/
      electron.vite.config.ts
      package.json

    vps-agent/
      cmd/hoopoe-agent/
      internal/
        api/
        auth/
        jobs/
        adapters/
          acfs/
          br/
          bv/
          ntm/
          agentmail/
          git/
          health/
          rch/
        events/
        audit/
        config/
      go.mod

  packages/
    schemas/
      openapi.yaml
      src/generated/
    ui/
      components/
    prompts/
      kickoff.md
      plan-refine.md
      bead-convert.md
      review-fresh-eyes.md
    test-fixtures/
      sample-br-output/
      sample-bv-output/
      sample-ntm-snapshot/

  scripts/
    install-vps-agent.sh
    dev-vps-tunnel.sh
    package-mac.sh

  docs/
    architecture.md
    security.md
    integration-contracts.md
    mvp.md
```

---

## 24. Final target behavior

A successful Hoopoe session should feel like this:

1. User opens Hoopoe.
2. Hoopoe reconnects to the VPS and project.
3. Plans show current plan state.
4. Beads show what is ready and blocked.
5. User launches a swarm.
6. NTM starts agents.
7. Agents read `AGENTS.md`, join Agent Mail, use `bv`, claim beads through `br`, reserve files, implement, test, and report.
8. Hoopoe shows live agent state, messages, logs, reservations, graph movement, code health, and warnings.
9. Orchestrator nudges idle/stuck/rate-limited agents.
10. When beads are done, Hoopoe transitions into review mode.
11. Review findings become new beads or are closed.
12. Code health gates converge.
13. Hoopoe helps land the session with synced beads, clean Git status, tests passing, and a restartable audit trail.

That is the desktop app version of the Flywheel: not a replacement for the terminal tools, but a **single cockpit for planning, graph curation, swarm tending, and quality convergence**.

[1]: https://agent-flywheel.com/complete-guide "The Complete Flywheel Guide - Planning, Beads & Agent Swarms | Agent Flywheel"
[2]: https://raw.githubusercontent.com/Dicklesworthstone/agentic_coding_flywheel_setup/main/README.md "raw.githubusercontent.com"
[3]: https://raw.githubusercontent.com/Dicklesworthstone/ntm/main/README.md "raw.githubusercontent.com"
[4]: https://raw.githubusercontent.com/Dicklesworthstone/beads_rust/main/README.md "raw.githubusercontent.com"
[5]: https://raw.githubusercontent.com/Dicklesworthstone/repo_updater/main/README.md "raw.githubusercontent.com"
[6]: https://jeffreys-skills.md/skills/beads-workflow "Jeffrey's Skills.md"
[7]: https://jeffreys-skills.md/skills/vibing-with-ntm "Jeffrey's Skills.md"
