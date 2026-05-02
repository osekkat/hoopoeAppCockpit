# `br` (beads_rust) integration contract

> Canonical source of truth for **bead state** (issues, deps, status, priority). Never run by Hoopoe except through this adapter; `br` is non-invasive (it never touches git on its own — Hoopoe commits `.beads/issues.jsonl` after `sync --flush-only`).

## Source of truth

| Field    | Value                                                       |
| -------- | ----------------------------------------------------------- |
| Tool     | `br` (beads_rust)                                           |
| Repo     | <https://github.com/Dicklesworthstone/beads_rust>           |
| Observed | `br 0.1.35` (research-spike 2026-05-02)                     |
| Min compatible | 0.1.30+ (`--robot-*` flags assumed; `--json` stable)  |
| Skill    | `beads-workflow` (loaded into Stage 02 agents at runtime)   |

## Adapter precedence (per `plan.md` §2.3)

1. **`br <subcmd> --json`** — primary surface. Emits machine-readable JSON for `list`, `ready`, `stats`, `dep cycles`, `info`, `show`, `search`, `count`, `update`, `close`, `create`.
2. **`br schema`** — JSON Schema definitions for `br`'s output types. Adapter consumers may use this to validate at runtime; not required.
3. **`.beads/issues.jsonl`** raw read — used by `bv` for graph analysis. Hoopoe **does not parse this directly** in the daemon — call `br list --json --limit ...` instead. The file is canonical-on-disk and survives `br` upgrades.
4. **No bare `br`** — `br` without args prints help (no TUI), but the adapter still uses subcommands for clarity and stability.

## Capability IDs (per `plan.md` §2.8)

| capId                  | Required by                              | Surface                                                 | Failure mode                                                       |
| ---------------------- | ---------------------------------------- | ------------------------------------------------------- | ------------------------------------------------------------------ |
| `br.issues.read`       | Beads stage Kanban / DAG, bead drawer    | `br list --status=<x> --json --limit <n>`               | Missing → `degraded` (read fixture or empty list)                  |
| `br.issues.update`     | Status changes, assignment               | `br update <id> --status=<x> --owner=<x>`               | Conflict → re-fetch, surface to user                               |
| `br.dep.add`           | DAG editing                              | `br dep add <issue> <depends-on>`                       | Cycle → 4xx error; refuse                                          |
| `br.dep.cycles`        | Beads polish round                       | `br dep cycles --json`                                  | Empty result is normal                                             |
| `br.ready`             | "Ready frontier" highlight               | `br ready --json`                                       | Empty result is normal                                             |
| `br.create`            | Convert plan → beads (Phase 6)           | `br create --title=... --type=task --priority=2`        | Validation error → surface line/col                                |
| `br.close`             | Bead completion + audit                  | `br close <id> --reason '<short summary>'`              | Already-closed → idempotent                                        |
| `br.sync.flush_only`   | After every mutation                     | `br sync --flush-only`                                  | Always non-invasive (NEVER runs git — Hoopoe is responsible)       |
| `br.audit.read`        | Activity panel timeline (Phase 9)        | `br audit --json --since <iso>`                         | New surface — may not exist on older versions                       |
| `br.schema`            | Adapter validation                       | `br schema`                                             | Output ~63 KB; stream-parse if memory matters                      |
| `br.tui`               | (deliberately unused)                    | `bv` is the TUI surface; `br` itself prints help, no TUI to avoid | `blocked-by-policy` — adapters never invoke bare `br`              |

## Command surfaces (observed)

### Read

| Label                   | argv                                                | Exit | stdout shape                                                                  | Notes                                                                  |
| ----------------------- | --------------------------------------------------- | ---- | ----------------------------------------------------------------------------- | ---------------------------------------------------------------------- |
| `list_open`             | `br list --status=open --json --limit 250`          | 0    | `{ issues: [...], total: <int>, limit: <int>, offset: <int>, has_more: bool }` | 207 issues observed; 862 KB stdout. Page via `--offset`.                |
| `ready`                 | `br ready --json`                                   | 0    | Array of issue objects (top-level array, **not** wrapped in `{issues: ...}`). | **Schema differs from `list`**! Adapters must branch.                  |
| `stats`                 | `br stats --json`                                   | 0    | `{ open, closed, in_progress, ... }`                                          | Stable across versions.                                                |
| `cycles`                | `br dep cycles --json`                              | 0    | `[]` when none; non-empty array of cycle paths otherwise                       | 33 bytes observed in healthy state.                                    |
| `schema`                | `br schema`                                         | 0    | JSON Schema draft document (multi-KB)                                         | 63 KB observed. Not a list of issues — Hoopoe **never** uses for issue data. |
| `info`                  | `br info --json`                                    | 0    | Workspace metadata: db path, jsonl path, version, actor                       | Fast (< 50 ms).                                                        |
| `show`                  | `br show <id>`                                      | 0    | Human-readable; for JSON use `br list --json --filter id=<id>` or read `show --json` if supported | `br show` text-only output is **not** the source of truth — `br list --json` is. |

### Write

| Label                   | argv                                                                                       | Exit | Notes                                                                  |
| ----------------------- | ------------------------------------------------------------------------------------------ | ---- | ---------------------------------------------------------------------- |
| `update_status`         | `br update <id> --status=in_progress`                                                       | 0/4  | Exit 4 with `Validation failed: claim: cannot claim blocked issue: ...` when deps not met. Use `--force` only with explicit user/orchestrator approval. |
| `claim`                 | `br update <id> --status=in_progress --owner=<actor>`                                       | 0/4  | Atomic claim shortcut: `br update <id> --claim`.                       |
| `close`                 | `br close <id> --reason '<short summary>'`                                                  | 0    | Multiple IDs allowed. `--force` for blocked.                           |
| `create`                | `br create --title='...' --type=task --priority=2`                                          | 0    | Returns the new ID on stdout (use `br q` for ID-only output).          |
| `dep_add`               | `br dep add <issue> <depends-on>`                                                           | 0/4  | 4 on cycle.                                                            |
| `sync_flush_only`       | `br sync --flush-only`                                                                      | 0    | Writes `.beads/issues.jsonl`. **Never runs git.** Hoopoe `git add .beads/ && git commit` afterward. |

## Failure modes & recovery

| Symptom                                                  | Root cause                                       | Hoopoe response                                                                  |
| -------------------------------------------------------- | ------------------------------------------------ | -------------------------------------------------------------------------------- |
| `Validation failed: claim: cannot claim blocked issue`   | Dep graph blocks the claim                       | Surface; offer `--force` only with orchestrator/user approval (not autopilot).   |
| `Error: database is locked` / SQLite lock                | Concurrent writers                               | Exponential backoff (max 3 retries); surface if persistent.                       |
| `Error: schema version mismatch`                         | `br` upgrade landed; DB needs migration          | `br doctor --repair` (with approval).                                            |
| `--json` returns empty array (instead of error)          | Filter matched zero rows                          | Treat as empty result, not as failure.                                            |
| `br list --json` truncated                               | Output > daemon `--max-bytes`                    | Page via `--limit` + `--offset`. Never raise `--max-bytes` past the schema cap.  |

## Authentication / credentials

- None. `br` operates on the local `.beads/` directory.
- The actor name (`--actor <x>`) is recorded in the audit trail. Hoopoe sets this to the agent identity (e.g. `FuchsiaStone`) so audit logs are per-agent.

## Known gotchas

See [`../research-spike/gotchas.md`](../research-spike/gotchas.md) (`hp-d54`). Highlights:

- **`br list --json` returns `{issues: [...], total, ...}`; `br ready --json` returns a top-level array.** Easy parser bug.
- `br schema` is a JSON Schema document, **not** issue data. Adapters that confuse the two will appear to "find no issues."
- `br sync --flush-only` does **not** commit to git. Hoopoe must `git add .beads/ && git commit` separately. Forgetting this is the most common drift cause.
- `bv` is the TUI for browsing beads; **never** run bare `bv` in agent sessions (Guardrail 1, see [`bv.md`](bv.md)).

## Test fixtures

| Scenario | Fixture path                                                                | What it asserts                                                |
| -------- | --------------------------------------------------------------------------- | -------------------------------------------------------------- |
| `fresh`  | `packages/fixtures/phase0-2026-05-02/scenarios/fresh/snapshot.json`         | Empty `cycles`, populated `list_open`, healthy `stats`.        |
| `active` | `packages/fixtures/phase0-.../scenarios/active/snapshot.json`               | 1+ in-progress, 1+ unblocked-but-deferred, populated `ready`.  |
| `failure`| `packages/fixtures/phase0-.../scenarios/failure/snapshot.json`              | `database is locked` captured; `dep cycles` non-empty.         |

Adapter contract tests assert that `br.issues.read` capability is `degraded` if the parser succeeds but `total` is missing — capability is not just "did it parse."

## Adapter notes (Hoopoe Go side)

- Lives at `apps/daemon/internal/adapters/br/` (Phase 6, bead `hp-dz8`).
- Two read models maintained in the daemon: (a) hot path = `br list --json` cache invalidated by `br sync --flush-only` events; (b) cold path = `.beads/issues.jsonl` for cold-start.
- `br.audit.read` (when available) feeds the Activity panel timeline; otherwise fall back to inferring audit from the JSONL append log.
- Mutations go through the typed `ActionPlan` (`plan.md` §8.3.1). Direct `br update` from the renderer is forbidden (Guardrail 2).
- The desktop **never** invokes `br` directly. All bead state flows through the daemon's `br` adapter and the WS event channel.
