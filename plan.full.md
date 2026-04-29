# Hoopoe — Ultimate Hybrid Implementation Plan

## 0. Executive thesis

Hoopoe is a macOS Electron desktop app that turns the Agentic Coding Flywheel from a powerful but manual collection of CLIs, tmux sessions, agent prompts, mail messages, bead graphs, and build/test logs into one staged operational cockpit.

The strategic design constraint is non-negotiable:

> **Hoopoe is the cockpit, not the engine. The VPS is the execution plane. The existing Flywheel tools remain the source-of-truth systems.**

Hoopoe should centralize, visualize, automate, and audit the Flywheel. It should not replace `br`, `bv`, `ntm`, Agent Mail, `rch`, ACFS, CAAM, DCG, CASS, Git, or the agent CLIs. Its job is to make the workflow visible, resumable, safe, and less manually operated.

The product should feel like the reference design: a dense, warm, Mac-native control center organized into five numbered stages:

```text
01 Plans
02 Beads
03 Activity
04 Swarm
05 Code Health
```

The user spends most of their meaningful cognitive effort in **Plans** and **Beads**. The later stages are mostly machine-tending, review, intervention, and quality convergence. The engineering roadmap should mirror that distribution: get the cockpit and setup working first, then prioritize plan creation and bead curation before pouring months into perfect swarm telemetry.

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

Hoopoe can maintain a cache and append-only event log, but it should always be able to answer:

> What is true if we ignore the Hoopoe cache and re-read Git, `br`, `bv`, NTM, Agent Mail, and test reports?

That question should guide every integration boundary.

### 1.2 The desktop app is not the orchestrator of record

Electron can sleep, crash, lose Wi-Fi, or be closed. The swarm must continue. All long-running jobs, operator loops, state reconciliation, and review cycles must run on the VPS under the Hoopoe daemon and/or NTM.

The desktop app reconnects, replays events, and rehydrates UI state. It does not own the running swarm.

### 1.3 Use robot/API surfaces first, shell parsing last

Integration precedence:

1. Official REST/SSE/WebSocket/OpenAPI surfaces, especially NTM `serve`.
2. Tool-provided robot/JSON output, especially `ntm --robot-*`, `bv --robot-*`, `br --json`, `ru --json`.
3. Stable repo files such as `.beads/issues.jsonl`, plan metadata, health report files.
4. Direct SQLite reads only when documented and read-only.
5. Human CLI output parsing only as a fallback, behind tests and version pins.

Bare `bv` should never be invoked from automation because it launches an interactive TUI. Hoopoe should use only `bv --robot-*` surfaces for machine reads.

### 1.4 Every automation must be inspectable

Hoopoe should never feel like hidden magic. Every meaningful automated action should produce an event:

```text
who/what triggered it
which command or API call ran
why it ran
what input scope it used
what output/result happened
which artifact/log stores the evidence
```

This makes the tool trustworthy when it reopens a stalled bead, kills a wedged pane, force-releases a stale reservation, queues a build, or asks agents to re-read `AGENTS.md`.

### 1.5 Build for restartability

Every stage should be restartable from artifacts:

- Plans are markdown plus history artifacts.
- Beads are in `.beads` and synced to JSONL.
- Swarms have NTM sessions, checkpoints, logs, timelines, and mail threads.
- Code health has persisted snapshots.
- Jobs have status, logs, inputs, and output artifacts.

A user should be able to close Hoopoe, reopen it, and understand exactly where the project is.

### 1.6 Make the first successful run boring

The first install and first project launch should optimize for reliability over maximal automation. Provider-provisioned VPS creation is valuable, but the MVP should support existing VPS connection first, then add provider plugins. Full provider automation should not block the core product from working.

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
│  - adapters around ACFS, br, bv, ntm, Agent Mail, git, rch     │
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
│  - ru, rch, CAAM, DCG, CASS, UBS, language runtimes            │
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

The daemon owns:

- Hoopoe job state;
- Hoopoe event log;
- read-model cache;
- UI-only preferences;
- plan metadata created by Hoopoe;
- onboarding/install state;
- health snapshots generated by Hoopoe;
- workflow audit events.

The daemon does **not** own:

- bead truth;
- Git truth;
- NTM session truth;
- Agent Mail truth;
- file reservation truth;
- test report truth when emitted by the project’s tools.

For each dashboard load, the daemon should reconcile its cache against canonical state. Caches can be stale; canonical tool state wins.

### 2.3 Integration hierarchy

The daemon should implement adapters in this order:

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
  3. generic analyzers such as lizard/tokei/cloc/scc
```

### 2.4 Default network posture

Default mode:

- daemon binds to `127.0.0.1` on the VPS;
- Electron creates an SSH tunnel;
- API calls go to `localhost:<forwarded-port>`;
- no public daemon port is exposed.

Advanced mode:

- daemon may expose HTTPS on a public or private interface;
- mTLS client certificates are pinned during provisioning;
- firewall rules restrict access;
- bearer token still required on top of mTLS.

This gives the security benefits of SSH-tunneled localhost by default while leaving room for provider-managed or team scenarios later.

---

## 3. Recommended technology stack

### 3.1 Desktop app

| Area | Recommendation | Notes |
|---|---|---|
| Shell | Electron | macOS-first, mature notarization, native menu, process control |
| Language | TypeScript | shared types and safe renderer/main boundaries |
| Renderer | React + Vite | fast iteration, strong ecosystem |
| Routing | TanStack Router | typed stage routes and nested views |
| Server state | TanStack Query | caching, invalidation, streaming updates |
| Local UI state | Zustand | ephemeral UI state without reducer boilerplate |
| Styling | Tailwind with custom design tokens | encode the reference look as tokens, not one-off CSS |
| UI primitives | Radix/shadcn selectively | only where they fit the custom Hoopoe aesthetic |
| Markdown editor | CodeMirror 6 | markdown, diff, search, optional Vim mode |
| Terminal | xterm.js | faithful pane/log rendering |
| Graph | React Flow first; Cytoscape if needed | React Flow is enough for MVP DAG/force views |
| Kanban | bespoke or small DnD layer | cards need dense custom behavior |
| Charts | lightweight SVG/Recharts/visx | sparklines, coverage trends, spend curves |
| Secrets | macOS Keychain via keytar/equivalent | SSH keys, provider tokens, local auth material |
| Local cache | SQLite or IndexedDB | reconnect replay, project list, UI preferences |

### 3.2 VPS daemon

Use **Go** for the v1 daemon.

Rationale:

- static binary deployment;
- excellent process supervision and concurrency primitives;
- strong HTTP/WebSocket ecosystem;
- simple systemd packaging;
- aligns well with NTM’s Go-based control-plane behavior;
- avoids `node_modules` on the VPS;
- lower operational complexity than Rust for fast-moving orchestration code.

Rust is a credible alternative, especially if the team strongly prefers it or wants tight alignment with `br`. Node/Bun is acceptable for prototypes but should not be the production daemon unless the team deliberately accepts its dependency footprint and runtime management costs.

Recommended Go libraries:

```text
HTTP/router:        chi, echo, or stdlib + generated OpenAPI bindings
WebSocket:          nhooyr.io/websocket or gorilla/websocket
SSE:                small custom SSE writer or r3labs/sse
SQLite:             modernc.org/sqlite or mattn/go-sqlite3
PTY fallback:       creack/pty, or tmux capture where safer
Config:             koanf or clean hand-rolled TOML/JSON
Process execution:  os/exec with context, process groups, timeouts
OpenAPI:            oapi-codegen or similar
```

### 3.3 Shared contracts

Define OpenAPI and JSON schemas in a shared package:

```text
packages/schemas/
  openapi.yaml
  events.schema.json
  generated/
    ts-client/
    go-types/
```

Generate TypeScript client types for the Electron app and Go structs for the daemon. Avoid hand-maintaining duplicate shape definitions.

---

## 4. Repository structure

```text
hoopoe/
  apps/
    desktop/
      src/
        main/
          ssh/
          tunnel/
          keychain/
          updater/
          ipc/
        preload/
        renderer/
          app/
          routes/
            connect/
            plans/
            beads/
            activity/
            swarm/
            code-health/
            settings/
          components/
          features/
          stores/
          api/
      electron.vite.config.ts
      package.json

    vps-agent/
      cmd/hoopoe-agent/
      internal/
        api/
        auth/
        audit/
        config/
        events/
        jobs/
        reconcile/
        command/
        adapters/
          acfs/
          br/
          bv/
          ntm/
          agentmail/
          git/
          ru/
          rch/
          caam/
          dcg/
          cass/
          health/
          tmux/
        orchestration/
          operator_loop/
          build_queue/
          review_rounds/
          stalled_beads/
        storage/
      go.mod

  packages/
    schemas/
      openapi.yaml
      src/generated/
    design-system/
      tokens.css
      components/
      stories/
    prompts/
      planning/
      beads/
      swarm/
      review/
      health/
    test-fixtures/
      br/
      bv/
      ntm/
      agent-mail/
      health/

  scripts/
    install-vps-agent.sh
    bootstrap-existing-vps.sh
    dev-tunnel.sh
    package-mac.sh
    generate-openapi.sh

  docs/
    architecture.md
    security.md
    source-of-truth.md
    integration-contracts.md
    onboarding.md
    mvp.md
    testing.md
    release.md
```

---

## 5. Persistent data layout

### 5.1 Project layout on the VPS

```text
/data/projects/<project-slug>/
  .git/
  .beads/
    issues.jsonl
    beads.db
  .ntm/
    checkpoints/
    pipelines/
    workflows/
  AGENTS.md
  README.md
  plans/                       # optional user-facing plan location
  .hoopoe/
    project.json
    plans/
      plan-001/
        plan.md
        meta.json
        drafts/
          claude.md
          codex.md
          gemini.md
        synthesis.md
        refinement-round-001.md
        refinement-round-002.md
        bead-polish-round-001.md
        bead-polish-round-002.md
        traceability.json
        history.jsonl
    jobs/
      job-*.json
      logs/
    health/
      snapshot-*.json
      coverage/
      complexity/
    swarms/
      swarm-*.json
    events/
      hoopoe-events.jsonl
    artifacts/
      reviews/
      screenshots/
      diffs/
```

### 5.2 Daemon global layout

```text
~/.hoopoe/
  config.json
  auth/
    server-token.enc
    client-certs/
  audit.jsonl
  daemon.db
  logs/
  versions/
  cache/
```

### 5.3 Desktop local layout

```text
~/Library/Application Support/Hoopoe/
  config.sqlite
  event-cache.sqlite
  connection-profiles.json
  logs/
```

Secrets live in macOS Keychain, not directly in those files.

---

## 6. Core domain model

### 6.1 Entities

```ts
type VpsHost = {
  id: string;
  name: string;
  host: string;
  sshUser: string;
  sshFingerprint: string;
  tunnelLocalPort?: number;
  daemonVersion?: string;
  acfsVersion?: string;
  setupStatus: "unknown" | "connecting" | "needs_setup" | "installing" | "ready" | "error";
  diskFreeGb?: number;
  lastSeenAt?: string;
};

type Project = {
  id: string;
  slug: string;
  name: string;
  repoUrl?: string;
  remotePath: string;
  branch: string;
  gitStatus: RepoStatus;
  activePlanId?: string;
  lifecycleState: ProjectLifecycleState;
  toolHealth: ToolHealthReport;
  createdAt: string;
  updatedAt: string;
};

type Plan = {
  id: string;
  projectId: string;
  title: string;
  path: string;
  state: PlanState;
  lineCount: number;
  beadCount: number;
  qualityScore?: PlanQualityScore;
  linkedBeadIds: string[];
  currentRound: number;
  createdAt: string;
  updatedAt: string;
};

type Bead = {
  id: string;
  title: string;
  description?: string;
  status: "open" | "ready" | "in_progress" | "review" | "closed" | "blocked";
  priority: 0 | 1 | 2 | 3 | 4;
  type?: "feature" | "bug" | "task" | "test" | "docs" | "refactor" | "security" | "perf";
  labels: string[];
  dependencies: string[];
  dependents: string[];
  ownerAgentId?: string;
  planId?: string;
  planSectionRefs?: string[];
  readiness?: BeadReadiness;
  graphMetrics?: BeadGraphMetrics;
  lastActivityAt?: string;
  updatedAt: string;
};

type SwarmSession = {
  id: string;
  projectId: string;
  ntmSessionName: string;
  mode: "implementation" | "review" | "test-hardening" | "ui-polish" | "planning" | "bead-polish";
  status: "starting" | "running" | "paused" | "reviewing" | "stopping" | "stopped" | "error";
  agents: Agent[];
  launchSpec: SwarmLaunchSpec;
  budgetPolicy: BudgetPolicy;
  buildQueuePolicy: BuildQueuePolicy;
  startedAt: string;
  stoppedAt?: string;
};

type Agent = {
  id: string;
  displayName: string;
  family: "claude" | "codex" | "gemini" | "cursor" | "aider" | "amp" | "custom";
  model?: string;
  ntmPaneId?: string;
  mailName?: string;
  status: "working" | "idle" | "awaiting_review" | "reviewing" | "rate_limited" | "wedged" | "stopped" | "error";
  currentBeadId?: string;
  cpuPct?: number;
  ramMb?: number;
  tokenEstimate?: number;
  spendEstimateUsd?: number;
  lastOutputAt?: string;
  lastMailAt?: string;
  activeReservations: FileReservation[];
};
```

### 6.2 Lifecycle states

Project states:

```text
unconfigured
  → vps_ready
  → tools_installed
  → project_imported
  → planning
  → plan_finalized
  → bead_conversion_running
  → beads_created
  → beads_polishing
  → beads_finalized
  → swarm_ready
  → swarm_running
  → review_rounds
  → quality_gates
  → completed
```

Plan states:

```text
draft
  → competing_plans_running
  → candidates_ready
  → synthesis_running
  → synthesized
  → refining
  → locked
  → bead_conversion_running
  → beads_created
  → beads_polishing
  → beads_finalized
  → implementation_active
  → review_active
  → completed
```

Swarm states:

```text
not_created
  → composing
  → launching
  → running
  → tending
  → review_mode
  → converging
  → finalizing
  → stopped
```

### 6.3 Gate invariants

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

---

## 7. Security and authentication

### 7.1 Secrets model

Local Mac secrets:

- SSH private key reference or generated key material stored via macOS Keychain;
- provider API tokens for provisioning, if used;
- local Hoopoe connection profile tokens;
- optional client certificate material for direct mTLS mode.

VPS secrets:

- agent CLI credentials handled by existing CLI/ACFS mechanisms;
- API keys for planning/model calls if the user opts into server-side planning;
- Hoopoe daemon auth token;
- encrypted configuration files;
- audit logs with redaction.

The desktop should never store raw model API keys longer than necessary if the user chooses to send them to the VPS. Provide an explicit option:

```text
[Recommended] Store model/API credentials on VPS for planning and agent workflows.
[Privacy-max] Use desktop-only API calls for planning; do not store these keys on VPS.
```

### 7.2 Bootstrap authentication

First connection flow:

1. User provides host/user/key or provider creates host.
2. Electron verifies SSH host fingerprint.
3. Electron copies bootstrap script and daemon installer.
4. Daemon generates local auth token and optional certificate pair.
5. Electron stores connection material in Keychain.
6. Daemon binds to `127.0.0.1` by default.
7. Electron establishes SSH tunnel and performs version handshake.

### 7.3 Steady-state authentication

Default:

- SSH tunnel required;
- daemon token required;
- API requests include short-lived session bearer;
- event stream includes reconnect cursor and auth.

Advanced:

- mTLS direct HTTPS;
- pinned server certificate;
- client certificate rotation;
- firewall restrictions;
- still use short-lived bearer tokens for revocation.

### 7.4 Command safety

The daemon must not expose arbitrary shell execution as a normal API. It should execute typed commands with a policy layer:

```ts
type CommandSpec = {
  id: string;
  projectId?: string;
  executable: "acfs" | "br" | "bv" | "ntm" | "git" | "ru" | "rch" | "caam" | "dcg" | "cass" | "test-runner" | "custom";
  args: string[];
  cwd: string;
  timeoutSec: number;
  envPolicy: "minimal" | "project" | "agent";
  requiresApproval: boolean;
  approvalReason?: string;
  redactionPatterns: string[];
  allowedPathRoots: string[];
};
```

Rules:

- command path must be inside a registered project or approved tool path;
- destructive Git/filesystem operations require explicit approval;
- `sudo` commands require setup mode or explicit approval;
- build/test commands go through the build queue;
- secrets are redacted before storage and streaming;
- stdout/stderr are separated;
- every command has an audit event;
- DCG/NTM safety checks should be consulted where available.

### 7.5 Human approval checkpoints

Require approval for:

- deleting a project;
- force-pushing;
- `git reset --hard`;
- checking out/rebasing over active swarm work;
- force-releasing many file reservations;
- killing an entire swarm;
- switching to high-autonomy mode;
- exposing daemon ports publicly;
- reinstalling ACFS;
- importing provider credentials;
- changing budget caps upward;
- running unrecognized custom scripts.

---

## 8. First install and VPS onboarding

### 8.1 First-run wizard

The onboarding wizard should be a stage-zero experience, visually consistent with the rest of the app.

```text
STAGE 0 — CONNECT
```

Steps:

1. Explain that Hoopoe controls a user-owned VPS.
2. Choose setup path:
   - Connect existing VPS.
   - Provision new VPS.
   - Local demo mode.
3. Configure SSH:
   - generate key;
   - import key;
   - paste host/user;
   - verify fingerprint.
4. Run preflight:
   - OS version;
   - CPU/RAM/disk;
   - network;
   - Git/curl/tmux basics;
   - disk free;
   - permissions.
5. Install or verify ACFS.
6. Install or update Hoopoe daemon.
7. Start daemon as systemd service.
8. Establish tunnel.
9. Run tool inventory.
10. Configure optional credentials:
    - Anthropic/OpenAI/Google planning keys;
    - CAAM accounts;
    - GitHub SSH/pat setup if needed.
11. Show “VPS Ready”.

### 8.2 Existing VPS first, provider automation second

MVP should support **existing VPS** first because it is easiest to make reliable and fastest to debug. Provider automation should be designed from day one but shipped after the tunnel/daemon/tooling path works.

Provider plugin interface:

```ts
interface VpsProvider {
  id: "hetzner" | "digitalocean" | "linode" | "vultr" | "ovh" | "contabo" | "custom";
  listRegions(): Promise<Region[]>;
  listSizes(region: string): Promise<InstanceSize[]>;
  createInstance(spec: InstanceSpec): Promise<ProvisionedHost>;
  destroyInstance(id: string): Promise<void>;
  estimateMonthlyCost(spec: InstanceSpec): Promise<CostEstimate>;
}
```

Recommended rollout:

- Phase 1: existing Ubuntu VPS.
- Phase 2: one provider plugin, preferably the provider the team uses most.
- Phase 3: additional providers.
- Phase 4: one-click teardown and cost inventory.

### 8.3 Bootstrap flow

Bootstrap script responsibilities:

```text
1. verify OS and basic dependencies
2. install missing base packages
3. install or verify ACFS
4. run ACFS doctor/inventory
5. install Hoopoe daemon binary
6. create config and auth token
7. install systemd unit
8. start daemon
9. print machine-readable result JSON
```

The wizard should stream logs but also show structured checkpoint cards:

```json
{
  "stage": "install_acfs",
  "status": "succeeded",
  "startedAt": "...",
  "endedAt": "...",
  "details": {
    "acfsVersion": "0.7.0",
    "toolsInstalled": ["ntm", "br", "bv", "agent-mail"]
  }
}
```

Failures resume from checkpoints rather than starting from scratch.

### 8.4 Tool inventory

After setup, capture versions and health:

```json
{
  "tools": {
    "git": { "status": "ok", "version": "..." },
    "tmux": { "status": "ok", "version": "..." },
    "ntm": { "status": "ok", "version": "...", "serve": "available" },
    "br": { "status": "ok", "version": "..." },
    "bv": { "status": "ok", "version": "...", "robot": "available" },
    "agentMail": { "status": "ok" },
    "rch": { "status": "ok_or_missing" },
    "caam": { "status": "ok_or_missing" },
    "dcg": { "status": "ok_or_missing" },
    "claude": { "status": "installed_auth_unknown" },
    "codex": { "status": "installed_auth_unknown" },
    "gemini": { "status": "installed_auth_unknown" }
  }
}
```

Show tool problems as fixable checklist items, not as a wall of terminal output.

---

## 9. Project setup and Git integration

### 9.1 Project flows

Support three flows:

| Flow | Behavior |
|---|---|
| New project | create directory, initialize Git, seed `AGENTS.md`, initialize `.hoopoe`, initialize `br`, optionally create remote |
| Clone project | clone repo, detect stack, initialize missing Hoopoe/Beads files, run preflight |
| Existing VPS project | select path, validate Git, register project, initialize missing pieces |

### 9.2 Project readiness checks

On project registration:

- Is it under an allowed projects root?
- Is it a Git repo?
- What branch is active?
- Is working tree clean/dirty/conflicted?
- Is `.beads` present and healthy?
- Is `br ready --json` usable?
- Is `bv --robot-triage` usable?
- Is `AGENTS.md` present and current?
- Is Agent Mail configured for the project key?
- Are NTM deps healthy for this project?
- Are package manager/test commands detectable?
- Are coverage reports available?
- Is there an existing NTM session for this repo?
- Are there stale file reservations?
- Are there orphaned in-progress beads?

### 9.3 Project metadata

`.hoopoe/project.json`:

```json
{
  "schemaVersion": 1,
  "projectId": "cass-memory",
  "displayName": "CASS Memory System",
  "repoUrl": "git@github.com:Dicklesworthstone/cass-memory.git",
  "defaultBranch": "main",
  "projectRoot": "/data/projects/cass-memory",
  "planDirectory": ".hoopoe/plans",
  "activePlanId": "plan-001",
  "agentMailProjectKey": "/data/projects/cass-memory",
  "health": {
    "preferredTestCommand": "npm test",
    "preferredCoverageCommand": "npm run coverage",
    "preferRch": true
  },
  "createdAt": "...",
  "updatedAt": "..."
}
```

### 9.4 Repo status and synchronization

For a single active project, use Git plumbing directly. For multi-repo status/sync, integrate `ru --json` and `ru --non-interactive`.

Repo status read model:

```ts
type RepoStatus = {
  clean: boolean;
  branch: string;
  upstream?: string;
  ahead: number;
  behind: number;
  hasConflicts: boolean;
  modifiedCount: number;
  stagedCount: number;
  untrackedCount: number;
  deletedCount: number;
  lastCommitSha?: string;
  lastCommitMessage?: string;
  lastCommitAt?: string;
};
```

Hoopoe should surface Git state constantly in the top bar because dirty/clean status affects launch, review, and landing.

---

## 10. Stage 1 — Plans

### 10.1 Purpose

Plans are the highest-leverage artifact in the system. The Plan workspace must be treated as a first-class product, not as a textarea plus “generate” button.

The goal is to turn a rough idea or existing-codebase feature request into a deeply reasoned, self-contained markdown plan that can survive conversion into beads without losing architecture, tests, user workflows, and edge cases.

### 10.2 Plans screen

List view:

- plan ID;
- title;
- status pill: `DRAFT`, `PLANNING`, `SYNTHESIZED`, `REFINING`, `LOCKED`, `CONVERTED`;
- line count;
- linked bead count;
- last activity;
- source project/branch;
- quality score;
- quick action: Convert to beads.

Detail view:

```text
┌─────────────────────────────────────────────────────────────┐
│ Header: plan title, status, actions                         │
├─────────────────────┬─────────────────────┬─────────────────┤
│ Markdown editor     │ Preview / diff       │ Artifact rail   │
│ CodeMirror          │ version comparison   │ drafts, rounds  │
└─────────────────────┴─────────────────────┴─────────────────┘
```

Actions:

- Import markdown.
- Create from rough idea.
- Generate competing plans.
- Compare candidates.
- Synthesize master plan.
- Run refinement round.
- Ask for fresh-eyes critique.
- Extract unresolved decisions.
- Add project-code investigation context.
- Lock plan.
- Convert to beads.

### 10.3 Plan creation modes

#### Mode A: Import an existing plan

- User pastes or selects markdown.
- Hoopoe creates plan directory and metadata.
- User may immediately mark as draft/final.
- Hoopoe can run a plan quality review before conversion.

#### Mode B: Create from rough idea

Inputs:

- rough concept;
- target users;
- project type;
- stack preference;
- constraints;
- deployment target;
- risk areas;
- desired agent autonomy;
- testing expectations;
- UI references/screenshots if relevant.

Pipeline:

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

#### Mode C: Extend existing codebase

Before planning, agents inspect:

- README;
- AGENTS.md;
- architecture docs;
- package/build files;
- code structure;
- existing tests;
- existing beads;
- current health hot spots.

Then they generate implementation-aware plans that include migration risk, compatibility, testing, and integration points.

### 10.4 Where planning runs

Default: planning runs on the VPS daemon as jobs so artifacts land inside the project and survive desktop disconnects.

Options:

- **Server-side API mode:** daemon uses BYOK API keys stored on VPS.
- **Server-side CLI mode:** daemon uses configured Claude/Codex/Gemini CLIs where appropriate.
- **Desktop-only API mode:** Electron main process calls APIs and writes artifacts to VPS, for users who do not want planning keys stored on the VPS.

The UI should make credential location explicit.

### 10.5 Plan artifacts

```text
.hoopoe/plans/plan-001/
  plan.md
  meta.json
  drafts/
    claude.md
    codex.md
    gemini.md
  comparative-review.md
  synthesis.md
  refinement-round-001.md
  refinement-round-002.md
  fresh-eyes-001.md
  unresolved-decisions.md
  history.jsonl
```

`meta.json`:

```json
{
  "schemaVersion": 1,
  "id": "plan-001",
  "title": "Hoopoe Core Architecture",
  "state": "refining",
  "createdAt": "...",
  "updatedAt": "...",
  "source": "rough_idea",
  "models": ["claude", "codex", "gemini"],
  "rounds": [
    {
      "id": "round-001",
      "type": "synthesis",
      "status": "succeeded",
      "artifacts": ["synthesis.md"]
    }
  ],
  "linkedBeadIds": []
}
```

### 10.6 Plan quality tracker

The tracker should combine deterministic checks and model-based critique.

Deterministic checks:

- line/section coverage;
- required headings present;
- unresolved decisions explicitly listed;
- testing section present;
- deployment section present;
- risks section present;
- plan-to-bead readiness markers present;
- no empty sections.

Model-based checks:

- user workflows explicit;
- architecture coherent;
- dependencies and sequencing clear;
- implementation details sufficient;
- failure modes covered;
- security/privacy implications covered;
- testing obligations concrete;
- UI/UX behavior described where relevant;
- migration/rollback included when needed.

Output:

```ts
type PlanQualityScore = {
  overall: number;
  dimensions: {
    intentClarity: number;
    architectureSpecificity: number;
    workflowCoverage: number;
    implementationDetail: number;
    testingSpecificity: number;
    riskCoverage: number;
    beadReadiness: number;
  };
  blockers: string[];
  recommendations: string[];
};
```

This score is a guide, not truth. The user can override it.

### 10.7 Locking a plan

Locking a plan should:

1. write final `plan.md`;
2. create a snapshot hash;
3. require unresolved decisions to be accepted or resolved;
4. mark metadata `locked`;
5. enable “Convert to beads”.

A locked plan can still be amended, but amendments create a new version and can trigger bead delta analysis.

---

## 11. Stage 2 — Beads: conversion, curation, visualization

### 11.1 Purpose

The bead stage is where Hoopoe prevents the plan-bead gap. It must ensure that the final markdown plan becomes dependency-aware, self-contained, testable work units that agents can execute without improvising major architecture.

### 11.2 Conversion workflow

When the user clicks **Convert to beads**:

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

Conversion must be explicit and auditable. Hoopoe should record:

- input plan hash;
- prompt/instructions used;
- model/agent used;
- bead IDs created;
- dependencies added;
- `br` commands run;
- sync status;
- unresolved conversion concerns.

### 11.3 Plan-to-bead traceability

`traceability.json`:

```json
{
  "schemaVersion": 1,
  "planId": "plan-001",
  "planHash": "sha256:...",
  "generatedAt": "...",
  "beads": [
    {
      "beadId": "br-a1b2c3",
      "title": "Implement daemon job runner",
      "planSections": ["6. VPS daemon", "19. Jobs"],
      "coverage": "full",
      "testObligations": ["job lifecycle tests", "interrupted job resume test"],
      "notes": "Core daemon work"
    }
  ],
  "unmappedPlanSections": [],
  "orphanBeads": [],
  "oversizedBeads": [],
  "duplicateCandidates": []
}
```

### 11.4 Bead polish rounds

Each polish round should be a tracked job and artifact:

```text
Round 1: plan coverage
Round 2: dependency correctness
Round 3: granularity and split/merge
Round 4: test obligations and acceptance criteria
Round 5: parallel execution tracks
Round 6: fresh-eyes review of bead graph
```

Each round writes:

```text
.hoopoe/plans/plan-001/bead-polish-round-003.md
```

Contents:

- model/tool used;
- inputs;
- recommendations;
- accepted changes;
- rejected changes;
- new beads;
- removed/merged beads;
- dependency changes;
- remaining concerns;
- `br`/`bv` outputs.

### 11.5 Bead quality tracker

Bead-set dimensions:

```ts
type BeadSetQuality = {
  overall: number;
  planCoverage: number;
  dependencyCorrectness: number;
  granularity: number;
  readySetSize: number;
  testability: number;
  duplicateRisk: number;
  parallelism: number;
  contextRichness: number;
  blockers: string[];
  suggestedActions: BeadPolishAction[];
};
```

Heuristics:

- every material plan section maps to at least one bead;
- each P0/P1 bead has acceptance criteria;
- each implementation bead has test obligations;
- no obvious duplicates;
- no unexpected cycles;
- no bead is too vague;
- no bead is too large;
- the ready set is large enough for the desired swarm;
- graph has enough independent tracks for parallelism;
- critical path beads are high priority.

### 11.6 Kanban view

Columns:

- Backlog
- Ready
- In Progress
- Review
- Closed
- Stalled
- Blocked

Card contents:

- bead ID;
- title;
- priority chip;
- type icon;
- dependency count;
- unblock count;
- current owner/agent chip;
- plan coverage marker;
- test obligation marker;
- last activity age;
- coverage/complexity if linked to files;
- reservation conflict indicator.

Interactions:

- open bead details;
- update status via `br`;
- add/edit dependency via `br`;
- create follow-up bead;
- split bead;
- merge duplicate;
- send to review;
- assign to agent through NTM/Agent Mail;
- view mail thread;
- view touched files/commits.

### 11.7 DAG and Force views

Views:

- **Kanban**: execution state.
- **DAG**: dependency structure.
- **Force**: cluster/hotspot exploration.

DAG overlays:

- critical path;
- ready frontier;
- blocked nodes;
- cycle warnings;
- PageRank/betweenness/HITS metrics;
- labels/domains;
- agent ownership;
- review status;
- plan section grouping.

The Force view is useful for visualizing graph clusters and overloaded domains; the DAG is better for exact dependency reasoning.

### 11.8 Bead detail drawer

Tabs:

1. Overview.
2. Full context/description.
3. Dependencies.
4. Plan traceability.
5. Agent Mail thread.
6. Files and reservations.
7. Tests and health.
8. Commits/diffs.
9. Review findings.
10. Audit history.

---

## 12. Stage 3 — Activity and Agent Mail

### 12.1 Purpose

Activity is the coordination ledger. It should combine Agent Mail, NTM events, bead updates, file reservations, build/test events, and orchestrator interventions into one readable timeline.

### 12.2 Timeline events

Event types:

- agent registered;
- mail sent/received;
- urgent mail;
- bead claimed;
- bead status changed;
- file reserved;
- reservation renewed;
- reservation released;
- reservation conflict;
- build/test started;
- build/test completed;
- test failed;
- rate limit detected;
- pane wedged;
- orchestrator intervention;
- review request;
- review finding;
- commit created;
- health snapshot updated.

### 12.3 Timeline UI

Each row:

```text
[agent chip] → [target chip] [bead pill] [URGENT] title        timestamp
summary/body preview
```

Interactions:

- click bead pill → bead detail drawer;
- click agent chip → swarm agent tile;
- click file path → file/reservation view;
- click review finding → finding detail;
- reply as human overseer;
- broadcast to swarm;
- create bead from message;
- mark item acknowledged.

### 12.4 File reservations view

Show:

- path/glob;
- owner agent;
- bead ID;
- exclusive/shared;
- reason;
- TTL remaining;
- last renewal;
- conflict risk;
- stale status;
- release/force release actions.

Reservations should be treated as advisory, not hard locks, matching the Flywheel coordination model. Hoopoe should surface stale reservations and conflict warnings without pretending the GUI can prevent every file edit.

### 12.5 Human overseer compose

The user can send:

- direct message to one agent;
- message to all agents;
- message to all agents working on a label/domain;
- message to agents touching a file set;
- message to reviewers only;
- urgent broadcast;
- review prompt;
- AGENTS.md reread prompt;
- `bv --robot-triage` prompt;
- build contention warning.

Hoopoe should offer context attachments:

- selected bead;
- latest `bv --robot-triage`;
- current ready set;
- code health snapshot;
- relevant mail thread;
- selected file metrics;
- current build queue state.

---

## 13. Stage 4 — Swarm launch and monitoring

### 13.1 Purpose

The Swarm workspace is mission control. It launches agents through NTM, shows live status, exposes logs/panes, tracks costs and rate limits, and lets the user intervene without dropping into raw tmux.

### 13.2 Swarm composition form

Inputs:

```ts
type SwarmLaunchSpec = {
  projectId: string;
  planId?: string;
  beadScope?: {
    priorities?: number[];
    labels?: string[];
    beadIds?: string[];
    excludeBeadIds?: string[];
  };
  agents: {
    claude: number;
    codex: number;
    gemini: number;
    cursor?: number;
    aider?: number;
    amp?: number;
  };
  mode: "implementation" | "review" | "test-hardening" | "ui-polish" | "planning" | "bead-polish";
  staggerSeconds: number;
  maxConcurrentBuilds: number;
  maxConcurrentTests: number;
  preferRch: boolean;
  requireAgentMail: boolean;
  requireFileReservations: boolean;
  budgetPolicy: BudgetPolicy;
  safetyLevel: "safe" | "normal" | "high-autonomy";
  promptTemplateId: string;
};
```

Default launch policy:

- stagger starts by at least 30 seconds;
- force `AGENTS.md` and README reread;
- require Agent Mail registration;
- require `bv --robot-triage` and `br ready --json` before claiming work;
- mark claimed beads `in_progress`;
- reserve files before edits;
- include bead ID in mail subjects, reservation reasons, and commit messages;
- use `rch` for builds/tests when configured;
- do not run bare `bv`;
- avoid concurrent builds for same project;
- self-review with fresh eyes before review/close;
- report blockers quickly;
- do not wait in communication purgatory.

### 13.3 Launch sequence

```text
1. Reconcile project state.
2. Verify launch gates.
3. Show warnings: dirty Git, stale reservations, no ready beads, low disk, missing Agent Mail.
4. Create swarm spec and audit event.
5. Call NTM spawn/add as appropriate.
6. Stagger agent starts.
7. Send kickoff prompt to each agent.
8. Start event subscriptions.
9. Start operator loop.
10. Show swarm dashboard.
```

### 13.4 Kickoff prompt template

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

### 13.5 Swarm dashboard

Top metrics:

- working / total agents;
- open/ready/in-progress/review/closed bead counts;
- tokens/spend estimate;
- build queue depth;
- urgent mail count;
- disk free;
- velocity:
  - beads/day;
  - LOC/day;
  - review findings/hour;
  - test pass/fail trend.

Agent card:

- two-letter tile (`C1`, `X2`, `G1`, etc.);
- agent family/model;
- status pill;
- current bead;
- runtime;
- CPU/RAM sparkline;
- token/spend estimate;
- active reservation count;
- last mail age;
- last output age;
- terminal/log tail;
- quick actions.

Quick actions:

- send message;
- ask for summary;
- ask to reread AGENTS.md;
- ask to run `bv --robot-triage`;
- interrupt;
- restart;
- kill;
- release reservations;
- assign bead;
- switch to review mode;
- open full terminal.

### 13.6 PTY and pane streaming strategy

Terminal fidelity is a major credibility risk. Implement it deliberately.

Preferred path:

- use NTM WebSocket/robot-tail surfaces where available;
- subscribe to structured pane output and status events;
- render in xterm.js.

Fallback path:

- daemon uses tmux capture/read loop;
- maintains a per-pane 50–100KB ring buffer;
- sends initial ring on attach;
- streams diffs at a capped rate, e.g. 5–10Hz;
- strips or preserves ANSI depending on rendering mode;
- compresses messages if needed;
- stores last-event IDs for reconnect.

Important details:

- do not make one SSH connection per pane;
- do not poll every pane aggressively from Electron;
- do not parse terminal output as the source of truth when NTM/Agent Mail/`br` can tell you state;
- terminal content is for observability and manual intervention.

### 13.7 Cost and rate-limit guardrails

Budget policy:

```ts
type BudgetPolicy = {
  perAgentUsd?: number;
  perSwarmUsd?: number;
  alertAtPct: number;
  hardStopAtPct?: number;
  allowOverride: boolean;
  rateLimitBackoff: "pause" | "rotate-account" | "restart" | "notify-only";
};
```

Hoopoe should show estimates as estimates unless the model CLI exposes exact usage. Avoid false precision.

Rate-limit detection:

- parse known CLI status messages;
- observe NTM health/status events;
- detect long no-output periods plus recent rate-limit text;
- integrate CAAM if configured;
- mark card `Rate limited`;
- ask orchestrator to reassign or pause if needed.

---

## 14. Operator loop

### 14.1 Purpose

The operator loop is the machine-tending brain. It should run on the VPS every few minutes during active swarms, with a default cadence of four minutes. It should be visible, configurable, and auditable.

### 14.2 Loop algorithm

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

### 14.3 Stalled bead detection

A bead is a stalled candidate if:

- status is `in_progress`;
- owner agent is idle/wedged/stopped/rate-limited;
- no Agent Mail activity for the bead in N minutes;
- no pane output from owner in N minutes;
- no file modification in N minutes;
- no test/build activity related to it;
- reservation expired or stale;
- repeated loop ticks produced no progress.

Actions by severity:

1. Ask owner for explicit status.
2. Send AGENTS.md reread + current bead context.
3. Ask owner to either proceed, ask for help, or release bead.
4. Reopen bead if owner is gone.
5. Force-release stale reservations with audit note.
6. Create blocker bead if issue is legitimate.
7. Reassign to another agent.
8. Alert human for destructive/ambiguous cases.

### 14.4 Build/test contention control

Centralize expensive commands.

```ts
type BuildQueuePolicy = {
  preferRch: boolean;
  allowLocalFallback: boolean;
  maxConcurrentBuilds: number;
  maxConcurrentTests: number;
  dedupeIdenticalCommandsWindowSec: number;
  cooldownAfterFailureSec: number;
  diskPressurePauseThresholdGb: number;
};
```

Behavior:

- agents may request tests/builds;
- daemon queues and dedupes;
- identical commands reuse recent results when safe;
- `rch` used when available/configured;
- UI shows queue and currently running jobs;
- orchestrator warns agents when contention is high;
- stale artifacts cleanup runs under disk pressure.

### 14.5 Strategic drift detection

A swarm can be busy but no longer closing the product gap. Hoopoe should flag this when:

- many commits but few beads closed;
- lots of low-priority work while P0/P1 critical path is unchanged;
- repeated review findings in same domain;
- code health worsens while beads close;
- agents create many new beads without closing old ones;
- user-defined success criteria remain unmapped.

Actions:

- stop/slow swarm;
- run reality-check review;
- generate drift report;
- create or revise beads;
- ask human to approve a new plan/bead refinement round.

---

## 15. Stage 5 — Code Health

### 15.1 Purpose

Code Health turns “agents are coding” into measurable quality. It should show tests, coverage, complexity, churn, hotspots, review findings, and quality trends in a way that feeds back into beads.

### 15.2 Health adapters

```ts
interface HealthAdapter {
  detect(projectPath: string): Promise<DetectionResult>;
  discoverCommands(projectPath: string): Promise<HealthCommandDiscovery>;
  runSnapshot(projectId: string, policy: HealthRunPolicy): AsyncIterable<JobEvent>;
  parseReports(projectPath: string): Promise<CodeHealthSnapshot>;
}
```

Initial adapters:

| Ecosystem | Tests | Coverage | Complexity |
|---|---|---|---|
| TS/JS | npm/pnpm/yarn, Vitest/Jest, Playwright/Cypress | lcov, coverage-summary.json, cobertura | lizard, ESLint complexity, ts-complexity if useful |
| Python | pytest/unittest | coverage.py XML/JSON | radon, lizard |
| Rust | cargo test | cargo llvm-cov, tarpaulin/grcov | lizard |
| Go | go test | go test -coverprofile | gocyclo, lizard |
| Generic | shell command config | lcov/cobertura | lizard, scc/tokei/cloc |

### 15.3 Snapshot schema

```ts
type CodeHealthSnapshot = {
  id: string;
  projectId: string;
  gitSha: string;
  createdAt: string;
  summary: {
    writtenFiles: number;
    testFiles: number;
    unitTests?: number;
    integrationTests?: number;
    e2eTests?: number;
    avgCoverage?: number;
    avgComplexity?: number;
    hotspotCount: number;
    buildStatus: "pass" | "fail" | "unknown";
    lintStatus: "pass" | "fail" | "unknown";
  };
  files: FileHealthMetric[];
  trends?: HealthTrendSummary;
};

type FileHealthMetric = {
  path: string;
  loc?: number;
  complexity?: number;
  coveragePct?: number;
  churnScore?: number;
  ownerAgentId?: string;
  linkedBeadIds: string[];
  hotspotReasons: string[];
};
```

### 15.4 Code Health UI

KPI cards:

- written files;
- average coverage;
- average complexity;
- hotspots.

Table columns:

- file;
- LOC;
- complexity;
- coverage bar;
- churn;
- owner agent;
- linked bead;
- hotspot reasons;
- action.

Actions:

- create bead from hotspot;
- ask review agent to inspect file;
- run targeted tests;
- view coverage details;
- view commits touching file;
- open related mail thread;
- add to review round.

### 15.5 Hotspot scoring

```text
hotspot_score = weighted_sum(
  high_complexity,
  low_coverage,
  high_churn,
  recent_agent_changes,
  failed_tests_nearby,
  review_findings_nearby,
  critical_path_bead_link
)
```

Default thresholds:

- complexity >= 20;
- coverage < 60%;
- repeated test failures;
- high churn in recent swarm round;
- no linked tests for recently modified production file.

Thresholds should be configurable per project.

---

## 16. Review and hardening workflow

### 16.1 Transition into review mode

When implementation beads are done or nearly done, Hoopoe should propose review mode.

Prerequisites:

- no obvious active implementation beads without owner;
- all P0/P1 ready beads either closed, in review, or intentionally deferred;
- Git status understood;
- latest health snapshot available;
- build/test queue not overloaded.

### 16.2 Review rounds

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

Each round creates:

- round artifact;
- prompts used;
- agents involved;
- findings;
- fixes;
- new beads;
- false positives;
- test/health deltas;
- cost/time summary.

### 16.3 Finding lifecycle

```text
new
  → triaged
  → fix_now
  → new_bead
  → false_positive
  → needs_human
  → closed
```

Every finding must resolve to one of:

- fixed immediately;
- converted to a bead;
- attached as a blocker to an existing bead;
- rejected as false positive with note;
- escalated to human decision.

### 16.4 Convergence detector

Track:

- findings per round;
- severe findings per round;
- duplicate findings;
- fixes per round;
- new beads per round;
- test failures fixed;
- coverage delta;
- complexity delta;
- cost/time per useful finding.

Convergence states:

```text
not_started
  → high_yield
  → medium_yield
  → low_yield
  → saturated
  → final_gate_ready
```

A round is “saturated” when new useful findings are low relative to cost and effort, and the remaining findings are mostly duplicates, low severity, or already tracked as beads.

### 16.5 Specialized audits

When review saturation is reached but the user wants further hardening, Hoopoe should offer targeted skills/workflows:

- mock-code finder;
- deadlock/concurrency finder;
- security audit for SaaS;
- performance profiling;
- project reality check;
- reasoning-mode analysis;
- golden artifact testing;
- fuzzing;
- e2e testing with logging/no mocks;
- UI polish review.

Each audit should create beads instead of free-floating todos.

---

## 17. VPS daemon API

### 17.1 Core endpoints

```http
GET  /v1/health
GET  /v1/version
GET  /v1/system/info
GET  /v1/system/tools
POST /v1/system/acfs/install
GET  /v1/system/acfs/doctor
POST /v1/system/acfs/update

GET  /v1/projects
POST /v1/projects
GET  /v1/projects/:projectId
PATCH /v1/projects/:projectId
GET  /v1/projects/:projectId/repo-status
POST /v1/projects/:projectId/reconcile
POST /v1/projects/:projectId/sync

GET  /v1/projects/:projectId/plans
POST /v1/projects/:projectId/plans
GET  /v1/projects/:projectId/plans/:planId
PATCH /v1/projects/:projectId/plans/:planId
POST /v1/projects/:projectId/plans/:planId/generate-candidates
POST /v1/projects/:projectId/plans/:planId/synthesize
POST /v1/projects/:projectId/plans/:planId/refine
POST /v1/projects/:projectId/plans/:planId/fresh-eyes
POST /v1/projects/:projectId/plans/:planId/lock
POST /v1/projects/:projectId/plans/:planId/convert-to-beads

GET  /v1/projects/:projectId/beads
GET  /v1/projects/:projectId/beads/:beadId
PATCH /v1/projects/:projectId/beads/:beadId
POST /v1/projects/:projectId/beads/:beadId/dependencies
GET  /v1/projects/:projectId/bead-graph
GET  /v1/projects/:projectId/bv/triage
GET  /v1/projects/:projectId/bv/plan
GET  /v1/projects/:projectId/bv/insights
POST /v1/projects/:projectId/bead-polish

POST /v1/projects/:projectId/swarms
GET  /v1/projects/:projectId/swarms
GET  /v1/projects/:projectId/swarms/:swarmId
POST /v1/projects/:projectId/swarms/:swarmId/send
POST /v1/projects/:projectId/swarms/:swarmId/broadcast
POST /v1/projects/:projectId/swarms/:swarmId/interrupt
POST /v1/projects/:projectId/swarms/:swarmId/stop
POST /v1/projects/:projectId/swarms/:swarmId/review-mode
POST /v1/projects/:projectId/swarms/:swarmId/operator-tick
GET  /v1/projects/:projectId/swarms/:swarmId/agents/:agentId/tail

GET  /v1/projects/:projectId/activity
GET  /v1/projects/:projectId/mail
POST /v1/projects/:projectId/mail
GET  /v1/projects/:projectId/reservations
POST /v1/projects/:projectId/reservations/:reservationId/release

GET  /v1/projects/:projectId/code-health
POST /v1/projects/:projectId/code-health/run
POST /v1/projects/:projectId/code-health/create-bead

GET  /v1/jobs
GET  /v1/jobs/:jobId
GET  /v1/jobs/:jobId/events
POST /v1/jobs/:jobId/cancel

GET  /v1/audit
GET  /v1/events
```

### 17.2 Event stream

Use SSE for general app events and WebSocket for high-frequency terminal/pane streams.

General events:

```ts
type HoopoeEvent =
  | { type: "job.started"; job: Job }
  | { type: "job.log"; jobId: string; stream: "stdout" | "stderr"; text: string }
  | { type: "job.finished"; jobId: string; result: JobResult }
  | { type: "project.reconciled"; projectId: string; summary: ReconcileSummary }
  | { type: "plan.updated"; projectId: string; plan: Plan }
  | { type: "bead.updated"; projectId: string; bead: Bead }
  | { type: "bead.graph.updated"; projectId: string; graph: BeadGraphSummary }
  | { type: "swarm.updated"; swarm: SwarmSession }
  | { type: "agent.status"; swarmId: string; agent: Agent }
  | { type: "mail.message"; projectId: string; message: AgentMailMessage }
  | { type: "reservation.updated"; projectId: string; reservation: FileReservation }
  | { type: "health.updated"; projectId: string; snapshot: CodeHealthSnapshot }
  | { type: "operator.tick"; swarmId: string; result: OperatorTickSummary }
  | { type: "operator.alert"; swarmId: string; alert: Alert };
```

Pane stream events:

```ts
type PaneStreamEvent =
  | { type: "pane.attach"; agentId: string; ring: string; cursor: string }
  | { type: "pane.bytes"; agentId: string; data: string; cursor: string }
  | { type: "pane.resize"; agentId: string; cols: number; rows: number }
  | { type: "pane.detached"; agentId: string; reason: string };
```

---

## 18. Jobs and command execution

### 18.1 Job model

All long-running operations are jobs.

```ts
type Job = {
  id: string;
  projectId?: string;
  planId?: string;
  beadId?: string;
  swarmId?: string;
  kind:
    | "vps_setup"
    | "tool_inventory"
    | "repo_clone"
    | "repo_sync"
    | "plan_generate"
    | "plan_synthesize"
    | "plan_refine"
    | "plan_fresh_eyes"
    | "bead_convert"
    | "bead_polish"
    | "swarm_launch"
    | "swarm_prompt"
    | "operator_tick"
    | "review_round"
    | "health_snapshot"
    | "test_run"
    | "artifact_cleanup";
  status: "queued" | "running" | "succeeded" | "failed" | "cancelled";
  commandPreview?: string;
  createdBy: "user" | "operator_loop" | "system";
  startedAt?: string;
  endedAt?: string;
  logsPath: string;
  artifactPaths: string[];
  auditEventIds: string[];
};
```

### 18.2 Job lifecycle

```text
queued
  → running
  → succeeded | failed | cancelled
```

Jobs must support:

- live logs;
- cancellation where safe;
- idempotent resume for setup/install jobs;
- artifact capture;
- result summary;
- audit linkage.

### 18.3 Command parser tests

Every adapter that parses CLI output should have:

- fixture samples;
- golden output tests;
- version-specific tests;
- failure-mode tests;
- unknown-field tolerance where possible.

---

## 19. Design system and UX

### 19.1 Visual identity

Encode the reference design as tokens.

Core tokens:

```text
sidebar-bg:      near-black warm brown
content-bg:      warm cream
card-bg:         slightly lighter cream
border:          soft warm gray
accent-orange:   Hoopoe orange
success-green:   muted green
warning-amber:   muted amber
danger-red:      muted red
text-primary:    deep brown/black
text-muted:      warm gray
```

### 19.2 Layout primitives

- two-zone shell: dark sidebar, cream content;
- numbered workflow nav;
- stage header with uppercase tracked label;
- dense card grid;
- split panes;
- drawer detail panels;
- sticky top repo/status bar;
- command palette;
- status footer with VPS/swarm state.

### 19.3 Reusable components

- `StageHeader`;
- `ProjectCard`;
- `StatusPill`;
- `PriorityChip`;
- `AgentTile`;
- `BeadCard`;
- `CoverageBar`;
- `ComplexityBadge`;
- `TerminalPane`;
- `TimelineRow`;
- `HealthKpiCard`;
- `JobLogViewer`;
- `ApprovalDialog`;
- `CommandPalette`.

### 19.4 Agent identity tiles

Agent families:

```text
C1/C2/...  Claude      orange
X1/X2/...  Codex       green
G1/G2/...  Gemini      blue
U1/...     Cursor      gray
A1/...     Aider       purple
P1/...     Amp         red/brown
```

Use the same tile component in Activity, Swarm, Bead cards, and reservation lists.

### 19.5 Command palette

The command palette should include:

- create/import plan;
- run plan critique;
- convert plan to beads;
- run bead polish round;
- show ready beads;
- show critical path;
- launch swarm;
- broadcast message;
- run operator tick now;
- show stuck beads;
- show stale reservations;
- run code health scan;
- create bead from hotspot;
- run review round;
- run ACFS doctor;
- open terminal;
- sync repo;
- show audit log.

---

## 20. Testing strategy

### 20.1 Desktop tests

- unit tests for UI state machines;
- API client tests with mocked daemon;
- component tests for stage views;
- Playwright E2E against mocked daemon;
- visual regression tests for design system components;
- reconnect/resume tests for event stream;
- Keychain/tunnel integration tests where possible.

### 20.2 Daemon tests

- unit tests for adapters;
- command allowlist/policy tests;
- job lifecycle tests;
- audit log tests;
- event stream tests;
- parser golden tests for `br`, `bv`, `ntm`, Agent Mail, `ru`;
- reconciliation tests;
- build queue tests;
- stalled bead detection tests;
- rate-limit detection tests;
- security redaction tests.

### 20.3 Integration tests

Use fixture repos:

```text
fixture-basic-js
fixture-python
fixture-rust
fixture-go
fixture-beads-graph
fixture-agent-mail
fixture-ntm-session
```

Scenarios:

- initialize project;
- import plan;
- convert plan to beads using mock agent;
- load Kanban/DAG;
- simulate Agent Mail messages;
- simulate stale reservations;
- launch mock NTM session;
- stream pane output;
- run health snapshot;
- create bead from hotspot;
- reconnect after daemon restart.

### 20.4 End-to-end tests

Disposable VPS/VM test:

1. provision Ubuntu host;
2. install ACFS;
3. install Hoopoe daemon;
4. connect desktop or test harness;
5. import fixture repo;
6. create plan;
7. convert to beads;
8. launch tiny mock swarm or real two-agent smoke swarm;
9. ingest Agent Mail;
10. run health scan;
11. shut down cleanly.

### 20.5 Production smoke checks

Before each release:

- fresh install works;
- existing VPS reconnect works;
- daemon upgrade works;
- SSH tunnel resumes after sleep simulation;
- no secrets appear in logs;
- known tool versions pass adapter tests;
- old project metadata migrates.

---

## 21. Observability, audit, and recovery

### 21.1 Audit log

Every meaningful daemon action writes to `~/.hoopoe/audit.jsonl`:

```json
{
  "id": "evt-...",
  "time": "...",
  "actor": "user|operator_loop|system",
  "projectId": "...",
  "action": "bead.reopen_stalled",
  "reason": "agent wedged; no activity for 45m",
  "commandPreview": "br update br-a1b2 --status open",
  "result": "succeeded",
  "artifacts": [".hoopoe/jobs/job-123.json"]
}
```

### 21.2 Reconnect and replay

The event stream should support:

- last-event-id cursor;
- replay from append-only event log;
- catch-up after laptop sleep;
- daemon restart recovery;
- pane ring buffer reattach.

### 21.3 Recovery screens

Add a “Recovery” or “Diagnostics” panel:

- daemon status;
- tunnel status;
- NTM sessions;
- active jobs;
- stuck jobs;
- stale locks;
- last operator ticks;
- tool versions;
- disk pressure;
- recent audit events;
- repair actions.

---

## 22. Packaging and updates

### 22.1 Desktop packaging

- macOS signed and notarized app;
- DMG distribution for v1;
- Sparkle/electron-updater-style auto-update;
- crash reporting opt-in;
- telemetry opt-in/off by default if desired;
- migration scripts for local settings.

### 22.2 Daemon updates

The desktop app should detect daemon version mismatch and offer upgrade.

Upgrade flow:

1. download signed daemon binary or copy bundled binary;
2. verify checksum/signature;
3. stop service;
4. backup config/db;
5. install binary;
6. start service;
7. verify `/v1/version`;
8. run compatibility checks.

### 22.3 Tool version pinning

For production reliability:

- record ACFS/tool versions;
- warn on unsupported versions;
- allow user to pin/upgrade;
- run adapter contract tests against pinned versions;
- show drift in settings.

---

## 23. Milestone roadmap

### Phase 0 — Research spike and integration contract

Goal: prove the stack can be read and controlled from code.

Deliverables:

- test VPS with ACFS;
- NTM server running;
- sample project with `br` and `bv`;
- script that outputs a full JSON snapshot:
  - Git;
  - beads;
  - `bv` triage;
  - NTM session;
  - Agent Mail messages/reservations;
  - health metrics;
- documented command/API contracts;
- parser fixtures.

Exit criteria:

- one command produces a reliable machine-readable project snapshot.

### Phase 1 — Desktop shell and design system

Deliverables:

- Electron/Vite/React app;
- dark sidebar + cream content shell;
- five routed empty stages;
- design tokens;
- reusable stage/header/card components;
- command palette shell;
- local settings store;
- Keychain integration stub.

Exit criteria:

- visual review against reference design passes; app can navigate stages.

### Phase 2 — VPS connection and daemon skeleton

Deliverables:

- SSH profile manager;
- tunnel manager;
- daemon Go skeleton;
- `/health`, `/version`, `/events`, `/jobs`;
- auth token;
- systemd install script;
- bootstrap over SSH;
- job log streaming.

Exit criteria:

- connect to existing VPS, install daemon, stream a remote job log.

### Phase 3 — ACFS onboarding and tool inventory

Deliverables:

- setup wizard;
- preflight checks;
- ACFS install/doctor integration;
- resumable setup checkpoints;
- tool inventory screen;
- daemon upgrade flow.

Exit criteria:

- fresh supported VPS reaches “ready” state from Hoopoe.

### Phase 4 — Project registry and Git

Deliverables:

- create/import/clone project;
- project readiness checks;
- `.hoopoe` initialization;
- Git status top bar;
- AGENTS.md detection/editor link;
- `br` initialization check;
- `ru --json` multi-repo support where applicable.

Exit criteria:

- user can open a repo-backed project and see accurate Git/tool state.

### Phase 5 — Plans workspace

Deliverables:

- plan cards;
- CodeMirror plan editor;
- artifact rail;
- import/create flows;
- multi-model candidate jobs;
- synthesis and refinement artifacts;
- plan quality tracker;
- lock plan action.

Exit criteria:

- one-paragraph idea can become a locked plan with candidate/synthesis artifacts.

### Phase 6 — Bead conversion and quality tracker

Deliverables:

- `br` adapter;
- plan-to-beads job;
- `br sync --flush-only`;
- traceability map;
- bead quality tracker;
- polish round jobs;
- `bv` adapter.

Exit criteria:

- locked plan converts into real `br` beads with dependencies and traceability.

### Phase 7 — Kanban, DAG, Force views

Deliverables:

- Kanban columns/cards;
- bead drawer;
- DAG graph;
- Force graph;
- filters;
- dependency editing;
- cycle warnings;
- critical path and ready frontier;
- `bv --robot-triage` panel.

Exit criteria:

- user can curate beads visually and understand graph state without a terminal.

### Phase 8 — Swarm launch MVP

Deliverables:

- swarm composition form;
- NTM launch integration;
- staggered kickoff;
- launch prompt renderer;
- agent grid;
- basic agent status;
- terminal/log tail;
- send/broadcast/interrupt/stop.

Exit criteria:

- launch and observe a mixed small swarm against a real bead set.

### Phase 9 — Activity and Agent Mail

Deliverables:

- timeline;
- mail ingestion;
- reservation view;
- urgent alerts;
- overseer compose;
- bead/agent pivot links;
- conflict/stale reservation warnings.

Exit criteria:

- user can coordinate the swarm from Hoopoe without opening Agent Mail manually.

### Phase 10 — Operator loop

Deliverables:

- 4-minute loop;
- idle/wedged/rate-limit detection;
- stale bead detection;
- stale reservation detection;
- build contention detection;
- disk cleanup;
- marching orders;
- audit event visibility.

Exit criteria:

- Hoopoe can tend a real swarm for an hour with visible, explainable interventions.

### Phase 11 — Code Health

Deliverables:

- health adapter discovery;
- test/coverage/complexity parsing;
- health snapshots;
- KPI cards;
- file health table;
- hotspot scoring;
- create bead from hotspot;
- trends.

Exit criteria:

- swarm round updates health; low-coverage/high-complexity files can become beads.

### Phase 12 — Review mode and convergence

Deliverables:

- review-only swarm mode;
- review round jobs;
- fresh-eyes prompts;
- cross-agent review;
- finding tracker;
- finding-to-bead conversion;
- convergence dashboard;
- final landing checklist.

Exit criteria:

- completed implementation transitions into structured review/hardening and reaches final gate.

### Phase 13 — Provider automation and production polish

Deliverables:

- one provider plugin;
- cost estimate and teardown;
- polished empty/loading/error states;
- onboarding tour;
- diagnostics screen;
- crash reports opt-in;
- signed/notarized app;
- daemon upgrade system;
- documentation and demo project.

Exit criteria:

- a new user can install Hoopoe, connect/provision a VPS, import a project, create a plan, convert beads, launch agents, monitor review, and land a small project.

---

## 24. MVP scope

### 24.1 MVP must include

- Electron app with five-stage shell and design system.
- Existing VPS connection.
- Hoopoe daemon install over SSH.
- SSH tunnel and event stream.
- ACFS install/doctor/tool inventory.
- Project import/create.
- Plan import/create/editor.
- Plan-to-beads conversion through `br`.
- Bead Kanban and basic DAG.
- `bv --robot-triage` panel.
- NTM swarm launch.
- Agent grid with status and log tail.
- Agent Mail timeline.
- Basic operator loop for idle/stale agents.
- Basic code health scan.
- Audit log.

### 24.2 MVP can defer

- multi-provider automatic VPS provisioning;
- perfect PTY fidelity for all panes;
- direct mTLS public daemon mode;
- full spend precision;
- advanced Force graph interactions;
- full language coverage;
- CASS/CM deep memory workflows;
- collaborative multi-user teams;
- hosted relay/cloud sync;
- Mac App Store distribution.

### 24.3 Walking skeleton order

Build first:

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

## 25. Risks and mitigations

### Risk: PTY streaming fidelity fails

Mitigation:

- prototype a single pane early;
- use NTM stream/robot surfaces first;
- keep tmux capture fallback;
- use ring buffers and reconnect cursors;
- treat terminal output as observability, not canonical state.

### Risk: tool output drift breaks adapters

Mitigation:

- prefer robot/API/JSON surfaces;
- pin versions;
- golden tests;
- tool inventory and compatibility warnings;
- user-controlled updates.

### Risk: Hoopoe cache diverges

Mitigation:

- periodic reconciliation;
- canonical tool state wins;
- explicit stale cache indicators;
- “reload from tools” action;
- source-of-truth table in docs.

### Risk: first install is brittle

Mitigation:

- existing VPS first;
- checkpointed setup;
- clear logs plus structured steps;
- one provider only after manual path works;
- diagnostics and resume.

### Risk: costs run away

Mitigation:

- budget caps;
- alert thresholds;
- rate-limit detection;
- CAAM integration when configured;
- stop/pause policies;
- spend estimates labeled clearly.

### Risk: agents compete for builds/tests

Mitigation:

- build queue;
- `rch` preference;
- dedupe repeated commands;
- operator warnings;
- disk pressure cleanup.

### Risk: stale agents hold beads/reservations hostage

Mitigation:

- stalled bead detection;
- stale reservation detector;
- forced release with audit;
- reopen/reassign workflows;
- review of in-progress age.

### Risk: unsafe commands are accidentally exposed

Mitigation:

- typed command specs;
- allowlist;
- path sandboxing;
- approval gates;
- DCG/NTM safety checks;
- audit log;
- no arbitrary shell API.

### Risk: planning quality is weak

Mitigation:

- competing model candidates;
- synthesis artifacts;
- quality tracker;
- fresh-eyes review;
- lock gate;
- bead traceability.

### Risk: users trust subjective scores too much

Mitigation:

- label scores as decision aids;
- show underlying evidence;
- allow override;
- keep canonical artifacts visible.

### Risk: laptop sleep breaks perception of reliability

Mitigation:

- VPS owns jobs/loops;
- event replay;
- pane ring buffers;
- reconnect UI;
- no swarm dependency on Electron process.

---

## 26. Definition of success

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

## 27. Immediate first engineering tasks

This week:

1. Scaffold Electron + React + TypeScript.
2. Create design tokens and five-stage shell.
3. Scaffold Go daemon with `/health`, `/version`, `/events`, `/jobs`.
4. Implement local app connection profile storage.
5. Implement SSH connection test and tunnel.
6. Write bootstrap script for existing Ubuntu VPS.
7. Install daemon via SSH and start systemd service.
8. Build one remote job: `tool_inventory`.
9. Build one project endpoint: list registered projects.
10. Build one UI slice: Stage 1 showing an empty/mocked plan list.

Next week:

1. Add project registration.
2. Add `br` list adapter.
3. Add `bv --robot-triage` adapter.
4. Add Kanban prototype.
5. Add event replay.
6. Add parser fixture tests.
7. Add command audit log.
8. Add setup diagnostics.

Do not start with provider automation, spend charts, or polished graph animations. The first milestone is a working cockpit connected to a real VPS daemon with one real project and one real tool adapter.

---

## 28. Reference sources and integration assumptions

This plan assumes Hoopoe integrates with the current public behavior of:

- Agentic Coding Flywheel methodology: `agent-flywheel.com/complete-guide`
- ACFS setup: `github.com/Dicklesworthstone/agentic_coding_flywheel_setup`
- NTM: `github.com/Dicklesworthstone/ntm`
- Beads Rust: `github.com/Dicklesworthstone/beads_rust`
- Repo Updater: `github.com/Dicklesworthstone/repo_updater`
- Agent Mail: `github.com/Dicklesworthstone/mcp_agent_mail`
- Beads workflow skill: `jeffreys-skills.md/skills/beads-workflow`
- Vibing with NTM skill: `jeffreys-skills.md/skills/vibing-with-ntm`

Before implementation, Phase 0 should verify actual installed command names, output formats, version compatibility, and exact API surfaces on a fresh ACFS VPS.
