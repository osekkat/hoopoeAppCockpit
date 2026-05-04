export const NOTIFICATION_CATEGORIES = [
  "swarm.halted",
  "approval.requested.destructive",
  "rate_limit.subscription_exhausted",
  "plan.round_complete",
  "build.test_failed",
  "mail.urgent",
  "pane.wedged",
  "daemon.connection_lost",
  "project.switched",
] as const;

export type NotificationCategory = (typeof NOTIFICATION_CATEGORIES)[number];

export type NotificationActionId =
  | "view"
  | "resume"
  | "review"
  | "switch_account"
  | "pause"
  | "open_plan"
  | "view_thread"
  | "pause_swarm"
  | "diagnostics"
  | "dismiss";

export interface NotificationAction {
  readonly id: NotificationActionId;
  readonly label: string;
}

export interface NotificationDispatchInput {
  readonly category: NotificationCategory;
  readonly projectId: string;
  readonly projectName: string;
  readonly title?: string;
  readonly body: string;
  readonly correlationId: string;
  readonly route?: string;
  readonly actions?: readonly NotificationAction[];
  readonly actorId?: string;
  readonly at?: string;
}

export interface NotificationRecord {
  readonly notificationId: string;
  readonly category: NotificationCategory;
  readonly projectId: string;
  readonly projectName: string;
  readonly title: string;
  readonly body: string;
  readonly correlationId: string;
  readonly route: string;
  readonly actions: readonly NotificationAction[];
  readonly delivered: boolean;
  readonly suppressedByQuietHours: boolean;
  readonly suppressedByUserSetting: boolean;
  readonly suppressedFocused: boolean;
  readonly osDndSuppressed: boolean;
  readonly at: string;
}

export interface NotificationSettings {
  readonly enabledCategories: Partial<Record<NotificationCategory, boolean>>;
  readonly quietHours: QuietHoursSettings;
}

export interface QuietHoursSettings {
  readonly enabled: boolean;
  readonly startLocal: string;
  readonly endLocal: string;
  readonly exemptCategories: readonly NotificationCategory[];
}

export interface NativeNotificationPayload {
  readonly notificationId: string;
  readonly title: string;
  readonly body: string;
  readonly actions: readonly NotificationAction[];
}

export interface NativeNotificationDriver {
  readonly deliver: (payload: NativeNotificationPayload) => void;
}

export interface DockBadgeDriver {
  readonly setBadge: (label: string) => void;
}

export interface FocusStateProvider {
  readonly isFocusedForProject: (projectId: string) => boolean;
}

export interface DoNotDisturbProvider {
  readonly isDoNotDisturbEnabled: () => boolean;
}

export type NotificationRouteHandler = (
  route: string,
  record: NotificationRecord,
  actionId: NotificationActionId,
) => void;

export type NotificationAuditSink = (event: NotificationAuditEvent) => void;

export type NotificationAuditEvent =
  | NotificationDispatchAuditEvent
  | NotificationClickAuditEvent
  | NotificationDigestAuditEvent
  | DockBadgeAuditEvent;

export interface NotificationDispatchAuditEvent {
  readonly kind: "notification.dispatched";
  readonly category: NotificationCategory;
  readonly projectId: string;
  readonly correlationId: string;
  readonly notificationId: string;
  readonly suppressedByQuietHours: boolean;
  readonly suppressedByUserSetting: boolean;
  readonly suppressedFocused: boolean;
  readonly osDndSuppressed: boolean;
  readonly focusedAtDispatch: boolean;
  readonly delivered: boolean;
  readonly at: string;
}

export interface NotificationClickAuditEvent {
  readonly kind: "notification.click_routed";
  readonly category: NotificationCategory;
  readonly projectId: string;
  readonly correlationId: string;
  readonly notificationId: string;
  readonly actionId: NotificationActionId;
  readonly routeTarget: string | null;
  readonly at: string;
}

export interface NotificationDigestAuditEvent {
  readonly kind: "notification.digest_delivered";
  readonly queuedCount: number;
  readonly queuedCategories: readonly NotificationCategory[];
  readonly oldestEventAgeSeconds: number;
  readonly at: string;
}

export interface DockBadgeAuditEvent {
  readonly kind: "dock_badge.set" | "dock_badge.reset";
  readonly count: number;
  readonly previousCount?: number;
  readonly reason?: "activity_panel_open" | "mark_all_read" | "app_activate";
  readonly sourceCategories?: readonly NotificationCategory[];
  readonly at: string;
}

export interface NotificationManagerOptions {
  readonly native: NativeNotificationDriver;
  readonly dock: DockBadgeDriver;
  readonly audit: NotificationAuditSink;
  readonly focus?: FocusStateProvider;
  readonly dnd?: DoNotDisturbProvider;
  readonly route?: NotificationRouteHandler;
  readonly settings?: Partial<NotificationSettings>;
  readonly now?: () => Date;
  readonly idFactory?: () => string;
}

export interface ElectronNotificationLike {
  on?(
    event: "action",
    listener: (details: { readonly actionIndex: number }, actionIndex?: number) => void,
  ): ElectronNotificationLike;
  on?(event: "click", listener: () => void): ElectronNotificationLike;
  show(): void;
}

export interface ElectronNotificationConstructorLike {
  new(options: {
    title: string;
    body: string;
    actions?: readonly { type: "button"; text: string }[];
  }): ElectronNotificationLike;
}

export interface ElectronDockLike {
  setBadge(label: string): void;
}

export type ElectronNotificationActivationHandler = (input: {
  readonly notificationId: string;
  readonly actionId: NotificationActionId;
}) => void;

export const DEFAULT_QUIET_HOURS_EXEMPT_CATEGORIES: readonly NotificationCategory[] = [
  "approval.requested.destructive",
  "daemon.connection_lost",
];

export const DEFAULT_NOTIFICATION_ENABLED: Readonly<Record<NotificationCategory, boolean>> = {
  "swarm.halted": true,
  "approval.requested.destructive": true,
  "rate_limit.subscription_exhausted": true,
  "plan.round_complete": false,
  "build.test_failed": false,
  "mail.urgent": true,
  "pane.wedged": true,
  "daemon.connection_lost": true,
  "project.switched": false,
};

const DEFAULT_ACTIONS: Readonly<Record<NotificationCategory, readonly NotificationAction[]>> = {
  "swarm.halted": [
    { id: "view", label: "View" },
    { id: "resume", label: "Resume" },
    { id: "dismiss", label: "Dismiss" },
  ],
  "approval.requested.destructive": [
    { id: "review", label: "Review" },
    { id: "dismiss", label: "Dismiss" },
  ],
  "rate_limit.subscription_exhausted": [
    { id: "switch_account", label: "Switch account" },
    { id: "pause", label: "Pause" },
    { id: "dismiss", label: "Dismiss" },
  ],
  "plan.round_complete": [
    { id: "open_plan", label: "Open plan" },
    { id: "dismiss", label: "Dismiss" },
  ],
  "build.test_failed": [
    { id: "view", label: "View" },
    { id: "dismiss", label: "Dismiss" },
  ],
  "mail.urgent": [
    { id: "view_thread", label: "View thread" },
    { id: "dismiss", label: "Dismiss" },
  ],
  "pane.wedged": [
    { id: "view", label: "View" },
    { id: "pause_swarm", label: "Pause swarm" },
    { id: "dismiss", label: "Dismiss" },
  ],
  "daemon.connection_lost": [
    { id: "diagnostics", label: "Diagnostics" },
    { id: "dismiss", label: "Dismiss" },
  ],
  "project.switched": [{ id: "dismiss", label: "Dismiss" }],
};

const DEFAULT_SETTINGS: NotificationSettings = {
  enabledCategories: DEFAULT_NOTIFICATION_ENABLED,
  quietHours: {
    enabled: false,
    startLocal: "22:00",
    endLocal: "07:00",
    exemptCategories: DEFAULT_QUIET_HOURS_EXEMPT_CATEGORIES,
  },
};

export class NotificationManager {
  readonly #native: NativeNotificationDriver;
  readonly #dock: DockBadgeDriver;
  readonly #audit: NotificationAuditSink;
  readonly #focus: FocusStateProvider;
  readonly #dnd: DoNotDisturbProvider;
  readonly #route: NotificationRouteHandler | undefined;
  readonly #now: () => Date;
  readonly #idFactory: () => string;
  #settings: NotificationSettings;
  readonly #records = new Map<string, NotificationRecord>();
  readonly #queuedQuietHours: NotificationRecord[] = [];
  readonly #unreadByProject = new Map<string, number>();
  readonly #unreadCategories = new Map<NotificationCategory, number>();

  constructor(options: NotificationManagerOptions) {
    this.#native = options.native;
    this.#dock = options.dock;
    this.#audit = options.audit;
    this.#focus = options.focus ?? { isFocusedForProject: () => false };
    this.#dnd = options.dnd ?? { isDoNotDisturbEnabled: () => false };
    this.#route = options.route;
    this.#now = options.now ?? (() => new Date());
    this.#idFactory = options.idFactory ?? defaultIdFactory;
    this.#settings = mergeSettings(options.settings ?? {});
  }

  updateSettings(settings: Partial<NotificationSettings>): void {
    this.#settings = mergeSettings(settings);
  }

  dispatch(input: NotificationDispatchInput): NotificationRecord {
    const now = this.#coerceNow(input.at);
    const category = assertCategory(input.category);
    const projectId = cleanIdentifier(input.projectId, "projectId");
    const projectName = cleanDisplay(input.projectName, "projectName");
    const correlationId = cleanIdentifier(input.correlationId, "correlationId");
    const actions = normalizeActions(input.actions ?? DEFAULT_ACTIONS[category]);
    const notificationId = this.#idFactory();
    const focusedAtDispatch = this.#focus.isFocusedForProject(projectId);
    const suppressedByUserSetting = this.#isDisabled(category);
    const suppressedByQuietHours =
      !suppressedByUserSetting &&
      this.#isQuietHours(now) &&
      !this.#settings.quietHours.exemptCategories.includes(category);
    const osDndSuppressed =
      !suppressedByUserSetting &&
      !suppressedByQuietHours &&
      this.#dnd.isDoNotDisturbEnabled();
    const suppressedFocused =
      !suppressedByUserSetting &&
      !suppressedByQuietHours &&
      !osDndSuppressed &&
      focusedAtDispatch;
    const delivered =
      !suppressedByUserSetting &&
      !suppressedByQuietHours &&
      !suppressedFocused &&
      !osDndSuppressed;
    const title = input.title
      ? cleanDisplay(input.title, "title")
      : defaultTitle(category, projectName);
    const route = input.route ? cleanRoute(input.route) : defaultRoute(category, projectId);
    const record: NotificationRecord = {
      notificationId,
      category,
      projectId,
      projectName,
      title,
      body: cleanDisplay(input.body, "body"),
      correlationId,
      route,
      actions,
      delivered,
      suppressedByQuietHours,
      suppressedByUserSetting,
      suppressedFocused,
      osDndSuppressed,
      at: now.toISOString(),
    };
    this.#records.set(notificationId, record);

    if (suppressedByQuietHours) {
      this.#queuedQuietHours.push(record);
    } else if (!suppressedByUserSetting) {
      this.#incrementUnread(record);
    }

    if (delivered) {
      this.#native.deliver({
        notificationId,
        title: record.title,
        body: record.body,
        actions,
      });
    }

    this.#emit({
      kind: "notification.dispatched",
      category,
      projectId,
      correlationId,
      notificationId,
      suppressedByQuietHours,
      suppressedByUserSetting,
      suppressedFocused,
      osDndSuppressed,
      focusedAtDispatch,
      delivered,
      at: record.at,
    });
    return record;
  }

  handleNotificationClick(input: {
    readonly notificationId: string;
    readonly actionId?: NotificationActionId;
  }): NotificationRecord | null {
    const record = this.#records.get(input.notificationId);
    if (!record) return null;
    const actionId = input.actionId ?? "view";
    const routeTarget = actionId === "dismiss" ? null : record.route;
    this.#emit({
      kind: "notification.click_routed",
      category: record.category,
      projectId: record.projectId,
      correlationId: record.correlationId,
      notificationId: record.notificationId,
      actionId,
      routeTarget,
      at: this.#now().toISOString(),
    });
    if (routeTarget && this.#route) {
      this.#route(routeTarget, record, actionId);
    }
    return record;
  }

  resetBadge(reason: "activity_panel_open" | "mark_all_read" | "app_activate"): void {
    const previousCount = this.unreadCount();
    this.#unreadByProject.clear();
    this.#unreadCategories.clear();
    this.#dock.setBadge("");
    this.#emit({
      kind: "dock_badge.reset",
      count: 0,
      previousCount,
      reason,
      at: this.#now().toISOString(),
    });
  }

  markAllRead(projectId?: string): void {
    if (!projectId) {
      this.resetBadge("mark_all_read");
      return;
    }
    this.#unreadByProject.delete(projectId);
    this.#rebuildCategoryCounts();
    this.#syncDockBadge();
  }

  projectWithHighestUrgentCount(): string | null {
    let bestProject: string | null = null;
    let bestCount = 0;
    for (const [projectId, count] of this.#unreadByProject.entries()) {
      if (count <= bestCount) continue;
      bestProject = projectId;
      bestCount = count;
    }
    return bestProject;
  }

  flushQuietHoursDigest(): NotificationRecord | null {
    if (this.#queuedQuietHours.length === 0) return null;
    const now = this.#now();
    if (this.#isQuietHours(now)) return null;

    const queued = this.#queuedQuietHours.splice(0);
    const categories = uniqueSorted(queued.map((record) => record.category));
    const notificationId = this.#idFactory();
    const oldestMs = Math.min(...queued.map((record) => Date.parse(record.at)));
    const record: NotificationRecord = {
      notificationId,
      category: "mail.urgent",
      projectId: "__digest__",
      projectName: "All projects",
      title: "Hoopoe - missed urgent events",
      body: `${queued.length} missed events while quiet hours were active.`,
      correlationId: "quiet-hours-digest",
      route: "/activity",
      actions: [{ id: "view", label: "View" }, { id: "dismiss", label: "Dismiss" }],
      delivered: true,
      suppressedByQuietHours: false,
      suppressedByUserSetting: false,
      suppressedFocused: false,
      osDndSuppressed: false,
      at: now.toISOString(),
    };
    this.#records.set(notificationId, record);
    for (const queuedRecord of queued) {
      this.#incrementUnread(queuedRecord);
    }
    this.#native.deliver({
      notificationId,
      title: record.title,
      body: record.body,
      actions: record.actions,
    });
    this.#emit({
      kind: "notification.digest_delivered",
      queuedCount: queued.length,
      queuedCategories: categories,
      oldestEventAgeSeconds: Math.max(0, Math.floor((now.getTime() - oldestMs) / 1000)),
      at: now.toISOString(),
    });
    return record;
  }

  unreadCount(): number {
    let count = 0;
    for (const value of this.#unreadByProject.values()) count += value;
    return count;
  }

  queuedQuietHoursCount(): number {
    return this.#queuedQuietHours.length;
  }

  #isDisabled(category: NotificationCategory): boolean {
    return this.#settings.enabledCategories[category] === false;
  }

  #isQuietHours(now: Date): boolean {
    const quiet = this.#settings.quietHours;
    if (!quiet.enabled) return false;
    const current = now.getHours() * 60 + now.getMinutes();
    const start = parseLocalTime(quiet.startLocal, "quietHours.startLocal");
    const end = parseLocalTime(quiet.endLocal, "quietHours.endLocal");
    if (start === end) return true;
    if (start < end) return current >= start && current < end;
    return current >= start || current < end;
  }

  #incrementUnread(record: NotificationRecord): void {
    this.#unreadByProject.set(
      record.projectId,
      (this.#unreadByProject.get(record.projectId) ?? 0) + 1,
    );
    this.#unreadCategories.set(
      record.category,
      (this.#unreadCategories.get(record.category) ?? 0) + 1,
    );
    this.#syncDockBadge();
  }

  #syncDockBadge(): void {
    const count = this.unreadCount();
    this.#dock.setBadge(count === 0 ? "" : formatDockBadge(count));
    this.#emit({
      kind: "dock_badge.set",
      count,
      sourceCategories: uniqueSorted([...this.#unreadCategories.keys()]),
      at: this.#now().toISOString(),
    });
  }

  #rebuildCategoryCounts(): void {
    this.#unreadCategories.clear();
    const liveProjectIds = new Set(this.#unreadByProject.keys());
    for (const record of this.#records.values()) {
      if (!liveProjectIds.has(record.projectId)) continue;
      if (record.suppressedByUserSetting || record.suppressedByQuietHours) continue;
      this.#unreadCategories.set(
        record.category,
        (this.#unreadCategories.get(record.category) ?? 0) + 1,
      );
    }
  }

  #coerceNow(value: string | undefined): Date {
    if (!value) return this.#now();
    const parsed = new Date(value);
    if (Number.isNaN(parsed.getTime())) {
      throw new NotificationManagerError("invalid_input", "at must be RFC3339");
    }
    return parsed;
  }

  #emit(event: NotificationAuditEvent): void {
    try {
      this.#audit(event);
    } catch {
      // Notification delivery must not be blocked by telemetry/audit sinks.
    }
  }
}

export class NotificationManagerError extends Error {
  override readonly name = "NotificationManagerError";
  readonly code: "invalid_input";

  constructor(code: NotificationManagerError["code"], message: string) {
    super(message);
    this.code = code;
  }
}

export function createElectronNotificationDriver(
  NotificationCtor: ElectronNotificationConstructorLike,
  onActivation?: ElectronNotificationActivationHandler,
): NativeNotificationDriver {
  return {
    deliver(payload) {
      const routedActions = payload.actions.filter((action) => action.id !== "dismiss");
      const notification = new NotificationCtor({
        title: payload.title,
        body: payload.body,
        actions: routedActions.map((action) => ({ type: "button", text: action.label })),
      });
      notification.on?.("click", () => {
        onActivation?.({ notificationId: payload.notificationId, actionId: "view" });
      });
      notification.on?.("action", (details, legacyActionIndex) => {
        const actionIndex = details.actionIndex ?? legacyActionIndex ?? -1;
        const actionId = routedActions[actionIndex]?.id;
        if (!actionId) return;
        onActivation?.({ notificationId: payload.notificationId, actionId });
      });
      notification.show();
    },
  };
}

export function createElectronDockBadgeDriver(dock: ElectronDockLike | undefined): DockBadgeDriver {
  return {
    setBadge(label) {
      dock?.setBadge(label);
    },
  };
}

export function defaultNotificationSettings(): NotificationSettings {
  return mergeSettings({});
}

function mergeSettings(input: Partial<NotificationSettings>): NotificationSettings {
  return {
    enabledCategories: {
      ...DEFAULT_NOTIFICATION_ENABLED,
      ...(input.enabledCategories ?? {}),
    },
    quietHours: {
      ...DEFAULT_SETTINGS.quietHours,
      ...(input.quietHours ?? {}),
      exemptCategories:
        input.quietHours?.exemptCategories ?? DEFAULT_SETTINGS.quietHours.exemptCategories,
    },
  };
}

function assertCategory(category: NotificationCategory): NotificationCategory {
  if ((NOTIFICATION_CATEGORIES as readonly string[]).includes(category)) return category;
  throw new NotificationManagerError("invalid_input", `unknown notification category: ${String(category)}`);
}

function normalizeActions(actions: readonly NotificationAction[]): readonly NotificationAction[] {
  if (actions.length === 0) {
    throw new NotificationManagerError("invalid_input", "at least one action is required");
  }
  const seen = new Set<string>();
  return actions.map((action) => {
    const id = assertActionId(action.id);
    if (seen.has(id)) {
      throw new NotificationManagerError("invalid_input", `duplicate action id: ${id}`);
    }
    seen.add(id);
    return {
      id,
      label: cleanDisplay(action.label, "action.label"),
    };
  });
}

function assertActionId(actionId: NotificationActionId): NotificationActionId {
  const allowed: readonly NotificationActionId[] = [
    "view",
    "resume",
    "review",
    "switch_account",
    "pause",
    "open_plan",
    "view_thread",
    "pause_swarm",
    "diagnostics",
    "dismiss",
  ];
  if (allowed.includes(actionId)) return actionId;
  throw new NotificationManagerError("invalid_input", `unknown action id: ${String(actionId)}`);
}

function cleanIdentifier(value: string, field: string): string {
  const trimmed = value.trim();
  if (!/^[A-Za-z0-9][A-Za-z0-9._:-]{0,159}$/.test(trimmed)) {
    throw new NotificationManagerError("invalid_input", `${field} is invalid`);
  }
  return trimmed;
}

function cleanDisplay(value: string, field: string): string {
  const trimmed = value.trim();
  if (trimmed === "" || trimmed.length > 280 || /[\u0000-\u001f]/.test(trimmed)) {
    throw new NotificationManagerError("invalid_input", `${field} is invalid`);
  }
  return trimmed;
}

function cleanRoute(value: string): string {
  const trimmed = value.trim();
  if (!/^\/[A-Za-z0-9._:/-]{0,240}$/.test(trimmed) || trimmed.includes("//") || trimmed.includes("..")) {
    throw new NotificationManagerError("invalid_input", "route is invalid");
  }
  return trimmed;
}

function parseLocalTime(value: string, field: string): number {
  const match = /^([01]\d|2[0-3]):([0-5]\d)$/.exec(value);
  if (!match) {
    throw new NotificationManagerError("invalid_input", `${field} must be HH:MM`);
  }
  return Number(match[1]) * 60 + Number(match[2]);
}

function defaultTitle(category: NotificationCategory, projectName: string): string {
  switch (category) {
    case "swarm.halted":
      return `Hoopoe - Swarm halted in ${projectName}`;
    case "approval.requested.destructive":
      return `Hoopoe - Approval needed in ${projectName}`;
    case "rate_limit.subscription_exhausted":
      return `Hoopoe - Rate limit in ${projectName}`;
    case "plan.round_complete":
      return `Hoopoe - Plan round complete in ${projectName}`;
    case "build.test_failed":
      return `Hoopoe - Build failed in ${projectName}`;
    case "mail.urgent":
      return `Hoopoe - Urgent mail in ${projectName}`;
    case "pane.wedged":
      return `Hoopoe - Pane wedged in ${projectName}`;
    case "daemon.connection_lost":
      return "Hoopoe - Daemon connection lost";
    case "project.switched":
      return "Hoopoe - Project switched";
  }
}

function defaultRoute(category: NotificationCategory, projectId: string): string {
  switch (category) {
    case "approval.requested.destructive":
      return `/${projectId}/diag`;
    case "plan.round_complete":
      return `/${projectId}/plan`;
    case "build.test_failed":
    case "pane.wedged":
    case "daemon.connection_lost":
      return `/${projectId}/diag`;
    case "swarm.halted":
    case "rate_limit.subscription_exhausted":
    case "mail.urgent":
      return `/${projectId}/swarm`;
    case "project.switched":
      return `/${projectId}/plan`;
  }
}

function formatDockBadge(count: number): string {
  return count > 99 ? "99+" : String(count);
}

function uniqueSorted<TValue extends string>(values: readonly TValue[]): readonly TValue[] {
  return [...new Set(values)].sort();
}

let fallbackIdCounter = 0;
function defaultIdFactory(): string {
  fallbackIdCounter += 1;
  return `notification_${Date.now().toString(36)}_${fallbackIdCounter.toString(36)}`;
}
