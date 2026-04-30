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

The product is organized into four numbered stages:

```text
01 Planning
02 Beads
03 Swarm
04 Debugging / Hardening
```

Plus one cross-stage UI surface — the **Activity panel** — which shows the live message stream between agents (via Agent Mail) and between the user and the orchestrator agent. Activity is not a stage; it is a persistent panel available from any stage, most heavily used during Swarm and Debugging.

The user spends most of their meaningful cognitive effort in **Planning** and **Beads**. The later stages are mostly machine-tending, review, intervention, and quality convergence. The engineering roadmap mirrors that distribution: cockpit + setup first, then plan creation and bead curation, before pouring months into perfect swarm telemetry.

---

## 1. Product principles

### 1.1 Preserve native sources of truth

Hoopoe must never become a fragile parallel database that silently diverges from the tools agents actually use.


| Domain                            | Canonical source                                                                                                                                                                                                                                     | Hoopoe role                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                   |
| --------------------------------- | ---------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | ----------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| Code (canonical)                  | **Origin** (GitHub / GitLab / etc.) — the durable, team-shared, CI-attached source of truth. Survives the VPS being destroyed and rebuilt.                                                                                                           | Hoopoe never writes to origin directly; pushes happen from the VPS clone via the daemon. Display all commit history, diffs, branches, tags from origin (via the desktop's local clone).                                                                                                                                                                                                                                                                                                                                                                                                                                       |
| Code (VPS working state)          | VPS clone at `/data/projects/<project>/` — a clone of origin where agents actually work. Commits land here first, then are pushed to origin per the swarm push policy (§7.3). Uncommitted or unpushed work is durable only until the VPS disk fails. | Trigger Git operations *via the daemon* (which executes them on the VPS) — never run project-level `git` from the desktop; safety gates on destructive ops; audit every Git op Hoopoe initiates. Surface "N unpushed commits" as a status indicator. WIP reads (working-tree diffs, staged hunks, currently-edited files) come from daemon RPCs, not the local clone.                                                                                                                                                                                                                                                         |
| Code (desktop sync-driven mirror) | Local clone at `~/Library/Application Support/Hoopoe/projects/<project-id>/repo/`, fetched from **origin** (not from the VPS), kept in sync with origin via `origin_updated` events + 60s safety-net fetch                                           | Fast local file reads, diffs, blame, ripgrep, "open in editor" links over the *canonical* (pushed) history; never a write target through Hoopoe; full detail in §7.7. By definition shows only what has reached origin — uncommitted/unpushed VPS work is read via daemon RPCs instead.                                                                                                                                                                                                                                                                                                                                       |
| Plans                             | Markdown files in repo                                                                                                                                                                                                                               | editor, versioning, synthesis artifacts, state metadata                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                       |
| Beads                             | `br` / `.beads`                                                                                                                                                                                                                                      | command wrapper, read model, kanban/DAG visualization                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                         |
| Bead graph intelligence           | `bv --robot-`*                                                                                                                                                                                                                                       | triage panels, graph metrics, launch readiness                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                |
| Swarm sessions                    | `ntm` + tmux                                                                                                                                                                                                                                         | launch, observe, send, recover, checkpoint                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                    |
| Agent communication               | Agent Mail                                                                                                                                                                                                                                           | timeline, threads, reservations, overseer broadcast                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                           |
| Build/test execution              | `rch`, language-native test runners, job queue, NTM pipelines/controllers                                                                                                                                                                            | dedupe, throttle, stream logs, record artifacts; tend per the `vibing-with-ntm` playbook (build/test contention detection, repeated-failed-test recovery, queue depth thresholds, stale-artifact cleanup)                                                                                                                                                                                                                                                                                                                                                                                                                     |
| Code health                       | coverage/complexity reports + Git                                                                                                                                                                                                                    | normalize snapshots and trends; trigger snapshots and review rounds per the `vibing-with-ntm` playbook (post-round snapshot cadence, hotspot-targeted review prompts, convergence detection feeding the flip into review-only mode)                                                                                                                                                                                                                                                                                                                                                                                           |
| Swarm tending methodology         | `ntm` and `vibing-with-ntm` skills (jeffreys-skills.md, agentskills.io standard)                                                                                                                                                                     | Hoopoe loads these skills directly into the tending agents (§8) at runtime — they are the behavioral spec, not a source of inspiration to reimplement in Go. The daemon's job is the scheduler, pre-scripts, approval gates, and audit; the skills are the judgment.                                                                                                                                                                                                                                                                                                                                                          |
| Agent skill installation/updates  | `jsm` (Jeffrey's Skills.md CLI, [jeffreys-skills.md](https://jeffreys-skills.md/dashboard)) — preferred path; `jfp` (Jeffrey's Prompts, ACFS-installed) — free fallback. Both install from `jeffreys-skills.md` per the agentskills.io standard.     | Hoopoe's skill loader (§12 Phase 10) prefers `jsm` when the user has a subscription configured: SHA-256 deterministic versioning gives reproducible skill loads, cross-device sync keeps multi-workstation users aligned, and the premium catalog includes curated skills beyond the free set. Falls back to `jfp` when `jsm` is unavailable or unconfigured. Either way Hoopoe never reimplements skill fetch/cache; upstream skill changes propagate without Hoopoe code changes. Verify skill compatibility at swarm-launch time, pin skill versions by SHA-256 when `jsm` is in use, and warn on stale or drifted copies. |
| Safety approvals                  | NTM/DCG/SLB/policy tools                                                                                                                                                                                                                             | surface state, require approvals, audit decisions                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                             |
| Destructive-command policy        | `DCG` (Destructive Command Guard) — Claude Code hook intercepting dangerous git/fs commands at the agent boundary                                                                                                                                    | Hoopoe does not run a parallel guard. DCG verdicts (`blocked`, `requires_confirmation`, `allowed`) are ingested as approval-source events and merged into the approvals queue (§5.3), so blocked actions appear in the same UI as Hoopoe-policy approvals with the DCG rule attached.                                                                                                                                                                                                                                                                                                                                         |
| Two-person rule (high-risk)       | `SLB` (Simultaneous Launch Button)                                                                                                                                                                                                                   | Optional add-on for the `autopilot` safety preset (§7.3). When enabled, SLB co-signature is required for the destructive-action class; Hoopoe's approvals UI reflects the SLB state and never bypasses it.                                                                                                                                                                                                                                                                                                                                                                                                                    |
| LLM provider account credentials  | `CAAM` (Cross-Agent Account Manager)                                                                                                                                                                                                                 | Hoopoe never stores agent-CLI credentials directly; account inventory, current-account-per-agent, and switch-account actions are read from / driven through CAAM. The Activity panel can offer "switch account" as a recovery action when an agent rate-limits (§7.3).                                                                                                                                                                                                                                                                                                                                                        |
| LLM cost & rate-limit telemetry   | `caut` (coding agent usage tracker), `rano` (network observer for AI CLIs), per-CLI status messages, NTM events                                                                                                                                      | The top-bar cost pill (§7.6) and the `watch-safety-thresholds` tending job (§8.4) read from `caut`'s usage snapshots; `rano` provides per-call latency/error signals for diagnostics. Hoopoe never invents spend numbers; if `caut` is unavailable the UI says so rather than displaying a fake estimate.                                                                                                                                                                                                                                                                                                                     |
| Session resumption across CLIs    | `casr` (Cross-Agent Session Resumer)                                                                                                                                                                                                                 | When an agent rate-limits, crashes, or needs to swap providers, Hoopoe's recovery action invokes `casr` to convert the in-flight session and resume under a different account/CLI rather than discarding context. Surfaced both in the Activity panel and in `tend-swarm`'s repertoire (§8.4).                                                                                                                                                                                                                                                                                                                                |
| System resource health            | `srp` (System Resource Protection — ananicy-cpp + monitor), `sbh` (disk-pressure defense), `pt` (process-terminator)                                                                                                                                 | The `watch-safety-thresholds` pre-script (§8.4) reads disk/CPU/load signals from `srp`, runs `sbh`-driven cleanup under disk pressure, and uses `pt` to kill genuinely wedged processes (with audit). These are the deterministic actuators behind "disk pressure cleanup" and "kill wedged pane."                                                                                                                                                                                                                                                                                                                            |
| Bug scanning & code review tools  | `UBS` (Ultimate Bug Scanner) — primary; specialized audit skills (mock-code, deadlock, security, perf, fuzzing) — secondary                                                                                                                          | Standard tool invoked by §9.2 review rounds (especially round 8 specialized audits) and §9.5 specialized audits. Findings flow into the §9.3 finding ledger with the source tool stamped on each finding so cross-tool deduping is possible.                                                                                                                                                                                                                                                                                                                                                                                  |
| Plan refinement automation        | `apr` (Automated iterative spec refinement) — *candidate, decision pending, see §7.1*                                                                                                                                                                | Open architectural question: does Hoopoe's §7.1 planning pipeline (candidates → synthesis → fresh-eyes critique → refinement rounds) own this in-house, or delegate to `apr` and present results in the Plan workspace? The plan currently owns it; the alternative is recorded as an open decision.                                                                                                                                                                                                                                                                                                                          |


Hoopoe maintains a cache and append-only event log, but it should always be able to answer:

> What is true if we ignore the Hoopoe cache and re-read Git, `br`, `bv`, NTM, Agent Mail, and test reports?

That question guides every integration boundary.

### 1.2 The desktop app is not the orchestrator of record

Electron can sleep, crash, lose Wi-Fi, or be closed. The swarm must continue. All long-running jobs, the tending scheduler (§8), state reconciliation, and review cycles run on the VPS under the Hoopoe daemon and/or NTM. The desktop reconnects, replays events, and rehydrates UI state. It does not own the running swarm.

### 1.3 Use robot/API surfaces first, shell parsing last

Integration precedence:

1. Official REST/SSE/WebSocket/OpenAPI surfaces, especially NTM `serve`.
2. Tool-provided robot/JSON output: `ntm --robot-`*, `bv --robot-*`, `br --json`, `ru --json`.
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

### 1.7 Sync-driven mirrors are allowed; parallel sources of truth are not

Hoopoe may keep local sync-driven mirrors of canonical state — file contents from a local Git clone of origin, the daemon's read-model cache of bead state, health snapshots, plan artifacts — when they are a meaningful UX or performance win. The rule is strict: a sync-driven mirror (a) is fed from canonical state on change events (or a bounded periodic refresh) and never written to by the user through Hoopoe, (b) carries enough metadata (source SHA, snapshot timestamp, sequence number) to be reconciled or invalidated against canonical state at any moment, and (c) is replaceable by re-fetching from canonical state without data loss. The desktop's local Git clone of origin (§7.7) is the largest example; the daemon's bead/NTM/Agent-Mail read models are smaller ones. None of these are sources of truth, and the UI should never make the user feel like they are.

### 1.8 Tend agents with skill-attached jobs, not bespoke loops

When Hoopoe needs to tend the swarm — detect idle/wedged/rate-limited agents, recover stalled beads, push stale commits, decide whether to flip into review mode, surface drift — the implementation is **a scheduler running skill-attached jobs**, not a hand-coded Go state machine. Each tending concern is declared as a job (schedule + pre-script + skill(s) + prompt + delivery target), modeled on the pattern Nous Research's Hermes Agent ([github.com/NousResearch/hermes-agent](https://github.com/NousResearch/hermes-agent)) ships at scale and on the `[agentskills.io](https://agentskills.io)` open skill standard.

The pre-script is deterministic Go that does the cheap mechanical reconcile (read canonical state, evaluate threshold conditions, perform safe deterministic actions like force-stop on a budget breach). When the pre-script detects nothing actionable, it returns `{"wakeAgent": false}` and no LLM fires — the tick costs zero. Only when the pre-script detects a condition that needs judgment does the agent wake, with the relevant skill (e.g., `vibing-with-ntm`) loaded into its context as the authoritative behavioral spec. The agent reasons, decides, and acts via daemon RPCs that go through the same approval gates as user actions. If the agent decides nothing was actually warranted on closer inspection, it replies `[SILENT]` and produces no Activity-panel noise (audit is preserved regardless).

This rule exists because the alternative — translating `vibing-with-ntm` and similar playbooks into Go state machines — guarantees drift between what the skill says and what the code does, costs months of engineering, and forecloses the ability to adapt to novel situations the skill author thought of but the code didn't. Skills are the spec; jobs are the substrate that loads them. See §8 for the full job format, the initial job set, and the implementation pattern.

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
                ▲              │
   git fetch    │              │ SSH tunnel by default
   from origin  │              │ optional pinned mTLS direct mode later
   (read-only)  │              ▼
                │      VPS localhost
                │      ┌──────────────────────────────────────────────────────────────┐
                │      │ Hoopoe VPS Daemon                                             │
                │      │  - REST API + SSE/WebSocket event stream                      │
                │      │  - job runner and append-only audit log                       │
                │      │  - adapters around ACFS, br, bv, ntm, Agent Mail, git, rch    │
                │      │  - cached read models and reconciliation loops                │
                │      │  - PTY/tmux/NTM pane streaming bridge                         │
                │      │  - tending scheduler (§8) and build/test queue                │
                │      └──────────────────────────────┬───────────────────────────────┘
                │                                     │
                │                                     ▼
                │      Existing Flywheel stack on VPS
                │      ┌──────────────────────────────────────────────────────────────┐
                │      │ ACFS-installed tooling                                        │
                │      │  - Claude Code, Codex CLI, Gemini CLI                         │
                │      │  - NTM / tmux                                                 │
                │      │  - br / bv                                                    │
                │      │  - Agent Mail                                                 │
                │      │  - ru, rch, CAAM, DCG, CASS, UBS, language runtimes           │
                │      │  - project Git repos under /data/projects ───── push/pull ─── ▶ origin (GitHub/GitLab/etc.)
                │      └──────────────────────────────────────────────────────────────┘                              │
                │                                                                                                    │
                └────────────────────────────────────────────────────────────────────────────────────────────────────┘
                            (desktop fetches the same origin the VPS pushes to;
                             local clone is read-only mirror, never a write target)

Local sync-driven mirror (on desktop, see §1.7 / §7.7)
┌──────────────────────────────────────────────────────────────┐
│ ~/Library/Application Support/Hoopoe/projects/<id>/repo/     │
│  - full clone of project, all refs fetched                   │
│  - sync triggered by `origin_updated` WS event from daemon   │
│    events from the daemon, plus 60s safety-net fetch         │
│  - feeds fast file reads, diffs, blame, ripgrep, "open in    │
│    editor" links across all stages                           │
│  - never a write target through Hoopoe                       │
└──────────────────────────────────────────────────────────────┘
```

### 2.1 Why the VPS daemon is necessary

Direct SSH command execution from Electron is good enough for bootstrap but not for the product. The daemon is required because Hoopoe needs:

- stable APIs instead of ad hoc command strings;
- background jobs that survive Electron restarts;
- the tending scheduler (§8) and periodic reconciliation;
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

### 2.5 Transport ladder and desktop connection FSM

The steady-state transport is deliberately conservative, but the implementation should be explicit about fallbacks and future upgrades.

**Transport ladder:**

1. **Bootstrap SSH** — used for first connect, preflight, ACFS installation, daemon install/upgrade, recovery shells, and emergency diagnostics. Not used for normal hot-path app traffic after the daemon is reachable.
2. **SSH local tunnel** — v1 default. Daemon binds to `127.0.0.1` on the VPS; desktop forwards a local port and speaks HTTPS/WS to `localhost:<forwarded-port>`.
3. **Tailscale / tailnet mode** — optional later. The desktop may manage a small `tsnet` sidecar or use an already-installed Tailscale daemon, then connect to a daemon listener bound only to the tailnet interface. This is attractive for teams and long-lived reconnect reliability but should not be a v1 blocker.
4. **Pinned mTLS direct mode** — advanced/team mode after v1. Requires explicit opt-in, firewall restriction, bearer auth on top of mTLS, and loud diagnostics if exposed publicly.
5. **Recovery SSH shell** — manual break-glass action surfaced in Diagnostics. It opens a terminal-like recovery channel but does not grant the renderer arbitrary shell execution.

**Desktop `ConnectionManager` FSM:**

```text
unconfigured
  → ssh_probing
  → bootstrapping
  → tunnel_connecting
  → authenticating
  → ready
  → degraded
  → reconnecting
  → ready
  → disconnected
```

Triggers include macOS sleep/wake, tunnel death, daemon health failure, WS heartbeat timeout, version mismatch, expired bearer, and network change. Backoff uses jitter with a 30-second cap. Reconnect always performs: tunnel check → bearer/session check → WS-token refresh → subscribe with sequence cursors → fetch one snapshot for active channels → reconcile local clone if project is active.

### 2.6 Seed daemon API and event contract

The formal OpenAPI lives in `packages/schemas/openapi.yaml`, but Phase 2 needs a seed contract that engineers can implement immediately. All write endpoints accept an `Idempotency-Key`; all errors use RFC 7807-style `problem+json`; all state-changing calls write audit entries.

```text
System      GET  /v1/health
            GET  /v1/version
            GET  /v1/compatibility
            GET  /v1/capabilities
            GET  /v1/system/specs
            GET  /v1/system/processes

Auth        POST /v1/auth/bootstrap/bearer
            POST /v1/auth/ws-token
            POST /v1/auth/session/revoke
            POST /v1/auth/rotate-secret        # owner only, explicit approval

Events      GET  /v1/events/replay?channel=&sinceSequence=
            GET  /v1/events/sse?channels=
            GET  /v1/events/ws-token
            WS   /v1/events/ws

Jobs        GET  /v1/jobs
            POST /v1/jobs/{id}/cancel
            GET  /v1/jobs/{id}/log?offset=
            GET  /v1/jobs/{id}/artifacts

Bootstrap   POST /v1/bootstrap/preflight
            POST /v1/bootstrap/acfs/start
            POST /v1/bootstrap/acfs/resume
            POST /v1/bootstrap/daemon/upgrade

Projects    GET  /v1/projects
            POST /v1/projects
            GET  /v1/projects/{projectId}
            POST /v1/projects/{projectId}/activate
            GET  /v1/projects/{projectId}/readiness

Git         GET  /v1/projects/{projectId}/git/status
            GET  /v1/projects/{projectId}/git/staged-diff
            GET  /v1/projects/{projectId}/git/unstaged-diff
            GET  /v1/projects/{projectId}/git/unpushed-commits
            POST /v1/projects/{projectId}/git/push

Plans       GET  /v1/projects/{projectId}/plans
            POST /v1/projects/{projectId}/plans
            POST /v1/projects/{projectId}/plans/{planId}/rounds
            POST /v1/projects/{projectId}/plans/{planId}/lock
            GET  /v1/projects/{projectId}/plans/{planId}/artifacts

Beads       GET  /v1/projects/{projectId}/beads
            GET  /v1/projects/{projectId}/beads/graph
            GET  /v1/projects/{projectId}/beads/ready
            GET  /v1/projects/{projectId}/beads/{beadId}
            PATCH /v1/projects/{projectId}/beads/{beadId}
            POST /v1/projects/{projectId}/beads/conversion-runs
            POST /v1/projects/{projectId}/beads/polish-runs

Swarm       POST /v1/projects/{projectId}/swarms
            GET  /v1/projects/{projectId}/swarms/{swarmId}
            POST /v1/projects/{projectId}/swarms/{swarmId}/broadcast
            POST /v1/projects/{projectId}/agents/{agentId}/send
            POST /v1/projects/{projectId}/agents/{agentId}/interrupt
            POST /v1/projects/{projectId}/agents/{agentId}/stop

Mail        GET  /v1/projects/{projectId}/mail/messages
            GET  /v1/projects/{projectId}/mail/threads/{threadId}
            POST /v1/projects/{projectId}/mail/messages
            GET  /v1/projects/{projectId}/reservations
            POST /v1/projects/{projectId}/reservations/force-release

Health      GET  /v1/projects/{projectId}/health/summary
            GET  /v1/projects/{projectId}/health/files
            POST /v1/projects/{projectId}/health/snapshots

Reviews     POST /v1/projects/{projectId}/reviews
            GET  /v1/projects/{projectId}/reviews/{reviewId}
            GET  /v1/projects/{projectId}/findings
            PATCH /v1/projects/{projectId}/findings/{findingId}

Tending     GET  /v1/projects/{projectId}/tending/jobs
            POST /v1/projects/{projectId}/tending/jobs/{jobId}/run
            PATCH /v1/projects/{projectId}/tending/jobs/{jobId}

Approvals   GET  /v1/projects/{projectId}/approvals
            GET  /v1/projects/{projectId}/approvals/{approvalId}
            POST /v1/projects/{projectId}/approvals/{approvalId}/approve
            POST /v1/projects/{projectId}/approvals/{approvalId}/deny
            POST /v1/projects/{projectId}/approvals/{approvalId}/extend
```

**WebSocket envelope:**

```json
{"op":"subscribe","channels":["project:abc","swarm:sw-123","activity:abc"],"cursors":{"project:abc":182}}
{
  "eventId": "evt_01...",
  "schemaVersion": 1,
  "channel": "project:abc",
  "type": "bead.changed",
  "sequence": 183,
  "time": "...",
  "actor": {"kind": "agent", "id": "ag_123"},
  "causationId": "cmd_01...",
  "correlationId": "swarm_01...",
  "data": {}
}
{"channel":"_system","type":"heartbeat","sequence":9821,"time":"..."}
{"channel":"project:abc","type":"_gap","from":120,"to":183,"repair":"replayEvents"}
```

Channels are bounded. Terminal/log streams are chunked and offset-addressable. A slow renderer can lag without causing daemon memory blowups; when it lags past an in-memory ring, the daemon emits `_lag` or `_gap` with a persisted log offset so the client fetches from disk.

### 2.7 Daemon process manager and job registry

The daemon needs a real process-management substrate, not scattered `exec.Command` calls. The substrate is greenfield Go but should follow these invariants:

- **One job registry** keyed by sortable IDs. Jobs survive HTTP requests and desktop disconnects; interrupted jobs are visible after daemon restart.
- **One process group per child** where possible. Cancellation sends SIGTERM to the group, waits a grace period, then escalates to SIGKILL. Long-running children should not become orphans.
- **Bounded semaphores** per resource: `llm_calls`, `git_ops_per_project`, `br_ops_per_project`, `health_runs_per_project`, `swarm_spawns_global`, `terminal_streams_per_client`.
- **Persistent logs** under `~/.hoopoe/logs/{jobId}.log`; in-memory rings are only an acceleration layer.
- **Chunked log API** with byte offsets, not "one huge terminal string." Terminal attach sends the latest ring plus the current offset.
- **Structured status** (`queued`, `running`, `waiting_approval`, `canceling`, `succeeded`, `failed`, `interrupted`) stored in daemon SQLite.
- **Artifact registry** for plan outputs, conversion traces, health snapshots, review findings, and bootstrap logs.
- **Idempotency keys** on every write endpoint that can be retried by a reconnecting desktop.

The build/test queue (§2.1, §8.5) is implemented on top of this job substrate. It dedupes equivalent commands when safe, throttles by project, and records exact command provenance for every result. It is also the source of truth for test/build evidence used by landing, reviews, health snapshots, and convergence.

Each result records:

- project ID, worktree path, branch, commit SHA;
- command, normalized argv, environment digest;
- tool versions and runner profile;
- lockfile/package manifest digest;
- started/completed timestamps and duration;
- stdout/stderr artifact IDs;
- parsed test cases, failures, coverage files, and exit code;
- failure fingerprint;
- cache key and cache-hit/cache-miss reason.

The queue may reuse a recent result only when commit, command, environment, and declared inputs match. Repeated failure fingerprints create "known failure" records so agents stop burning cycles on unchanged failures. Suspected flakes are marked separately and can trigger a flake-hardening bead.

### 2.8 Capability registry and degraded-mode contract

Hoopoe gates behavior by capabilities, not by optimistic assumptions about installed tool versions.

Each adapter reports:

```json
{
  "tool": "ntm",
  "version": "x.y.z",
  "source": "ntm serve",
  "capabilities": {
    "ntm.sessions.list": {"status":"ok"},
    "ntm.panes.stream": {"status":"ok", "transport":"websocket"},
    "ntm.approvals.list": {"status":"missing", "fallback":"none"},
    "ntm.robot.snapshot": {"status":"ok"}
  },
  "lastCheckedAt": "...",
  "fixturesVersion": "phase0-2026-04-30"
}
```

The desktop never assumes a feature exists because a tool is installed. It checks `/v1/capabilities` and renders one of: available, degraded, unavailable, or blocked-by-policy. Every major UI button declares the capability IDs it requires. Diagnostics explains the exact missing capability and the fallback in use.

Adapter contract tests assert capabilities, not just parser success. A fixture that parses but cannot satisfy the capability must not mark the feature as available.

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

### 4.1 Lifecycle states

In v1, a Hoopoe install pairs with exactly one VPS, and that VPS holds N projects (see [ADR-0001](docs/adr/0001-single-vps-per-install-v1.md)). VPS state and project state are therefore two independent lifecycles, not one combined machine. The VPS must reach `ready` before any project on it can advance past `imported`.

**VPS lifecycle** (per install — one VPS in v1):

```text
unconfigured
  → ssh_verified
  → daemon_running
  → tools_installed
  → ready
```

**Project lifecycle** (per project — N per VPS):

```text
imported
  → planning
  → plan_finalized
  → beads_created
  → beads_finalized
  → swarm_running
  → hardening_rounds
  → quality_gates
  → completed
```

Plan and swarm sub-states are derivable from canonical sources (`br list`, NTM session state, plan metadata) and live in `packages/schemas/`, not here. When multi-VPS becomes a real need, a `Connection` entity will own VPS state and projects will key to it; until then, the implicit single Connection has no UI and no schema presence.

### 4.2 Gate invariants

Before advancing from one stage to the next, these must be true:


| Gate                       | Must be true                                                                                                                        |
| -------------------------- | ----------------------------------------------------------------------------------------------------------------------------------- |
| VPS ready (precondition)   | SSH verified, daemon reachable, ACFS installed or intentionally skipped, tool versions recorded                                     |
| Project imported           | Git repo present, branch known, `AGENTS.md` present, `.hoopoe` initialized, tool detection done                                     |
| Plan locked     | plan self-contained, major decisions explicit, testing strategy present, unresolved decisions listed or accepted                    |
| Beads created   | `br` contains beads linked to plan, `.beads/issues.jsonl` flushed, conversion artifacts saved                                       |
| Beads finalized | plan-to-bead coverage checked, dependencies checked, ready set sufficient, bead clarity/testability acceptable                      |
| Launch ready    | NTM healthy, Agent Mail healthy, `bv --robot-*` healthy, `br ready --json` nonempty or intentionally scoped, build queue policy set |
| Hardening ready | implementation beads closed or intentionally deferred, no obvious stuck in-progress beads, review prompts available                 |
| Ship ready      | tests/builds pass or exceptions documented, code health gates pass or follow-up beads exist, Git/beads synced                       |


Entity schemas (Project, Plan, Bead, SwarmSession, Agent, FileReservation, BudgetPolicy, etc.) live in `packages/schemas/`.

---

## 5. Security model

### 5.1 Secrets

- **Local Mac:** SSH private key referenced or generated and stored in macOS Keychain; provider API tokens (if used); local connection-profile tokens; optional client-cert material for advanced mode; planning API keys *only* when the user opts into desktop-only planning mode (see §7.1).
- **VPS:** agent CLI credentials handled by ACFS/CAAM; planning API keys for server-side API mode (default); daemon auth token; encrypted config files; redacted audit logs.

Planning runs on the VPS by default (see §7.1 for the architectural rationale), so planning API keys live on the VPS in the daemon's encrypted config alongside the other credentials it manages. The desktop-only API mode is an opt-in privacy escape hatch; when active, the desktop holds the keys via Electron `safeStorage` for the duration of a planning job and never persists them in plaintext.

### 5.2 Auth model: pairing → bearer → WS-token

Adopt the three-token shape from t3code, adapted for Hoopoe's SSH-tunnel transport:

1. **Pairing token** (12-char Crockford alphabet, no `0/I/O` confusables) — issued by the daemon at first start (and re-issuable via `hoopoe auth pairing create`). Persisted in the daemon's append-only event log + seeded as a single-use in-memory grant so first launch works before the desktop has any credentials. Single-use; consumed atomically at `/v1/auth/bootstrap/bearer`.
2. **Bearer session token** (HMAC-signed `base64url(claims).base64url(sig)`, 30-day TTL) — minted by consuming the pairing token over the SSH tunnel. Persisted on desktop via Electron `safeStorage.encryptString`. Used for all HTTP calls.
3. **WS token** (5-min, stateless HMAC over the daemon's signing secret, with `sid` claim that's looked up against the bearer table) — issued just-in-time from `/v1/auth/ws-token` immediately before a WebSocket connect, passed as `?wsToken=...`. Used for WebSocket only.

Roles: `owner` (mints/revokes pairing tokens, manages sessions) vs. `client` (read/write data only). Single signing secret on the daemon; rotating it revokes everything.

CLI surface on the daemon: `hoopoe auth {pairing,session} {create,list,revoke}` — same shape as t3code's `t3 auth` for operator familiarity.

**Bootstrap auth flow.** During VPS onboarding, `bootstrap.sh` starts the daemon, which prints its initial pairing token to stdout. The desktop captures it through the SSH session (the SSH channel is already authenticated) and immediately exchanges it over the tunnel for a 30-day bearer. No QR code, no out-of-band transfer — the SSH session *is* the trusted bootstrap channel.

**For local demo mode** (when daemon and desktop run on the same Mac for development), use t3code's **FD-3 envelope pattern**: the desktop spawns the daemon with `stdio: ['ignore', 'pipe', 'pipe', 'pipe']`, writes a per-launch JSON envelope `{port, token, ...}` to FD 3, and closes it. Secrets never appear in `ps`, env, or argv.

**Steady state.** SSH tunnel + bearer (HTTP) + WS-token (WebSocket) + reconnect cursor on the event stream. mTLS-direct mode is optional for advanced/team scenarios; SSH tunneling is the v1 default.

### 5.3 Command safety

**All project-level command execution happens on the VPS, via the daemon.** The desktop never invokes project-level `git`, `br`, `bv`, `ntm`, `ssh` (other than for the tunnel itself), or any project-level shell command directly. Every action that touches project state — staging a hunk, creating a commit, pushing a branch, running a test, starting a swarm, claiming a bead, sending Agent Mail — goes through a typed daemon RPC that executes the underlying tool on the VPS where the repo, agents, and toolchain actually live. This keeps the source-of-truth boundary clean (§1.1), keeps audit logging in one place (§10), and means the desktop has no installed-toolchain dependency beyond Electron itself.

**One narrow exception: the desktop's local sync-driven code mirror (§1.7, §7.7).** The desktop runs `git fetch` (and read-only plumbing like `git show` / `git log` / `git rev-parse` / `git ls-tree`) against its own clone at `~/Library/Application Support/Hoopoe/projects/<project-id>/repo/`. This clone is fetched from **origin**, never from the VPS, and is never a write target through Hoopoe. These reads are not "project-level command execution" in the sense above — they manipulate a local read-only mirror, not the canonical VPS Git repo, and they produce no audit-worthy state changes. Implementation can use `simple-git`, `nodegit`, or `isomorphic-git`; the choice is not load-bearing as long as it's read-only.

The daemon never exposes arbitrary shell execution as a normal API. It executes typed commands through a policy layer: command path must be inside a registered project or approved tool path; destructive Git/filesystem operations require explicit approval; `sudo` requires setup mode or explicit approval; build/test commands go through the build queue; secrets are redacted before storage and streaming; every command writes an audit event.

`**DCG` is the agent-side guard; the daemon ingests its verdicts.** `DCG` (Destructive Command Guard, installed by ACFS as a Claude Code hook) intercepts dangerous git/fs commands at the agent boundary before they reach the shell. Hoopoe does **not** run a parallel guard; instead, the daemon ingests DCG outcomes (`blocked`, `requires_confirmation`, `allowed`) as approval-source events and merges them into the same approvals queue used for Hoopoe-policy approvals. The user sees one unified approvals UI showing both Hoopoe-policy and DCG-policy gates, each with its source rule attached. This avoids the worst failure mode (two guards with subtly different rules silently disagreeing) and gives DCG-blocked actions the same audit, approval-scope, and expiry treatment as everything else.

Human approval is required before destructive operations (deleting projects, force-pushing, hard resets over active swarm work, killing swarms, exposing daemon ports, raising budget caps, importing provider credentials, running unrecognized custom scripts). The exact list is curated per feature in `docs/security.md`.

Approval requests are durable entities, not transient dialogs. Each approval records:

- requested action and typed command spec;
- actor requesting it: user, tending job, agent, repair action;
- reason and linked evidence;
- affected project/bead/branch/files;
- exact command preview or API operation;
- policy rule that requires approval;
- allowed scope: once, this bead, this swarm, this project session;
- expiry time;
- risk class;
- approval/denial actor and note.

Approvals can be viewed from the Activity panel and Diagnostics. Denials are also audited and may create a follow-up finding or blocker bead.

### 5.4 Renderer, cache, and transport hardening

- Electron renderer runs with `contextIsolation: true`, `sandbox: true` where compatible, `nodeIntegration: false`, strict CSP, and no direct access to SSH, Keychain, filesystem, or daemon tokens.
- The preload exposes a small typed API only. All privileged operations go through main-process IPC handlers that validate schemas and user intent.
- Bearer tokens and SSH passphrases live in Keychain/safeStorage; they are never written to the desktop SQLite cache.
- Daemon logs and audit entries pass through a redaction layer before persistence and before streaming to clients.
- TLS/mTLS fingerprints in direct or tailnet mode use TOFU only when the fingerprint arrived over an authenticated SSH bootstrap channel.
- The daemon never binds publicly unless a config flag and a runtime confirmation both exist; the Diagnostics page shows a red warning for any non-local/tailnet bind.
- Local code clone paths are treated as untrusted file contents. Rendering code uses escaping; Markdown preview disables unsafe HTML by default.

### 5.5 Privacy and model-context policy

Each project has a model-context policy:

```text
.hoopoe/model-context-policy.json
```

It controls:

- whether raw source files may be sent to external model APIs;
- whether only summaries/indexes may be sent;
- excluded paths and glob patterns;
- maximum context size per job;
- whether logs may be included;
- whether secrets/scanned sensitive strings block the job or are redacted;
- provider allowlist;
- per-stage defaults for Planning, Beads, Swarm, Tending, and Review.

Before any model call, the daemon records a context manifest: source files/artifacts included, redactions applied, provider/model/account, and policy rule that allowed the call. The UI shows this manifest beside plan/review/tending artifacts.

---

## 6. First install and VPS onboarding

### 6.1 First-run wizard (Stage 0 — Connect)

Steps:

1. Explain that Hoopoe controls a user-owned VPS.
2. Choose path: connect existing VPS, provision new, or local demo. Local demo may run in either real local-daemon mode or mock/replay mode.
3. Configure SSH (generate or import key, paste host/user, verify fingerprint).
4. Run preflight (OS version, CPU/RAM/disk, network, base tools, permissions).
5. Install or verify ACFS.
6. Install or update Hoopoe daemon.
7. Start daemon as systemd service.
8. Establish tunnel.
9. Run tool inventory.
10. Configure optional credentials (planning keys, CAAM accounts, GitHub auth, `[jsm` subscription](https://jeffreys-skills.md/dashboard) for premium-skill installs with SHA-256 versioning + cross-device sync — `jfp` is used as the free fallback when `jsm` is not configured).
11. Show "VPS Ready."

### 6.2 Existing VPS first, provider automation second

The MVP supports **existing VPS** first because it is easiest to make reliable and fastest to debug. Provider automation is designed from day one but ships after the tunnel/daemon/tooling path works. Recommended rollout: existing VPS → one provider plugin (the team's preferred provider) → additional providers → one-click teardown and cost inventory.

Provider plugin contract (in `packages/schemas/`): `listRegions`, `listSizes`, `createInstance`, `destroyInstance`, `estimateMonthlyCost`.

### 6.3 Bootstrap flow

```text
1. verify OS and basic dependencies
2. install missing base packages
3. verify bootstrap source pins, release checksums, and expected installer URLs
4. install or verify ACFS
5. run ACFS doctor/inventory
6. record exact ACFS/tool versions, source URLs, commits, and checksums
7. install Hoopoe daemon binary
8. create daemon config and signing secret
9. install systemd unit (Type=notify) and start daemon as the least-privileged Hoopoe service user compatible with the project paths
10. daemon emits initial pairing token to stdout (captured by SSH)
11. desktop opens SSH tunnel, exchanges pairing token → 30d bearer
12. bearer persisted in macOS Keychain via safeStorage
13. version handshake; print machine-readable result JSON
```

The wizard streams logs and shows structured checkpoint cards. Failures resume from checkpoints rather than starting from scratch.

### 6.4 Structured ACFS bootstrap parsing

ACFS remains the canonical installer. Hoopoe wraps it with a phase parser but must not depend on brittle cosmetics.

The bootstrap runner records every line to `~/.hoopoe/logs/bootstrap-{runId}.log` and emits structured events when it sees stable markers:

```text
phase.start       {phase, name}
phase.line        {phase, stream, offset, text}
phase.checkpoint  {phase, key, status}
phase.end         {phase, rc, durationMs}
phase.fail        {phase, rc, lastLines, resumeHint}
```

If markers change or parsing confidence drops, the UI falls back to raw-log mode while still preserving the run, exit code, and resume action. Phase 0 must capture fixtures for a clean install, a partially complete install, a failed dependency install, and an already-installed ACFS run.

### 6.5 Daemon systemd unit hardening

The daemon service should be easy to debug but not sloppy. The exact unit may change, but the hardening target is:

```ini
[Service]
Type=notify
Restart=on-failure
WatchdogSec=30
KillMode=mixed
TimeoutStopSec=20
LimitNOFILE=65536
ProtectSystem=strict
ReadWritePaths=%h/.hoopoe /data/projects /tmp
NoNewPrivileges=true
PrivateTmp=true
```

`ProtectSystem=strict` may need relaxation during early ACFS setup; setup mode and steady-state mode should use different units or drop-ins. The steady-state daemon should not run as root. Privileged setup/repair actions go through a separate, explicit setup helper or temporary elevated command path with approval and audit. The Diagnostics screen shows the active unit, last restart reason, and watchdog status.

---

## 7. The four stages and the activity surface — strategic intent

UI specs, component inventories, and detailed view layouts live in `packages/design-system/`. This section captures only the strategic intent and the load-bearing decisions per stage. §7.1–§7.4 are the four numbered stages the user navigates between; §7.5 is the Activity panel, a cross-stage UI surface available from any stage.

### 7.1 Planning

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

**Where planning runs.** Planning runs on the VPS daemon as jobs. This is not a configurable default for performance — it is a load-bearing architectural decision that follows from §1.2 (the desktop is not the orchestrator of record) and §1.5 (everything must be restartable from artifacts). The pipeline above is a long-running, multi-stage, multi-model job graph that takes minutes to hours and produces durable artifacts the rest of the flywheel consumes. Putting that on the desktop creates the same class of problems we banned for swarms: a closed laptop kills the run, artifacts arrive over a flaky tunnel instead of landing next to the repo, audit events fragment across two writers, and the server-side CLI mode (which depends on Claude Code / Codex CLI / Gemini CLI / OpenCode installed by ACFS and accounts managed by CAAM on the VPS) becomes impossible. Planning is structurally the same shape as a swarm job — it belongs in the same place.

The desktop owns the *editing* surface (CodeMirror, artifact rail, quality tracker UI, diff/version views, lock action) and subscribes to the daemon's job event stream. It does not own the pipeline.

Three execution modes, in order of preference:

1. **Server-side CLI mode** (preferred when available) — daemon shells out to the configured Claude Code / Codex CLI / Gemini CLI / OpenCode. Lets users on Claude Max / GPT Pro / Gemini Ultra subscriptions plan without burning API credits. OpenCode is the catch-all for users without a subscription on a specific provider, since it can drive whichever model the user does have credentials for. Requires ACFS + CAAM.
2. **Server-side API mode** — daemon calls model APIs directly using BYOK keys stored in the daemon's encrypted config. Use when CLIs are unavailable or the user prefers per-call cost control.
3. **Desktop-only API mode** (escape hatch) — Electron calls model APIs from the user's laptop and streams artifacts back over the tunnel for the daemon to write. Exists only for users who refuse to put model keys on a VPS, even encrypted. The UI must surface the tradeoffs explicitly: no restartability (closing the lid kills the run), no CLI mode, audit events arrive only when the desktop is online, and the planning job cannot be shared if Hoopoe later supports multi-user teams. This mode is opt-in, never the default.

The UI must make credential location explicit on every plan job: which mode, which model, which account, where the keys live.

**Quality dimensions** (deterministic + model-based, scored as guidance, not truth): intent clarity, architecture specificity, workflow coverage, implementation detail, testing specificity, risk coverage, bead readiness.

**Artifact layout.** Planning artifacts are stored inside the project so Git, audit, and later bead conversion can find them:

```text
.hoopoe/plans/<plan-id>/
  plan.md
  meta.json
  drafts/
    claude.md
    codex-or-openai.md
    gemini.md
    other.md
  comparative-matrix.md
  synthesis.md
  fresh-eyes-critique.md
  refinement-round-001.md
  refinement-round-002.md
  unresolved-decisions.md
  history.jsonl
```

`meta.json` records schema version, source mode, project commit SHA at planning time, model/provider/account used, input hash, round IDs, artifact paths, linked bead IDs, lock state, and cost summary. `history.jsonl` records every meaningful action and is mirrored into the daemon audit log.

**Plan round cost and cache discipline.** Each model call records provider, model, request ID when available, input/output token estimates, cost estimate, latency, temperature/reasoning settings, input hash, and artifact hash. Server-side API mode uses a content-addressed cache under `~/.hoopoe/cache/llm/{sha256}.json` with a default 14-day TTL. Defaults: per-round soft cap, per-project monthly cap, and a preflight estimate when providers expose enough pricing/token data. If estimates are unavailable, the UI says so instead of inventing precision.

**Locking.** Writes final `plan.md`, creates a snapshot hash, requires unresolved decisions to be accepted or resolved, marks metadata `locked`, enables "Convert to beads." Amendments to a locked plan create a new version and can trigger bead delta analysis.

**Open architectural decision: own the pipeline vs. delegate to `apr`.** The `apr` tool (Automated iterative spec refinement, installed by ACFS) already implements automated iterative refinement with extended AI reasoning — overlapping substantially with the candidates → synthesis → fresh-eyes critique → refinement-rounds pipeline above. Two viable shapes:

- **Own it (current plan):** Hoopoe's planning job graph implements every step itself. Maximum UI control over per-step prompts, artifact layout, quality dimensions, and per-round cost ledgering. Cost: more code to maintain; we duplicate `apr`'s methodology evolution.
- **Delegate to `apr`:** Hoopoe orchestrates `apr` runs from the Plan workspace, captures its artifacts into `.hoopoe/plans/<plan-id>/`, and presents them in the same UI. Cost: less control over the inner pipeline; we inherit `apr`'s schedule and prompts.

Default is "own it" until Phase 5 prototyping shows whether `apr`'s output quality and artifact shape can carry the Plan workspace as-is. Either way, `apr` is listed in the §1.1 source-of-truth table as a candidate so the decision is explicit, not silent.

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

**Implementation evidence traceability.** After swarm launch, traceability extends beyond bead creation:

```text
plan section
  → bead
  → branch/worktree
  → commits
  → files touched
  → tests/builds run
  → health deltas
  → review findings
  → landing queue item / PR / merge commit
```

This produces `implementation-evidence.jsonl` under the plan/bead artifact store. The UI can answer:

- which plan sections are implemented but untested;
- which sections have code but no review;
- which beads closed without a landing queue item;
- which review findings map back to which plan risks;
- what evidence supports the final ship gate.

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

### 7.3 Swarm

**Purpose.** Mission control. Launches agents through NTM, shows live status, exposes logs/panes, tracks costs and rate limits, lets the user intervene without dropping into raw tmux.

**Methodology source.** Swarm launch, marching-orders dispatch, pane recovery, build/test contention handling, review-mode flips, and convergence detection all follow the `ntm` and `vibing-with-ntm` skills (jeffreys-skills.md). NTM itself is the orchestration layer Hoopoe wraps; the `vibing-with-ntm` skill is the playbook for *how* to use it well. **These skills are loaded directly into the tending agents at runtime (§8) — they are not reimplemented in Go.** This UI is the visual surface of those skills; the tending scheduler (§8) is the execution surface. When behavior is ambiguous, the skills are the spec. See §8 for the tending side and §17 for the reference links.

**Default launch policy.** Stagger starts by ≥30 seconds; force `AGENTS.md` and README reread; require Agent Mail registration; require `bv --robot-triage` and `br ready --json` before claiming work; mark claimed beads `in_progress`; reserve files before edits; include bead ID in mail subjects, reservation reasons, and commit messages; use `rch` for builds/tests when configured; never invoke bare `bv`; avoid concurrent builds for same project; self-review with fresh eyes before review/close; report blockers quickly; do not wait in communication purgatory.

**Safety presets.** Presets are just approval-policy bundles, not different code paths: `supervised` (approval for writes/destructive actions), `guided` (approval for destructive/unrecognized actions), and `autopilot` (typed allowed actions proceed, destructive/unrecognized still gated). The default is `guided`. The user can inspect exactly which actions each preset gates before launch. **Optional SLB add-on:** any preset can be configured to require `SLB` (Simultaneous Launch Button) two-person co-signature for the destructive-action class. When enabled, Hoopoe's approvals UI displays the SLB state and never bypasses it; especially relevant when running `autopilot` against a shared/team VPS.

**Push policy (canonical-state freshness).** Origin is the canonical source of truth for code (§1.1); the VPS working tree is just where commits are *created* before they become canonical. So the swarm must push frequently, not at the end of a session. Default policy: agents push their bead's branch after every commit (or at minimum after every successful test run). The daemon enforces this via a post-commit auto-push hook configured at swarm-launch time, with audit events on every push attempt. Push failures (network, auth, conflict) surface immediately in the Activity panel as urgent items. Rationale: any commit that lives only on the VPS for more than a few minutes is a durability risk and a visibility gap — the desktop's local clone, CI, code review, and the human user can only see what's reached origin.

**Launch sequence.** Reconcile project state → verify launch gates → show warnings (dirty Git, stale reservations, no ready beads, low disk, missing Agent Mail, **N unpushed commits on VPS**) → create swarm spec and audit event → call NTM spawn/add → stagger agent starts → send kickoff prompt → start event subscriptions → activate the project's tending jobs (§8) → show swarm dashboard.

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
- Push every commit to origin promptly — your bead's branch should never sit unpushed on the VPS for more than a few minutes. Origin is the canonical source of truth; unpushed work is invisible to the user, CI, and other tools.
- Report blockers quickly.
- Do not get stuck in communication purgatory; if unblocked, choose useful ready work.
- When finished, run tests appropriate to the bead, self-review with fresh eyes, then move to review or close according to project policy.
```

**PTY and pane streaming strategy.** Terminal fidelity is a major credibility risk.

- Preferred: NTM WebSocket/robot-tail surfaces; subscribe to structured pane output and status events; render in xterm.js.
- Fallback 1: daemon attaches `tmux pipe-pane` to stream pane output into byte-addressable logs with per-pane ring buffers and reconnect offsets.
- Fallback 2: daemon uses tmux `capture-pane` polling only when pipe streaming is unavailable; per-pane 50–100KB ring buffer, initial ring on attach, low-frequency diff streaming, cursor-based reconnect.
- Do NOT make one SSH connection per pane. Do NOT poll every pane aggressively from Electron. Do NOT parse terminal output as the source of truth when NTM, Agent Mail, `br`, or `bv` already know the state. Terminal content is observability and manual-intervention surface, not canonical state.

**Cost and rate-limit guardrails.** Per-agent and per-swarm budget caps with alert/hard-stop thresholds; cost telemetry primarily from `caut` (per-provider usage tracker) with `rano` (network observer for AI CLIs) supplying per-call latency/error signals; rate-limit detection via CLI status messages, NTM events, long-no-output heuristics, and CAAM if configured. Show estimates as estimates — avoid false precision; if `caut` is unavailable for a provider, the UI labels the cost cell as "unmeasured" rather than displaying a fabricated number.

**Recovery actions when an agent rate-limits or wedges.** Detection is one thing; what Hoopoe lets the user (or the `tend-swarm` job, §8.4) actually *do* is a defined set:

- **Switch account via `CAAM`** — rotate the agent to a different provider account that hasn't hit its limit. Audit entry records source/target accounts and the limit signal that triggered the switch.
- **Resume the session via `casr`** — convert the in-flight session to a different CLI/provider when account-switching alone isn't enough (e.g., the user's Claude Max is exhausted but they have GPT Pro). Preserves context rather than restarting the bead from scratch.
- **Pause and notify** — leave the agent alive but stop assigning it new work; surface in the Activity panel for human decision.
- **Kill and reassign** — terminate the agent (using `pt` for genuinely wedged processes), force-release its bead claim with audit, and let the next `tend-swarm` tick reassign.
- **Send marching orders** — for soft drift (not stuck, just off-track), broadcast a corrective prompt instead of intervening at the process level.

These are exposed as buttons in the Activity panel (with the appropriate approval gates), as fields in the agent-tile context menu, and as actions the `tend-swarm` agent can take per the `vibing-with-ntm` skill.

**Not adopted: `wa` (WezTerm Automata).** ACFS installs `wa` as an alternative terminal-orchestration path, but Hoopoe deliberately commits to `tmux`/`NTM` for PTY (see "PTY and pane streaming strategy" above). Mixing two orchestration substrates would split the swarm between two state machines with no shared session model. If a future need surfaces (e.g., a class of agents that only run well under WezTerm), it should be a deliberate, planned addition — not an accidental parallel path.

### 7.4 Debugging / Hardening

**Purpose.** Once implementation beads are mostly closed, the work shifts from "writing code" to "making the code correct, safe, and shippable." This stage turns the agent fleet into a debug/review/harden engine that finds bugs, missing tests, hot spots, security issues, and architectural drift, and converts every finding into either an immediate fix or a new bead.

**Three components, one stage.** Code health metrics surface *what* needs hardening; review rounds *do* the hardening; the finding tracker *records the outcome*.

#### 7.4.1 Code health metrics — visible everywhere, not just here

Code health is a first-class continuous signal, not a once-per-release report. The user must be able to answer "what is our coverage / complexity / hotspot situation right now?" at any moment and from any stage — see §7.6 for the always-visible top-bar health pill that makes this true across the cockpit.

**Always-on metrics.** Coverage % (line and branch where supported); cyclomatic complexity (per-file and per-function); LOC and effective LOC; churn (last 7 / 30 days); hotspot count; test pass/fail counts and durations; ratio of files lacking any test coverage; complexity-to-coverage delta (how much risk is uncovered). Every metric is timestamped and snapshotted.

**Adapters per ecosystem.**

- TS/JS — vitest/jest + lcov + lizard/ts-complexity.
- Python — pytest + coverage.py + radon/lizard.
- Rust — cargo test + cargo llvm-cov + lizard.
- Go — go test -cover + gocyclo/lizard.
- Generic — configurable shell + lizard/scc/tokei/cloc.

**Hotspot scoring.** Weighted sum of high complexity, low coverage, high churn, recent agent changes, failed tests nearby, review findings nearby, and critical-path bead linkage. Default thresholds: complexity ≥ 20, coverage < 60%. Configurable per project.

**Where the metrics show up.**

- **Top bar (every stage)** — a compact health pill: average coverage %, average complexity, hotspot count, last-snapshot age. Click → opens the Debugging / Hardening stage's Health tab. Color-coded against project thresholds. (§7.6)
- **Debugging / Hardening — Health tab** — full surface: KPI cards (written files, avg coverage, avg complexity, hotspot count, uncovered-files ratio, recent test failures), sortable file table (path, LOC, complexity, coverage bar, churn, owner agent, linked bead, hotspot reasons), trend sparklines per metric, and a quick action to create a bead from any hotspot.
- **Beads stage — bead detail drawer** — per-bead "files touched" section shows current coverage and complexity for each file the bead is expected to modify, plus delta vs. last snapshot once work begins.
- **Swarm stage — agent tile** — per-agent code health delta since the agent started its current bead (did they raise or lower coverage? add complexity?).
- **Activity panel** — `health snapshot updated` events with summary of changes (e.g. "coverage 64% → 67%, 2 new hotspots, 1 hotspot resolved").

**Snapshot cadence.** On every push to main, after each swarm round, on demand from the Health tab, and from the `snapshot-health` tending job (§8.4) which fires on `vps_push_completed` events and at most once every 10 minutes (cached results reused if no relevant files changed).

**Worktree isolation.** Health jobs run in a dedicated Git worktree under `~/.hoopoe/work/<project-id>/health/<run-id>/` at the commit being measured. They do not run in the active VPS working tree used by agents. If worktree creation is blocked by Git state, Hoopoe reports the reason and schedules a retry rather than running coverage in-place and risking collisions with active edits.

#### 7.4.2 Review rounds

The *process* of inspecting the code (self-review, cross-agent, fresh-eyes, hotspot-targeted, specialized audits). Hotspots from §7.4.1 feed directly into review prompts so the rounds attack the highest-risk code first. Standard tooling: `UBS` (Ultimate Bug Scanner) is the default first-pass scanner — fast, deterministic, and ACFS-installed — invoked by review-round runners and feeding directly into the §9.3 finding ledger with `source: ubs` stamped on each finding. Specialized skills (mock-code finder, deadlock finder, security audit, performance profiling, etc., per §9.5) layer on top for targeted rounds. Full operational detail in §9.

#### 7.4.3 Finding tracker

The *outcome* ledger that turns every review observation into one of: fixed immediately, converted to a bead, attached as blocker to an existing bead, rejected as false positive with note, or escalated to human. Every finding records its source tool (`ubs`, `vibing-with-ntm` agent, specialized-audit skill, human reviewer) so cross-tool deduping and source-quality analysis are possible. Findings link to the file health metrics so closing a finding can be cross-checked against the coverage/complexity it was supposed to improve. Detail in §9.3.

**Convergence is the success criterion**, not "every bug is fixed." A round is "saturated" when new useful findings are low relative to cost and effort, and remaining findings are mostly duplicates, low severity, or already tracked as beads. The convergence detector (§9.4) tells the user when to ship vs. when to do another round.

**Stage views.** Health tab (per §7.4.1); Review tab (active review round, prompts, agents involved, live findings); Findings tab (full finding lifecycle ledger with bead links and health cross-references); Convergence tab (round-over-round trends + ship-readiness gate).

### 7.5 Activity panel — cross-stage messaging surface

**Not a stage.** Activity is a persistent UI panel — typically a side drawer or slide-over — available from any of the four stages. It does not have its own route; it overlays the current stage.

**Purpose.** Activity is the coordination ledger. It combines Agent Mail, NTM events, bead updates, file reservations, build/test events, orchestrator interventions, and the user-to-orchestrator chat into one readable, filterable timeline. It is the answer to "what just happened?" and "what is the orchestrator doing right now?" without the user having to leave the stage they're working in.

**Two primary message types.**

- **Agent ↔ agent** — Agent Mail traffic, file reservation requests/releases, bead claim notifications, blocker reports.
- **User ↔ orchestrator agent** — the human overseer's chat with the orchestrator: asking it what it's doing, telling it to pause/resume, broadcasting marching orders, approving destructive actions, asking it to investigate a specific bead or hotspot. The "orchestrator agent" is concretely the `orchestrator-chat` tending job (§8.4) — a real agent runtime with the `vibing-with-ntm` and `ntm` skills loaded, triggered by `user_message_in_activity_panel` events. It shares the same daemon RPC surface and approval gates as the scheduled tending jobs, so a user request like "force-release this stale reservation" goes through the same audit and safety machinery as if a `tend-swarm` tick had decided to do it.

**Event categories.** agent registered; mail sent/received/urgent; bead claimed/status changed; file reserved/renewed/released/conflicted; build/test started/completed/failed; rate limit detected; pane wedged; orchestrator intervention; review request/finding; commit created; health snapshot updated; user→orchestrator message; orchestrator→user message.

**Interactions.** Click a bead pill → bead detail. Click an agent chip → swarm tile. Click a file path → reservation view. Reply as human overseer; broadcast to swarm; create bead from message; mark acknowledged. The user-to-orchestrator chat is a first-class input box at the bottom of the panel.

**File reservations are advisory, not hard locks** — Hoopoe surfaces stale reservations and conflict warnings without pretending the GUI can prevent every file edit.

### 7.6 Cockpit chrome — always-visible top bar

The cockpit's top bar is persistent across all four stages and the Activity panel. It is not a stage; it is the always-on dashboard that answers "what is the project's current state?" at a glance without requiring the user to navigate.

**What it shows, left to right.**

- **Project / repo / branch** — clickable to switch project; shows clean/dirty Git state; warns on detached HEAD or unpushed commits.
- **Tool health** — green/yellow/red dots for VPS daemon, NTM, Agent Mail, `br`/`bv` versions; click → diagnostics screen.
- **Swarm state** — count of running agents, idle agents, wedged/rate-limited agents; click → Swarm stage.
- **Beads pulse** — ready / in-progress / blocked counts; click → Beads stage.
- **Code health pill** — avg coverage %, avg complexity, hotspot count, last-snapshot age. Color-coded against project thresholds (green/yellow/red). Click → Debugging / Hardening Health tab. This is the global surface for §7.4.1; the user must never have to dig to know whether code health is improving or degrading.
- **Cost / rate-limit indicator** — current spend vs. budget cap, any active rate limits. Spend numbers come from `caut` (per-provider usage tracker); rate-limit signals come from `caut`, `CAAM`, CLI status messages, NTM events, and `rano`-observed responses. If `caut` isn't reporting for an active provider the cell shows "unmeasured" rather than zero.
- **Activity panel toggle** — opens the cross-stage Activity drawer (§7.5); badged with unread count.

**Update cadence.** Top-bar values come from the daemon's reconciled cache (§2.2) and are pushed over the WebSocket event stream — no polling from the renderer. Health pill updates within seconds of a new snapshot landing.

### 7.7 Local code clone — desktop sync-driven mirror for code reads

The desktop maintains a local Git clone of every project, used purely as a sync-driven mirror to make code-reading interactions in the cockpit instant. This is an instance of the sync-driven mirror principle (§1.7) and the one narrow exception to "no project-level shell from the desktop" (§5.3).

**Why it exists.** Reading code is the single most frequent thing the cockpit does. The Beads drawer wants to show files a bead touches, the Health tab wants to render a file with its coverage gutter, hotspots want previews, review findings link to specific lines, and search ("where is `X` referenced?") is expected to be instant. Doing all of that over the SSH tunnel via daemon RPCs is workable but feels sluggish and fails when the laptop is on a poor connection. A local clone makes file open, diff render, blame, and ripgrep local-disk fast.

**What it shows vs. what daemon RPCs show.** The local clone reflects **origin** — the canonical, pushed history. Anything on the VPS that hasn't been pushed yet (committed but not pushed, staged but not committed, working-tree edits in flight) is *not* in the local clone and shouldn't be. Those WIP reads come from daemon RPCs that inspect the VPS working tree directly: `getWorkingTreeStatus`, `getStagedDiff`, `getUnstagedDiff`, `getUnpushedCommits`. The cockpit composes both: file history and current pushed contents from the local clone, plus a small "VPS WIP" overlay (e.g., "Agent X has 3 unpushed commits, 2 modified files") layered on top from the daemon. This split is what lets the local clone stay a strict origin mirror without losing the live-agent visibility a cockpit needs.

**What it is.**

- **Storage path:** `~/Library/Application Support/Hoopoe/projects/<project-id>/repo/`. Project metadata (last-fetched SHA, sync state, size) lives in `~/Library/Application Support/Hoopoe/projects/<project-id>/clone-state.json`.
- **Clone source:** **origin** (the same GitHub/GitLab/etc. remote the VPS pushes to), not the VPS. This avoids exposing extra surface on the VPS, leverages the user's existing Git credentials (`~/.ssh/`, GitHub CLI auth, etc.), and means the desktop and VPS reach the same canonical state through independent paths. Projects without an external remote are not supported in v1; the user must add an origin remote before importing.
- **Refs fetched:** all of them. `git fetch --all --tags --prune` on every sync. The default checked-out branch matches whatever the VPS working tree is on, but the UI can render content from any ref via `git show <ref>:path` without checking it out.
- **Initial clone:** full clone (not partial) for v1 simplicity. We can revisit `--filter=blob:none` later if monorepos make full clones painful.

**Sync model.**

- **Event-driven (primary):** the daemon watches origin (via the VPS clone's tracking refs after each `git fetch origin` or `git push`) and emits `origin_updated` events on the WebSocket stream whenever origin's refs advance — typically right after the VPS pushes, but also after pulls if other contributors push. Payload includes the affected refs and new SHAs. The desktop fetches in response, typically within 1–2 seconds of the event. Each fetch is logged with the triggering event ID for auditability.
- **Companion VPS-only events:** the daemon *also* emits `vps_commit_created` (agent committed but hasn't pushed yet) and `vps_push_completed` (the VPS just pushed N commits to origin). The desktop uses these for the "N unpushed commits" status indicator, but does **not** fetch from origin on `vps_commit_created` — there's nothing in origin to fetch yet. It fetches on `origin_updated` (which `vps_push_completed` will also trigger downstream).
- **Safety-net poll (secondary):** every 60 seconds the desktop runs a background `git fetch` per active project to catch anything the event stream missed (network drop, daemon restart, third-party push directly to origin, etc.). Configurable; disable in settings to save bandwidth on metered connections.
- **On-demand:** "Refresh" action in the project header forces a fetch immediately.
- **Reconcile on reconnect:** when the WebSocket reconnects after a gap (§10), the desktop fetches once and reconciles its sync cursor.

**What if the user edits files locally anyway.** Hoopoe does not chmod the working tree. Instead, a file watcher on the local clone detects modifications, untracked files, or local commits. When detected, the project header shows a yellow banner: *"Local clone has unsaved changes — Hoopoe ignores local edits. Make changes via the VPS (`ssh` or Cursor/VS Code Remote). [Discard local changes] [Open clone in Finder]"*. The "Discard" action runs `git reset --hard @{u} && git clean -fd` after a confirmation dialog. Hoopoe never auto-discards.

**Disk hygiene.**

- **Soft cap:** 2 GB per project clone. Crossing it shows a warning in the project's settings card with the option to clear blobs older than N days.
- **Hard cap:** 5 GB per project clone. Crossing it blocks further fetches until the user either clears the clone or raises the cap in settings.
- **Both caps configurable** in `~/.hoopoe/userdata/desktop-settings.json` per-project and globally.
- **Per-project actions** in settings: "Clear local clone" (deletes the directory; next access re-clones), "Reveal in Finder," "Open in terminal."
- **Project removal:** deleting a project from Hoopoe deletes its local clone. Confirmation dialog spells this out.
- **Total cache view** in global settings: list of clones with size, last-fetched, last-accessed; sortable; multi-select clear.

**Authentication.** The desktop uses the user's existing Git credentials — typically SSH keys in `~/.ssh/` already used to talk to the VPS, or GitHub/GitLab personal access tokens already in the system credential helper. Hoopoe does not introduce its own Git auth flow. If a clone fails because credentials aren't set up, the project header shows a clear error with a link to docs.

**What it powers across the cockpit.**

- **Beads drawer "files touched"** — file previews, current contents, diffs.
- **Debugging / Hardening Health tab** — file content with coverage gutter overlaid.
- **Hotspot previews and review-finding line anchors** — render the actual code in context.
- **"Open in editor" links** — open any file in the user's default editor (Cursor, VS Code, etc.) directly from a Hoopoe link. Works because the file is on the local disk.
- **Local search** — ripgrep over the local clone is the default search backend; the daemon's grep RPC is a fallback for uncommitted-on-VPS work.

**What it does NOT do.** It is never offered as a target for staging, committing, branching, merging, or pushing. The Hoopoe UI never exposes those actions against the local clone. All such operations go through the daemon as described in §5.3.

## 8. Tending: scheduled jobs + skills, not a bespoke loop

### 8.1 Approach

Tending the swarm — detecting idle/wedged/rate-limited agents, recovering stalled beads, pushing stale commits, deciding when to flip into review mode, surfacing strategic drift, watching safety thresholds — is implemented as **a scheduler running skill-attached jobs**, per principle §1.8. There is no bespoke "operator loop" written in Go. The behavioral spec for tending lives in the `vibing-with-ntm` and `ntm` skills (loaded into agent context when a job wakes); the daemon's job is to run a scheduler, run pre-scripts, dispatch agents with the right skills, gate destructive actions through the approval system, and write audit events.

This pattern is inspired by Nous Research's Hermes Agent, specifically the cron + skills + pre-script architecture: scheduled jobs, attached skills, delivery targets, and fresh agent sessions. Hoopoe adopts the shape, not Hermes's scheduler implementation or reliability assumptions.

Hoopoe's scheduler must be independently correct under long-running jobs, daemon restarts, clock skew, missed ticks, and slow delivery targets. A due job must never be silently skipped because another due job ran long. Scheduler correctness is a product requirement, not a borrowed property from the reference system.

### 8.2 Job format

A tending job is a declarative record stored in `~/.hoopoe/tending/jobs.json` (one file per project, atomic writes). The shape:

```yaml
id:                  unique stable ID
name:                human-readable
kind:                deterministic | gated_agent | orchestrator_chat | external_webhook
schedule:            cron expression | "every Nm/h/d" | "on event: <event-type>" | "on demand"
project_scope:       null (global) | project_id (project-scoped)
enabled_toolsets:    [br, bv, ntm, agent_mail, git_read, health_adapter, ...]
                     # narrow per-job to keep tool-schema prompt bloat low
script:              path to Go pre-script run by the daemon
                     # outputs structured JSON; final line is {"wakeAgent": bool, "context": {...}}
                     # may also perform safe deterministic actions before deciding
skills:              [vibing-with-ntm, ntm, ...]
                     # loaded into agent context when wakeAgent is true
                     # consumed via the agentskills.io standard
prompt:              template string with {{context.*}} interpolation from the pre-script's payload
deliver:             hoopoe_activity_panel | hoopoe_activity_urgent | local | external (Telegram, etc.)
repeat:              forever | N
paused:              bool
timeout:             duration
max_concurrency:     1 by default
misfire_policy:      skip | run_once | catch_up_bounded
retry_policy:        none | fixed | exponential
dead_letter_after:   N failures
audit_always:        bool (default true) — log even when wakeAgent is false or response is [SILENT]
```

Per-job lifecycle commands match Hermes's surface for operator familiarity: `hoopoe tending {list,create,edit,pause,resume,run,remove,status,tick}`. Plus a UI surface in the Diagnostics screen (§10) and the Activity panel.

### 8.3 The wakeAgent and [SILENT] patterns

Two patterns make cost and noise sane:

`**wakeAgent: false**` — the pre-script does the cheap mechanical reconcile (read canonical state, evaluate threshold conditions). If nothing is actionable, it returns `{"wakeAgent": false}` and the LLM never fires. A swarm-tending job ticking every 4 minutes with nothing wrong costs zero tokens. The pre-script may also take *immediate deterministic action* before deciding — e.g., a safety-watch job that detects a budget breach can halt the swarm in Go and skip the agent entirely, since the action requires no judgment.

`**[SILENT]*`* — when the agent does wake but decides on closer inspection that nothing was warranted, it replies with output starting `[SILENT]`. Delivery to the Activity panel is suppressed, but the run is still audited (model used, tokens, decision). This keeps the panel quiet during long stretches of healthy operation.

Together these mean: a healthy hour of swarm operation produces near-zero LLM cost and near-zero panel noise, while a stuck or drifting swarm produces exactly the right amount of agent attention and exactly the right amount of user-visible output.

### 8.3.1 Scheduler correctness invariants

- A recurring job must never be marked `completed` unless its repeat count is exhausted or it is explicitly disabled.
- Missing schedule dependencies, malformed cron expressions, or failed next-run calculation put the job into `error`, not `completed`.
- Each job execution holds a lease. If the daemon crashes, a later daemon may reclaim expired leases and mark the interrupted run.
- Jobs default to `max_concurrency: 1`.
- Every run has a timeout and cancellation path.
- Missed runs after daemon downtime follow the job's `misfire_policy`.
- Repeated failures move the job to a dead-letter state visible in Diagnostics.
- Deterministic jobs do not start an LLM session. They run pre-script logic and emit events only when action is taken or failure occurs.
- Due jobs are selected in one transaction and each due run receives a durable run record before execution begins.
- The tick loop dispatches due jobs through a bounded worker pool; one long run cannot block unrelated due jobs.
- Every due job resolves to one of: started, skipped_by_policy, skipped_by_misfire_policy, paused, leased_elsewhere, dead_lettered, or scheduler_error.
- "Skipped" is an explicit audited state, never the absence of a run record.
- Scheduler metrics include due runs, started runs, skipped runs, misfires, lease steals, dead letters, average tick duration, and longest queued delay.

### 8.4 Initial job set

Hoopoe ships with a small default set of tending jobs. They are user-editable — pause one, change a schedule, swap a skill, add a custom job — but the defaults are designed to cover everything the old §8 operator loop covered.

```yaml
- id: tend-swarm
  schedule: every 4m
  enabled_toolsets: [br, bv, ntm, agent_mail, git_read, caam, casr]
  script: tend-swarm.go    # reconcile NTM/br/bv/Agent Mail/Git/CAAM;
                            # detect idle, wedged, rate-limited, stalled-bead candidates,
                            # duplicate claims, agents not using Agent Mail, agents not updating br;
                            # surface CAAM-reported account exhaustion as a first-class detection;
                            # if none → wakeAgent: false
  skills: [vibing-with-ntm, ntm]
  prompt: |
    The swarm has the following detections this tick: {{context.detections}}.
    Read AGENTS.md and the vibing-with-ntm skill, then decide: send marching
    orders, reassign beads, ask an agent for status, force-release a stale
    reservation (with audit note), switch a rate-limited agent to a different
    account via CAAM, resume an exhausted-account agent under a different
    provider via casr, kill a wedged process via pt, or take any other
    action the skill prescribes. Destructive actions go through the same
    approvals queue as user-initiated ones. If on closer inspection nothing
    is warranted, reply [SILENT].
  deliver: hoopoe_activity_panel

- id: watch-safety-thresholds
  schedule: every 30s
  enabled_toolsets: [budget, ntm, caut, srp, sbh, pt]
  script: safety-watch.go    # checks per-agent and per-swarm budget caps (reading
                              # caut for per-provider spend); hard rate-limit halts;
                              # disk pressure (via srp signals); CPU/load (via srp);
                              # daemon health.
                              # CROSSING A HARD THRESHOLD takes immediate deterministic action:
                              #   - budget breach → halt swarm via NTM
                              #   - disk pressure → invoke sbh-driven cleanup of stale
                              #     artifacts (build/test outputs, old health snapshots,
                              #     terminal-log rings beyond retention)
                              #   - genuinely wedged process (no output + no syscalls
                              #     + over a deterministic threshold) → pt-driven kill
                              #     with audit
                              # Emits an urgent event for any threshold crossing.
                              # Always returns wakeAgent: false (no judgment needed).
  skills: []
  prompt: ""    # never wakes the agent
  deliver: hoopoe_activity_urgent
  audit_always: true

- id: push-stale-commits
  schedule: every 1m
  enabled_toolsets: [git_write]
  script: push-stale-commits.go    # detects unpushed commits older than threshold (default 5 minutes);
                                    # pushes them to origin via the daemon (with audit, after policy check);
                                    # never wakes the agent — push policy is mechanical (§7.3)
  skills: []
  prompt: ""
  deliver: hoopoe_activity_panel    # only when a push happens or fails
  audit_always: true

- id: snapshot-health
  schedule: on event: vps_push_completed | every 10m
  enabled_toolsets: [health_adapter, file_read]
  script: snapshot-health.go    # runs the language-appropriate coverage/complexity tools,
                                 # writes a snapshot artifact, emits health_snapshot_updated event.
                                 # Never wakes the agent — measurement is mechanical.
  skills: []
  prompt: ""
  deliver: hoopoe_activity_panel    # event-style entry only

- id: drift-check
  schedule: every 30m
  enabled_toolsets: [br, bv, git_read, health_adapter]
  script: drift-check.go    # checks "many commits, few beads closed", "P0 critical
                             # path stale", "review findings clustering in same domain",
                             # "code health worsens while beads close", "agents create
                             # many new beads without closing old ones".
                             # If none → wakeAgent: false.
  skills: [vibing-with-ntm]
  prompt: |
    Drift detections: {{context.detections}}.
    If meaningful strategic drift is occurring, summarize what's drifting and
    propose corrective action (slow/stop swarm, run reality-check review,
    revise beads, ask human to approve a plan refinement round). Otherwise
    reply [SILENT].
  deliver: hoopoe_activity_panel

- id: review-readiness-check
  schedule: every 15m
  enabled_toolsets: [br, bv]
  script: review-readiness.go    # checks if implementation beads are mostly closed,
                                  # P0/P1 ready beads handled, no obvious stuck in-progress,
                                  # latest health snapshot available — i.e., §9.1 prerequisites.
                                  # If review-mode threshold not crossed → wakeAgent: false
  skills: [vibing-with-ntm]
  prompt: |
    Implementation appears to be near-complete based on: {{context.signals}}.
    Per the vibing-with-ntm skill, decide whether to propose a flip to
    Debugging / Hardening (§7.4). If yes, post a proposal to the Activity
    panel for the user to confirm. If not yet ready, reply [SILENT].
  deliver: hoopoe_activity_panel

- id: orchestrator-chat
  schedule: on event: user_message_in_activity_panel
  enabled_toolsets: [br, bv, ntm, agent_mail, git_read, health_adapter]
  script: build-chat-context.go    # gathers the user's message, recent activity,
                                    # current swarm state as context for the agent
  skills: [vibing-with-ntm, ntm]
  prompt: |
    The user said: {{context.message}}.
    Current swarm state: {{context.state_summary}}.
    Respond as the orchestrator agent. You can answer questions, run reads,
    take actions via daemon RPCs (with approval gates for destructive ones),
    or broadcast marching orders to the swarm.
  deliver: hoopoe_activity_panel
```

That last job — `orchestrator-chat` — is the concrete realization of "the orchestrator agent the user chats with in the Activity panel" (§7.5). It's the same runtime as scheduled jobs; the only difference is the trigger is a user message instead of a clock.

### 8.5 What the pre-scripts cover (was: §8.3 / §8.4 / §8.5 of the old plan)

The detect/decide content from the old operator loop is preserved — it just lives in pre-scripts and prompts now, not in monolithic loop pseudocode.

- **Stalled bead detection** (was §8.3) → `tend-swarm.go` pre-script. A bead is a stalled candidate when status is `in_progress`, the owner is idle/wedged/stopped/rate-limited (rate-limit signals come from `caut`/`CAAM`/CLI status), no Agent Mail activity for the bead in N minutes, no pane output from the owner in N minutes, no file modification in N minutes, no test/build activity related to it, the reservation has expired, and repeated job ticks produced no progress. Actions by severity (ask owner for status → reread AGENTS.md → ask owner to proceed/help/release → switch account via `CAAM` if rate-limited → resume under another provider via `casr` if account is exhausted → reopen if owner is gone → force-release stale reservations with audit note → kill genuinely wedged process via `pt` with audit → create blocker bead → reassign → alert human) are decided by the agent reading the `vibing-with-ntm` skill, not hardcoded.
- **Build/test contention control** (was §8.4) → daemon's build queue (always-on infrastructure, not a tending job — see §2.1) plus `tend-swarm.go` surfacing contention as a detection for the agent to address. Agents request tests/builds; the daemon queues and dedupes; identical commands reuse recent results when safe, keyed by commit SHA, command, environment digest, and relevant input files; repeated failure fingerprints are surfaced to the tending agent as "known unchanged failure" instead of prompting another blind retry; `rch` is preferred when configured; UI shows queue and currently running jobs; the agent (via the skill) warns swarm members when contention is high; stale-artifact cleanup runs from `watch-safety-thresholds.go` (via `sbh`) under disk pressure detected by `srp`.
- **Strategic drift detection** (was §8.5) → `drift-check.go` pre-script + agent prompt. Same signals (many commits / few beads closed, low-priority work while P0/P1 critical path stale, review findings clustering, health worsening while beads close, agents creating new beads without closing old ones, user-defined success criteria unmapped). Same actions (slow/stop swarm, reality-check review, generate drift report, revise beads, ask human to approve a refinement round). The agent decides per the skill.

### 8.6 Why this is better than the old §8

- **No Go reimplementation of the skills.** When `vibing-with-ntm` evolves, behavior evolves — no PR to write.
- **Cost stays bounded.** A healthy swarm produces near-zero LLM tokens (`wakeAgent: false` on every pre-script).
- **Panel stays quiet.** Healthy ticks produce no Activity-panel entries (`[SILENT]` on agent runs that decide no action is needed).
- **Deterministic safety where it matters.** Hard thresholds (budget, rate-limit, disk full, push policy) are pre-script-only and never wait for an LLM.
- **The orchestrator chat becomes literal.** The user is talking to a real tending agent with `vibing-with-ntm` loaded, not a metaphor for daemon code.
- **One runtime.** Scheduled tending, the orchestrator chat, and any future user-initiated investigation jobs all run on the same agent runtime.
- **Auditability.** Every job tick writes an audit entry whether or not the agent woke and whether or not it spoke, including the pre-script's structured detection payload.
- **User-editable.** A user can pause `drift-check`, change `tend-swarm`'s cadence, swap in a custom skill, or add a project-specific job without touching daemon code.

---

## 9. Debugging and hardening detail

This section is the operational detail behind the Debugging / Hardening stage (§7.4). The strategic intent and UI surface live there; the review-round mechanics, finding lifecycle, convergence detection, and specialized audit catalog live here.

### 9.1 Transition into Debugging / Hardening

When implementation beads are done or nearly done, Hoopoe proposes the transition into Debugging / Hardening (formerly "review mode"). Prerequisites: no obvious active implementation beads without owner; all P0/P1 ready beads either closed, in review, or intentionally deferred; Git status understood; latest health snapshot available; build/test queue not overloaded.

### 9.2 Review rounds

```text
Round 0: UBS first-pass scan (deterministic, fast, ACFS-installed)
Round 1: original-agent self-review
Round 2: cross-agent review
Round 3: fresh-eyes new-session review
Round 4: random code exploration
Round 5: hotspot-targeted review (often re-running UBS scoped to hotspots)
Round 6: test/coverage hardening
Round 7: UI/UX polish if applicable
Round 8: security/performance/deadlock/mock-code specialized skills if applicable
Round 9: final landing checklist
```

Round 0 is the cheap, deterministic first pass — `UBS` against the changed surface — and it runs automatically when entering Debugging / Hardening so the agent-driven rounds don't waste cycles on bugs a static scanner would catch. Each round writes an artifact recording: model/tool used (with `source: ubs` / `source: agent:<id>` / `source: skill:<name>` stamped on every finding for cross-tool deduping), prompts, agents involved, findings, fixes, new beads, false positives, test/health deltas, cost/time summary.

### 9.2.1 Review execution modes

Review rounds can execute in two modes, selected per round:

1. **Direct LLM review** — daemon gathers the plan, bead context, recent diffs, health hotspots, AGENTS.md, and target files, then calls a provider directly. This is good for cheap fresh-eyes critique, random exploration, and targeted hotspot review where tool use is not required.
2. **Delegated agent review** — daemon sends marching orders to an NTM-managed agent pane, usually an orchestrator or fresh agent, and streams its output into the review topic. This is required for reviews that need tool use, live test execution, cross-agent coordination, UBS scans, or project-specific commands.

Both modes write findings into the same finding ledger and use the same prompt templates. The mode is an implementation detail; the user sees a consistent Review tab.

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

When review saturation is reached but the user wants further hardening, Hoopoe offers targeted skills/workflows: `UBS` re-run with stricter rules or scoped to recently-changed files, mock-code finder, deadlock/concurrency finder, security audit for SaaS, performance profiling, project reality check, reasoning-mode analysis, golden artifact testing, fuzzing, e2e testing with logging/no mocks, UI polish review. Each audit creates beads instead of free-floating todos and stamps each finding with the source skill so post-audit triage can see "the deadlock skill produced 12 findings, 8 were duplicates of UBS findings already filed."

---

## 10. Observability and recovery

Every meaningful daemon action writes to `~/.hoopoe/audit.jsonl` with actor, project, action, reason, command preview, result, and artifact pointers. Each entry carries a monotonic `seq` number per project (not just `time`) so multi-process actors — tending jobs + user actions + adapter callbacks all writing concurrently — order deterministically under clock skew.

**Sequence-cursor + snapshot-on-reconnect** (pattern lifted from t3code's `OrchestrationRecoveryCoordinator`). Every WebSocket event carries `sequence: NonNegativeInt` per channel. The desktop tracks `latestSequence` + `highestObservedSequence` per channel. On disconnect or detected gap, it calls `replayEvents(channel, sinceSequence)` against the daemon's append-only log and merges results idempotently. Subscribe-RPCs return a snapshot first, then live deltas, on the same stream — there is no separate "snapshot" vs "subscribe" path. This is what makes laptop sleep, daemon restart, and tunnel re-establishment work without state corruption.

A Recovery/Diagnostics screen exposes daemon status, tunnel status, NTM sessions, active and stuck jobs, stale locks, last operator ticks, tool versions, disk pressure, recent audit events, and repair actions.

Audit log schema and replay protocol in `docs/security.md`.

### 10.1 Backpressure, lag, and replay rules

- Each event channel has a bounded in-memory ring and an append-only persisted log where appropriate.
- High-volume streams (terminal output, bootstrap logs, build logs) are persisted as byte-addressable logs; events carry offsets and summaries, not the entire text.
- If a client falls behind an event ring, the daemon emits `_lag` with `lastPersistedOffset` or `_gap` with `sinceSequence` repair instructions.
- Renderer subscriptions coalesce noisy events such as `agent.tick` and `pane.output` by agent/pane at a fixed cadence.
- Replay is idempotent. Events carry stable IDs; the desktop ignores duplicates and applies newer sequence numbers only.
- Causality is preserved where possible. Events caused by a command, job, approval, or adapter callback include `causationId`; related event chains share a `correlationId`.
- Event schema changes are versioned. The daemon can emit compatibility warnings when a desktop client subscribes to a channel whose event schema is newer than the client understands.

### 10.2 Production diagnostics and repair actions

Diagnostics should include safe repair buttons, each backed by typed daemon RPC and audit:


| Repair                                  | Safety behavior                                                                                                                                                                                         |
| --------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| Restart daemon                          | Requires confirmation; streams systemd result; preserves job registry where possible.                                                                                                                   |
| Restart NTM / Agent Mail service        | Requires project/session impact warning.                                                                                                                                                                |
| Re-run ACFS doctor                      | Read-only unless the user approves fixes.                                                                                                                                                               |
| Clear desktop local clone               | Deletes the local mirror only; never touches VPS or origin.                                                                                                                                             |
| Force release stale reservation         | Requires reason; posts Agent Mail notice; audit entry links to stale evidence.                                                                                                                          |
| Replay events from sequence             | Read-only repair for UI gaps.                                                                                                                                                                           |
| Rebuild bead read model                 | Re-reads `br`/`.beads`; cache-only action.                                                                                                                                                              |
| Re-run health snapshot                  | Runs in isolated worktree; queues behind active health jobs.                                                                                                                                            |
| Update / verify skills (`jsm` or `jfp`) | Re-runs the active installer; verifies SHA-256s in `.hoopoe/skills.lock.json` when `jsm` is configured; reports drift; one click to upgrade pinned versions (which is itself an audited config change). |


### 10.3 Retention, compaction, and migrations

Hoopoe stores enough history to be restartable and auditable, but it does not keep unbounded local data forever by accident.

Default retention:

- audit log: retained indefinitely unless the user exports and prunes;
- terminal/build/bootstrap logs: 30 days, configurable per project;
- model raw artifacts: 30 days private retention by default, configurable;
- health snapshots: keep last N full snapshots plus compacted trend history;
- event replay log: keep enough to cover recent reconnects, then compact into snapshots;
- skill installs: when `jsm` is configured, the per-project `.hoopoe/skills.lock.json` records SHA-256-pinned versions of every loaded skill so a project's tending behavior is reproducible across machines and across time. Lock-file changes are audited; pinned versions can be bumped explicitly via the Diagnostics "Update / verify skills" repair (§10.2). When only `jfp` is available, Hoopoe records the installer-reported version string instead of a SHA-256 and labels the pin as "advisory" rather than "verified."

Every persisted table/file has a schema version. Daemon startup runs migrations with backup and rollback. `/v1/compatibility` reports daemon API version, minimum desktop version, event schema versions, migration state, and unsupported client warnings.

### 10.4 Backup, export, and restore

Hoopoe provides a project-scoped export bundle:

```text
hoopoe export project <project-id> --out hoopoe-project-<date>.tar.zst
```

The bundle includes:

- daemon project metadata;
- audit log slice for the project;
- event replay checkpoints;
- plan artifacts;
- bead conversion traces and traceability maps;
- health snapshots;
- review findings;
- landing queue history;
- artifact refs and selected blobs;
- capability/tool inventory;
- skill lock file (`.hoopoe/skills.lock.json`) with SHA-256 pins when `jsm` was used, advisory version strings when `jfp` was used — so a restored project recreates the exact tending behavior;
- redacted diagnostics.

Secrets are excluded by default. Optional encrypted backup targets may be configured later, but local export/restore ships before cloud backup.

Restore validates schema versions, artifact hashes, and compatibility before rehydrating project state.

### 10.5 Product SLOs and fault injection

Hoopoe tracks these v1 service-level targets during development:


| Area                                    | Target                                                  |
| --------------------------------------- | ------------------------------------------------------- |
| Desktop reconnect after laptop wake     | p95 under 10s after network returns                     |
| Event replay after 10-minute disconnect | no lost state; p95 under 5s for normal project channels |
| Activity event display latency          | p95 under 1s excluding terminal chunks                  |
| Terminal stream attach                  | latest ring visible under 2s                            |
| Local file open from clone              | p95 under 150ms for cached blobs                        |
| Bead Kanban load                        | p95 under 1s for 1,000 beads                            |
| DAG usable                              | 500 visible nodes, clustered beyond that                |
| Job cancellation                        | no orphan child process groups after cancel test        |


Fault-injection tests simulate: tunnel drop, daemon restart, desktop crash, VPS reboot, disk full, Git push failure, missing tool, malformed adapter output, stuck terminal stream, rate limit, slow renderer, and long-running scheduler job.

---

## 11. Packaging and updates

**Build pipeline** (lifted from t3code's `scripts/build-desktop-artifact.ts` + `.github/workflows/release.yml`). The orchestrator stages all dist artifacts to a temp directory, **synthesizes a self-contained `package.json`** resolving the workspace catalog into concrete versions, runs `bun install --production` there, then invokes `bunx electron-builder` against the staged dir. The staged `package.json` is the source of build truth, not the repo's — this avoids electron-builder's flaky monorepo workspace support.

**Desktop distribution.** macOS signed and notarized DMG (arm64 + x64) for v1. `electron-updater` against GitHub Releases, with channel selection (`latest` vs `nightly`) per-user via `desktop-settings.json`. 15-second startup delay before first update check, 4-hour poll. `--publish never` on builder, manual upload in a separate CI step (more control). For local update testing, use a mock-update-server pattern (`scripts/mock-update-server.ts`).

**Daemon distribution.** Single static Go binary, downloaded via signed release URL, verified by checksum, signature, and provenance attestation. Release metadata includes checksums, signing identity, SBOM, artifact attestation, source commit, builder identity, and minimum compatible desktop/API versions. Bootstrap records the verified metadata in the daemon inventory so Diagnostics can show exactly what is running and where it came from. Upgrade flow: download → verify checksum/signature/provenance/SBOM policy → `stop service` → backup config/db → install → start → verify `/v1/version` → run compatibility checks. If migration fails, the daemon rolls back to the previous binary and database backup. Desktop refuses write actions during migration and shows read-only diagnostics until compatibility is restored. Desktop detects daemon version mismatch and offers upgrade.

Bootstrap refuses daemon installation when provenance verification fails unless the user explicitly enables an insecure development override. The override is audited and visibly marked in Diagnostics.

**Tool version pinning.** Record ACFS/tool versions, warn on unsupported versions, allow user to pin/upgrade, run adapter contract tests against pinned versions, show drift in settings.

**Cross-platform stance.** Mac-only for v1. The lifted build matrix supports Linux AppImage and Windows NSIS; we keep those code paths but don't ship those targets in v1.

---

## 12. Milestone roadmap

### Phase 0 — Research spike and integration contract

Goal: prove the stack can be read and controlled from code.

Deliverables: test VPS with ACFS; NTM server running; sample project with `br`/`bv`; one script that produces a full machine-readable JSON snapshot covering Git, beads, `bv` triage, NTM session, Agent Mail messages/reservations, and health metrics; documented command/API contracts; parser fixtures. Fixtures must be stored in a form that can drive Mock Flywheel Mode: normalized snapshots, event streams, pane-output logs, build/test logs, Agent Mail messages, reservation conflicts, health snapshots, and failure cases.

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
- Four-stage routed shell with `STAGE N — VERB` chrome (Planning, Beads, Swarm, Debugging / Hardening) plus a global Activity panel drawer.
- Reusable components: `StageHeader`, `AgentTile`, `BeadCard`, `StatusPill`, `PriorityChip`, `CoverageBar`, `TerminalPane`, `TimelineRow`, `HealthKpiCard`, `ApprovalDialog`, `CommandPalette`.
- ⌘K command palette with the registry from above.
- macOS Keychain integration via Electron `safeStorage`.

**Exit:** visual review against reference design passes; app can navigate stages; `bun run dist:desktop:dmg:arm64` produces a signed/notarized DMG; auto-update flow works against `mock-update-server`; settings hot-reload demonstrated; ⌘K palette opens; Mock Flywheel Mode can replay at least one fixture project without connecting to a VPS, so stage UI and Activity panel behavior are testable in CI.

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

### Phase 4 — Project registry, Git, and the desktop local clone

Create/import/clone project; project readiness checks; `.hoopoe` initialization; **daemon-side Git watcher** that tracks both VPS-local and origin state and emits three event types on the WebSocket stream: `vps_commit_created` (new commit in VPS working tree, not yet pushed), `vps_push_completed` (VPS pushed N commits to origin), and `origin_updated` (origin's refs advanced — fired by `vps_push_completed` and also by external pushes detected via `git fetch origin --dry-run` polling); **post-commit auto-push hook** installed at swarm-launch time per §7.3 push policy, with audit on every push attempt and surfacing of push failures; **desktop local clone (§7.7) — initial clone from origin, `clone-state.json` metadata, file watcher for local-edit detection, project-header dirty banner, "Clear local clone" / "Reveal in Finder" / "Open in terminal" actions, soft/hard size caps**; **clone-sync subsystem on desktop** — subscribe to `origin_updated` and run `git fetch --all --tags --prune` on event, render "N unpushed commits on VPS" indicator from `vps_commit_created` / `vps_push_completed` events without fetching, 60s safety-net poll, on-demand refresh button, fetch-on-WS-reconnect; **daemon RPCs for VPS WIP reads** — `getWorkingTreeStatus`, `getStagedDiff`, `getUnstagedDiff`, `getUnpushedCommits` for the live agent activity overlay; cockpit top bar (§7.6) wired up with project/branch/Git status (origin state from local clone, VPS state from daemon RPCs), tool-health dots, swarm state, beads pulse, a minimal code-health pill from the seed health adapter, cost indicator, Activity panel toggle; AGENTS.md detection/editor link; `br` initialization check; `ru --json` multi-repo support.

**Exit:** user can open a repo-backed project, see accurate Git/tool state, browse files instantly from the local clone, see live "N unpushed commits / M modified files" overlay from the daemon, and a commit made on the VPS by an agent appears in the desktop's local clone within ~3 seconds end-to-end (commit → auto-push → `origin_updated` event → desktop fetch).

### Phase 5 — Planning workspace

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

### Phase 9 — Activity panel and Agent Mail

Cross-stage Activity drawer (see §7.5); Agent Mail ingestion; agent↔agent message timeline; user↔orchestrator chat input; reservation view; urgent alerts; overseer compose; bead/agent pivot links; conflict/stale reservation warnings.

**Exit:** user can coordinate the swarm from any stage without opening Agent Mail manually, and can hold a live conversation with the orchestrator agent through the Activity panel.

### Phase 10 — Tending scheduler + initial job set

Build the scheduler infrastructure and the initial set of tending jobs (§8). Concretely:

- **Scheduler infrastructure** — `~/.hoopoe/tending/jobs.json` storage with atomic writes; tick loop with file lock to prevent overlapping ticks; cron expression / interval / event-trigger / on-demand schedule resolver; pre-script runner (Go subprocess); `wakeAgent` and `[SILENT]` plumbing; agent runtime that loads declared skills from the agentskills.io standard; per-job toolset budget enforcement; delivery dispatcher with `hoopoe_activity_panel`, `hoopoe_activity_urgent`, `local`, and external (Telegram/etc.) targets; audit-on-every-tick regardless of wake/silence.
- **Job lifecycle CLI and UI** — `hoopoe tending {list,create,edit,pause,resume,run,remove,status,tick}`; Diagnostics screen (§10) shows job table with last run, next run, last decision, recent audit entries, pause toggles.
- **Initial job set (§8.4)** — `tend-swarm`, `watch-safety-thresholds`, `push-stale-commits`, `snapshot-health`, `drift-check`, `review-readiness-check`, `orchestrator-chat`. Each ships with its pre-script and prompt; users can edit them post-install.
- **Skill loader** — dual-path resolution. **Preferred:** `jsm` (Jeffrey's Skills.md CLI, [jeffreys-skills.md](https://jeffreys-skills.md/dashboard)) when the user has a subscription configured, because SHA-256 deterministic versioning lets Hoopoe pin specific skill commits per project (`.hoopoe/skills.lock.json`) and cross-device sync keeps multi-workstation users aligned. **Fallback:** `jfp` (Jeffrey's Prompts, ACFS-installed, free) for users without a `jsm` subscription — sufficient for the open-source skills like `vibing-with-ntm` and `ntm` that the default tending jobs require. Resolution order at swarm-launch time: (1) check `.hoopoe/skills.lock.json` for pinned SHA-256s and verify against `jsm` cache; (2) if `jsm` unavailable or the skill isn't in the premium catalog, try `jfp`; (3) if both fail, refuse to launch the swarm and surface the missing skills in Diagnostics with one-click install/update buttons for whichever installer is configured. Hoopoe never reimplements skill-fetch logic — when `jeffreys-skills.md` evolves, `jsm` or `jfp` brings the changes in without a Hoopoe code release.
- **Approval gate integration** — destructive actions taken by tending agents (force-release stale reservation, halt swarm, force-push, etc.) go through the same approval surface as user-initiated ones (§5.3).

**Exit:** Hoopoe can tend a real swarm for an hour with visible, explainable, skill-driven interventions; a healthy hour produces near-zero LLM cost (most ticks `wakeAgent: false`) and near-zero Activity-panel noise (most agent runs `[SILENT]`); a stuck or drifting swarm triggers exactly the agent attention and panel output the situation warrants; the user can chat with the `orchestrator-chat` agent in the Activity panel and the orchestrator can read state, propose actions, and execute them via the same approval gates as user actions.

### Phase 11 — Debugging / Hardening: code health metrics

First subsystem of the Debugging / Hardening stage (§7.4.1). Health adapter discovery (TS/JS, Python, Rust, Go, generic); test/coverage/complexity parsing; health snapshots persisted as artifacts; KPI cards; sortable file health table (path, LOC, complexity, coverage bar, churn, owner agent, linked bead, hotspot reasons); hotspot scoring; trend sparklines; create bead from hotspot; **light up the top-bar code-health pill (§7.6) so coverage / complexity / hotspot count are visible from every stage**; per-bead "files touched" health rollup in the Beads drawer; per-agent code-health-delta on the Swarm agent tile; `health snapshot updated` events in the Activity panel.

**Exit:** swarm round updates health; low-coverage/high-complexity files can become beads; user can see project coverage, average complexity, and hotspot count from any stage in the cockpit without navigating to the Health tab.

### Phase 12 — Debugging / Hardening: review rounds and convergence

Second subsystem of the Debugging / Hardening stage (§7.4). Hardening-mode swarm launch; review round jobs; fresh-eyes prompts; cross-agent review; finding tracker; finding-to-bead conversion; convergence dashboard; final landing checklist.

**Exit:** completed implementation transitions into structured debugging/hardening and reaches the final gate.

### Phase 13 — Provider automation and production polish

One provider plugin (Hetzner first); cost estimate and teardown; polished empty/loading/error states; onboarding tour; diagnostics screen; crash reports opt-in; daemon upgrade system end-to-end; documentation and demo project.

(Signed/notarized DMG and auto-update infrastructure are already in place from Phase 1's lift — this phase is about polish, error UX, and provider automation, not building the release pipeline.)

**Exit:** a new user can install Hoopoe, connect/provision a VPS, import a project, create a plan, convert beads, launch agents, monitor review, and land a small project.

---

## 13. MVP scope

### Must include

Electron app with four-stage shell (Planning, Beads, Swarm, Debugging / Hardening) plus the Activity panel drawer and the design system; existing-VPS connection; daemon install over SSH; SSH tunnel and event stream; ACFS install/doctor/tool inventory; skill installer wired to the tending agents — `jfp` mandatory (free fallback, ACFS-installed), `jsm` recommended (preferred when subscription configured, enables SHA-256 pinning + cross-device sync per §12 Phase 10); project import/create; plan import/create/editor; plan-to-beads conversion through `br`; bead Kanban and basic DAG; `bv --robot-triage` panel; NTM swarm launch; agent grid with status and log tail; Agent Mail timeline in the Activity panel; user↔orchestrator chat in the Activity panel (backed by the `orchestrator-chat` tending job, §8.4); tending scheduler (§8) with at minimum the `tend-swarm`, `watch-safety-thresholds`, `push-stale-commits`, and `orchestrator-chat` jobs running with `vibing-with-ntm` and `ntm` skills loaded; `caut` adapter feeding the top-bar cost pill and the `watch-safety-thresholds` budget checks; `CAAM` adapter exposing account inventory and the "switch account" recovery action; `DCG` verdict ingestion into the unified approvals queue; basic code health scan; one `UBS` review round wired into the Debugging/Hardening review-round runner; audit log.

### Can defer

Multi-provider automatic VPS provisioning; perfect PTY fidelity for all panes; direct mTLS public daemon mode; full spend precision (precision beyond what `caut` reports); advanced Force graph interactions; full language coverage; CASS/CM deep memory workflows; `casr` cross-provider session resumption (CAAM-only account switching ships first; `casr` follows when the cross-CLI conversion path stabilizes); `apr`-delegated planning (decision per §7.1); SLB two-person rule integration; `rano` per-call latency dashboards; collaborative multi-user teams; hosted relay/cloud sync; Mac App Store distribution.

### Must include for development and demos

Mock Flywheel Mode: a fixture-backed daemon or adapter layer that replays snapshots, event streams, logs, Agent Mail, reservations, bead changes, and health snapshots. It is not a user-facing substitute for the real Flywheel, but it is required for deterministic UI development, release smoke tests, and support reproduction bundles.

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
12. Seed health adapter: detected test command, last known test result, LOC/file counts, and basic hotspot seed.

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

`**PubSub.unbounded` patterns leak through.** T3code uses `PubSub.unbounded` everywhere — a memory-leak landmine when slow consumers sit on fast producers (terminal output, activity stream). Hoopoe's daemon must bound channels at design time. Catch via load tests with a wedged consumer.

---

## 15. Definition of success

A successful Hoopoe session looks like this:

1. User opens the app.
2. Hoopoe reconnects to VPS and project.
3. Top bar shows repo, branch, cleanliness, tool health, swarm state, beads pulse, **code-health pill (coverage / complexity / hotspot count)**, cost vs. budget, and the Activity panel toggle (§7.6).
4. Planning shows current plan artifacts and status.
5. Beads show ready, blocked, critical, stale, and in-review work.
6. User launches a mixed NTM swarm.
7. Agents read `AGENTS.md`, register with Agent Mail, use `bv`, claim beads through `br`, reserve files, implement, test, push to origin promptly, and report.
8. Hoopoe streams live agent state, mail, reservations, build/test events, and graph changes through the stage views and the Activity panel; commits made on the VPS appear in the desktop's local clone within seconds of being pushed to origin.
9. Tending jobs (§8) wake when conditions warrant — nudging idle/stuck/rate-limited agents, force-releasing stale reservations, pushing unpushed commits, surfacing drift — each intervention explained via the `vibing-with-ntm` skill and audited; healthy stretches stay quiet (`wakeAgent: false` and `[SILENT]`); the user can chat with the orchestrator agent from the Activity panel.
10. Debugging / Hardening surfaces code-health metrics after commits/rounds.
11. Debugging / Hardening runs fresh-eyes and cross-agent review rounds.
12. Findings become fixes or beads, and the evidence graph links plan sections to beads, branches, commits, tests, health deltas, review findings, and landing status.
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
6. Build the four-stage routed shell (Planning, Beads, Swarm, Debugging / Hardening) with `STAGE N — VERB` chrome, the global Activity panel drawer, and the reusable component set.

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

**Phase 2.5 — API/process contract hardening before feature build-out.** Do this before Planning/Beads UI depends on daemon behavior.

1. Implement the seed REST/WS contract in §2.6 with generated schema tests.

1a. Implement `/v1/capabilities` and `/v1/compatibility`; wire all stage routes to capability-gated feature flags before building the feature UI.
2. Implement the job registry and process manager invariants in §2.7.
3. Add bounded channel/load tests using synthetic high-volume terminal output and a deliberately slow client.
4. Add idempotency-key tests for retrying write endpoints through a simulated tunnel drop.
5. Add persisted log offset tests: stream 100 MB of output, disconnect, reconnect, and fetch the missing range by offset.
6. Create `docs/api-seed.md`, `docs/process-manager.md`, and `docs/reconnect-replay.md` from the implementation as living references.
7. Add the first chaos/fault-injection suite: tunnel drop, daemon restart, disk pressure, slow renderer, malformed adapter output, and long-running scheduler job.
8. Start collecting the SLO metrics in §10.5 in dev builds and Mock Flywheel Mode.

Do not start with provider automation, spend charts, or polished graph animations. The first milestone is a working cockpit connected to a real VPS daemon with one real project and one real tool adapter.

---

## 17. References

**Methodology (the playbooks Hoopoe codifies as software).**

- Agentic Coding Flywheel methodology: `agent-flywheel.com/complete-guide`
- Beads workflow skill: `jeffreys-skills.md/skills/beads-workflow` — authoritative for Stage 02 (plan-to-beads conversion, polish rounds, traceability).
- `ntm` skill: `jeffreys-skills.md/skills/ntm` — tool reference for NTM (spawn, marching orders, inbox, robot-mode, pipelines/controllers/serve, safety/policy/approvals).
- `vibing-with-ntm` skill: `jeffreys-skills.md/skills/vibing-with-ntm` — authoritative behavioral spec for Stage 03 (Swarm, §7.3) and the tending jobs (§8). Covers tending swarms, recovering stuck/rate-limited panes, build/test contention handling, review-only mode, convergence detection, and multi-agent coordination via Agent Mail + Beads + BV. Loaded directly into Hoopoe's tending agents at runtime — not reimplemented in code.
- `agentskills.io` open skill standard: the on-disk format Hoopoe consumes when loading skills into tending agents. Compatible with the same library Hermes Agent and other agent products use.
- [Jeffrey's Skills.md](https://jeffreys-skills.md/dashboard) (skill catalog) and `jsm` (its CLI) — the canonical hosted source for `vibing-with-ntm`, `ntm`, and other curated Claude Code skills, with SHA-256 deterministic versioning and cross-device sync. Premium subscription. Hoopoe's skill loader prefers `jsm` when configured; the open-source skills are also reachable via the free ACFS-installed `jfp`. The skill *content* is the same; the difference is integrity guarantees, sync, and access to premium catalog entries.

**Architectural reference (the implementation pattern Hoopoe lifts).**

- Hermes Agent (Nous Research): `[github.com/NousResearch/hermes-agent](https://github.com/NousResearch/hermes-agent)`, [architecture docs](https://hermes-agent.nousresearch.com/docs/developer-guide/architecture), [cron docs](https://hermes-agent.nousresearch.com/docs/user-guide/features/cron). The pattern Hoopoe lifts for §8 tending: scheduler + skill-attached jobs + pre-script (`wakeAgent: false`) + agent-with-skill execution + `[SILENT]` for noise control + `context_from` for job chaining + atomic JSON job storage + per-job `enabled_toolsets` + one agent runtime serving multiple entry points. Hoopoe is not a Hermes deployment, but the tending machinery is shaped exactly like Hermes's cron subsystem.

**Tools (core flywheel — Hoopoe wraps).**

- ACFS setup: `github.com/Dicklesworthstone/agentic_coding_flywheel_setup` — canonical installer; `acfs.manifest.yaml` is the authoritative tool list Hoopoe must keep its adapter inventory aligned with.
- NTM: `github.com/Dicklesworthstone/ntm`
- Beads Rust (`br`): `github.com/Dicklesworthstone/beads_rust`
- `bv`: bead-graph triage TUI / `--robot-`* JSON surfaces (installed alongside `br`).
- Agent Mail: `github.com/Dicklesworthstone/mcp_agent_mail`
- Repo Updater (`ru`): `github.com/Dicklesworthstone/repo_updater`
- Remote Compilation Helper (`rch`): build offload — referenced throughout §7.3 / §8.5.

**Tools (safety, accounts, observability — Hoopoe surfaces).**

- `DCG` (Destructive Command Guard) — Claude Code hook intercepting dangerous commands; verdicts ingested into Hoopoe's approvals queue (§5.3).
- `SLB` (Simultaneous Launch Button) — optional two-person co-signature; integrates with safety presets (§7.3).
- `CAAM` (Cross-Agent Account Manager) — provider-account inventory and instant switching; backs the rate-limit recovery action (§7.3, §8.4).
- `caut` (coding agent usage tracker) — per-provider usage; backs the top-bar cost pill (§7.6) and `watch-safety-thresholds` budget checks (§8.4).
- `rano` (network observer for AI CLIs) — per-call latency/error signals; diagnostics-only for v1 (§7.3).
- `casr` (Cross-Agent Session Resumer) — converts in-flight sessions across CLIs/providers; backs cross-provider recovery (§7.3, §8.4).
- `pt` (process-terminator) — deterministic actuator for killing genuinely wedged processes (§8.4 `watch-safety-thresholds`).
- `srp` (System Resource Protection) — disk/CPU/load signals for `watch-safety-thresholds`.
- `sbh` (disk-pressure defense) — stale-artifact cleanup invoked under disk pressure (§8.5).
- `UBS` (Ultimate Bug Scanner) — first-pass scanner in review rounds 0/5 and the specialized-audit catalog (§7.4.2, §9.2, §9.5).

**Tools (skills, planning — Hoopoe delegates).**

- `jsm` ([Jeffrey's Skills.md](https://jeffreys-skills.md/dashboard) CLI) — **preferred** install/update mechanism for `vibing-with-ntm`, `ntm`, and other skills. SHA-256 deterministic versioning enables per-project skill-version pinning (`.hoopoe/skills.lock.json`); cross-device sync keeps multi-workstation users aligned. Premium subscription required.
- `jfp` (Jeffrey's Prompts, ACFS-installed) — **free fallback** install/update mechanism. Sufficient for the open-source skills the default tending jobs require. Used when `jsm` is unavailable or unconfigured. Hoopoe's skill loader (§12 Phase 10) shells out to `jsm` then `jfp` rather than reimplementing fetch/cache.
- `apr` (Automated iterative spec refinement) — *candidate* for delegating the Planning pipeline; decision pending per §7.1.
- `agent.opencode` (OpenCode multi-provider harness) — listed as a Server-side CLI mode option for users without a Claude Max / GPT Pro / Gemini Ultra subscription on a specific provider (§7.1).

**Tools (deliberately not adopted).**

- `wa` (WezTerm Automata) — alternative terminal-orchestration substrate; Hoopoe commits to `tmux`/`NTM` for PTY (§7.3) to avoid splitting the swarm across two state machines.

**Tools (out of scope — installed by ACFS but not Hoopoe's responsibility).**

- Project-side infra: `postgres`, `supabase`, `vercel`, `wrangler`, `vault`.
- Language runtimes: `bun`, `uv`, `rust`, `go`, `nvm`.
- Shell UX: `zsh`, `oh-my-zsh`, `p10k`, `atuin`, `zoxide`, `lazygit`, `lazydocker`, `ast-grep`.
- Hoopoe assumes these are present (or absent) but does not surface them.

When the plan and a referenced skill disagree on swarm or tending behavior, the skill wins — the plan is summarizing the skill, not redefining it. Hermes Agent is an architectural reference for scheduler + skills patterns. Hoopoe-specific safety, audit, approval, scheduler correctness, and source-of-truth rules override Hermes behavior whenever they differ.

Phase 0 must verify actual installed command names, output formats, version compatibility, and exact API surfaces on a fresh ACFS VPS before downstream phases assume them. Specifically: `caut` JSON snapshot shape, `CAAM` account-list/switch CLI, `DCG` verdict format, `UBS` finding format, `jsm` and `jfp` install/list/verify/update CLIs (and their SHA-256 / version-string output formats so the lock-file design in §10.3 is grounded in what each tool actually emits), `pt`/`srp`/`sbh` invocation contracts.

---

## 18. Verification and acceptance matrix

### 18.1 Milestone acceptance tests


| Milestone | Acceptance test                                                                                                                                                      |
| --------- | -------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| Phase 0   | On a real ACFS VPS, one command emits a normalized JSON snapshot for Git, beads, `bv`, NTM, Agent Mail, reservations, and health; fixtures are committed.            |
| Phase 1   | Signed/notarized DMG opens, navigates all four stages, hot-reloads settings, opens ⌘K, and runs a desktop unit test suite.                                           |
| Phase 2   | Fresh VPS → daemon installed → pairing consumed → bearer stored → WS connected → simulated disconnect/reconnect replays without state corruption.                    |
| Phase 3   | ACFS install/doctor runs through Hoopoe with structured checkpoints; a failed run resumes; raw-log fallback works when markers are missing.                          |
| Phase 4   | Import a repo with origin; local clone fetches origin; a VPS commit auto-pushes; desktop fetches on `origin_updated`; WIP overlay shows unpushed/modified VPS state. |
| Phase 5   | One-paragraph idea produces candidates, comparison, synthesis, refinement, lock, cost ledger, and persisted artifacts after desktop restart.                         |
| Phase 6   | Locked plan converts to `br` beads; Kanban and traceability match `br`/`.beads`; polish metrics and manual curation round-trip through `br`.                         |
| Phase 7   | DAG renders a fixture graph, highlights critical path and ready frontier, and remains usable at 500 visible nodes through clustering/virtualization.                 |
| Phase 8   | A small mixed swarm launches through NTM, receives staggered kickoff, registers with Agent Mail, claims beads, and streams logs.                                     |
| Phase 9   | Activity drawer shows Agent Mail, reservations, bead changes, urgent alerts, and user↔orchestrator chat from any stage.                                              |
| Phase 10  | Healthy swarm hour produces mostly `wakeAgent:false` / `[SILENT]`; stuck fixture triggers a skill-driven intervention and audit.                                     |
| Phase 11  | Health scan runs in a worktree, persists raw/normalized artifacts, updates top-bar pill, and can create a bead from a hotspot.                                       |
| Phase 12  | Review round produces findings; each finding resolves to fix/new bead/false positive/human escalation; convergence dashboard updates.                                |
| Phase 13  | Existing-VPS path remains green while one provider plugin can create, estimate, and tear down an instance without affecting manual mode.                             |


### 18.2 End-to-end smoke scenario

Run before every beta release:

```text
1. install signed Hoopoe DMG on a clean macOS profile
2. connect to fresh Ubuntu VPS via SSH
3. run ACFS bootstrap and daemon install
4. import fixture repo with origin remote
5. create or import a plan
6. generate/refine/lock plan or use fixture locked plan
7. convert plan to beads
8. curate at least one bead and dependency in UI
9. launch a 2–3 agent smoke swarm or mock NTM swarm
10. ingest Agent Mail and reservations
11. create a commit on the VPS and verify origin/local-clone sync
12. run health snapshot in isolated worktree
13. run one fresh-eyes review and resolve one finding into a bead
14. kill/restart desktop and daemon; verify replay/recovery
15. upgrade daemon; verify compatibility checks
16. confirm no secrets appear in logs or audit artifacts
```

### 18.3 Adapter contract tests

Each adapter has golden fixtures for normal output, missing tool, unsupported version, malformed JSON, timeout, and high-volume output. For `br`, `bv`, `ntm`, and Agent Mail, Phase 0 fixtures are mandatory before feature work depends on the adapter. Tests assert that human CLI/TUI output is never parsed unless the adapter explicitly marks it as fallback mode.

### 18.4 Release smoke checks

- signed app launches on arm64 and x64 macOS;
- auto-update mock server upgrades stable → beta and beta → stable channel choices;
- desktop settings/keybindings survive migration;
- daemon upgrade backs up config/db and passes `/v1/version` compatibility;
- local clone cache can be cleared and rebuilt;
- event replay works after simulated laptop sleep;
- process manager cancels a long-running job without orphan children;
- health worktree cleanup leaves no stale multi-GB directories;
- audit log redaction catches model keys, bearer tokens, SSH passphrases, and provider credentials.
- project export/restore preserves plan, beads traceability, findings, landing history, and artifact hashes without including secrets.

---

## Appendix A — Where the operational details live now

The full earlier version is preserved at `plan.full.md`. Detail moved out of this document:


| Cut from                                                                                                                                                 | New home                                       |
| -------------------------------------------------------------------------------------------------------------------------------------------------------- | ---------------------------------------------- |
| Repo workspace structure                                                                                                                                 | top-level `README.md` at scaffold time         |
| Persistent data layout (`/data/projects/...`, `~/.hoopoe/`, `~/Library/Application Support/Hoopoe/`)                                                     | `docs/source-of-truth.md`                      |
| Entity schemas (`VpsHost`, `Project`, `Plan`, `Bead`, `SwarmSession`, `Agent`, `FileReservation`, `BudgetPolicy`, `BuildQueuePolicy`, `SwarmLaunchSpec`) | `packages/schemas/` (TS + Go generated)        |
| `CommandSpec`, full approval-checkpoint matrix, audit-log schema                                                                                         | `docs/security.md`                             |
| Tool-inventory JSON schema                                                                                                                               | `packages/schemas/`                            |
| `project.json`, full readiness-check list                                                                                                                | `packages/schemas/` + `docs/onboarding.md`     |
| Planning/Beads/Swarm/Debugging UI mockups + Activity panel mockups, columns, drawers, KPI cards                                                          | `packages/design-system/` (Storybook + tokens) |
| `PlanQualityScore`, `BeadSetQuality`, `CodeHealthSnapshot`, `FileHealthMetric`                                                                           | `packages/schemas/`                            |
| Daemon REST endpoint list                                                                                                                                | `packages/schemas/openapi.yaml`                |
| `Job` model, lifecycle, kind enum                                                                                                                        | `packages/schemas/`                            |
| Design-system component inventory (`StageHeader`, `BeadCard`, `AgentTile`, etc.)                                                                         | `packages/design-system/README.md`             |
| Testing strategy detail (desktop tests, daemon tests, integration scenarios, E2E disposable VPS, smoke checks)                                           | `docs/testing.md`                              |
| Provider plugin contract                                                                                                                                 | `packages/schemas/`                            |
| Pane stream event types                                                                                                                                  | `packages/schemas/`                            |


Schemas, API contracts, and component inventories belong in source code so the type system and tests catch drift. The plan reserves itself for vision, decisions, and roadmap.

---

## Appendix B — T3 Code lift inventory

Source: `github.com/pingdotgg/t3code`, MIT License (Copyright 2026 T3 Tools Inc.). Pin a specific commit SHA in `docs/source-provenance.md` at lift time. MIT requires only that the copyright notice be preserved in any substantial portion of the source.

**Vendoring layout.** Lifted files land under `apps/desktop/src/vendored/t3code/` with MIT notice preserved at the top of each file. Adaptations (rebranding, rewiring) happen in our own files that import from `vendored/`. Do not edit `vendored/` files in place except for mechanical mass renames — keep the diff against upstream small enough to re-merge later if needed.

### Files lifted


| Source (`/tmp/t3code-pinned/`)                                           | Hoopoe target                                                                                                                                                            | Adaptation                                                                                                                                                                                                                                                                                |
| ------------------------------------------------------------------------ | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------ | ----------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `apps/desktop/src/clientPersistence.ts`                                  | `apps/desktop/src/vendored/t3code/clientPersistence.ts`                                                                                                                  | Replace `T3CODE_*` env keys with `HOOPOE_*`; keep `safeStorage.encryptString` logic intact                                                                                                                                                                                                |
| `apps/desktop/src/backendPort.ts`                                        | same                                                                                                                                                                     | Default port → Hoopoe's                                                                                                                                                                                                                                                                   |
| `apps/desktop/src/backendReadiness.ts`                                   | same                                                                                                                                                                     | Replace t3code stdout signature with Hoopoe daemon's "listening on" line                                                                                                                                                                                                                  |
| `apps/desktop/src/serverListeningDetector.ts`                            | same                                                                                                                                                                     | Update parser regex                                                                                                                                                                                                                                                                       |
| `apps/desktop/src/desktopSettings.ts`                                    | same                                                                                                                                                                     | Schema swap (HoopoeDesktopSettings)                                                                                                                                                                                                                                                       |
| `apps/desktop/src/updateMachine.ts`                                      | same                                                                                                                                                                     | None                                                                                                                                                                                                                                                                                      |
| `apps/desktop/src/updateChannels.ts`                                     | same                                                                                                                                                                     | None                                                                                                                                                                                                                                                                                      |
| `apps/desktop/src/updateState.ts`                                        | same                                                                                                                                                                     | None                                                                                                                                                                                                                                                                                      |
| `apps/desktop/src/runtimeArch.ts`                                        | same                                                                                                                                                                     | None                                                                                                                                                                                                                                                                                      |
| `apps/desktop/src/syncShellEnvironment.ts`                               | same                                                                                                                                                                     | None                                                                                                                                                                                                                                                                                      |
| `apps/desktop/src/windowReveal.ts`                                       | same                                                                                                                                                                     | None                                                                                                                                                                                                                                                                                      |
| `apps/desktop/src/confirmDialog.ts`                                      | same                                                                                                                                                                     | None                                                                                                                                                                                                                                                                                      |
| `apps/desktop/src/appBranding.ts`                                        | same                                                                                                                                                                     | Replace branding strings                                                                                                                                                                                                                                                                  |
| `apps/desktop/src/main.ts` (2,175 lines)                                 | **decomposed** into `BackendLifecycle.ts`, `UpdateMachine.ts`, `IpcRegistry.ts`, `WindowManager.ts`, `SettingsBridge.ts`, `AuthBridge.ts` under `apps/desktop/src/main/` | Major: split monolith on day one. Drop `ELECTRON_RUN_AS_NODE` codepath. Replace `process.execPath`-as-Node trick with launching Hoopoe's Go daemon binary. Drop the local-pairing-token-via-FD-3 path for v1 (not needed when daemon is remote); keep the code for future local-demo-mode |
| `scripts/build-desktop-artifact.ts`                                      | `scripts/build-desktop-artifact.ts`                                                                                                                                      | Strip Linux/Windows targets from default; keep code paths under flags. Replace `@t3tools` → `@hoopoe`                                                                                                                                                                                     |
| `scripts/mock-update-server.ts`                                          | same                                                                                                                                                                     | None                                                                                                                                                                                                                                                                                      |
| `scripts/release-smoke.ts`                                               | same                                                                                                                                                                     | None                                                                                                                                                                                                                                                                                      |
| `.github/workflows/release.yml`                                          | same                                                                                                                                                                     | Strip Linux + Windows matrix entries (keep mac arm64 + x64). Update secrets (CSC_LINK, APPLE_API_KEY, GH_TOKEN). Rename workflows                                                                                                                                                         |
| `.github/workflows/*.yml` (typecheck, lint, test)                        | same                                                                                                                                                                     | Adapt to Hoopoe's command names                                                                                                                                                                                                                                                           |
| `apps/desktop/scripts/{dev-electron, start-electron, smoke-test}.mjs`    | same                                                                                                                                                                     | None                                                                                                                                                                                                                                                                                      |
| `electron-builder` config (in `apps/desktop/package.json` / `build.yml`) | same                                                                                                                                                                     | Update `productName`, `appId`, signing identity                                                                                                                                                                                                                                           |


### Patterns lifted (re-implemented, not copied)

| Pattern                                          | Source reference                                                           | Hoopoe location                                                                                          |
| ------------------------------------------------ | -------------------------------------------------------------------------- | -------------------------------------------------------------------------------------------------------- | -------------------------------- | -------------------------------- |
| Two-token auth (pairing → bearer → WS-token)     | `apps/server/src/auth/Layers/{Bootstrap,Session}CredentialService.ts`      | `apps/daemon/internal/auth/` (Go, greenfield)                                                            |
| Settings three-store split + hot reload + PubSub | `apps/server/src/serverSettings.ts`                                        | `apps/daemon/internal/settings/` (Go) + `apps/desktop/src/main/SettingsBridge.ts`                        |
| Keybindings AST + file watch + last-rule-wins    | `apps/server/src/keybindings.ts`, `apps/web/src/keybindings.ts`            | `apps/desktop/src/main/keybindings/` (TS, no Effect)                                                     |
| Sequence-cursor + snapshot-on-reconnect          | `apps/web/src/orchestrationRecovery.ts`, `apps/web/src/rpc/wsTransport.ts` | `apps/desktop/src/renderer/sync/` (TS) + `apps/daemon/internal/events/` (Go)                             |
| FD-3 bootstrap envelope (local demo mode only)   | `apps/desktop/src/main.ts:1395-1413`                                       | `apps/desktop/src/main/BackendLifecycle.ts` (deferred to local-demo path)                                |
| Atomic file write (tempfile + rename)            | `apps/server/src/atomicWrite.ts`                                           | `apps/daemon/internal/storage/` (Go: `os.Rename` + `Sync`) + `apps/desktop/src/main/atomicWrite.ts` (TS) |
| `auth pairing/session create                     | list                                                                       | revoke` CLI shape                                                                                        | `apps/server/src/cli.ts:809-969` | `apps/daemon/cmd/hoopoe/auth.go` |

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

1. `**PubSub.unbounded` everywhere** — bound all channels in the daemon.
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

---

## Appendix C — Non-negotiable implementation guardrails

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

