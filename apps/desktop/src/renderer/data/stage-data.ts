import { useQuery } from "@tanstack/react-query";
import healthyHourMailDump from "../../../../../packages/fixtures/scenarios/healthy-hour/agent-mail-dump.json" with { type: "json" };
import healthyHourBrList from "../../../../../packages/fixtures/scenarios/healthy-hour/br-list.json" with { type: "json" };
import healthyHourMeta from "../../../../../packages/fixtures/scenarios/healthy-hour/meta.json" with { type: "json" };
import healthyHourNtmSnapshot from "../../../../../packages/fixtures/scenarios/healthy-hour/ntm-snapshot.json" with { type: "json" };

const MOCK_STAGE_PROJECT_IDS = new Set(["local-demo", "mock-flywheel-project"]);

const MOCK_FLYWHEEL_COMMANDS = {
  getBeads: "mock-flywheel.beads.get",
  getSwarmSnapshot: "mock-flywheel.swarm.snapshot",
  getMailDump: "mock-flywheel.mail.dump",
} as const;

export interface StageFixtureSource {
  readonly scenarioId: string;
  readonly fixturesVersion: string;
  readonly capturedAt: string;
  readonly vpsId: string;
  readonly transport: "daemon-rpc" | "fixture-fallback";
}

export interface BeadStageItem {
  readonly id: string;
  readonly title: string;
  readonly status: string;
  readonly priority: number;
  readonly issueType: string;
  readonly updatedAt: string;
  readonly descriptionSnippet: string;
}

export interface BeadStatusCount {
  readonly status: string;
  readonly count: number;
}

export interface BeadsStageData {
  readonly projectId: string;
  readonly source: StageFixtureSource;
  readonly total: number;
  readonly renderedCount: number;
  readonly statusCounts: readonly BeadStatusCount[];
  readonly beads: readonly BeadStageItem[];
}

export interface SwarmAgent {
  readonly id: string;
  readonly agent: string;
  readonly program: string;
  readonly model: string;
  readonly state: string;
  readonly bead: string | null;
  readonly lastActivityAt: string;
}

export interface SwarmSession {
  readonly id: string;
  readonly agents: readonly SwarmAgent[];
}

export interface SwarmBeadAssignment {
  readonly beadId: string;
  readonly agents: readonly string[];
}

export interface SwarmMailMessage {
  readonly id: string;
  readonly threadId: string;
  readonly from: string;
  readonly subject: string;
}

export interface SwarmStageData {
  readonly projectId: string;
  readonly source: StageFixtureSource;
  readonly counters: {
    readonly sessions: number;
    readonly panes: number;
    readonly alive: number;
    readonly wedged: number;
  };
  readonly sessions: readonly SwarmSession[];
  readonly beadBoard: readonly SwarmBeadAssignment[];
  readonly mail: {
    readonly unreadTotal: number;
    readonly threads: readonly string[];
    readonly messages: readonly SwarmMailMessage[];
  };
}

interface RendererDaemonBridge {
  readonly daemon?: {
    readonly request?: (method: string, body: unknown) => Promise<unknown>;
  };
}

interface StagePayload<T> {
  readonly payload: T;
  readonly transport: StageFixtureSource["transport"];
}

export function useBeadsStageQuery(projectId: string) {
  return useQuery({
    queryKey: ["stage-data", "beads", projectId],
    queryFn: () => loadBeadsStageData(projectId),
    staleTime: Number.POSITIVE_INFINITY,
  });
}

export function useSwarmStageQuery(projectId: string) {
  return useQuery({
    queryKey: ["stage-data", "swarm", projectId],
    queryFn: () => loadSwarmStageData(projectId),
    staleTime: Number.POSITIVE_INFINITY,
  });
}

export async function loadBeadsStageData(projectId: string): Promise<BeadsStageData> {
  const { payload, transport } = await requestStagePayload(
    MOCK_FLYWHEEL_COMMANDS.getBeads,
    { projectId },
    healthyHourBrList,
    projectId,
  );
  return normalizeBeadsStagePayload(projectId, payload, sourceFor(transport));
}

export async function loadSwarmStageData(projectId: string): Promise<SwarmStageData> {
  const [swarm, mail] = await Promise.all([
    requestStagePayload(
      MOCK_FLYWHEEL_COMMANDS.getSwarmSnapshot,
      { projectId },
      healthyHourNtmSnapshot,
      projectId,
    ),
    requestStagePayload(
      MOCK_FLYWHEEL_COMMANDS.getMailDump,
      { projectId },
      healthyHourMailDump,
      projectId,
    ),
  ]);

  const transport =
    swarm.transport === "daemon-rpc" || mail.transport === "daemon-rpc"
      ? "daemon-rpc"
      : "fixture-fallback";
  return normalizeSwarmStagePayload(projectId, swarm.payload, mail.payload, sourceFor(transport));
}

export function normalizeBeadsStagePayload(
  projectId: string,
  payload: unknown,
  source: StageFixtureSource,
): BeadsStageData {
  const root = recordOf(payload);
  const issueValues = arrayField(root, "issues");
  const beads = issueValues
    .map(normalizeBead)
    .filter((bead): bead is BeadStageItem => bead !== null)
    .slice(0, 12);

  return {
    projectId,
    source,
    total: numberField(root, "total", beads.length),
    renderedCount: beads.length,
    statusCounts: statusCountsFor(beads),
    beads,
  };
}

export function normalizeSwarmStagePayload(
  projectId: string,
  swarmPayload: unknown,
  mailPayload: unknown,
  source: StageFixtureSource,
): SwarmStageData {
  const swarmRoot = recordOf(swarmPayload);
  const countersRoot = recordOf(swarmRoot.counters);
  const sessions = arrayField(swarmRoot, "sessions")
    .map(normalizeSession)
    .filter((session): session is SwarmSession => session !== null);
  const agents = sessions.flatMap((session) => session.agents);

  const mailRoot = recordOf(mailPayload);
  const messages = arrayField(mailRoot, "messages")
    .map(normalizeMailMessage)
    .filter((message): message is SwarmMailMessage => message !== null)
    .slice(0, 5);

  return {
    projectId,
    source,
    counters: {
      sessions: numberField(countersRoot, "sessions", sessions.length),
      panes: numberField(countersRoot, "panes", agents.length),
      alive: numberField(countersRoot, "alive", agents.length),
      wedged: numberField(countersRoot, "wedged", 0),
    },
    sessions,
    beadBoard: beadBoardFor(agents),
    mail: {
      unreadTotal: numberField(mailRoot, "unread_total", 0),
      threads: arrayField(mailRoot, "threads").filter(isString).slice(0, 8),
      messages,
    },
  };
}

export function isMockStageProject(projectId: string): boolean {
  return MOCK_STAGE_PROJECT_IDS.has(projectId);
}

async function requestStagePayload<T>(
  method: string,
  body: unknown,
  fallbackPayload: T,
  projectId: string,
): Promise<StagePayload<unknown>> {
  const request = rendererDaemonRequest();
  if (request) {
    return { payload: await request(method, body), transport: "daemon-rpc" };
  }

  if (!isMockStageProject(projectId)) {
    throw new Error("Hoopoe daemon RPC bridge is not available for this project.");
  }

  return { payload: fallbackPayload, transport: "fixture-fallback" };
}

function rendererDaemonRequest(): ((method: string, body: unknown) => Promise<unknown>) | null {
  if (typeof window === "undefined") return null;
  const hoopoe = (window as Window & { readonly hoopoe?: RendererDaemonBridge }).hoopoe;
  const request = hoopoe?.daemon?.request;
  return typeof request === "function" ? request : null;
}

function sourceFor(transport: StageFixtureSource["transport"]): StageFixtureSource {
  const meta = recordOf(healthyHourMeta);
  return {
    scenarioId: stringField(meta, "scenario", "healthy-hour"),
    fixturesVersion: stringField(meta, "fixturesVersion", "phase0-unknown"),
    capturedAt: stringField(meta, "capturedAt", "unknown"),
    vpsId: stringField(meta, "vpsId", "mock"),
    transport,
  };
}

function normalizeBead(value: unknown): BeadStageItem | null {
  const bead = recordOf(value);
  const id = stringField(bead, "id", "");
  if (!id) return null;
  return {
    id,
    title: stringField(bead, "title", "Untitled bead"),
    status: stringField(bead, "status", "unknown"),
    priority: numberField(bead, "priority", 0),
    issueType: stringField(bead, "issue_type", "task"),
    updatedAt: stringField(bead, "updated_at", ""),
    descriptionSnippet: snippet(stringField(bead, "description", "")),
  };
}

function normalizeSession(value: unknown): SwarmSession | null {
  const session = recordOf(value);
  const id = stringField(session, "id", "");
  if (!id) return null;
  const agents = arrayField(session, "panes")
    .map(normalizeAgent)
    .filter((agent): agent is SwarmAgent => agent !== null);
  return { id, agents };
}

function normalizeAgent(value: unknown): SwarmAgent | null {
  const agent = recordOf(value);
  const id = stringField(agent, "id", "");
  const name = stringField(agent, "agent", "");
  if (!id || !name) return null;
  const bead = stringField(agent, "bead", "");
  return {
    id,
    agent: name,
    program: stringField(agent, "program", "unknown"),
    model: stringField(agent, "model", "unknown"),
    state: stringField(agent, "state", "unknown"),
    bead: bead.length > 0 ? bead : null,
    lastActivityAt: stringField(agent, "last_activity_ts", ""),
  };
}

function normalizeMailMessage(value: unknown): SwarmMailMessage | null {
  const message = recordOf(value);
  const id = stringField(message, "id", "");
  const subject = stringField(message, "subject", "");
  if (!id || !subject) return null;
  return {
    id,
    threadId: stringField(message, "thread_id", "unknown"),
    from: stringField(message, "from", "unknown"),
    subject,
  };
}

function beadBoardFor(agents: readonly SwarmAgent[]): readonly SwarmBeadAssignment[] {
  const board = new Map<string, string[]>();
  for (const agent of agents) {
    if (!agent.bead) continue;
    const names = board.get(agent.bead) ?? [];
    names.push(agent.agent);
    board.set(agent.bead, names);
  }
  return Array.from(board.entries())
    .map(([beadId, names]) => ({ beadId, agents: names.sort((a, b) => a.localeCompare(b)) }))
    .sort((a, b) => a.beadId.localeCompare(b.beadId));
}

function statusCountsFor(beads: readonly BeadStageItem[]): readonly BeadStatusCount[] {
  const counts = new Map<string, number>();
  for (const bead of beads) {
    counts.set(bead.status, (counts.get(bead.status) ?? 0) + 1);
  }
  return Array.from(counts.entries())
    .map(([status, count]) => ({ status, count }))
    .sort((a, b) => a.status.localeCompare(b.status));
}

function recordOf(value: unknown): Record<string, unknown> {
  return value !== null && typeof value === "object" && !Array.isArray(value)
    ? (value as Record<string, unknown>)
    : {};
}

function arrayField(record: Record<string, unknown>, key: string): readonly unknown[] {
  const value = record[key];
  return Array.isArray(value) ? value : [];
}

function stringField(
  record: Record<string, unknown>,
  key: string,
  fallback: string,
): string {
  const value = record[key];
  if (typeof value === "string") return value;
  if (typeof value === "number" || typeof value === "boolean") return String(value);
  return fallback;
}

function numberField(
  record: Record<string, unknown>,
  key: string,
  fallback: number,
): number {
  const value = record[key];
  return typeof value === "number" && Number.isFinite(value) ? value : fallback;
}

function snippet(text: string): string {
  const compact = text.replace(/\s+/g, " ").trim();
  return compact.length > 180 ? `${compact.slice(0, 177)}...` : compact;
}

function isString(value: unknown): value is string {
  return typeof value === "string";
}
