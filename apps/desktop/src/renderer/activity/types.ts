// Hoopoe-owned. ActivityEvent shapes consumed by the Activity drawer
// (hp-1r4) per plan.md §7.5. The 16 event kinds map to 8 design-system
// TimelineRow kinds for rendering — see `mapToTimelineKind`.

import type { TimelineRowKind } from "@hoopoe/design-system";

/** Discriminator for every event kind the Activity drawer surfaces. */
export type ActivityEventKind =
  | "agent.registered"
  | "mail.sent"
  | "mail.received"
  | "mail.urgent"
  | "bead.claimed"
  | "bead.status_changed"
  | "reservation.requested"
  | "reservation.renewed"
  | "reservation.released"
  | "reservation.conflicted"
  | "build.started"
  | "build.completed"
  | "build.failed"
  | "rate_limit.detected"
  | "pane.wedged"
  | "orchestrator.intervention"
  | "review.request"
  | "review.finding"
  | "commit.created"
  | "health.snapshot_updated"
  | "user.message_to_orchestrator"
  | "orchestrator.message_to_user";

/** High-level category buckets the FilterBar surfaces. Mapped from
 *  ActivityEventKind via `categoryFor`. */
export type ActivityCategory =
  | "agents"
  | "mail"
  | "beads"
  | "reservations"
  | "builds"
  | "safety"
  | "review"
  | "commits"
  | "health"
  | "chat";

/** Importance flag — the drawer surfaces urgent events with a tone
 *  highlight; macOS Notification Center integration (hp-hrsv) consumes
 *  this too. */
export type ActivityImportance = "info" | "warn" | "urgent";

/** Click-to-pivot target — opening the drawer's filter is one thing,
 *  clicking through to a specific surface is another. Each target is a
 *  tagged union; the `RootLayout`-level dispatcher handles routing. */
export type ActivityPivot =
  | { readonly kind: "bead"; readonly beadId: string; readonly projectId: string }
  | { readonly kind: "agent"; readonly agentId: string; readonly swarmId?: string }
  | { readonly kind: "reservation"; readonly path: string; readonly projectId: string }
  | { readonly kind: "build"; readonly runId: string; readonly projectId: string }
  | { readonly kind: "commit"; readonly sha: string; readonly projectId: string }
  | { readonly kind: "review"; readonly findingId: string; readonly projectId: string };

/** Single timeline entry. The `data` field carries kind-specific
 *  payload; `summary` + `actor` are required for rendering. */
export interface ActivityEvent {
  readonly id: string;
  readonly kind: ActivityEventKind;
  readonly category: ActivityCategory;
  readonly importance: ActivityImportance;
  readonly summary: string;
  readonly timestamp: string; // RFC3339
  readonly actor: ActivityActor;
  readonly pills?: readonly ActivityPill[];
  readonly inlinePreview?: string | null;
  readonly pivot?: ActivityPivot | null;
  readonly correlationId?: string;
  /** Read receipts — set true after the user clicks the row. The store
   *  derives the badge count from the unread set. */
  readonly read?: boolean;
}

export interface ActivityActor {
  readonly id: string;
  readonly displayName: string;
  readonly kind: "user" | "agent" | "system" | "orchestrator";
  /** Agent harness family — used by the design-system TimelineRow to
   *  pick the right tone. Subset of design-system's AgentFamily. */
  readonly harness?: "claude" | "codex" | "gemini" | "oracle";
}

export interface ActivityPill {
  readonly id: string;
  readonly label: string;
  readonly tone?: "ok" | "warn" | "fail" | "muted";
}

/** Filter applied to the visible timeline. Empty arrays = no filter on
 *  that axis. `text` filters across summary + pills + actor display. */
export interface ActivityFilter {
  readonly categories: readonly ActivityCategory[];
  readonly importance: readonly ActivityImportance[];
  readonly relatedBeadId: string | null;
  readonly relatedAgentId: string | null;
  /** ISO timestamp. Null disables the lower bound. */
  readonly sinceTs: string | null;
  readonly text: string;
}

export const EMPTY_FILTER: ActivityFilter = {
  categories: [],
  importance: [],
  relatedBeadId: null,
  relatedAgentId: null,
  sinceTs: null,
  text: "",
};

/** Map ActivityEventKind → design-system TimelineRowKind. The drawer
 *  uses this when constructing TimelineRow props. */
export function mapToTimelineKind(kind: ActivityEventKind): TimelineRowKind {
  switch (kind) {
    case "mail.sent":
    case "mail.received":
    case "mail.urgent":
      return "mail";
    case "reservation.requested":
    case "reservation.renewed":
    case "reservation.released":
    case "reservation.conflicted":
      return "reservation";
    case "build.started":
    case "build.completed":
    case "build.failed":
      return "build";
    case "rate_limit.detected":
    case "pane.wedged":
    case "orchestrator.intervention":
      return "agent-decision";
    case "review.request":
    case "review.finding":
      return "approval";
    case "agent.registered":
    case "bead.claimed":
    case "bead.status_changed":
    case "commit.created":
    case "health.snapshot_updated":
      return "audit";
    case "user.message_to_orchestrator":
      return "user-message";
    case "orchestrator.message_to_user":
      return "orchestrator-reply";
  }
}

/** Map ActivityEventKind → ActivityCategory for FilterBar grouping. */
export function categoryFor(kind: ActivityEventKind): ActivityCategory {
  switch (kind) {
    case "agent.registered":
      return "agents";
    case "mail.sent":
    case "mail.received":
    case "mail.urgent":
      return "mail";
    case "bead.claimed":
    case "bead.status_changed":
      return "beads";
    case "reservation.requested":
    case "reservation.renewed":
    case "reservation.released":
    case "reservation.conflicted":
      return "reservations";
    case "build.started":
    case "build.completed":
    case "build.failed":
      return "builds";
    case "rate_limit.detected":
    case "pane.wedged":
    case "orchestrator.intervention":
      return "safety";
    case "review.request":
    case "review.finding":
      return "review";
    case "commit.created":
      return "commits";
    case "health.snapshot_updated":
      return "health";
    case "user.message_to_orchestrator":
    case "orchestrator.message_to_user":
      return "chat";
  }
}

/** All categories in canonical filter-bar order. */
export const ACTIVITY_CATEGORIES: readonly ActivityCategory[] = [
  "agents",
  "mail",
  "beads",
  "reservations",
  "builds",
  "safety",
  "review",
  "commits",
  "health",
  "chat",
];

export const ACTIVITY_CATEGORY_LABELS: Record<ActivityCategory, string> = {
  agents: "Agents",
  mail: "Mail",
  beads: "Beads",
  reservations: "Reservations",
  builds: "Builds",
  safety: "Safety",
  review: "Review",
  commits: "Commits",
  health: "Health",
  chat: "Chat",
};
