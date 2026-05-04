export type AuditActorKind =
  | "user"
  | "agent"
  | "tending_job"
  | "repair_action"
  | "mock"
  | "pre_script"
  | "system";

export type AuditCategory =
  | "project"
  | "plan"
  | "beads"
  | "swarm"
  | "mail"
  | "health"
  | "review"
  | "tending"
  | "approval"
  | "repair"
  | "auth"
  | "config"
  | "audit";

export type AuditSeverity = "info" | "notice" | "warning" | "urgent";
export type AuditOutcome = "success" | "failure" | "blocked" | "silent" | "approval_required";
export type AuditTimeRange = "1h" | "24h" | "7d" | "30d" | "custom";
export type AuditGroupBy = "none" | "correlation" | "hour" | "actor";

export interface AuditActor {
  readonly kind: AuditActorKind;
  readonly id: string;
  readonly displayName: string;
}

export interface AuditLinkedArtifact {
  readonly kind: "bead" | "plan" | "project" | "swarm" | "approval" | "pane" | "export" | "job";
  readonly id: string;
  readonly label: string;
  readonly target?: string | null;
  readonly resolved: boolean;
}

export interface AuditLogEntry {
  readonly id: string;
  readonly seq: number;
  readonly timestamp: string;
  readonly projectId: string;
  readonly actor: AuditActor;
  readonly actionType: string;
  readonly category: AuditCategory;
  readonly severity: AuditSeverity;
  readonly outcome: AuditOutcome;
  readonly summary: string;
  readonly reason: string;
  readonly correlationId: string;
  readonly causationId?: string | null;
  readonly commandPreview?: string | null;
  readonly postconditions?: readonly string[];
  readonly linkedArtifacts?: readonly AuditLinkedArtifact[];
  readonly redactionMarkers?: readonly string[];
}

export interface AuditFilterState {
  readonly timeRange: AuditTimeRange;
  readonly customFrom: string;
  readonly customTo: string;
  readonly actorKinds: readonly AuditActorKind[];
  readonly categories: readonly AuditCategory[];
  readonly severities: readonly AuditSeverity[];
  readonly outcomes: readonly AuditOutcome[];
  readonly projectIds: readonly string[];
  readonly correlationId: string;
  readonly causationId: string;
  readonly query: string;
  readonly groupBy: AuditGroupBy;
  readonly selectedEntryId: string | null;
}

export interface AuditFacet {
  readonly value: string;
  readonly count: number;
}

export interface AuditGroup {
  readonly id: string;
  readonly label: string;
  readonly entries: readonly AuditLogEntry[];
}

export interface AuditExportPreview {
  readonly fileName: string;
  readonly sha256: string;
  readonly totalEntries: number;
  readonly redacted: true;
}

export interface AuditExplorerModel {
  readonly entries: readonly AuditLogEntry[];
  readonly filteredEntries: readonly AuditLogEntry[];
  readonly groups: readonly AuditGroup[];
  readonly selectedEntry: AuditLogEntry | null;
  readonly facets: {
    readonly actorKinds: readonly AuditFacet[];
    readonly categories: readonly AuditFacet[];
    readonly severities: readonly AuditFacet[];
    readonly outcomes: readonly AuditFacet[];
    readonly projects: readonly AuditFacet[];
  };
  readonly exportPreview: AuditExportPreview;
  readonly emptyReason: "no-entries" | "filtered-out" | null;
}

export const defaultAuditFilters: AuditFilterState = {
  timeRange: "24h",
  customFrom: "",
  customTo: "",
  actorKinds: [],
  categories: [],
  severities: [],
  outcomes: [],
  projectIds: [],
  correlationId: "",
  causationId: "",
  query: "",
  groupBy: "correlation",
  selectedEntryId: null,
};

export const auditActorKinds: readonly AuditActorKind[] = [
  "user",
  "agent",
  "tending_job",
  "repair_action",
  "mock",
  "pre_script",
  "system",
];

export const auditCategories: readonly AuditCategory[] = [
  "project",
  "plan",
  "beads",
  "swarm",
  "mail",
  "health",
  "review",
  "tending",
  "approval",
  "repair",
  "auth",
  "config",
  "audit",
];

export const auditSeverities: readonly AuditSeverity[] = ["info", "notice", "warning", "urgent"];
export const auditOutcomes: readonly AuditOutcome[] = [
  "success",
  "failure",
  "blocked",
  "silent",
  "approval_required",
];

export function buildAuditExplorerModel(
  entries: readonly AuditLogEntry[],
  filters: AuditFilterState,
  now: Date = new Date("2026-05-04T08:00:00.000Z"),
): AuditExplorerModel {
  const sortedEntries = [...entries].sort(compareAuditEntriesDesc);
  const filteredEntries = sortedEntries.filter((entry) => auditEntryMatches(entry, filters, now));
  const selectedEntry =
    filteredEntries.find((entry) => entry.id === filters.selectedEntryId) ??
    filteredEntries[0] ??
    null;
  return {
    entries: sortedEntries,
    filteredEntries,
    groups: groupAuditEntries(filteredEntries, filters.groupBy),
    selectedEntry,
    facets: {
      actorKinds: facetCounts(filteredEntries, (entry) => entry.actor.kind),
      categories: facetCounts(filteredEntries, (entry) => entry.category),
      severities: facetCounts(filteredEntries, (entry) => entry.severity),
      outcomes: facetCounts(filteredEntries, (entry) => entry.outcome),
      projects: facetCounts(filteredEntries, (entry) => entry.projectId),
    },
    exportPreview: buildAuditExportPreview(filteredEntries, now),
    emptyReason: entries.length === 0 ? "no-entries" : filteredEntries.length === 0 ? "filtered-out" : null,
  };
}

export function updateAuditFilterSet<TValue extends string>(
  values: readonly TValue[],
  value: TValue,
): readonly TValue[] {
  return values.includes(value)
    ? values.filter((candidate) => candidate !== value)
    : [...values, value];
}

export function auditCorrelationChain(
  entries: readonly AuditLogEntry[],
  correlationId: string,
): readonly AuditLogEntry[] {
  const target = correlationId.trim();
  if (target === "") return [];
  return entries
    .filter((entry) => entry.correlationId === target)
    .sort(compareAuditEntriesAsc);
}

export function createFixtureAuditEntries(projectId: string): readonly AuditLogEntry[] {
  const project = projectId || "local-demo";
  return [
    entry({
      id: "audit-001",
      seq: 1,
      timestamp: "2026-05-04T07:02:00.000Z",
      projectId: project,
      actor: actor("system", "daemon", "daemon"),
      actionType: "project.imported",
      category: "project",
      outcome: "success",
      summary: "Project registry imported the local demo",
      reason: "User selected an existing repository during first-run.",
      correlationId: "corr-project-import",
      linkedArtifacts: [{ kind: "project", id: project, label: project, resolved: true }],
    }),
    entry({
      id: "audit-002",
      seq: 2,
      timestamp: "2026-05-04T07:08:00.000Z",
      projectId: project,
      actor: actor("agent", "BlueHill", "BlueHill"),
      actionType: "bead.claimed",
      category: "beads",
      outcome: "success",
      summary: "BlueHill claimed hp-k6r",
      reason: "Ready-frontier assignment from br.",
      correlationId: "corr-swarm-1",
      linkedArtifacts: [{ kind: "bead", id: "hp-k6r", label: "hp-k6r", resolved: true }],
    }),
    entry({
      id: "audit-003",
      seq: 3,
      timestamp: "2026-05-04T07:12:00.000Z",
      projectId: project,
      actor: actor("tending_job", "tend-swarm", "tend-swarm"),
      actionType: "tending.tick",
      category: "tending",
      outcome: "silent",
      severity: "notice",
      summary: "Healthy tending tick suppressed Activity panel output",
      reason: "Pre-script found no actionable drift; wakeAgent=false.",
      correlationId: "corr-tending-healthy",
      postconditions: ["audit entry recorded", "no LLM wake requested"],
    }),
    entry({
      id: "audit-004",
      seq: 4,
      timestamp: "2026-05-04T07:18:00.000Z",
      projectId: project,
      actor: actor("system", "approval-queue", "approval queue"),
      actionType: "approval.created",
      category: "approval",
      outcome: "approval_required",
      severity: "warning",
      summary: "Approval required for secret rotation",
      reason: "Critical auth change requested by owner.",
      correlationId: "corr-auth-rotation",
      linkedArtifacts: [{ kind: "approval", id: "appr-rot-1", label: "appr-rot-1", resolved: true }],
    }),
    entry({
      id: "audit-005",
      seq: 5,
      timestamp: "2026-05-04T07:19:00.000Z",
      projectId: project,
      actor: actor("user", "operator", "operator"),
      actionType: "approval.approved",
      category: "approval",
      outcome: "success",
      summary: "Operator approved auth rotation",
      reason: "Scope: this project session; expiry: 15 minutes.",
      correlationId: "corr-auth-rotation",
      causationId: "audit-004",
      linkedArtifacts: [{ kind: "approval", id: "appr-rot-1", label: "appr-rot-1", resolved: true }],
    }),
    entry({
      id: "audit-006",
      seq: 6,
      timestamp: "2026-05-04T07:20:00.000Z",
      projectId: project,
      actor: actor("system", "daemon.auth", "daemon auth"),
      actionType: "auth.secret_rotated",
      category: "auth",
      outcome: "success",
      summary: "Bearer secret rotated and stale sessions revoked",
      reason: "All session material redacted before persistence.",
      commandPreview: "hoopoe auth rotate-secret --approval appr-rot-1 [REDACTED:bearer-token]",
      correlationId: "corr-auth-rotation",
      causationId: "audit-005",
      redactionMarkers: ["[REDACTED:bearer-token]"],
      postconditions: ["new pairing token minted", "stale bearer rejected"],
    }),
    entry({
      id: "audit-007",
      seq: 7,
      timestamp: "2026-05-04T07:25:00.000Z",
      projectId: project,
      actor: actor("repair_action", "force-release", "force-release"),
      actionType: "repair.force_release_reservation",
      category: "repair",
      outcome: "success",
      summary: "Stale reservation force-released",
      reason: "Diagnostics repair tray released reservation after owner timeout.",
      correlationId: "corr-reservation-repair",
      linkedArtifacts: [{ kind: "bead", id: "hp-1wg8", label: "hp-1wg8", resolved: true }],
    }),
    entry({
      id: "audit-008",
      seq: 8,
      timestamp: "2026-05-04T07:25:03.000Z",
      projectId: project,
      actor: actor("system", "agent-mail", "Agent Mail"),
      actionType: "mail.reservation_released",
      category: "mail",
      outcome: "success",
      summary: "Agent Mail reservation released",
      reason: "Adapter confirmed force-release side effect.",
      correlationId: "corr-reservation-repair",
      causationId: "audit-007",
      linkedArtifacts: [{ kind: "bead", id: "hp-1wg8", label: "hp-1wg8", resolved: true }],
    }),
    entry({
      id: "audit-009",
      seq: 9,
      timestamp: "2026-05-04T07:36:00.000Z",
      projectId: project,
      actor: actor("agent", "FuchsiaPond", "FuchsiaPond"),
      actionType: "build.failed",
      category: "health",
      outcome: "failure",
      severity: "urgent",
      summary: "Typecheck failed in renderer test pass",
      reason: "Missing diagnostics export was caught before commit.",
      correlationId: "corr-build-red",
      linkedArtifacts: [{ kind: "job", id: "job-build-1", label: "job-build-1", resolved: true }],
    }),
    entry({
      id: "audit-010",
      seq: 10,
      timestamp: "2026-05-04T07:44:00.000Z",
      projectId: project,
      actor: actor("user", "operator", "operator"),
      actionType: "audit.export_completed",
      category: "audit",
      outcome: "success",
      summary: "Redacted audit slice exported",
      reason: "Correlation chain exported for support handoff.",
      correlationId: "corr-export",
      linkedArtifacts: [{ kind: "export", id: "audit-slice-20260504", label: "audit-slice-20260504", resolved: true }],
    }),
  ];
}

function auditEntryMatches(entry: AuditLogEntry, filters: AuditFilterState, now: Date): boolean {
  const bounds = timeBounds(filters, now);
  const ts = new Date(entry.timestamp);
  if (bounds.from && ts < bounds.from) return false;
  if (bounds.to && ts > bounds.to) return false;
  if (!setContains(filters.actorKinds, entry.actor.kind)) return false;
  if (!setContains(filters.categories, entry.category)) return false;
  if (!setContains(filters.severities, entry.severity)) return false;
  if (!setContains(filters.outcomes, entry.outcome)) return false;
  if (!setContains(filters.projectIds, entry.projectId)) return false;
  if (filters.correlationId.trim() !== "" && !entry.correlationId.includes(filters.correlationId.trim())) return false;
  if (filters.causationId.trim() !== "" && !(entry.causationId ?? "").includes(filters.causationId.trim())) return false;
  const query = filters.query.trim().toLowerCase();
  if (query === "") return true;
  return searchableAuditText(entry).includes(query);
}

function timeBounds(filters: AuditFilterState, now: Date): { readonly from: Date | null; readonly to: Date | null } {
  if (filters.timeRange === "custom") {
    return {
      from: parseDate(filters.customFrom),
      to: parseDate(filters.customTo),
    };
  }
  const hours = filters.timeRange === "1h" ? 1 : filters.timeRange === "24h" ? 24 : filters.timeRange === "7d" ? 168 : 720;
  return {
    from: new Date(now.getTime() - hours * 60 * 60 * 1000),
    to: now,
  };
}

function parseDate(value: string): Date | null {
  const trimmed = value.trim();
  if (trimmed === "") return null;
  const date = new Date(trimmed);
  return Number.isNaN(date.getTime()) ? null : date;
}

function setContains<TValue extends string>(values: readonly TValue[], value: TValue): boolean {
  return values.length === 0 || values.includes(value);
}

function searchableAuditText(entry: AuditLogEntry): string {
  return [
    entry.id,
    entry.projectId,
    entry.actor.id,
    entry.actor.displayName,
    entry.actor.kind,
    entry.actionType,
    entry.category,
    entry.outcome,
    entry.severity,
    entry.summary,
    entry.reason,
    entry.correlationId,
    entry.causationId ?? "",
    entry.commandPreview ?? "",
    ...(entry.postconditions ?? []),
    ...(entry.redactionMarkers ?? []),
    ...(entry.linkedArtifacts ?? []).flatMap((artifact) => [artifact.kind, artifact.id, artifact.label, artifact.target ?? ""]),
  ]
    .join(" ")
    .toLowerCase();
}

function groupAuditEntries(entries: readonly AuditLogEntry[], groupBy: AuditGroupBy): readonly AuditGroup[] {
  if (groupBy === "none") {
    return [{ id: "all", label: "All entries", entries }];
  }
  const groups = new Map<string, AuditLogEntry[]>();
  for (const entry of entries) {
    const id = groupId(entry, groupBy);
    groups.set(id, [...(groups.get(id) ?? []), entry]);
  }
  return [...groups.entries()].map(([id, groupEntries]) => ({
    id,
    label: groupLabel(id, groupBy, groupEntries),
    entries: groupEntries,
  }));
}

function groupId(entry: AuditLogEntry, groupBy: Exclude<AuditGroupBy, "none">): string {
  if (groupBy === "correlation") return entry.correlationId || "uncorrelated";
  if (groupBy === "actor") return `${entry.actor.kind}:${entry.actor.id}`;
  return entry.timestamp.slice(0, 13) + ":00";
}

function groupLabel(id: string, groupBy: AuditGroupBy, entries: readonly AuditLogEntry[]): string {
  if (groupBy === "correlation") return `${id} (${entries.length})`;
  if (groupBy === "actor") return `${entries[0]?.actor.displayName ?? id} (${entries.length})`;
  if (groupBy === "hour") return `${id.replace("T", " ")} (${entries.length})`;
  return `All entries (${entries.length})`;
}

function facetCounts(entries: readonly AuditLogEntry[], select: (entry: AuditLogEntry) => string): readonly AuditFacet[] {
  const counts = new Map<string, number>();
  for (const entry of entries) {
    const value = select(entry);
    counts.set(value, (counts.get(value) ?? 0) + 1);
  }
  return [...counts.entries()]
    .map(([value, count]) => ({ value, count }))
    .sort((a, b) => b.count - a.count || a.value.localeCompare(b.value));
}

function buildAuditExportPreview(entries: readonly AuditLogEntry[], now: Date): AuditExportPreview {
  const body = JSON.stringify(entries.map((entry) => entry.id));
  return {
    fileName: `audit-slice-${isoSlug(now)}.json`,
    sha256: pseudoSha256(body),
    totalEntries: entries.length,
    redacted: true,
  };
}

function compareAuditEntriesDesc(a: AuditLogEntry, b: AuditLogEntry): number {
  return Date.parse(b.timestamp) - Date.parse(a.timestamp) || b.seq - a.seq;
}

function compareAuditEntriesAsc(a: AuditLogEntry, b: AuditLogEntry): number {
  return Date.parse(a.timestamp) - Date.parse(b.timestamp) || a.seq - b.seq;
}

function entry(input: Omit<AuditLogEntry, "severity"> & { readonly severity?: AuditSeverity }): AuditLogEntry {
  return {
    severity: severityFor(input.outcome),
    ...input,
  };
}

function actor(kind: AuditActorKind, id: string, displayName: string): AuditActor {
  return { kind, id, displayName };
}

function severityFor(outcome: AuditOutcome): AuditSeverity {
  if (outcome === "failure" || outcome === "blocked") return "urgent";
  if (outcome === "approval_required") return "warning";
  if (outcome === "silent") return "notice";
  return "info";
}

function isoSlug(date: Date): string {
  return date.toISOString().replaceAll("-", "").replaceAll(":", "").replace(/\.\d{3}Z$/, "Z");
}

function pseudoSha256(value: string): string {
  let hash = 0x811c9dc5;
  for (let index = 0; index < value.length; index += 1) {
    hash ^= value.charCodeAt(index);
    hash = Math.imul(hash, 0x01000193);
  }
  const chunk = (hash >>> 0).toString(16).padStart(8, "0");
  return chunk.repeat(8);
}
