// Hoopoe-owned. The user-facing Settings screen (hp-wg5p).
//
// Layout (per the bead): left rail (section navigation) + right pane
// (search + scrollable section content). Cmd+Comma (macOS-standard)
// opens; Esc closes.
//
// State model: the renderer holds an in-memory `userOverrides` +
// `projectOverrides` that mirror SettingsBridge tiers; on mutation, the
// onSave callback is invoked with the dotted key path + new value so
// the main process can persist + audit. This component does NOT call
// IPC directly — the host (route or modal) wires the props to IPC.

import * as React from "react";
import { useEffect, useMemo, useState } from "react";
import { RotateCcw, SearchX } from "lucide-react";
import {
  SETTING_DESCRIPTORS,
  SECTION_LABELS,
  SECTION_ORDER,
  groupBySections,
  resolveSettingSource,
  resolveWriteTier,
  type SettingDescriptor,
  type SettingSection,
} from "./SettingsModel.ts";
import { dimmedDescriptors, searchSettings } from "./SettingsSearch.ts";
import { SettingRow } from "./SettingRow.tsx";
import { StateSurface } from "../state-view/index.ts";

export interface SettingsScreenProps {
  /** Defaults map (per-key). */
  readonly defaults: Record<string, unknown>;
  /** Current global (user-tier) overrides. */
  readonly globalOverrides: Record<string, unknown>;
  /** Current project-tier overrides (or empty when no project active, OR
   *  when a project IS active but has no overrides yet). Do not infer
   *  project-active state from this — use `activeProjectId` (hp-sfs0). */
  readonly projectOverrides: Record<string, unknown>;
  /** Identifier of the currently active project, or `null`/`undefined`
   *  when no project context is selected. Drives the write-tier decision
   *  for `project.*` descriptors so the first save lands on the project
   *  tier instead of accidentally mutating global defaults (hp-sfs0). */
  readonly activeProjectId?: string | null;
  /** Env overrides (highest precedence). */
  readonly envOverrides?: Record<string, unknown>;
  /** Whether dev-only settings should be visible. */
  readonly devModeEnabled?: boolean;
  /** Callback when a setting changes. Host persists via IPC. */
  readonly onSave: (key: string, value: unknown, tier: "global" | "project") => void;
  /** Callback to reset a key. Host clears the value at the appropriate
   *  tier and re-broadcasts. */
  readonly onReset?: (key: string) => void;
  /** Callback when the user clicks "Restart Hoopoe" in the banner. */
  readonly onRestartRequested?: (reason: string) => void;
  /** Initial section. Default: "global". */
  readonly initialSection?: SettingSection;
}

export function SettingsScreen(props: SettingsScreenProps): React.JSX.Element {
  const {
    defaults,
    globalOverrides,
    projectOverrides,
    activeProjectId = null,
    envOverrides = {},
    devModeEnabled = false,
    onSave,
    onReset,
    onRestartRequested,
    initialSection = "global",
  } = props;
  const [activeSection, setActiveSection] = useState<SettingSection>(initialSection);
  const [query, setQuery] = useState("");
  const [restartPendingFor, setRestartPendingFor] = useState<string | null>(null);

  const visible = useMemo(
    () => SETTING_DESCRIPTORS.filter((d) => devModeEnabled || !d.devOnly),
    [devModeEnabled],
  );
  const grouped = useMemo(() => groupBySections(visible), [visible]);
  const dimSet = useMemo(() => new Set(dimmedDescriptors(query, visible).map((d) => d.key)), [
    query,
    visible,
  ]);
  const queryHits = useMemo(() => searchSettings(query, visible), [query, visible]);
  const matchedTermsByKey = useMemo(() => {
    const m = new Map<string, readonly string[]>();
    for (const hit of queryHits) m.set(hit.descriptor.key, hit.matchedTerms);
    return m;
  }, [queryHits]);
  const searchActive = query.trim().length > 0;
  const noSearchMatches = searchActive && queryHits.length === 0;
  const activeRows = grouped[activeSection];

  // Cmd+Comma to open is the host's job (registers the global shortcut);
  // here we honor Esc-to-close via the host-wired handler. Keyboard nav
  // for the section rail uses standard tab order.

  // When a non-empty query lands, jump to the section of the first hit
  // so the user sees results immediately.
  useEffect(() => {
    if (query.trim().length === 0 || queryHits.length === 0) return;
    const firstHitSection = queryHits[0]?.descriptor.section;
    if (firstHitSection && firstHitSection !== activeSection) {
      setActiveSection(firstHitSection);
    }
  }, [query, queryHits, activeSection]);

  const handleChange = (descriptor: SettingDescriptor, value: unknown): void => {
    onSave(descriptor.key, value, resolveWriteTier(descriptor, activeProjectId));
    if (descriptor.restartRequired) {
      setRestartPendingFor(descriptor.key);
    }
  };

  const handleReset = (descriptor: SettingDescriptor): void => {
    if (!onReset) return;
    onReset(descriptor.key);
    if (descriptor.restartRequired) {
      setRestartPendingFor(descriptor.key);
    }
  };

  return (
    <div className="hh-settings-screen" role="dialog" aria-label="Settings">
      <aside className="hh-settings-rail" aria-label="Settings sections">
        {SECTION_ORDER.map((section) => {
          const count = grouped[section].length;
          const active = section === activeSection;
          return (
            <button
              key={section}
              type="button"
              className="hh-settings-rail__btn"
              data-active={active ? "true" : "false"}
              onClick={() => setActiveSection(section)}
              aria-current={active ? "page" : undefined}
            >
              <span className="hh-settings-rail__label">{SECTION_LABELS[section]}</span>
              <span className="hh-settings-rail__count" aria-label={`${count} settings`}>
                {count}
              </span>
            </button>
          );
        })}
      </aside>

      <section className="hh-settings-pane" aria-labelledby="hh-settings-section-heading">
        <div className="hh-settings-pane__head">
          <input
            type="search"
            className="hh-settings-search"
            placeholder="⌘F  Search settings"
            value={query}
            onChange={(e) => setQuery(e.target.value)}
            aria-label="Search settings"
          />
        </div>

        {restartPendingFor ? (
          <div className="hh-settings-restart-banner" role="status" aria-live="polite">
            <span>Restart Hoopoe to apply the change to “{restartPendingFor}”.</span>
            {onRestartRequested ? (
              <button
                type="button"
                className="hh-settings-restart-banner__btn"
                onClick={() => onRestartRequested("setting-changed:" + restartPendingFor)}
              >
                Restart now
              </button>
            ) : null}
          </div>
        ) : null}

        <h2 id="hh-settings-section-heading" className="hh-settings-pane__heading">
          {SECTION_LABELS[activeSection]}
        </h2>

        <div
          className="hh-settings-pane__rows"
          role="list"
          aria-live="polite"
          data-search-active={searchActive ? "true" : "false"}
        >
          {noSearchMatches ? (
            <StateSurface
              variant="empty"
              density="compact"
              icon={<SearchX size={18} strokeWidth={2.1} />}
              title="No settings match"
              description="Try a different setting name, section, or policy keyword."
              details={["Search checks labels, descriptions, keywords, and section names."]}
              actions={[
                {
                  label: "Clear search",
                  icon: <RotateCcw size={13} strokeWidth={2.1} />,
                  onClick: () => setQuery(""),
                  variant: "primary",
                },
              ]}
              testId="settings-search-empty"
            />
          ) : activeRows.length === 0 ? (
            <StateSurface
              variant="empty"
              density="compact"
              title="No settings in this section"
              description="This section has no visible settings with the current mode."
              details={["Enable development mode to see dev-only settings when appropriate."]}
              testId="settings-section-empty"
            />
          ) : activeRows.map((descriptor) => {
            const resolution = resolveSettingSource(
              defaults,
              globalOverrides,
              projectOverrides,
              envOverrides,
              descriptor.key,
            );
            const dimmed = dimSet.has(descriptor.key);
            const matchedTerms = matchedTermsByKey.get(descriptor.key);
            return (
              <SettingRow
                key={descriptor.key}
                descriptor={descriptor}
                resolution={resolution}
                onChange={(value) => handleChange(descriptor, value)}
                dimmed={dimmed}
                {...(onReset ? { onReset: () => handleReset(descriptor) } : {})}
                {...(matchedTerms ? { highlightTerms: matchedTerms } : {})}
              />
            );
          })}
        </div>
      </section>
    </div>
  );
}
