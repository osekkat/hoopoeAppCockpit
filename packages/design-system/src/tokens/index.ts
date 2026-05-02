export type ThemeName = "dark" | "light";

export type AgentFamily = "claude" | "codex" | "gemini" | "oracle";

export type StatusTone =
  | "ready"
  | "claimed"
  | "in_progress"
  | "in_review"
  | "closed"
  | "blocked"
  | "queued"
  | "running"
  | "waiting_approval"
  | "canceling"
  | "succeeded"
  | "failed"
  | "interrupted"
  | "pending"
  | "approved"
  | "denied"
  | "expired"
  | "superseded_by"
  | "ok"
  | "degraded"
  | "missing"
  | "blocked-by-policy"
  | "muted";

export type ToolHealthTone = "green" | "yellow" | "red";
export type PriorityChipVariant = "p0" | "p1" | "p2" | "p3" | "p4";
export type CoverageBand = "low" | "medium" | "high";

export interface ToneToken {
  readonly bg: string;
  readonly fg: string;
  readonly border: string;
  readonly dot: string;
}

export interface CoverageRampStop extends ToneToken {
  readonly threshold: number;
  readonly label: CoverageBand;
}

export const hoopoeTokens = {
  color: {
    brand: {
      russet: "#C25A2E",
      russetDeep: "#A0421C",
      russetDark: "#E58253",
      systemBlue: "#0A84FF",
    },
    surface: {
      dark: {
        base: "#1E1E20",
        baseDeep: "#0F0F11",
        panel: "#26262A",
        panelAlt: "#1F1F23",
        glass: "rgba(35,35,39,0.74)",
        sidebar: "rgba(18,18,20,0.78)",
        border: "rgba(255,255,255,0.14)",
        borderSoft: "rgba(255,255,255,0.08)",
        text: "#F3F1EA",
        textDim: "#B9B4AA",
        textMute: "#807A70",
      },
      light: {
        base: "#ECECEE",
        baseDeep: "#D8DBE0",
        panel: "#FFFFFF",
        panelAlt: "#F7F7F9",
        glass: "rgba(255,255,255,0.72)",
        sidebar: "rgba(250,249,247,0.78)",
        border: "rgba(31,35,40,0.14)",
        borderSoft: "rgba(31,35,40,0.08)",
        text: "#29261B",
        textDim: "#6B665A",
        textMute: "#9A9488",
      },
    },
    semantic: {
      success: "#30B66B",
      successDark: "#32D173",
      warning: "#E9963F",
      warningDark: "#F2A045",
      danger: "#E5484D",
      dangerDark: "#FF5C60",
      info: "#0A84FF",
      muted: "#8E8E93",
    },
    agent: {
      claude: "#C25A2E",
      codex: "#10A37F",
      gemini: "#4285F4",
      oracle: "#7A5CFF",
    },
  },
  typography: {
    sans: [
      "Inter",
      "Geist",
      "ui-sans-serif",
      "system-ui",
      "-apple-system",
      "BlinkMacSystemFont",
      "Segoe UI",
      "sans-serif",
    ],
    mono: [
      "JetBrains Mono",
      "SFMono-Regular",
      "Consolas",
      "Liberation Mono",
      "monospace",
    ],
  },
  spacing: {
    px: "1px",
    0: "0",
    0.5: "2px",
    1: "4px",
    1.5: "6px",
    2: "8px",
    3: "12px",
    4: "16px",
    5: "20px",
    6: "24px",
    8: "32px",
    10: "40px",
    12: "48px",
    16: "64px",
  },
  radius: {
    sm: "4px",
    md: "6px",
    lg: "8px",
    xl: "12px",
    full: "999px",
  },
  shadow: {
    glass: "0 24px 80px rgba(0,0,0,0.24)",
    panel: "0 10px 36px rgba(0,0,0,0.16)",
    soft: "0 4px 18px rgba(0,0,0,0.10)",
    inset: "0 1px 0 rgba(255,255,255,0.18) inset",
  },
} as const;

export const statusTones: Record<StatusTone, ToneToken> = {
  ready: tone("#EAF3FF", "#075EA8", "#B7D7FF", "#0A84FF"),
  claimed: tone("#F1EEFF", "#5A45C8", "#D3CAFF", "#7A5CFF"),
  in_progress: tone("#E8F7F1", "#0D6D46", "#B9E5D1", "#10A37F"),
  in_review: tone("#FFF4E7", "#9B5717", "#F8D2A5", "#E9963F"),
  closed: tone("#EAF7EE", "#1D6C40", "#BDE4CA", "#30B66B"),
  blocked: tone("#FFECEC", "#A92C31", "#FFC7C9", "#E5484D"),
  queued: tone("#F3F3F5", "#5E5E66", "#DADAE0", "#8E8E93"),
  running: tone("#E8F7F1", "#0D6D46", "#B9E5D1", "#10A37F"),
  waiting_approval: tone("#FFF4E7", "#9B5717", "#F8D2A5", "#E9963F"),
  canceling: tone("#F3F3F5", "#5E5E66", "#DADAE0", "#8E8E93"),
  succeeded: tone("#EAF7EE", "#1D6C40", "#BDE4CA", "#30B66B"),
  failed: tone("#FFECEC", "#A92C31", "#FFC7C9", "#E5484D"),
  interrupted: tone("#F1EEFF", "#5A45C8", "#D3CAFF", "#7A5CFF"),
  pending: tone("#F3F3F5", "#5E5E66", "#DADAE0", "#8E8E93"),
  approved: tone("#EAF7EE", "#1D6C40", "#BDE4CA", "#30B66B"),
  denied: tone("#FFECEC", "#A92C31", "#FFC7C9", "#E5484D"),
  expired: tone("#F3F3F5", "#5E5E66", "#DADAE0", "#8E8E93"),
  superseded_by: tone("#F1EEFF", "#5A45C8", "#D3CAFF", "#7A5CFF"),
  ok: tone("#EAF7EE", "#1D6C40", "#BDE4CA", "#30B66B"),
  degraded: tone("#FFF4E7", "#9B5717", "#F8D2A5", "#E9963F"),
  missing: tone("#FFECEC", "#A92C31", "#FFC7C9", "#E5484D"),
  "blocked-by-policy": tone("#FFECEC", "#A92C31", "#FFC7C9", "#E5484D"),
  muted: tone("#F3F3F5", "#5E5E66", "#DADAE0", "#8E8E93"),
};

export const toolHealthTones: Record<ToolHealthTone, ToneToken> = {
  green: tone("#EAF7EE", "#1D6C40", "#BDE4CA", "#30B66B"),
  yellow: tone("#FFF4E7", "#9B5717", "#F8D2A5", "#E9963F"),
  red: tone("#FFECEC", "#A92C31", "#FFC7C9", "#E5484D"),
};

export const priorityTones: Record<PriorityChipVariant, ToneToken> = {
  p0: tone("#FFECEC", "#A92C31", "#FFC7C9", "#E5484D"),
  p1: tone("#FFF4E7", "#9B5717", "#F8D2A5", "#E9963F"),
  p2: tone("#EAF3FF", "#075EA8", "#B7D7FF", "#0A84FF"),
  p3: tone("#F1EEFF", "#5A45C8", "#D3CAFF", "#7A5CFF"),
  p4: tone("#F3F3F5", "#5E5E66", "#DADAE0", "#8E8E93"),
};

export const agentFamilyTones: Record<AgentFamily, ToneToken> = {
  claude: tone("#FEF0EA", "#8A3A17", "#F3C9B7", "#C25A2E"),
  codex: tone("#E8F7F1", "#0A6C52", "#B9E5D1", "#10A37F"),
  gemini: tone("#EAF3FF", "#245DAE", "#B7D7FF", "#4285F4"),
  oracle: tone("#F1EEFF", "#533CC5", "#D3CAFF", "#7A5CFF"),
};

export const coverageRamp: ReadonlyArray<CoverageRampStop> = [
  toneStop(0, "low", "#FFECEC", "#A92C31", "#FFC7C9", "#E5484D"),
  toneStop(60, "medium", "#FFF4E7", "#9B5717", "#F8D2A5", "#E9963F"),
  toneStop(80, "high", "#EAF7EE", "#1D6C40", "#BDE4CA", "#30B66B"),
];

export const coverageThresholds = {
  low: 0,
  medium: 60,
  high: 80,
} as const;

export const cssVariableThemes: Record<ThemeName, Record<string, string>> = {
  dark: cssVariablesForTheme("dark"),
  light: cssVariablesForTheme("light"),
};

export const tailwindTokenTheme = {
  colors: {
    hoopoe: {
      russet: hoopoeTokens.color.brand.russet,
      "russet-deep": hoopoeTokens.color.brand.russetDeep,
      "russet-dark": hoopoeTokens.color.brand.russetDark,
      blue: hoopoeTokens.color.brand.systemBlue,
    },
    surface: {
      base: "var(--surface-base)",
      "base-deep": "var(--surface-base-deep)",
      panel: "var(--surface-panel)",
      "panel-alt": "var(--surface-panel-alt)",
      glass: "var(--surface-glass)",
      sidebar: "var(--surface-sidebar)",
      border: "var(--surface-border)",
      "border-soft": "var(--surface-border-soft)",
    },
    text: {
      primary: "var(--text-primary)",
      secondary: "var(--text-secondary)",
      muted: "var(--text-muted)",
    },
    status: tailwindToneColors(statusTones),
    priority: tailwindToneColors(priorityTones),
    agent: tailwindToneColors(agentFamilyTones),
    "tool-health": tailwindToneColors(toolHealthTones),
  },
  borderRadius: hoopoeTokens.radius,
  boxShadow: hoopoeTokens.shadow,
  spacing: hoopoeTokens.spacing,
  fontFamily: {
    sans: hoopoeTokens.typography.sans,
    mono: hoopoeTokens.typography.mono,
  },
} as const;

export type StatusPillVariant = StatusTone;
export const COVERAGE_RAMP = coverageRamp;

function tone(bg: string, fg: string, border: string, dot: string): ToneToken {
  return { bg, fg, border, dot };
}

function toneStop(
  threshold: number,
  label: CoverageBand,
  bg: string,
  fg: string,
  border: string,
  dot: string,
): CoverageRampStop {
  return { threshold, label, bg, fg, border, dot };
}

function tailwindToneColors<TTone extends string>(
  tones: Record<TTone, ToneToken>,
): Record<TTone, ToneToken> {
  return tones;
}

function cssVariablesForTheme(theme: ThemeName): Record<string, string> {
  const surface = hoopoeTokens.color.surface[theme];
  const brandRusset =
    theme === "dark"
      ? hoopoeTokens.color.brand.russetDark
      : hoopoeTokens.color.brand.russet;

  return {
    "--hoopoe-russet": brandRusset,
    "--hoopoe-russet-deep": hoopoeTokens.color.brand.russetDeep,
    "--hoopoe-blue": hoopoeTokens.color.brand.systemBlue,
    "--surface-base": surface.base,
    "--surface-base-deep": surface.baseDeep,
    "--surface-panel": surface.panel,
    "--surface-panel-alt": surface.panelAlt,
    "--surface-glass": surface.glass,
    "--surface-sidebar": surface.sidebar,
    "--surface-border": surface.border,
    "--surface-border-soft": surface.borderSoft,
    "--text-primary": surface.text,
    "--text-secondary": surface.textDim,
    "--text-muted": surface.textMute,
    "--status-success": hoopoeTokens.color.semantic.success,
    "--status-warning": hoopoeTokens.color.semantic.warning,
    "--status-danger": hoopoeTokens.color.semantic.danger,
    "--status-info": hoopoeTokens.color.semantic.info,
    "--status-muted": hoopoeTokens.color.semantic.muted,
    "--agent-claude": hoopoeTokens.color.agent.claude,
    "--agent-codex": hoopoeTokens.color.agent.codex,
    "--agent-gemini": hoopoeTokens.color.agent.gemini,
    "--agent-oracle": hoopoeTokens.color.agent.oracle,
    "--coverage-low-threshold": String(coverageThresholds.medium),
    "--coverage-high-threshold": String(coverageThresholds.high),
    "--radius-sm": hoopoeTokens.radius.sm,
    "--radius-md": hoopoeTokens.radius.md,
    "--radius-lg": hoopoeTokens.radius.lg,
    "--radius-full": hoopoeTokens.radius.full,
    "--shadow-glass": hoopoeTokens.shadow.glass,
    "--shadow-panel": hoopoeTokens.shadow.panel,
    "--shadow-soft": hoopoeTokens.shadow.soft,
    "--shadow-inset": hoopoeTokens.shadow.inset,
    ...toneVariables("status", statusTones),
    ...toneVariables("priority", priorityTones),
    ...toneVariables("agent", agentFamilyTones),
    ...toneVariables("tool-health", toolHealthTones),
  };
}

function toneVariables<TTone extends string>(
  prefix: string,
  tones: Record<TTone, ToneToken>,
): Record<string, string> {
  const variables: Record<string, string> = {};

  for (const [name, toneValue] of Object.entries<ToneToken>(tones)) {
    variables[`--${prefix}-${name}-bg`] = toneValue.bg;
    variables[`--${prefix}-${name}-fg`] = toneValue.fg;
    variables[`--${prefix}-${name}-border`] = toneValue.border;
    variables[`--${prefix}-${name}-dot`] = toneValue.dot;
  }

  return variables;
}
