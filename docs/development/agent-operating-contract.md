# Hoopoe Agent Operating Contract

This document explains the rationale behind the root `AGENTS.md` contract for
agents building Hoopoe itself. The short version is simple: Hoopoe is being
built with the same Agentic Coding Flywheel it intends to wrap, so its own
development workflow must preserve the same sources of truth, coordination
rules, and safety boundaries that the product exposes to users.

The root `AGENTS.md` remains the operational contract. `plan.md` remains the
strategic source of truth. If this document conflicts with either one, fix this
document.

## Product Boundary

Hoopoe is a macOS Electron cockpit for the Agentic Coding Flywheel. It is not
the engine. The VPS remains the execution plane, and the native tools remain
canonical:

- `br` owns bead state.
- `bv --robot-*` owns bead graph intelligence.
- NTM owns swarm sessions.
- Agent Mail owns agent messages and file reservations.
- Git origin owns canonical code.
- The VPS working tree is where agents work before pushing to origin.
- The desktop local clone is a read-only, sync-driven mirror of origin.

The UI has four stages plus one cross-stage Activity panel:

- Stage 01 Planning: create, compare, refine, and lock plans.
- Stage 02 Beads: convert locked plans into traceable, dependency-aware beads.
- Stage 03 Swarm: launch and observe agents through bead and agent state.
- Stage 04 Debugging / Hardening: run health, review, finding, and convergence
  workflows.
- Activity panel: Agent Mail, reservations, urgent events, approvals, and the
  user-to-orchestrator chat surface.

Terminal panes are not part of the default swarm UI. Raw pane output is only for
Diagnostics behind an explicit audited toggle.

## Repository Shape

The current checkout may be earlier than the target monorepo. Agents must read
the on-disk tree before assuming paths exist. The target shape is:

- `apps/desktop/`: Electron, TypeScript, React, Vite, Tailwind.
- `apps/daemon/`: Go daemon on the VPS.
- `packages/schemas/`: OpenAPI, generated TS client, generated Go types.
- `packages/design-system/`: tokens, Storybook, reusable UI components.
- `packages/fixtures/`: Mock Flywheel Mode fixture corpus.
- `docs/`: architecture docs, ADRs, security, process manager, testing.
- `.beads/`: bead state managed by `br`.

Vendored T3 Code files live only under
`apps/desktop/src/vendored/t3code/`, with MIT notices preserved. Adaptations
live outside `vendored/`.

## Toolchain

Use the surface-specific toolchain:

- Desktop: Bun, Turbo, TypeScript, React, Vite, Tailwind, TanStack Router,
  TanStack Query, Zustand.
- Daemon: Go, HTTP/WS, SQLite, process management, typed adapters.
- Shared schemas: OpenAPI as the source of truth, generated clients and types.
- Coordination: Agent Mail for messages and reservations.
- Work selection: `br --json` and `bv --robot-*`.
- Builds and tests: wrap commands with `rch exec --`.
- Review: run `ubs` on changed supported files before commit.

Expected commands once the relevant surface exists:

```bash
rch exec -- bun run typecheck
rch exec -- bun run lint
rch exec -- bun run test
rch exec -- bun run build
```

Daemon work uses:

```bash
cd apps/daemon
rch exec -- go build ./...
rch exec -- go vet ./...
rch exec -- go test ./...
```

Schema work uses:

```bash
rch exec -- bun run --cwd packages/schemas generate
rch exec -- bun run --cwd packages/schemas validate
```

Do not run cleanup scripts or destructive commands to get tests passing. If a
command would delete files, it needs explicit human permission.

## Coordination Rules

Every agent working in this repo must:

1. Register with Agent Mail at session start.
2. Read the inbox and answer ack-required mail before editing.
3. Select work from Phase 0 or Phase 1 unless explicitly assigned otherwise.
4. Claim exactly one bead with `br update <id> --status in_progress`.
5. Reserve the intended edit paths through Agent Mail before editing.
6. Use bead IDs in mail thread IDs, reservation reasons, and commit messages.
7. Announce cross-domain pulls in `hoopoe-intro`.
8. Announce long builds in `hoopoe-builds`.
9. Re-fetch the inbox after meaningful actions.
10. Release reservations when done.

Reservations are advisory, but they are the collision-avoidance protocol. If a
path is already reserved, narrow your scope, coordinate with the holder, or
choose different work.

## Coding Constraints

The most important implementation constraints are:

- Do not delete files without explicit written permission.
- Do not run `git reset --hard`, `git clean -fd`, `rm -rf`, or equivalent
  destructive commands without the exact user-approved command and follow-up
  confirmation.
- Do not parse bare `bv` output. Bare `bv` opens a TUI. Use robot mode only.
- Do not import direct provider SDKs or introduce API-key configuration fields
  for model providers.
- Do not expose arbitrary shell execution from the renderer or normal daemon
  API.
- Do not let the desktop local clone become a write target.
- Do not let local caches become canonical.
- Do not run health or coverage jobs in the active agent working tree by
  default.
- Do not surface raw terminal panes in the default swarm UI.
- Do not edit vendored T3 Code files in place except for permitted mechanical
  mass renames.
- Do not apply script-based source rewrites in this repository.

Hoopoe has no users yet, so avoid compatibility shims for non-existent legacy
behavior. Fix the shape directly when the plan calls for it.

## Per-Bead Loop

Use this loop for every implementation bead:

1. Read `AGENTS.md`, `README.md`, and relevant `plan.md` sections.
2. Fetch Agent Mail inbox.
3. Run robot-mode triage:

   ```bash
   bv --recipe actionable --robot-plan
   bv --robot-triage
   br ready --json
   br list --status=open --json
   ```

4. Pick a Phase 0 or Phase 1 bead in your assigned domain.
5. Claim the bead.
6. Reserve files.
7. Implement the narrowest complete diff.
8. Run `ubs` on changed supported files.
9. Run relevant tests through `rch exec --`.
10. Commit only the files you changed, by explicit path.
11. Close the bead with `br close <id> --reason "..."`
12. Run `br sync --flush-only`.
13. Commit the `.beads/` sync explicitly.
14. Release reservations.
15. Send the completion note on the bead thread.

If the assigned domain is blocked by another Phase 0 or Phase 1 bead, announce
the cross-domain pull before taking adjacent work. If no safe repo edit is
available, draft outside the repo in a clearly named `/tmp/hoopoe-*-draft/`
directory and do not claim ownership of reserved paths.

## Commit Policy

Commits should be small, explicit, and traceable:

- Include the bead ID in the commit message.
- Stage explicit files only. Do not use `git add .` or `git add -A`.
- Do not revert, stash, overwrite, or clean up work you did not make.
- Treat unfamiliar diffs as peer work and work around them.
- Run `br sync --flush-only` after bead status changes and commit the exported
  `.beads/` change intentionally.

The product push policy says agents push promptly to origin when running under
Hoopoe-managed swarms. In this local development session, follow the human's
explicit branch and push instructions.

## Default Skills

Hoopoe development swarms should load skills that match the work:

- `vibing-with-ntm` for swarm tending and intervention behavior.
- `ntm` for NTM tool operation.
- `beads-workflow`, `beads-br`, and `beads-bv` for planning-to-beads and bead
  graph work.
- `agent-mail` for reservations, messages, inboxes, and handoffs.
- `ubs` for bug scanning before commits.

When a skill and `plan.md` disagree on swarm or tending behavior, the skill is
the methodology source for that behavior. Hoopoe-specific safety, audit,
approval, and source-of-truth rules still override generic tool behavior.

## Smoke Expectation

Once the project importer exists, Hoopoe's own importer should assert that this
repository exposes a root `AGENTS.md` and can open the companion contract at
`docs/development/agent-operating-contract.md`. Until that importer lands, this
document serves as the human-readable companion for the current dogfood swarm.
