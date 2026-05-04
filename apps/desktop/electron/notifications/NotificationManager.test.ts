import { describe, expect, test } from "bun:test";
import {
  DEFAULT_NOTIFICATION_ENABLED,
  NotificationManager,
  NotificationManagerError,
  createElectronDockBadgeDriver,
  createElectronNotificationDriver,
  defaultNotificationSettings,
  type DockBadgeDriver,
  type NativeNotificationDriver,
  type NativeNotificationPayload,
  type NotificationAuditEvent,
  type NotificationRecord,
  type NotificationRouteHandler,
} from "./NotificationManager.ts";

interface Harness {
  readonly manager: NotificationManager;
  readonly delivered: NativeNotificationPayload[];
  readonly badges: string[];
  readonly audits: NotificationAuditEvent[];
  readonly routes: Array<{ readonly route: string; readonly actionId: string }>;
}

function makeHarness(options: {
  readonly now?: Date;
  readonly focused?: boolean;
  readonly dnd?: boolean;
  readonly settings?: ConstructorParameters<typeof NotificationManager>[0]["settings"];
} = {}): Harness {
  const delivered: NativeNotificationPayload[] = [];
  const badges: string[] = [];
  const audits: NotificationAuditEvent[] = [];
  const routes: Array<{ readonly route: string; readonly actionId: string }> = [];
  const native: NativeNotificationDriver = {
    deliver: (payload) => delivered.push(payload),
  };
  const dock: DockBadgeDriver = {
    setBadge: (label) => badges.push(label),
  };
  const route: NotificationRouteHandler = (target, _record, actionId) => {
    routes.push({ route: target, actionId });
  };
  const manager = new NotificationManager({
    native,
    dock,
    audit: (event) => audits.push(event),
    focus: { isFocusedForProject: () => options.focused ?? false },
    dnd: { isDoNotDisturbEnabled: () => options.dnd ?? false },
    route,
    settings: options.settings,
    now: () => options.now ?? new Date("2026-05-04T08:00:00.000Z"),
    idFactory: idSequence("notif"),
  });
  return { manager, delivered, badges, audits, routes };
}

function dispatchUrgent(manager: NotificationManager, overrides: Partial<Parameters<NotificationManager["dispatch"]>[0]> = {}): NotificationRecord {
  return manager.dispatch({
    category: "swarm.halted",
    projectId: "local-demo",
    projectName: "Local demo",
    body: "Safety threshold halted the swarm.",
    correlationId: "corr-swarm-halt",
    ...overrides,
  });
}

function idSequence(prefix: string): () => string {
  let counter = 0;
  return () => {
    counter += 1;
    return `${prefix}-${counter}`;
  };
}

describe("NotificationManager", () => {
  test("category opt-out suppresses native delivery, badge increment, and audits the suppression", () => {
    const h = makeHarness({
      settings: {
        enabledCategories: {
          "plan.round_complete": false,
        },
      },
    });

    const record = dispatchUrgent(h.manager, {
      category: "plan.round_complete",
      body: "Round 4 is complete.",
      correlationId: "corr-plan-round",
    });

    expect(record.suppressedByUserSetting).toBe(true);
    expect(record.delivered).toBe(false);
    expect(h.delivered).toHaveLength(0);
    expect(h.badges).toHaveLength(0);
    expect(h.audits[0]).toMatchObject({
      kind: "notification.dispatched",
      suppressedByUserSetting: true,
      delivered: false,
    });
  });

  test("quiet hours queues non-exempt urgent events but still delivers destructive approval", () => {
    const h = makeHarness({
      now: new Date("2026-05-04T23:15:00.000Z"),
      settings: {
        quietHours: {
          enabled: true,
          startLocal: "22:00",
          endLocal: "07:00",
          exemptCategories: ["approval.requested.destructive", "daemon.connection_lost"],
        },
      },
    });

    const queued = dispatchUrgent(h.manager, {
      category: "mail.urgent",
      body: "Human reply requested.",
      correlationId: "corr-mail",
    });
    const delivered = dispatchUrgent(h.manager, {
      category: "approval.requested.destructive",
      body: "Destroying a VPS needs approval.",
      correlationId: "corr-approval",
    });

    expect(queued.suppressedByQuietHours).toBe(true);
    expect(delivered.delivered).toBe(true);
    expect(h.manager.queuedQuietHoursCount()).toBe(1);
    expect(h.delivered).toHaveLength(1);
    expect(h.badges.at(-1)).toBe("1");
  });

  test("quiet-hours digest delivers queued events after the quiet window", () => {
    let now = new Date("2026-05-04T23:10:00.000Z");
    const h = makeHarness({
      get now() {
        return now;
      },
      settings: {
        quietHours: {
          enabled: true,
          startLocal: "22:00",
          endLocal: "07:00",
          exemptCategories: ["daemon.connection_lost"],
        },
      },
    });

    dispatchUrgent(h.manager, { category: "mail.urgent", correlationId: "corr-mail-1" });
    dispatchUrgent(h.manager, { category: "pane.wedged", correlationId: "corr-pane-1" });
    expect(h.delivered).toHaveLength(0);

    now = new Date("2026-05-05T07:01:00.000Z");
    const digest = h.manager.flushQuietHoursDigest();

    expect(digest?.title).toBe("Hoopoe - missed urgent events");
    expect(digest?.body).toContain("2 missed events");
    expect(h.delivered).toHaveLength(1);
    expect(h.badges.at(-1)).toBe("2");
    const digestAudit = h.audits.find((event) => event.kind === "notification.digest_delivered");
    expect(digestAudit).toMatchObject({
      kind: "notification.digest_delivered",
      queuedCount: 2,
    });
  });

  test("dock badge increments and reset emits an audited reset", () => {
    const h = makeHarness();

    dispatchUrgent(h.manager, { correlationId: "corr-1" });
    dispatchUrgent(h.manager, { correlationId: "corr-2" });
    h.manager.resetBadge("activity_panel_open");

    expect(h.badges).toEqual(["1", "2", ""]);
    expect(h.manager.unreadCount()).toBe(0);
    expect(h.audits.at(-1)).toMatchObject({
      kind: "dock_badge.reset",
      previousCount: 2,
      reason: "activity_panel_open",
    });
  });

  test("cross-project badge sum and highest-count project are stable", () => {
    const h = makeHarness();

    dispatchUrgent(h.manager, { projectId: "project-a", correlationId: "corr-a1" });
    dispatchUrgent(h.manager, { projectId: "project-a", correlationId: "corr-a2" });
    dispatchUrgent(h.manager, { projectId: "project-b", correlationId: "corr-b1" });

    expect(h.badges.at(-1)).toBe("3");
    expect(h.manager.projectWithHighestUrgentCount()).toBe("project-a");
  });

  test("click routing audits the action and calls the route handler", () => {
    const h = makeHarness();
    const record = dispatchUrgent(h.manager, {
      route: "/local-demo/swarm",
      correlationId: "corr-click",
    });

    h.manager.handleNotificationClick({
      notificationId: record.notificationId,
      actionId: "resume",
    });

    expect(h.routes).toEqual([{ route: "/local-demo/swarm", actionId: "resume" }]);
    expect(h.audits.at(-1)).toMatchObject({
      kind: "notification.click_routed",
      actionId: "resume",
      correlationId: "corr-click",
      routeTarget: "/local-demo/swarm",
    });
  });

  test("dismiss click is audited without routing", () => {
    const h = makeHarness();
    const record = dispatchUrgent(h.manager);

    h.manager.handleNotificationClick({
      notificationId: record.notificationId,
      actionId: "dismiss",
    });

    expect(h.routes).toHaveLength(0);
    expect(h.audits.at(-1)).toMatchObject({
      kind: "notification.click_routed",
      actionId: "dismiss",
      routeTarget: null,
    });
  });

  test("DND suppresses native delivery but still increments dock badge and audit", () => {
    const h = makeHarness({ dnd: true });

    const record = dispatchUrgent(h.manager);

    expect(record.osDndSuppressed).toBe(true);
    expect(record.delivered).toBe(false);
    expect(h.delivered).toHaveLength(0);
    expect(h.badges.at(-1)).toBe("1");
    expect(h.audits[1]).toMatchObject({
      kind: "notification.dispatched",
      osDndSuppressed: true,
    });
  });

  test("focused matching project suppresses native delivery but increments dock badge", () => {
    const h = makeHarness({ focused: true });

    const record = dispatchUrgent(h.manager);

    expect(record.suppressedFocused).toBe(true);
    expect(record.delivered).toBe(false);
    expect(h.delivered).toHaveLength(0);
    expect(h.badges.at(-1)).toBe("1");
    expect(h.audits[1]).toMatchObject({
      kind: "notification.dispatched",
      focusedAtDispatch: true,
      suppressedFocused: true,
    });
  });

  test("default category matrix matches the hp-hrsv opt-out contract", () => {
    expect(DEFAULT_NOTIFICATION_ENABLED["swarm.halted"]).toBe(true);
    expect(DEFAULT_NOTIFICATION_ENABLED["approval.requested.destructive"]).toBe(true);
    expect(DEFAULT_NOTIFICATION_ENABLED["rate_limit.subscription_exhausted"]).toBe(true);
    expect(DEFAULT_NOTIFICATION_ENABLED["plan.round_complete"]).toBe(false);
    expect(DEFAULT_NOTIFICATION_ENABLED["build.test_failed"]).toBe(false);
    expect(DEFAULT_NOTIFICATION_ENABLED["mail.urgent"]).toBe(true);
    expect(DEFAULT_NOTIFICATION_ENABLED["pane.wedged"]).toBe(true);
    expect(DEFAULT_NOTIFICATION_ENABLED["daemon.connection_lost"]).toBe(true);
    expect(DEFAULT_NOTIFICATION_ENABLED["project.switched"]).toBe(false);
    expect(defaultNotificationSettings().quietHours.enabled).toBe(false);
  });

  test("native Electron driver maps action buttons and shows the notification", () => {
    const constructed: Array<{ title: string; body: string; actions?: readonly { type: "button"; text: string }[] }> = [];
    let showCount = 0;
    class FakeNotification {
      constructor(options: { title: string; body: string; actions?: readonly { type: "button"; text: string }[] }) {
        constructed.push(options);
      }
      show(): void {
        showCount += 1;
      }
    }

    const driver = createElectronNotificationDriver(FakeNotification);
    driver.deliver({
      notificationId: "n-1",
      title: "Hoopoe - Swarm halted",
      body: "Threshold crossed.",
      actions: [
        { id: "view", label: "View" },
        { id: "dismiss", label: "Dismiss" },
      ],
    });

    expect(showCount).toBe(1);
    expect(constructed[0]?.actions).toEqual([{ type: "button", text: "View" }]);
  });

  test("native Electron driver routes notification clicks and action buttons", () => {
    const activations: Array<{ notificationId: string; actionId: string }> = [];
    const handlers: Partial<Record<"click" | "action", (...args: unknown[]) => void>> = {};
    class FakeNotification {
      on(event: "click" | "action", listener: (...args: unknown[]) => void): this {
        handlers[event] = listener;
        return this;
      }
      show(): void {}
    }

    const driver = createElectronNotificationDriver(FakeNotification, (activation) => {
      activations.push(activation);
    });
    driver.deliver({
      notificationId: "n-2",
      title: "Hoopoe - Swarm halted",
      body: "Threshold crossed.",
      actions: [
        { id: "view", label: "View" },
        { id: "resume", label: "Resume" },
        { id: "dismiss", label: "Dismiss" },
      ],
    });

    handlers.click?.();
    handlers.action?.({ actionIndex: 1 });
    handlers.action?.({ actionIndex: 2 });

    expect(activations).toEqual([
      { notificationId: "n-2", actionId: "view" },
      { notificationId: "n-2", actionId: "resume" },
    ]);
  });

  test("dock driver clears and sets the macOS dock badge", () => {
    const labels: string[] = [];
    const driver = createElectronDockBadgeDriver({
      setBadge: (label) => labels.push(label),
    });

    driver.setBadge("3");
    driver.setBadge("");

    expect(labels).toEqual(["3", ""]);
  });

  test("invalid payloads are rejected before native delivery", () => {
    const h = makeHarness();

    expect(() =>
      dispatchUrgent(h.manager, {
        projectId: "../bad",
      }),
    ).toThrow(NotificationManagerError);
    expect(() =>
      dispatchUrgent(h.manager, {
        route: "/local-demo/../../shell",
      }),
    ).toThrow(NotificationManagerError);
    expect(h.delivered).toHaveLength(0);
    expect(h.badges).toHaveLength(0);
  });
});
