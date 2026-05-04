# Hoopoe docs

Architecture references, ADRs, and operational runbooks for the Hoopoe
cockpit. The strategic plan in `plan.md` (project root) is authoritative;
this directory holds the longer-form, code-near references that the plan
points to.

## Inventory

| File | Purpose |
| --- | --- |
| `source-of-truth.md` | Persistent data layout and canonical ownership rules. |
| `getting-started.md` | Contributor setup, work selection, verification, and commit flow. |
| `api-seed.md` | Seed daemon REST/WS contract per `plan.md §2.6`. |
| `process-manager.md` | Job registry, process, log, and artifact invariants per `plan.md §2.7`. |
| `reconnect-replay.md` | Event replay and reconnect rules. |
| `security.md` | Threat model, CommandSpec, approvals, audit log, bind safety, release verification. |
| `onboarding.md` | Project metadata, readiness checks, checkpoints, onboarding modes. |
| `wizard.md` | First-run wizard step contract and failure surfaces. |
| `testing.md` | Runner taxonomy, evidence layout, integration/e2e strategy, smoke checks. |
| `upgrade-and-rollback.md` | Verified daemon upgrade flow and rollback behavior. |
| `troubleshooting.md` | Canonical-state-first recovery runbook. |
| `risks.md` | `plan.md §14` risk evidence matrix. |
| `source-provenance.md` | Pinned external-source SHAs and attribution rules for the t3code lift. |
| `capabilities.md` | Capability registry and degraded-mode contract. |
| `observability.md` | Structured logs, metrics, and visibility contracts. |
| `operations.md` | Operator-facing daemon/system operations notes. |
| `integration-contracts/` | Tool adapter contracts captured from robot/API surfaces. |
| `research-spike/` | Phase 0 research notes and gotchas. |
| `adr/` | Architecture decision records. |

## Conventions

- Markdown only; one topic per file.
- File paths in references should be repo-relative (`apps/desktop/src/...`).
- ADRs follow the standard format: Context, Decision, Consequences,
  Alternatives, Status.
- When `plan.md` and a doc here disagree, `plan.md` wins; reconcile by
  fixing the doc.
