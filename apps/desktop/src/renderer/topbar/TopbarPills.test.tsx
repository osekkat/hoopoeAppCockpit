// hp-4ya — render tests for the seeded top-bar pill components.
// hp-m79e — ToolHealthPill VPS dot now reads from the tunnel FSM store.
//
// We focus on the "no active project" variant where each pill renders as
// a plain <span> (no router context required). This exercises the seed
// data path + aria text + visible counts. The TanStack Router <Link>
// variant is exercised via Playwright E2E (see tests/e2e/) and by the
// existing shell.test.tsx integration tests — SSR snapshot rendering of
// <RouterProvider> requires async route preloading that doesn't compose
// with `renderToStaticMarkup`.

import { afterEach, beforeEach, expect, test } from "bun:test";
import { renderToStaticMarkup } from "react-dom/server";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import {
  BeadsPulsePill,
  CodeHealthPill,
  SubscriptionPill,
  SwarmStatePill,
  ToolHealthPill,
  powerAssertionAria,
} from "./index.ts";
import { startPowerSnapshotPoller, type PowerAssertionSnapshot } from "./TopbarPills.tsx";
import type { ActivityEventInput } from "../activity/index.ts";
import { useTunnelStore } from "../tunnel/tunnel-store.ts";

function withQueryClient(node: React.ReactNode): React.ReactNode {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false, gcTime: 0 } },
  });
  return <QueryClientProvider client={client}>{node}</QueryClientProvider>;
}

function render(node: React.ReactNode): string {
  return renderToStaticMarkup(withQueryClient(node));
}

async function flushMicrotasks(): Promise<void> {
  await Promise.resolve();
  await Promise.resolve();
}

function createTimerHarness() {
  const scheduled: Array<{
    readonly delayMs: number;
    readonly callback: () => void;
    readonly cancelled: boolean;
  }> = [];
  const setTimeoutFn = ((handler: Parameters<Window["setTimeout"]>[0], delayMs?: number) => {
    const callback = typeof handler === "function" ? () => handler() : () => {};
    scheduled.push({ callback, delayMs: delayMs ?? 0, cancelled: false });
    return scheduled.length - 1;
  }) as Window["setTimeout"];
  const clearTimeoutFn = ((handle?: ReturnType<Window["setTimeout"]>) => {
    if (handle === undefined) return;
    const entry = scheduled[handle];
    if (entry) {
      scheduled[handle] = { ...entry, cancelled: true };
    }
  }) as Window["clearTimeout"];

  return {
    scheduled,
    setTimeoutFn,
    clearTimeoutFn,
  };
}

// hp-m79e: ensure the topbar render tests see a clean tunnel store —
// other test files (e.g. tunnel-store.test.ts) populate the singleton,
// and bun runs test files in parallel.
beforeEach(() => {
  useTunnelStore.getState().clear();
});

afterEach(() => {
  useTunnelStore.getState().clear();
});

test("ToolHealthPill: null project renders disabled span with 5 unknown dots", () => {
  const html = render(<ToolHealthPill project={null} />);
  expect(html).toContain('data-testid="topbar-tool-health"');
  expect(html).toContain("hh-topbar-pill-disabled");
  // 5 dots, all unknown (vps/ntm/mail/br/bv).
  const matches = html.match(/hh-tool-dot hh-dot-unknown/g) ?? [];
  expect(matches.length).toBe(5);
  // Aria lists each component's state when not all healthy.
  expect(html).toContain("VPS unknown");
  expect(html).toContain("NTM unknown");
  expect(html).toContain("Mail unknown");
  expect(html).toContain("br unknown");
  expect(html).toContain("bv unknown");
  // Should NOT render an anchor when there's no project to link to.
  expect(html).not.toMatch(/<a[^>]*data-testid="topbar-tool-health"/);
});

// hp-m79e: ToolHealthPill's VPS dot is now driven by `selectVpsHealthDot`
// from `tunnel-store.ts`. The FSM-state → HealthDot mapping is exercised
// directly in `tunnel/tunnel-store.test.ts` (selectVpsHealthDot tests)
// and `tunnel/format-helpers.test.ts` (tunnelHealthDot tests). Adding
// render-time coverage here would race the cross-file parallel test
// runner on the global Zustand singleton, so the wiring is type-checked
// + tested at the selector layer instead.

test("SwarmStatePill: null project shows 0/0 idle counts + idle variant", () => {
  const html = render(<SwarmStatePill project={null} />);
  expect(html).toContain('data-testid="topbar-swarm"');
  expect(html).toContain('data-variant="idle"');
  expect(html).toContain("Swarm: 0 running, 0 idle, 0 wedged");
  // Running count == 0.
  expect(html).toMatch(/<strong>0<\/strong>/);
});

test("BeadsPulsePill: null project shows 0 ready / 0 WIP", () => {
  const html = render(<BeadsPulsePill project={null} />);
  expect(html).toContain('data-testid="topbar-beads"');
  expect(html).toContain("Beads: 0 ready, 0 in progress, 0 blocked");
  expect(html).toContain("0 WIP");
});

test("CodeHealthPill: 'no snapshot' state when seed", () => {
  const html = render(<CodeHealthPill project={null} />);
  expect(html).toContain('data-testid="topbar-code-health"');
  expect(html).toContain('data-variant="unknown"');
  expect(html).toContain("no snapshot");
  expect(html).toContain('aria-label="No code-health snapshot yet"');
});

test("CodeHealthPill: zero hotspots → no hotspots callout", () => {
  const html = render(<CodeHealthPill project={null} />);
  expect(html).not.toContain("hh-pill-alert");
  expect(html).not.toContain("hotspot");
});

test("SubscriptionPill: idle when seed shows no usage", () => {
  const html = render(<SubscriptionPill project={null} />);
  expect(html).toContain('data-testid="topbar-subscription"');
  expect(html).toContain('data-variant="ok"');
  expect(html).toContain('aria-label="Subscription usage idle"');
});

test("powerAssertionAria names active Pro rounds and mechanism", () => {
  expect(
    powerAssertionAria({
      active: true,
      assertionId: "pa-1",
      mechanism: "nsprocessinfo",
      level: "app-suspension",
      ownerRoundIds: ["round-1", "round-2"],
      heldCount: 2,
      acquiredAt: "2026-05-04T00:00:00Z",
    }),
  ).toBe("Mac kept awake for 2 Pro rounds; 2 active assertions via nsprocessinfo");
});

test("power snapshot poller catches snapshot failures and backs off with one Activity warning", async () => {
  const timers = createTimerHarness();
  const events: ActivityEventInput[] = [];
  const stop = startPowerSnapshotPoller({
    bridge: {
      snapshot: async () => {
        throw new Error("IPC handler missing");
      },
    },
    onSnapshot: () => {
      throw new Error("unexpected snapshot");
    },
    onFailure: (event) => events.push(event),
    now: () => "2026-05-04T08:55:00.000Z",
    pollIntervalMs: 5,
    failureRetryMs: 30,
    setTimeoutFn: timers.setTimeoutFn,
    clearTimeoutFn: timers.clearTimeoutFn,
  });

  await flushMicrotasks();
  expect(events).toHaveLength(1);
  expect(events[0]?.kind).toBe("health.snapshot_updated");
  expect(events[0]?.importance).toBe("warn");
  expect(events[0]?.summary).toContain("Power assertion status unavailable");
  expect(timers.scheduled[0]?.delayMs).toBe(30);

  timers.scheduled[0]?.callback();
  await flushMicrotasks();
  expect(events).toHaveLength(1);
  expect(timers.scheduled[1]?.delayMs).toBe(30);

  stop();
  expect(timers.scheduled[1]?.cancelled).toBe(true);
});

test("power snapshot poller resumes normal cadence after a recovered snapshot", async () => {
  const timers = createTimerHarness();
  const events: ActivityEventInput[] = [];
  const snapshots: PowerAssertionSnapshot[] = [];
  let callCount = 0;

  const stop = startPowerSnapshotPoller({
    bridge: {
      snapshot: async <O,>() => {
        callCount += 1;
        if (callCount === 1) {
          throw new Error("temporary bridge failure");
        }
        return {
          active: true,
          assertionId: "pa-1",
          mechanism: "powersaveblocker",
          level: "system",
          ownerRoundIds: ["round-1"],
          heldCount: 1,
          acquiredAt: "2026-05-04T08:00:00.000Z",
        } as O;
      },
    },
    onSnapshot: (snapshot) => snapshots.push(snapshot),
    onFailure: (event) => events.push(event),
    now: () => "2026-05-04T08:55:00.000Z",
    pollIntervalMs: 5,
    failureRetryMs: 30,
    setTimeoutFn: timers.setTimeoutFn,
    clearTimeoutFn: timers.clearTimeoutFn,
  });

  await flushMicrotasks();
  expect(events).toHaveLength(1);
  expect(timers.scheduled[0]?.delayMs).toBe(30);

  timers.scheduled[0]?.callback();
  await flushMicrotasks();
  expect(snapshots).toHaveLength(1);
  expect(snapshots[0]?.assertionId).toBe("pa-1");
  expect(timers.scheduled[1]?.delayMs).toBe(5);

  stop();
});

test("Seed pills render together without conflicting test-ids", () => {
  const html = render(
    <>
      <ToolHealthPill project={null} />
      <SwarmStatePill project={null} />
      <BeadsPulsePill project={null} />
      <CodeHealthPill project={null} />
      <SubscriptionPill project={null} />
    </>,
  );
  for (const id of [
    "topbar-tool-health",
    "topbar-swarm",
    "topbar-beads",
    "topbar-code-health",
    "topbar-subscription",
  ]) {
    expect(html).toContain(`data-testid="${id}"`);
  }
  // All five appear as disabled (non-link) when no active project.
  const disabledMatches = html.match(/hh-topbar-pill hh-topbar-pill-disabled/g) ?? [];
  expect(disabledMatches.length).toBe(5);
});

test("Pills carry an icon (lucide svg) so screen readers + sighted users converge", () => {
  const html = render(
    <>
      <ToolHealthPill project={null} />
      <SwarmStatePill project={null} />
      <BeadsPulsePill project={null} />
      <CodeHealthPill project={null} />
      <SubscriptionPill project={null} />
    </>,
  );
  // Every pill should include exactly one Lucide <svg> child.
  const svgMatches = html.match(/<svg[^>]*lucide/g) ?? [];
  expect(svgMatches.length).toBeGreaterThanOrEqual(5);
});
