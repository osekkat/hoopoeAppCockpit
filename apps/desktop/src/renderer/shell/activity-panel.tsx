// hp-1r4 superseded the original placeholder activity-panel.tsx (a
// 65-line static row list) with the real Activity drawer in
// `apps/desktop/src/renderer/activity/`. This file remains as a thin
// re-export so any consumer that imported `ActivityPanel` from here
// keeps working without source changes.
//
// New code should import from `../activity/index.ts` directly.

export { ActivityDrawer as ActivityPanel } from "../activity/index.ts";
export type { ActivityDrawerProps as ActivityPanelProps } from "../activity/index.ts";
