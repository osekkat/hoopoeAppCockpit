// Hoopoe-owned. Activity drawer filter bar — category multiselect + text
// search + importance pills + reset. Time-range and bead/agent filters
// land when those signals exist (post hp-3se / hp-tg6); the public
// API already accepts them so wiring is mechanical.

import {
  ACTIVITY_CATEGORIES,
  ACTIVITY_CATEGORY_LABELS,
  type ActivityCategory,
  type ActivityFilter,
  type ActivityImportance,
} from "./types.ts";

const IMPORTANCE_OPTIONS: ReadonlyArray<{ id: ActivityImportance; label: string }> = [
  { id: "info", label: "Info" },
  { id: "warn", label: "Warn" },
  { id: "urgent", label: "Urgent" },
];

export interface FilterBarProps {
  readonly filter: ActivityFilter;
  readonly onToggleCategory: (category: ActivityCategory) => void;
  readonly onToggleImportance: (importance: ActivityImportance) => void;
  readonly onSetText: (text: string) => void;
  readonly onReset: () => void;
}

export function FilterBar({
  filter,
  onToggleCategory,
  onToggleImportance,
  onSetText,
  onReset,
}: FilterBarProps) {
  const hasActiveFilter =
    filter.categories.length > 0 ||
    filter.importance.length > 0 ||
    filter.text.length > 0 ||
    filter.relatedBeadId !== null ||
    filter.relatedAgentId !== null ||
    filter.sinceTs !== null;

  return (
    <div
      className="hh-activity-filterbar"
      role="search"
      aria-label="Activity filters"
    >
      <div className="hh-activity-filterbar-row">
        <input
          aria-label="Search activity"
          className="hh-activity-search"
          placeholder="Search summary, actor, pills…"
          type="search"
          value={filter.text}
          onChange={(event) => onSetText(event.currentTarget.value)}
        />
        {hasActiveFilter && (
          <button
            className="hh-text-button"
            onClick={onReset}
            type="button"
            aria-label="Reset activity filters"
          >
            Reset
          </button>
        )}
      </div>

      <div
        className="hh-activity-filterbar-row"
        role="group"
        aria-label="Filter by importance"
      >
        {IMPORTANCE_OPTIONS.map((opt) => {
          const active = filter.importance.includes(opt.id);
          return (
            <button
              aria-pressed={active}
              className="hh-activity-filter-chip"
              data-active={active}
              data-importance={opt.id}
              key={opt.id}
              onClick={() => onToggleImportance(opt.id)}
              type="button"
            >
              {opt.label}
            </button>
          );
        })}
      </div>

      <div
        className="hh-activity-filterbar-row hh-activity-filterbar-categories"
        role="group"
        aria-label="Filter by category"
      >
        {ACTIVITY_CATEGORIES.map((category) => {
          const active = filter.categories.includes(category);
          return (
            <button
              aria-pressed={active}
              className="hh-activity-filter-chip"
              data-active={active}
              data-category={category}
              key={category}
              onClick={() => onToggleCategory(category)}
              type="button"
            >
              {ACTIVITY_CATEGORY_LABELS[category]}
            </button>
          );
        })}
      </div>
    </div>
  );
}
