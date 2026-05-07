// Package ratelimit owns the multi-source rate-limit detection
// aggregator: it fuses the six §7.3 signals (caut, CAAM, CLI status,
// NTM pane, long-no-output, rano) into one typed per-agent severity
// state plus a recommended recovery action.
//
// hp-v6cq engine-first slice: this package starts with the pure
// classifier (Classify(signals) → RateLimitState) and the typed
// severity/recommendation surface. The event-bus subscriber, the
// per-agent observation ring, the 30s ceiling tick, and the
// `agent.rate_limit_state.changed` event emission are follow-up
// cuts on the same bead.
//
// Per plan.md §7.3 Subscription-quota and rate-limit guardrails +
// §8.4 tend-swarm + §7.6 top-bar usage pill.
package ratelimit
