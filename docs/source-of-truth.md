# Source Of Truth

`plan.md §1.1` is the authority for Hoopoe's source-of-truth boundary. This
file is the code-near reference for persistent data paths and reconciliation
rules. It exists because `plan.md` Appendix A moved the data-layout detail here.

## Canonical Owners

| Domain | Canonical source | Hoopoe behavior |
| --- | --- | --- |
| Code, branches, tags | Git origin | Desktop mirrors origin read-only; daemon drives VPS-side git actions. |
| VPS working state | `/data/projects/<project-id>/repo/` | Agents and daemon actions operate here before pushing. |
| Desktop code mirror | `~/Library/Application Support/Hoopoe/projects/<project-id>/repo/` | Read-only sync mirror of origin for fast UI reads, diffs, blame, and ripgrep. |
| Plans | `.hoopoe/plans/<plan-id>/` in the repo | Locked plans become the basis for bead conversion and traceability. |
| Beads | `br` and `.beads/` | Hoopoe reads/writes through typed daemon RPCs; `bv --robot-*` owns graph intelligence. |
| Swarm sessions | NTM + tmux | Hoopoe renders session state; it does not replace NTM. |
| Agent messages and reservations | Agent Mail | The Activity panel mirrors Agent Mail threads and reservation state. |
| Build/test evidence | `rch`, runner envelopes, language-native test reports | Hoopoe stores evidence pointers and snapshots, not a parallel result authority. |
| Safety approvals | NTM/DCG/SLB plus Hoopoe approvals queue | Destructive or privileged actions are typed, audited, and checkpointed. |
| LLM credentials | CAAM | No provider API keys or direct provider SDKs are Hoopoe configuration. |
| Subscription usage | `caut` | Hoopoe displays quota signals and launch warnings. |

## Persistent Paths

| Path | Owner | Contents | Canonical? |
| --- | --- | --- | --- |
| `/data/projects/<project-id>/repo/` | VPS daemon / agents | Active project checkout where agents edit, test, commit, and push. | Working state only. |
| `/data/projects/<project-id>/worktrees/` | VPS daemon | Dedicated worktrees for health, review, and isolated jobs. | No, derived from origin/VPS work. |
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
  users may inspect the mirror in Finder, but Hoopoe repair/write flows target
  the VPS clone or origin-derived sync state only.
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
