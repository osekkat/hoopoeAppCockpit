// Hoopoe-owned. Activity-drawer state — events table + filters + unread
// tracking. Mirrors the existing renderer/store.ts zustand pattern.
//
// Real-event ingestion lands in hp-3se (Agent Mail → timeline) and
// downstream WS-channel beads. For hp-1r4 the store also ships a
// fixture seeder so the drawer renders meaningfully on day-1.

import { create } from "zustand";
import {
  EMPTY_FILTER,
  type ActivityCategory,
  type ActivityEvent,
  type ActivityEventKind,
  type ActivityFilter,
  type ActivityImportance,
  categoryFor,
} from "./types.ts";

const MAX_EVENTS = 1000;

export interface ActivityStoreState {
  readonly events: readonly ActivityEvent[];
  readonly filter: ActivityFilter;
  readonly unreadCount: number;

  // Mutation actions.
  readonly addEvent: (event: ActivityEventInput) => void;
  readonly addEvents: (events: readonly ActivityEventInput[]) => void;
  readonly markRead: (eventId: string) => void;
  readonly markAllRead: () => void;
  readonly setFilter: (filter: Partial<ActivityFilter>) => void;
  readonly resetFilter: () => void;
  readonly toggleCategory: (category: ActivityCategory) => void;
  readonly toggleImportance: (importance: ActivityImportance) => void;
  readonly setText: (text: string) => void;
  readonly clearAll: () => void;
}

/** Input shape for addEvent — id is generated if missing, category is
 *  derived from kind, read defaults to false. */
export type ActivityEventInput =
  Omit<ActivityEvent, "id" | "category" | "read"> & {
    readonly id?: string;
    readonly category?: ActivityCategory;
    readonly read?: boolean;
  };

let counter = 0;
function generateId(prefix = "evt"): string {
  counter += 1;
  return `${prefix}_${Date.now().toString(36)}_${counter.toString(36)}`;
}

function normalizeEvent(input: ActivityEventInput): ActivityEvent {
  const id = input.id ?? generateId();
  const category = input.category ?? categoryFor(input.kind);
  const event: ActivityEvent = {
    id,
    kind: input.kind,
    category,
    importance: input.importance,
    summary: input.summary,
    timestamp: input.timestamp,
    actor: input.actor,
    inlinePreview: input.inlinePreview ?? null,
    pivot: input.pivot ?? null,
    read: input.read ?? false,
  };
  if (input.pills !== undefined) {
    Object.assign(event, { pills: input.pills });
  }
  if (input.correlationId !== undefined) {
    Object.assign(event, { correlationId: input.correlationId });
  }
  return event;
}

function computeUnread(events: readonly ActivityEvent[]): number {
  let count = 0;
  for (const e of events) {
    if (!e.read) count += 1;
  }
  return count;
}

/** Apply the filter to the events list. Pure function — exposed so tests
 *  can pin the filter logic without booting the whole store. */
export function applyFilter(
  events: readonly ActivityEvent[],
  filter: ActivityFilter,
): readonly ActivityEvent[] {
  return events.filter((e) => {
    if (filter.categories.length > 0 && !filter.categories.includes(e.category)) {
      return false;
    }
    if (filter.importance.length > 0 && !filter.importance.includes(e.importance)) {
      return false;
    }
    if (filter.relatedBeadId && e.pivot?.kind !== "bead") return false;
    if (filter.relatedBeadId && e.pivot?.kind === "bead" && e.pivot.beadId !== filter.relatedBeadId) {
      return false;
    }
    if (filter.relatedAgentId && e.actor.id !== filter.relatedAgentId) {
      return false;
    }
    if (filter.sinceTs && e.timestamp < filter.sinceTs) {
      return false;
    }
    if (filter.text) {
      const needle = filter.text.toLowerCase();
      const hay = `${e.summary} ${e.actor.displayName} ${(e.pills ?? []).map((p) => p.label).join(" ")}`.toLowerCase();
      if (!hay.includes(needle)) return false;
    }
    return true;
  });
}

export const useActivityStore = create<ActivityStoreState>((set) => ({
  events: [],
  filter: EMPTY_FILTER,
  unreadCount: 0,

  addEvent: (input) => {
    set((state) => {
      const next = trimEvents([normalizeEvent(input), ...state.events]);
      return { events: next, unreadCount: computeUnread(next) };
    });
  },

  addEvents: (inputs) => {
    set((state) => {
      const additions = inputs.map((i) => normalizeEvent(i));
      const next = trimEvents([...additions, ...state.events]);
      return { events: next, unreadCount: computeUnread(next) };
    });
  },

  markRead: (eventId) => {
    set((state) => {
      let mutated = false;
      const next = state.events.map((e) => {
        if (e.id !== eventId) return e;
        if (e.read) return e;
        mutated = true;
        return { ...e, read: true };
      });
      if (!mutated) return {};
      return { events: next, unreadCount: computeUnread(next) };
    });
  },

  markAllRead: () => {
    set((state) => {
      if (state.unreadCount === 0) return {};
      const next = state.events.map((e) => (e.read ? e : { ...e, read: true }));
      return { events: next, unreadCount: 0 };
    });
  },

  setFilter: (partial) => {
    set((state) => ({ filter: { ...state.filter, ...partial } }));
  },

  resetFilter: () => {
    set({ filter: EMPTY_FILTER });
  },

  toggleCategory: (category) => {
    set((state) => {
      const has = state.filter.categories.includes(category);
      const next = has
        ? state.filter.categories.filter((c) => c !== category)
        : [...state.filter.categories, category];
      return { filter: { ...state.filter, categories: next } };
    });
  },

  toggleImportance: (importance) => {
    set((state) => {
      const has = state.filter.importance.includes(importance);
      const next = has
        ? state.filter.importance.filter((i) => i !== importance)
        : [...state.filter.importance, importance];
      return { filter: { ...state.filter, importance: next } };
    });
  },

  setText: (text) => {
    set((state) => ({ filter: { ...state.filter, text } }));
  },

  clearAll: () => {
    set({ events: [], unreadCount: 0 });
  },
}));

function trimEvents(events: ActivityEvent[]): readonly ActivityEvent[] {
  if (events.length <= MAX_EVENTS) return events;
  return events.slice(0, MAX_EVENTS);
}

/** Test-only: reset the store. Mirrors the pattern the shell store uses. */
export function resetActivityStoreForTests(): void {
  useActivityStore.setState({
    events: [],
    filter: EMPTY_FILTER,
    unreadCount: 0,
  });
}

/** Build a fixture event set covering every ActivityEventKind. Used by
 *  storybook + unit tests. */
export function buildFixtureEvents(): readonly ActivityEventInput[] {
  const t0 = "2026-05-04T00:00:00Z";
  const fix = (
    kind: ActivityEventKind,
    summary: string,
    importance: ActivityImportance = "info",
    extras: Partial<ActivityEventInput> = {},
  ): ActivityEventInput => ({
    kind,
    summary,
    importance,
    timestamp: t0,
    actor: extras.actor ?? {
      id: "ag_demo",
      displayName: "demo-agent",
      kind: "agent",
      harness: "claude",
    },
    ...extras,
  });

  return [
    fix("agent.registered", "BlueLake registered with project alpha"),
    fix("mail.sent", "Outbound to RedMountain — capability shape"),
    fix("mail.received", "Reply from BrownStone — fixture aligned"),
    fix("mail.urgent", "URGENT: rate-limited; CAAM action requested", "urgent"),
    fix("bead.claimed", "BlueLake claimed hp-r33"),
    fix("bead.status_changed", "hp-r33 → in_progress"),
    fix("reservation.requested", "Reserved apps/daemon/internal/auth/**"),
    fix("reservation.renewed", "Renewed packages/schemas/** (45min)"),
    fix("reservation.released", "Released apps/desktop/src/capabilities/**"),
    fix("reservation.conflicted", "CONFLICT: api/router.go held by FuchsiaPond", "warn"),
    fix("build.started", "rch exec -- go build ./..."),
    fix("build.completed", "Build succeeded in 12.3s"),
    fix("build.failed", "Build failed — type error in api/auth.go", "warn"),
    fix("rate_limit.detected", "Claude Max throttled; ETA 4 min", "warn"),
    fix("pane.wedged", "Pane 5 wedged at npm install prompt", "warn"),
    fix("orchestrator.intervention", "Orchestrator paused swarm — review queue"),
    fix("review.request", "Round 0 UBS scan requested for hp-r33"),
    fix("review.finding", "[HIGH] potential SQL injection in adapter.go", "urgent"),
    fix("commit.created", "[hp-r33] capability registry shipped"),
    fix("health.snapshot_updated", "Coverage 71.2% (+1.4%) on apps/daemon"),
    fix("user.message_to_orchestrator", "Are we on track for Phase 2 close?", "info", {
      actor: { id: "user_local", displayName: "you", kind: "user" },
    }),
    fix("orchestrator.message_to_user", "Yes — 6 P0 beads closed, 2 in flight", "info", {
      actor: { id: "ag_orchestrator", displayName: "orchestrator-chat", kind: "orchestrator" },
    }),
  ];
}
