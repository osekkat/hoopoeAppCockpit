# Hoopoe Plan Revision Notes

## What the competing plans improved over my original plan

1. They were stronger on product focus and build order. The strongest improvement was the framing that Hoopoe must spend disproportionate product polish on Plans and Beads because those are where the user’s real thinking and leverage happen. My original plan had the right five-stage structure, but it did not sufficiently translate that into roadmap priority.

2. They were more concrete about the reference design. My original plan described the stages and general UI, but the revised plan now treats the visual system as a real implementation surface: design tokens, dark sidebar, cream content zone, status pills, agent identity tiles, priority chips, coverage bars, and stage chrome.

3. They were more explicit about daemon necessity. My original plan argued for a VPS-side agent, but the revised plan now explains the exact failure modes of “just SSH and shell”: PTY mirroring, subscriptions, cold starts, event replay, and background jobs.

4. They added better first-install thinking. My original plan correctly favored guided provisioning for v1, but the revised plan now defines a provider plugin interface, keeps existing-VPS support as the MVP path, and adds checkpointed setup, tool inventory, daemon install, and failure resume.

5. They pushed more specificity into PTY streaming and terminal mirroring. The revised plan now separates canonical state from terminal observability, uses NTM streams first, ring buffers and diff streaming second, and tmux capture only as fallback.

6. They improved the data model. The revised plan now includes concrete entity schemas, `.hoopoe` directory layout, daemon global layout, desktop cache layout, lifecycle states, and gate invariants.

7. They were stronger on cost and rate-limit guardrails. The revised plan adds budget policy, alert thresholds, hard stop behavior, rate-limit detection, CAAM integration points, and “estimate not exact” labeling.

8. They were stronger on testing. The revised plan now includes desktop tests, daemon tests, parser golden tests, integration tests, disposable VPS/VM end-to-end tests, and production smoke checks.

9. They were stronger on code health feedback loops. The revised plan now formalizes language adapters, snapshot schemas, hotspot scoring, and automatic bead creation from health findings.

10. They were stronger on real-world recovery. The revised plan now emphasizes audit logs, event replay, reconnect after laptop sleep, diagnostics, stale reservations, stuck jobs, and cache reconciliation.

## Core architectural decisions in the revised plan

- Keep existing Flywheel tools canonical; Hoopoe is the API facade and cockpit.
- Use a VPS daemon because the workflow needs durable jobs, event streams, PTY bridging, and reconciliation.
- Bind the daemon to localhost by default and reach it through SSH tunneling.
- Prefer Go for the v1 daemon because it is operationally simple, static-binary friendly, and aligned with NTM-style orchestration.
- Build existing-VPS onboarding before provider automation.
- Prioritize Plans and Beads before perfect Swarm/Health polish.
- Treat terminal output as observability, not source of truth.
- Make every automation auditable and explainable.
