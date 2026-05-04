import { hoopoeTokens } from "../../tokens/index.ts";

const { spacing, radius, typography } = hoopoeTokens;
const { brand, surface } = hoopoeTokens.color;
const darkSurface = surface.dark;

export type StageHeaderStageId = "plan" | "bead" | "swarm" | "harden" | "diagnostics";
export type StageHeaderActionTone = "primary" | "secondary" | "danger";

export interface StageHeaderBreadcrumbItem {
  readonly label: string;
  readonly href?: string;
}

export interface StageHeaderAction {
  readonly id: string;
  readonly label: string;
  readonly ariaLabel?: string;
  readonly tone?: StageHeaderActionTone;
  readonly disabled?: boolean;
}

export interface StageHeaderProps {
  readonly stageId: StageHeaderStageId;
  readonly projectName: string;
  readonly breadcrumb?: readonly (string | StageHeaderBreadcrumbItem)[];
  readonly title?: string;
  readonly subtitle?: string | null;
  readonly actions?: readonly StageHeaderAction[];
  readonly activeActionId?: string | null;
  readonly onAction?: (action: StageHeaderAction, event: MouseEvent) => void;
}

export interface StageHeaderStageModel {
  readonly id: StageHeaderStageId;
  readonly number: string;
  readonly verb: string;
  readonly label: string;
}

export interface StageHeaderBreadcrumbModel {
  readonly label: string;
  readonly href: string | null;
}

export interface StageHeaderActionModel extends Required<Omit<StageHeaderAction, "ariaLabel">> {
  readonly ariaLabel: string;
  readonly active: boolean;
}

export interface StageHeaderModel {
  readonly stage: StageHeaderStageModel;
  readonly kicker: string;
  readonly title: string;
  readonly subtitle: string | null;
  readonly breadcrumbs: readonly StageHeaderBreadcrumbModel[];
  readonly actions: readonly StageHeaderActionModel[];
  readonly className: string;
  readonly ariaLabel: string;
  readonly style: Readonly<Record<string, string>>;
}

export const stageHeaderStages = {
  plan: { id: "plan", number: "01", verb: "PLAN", label: "Planning" },
  bead: { id: "bead", number: "02", verb: "BEAD", label: "Beads" },
  swarm: { id: "swarm", number: "03", verb: "SWARM", label: "Swarm" },
  harden: { id: "harden", number: "04", verb: "HARDEN", label: "Debugging / Hardening" },
  diagnostics: {
    id: "diagnostics",
    number: "DX",
    verb: "INSPECT",
    label: "Diagnostics",
  },
} as const satisfies Record<StageHeaderStageId, StageHeaderStageModel>;

export function getStageHeaderModel(props: StageHeaderProps): StageHeaderModel {
  const stage = stageHeaderStages[props.stageId];
  const breadcrumbs = normalizeBreadcrumbs(props.projectName, props.breadcrumb ?? []);
  const activeActionId = props.activeActionId ?? null;
  const actions = (props.actions ?? []).map((action) => ({
    id: action.id,
    label: action.label,
    ariaLabel: action.ariaLabel ?? action.label,
    tone: action.tone ?? "secondary",
    disabled: Boolean(action.disabled),
    active: action.id === activeActionId,
  }));
  const title = props.title ?? stage.label;

  return {
    stage,
    kicker: `STAGE ${stage.number} \u2014 ${stage.verb}`,
    title,
    subtitle: props.subtitle ?? null,
    breadcrumbs,
    actions,
    className: `hoopoe-stage-header hoopoe-stage-header--${stage.id}`,
    ariaLabel: `${stage.label} stage header`,
    style: stageHeaderStyle(),
  };
}

export function renderStageHeaderElement(
  props: StageHeaderProps,
  ownerDocument: Document = document,
): HTMLElement {
  const model = getStageHeaderModel(props);
  const header = ownerDocument.createElement("header");
  const heading = ownerDocument.createElement("div");
  const kicker = ownerDocument.createElement("span");
  const titleRow = ownerDocument.createElement("div");
  const marker = ownerDocument.createElement("span");
  const title = ownerDocument.createElement("h1");
  const breadcrumb = ownerDocument.createElement("nav");
  const actionRow = ownerDocument.createElement("div");

  header.className = model.className;
  header.setAttribute("aria-label", model.ariaLabel);
  assignStyle(header, model.style);

  heading.style.display = "grid";
  heading.style.gap = spacing[2];
  heading.style.minWidth = "0";

  kicker.textContent = model.kicker;
  assignStyle(kicker, kickerStyle());

  titleRow.style.display = "grid";
  titleRow.style.gridTemplateColumns = "auto minmax(0, 1fr)";
  titleRow.style.alignItems = "center";
  titleRow.style.gap = spacing[3];

  marker.textContent = model.stage.number;
  marker.setAttribute("aria-hidden", "true");
  assignStyle(marker, markerStyle());

  title.textContent = model.title;
  assignStyle(title, titleStyle());
  titleRow.append(marker, title);

  breadcrumb.setAttribute("aria-label", "Breadcrumb");
  breadcrumb.style.display = "flex";
  breadcrumb.style.flexWrap = "wrap";
  breadcrumb.style.alignItems = "center";
  breadcrumb.style.gap = spacing[1.5];
  appendBreadcrumbs(breadcrumb, model.breadcrumbs, ownerDocument);

  heading.append(kicker, titleRow, breadcrumb);

  if (model.subtitle !== null) {
    const subtitle = ownerDocument.createElement("p");
    subtitle.textContent = model.subtitle;
    assignStyle(subtitle, subtitleStyle());
    heading.append(subtitle);
  }

  actionRow.style.display = "flex";
  actionRow.style.flexWrap = "wrap";
  actionRow.style.justifyContent = "end";
  actionRow.style.alignItems = "center";
  actionRow.style.gap = spacing[2];

  for (const action of model.actions) {
    actionRow.append(renderActionButton(action, props.onAction, ownerDocument));
  }

  header.append(heading);
  if (model.actions.length > 0) {
    header.append(actionRow);
  }

  return header;
}

function normalizeBreadcrumbs(
  projectName: string,
  breadcrumb: readonly (string | StageHeaderBreadcrumbItem)[],
): readonly StageHeaderBreadcrumbModel[] {
  return [projectName, ...breadcrumb].map((item) => {
    if (typeof item === "string") {
      return { label: item, href: null };
    }

    return { label: item.label, href: item.href ?? null };
  });
}

function appendBreadcrumbs(
  nav: HTMLElement,
  breadcrumbs: readonly StageHeaderBreadcrumbModel[],
  ownerDocument: Document,
): void {
  breadcrumbs.forEach((item, index) => {
    if (index > 0) {
      const separator = ownerDocument.createElement("span");
      separator.textContent = "/";
      separator.setAttribute("aria-hidden", "true");
      separator.style.color = darkSurface.textMute;
      nav.append(separator);
    }

    const node =
      item.href === null
        ? ownerDocument.createElement("span")
        : ownerDocument.createElement("a");

    node.textContent = item.label;
    if (item.href !== null) {
      node.setAttribute("href", item.href);
    }
    assignStyle(node, breadcrumbItemStyle(index === breadcrumbs.length - 1));
    nav.append(node);
  });
}

function renderActionButton(
  action: StageHeaderActionModel,
  onAction: StageHeaderProps["onAction"],
  ownerDocument: Document,
): HTMLButtonElement {
  const button = ownerDocument.createElement("button");

  button.type = "button";
  button.textContent = action.label;
  button.disabled = action.disabled;
  button.setAttribute("aria-label", action.ariaLabel);
  button.setAttribute("aria-pressed", action.active ? "true" : "false");
  assignStyle(button, actionButtonStyle(action));
  button.onclick = (event) => onAction?.(action, event);

  return button;
}

function stageHeaderStyle(): Readonly<Record<string, string>> {
  return {
    display: "grid",
    gridTemplateColumns: "minmax(0, 1fr) auto",
    alignItems: "start",
    gap: spacing[5],
    padding: `${spacing[5]} ${spacing[6]}`,
    borderBottom: `1px solid ${darkSurface.border}`,
    background: darkSurface.panelAlt,
    color: darkSurface.text,
    fontFamily: typography.sans.join(", "),
  };
}

function kickerStyle(): Readonly<Record<string, string>> {
  return {
    color: brand.russetDark,
    fontSize: "11px",
    fontWeight: "850",
    letterSpacing: "0",
  };
}

function markerStyle(): Readonly<Record<string, string>> {
  return {
    display: "inline-grid",
    placeItems: "center",
    minWidth: "34px",
    height: "34px",
    borderRadius: radius.lg,
    border: `1px solid ${darkSurface.border}`,
    background: darkSurface.panel,
    color: darkSurface.text,
    fontSize: "12px",
    fontWeight: "850",
  };
}

function titleStyle(): Readonly<Record<string, string>> {
  return {
    margin: "0",
    color: darkSurface.text,
    fontSize: "26px",
    fontWeight: "850",
    lineHeight: "1.1",
    letterSpacing: "0",
  };
}

function subtitleStyle(): Readonly<Record<string, string>> {
  return {
    margin: "0",
    maxWidth: "680px",
    color: darkSurface.textDim,
    fontSize: "13px",
    lineHeight: "1.45",
  };
}

function breadcrumbItemStyle(active: boolean): Readonly<Record<string, string>> {
  return {
    color: active ? darkSurface.text : darkSurface.textDim,
    fontSize: "12px",
    fontWeight: active ? "750" : "600",
    lineHeight: "1.2",
    textDecoration: "none",
  };
}

function actionButtonStyle(
  action: StageHeaderActionModel,
): Readonly<Record<string, string>> {
  const tone =
    action.tone === "danger"
      ? {
          background: "#FFECEC",
          color: "#A92C31",
          border: "#FFC7C9",
        }
      : action.tone === "primary"
        ? {
            background: brand.russet,
            color: "#FFFFFF",
            border: brand.russetDeep,
          }
        : {
            background: darkSurface.panel,
            color: darkSurface.text,
            border: darkSurface.border,
          };

  return {
    minHeight: "34px",
    padding: `${spacing[2]} ${spacing[3]}`,
    borderRadius: radius.md,
    border: `1px solid ${tone.border}`,
    background: action.active ? darkSurface.baseDeep : tone.background,
    color: tone.color,
    font: "inherit",
    fontSize: "12px",
    fontWeight: "750",
    letterSpacing: "0",
    opacity: action.disabled ? "0.52" : "1",
    cursor: action.disabled ? "not-allowed" : "pointer",
  };
}

function assignStyle(element: HTMLElement, styles: Readonly<Record<string, string>>): void {
  for (const [name, value] of Object.entries(styles)) {
    element.style.setProperty(kebabCase(name), value);
  }
}

function kebabCase(value: string): string {
  return value.replace(/[A-Z]/g, (match) => `-${match.toLowerCase()}`);
}
