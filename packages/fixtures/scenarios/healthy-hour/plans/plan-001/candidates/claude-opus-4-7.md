# Candidate — Claude (Opus 4.7)

## Recommendation: Cockpit-first, VPS-only, subscription-only

A desktop cockpit (Electron + React) on top of an HTTP/WS daemon running on the VPS. macOS
first. Subscription-only — every model reach goes through Claude Code, Codex CLI, Gemini CLI,
or `oracle --engine browser`. No BYOK.

## Strengths

- **Single deploy target.** Static Go binary on the VPS, signed DMG on the laptop.
- **Auditable.** Every agent action emits an `ActionPlan` the daemon executes; postcondition
  verification against canonical state.
- **Scoped MVP.** Phase 0 produces JSON snapshots from a real ACFS VPS before any adapter
  ships. Phase 1 vendors the desktop scaffold from t3code and decomposes the 2,175-line
  `main.ts` on day one.

## Weaknesses

- macOS-only narrows the audience; Linux power users will want a port.
- Subscription-only can block users with API-key-only providers, but the BYOK alternative
  fragments the threat model.

## Cost: ~$440-$656/month (VPS + Claude Max + ChatGPT Pro + optional Gemini Ultra).

## Quality dimensions

| Dimension       | Score | Notes                                                         |
| --------------- | ----- | ------------------------------------------------------------- |
| Vision clarity  | 9/10  | Cockpit-not-engine framing is unambiguous.                    |
| Risk coverage   | 8/10  | §14 lists 14 named risks with mitigations.                    |
| Phase sequence  | 9/10  | Phase 0 → 1 → 2 → 2.5 → 3+ is well-ordered.                  |
| Test strategy   | 8/10  | Mock Flywheel Mode required; capability-asserting tests.      |
| Cost realism    | 9/10  | Mirrors agent-flywheel.com numbers.                           |
