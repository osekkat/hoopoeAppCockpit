import { agentFamilyTones, statusTones } from "../../tokens/index.ts";
import type { AgentFamily, ToneToken } from "../../tokens/index.ts";

export type TimelineRowKind =
  | "mail"
  | "reservation"
  | "build"
  | "approval"
  | "audit"
  | "agent-decision"
  | "user-message"
  | "orchestrator-reply";

export interface TimelineRowActor {
  readonly id: string;
  readonly displayName: string;
  readonly kind: "user" | "agent" | "system" | "orchestrator";
  readonly harness?: AgentFamily;
}

export interface TimelineRowPill {
  readonly id: string;
  readonly label: string;
  readonly tone?: ToneToken;
}

export interface TimelineRowProps {
  readonly id: string;
  readonly kind: TimelineRowKind;
  readonly timestampLabel: string;
  readonly actor: TimelineRowActor;
  readonly summary: string;
  readonly pills?: ReadonlyArray<TimelineRowPill>;
  readonly inlinePreview?: string | null;
  readonly clickTarget?: string | null;
  readonly unread?: boolean;
}

export interface TimelineRowModel {
  readonly id: string;
  readonly kind: TimelineRowKind;
  readonly kindLabel: string;
  readonly kindMarker: string;
  readonly timestampLabel: string;
  readonly actor: TimelineRowActor & { readonly tone: ToneToken | null };
  readonly summary: string;
  readonly pills: ReadonlyArray<TimelineRowPill>;
  readonly inlinePreview: string | null;
  readonly clickTarget: string | null;
  readonly unread: boolean;
  readonly ariaLabel: string;
}

const KIND_LABELS: Record<TimelineRowKind, string> = {
  mail: "Mail",
  reservation: "Reservation",
  build: "Build",
  approval: "Approval",
  audit: "Audit",
  "agent-decision": "Agent decision",
  "user-message": "User message",
  "orchestrator-reply": "Orchestrator reply",
};

const KIND_MARKERS: Record<TimelineRowKind, string> = {
  mail: "✉",
  reservation: "🔒",
  build: "⚙",
  approval: "?",
  audit: "✓",
  "agent-decision": "◆",
  "user-message": "›",
  "orchestrator-reply": "‹",
};

export const timelineRowKinds: ReadonlyArray<TimelineRowKind> = [
  "mail",
  "reservation",
  "build",
  "approval",
  "audit",
  "agent-decision",
  "user-message",
  "orchestrator-reply",
];

export function getTimelineRowModel(props: TimelineRowProps): TimelineRowModel {
  const kindLabel = KIND_LABELS[props.kind];
  const kindMarker = KIND_MARKERS[props.kind];
  const actorTone =
    props.actor.kind === "agent" && props.actor.harness
      ? agentFamilyTones[props.actor.harness]
      : props.actor.kind === "user"
        ? statusTones.muted
        : null;
  const ariaLabel = `${kindLabel} from ${props.actor.displayName} at ${props.timestampLabel}: ${props.summary}`;
  return {
    id: props.id,
    kind: props.kind,
    kindLabel,
    kindMarker,
    timestampLabel: props.timestampLabel,
    actor: { ...props.actor, tone: actorTone },
    summary: props.summary,
    pills: props.pills ?? [],
    inlinePreview: props.inlinePreview ?? null,
    clickTarget: props.clickTarget ?? null,
    unread: Boolean(props.unread),
    ariaLabel,
  };
}
