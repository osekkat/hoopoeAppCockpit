# Three-mode review — skill-attached tending jobs decision

> **Status:** Accepted (the architectural decision under review here is `plan.md §1.8` + `§8`).
> **Source bead:** `hp-k21` — modes-of-reasoning review of the swarm-tending architecture.
> **Cross-references:** `plan.md §1.8`, `§8.3.1`, `§8.5`, `§8.8`; `AGENTS.md` "Tending: scheduler + skill-attached jobs".

This note records the symbolic / empirical / dialectical review that the
team owes the original decision so the proof obligation it shifts onto
the codebase is explicit and checkable, not merely prose. It is linked
from `plan.md §1.8` ("Tend agents with skill-attached jobs, not bespoke
loops") and feeds the Phase 10 architecture-checkpoint bead.

## Context — the decision under review

`plan.md §1.8` declares: Hoopoe tends the swarm through a **scheduler
running skill-attached jobs**, not through a bespoke Go operator loop.
The four-layer split is the load-bearing constraint:

```
Layer 1: Scheduler (Go)        cron + interval + event triggers + on-demand
Layer 2: Pre-script (Go)       cheap mechanical reconcile; emits {wakeAgent, context}
Layer 3: Agent runtime         spawns agent with skills loaded; emits typed ActionPlans
Layer 4: Skills (content)      vibing-with-ntm, ntm; pinned via jsm or jfp
```

Mutating actions go through a typed `ActionPlan` (§8.3.1); the **daemon
— not the model — is the executor**, with policy + idempotency +
approvals + postcondition verification against canonical state.

## Symbolic mode — is the architecture internally consistent?

**Premises** (from `plan.md`):

- §1.1 — native sources of truth win; Hoopoe wraps, never replaces, them.
- §1.8 — `vibing-with-ntm` / `ntm` skills are the canonical tending
  methodology and are loaded into agents at runtime, not reimplemented
  in Go.
- §8.3.1 — the daemon executes typed ActionPlans with policy,
  idempotency, approvals, and postcondition checks; the model proposes,
  the daemon disposes.

**Inference.** The architecture is internally consistent if **judgment
stays in loaded skills** while **mutation stays in typed daemon
execution**. That preserves the source-of-truth boundary: skills decide,
the daemon validates and executes, canonical tools (`br`, `bv`, `ntm`,
Agent Mail, Git) verify outcomes.

**Tension.** The proof obligation shifts from a single Go state machine
to **contracts between four layers**: scheduler ↔ pre-script ↔ agent
runtime ↔ skills. Each contract must be explicitly typed and tested or
the symbolic separation reduces to prose. Specifically:

- Pre-script ↔ agent: `{wakeAgent, context}` envelope is the typed
  hand-off — versioned, schema-validated, and asserted in fixtures.
- Agent ↔ daemon: `ActionPlan` is the typed hand-off — every mutating
  action goes through it, every action carries a postcondition, every
  postcondition is verified against canonical state.
- Daemon ↔ skills: skill digest + advisory version are pinned via `jsm`
  / `jfp` and recorded with each tending decision so reproducibility
  survives skill drift.

When any of those contracts loses its conformance test, the symbolic
separation is gone and a hidden Go operator loop has effectively been
rebuilt by accident.

## Empirical mode — does the code support the claim today?

**What is in place**:

- `apps/daemon/internal/scheduler/` is the scheduler substrate.
- `apps/daemon/internal/tending/prescript/runner.go` runs deterministic
  pre-scripts, handles the `wakeAgent` flag, executes deterministic
  action intents through the ActionPlan executor, and dispatches an
  agent runtime with declared skills.
- `apps/daemon/internal/skills/` contains lockfile, digest, and loader
  logic for `jsm` / `jfp` resolution.
- `packages/fixtures/scenarios/` holds the §8.8 replay fixtures for
  `healthy-hour`, `wedged-pane`, `rate-limited-no-caam`,
  `rate-limited-with-caam`, `stale-reservation`, `commit-burst`,
  `budget-breach`, `skill-drift`, `missing-tool`,
  `postcondition-failure`, and `action-arbitration`.

**What this proves**: substrate exists and is fixture-replayable.

**What this does *not* prove**: production behavioral quality. The
behavioral story still depends on:

- The pinned content of `vibing-with-ntm` and `ntm` skills.
- The prompt and context-manifest envelopes the agent runtime feeds in.
- End-to-end evidence that **real** NTM / Agent Mail / `br` / `bv`
  state produces the expected pre-script payloads and ActionPlans on a
  real ACFS VPS — Phase 0 fixture pack territory (`hp-r7i`, `hp-jvm`,
  `hp-7cs`, `hp-vtwm`).

Symbolic + empirical together: the substrate honors the decision; the
production confidence claim still has open evidence beads tied to
Phase 0 capture and Phase 10 tending integration.

## Dialectical mode — what is the strongest counter-argument?

**Thesis.** Avoid reimplementing `vibing-with-ntm` in Go. Skill-attached
tending preserves methodology drift control and keeps Hoopoe a
**cockpit**, not a replacement engine.

**Antithesis.** Putting judgment behind an LLM + skill runtime
increases:

- **Latency.** A deterministic Go loop can run tens of times per
  second; a skill-attached agent run takes seconds and consumes
  subscription quota.
- **Observability complexity.** A Go state machine has one log; an
  agent run has prompt input, context manifest, skill digest, model
  output, ActionPlan, postcondition evidence. More surfaces to keep in
  sync.
- **Reproducibility burden.** Pinning skill content + model + context
  exactly is harder than pinning a single Go binary. Skill drift is a
  real risk (`plan.md §14`).

**Synthesis** (the position this review accepts):

- **Keep the current decision.** Replacing skills with a Go loop would
  rebuild the engine the cockpit is meant to wrap, violating §1.1.
- **But harden the seam:**
  - **Deterministic safety actions stay pre-script-only.** Anything
    where latency or reproducibility is non-negotiable (kill wedged
    PIDs, halt on budget breach, abort on rate-limit) runs in the
    pre-script layer with no agent in the loop.
  - **Judgment-class actions must produce typed ActionPlans with
    evidence refs.** Every approval-gated, audit-relevant, or
    state-mutating action goes through the daemon executor with
    postcondition verification.
  - **Every job has a §8.8 replay fixture.** Each fixture must record
    enough metadata — context manifest, loaded skill digest, advisory
    version, postcondition evidence — to make the symbolic contract
    **empirically testable**, not only documentary.
  - **Phase 10 architecture checkpoint.** After the initial tending job
    set ships, audit the codebase for Go that has quietly become a
    hidden operator loop (`if-else` cascades replacing skill judgment,
    state machines bypassing ActionPlan, side effects skipping
    postcondition verification). Resolve drift by improving the
    skill / fixture / contract surface, never by inlining the
    judgment in Go.

## Acceptance — what concrete artifacts close this review

- **This document** records the three-mode analysis and is linked from
  `plan.md §1.8` so future contributors find the rationale alongside
  the decision.
- **Phase 10 audit checkpoint bead** verifies that production tending
  behavior remains skill-attached, that no Go module under
  `apps/daemon/internal/tending/` has accreted hidden judgment logic,
  and that every shipped tending action has a §8.8 fixture with the
  required skill / context / postcondition metadata.
- **§8.8 replay fixtures** continue to grow each metadata field as it
  becomes testable — `loaded_skill_digest`, `advisory_version`,
  `context_manifest_sha`, `postcondition_evidence_ref`. The fixture
  contract refuses scenarios that omit them once the harness reaches
  parity with production.

## Cross-references

- `plan.md §1.8` — "Tend agents with skill-attached jobs, not bespoke loops".
- `plan.md §8` — Tending: scheduler, pre-script, agent runtime, skills.
- `plan.md §8.3.1` — ActionPlan execution contract.
- `plan.md §8.8` — Tending evaluation harness fixture matrix.
- `plan.md §14` — Risks: lifted code carrying Codex-shaped assumptions; upstream
  skill drift.
- `AGENTS.md` "Tending: scheduler + skill-attached jobs".
- `packages/fixtures/scenarios/` — §8.8 replay fixtures per scenario.
- `apps/daemon/internal/skills/` — `jsm` / `jfp` resolution.
- `apps/daemon/internal/tending/prescript/` — pre-script runner.
- `apps/daemon/internal/scheduler/` — scheduler substrate.
