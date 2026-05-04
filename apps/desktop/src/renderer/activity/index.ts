// Public entry point for the Activity drawer (hp-1r4).

export { ActivityDrawer } from "./ActivityDrawer.tsx";
export type { ActivityDrawerProps } from "./ActivityDrawer.tsx";

export {
  applyFilter,
  buildFixtureEvents,
  resetActivityStoreForTests,
  useActivityStore,
} from "./store.ts";
export type {
  ActivityEventInput,
  ActivityStoreState,
} from "./store.ts";

export {
  ACTIVITY_CATEGORIES,
  ACTIVITY_CATEGORY_LABELS,
  EMPTY_FILTER,
  categoryFor,
  mapToTimelineKind,
} from "./types.ts";
export type {
  ActivityActor,
  ActivityCategory,
  ActivityEvent,
  ActivityEventKind,
  ActivityFilter,
  ActivityImportance,
  ActivityPill,
  ActivityPivot,
} from "./types.ts";

export type { ActivityContextAction } from "./TimelineList.tsx";
