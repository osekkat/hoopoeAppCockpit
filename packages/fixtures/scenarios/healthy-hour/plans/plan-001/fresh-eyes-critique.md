# Fresh-eyes critique

## What's strong

- Cockpit-not-engine framing is the load-bearing decision; everything else flows from it.
- Phase 0 → Phase 1 → Phase 2.5 hardening is the right sequence.
- Subscription-only is a brave call but matches reality: CAAM is real, and BYOK doubles the
  threat surface for marginal benefit.

## What's risky

- **Electron memory pressure.** A 16-GB MacBook running 8 agent panes + Hoopoe will feel it.
  Mitigation: aggressively prune renderer state on stage transition; defer xterm.js rendering
  to Diagnostics.
- **Tunnel flapping.** Three-token auth (pairing → bearer → WS-token) is sensible but the
  reconnect path is a known footgun. Mitigation: sequence-cursor + snapshot-on-reconnect must
  be in Phase 2 acceptance tests, not deferred.
- **Adapter brittleness.** Parsing `bv` output is forbidden (G1) but capability probing for
  `bv --robot-help` versions still requires version-aware code. Mitigation: every adapter
  reports `/v1/capabilities` and stage routes are gated on capability IDs.

## What's missing

- **Disaster recovery.** What happens if the VPS dies mid-swarm? Hoopoe must surface the
  failure clearly and offer a "restart from canonical" path (re-clone, re-launch swarm) without
  data loss.
- **Multi-user.** v1 is single-operator; multi-operator collaboration is post-v1.

## Verdict

Carry forward. Refinement rounds should harden the tunnel reconnect path and add a DR section.
