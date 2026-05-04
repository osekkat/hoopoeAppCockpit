# Hoopoe Risk Register

This file maps the plan.md Section 14 named risks to concrete mitigation
owners and acceptance evidence. The daemon package
`apps/daemon/internal/risks` is the release-smoke source of truth: its tests
fail when a risk loses an owner, bead reference, mitigation, or evidence path.

| ID | Risk | Owner | Mitigation evidence | Beads | Acceptance evidence |
| --- | --- | --- | --- | --- | --- |
| RISK-01 | PTY streaming fidelity fails | daemon/process | Prefer NTM robot/stream surfaces before raw pane capture. Treat terminal output as observability, not canonical state. | hp-gkk, hp-2qn | `apps/daemon/internal/process/manager_test.go`; `apps/daemon/internal/jobs/log/store_test.go` |
| RISK-02 | Tool output drift breaks adapters | daemon/adapters | Prefer robot/API/JSON surfaces and report missing, unsupported, malformed, timeout, and high-volume output through capabilities. | hp-r33, hp-dz8, hp-g1j, hp-yqs | `apps/daemon/internal/adapters/br/br_test.go`; `apps/daemon/internal/capabilities/contract_test.go` |
| RISK-03 | Hoopoe cache diverges from canonical state | daemon/projects | Canonical tool state wins; stale or missing canonical facts block gates and sync surfaces. | hp-2n1, hp-ilt, hp-r33 | `apps/daemon/internal/clone/sync/sync_test.go`; `apps/daemon/internal/projects/gates/gates_test.go` |
| RISK-04 | First install is brittle | daemon/onboarding | Existing-VPS first, checkpointed bootstrap, raw-log fallback, repair hints, and daemon upgrade rollback. | hp-qq9, hp-wa3, hp-4iz | `apps/daemon/internal/onboarding/acfs/parser_test.go`; `apps/daemon/internal/onboarding/checkpoints/checkpoints_test.go`; `apps/daemon/internal/upgrade/service_test.go` |
| RISK-05 | Subscription rate-limits exhaust mid-swarm | daemon/inventory | Use caut for subscription usage, CAAM for account state, and inventory warnings without provider API-key accounting. | hp-0ug, hp-g1j, hp-xr8 | `apps/daemon/internal/adapters/caut/client_test.go`; `apps/daemon/internal/adapters/caam/client_test.go`; `apps/daemon/internal/inventory/service_test.go` |
| RISK-06 | Agents compete for builds/tests | daemon/scheduler | Prefer rch, keep scheduler runs inspectable, and make cleanup/disk pressure explicit. | hp-5s4, hp-6yw, hp-4ya | `apps/daemon/internal/adapters/rch/rch_test.go`; `apps/daemon/internal/scheduler/scheduler_test.go`; `apps/daemon/internal/tending/worktreecleanup/cleanup_test.go` |
| RISK-07 | Stale agents hold beads/reservations hostage | daemon/tending | Agent Mail remains the reservation source of truth; typed action plans handle status prompts and force-release policy with audit. | hp-0d7, hp-209, hp-5s4 | `apps/daemon/internal/adapters/agentmail/client_test.go`; `apps/daemon/internal/tending/prescript/runner_test.go`; `packages/tending-actions/test/loader.test.ts` |
| RISK-08 | Unsafe commands accidentally exposed | daemon/security | Mutations use typed action specs, allowlists, sandboxing, approvals, DCG/SLB, and audit. | hp-209, hp-ki3h, hp-dmoj, hp-rcm, hp-kuh | `apps/daemon/internal/agent/executor_test.go`; `apps/daemon/internal/security/privsep/privsep_test.go`; `apps/daemon/internal/clone/sandbox/sandbox_test.go` |
| RISK-09 | Planning quality is weak | daemon/beadflow | Plan-to-bead traceability, quality dimensions, and lock/conversion gates make planning gaps visible before swarm launch. | hp-3ab, hp-al4, hp-ktn | `apps/daemon/internal/beadflow/beadflow_test.go`; `apps/daemon/internal/beadflow/conversion_test.go`; `apps/daemon/internal/projects/gates/gates_test.go` |
| RISK-10 | Users trust subjective scores too much | daemon/review | Scores stay paired with findings, evidence, documented exceptions, follow-up beads, and override paths. | hp-sqd, hp-r3l, hp-mzg | `apps/daemon/internal/beadflow/quality.go`; `apps/daemon/internal/convergence/convergence_test.go`; `apps/daemon/internal/review/review_test.go` |
| RISK-11 | Laptop sleep breaks perception of reliability | daemon/transport | The VPS daemon owns jobs and replayable event streams; reconnect relies on sequence cursors and snapshots. | hp-e7k, hp-2qn, hp-gkk | `apps/daemon/internal/transport/server_test.go`; `apps/daemon/internal/chaos/scenarios_test.go`; `docs/reconnect-replay.md` |
| RISK-12 | Lifted code carries Codex-shaped assumptions | desktop/source-provenance | Vendored t3code stays isolated, Hoopoe-owned wrappers adapt it, and Codex/thread/provider/chat assumptions are scrubbed. | hp-1.5, hp-dtx | `scripts/codex-shape-scrub/check-codex-shape-scrub.test.ts`; `apps/desktop/tests/smoke/t3code-lift-smoke.test.ts`; `docs/source-provenance.md` |
| RISK-13 | Upstream t3code drift | desktop/source-provenance | Vendored source is pinned; security-relevant upstream changes are reviewed and cherry-picked deliberately. | hp-1.5 | `docs/source-provenance.md`; `apps/desktop/src/vendored/t3code/README.md`; `apps/desktop/src/vendored/t3code/LICENSE` |
| RISK-14 | PubSub.unbounded patterns leak through | daemon/transport | Daemon channels are bounded by design and covered by burst, slow-consumer, and wedged-consumer load tests. | hp-q3p | `apps/daemon/internal/transport/load/eventhub_burst_test.go`; `apps/daemon/internal/transport/load/eventhub_slow_consumer_test.go`; `apps/daemon/internal/transport/load/eventhub_wedged_consumer_test.go` |

## Release Smoke

Run:

```bash
rch exec -- go test ./internal/risks/...
```

The release smoke enforces:

- exactly 14 risks matching plan.md Section 14;
- one owner per risk;
- at least one mitigation, bead reference, and acceptance evidence path per risk;
- all referenced evidence paths exist in the repository;
- this document mirrors every risk ID, title, owner, and bead reference.
