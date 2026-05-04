# Hoopoe v1 — cockpit MVP for ACFS swarms

> Locked plan, version 7. Source-of-truth strategic document for the Hoopoe cockpit.

## 0. Executive thesis

Hoopoe is the **cockpit** for the Agentic Coding Flywheel — not the engine. The VPS is the
execution plane. `br`, `bv`, `ntm`, Agent Mail, ACFS, CAAM, DCG, CASS, Git, and the agent CLIs
remain the source-of-truth systems. Hoopoe centralizes, visualizes, automates, and audits the
workflow without replacing them.

## 1. Product principles

1. Preserve native sources of truth. Wrap, do not replace.
2. The desktop is not the orchestrator of record — the VPS daemon owns long-running jobs.
3. Robot/API surfaces first, shell parsing last.
4. Every automation must be inspectable.
5. Build for restartability.
6. Make the first successful run boring.
7. Sync-driven mirrors allowed; parallel sources of truth not.
8. Tend agents with skill-attached jobs, not bespoke loops.

## 2. Stages

The cockpit is `STAGE N — VERB` chrome:

- **01 Planning** — chat-box → 3-4 candidate models → comparative matrix → synthesis →
  fresh-eyes critique → 4-5 refinement rounds → lock.
- **02 Beads** — locked plan → `br` beads with traceability map.
- **03 Swarm** — NTM agent launch with composition picker; bead state + agent state +
  Activity panel only (no terminals by default).
- **04 Debugging / Hardening** — code health metrics, review rounds, finding tracker.

The Activity panel is a cross-stage drawer hosting agent ↔ agent mail and the user ↔
orchestrator chat.
