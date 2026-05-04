# Refinement round 001 — tunnel reconnect + DR

## Changes from synthesis

1. **Tunnel reconnect:** Phase 2 acceptance tests now include three named scenarios — laptop
   sleep > 30s, daemon restart, tunnel TCP-reset. Sequence-cursor + snapshot-on-reconnect must
   pass all three before Phase 2 is closed.
2. **DR ("VPS dies mid-swarm"):** new §17 "Disaster recovery" section. Hoopoe surfaces the
   failure, offers a "restart from canonical" flow (re-clone, re-launch swarm composition,
   re-attach Agent Mail thread). Local cache loss is acceptable; canonical state on origin +
   `br` JSONL is sufficient for restart.
3. **Renderer memory pressure:** new §14.7 risk. Mitigation: aggressively prune renderer
   state on stage transition; defer xterm.js rendering to Diagnostics; abstracted Swarm
   dashboard does not surface raw panes by default (G12).

## Quality-dimension delta (vs. synthesis)

| Dimension       | Synthesis | Refinement-001 | Notes                                |
| --------------- | --------- | -------------- | ------------------------------------ |
| Vision clarity  | 9/10      | 9/10           | Unchanged.                           |
| Risk coverage   | 8/10      | 9/10           | DR + memory-pressure now explicit.   |
| Phase sequence  | 9/10      | 9/10           | Unchanged.                           |
| Test strategy   | 8/10      | 9/10           | Phase 2 acceptance tests strengthened.|
| Cost realism    | 9/10      | 9/10           | Unchanged.                           |

## Meaningful changes? **YES** — risk coverage + test strategy both moved.
