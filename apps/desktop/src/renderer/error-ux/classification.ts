// Severity + surface derivation (hp-8dym).
//
// The daemon's problem-type registry already classifies surface +
// actionability per problem id. The renderer only needs to:
//   1. Derive a coarse severity bucket from `envelope.status` when the
//      caller did not provide one. Status codes are the canonical
//      classification; renderer-side overrides are exceptional.
//   2. Decide the final routing surface, honoring an explicit override
//      when the publisher chose one.
//
// Behavior table for severity derivation:
//   status < 300            → "info"
//   status 300-399          → "info"
//   status 400-499          → "warning" (or "error" if 422 or 423)
//   status 500-599          → "error"
//   actionability "manual"  → bumps "warning" → "error" because the
//                             user has nothing automatic to try.
//
// The (severity × actionability → surface) matrix in hp-8dym's spec
// lives on the *registry* side: the daemon's YAML pre-classifies every
// error to a `surface`. The renderer trusts that classification by
// default and only overrides when the publisher passes
// `surfaceOverride`.

import type {
  ErrorSeverity,
  ProblemActionability,
  ProblemEnvelope,
  ProblemSurface,
} from "./types.ts";

export function deriveSeverity(
  envelope: ProblemEnvelope,
  explicit?: ErrorSeverity,
): ErrorSeverity {
  if (explicit !== undefined) return explicit;
  const status = envelope.status;
  if (status >= 500) return "error";
  if (status === 422 || status === 423) return "error";
  if (status >= 400) {
    if (envelope.actionability === "manual") return "error";
    return "warning";
  }
  return "info";
}

export function deriveSurface(
  envelope: ProblemEnvelope,
  override?: ProblemSurface,
): ProblemSurface {
  return override ?? envelope.surface;
}

export function isModalSurface(surface: ProblemSurface): boolean {
  return surface === "blocking_modal";
}

export function isToastSurface(surface: ProblemSurface): boolean {
  return surface === "toast";
}

export function isBannerSurface(surface: ProblemSurface): boolean {
  return surface === "banner";
}

export function isInlinePillSurface(surface: ProblemSurface): boolean {
  return surface === "inline_pill";
}

/** Default ARIA live politeness given a severity. Info is `polite`,
 *  warnings and above are `assertive`. */
export function ariaLiveFor(severity: ErrorSeverity): "polite" | "assertive" {
  return severity === "info" ? "polite" : "assertive";
}

/** Default dismissibility given severity + surface. Blocking severity
 *  on a modal is the only non-dismissible default. */
export function defaultDismissible(
  severity: ErrorSeverity,
  surface: ProblemSurface,
  explicit?: boolean,
): boolean {
  if (explicit !== undefined) return explicit;
  if (severity === "blocking" && surface === "blocking_modal") return false;
  return true;
}

export function defaultAutoDismissMs(
  severity: ErrorSeverity,
  surface: ProblemSurface,
  explicit?: number,
): number | null {
  if (surface !== "toast") return null;
  if (explicit !== undefined) {
    return Math.min(Math.max(explicit, 1000), 30_000);
  }
  switch (severity) {
    case "info":
      return 5_000;
    case "warning":
      return 7_000;
    case "error":
      return 10_000;
    case "critical":
      return 15_000;
    case "blocking":
      return null;
    default:
      return null;
  }
}

/** Default action-button label when none is supplied. Drawn from
 *  hp-g6sp's actionability vocabulary. */
export function defaultActionLabel(actionability: ProblemActionability): string {
  switch (actionability) {
    case "reload":
      return "Reload";
    case "re-pair":
      return "Re-pair";
    case "edit-deps":
      return "Edit dependencies";
    case "switch-account":
      return "Switch account";
    case "open-docs":
      return "Open docs";
    case "manual":
      return "Acknowledge";
  }
}
