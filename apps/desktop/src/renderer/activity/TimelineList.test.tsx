// Hoopoe-owned. SSR + pure-helper tests for the activity timeline
// context-menu disclosure pattern (hp-r8ch). DOM-driven keyboard/focus
// tests live in the desktop e2e suite; bun:test has no DOM environment.

import { describe, expect, test } from "bun:test";
import { renderToStaticMarkup } from "react-dom/server";
import {
  TimelineList,
  isContextMenuKeyEvent,
  type ActivityContextAction,
} from "./TimelineList.tsx";
import type { ActivityEvent } from "./types.ts";

const sampleEvent: ActivityEvent = {
  id: "evt-1",
  kind: "agent_mail_message",
  timestamp: "2026-05-04T00:00:00Z",
  actor: { id: "agent-a", displayName: "Agent A", kind: "agent" },
  summary: "Sample event",
  importance: "info",
  read: false,
};

describe("isContextMenuKeyEvent (hp-r8ch)", () => {
  test("ContextMenu key opens the menu regardless of modifiers", () => {
    expect(isContextMenuKeyEvent("ContextMenu", false)).toBe(true);
    expect(isContextMenuKeyEvent("ContextMenu", true)).toBe(true);
  });
  test("Shift+F10 opens the menu", () => {
    expect(isContextMenuKeyEvent("F10", true)).toBe(true);
  });
  test("F10 alone does NOT open the menu", () => {
    expect(isContextMenuKeyEvent("F10", false)).toBe(false);
  });
  test("other keys are ignored", () => {
    expect(isContextMenuKeyEvent("Enter", false)).toBe(false);
    expect(isContextMenuKeyEvent(" ", false)).toBe(false);
    expect(isContextMenuKeyEvent("ArrowDown", false)).toBe(false);
    expect(isContextMenuKeyEvent("F10", false)).toBe(false);
    // Defensive: an empty key (some synthetic events) must not trigger.
    expect(isContextMenuKeyEvent("", true)).toBe(false);
  });
});

describe("TimelineList row ARIA disclosure (hp-r8ch)", () => {
  test("rows declare aria-haspopup=menu so screen readers announce the menu trigger", () => {
    const html = renderToStaticMarkup(
      <TimelineList
        events={[sampleEvent]}
        onContextAction={(_event: ActivityEvent, _action: ActivityContextAction) => undefined}
      />,
    );
    expect(html).toContain('aria-haspopup="menu"');
  });
  test("rows declare aria-expanded with the menu-closed default in SSR", () => {
    // Closed-state SSR: aria-expanded must be present (false) so the row's
    // disclosure state is observable from the first frame.
    const html = renderToStaticMarkup(
      <TimelineList
        events={[sampleEvent]}
        onContextAction={(_event: ActivityEvent, _action: ActivityContextAction) => undefined}
      />,
    );
    expect(html).toContain('aria-expanded="false"');
    // aria-controls is omitted while the menu is closed (no DOM node to
    // point to yet), per the disclosure pattern. When the menu opens,
    // the controlled element id is ACTIVITY_CONTEXT_MENU_DOM_ID.
    expect(html).not.toContain("aria-controls=");
  });
});
