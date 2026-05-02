# `git` integration contract

> Canonical source of truth for code (`plan.md` §1.1). The desktop reads its local sync-mirror clone (`§7.7`); all writes go through the daemon's GitAdapter (`§5.3`). The local clone is **never** a write target (Guardrail 3).

## Source of truth

| Field    | Value                                                         |
| -------- | ------------------------------------------------------------- |
| Tool     | git                                                           |
| Repo     | <https://git-scm.com/>                                        |
| Observed | `git version 2.51.0` (research-spike 2026-05-02 dev box)      |
| Min compatible | 2.30+ (worktree, sparse-checkout v2)                    |
| Docs     | <https://git-scm.com/docs>                                    |

## Adapter precedence (per `plan.md` §2.3)

1. **Daemon-side, libgit2-or-shell** for status/diff/push/log/worktree on the VPS clones at `/data/projects/<project>/`. Hoopoe ships shell-out by default; libgit2 is post-v1 if perf demands.
2. **Desktop-side, shell** for the read-only sync mirror at `~/Library/Application Support/Hoopoe/projects/<project-id>/repo/`. Reads only — never `git add`, `git commit`, `git push` from here (Guardrail 3).
3. **Origin** is canonical (`§1.1`): GitHub / GitLab / etc. The desktop's local clone fetches from origin, **not** from the VPS clone.

## Capability IDs (per `plan.md` §2.8)

| capId                  | Required by                              | Surface                                                      | Failure mode                                       |
| ---------------------- | ---------------------------------------- | ------------------------------------------------------------ | -------------------------------------------------- |
| `git.status.read`      | Top-bar branch indicator, Bead drawer    | `git status --porcelain=v2 --branch`                         | Missing → `degraded`; lock contention → retry      |
| `git.diff.read`        | Bead drawer "files" tab                  | `git diff --stat HEAD`, `git diff --stat --cached`           | Large diffs truncated at the daemon                |
| `git.unpushed.list`    | Top-bar VPS WIP overlay (`§7.7`)         | `git log --branches --not --remotes --pretty=...`            | Empty result is normal                              |
| `git.push`             | Auto-push hook (`§7.3`); rare daemon RPC | `git push origin <branch>` (with `-u` first time)            | Auth failure → audit + Activity panel notification |
| `git.commit`           | Daemon-side only (never desktop)         | `git commit -m '[<bead>] ...'`                               | Pre-commit hook failure → leave WIP, surface       |
| `git.fetch.origin`     | Local clone sync (`§7.7`); daemon clone  | `git fetch origin --prune`                                   | Network → backoff; auth → re-prompt                |
| `git.worktree.create`  | Health jobs (`§7.4.1`); review rounds    | `git worktree add ~/.hoopoe/work/<project>/health/<run>/`    | Out-of-disk → fail, surface in Activity            |
| `git.worktree.remove`  | Worktree cleanup                         | `git worktree remove --force`                                | Stale lock → `prune` then `remove`                 |
| `git.config.get`       | Audit attribution, signing config        | `git config --get user.email`, `--get user.signingkey`       | Missing → fall back to system identity              |

## Command surfaces (observed)

### Read-side (daemon + desktop sync mirror)

| Label                  | argv                                                              | Exit | Notes                                                                  |
| ---------------------- | ----------------------------------------------------------------- | ---- | ---------------------------------------------------------------------- |
| `status_porcelain`     | `git status --porcelain=v2 --branch`                              | 0    | Stable parser format. Newlines + `# branch.head <name>`.               |
| `branch_show`          | `git branch --show-current`                                       | 0    | Empty stdout when in detached-HEAD; check `rev-parse HEAD`.            |
| `head_sha`             | `git rev-parse HEAD`                                              | 0/128| 128 when not a repo or repo empty.                                     |
| `log_short`            | `git log -n 10 --pretty=format:'%H %ci %s'`                       | 0    | ISO 8601 commit dates. No trailing newline → handle EOF.               |
| `remote`               | `git remote -v`                                                   | 0    | Two lines per remote (`fetch` + `push`).                               |
| `diff_stat`            | `git diff --stat HEAD`                                            | 0    | Empty stdout when clean.                                               |
| `diff_staged_stat`     | `git diff --stat --cached`                                        | 0    | Same as above.                                                         |
| `unpushed`             | `git log --branches --not --remotes --pretty=format:'%H %s'`      | 0    | Empty stdout when nothing unpushed.                                    |

### Write-side (daemon only — never invoked by desktop)

| Label                  | argv                                                              | Exit | Notes                                                                  |
| ---------------------- | ----------------------------------------------------------------- | ---- | ---------------------------------------------------------------------- |
| `commit`               | `git commit --no-edit -m '<msg>'`                                 | 0    | `-m` is HEREDOC for multiline. Bead ID required in subject.            |
| `push_branch`          | `git push origin HEAD`                                            | 0    | First push uses `-u origin <branch>`. See `§7.3` push policy.          |
| `worktree_add`         | `git worktree add <path> <ref>`                                   | 0    | `<path>` under `~/.hoopoe/work/<project>/`; never inside repo.         |
| `worktree_remove`      | `git worktree remove --force <path>`                              | 0    | Idempotent if `prune` first.                                           |
| `fetch`                | `git fetch origin --prune`                                        | 0    | Used by clone-sync subsystem (`hp-ind`).                               |

## Failure modes & recovery

| Symptom                                                  | Root cause                                          | Hoopoe response                                                                         |
| -------------------------------------------------------- | --------------------------------------------------- | --------------------------------------------------------------------------------------- |
| `fatal: not a git repository`                            | Path mis-resolved, repo not yet cloned              | Adapter reports `git.status.read` missing; UI shows "not a git repo" badge.             |
| `fatal: unable to access 'origin/...': could not resolve host` | Network                                          | Backoff + retry; surface in Activity panel after 3 retries.                             |
| `error: failed to push some refs to 'origin/...'`        | Stale tip; needs rebase or force                    | **Never auto-force.** Surface to user; require approval (`plan.md` §5.3 approvals queue). |
| `Pre-commit hook failed`                                 | Local hook rejection (e.g. lint)                    | Leave WIP. Surface to Activity panel + bead drawer; wake the orchestrator-chat agent.   |
| `index.lock` exists                                      | Concurrent git operation                            | Wait + retry up to 5s; if still locked, surface diagnostic.                             |
| Diff > 10 MB                                             | Massive WIP (binary, generated)                     | Truncate at the daemon; offer "open in editor" deep-link instead.                       |

## Authentication / credentials

- **SSH keys.** Daemon uses the VPS user's `~/.ssh/id_ed25519` for `origin`. Desktop's sync mirror uses the user's macOS SSH agent.
- **HTTPS PAT.** Falls back to git's credential helper (libsecret on Linux daemon; macOS Keychain on desktop).
- **Signing.** `user.signingkey` honored; daemon does **not** mutate signing config.
- No CAAM involvement (git is not a model provider).

## Known gotchas

See [`../research-spike/gotchas.md`](../research-spike/gotchas.md) (`hp-d54`) for the structured catalog. Highlights so far:

- `git status --porcelain=v2 --branch` and `--porcelain=v1` parse incompatibly. Adapters must pin v2.
- `git log` with no commits returns exit 128 (not 0 with empty stdout). Probe `rev-parse HEAD` first.
- `git push -u origin HEAD` with no upstream prints to **stderr** even on success. Don't treat stderr-non-empty as failure.

## Test fixtures

| Scenario | Fixture path                                                        | What it asserts                                              |
| -------- | ------------------------------------------------------------------- | ------------------------------------------------------------ |
| `fresh`  | `packages/fixtures/phase0-2026-05-02/scenarios/fresh/snapshot.json` | Clean WIP, no unpushed, single remote                        |
| `active` | `packages/fixtures/phase0-.../scenarios/active/snapshot.json`       | 3+ unpushed commits, dirty WIP, mid-rebase ok                |
| `failure`| `packages/fixtures/phase0-.../scenarios/failure/snapshot.json`      | `index.lock` present, `fatal: not a git repository` captured |

Adapter contract tests (`plan.md` §18.3) consume these via the snapshot envelope; the parser MUST validate against `scripts/research-spike/schema/snapshot.schema.json`.

## Adapter notes (Hoopoe Go side)

- Lives at `apps/daemon/internal/adapters/git/` (Phase 4, bead `hp-w8m`, `hp-zks`).
- Long operations (`fetch`, `push`) run on the daemon's job runner (`§2.7`, `hp-gkk`); short reads are inline.
- Idempotency keys on push (Idempotency-Key header at the daemon RPC layer).
- Redaction (per `plan.md` §5.1, `hp-je1p`): scrub auth headers from `git fetch --verbose` stderr before logging.
- Worktrees for health jobs: `~/.hoopoe/work/<project>/health/<run-id>/` per Guardrail 5.
