# `@hoopoe/daemon`

Hoopoe's VPS-side daemon — a Go binary that runs on the user's VPS and
exposes a typed API facade over the Agentic Coding Flywheel toolchain
(`br`, `bv`, `ntm`, Agent Mail, `git`, `rch`, ACFS, CAAM, DCG, ...).

## Status

Pre-Phase-1 scaffold (hp-xru). The daemon is **greenfield Go** — auth,
settings, and protocol *shapes* are taken from t3code (`plan.md` Appendix B)
but the implementation is ours. Phase 2 lands the HTTP/WS scaffolding,
sequence-cursor event stream, bootstrap and session credentials, and the
systemd `Type=notify` integration.

## Build

```bash
cd apps/daemon
go build ./...
go vet ./...
go test ./...
```

`bun run build` / `bun run typecheck` / `bun run test` at the project root
proxy to these via Turbo and the thin `package.json` in this directory, so
the workspace presents a uniform task surface.

## Why Go (per `plan.md §3`)

- Static cross-compiled binary — single-file deploy over SSH; no native
  module rebuild on the target.
- `Type=notify` systemd integration via `sd_notify`.
- Strong concurrency primitives for goroutine-per-PTY-stream + WebSocket
  fanout.
- No `node_modules` competing with the user's project for VPS inodes/disk.
- Lower baseline memory than Node/Bun for long-lived processes.
- Mature production debugging (`pprof`/`delve`/`strace`).
- Same family as **NTM** (also Go), kubelet, containerd, Tailscale, Caddy:
  long-lived control-plane daemons multiplexing subprocesses + exposing
  HTTP/WS on Linux servers.

## What this daemon is not

- **Not a new canonical database.** Bead truth lives in `br`, NTM session
  truth in `ntm`, Agent Mail truth in Agent Mail, Git truth in origin and
  the VPS clone. The daemon owns Hoopoe's job state, event log, read-model
  cache, plan metadata, onboarding state, health snapshots Hoopoe
  generates, and audit events — **and reconciles its cache against
  canonical state on every read** (`plan.md §1.1`, `§2.2`).
- **Not the renderer's shell.** The daemon never exposes arbitrary shell
  execution — all project-level commands flow through typed RPCs
  (`Guardrail 2`).
