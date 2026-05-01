# Design decisions and open questions

Companion to `README.md`. This file is the ledger of design choices the v1 mockups have made and the unresolved conflicts they raise against `plan.md`.

Convention: each entry has a status (`adopted` / `unresolved` / `deferred`), a date stamp on resolution, and either a recommended resolution or the resolution itself. Date format: `YYYY-MM-DD`.

---

## Adopted — design choices the plan should pick up

### A1. macOS Tahoe / Liquid Glass aesthetic

**Status:** `adopted` · **Decided:** 2026-05-01

The cockpit chrome is macOS Tahoe-style: frosted-glass sidebar with `backdrop-filter: blur(40px) saturate(180%)`, traffic lights in the top-left, single-pixel borders, big window shadow, light + dark themes both first-class. Primitives in `mockups/v1/macos-window.jsx` and the inlined styles in `mockups/v1/app.jsx` (`MacShell`, `Sidebar`, `Toolbar`).

Implication for `plan.md` §3 (desktop tech) and §11 (cross-platform stance): the Mac-only v1 position is reinforced by adopting Mac-native vibrancy as a load-bearing visual primitive. Linux/Windows builds (kept as code paths in §11 / Appendix B) would need a different chrome and are not designed.

### A2. Hoopoe-russet `#C25A2E` accent + system-blue `#0A84FF` secondary

**Status:** `adopted` · **Decided:** 2026-05-01

Primary actions, brand affordances (the hoopoe's crest, "New Project," "Continue refining," etc.) are russet; secondary / system affordances and selection use system blue. Dark-theme accent is the lighter `#E58253`. Full token table in `mockups/v1/app.jsx` `TOKENS`.

### A3. Sidebar projects → plans tree with Plans / Files toggle

**Status:** `adopted` · **Decided:** 2026-05-01

The sidebar lists projects, each expandable to its plan documents (`PLANS[projectId]`). A binary segmented control above the list flips the inner view between **Plans** (just plan documents) and **Files** (full workspace tree). Implementation in `mockups/v1/app.jsx` `Sidebar` + `FileTree`.

This refines `plan.md` §7 / §7.1 ("Within a project, the user can have multiple plans over the project's lifetime"; "import an existing plan"; "create a new plan from a rough idea") — the design pins down how multiple plans and the rest of the workspace coexist in one navigation surface.

### A4. Plan-creation flow honors §7.1 pipeline beat-for-beat

**Status:** `adopted` · **Decided:** 2026-05-01

`mockups/v1/wizard.jsx` implements the pipeline `plan.md` §7.1 specifies: chat-box brief → primary + up to 3 competing models → comparative matrix → synthesis → fresh-eyes critique → refinement rounds → lock. Default primary is **ChatGPT Pro** with the "Recommended" tag; default competing set is **Opus / Gemini 3 Pro / Grok Heavy**.

### A5. Plan brief adds a kickoff-mode toggle

**Status:** `adopted` · **Decided:** 2026-05-01

The plan brief screen (`mockups/v1/wizard.jsx` `StepBrief`) adds a binary toggle the plan currently leaves implicit: **"Ask clarifying questions"** (models interview the user before drafting) vs **"Take a first shot"** (models go straight to a draft). Default is `clarify`.

`plan.md` §7.1 should mention this — it's a meaningful UX commitment about how the chat box behaves on submit.

### A6. Beads stage: DAG (layered topological) + Kanban (5 columns)

**Status:** `adopted` · **Decided:** 2026-05-01

The Beads stage offers two views, switchable via a segmented control in the toolbar:

- **DAG** — top-down layered topological layout, layer = longest-path-from-root. Sibling order within a layer follows average parent x-coordinate to minimize edge crossings. SVG arrow edges, orthogonal jog routing. Implementation: `mockups/v1/beads.jsx` `DAGView`.
- **Kanban** — five columns: `queued / running / review / done / blocked`. `mockups/v1/beads.jsx` `KanbanView`.

`plan.md` §7.2 already specifies "Kanban (execution state), DAG (dependency structure), Force (cluster/hotspot exploration)" — the design pins the DAG layout algorithm and defers Force to a later phase. §13 lists "advanced Force graph interactions" as deferred, so this is consistent.

Each bead also displays the **competing planner model that authored it** as a small colored dot (one of `gpt-pro / opus / gemini / grok`). This is a richer traceability than `plan.md` §7.2's `traceability.json` describes — that maps beads to plan sections, this maps beads to authoring model. Both are useful; both should ship.

### A7. Refinement-rounds modal exposes the `br` prompt as a first-class artifact

**Status:** `adopted` · **Decided:** 2026-05-01

The Beads stage's "Rounds" modal (`mockups/v1/beads.jsx` `RoundsModal`) shows, per round: which model ran, status (done / active / pending), duration, action summary, and **the literal prompt sent to that model**. The prompt itself is a `<pre>` block the user can read.

This is a design commitment `plan.md` §7.2 leaves implicit. §7.2 says "Polish rounds (each round is a tracked job with its own artifact)" but doesn't prescribe the artifact UI. Surfacing the prompt is consistent with §1.4 ("Every automation must be inspectable") and Appendix C #10 ("Do not suppress audit entries just because a job returned `[SILENT]`"). Pin it.

### A8. Composition picker UX: binary mode toggle + manual sliders

**Status:** `adopted` · **Decided:** 2026-05-01

The Swarm launch panel (`mockups/v1/swarm.jsx` `LaunchPanel`) implements `plan.md` §7.3 "Manual ratios" and "Let Hoopoe choose" as a two-card binary toggle. Manual mode shows one row per harness with a `0..8` range slider for count.

Already covered by §7.3; recorded here for traceability.

---

## Unresolved — contradictions with `plan.md` that need a decision

### U1. Swarm dashboard surfaces live agent log streams (`think` kind, "streaming…")

**Status:** `unresolved` · **Raised:** 2026-05-01 · **Owner:** product / design

**The conflict.** `mockups/v1/swarm.jsx` `WorkerCard` shows a "Mini log" with kinds `{tool, read, edit, think, log}`. The `think` kind carries reasoning text:

```js
{ t: 12, kind: 'think', text: 'Reconciling sidecar protobuf surface with bead-graph IPC...' }
```

The `WorkerLogPanel` side panel shows full live log entries with a "streaming…" indicator and typing-dots animation. The default Swarm UI shows this for every agent.

This conflicts with `plan.md` §7.3 and Appendix C #12:

- §7.3 (line 944): *"The user never sees terminal output by default — only abstracted bead state and agent state."*
- §7.3 (line 1021): *"Terminal scrollback is observability noise the user shouldn't have to decode... raw `git status` output and Codex thinking-token streams do not [answer 'what's happening?' precisely]."*
- Appendix C #12: *"Do not surface raw terminal panes in the default swarm UI. PTY plumbing exists on the daemon side for tending and forensics; the user-visible Swarm dashboard shows bead state + agent state + Activity panel only."*

The `tool` / `read` / `edit` / `log` entries are fine — they ARE the "recent decisions" §7.3 explicitly allows ("opened bead B-142", "ran 3 tests, all passed", "pushed 2 commits"). The conflict is specifically:

1. The `think` kind with reasoning text — exactly the "Codex thinking-token streams" §7.3 calls out.
2. The "streaming…" indicator + typing dots in `WorkerLogPanel` — pushes the surface into terminal-replacement territory.

**Recommended resolution.** Drop the `think` kind from log entries; remove "streaming…" / typing-dots from the default `WorkerLogPanel`; keep `tool / read / edit / log` as discrete completed-action entries (the "recent decisions" abstraction). The full pane stream remains reachable via Diagnostics → "Show raw pane" (`plan.md` §10.2) for forensic use, audited on toggle.

This preserves §7.3 / Appendix C #12 as load-bearing while keeping most of the design's information density.

**Resolve before:** Phase 8 (Swarm MVP). If the design is changed in Claude.ai/design, copy the updated files into `design/mockups/v2/` and update this entry.

### U2. Swarm shows API token cost ($/1k tok) per model and dollar estimates

**Status:** `unresolved` · **Raised:** 2026-05-01 · **Owner:** product

**The conflict.** `mockups/v1/swarm.jsx` `SWARM_MODELS`:

```js
{ id: 'gpt-5-5-xhigh',         name: 'GPT-5.5 xhigh',     dot: '#10A37F', cost: 0.020 },
{ id: 'claude-opus-4-7-max',   name: 'Claude Opus 4.7 max', dot: '#C25A2E', cost: 0.018 },
{ id: 'gemini-3-1-pro',        name: 'Gemini 3.1 Pro',    dot: '#4285F4', cost: 0.012 },
```

The launch panel shows `$0.0200/1k tok` per model; the auto-recommendation shows "estimated cost: $4.20"; per-worker cards track `tokens` and `cost`.

This conflicts with:
- `plan.md` §7.6 (line 1122): *"Hoopoe is subscription-only (§13), so this surface tracks **subscription budget**, not API-token dollars."*
- `plan.md` §13 (line 1947): *"Hoopoe does not support BYOK API keys, has no direct-API path to OpenAI/Anthropic/Google, and will not gain one — see §5.1, §7.1, and Appendix C for the architectural rationale."*
- Appendix C #11: *"Do not call provider APIs directly. No `OPENAI_API_KEY` / `ANTHROPIC_API_KEY` / `GEMINI_API_KEY` config field anywhere..."*

Per-token dollar costs are only meaningful on a BYOK / direct-API path, which Hoopoe deliberately doesn't have. The meaningful metric on Claude Max / GPT Pro / Gemini Ultra is **% of daily/weekly subscription quota** (sourced from `caut`).

**Recommended resolution.** In the launch panel, replace `$/1k tok` per model with **"X% of daily Claude Max quota"** / **"Y% of daily GPT Pro quota"** / **"Z of N weekly Gemini Deep Think runs"** sourced from `caut`. Replace "estimated cost: $4.20" with "estimated wall-clock: ~38 min · estimated quota burn: ~12% of daily Claude Max" (or "unmeasured" when `caut` has no data, per §7.6). Per-worker stats: replace `tokens` / `cost` with quota usage + wall-clock elapsed.

**Resolve before:** Phase 8 (Swarm MVP). Same as U1 — fix in Claude.ai/design and re-import.

### U3. Provision-VPS card lists "Hetzner, DigitalOcean, or AWS"

**Status:** `unresolved` · **Raised:** 2026-05-01 · **Owner:** content

**The conflict.** `mockups/v1/firstrun.jsx` `ModeChoice` "Provision a new VPS" card describes: *"Spin up a fresh server through Hetzner, DigitalOcean, or AWS."*

`plan.md` §6.2 explicitly recommends **Contabo** (top pick) and **OVH** first, with Hetzner / DigitalOcean / Linode as common alternatives, and AWS / GCP / Azure **deliberately not recommended**: *"billing is unpredictable and equivalent specs cost 3–5× more, exactly the failure mode the canonical guide calls out."*

**Recommended resolution.** Update the card text to: *"Spin up a fresh server through Contabo, OVH, or Hetzner."* Drop the AWS mention. The card is `tag: 'Coming in v1.1'` and disabled anyway; this is a content fix, not a UX change.

**Resolve before:** Phase 13 (provider automation). Trivial fix; can land any time.

### U4. "Workers" terminology vs. plan's "agents"

**Status:** `unresolved` · **Raised:** 2026-05-01 · **Owner:** content

**The conflict.** `mockups/v1/swarm.jsx` consistently says *"workers"* — `SWARM_MODELS`, `INITIAL_WORKERS`, `WorkerCard`, `WorkerLogPanel`, "forager-01" / "forager-02" worker names, `LaunchPanel` "X workers" copy.

`plan.md` and the agent-flywheel methodology consistently say **"agents."**

**Recommended resolution.** Rename "workers" → "agents" throughout the Swarm UI. Worker names like `forager-01` / `forager-02` are fine as labels; the noun should match the plan.

**Resolve before:** Phase 8 (Swarm MVP). Trivial.

### U5. No CAAM account picker per agent in launch panel

**Status:** `unresolved` · **Raised:** 2026-05-01 · **Owner:** design

**The conflict.** `mockups/v1/swarm.jsx` `LaunchPanel` lets the user pick a count per harness but doesn't surface which `CAAM`-managed account each agent will run under, nor the account-pressure warning the plan calls for.

`plan.md` §12 Phase 8: *"per-harness CAAM-account selection, account-pressure warning when requested count exceeds available accounts."*

`plan.md` §7.3 (line 952): *"Hoopoe greys out harnesses for which the user has no configured subscription and shows a warning if the requested count exceeds the available account count for a given harness (e.g., 'you have 1 Claude Max account but requested 4 Claude Code agents — they will share the account and may rate-limit')."*

**Recommended resolution.** Extend the manual-mode row layout to include a CAAM-account picker per harness (when more than one is configured) and show a yellow warning banner under the row when count > available accounts.

**Resolve before:** Phase 8.

### U6. Activity panel (§7.5) is missing entirely

**Status:** `deferred` · **Raised:** 2026-05-01 · **Owner:** design

**The gap.** `plan.md` §7.5 describes the cross-stage Activity drawer (Agent Mail timeline + user↔orchestrator chat input + bead/agent pivot links + reservation view + urgent alerts) as a persistent surface available from any stage. The v1 mockups don't show it.

**Status rationale.** Phase 9 territory. Out of scope for the v1 sketches by intent. Recorded here so it isn't forgotten.

**Resolve before:** Phase 9.

### U7. Persistent top-bar / cockpit chrome (§7.6) is missing

**Status:** `deferred` · **Raised:** 2026-05-01 · **Owner:** design

**The gap.** `plan.md` §7.6 specifies an always-visible top bar with project / branch / Git status, tool-health dots, swarm state, beads pulse, code-health pill, subscription-usage pill, and Activity panel toggle — visible from every stage. The v1 mockups use a per-stage toolbar with breadcrumb + tabs instead, with no global top bar. This means coverage / hotspots / subscription quota aren't visible from Planning or Beads.

**Status rationale.** §12 Phase 4 calls for the top bar's first cut and Phase 11 lights up the code-health pill; v1 sketches predate that work.

**Resolve before:** Phase 4 (project registry).

### U8. First-run wizard is 11 steps; canonical agent-flywheel.com is 13 — explicit mapping not yet recorded

**Status:** `unresolved` · **Raised:** 2026-05-01 · **Owner:** product / design

**The gap.** `plan.md` §6.1 has a table mapping the canonical 13-step agent-flywheel.com wizard to where Hoopoe's wrapper sits in each. The v1 mockups (`mockups/v1/firstrun.jsx`) implement an 11-step Hoopoe-specific flow:

```
0. Welcome
1. Mode (existing VPS / provision / local demo)
2. SSH
3. Preflight
4. ACFS Install
5. Daemon
6. Tools
7. Subscriptions
8. Oracle
9. First project
10. Ready
```

The 11-step variant merges several canonical steps (OS choice, terminal install, key generation, rent-VPS, create-instance) into a smaller surface and treats Pro setup as Oracle. The mapping to canonical 1..13 is not explicit anywhere.

**Recommended resolution.** Either (a) extend `plan.md` §6.1's table to add a "Hoopoe v1 wizard step" column showing how each of the 11 steps maps to canonical 1..13, or (b) widen the wizard to match canonical 13 if the merges turn out to lose information.

**Resolve before:** Phase 3 (ACFS onboarding).

### U9. ACFS install phase names not aligned with §6.4 structured-event names

**Status:** `unresolved` · **Raised:** 2026-05-01 · **Owner:** engineering

**The gap.** `mockups/v1/firstrun.jsx` `AcfsInstall` defines phases as: `prereqs / packages / runtimes / agentclis / ntm / brbv / mail / safety / skills / done`. `plan.md` §6.4 specifies structured events `phase.start / phase.line / phase.checkpoint / phase.end / phase.fail` with phase IDs that should match what the upstream ACFS installer emits.

**Recommended resolution.** Defer to Phase 0 fixture capture (`plan.md` §16 task 1 — "stand up a 64 GB Ubuntu 24+ VPS with ACFS installed via the canonical curl|bash one-liner... step-by-step notes from this run become the basis for §6.4's structured ACFS bootstrap parsing"). The phase names in the mockup are placeholders; align them to the actual installer output once Phase 0 lands.

**Resolve before:** Phase 3 (ACFS onboarding).

---

## How to add a new entry

1. Decide whether it's `adopted` (a design choice the plan should adopt) or `unresolved` (a contradiction needing resolution).
2. Add an entry under the right section with status, date, owner, conflict description (with `plan.md` line references), and recommended resolution.
3. When resolved, update status, add `**Resolved:** YYYY-MM-DD`, and note where the change landed (mockup version, plan.md commit, etc.).
4. Don't delete resolved entries — they're history, not noise.
