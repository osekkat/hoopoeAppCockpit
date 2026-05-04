# `rch` integration contract

> Remote Compilation Helper for build/test offload. Preferred when configured;
> build queues must fall back cleanly to direct execution when `rch.run` is
> missing or degraded.

## Source of truth

| Field          | Value                                  |
| -------------- | -------------------------------------- |
| Tool           | `rch`                                  |
| Repo           | TBD (operator-installed ACFS utility)  |
| Observed       | `rch exec -- ...` in Phase 2 swarm use |
| Min compatible | `1.0.16+`                              |

## Adapter precedence

1. `rch exec -- <argv...>` — direct argv execution, no shell.
2. `rch --version` — read-only capability probe.
3. Direct build/test execution — fallback when `rch.run` is unavailable.

## Capability IDs

| capId     | Required by              | Surface          | Notes                                      |
| --------- | ------------------------ | ---------------- | ------------------------------------------ |
| `rch.run` | Build/test queue, swarm tending | `rch exec --` | Missing/degraded falls back to direct runner |

## Command contract

The daemon adapter constructs argv as:

```text
rch exec -- <command> <args...>
```

It rejects shell interpreters as the command head (`sh`, `bash`, `zsh`,
`fish`, `dash`) and rejects control characters in argv. Build queue callers pass
typed command arrays; the adapter never receives or evaluates a command string.

The adapter sets deterministic environment defaults:

- `RCH_VISIBILITY=summary`
- `NO_COLOR=1`
- `LC_ALL=C`
- `LANG=C`

Caller-provided environment is sorted before digesting and before invocation.

## Result shape

Each run records:

- project ID, worktree path, branch, commit SHA
- original command and normalized `rch exec --` argv
- environment digest
- runner profile and worker target
- started/completed timestamps, duration, exit code
- bounded stdout/stderr artifacts
- parsed `[RCH] ...` summary when present
- failure fingerprint for non-zero exits

Non-zero build/test exits are returned as results, not adapter errors. Adapter
errors are reserved for invalid requests, missing binary, failed process spawn,
and command-contract violations.

## Failure modes & recovery

| Symptom                       | Root cause                      | Hoopoe response                                      |
| ----------------------------- | ------------------------------- | ---------------------------------------------------- |
| `rch` binary missing          | RCH not installed/configured    | Report `rch.run=missing`; direct runner may execute. |
| `[RCH] local (...)` summary    | Hook disabled / policy fallback | Mark result local; keep direct fallback visible.     |
| `RCH-E*` failure code          | Worker/sync/storage failure     | Stamp result summary; Diagnostics surfaces code.     |
| Build command exits non-zero   | Real test/build failure         | Record failure fingerprint; do not retry blindly.    |
| Very large output              | Verbose test/build logs         | Bound stdout/stderr; set `outputTruncated=true`.     |

## Authentication / credentials

None in Hoopoe. RCH owns its SSH worker credentials and configuration. Hoopoe
only invokes the local `rch` CLI.

## Adapter notes

- Go package: `apps/daemon/internal/adapters/rch/`.
- Capability tool id: `rch`, capability id: `rch.run`.
- The future build queue (`hp-977`) decides whether project policy allows rch.
  This adapter only provides a deterministic execution surface and normalized
  result metadata.
