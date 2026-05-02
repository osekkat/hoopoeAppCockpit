// Hoopoe-owned. Renderer-side settings model (hp-wg5p).
//
// The Settings screen renders rows from a typed catalog of
// SettingDescriptor objects. The catalog is the single source of truth
// for: which settings exist, what their widgets are, what defaults
// apply, what validation runs, what audit policy fires, and how source
// resolution (default ← global ← project ← env) presents.
//
// This file is renderer-only — no Node imports. The main-process
// SettingsBridge owns persistence + actually writing audits via
// SettingsAuditTrail.ts.
//
// Cross-references:
//   - bead hp-wg5p
//   - apps/desktop/src/main/SettingsBridge.ts (persistence)
//   - apps/desktop/src/main/SettingsAuditTrail.ts (audit policy)

export type SettingSection =
  | "global"
  | "project"
  | "accounts"
  | "skills"
  | "diagnostics"
  | "about";

export type SettingWidgetKind =
  | "toggle"
  | "enum"
  | "number"
  | "text"
  | "path"
  | "json"
  | "readonly";

export type SettingSourceTier = "default" | "global" | "project" | "env";

export interface SourceResolution<T> {
  readonly tier: SettingSourceTier;
  readonly value: T;
}

export interface SettingDescriptor<T = unknown> {
  /** Dotted key into the resolved settings tree (matches SettingsBridge). */
  readonly key: string;
  /** Section this setting renders under in the left rail. */
  readonly section: SettingSection;
  /** Short user-facing label. */
  readonly label: string;
  /** One-sentence description shown beneath the widget. */
  readonly description: string;
  /** Widget kind — drives which renderer the row uses. */
  readonly widget: SettingWidgetKind;
  /** Default value. */
  readonly defaultValue: T;
  /** When `widget==="enum"`, the allowed values + per-value labels. */
  readonly options?: ReadonlyArray<{ readonly value: T; readonly label: string }>;
  /** Validator returning either `null` (ok) or an error message. */
  readonly validate?: (value: unknown) => string | null;
  /** When true, changing this setting writes an audit entry (mirrors
   *  `SettingsAuditTrail.SECURITY_RELEVANT_SETTING_KEYS`; the renderer
   *  surfaces "this change is audited" inline). */
  readonly audited?: boolean;
  /** When true, the setting requires desktop relaunch — banner shows
   *  "Restart Hoopoe to apply." after change. */
  readonly restartRequired?: boolean;
  /** When true, hide this setting unless `dev mode` is on — used for
   *  mock-flywheel toggle and other developer-only settings. */
  readonly devOnly?: boolean;
  /** Searchable keywords (in addition to label + description). */
  readonly keywords?: readonly string[];
}

/** Curated catalog. Adding a setting here adds the row to the screen. */
export const SETTING_DESCRIPTORS: readonly SettingDescriptor[] = [
  // ----- Global -----
  {
    key: "desktop.updateChannel",
    section: "global",
    label: "Update channel",
    description: "Stable releases (latest) or nightly previews. Restart required.",
    widget: "enum",
    defaultValue: "latest",
    options: [
      { value: "latest", label: "Stable (latest)" },
      { value: "nightly", label: "Nightly preview" },
    ],
    audited: true,
    restartRequired: true,
    keywords: ["update", "channel", "release", "version"],
  },
  {
    key: "desktop.telemetryOptIn",
    section: "global",
    label: "Telemetry",
    description: "Send anonymized usage to Hoopoe maintainers. Off by default.",
    widget: "toggle",
    defaultValue: false,
    audited: true,
    keywords: ["telemetry", "analytics", "metrics", "privacy"],
  },
  {
    key: "desktop.mockFlywheelEnabled",
    section: "global",
    label: "Mock Flywheel mode",
    description:
      "Boot the desktop against a fixture corpus instead of a real VPS. Developer-only.",
    widget: "toggle",
    defaultValue: false,
    audited: true,
    devOnly: true,
    keywords: ["mock", "flywheel", "fixture", "demo", "developer"],
  },
  {
    key: "desktop.serverExposureMode",
    section: "global",
    label: "Daemon network exposure",
    description:
      "local-only (default; safe) or network-accessible (advanced; required for tailnet).",
    widget: "enum",
    defaultValue: "local-only",
    options: [
      { value: "local-only", label: "Local only (recommended)" },
      { value: "network-accessible", label: "Network accessible" },
    ],
    audited: true,
    restartRequired: true,
    keywords: ["network", "exposure", "tailnet", "binding", "security"],
  },
  {
    key: "desktop.editorCommand",
    section: "global",
    label: 'Editor command for "Open in editor"',
    description: "Shell command used when clicking deep-links to source files.",
    widget: "text",
    defaultValue: "code",
    keywords: ["editor", "vscode", "vim", "open"],
  },
  {
    key: "desktop.logRetentionDays",
    section: "global",
    label: "Log retention (days)",
    description: "How many days of structured logs to keep before pruning.",
    widget: "number",
    defaultValue: 14,
    validate: (v) => (typeof v === "number" && v >= 1 && v <= 365 ? null : "1–365"),
    keywords: ["log", "retention", "prune"],
  },

  // ----- Project -----
  {
    key: "project.localCloneSoftCapMb",
    section: "project",
    label: "Local clone soft cap (MB)",
    description: "Warning threshold for the desktop's sync mirror size.",
    widget: "number",
    defaultValue: 1024,
    validate: (v) => (typeof v === "number" && v >= 64 ? null : "≥ 64 MB"),
    keywords: ["clone", "cap", "size", "disk"],
  },
  {
    key: "project.localCloneHardCapMb",
    section: "project",
    label: "Local clone hard cap (MB)",
    description: "Hard limit; sync stops above this.",
    widget: "number",
    defaultValue: 4096,
    validate: (v) => (typeof v === "number" && v >= 128 ? null : "≥ 128 MB"),
    keywords: ["clone", "cap", "size", "disk"],
  },
  {
    key: "project.pushPolicy",
    section: "project",
    label: "Push policy",
    description: "When agents push their bead branch to origin.",
    widget: "enum",
    defaultValue: "auto-push-every-commit",
    options: [
      { value: "auto-push-every-commit", label: "After every commit" },
      { value: "auto-push-on-test-pass", label: "After successful test run" },
      { value: "manual", label: "Manual" },
    ],
    audited: true,
    keywords: ["push", "git", "auto-push", "policy"],
  },
  {
    key: "project.safetyPreset",
    section: "project",
    label: "Default swarm safety preset",
    description: "How aggressive the unified approvals queue is for this project.",
    widget: "enum",
    defaultValue: "supervised",
    options: [
      { value: "supervised", label: "Supervised (every action approved)" },
      { value: "guided", label: "Guided (low-risk auto, others approved)" },
      { value: "autopilot", label: "Autopilot (audit-only, no approval prompts)" },
    ],
    audited: true,
    keywords: ["safety", "approval", "autopilot", "supervised"],
  },

  // ----- Accounts -----
  {
    key: "accounts.caamSummary",
    section: "accounts",
    label: "CAAM accounts",
    description: "Active accounts per provider; switch via CAAM (audited).",
    widget: "readonly",
    defaultValue: "—",
    keywords: ["caam", "account", "provider", "claude", "gpt", "gemini"],
  },
  {
    key: "accounts.jsmSubscription",
    section: "accounts",
    label: "jsm subscription",
    description: "Premium skills source. jfp fallback is always active.",
    widget: "readonly",
    defaultValue: "—",
    keywords: ["jsm", "subscription", "skills", "jeffreys"],
  },

  // ----- Skills -----
  {
    key: "skills.installerPreference",
    section: "skills",
    label: "Skill installer preference",
    description: "jsm preferred (SHA-pinned) or jfp only (advisory).",
    widget: "enum",
    defaultValue: "jsm-preferred",
    options: [
      { value: "jsm-preferred", label: "jsm preferred (jfp fallback)" },
      { value: "jfp-only", label: "jfp only" },
    ],
    audited: true,
    keywords: ["jsm", "jfp", "skill", "installer"],
  },
  {
    key: "skills.lockfile",
    section: "skills",
    label: "Skills lockfile",
    description: ".hoopoe/skills.lock.json — current pinned skill set (audited on write).",
    widget: "readonly",
    defaultValue: "—",
    audited: true,
    keywords: ["skill", "lock", "sha", "pin"],
  },

  // ----- Diagnostics -----
  {
    key: "diagnostics.showRawPaneToggle",
    section: "diagnostics",
    label: "Show raw pane toggle (per-agent default)",
    description: "Default OFF. Per-agent override available in Diagnostics.",
    widget: "toggle",
    defaultValue: false,
    keywords: ["pane", "raw", "terminal", "diagnostics", "scrollback"],
  },
  {
    key: "diagnostics.recoveryShellAccess",
    section: "diagnostics",
    label: "Recovery shell access",
    description: "Enable last-resort shell access from Diagnostics. Audited.",
    widget: "toggle",
    defaultValue: false,
    audited: true,
    keywords: ["recovery", "shell", "diagnostics", "emergency"],
  },

  // ----- About -----
  {
    key: "about.hoopoeVersion",
    section: "about",
    label: "Hoopoe version",
    description: "Desktop app build identifier.",
    widget: "readonly",
    defaultValue: "0.0.0-dev",
    keywords: ["about", "version", "build"],
  },
  {
    key: "about.daemonVersion",
    section: "about",
    label: "Daemon version",
    description: "Connected daemon binary identifier.",
    widget: "readonly",
    defaultValue: "—",
    keywords: ["about", "daemon", "version"],
  },
];

export const SECTION_LABELS: Record<SettingSection, string> = {
  global: "Global",
  project: "Project",
  accounts: "Accounts",
  skills: "Skills",
  diagnostics: "Diagnostics",
  about: "About",
};

export const SECTION_ORDER: readonly SettingSection[] = [
  "global",
  "project",
  "accounts",
  "skills",
  "diagnostics",
  "about",
];

/** Build the source-resolution tier for a given setting under the
 *  default-←-global-←-project-←-env precedence. Pure function so it can
 *  be unit-tested. */
export function resolveSettingSource<T>(
  defaults: Record<string, unknown>,
  global: Record<string, unknown>,
  project: Record<string, unknown>,
  env: Record<string, unknown>,
  key: string,
): SourceResolution<T> {
  const envV = readDotted(env, key);
  if (envV !== undefined) return { tier: "env", value: envV as T };
  const projectV = readDotted(project, key);
  if (projectV !== undefined) return { tier: "project", value: projectV as T };
  const globalV = readDotted(global, key);
  if (globalV !== undefined) return { tier: "global", value: globalV as T };
  const defaultV = readDotted(defaults, key);
  return { tier: "default", value: defaultV as T };
}

/** Group descriptors by section. */
export function groupBySections(
  descriptors: readonly SettingDescriptor[] = SETTING_DESCRIPTORS,
): Record<SettingSection, SettingDescriptor[]> {
  const out: Record<SettingSection, SettingDescriptor[]> = {
    global: [],
    project: [],
    accounts: [],
    skills: [],
    diagnostics: [],
    about: [],
  };
  for (const d of descriptors) {
    out[d.section].push(d);
  }
  return out;
}

function readDotted(obj: Record<string, unknown>, key: string): unknown {
  const parts = key.split(".");
  let cur: unknown = obj;
  for (const p of parts) {
    if (cur == null || typeof cur !== "object") return undefined;
    cur = (cur as Record<string, unknown>)[p];
  }
  return cur;
}
