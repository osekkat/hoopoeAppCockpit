# `internal/transport/load` — Phase 2.5 load tests (hp-q3p)

Bounded-channel + slow-consumer + bounded-semaphore stress tests
covering Appendix B anti-pattern #1 (`PubSub.unbounded` everywhere)
and §10.1 backpressure invariants.

## Why a separate package

Normal `go test ./...` should not pay the cost of these load tests
(they take seconds-to-minutes to drive the channels through their
rate-limit / overflow / cancellation paths). Each test calls
`testing.Short()` and skips when set, so:

```bash
# Normal CI lane (load tests skipped):
go test -short ./...

# hp-q3p invocation (per the bead):
rch exec -- go test ./internal/transport/load/...

# Local long-form:
go test -timeout 5m ./internal/transport/load/...
```

## What's covered

| File | Scenario | Asserts |
|---|---|---|
| `eventhub_slow_consumer_test.go` | High-volume publish, slow consumer (10k events into 16-slot subscriber buffer) | subscriber channel depth ≤ subscriberCapacity at all times; `_lag` markers emitted; Replay() closes the cursor gap. |
| `eventhub_burst_test.go` | 10k publish + fast consumer | monotonic in-order delivery; no `_lag` (sanity); throughput floor. |
| `eventhub_wedged_consumer_test.go` | Subscribe + never read | publisher does NOT block on a wedged subscriber; subscriber channel stays bounded; ctx cancellation cleans up. |
| `semaphore_stress_test.go` | 200 goroutines × limit-of-4 ResourceLimiter; plus context cancellation while blocked | simultaneous holders never exceed limit; cancelled acquire returns ctx.Err() promptly; InUse() reports bounded values; no goroutine leaks. |

## Cross-references

- `apps/daemon/internal/api/events.go` — `EventHub`, `Subscriber`,
  `subscriber.deliver` (the bounded-channel path under test).
- `apps/daemon/internal/jobs/limits.go` — `ResourceLimiter`.
- `apps/daemon/internal/chaos/scenarios.go` — qualitative chaos
  scenarios (tunnel drop, daemon restart, slow renderer, etc.).
  These load tests are the **quantitative** complement: chaos
  scenarios prove the daemon *can* recover from a fault; these tests
  prove it stays bounded under volume.
