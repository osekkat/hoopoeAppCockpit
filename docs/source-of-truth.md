# Source Of Truth

`plan.md §1.1` is the authority for Hoopoe's source-of-truth boundary. This
file is the code-near reference for persistent data paths and reconciliation
rules. It exists because `plan.md` Appendix A moved the data-layout detail here.

## Canonical Owners

This table is scoped to the persistent-path owners the daemon, desktop, or in-repo
artifacts actually manage. The full canonical-owner list — including tool-only
owners that do not own persistent paths in this checkout — lives in
`plan.md §1.1`; entries omitted here on purpose are listed under
[Tool-only canonical owners](#tool-only-canonical-owners).

| Domain | Canonical source | Hoopoe behavior |
| --- | --- | --- |
| Code, branches, tags | Git origin | Desktop mirrors origin read-only; daemon drives VPS-side git actions. |
| VPS working state | `/data/projects/<project-id>/` | Agents and daemon actions operate here before pushing. Matches `ProjectRepoRef.VpsClonePath` in `packages/schemas/openapi.yaml` and `project.RootPath` in `apps/daemon/internal/projects/projects.go`. |
| Desktop code mirror | `~/Library/Application Support/Hoopoe/projects/<project-id>/repo/` | Read-only sync mirror of origin for fast UI reads, diffs, blame, and ripgrep. |
| Plans | `.hoopoe/plans/<plan-id>/` in the repo | Locked plans become the basis for bead conversion and traceability; Hoopoe's §7.1 planning pipeline writes its candidates / matrix / synthesis / refinement artifacts under the same plan-id directory. |
| Beads | `br` and `.beads/` | Hoopoe reads/writes through typed daemon RPCs. |
| Bead graph intelligence | `bv --robot-*` reading from `.beads/beads.jsonl` | Triage panels, graph metrics, and launch readiness consume `bv --robot-*` JSON only; bare `bv` is forbidden in automation (Guardrail 1). |
| Swarm sessions | NTM + tmux | Hoopoe renders session state; it does not replace NTM. |
| Agent messages and reservations | Agent Mail | The Activity panel mirrors Agent Mail threads and reservation state. |
| Build/test evidence | `rch`, runner envelopes, language-native test reports | Hoopoe stores evidence pointers and snapshots, not a parallel result authority. |
| Code health | Coverage/complexity reports + Git history; isolated worktree snapshots under `~/.hoopoe/work/<project-id>/health/<run-id>/` | Normalize snapshots and trends per the `vibing-with-ntm` cadence; health/coverage jobs run in dedicated worktrees (Guardrail 5), not the active agent tree. |
| Safety approvals | NTM/DCG/SLB plus Hoopoe approvals queue | Destructive or privileged actions are typed, audited, and checkpointed. DCG verdicts are ingested into the unified queue, not run as a parallel guard. |
| LLM credentials | CAAM | No provider API keys or direct provider SDKs are Hoopoe configuration (Guardrail 11). |
| Subscription-usage telemetry | `caut` (per-provider quota), `rano` (per-call latency/error), per-CLI status messages, NTM events | Hoopoe displays quota signals and launch warnings; if `caut` is unavailable the UI says "unmeasured" rather than displaying a fake estimate. |

### Tool-only canonical owners

These plan §1.1 domains are intentionally omitted from the table above because
they do not own a persistent path inside the Hoopoe checkout — their canonical
state lives in the named tool, and Hoopoe wraps without mirroring. See
`plan.md §1.1` for the full per-row contract.

| Domain | Canonical tool | Why omitted here |
| --- | --- | --- |
| Swarm tending methodology | `ntm` + `vibing-with-ntm` skills | Methodology spec loaded into tending agents at runtime; not reimplemented in Go and not persisted by Hoopoe. |
| Agent skill installation/updates | `jsm` (preferred) / `jfp` (fallback) | Skill cache is owned by the installer tool; Hoopoe pins by SHA-256 and never reimplements fetch/cache. |
| Destructive-command policy | `DCG` | Policy lives in DCG; verdicts are ingested as approval-source events into the unified queue (already covered by the **Safety approvals** row). |
| Two-person rule (high-risk) | `SLB` | Co-signature state belongs to SLB under the `autopilot` preset; Hoopoe's approvals UI reflects but never bypasses it. |
| ChatGPT Pro web reach | `oracle --engine browser` | Browser session state lives in Oracle's profile; Hoopoe shells out via `oracle serve` / `--remote-host`. |
| Session resumption across CLIs | `casr` | Session conversion state belongs to `casr`; Hoopoe invokes it as a recovery action. |
| System resource health | `srp`, `sbh`, `pt` | Disk/CPU/load actuator state belongs to the named tools; Hoopoe consumes their signals via the `watch-safety-thresholds` pre-script. |
| Bug scanning & code review tools | `UBS` plus specialized audit skills | Findings flow into the §9.3 finding ledger (per-review-round artifact); the source tool stamps each finding for cross-tool deduping. |
| Plan refinement automation | Hoopoe's §7.1 planning pipeline (in-house); `apr` is **not** Hoopoe's planning backend | Planning artifacts share the `.hoopoe/plans/<plan-id>/` path already covered by the **Plans** row above; the methodology itself is not a separate persistent owner. |
| Planning model execution | Subscription-backed CLIs (Claude Code / Codex / Gemini) plus `oracle` for ChatGPT Pro | Subscription credentials live in CAAM; reach state belongs to the CLI/Oracle, never direct provider APIs (Guardrail 11). |

## Persistent Paths

| Path | Owner | Contents | Canonical? |
| --- | --- | --- | --- |
| `/data/projects/<project-id>/` | VPS daemon / agents | Active project checkout where agents edit, test, commit, and push. | Working state only. |
| `~/.hoopoe/work/<project-id>/` | VPS daemon | Parent directory for all isolated job worktrees (health, review, etc.) — never under the active checkout, per Guardrail 5. | No, derived from origin/VPS work. |
| `~/.hoopoe/daemon.db` | VPS daemon | Hoopoe job registry, event read models, onboarding state, preferences, and caches. | Canonical only for Hoopoe-owned state. |
| `~/.hoopoe/audit.jsonl` | VPS daemon | Append-only audit events for actions, approvals, jobs, and safety decisions. | Canonical audit log. |
| `~/.hoopoe/logs/` | VPS daemon | Structured daemon/bootstrap logs. | Diagnostic evidence. |
| `~/.hoopoe/work/<project-id>/health/<run-id>/` | VPS daemon | Isolated health/coverage worktrees per Guardrail 5. | Disposable. |
| `~/Library/Application Support/Hoopoe/projects/<project-id>/repo/` | Desktop | Sync-driven local clone fetched from origin. | Read-only mirror. |
| `~/Library/Application Support/Hoopoe/client-settings.json` | Desktop | Non-secret UI/client settings and profile references. | Desktop preference state. |
| macOS Keychain / `safeStorage` | Desktop | SSH passphrases, bearer material, and local secrets. | Secret store. |

## Reconciliation Rules

- Canonical tool state wins over Hoopoe caches. If `br`, Git, NTM, Agent Mail,
  or a test evidence file disagrees with a Hoopoe read model, refresh the read
  model and mark the stale surface degraded until refresh completes.
- The desktop local clone is never a write target. Any staging, committing,
  branching, merging, or pushing request goes to the daemon and runs on the VPS.
  Resetting, cleaning, checking out branches, or deleting mirror contents is
  also forbidden from Hoopoe. The retired "Discard local changes" channel
  validates project state, emits audit, and refuses with a read-only error;
  users may inspect the mirror in Finder, but Hoopoe never performs write-repair
  inside the desktop mirror. If the mirror is corrupt or dirty, recovery is to
  abandon/recreate the mirror and fetch fresh bytes from origin, or to surface
  the problem for manual operator action outside Hoopoe.
- Health and coverage jobs do not run in the active agent working tree by
  default. They use dedicated worktrees under `~/.hoopoe/work/...`.
- Terminal output is observability, not truth. Prefer structured robot/API
  surfaces and persisted job logs with sequence cursors.
- Every reconciliation action emits an audit entry with actor, source,
  canonical snapshot, decision, and resulting cache version.

## Cross-References

- `plan.md §1.1` — native sources of truth.
- `plan.md §2.2` — daemon-owned state vs native tool state.
- `plan.md §7.7` — desktop local clone contract.
- `docs/security.md` — approval matrix and audit schema.
- `docs/process-manager.md` — job registry and process invariants.
- `docs/reconnect-replay.md` — replay and sequence-cursor behavior.
