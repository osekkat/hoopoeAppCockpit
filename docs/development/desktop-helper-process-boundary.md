# Desktop Helper-Process Boundary

Hoopoe's durable project work belongs to the VPS daemon, NTM, and the native
Flywheel tools. The desktop may start a short list of local helper processes,
but those helpers do not make Electron the orchestrator of record.

This note clarifies the exception to `plan.md §1.2` and Guardrail 2:
desktop-owned helper processes are allowed only when they are local
bootstrap, connection, read-only mirror, or macOS UX support. Project jobs,
swarm launches, tending jobs, review rounds, build/test commands, and mutating
bead/Git operations remain daemon-owned.

## Allowed Categories

| Category | Allowed desktop surface | Purpose | Boundary |
| --- | --- | --- | --- |
| Daemon bootstrap / local demo | `apps/desktop/src/main/BackendLifecycle.ts` | Start the Hoopoe Go daemon binary for local development, packaged desktop bootstrap, or Mock Flywheel/local-demo flows. | The spawned daemon owns its own HTTP/job API. Electron is a parent process, not the job registry. |
| SSH tunnel and network signals | `apps/desktop/electron/tunnel/**` | Maintain the local SSH tunnel and observe macOS network route/SSID/VPN changes. | Tunnel lifecycle only; no project command execution. |
| SSH key generation | `apps/desktop/src/main/SshKeyService.ts` | Generate/list user SSH keys with explicit `ssh-keygen` argv. | Key material setup only; no project jobs. |
| Desktop origin mirror Git | `apps/desktop/electron/clone/**` | Clone/fetch/read the desktop's sync-driven origin mirror. | Origin mirror is read-only for Hoopoe. Mutating Git commands are rejected by the clone wrapper. |
| Project readiness local UX | `apps/desktop/electron/projects/lifecycle.ts` | Probe Git origin/branch and run `br init` during project import when `.beads/` is missing. | Bootstrap check only; bead mutations after import go through daemon/tool contracts. |
| macOS power assertion | `apps/desktop/src/main/macPowerAssert.ts` | Keep the laptop awake during local Oracle/browser or demo flows using Electron/NSProcessInfo/caffeinate fallback. | Local power-management only; no project state authority. |

Tests and test utilities may spawn local fixtures or harnesses, but production
desktop code must fit one of the categories above.

## Forbidden Desktop-Owned Work

The desktop must not directly start or supervise:

- NTM or tmux sessions for real swarms.
- `rch`, language build/test runners, or review/health jobs.
- Tending scheduler runs or agent recovery actions.
- `br`/`bv` mutations after project bootstrap.
- mutating Git operations against either the VPS working tree or the desktop
  origin mirror.
- arbitrary shell commands assembled from renderer or project input.

Renderer code remains stricter: it cannot import `child_process`, `fs`, `net`,
or Electron directly. Any helper process above is main-process/electron-side
only and exposed through typed, allowlisted IPC.

## BackendLifecycle Semantics

`BackendLifecycle.spawnBackend` starts the Hoopoe daemon binary and waits for
HTTP readiness. In production this is a bootstrap/packaging concern: the
daemon process is the durable owner of `/v1/jobs`, tending routes, process
tracking, audit, idempotency, and restart recovery.

Local demo uses the same parent-child process relationship, but the demo daemon
serves fixture-backed Mock Flywheel state. It must not be mistaken for
Electron owning swarm or tending work. If a future feature needs to create a
job, launch a swarm, run tests, or execute a tending action, it should add a
typed daemon route or ActionPlan executor path rather than adding another
desktop child process.

## Enforcement

`scripts/desktop-helper-boundary/check-desktop-helper-boundary.ts` scans
production desktop source under `apps/desktop/src` and
`apps/desktop/electron`. It fails if a process-launch command appears outside
the approved helper modules, or if an approved helper starts a command outside
its category. Test files and test utilities are excluded.

The gate is wired into root `bun run lint` and `bun run test` as
`lint:desktop-helper-boundary` and `test:desktop-helper-boundary`.
