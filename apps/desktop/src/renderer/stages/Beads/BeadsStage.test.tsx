import { describe, expect, test } from "bun:test";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { renderToStaticMarkup } from "react-dom/server";
import type { BeadsStageData } from "../../data/stage-data.ts";
import {
  BEAD_VIEW_PANEL_IDS,
  BEAD_VIEW_TAB_IDS,
  BeadsStage,
  BeadsViewTabs,
  type BeadView,
  beadsViewPanelA11yProps,
  handleBeadsViewTabKeyDown,
  nextBeadsViewForTabKey,
} from "./BeadsStage.tsx";

describe("Beads view switcher accessibility", () => {
  test("renders tabs with aria-controls and roving tab stops", () => {
    const html = renderToStaticMarkup(<BeadsViewTabs view="list" onSelect={() => undefined} />);

    expect(html).toContain("role=\"tablist\"");
    expect(html).toContain(`id="${BEAD_VIEW_TAB_IDS.list}"`);
    expect(html).toContain(`aria-controls="${BEAD_VIEW_PANEL_IDS.list}"`);
    expect(html).toContain("aria-selected=\"true\"");
    expect(html).toContain("tabindex=\"0\"");
    expect(html).toContain(`id="${BEAD_VIEW_TAB_IDS.dag}"`);
    expect(html).toContain(`aria-controls="${BEAD_VIEW_PANEL_IDS.dag}"`);
    expect(html).toContain("aria-selected=\"false\"");
    expect(html).toContain("tabindex=\"-1\"");
  });

  test("links the selected panel back to its controlling tab", () => {
    const tabs = renderToStaticMarkup(<BeadsViewTabs view="dag" onSelect={() => undefined} />);
    const panel = renderToStaticMarkup(<section {...beadsViewPanelA11yProps("dag")} />);

    expect(tabs).toContain(`id="${BEAD_VIEW_TAB_IDS.dag}"`);
    expect(tabs).toContain(`aria-controls="${BEAD_VIEW_PANEL_IDS.dag}"`);
    expect(tabs).toContain("aria-selected=\"true\"");
    expect(panel).toContain("role=\"tabpanel\"");
    expect(panel).toContain(`id="${BEAD_VIEW_PANEL_IDS.dag}"`);
    expect(panel).toContain(`aria-labelledby="${BEAD_VIEW_TAB_IDS.dag}"`);
    expect(panel).toContain("tabindex=\"0\"");
  });

  test("maps Left, Right, Home, and End keys to the expected tab", () => {
    expect(nextBeadsViewForTabKey("list", "ArrowRight")).toBe("dag");
    expect(nextBeadsViewForTabKey("dag", "ArrowRight")).toBe("list");
    expect(nextBeadsViewForTabKey("dag", "ArrowLeft")).toBe("list");
    expect(nextBeadsViewForTabKey("list", "ArrowLeft")).toBe("dag");
    expect(nextBeadsViewForTabKey("dag", "Home")).toBe("list");
    expect(nextBeadsViewForTabKey("list", "End")).toBe("dag");
    expect(nextBeadsViewForTabKey("list", "Enter")).toBeNull();
  });

  test("key handler prevents default browser movement and focuses the selected tab", () => {
    const selected: BeadView[] = [];
    const focused: BeadView[] = [];
    let prevented = 0;

    handleBeadsViewTabKeyDown(
      "list",
      {
        key: "ArrowRight",
        preventDefault: () => {
          prevented += 1;
        },
      },
      (view) => selected.push(view),
      (view) => focused.push(view),
    );

    expect(prevented).toBe(1);
    expect(selected).toEqual(["dag"]);
    expect(focused).toEqual(["dag"]);
  });

  test("non-navigation keys do not change the selected tab", () => {
    const selected: BeadView[] = [];
    let prevented = 0;

    handleBeadsViewTabKeyDown(
      "dag",
      {
        key: "Enter",
        preventDefault: () => {
          prevented += 1;
        },
      },
      (view) => selected.push(view),
    );

    expect(prevented).toBe(0);
    expect(selected).toEqual([]);
  });

  test("empty list state still renders inside the list tabpanel", () => {
    const html = renderEmptyStage("list");

    expect(html).toContain("data-testid=\"beads-list-empty\"");
    expect(html).toContain(`role="tabpanel"`);
    expect(html).toContain(`id="${BEAD_VIEW_PANEL_IDS.list}"`);
    expect(html).toContain(`aria-labelledby="${BEAD_VIEW_TAB_IDS.list}"`);
  });

  test("empty DAG state still renders inside the DAG tabpanel", () => {
    const html = renderEmptyStage("dag");

    expect(html).toContain("data-testid=\"beads-dag-empty\"");
    expect(html).toContain(`role="tabpanel"`);
    expect(html).toContain(`id="${BEAD_VIEW_PANEL_IDS.dag}"`);
    expect(html).toContain(`aria-labelledby="${BEAD_VIEW_TAB_IDS.dag}"`);
  });
});

function renderEmptyStage(initialView: BeadView): string {
  const projectId = "local-demo";
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  client.setQueryData(["stage-data", "beads", projectId], emptyBeadsData(projectId));

  return renderToStaticMarkup(
    <QueryClientProvider client={client}>
      <BeadsStage projectId={projectId} initialView={initialView} />
    </QueryClientProvider>,
  );
}

function emptyBeadsData(projectId: string): BeadsStageData {
  return {
    projectId,
    source: {
      scenarioId: "test-empty",
      fixturesVersion: "test",
      capturedAt: "2026-05-05T00:00:00.000Z",
      vpsId: "test-vps",
      transport: "fixture-fallback",
    },
    total: 0,
    renderedCount: 0,
    statusCounts: [],
    beads: [],
  };
}
