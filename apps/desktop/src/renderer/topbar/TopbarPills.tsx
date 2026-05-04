// hp-4ya — Five top-bar pill components: Tool health, Swarm, Beads,
// Code health, Subscription. Each pill subscribes to a dedicated query
// hook from `topbar-data.ts` and click-throughs to the appropriate stage
// or Diagnostics. Co-located here because they share style hooks.

import { Link } from "@tanstack/react-router";
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
} from "lucide-react";
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
  const Icon = data.wedged > 0 ? AlertTriangle : data.running > 0 ? Activity : CirclePause;
  const variant = data.wedged > 0 ? "alert" : data.running > 0 ? "active" : "idle";
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
          <span className="hh-pill-alert">{data.hotspotCount} hotspot{data.hotspotCount === 1 ? "" : "s"}</span>
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

function PillLink({
  ariaLabel,
  children,
  icon,
  label,
  project,
  stage,
  ...rest
}: PillLinkProps) {
  // Without a project, render a non-interactive pill so the chrome still
  // shows seed data on the project picker route.
  if (!project) {
    return (
      <span
        aria-label={ariaLabel}
        className="hh-topbar-pill hh-topbar-pill-disabled"
        {...rest}
      >
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
