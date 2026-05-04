// hp-4ya — Five top-bar pill components: Tool health, Swarm, Beads,
// Code health, Subscription. Each pill subscribes to a dedicated query
// hook from `topbar-data.ts` and click-throughs to the appropriate stage
// or Diagnostics. Co-located here because they share style hooks.

import { Link } from "@tanstack/react-router";
import { useEffect, useRef, useState } from "react";
import {
  Activity,
  AlertTriangle,
  CircleAlert,
  CircleCheck,
  CircleDashed,
  CircleHelp,
  CirclePause,
  GaugeCircle,
  ListChecks,
  Network,
  ServerCog,
  Zap,
} from "lucide-react";
import { useActivityStore, type ActivityEventInput } from "../activity/index.ts";
import {
  codeHealthAria,
  dotClass,
  subscriptionAria,
  toolHealthAria,
  useBeadsPulseQuery,
  useCodeHealthQuery,
  useSubscriptionUsageQuery,
  useSwarmStateQuery,
  useToolHealthQuery,
  type CodeHealthSummary,
  type HealthDot,
  type SubscriptionUsageSummary,
  type ToolHealthSnapshot,
} from "./topbar-data.ts";
import { selectVpsHealthDot, useTunnelStore } from "../tunnel/tunnel-store.ts";
import type { ShellProjectSummary } from "../store.ts";

interface PillProps {
  readonly project: ShellProjectSummary | null;
}

export interface PowerAssertionSnapshot {
  readonly active: boolean;
  readonly assertionId: string | null;
  readonly mechanism: "powersaveblocker" | "nsprocessinfo" | "caffeinate" | null;
  readonly level: "display" | "app-suspension" | "system" | null;
  readonly ownerRoundIds: readonly string[];
  readonly heldCount: number;
  readonly acquiredAt: string | null;
}

interface ResolvedPowerBridge {
  readonly snapshot: <O>() => Promise<O>;
}

type WindowTimerHandle = ReturnType<Window["setTimeout"]>;

interface PowerSnapshotPollerOptions {
  readonly bridge: ResolvedPowerBridge;
  readonly onSnapshot: (snapshot: PowerAssertionSnapshot) => void;
  readonly onFailure: (event: ActivityEventInput) => void;
  readonly now?: () => string;
  readonly pollIntervalMs?: number;
  readonly failureRetryMs?: number;
  readonly setTimeoutFn?: Window["setTimeout"];
  readonly clearTimeoutFn?: Window["clearTimeout"];
}

const POWER_SNAPSHOT_POLL_INTERVAL_MS = 5_000;
const POWER_SNAPSHOT_FAILURE_RETRY_MS = 30_000;

// ── Mac awake assertion ─────────────────────────────────────────────────────

export function PowerAssertionPill({ project }: PillProps) {
  const [snapshot, setSnapshot] = useState<PowerAssertionSnapshot | null>(null);
  const addEvent = useActivityStore((state) => state.addEvent);
  const announcedAssertionId = useRef<string | null>(null);

  useEffect(() => {
    const bridge = resolvePowerBridge();
    if (!bridge) return;
    return startPowerSnapshotPoller({
      bridge,
      onSnapshot: setSnapshot,
      onFailure: addEvent,
    });
  }, [addEvent]);

  useEffect(() => {
    if (!snapshot?.active || !snapshot.assertionId) return;
    if (announcedAssertionId.current === snapshot.assertionId) return;
    announcedAssertionId.current = snapshot.assertionId;
    addEvent({
      kind: "orchestrator.intervention",
      importance: "urgent",
      summary: `Pro round started — Mac will stay awake until ${snapshot.ownerRoundIds.join(", ") || "the round"} completes`,
      timestamp: new Date().toISOString(),
      actor: {
        id: "desktop-power",
        displayName: "Desktop power manager",
        kind: "system",
      },
      pills: [
        { id: "mechanism", label: snapshot.mechanism ?? "power assertion", tone: "warn" },
        { id: "held", label: `${snapshot.heldCount} active`, tone: "ok" },
      ],
      correlationId: snapshot.assertionId,
    });
  }, [addEvent, snapshot]);

  if (!snapshot?.active) return null;
  return (
    <PillLink
      ariaLabel={powerAssertionAria(snapshot)}
      data-testid="topbar-power-assertion"
      data-variant="warning"
      icon={<Zap size={14} strokeWidth={2.1} />}
      label="Awake"
      project={project}
      stage="diag"
    >
      <strong>{snapshot.heldCount}</strong>
      <span className="hh-pill-sep">·</span>
      <span>{snapshot.level ?? "held"}</span>
    </PillLink>
  );
}

export function startPowerSnapshotPoller({
  bridge,
  onSnapshot,
  onFailure,
  now = () => new Date().toISOString(),
  pollIntervalMs = POWER_SNAPSHOT_POLL_INTERVAL_MS,
  failureRetryMs = POWER_SNAPSHOT_FAILURE_RETRY_MS,
  setTimeoutFn = window.setTimeout.bind(window),
  clearTimeoutFn = window.clearTimeout.bind(window),
}: PowerSnapshotPollerOptions): () => void {
  let cancelled = false;
  let timer: WindowTimerHandle | null = null;
  let failureAnnounced = false;

  const schedule = (delayMs: number) => {
    if (cancelled) return;
    timer = setTimeoutFn(() => {
      void load();
    }, delayMs);
  };

  const load = async () => {
    try {
      const next = await bridge.snapshot<PowerAssertionSnapshot>();
      if (cancelled) return;
      failureAnnounced = false;
      onSnapshot(next);
      schedule(pollIntervalMs);
    } catch {
      if (cancelled) return;
      if (!failureAnnounced) {
        failureAnnounced = true;
        onFailure(buildPowerSnapshotFailureEvent(now()));
      }
      schedule(failureRetryMs);
    }
  };

  void load();

  return () => {
    cancelled = true;
    if (timer !== null) {
      clearTimeoutFn(timer);
    }
  };
}

export function buildPowerSnapshotFailureEvent(timestamp: string): ActivityEventInput {
  return {
    kind: "health.snapshot_updated",
    importance: "warn",
    summary: "Power assertion status unavailable; polling slowed while the bridge recovers",
    timestamp,
    actor: {
      id: "desktop-power",
      displayName: "Desktop power manager",
      kind: "system",
    },
    inlinePreview:
      "The top bar could not read hoopoe.power.snapshot. Pro-round power assertions may still be active; check Diagnostics if this persists.",
    pills: [
      { id: "surface", label: "Diagnostics", tone: "warn" },
      { id: "retry", label: "retrying slowly", tone: "muted" },
    ],
    correlationId: "desktop-power-snapshot",
  };
}

function resolvePowerBridge(): ResolvedPowerBridge | null {
  if (typeof window === "undefined") return null;
  const snapshot = window.hoopoe?.power?.snapshot;
  if (typeof snapshot !== "function") return null;
  return { snapshot };
}

export function powerAssertionAria(snapshot: PowerAssertionSnapshot): string {
  const rounds =
    snapshot.ownerRoundIds.length === 1
      ? snapshot.ownerRoundIds[0]
      : `${snapshot.ownerRoundIds.length} Pro rounds`;
  return `Mac kept awake for ${rounds}; ${snapshot.heldCount} active assertion${snapshot.heldCount === 1 ? "" : "s"} via ${snapshot.mechanism ?? "unknown mechanism"}`;
}

// ── Tool health ───────────────────────────────────────────────────────────

export function ToolHealthPill({ project }: PillProps) {
  const query = useToolHealthQuery(project);
  // hp-m79e: VPS dot prefers the live tunnel FSM snapshot over the
  // capability-registry view. The tunnel store reflects ConnectionManager
  // state changes (sleep/wake, heartbeat fail, reconnect) at sub-second
  // latency; the capabilities query is a 30s-stale read. Until the tunnel
  // store has received a snapshot (`receivedAt === null`), `selectVpsHealthDot`
  // returns `unknown` and the query data wins; once the FSM is live, the
  // store is the source of truth for the VPS dot specifically.
  const tunnelDot = useTunnelStore(selectVpsHealthDot);
  const tunnelHasSnapshot = useTunnelStore((s) => s.receivedAt !== null);
  const baseSnapshot = query.data ?? {
    vps: "unknown" as HealthDot,
    ntm: "unknown" as HealthDot,
    mail: "unknown" as HealthDot,
    br: "unknown" as HealthDot,
    bv: "unknown" as HealthDot,
    allHealthy: false,
    anyOffline: false,
  };
  const snapshot: ToolHealthSnapshot = tunnelHasSnapshot
    ? {
        ...baseSnapshot,
        vps: tunnelDot,
        allHealthy:
          tunnelDot === "healthy" &&
          baseSnapshot.ntm === "healthy" &&
          baseSnapshot.mail === "healthy" &&
          baseSnapshot.br === "healthy" &&
          baseSnapshot.bv === "healthy",
        anyOffline:
          tunnelDot === "offline" ||
          baseSnapshot.ntm === "offline" ||
          baseSnapshot.mail === "offline" ||
          baseSnapshot.br === "offline" ||
          baseSnapshot.bv === "offline",
      }
    : baseSnapshot;
  return (
    <PillLink
      ariaLabel={toolHealthAria(snapshot)}
      data-testid="topbar-tool-health"
      icon={<ServerCog size={14} strokeWidth={2.1} />}
      label="Tools"
      project={project}
      stage="diag"
    >
      <span className="hh-tool-dots" aria-hidden="true">
        <DotSpan dot={snapshot.vps} title="VPS daemon" />
        <DotSpan dot={snapshot.ntm} title="NTM" />
        <DotSpan dot={snapshot.mail} title="Agent Mail" />
        <DotSpan dot={snapshot.br} title="br" />
        <DotSpan dot={snapshot.bv} title="bv" />
      </span>
    </PillLink>
  );
}

function DotSpan({ dot, title }: { readonly dot: HealthDot; readonly title: string }) {
  return <span className={`hh-tool-dot ${dotClass(dot)}`} title={title} />;
}

// ── Swarm state ──────────────────────────────────────────────────────────

export function SwarmStatePill({ project }: PillProps) {
  const query = useSwarmStateQuery(project);
  const data = query.data ?? { running: 0, idle: 0, wedged: 0, total: 0 };
  const { Icon, variant } = swarmVisual(data);
  return (
    <PillLink
      ariaLabel={`Swarm: ${data.running} running, ${data.idle} idle, ${data.wedged} wedged`}
      data-testid="topbar-swarm"
      data-variant={variant}
      icon={<Icon size={14} strokeWidth={2.1} />}
      label="Swarm"
      project={project}
      stage="swarm"
    >
      <strong>{data.running}</strong>
      <span className="hh-pill-sep">/</span>
      <span>{data.idle}</span>
      {data.wedged > 0 ? (
        <>
          <span className="hh-pill-sep">·</span>
          <span className="hh-pill-alert">{data.wedged} wedged</span>
        </>
      ) : null}
    </PillLink>
  );
}

function swarmVisual(data: { readonly wedged: number; readonly running: number }) {
  if (data.wedged > 0) {
    return { Icon: AlertTriangle, variant: "alert" };
  }
  if (data.running > 0) {
    return { Icon: Activity, variant: "active" };
  }
  return { Icon: CirclePause, variant: "idle" };
}

// ── Beads pulse ──────────────────────────────────────────────────────────

export function BeadsPulsePill({ project }: PillProps) {
  const query = useBeadsPulseQuery(project);
  const data = query.data ?? { ready: 0, inProgress: 0, blocked: 0 };
  return (
    <PillLink
      ariaLabel={`Beads: ${data.ready} ready, ${data.inProgress} in progress, ${data.blocked} blocked`}
      data-testid="topbar-beads"
      icon={<ListChecks size={14} strokeWidth={2.1} />}
      label="Beads"
      project={project}
      stage="bead"
    >
      <strong>{data.ready}</strong>
      <span className="hh-pill-sep">·</span>
      <span>{data.inProgress} WIP</span>
      {data.blocked > 0 ? (
        <>
          <span className="hh-pill-sep">·</span>
          <span className="hh-pill-alert">{data.blocked} blocked</span>
        </>
      ) : null}
    </PillLink>
  );
}

// ── Code health ──────────────────────────────────────────────────────────

export function CodeHealthPill({ project }: PillProps) {
  const query = useCodeHealthQuery(project);
  const data: CodeHealthSummary = query.data ?? {
    coveragePercent: null,
    avgComplexity: null,
    hotspotCount: 0,
    lastSnapshotAgeMinutes: null,
    verdict: "unknown",
  };
  return (
    <PillLink
      ariaLabel={codeHealthAria(data)}
      data-testid="topbar-code-health"
      data-variant={data.verdict}
      icon={<GaugeCircle size={14} strokeWidth={2.1} />}
      label="Health"
      project={project}
      stage="harden"
    >
      {data.coveragePercent !== null ? (
        <strong>{data.coveragePercent}%</strong>
      ) : (
        <span className="hh-pill-muted">no snapshot</span>
      )}
      {data.hotspotCount > 0 ? (
        <>
          <span className="hh-pill-sep">·</span>
          <span className="hh-pill-alert">
            {data.hotspotCount} hotspot{data.hotspotCount === 1 ? "" : "s"}
          </span>
        </>
      ) : null}
    </PillLink>
  );
}

// ── Subscription usage ───────────────────────────────────────────────────

export function SubscriptionPill({ project }: PillProps) {
  const query = useSubscriptionUsageQuery();
  const data: SubscriptionUsageSummary = query.data ?? {
    providers: [],
    anyRateLimited: false,
    maxUsagePercent: 0,
  };
  // Subscription click-through points at Diagnostics (no dedicated stage).
  return (
    <PillLink
      ariaLabel={subscriptionAria(data)}
      data-testid="topbar-subscription"
      data-variant={data.anyRateLimited ? "alert" : data.maxUsagePercent > 80 ? "warning" : "ok"}
      icon={<Network size={14} strokeWidth={2.1} />}
      label="Subs"
      project={project}
      stage="diag"
    >
      {data.anyRateLimited ? (
        <span className="hh-pill-alert">rate-limited</span>
      ) : data.maxUsagePercent > 0 ? (
        <strong>{data.maxUsagePercent}%</strong>
      ) : (
        <span className="hh-pill-muted">idle</span>
      )}
    </PillLink>
  );
}

// ── Shared chrome ────────────────────────────────────────────────────────

interface PillLinkProps {
  readonly ariaLabel: string;
  readonly children: React.ReactNode;
  readonly icon: React.ReactNode;
  readonly label: string;
  readonly project: ShellProjectSummary | null;
  readonly stage: "diag" | "swarm" | "bead" | "harden";
  readonly "data-testid": string;
  readonly "data-variant"?: string;
}

function PillLink({ ariaLabel, children, icon, label, project, stage, ...rest }: PillLinkProps) {
  // Without a project, render a non-interactive pill so the chrome still
  // shows seed data on the project picker route.
  if (!project) {
    return (
      <span aria-label={ariaLabel} className="hh-topbar-pill hh-topbar-pill-disabled" {...rest}>
        {icon}
        <span>{label}</span>
        {children}
      </span>
    );
  }
  const stageRoute = STAGE_ROUTE[stage];
  return (
    <Link
      aria-label={ariaLabel}
      className="hh-topbar-pill hh-topbar-pill-link"
      params={{ projectId: project.id }}
      to={stageRoute}
      {...rest}
    >
      {icon}
      <span>{label}</span>
      {children}
    </Link>
  );
}

const STAGE_ROUTE = {
  diag: "/$projectId/diag",
  swarm: "/$projectId/swarm",
  bead: "/$projectId/bead",
  harden: "/$projectId/harden",
} as const;

// ── Loading-state primitives (re-exported for callers needing manual control) ──

export const HEALTH_DOT_ICONS = {
  healthy: CircleCheck,
  degraded: CircleAlert,
  offline: CircleAlert,
  unknown: CircleHelp,
} as const satisfies Record<HealthDot, typeof CircleCheck>;

export const SWARM_IDLE_ICON = CircleDashed;
