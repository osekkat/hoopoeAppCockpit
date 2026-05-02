import { hoopoeTokens, statusTones } from "../../tokens/index.ts";
import type { ToneToken } from "../../tokens/index.ts";

/**
 * `TerminalPane` is the xterm.js-backed component that surfaces raw PTY
 * scrollback. **Per Hoopoe Guardrail #12 (plan.md Appendix C #12) it is
 * forbidden in the default Swarm UI.** Its only legitimate use is the
 * Diagnostics "Show raw pane" debug toggle, which audits every reveal.
 *
 * The model returned by `getTerminalPaneModel` enforces this at the shape
 * level: `surface` is always `"diagnostics"`, the renderer must read a
 * banner from `policyBanner`, and toggling visibility on requires the
 * caller to acknowledge `auditOnRevealAcknowledged: true`. A renderer that
 * tries to mount a TerminalPane without that flag is calling out of its
 * lane.
 */
export type TerminalPaneSurface = "diagnostics";

export type TerminalPaneVisibility = "hidden" | "revealed";

export interface TerminalPaneAuditEntry {
  readonly action: "show-raw-pane" | "hide-raw-pane";
  readonly agentId: string;
  readonly userId: string;
  readonly reason: string | null;
  readonly capturedAt: string;
}

export interface TerminalPaneProps {
  /** Constraint: must be the literal `"diagnostics"`. The compiler will
   * reject any other value, making misuse a build-time error. */
  readonly surface: TerminalPaneSurface;
  readonly agentId: string;
  readonly userId: string;
  readonly visibility: TerminalPaneVisibility;
  /** Caller acknowledges that the audit-on-reveal hook has fired (or will
   * fire synchronously around the visibility change). Without this flag,
   * `getTerminalPaneModel` clamps `visibility` to `"hidden"` and emits a
   * policy banner. */
  readonly auditOnRevealAcknowledged: boolean;
  readonly reasonForReveal?: string | null;
  /** Audit-log entries the caller has already produced. Surfaced verbatim
   * in the model so the diagnostics screen can show them. */
  readonly auditTrail?: ReadonlyArray<TerminalPaneAuditEntry>;
}

export interface TerminalPaneModel {
  readonly surface: TerminalPaneSurface;
  readonly visibility: TerminalPaneVisibility;
  readonly policyBanner: {
    readonly tone: ToneToken;
    readonly text: string;
    readonly auditMustFire: boolean;
  };
  readonly background: string;
  readonly foreground: string;
  readonly fontFamily: ReadonlyArray<string>;
  readonly auditTrail: ReadonlyArray<TerminalPaneAuditEntry>;
  readonly ariaLabel: string;
}

const HIDDEN_BANNER = {
  text:
    "Raw pane is hidden by default per Guardrail #12. Reveal requires an explicit audit entry.",
};

const REVEAL_BANNER = {
  text:
    "Raw pane revealed via Diagnostics. Every reveal is audit-logged; close when done forensic-inspecting.",
};

export function getTerminalPaneModel(props: TerminalPaneProps): TerminalPaneModel {
  // If the caller didn't acknowledge the audit, the pane stays hidden
  // regardless of the requested visibility. This is the structural enforcer
  // for Guardrail #12.
  const visibility: TerminalPaneVisibility =
    props.visibility === "revealed" && props.auditOnRevealAcknowledged
      ? "revealed"
      : "hidden";
  const banner = visibility === "revealed" ? REVEAL_BANNER : HIDDEN_BANNER;
  const tone = visibility === "revealed" ? statusTones.degraded : statusTones.muted;
  const auditTrail = props.auditTrail ?? [];
  const ariaLabel =
    visibility === "revealed"
      ? `Raw terminal pane for agent ${props.agentId}, revealed by ${props.userId}.`
      : `Raw terminal pane for agent ${props.agentId} is hidden by default.`;

  return {
    surface: props.surface,
    visibility,
    policyBanner: {
      tone,
      text: banner.text,
      auditMustFire: visibility === "revealed",
    },
    background: hoopoeTokens.color.surface.dark.baseDeep,
    foreground: hoopoeTokens.color.surface.dark.text,
    fontFamily: hoopoeTokens.typography.mono,
    auditTrail,
    ariaLabel,
  };
}

/** Construct the audit entry the caller must persist around a reveal/hide.
 * The diagnostics screen calls this and pipes the result through the
 * existing audit-log writer (hp-je1p). The model returned by
 * `getTerminalPaneModel` does not write to disk on its own — that's the
 * renderer's job. */
export function buildTerminalPaneAuditEntry(input: {
  readonly action: TerminalPaneAuditEntry["action"];
  readonly agentId: string;
  readonly userId: string;
  readonly reason: string | null;
  readonly clock?: () => Date;
}): TerminalPaneAuditEntry {
  const clock = input.clock ?? (() => new Date());
  return {
    action: input.action,
    agentId: input.agentId,
    userId: input.userId,
    reason: input.reason,
    capturedAt: clock().toISOString(),
  };
}
