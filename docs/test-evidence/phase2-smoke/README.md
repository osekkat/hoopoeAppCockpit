# Phase 2 Smoke Evidence

`hp-awh` adds the daemon-side Mock Flywheel equivalent of the Phase 2
acceptance smoke:

```bash
cd apps/daemon
rch exec -- go test ./internal/acceptance/...
```

The suite emits a JSON-serializable report with five acceptance steps:

- cold daemon bootstrap to pairing, bearer, and WS token;
- tool-inventory job log streaming with byte-offset resume;
- disconnect/reconnect event replay with ordered, idempotent merge;
- macOS sleep/wake reconnect p95 under the configured SLO;
- daemon restart recovery for interrupted jobs while the bearer remains usable.

Real research-spike VPS runs should drop host-specific JSONL or JSON reports in
this directory using one subdirectory per run ID.
