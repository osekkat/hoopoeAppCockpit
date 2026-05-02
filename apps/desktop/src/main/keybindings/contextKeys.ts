// Hoopoe-owned. The known set of when-clause context keys for the cockpit.
// Closes Appendix B anti-pattern #7 (unknown context keys silently false).
// Keybindings reference these via `when:` clauses; the compiler validates
// every identifier against this set and throws `UnknownContextKeyError` on
// a typo.
//
// When a new stage / panel / approval surface introduces a new boolean
// state worth gating on, add the key here AND register it on the
// CommandRegistry instance (see `apps/desktop/src/main/keybindings/index.ts`).

export const HOOPOE_CONTEXT_KEYS = [
  // Stages — at most one is true at a time.
  "stage.planning",
  "stage.beads",
  "stage.swarm",
  "stage.harden",
  "stage.diagnostics",

  // Project state.
  "project.active",
  "project.locked",

  // Selection state.
  "agent.selected",
  "bead.selected",

  // Counters as booleans (renderer derives `approvals.pending` =
  // `pendingApprovals > 0`). Concrete numeric values, when needed, live in
  // a separate context surface — `when` clauses are pure boolean.
  "approvals.pending",
  "swarm.active",

  // Activity panel.
  "activity.open",

  // Command-palette focus state (used to gate fall-through bindings while
  // the palette captures keystrokes).
  "commandPalette.open",
] as const;

export type HoopoeContextKey = (typeof HOOPOE_CONTEXT_KEYS)[number];
