# Hoopoe docs

Architecture references, ADRs, and operational runbooks for the Hoopoe
cockpit. The strategic plan in `plan.md` (project root) is authoritative;
this directory holds the longer-form, code-near references that the plan
points to.

## Inventory

| File                                  | Purpose                                                                         |
| ------------------------------------- | ------------------------------------------------------------------------------- |
| `source-provenance.md`                | Pinned external-source SHAs and attribution rules (t3code lift).                |
| (forthcoming) `api-seed.md`           | Seed daemon REST/WS contract per `plan.md §2.6`. Lands in Phase 2.5.            |
| `process-manager.md`                  | Job registry, process, log, and artifact invariants per `plan.md §2.7`.         |
| `reconnect-replay.md`                 | Job-log offset reconnect rules and replay boundaries.                           |
| (forthcoming) `security.md`           | Threat model, secrets surface, audit-log contract per `plan.md §5`.             |
| (forthcoming) `adr/0001-…`            | Architecture decision records.                                                  |

## Conventions

- Markdown only; one topic per file.
- File paths in references should be repo-relative (`apps/desktop/src/...`).
- ADRs follow the standard format: Context, Decision, Consequences,
  Alternatives, Status.
- When `plan.md` and a doc here disagree, `plan.md` wins; reconcile by
  fixing the doc.
