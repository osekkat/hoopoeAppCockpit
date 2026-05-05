import { afterEach, beforeEach, describe, expect, test } from "bun:test";
import { createElement } from "react";
import { renderToStaticMarkup } from "react-dom/server";
import {
  applyFilter,
  buildFixtureEvents,
  resetActivityStoreForTests,
  useActivityStore,
} from "./index.ts";
import {
  ACTIVITY_CATEGORIES,
  categoryFor,
  mapToTimelineKind,
  type ActivityEvent,
  type ActivityEventKind,
} from "./types.ts";
import { TimelineList } from "./TimelineList.tsx";

const T = (n: number) => `2026-05-04T00:0${n}:00Z`;

const baseEvent = (
  overrides: Partial<ActivityEvent> & { kind: ActivityEventKind; id: string },
): ActivityEvent => ({
  id: overrides.id,
  kind: overrides.kind,
  category: categoryFor(overrides.kind),
  importance: overrides.importance ?? "info",
  summary: overrides.summary ?? "test event",
  timestamp: overrides.timestamp ?? T(0),
  actor: overrides.actor ?? {
    id: "ag_test",
    displayName: "test-agent",
    kind: "agent",
    harness: "claude",
  },
  inlinePreview: overrides.inlinePreview ?? null,
  pivot: overrides.pivot ?? null,
  read: overrides.read ?? false,
});

describe("activity store", () => {
  beforeEach(() => resetActivityStoreForTests());
  afterEach(() => resetActivityStoreForTests());

  test("addEvent normalizes id + category + read=false", () => {
    useActivityStore.getState().addEvent({
      kind: "bead.claimed",
      summary: "claimed hp-x",
      timestamp: T(0),
      importance: "info",
      actor: { id: "ag_x", displayName: "x", kind: "agent" },
    });
    const events = useActivityStore.getState().events;
    expect(events.length).toBe(1);
    expect(events[0].id.length).toBeGreaterThan(0);
    expect(events[0].category).toBe("beads");
    expect(events[0].read).toBe(false);
    expect(useActivityStore.getState().unreadCount).toBe(1);
  });

  test("markRead flips read flag + decrements unread count", () => {
    const store = useActivityStore.getState();
    store.addEvent({
      id: "evt-1",
      kind: "mail.urgent",
      summary: "urgent",
      timestamp: T(0),
      importance: "urgent",
      actor: { id: "ag_x", displayName: "x", kind: "agent" },
    });
    expect(useActivityStore.getState().unreadCount).toBe(1);
    useActivityStore.getState().markRead("evt-1");
    expect(useActivityStore.getState().unreadCount).toBe(0);
    expect(useActivityStore.getState().events[0].read).toBe(true);
  });

  test("markRead is idempotent", () => {
    const store = useActivityStore.getState();
    store.addEvent({
      id: "evt-1",
      kind: "mail.received",
      summary: "x",
      timestamp: T(0),
      importance: "info",
      actor: { id: "ag", displayName: "ag", kind: "agent" },
    });
    useActivityStore.getState().markRead("evt-1");
    useActivityStore.getState().markRead("evt-1");
    expect(useActivityStore.getState().unreadCount).toBe(0);
  });

  test("markAllRead clears the unread set", () => {
    const inputs = buildFixtureEvents();
    useActivityStore.getState().addEvents(inputs);
    expect(useActivityStore.getState().unreadCount).toBe(inputs.length);
    useActivityStore.getState().markAllRead();
    expect(useActivityStore.getState().unreadCount).toBe(0);
  });

  test("setFilter merges partial updates", () => {
    useActivityStore.getState().setFilter({ text: "foo" });
    expect(useActivityStore.getState().filter.text).toBe("foo");
    useActivityStore.getState().setFilter({ relatedBeadId: "hp-r33" });
    expect(useActivityStore.getState().filter.relatedBeadId).toBe("hp-r33");
    expect(useActivityStore.getState().filter.text).toBe("foo");
  });

  test("toggleCategory adds + removes idempotently", () => {
    const store = useActivityStore.getState();
    store.toggleCategory("mail");
    expect(useActivityStore.getState().filter.categories).toEqual(["mail"]);
    useActivityStore.getState().toggleCategory("mail");
    expect(useActivityStore.getState().filter.categories).toEqual([]);
  });

  test("toggleImportance ditto", () => {
    useActivityStore.getState().toggleImportance("urgent");
    useActivityStore.getState().toggleImportance("warn");
    expect(useActivityStore.getState().filter.importance.sort()).toEqual(["urgent", "warn"]);
    useActivityStore.getState().toggleImportance("urgent");
    expect(useActivityStore.getState().filter.importance).toEqual(["warn"]);
  });

  test("resetFilter clears every filter axis", () => {
    useActivityStore.getState().setFilter({
      categories: ["mail"],
      importance: ["urgent"],
      text: "foo",
      relatedBeadId: "hp-x",
    });
    useActivityStore.getState().resetFilter();
    const f = useActivityStore.getState().filter;
    expect(f.categories).toEqual([]);
    expect(f.importance).toEqual([]);
    expect(f.text).toBe("");
    expect(f.relatedBeadId).toBeNull();
  });

  test("clearAll wipes events + unread", () => {
    useActivityStore.getState().addEvents(buildFixtureEvents());
    useActivityStore.getState().clearAll();
    expect(useActivityStore.getState().events).toEqual([]);
    expect(useActivityStore.getState().unreadCount).toBe(0);
  });

  test("applyFilter against current store state respects current filter", () => {
    // hp-habi: visibleEvents() was removed from the store interface — calling
    // it inside a Zustand selector returned a fresh array each render and
    // tripped useSyncExternalStore's "Maximum update depth exceeded" loop
    // (see commit 8661828). Tests now call applyFilter against a snapshot.
    const store = useActivityStore.getState();
    store.addEvents(buildFixtureEvents());
    useActivityStore.getState().toggleCategory("safety");
    const { events, filter } = useActivityStore.getState();
    const visible = applyFilter(events, filter);
    for (const e of visible) {
      expect(e.category).toBe("safety");
    }
  });

  test("ring-buffers at MAX_EVENTS", () => {
    const inputs = Array.from({ length: 1100 }, (_, i) => ({
      id: `evt-${i}`,
      kind: "audit" as never,
      summary: `s ${i}`,
      timestamp: T(0),
      importance: "info" as const,
      actor: { id: "x", displayName: "x", kind: "agent" as const },
    }));
    // Use a real kind; "audit" isn't one — use bead.claimed for the volume test.
    const real = inputs.map((i) => ({ ...i, kind: "bead.claimed" as ActivityEventKind }));
    useActivityStore.getState().addEvents(real);
    expect(useActivityStore.getState().events.length).toBe(1000);
  });
});

describe("applyFilter", () => {
  const events: readonly ActivityEvent[] = buildFixtureEvents().map((input, i) => ({
    ...baseEvent({ id: `f-${i}`, kind: input.kind, summary: input.summary, importance: input.importance, actor: input.actor }),
    timestamp: input.timestamp,
  }));

  test("empty filter returns all events", () => {
    const out = applyFilter(events, {
      categories: [],
      importance: [],
      relatedBeadId: null,
      relatedAgentId: null,
      sinceTs: null,
      text: "",
    });
    expect(out.length).toBe(events.length);
  });

  test("category narrows results", () => {
    const out = applyFilter(events, {
      categories: ["mail"],
      importance: [],
      relatedBeadId: null,
      relatedAgentId: null,
      sinceTs: null,
      text: "",
    });
    for (const e of out) expect(e.category).toBe("mail");
  });

  test("importance narrows results", () => {
    const out = applyFilter(events, {
      categories: [],
      importance: ["urgent"],
      relatedBeadId: null,
      relatedAgentId: null,
      sinceTs: null,
      text: "",
    });
    for (const e of out) expect(e.importance).toBe("urgent");
  });

  test("text search matches summary", () => {
    const out = applyFilter(events, {
      categories: [],
      importance: [],
      relatedBeadId: null,
      relatedAgentId: null,
      sinceTs: null,
      text: "rate-limited",
    });
    expect(out.length).toBeGreaterThan(0);
    for (const e of out) expect(e.summary.toLowerCase()).toContain("rate-limited");
  });

  test("text search matches actor display name", () => {
    const out = applyFilter(events, {
      categories: [],
      importance: [],
      relatedBeadId: null,
      relatedAgentId: null,
      sinceTs: null,
      text: "orchestrator-chat",
    });
    expect(out.length).toBeGreaterThan(0);
  });

  test("sinceTs filters older events", () => {
    const out = applyFilter(events, {
      categories: [],
      importance: [],
      relatedBeadId: null,
      relatedAgentId: null,
      sinceTs: "2026-05-04T00:00:01Z",
      text: "",
    });
    for (const e of out) expect(e.timestamp >= "2026-05-04T00:00:01Z").toBe(true);
  });

  test("relatedAgentId filters to that actor", () => {
    const filtered = applyFilter(events, {
      categories: [],
      importance: [],
      relatedBeadId: null,
      relatedAgentId: "user_local",
      sinceTs: null,
      text: "",
    });
    for (const e of filtered) expect(e.actor.id).toBe("user_local");
  });
});

describe("type mappings", () => {
  test("every ActivityEventKind maps to a TimelineRowKind", () => {
    const allKinds: ActivityEventKind[] = [
      "agent.registered",
      "mail.sent",
      "mail.received",
      "mail.urgent",
      "bead.claimed",
      "bead.status_changed",
      "reservation.requested",
      "reservation.renewed",
      "reservation.released",
      "reservation.conflicted",
      "build.started",
      "build.completed",
      "build.failed",
      "rate_limit.detected",
      "pane.wedged",
      "orchestrator.intervention",
      "review.request",
      "review.finding",
      "commit.created",
      "health.snapshot_updated",
      "user.message_to_orchestrator",
      "orchestrator.message_to_user",
    ];
    for (const k of allKinds) {
      expect(mapToTimelineKind(k)).toMatch(
        /^(mail|reservation|build|approval|audit|agent-decision|user-message|orchestrator-reply)$/,
      );
      expect(ACTIVITY_CATEGORIES).toContain(categoryFor(k));
    }
  });
});

describe("TimelineList empty states", () => {
  test("distinguishes no activity from filtered results", () => {
    const emptyMarkup = renderToStaticMarkup(
      createElement(TimelineList, { emptyReason: "no-events", events: [] }),
    );
    const filteredMarkup = renderToStaticMarkup(
      createElement(TimelineList, {
        emptyReason: "filtered",
        events: [],
        onResetFilters: () => undefined,
      }),
    );

    expect(emptyMarkup).toContain("No activity yet");
    expect(emptyMarkup).toContain("orchestrator");
    expect(filteredMarkup).toContain("No matching events");
    expect(filteredMarkup).toContain("Clear filters");
  });
});

describe("buildFixtureEvents", () => {
  test("covers every ActivityEventKind", () => {
    const fixtures = buildFixtureEvents();
    const kinds = new Set(fixtures.map((e) => e.kind));
    const expected: ActivityEventKind[] = [
      "agent.registered",
      "mail.sent",
      "mail.received",
      "mail.urgent",
      "bead.claimed",
      "bead.status_changed",
      "reservation.requested",
      "reservation.renewed",
      "reservation.released",
      "reservation.conflicted",
      "build.started",
      "build.completed",
      "build.failed",
      "rate_limit.detected",
      "pane.wedged",
      "orchestrator.intervention",
      "review.request",
      "review.finding",
      "commit.created",
      "health.snapshot_updated",
      "user.message_to_orchestrator",
      "orchestrator.message_to_user",
    ];
    for (const k of expected) {
      expect(kinds.has(k)).toBe(true);
    }
  });
});
