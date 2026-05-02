import { hoopoeTokens } from "../../tokens/index.ts";
import { buildTerminalPaneAuditEntry, getTerminalPaneModel } from "./terminal-pane.ts";
import type { TerminalPaneProps } from "./terminal-pane.ts";

const meta = {
  title: "Components/TerminalPane",
  parameters: { layout: "fullscreen" },
};

export default meta;

export const HiddenByDefault = {
  render: () =>
    renderTerminalPane({
      surface: "diagnostics",
      agentId: "BlueHill",
      userId: "operator",
      visibility: "revealed",
      auditOnRevealAcknowledged: false,
      reasonForReveal: "Attempted reveal without audit acknowledgement",
    }),
};

export const DiagnosticsRevealed = {
  render: () =>
    renderTerminalPane({
      surface: "diagnostics",
      agentId: "FuchsiaPond",
      userId: "operator",
      visibility: "revealed",
      auditOnRevealAcknowledged: true,
      reasonForReveal: "Inspect stuck smoke runner output",
      auditTrail: [
        buildTerminalPaneAuditEntry({
          action: "show-raw-pane",
          agentId: "FuchsiaPond",
          userId: "operator",
          reason: "Inspect stuck smoke runner output",
          clock: () => new Date("2026-05-02T20:47:00.000Z"),
        }),
      ],
    }),
};

function renderTerminalPane(props: TerminalPaneProps): HTMLElement {
  const model = getTerminalPaneModel(props);
  const shell = document.createElement("main");
  const panel = document.createElement("section");
  const banner = document.createElement("p");
  const terminal = document.createElement("pre");
  const audit = document.createElement("ol");

  shell.style.minHeight = "100vh";
  shell.style.padding = "32px";
  shell.style.background = hoopoeTokens.color.surface.dark.baseDeep;
  shell.style.fontFamily = hoopoeTokens.typography.sans.join(", ");

  panel.setAttribute("aria-label", model.ariaLabel);
  panel.style.display = "grid";
  panel.style.gap = hoopoeTokens.spacing[4];
  panel.style.maxWidth = "920px";
  panel.style.padding = hoopoeTokens.spacing[4];
  panel.style.borderRadius = hoopoeTokens.radius.lg;
  panel.style.border = `1px solid ${hoopoeTokens.color.surface.dark.border}`;
  panel.style.background = hoopoeTokens.color.surface.dark.panel;
  panel.style.color = hoopoeTokens.color.surface.dark.text;

  banner.textContent = model.policyBanner.text;
  banner.style.margin = "0";
  banner.style.padding = hoopoeTokens.spacing[3];
  banner.style.borderRadius = hoopoeTokens.radius.md;
  banner.style.border = `1px solid ${model.policyBanner.tone.border}`;
  banner.style.background = model.policyBanner.tone.bg;
  banner.style.color = model.policyBanner.tone.fg;
  banner.style.fontSize = "13px";
  banner.style.fontWeight = "800";
  banner.style.letterSpacing = "0";

  terminal.textContent =
    model.visibility === "revealed"
      ? '$ ntm status --robot\n{"agent":"FuchsiaPond","state":"diagnostics"}\n$ tail -n 3 pane.log\n[diagnostics] raw pane stream attached'
      : "Raw pane hidden until Diagnostics records the reveal audit entry.";
  terminal.style.margin = "0";
  terminal.style.minHeight = "180px";
  terminal.style.padding = hoopoeTokens.spacing[4];
  terminal.style.borderRadius = hoopoeTokens.radius.md;
  terminal.style.border = `1px solid ${hoopoeTokens.color.surface.dark.border}`;
  terminal.style.background = model.background;
  terminal.style.color = model.foreground;
  terminal.style.fontFamily = model.fontFamily.join(", ");
  terminal.style.fontSize = "12px";
  terminal.style.lineHeight = "1.45";
  terminal.style.whiteSpace = "pre-wrap";

  audit.style.margin = "0";
  audit.style.paddingLeft = "20px";
  audit.style.color = hoopoeTokens.color.surface.dark.textDim;
  audit.style.fontSize = "12px";
  for (const entry of model.auditTrail) {
    const row = document.createElement("li");
    row.textContent = `${entry.capturedAt} ${entry.action} by ${entry.userId}`;
    audit.append(row);
  }

  panel.append(banner, terminal, audit);
  shell.append(panel);
  return shell;
}
