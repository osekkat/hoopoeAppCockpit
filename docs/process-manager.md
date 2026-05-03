# Daemon Job Registry and Process Manager

This note records the hp-gkk substrate invariants now enforced by
`apps/daemon/internal/jobs` and `apps/daemon/internal/process`. The OpenAPI
schema is authoritative for the wire shape; `jobs.Job.ToSchema` maps the
daemon's internal persisted entity to the generated `schemas.Job` type from
`packages/schemas/go`. These packages are the daemon-owned implementation
boundary used by HTTP handlers, schedulers, and process runners.

## Interfaces

`jobs.Reader` is the narrow read surface for `/v1/jobs`: `List`, `Get`,
`ReadLog`, and `ListArtifacts`.

`jobs.Controller` embeds `Reader` and adds `Cancel` for
`POST /v1/jobs/{id}/cancel`.

`jobs.Registry` is the full daemon substrate used by schedulers and runners:
`Create`, `Lease`, `Heartbeat`, `Complete`, `Fail`, `Interrupt`,
`AttachProcess`, `DetachProcess`, `RecoverInterrupted`, `AppendLog`, and
`AddArtifact`.

The API package should depend on `Reader` or `Controller`, never own job state.

## Persisted Job Entity

Every job records:

- `id`, `kind`, `schemaVersion`, and structured `status`
- `leaseHolder` and `leaseExpiresAt`
- `correlationId`, `causationId`, and `idempotencyKey`
- audit metadata for actor, request ID, reason, and correlation
- optional process reference with PID, PGID, PTY flag, and reattach timestamp
- optional failure fingerprint and artifact references
- created, updated, started, and completed timestamps

The current `FileStore` is a small durable store behind the registry interface.
SQLite can replace that store without changing daemon callers when the migration
runner lands.

## Process Invariants

- A process cannot start without a job ID.
- A job can have at most one tracked process.
- Started children get their own process group.
- Stop sends SIGTERM to the process group, waits for the grace period, then
  escalates to SIGKILL.
- Kill sends SIGKILL to the process group.
- Adopted live processes can be reattached by PID/PGID on daemon restart.

## Restart Invariant

On daemon restart, `RecoverInterrupted` receives the live child-process set from
the process manager:

- running jobs with a matching live process remain running and receive a
  `reattachedAt` stamp;
- running, waiting, or canceling jobs without a live process are marked
  `interrupted` with `process.crashed_recovered` evidence;
- terminal and queued jobs are left unchanged.

This keeps the product invariant true after restart: a running job always has a
live process, or else it has been made visibly interrupted.

## Log and Artifact Boundaries

Logs are byte-addressable. Callers append bytes with `AppendLog` and fetch
bounded chunks with `ReadLog(jobID, offset, limit)`. List responses never embed
terminal output or full logs.

Artifacts are registry references only: kind, URI, digest, and timestamp. The
blob store or artifact retention policy is intentionally separate.

## Tests

The initial unit coverage asserts:

- idempotent create returns the same job for the same key and rejects mismatched
  reuse;
- the internal job entity maps to the generated OpenAPI `schemas.Job` shape;
- lease, heartbeat, complete, and file-backed reload preserve state;
- restart recovery marks missing live processes interrupted;
- restart recovery reattaches live processes from the process manager snapshot;
- chunked log reads honor offsets;
- resource semaphores block at capacity and respect context cancellation;
- process start rejects missing job IDs;
- one process per job is enforced;
- stop terminates a child process group without leaving the child alive;
- process adoption records a live PID/PGID for restart reconciliation.
