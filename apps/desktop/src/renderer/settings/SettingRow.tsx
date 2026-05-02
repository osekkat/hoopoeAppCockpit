// Hoopoe-owned. One row of the Settings screen (hp-wg5p).
//
// Renders a setting's label / widget / description / source-tier badge /
// reset-to-default control / audited-badge / restart-required badge,
// gated on the descriptor + current resolved source.

import * as React from "react";
import { useId, useState } from "react";
import {
  type SettingDescriptor,
  type SourceResolution,
  type SettingWidgetKind,
} from "./SettingsModel.ts";

export interface SettingRowProps<T = unknown> {
  readonly descriptor: SettingDescriptor<T>;
  readonly resolution: SourceResolution<T>;
  readonly onChange: (value: T) => void;
  readonly onReset?: () => void;
  /** Subtle dim when search is active and this row didn't match. */
  readonly dimmed?: boolean;
  /** Highlighted terms inline (lowercased). */
  readonly highlightTerms?: readonly string[];
}

const sourceLabel: Record<SourceResolution<unknown>["tier"], string> = {
  default: "Default",
  global: "Global",
  project: "Project",
  env: "Env override",
};

export function SettingRow<T>(props: SettingRowProps<T>): React.JSX.Element {
  const { descriptor, resolution, onChange, onReset, dimmed, highlightTerms } = props;
  const id = useId();
  const [error, setError] = useState<string | null>(null);

  const handle = (next: T): void => {
    if (descriptor.validate) {
      const err = descriptor.validate(next);
      if (err) {
        setError(err);
        return;
      }
    }
    setError(null);
    onChange(next);
  };

  return (
    <div
      className="hh-settings-row"
      data-key={descriptor.key}
      data-dimmed={dimmed ? "true" : "false"}
      data-audited={descriptor.audited ? "true" : "false"}
      data-restart={descriptor.restartRequired ? "true" : "false"}
      role="group"
      aria-labelledby={`${id}-label`}
    >
      <div className="hh-settings-row__head">
        <label className="hh-settings-row__label" htmlFor={`${id}-widget`} id={`${id}-label`}>
          {highlight(descriptor.label, highlightTerms ?? [])}
        </label>
        <div className="hh-settings-row__badges">
          {descriptor.audited ? (
            <span className="hh-badge hh-badge--audit" aria-label="Audited on change">
              Audited
            </span>
          ) : null}
          {descriptor.restartRequired ? (
            <span className="hh-badge hh-badge--restart" aria-label="Restart required after change">
              Restart required
            </span>
          ) : null}
          {descriptor.devOnly ? (
            <span className="hh-badge hh-badge--dev" aria-label="Developer-only">
              Dev
            </span>
          ) : null}
        </div>
      </div>
      <div className="hh-settings-row__widget">
        {renderWidget(descriptor.widget, descriptor, resolution.value, handle, `${id}-widget`)}
        {onReset ? (
          <button
            type="button"
            className="hh-settings-row__reset"
            onClick={onReset}
            aria-label={`Reset ${descriptor.label} to default`}
            title="Reset to default"
            disabled={resolution.tier === "default"}
          >
            ↺
          </button>
        ) : null}
      </div>
      <div className="hh-settings-row__meta">
        <span className="hh-settings-row__desc">
          {highlight(descriptor.description, highlightTerms ?? [])}
        </span>
        <span className="hh-settings-row__source" aria-label="Resolved source">
          Source: {sourceLabel[resolution.tier]}
        </span>
      </div>
      {error ? (
        <p className="hh-settings-row__error" role="alert">
          {error}
        </p>
      ) : null}
    </div>
  );
}

function renderWidget<T>(
  kind: SettingWidgetKind,
  descriptor: SettingDescriptor<T>,
  current: T,
  onChange: (next: T) => void,
  widgetId: string,
): React.JSX.Element {
  switch (kind) {
    case "toggle":
      return (
        <input
          id={widgetId}
          type="checkbox"
          className="hh-settings-row__toggle"
          checked={Boolean(current)}
          onChange={(e) => onChange((e.target.checked as unknown) as T)}
        />
      );
    case "enum":
      return (
        <select
          id={widgetId}
          className="hh-settings-row__select"
          value={(current as unknown) as string}
          onChange={(e) => onChange((e.target.value as unknown) as T)}
        >
          {(descriptor.options ?? []).map((opt) => (
            <option key={String(opt.value)} value={(opt.value as unknown) as string}>
              {opt.label}
            </option>
          ))}
        </select>
      );
    case "number":
      return (
        <input
          id={widgetId}
          type="number"
          className="hh-settings-row__number"
          value={Number(current)}
          onChange={(e) => onChange((Number(e.target.value) as unknown) as T)}
        />
      );
    case "text":
      return (
        <input
          id={widgetId}
          type="text"
          className="hh-settings-row__text"
          value={String(current)}
          onChange={(e) => onChange((e.target.value as unknown) as T)}
        />
      );
    case "path":
      return (
        <input
          id={widgetId}
          type="text"
          className="hh-settings-row__path"
          value={String(current)}
          onChange={(e) => onChange((e.target.value as unknown) as T)}
          spellCheck={false}
        />
      );
    case "json":
      return (
        <textarea
          id={widgetId}
          className="hh-settings-row__json"
          value={JSON.stringify(current, null, 2)}
          rows={6}
          spellCheck={false}
          onChange={(e) => {
            try {
              onChange(JSON.parse(e.target.value) as T);
            } catch {
              // Validation handled in handle() — this onChange runs only
              // when JSON parses; otherwise the row's validate path will
              // catch it on commit.
            }
          }}
        />
      );
    case "readonly":
      return (
        <span id={widgetId} className="hh-settings-row__readonly">
          {String(current)}
        </span>
      );
  }
}

/** Highlight terms inline. Case-insensitive substring match; wraps each
 *  match in a `<mark>`. Stable: returns a single fragment. */
function highlight(text: string, terms: readonly string[]): React.ReactElement {
  if (terms.length === 0) return <>{text}</>;
  let chunks: Array<string | { mark: string }> = [text];
  for (const term of terms) {
    if (!term) continue;
    const next: typeof chunks = [];
    for (const chunk of chunks) {
      if (typeof chunk !== "string") {
        next.push(chunk);
        continue;
      }
      const lc = chunk.toLowerCase();
      let i = 0;
      while (i < chunk.length) {
        const found = lc.indexOf(term, i);
        if (found < 0) {
          next.push(chunk.slice(i));
          break;
        }
        if (found > i) next.push(chunk.slice(i, found));
        next.push({ mark: chunk.slice(found, found + term.length) });
        i = found + term.length;
      }
    }
    chunks = next;
  }
  return (
    <>
      {chunks.map((c, i) =>
        typeof c === "string" ? (
          <span key={i}>{c}</span>
        ) : (
          <mark key={i} className="hh-settings-row__mark">
            {c.mark}
          </mark>
        ),
      )}
    </>
  );
}
