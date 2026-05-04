# Getting Started

This document gets a contributor from a fresh checkout to useful verification
commands. `AGENTS.md` is still required reading for AI agents before editing.

## Prerequisites

- Go for the daemon.
- Bun for desktop/packages.
- `br` and `bv` for issue tracking and graph triage.
- Agent Mail MCP server for reservations and coordination.
- `rch` for shared build/test execution.

The repository is built by the same workflow Hoopoe wraps. Use the native tool
surfaces rather than ad hoc shell parsing.

## Setup

```bash
git clone https://github.com/osekkat/hoopoeAppCockpit.git
cd hoopoeAppCockpit
bun install
```

Read:

1. `AGENTS.md`
2. `README.md`
3. `plan.md`
4. `docs/README.md`

## Try The Cockpit

For a no-VPS walkthrough, use the local demo path in the first-run wizard. It
loads Mock Flywheel fixtures for Planning, Beads, Swarm, Hardening, Activity,
and Diagnostics. After the wizard success screen, the guided tour opens once
and can be skipped, completed, or resumed from Diagnostics.

The demo path is not a second source of truth. It exercises the renderer
against fixture-backed daemon shapes so contributors can inspect the product
flow before pairing a real VPS.

## Work Selection

```bash
CI=1 br ready --json
bv --recipe actionable --robot-plan
```

Claim with `br update <id> --status in_progress --json` when the tracker write
path is healthy. If the tracker refuses a write, do not force it; coordinate in
Agent Mail and record the blocker.

## Verification

Daemon:

```bash
cd apps/daemon
rch exec -- go test ./...
rch exec -- go build ./...
rch exec -- go vet ./...
```

Desktop:

```bash
rch exec -- bun run --cwd apps/desktop typecheck
rch exec -- bun run --cwd apps/desktop test
rch exec -- bun run --cwd apps/desktop build
```

Docs-only changes should still run a cheap repo check and any touched-surface
tests. If daemon docs reference generated Go contracts, run the daemon smoke:

```bash
cd apps/daemon
rch exec -- go test ./...
```

Run UBS on changed supported files before committing:

```bash
ubs <changed files>
```

## Commit And Push

Use bead-prefixed commit messages:

```bash
git add <your files>
git commit -m "[hp-xxxx] terse description"
git push
```

Do not stage unrelated dirty files. This repo is a shared multi-agent worktree.

## Cross-References

- `docs/source-of-truth.md` — where state lives.
- `docs/onboarding.md` — product onboarding flow.
- `docs/wizard.md` — first-run wizard and guided-tour handoff.
- `docs/testing.md` — runner/evidence details.
- `docs/troubleshooting.md` — common recovery paths.
