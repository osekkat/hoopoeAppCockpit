# ADR-0001: Single VPS Per Install For v1

- **Status:** Accepted (v1)
- **Date:** 2026-05-04
- **Related plan sections:** §4.1, §6, §13
- **Supersedes:** —
- **Superseded by:** —

## Context

Hoopoe's v1 target user is a solo builder or small team running the Agentic
Coding Flywheel on one VPS. The VPS is the execution plane: it owns daemon
jobs, ACFS tooling, agent sessions, VPS working trees, and build/test
execution. A single Hoopoe desktop install can manage many projects on that
one VPS.

Multi-VPS support would add a new `Connection` entity, per-VPS auth, per-VPS
capabilities, cross-connection project movement, and UI switching. That
complexity does not help the first successful run.

## Decision

v1 supports one paired VPS per desktop install. Projects belong to that
implicit connection. The UI does not expose a connection picker in v1.

The code should leave mechanical seams for a later `Connection` entity:

- project metadata can gain `connectionId`;
- auth/bearer storage can be keyed by connection;
- top bar can expand from project selector to connection + project selector;
- readiness checks already distinguish VPS lifecycle from project lifecycle.

The daemon-side migration scaffold lives in
`apps/daemon/internal/connections`. It treats v1 as one implicit
`Connection` with `connectionId = "vps_local"` and keeps existing project
`vpsId` values as the legacy backfill source. The eventual schema migration is
additive: create `connections`, add nullable `projects.connection_id`,
backfill it from `projects.vps_id`, then add indexes/foreign-key enforcement
after all projects have converged. Connection records store secret references
only; bearer tokens and SSH key material remain in the daemon secret store.

## Consequences

- Existing-VPS onboarding stays short and boring.
- Provider plugins create or attach the single configured VPS; they do not
  introduce a provider account switchboard in v1.
- Diagnostics talks about "the VPS" rather than "a connection".
- Cross-project operations are allowed within the single VPS.
- Multi-VPS migration is post-MVP and tracked separately.

## Alternatives Considered

| Alternative | Reason rejected for v1 |
| --- | --- |
| First-class multi-VPS from day one | Adds auth, UI, schema, and failure modes before the base flow is reliable. |
| One Hoopoe install per project | Makes cross-project `ru`, shared build/test contention, and shared account visibility worse. |
| Provider account as the top-level entity | Violates existing-VPS-first and makes provider automation block onboarding. |

## Cross-References

- `plan.md §4.1` — VPS lifecycle vs project lifecycle.
- `plan.md §6` — onboarding.
- `plan.md §13` — MVP scope and deferred work.
- `docs/onboarding.md` — readiness model.
- `docs/source-of-truth.md` — persistent paths.
